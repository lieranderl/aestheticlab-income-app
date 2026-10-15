package domain_test

import (
	"testing"

	"github.com/lieranderl/aestheticlab-income-app/internal/domain"
)

var (
	cash = domain.Account(domain.KassaCash)
	card = domain.Account(domain.KassaCard)
	anna = domain.Person("anna")
	olga = domain.Person("olga")
)

func confirmed(typ domain.MovementType, amount domain.Cents, from, to domain.Party) domain.Movement {
	return domain.Movement{Type: typ, Status: domain.Confirmed, Amount: amount, From: from, To: to}
}

func fold(t *testing.T, start domain.Balances, reports []domain.PersonReport, moves ...domain.Movement) domain.Balances {
	t.Helper()
	b, err := domain.Fold(start, reports, moves)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestEachMovementMovesMoneyWhereTheLedgerSays(t *testing.T) {
	expense := confirmed(domain.Expense, 3000, olga, domain.Outside)
	expense.ExpenseCategory = "materials"
	kassaExpense := confirmed(domain.Expense, 1500, card, domain.Outside)
	kassaExpense.ExpenseCategory = "internet"

	b := fold(t, domain.Balances{}, nil,
		confirmed(domain.Opening, 50000, domain.Salon, cash),
		confirmed(domain.Opening, 120000, domain.Salon, card),
		confirmed(domain.Opening, -2500, domain.Salon, anna), // she owed the salon after October
		confirmed(domain.Deposit, 10000, anna, cash),         // she puts €100 into the till
		confirmed(domain.Payout, 20000, card, olga),          // the salon pays Olga €200
		expense,      // Olga paid €30 for materials
		kassaExpense, // the card paid €15 internet
		confirmed(domain.Transfer, 30000, cash, card),           // Anna banks €300 of cash
		confirmed(domain.Adjustment, 700, cash, domain.Outside), // the recount was €7 short
		confirmed(domain.Salary, 100000, domain.Salon, anna),
		confirmed(domain.Dividend, 39343, domain.Salon, olga),
	)
	want := map[string]domain.Cents{
		"cash": 50000 + 10000 - 30000 - 700,
		"card": 120000 - 20000 - 1500 + 30000,
		"anna": -2500 + 10000 + 100000,
		"olga": -20000 + 3000 + 39343,
	}
	got := map[string]domain.Cents{
		"cash": b.Accounts[domain.KassaCash], "card": b.Accounts[domain.KassaCard],
		"anna": b.Persons["anna"], "olga": b.Persons["olga"],
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %d, want %d", k, got[k], v)
		}
	}
}

func TestPendingAndVoidedMovementsDoNotCount(t *testing.T) {
	pending := confirmed(domain.Deposit, 10000, anna, cash)
	pending.Status = domain.Pending
	voided := confirmed(domain.Payout, 5000, card, anna)
	voided.Status = domain.Voided
	b := fold(t, domain.Balances{}, nil, pending, voided)
	if b.Accounts[domain.KassaCash] != 0 || b.Accounts[domain.KassaCard] != 0 || b.Persons["anna"] != 0 {
		t.Fatalf("balances changed: %+v", b)
	}
}

func TestAReportFillsTheKassaAndChangesHerBalance(t *testing.T) {
	spec := mustReport(t, domain.ReportInput{
		Revenue:  map[string]domain.Cents{"services": 60000, "creams": 5000, "certificates": 10000},
		Payments: map[string]domain.Cents{"card": 60000, "cash": 15000},
		SelfPct:  5000,
	})
	b := fold(t, domain.Balances{}, []domain.PersonReport{{Person: "anna", Report: spec}})
	if b.Accounts[domain.KassaCard] != 60000 || b.Accounts[domain.KassaCash] != 0 || b.Persons["anna"] != 17500 {
		t.Fatalf("card %d cash %d anna %d; want 60000 0 17500",
			b.Accounts[domain.KassaCard], b.Accounts[domain.KassaCash], b.Persons["anna"])
	}
	if b.CertificatesOutstanding != 10000 {
		t.Fatalf("outstanding certificates = %d, want 10000", b.CertificatesOutstanding)
	}
}

func TestATransferToAnnasCardFillsTheCardNotTheTill(t *testing.T) {
	r := entry(t, "olga", "2026-11-08", domain.ReportInput{
		Revenue: map[string]domain.Cents{"services": 20000}, Payments: map[string]domain.Cents{"cash": 20000},
		SelfPct: 7000, TransferToCard: ptr(2000),
	})
	b := fold(t, domain.Balances{}, []domain.PersonReport{r})
	if b.Accounts[domain.KassaCard] != 2000 || b.Accounts[domain.KassaCash] != 4000 || b.Persons["olga"] != 0 {
		t.Fatalf("balances %+v, want €20 on the card, €40 in the till, Olga settled", b)
	}
}

func TestOutstandingCertificatesStartFromTheOpeningAmount(t *testing.T) {
	sale := mustReport(t, domain.ReportInput{
		Revenue: map[string]domain.Cents{"certificates": 10000}, Payments: map[string]domain.Cents{"card": 10000}, SelfPct: 5000,
	})
	redemption := mustReport(t, domain.ReportInput{
		Revenue: map[string]domain.Cents{"services": 15000}, Payments: map[string]domain.Cents{"certificate": 15000}, SelfPct: 7000,
	})
	b := fold(t, domain.Balances{},
		[]domain.PersonReport{{Person: "anna", Report: sale}, {Person: "olga", Report: redemption}},
		confirmed(domain.Opening, 50000, domain.Salon, domain.Certificates),
	)
	if b.CertificatesOutstanding != 45000 || b.CertificatesNeedChecking() {
		t.Fatalf("outstanding = %d (check %v), want 45000 and no warning", b.CertificatesOutstanding, b.CertificatesNeedChecking())
	}
}

func TestARedemptionTheOpeningMissedRaisesTheCertificateWarning(t *testing.T) {
	redemption := mustReport(t, domain.ReportInput{
		Revenue: map[string]domain.Cents{"services": 5000}, Payments: map[string]domain.Cents{"certificate": 5000}, SelfPct: 7000,
	})
	b := fold(t, domain.Balances{}, []domain.PersonReport{{Person: "olga", Report: redemption}})
	if b.CertificatesOutstanding != -5000 || !b.CertificatesNeedChecking() {
		t.Fatalf("outstanding = %d, warning %v; want -5000 and a warning", b.CertificatesOutstanding, b.CertificatesNeedChecking())
	}
}

func TestATransferBetweenTillAndCardKeepsTheKassaTotal(t *testing.T) {
	start := domain.Balances{Accounts: map[string]domain.Cents{domain.KassaCash: 40000, domain.KassaCard: 10000}}
	b := fold(t, start, nil, confirmed(domain.Transfer, 25000, cash, card))
	if b.KassaTotal() != 50000 || b.Accounts[domain.KassaCash] != 15000 {
		t.Fatalf("total %d cash %d; want 50000 15000", b.KassaTotal(), b.Accounts[domain.KassaCash])
	}
}

func TestFoldingNeverChangesTheStartingBalances(t *testing.T) {
	start := domain.Balances{Persons: map[string]domain.Cents{"anna": 100}}
	_ = fold(t, start, nil, confirmed(domain.Salary, 100000, domain.Salon, anna))
	if start.Persons["anna"] != 100 {
		t.Fatalf("start balances were mutated: %d", start.Persons["anna"])
	}
}

func TestMalformedMovementsAreRejected(t *testing.T) {
	noCategory := confirmed(domain.Expense, 100, card, domain.Outside)
	cases := map[string]domain.Movement{
		"zero amount":                     confirmed(domain.Deposit, 0, anna, cash),
		"negative deposit":                confirmed(domain.Deposit, -5, anna, cash),
		"deposit to outside":              confirmed(domain.Deposit, 100, anna, domain.Outside),
		"payout from a person":            confirmed(domain.Payout, 100, olga, anna),
		"expense without category":        noCategory,
		"transfer to the same account":    confirmed(domain.Transfer, 100, cash, cash),
		"adjustment of a person":          confirmed(domain.Adjustment, 100, anna, domain.Outside),
		"salary not from the salon":       confirmed(domain.Salary, 100, card, anna),
		"«Себе» bonus not from the salon": confirmed(domain.SelfBonus, 100, card, anna),
		"negative opening of the kassa":   confirmed(domain.Opening, -100, domain.Salon, cash),
		"unknown type":                    confirmed("gift", 100, domain.Salon, anna),
		"empty status":                    {Type: domain.Deposit, Amount: 100, From: anna, To: cash},
		"misspelled status":               {Type: domain.Deposit, Status: "confirmd", Amount: 100, From: anna, To: cash},
	}
	for name, m := range cases {
		if err := domain.ValidateMovement(m); err == nil {
			t.Errorf("%s: accepted", name)
		}
		if _, err := domain.Fold(domain.Balances{}, nil, []domain.Movement{m}); err == nil {
			t.Errorf("%s: folded", name)
		}
	}
}
