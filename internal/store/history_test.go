package store_test

import (
	"slices"
	"testing"
	"time"

	"github.com/lieranderl/aestheticlab-income-app/internal/domain"
	"github.com/lieranderl/aestheticlab-income-app/internal/store"
)

func TestExpectedReportersPreservesHistoricalStaffStatus(t *testing.T) {
	eachSalon(t, func(t *testing.T, s *salon) {
		s.at("2026-11-21")
		_, err := s.UpdateUser(ctx, s.admin, s.maria, func(u *store.User) error { u.Active = false; return nil })
		must(t, err)
		_, err = s.UpdateUser(ctx, s.admin, s.olga, func(u *store.User) error { u.SubmitsReports = false; return nil })
		must(t, err)
		newPerson := s.addPerson(t, "new-person", "Новый сотрудник")
		for day, want := range map[string][]string{
			"2026-11-19": nil,
			"2026-11-20": {s.anna, s.olga, s.maria, s.vera},
			"2026-11-21": {s.anna, s.vera, newPerson},
		} {
			got, err := s.ExpectedReporters(ctx, date(t, day))
			must(t, err)
			slices.Sort(want)
			if !slices.Equal(got, want) {
				t.Fatalf("%s: got %v, want %v", day, got, want)
			}
		}
		s.at("2026-11-22")
		_, err = s.UpdateUser(ctx, s.admin, s.maria, func(u *store.User) error { u.Active = true; return nil })
		must(t, err)
		got, err := s.ExpectedReporters(ctx, date(t, "2026-11-21"))
		must(t, err)
		if slices.Contains(got, s.maria) {
			t.Fatal("reactivation added a historical gap")
		}
	})
}

func TestExpectedReportersUsesSalonMidnight(t *testing.T) {
	eachSalon(t, func(t *testing.T, s *salon) {
		*s.clock = time.Date(2026, 11, 20, 23, 0, 0, 0, time.UTC)
		uid := s.addPerson(t, "midnight", "Полночь")
		for day, expected := range map[string]bool{"2026-11-20": false, "2026-11-21": true} {
			got, err := s.ExpectedReporters(ctx, date(t, day))
			must(t, err)
			if slices.Contains(got, uid) != expected {
				t.Fatalf("wrong creation boundary on %s", day)
			}
		}
	})
}

func TestHistoryReadsOnlyTheRequestedReportAndAuditDates(t *testing.T) {
	eachSalon(t, func(t *testing.T, s *salon) {
		s.at("2026-11-01")
		_, err := s.SaveReport(ctx, s.actor(s.olga), store.ReportEntry{Person: s.olga, Date: domain.Date{Year: 2026, Mon: 11, Day: 1}, Revenue: map[string]domain.Cents{"services": 20000}, Payments: map[string]domain.Cents{"cash": 20000}})
		must(t, err)
		s.at("2026-11-02")
		_, err = s.SaveReport(ctx, s.actor(s.olga), store.ReportEntry{Person: s.olga, Date: domain.Date{Year: 2026, Mon: 11, Day: 2}, Revenue: map[string]domain.Cents{"services": 10000}, Payments: map[string]domain.Cents{"card": 10000}})
		must(t, err)
		from := domain.Date{Year: 2026, Mon: 11, Day: 1}
		reports, err := s.ReportsBetween(ctx, from, from)
		must(t, err)
		if len(reports) != 1 || reports[0].Report.Total != 20000 {
			t.Fatalf("reports: %+v", reports)
		}
		rows, err := s.AuditBetween(ctx, from, from)
		must(t, err)
		found := 0
		for _, row := range rows {
			if row.EntityID == s.olga+"_2026-11-01" {
				found++
			}
			if row.EntityID == s.olga+"_2026-11-02" {
				t.Fatal("audit includes the next day")
			}
		}
		if found != 1 {
			t.Fatalf("first day's report audit count %d", found)
		}
	})
}

func TestAuditPeriodIncludesTheWholeSalonDayAtUTCBoundaries(t *testing.T) {
	eachSalon(t, func(t *testing.T, s *salon) {
		for i, stamp := range []string{"2026-10-31T22:59:59Z", "2026-10-31T23:00:00Z", "2026-11-01T22:59:59.999999Z", "2026-11-01T23:00:00Z"} {
			at, err := time.Parse(time.RFC3339Nano, stamp)
			must(t, err)
			*s.clock = at
			_, err = s.UpdateUser(ctx, s.admin, s.olga, func(u *store.User) error {
				u.DisplayName = []string{"До", "Начало", "Конец", "После"}[i]
				return nil
			})
			must(t, err)
		}
		day := domain.Date{Year: 2026, Mon: 11, Day: 1}
		rows, err := s.AuditBetween(ctx, day, day)
		must(t, err)
		if len(rows) != 2 {
			t.Fatalf("salon day: got %d rows, want two", len(rows))
		}
		for _, row := range rows {
			if s.LocalTime(row.At).Format(time.DateOnly) != "2026-11-01" {
				t.Fatalf("outside day: %v", row.At)
			}
		}
	})
}

func TestExpectedReporterRangePreservesDailyRosterAndSalonMidnight(t *testing.T) {
	eachSalon(t, func(t *testing.T, s *salon) {
		s.at("2026-11-21")
		_, err := s.UpdateUser(ctx, s.admin, s.maria, func(u *store.User) error { u.Active = false; return nil })
		must(t, err)
		_, err = s.UpdateUser(ctx, s.admin, s.olga, func(u *store.User) error { u.SubmitsReports = false; return nil })
		must(t, err)
		newcomer := s.addPerson(t, "newcomer", "Новый сотрудник")
		*s.clock = time.Date(2026, 11, 21, 23, 0, 0, 0, time.UTC)
		midnight := s.addPerson(t, "midnight-range", "Полночь")
		s.at("2026-11-22")
		_, err = s.UpdateUser(ctx, s.admin, s.maria, func(u *store.User) error { u.Active = true; return nil })
		must(t, err)
		s.at("2026-11-24")
		_, err = s.UpdateUser(ctx, s.admin, s.maria, func(u *store.User) error { u.Active = false; return nil })
		must(t, err)
		rosters, err := s.ExpectedReportersBetween(ctx, date(t, "2026-11-19"), date(t, "2026-11-23"))
		must(t, err)
		for day, want := range map[string][]string{
			"2026-11-19": nil,
			"2026-11-20": {s.anna, s.olga, s.maria, s.vera},
			"2026-11-21": {s.anna, s.vera, newcomer},
			"2026-11-22": {s.anna, s.vera, s.maria, newcomer, midnight},
			"2026-11-23": {s.anna, s.vera, s.maria, newcomer, midnight},
		} {
			slices.Sort(want)
			if got := rosters[date(t, day)]; !slices.Equal(got, want) {
				t.Fatalf("%s roster %v, want %v", day, got, want)
			}
		}
		if len(rosters) != 5 {
			t.Fatalf("range has %d days", len(rosters))
		}
		_, err = s.ExpectedReportersBetween(ctx, date(t, "2026-11-23"), date(t, "2026-11-19"))
		wantErr(t, "reversed roster period", err, store.ErrInvalid)
	})
}
