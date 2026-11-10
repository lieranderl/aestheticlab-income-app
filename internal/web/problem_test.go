package web_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// A refused request must never strand someone on a bare line of text: the
// installed iPhone app has no back button.
func TestRefusedRequestsKeepTheNavigation(t *testing.T) {
	s := newSite(t)
	admin := s.login("anna", annaPass)
	staff := s.login("olga", olgaPass)
	for name, tc := range map[string]struct {
		path    string
		cookies []*http.Cookie
		status  int
		text    string
	}{
		"unknown movement kind": {"/admin/movements/new?type=bogus", admin, http.StatusBadRequest, "Проверьте вид движения"},
		"staff movement kind":   {"/movements/new?type=payout", staff, http.StatusBadRequest, "Проверьте вид движения"},
		"bad trends metric":     {"/admin/trends?metric=bogus", admin, http.StatusBadRequest, "Проверьте показатель"},
		"bad export period":     {"/admin/export/download?format=xlsx&from=bogus", admin, http.StatusBadRequest, "Проверьте дату начала"},
	} {
		res := s.get(tc.path, tc.cookies)
		wantPage(t, name, res, tc.status, tc.text, `aria-label="Навигация"`, "На главную", "<title>")
		if !strings.HasPrefix(res.Header().Get("Content-Type"), "text/html") {
			t.Errorf("%s: served as %q, want a page", name, res.Header().Get("Content-Type"))
		}
	}
	back := s.do(request{method: http.MethodGet, path: "/admin/movements/new?type=bogus", cookies: admin, headers: map[string]string{"Referer": "http://example.com/admin/kassa"}})
	wantPage(t, "way back", back, http.StatusBadRequest, `href="/admin/kassa"`, "Назад")
	evil := s.do(request{method: http.MethodGet, path: "/admin/movements/new?type=bogus", cookies: admin, headers: map[string]string{"Referer": "https://evil.example/admin/kassa"}})
	if strings.Contains(evil.Body.String(), "Назад") {
		t.Fatal("a foreign referrer became the way back")
	}
	sse := s.do(request{method: http.MethodPost, path: "/admin/movements", form: url.Values{"type": {"bogus"}}, cookies: admin, datastar: true})
	wantPage(t, "in-page request", sse, http.StatusOK, "datastar-patch-elements", `id="problem-toast"`, "Проверьте вид движения")
	wantPage(t, "unknown page", s.get("/admin/nowhere", admin), http.StatusNotFound, "Страница не найдена", `aria-label="Навигация"`)
}
