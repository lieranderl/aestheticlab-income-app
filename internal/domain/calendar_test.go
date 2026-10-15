package domain_test

import (
	"errors"
	"testing"
	"time"

	"github.com/lieranderl/aestheticlab-income-app/internal/domain"
)

func month(t *testing.T, s string) domain.Month {
	t.Helper()
	m, err := domain.ParseMonth(s)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func date(t *testing.T, s string) domain.Date {
	t.Helper()
	d, err := domain.ParseDate(s)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func problem(t *testing.T, err error) domain.Problem {
	t.Helper()
	var ve *domain.ValidationError
	if err == nil {
		return ""
	}
	if !errors.As(err, &ve) {
		t.Fatalf("error %v is not a ValidationError", err)
	}
	return ve.Problem
}

func TestMonthsAndDatesReadAndPrintAsISO(t *testing.T) {
	if got := month(t, "2026-11").String(); got != "2026-11" {
		t.Errorf("month = %q", got)
	}
	if got := month(t, "2026-12").Next().String(); got != "2027-01" {
		t.Errorf("next of December = %q", got)
	}
	if got := month(t, "2027-01").Prev().String(); got != "2026-12" {
		t.Errorf("previous of January = %q", got)
	}
	d := date(t, "2026-11-08")
	if d.String() != "2026-11-08" || d.Month().String() != "2026-11" {
		t.Errorf("date = %q in %q", d, d.Month())
	}
	if got := month(t, "2026-02").LastDay().String(); got != "2026-02-28" {
		t.Errorf("last day of February 2026 = %q", got)
	}
	if got := month(t, "2026-11").FirstDay().String(); got != "2026-11-01" {
		t.Errorf("first day = %q", got)
	}
	if got := date(t, "2026-11-30").AddDays(1).String(); got != "2026-12-01" {
		t.Errorf("the day after 30 November = %q", got)
	}
	if got := date(t, "2026-11-01").AddDays(-1).String(); got != "2026-10-31" {
		t.Errorf("the day before 1 November = %q", got)
	}
	if got := date(t, "2026-11-20").Time().Weekday(); got != time.Friday {
		t.Errorf("20 November 2026 is a %v", got)
	}
	for _, bad := range []string{"2026-13", "2026-1", "26-11", "2026/11", ""} {
		if _, err := domain.ParseMonth(bad); err == nil {
			t.Errorf("ParseMonth(%q) accepted", bad)
		}
	}
	for _, bad := range []string{"2026-02-30", "2026-11-8", "2026-11", "x"} {
		if _, err := domain.ParseDate(bad); err == nil {
			t.Errorf("ParseDate(%q) accepted", bad)
		}
	}
}

func TestTodayIsTheSalonsCalendarDayInBrussels(t *testing.T) {
	brussels, err := time.LoadLocation("Europe/Brussels")
	if err != nil {
		t.Fatal(err)
	}
	// 23:30 UTC on 31 October is already 1 November in Brussels (UTC+1).
	now := time.Date(2026, 10, 31, 23, 30, 0, 0, time.UTC)
	if got := domain.Today(now, brussels).String(); got != "2026-11-01" {
		t.Fatalf("today = %q, want 2026-11-01", got)
	}
}

func TestReportsAndMovementsAreDatedFromTheStartMonthUntilToday(t *testing.T) {
	today := date(t, "2026-11-08")
	prod := month(t, "2026-11")
	trial := month(t, "2026-10")
	cases := []struct {
		date  string
		start domain.Month
		want  domain.Problem
	}{
		{"2026-11-08", prod, ""},
		{"2026-11-01", prod, ""},
		{"2026-10-31", prod, domain.ProblemBeforeStartMonth},
		{"2026-10-31", trial, ""}, // the October trial accepts October
		{"2026-11-09", prod, domain.ProblemFutureDate},
	}
	for _, c := range cases {
		if got := problem(t, domain.CheckDate(date(t, c.date), today, c.start)); got != c.want {
			t.Errorf("CheckDate(%s, start %s) = %q, want %q", c.date, c.start, got, c.want)
		}
	}
}

func TestAClosedMonthIsLockedForEveryone(t *testing.T) {
	if got := problem(t, domain.CheckMonthOpen(domain.MonthClosed)); got != domain.ProblemMonthClosed {
		t.Fatalf("closed month = %q, want %q", got, domain.ProblemMonthClosed)
	}
	if err := domain.CheckMonthOpen(domain.MonthOpen); err != nil {
		t.Fatalf("open month: %v", err)
	}
}

func TestMonthsCloseOnlyInOrderAndWithNothingPending(t *testing.T) {
	start := month(t, "2026-11")
	cases := []struct {
		name       string
		m          string
		prevClosed bool
		pending    int
		want       domain.Problem
	}{
		{"the start month needs no previous month", "2026-11", false, 0, ""},
		{"December after November closed", "2026-12", true, 0, ""},
		{"December while November is open", "2026-12", false, 0, domain.ProblemPreviousMonthOpen},
		{"pending movements block closing", "2026-11", false, 2, domain.ProblemPendingMovements},
		{"before the start month", "2026-10", true, 0, domain.ProblemBeforeStartMonth},
	}
	for _, c := range cases {
		got := problem(t, domain.CheckCanClose(month(t, c.m), start, c.prevClosed, c.pending))
		if got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
	// The October trial starts a month earlier, so October needs no predecessor there.
	if err := domain.CheckCanClose(month(t, "2026-10"), month(t, "2026-10"), false, 0); err != nil {
		t.Errorf("trial start month: %v", err)
	}
}

func TestOnlyTheNewestClosedMonthCanBeReopened(t *testing.T) {
	newest := month(t, "2026-12")
	if err := domain.CheckCanReopen(month(t, "2026-12"), &newest); err != nil {
		t.Errorf("reopen newest: %v", err)
	}
	if got := problem(t, domain.CheckCanReopen(month(t, "2026-11"), &newest)); got != domain.ProblemNotNewestClosed {
		t.Errorf("reopen older = %q, want %q", got, domain.ProblemNotNewestClosed)
	}
	if got := problem(t, domain.CheckCanReopen(month(t, "2026-11"), nil)); got != domain.ProblemNotNewestClosed {
		t.Errorf("reopen with nothing closed = %q", got)
	}
}

func TestTermsApplyFromTheirMonthAndNeverChangeAMonthWithReports(t *testing.T) {
	history := domain.TermsHistory{
		month(t, "2026-11"): {SelfPct: 5000, SalaryCents: 100000, DividendWeight: 1},
		month(t, "2027-01"): {SelfPct: 5000, SalaryCents: 120000, DividendWeight: 1},
	}
	cases := map[string]domain.Cents{"2026-11": 100000, "2026-12": 100000, "2027-01": 120000, "2027-06": 120000}
	for m, want := range cases {
		terms, ok := history.For(month(t, m))
		if !ok || terms.SalaryCents != want {
			t.Errorf("salary in %s = %d (found %v), want %d", m, terms.SalaryCents, ok, want)
		}
	}
	if _, ok := history.For(month(t, "2026-10")); ok {
		t.Error("terms before the first entry must not exist")
	}

	if err := domain.CheckCanStartTerms(domain.MonthOpen, false); err != nil {
		t.Errorf("open month without reports: %v", err)
	}
	if got := problem(t, domain.CheckCanStartTerms(domain.MonthOpen, true)); got != domain.ProblemTermsMonthHasReports {
		t.Errorf("month with reports = %q", got)
	}
	if got := problem(t, domain.CheckCanStartTerms(domain.MonthClosed, false)); got != domain.ProblemMonthClosed {
		t.Errorf("closed month = %q", got)
	}
}
