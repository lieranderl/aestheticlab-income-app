package web_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/lieranderl/aestheticlab-income-app/internal/domain"
	"github.com/lieranderl/aestheticlab-income-app/internal/web"
)

func TestEveryAdminRouteRequiresAuthenticationAndAdminRole(t *testing.T) {
	s := newSite(t)
	s.withTerms(s.olga, 7000)
	s.withTerms(s.anna, 5000)

	olgaCookie := s.login("olga", olgaPass)
	adminCookie := s.login("anna", annaPass)

	// Save a day report so /admin/reports/{uid}/{date} has a real target
	s.saveDay(s.olga, 10, map[string]domain.Cents{"services": 5000}, map[string]domain.Cents{"cash": 5000})

	routes := []struct {
		name   string
		method string
		path   string
		body   url.Values
	}{
		{name: "more screen", method: http.MethodGet, path: "/more"},
		{name: "admin day", method: http.MethodGet, path: "/admin/day?date=2026-11-10"},
		{name: "admin month", method: http.MethodGet, path: "/admin/month?m=2026-11"},
		{name: "admin month close page", method: http.MethodGet, path: "/admin/month/2026-10/close"},
		{name: "admin month close post", method: http.MethodPost, path: "/admin/month/2026-10/close", body: url.Values{"revision": {"1"}}},
		{name: "admin month reopen page", method: http.MethodGet, path: "/admin/month/2026-10/reopen"},
		{name: "admin month reopen post", method: http.MethodPost, path: "/admin/month/2026-10/reopen", body: url.Values{"revision": {"1"}}},
		{name: "admin kassa", method: http.MethodGet, path: "/admin/kassa"},
		{name: "admin new movement page", method: http.MethodGet, path: "/admin/movements/new"},
		{name: "admin movement post", method: http.MethodPost, path: "/admin/movements", body: url.Values{"amount": {"10"}}},
		{name: "admin movement confirm", method: http.MethodPost, path: "/admin/movements/nonexistent/confirm", body: url.Values{"version": {"1"}}},
		{name: "admin recount page", method: http.MethodGet, path: "/admin/accounts/kassa_cash/recount"},
		{name: "admin recount post", method: http.MethodPost, path: "/admin/accounts/kassa_cash/recount", body: url.Values{"counted": {"100"}}},
		{name: "admin history", method: http.MethodGet, path: "/admin/history"},
		{name: "admin movement history", method: http.MethodGet, path: "/admin/history/movements"},
		{name: "admin audit log", method: http.MethodGet, path: "/admin/audit"},
		{name: "admin report edit page", method: http.MethodGet, path: "/admin/reports/" + s.olga.ID + "/2026-11-10"},
		{name: "admin report edit post", method: http.MethodPost, path: "/admin/reports/" + s.olga.ID + "/2026-11-10", body: url.Values{"services": {"50"}}},
		{name: "admin staff list", method: http.MethodGet, path: "/admin/users"},
		{name: "admin new staff page", method: http.MethodGet, path: "/admin/users/new"},
		{name: "admin create staff post", method: http.MethodPost, path: "/admin/users", body: url.Values{"username": {"newuser"}}},
		{name: "admin edit staff page", method: http.MethodGet, path: "/admin/users/" + s.olga.ID},
		{name: "admin update staff post", method: http.MethodPost, path: "/admin/users/" + s.olga.ID, body: url.Values{"name": {"Ольга 2"}, "active": {"1"}, "reporter": {"1"}}},
		{name: "admin reset password post", method: http.MethodPost, path: "/admin/users/" + s.olga.ID + "/password"},
		{name: "admin set terms post", method: http.MethodPost, path: "/admin/users/" + s.olga.ID + "/terms", body: url.Values{"selfPct": {"70"}}},
		{name: "admin trends placeholder", method: http.MethodGet, path: "/admin/trends"},
		{name: "admin categories placeholder", method: http.MethodGet, path: "/admin/categories"},
		{name: "admin export placeholder", method: http.MethodGet, path: "/admin/export"},
		{name: "admin unknown route catch-all", method: http.MethodGet, path: "/admin/nonexistent-subpath"},
	}

	for _, rt := range routes {
		t.Run(rt.name, func(t *testing.T) {
			// 1. Unauthenticated request must redirect to /login.
			var unauthRes *httptest.ResponseRecorder
			if rt.method == http.MethodGet {
				unauthRes = s.get(rt.path, nil)
			} else {
				unauthRes = s.post(rt.path, rt.body, nil)
			}

			if unauthRes.Code != http.StatusSeeOther {
				t.Errorf("unauthenticated %s %s: status = %d, want %d", rt.method, rt.path, unauthRes.Code, http.StatusSeeOther)
			}
			if loc := unauthRes.Header().Get("Location"); loc != "/login" {
				t.Errorf("unauthenticated %s %s: Location = %q, want /login", rt.method, rt.path, loc)
			}

			// 2. Non-admin authenticated user must receive 403 Forbidden with «Нет доступа».
			var staffRes *httptest.ResponseRecorder
			if rt.method == http.MethodGet {
				staffRes = s.get(rt.path, olgaCookie)
			} else {
				staffRes = s.post(rt.path, rt.body, olgaCookie)
			}

			if staffRes.Code != http.StatusForbidden {
				t.Errorf("staff %s %s: status = %d, want %d (Forbidden)", rt.method, rt.path, staffRes.Code, http.StatusForbidden)
			}

			// 3. Admin user must NOT receive 403 Forbidden.
			var adminRes *httptest.ResponseRecorder
			if rt.method == http.MethodGet {
				adminRes = s.get(rt.path, adminCookie)
			} else {
				adminRes = s.post(rt.path, rt.body, adminCookie)
			}

			if adminRes.Code == http.StatusForbidden {
				t.Errorf("admin %s %s: unexpectedly got 403 Forbidden", rt.method, rt.path)
			}
		})
	}
}

func TestSecurityHeadersAndCSPReview(t *testing.T) {
	// HTTPS configuration (production)
	hSecure := handler(t, web.Config{BaseURL: "https://kassa.aestheticlab.be"})
	resSecure := get(t, hSecure, "/readyz")

	headers := resSecure.Header()

	// Verify all strict security headers
	if ct := headers.Get("X-Content-Type-Options"); ct != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", ct)
	}
	if fo := headers.Get("X-Frame-Options"); fo != "DENY" {
		t.Errorf("X-Frame-Options = %q, want DENY", fo)
	}
	if rp := headers.Get("Referrer-Policy"); rp != "strict-origin-when-cross-origin" {
		t.Errorf("Referrer-Policy = %q, want strict-origin-when-cross-origin", rp)
	}
	if hsts := headers.Get("Strict-Transport-Security"); !strings.Contains(hsts, "max-age=") {
		t.Errorf("HSTS header missing or invalid over HTTPS: %q", hsts)
	}
	if perm := headers.Get("Permissions-Policy"); !strings.Contains(perm, "camera=()") || !strings.Contains(perm, "microphone=()") {
		t.Errorf("Permissions-Policy missing expected restrictions: %q", perm)
	}

	// Verify exact CSP directives
	csp := headers.Get("Content-Security-Policy")
	expectedDirectives := []string{
		"default-src 'self'",
		"script-src 'self' 'unsafe-eval'",
		"style-src 'self'",
		"img-src 'self' data:",
		"font-src 'self'",
		"connect-src 'self'",
		"object-src 'none'",
		"base-uri 'self'",
		"frame-ancestors 'none'",
		"form-action 'self'",
		"manifest-src 'self'",
	}
	for _, dir := range expectedDirectives {
		if !strings.Contains(csp, dir) {
			t.Errorf("CSP missing directive %q: got %q", dir, csp)
		}
	}

	// HTTP configuration (local development) should omit HSTS
	hInsecure := handler(t, web.Config{BaseURL: "http://localhost:8080"})
	resInsecure := get(t, hInsecure, "/readyz")
	if hsts := resInsecure.Header().Get("Strict-Transport-Security"); hsts != "" {
		t.Errorf("HSTS should not be set over HTTP, got %q", hsts)
	}
}
