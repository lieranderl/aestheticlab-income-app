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
