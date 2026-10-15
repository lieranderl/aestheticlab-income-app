package web_test

import (
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/lieranderl/aestheticlab-income-app/internal/domain"
	"github.com/lieranderl/aestheticlab-income-app/internal/store"
)

// withTerms gives a person «Себе» terms from the start month.
func (s *site) withTerms(u store.User, selfPct domain.BasisPoints) {
	s.t.Helper()
	if err := s.st.SetTerms(ctx, s.anna.Actor(), u.ID, s.st.StartMonth(), domain.Terms{SelfPct: selfPct}, ""); err != nil {
		s.t.Fatal(err)
	}
}

// specDay is the spec's example day: services 600, creams 50, a certificate
// sold for 100, paid 600 by card and 150 in cash.
func specDay(version string) url.Values {
	return url.Values{
		"r_services": {"600"}, "r_creams": {"50"}, "r_certificates": {"100"},
		"p_card": {"600"}, "p_cash": {"150"}, "version": {version},
	}
}

func TestTheSpecDayIsSavedFromTheDailyForm(t *testing.T) {
	s := newSite(t)
	s.withTerms(s.anna, 5000)
	s.seedLegacyBlank(s.anna, 20)
	anna := s.login("anna", annaPass)

	wantPage(t, "empty form", s.get("/reports/day", anna), http.StatusOK,
		`id="report-form"`, `action="/reports/2026-11-20"`, `name="r_services"`, `name="r_certificates"`,
		`name="p_card"`, `name="p_cash"`, `name="p_paid_by_certificate"`, `inputmode="decimal"`, "20.11.2026", "Сохранить")

	res := s.post("/reports/2026-11-20", specDay("1"), anna)
	wantRedirect(t, "save without JS", res, "/reports/day?date=2026-11-20&saved=1")

	saved, err := s.st.Report(ctx, s.anna.ID, domain.Date{Year: 2026, Mon: 11, Day: 20})
	if err != nil {
		t.Fatal(err)
	}
	r := saved.Report
	if r.Total != 75000 || r.SelfAmount != 32500 || r.SalonShare != 42500 || r.CashToKassa != 0 || r.BalanceDelta != 17500 {
		t.Fatalf("saved %+v, want total 750, «Себе» 325, «Салону» 425, nothing left in the kassa, +175", r)
	}

	wantPage(t, "saved form", s.get("/reports/day?date=2026-11-20&saved=1", anna), http.StatusOK,
		"Сохранено ✓", `value="600"`, `value="150"`, `name="version" value="2"`,
		"€325", "(50%)", "€425", "Остальные €150 наличными — ваши. Салон должен вам €175")
}

func TestSavingWithDatastarPatchesTheFormInEditMode(t *testing.T) {
	s := newSite(t)
	s.withTerms(s.anna, 5000)
	s.seedLegacyBlank(s.anna, 20)
	anna := s.login("anna", annaPass)
	res := s.do(request{method: http.MethodPost, path: "/reports/2026-11-20", form: specDay("1"), cookies: anna, datastar: true})
	wantPage(t, "Datastar save", res, http.StatusOK, "datastar-patch-elements", `id="report-form"`,
		"Сохранено ✓", `name="version" value="2"`, `data-version="2"`)
}

func TestPaymentsThatDoNotAddUpAreRefusedWithTheDifference(t *testing.T) {
	s := newSite(t)
	s.withTerms(s.anna, 5000)
	s.seedLegacyBlank(s.anna, 20)
	anna := s.login("anna", annaPass)
	day := specDay("1")
	day.Set("p_cash", "100")
	res := s.post("/reports/2026-11-20", day, anna)
	wantPage(t, "short payments", res, http.StatusUnprocessableEntity,
		"Сумма оплат меньше выручки на €50 — проверьте наличные", `value="600"`, `value="100"`)
	if saved := s.report(s.anna, 20); saved.Version != 1 || saved.Report.Total != 0 {
		t.Fatal("invalid edit changed the existing legacy report")
	}
	day.Set("p_cash", "1 50")
	wantPage(t, "unreadable amount", s.post("/reports/2026-11-20", day, anna), http.StatusUnprocessableEntity,
		"Проверьте суммы", `aria-invalid="true"`)
}

func TestCashLeftInTheKassaAndAHandSetSelfAreKept(t *testing.T) {
	s := newSite(t)
	s.withTerms(s.olga, 7000)
	s.seedLegacyBlank(s.olga, 20)
	olga := s.login("olga", olgaPass)
	day := url.Values{"r_services": {"200"}, "p_cash": {"200"}, "cashToKassa": {"100"}, "version": {"1"}}
	wantRedirect(t, "cash day", s.post("/reports/2026-11-20", day, olga), "/reports/day?date=2026-11-20&saved=1")
	r := s.report(s.olga, 20).Report
	if r.SelfAmount != 14000 || r.CashToKassa != 10000 || r.BalanceDelta != 4000 {
		t.Fatalf("cash day %+v, want «Себе» 140, 100 left in the kassa, the salon owes 40", r)
	}
	wantPage(t, "cash day", s.get("/reports/day?date=2026-11-20", olga), http.StatusOK,
		`name="cashToKassa" value="100"`, "Остальные €100 наличными — ваши. Салон должен вам €40")

	day = url.Values{"r_services": {"200"}, "p_cash": {"200"}, "selfManual": {"1"}, "selfOverride": {"150"}, "version": {"2"}}
	wantRedirect(t, "hand-set «Себе»", s.post("/reports/2026-11-20", day, olga), "/reports/day?date=2026-11-20&saved=1")
	r = s.report(s.olga, 20).Report
	if !r.SelfOverridden || r.SelfAmount != 15000 || r.CashToKassa != 5000 || r.BalanceDelta != 0 {
		t.Fatalf("hand-set «Себе» %+v, want 150 with the suggested 50 left in the kassa", r)
	}
	wantPage(t, "hand-set «Себе»", s.get("/reports/day?date=2026-11-20", olga), http.StatusOK,
		`name="selfManual" value="1"`, `checked`, `name="selfOverride" value="150"`, "Расчёт за день: 0")
}

func TestAStaleFormIsRefused(t *testing.T) {
	s := newSite(t)
	s.withTerms(s.anna, 5000)
	s.seedLegacyBlank(s.anna, 20)
	anna := s.login("anna", annaPass)
	s.post("/reports/2026-11-20", specDay("1"), anna)
	res := s.post("/reports/2026-11-20", specDay("1"), anna)
	wantPage(t, "second save on the version-0 form", res, http.StatusUnprocessableEntity,
		"Запись изменена другим пользователем — показаны сохранённые суммы", `name="version" value="2"`)
}

func TestTheFormOpensOnlyDaysThatCanHaveReports(t *testing.T) {
	s := newSite(t)
	s.withTerms(s.olga, 7000)
	olga := s.login("olga", olgaPass)
	wantPage(t, "before start", s.get("/?date=2026-10-30", olga), http.StatusOK, `value="2026-11-01"`, "01.11.2026", `min="2026-11-01"`, `max="2026-11-20"`)
	wantPage(t, "future", s.get("/?date=2026-11-21", olga), http.StatusOK, "20.11.2026", `href="/clients/new?date=2026-11-20"`)
	wantPage(t, "unreadable", s.get("/?date=soon", olga), http.StatusOK, `href="/clients/new?date=2026-11-20"`)
	wantPage(t, "past", s.get("/?date=2026-11-02", olga), http.StatusOK, `href="/?date=2026-11-01"`, `href="/?date=2026-11-03"`)
	form := url.Values{"token": {"date-client-token"}, "version": {"0"}, "services": {"100"}, "date": {"2026-11-21"}}
	wantPage(t, "future save", s.post("/clients", form, olga), http.StatusUnprocessableEntity, "Клиента за будущий день сохранить нельзя")
	form.Set("date", "2026-10-30")
	wantPage(t, "before start save", s.post("/clients", form, olga), http.StatusUnprocessableEntity, "Этот месяц раньше начала работы приложения")
}

func TestWithoutTermsTheFormIsReadOnly(t *testing.T) {
	s := newSite(t)
	maria := s.login("maria", mariaPass)
	wantPage(t, "no terms", s.get("/clients/new", maria), http.StatusOK, "Ваши условия («Себе» %) ещё не заданы", `<fieldset class="contents" disabled`)
	form := url.Values{"token": {"terms-client-token"}, "date": {"2026-11-20"}, "version": {"0"}, "services": {"100"}}
	wantPage(t, "saving without terms", s.post("/clients", form, maria), http.StatusUnprocessableEntity, "Ваши условия («Себе» %) ещё не заданы")
}

func TestAClosedMonthShowsItsReportsReadOnly(t *testing.T) {
	s := newSite(t)
	s.withTerms(s.anna, 5000)
	s.seedLegacyBlank(s.anna, 20)
	anna := s.login("anna", annaPass)
	s.post("/reports/2026-11-20", specDay("1"), anna)
	*s.clock = s.clock.AddDate(0, 0, 15) // 5 December
	if _, err := s.st.CloseMonth(ctx, s.anna.Actor(), domain.Month{Year: 2026, Mon: 11}); err != nil {
		t.Fatal(err)
	}
	wantPage(t, "closed November", s.get("/reports/day?date=2026-11-20", anna), http.StatusOK,
		"Месяц закрыт — изменения недоступны", `value="600"`, "disabled")
	wantPage(t, "saving in closed November", s.post("/reports/2026-11-20", specDay("2"), anna), http.StatusUnprocessableEntity,
		"Месяц закрыт — изменения недоступны")
}

func TestPeopleWhoDoNotReportHaveNoForm(t *testing.T) {
	s := newSite(t)
	vera, err := s.st.CreateUser(ctx, s.anna.Actor(), store.NewUser{Username: "vera", DisplayName: "Вера", Role: store.RoleEmployee, PasswordHash: hash(t, "vera-password")})
	if err != nil {
		t.Fatal(err)
	}
	s.settle(vera)
	cookies := s.login("vera", "vera-password")
	wantRedirect(t, "start page", s.get("/", cookies), "/my")
	day := url.Values{"r_services": {"100"}, "p_card": {"100"}, "version": {"1"}}
	if res := s.post("/reports/2026-11-20", day, cookies); res.Code != http.StatusNotFound {
		t.Fatalf("a save from someone who doesn't report: %d", res.Code)
	}
}

// report reads a person's November report for a day.
func (s *site) report(u store.User, day int) store.SavedReport {
	s.t.Helper()
	r, err := s.st.Report(ctx, u.ID, domain.Date{Year: 2026, Mon: 11, Day: day})
	if err != nil {
		s.t.Fatal(err)
	}
	return r
}

func TestASavedZeroStaysZeroWhenTheDayIsSavedAgain(t *testing.T) {
	s := newSite(t)
	s.withTerms(s.olga, 7000)
	s.seedLegacyBlank(s.olga, 20)
	olga := s.login("olga", olgaPass)
	day := url.Values{"r_services": {"200"}, "p_cash": {"200"}, "cashToKassa": {"0"}, "version": {"1"}}
	s.post("/reports/2026-11-20", day, olga)
	wantPage(t, "kept all the cash", s.get("/reports/day?date=2026-11-20", olga), http.StatusOK,
		`name="cashToKassa" value="0"`, "Вы должны салону €60")
	day.Set("version", "2")
	s.post("/reports/2026-11-20", day, olga)
	if r := s.report(s.olga, 20).Report; r.CashToKassa != 0 || r.BalanceDelta != -6000 {
		t.Fatalf("saved again: %+v, want nothing in the kassa and −60", r)
	}

	card := url.Values{"r_services": {"200"}, "p_card": {"200"}, "selfManual": {"1"}, "selfOverride": {"0"}, "version": {"3"}}
	wantRedirect(t, "«Себе» set to 0", s.post("/reports/2026-11-20", card, olga), "/reports/day?date=2026-11-20&saved=1")
	wantPage(t, "«Себе» set to 0", s.get("/reports/day?date=2026-11-20", olga), http.StatusOK, `name="selfOverride" value="0"`)
}

func TestAfterAConflictTheSavedFiguresAreShown(t *testing.T) {
	s := newSite(t)
	s.withTerms(s.anna, 5000)
	s.seedLegacyBlank(s.anna, 20)
	anna := s.login("anna", annaPass)
	s.post("/reports/2026-11-20", specDay("1"), anna)
	stale := specDay("1")
	stale.Set("r_services", "700")
	stale.Set("p_card", "700")
	res := s.post("/reports/2026-11-20", stale, anna)
	wantPage(t, "conflict", res, http.StatusUnprocessableEntity, `value="600"`, `name="version" value="2"`)
	if strings.Contains(res.Body.String(), `value="700"`) {
		t.Fatal("the conflict page offers the stale figures for saving over the newer report")
	}
}

func TestTheSalonsShareCanBeTransferredToAnnasCard(t *testing.T) {
	s := newSite(t)
	s.withTerms(s.olga, 7000)
	s.seedLegacyBlank(s.olga, 20)
	olga := s.login("olga", olgaPass)
	wantPage(t, "form", s.get("/reports/day", olga), http.StatusOK, "Наличными в кассу", "Переводом на карту салона", `name="transferToCard"`)

	day := url.Values{"r_services": {"200"}, "p_cash": {"200"}, "transferToCard": {"60"}, "version": {"1"}}
	wantRedirect(t, "transfer", s.post("/reports/2026-11-20", day, olga), "/reports/day?date=2026-11-20&saved=1")
	if r := s.report(s.olga, 20).Report; r.TransferToCard != 6000 || r.CashToKassa != 0 || r.BalanceDelta != 0 {
		t.Fatalf("saved %+v, want €60 to the card, nothing in the till, settled", r)
	}
	wantPage(t, "saved transfer", s.get("/reports/day?date=2026-11-20", olga), http.StatusOK,
		`name="transferToCard" value="60"`, `name="cashToKassa" value=""`, `placeholder="0"`, "Остальные €140 наличными — ваши. Расчёт за день: 0")

	day = url.Values{"r_services": {"200"}, "p_cash": {"200"}, "transferToCard": {"150"}, "cashToKassa": {"100"}, "version": {"2"}}
	wantPage(t, "more than the cash", s.post("/reports/2026-11-20", day, olga), http.StatusUnprocessableEntity,
		"В кассу и на карту вместе — не больше суммы наличных")
}

func TestARefusedCashDayKeepsTheKassaFieldsOnScreen(t *testing.T) {
	s := newSite(t)
	s.withTerms(s.olga, 7000)
	s.seedLegacyBlank(s.olga, 20)
	olga := s.login("olga", olgaPass)
	day := url.Values{"r_services": {"200"}, "p_cash": {"200"}, "cashToKassa": {"300"}, "version": {"1"}}
	res := s.post("/reports/2026-11-20", day, olga)
	wantPage(t, "too much in the till", res, http.StatusUnprocessableEntity,
		"В кассу и на карту вместе — не больше суммы наличных", `name="cashToKassa" value="300"`)
	if strings.Contains(res.Body.String(), "card bg-base-100 shadow-sm hidden") {
		t.Fatal("the «Оставьте в кассе» section is hidden, so the field at fault can't be corrected without JS")
	}
}

// A syntax error in a Datastar expression fails silently in the browser, so
// every expression on the daily form must at least balance its brackets.
func TestTheFormsLiveExpressionsBalanceTheirBrackets(t *testing.T) {
	s := newSite(t)
	s.withTerms(s.anna, 5000)
	s.seedLegacyBlank(s.anna, 20)
	page := s.get("/reports/day", s.login("anna", annaPass)).Body.String()
	attr := regexp.MustCompile(`data-(?:computed|effect|text|class|on|show|attr|signals)[\w:-]*="([^"]*)"`)
	found := 0
	for _, m := range attr.FindAllStringSubmatch(page, -1) {
		expr := html.UnescapeString(m[1])
		if err := balanced(expr); err != "" {
			t.Errorf("%s in %s", err, expr)
		}
		found++
	}
	if found < 20 {
		t.Fatalf("only %d expressions found; the pattern is stale", found)
	}
}

// balanced checks (), [] and {} outside string literals.
func balanced(expr string) string {
	var stack []rune
	pairs := map[rune]rune{')': '(', ']': '[', '}': '{'}
	var quote rune
	for i, r := range expr {
		switch {
		case quote != 0:
			if r == quote && (i == 0 || expr[i-1] != '\\') {
				quote = 0
			}
		case r == '\'' || r == '"' || r == '`':
			quote = r
		case r == '(' || r == '[' || r == '{':
			stack = append(stack, r)
		case pairs[r] != 0:
			if len(stack) == 0 || stack[len(stack)-1] != pairs[r] {
				return "unbalanced " + string(r)
			}
			stack = stack[:len(stack)-1]
		}
	}
	if quote != 0 || len(stack) > 0 {
		return "unclosed bracket or quote"
	}
	return ""
}

// seedLegacyBlank models an already-existing daily row; the new UI cannot
// create daily rows. Corrections still use the same public money seam.
func (s *site) seedLegacyBlank(u store.User, day int) {
	s.t.Helper()
	_, err := s.st.SaveReport(ctx, u.Actor(), store.ReportEntry{Person: u.ID, Date: domain.Date{Year: 2026, Mon: 11, Day: day}})
	if err != nil {
		s.t.Fatal(err)
	}
}

func (s *site) seedLegacyForm(u store.User, day int, form url.Values) {
	s.t.Helper()
	e := store.ReportEntry{Person: u.ID, Date: domain.Date{Year: 2026, Mon: 11, Day: day}, Revenue: map[string]domain.Cents{}, Payments: map[string]domain.Cents{}}
	for key, values := range form {
		if strings.HasPrefix(key, "r_") || strings.HasPrefix(key, "p_") {
			v, err := domain.ParseAmount(values[0])
			if err != nil {
				s.t.Fatal(err)
			}
			if id, ok := strings.CutPrefix(key, "r_"); ok {
				e.Revenue[id] = v
			} else {
				e.Payments[strings.TrimPrefix(key, "p_")] = v
			}
		}
	}
	if form.Get("selfManual") == "1" {
		v, err := domain.ParseAmount(form.Get("selfOverride"))
		if err != nil {
			s.t.Fatal(err)
		}
		e.SelfOverride = &v
	}
	_, err := s.st.SaveReport(ctx, u.Actor(), e)
	if err != nil {
		s.t.Fatal(err)
	}
}
