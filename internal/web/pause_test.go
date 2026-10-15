package web_test

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/lieranderl/aestheticlab-income-app/internal/store"
)

func TestFinancialWritesPausedRejectsAllFinancialWrites(t *testing.T) {
	s := newSiteOptions(t, store.WithFinancialWritesPaused(true))
	s.withTerms(s.olga, 7000)
	s.withTerms(s.anna, 5000)

	z := s.login("olga", olgaPass)
	j := s.login("anna", annaPass)

	// 1. Pages show the pause notification banner.
	wantPage(t, "client day page shows pause banner", s.get("/?date=2026-11-20", z), http.StatusOK, "Запись финансовых операций временно приостановлена")
	wantPage(t, "admin kassa page shows pause banner", s.get("/admin/kassa", j), http.StatusOK, "Запись финансовых операций временно приостановлена")
	wantPage(t, "my page shows pause banner", s.get("/my?m=2026-11", z), http.StatusOK, "Запись финансовых операций временно приостановлена")
	wantPage(t, "admin month shows pause banner", s.get("/admin/month?m=2026-11", j), http.StatusOK, "Запись финансовых операций временно приостановлена")

	// 2. Staff cannot create client entries.
	clientForm := url.Values{
		"date":              {"2026-11-20"},
		"token":             {"http-pause-client-token"},
		"version":           {"0"},
		"services":          {"100"},
		"extrasDestination": {"cash"},
		"serviceCash":       {"50"},
	}
	res := s.post("/clients", clientForm, z)
	if res.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected status 422 for paused client save, got %d", res.Code)
	}
	if !containsText(res.Body.String(), "Запись финансовых операций временно приостановлена") {
		t.Fatalf("expected pause error in body, got: %s", res.Body.String())
	}

	// 3. Staff cannot record movements.
	movForm := url.Values{
		"type":   {"deposit"},
		"amount": {"50"},
		"date":   {"2026-11-20"},
		"from":   {"person:" + s.olga.ID},
		"to":     {"account:kassa_cash"},
	}
	res = s.post("/movements", movForm, z)
	if res.Code != http.StatusUnprocessableEntity && res.Code != http.StatusConflict {
		t.Fatalf("expected status 422/409 for paused movement, got %d", res.Code)
	}
	if !containsText(res.Body.String(), "Запись финансовых операций временно приостановлена") {
		t.Fatalf("expected pause error in movement body, got: %s", res.Body.String())
	}

	// 4. Admin cannot record movements.
	adminMovForm := url.Values{
		"type":   {"deposit"},
		"amount": {"50"},
		"date":   {"2026-11-20"},
		"from":   {"person:" + s.anna.ID},
		"to":     {"account:kassa_cash"},
	}
	res = s.post("/admin/movements", adminMovForm, j)
	if res.Code != http.StatusUnprocessableEntity && res.Code != http.StatusConflict {
		t.Fatalf("expected status 422/409 for admin movement, got %d", res.Code)
	}
	if !containsText(res.Body.String(), "Запись финансовых операций временно приостановлена") {
		t.Fatalf("expected pause error in admin movement body, got: %s", res.Body.String())
	}

	// 5. Admin cannot recount kassa.
	recountForm := url.Values{
		"counted": {"100"},
		"note":    {"проверка кассы"},
	}
	res = s.post("/admin/accounts/kassa_cash/recount", recountForm, j)
	if res.Code != http.StatusUnprocessableEntity && res.Code != http.StatusConflict {
		t.Fatalf("expected status 422/409 for recount, got %d", res.Code)
	}
	if !containsText(res.Body.String(), "Запись финансовых операций временно приостановлена") {
		t.Fatalf("expected pause error in recount body, got: %s", res.Body.String())
	}

	// 6. Admin cannot save daily reports.
	reportForm := specDay("1")
	res = s.post("/reports/2026-11-20", reportForm, j)
	if res.Code != http.StatusUnprocessableEntity && res.Code != http.StatusConflict {
		t.Fatalf("expected status 422/409 for daily report, got %d", res.Code)
	}

	// 7. Financial reads remain fully functional.
	wantPage(t, "admin day", s.get("/admin/day?date=2026-11-20", j), http.StatusOK, "День")
	wantPage(t, "admin month", s.get("/admin/month?m=2026-11", j), http.StatusOK, "Месяц")
	wantPage(t, "admin kassa", s.get("/admin/kassa", j), http.StatusOK, "Касса")
	wantPage(t, "my page", s.get("/my?m=2026-11", z), http.StatusOK, "Мои записи")
}

func TestFinancialWritesPausedMonthCloseAndReopen(t *testing.T) {
	s := settlementSite(t)
	cookie := s.login("anna", annaPass)

	// Fetch confirmation revision
	p := s.get("/admin/month/2026-10/close", cookie)
	revision := hiddenRevision(t, p.Body.String())

	// Pause financial writes
	s.st.SetFinancialWritesPaused(true)

	// Close attempt is blocked
	res := s.post("/admin/month/2026-10/close", url.Values{"revision": {revision}}, cookie)
	if res.Code != http.StatusUnprocessableEntity && res.Code != http.StatusConflict {
		t.Fatalf("expected 422/409 for paused month close, got %d", res.Code)
	}
	if !containsText(res.Body.String(), "Запись финансовых операций временно приостановлена") {
		t.Fatalf("expected pause error for month close, got: %s", res.Body.String())
	}

	// Unpause, close month
	s.st.SetFinancialWritesPaused(false)
	fresh := hiddenRevision(t, s.get("/admin/month/2026-10/close", cookie).Body.String())
	wantRedirect(t, "close", s.post("/admin/month/2026-10/close", url.Values{"revision": {fresh}}, cookie), "/admin/month?m=2026-10&tab=settlement&saved=1")

	// Prepare reopen
	reopenRev := hiddenRevision(t, s.get("/admin/month/2026-10/reopen", cookie).Body.String())

	// Pause financial writes again
	s.st.SetFinancialWritesPaused(true)

	// Reopen attempt is blocked
	res = s.post("/admin/month/2026-10/reopen", url.Values{"revision": {reopenRev}}, cookie)
	if res.Code != http.StatusUnprocessableEntity && res.Code != http.StatusConflict {
		t.Fatalf("expected 422/409 for paused month reopen, got %d", res.Code)
	}
	if !containsText(res.Body.String(), "Запись финансовых операций временно приостановлена") {
		t.Fatalf("expected pause error for month reopen, got: %s", res.Body.String())
	}
}

func containsText(s, sub string) bool {
	return len(s) > 0 && len(sub) > 0 && (s == sub || len(s) >= len(sub) && (stringContains(s, sub)))
}

func stringContains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
