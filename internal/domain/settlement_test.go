package domain_test

import (
	"errors"
	"testing"

	"github.com/lieranderl/aestheticlab-income-app/internal/domain"
)

// staffTerms are the salon's terms as in the sheet's «2 Debit Credit» tab:
// «Себе» = 100% − Salon Split %, Anna's €1 200 salary, dividends 1/3 to
// Anna, Maria and Olga.
func staffTerms(t *testing.T) map[string]domain.TermsHistory {
	nov := month(t, "2026-11")
	return map[string]domain.TermsHistory{
		"anna":  {nov: {SelfPct: 4500, SalaryCents: 120000, DividendWeight: 1}},
		"maria": {nov: {SelfPct: 6500, DividendWeight: 1}},
		"olga":  {nov: {SelfPct: 6500, DividendWeight: 1}},
		"vera":  {nov: {SelfPct: 4000}},
	}
}

// sheetSeptember rebuilds the sheet's September 2026 inputs as one month of
// app data: each person's revenue as a cash day whose cash they kept, the
// money they returned as deposits, staff-paid and kassa expenses, and the
// payouts already made.
func sheetSeptember(t *testing.T) domain.MonthInput {
	t.Helper()
	nov := month(t, "2026-11")
	day := date(t, "2026-11-15")
	terms := staffTerms(t)
	people := []struct {
		id                  string
		revenue, returned   domain.Cents
		staffExpense, payed domain.Cents
	}{
		{"maria", 742100, 205000, 4800, 0},
		{"olga", 568000, 151000, 21500, 0},
		{"vera", 81000, 81000, 0, 41250},
		{"anna", 143000, 143000, 0, 195000},
	}
	in := domain.MonthInput{Month: nov, Terms: terms}
	for _, p := range people {
		pct := terms[p.id][nov].SelfPct
		ri := domain.ReportInput{
			Revenue:     map[string]domain.Cents{"services": p.revenue},
			Payments:    map[string]domain.Cents{"cash": p.revenue},
			SelfPct:     pct,
			CashToKassa: ptr(0),
		}
		r := mustReport(t, ri)
		in.Reports = append(in.Reports, domain.PersonReport{Person: p.id, Date: day, Report: r, Revenue: ri.Revenue, Payments: ri.Payments})
		in.Movements = append(in.Movements, dated(confirmed(domain.Deposit, p.returned, domain.Person(p.id), cash), day))
		if p.staffExpense > 0 {
			e := confirmed(domain.Expense, p.staffExpense, domain.Person(p.id), domain.Outside)
			e.ExpenseCategory = "materials"
			in.Movements = append(in.Movements, dated(e, day))
		}
		if p.payed > 0 {
			in.Movements = append(in.Movements, dated(confirmed(domain.Payout, p.payed, card, domain.Person(p.id)), day))
		}
	}
	// Salon expense rows were €2 869, of which €263 paid by Maria and Olga.
	kassa := confirmed(domain.Expense, 286900-4800-21500, card, domain.Outside)
	kassa.ExpenseCategory = "rent"
	in.Movements = append(in.Movements, dated(kassa, day))
	return in
}

func dated(m domain.Movement, d domain.Date) domain.Movement { m.Date = d; return m }

func TestTheSheetsSeptemberSettlementIsReproduced(t *testing.T) {
	c, err := domain.CloseMonth(sheetSeptember(t))
	if err != nil {
		t.Fatal(err)
	}
	res := c.Result
	if res.SalonIncome != 585785 || res.Expenses != 286900 || res.Salaries != 120000 || res.Profit != 178885 {
		t.Fatalf("income %d expenses %d salaries %d profit %d; want 585785 286900 120000 178885",
			res.SalonIncome, res.Expenses, res.Salaries, res.Profit)
	}
	// €1 788,85 / 3 = €596,283…: the left-over cent goes to the first owner by
	// ID (anna). The sheet shows 596,28 for everyone because it keeps the
	// fraction, so Anna's balance is one cent above the sheet's 489,78.
	wantDividend := map[string]domain.Cents{"anna": 59629, "maria": 59628, "olga": 59628, "vera": 0}
	wantBalance := map[string]domain.Cents{"maria": 9693, "olga": 33328, "vera": -8850, "anna": 48979}
	for p := range wantBalance {
		if got := res.People[p].Dividend; got != wantDividend[p] {
			t.Errorf("%s dividend = %d, want %d", p, got, wantDividend[p])
		}
		if got := c.Balances.Persons[p]; got != wantBalance[p] {
			t.Errorf("%s balance = %d, want %d", p, got, wantBalance[p])
		}
	}
	if got := res.People["maria"]; got.Revenue != 742100 || got.Self != 482365 || got.Earned != 482365+59628 || got.WorkingDays != 1 {
		t.Errorf("maria = %+v", got)
	}
	if res.ExpensesByCategory["materials"] != 26300 || res.ExpensesByCategory["rent"] != 260600 {
		t.Errorf("expenses by category = %v", res.ExpensesByCategory)
	}
}

func TestClosingAccruesSalaryAndDividendsOnTheLastDay(t *testing.T) {
	c, err := domain.CloseMonth(sheetSeptember(t))
	if err != nil {
		t.Fatal(err)
	}
	var salary, dividends int
	for _, a := range c.Accruals {
		if a.Status != domain.Confirmed || a.Date.String() != "2026-11-30" || a.From != domain.Salon {
			t.Errorf("accrual %+v", a)
		}
		switch a.Type {
		case domain.Salary:
			salary++
			if a.To != anna || a.Amount != 120000 {
				t.Errorf("salary accrual %+v", a)
			}
		case domain.Dividend:
			dividends++
		}
	}
	if salary != 1 || dividends != 3 {
		t.Fatalf("%d salary and %d dividend accruals, want 1 and 3 (none for zero amounts)", salary, dividends)
	}
}

func TestALossMonthPaysNoDividends(t *testing.T) {
	in := sheetSeptember(t)
	big := confirmed(domain.Expense, 200000, card, domain.Outside)
	big.ExpenseCategory = "equipment"
	in.Movements = append(in.Movements, dated(big, date(t, "2026-11-20")))
	c, err := domain.CloseMonth(in)
	if err != nil {
		t.Fatal(err)
	}
	if c.Result.Profit >= 0 || c.Result.DividendPool != 0 {
		t.Fatalf("profit %d pool %d; want a loss and no pool", c.Result.Profit, c.Result.DividendPool)
	}
	for _, a := range c.Accruals {
		if a.Type == domain.Dividend {
			t.Fatalf("dividend accrued in a loss month: %+v", a)
		}
	}
}

func TestAMonthIsSettledWithTheTermsInForceThenNotToday(t *testing.T) {
	in := sheetSeptember(t)
	// Anna's salary rises to €1 400 from December; November is closed again later.
	in.Terms["anna"][month(t, "2026-12")] = domain.Terms{SelfPct: 4500, SalaryCents: 140000, DividendWeight: 1}
	c, err := domain.CloseMonth(in)
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Result.People["anna"].Salary; got != 120000 {
		t.Fatalf("November salary = %d, want 120000", got)
	}
	if c.Terms["anna"].SalaryCents != 120000 {
		t.Fatalf("snapshot records terms %+v, want November's", c.Terms["anna"])
	}
}

func TestAMonthsSnapshotContainsOnlyThatMonth(t *testing.T) {
	in := sheetSeptember(t)
	december := dated(confirmed(domain.Deposit, 5000, anna, cash), date(t, "2026-12-02"))
	in.Movements = append(in.Movements, december)
	if _, err := domain.CloseMonth(in); problem(t, err) != domain.ProblemOutsideMonth {
		t.Fatalf("closing November with a December row: %v, want %q", err, domain.ProblemOutsideMonth)
	}
	in = sheetSeptember(t)
	late := in.Reports[0]
	late.Date = date(t, "2026-12-01")
	in.Reports = append(in.Reports, late)
	if _, err := domain.CloseMonth(in); problem(t, err) != domain.ProblemOutsideMonth {
		t.Fatalf("closing November with a December report: %v", err)
	}
}

func TestClosingThenFoldingLaterMonthsCountsEverythingOnce(t *testing.T) {
	nov := sheetSeptember(t)
	closing, err := domain.CloseMonth(nov)
	if err != nil {
		t.Fatal(err)
	}
	decRow := dated(confirmed(domain.Payout, 48979, card, anna), date(t, "2026-12-03"))
	afterClose := fold(t, closing.Balances, nil, decRow)

	all := append(append([]domain.Movement{}, nov.Movements...), closing.Accruals...)
	all = append(all, decRow)
	direct := fold(t, domain.Balances{}, nov.Reports, all...)
	for _, p := range []string{"anna", "maria", "olga", "vera"} {
		if afterClose.Persons[p] != direct.Persons[p] {
			t.Errorf("%s: snapshot+December %d != everything at once %d", p, afterClose.Persons[p], direct.Persons[p])
		}
	}
	if afterClose.Persons["anna"] != 0 {
		t.Errorf("after paying her €489,79, anna = %d, want 0", afterClose.Persons["anna"])
	}
}

func TestClosingIsRefusedWhileMovementsArePending(t *testing.T) {
	in := sheetSeptember(t)
	p := dated(confirmed(domain.Deposit, 1000, olga, cash), date(t, "2026-11-20"))
	p.Status = domain.Pending
	in.Movements = append(in.Movements, p)
	if _, err := domain.CloseMonth(in); problem(t, err) != domain.ProblemPendingMovements {
		t.Fatalf("err = %v, want %q", err, domain.ProblemPendingMovements)
	}
}

func TestAPersonWithoutTermsForTheMonthIsAnError(t *testing.T) {
	in := sheetSeptember(t)
	delete(in.Terms, "vera")
	_, err := domain.CloseMonth(in)
	var ve *domain.ValidationError
	if !errors.As(err, &ve) || ve.Problem != domain.ProblemMissingTerms {
		t.Fatalf("err = %v, want %q", err, domain.ProblemMissingTerms)
	}
}

// Someone whose money moves in the month, or who carries a balance into it,
// needs terms too; otherwise the balances would hold their money while the
// settlement left them out.
func TestEveryoneWithMoneyInTheMonthNeedsTerms(t *testing.T) {
	sep := date(t, "2026-11-15") // the sheet's September, replayed as November
	for name, change := range map[string]func(*domain.MonthInput){
		"a deposit": func(in *domain.MonthInput) {
			in.Movements = append(in.Movements, dated(confirmed(domain.Deposit, 100, domain.Person("new"), cash), sep))
		},
		"a payout": func(in *domain.MonthInput) {
			in.Movements = append(in.Movements, dated(confirmed(domain.Payout, 100, cash, domain.Person("new")), sep))
		},
		"an opening balance": func(in *domain.MonthInput) {
			in.Opening = domain.Balances{Persons: map[string]domain.Cents{"new": -250}}
		},
	} {
		in := sheetSeptember(t)
		change(&in)
		_, err := domain.CloseMonth(in)
		var ve *domain.ValidationError
		if !errors.As(err, &ve) || ve.Problem != domain.ProblemMissingTerms {
			t.Errorf("%s without terms: err = %v, want %q", name, err, domain.ProblemMissingTerms)
		}
	}
	in := sheetSeptember(t)
	voided := dated(confirmed(domain.Deposit, 100, domain.Person("new"), cash), sep)
	voided.Status = domain.Voided
	in.Movements = append(in.Movements, voided)
	if _, err := domain.CloseMonth(in); err != nil {
		t.Errorf("a voided row needs no terms: %v", err)
	}
}

func TestDividendsSplitExactlyByWeight(t *testing.T) {
	cases := []struct {
		pool    domain.Cents
		weights map[string]int64
		want    map[string]domain.Cents
	}{
		{178885, map[string]int64{"olga": 1, "anna": 1, "maria": 1}, map[string]domain.Cents{"anna": 59629, "maria": 59628, "olga": 59628}},
		{100, map[string]int64{"a": 2, "b": 1}, map[string]domain.Cents{"a": 67, "b": 33}},
		{5, map[string]int64{"a": 1, "b": 1, "c": 0}, map[string]domain.Cents{"a": 3, "b": 2, "c": 0}},
		{0, map[string]int64{"a": 1}, map[string]domain.Cents{"a": 0}},
		{100, map[string]int64{"a": 0}, map[string]domain.Cents{"a": 0}},
	}
	for _, c := range cases {
		got := domain.SplitByWeight(c.pool, c.weights)
		var sum domain.Cents
		for k, v := range c.want {
			if got[k] != v {
				t.Errorf("split %d %v: %s = %d, want %d", c.pool, c.weights, k, got[k], v)
			}
			sum += got[k]
		}
		if anyWeight(c.weights) && sum != c.pool {
			t.Errorf("split %d %v sums to %d", c.pool, c.weights, sum)
		}
	}
}

func anyWeight(w map[string]int64) bool {
	for _, v := range w {
		if v > 0 {
			return true
		}
	}
	return false
}

func TestTheSettlementScreenSuggestsOneActionPerPerson(t *testing.T) {
	cases := []struct {
		balance domain.Cents
		want    domain.Action
	}{
		{9693, domain.Action{Kind: domain.SalonPays, Amount: 9693}},
		{-8850, domain.Action{Kind: domain.PersonPays, Amount: 8850}},
		{0, domain.Action{Kind: domain.Settled}},
	}
	for _, c := range cases {
		if got := domain.SuggestAction(c.balance); got != c.want {
			t.Errorf("SuggestAction(%d) = %+v, want %+v", c.balance, got, c.want)
		}
	}
}

func TestAMonthAboveTheThresholdEarnsTheHigherSelfPercentForTheWholeMonth(t *testing.T) {
	in := sheetSeptember(t)
	nov := month(t, "2026-11")
	// Maria: 65%, or 75% for the whole month when her «Себе» base passes
	// €6 000 (it is €7 421). Olga's threshold of €7 000 is not reached.
	in.Terms["maria"][nov] = domain.Terms{SelfPct: 6500, DividendWeight: 1, HighSelfFromCents: 600000, HighSelfPct: 7500}
	in.Terms["olga"][nov] = domain.Terms{SelfPct: 6500, DividendWeight: 1, HighSelfFromCents: 700000, HighSelfPct: 7500}
	c, err := domain.CloseMonth(in)
	if err != nil {
		t.Fatal(err)
	}
	res := c.Result
	// 10% more of €7 421 is €742,10; it is the salon's cost before dividends.
	if res.SelfBonuses != 74210 || res.Profit != 178885-74210 {
		t.Fatalf("self bonuses %d profit %d; want 74210 and %d", res.SelfBonuses, res.Profit, 178885-74210)
	}
	// €1 046,75 / 3: the two left-over cents go to anna and maria.
	maria := res.People["maria"]
	if maria.SelfBonus != 74210 || maria.Earned != 482365+74210+34892 {
		t.Errorf("maria = %+v", maria)
	}
	var dividends domain.Cents
	for p, want := range map[string]domain.Cents{"anna": 34892, "maria": 34892, "olga": 34891, "vera": 0} {
		if got := res.People[p].Dividend; got != want {
			t.Errorf("%s dividend = %d, want %d", p, got, want)
		}
		dividends += res.People[p].Dividend
	}
	if dividends != res.DividendPool {
		t.Errorf("dividends %d of pool %d", dividends, res.DividendPool)
	}
	if trend := domain.Trend([]domain.Month{nov}, map[domain.Month]domain.MonthResult{nov: res}, nil); trend[0].SelfBonuses != 74210 {
		t.Errorf("trend «Себе» bonuses = %d", trend[0].SelfBonuses)
	}
	if res.People["olga"].SelfBonus != 0 {
		t.Errorf("olga below her threshold got %d", res.People["olga"].SelfBonus)
	}
	if got := c.Balances.Persons["maria"]; got != 9693-59628+74210+34892 {
		t.Errorf("maria balance = %d", got)
	}
	var bonuses []domain.Movement
	for _, a := range c.Accruals {
		if a.Type == domain.SelfBonus {
			bonuses = append(bonuses, a)
		}
	}
	if len(bonuses) != 1 || bonuses[0].To != domain.Person("maria") || bonuses[0].Amount != 74210 || bonuses[0].Date.String() != "2026-11-30" || bonuses[0].From != domain.Salon {
		t.Fatalf("self bonus accruals = %+v", bonuses)
	}
}

func TestTheHigherSelfPercentStartsAboveTheThreshold(t *testing.T) {
	terms := domain.Terms{SelfPct: 5000, HighSelfFromCents: 500000, HighSelfPct: 7000}
	for _, c := range []struct {
		base, want domain.Cents
	}{
		{0, 0},
		{499999, 0},
		{500000, 0},      // exactly the threshold is not «больше»
		{500001, 100000}, // 20% of €5 000,01 = €1 000,002
		{600000, 120000}, // the whole month: 70% − 50% of €6 000
	} {
		if got := domain.SelfBonusFor(c.base, c.base, terms); got != c.want {
			t.Errorf("base %d: bonus %d, want %d", c.base, got, c.want)
		}
	}
	if got := domain.SelfBonusFor(900000, 900000, domain.Terms{SelfPct: 5000}); got != 0 {
		t.Errorf("no threshold: bonus %d", got)
	}
}

func TestTermsAreChecked(t *testing.T) {
	for _, c := range []struct {
		name  string
		terms domain.Terms
		ok    bool
	}{
		{"plain", domain.Terms{SelfPct: 5000, SalaryCents: 100000, DividendWeight: 1}, true},
		{"tiered", domain.Terms{SelfPct: 5000, HighSelfFromCents: 500000, HighSelfPct: 7000}, true},
		{"self above 100%", domain.Terms{SelfPct: 10001}, false},
		{"negative salary", domain.Terms{SalaryCents: -1}, false},
		{"negative share", domain.Terms{DividendWeight: -1}, false},
		{"threshold without percent", domain.Terms{SelfPct: 5000, HighSelfFromCents: 500000}, false},
		{"percent without threshold", domain.Terms{SelfPct: 5000, HighSelfPct: 7000}, false},
		{"higher percent not higher", domain.Terms{SelfPct: 7000, HighSelfFromCents: 500000, HighSelfPct: 7000}, false},
		{"higher percent above 100%", domain.Terms{SelfPct: 7000, HighSelfFromCents: 500000, HighSelfPct: 10001}, false},
	} {
		if err := domain.ValidateTerms(c.terms); (err == nil) != c.ok {
			t.Errorf("%s: err = %v", c.name, err)
		}
	}
}

// tiered gives Maria 65%, or high% above threshold, in the reference month.
func tiered(t *testing.T, threshold domain.Cents, high domain.BasisPoints) domain.MonthInput {
	in := sheetSeptember(t)
	in.Terms["maria"][month(t, "2026-11")] = domain.Terms{SelfPct: 6500, DividendWeight: 1, HighSelfFromCents: threshold, HighSelfPct: high}
	return in
}

func TestAnOverriddenDayCountsTowardTheThresholdButEarnsNoBonus(t *testing.T) {
	// €7 421 of normal days is below the €8 000 threshold; a €1 000 day with
	// «Себе» set by hand to 0 takes the base over it. The bonus is 10% of the
	// normal days only: the overridden day keeps the «Себе» given to it.
	in := tiered(t, 800000, 7500)
	ri := domain.ReportInput{
		Revenue: map[string]domain.Cents{"services": 100000}, Payments: map[string]domain.Cents{"card": 100000},
		SelfPct: 6500, SelfOverride: ptr(0),
	}
	in.Reports = append(in.Reports, domain.PersonReport{Person: "maria", Date: date(t, "2026-11-16"), Report: mustReport(t, ri), Revenue: ri.Revenue, Payments: ri.Payments})
	c, err := domain.CloseMonth(in)
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Result.People["maria"]; got.SelfBase != 842100 || got.SelfBonus != 74210 {
		t.Fatalf("maria = %+v; want base 842100 and bonus 74210", got)
	}
}

func TestABonusCanTurnTheMonthIntoALoss(t *testing.T) {
	// 100% above €6 000: 35% more of €7 421 is €2 597,35, above the €1 788,85 profit.
	c, err := domain.CloseMonth(tiered(t, 600000, 10000))
	if err != nil {
		t.Fatal(err)
	}
	if c.Result.SelfBonuses != 259735 || c.Result.Profit != 178885-259735 || c.Result.DividendPool != 0 {
		t.Fatalf("bonuses %d profit %d pool %d", c.Result.SelfBonuses, c.Result.Profit, c.Result.DividendPool)
	}
	var bonus domain.Cents
	for _, a := range c.Accruals {
		switch a.Type {
		case domain.Dividend:
			t.Errorf("dividend in a loss month: %+v", a)
		case domain.SelfBonus:
			bonus += a.Amount
		}
	}
	if bonus != 259735 {
		t.Errorf("bonus accrued %d, want it in full", bonus)
	}
}

func TestAnOpenMonthPreviewIncludesTheSelfBonus(t *testing.T) {
	res, err := domain.PreviewMonth(tiered(t, 600000, 7500))
	if err != nil {
		t.Fatal(err)
	}
	if res.SelfBonuses != 74210 || res.People["maria"].SelfBonus != 74210 || res.Profit != 178885-74210 {
		t.Fatalf("preview bonuses %d maria %d profit %d", res.SelfBonuses, res.People["maria"].SelfBonus, res.Profit)
	}
}

func TestReportsSavedAtAnotherRateThanTheTermsAreRefused(t *testing.T) {
	// The bonus assumes the month's reports used the terms' «Себе» rate.
	in := sheetSeptember(t)
	in.Terms["maria"][month(t, "2026-11")] = domain.Terms{SelfPct: 5000, DividendWeight: 1, HighSelfFromCents: 500000, HighSelfPct: 8000}
	_, err := domain.CloseMonth(in)
	var ve *domain.ValidationError
	if !errors.As(err, &ve) || ve.Problem != domain.ProblemSelfRateMismatch {
		t.Fatalf("err = %v, want %s", err, domain.ProblemSelfRateMismatch)
	}
}

func TestPersonResultSalonShare(t *testing.T) {
	p := domain.PersonResult{Revenue: 100000, Self: 60000}
	if got := p.SalonShare(); got != 40000 {
		t.Fatalf("p.SalonShare() = %d, want 40000", got)
	}
}
