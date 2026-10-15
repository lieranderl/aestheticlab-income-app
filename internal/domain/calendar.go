package domain

import (
	"errors"
	"fmt"
	"time"
)

// Month is a calendar month, written "2026-11".
type Month struct {
	Year int
	Mon  time.Month
}

// Date is a salon calendar day, written "2026-11-08".
type Date struct {
	Year int
	Mon  time.Month
	Day  int
}

var errBadCalendar = errors.New("invalid month or date")

// ParseMonth reads "YYYY-MM".
func ParseMonth(s string) (Month, error) {
	t, err := time.Parse("2006-01", s)
	if err != nil || len(s) != 7 {
		return Month{}, fmt.Errorf("%w: %q", errBadCalendar, s)
	}
	return Month{t.Year(), t.Month()}, nil
}

// ParseDate reads "YYYY-MM-DD" and rejects impossible days.
func ParseDate(s string) (Date, error) {
	t, err := time.Parse(time.DateOnly, s)
	if err != nil || len(s) != 10 {
		return Date{}, fmt.Errorf("%w: %q", errBadCalendar, s)
	}
	return Date{t.Year(), t.Month(), t.Day()}, nil
}

// Today is the salon's calendar day at now in loc (Europe/Brussels).
func Today(now time.Time, loc *time.Location) Date {
	t := now.In(loc)
	return Date{t.Year(), t.Month(), t.Day()}
}

func (m Month) String() string { return fmt.Sprintf("%04d-%02d", m.Year, int(m.Mon)) }
func (d Date) String() string  { return fmt.Sprintf("%04d-%02d-%02d", d.Year, int(d.Mon), d.Day) }

// Month is the month the date falls in.
func (d Date) Month() Month { return Month{d.Year, d.Mon} }

// Time is the date at midnight UTC, for formatting and weekdays.
func (d Date) Time() time.Time { return time.Date(d.Year, d.Mon, d.Day, 0, 0, 0, 0, time.UTC) }

// AddDays is the date n days later (earlier for n < 0).
func (d Date) AddDays(n int) Date {
	t := d.Time().AddDate(0, 0, n)
	return Date{t.Year(), t.Month(), t.Day()}
}

// Before reports whether d is earlier than o.
func (d Date) Before(o Date) bool { return d.String() < o.String() }

// Before reports whether m is earlier than o.
func (m Month) Before(o Month) bool { return m.String() < o.String() }

func (m Month) first() time.Time { return time.Date(m.Year, m.Mon, 1, 0, 0, 0, 0, time.UTC) }

// Next is the following month.
func (m Month) Next() Month { t := m.first().AddDate(0, 1, 0); return Month{t.Year(), t.Month()} }

// Prev is the preceding month.
func (m Month) Prev() Month { t := m.first().AddDate(0, -1, 0); return Month{t.Year(), t.Month()} }

// FirstDay is the 1st of the month (the date of opening rows in the start month).
func (m Month) FirstDay() Date { return Date{m.Year, m.Mon, 1} }

// LastDay is the last day of the month (the date of salary and dividend accruals).
func (m Month) LastDay() Date {
	t := m.first().AddDate(0, 1, -1)
	return Date{t.Year(), t.Month(), t.Day()}
}

// MonthStatus is whether a month still accepts writes.
type MonthStatus string

// A month with no months/{YYYY-MM} doc is open.
const (
	MonthOpen   MonthStatus = "open"
	MonthClosed MonthStatus = "closed"
)

// Reasons a calendar rule rejects a write.
const (
	ProblemBeforeStartMonth     Problem = "before_start_month"
	ProblemFutureDate           Problem = "future_date"
	ProblemMonthClosed          Problem = "month_closed"
	ProblemPreviousMonthOpen    Problem = "previous_month_open"
	ProblemPendingMovements     Problem = "pending_movements"
	ProblemNotNewestClosed      Problem = "not_newest_closed"
	ProblemTermsMonthHasReports Problem = "terms_month_has_reports"
)

// CheckDate accepts report and movement dates from the start month
// (INCOME_START_MONTH) up to today; nothing in the future.
func CheckDate(d, today Date, start Month) error {
	switch {
	case d.Month().Before(start):
		return invalid("date", ProblemBeforeStartMonth)
	case today.Before(d):
		return invalid("date", ProblemFutureDate)
	}
	return nil
}

// CheckMonthOpen rejects any write, the admin's included, to a closed month.
func CheckMonthOpen(status MonthStatus) error {
	if status == MonthClosed {
		return invalid("month", ProblemMonthClosed)
	}
	return nil
}

// CheckCanClose allows closing m only in order (the previous month closed,
// except for the start month) and with no pending movements dated in m.
func CheckCanClose(m, start Month, prevClosed bool, pending int) error {
	switch {
	case m.Before(start):
		return invalid("month", ProblemBeforeStartMonth)
	case m != start && !prevClosed:
		return invalid("month", ProblemPreviousMonthOpen)
	case pending > 0:
		return invalid("month", ProblemPendingMovements)
	}
	return nil
}

// CheckCanReopen allows reopening only the newest closed month, so no
// remaining snapshot ever depended on the month that changes.
func CheckCanReopen(m Month, newestClosed *Month) error {
	if newestClosed == nil || *newestClosed != m {
		return invalid("month", ProblemNotNewestClosed)
	}
	return nil
}

// Terms are a person's money terms in force from a month on. With a
// threshold, a month whose «Себе» base is above it earns HighSelfPct on the
// whole base: reports use SelfPct, and closing the month accrues the
// difference (SelfBonusFor).
type Terms struct {
	SelfPct           BasisPoints
	SalaryCents       Cents
	DividendWeight    int64
	HighSelfFromCents Cents       // 0 = one rate all month
	HighSelfPct       BasisPoints // the whole month's rate above the threshold
}

// ProblemBadTerms rejects terms out of range: «Себе» above 100%, negative
// amounts, or a higher rate without a threshold, not above «Себе» or above 100%.
const ProblemBadTerms Problem = "bad_terms"

// ValidateTerms checks one set of terms.
func ValidateTerms(t Terms) error {
	ok := t.SelfPct >= 0 && t.SelfPct <= 10000 && t.SalaryCents >= 0 && t.DividendWeight >= 0 && t.HighSelfFromCents >= 0
	if t.HighSelfFromCents > 0 {
		ok = ok && t.HighSelfPct > t.SelfPct && t.HighSelfPct <= 10000
	} else {
		ok = ok && t.HighSelfPct == 0
	}
	if !ok {
		return invalid("terms", ProblemBadTerms)
	}
	return nil
}

// SelfBonusFor is what a month's «Себе» base above the threshold adds at
// close: the higher rate's difference on bonusBase, rounded once. base is
// every report's «Себе» base; bonusBase leaves out days whose «Себе» was set
// by hand, which keep exactly what was given to them.
func SelfBonusFor(base, bonusBase Cents, t Terms) Cents {
	if t.HighSelfFromCents <= 0 || base <= t.HighSelfFromCents || t.HighSelfPct <= t.SelfPct {
		return 0
	}
	return ApplyPercent(bonusBase, t.HighSelfPct-t.SelfPct)
}

// TermsHistory holds a person's terms by the month they start.
type TermsHistory map[Month]Terms

// For returns the terms in force in m: the entry with the latest start
// month not after m. Today's values are never used for past months.
func (h TermsHistory) For(m Month) (Terms, bool) {
	var (
		best  Month
		found bool
	)
	for start := range h {
		if !m.Before(start) && (!found || best.Before(start)) {
			best, found = start, true
		}
	}
	return h[best], found
}

// CheckCanStartTerms allows new terms only in an open month in which the
// person has no reports yet, so one month never mixes two rates.
func CheckCanStartTerms(status MonthStatus, personHasReports bool) error {
	if err := CheckMonthOpen(status); err != nil {
		return err
	}
	if personHasReports {
		return invalid("terms", ProblemTermsMonthHasReports)
	}
	return nil
}
