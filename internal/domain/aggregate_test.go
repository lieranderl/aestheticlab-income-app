package domain_test

import (
	"reflect"
	"testing"

	"github.com/lieranderl/aestheticlab-income-app/internal/domain"
)

func entry(t *testing.T, person, day string, in domain.ReportInput) domain.PersonReport {
	t.Helper()
	pr, err := domain.NewPersonReport(person, date(t, day), in, salon)
	if err != nil {
		t.Fatal(err)
	}
	return pr
}

func TestADayIsSummedByCategoryPaymentAndPerson(t *testing.T) {
	reports := []domain.PersonReport{
		entry(t, "anna", "2026-11-08", domain.ReportInput{
			Revenue:  map[string]domain.Cents{"services": 60000, "creams": 5000, "certificates": 10000},
			Payments: map[string]domain.Cents{"card": 60000, "cash": 15000}, SelfPct: 5000,
		}),
		entry(t, "olga", "2026-11-08", domain.ReportInput{
			Revenue:  map[string]domain.Cents{"services": 20000},
			Payments: map[string]domain.Cents{"cash": 20000}, SelfPct: 7000,
		}),
	}
	s := domain.Summarize(reports)
	if s.Total != 95000 || s.Self != 32500+14000 || s.SalonShare != 42500+6000 || s.SalonIncome != 32500+6000 {
		t.Errorf("totals: %+v", s)
	}
	if s.MoneyReceived != 60000+15000+20000 || s.CashToKassa != 6000 || s.Prepaid != 0 {
		t.Errorf("money: received %d left %d prepaid %d", s.MoneyReceived, s.CashToKassa, s.Prepaid)
	}
	wantCat := map[string]domain.Cents{"services": 80000, "creams": 5000, "certificates": 10000}
	if !reflect.DeepEqual(s.ByCategory, wantCat) {
		t.Errorf("by category = %v, want %v", s.ByCategory, wantCat)
	}
	wantPay := map[string]domain.Cents{"card": 60000, "cash": 35000}
	if !reflect.DeepEqual(s.ByPayment, wantPay) {
		t.Errorf("by payment = %v, want %v", s.ByPayment, wantPay)
	}
	if s.WorkingDays != 1 || s.People["olga"].Total != 20000 || s.People["olga"].WorkingDays != 1 {
		t.Errorf("days %d, olga %+v", s.WorkingDays, s.People["olga"])
	}
}

func TestWorkingDaysCountDistinctDays(t *testing.T) {
	in := domain.ReportInput{Revenue: map[string]domain.Cents{"services": 100}, Payments: map[string]domain.Cents{"card": 100}, SelfPct: 5000}
	s := domain.Summarize([]domain.PersonReport{
		entry(t, "anna", "2026-11-02", in),
		entry(t, "olga", "2026-11-02", in),
		entry(t, "olga", "2026-11-03", in),
	})
	if s.WorkingDays != 2 || s.People["olga"].WorkingDays != 2 || s.People["anna"].WorkingDays != 1 {
		t.Fatalf("salon %d, olga %d, anna %d; want 2, 2, 1", s.WorkingDays, s.People["olga"].WorkingDays, s.People["anna"].WorkingDays)
	}
}

func TestTheDayScreenListsWhoHasNotReported(t *testing.T) {
	in := domain.ReportInput{SelfPct: 5000}
	got := domain.MissingReporters([]string{"olga", "anna", "maria", "vera"}, []domain.PersonReport{entry(t, "anna", "2026-11-08", in)})
	if want := []string{"maria", "olga", "vera"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("missing = %v, want %v", got, want)
	}
}

func TestAnOpenMonthPreviewIgnoresPendingRowsAndAccruesNothing(t *testing.T) {
	in := sheetSeptember(t)
	p := dated(confirmed(domain.Deposit, 1000, olga, cash), date(t, "2026-11-20"))
	p.Status = domain.Pending
	in.Movements = append(in.Movements, p)
	preview, err := domain.PreviewMonth(in)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Profit != 178885 || preview.People["olga"].Dividend != 59628 {
		t.Fatalf("preview profit %d, olga dividend %d; want 178885, 59628", preview.Profit, preview.People["olga"].Dividend)
	}
	if preview.RevenueByCategory["services"] != 742100+568000+81000+143000 {
		t.Fatalf("revenue by category = %v", preview.RevenueByCategory)
	}
}

func TestTrendsMixSettledOpenAndImportedMonths(t *testing.T) {
	window := domain.Window(month(t, "2026-11"), 3)
	if got := []string{window[0].String(), window[1].String(), window[2].String()}; !reflect.DeepEqual(got, []string{"2026-09", "2026-10", "2026-11"}) {
		t.Fatalf("window = %v", got)
	}
	closing, err := domain.CloseMonth(sheetSeptember(t))
	if err != nil {
		t.Fatal(err)
	}
	imported := map[domain.Month]domain.ImportedMonth{
		month(t, "2026-09"): {RevenueTotal: 1534100, SalonIncome: 585785, Expenses: 286900, ManagerSalary: 120000, Profit: 178885, Dividends: 178885,
			RevenueByPerson: map[string]domain.Cents{"maria": 742100, "olga": 568000, "anna": 143000, "vera": 81000}},
	}
	points := domain.Trend(window, map[domain.Month]domain.MonthResult{month(t, "2026-11"): closing.Result}, imported)
	sep, oct, nov := points[0], points[1], points[2]
	if !sep.Imported || sep.Revenue != 1534100 || sep.Profit != 178885 || sep.RevenueByPerson["olga"] != 568000 {
		t.Errorf("September (imported) = %+v", sep)
	}
	if oct.Imported || oct.Revenue != 0 || oct.HasData {
		t.Errorf("October (no data) = %+v", oct)
	}
	if nov.Imported || !nov.HasData || nov.Revenue != 1534100 || nov.Dividends != 178885 || nov.Salaries != 120000 || nov.RevenueByPerson["maria"] != 742100 {
		t.Errorf("November (app) = %+v", nov)
	}
	if nov.Self != 482365+369200+32400+64350 {
		t.Errorf("November «Себе» = %d", nov.Self)
	}
}

func TestAMonthShowsHowFarItIsFromTheHigherSelfRate(t *testing.T) {
	terms := domain.Terms{SelfPct: 5000, HighSelfFromCents: 500000, HighSelfPct: 7000}
	for _, c := range []struct {
		base      domain.Cents
		reached   bool
		remaining domain.Cents
	}{
		{400000, false, 100001}, // €1 000,01 more makes it strictly above €5 000
		{500000, false, 1},
		{500001, true, 0},
		{600000, true, 0},
	} {
		p, ok := domain.SelfProgressFor(c.base, terms)
		if !ok || p.Reached != c.reached || p.Remaining != c.remaining || p.HighPct != 7000 || p.Threshold != 500000 {
			t.Errorf("base %d: %+v, %v; want reached %v, remaining %d", c.base, p, ok, c.reached, c.remaining)
		}
	}
	if _, ok := domain.SelfProgressFor(400000, domain.Terms{SelfPct: 7000}); ok {
		t.Error("terms without a threshold show progress")
	}
}

func TestASummaryAddsUpTheSelfBase(t *testing.T) {
	s := domain.Summarize([]domain.PersonReport{entry(t, "anna", "2026-11-08", domain.ReportInput{
		Revenue:  map[string]domain.Cents{"services": 60000, "creams": 5000, "certificates": 10000},
		Payments: map[string]domain.Cents{"card": 60000, "cash": 15000}, SelfPct: 5000,
	})})
	if s.SelfBase != 65000 || s.People["anna"].SelfBase != 65000 {
		t.Errorf("«Себе» base = %d, anna's %d; want 650 (the certificate sale is not part of it)", s.SelfBase, s.People["anna"].SelfBase)
	}
}

// The screenshots of 2026-10-21: «Себе» is not the cash in hand, it is the
// cash in hand plus what the salon owes (or minus what is owed to it).
func TestSelfIsTheCashKeptPlusWhatTheSalonOwes(t *testing.T) {
	cardDay, err := domain.NewClientReport("olga", date(t, "2026-10-02"), domain.ClientInput{
		Services: 42700, ServiceCard: 32700, Extras: map[string]domain.Cents{"creams": 10000}, ExtrasDestination: domain.ToKassaCard,
	}, 5000)
	if err != nil {
		t.Fatal(err)
	}
	cashDay, err := domain.NewClientReport("olga", date(t, "2026-10-20"), domain.ClientInput{Services: 36500}, 5000)
	if err != nil {
		t.Fatal(err)
	}
	if got := cardDay.Report.CashKept(); got != 10000 {
		t.Errorf("card day: cash kept %d, want 100", got)
	}
	if got := cashDay.Report.CashKept(); got != 36500 {
		t.Errorf("cash day: cash kept %d, want 365", got)
	}
	s := domain.Summarize([]domain.PersonReport{cardDay, cashDay})
	if s.Self != 39600 || s.CashKept != 46500 || s.BalanceDelta != -6900 {
		t.Errorf("month: «Себе» %d, kept %d, delta %d; want 396 = 465 − 69", s.Self, s.CashKept, s.BalanceDelta)
	}
	if s.CashKept+s.BalanceDelta != s.Self {
		t.Error("kept + delta must equal «Себе»")
	}
}
