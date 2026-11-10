package domain_test

import (
	"testing"

	"github.com/lieranderl/aestheticlab-income-app/internal/domain"
)

// Reference month profit is €1 788,85 (dividends 596,28 / 596,28 / 596,29).
const referenceProfit domain.Cents = 178885

func TestARecountDifferenceCountsInTheMonthsProfit(t *testing.T) {
	in := sheetSeptember(t)
	day := date(t, "2026-11-28")
	in.Movements = append(in.Movements,
		dated(confirmed(domain.Adjustment, 3000, cash, domain.Outside), day), // €30 missing from the till
		dated(confirmed(domain.Adjustment, 1000, domain.Outside, card), day), // €10 more on the card
	)
	c, err := domain.CloseMonth(in)
	if err != nil {
		t.Fatal(err)
	}
	r := c.Result
	if r.KassaDifference != -2000 || r.Profit != referenceProfit-2000 || r.DividendPool != referenceProfit-2000 {
		t.Fatalf("difference %d profit %d pool %d; want −2000, %d, %d", r.KassaDifference, r.Profit, r.DividendPool, referenceProfit-2000, referenceProfit-2000)
	}
	if r.Expenses != 286900 {
		t.Fatalf("expenses %d: a recount is not an expense", r.Expenses)
	}
	var paid domain.Cents
	for _, a := range c.Accruals {
		if a.Type == domain.Dividend {
			paid += a.Amount
		}
	}
	if paid != r.DividendPool {
		t.Fatalf("dividends %d, want the whole pool %d", paid, r.DividendPool)
	}
}

func TestAnUnrecoveredLossIsTakenOffLaterProfitBeforeDividends(t *testing.T) {
	for _, tc := range []struct {
		name                string
		carriedIn, extra    domain.Cents // extra: an added expense making this month's profit smaller
		profit, pool, carry domain.Cents
	}{
		{"no loss to recover", 0, 0, referenceProfit, referenceProfit, 0},
		{"profit covers the loss", 50000, 0, referenceProfit, referenceProfit - 50000, 0},
		{"profit equals the loss", referenceProfit, 0, referenceProfit, 0, 0},
		{"profit only covers part", 200000, 0, referenceProfit, 0, 200000 - referenceProfit},
		{"a new loss adds to it", 10000, referenceProfit + 5000, -5000, 0, 15000},
		{"a new loss with nothing before", 0, referenceProfit + 5000, -5000, 0, 5000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := sheetSeptember(t)
			in.CarriedLoss = tc.carriedIn
			if tc.extra > 0 {
				e := confirmed(domain.Expense, tc.extra, card, domain.Outside)
				e.ExpenseCategory = "equipment"
				in.Movements = append(in.Movements, dated(e, date(t, "2026-11-20")))
			}
			c, err := domain.CloseMonth(in)
			if err != nil {
				t.Fatal(err)
			}
			r := c.Result
			if r.Profit != tc.profit || r.LossCarriedIn != tc.carriedIn || r.DividendPool != tc.pool || r.LossCarriedOut != tc.carry {
				t.Fatalf("profit %d in %d pool %d out %d; want %d %d %d %d", r.Profit, r.LossCarriedIn, r.DividendPool, r.LossCarriedOut, tc.profit, tc.carriedIn, tc.pool, tc.carry)
			}
			for _, a := range c.Accruals {
				if a.Type == domain.Dividend && tc.pool == 0 {
					t.Fatalf("dividend accrued while a loss is unrecovered: %+v", a)
				}
			}
		})
	}
}

func TestACarriedLossMustNotBeNegative(t *testing.T) {
	in := sheetSeptember(t)
	in.CarriedLoss = -1
	if _, err := domain.CloseMonth(in); problem(t, err) != domain.ProblemNegativeAmount {
		t.Fatalf("a negative carried loss was accepted: %v", err)
	}
}

func TestAMonthBeforeTheNewRulesSettlesLikeTheSheet(t *testing.T) {
	in := sheetSeptember(t)
	in.LegacyProfit = true
	in.CarriedLoss = 50000
	in.Movements = append(in.Movements, dated(confirmed(domain.Adjustment, 3000, cash, domain.Outside), date(t, "2026-11-28")))
	extra := confirmed(domain.Expense, referenceProfit+5000, card, domain.Outside)
	extra.ExpenseCategory = "equipment"
	loss := in
	loss.Movements = append(append([]domain.Movement(nil), in.Movements...), dated(extra, date(t, "2026-11-20")))
	for name, tc := range map[string]struct {
		in                        domain.MonthInput
		profit, pool, in_, out, k domain.Cents
	}{
		"a recount and a carried loss are ignored": {in, referenceProfit, referenceProfit, 0, 0, 0},
		"a loss is not carried out":                {loss, -5000, 0, 0, 0, 0},
	} {
		c, err := domain.CloseMonth(tc.in)
		if err != nil {
			t.Fatal(err)
		}
		r := c.Result
		if r.Profit != tc.profit || r.DividendPool != tc.pool || r.LossCarriedIn != tc.in_ || r.LossCarriedOut != tc.out || r.KassaDifference != tc.k {
			t.Fatalf("%s: profit %d pool %d in %d out %d kassa %d", name, r.Profit, r.DividendPool, r.LossCarriedIn, r.LossCarriedOut, r.KassaDifference)
		}
	}
}

func TestAThirtyEuroShortageAloneLowersTheReferenceProfit(t *testing.T) {
	in := sheetSeptember(t)
	in.Movements = append(in.Movements, dated(confirmed(domain.Adjustment, 3000, cash, domain.Outside), date(t, "2026-11-28")))
	c, err := domain.CloseMonth(in)
	if err != nil {
		t.Fatal(err)
	}
	if c.Result.Profit != 175885 || c.Result.DividendPool != 175885 {
		t.Fatalf("profit %d pool %d, want 1758.85", c.Result.Profit, c.Result.DividendPool)
	}
}
