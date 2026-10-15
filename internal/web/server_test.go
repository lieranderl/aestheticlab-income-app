package web_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"github.com/lieranderl/aestheticlab-income-app/internal/web"
)

// get serves one GET request; a recorder needs no body to close.
func get(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func handler(t *testing.T, cfg web.Config) http.Handler {
	t.Helper()
	if cfg.PasswordCost == 0 {
		cfg.PasswordCost = bcrypt.MinCost // bcrypt's default cost is slow under -race
	}
	h, err := web.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func TestTheReadinessCheckAnswersReady(t *testing.T) {
	res := get(t, handler(t, web.Config{}), "/readyz")
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.Code)
	}
	if got := res.Body.String(); got != "ready\n" {
		t.Fatalf("body = %q, want %q", got, "ready\n")
	}
}

func TestTheReadinessCheckFailsWhenTheDatabaseIsUnreachable(t *testing.T) {
	h := handler(t, web.Config{Ready: func(*http.Request) error { return errors.New("firestore down") }})
	res := get(t, h, "/readyz")
	if res.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", res.Code)
	}
	if strings.Contains(res.Body.String(), "firestore down") {
		t.Fatal("the readiness check must not reveal internal errors")
	}
}

func TestEveryResponseCarriesTheSecurityHeaders(t *testing.T) {
	res := get(t, handler(t, web.Config{BaseURL: "https://kassa.example.com"}), "/login")
	want := map[string]string{
		"Content-Security-Policy":   "default-src 'self'; script-src 'self' 'unsafe-eval'; style-src 'self'; img-src 'self' data:; font-src 'self'; connect-src 'self'; object-src 'none'; base-uri 'self'; frame-ancestors 'none'; form-action 'self'; manifest-src 'self'",
		"X-Content-Type-Options":    "nosniff",
		"X-Frame-Options":           "DENY",
		"Referrer-Policy":           "strict-origin-when-cross-origin",
		"Strict-Transport-Security": "max-age=31536000; includeSubDomains",
	}
	for name, value := range want {
		if got := res.Header().Get(name); got != value {
			t.Errorf("%s = %q, want %q", name, got, value)
		}
	}
}

func TestLocalDevelopmentOverHTTPHasNoHSTS(t *testing.T) {
	res := get(t, handler(t, web.Config{BaseURL: "http://localhost:8080"}), "/login")
	if got := res.Header().Get("Strict-Transport-Security"); got != "" {
		t.Fatalf("HSTS = %q, want none over plain HTTP", got)
	}
}

func TestTheFrontendAssetsAreServed(t *testing.T) {
	h := handler(t, web.Config{})
	for path, contentType := range map[string]string{
		"/static/app.css":     "text/css; charset=utf-8",
		"/static/datastar.js": "text/javascript; charset=utf-8",
		"/static/fonts/onest-cyrillic-wght-normal.woff2": "font/woff2",
		"/static/icons/favicon.svg":                      "image/svg+xml",
		"/static/draft.js":                               "text/javascript; charset=utf-8",
		"/static/icons/icon-192.png":                     "image/png",
		"/static/icons/icon-512.png":                     "image/png",
		"/static/icons/icon-maskable-512.png":            "image/png",
		"/static/icons/apple-touch-icon.png":             "image/png",
	} {
		res := get(t, h, path)
		if res.Code != http.StatusOK {
			t.Errorf("%s: status = %d, want 200", path, res.Code)
			continue
		}
		if got := res.Header().Get("Content-Type"); got != contentType {
			t.Errorf("%s: Content-Type = %q, want %q", path, got, contentType)
		}
	}
}

func TestTheAppInstallsToTheHomeScreen(t *testing.T) {
	h := handler(t, web.Config{})
	res := get(t, h, "/manifest.webmanifest")
	if res.Code != http.StatusOK || res.Header().Get("Content-Type") != "application/manifest+json" {
		t.Fatalf("manifest: status %d, Content-Type %q", res.Code, res.Header().Get("Content-Type"))
	}
	var m struct {
		Name            string `json:"name"`
		ShortName       string `json:"short_name"`
		StartURL        string `json:"start_url"`
		Display         string `json:"display"`
		ThemeColor      string `json:"theme_color"`
		BackgroundColor string `json:"background_color"`
		Icons           []struct {
			Src     string `json:"src"`
			Sizes   string `json:"sizes"`
			Type    string `json:"type"`
			Purpose string `json:"purpose"`
		} `json:"icons"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	if m.Name != "Aesthetic Lab — Касса" || m.ShortName != "Касса" || m.StartURL != "/" || m.Display != "standalone" ||
		m.ThemeColor != "#8b9687" || m.BackgroundColor != "#fffaf3" {
		t.Errorf("manifest = %+v", m)
	}
	purposes := map[string]bool{}
	for _, icon := range m.Icons {
		if get(t, h, icon.Src).Code != http.StatusOK || icon.Type != "image/png" {
			t.Errorf("icon %+v is not served as a PNG", icon)
		}
		purposes[icon.Sizes+" "+icon.Purpose] = true
	}
	if !purposes["192x192 any"] || !purposes["512x512 any"] || !purposes["512x512 maskable"] {
		t.Errorf("icons = %+v, want 192 and 512 plus a maskable 512", m.Icons)
	}

	page := get(t, h, "/login").Body.String()
	for _, tag := range []string{
		`<link rel="manifest" href="/manifest.webmanifest"`, `<link rel="apple-touch-icon" href="/static/icons/apple-touch-icon.png"`,
		`<meta name="apple-mobile-web-app-capable" content="yes"`, `<meta name="apple-mobile-web-app-title" content="Касса"`,
	} {
		if !strings.Contains(page, tag) {
			t.Errorf("login page lacks %s", tag)
		}
	}
}

func TestCrossSiteFormPostsAreRejected(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader("username=anna&password=x"))
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	rec := httptest.NewRecorder()
	handler(t, web.Config{}).ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

func TestUnknownPagesAreNotFound(t *testing.T) {
	if res := get(t, handler(t, web.Config{}), "/no-such-page"); res.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", res.Code)
	}
}

func TestServiceWorkerAndOfflinePageAreServed(t *testing.T) {
	h := handler(t, web.Config{})

	// 1. Service worker script is served at the root with required headers.
	sw := get(t, h, "/sw.js")
	if sw.Code != http.StatusOK {
		t.Fatalf("sw.js status = %d, want 200", sw.Code)
	}
	if ct := sw.Header().Get("Content-Type"); !strings.Contains(ct, "text/javascript") {
		t.Errorf("sw.js Content-Type = %q, want text/javascript", ct)
	}
	if allowed := sw.Header().Get("Service-Worker-Allowed"); allowed != "/" {
		t.Errorf("sw.js Service-Worker-Allowed = %q, want /", allowed)
	}
	if !strings.Contains(sw.Body.String(), "/offline") || !strings.Contains(sw.Body.String(), "kassa-v3") {
		t.Errorf("sw.js body missing expected contents: %s", sw.Body.String())
	}

	// 2. Offline fallback page is publicly served.
	offline := get(t, h, "/offline")
	if offline.Code != http.StatusOK {
		t.Fatalf("offline status = %d, want 200", offline.Code)
	}
	if ct := offline.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Errorf("offline Content-Type = %q, want text/html", ct)
	}
	body := offline.Body.String()
	if !strings.Contains(body, "Нет соединения") {
		t.Errorf("offline page missing 'Нет соединения'")
	}
	if !strings.Contains(body, "Повторить") {
		t.Errorf("offline page missing 'Повторить'")
	}

	// 3. Layout registers the service worker.
	login := get(t, h, "/login").Body.String()
	if !strings.Contains(login, "/sw.js") {
		t.Errorf("layout missing service worker registration referencing /sw.js")
	}
}
