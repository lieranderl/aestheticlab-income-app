package web_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// withFilterCookies adds the remembered-filter cookies a response set to
// the session cookies, as a browser would.
func withFilterCookies(session []*http.Cookie, res *httptest.ResponseRecorder) []*http.Cookie {
	out := append([]*http.Cookie(nil), session...)
	for _, c := range res.Result().Cookies() {
		if strings.HasPrefix(c.Name, "kassa_filter_") && c.MaxAge >= 0 {
			out = append(out, c)
		}
	}
	return out
}

func filterCookie(res *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, c := range res.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func TestHistoryTabsKeepPeriodAndPerson(t *testing.T) {
	s := newSite(t)
	anna := s.login("anna", annaPass)
	page := s.get("/admin/history?from=2026-11-01&to=2026-11-02&person="+s.olga.ID+"&category=services", anna)
	wantPage(t, "records", page, http.StatusOK,
		`href="/admin/history/movements?from=2026-11-01&amp;person=`+s.olga.ID+`&amp;to=2026-11-02"`)

	page = s.get("/admin/history/movements?from=2026-11-01&to=2026-11-02&person="+s.olga.ID+"&type=expense", anna)
	wantPage(t, "movements", page, http.StatusOK,
		`href="/admin/history?from=2026-11-01&amp;person=`+s.olga.ID+`&amp;to=2026-11-02"`)
}

func TestHistoryRemembersLastFilterPerTab(t *testing.T) {
	s := newSite(t)
	anna := s.login("anna", annaPass)
	res := s.get("/admin/history?from=2026-11-01&to=2026-11-02&person="+s.olga.ID+"&category=services", anna)
	if filterCookie(res, "kassa_filter_records") == nil {
		t.Fatal("records filter not remembered")
	}
	jar := withFilterCookies(anna, res)

	// Opening «История» from the menu shows the last filter.
	wantPage(t, "bare records", s.get("/admin/history", jar), http.StatusOK,
		"Убрать фильтр: Ольга", "Убрать фильтр: Услуги", "01.11.2026 — 02.11.2026")

	// Movements remembers its own type, and the tab back to records keeps
	// the records category.
	res = s.get("/admin/history/movements?from=2026-11-01&to=2026-11-02&person="+s.olga.ID+"&type=expense", jar)
	jar = withFilterCookies(jar, res)
	wantPage(t, "movements tab back", res, http.StatusOK,
		`href="/admin/history?category=services&amp;from=2026-11-01&amp;person=`+s.olga.ID+`&amp;to=2026-11-02"`)
	wantPage(t, "bare movements", s.get("/admin/history/movements", jar), http.StatusOK, "Убрать фильтр: Расход")

	// «Сбросить» forgets it.
	res = s.get("/admin/history?reset=1", jar)
	if c := filterCookie(res, "kassa_filter_records"); c == nil || c.MaxAge >= 0 {
		t.Fatal("reset keeps the remembered filter")
	}
	if strings.Contains(res.Body.String(), "Убрать фильтр: Ольга") {
		t.Fatal("reset still filters")
	}

	// A filter someone else left on the same phone does not apply.
	other := append(anna, &http.Cookie{ // #nosec G124 -- a request cookie in a test
		Name: "kassa_filter_records", Value: "someone-else|person=" + s.olga.ID})
	if strings.Contains(s.get("/admin/history", other).Body.String(), "Убрать фильтр: Ольга") {
		t.Fatal("filter leaks to another person")
	}
}

func TestAuditRemembersLastFilter(t *testing.T) {
	s := newSite(t)
	anna := s.login("anna", annaPass)
	res := s.get("/admin/audit?from=2026-11-01&to=2026-11-20&entity=report", anna)
	jar := withFilterCookies(anna, res)
	wantPage(t, "bare audit", s.get("/admin/audit", jar), http.StatusOK, "Убрать фильтр: Отчёт", "01.11.2026 — 20.11.2026")
}

func TestHistoryForgetsAnInvalidRememberedFilter(t *testing.T) {
	s := newSite(t)
	anna := s.login("anna", annaPass)
	bad := append(anna, &http.Cookie{ // #nosec G124 -- a request cookie in a test
		Name: "kassa_filter_records", Value: s.anna.ID + "|person=nobody"})
	res := s.get("/admin/history", bad)
	if c := filterCookie(res, "kassa_filter_records"); c == nil || c.MaxAge >= 0 {
		t.Fatalf("invalid filter kept: %d", res.Code)
	}
}
