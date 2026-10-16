package main

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/lieranderl/aestheticlab-income-app/internal/domain"
	"github.com/lieranderl/aestheticlab-income-app/internal/store"
)

// historySalon is the store as in production: the app starts in October
// 2026, so April–September are history.
func historySalon(t *testing.T) *store.Store {
	t.Helper()
	st := store.NewMemory(
		store.WithClock(func() time.Time { return time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC) }),
		store.WithStartMonth(domain.Month{Year: 2026, Mon: time.October}),
	)
	anna, err := st.SetUp(ctx, store.DefaultSetup(store.NewUser{Username: "anna", DisplayName: "Анна", Role: store.RoleAdmin, SubmitsReports: true, PasswordHash: "x"}))
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range []store.NewUser{
		{Username: "maria", DisplayName: "Мария", Role: store.RoleEmployee, SubmitsReports: true, PasswordHash: "x"},
		{Username: "olga", DisplayName: "Olga Petrova", Role: store.RoleEmployee, SubmitsReports: true, PasswordHash: "x"},
	} {
		if _, err := st.CreateUser(ctx, anna.Actor(), u); err != nil {
			t.Fatal(err)
		}
	}
	return st
}

// trendDashboard is the «4 Trend Dashboard» tab as Google Sheets downloads
// it (File → Download → CSV), with made-up figures.
const trendDashboard = `"   ✨ Aesthetic Lab — Executive Multi-Month Performance Dashboard",,,,,,,,,,,,
,,,,,,,,,,,,
📊 Multi-Month Financial Performance,,,,,,,,,👩‍🎨 Revenue Breakdown by Staff Master,,,
Month,Total Revenue,Salon Retained Share,Overhead Expenses,Manager Salary,Total Salon Expenses,Net Operating Profit,Owner Dividends,,Month,Maria,Olga,Anna
2026-08,"€10,000.00","€3,500.50","€1,800.00","€1,000.00","€2,800.00",€700.50,€700.50,,2026-08,"€6,000.00","€4,000.00",€0.00
2026-09,"€12,000.00","€3,600.00","€2,100.00","€1,000.00","€3,100.00",€500.00,€500.00,,2026-09,"€7,500.00","€3,800.00",€700.00
Total,"€22,000.00","€7,100.50","€3,900.00","€2,000.00","€5,900.00","€1,200.50","€1,200.50",,Total,"€13,500.00","€7,800.00",€700.00
Average,"€11,000.00","€3,550.25","€1,950.00","€1,000.00","€2,950.00",€600.25,€600.25,,Average,"€6,750.00","€3,900.00",€350.00
`

func TestSheetHistoryIsPrintedFirstThenLoadedOnce(t *testing.T) {
	st := historySalon(t)
	var out bytes.Buffer
	opt := historyOptions{admin: "anna", dryRun: true, source: "trend.csv"}
	if err := runHistory(ctx, st, opt, strings.NewReader(trendDashboard), &out); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"dry run", "2026-08", "€10\u202f000", "€3\u202f500,50", "€1\u202f800", "€700,50",
		"2026-09", "Мария (maria)", "€7\u202f500", "Olga Petrova (olga)", "Анна (anna)",
		"total", "€22\u202f000", "€1\u202f200,50", "matches the sheet's Total row",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("dry run lacks %q:\n%s", want, out.String())
		}
	}
	if got := imported(t, st); len(got) != 0 {
		t.Fatalf("a dry run wrote %v", got)
	}

	out.Reset()
	opt.dryRun = false
	if err := runHistory(ctx, st, opt, strings.NewReader(trendDashboard), &out); err != nil {
		t.Fatal(err)
	}
	maria, olga, anna := user(t, st, "maria").ID, user(t, st, "olga").ID, user(t, st, "anna").ID
	want := map[domain.Month]domain.ImportedMonth{
		{Year: 2026, Mon: time.August}: {
			RevenueByPerson: map[string]domain.Cents{maria: 600000, olga: 400000},
			RevenueTotal:    1000000, SalonIncome: 350050, Expenses: 180000, ManagerSalary: 100000, Profit: 70050, Dividends: 70050,
		},
		{Year: 2026, Mon: time.September}: {
			RevenueByPerson: map[string]domain.Cents{maria: 750000, olga: 380000, anna: 70000},
			RevenueTotal:    1200000, SalonIncome: 360000, Expenses: 210000, ManagerSalary: 100000, Profit: 50000, Dividends: 50000,
		},
	}
	if got := imported(t, st); !sameHistory(got, want) {
		t.Errorf("imported = %+v\nwant %+v", got, want)
	}
	if !strings.Contains(out.String(), "loaded 2") {
		t.Errorf("load report:\n%s", out.String())
	}

	out.Reset()
	if err := runHistory(ctx, st, opt, strings.NewReader(trendDashboard), &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "already loaded") || !strings.Contains(out.String(), "loaded 0") {
		t.Errorf("a second run should skip both months:\n%s", out.String())
	}
	entries, err := st.Audit(ctx, "2026-08")
	if err != nil || len(entries) != 1 {
		t.Errorf("audit for 2026-08 = %d rows (err %v), want 1", len(entries), err)
	}
}

func TestACorrectedSheetMonthReplacesTheImportedOne(t *testing.T) {
	st := historySalon(t)
	opt := historyOptions{admin: "anna", source: "trend.csv"}
	if err := runHistory(ctx, st, opt, strings.NewReader(trendDashboard), &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	fixed := strings.NewReplacer(`€500.00,€500.00,`, `€500.00,€450.00,`, `"€1,200.50","€1,200.50",`, `"€1,200.50","€1,150.50",`).Replace(trendDashboard)
	var out bytes.Buffer
	if err := runHistory(ctx, st, opt, strings.NewReader(fixed), &out); err != nil {
		t.Fatal(err)
	}
	if got := imported(t, st)[domain.Month{Year: 2026, Mon: time.September}].Dividends; got != 45000 {
		t.Errorf("September dividends = %v, want €450", got)
	}
	if !strings.Contains(out.String(), "replaces the imported figures") || !strings.Contains(out.String(), "loaded 1") {
		t.Errorf("report:\n%s", out.String())
	}
}

func TestASheetFileWithMistakesIsRefusedWhole(t *testing.T) {
	for _, c := range []struct{ name, old, new, wantErr string }{
		{"no header row", "Month,Total Revenue", "Monat,Total Revenue", "header"},
		{"a missing figure column", "Owner Dividends", "Dividends paid", `"Owner Dividends"`},
		{"staff revenue not adding up", `"€6,000.00","€4,000.00"`, `"€6,000.00","€4,100.00"`, "staff revenue"},
		{"profit not adding up", `€700.50,€700.50,,2026-08`, `€710.50,€700.50,,2026-08`, "profit"},
		{"expenses not adding up", `"€1,000.00","€2,800.00"`, `"€1,000.00","€2,900.00"`, "Total Salon Expenses"},
		{"total row not matching", `"€22,000.00","€7,100.50"`, `"€22,100.00","€7,100.50"`, "Total row"},
		{"a month of the app", "2026-09", "2026-10", "before 2026-10"},
		{"the same month twice", "2026-09", "2026-08", "twice"},
		{"staff block for another month", ",,2026-09,", ",,2026-07,", "2026-07"},
		{"an unknown person", "Maria,Olga,Anna", "Maria,Olga,Vera", `"Vera"`},
		{"an unreadable amount", `€700.50,€700.50,,2026-08`, `€700.50,seven,,2026-08`, `"seven"`},
		{"a negative revenue", "€0.00\n2026-09", "-€0.50\n2026-09", "below zero"},
		{"no months", "\n2026-0", "\nx2026-0", "no months"},
		{"a blank figure", `€700.50,€700.50,,2026-08`, `€700.50,,,2026-08`, "Owner Dividends"},
		{"a dash for a figure", `€700.50,€700.50,,2026-08`, `€700.50,-,,2026-08`, "Owner Dividends"},
		{"a blank staff cell", `"€4,000.00",€0.00`, `"€4,000.00",`, "revenue"},
		{"negative dividends", `€500.00,€500.00,`, `€500.00,-€500.00,`, "below zero"},
		{"no Total row", "\nTotal,", "\nSum,", "Total row"},
		{"two Total rows", "\nAverage,", "\nTotal,", "Total rows"},
	} {
		t.Run(c.name, func(t *testing.T) {
			st := historySalon(t)
			file := strings.ReplaceAll(trendDashboard, c.old, c.new)
			if file == trendDashboard {
				t.Fatalf("the edit %q found nothing to change", c.old)
			}
			err := runHistory(ctx, st, historyOptions{admin: "anna"}, strings.NewReader(file), &bytes.Buffer{})
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Fatalf("err = %v, want one naming %s", err, c.wantErr)
			}
			if got := imported(t, st); len(got) != 0 {
				t.Errorf("wrote %v", got)
			}
		})
	}
}

func TestSheetColumnsCanBeMatchedToPeopleByName(t *testing.T) {
	st := historySalon(t)
	file := strings.Replace(trendDashboard, "Maria,Olga,Anna", "Masha,Olga,Anna", 1)
	err := runHistory(ctx, st, historyOptions{admin: "anna", dryRun: true}, strings.NewReader(file), &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), `"Masha"`) || !strings.Contains(err.Error(), "-person") {
		t.Fatalf("an unknown column should name itself and -person: %v", err)
	}
	opt := historyOptions{admin: "anna", people: map[string]string{"Masha": "maria"}}
	if err := runHistory(ctx, st, opt, strings.NewReader(file), &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if got := imported(t, st)[domain.Month{Year: 2026, Mon: time.August}].RevenueByPerson[user(t, st, "maria").ID]; got != 600000 {
		t.Errorf("Masha's August = %v, want €6000 for maria", got)
	}
}

func TestSheetAmountsReadEitherLocale(t *testing.T) {
	for in, want := range map[string]domain.Cents{
		"€12,428.00": 1242800, "€699.00": 69900, "€0.00": 0, "€1,234": 123400, "12 428,50 €": 1242850,
		"€1.234,50": 123450, "-€2.50": -250, "−€2,50": -250, "€-2.50": -250, "": 0,
	} {
		got, err := sheetAmount(in)
		if err != nil || got != want {
			t.Errorf("sheetAmount(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, in := range []string{"seven", "€1,23,4", "€1.2.3", "€12,3456.00", "€ 1 2", "1,234,56", "12.", "12,", "€1,2", "€1.234,5", "-", "€ -"} {
		if _, err := sheetAmount(in); err == nil {
			t.Errorf("sheetAmount(%q) accepted", in)
		}
	}
}

func imported(t *testing.T, st *store.Store) map[domain.Month]domain.ImportedMonth {
	t.Helper()
	got, err := st.ImportedMonths(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func sameHistory(a, b map[domain.Month]domain.ImportedMonth) bool {
	return reflect.DeepEqual(a, b)
}

func TestABlankMonthRowIsLeftOut(t *testing.T) {
	st := historySalon(t)
	file := strings.Replace(trendDashboard, "\n2026-08,", "\n2026-07,,,,,,,,,2026-07,,,\n2026-08,", 1)
	if err := runHistory(ctx, st, historyOptions{admin: "anna"}, strings.NewReader(file), &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if got := imported(t, st); len(got) != 2 {
		t.Errorf("imported %d months, want August and September only: %v", len(got), got)
	}
}

func TestTheHistoryCommandNeedsTheStartMonthNamed(t *testing.T) {
	t.Setenv("INCOME_START_MONTH", "")
	if code := historyCmd([]string{"-project", "p", "trend.csv"}); code != 2 {
		t.Errorf("without -start-month: exit %d, want 2", code)
	}
}

func TestAnOversizedFileIsRefused(t *testing.T) {
	st := historySalon(t)
	big := trendDashboard + strings.Repeat(",,,,,,,,,,,,\n", 1<<17)
	err := runHistory(ctx, st, historyOptions{admin: "anna"}, strings.NewReader(big), &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "MiB") {
		t.Fatalf("err = %v", err)
	}
}
