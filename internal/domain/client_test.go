package domain_test

import (
	"github.com/lieranderl/aestheticlab-income-app/internal/domain"
	"math/rand"
	"testing"
)

func TestClientExtrasGoEntirelyToKassaAndOnlyServicesEarnSelf(t *testing.T) {
	r, err := domain.NewClientReport("olga", domain.Date{Year: 2026, Mon: 11, Day: 20}, domain.ClientInput{Services: 10000, Extras: map[string]domain.Cents{"creams": 2000}, ExtrasDestination: domain.ToCash, ServiceCash: 3000}, 7000)
	if err != nil {
		t.Fatal(err)
	}
	if r.Report.Total != 12000 || r.Report.SelfAmount != 7000 || r.Report.SalonShare != 5000 || r.Report.CashToKassa != 5000 || r.Report.BalanceDelta != 0 {
		t.Fatalf("client: %+v", r.Report)
	}
}

func TestCertificateRedemptionDoesNotDepositMoneyAgain(t *testing.T) {
	r, err := domain.NewClientReport("olga", domain.Date{Year: 2026, Mon: 11, Day: 20}, domain.ClientInput{Services: 10000, CertificatePaid: 10000}, 7000)
	if err != nil {
		t.Fatal(err)
	}
	if r.Report.Prepaid != 10000 || r.Report.CardIn != 0 || r.Report.CashToKassa != 0 || r.Report.BalanceDelta != 7000 || r.Report.SalonIncome != 3000 {
		t.Fatalf("redeemed %+v", r.Report)
	}
}

func TestClientContributionsAndExtrasAreValidated(t *testing.T) {
	cases := []domain.ClientInput{{Services: 10000, ServiceCash: 6000, ServiceCard: 5000}, {Services: 10000, CertificatePaid: 10001}, {Services: -1}, {Services: 10000, Extras: map[string]domain.Cents{"oil": 1000}}, {Services: 10000, Extras: map[string]domain.Cents{"other": 1000}, ExtrasDestination: domain.ToCash}, {Services: 10000, Extras: map[string]domain.Cents{"oil": -1}, ExtrasDestination: domain.ToCash}}
	for i, in := range cases {
		if _, err := domain.NewClientReport("olga", domain.Date{Year: 2026, Mon: 11, Day: 20}, in, 7000); err == nil {
			t.Fatalf("case%d accepted", i)
		}
	}
}

func TestClientCardExtrasCertificateSaleAndPartialRedemption(t *testing.T) {
	cases := []struct {
		name                                        string
		in                                          domain.ClientInput
		total, self, income, cash, card, pre, delta domain.Cents
	}{
		{"oil on card", domain.ClientInput{Services: 10000, Extras: map[string]domain.Cents{"oil": 1000}, ExtrasDestination: domain.ToKassaCard, ServiceCash: 3000}, 11000, 7000, 4000, 3000, 1000, 0, 0},
		{"certificate sale", domain.ClientInput{Extras: map[string]domain.Cents{"certificates": 10000}, ExtrasDestination: domain.ToKassaCard}, 10000, 0, 0, 0, 10000, 0, 0},
		{"partial redemption", domain.ClientInput{Services: 10000, CertificatePaid: 4000, Extras: map[string]domain.Cents{"creams": 2000}, ExtrasDestination: domain.ToKassaCard, ServiceCash: 2000, ServiceCard: 1000}, 12000, 7000, 5000, 2000, 3000, 4000, 4000},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			pr, err := domain.NewClientReport("olga", domain.Date{Year: 2026, Mon: 11, Day: 20}, tt.in, 7000)
			if err != nil {
				t.Fatal(err)
			}
			r := pr.Report
			if r.Total != tt.total || r.SelfAmount != tt.self || r.SalonIncome != tt.income || r.CashToKassa != tt.cash || r.CardIn != tt.card || r.Prepaid != tt.pre || r.BalanceDelta != tt.delta {
				t.Fatalf("%+v", r)
			}
		})
	}
}

func TestClientFundsHeldAndBalanceAlwaysEqualSelf(t *testing.T) {
	rng := rand.New(rand.NewSource(14)) // #nosec G404 -- Deterministic money test inputs, never used for security.
	for range 20000 {
		services := domain.Cents(rng.Intn(100000) + 1)
		pre := domain.Cents(rng.Int63n(int64(services) + 1))
		available := services - pre
		cash := domain.Cents(rng.Int63n(int64(available) + 1))
		card := domain.Cents(rng.Int63n(int64(available-cash) + 1))
		dest := domain.ToCash
		if rng.Intn(2) == 0 {
			dest = domain.ToKassaCard
		}
		in := domain.ClientInput{Services: services, CertificatePaid: pre, ServiceCash: cash, ServiceCard: card, Extras: map[string]domain.Cents{"creams": domain.Cents(rng.Intn(10000)), "oil": domain.Cents(rng.Intn(10000)), "certificates": domain.Cents(rng.Intn(10000))}, ExtrasDestination: dest}
		pr, err := domain.NewClientReport("olga", domain.Date{Year: 2026, Mon: 11, Day: 20}, in, domain.BasisPoints(rng.Intn(10001)))
		if err != nil {
			t.Fatal(err)
		}
		held := services - pre - cash - card
		if pr.Report.CashKept() != held {
			t.Fatalf("cash kept %d, want %d: %+v", pr.Report.CashKept(), held, in)
		}
		if held+pr.Report.BalanceDelta != pr.Report.SelfAmount {
			t.Fatalf("invariant %+v -> %+v", in, pr.Report)
		}
	}
}
