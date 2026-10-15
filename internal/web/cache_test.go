package web_test

import (
	"net/http"
	"net/url"
	"testing"
)

// Signed-in pages, Datastar answers, login/logout and financial exports
// carry names and money, so no browser or proxy cache may keep them; the
// public static files stay cacheable.
func TestPrivateResponsesAreNeverCached(t *testing.T) {
	s := newSite(t)
	olga := s.login("olga", olgaPass)
	anna := s.login("anna", annaPass)
	for name, res := range map[string]interface {
		Header() http.Header
	}{
		"home":            s.get("/", olga),
		"my balance":      s.get("/my", olga),
		"admin forbidden": s.get("/admin/kassa", olga),
		"signed out":      s.get("/my", nil),
		"datastar form":   s.do(request{method: http.MethodPost, path: "/profile/password", form: url.Values{"current": {"x"}}, cookies: olga, datastar: true}),
		"export":          s.get("/admin/export/download?from=2026-11-01&to=2026-11-10", anna),
		"export page":     s.get("/admin/export", anna),
		"login":           s.post("/login", url.Values{"username": {"maria"}, "password": {mariaPass}}, nil),
		"logout":          s.post("/logout", url.Values{}, olga),
	} {
		if got := res.Header().Get("Cache-Control"); got != "private, no-store" {
			t.Errorf("%s: Cache-Control %q, want private, no-store", name, got)
		}
	}
	if got := s.get("/static/app.css", nil).Header().Get("Cache-Control"); got == "private, no-store" {
		t.Errorf("static asset: Cache-Control %q", got)
	}
}
