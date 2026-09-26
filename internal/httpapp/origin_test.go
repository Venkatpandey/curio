package httpapp

import (
	"io"
	"log/slog"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"curio/internal/config"
	"curio/internal/store"
)

func TestLANAndProxyOriginProtection(t *testing.T) {
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	handler, err := New(db, config.Config{AccessPassword: testPassword, GuestEnabled: true, BaseURL: "https://curio.example.com"}, slog.New(slog.NewTextHandler(io.Discard, nil)), "test")
	if err != nil {
		t.Fatal(err)
	}
	get := httptest.NewRequest("GET", "http://192.168.0.10:8090/discover?kind=fact&id=octopus", nil)
	page := httptest.NewRecorder()
	handler.ServeHTTP(page, get)
	if page.Code != 200 || page.Header().Get("Referrer-Policy") != "same-origin" {
		t.Fatal("LAN form policy", page.Code)
	}
	csrf := csrfPattern.FindStringSubmatch(page.Body.String())[1]
	cookies := page.Result().Cookies()
	for _, tc := range []struct {
		name, origin, fetchSite, host, forwarded, token string
		status                                          int
	}{
		{"LAN browser without fetch metadata", "http://192.168.0.10:8090", "", "192.168.0.10:8090", "", csrf, 200},
		{"explicit proxy origin with rewritten host", "https://curio.example.com", "", "curio:8080", "", csrf, 200},
		{"explicit proxy origin with cross-site metadata", "https://curio.example.com", "cross-site", "curio:8080", "", csrf, 200},
		{"null origin remains blocked", "null", "", "192.168.0.10:8090", "", csrf, 403},
		{"other site remains blocked", "https://evil.example", "", "curio:8080", "", csrf, 403},
		{"spoofed forwarded host remains blocked", "https://evil.example", "", "curio:8080", "evil.example", csrf, 403},
		{"similar host remains blocked", "https://curio.example.com.evil.example", "", "curio:8080", "", csrf, 403},
		{"trusted origin still needs CSRF", "https://curio.example.com", "", "curio:8080", "", "bad", 403},
		{"LAN origin still needs CSRF", "http://192.168.0.10:8090", "", "192.168.0.10:8090", "", "bad", 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			form := url.Values{"id": {"octopus"}, "kind": {"fact"}, "answer": {"1"}, "csrf": {tc.token}}
			req := httptest.NewRequest("POST", "http://curio:8080/play", strings.NewReader(form.Encode()))
			req.Host = tc.host
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.Header.Set("Origin", tc.origin)
			if tc.fetchSite != "" {
				req.Header.Set("Sec-Fetch-Site", tc.fetchSite)
			}
			if tc.forwarded != "" {
				req.Header.Set("X-Forwarded-Host", tc.forwarded)
			}
			for _, cookie := range cookies {
				req.AddCookie(cookie)
			}
			res := httptest.NewRecorder()
			handler.ServeHTTP(res, req)
			if res.Code != tc.status {
				t.Fatalf("status %d, want %d: %s", res.Code, tc.status, res.Body.String())
			}
		})
	}
}
