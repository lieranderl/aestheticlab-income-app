package web_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/lieranderl/aestheticlab-income-app/internal/domain"
)

func TestMyRecordsShowTheMonthsFiguresAndReports(t *testing.T) {
	s := newSite(t)
	terms := domain.Terms{SelfPct: 7000, HighSelfFromCents: 500000, HighSelfPct: 8000}
	if err := s.st.SetTerms(ctx, s.anna.Actor(), s.olga.ID, s.st.StartMonth(), terms, ""); err != nil {
		t.Fatal(err)
	}
	s.withTerms(s.maria, 7000)
	olga, maria := s.login("olga", olgaPass), s.login("maria", mariaPass)
	s.seedLegacyForm(s.olga, 19, url.Values{"r_services": {"200"}, "p_cash": {"200"}, "version": {"0"}})
	s.seedLegacyForm(s.olga, 20, url.Values{"r_services": {"300"}, "r_certificates": {"100"}, "p_card": {"400"}, "version": {"0"}})
	s.seedLegacyForm(s.maria, 20, url.Values{"r_services": {"999"}, "p_card": {"999"}, "version": {"0"}})

	page := s.get("/my", olga)
	wantPage(t, "Olga's November", page, http.StatusOK,
		"Ноябрь 2026", "Выручка", "€600", "«Себе»", "€350", "«Салону»", "€250", "Рабочие дни", ">2</dd>",
		"пт, 20.11", "чт, 19.11", `href="/reports/day?date=2026-11-20"`, "Салон должен вам €210", "Расчёт за день: 0",
		"80%", "€4\u202f500,01") // «До 80% осталось €4 500,01»: base 500 of 5 000
	if strings.Contains(page.Body.String(), "€999") {
		t.Fatal("Olga sees Maria's report")
	}
	wantPage(t, "a month before the start", s.get("/my?m=2026-10", olga), http.StatusOK, "Ноябрь 2026")
	wantPage(t, "a month to come", s.get("/my?m=2027-01", olga), http.StatusOK, "Ноябрь 2026")
	wantPage(t, "an empty month", s.get("/my?m=2026-11", maria), http.StatusOK, "€999", ">1</dd>")
}

func TestMyRecordsSwitchMonths(t *testing.T) {
	s := newSite(t)
	s.withTerms(s.olga, 7000)
	*s.clock = s.clock.AddDate(0, 1, 0) // 20 December
	olga := s.login("olga", olgaPass)
	page := s.get("/my?m=2026-12", olga)
	wantPage(t, "December", page, http.StatusOK, "Декабрь 2026", `href="/my?m=2026-11"`, "Отчётов за этот месяц нет")
	if strings.Contains(page.Body.String(), `href="/my?m=2027-01"`) {
		t.Error("the month switcher goes past the current month")
	}
	page = s.get("/my?m=2026-11", olga)
	wantPage(t, "November", page, http.StatusOK, "Ноябрь 2026", `href="/my?m=2026-12"`)
	if strings.Contains(page.Body.String(), `href="/my?m=2026-10"`) {
		t.Error("the month switcher goes before the start month")
	}
}

func TestTheProfileExplainsHowToInstallTheApp(t *testing.T) {
	s := newSite(t)
	wantPage(t, "profile", s.get("/profile", s.login("olga", olgaPass)), http.StatusOK,
		"Как установить на iPhone", "«На экран „Домой“»", "Как установить на Android", "«Установить приложение»")
}

func TestTheHigherRateLeavesOutHandSetDays(t *testing.T) {
	s := newSite(t)
	terms := domain.Terms{SelfPct: 7000, HighSelfFromCents: 10000, HighSelfPct: 8000}
	if err := s.st.SetTerms(ctx, s.anna.Actor(), s.olga.ID, s.st.StartMonth(), terms, ""); err != nil {
		t.Fatal(err)
	}
	olga := s.login("olga", olgaPass)
	s.seedLegacyForm(s.olga, 19, url.Values{"r_services": {"200"}, "p_card": {"200"}, "version": {"0"}})
	wantPage(t, "above the threshold", s.get("/my", olga), http.StatusOK, "за весь месяц «Себе» 80%")
	s.seedLegacyForm(s.olga, 20, url.Values{"r_services": {"100"}, "p_card": {"100"}, "selfManual": {"1"}, "selfOverride": {"50"}, "version": {"0"}})
	wantPage(t, "with a hand-set day", s.get("/my", olga), http.StatusOK, "кроме дней, где «Себе» задано вручную")
}
