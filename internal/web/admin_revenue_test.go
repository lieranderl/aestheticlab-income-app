package web_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/lieranderl/aestheticlab-income-app/internal/domain"
	"github.com/lieranderl/aestheticlab-income-app/internal/store"
)

func (s *site) saveDay(u store.User, day int, revenue, payments map[string]domain.Cents) {
	s.t.Helper()
	_, err := s.st.SaveReport(ctx, u.Actor(), store.ReportEntry{
		Person: u.ID, Date: domain.Date{Year: 2026, Mon: 11, Day: day}, Revenue: revenue, Payments: payments,
	})
	if err != nil {
		s.t.Fatal(err)
	}
}

func TestAdminHistoryFiltersByPeriodPersonAndCategory(t *testing.T) {
	s := newSite(t)
	s.withTerms(s.olga, 7000)
	s.withTerms(s.anna, 5000)
	s.saveDay(s.olga, 1, map[string]domain.Cents{"services": 20000}, map[string]domain.Cents{"cash": 20000})
	s.saveDay(s.olga, 2, map[string]domain.Cents{"creams": 1000}, map[string]domain.Cents{"card": 1000})
	s.saveDay(s.anna, 2, map[string]domain.Cents{"services": 9900}, map[string]domain.Cents{"card": 9900})
	anna := s.login("anna", annaPass)
	path := "/admin/history?from=2026-11-01&to=2026-11-02&person=" + s.olga.ID + "&category=services"
	page := s.get(path, anna)
	wantPage(t, "filtered history", page, http.StatusOK, "Фильтры", "Сбросить", "€200", "01.11.2026")
	if strings.Contains(page.Body.String(), ">€10<") || strings.Contains(page.Body.String(), ">€99<") {
		t.Fatal("history includes excluded reports")
	}
	wantPage(t, "reversed dates", s.get("/admin/history?from=2026-11-02&to=2026-11-01", anna), http.StatusBadRequest, "Проверьте период")
	for _, p := range []string{"/admin/month", "/admin/history", "/admin/audit"} {
		if res := s.get(p, s.login("olga", olgaPass)); res.Code != http.StatusForbidden {
			t.Fatalf("staff sees %s: %d", p, res.Code)
		}
	}
}

func TestHistoryPagesFiftyReportsWithoutLosingFilters(t *testing.T) {
	s := newSite(t)
	for _, u := range []store.User{s.anna, s.olga, s.maria} {
		s.withTerms(u, 5000)
		for day := 1; day <= 20; day++ {
			s.saveDay(u, day, map[string]domain.Cents{"services": 100}, map[string]domain.Cents{"card": 100})
		}
	}
	anna := s.login("anna", annaPass)
	page := s.get("/admin/history?from=2026-11-01&to=2026-11-20&category=services", anna)
	wantPage(t, "first 50", page, http.StatusOK, "Показать ещё", "page=2", "category=services", "Записи 1–50 из 60")
	wantPage(t, "last 10", s.get("/admin/history?from=2026-11-01&to=2026-11-20&category=services&page=2", anna), http.StatusOK, "Записи 51–60 из 60", "Предыдущие записи")
}

func TestAdminChangeLogShowsWhoChangedReportAmounts(t *testing.T) {
	s := newSite(t)
	s.withTerms(s.olga, 7000)
	s.saveDay(s.olga, 1, map[string]domain.Cents{"services": 20000}, map[string]domain.Cents{"card": 20000})
	path := "/admin/reports/" + s.olga.ID + "/2026-11-01"
	anna := s.login("anna", annaPass)
	s.post(path, url.Values{"r_services": {"300"}, "p_card": {"300"}, "version": {"1"}}, anna)
	page := s.get("/admin/audit?entity=report", anna)
	wantPage(t, "report changes", page, http.StatusOK, "Журнал изменений", "Анна", "Изменение", "Ольга", "€200 → €300", "Выручка")
	if strings.Contains(page.Body.String(), "passwordHash") || strings.Contains(page.Body.String(), "sessionVersion") {
		t.Fatal("change log exposes internal snapshots")
	}
}

func TestChangeLogShowsPaymentCorrectionsEvenWhenRevenueIsUnchanged(t *testing.T) {
	s := newSite(t)
	s.withTerms(s.olga, 7000)
	s.saveDay(s.olga, 1, map[string]domain.Cents{"services": 10000}, map[string]domain.Cents{"card": 10000})
	anna := s.login("anna", annaPass)
	path := "/admin/reports/" + s.olga.ID + "/2026-11-01"
	s.post(path, url.Values{"r_services": {"100"}, "p_paid_by_certificate": {"100"}, "version": {"1"}}, anna)
	wantPage(t, "payment correction", s.get("/admin/audit?entity=report&person="+s.anna.ID, anna), http.StatusOK, "Оплата · Карта", "€100 → €0", "€0 → €100")
}

func TestChangeLogShowsRoleChanges(t *testing.T) {
	s := newSite(t)
	_, err := s.st.UpdateUser(ctx, s.anna.Actor(), s.olga.ID, func(u *store.User) error { u.Role = store.RoleAdmin; return nil })
	if err != nil {
		t.Fatal(err)
	}
	wantPage(t, "role change", s.get("/admin/audit?entity=user", s.login("anna", annaPass)), http.StatusOK, "Роль", "Сотрудник → Администратор")
}

func TestChangeLogShowsManualSelfOverridesEvenWhenTheAmountMatches(t *testing.T) {
	s := newSite(t)
	s.withTerms(s.olga, 7000)
	s.saveDay(s.olga, 1, map[string]domain.Cents{"services": 10000}, map[string]domain.Cents{"card": 10000})
	anna := s.login("anna", annaPass)
	path := "/admin/reports/" + s.olga.ID + "/2026-11-01"
	s.post(path, url.Values{"r_services": {"100"}, "p_card": {"100"}, "selfManual": {"1"}, "selfOverride": {"70"}, "version": {"1"}}, anna)
	wantPage(t, "override flag", s.get("/admin/audit?entity=report", anna), http.StatusOK, "«Себе» задано вручную", "Нет → Да")
}

func TestAdminSeesTheDaysTotalsAndMissingReporters(t *testing.T) {
	s := newSite(t)
	s.withTerms(s.anna, 5000)
	s.withTerms(s.olga, 7000)
	s.saveDay(s.anna, 1, map[string]domain.Cents{"services": 60000, "creams": 5000, "certificates": 10000}, map[string]domain.Cents{"card": 60000, "cash": 15000})
	s.saveDay(s.olga, 1, map[string]domain.Cents{"services": 20000}, map[string]domain.Cents{"cash": 20000})
	anna := s.login("anna", annaPass)
	wantPage(t, "day totals", s.get("/admin/day?date=2026-11-01", anna), http.StatusOK,
		"01.11.2026", "€950", "€465", "€485", "€600", "€150", "€200", "€60",
		"/admin/reports/"+s.olga.ID+"/2026-11-01", "Изменить")
	reports, err := s.st.ReportsOn(ctx, domain.Date{Year: 2026, Mon: 11, Day: 1})
	if err != nil {
		t.Fatal(err)
	}
	var originals []domain.PersonReport
	for _, rep := range reports {
		originals = append(originals, rep.PersonReport)
	}
	if got := domain.Summarize(originals).Cash; got != 35000 {
		t.Fatalf("legacy payment cash changed: %d", got)
	}
	if res := s.get("/admin/day", s.login("olga", olgaPass)); res.Code != http.StatusForbidden {
		t.Fatalf("staff sees day: %d", res.Code)
	}
	// The next day is empty, rather than silently showing the previous day's totals.
	wantPage(t, "empty day", s.get("/admin/day?date=2026-11-02", anna), http.StatusOK, "Отчётов за этот день нет", "€0")
	wantPage(t, "current missing reporters", s.get("/admin/day?date=2026-11-20", anna), http.StatusOK, "Не сдали отчёт", "Мария")
}

func TestHistoricalMissingReportsUseThatDaysStaffStatus(t *testing.T) {
	s := newSite(t)
	s.withTerms(s.anna, 5000)
	s.saveDay(s.anna, 20, map[string]domain.Cents{"services": 10000}, map[string]domain.Cents{"card": 10000})
	*s.clock = s.clock.AddDate(0, 0, 1)
	_, err := s.st.UpdateUser(ctx, s.anna.Actor(), s.maria.ID, func(u *store.User) error { u.Active = false; return nil })
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.st.UpdateUser(ctx, s.anna.Actor(), s.olga.ID, func(u *store.User) error { u.SubmitsReports = false; return nil })
	if err != nil {
		t.Fatal(err)
	}
	s.person("vera", "Вера", "vera-password")
	anna := s.login("anna", annaPass)
	old := s.get("/admin/day?date=2026-11-20", anna)
	wantPage(t, "historical gaps", old, http.StatusOK, "Не сдали отчёт", "Мария", "Ольга")
	if strings.Contains(old.Body.String(), "Вера") {
		t.Fatal("new staff is missing before account creation")
	}
	before := s.get("/admin/day?date=2026-11-19", anna)
	if strings.Contains(before.Body.String(), "Не сдали отчёт") {
		t.Fatal("reporters are listed before their accounts existed")
	}
	current := s.get("/admin/day?date=2026-11-21", anna)
	wantPage(t, "current roster", current, http.StatusOK, "Вера")
	if strings.Contains(current.Body.String(), "Мария") || strings.Contains(current.Body.String(), "Ольга") {
		t.Fatal("disabled staff are expected on the current day")
	}
}

func TestAccountRecountAuditShowsItsAccountAndBothAmounts(t *testing.T) {
	s := newSite(t)
	for account, counted := range map[string]domain.Cents{"kassa_cash": 7500, "kassa_card": 12000} {
		if _, err := s.st.Recount(ctx, s.anna.Actor(), account, counted, "Проверка пересчёта"); err != nil {
			t.Fatal(err)
		}
	}
	page := s.get("/admin/audit?entity=account", s.login("anna", annaPass))
	wantPage(t, "recount log", page, http.StatusOK, "По учёту", "При пересчёте")
	found := 0
	for _, card := range strings.Split(page.Body.String(), "<article") {
		if !strings.Contains(card, "Пересчёт кассы") {
			continue
		}
		found++
		if !strings.Contains(card, "€0") || strings.Contains(card, "Название") {
			t.Fatalf("incorrect recount detail: %s", card)
		}
		switch {
		case strings.Contains(card, "Касса — наличные"):
			if !strings.Contains(card, "€75") {
				t.Fatal("cash recount lacks its counted amount")
			}
		case strings.Contains(card, "Касса — карта салона"):
			if !strings.Contains(card, "€120") {
				t.Fatal("card recount lacks its counted amount")
			}
		default:
			t.Fatal("recount has no account identity")
		}
	}
	if found != 2 {
		t.Fatalf("got %d recount cards, want 2", found)
	}
}

func TestAdminMonthAddsCategoriesPaymentsAndWorkingDays(t *testing.T) {
	s := newSite(t)
	s.withTerms(s.anna, 5000)
	s.withTerms(s.olga, 7000)
	s.saveDay(s.anna, 1, map[string]domain.Cents{"services": 60000, "creams": 5000, "certificates": 10000}, map[string]domain.Cents{"card": 60000, "cash": 15000})
	s.saveDay(s.olga, 1, map[string]domain.Cents{"services": 20000}, map[string]domain.Cents{"cash": 20000})
	s.saveDay(s.olga, 2, map[string]domain.Cents{"services": 10000}, map[string]domain.Cents{"paid_by_certificate": 10000})
	wantPage(t, "November totals", s.get("/admin/month?m=2026-11", s.login("anna", annaPass)), http.StatusOK,
		"Ноябрь 2026", "€1\u202f050", "€535", "€515", "€900", "€50", "€100", "€600", "€350", "По сотрудникам", "Рабочие дни", "Ольга", "€300")
}

func TestAdminEditsAnotherPersonsReportWithTheirTermsAndAudit(t *testing.T) {
	s := newSite(t)
	s.withTerms(s.anna, 5000)
	s.withTerms(s.olga, 7000)
	s.saveDay(s.olga, 1, map[string]domain.Cents{"services": 20000}, map[string]domain.Cents{"cash": 20000})
	path := "/admin/reports/" + s.olga.ID + "/2026-11-01"
	anna := s.login("anna", annaPass)
	wantPage(t, "edit another person", s.get(path, anna), http.StatusOK, "Отчёт · Ольга", `action="`+path+`"`, "(70%)", `value="200"`)
	values := url.Values{"r_services": {"300"}, "p_card": {"300"}, "version": {"1"}}
	wantRedirect(t, "admin save", s.post(path, values, anna), path+"?saved=1")
	rep := s.report(s.olga, 1)
	if rep.Report.SelfAmount != 21000 || rep.UpdatedBy != s.anna.ID || rep.Version != 2 {
		t.Fatalf("admin save: %+v", rep)
	}
	audit, err := s.st.Audit(ctx, s.olga.ID+"_2026-11-01")
	if err != nil || len(audit) != 2 || audit[0].ActorID != s.anna.ID {
		t.Fatalf("audit: %+v, %v", audit, err)
	}
	wantPage(t, "stale admin save", s.post(path, values, anna), http.StatusUnprocessableEntity, `value="300"`, `name="version" value="2"`, "Запись изменена")
	if res := s.post(path, values, s.login("maria", mariaPass)); res.Code != http.StatusForbidden {
		t.Fatalf("staff edits another person: %d", res.Code)
	}
	*s.clock = s.clock.AddDate(0, 0, 15)
	if _, err := s.st.CloseMonth(ctx, s.anna.Actor(), domain.Month{Year: 2026, Mon: 11}); err != nil {
		t.Fatal(err)
	}
	wantPage(t, "closed admin form", s.get(path, anna), http.StatusOK, "Месяц закрыт — изменения недоступны")
	wantPage(t, "closed admin day", s.get("/admin/day?date=2026-11-01", anna), http.StatusOK, "Месяц закрыт — изменения недоступны")
	values.Set("version", "2")
	wantPage(t, "closed admin save", s.post(path, values, anna), http.StatusUnprocessableEntity, "Месяц закрыт — изменения недоступны")
}
