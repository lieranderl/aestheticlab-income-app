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
