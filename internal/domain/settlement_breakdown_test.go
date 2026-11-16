package domain_test

import (
	"github.com/lieranderl/aestheticlab-income-app/internal/domain"
	"testing"
)

func TestBreakdownCountsOpeningAndAccrualsExactlyOnce(t *testing.T) {
	b := domain.BreakdownFor("olga", 1200, []domain.PersonReport{{Person: "olga", Report: domain.Report{BalanceDelta: -2000}}}, []domain.Movement{
		{Type: domain.Opening, Status: domain.Confirmed, Amount: 100, From: domain.Salon, To: domain.Person("olga")},
		{Type: domain.Deposit, Status: domain.Confirmed, Amount: 3000, From: domain.Person("olga"), To: domain.Account(domain.KassaCash)},
		{Type: domain.Expense, Status: domain.Confirmed, Amount: 400, From: domain.Person("olga"), To: domain.Outside},
		{Type: domain.Payout, Status: domain.Confirmed, Amount: 800, From: domain.Account(domain.KassaCash), To: domain.Person("olga")},
		{Type: domain.Dividend, Status: domain.Confirmed, Amount: 500, From: domain.Salon, To: domain.Person("olga")},
		{Type: domain.Salary, Status: domain.Voided, Amount: 90000, From: domain.Salon, To: domain.Person("olga")},
	}, domain.PersonResult{Salary: 1000, SelfBonus: 200, Dividend: 500})
	if b.Opening != 1300 || b.Payouts != -800 || b.Closing != 3600 {
		t.Fatalf("breakdown %+v", b)
	}
}

// 2026-11-16 production feedback: a netted «Отчёты» row hid «Себе» and the
// cash the person kept, so «Всего заработано» could not be traced to the
// balance. The breakdown keeps both; «Себе» − cash kept == the report deltas.
func TestBreakdownSplitsSelfFromCashKept(t *testing.T) {
	kept := domain.PersonReport{Person: "julia", Report: domain.Report{SelfAmount: 5250, Cash: 10500, BalanceDelta: -5250}}
	card := domain.PersonReport{Person: "julia", Report: domain.Report{SelfAmount: 16350, CardIn: 42700, BalanceDelta: 16350}}
	other := domain.PersonReport{Person: "olga", Report: domain.Report{SelfAmount: 999, Cash: 999}}
	b := domain.BreakdownFor("julia", 0, []domain.PersonReport{kept, card, other}, nil, domain.PersonResult{})
	if b.Self != 21600 || b.CashKept != -10500 || b.Reports != 11100 || b.Self+b.CashKept != b.Reports || b.Closing != 11100 {
		t.Fatalf("breakdown %+v", b)
	}
}
