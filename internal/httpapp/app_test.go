package httpapp

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"curio/internal/config"
	"curio/internal/store"
)

const testPassword = "one-shared-household-pass"

var csrfPattern = regexp.MustCompile(`name="csrf" value="([^"]+)"`)

type fixture struct {
	t      *testing.T
	server *httptest.Server
	db     *store.Store
}

func setup(t *testing.T, guest bool) *fixture {
	t.Helper()
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	handler, err := New(db, config.Config{AccessPassword: testPassword, GuestEnabled: guest}, slog.New(slog.NewTextHandler(io.Discard, nil)), "test")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(func() { server.Close(); db.Close() })
	return &fixture{t, server, db}
}
func (f *fixture) client() *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}
func (f *fixture) get(c *http.Client, path string) (*http.Response, string) {
	f.t.Helper()
	r, e := c.Get(f.server.URL + path)
	if e != nil {
		f.t.Fatal(e)
	}
	defer r.Body.Close()
	b, e := io.ReadAll(r.Body)
	if e != nil {
		f.t.Fatal(e)
	}
	return r, string(b)
}
func (f *fixture) post(c *http.Client, path string, v url.Values) (*http.Response, string) {
	f.t.Helper()
	r, e := c.PostForm(f.server.URL+path, v)
	if e != nil {
		f.t.Fatal(e)
	}
	defer r.Body.Close()
	b, e := io.ReadAll(r.Body)
	if e != nil {
		f.t.Fatal(e)
	}
	return r, string(b)
}
func (f *fixture) csrf(c *http.Client, path string) string {
	f.t.Helper()
	res, body := f.get(c, path)
	if res.StatusCode != 200 {
		f.t.Fatalf("GET %s: %d %s", path, res.StatusCode, body)
	}
	m := csrfPattern.FindStringSubmatch(body)
	if len(m) != 2 {
		f.t.Fatal("missing CSRF form token")
	}
	return m[1]
}
func (f *fixture) enter(c *http.Client, name string) {
	f.t.Helper()
	csrf := f.csrf(c, "/enter")
	r, b := f.post(c, "/enter", url.Values{"csrf": {csrf}, "username": {name}, "password": {testPassword}})
	if r.StatusCode != 303 || r.Header.Get("Location") != "/" {
		f.t.Fatalf("entry failed: %d %s", r.StatusCode, b)
	}
}
func TestHouseholdEntryIsolationAndLogout(t *testing.T) {
	f := setup(t, false)
	alice, bob := f.client(), f.client()
	if r, _ := f.get(alice, "/"); r.StatusCode != 303 {
		t.Fatal("guest access ignored disabled setting")
	}
	csrf := f.csrf(alice, "/enter")
	if r, _ := f.post(alice, "/enter", url.Values{"csrf": {csrf}, "username": {"Alice"}, "password": {"wrong"}}); r.StatusCode != 401 {
		t.Fatal("wrong shared password accepted")
	}
	f.enter(alice, "Alice")
	f.enter(bob, "Bob")
	csrf = f.csrf(alice, "/settings")
	r, b := f.post(alice, "/settings", url.Values{"csrf": {csrf}, "display_name": {"Alice <script>bad</script>"}, "theme": {"dark"}, "interest": {"Space", "Nature"}, "home_label": {"Berlin"}, "latitude": {"52.52"}, "longitude": {"13.405"}})
	if r.StatusCode != 303 {
		t.Fatal("settings save", r.StatusCode, b)
	}
	_, b = f.get(alice, "/settings")
	if !strings.Contains(b, "Alice &lt;script&gt;bad&lt;/script&gt;") || !strings.Contains(b, `data-theme="dark"`) {
		t.Fatal("escaped settings/theme missing")
	}
	_, b = f.get(bob, "/settings")
	if strings.Contains(b, "Berlin") || strings.Contains(b, "Alice") || !strings.Contains(b, `data-theme="dark"`) {
		t.Fatal("profile data leaked")
	}
	uri, _ := url.Parse(f.server.URL)
	cookies := alice.Jar.Cookies(uri)
	var old *http.Cookie
	for _, c := range cookies {
		if c.Name == "curio_session" {
			old = c
		}
	}
	if old == nil {
		t.Fatal("no session cookie")
	}
	if r, _ := f.post(alice, "/logout", url.Values{"csrf": {csrf}}); r.StatusCode != 303 {
		t.Fatal("logout failed")
	}
	stale := f.client()
	stale.Jar.SetCookies(uri, []*http.Cookie{old})
	if r, _ := f.get(stale, "/settings"); r.StatusCode != 303 {
		t.Fatal("revoked session still works")
	}
	f.enter(alice, "ALICE")
	_, b = f.get(alice, "/settings")
	if !strings.Contains(b, "Berlin") {
		t.Fatal("reentry lost profile")
	}
}
func TestCSRFAndBoundaryValidation(t *testing.T) {
	f := setup(t, true)
	c := f.client()
	csrf := f.csrf(c, "/enter")
	if r, _ := f.post(c, "/enter", url.Values{"username": {"Alice"}, "password": {testPassword}}); r.StatusCode != 403 {
		t.Fatal("missing CSRF accepted")
	}
	req, _ := http.NewRequest("POST", f.server.URL+"/enter", strings.NewReader(url.Values{"csrf": {csrf}, "username": {"Alice"}, "password": {testPassword}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "https://evil.example")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	r, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if r.StatusCode != 403 {
		t.Fatal("cross-origin mutation accepted")
	}
	if r, _ := f.post(c, "/enter", url.Values{"csrf": {csrf}, "username": {"<script>"}, "password": {testPassword}}); r.StatusCode != 422 {
		t.Fatal("invalid username accepted")
	}
	f.enter(c, "Alice")
	csrf = f.csrf(c, "/settings")
	for _, values := range []url.Values{
		{"display_name": {"Alice"}, "theme": {"light"}, "latitude": {"NaN"}, "longitude": {"0"}},
		{"display_name": {"Alice"}, "theme": {"light"}, "latitude": {"90"}},
		{"display_name": {"Alice"}, "theme": {"neon"}},
		{"display_name": {"Alice"}, "theme": {"light"}, "interest": {"not-a-category"}},
	} {
		values.Set("csrf", csrf)
		if r, _ := f.post(c, "/settings", values); r.StatusCode != 422 {
			t.Fatal("invalid settings accepted", values)
		}
	}
	if r, _ := f.post(c, "/settings", url.Values{"csrf": {csrf}, "display_name": {strings.Repeat("x", 10000)}}); r.StatusCode != 400 {
		t.Fatal("oversized body accepted")
	}
	for _, path := range []string{"/", "/discover?kind=place", "/discover?kind=fact", "/discover?kind=surprise"} {
		res, body := f.get(c, path)
		if res.StatusCode == 303 {
			res, body = f.get(c, res.Header.Get("Location"))
		}
		if res.StatusCode != 200 || !strings.Contains(body, "<h1>") {
			t.Fatal("page failed", path, res.StatusCode)
		}
		if res.Header.Get("Cache-Control") != "no-store" {
			t.Fatal("personal page cacheable")
		}
	}
	if r, _ := f.get(c, "/discover?kind=invalid"); r.StatusCode != 400 {
		t.Fatal("invalid kind accepted")
	}
	if r, _ := f.get(c, "/missing"); r.StatusCode != 404 {
		t.Fatal("missing path not 404")
	}
}
func TestPublicAssetsHealthAndHTTPSCookies(t *testing.T) {
	f := setup(t, true)
	c := f.client()
	for _, path := range []string{"/health", "/static/app.css", "/static/app.js", "/static/photos/tristan.jpg", "/static/icon-192.png", "/static/icon-512.png", "/static/manifest.webmanifest", "/static/offline.html", "/sw.js"} {
		r, b := f.get(c, path)
		if r.StatusCode != 200 || b == "" {
			t.Fatal("public endpoint failed", path, r.StatusCode)
		}
		if r.Header.Get("X-Content-Type-Options") != "nosniff" {
			t.Fatal("missing security headers")
		}
	}
	handler, err := New(f.db, config.Config{AccessPassword: testPassword, BaseURL: "https://curio.example.com"}, slog.Default(), "test")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "https://curio.example.com/enter", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	cookies := rec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("no CSRF cookie")
	}
	for _, cookie := range cookies {
		if !cookie.Secure || !cookie.HttpOnly || !strings.HasPrefix(cookie.Name, "__Host-") || cookie.SameSite != http.SameSiteLaxMode {
			t.Fatal("insecure HTTPS cookie")
		}
	}
	if rec.Header().Get("Strict-Transport-Security") == "" {
		t.Fatal("missing HSTS")
	}
	if err = f.db.SetAccessKey(context.Background(), "replacement-household-password"); err != nil {
		t.Fatal(err)
	}
}
func TestRateLimiter(t *testing.T) {
	var l limiter
	now := time.Now()
	for i := 0; i < 10; i++ {
		if !l.allow("127.0.0.1", now) {
			t.Fatal("blocked too early")
		}
	}
	if l.allow("127.0.0.1", now) {
		t.Fatal("did not rate limit")
	}
	if !l.allow("127.0.0.2", now) {
		t.Fatal("other IP blocked")
	}
	if !l.allow("127.0.0.1", now.Add(16*time.Minute)) {
		t.Fatal("limit did not expire")
	}
}
