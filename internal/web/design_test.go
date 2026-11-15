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

// 2026-11-15 design review: every view switcher (month, history, category
// kind, trend period, movement type) is the same full-width tab box.
func TestViewSwitchersShareOneTabStyle(t *testing.T) {
	s := newSite(t)
	admin := s.login("anna", annaPass)
	for _, path := range []string{"/admin/month", "/admin/history", "/admin/categories", "/admin/trends", "/admin/movements/new"} {
		body := s.get(path, admin).Body.String()
		if !strings.Contains(body, `class="tabs tabs-box w-full`) {
			t.Errorf("%s: no full-width tab box", path)
		}
		if strings.Contains(body, "join-item btn") {
			t.Errorf("%s: still uses joined buttons as a view switcher", path)
		}
	}
}

// 2026-11-15 design review: at lg: movement lists read as a table, laid out
// from the same elements, so each movement still has exactly one confirm
// form and one void dialog (a duplicated table would open two).
func TestMovementListIsATableOnDesktopWithoutDuplicates(t *testing.T) {
	s := newSite(t)
	if _, err := s.st.RecordMovement(ctx, s.olga.Actor(), store.NewMovement{Type: domain.Deposit, Amount: 100, From: domain.Person(s.olga.ID), To: domain.Account(domain.KassaCash), Date: domain.Date{Year: 2026, Mon: 11, Day: 20}}); err != nil {
		t.Fatal(err)
	}
	body := s.get("/admin/kassa", s.login("anna", annaPass)).Body.String()
	if !strings.Contains(body, `<span>Дата</span><span>Движение</span><span class="text-right">Сумма</span>`) {
		t.Error("no desktop column header over the movement list")
	}
	ids := regexp.MustCompile(`id="movement-action-([^"]+)-confirm"`).FindAllStringSubmatch(body, -1)
	if len(ids) != 1 {
		t.Fatalf("want one confirm form for the pending movement, got %d", len(ids))
	}
	if n := strings.Count(body, `class="modal modal-bottom sm:modal-middle" data-ledger="`+ids[0][1]+`"`); n != 1 {
		t.Errorf("the pending movement has %d void dialogs, want 1", n)
	}
}

// 2026-11-15 production review: 36 of 50 change-log cards were empty. A
// client save bumps each category's usage counter, which is not a change
// anyone made; and a booking link is a real change the card did not show.
func TestChangeLogHasNoEmptyCards(t *testing.T) {
	s := newSite(t)
	if err := s.st.SetTerms(ctx, s.anna.Actor(), s.olga.ID, s.st.StartMonth(), domain.Terms{SelfPct: 5000, DividendWeight: 1}, ""); err != nil {
		t.Fatal(err)
	}
	saved, err := s.st.SaveClientReport(ctx, s.anna.Actor(), store.ClientEntry{
		Token: "test-client-token-change-log", Person: s.olga.ID, Date: domain.Date{Year: 2026, Mon: 11, Day: 20},
		Input: domain.ClientInput{Services: 10000, ServiceCash: 5000, Extras: map[string]domain.Cents{"creams": 1200}, ExtrasDestination: domain.ToCash},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.st.LinkClientBooking(ctx, s.anna.Actor(), saved.ID, saved.Version, domain.BookingRef{Calendar: "cal", Event: "ev"}); err != nil {
		t.Fatal(err)
	}
	body := s.get("/admin/audit?from=2026-11-20&to=2026-11-20", s.login("anna", annaPass)).Body.String()
	for _, card := range regexp.MustCompile(`(?s)<article\b.*?</article>`).FindAllString(body, -1) {
		if !strings.Contains(card, "<dl") && !strings.Contains(card, "<details") {
			t.Errorf("empty change-log card: %s", regexp.MustCompile(`<[^>]+>`).ReplaceAllString(card, " "))
		}
	}
	if !strings.Contains(body, "Запись в календаре") {
		t.Error("the booking link is not shown")
	}
}

// 2026-11-15 production review: the admin's movement form preselected the
// first person (the owner) as «Откуда» for a deposit and «Куда» for a
// payout. A person must be chosen; accounts and employees' own forms keep
// their defaults.
func TestAdminMovementFormMakesYouChooseThePerson(t *testing.T) {
	s := newSite(t)
	admin := s.login("anna", annaPass)
	for kind, field := range map[string]string{"deposit": "from", "payout": "to"} {
		body := s.get("/admin/movements/new?type="+kind, admin).Body.String()
		sel := regexp.MustCompile(`(?s)<select name="` + field + `".*?</select>`).FindString(body)
		if !strings.Contains(sel, `<option value="" selected disabled>Выберите сотрудника</option>`) {
			t.Errorf("%s: «%s» does not start with «Выберите сотрудника»: %s", kind, field, sel)
		}
		if strings.Contains(sel, `value="person:`) && regexp.MustCompile(`value="person:[^"]+" selected`).MatchString(sel) {
			t.Errorf("%s: a person is preselected", kind)
		}
	}
	own := s.get("/movements/new?type=deposit", s.login("olga", olgaPass)).Body.String()
	if strings.Contains(own, "Выберите сотрудника") {
		t.Error("an employee's own form asks to choose a person")
	}
}

// Owner, 2026-11-15: Касса's recent movements are sorted by date, newest
// first, whatever order they were entered in; a backfill still shows when it
// was entered.
func TestRecentKassaMovementsAreSortedByDate(t *testing.T) {
	s := newSite(t)
	for _, day := range []int{10, 3} { // the older one is entered last
		if _, err := s.st.RecordMovement(ctx, s.anna.Actor(), store.NewMovement{Type: domain.Expense, Amount: 1000, From: domain.Account(domain.KassaCash), To: domain.Outside, Date: domain.Date{Year: 2026, Mon: 11, Day: day}, Note: "кофе", ExpenseCategory: "materials"}); err != nil {
			t.Fatal(err)
		}
	}
	body := s.get("/admin/kassa", s.login("anna", annaPass)).Body.String()
	recent := body[strings.Index(body, "Последние движения"):]
	if i, j := strings.Index(recent, "10.11.2026"), strings.Index(recent, "03.11.2026"); i < 0 || j < 0 || i > j {
		t.Errorf("recent movements are not newest date first (10.11 at %d, 03.11 at %d)", i, j)
	}
	if !strings.Contains(body, "записано 20.11 · ") || strings.Contains(body, "записано 20.11.2026") {
		t.Error("a backfilled movement does not show when it was entered")
	}
}

// 2026-11-15 production review: «Мои записи» said «Расчёт: 0» and showed
// «Движения за месяц» over nothing.
func TestMyPageEmptyStatesSaySo(t *testing.T) {
	s := newSite(t)
	s.withTerms(s.olga, 6500)
	body := s.get("/my", s.login("olga", olgaPass)).Body.String()
	if strings.Contains(body, "Расчёт: 0") || !strings.Contains(body, "Расчёт с салоном: €0") {
		t.Error("a zero balance does not read «Расчёт с салоном: €0»")
	}
	if !strings.Contains(body, "Движений за месяц нет.") {
		t.Error("an empty movement list shows a heading over nothing")
	}
}

// 2026-11-15 production review: on «День» the client picker (one big button
// per person) sat between the totals and the breakdown, and an empty day
// showed «Услуги и продажи», «Сертификаты» and «Клиенты 0».
func TestAdminDayKeepsThePickerCompactAndHidesEmptySections(t *testing.T) {
	s := newSite(t)
	s.withTerms(s.olga, 6500)
	body := s.get("/admin/day?date=2026-11-20", s.login("anna", annaPass)).Body.String()
	picker := strings.Index(body, `<summary id="add-client-title" class="btn btn-primary btn-lg w-full`)
	if picker < 0 || picker > strings.Index(body, "Сверка дня по записям") && strings.Contains(body, "Сверка дня по записям") {
		t.Error("the client picker is not one control above the day's totals")
	}
	for _, empty := range []string{">Услуги и продажи<", ">Сертификаты<"} {
		if strings.Contains(body, empty) {
			t.Errorf("an empty day shows %s", empty)
		}
	}
	if strings.Index(body, "Отчётов за этот день нет.") < strings.Index(body, "Записи за день") {
		t.Error("the empty note is not under «Записи за день»")
	}
}

// Money audit follow-up: a deposit or payout sent without a person is
// refused on that field and books nothing.
func TestAdminMovementWithoutAPersonIsRefused(t *testing.T) {
	s := newSite(t)
	admin := s.login("anna", annaPass)
	for kind, field := range map[string]string{"deposit": "from", "payout": "to"} {
		form := s.get("/admin/movements/new?type="+kind, admin).Body.String()
		v := url.Values{"type": {kind}, "amount": {"10"}, "date": {"2026-11-20"}, "token": {hiddenValue(t, form, "token")}}
		if kind == "deposit" {
			v.Set("to", "account:"+domain.KassaCash)
		} else {
			v.Set("from", "account:"+domain.KassaCash)
		}
		res := s.post("/admin/movements", v, admin)
		if res.Code != http.StatusUnprocessableEntity {
			t.Fatalf("%s without a person: %d", kind, res.Code)
		}
		if !strings.Contains(res.Body.String(), "Выберите сотрудника") || !regexp.MustCompile(`<select name="`+field+`" class="[^"]*select-error`).MatchString(res.Body.String()) {
			t.Errorf("%s: the error is not on «%s»", kind, field)
		}
	}
	es, err := s.st.LedgerBetween(ctx, domain.Date{Year: 2026, Mon: 11, Day: 1}, domain.Date{Year: 2026, Mon: 11, Day: 30})
	if err != nil {
		t.Fatal(err)
	}
	if len(es) != 0 {
		t.Errorf("a movement without a person was booked: %d rows", len(es))
	}
}

// Money audit follow-up: opening balances stay out of Касса's recent list.
func TestKassaRecentLeavesOutOpenings(t *testing.T) {
	s := newSite(t)
	if _, err := s.st.RecordOpenings(ctx, s.anna.Actor(), []store.NewMovement{{Type: domain.Opening, Amount: 10000, From: domain.Salon, To: domain.Account(domain.KassaCash), Date: s.st.StartMonth().FirstDay()}}); err != nil {
		t.Fatal(err)
	}
	body := s.get("/admin/kassa", s.login("anna", annaPass)).Body.String()
	if strings.Contains(body, "Начальный остаток") {
		t.Error("an opening balance is listed among recent kassa movements")
	}
}

// Third production review: «Отчёт · Изменение» cards were empty when an edit
// changed nothing the card lists. Such an edit says «Суммы не изменились».
func TestARecordEditWithoutNewAmountsSaysSo(t *testing.T) {
	s := newSite(t)
	if err := s.st.SetTerms(ctx, s.anna.Actor(), s.olga.ID, s.st.StartMonth(), domain.Terms{SelfPct: 5000, DividendWeight: 1}, ""); err != nil {
		t.Fatal(err)
	}
	entry := store.ClientEntry{Token: "test-client-token-resave", Person: s.olga.ID, Date: domain.Date{Year: 2026, Mon: 11, Day: 20},
		Input: domain.ClientInput{Services: 10000, ServiceCash: 5000, ExtrasDestination: domain.ToCash}}
	saved, err := s.st.SaveClientReport(ctx, s.anna.Actor(), entry)
	if err != nil {
		t.Fatal(err)
	}
	entry.Version = saved.Version
	if _, err := s.st.SaveClientReport(ctx, s.anna.Actor(), entry); err != nil {
		t.Fatal(err)
	}
	body := s.get("/admin/audit?from=2026-11-20&to=2026-11-20&entity=report", s.login("anna", annaPass)).Body.String()
	for _, card := range regexp.MustCompile(`(?s)<article\b.*?</article>`).FindAllString(body, -1) {
		if !strings.Contains(card, "<dl") && !strings.Contains(card, "<details") {
			t.Errorf("empty card: %s", regexp.MustCompile(`<[^>]+>`).ReplaceAllString(card, " "))
		}
	}
	if !strings.Contains(body, "Суммы") || !strings.Contains(body, "не изменились") {
		t.Error("an edit without new amounts does not say so")
	}
}
