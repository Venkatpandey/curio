package httpapp

import (
	"bytes"
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"math"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"curio/internal/auth"
	"curio/internal/config"
	"curio/internal/content"
	"curio/internal/store"
	"curio/web"
)

var categories = []string{"Space", "Geography", "Nature", "Animals", "History", "Science", "Technology", "Food", "Language", "Useless knowledge"}
var usernamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{1,31}$`)

type App struct {
	store                 *store.Store
	config                config.Config
	templates             *template.Template
	provider              content.Provider
	logger                *slog.Logger
	version, passwordHash string
	limits                limiter
	authSlots             chan struct{}
}
type page struct {
	Title, View, CSRF, Error, Notice, Username, Kind, Version string
	User                                                      *store.User
	GuestEnabled                                              bool
	Settings                                                  store.Settings
	Categories                                                []string
	Item                                                      content.Item
	ContentCount                                              int
	Distance                                                  string
	Quiz                                                      content.Quiz
	Attempt                                                   *store.Attempt
	Progress                                                  store.Progress
	Revealed                                                  bool
	Reaction, FactCategory                                    string
	Favorite                                                  bool
	Collection                                                []store.CollectionEntry
	CollectionTab                                             string
	PageNumber, PreviousPage, NextPage                        int
	HasMore                                                   bool
	Preferences                                               []store.Preference
}
type requestState struct {
	session *store.Session
	csrf    string
}
type stateKey struct{}

func New(db *store.Store, c config.Config, logger *slog.Logger, version string) (http.Handler, error) {
	templates, err := template.New("").Funcs(template.FuncMap{"contains": slices.Contains[[]string, string], "coordinate": func(n sql.NullFloat64) string {
		if !n.Valid {
			return ""
		}
		return strconv.FormatFloat(n.Float64, 'f', -1, 64)
	}}).ParseFS(web.Files, "templates/*.html")
	if err != nil {
		return nil, err
	}
	hash, err := auth.HashPassword(c.AccessPassword)
	if err != nil {
		return nil, err
	}
	if err = db.SetAccessKey(context.Background(), c.AccessPassword); err != nil {
		return nil, err
	}
	if err = db.SeedContent(context.Background()); err != nil {
		return nil, err
	}
	a := &App{store: db, config: c, templates: templates, provider: db, logger: logger, version: version, passwordHash: hash, authSlots: make(chan struct{}, 2)}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", a.health)
	mux.HandleFunc("GET /media/{key}", a.media)
	static, _ := fs.Sub(web.Files, "static")
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(static))))
	mux.HandleFunc("GET /sw.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		body, _ := web.Files.ReadFile("static/sw.js")
		w.Write(body)
	})
	mux.Handle("GET /{$}", a.withState(a.home))
	mux.Handle("GET /enter", a.withState(a.enterPage))
	mux.Handle("POST /enter", a.withState(a.enter))
	mux.Handle("POST /logout", a.withState(a.logout))
	mux.Handle("GET /settings", a.withState(a.settingsPage))
	mux.Handle("POST /settings", a.withState(a.settingsSave))
	mux.Handle("GET /discover", a.withState(a.discover))
	mux.Handle("POST /play", a.withState(a.play))
	mux.Handle("POST /react", a.withState(a.react))
	mux.Handle("GET /collection", a.withState(a.collection))
	mux.Handle("POST /favorite", a.withState(a.favorite))
	mux.Handle("POST /reset-mix", a.withState(a.resetMix))
	mux.Handle("/", a.withState(func(w http.ResponseWriter, r *http.Request) {
		a.render(w, r, http.StatusNotFound, page{Title: "Not found", View: "error", Error: "This page wandered off. Head home to find something else."})
	}))
	return a.headers(http.NewCrossOriginProtection().Handler(mux)), nil
}
func (a *App) cookieName(name string) string {
	if a.config.SecureCookies() {
		return "__Host-curio_" + name
	}
	return "curio_" + name
}
func (a *App) cookie(w http.ResponseWriter, name, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{Name: a.cookieName(name), Value: value, Path: "/", MaxAge: maxAge, HttpOnly: true, Secure: a.config.SecureCookies(), SameSite: http.SameSiteLaxMode})
}
func (a *App) withState(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		state := requestState{}
		if cookie, err := r.Cookie(a.cookieName("session")); err == nil {
			session, err := a.store.Session(r.Context(), cookie.Value)
			if err == nil {
				state.session = &session
				state.csrf = session.CSRF
			} else if !errors.Is(err, sql.ErrNoRows) {
				a.fail(w, r, err)
				return
			} else {
				a.cookie(w, "session", "", -1)
			}
		}
		if state.csrf == "" {
			if c, err := r.Cookie(a.cookieName("csrf")); err == nil && len(c.Value) == 43 {
				state.csrf = c.Value
			} else {
				state.csrf = auth.Token()
				a.cookie(w, "csrf", state.csrf, 30*24*60*60)
			}
		}
		r = r.WithContext(context.WithValue(r.Context(), stateKey{}, state))
		if r.Method == http.MethodPost {
			r.Body = http.MaxBytesReader(w, r.Body, 8192)
			if err := r.ParseForm(); err != nil {
				http.Error(w, "Form too large or invalid.", http.StatusBadRequest)
				return
			}
			if subtle.ConstantTimeCompare([]byte(r.PostForm.Get("csrf")), []byte(state.csrf)) != 1 {
				a.render(w, r, http.StatusForbidden, page{Title: "Please retry", View: "error", Error: "This form expired. Reload the page and try again."})
				return
			}
		}
		next(w, r)
	})
}
func stateOf(r *http.Request) requestState {
	state, _ := r.Context().Value(stateKey{}).(requestState)
	return state
}
func (a *App) render(w http.ResponseWriter, r *http.Request, status int, p page) {
	state := stateOf(r)
	p.CSRF = state.csrf
	p.GuestEnabled = a.config.GuestEnabled
	p.Version = a.version
	p.Categories = categories
	if state.session != nil {
		p.User = &state.session.User
		var progressErr error
		p.Progress, progressErr = a.store.Progress(r.Context(), p.User.ID, time.Now())
		if progressErr != nil {
			a.logger.Error("load progress", "error", progressErr)
			http.Error(w, "Could not load progress.", 500)
			return
		}
		if p.Settings.Theme == "" {
			p.Settings = p.User.Settings
		}
	}
	if p.Settings.Theme == "" {
		p.Settings.Theme = "dark"
	}
	var buf bytes.Buffer
	if err := a.templates.ExecuteTemplate(&buf, "layout", p); err != nil {
		a.logger.Error("template render failed", "error", err)
		http.Error(w, "Could not display this page.", 500)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	w.Write(buf.Bytes())
}
func (a *App) fail(w http.ResponseWriter, r *http.Request, err error) {
	a.logger.Error("request failed", "path", r.URL.Path, "error", err)
	a.render(w, r, 500, page{Title: "Something went wrong", View: "error", Error: "Curio could not finish that request. Please try again."})
}
func (a *App) requireUser(w http.ResponseWriter, r *http.Request) *store.User {
	state := stateOf(r)
	if state.session == nil {
		http.Redirect(w, r, "/enter", http.StatusSeeOther)
		return nil
	}
	return &state.session.User
}
func (a *App) mayBrowse(w http.ResponseWriter, r *http.Request) bool {
	if a.config.GuestEnabled || stateOf(r).session != nil {
		return true
	}
	http.Redirect(w, r, "/enter", http.StatusSeeOther)
	return false
}
func (a *App) home(w http.ResponseWriter, r *http.Request) {
	if !a.mayBrowse(w, r) {
		return
	}
	count, err := a.store.ContentCount(r.Context())
	if err != nil {
		a.fail(w, r, err)
		return
	}
	a.render(w, r, 200, page{Title: "A little more curious", View: "home", ContentCount: count})
}
func (a *App) enterPage(w http.ResponseWriter, r *http.Request) {
	if stateOf(r).session != nil {
		http.Redirect(w, r, "/", 303)
		return
	}
	a.render(w, r, 200, page{Title: "Come on in", View: "enter"})
}
func (a *App) enter(w http.ResponseWriter, r *http.Request) {
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		ip = r.RemoteAddr
	}
	p := page{Title: "Come on in", View: "enter", Username: strings.TrimSpace(r.PostForm.Get("username"))}
	if !a.limits.allow(ip, time.Now()) {
		w.Header().Set("Retry-After", "900")
		p.Error = "Too many attempts. Try again in 15 minutes."
		a.render(w, r, 429, p)
		return
	}
	if !usernamePattern.MatchString(p.Username) {
		p.Error = "Use 2–32 letters, numbers, underscores or hyphens. Start with a letter or number."
		a.render(w, r, 422, p)
		return
	}
	password := r.PostForm.Get("password")
	if len(password) > 128 {
		p.Error = "That household password did not match."
		a.render(w, r, 401, p)
		return
	}
	select {
	case a.authSlots <- struct{}{}:
		defer func() { <-a.authSlots }()
	default:
		w.Header().Set("Retry-After", "2")
		p.Error = "Curio is busy. Try again in a moment."
		a.render(w, r, 429, p)
		return
	}
	if !auth.CheckPassword(a.passwordHash, password) {
		p.Error = "That household password did not match."
		a.render(w, r, 401, p)
		return
	}
	id, err := a.store.EnterUser(r.Context(), p.Username)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	if old, err := r.Cookie(a.cookieName("session")); err == nil {
		if err = a.store.DeleteSession(r.Context(), old.Value); err != nil {
			a.fail(w, r, err)
			return
		}
	}
	token, err := a.store.NewSession(r.Context(), id)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	a.cookie(w, "session", token, 30*24*60*60)
	http.Redirect(w, r, "/", 303)
}
func (a *App) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(a.cookieName("session")); err == nil {
		if err = a.store.DeleteSession(r.Context(), c.Value); err != nil {
			a.fail(w, r, err)
			return
		}
	}
	a.cookie(w, "session", "", -1)
	http.Redirect(w, r, "/enter", 303)
}
func (a *App) settingsPage(w http.ResponseWriter, r *http.Request) {
	if a.requireUser(w, r) == nil {
		return
	}
	p := page{Title: "Your corner", View: "settings"}
	if r.URL.Query().Get("saved") == "1" {
		p.Notice = "Your changes are saved."
	}
	a.render(w, r, 200, p)
}
func (a *App) settingsSave(w http.ResponseWriter, r *http.Request) {
	u := a.requireUser(w, r)
	if u == nil {
		return
	}
	v := store.Settings{DisplayName: strings.TrimSpace(r.PostForm.Get("display_name")), Theme: r.PostForm.Get("theme"), HomeLabel: strings.TrimSpace(r.PostForm.Get("home_label")), Interests: r.PostForm["interest"]}
	p := page{Title: "Your corner", View: "settings", Settings: v}
	problem := ""
	if utf8.RuneCountInString(v.DisplayName) < 1 || utf8.RuneCountInString(v.DisplayName) > 40 {
		problem = "Your display name must contain 1–40 characters."
	}
	if !slices.Contains([]string{"light", "dark", "system"}, v.Theme) {
		problem = "Choose a valid appearance."
		v.Theme = "system"
		p.Settings.Theme = "dark"
	}
	if utf8.RuneCountInString(v.HomeLabel) > 100 {
		problem = "Keep your home label under 101 characters."
	}
	lat, lon := strings.TrimSpace(r.PostForm.Get("latitude")), strings.TrimSpace(r.PostForm.Get("longitude"))
	if lat != "" || lon != "" {
		x, e1 := strconv.ParseFloat(lat, 64)
		y, e2 := strconv.ParseFloat(lon, 64)
		if e1 != nil || e2 != nil || math.IsNaN(x) || math.IsNaN(y) || math.IsInf(x, 0) || math.IsInf(y, 0) || x < -90 || x > 90 || y < -180 || y > 180 {
			problem = "Enter both coordinates: latitude −90 to 90, longitude −180 to 180."
		} else {
			v.Latitude = sql.NullFloat64{Float64: x, Valid: true}
			v.Longitude = sql.NullFloat64{Float64: y, Valid: true}
		}
	}
	slices.Sort(v.Interests)
	v.Interests = slices.Compact(v.Interests)
	for _, category := range v.Interests {
		if !slices.Contains(categories, category) {
			problem = "Choose interests from the available list."
		}
	}
	p.Settings = v
	if problem != "" {
		p.Error = problem
		a.render(w, r, 422, p)
		return
	}
	if err := a.store.SaveSettings(r.Context(), u.ID, v); err != nil {
		a.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/settings?saved=1", 303)
}
func (a *App) discover(w http.ResponseWriter, r *http.Request) {
	if !a.mayBrowse(w, r) {
		return
	}
	kind := r.URL.Query().Get("kind")
	if kind == "" {
		kind = "surprise"
	}
	if kind != "place" && kind != "fact" && kind != "surprise" {
		a.render(w, r, 400, page{Title: "Choose a discovery", View: "error", Error: "Choose a place, a fact, or a surprise from the home screen."})
		return
	}
	category := r.URL.Query().Get("category")
	if category != "" && (kind != "fact" || !slices.Contains([]string{"Space", "Animals"}, category)) {
		http.Error(w, "Unknown fact category.", 400)
		return
	}
	state := stateOf(r)
	recent := []string{}
	if cookie, err := r.Cookie(a.cookieName("seen")); err == nil && len(cookie.Value) <= 3000 {
		for _, id := range strings.Split(cookie.Value, ",") {
			if len(id) <= 100 {
				recent = append(recent, id)
			}
		}
	}
	id := r.URL.Query().Get("id")
	if id == "" {
		request := content.Request{Kind: kind, ExcludeID: r.URL.Query().Get("after"), RecentIDs: recent, Category: category}
		if state.session != nil {
			request.UserID = state.session.User.ID
		}
		item, err := a.provider.Discover(r.Context(), request)
		if err != nil {
			a.fail(w, r, err)
			return
		}
		http.Redirect(w, r, "/discover?kind="+kind+"&id="+url.QueryEscape(item.ID)+"&category="+url.QueryEscape(category), http.StatusSeeOther)
		return
	}
	item, err := a.store.Content(r.Context(), id)
	if errors.Is(err, sql.ErrNoRows) {
		a.render(w, r, 404, page{Title: "Discovery not found", View: "error", Error: "That discovery is not in this Curio collection. Choose another from home."})
		return
	}
	if err != nil {
		a.fail(w, r, err)
		return
	}
	if kind != "surprise" && item.Kind != kind {
		http.Redirect(w, r, "/discover?kind="+item.Kind+"&id="+url.QueryEscape(item.ID), 303)
		return
	}
	p := page{Title: item.Title, View: "discover", Item: item, Kind: kind, Quiz: item.Quiz(), FactCategory: category}
	if state.session != nil {
		if err = a.store.MarkSeen(r.Context(), state.session.User.ID, id); err != nil {
			a.fail(w, r, err)
			return
		}
		p.Favorite, err = a.store.Favorite(r.Context(), state.session.User.ID, id)
		if err != nil {
			a.fail(w, r, err)
			return
		}
		p.Attempt, err = a.store.Attempt(r.Context(), state.session.User.ID, id)
		if err != nil {
			a.fail(w, r, err)
			return
		}
		p.Reaction, err = a.store.Reaction(r.Context(), state.session.User.ID, id)
		if err != nil {
			a.fail(w, r, err)
			return
		}
		settings := state.session.User.Settings
		if settings.Latitude.Valid && settings.Longitude.Valid && item.Latitude != nil && item.Longitude != nil {
			p.Distance = fmt.Sprintf("%s km", formatNumber(content.DistanceKM(settings.Latitude.Float64, settings.Longitude.Float64, *item.Latitude, *item.Longitude)))
		}
	} else {
		recent = slices.DeleteFunc(recent, func(s string) bool { return s == id })
		recent = append(recent, id)
		if len(recent) > 20 {
			recent = recent[len(recent)-20:]
		}
		a.cookie(w, "seen", strings.Join(recent, ","), 30*24*60*60)
	}
	if guest, ok := r.Context().Value(guestAttemptKey{}).(*store.Attempt); ok {
		p.Attempt = guest
	}
	p.Revealed = p.Attempt != nil
	if state.session != nil {
		switch r.URL.Query().Get("saved") {
		case "saved":
			p.Notice = "Saved to your collection."
		case "removed":
			p.Notice = "Removed from favorites."
		}
	}
	a.render(w, r, 200, p)
}
func formatNumber(n int) string {
	s := strconv.Itoa(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}
func (a *App) media(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	if len(key) != 64 {
		http.NotFound(w, r)
		return
	}
	if _, err := hex.DecodeString(key); err != nil {
		http.NotFound(w, r)
		return
	}
	media, err := a.store.Media(r.Context(), key)
	if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "Photo unavailable.", 503)
		return
	}
	w.Header().Set("Content-Type", media.ContentType)
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("ETag", `"`+key+`"`)
	http.ServeContent(w, r, "photo", time.Time{}, bytes.NewReader(media.Body))
}

func (a *App) health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if err := a.store.Ping(ctx); err != nil {
		w.WriteHeader(503)
		json.NewEncoder(w).Encode(map[string]string{"status": "unavailable"})
		return
	}
	json.NewEncoder(w).Encode(map[string]string{"status": "ok", "version": a.version})
}
func (a *App) headers(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; font-src 'self'; connect-src 'self'; worker-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
		w.Header().Set("Permissions-Policy", "geolocation=(), camera=(), microphone=()")
		if a.config.SecureCookies() {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000")
		}
		start := time.Now()
		next.ServeHTTP(w, r)
		a.logger.Debug("request", "method", r.Method, "path", r.URL.Path, "duration", time.Since(start))
	})
}

type bucket struct {
	count int
	until time.Time
}
type limiter struct {
	mu     sync.Mutex
	ips    map[string]bucket
	global bucket
}

func (l *limiter) allow(ip string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.ips == nil {
		l.ips = make(map[string]bucket)
	}
	for key, b := range l.ips {
		if !now.Before(b.until) {
			delete(l.ips, key)
		}
	}
	if !now.Before(l.global.until) {
		l.global = bucket{until: now.Add(15 * time.Minute)}
	}
	b, ok := l.ips[ip]
	if !ok {
		if len(l.ips) >= 4096 {
			return false
		}
		b = bucket{until: now.Add(15 * time.Minute)}
	}
	if b.count >= 10 || l.global.count >= 120 {
		return false
	}
	b.count++
	l.global.count++
	l.ips[ip] = b
	return true
}
