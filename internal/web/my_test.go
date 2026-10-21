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

// The screenshots of 2026-10-21: the cash kept and «Себе» looked like two
// unrelated numbers. Every personal total and record now shows how they meet.
func TestPersonalPagesExplainHowCashKeptMakesUpSelf(t *testing.T) {
	s := newSite(t)
	if err := s.st.SetTerms(ctx, s.anna.Actor(), s.olga.ID, s.st.StartMonth(), domain.Terms{SelfPct: 5000, HighSelfFromCents: 300000, HighSelfPct: 7000}, ""); err != nil {
		t.Fatal(err)
	}
	olga := s.login("olga", olgaPass)
	for token, v := range map[string]url.Values{
		"self-clarity-card-day-token": {"date": {"2026-11-19"}, "services": {"427"}, "serviceCard": {"327"}, "creams": {"100"}, "extrasDestination": {"kassa_card"}},
		"self-clarity-cash-day-token": {"date": {"2026-11-20"}, "services": {"365"}, "extrasDestination": {"cash"}},
	} {
		v.Set("token", token)
		v.Set("version", "0")
		if res := s.post("/clients", v, olga); res.Code != http.StatusSeeOther {
			t.Fatalf("save %s: %d %s", token, res.Code, res.Body.String())
		}
	}
	wantPage(t, "day", s.get("/?date=2026-11-19", olga), http.StatusOK,
		"Ваши «Себе» за день", "Оставили себе наличными", "€100", "По записям салон должен вам", "+€113,50", "€213,50",
		"По записи: салон должен вам €113,50", "«Себе» €213,50 = €100 оставили себе наличными + €113,50 от салона")
	if strings.Contains(s.get("/?date=2026-11-18", olga).Body.String(), "Ваши «Себе»") {
		t.Error("an empty day shows the «Себе» split")
	}
	page := s.get("/my", olga)
	wantPage(t, "month", page, http.StatusOK,
		"Ваши «Себе» за месяц", "€465", "По записям вы должны салону", "−€69", "€396",
		"По записи: вы должны салону €182,50", "«Себе» €182,50 = €365 оставили себе наличными − €182,50 салону",
		"Услуги за месяц €792 из €3 000", "€2 208,01")
	for _, confusing := range []string{"База «Себе»", "У вас остаётся"} {
		if strings.Contains(page.Body.String(), confusing) {
			t.Errorf("still says %q", confusing)
		}
	}
}
