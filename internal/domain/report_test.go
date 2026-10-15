package domain_test

import (
	"errors"
	"math/rand/v2"
	"reflect"
	"testing"

	"github.com/lieranderl/aestheticlab-income-app/internal/domain"
)

// salon is the default catalog seeded in production (technical-solution §9).
var salon = domain.Catalog{
	Revenue: map[string]domain.RevenueCategory{
		"services":     {CountsTowardSelf: true},
		"creams":       {CountsTowardSelf: true},
		"certificates": {IsCertificateSale: true},
		"products":     {CountsTowardSelf: true},
		"other":        {CountsTowardSelf: true},
	},
	Payments: map[string]domain.PaymentMethod{
		"card":        {Destination: domain.ToKassaCard},
		"cash":        {Destination: domain.ToCash},
		"certificate": {Destination: domain.Prepaid},
	},
}

func ptr(c domain.Cents) *domain.Cents { return &c }

func TestAnnasSpecDayLeavesNoCashAndTheSalonOwesHer175(t *testing.T) {
	r, err := domain.ComputeReport(domain.ReportInput{
		Revenue:  map[string]domain.Cents{"services": 60000, "creams": 5000, "certificates": 10000, "other": 0},
		Payments: map[string]domain.Cents{"card": 60000, "cash": 15000},
		SelfPct:  5000,
	}, salon)
	if err != nil {
		t.Fatal(err)
	}
	want := domain.Report{
		Total: 75000, SelfBase: 65000, SelfPct: 5000, SelfAmount: 32500,
		SalonShare: 42500, CertificateSales: 10000, SalonIncome: 32500,
		CardIn: 60000, Prepaid: 0, Cash: 15000,
		SuggestedToKassa: 0, CashToKassa: 0, KassaCredit: 60000, BalanceDelta: 17500,
	}
	if r != want {
		t.Fatalf("report =\n%+v\nwant\n%+v", r, want)
	}
	if kept := r.Cash - r.CashToKassa; kept+r.BalanceDelta != r.SelfAmount {
		t.Fatalf("cash kept %d + balance %d != «Себе» %d", kept, r.BalanceDelta, r.SelfAmount)
	}
}

func TestACertificateSoldByCardGoesWhollyToTheSalon(t *testing.T) {
	r := mustReport(t, domain.ReportInput{
		Revenue:  map[string]domain.Cents{"certificates": 10000},
		Payments: map[string]domain.Cents{"card": 10000},
		SelfPct:  7000,
	})
	if r.SelfAmount != 0 || r.SalonShare != 10000 || r.CardIn != 10000 || r.BalanceDelta != 0 || r.SalonIncome != 0 {
		t.Fatalf("got «Себе» %d, «Салону» %d, cardIn %d, balance %d, income %d; want 0, 10000, 10000, 0, 0",
			r.SelfAmount, r.SalonShare, r.CardIn, r.BalanceDelta, r.SalonIncome)
	}
}

func TestAServicePaidByCertificateEarnsTheMasterHerShareOnce(t *testing.T) {
	r := mustReport(t, domain.ReportInput{
		Revenue:  map[string]domain.Cents{"services": 10000},
		Payments: map[string]domain.Cents{"certificate": 10000},
		SelfPct:  7000,
	})
	if r.SelfAmount != 7000 || r.SalonShare != 3000 || r.Prepaid != 10000 || r.BalanceDelta != 7000 || r.SalonIncome != 3000 {
		t.Fatalf("got «Себе» %d, «Салону» %d, prepaid %d, balance %d, income %d; want 7000, 3000, 10000, 7000, 3000",
			r.SelfAmount, r.SalonShare, r.Prepaid, r.BalanceDelta, r.SalonIncome)
	}
}

func TestACashOnlyDaySettlesToZeroWhenTheSuggestedCashIsLeft(t *testing.T) {
	r := mustReport(t, domain.ReportInput{
		Revenue:  map[string]domain.Cents{"services": 20000},
		Payments: map[string]domain.Cents{"cash": 20000},
		SelfPct:  7000,
	})
	if r.SalonShare != 6000 || r.SuggestedToKassa != 6000 || r.CashToKassa != 6000 || r.BalanceDelta != 0 {
		t.Fatalf("got «Салону» %d, suggested %d, left %d, balance %d; want 6000, 6000, 6000, 0",
			r.SalonShare, r.SuggestedToKassa, r.CashToKassa, r.BalanceDelta)
	}
}

func TestTheSalonsShareCanGoToAnnasCardInsteadOfTheTill(t *testing.T) {
	day := func(toCard domain.Cents, toKassa *domain.Cents) domain.ReportInput {
		return domain.ReportInput{
			Revenue: map[string]domain.Cents{"services": 20000}, Payments: map[string]domain.Cents{"cash": 20000},
			SelfPct: 7000, TransferToCard: ptr(toCard), CashToKassa: toKassa,
		}
	}
	// The whole €60 by transfer: nothing goes into the till, the day settles.
	r := mustReport(t, day(6000, nil))
	if r.TransferToCard != 6000 || r.CashToKassa != 0 || r.KassaCredit != 6000 || r.BalanceDelta != 0 || r.SuggestedToKassa != 6000 {
		t.Fatalf("all by transfer: %+v", r)
	}
	// Split: €20 to the card, the rest of the suggestion in cash.
	r = mustReport(t, day(2000, nil))
	if r.TransferToCard != 2000 || r.CashToKassa != 4000 || r.BalanceDelta != 0 {
		t.Fatalf("split: %+v", r)
	}
	// Typed amounts are kept: €30 by transfer and €10 in cash leave her owing €20.
	r = mustReport(t, day(3000, ptr(1000)))
	if r.CashToKassa != 1000 || r.BalanceDelta != -2000 {
		t.Fatalf("typed: %+v", r)
	}
	// Together they can't exceed the day's cash.
	if _, err := domain.ComputeReport(day(15000, ptr(10000)), salon); problem(t, err) != domain.ProblemCashToKassaOutOfRange {
		t.Fatalf("more than the cash: %v", err)
	}
	if _, err := domain.ComputeReport(day(-100, nil), salon); problem(t, err) != domain.ProblemCashToKassaOutOfRange {
		t.Fatalf("a negative transfer: %v", err)
	}
}

func TestLeavingLessCashThanSuggestedMeansSheOwesTheSalon(t *testing.T) {
	r := mustReport(t, domain.ReportInput{
		Revenue:     map[string]domain.Cents{"services": 20000},
		Payments:    map[string]domain.Cents{"cash": 20000},
		SelfPct:     7000,
		CashToKassa: ptr(1000),
	})
	if r.CashToKassa != 1000 || r.BalanceDelta != -5000 {
		t.Fatalf("left %d, balance %d; want 1000, -5000", r.CashToKassa, r.BalanceDelta)
	}
}

func TestAnOverriddenSelfAmountIsKeptAndMarked(t *testing.T) {
	r := mustReport(t, domain.ReportInput{
		Revenue:      map[string]domain.Cents{"services": 10000},
		Payments:     map[string]domain.Cents{"card": 10000},
		SelfPct:      5000,
		SelfOverride: ptr(6000),
	})
	if r.SelfAmount != 6000 || !r.SelfOverridden || r.SalonShare != 4000 || r.BalanceDelta != 6000 {
		t.Fatalf("got «Себе» %d overridden %v «Салону» %d balance %d; want 6000 true 4000 6000",
			r.SelfAmount, r.SelfOverridden, r.SalonShare, r.BalanceDelta)
	}
}

func TestAnEmptyDayIsAllZeros(t *testing.T) {
	r := mustReport(t, domain.ReportInput{SelfPct: 5000})
	if r != (domain.Report{SelfPct: 5000}) {
		t.Fatalf("empty day = %+v", r)
	}
}

func TestInvalidReportsAreRejectedWithAReason(t *testing.T) {
	cases := []struct {
		name string
		in   domain.ReportInput
		code domain.Problem
	}{
		{"payments short of revenue", domain.ReportInput{
			Revenue: map[string]domain.Cents{"services": 10000}, Payments: map[string]domain.Cents{"card": 5000}, SelfPct: 5000,
		}, domain.ProblemPaymentsMismatch},
		{"negative revenue", domain.ReportInput{
			Revenue: map[string]domain.Cents{"services": -1}, SelfPct: 5000,
		}, domain.ProblemNegativeAmount},
		{"negative payment", domain.ReportInput{
			Revenue: map[string]domain.Cents{"services": 0}, Payments: map[string]domain.Cents{"card": -1}, SelfPct: 5000,
		}, domain.ProblemNegativeAmount},
		{"unknown category", domain.ReportInput{
			Revenue: map[string]domain.Cents{"massage": 100}, Payments: map[string]domain.Cents{"card": 100}, SelfPct: 5000,
		}, domain.ProblemUnknownCategory},
		{"unknown payment method", domain.ReportInput{
			Revenue: map[string]domain.Cents{"services": 100}, Payments: map[string]domain.Cents{"crypto": 100}, SelfPct: 5000,
		}, domain.ProblemUnknownPaymentMethod},
		{"more cash left than taken", domain.ReportInput{
			Revenue: map[string]domain.Cents{"services": 100}, Payments: map[string]domain.Cents{"cash": 100}, SelfPct: 5000, CashToKassa: ptr(101),
		}, domain.ProblemCashToKassaOutOfRange},
		{"negative cash left", domain.ReportInput{
			Revenue: map[string]domain.Cents{"services": 100}, Payments: map[string]domain.Cents{"cash": 100}, SelfPct: 5000, CashToKassa: ptr(-1),
		}, domain.ProblemCashToKassaOutOfRange},
		{"«Себе» above total", domain.ReportInput{
			Revenue: map[string]domain.Cents{"services": 100}, Payments: map[string]domain.Cents{"card": 100}, SelfPct: 5000, SelfOverride: ptr(101),
		}, domain.ProblemSelfOutOfRange},
		{"percentage above 100%", domain.ReportInput{
			Revenue: map[string]domain.Cents{"services": 100}, Payments: map[string]domain.Cents{"card": 100}, SelfPct: 10001,
		}, domain.ProblemPercentOutOfRange},
	}
	for _, c := range cases {
		_, err := domain.ComputeReport(c.in, salon)
		var ve *domain.ValidationError
		if !errors.As(err, &ve) || ve.Problem != c.code {
			t.Errorf("%s: error = %v, want problem %q", c.name, err, c.code)
		}
	}
}

func TestPaymentMismatchReportsTheDifference(t *testing.T) {
	_, err := domain.ComputeReport(domain.ReportInput{
		Revenue: map[string]domain.Cents{"services": 10000}, Payments: map[string]domain.Cents{"card": 5000}, SelfPct: 5000,
	}, salon)
	var ve *domain.ValidationError
	if !errors.As(err, &ve) || ve.Difference != 5000 {
		t.Fatalf("error = %v, want a difference of 5000", err)
	}
}

// The core promise of «Оставьте в кассе»: whatever she leaves in the till,
// the cash she keeps plus what the salon owes her is exactly her «Себе».
func TestCashKeptPlusBalanceAlwaysEqualsSelf(t *testing.T) {
	rng := rand.New(rand.NewPCG(2026, 11)) //nolint:gosec // seeded on purpose: the property test must be reproducible, not secret
	for i := range 20000 {
		in := randomDay(rng)
		r, err := domain.ComputeReport(in, salon)
		if err != nil {
			t.Fatalf("case %d %+v: %v", i, in, err)
		}
		if kept := r.Cash - r.CashToKassa - r.TransferToCard; kept+r.BalanceDelta != r.SelfAmount {
			t.Fatalf("case %d %+v: kept %d + balance %d != «Себе» %d", i, in, kept, r.BalanceDelta, r.SelfAmount)
		}
		if r.SelfAmount+r.SalonShare != r.Total || r.SuggestedToKassa < 0 || r.SuggestedToKassa > r.Cash {
			t.Fatalf("case %d: split or suggestion out of range: %+v", i, r)
		}
		if in.CashToKassa == nil && r.BalanceDelta < 0 {
			t.Fatalf("case %d: leaving the suggested cash must never leave her owing the salon: %+v", i, r)
		}
	}
}

func randomDay(rng *rand.Rand) domain.ReportInput {
	amount := func() domain.Cents { return domain.Cents(rng.Int64N(50000)) }
	rev := map[string]domain.Cents{"services": amount(), "creams": amount(), "certificates": amount()}
	total := rev["services"] + rev["creams"] + rev["certificates"]
	card := domain.Cents(rng.Int64N(int64(total) + 1))
	prepaid := domain.Cents(rng.Int64N(int64(total-card) + 1))
	cash := total - card - prepaid
	in := domain.ReportInput{
		Revenue:  rev,
		Payments: map[string]domain.Cents{"card": card, "certificate": prepaid, "cash": cash},
		SelfPct:  domain.BasisPoints(rng.Int64N(10001)),
	}
	toCard := domain.Cents(0)
	if rng.IntN(3) == 0 {
		toCard = domain.Cents(rng.Int64N(int64(cash) + 1))
		in.TransferToCard = ptr(toCard)
	}
	if rng.IntN(2) == 0 {
		in.CashToKassa = ptr(domain.Cents(rng.Int64N(int64(cash-toCard) + 1)))
	}
	if rng.IntN(4) == 0 {
		in.SelfOverride = ptr(domain.Cents(rng.Int64N(int64(total) + 1)))
	}
	return in
}

func mustReport(t *testing.T, in domain.ReportInput) domain.Report {
	t.Helper()
	r, err := domain.ComputeReport(in, salon)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestAPaymentMethodWithAnUnknownDestinationIsRejected(t *testing.T) {
	cat := domain.Catalog{
		Revenue:  salon.Revenue,
		Payments: map[string]domain.PaymentMethod{"payconiq": {}, "cash": {Destination: domain.ToCash}},
	}
	_, err := domain.ComputeReport(domain.ReportInput{
		Revenue: map[string]domain.Cents{"services": 100}, Payments: map[string]domain.Cents{"payconiq": 100}, SelfPct: 5000,
	}, cat)
	if problem(t, err) != domain.ProblemUnknownDestination {
		t.Fatalf("err = %v, want %q", err, domain.ProblemUnknownDestination)
	}
}

func TestASavedReportKeepsTheCategoryFlagsItWasComputedWith(t *testing.T) {
	in := domain.ReportInput{
		Revenue:  map[string]domain.Cents{"services": 60000, "certificates": 10000},
		Payments: map[string]domain.Cents{"card": 70000},
		SelfPct:  5000,
	}
	pr, err := domain.NewPersonReport("anna", date(t, "2026-11-08"), in, salon)
	if err != nil {
		t.Fatal(err)
	}
	want := domain.Catalog{
		Revenue:  map[string]domain.RevenueCategory{"services": {CountsTowardSelf: true}, "certificates": {IsCertificateSale: true}},
		Payments: map[string]domain.PaymentMethod{"card": {Destination: domain.ToKassaCard}},
	}
	if !reflect.DeepEqual(pr.Flags, want) {
		t.Fatalf("flags = %+v, want only the ones used: %+v", pr.Flags, want)
	}
	// Changing the live catalog afterwards must not touch the saved flags.
	salon.Revenue["services"] = domain.RevenueCategory{}
	defer func() { salon.Revenue["services"] = domain.RevenueCategory{CountsTowardSelf: true} }()
	if !pr.Flags.Revenue["services"].CountsTowardSelf || pr.Report.SelfAmount != 30000 {
		t.Fatalf("saved report changed with the catalog: %+v", pr)
	}
}
