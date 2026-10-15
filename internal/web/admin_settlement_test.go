package web_test

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/lieranderl/aestheticlab-income-app/internal/domain"
	"github.com/lieranderl/aestheticlab-income-app/internal/store"
)

func settlementSite(t *testing.T) *site {
	t.Helper()
	s := newSiteOptions(t, store.WithStartMonth(domain.Month{Year: 2026, Mon: 10}))
	vera := s.person("vera", "Вера", "vera-password")
	for _, x := range []struct {
		u                                        store.User
		rate                                     domain.BasisPoints
		revenue, returned, expense, paid, salary domain.Cents
		weight                                   int64
	}{
		{s.maria, 6500, 742100, 205000, 4800, 0, 0, 1},
		{s.olga, 6500, 568000, 151000, 21500, 0, 0, 1},
		{vera, 4000, 81000, 81000, 0, 41250, 0, 0},
		{s.anna, 4500, 143000, 143000, 0, 195000, 120000, 1},
	} {
		if err := s.st.SetTerms(ctx, s.anna.Actor(), x.u.ID, s.st.StartMonth(), domain.Terms{SelfPct: x.rate, SalaryCents: x.salary, DividendWeight: x.weight}, ""); err != nil {
			t.Fatal(err)
		}
		zero := domain.Cents(0)
		day := domain.Date{Year: 2026, Mon: 10, Day: 20}
		if _, err := s.st.SaveReport(ctx, s.anna.Actor(), store.ReportEntry{Person: x.u.ID, Date: day, Revenue: map[string]domain.Cents{"services": x.revenue}, Payments: map[string]domain.Cents{"cash": x.revenue}, CashToKassa: &zero}); err != nil {
			t.Fatal(err)
		}
		for _, n := range []store.NewMovement{
			{Type: domain.Deposit, Amount: x.returned, From: domain.Person(x.u.ID), To: domain.Account(domain.KassaCard), Date: day},
			{Type: domain.Expense, Amount: x.expense, From: domain.Person(x.u.ID), To: domain.Outside, ExpenseCategory: "materials", Note: "Расход", Date: day},
			{Type: domain.Payout, Amount: x.paid, From: domain.Account(domain.KassaCard), To: domain.Person(x.u.ID), Date: day},
		} {
			if n.Amount > 0 {
				if _, err := s.st.RecordMovement(ctx, s.anna.Actor(), n); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	if _, err := s.st.RecordMovement(ctx, s.anna.Actor(), store.NewMovement{Type: domain.Expense, Amount: 260600, From: domain.Account(domain.KassaCard), To: domain.Outside, ExpenseCategory: "rent", Note: "Расходы салона", Date: domain.Date{Year: 2026, Mon: 10, Day: 20}}); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestSelectedOctoberSettlementShowsSpreadsheetResult(t *testing.T) {
	s := settlementSite(t)
	wantPage(t, "October settlement", s.get("/admin/month?m=2026-10&tab=settlement", s.login("anna", annaPass)), http.StatusOK,
		"Итоги и расчёт", "€5\u202f857,85", "€2\u202f869", "€1\u202f200", "€1\u202f788,85", "Предварительный расчёт", "не начислены")
}

func TestCloseAndReopenConfirmationsGuardAgainstChangedCalculations(t *testing.T) {
	s := settlementSite(t)
	cookie := s.login("anna", annaPass)
	p := s.get("/admin/month/2026-10/close", cookie)
	wantPage(t, "close confirmation", p, http.StatusOK, "Зарплаты", `name="revision"`, "Закрыть месяц")
	revision := hiddenRevision(t, p.Body.String())
	_, err := s.st.RecordMovement(ctx, s.anna.Actor(), store.NewMovement{Type: domain.Deposit, Amount: 100, From: domain.Person(s.olga.ID), To: domain.Account(domain.KassaCard), Date: domain.Date{Year: 2026, Mon: 10, Day: 21}})
	if err != nil {
		t.Fatal(err)
	}
	stale := s.post("/admin/month/2026-10/close", url.Values{"revision": {revision}}, cookie)
	wantPage(t, "stale confirmation", stale, http.StatusConflict, "Расчёт изменился", "Обновить расчёт")
	fresh := hiddenRevision(t, s.get("/admin/month/2026-10/close", cookie).Body.String())
	wantRedirect(t, "close", s.post("/admin/month/2026-10/close", url.Values{"revision": {fresh}}, cookie), "/admin/month?m=2026-10&tab=settlement&saved=1")
	wantPage(t, "repeat close", s.post("/admin/month/2026-10/close", url.Values{"revision": {fresh}}, cookie), http.StatusConflict, "Расчёт изменился")
	own := s.get("/my?m=2026-10", s.login("olga", olgaPass))
	wantPage(t, "own earned", own, http.StatusOK, "Зарплата", "Дивиденды", "Начислено при закрытии месяца")
	if strings.Contains(own.Body.String(), "Доход салона") || strings.Contains(own.Body.String(), "Мария") {
		t.Fatal("employee received salon or foreign financial data")
	}
	reopen := hiddenRevision(t, s.get("/admin/month/2026-10/reopen", cookie).Body.String())
	result := s.do(request{method: http.MethodPost, path: "/admin/month/2026-10/reopen", form: url.Values{"revision": {reopen}}, cookies: cookie, datastar: true})
	wantPage(t, "SSE reopen", result, http.StatusOK, "datastar-patch-signals", "redirectUrl")
	wantPage(t, "old confirmation after reopen", s.post("/admin/month/2026-10/close", url.Values{"revision": {fresh}}, cookie), http.StatusConflict, "Расчёт изменился")
	for _, path := range []string{"/admin/month/2026-10/close", "/admin/month/2026-10/reopen"} {
		if got := s.get(path, s.login("olga", olgaPass)); got.Code != http.StatusForbidden {
			t.Fatal("employee accesses month action")
		}
	}
	wantPage(t, "invalid month", s.get("/admin/month/bogus/close", cookie), http.StatusBadRequest, "Проверьте месяц")
}

func hiddenRevision(t *testing.T, body string) string {
	t.Helper()
	match := regexp.MustCompile(`name="revision" value="([a-f0-9]+)"`).FindStringSubmatch(body)
	if len(match) != 2 {
		t.Fatal("missing revision")
	}
	return match[1]
}

func TestSettlementPayoutPrefillsTodayAndRefreshesActualRemainingBalance(t *testing.T) {
	s := settlementSite(t)
	cookie := s.login("anna", annaPass)
	if _, err := s.st.CloseMonth(ctx, s.anna.Actor(), domain.Month{Year: 2026, Mon: 10}); err != nil {
		t.Fatal(err)
	}
	path := "/admin/movements/new?type=payout&settlement=2026-10&person=" + s.olga.ID
	p := s.get(path, cookie)
	wantPage(t, "settlement payout", p, http.StatusOK, `value="2026-11-20"`, `value="account:kassa_card" selected`, `name="settlementRevision"`, "Текущий остаток")
	revision := hiddenValue(t, p.Body.String(), "settlementRevision")
	form := url.Values{"type": {"payout"}, "amount": {"100"}, "date": {"2026-11-20"}, "from": {"account:kassa_card"}, "to": {"person:" + s.olga.ID}, "settlement": {"2026-10"}, "settlementPerson": {s.olga.ID}, "settlementRevision": {revision}}
	wantRedirect(t, "partial payout", s.post("/admin/movements", form, cookie), "/admin/month?m=2026-10&tab=settlement&saved=1")
	wantPage(t, "repeated payout", s.post("/admin/movements", form, cookie), http.StatusConflict, "Запись изменена", `value="100"`)
	refreshed := s.get(path, cookie)
	amount := hiddenValue(t, refreshed.Body.String(), "amount")
	if amount != "233,28" && amount != "233,29" {
		t.Fatalf("stale suggested amount %s", amount)
	}
	form.Set("settlementPerson", s.maria.ID)
	wantPage(t, "forged target", s.post("/admin/movements", form, cookie), http.StatusUnprocessableEntity, "Проверьте")
	wantPage(t, "invalid context", s.get("/admin/movements/new?type=payout&settlement=2026-09&person="+s.olga.ID, cookie), http.StatusBadRequest, "Проверьте")
	sse := s.do(request{method: http.MethodGet, path: path, cookies: cookie, datastar: true})
	wantPage(t, "SSE payout prefill", sse, http.StatusOK, "datastar-patch-elements", "settlementRevision", "_movementready")
	carry := s.get("/admin/month?m=2026-10&tab=settlement&carry=1", cookie)
	wantPage(t, "carry acknowledgement", carry, http.StatusOK, "Выплата или внесение не записаны")
}

func hiddenValue(t *testing.T, body, name string) string {
	t.Helper()
	match := regexp.MustCompile(`name="` + name + `" value="([^"]*)"`).FindStringSubmatch(body)
	if len(match) != 2 {
		t.Fatalf("missing %s", name)
	}
	return match[1]
}

func TestSettlementWarningsDoNotBlockClosingButPendingAndMonthOrderDo(t *testing.T) {
	s := newSite(t)
	s.withTerms(s.olga, 7000)
	cookie := s.login("anna", annaPass)
	wantPage(t, "running month", s.get("/admin/month?m=2026-11&tab=settlement", cookie), http.StatusOK, "Месяц ещё не закончился", "Не сдали отчёт", "Пересчёта наличных нет")
	*s.clock = s.clock.AddDate(0, 1, 0)
	cookie = s.login("anna", annaPass)
	p := s.get("/admin/month?m=2026-11&tab=settlement", cookie)
	wantPage(t, "warning-only past month", p, http.StatusOK, "Закрыть месяц", "Не сдали отчёт", "Пересчёта наличных нет")
	_, err := s.st.RecordMovement(ctx, s.olga.Actor(), store.NewMovement{Type: domain.Deposit, Amount: 100, From: domain.Person(s.olga.ID), To: domain.Account(domain.KassaCash), Date: domain.Date{Year: 2026, Mon: 11, Day: 20}})
	if err != nil {
		t.Fatal(err)
	}
	blocked := s.get("/admin/month?m=2026-11&tab=settlement", cookie)
	wantPage(t, "pending blocker", blocked, http.StatusOK, "Ожидают подтверждения: 1", "Открыть кассу")
	if strings.Contains(blocked.Body.String(), ">Закрыть месяц</a>") {
		t.Fatal("pending month exposes close action")
	}
	wantPage(t, "previous month blocker", s.get("/admin/month?m=2026-12&tab=settlement", cookie), http.StatusOK, "Сначала закройте предыдущий месяц")
	for _, m := range []string{"2026-10", "bogus", "2027-01"} {
		wantPage(t, "invalid selection", s.get("/admin/month?m="+m+"&tab=settlement", cookie), http.StatusBadRequest, "Проверьте месяц")
	}
}

func TestMissingTermsGiveActionableBlockedSettlement(t *testing.T) {
	s := newSite(t)
	_, err := s.st.RecordMovement(ctx, s.anna.Actor(), store.NewMovement{Type: domain.Deposit, Amount: 5000, From: domain.Person(s.olga.ID), To: domain.Account(domain.KassaCash), Date: s.st.Today()})
	if err != nil {
		t.Fatal(err)
	}
	p := s.get("/admin/month?m=2026-11&tab=settlement", s.login("anna", annaPass))
	wantPage(t, "missing terms", p, http.StatusOK, "Не хватает условий сотрудника", "проверьте условия за выбранный месяц")
	if strings.Contains(p.Body.String(), ">Закрыть месяц</a>") {
		t.Fatal("missing terms allows close")
	}
}

func TestSettlementHistoricalGapsUseCreatedDisabledAndReactivatedDates(t *testing.T) {
	s := newSite(t)
	cookie := s.login("anna", annaPass)
	*s.clock = s.clock.AddDate(0, 0, 1)
	_, err := s.st.UpdateUser(ctx, s.anna.Actor(), s.maria.ID, func(u *store.User) error { u.Active = false; return nil })
	if err != nil {
		t.Fatal(err)
	}
	vera := s.person("vera", "Вера", "vera-password")
	*s.clock = s.clock.AddDate(0, 0, 1)
	_, err = s.st.UpdateUser(ctx, s.anna.Actor(), s.maria.ID, func(u *store.User) error { u.Active = true; return nil })
	if err != nil {
		t.Fatal(err)
	}
	body := s.get("/admin/month?m=2026-11&tab=settlement", cookie).Body.String()
	for _, href := range []string{"/admin/reports/" + s.maria.ID + "/2026-11-20", "/admin/reports/" + s.maria.ID + "/2026-11-22", "/admin/reports/" + vera.ID + "/2026-11-21"} {
		if !strings.Contains(body, href) {
			t.Fatalf("missing historical gap %s", href)
		}
	}
	for _, href := range []string{"/admin/reports/" + s.maria.ID + "/2026-11-21", "/admin/reports/" + vera.ID + "/2026-11-20"} {
		if strings.Contains(body, href) {
			t.Fatalf("invented historical gap %s", href)
		}
	}
}

func TestMonthTabsAnnounceOnlyTheSelectedSection(t *testing.T) {
	s := newSite(t)
	cookie := s.login("anna", annaPass)
	for _, c := range []struct{ path, label string }{
		{"/admin/month?m=2026-11", "Выручка"},
		{"/admin/month?m=2026-11&tab=settlement", "Итоги и расчёт"},
	} {
		body := s.get(c.path, cookie).Body.String()
		nav := regexp.MustCompile(`(?s)<nav[^>]*aria-label="Раздел месяца"[^>]*>(.*?)</nav>`).FindStringSubmatch(body)
		if len(nav) != 2 {
			t.Fatal("missing month tabs")
		}
		links := regexp.MustCompile(`<a[^>]*>[^<]*</a>`).FindAllString(nav[1], -1)
		if len(links) != 2 {
			t.Fatal("missing section links")
		}
		for _, link := range links {
			if strings.Contains(link, ">"+c.label+"</a>") {
				if !strings.Contains(link, `aria-current="page"`) {
					t.Fatalf("active section unannounced: %s", link)
				}
			} else if strings.Contains(link, "aria-current=") {
				t.Fatalf("inactive section marked current: %s", link)
			}
		}
	}
}

func TestMonthConfirmationKeepsPendingBlockerAndWarningOnlyCloseBehavior(t *testing.T) {
	s := newSite(t)
	s.withTerms(s.olga, 7000)
	running := s.get("/admin/month/2026-11/close", s.login("anna", annaPass))
	wantPage(t, "unfinished confirmation", running, http.StatusOK, "Месяц ещё не закончился", "Обновить расчёт")
	*s.clock = s.clock.AddDate(0, 1, 0)
	cookie := s.login("anna", annaPass)
	pending, err := s.st.RecordMovement(ctx, s.olga.Actor(), store.NewMovement{Type: domain.Deposit, Amount: 100, From: domain.Person(s.olga.ID), To: domain.Account(domain.KassaCash), Date: domain.Date{Year: 2026, Mon: 11, Day: 20}})
	if err != nil {
		t.Fatal(err)
	}
	blocked := s.get("/admin/month/2026-11/close", cookie)
	wantPage(t, "pending confirmation", blocked, http.StatusOK, "Ожидают подтверждения: 1", "Обновить расчёт")
	if _, err := s.st.ConfirmMovement(ctx, s.anna.Actor(), pending.ID, pending.Version); err != nil {
		t.Fatal(err)
	}
	confirmation := s.get("/admin/month/2026-11/close", cookie)
	wantPage(t, "ready confirmation", confirmation, http.StatusOK, "Закрыть месяц", `name="revision"`, "Зарплаты")
	for _, unused := range []string{"Не сдали отчёт", "Текущий остаток", "Пересчёта наличных нет", `id="movement-form"`} {
		if strings.Contains(confirmation.Body.String(), unused) {
			t.Fatalf("confirmation includes page-only data: %s", unused)
		}
	}
	revision := hiddenRevision(t, confirmation.Body.String())
	result := s.do(request{method: http.MethodPost, path: "/admin/month/2026-11/close", form: url.Values{"revision": {revision}}, cookies: cookie, datastar: true})
	wantPage(t, "warning-only SSE close", result, http.StatusOK, "datastar-patch-signals", "redirectUrl")
	state, err := s.st.Month(ctx, domain.Month{Year: 2026, Mon: 11})
	if err != nil || state.Status != domain.MonthClosed {
		t.Fatalf("warning-only close %+v %v", state, err)
	}
}

func TestMonthAmbiguousResponseAdviceChecksMonthStateBeforeRepeating(t *testing.T) {
	s := settlementSite(t)
	cookie := s.login("anna", annaPass)
	for _, action := range []string{"close", "reopen"} {
		if action == "reopen" {
			if _, err := s.st.CloseMonth(ctx, s.anna.Actor(), domain.Month{Year: 2026, Mon: 10}); err != nil {
				t.Fatal(err)
			}
		}
		p := s.get("/admin/month/2026-10/"+action, cookie)
		wantPage(t, "month-specific ambiguous advice", p, http.StatusOK, "Не удалось получить ответ", "Обновите страницу и проверьте состояние месяца перед повторным подтверждением")
		if strings.Contains(p.Body.String(), "Проверьте движения и текущий баланс") {
			t.Fatal("month confirmation advises unrelated movement balance")
		}
	}
	generic := s.get("/admin/movements/new?type=deposit", cookie)
	wantPage(t, "generic movement advice preserved", generic, http.StatusOK, "Проверьте движения и текущий баланс перед повторным сохранением")
}

func TestOversizedSettlementFormPreservesAmountWithActionablePlainAndSSEErrors(t *testing.T) {
	for _, kind := range []domain.MovementType{domain.Payout, domain.Deposit} {
		t.Run(string(kind), func(t *testing.T) {
			s := newSite(t)
			cookie := s.login("anna", annaPass)
			initial := store.NewMovement{Type: domain.Deposit, Amount: 5000, From: domain.Person(s.olga.ID), To: domain.Account(domain.KassaCard), Date: s.st.Today()}
			from, to := "account:kassa_card", "person:"+s.olga.ID
			expected := domain.Cents(5000)
			if kind == domain.Deposit {
				initial.Type = domain.Payout
				initial.From, initial.To = initial.To, initial.From
				from, to = to, from
				expected = -5000
			}
			if _, err := s.st.RecordMovement(ctx, s.anna.Actor(), initial); err != nil {
				t.Fatal(err)
			}
			page := s.get("/admin/movements/new?type="+string(kind)+"&settlement=2026-11&person="+s.olga.ID, cookie)
			revision := hiddenValue(t, page.Body.String(), "settlementRevision")
			form := url.Values{"type": {string(kind)}, "amount": {"60"}, "date": {"2026-11-20"}, "from": {from}, "to": {to}, "settlement": {"2026-11"}, "settlementPerson": {s.olga.ID}, "settlementRevision": {revision}}
			for _, sse := range []bool{false, true} {
				response := s.do(request{method: http.MethodPost, path: "/admin/movements", form: form, cookies: cookie, datastar: sse})
				status := http.StatusUnprocessableEntity
				if sse {
					status = http.StatusOK
				}
				wantPage(t, "oversized settlement error", response, status, "Сумма больше текущего остатка", "уменьшите сумму или обновите расчёт", `name="amount" value="60"`)
				if sse && !strings.Contains(response.Body.String(), "datastar-patch-elements") {
					t.Fatal("SSE amount error did not patch form")
				}
				balance, err := s.st.PersonBalance(ctx, s.olga.ID)
				if err != nil || balance != expected {
					t.Fatalf("HTTP rejection changed balance %d %v", balance, err)
				}
			}
		})
	}
}
