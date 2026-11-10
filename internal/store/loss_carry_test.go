package store_test

import (
	"testing"
	"time"

	"github.com/lieranderl/aestheticlab-income-app/internal/domain"
	"github.com/lieranderl/aestheticlab-income-app/internal/store"
)

func TestALossAndARecountShortageCarryIntoTheNextClose(t *testing.T) {
	eachSalon(t, func(t *testing.T, s *salon) {
		nov, dec := month(t, "2026-11"), month(t, "2026-12")
		// November: Anna's €1 000 salary and no income — a €1 000 loss.
		*s.clock = time.Date(2026, 12, 2, 12, 0, 0, 0, time.UTC)
		closed, err := s.CloseMonth(ctx, s.admin, nov)
		must(t, err)
		if r := closed.Snapshot.Result; r.Profit != -100000 || r.DividendPool != 0 || r.LossCarriedOut != 100000 {
			t.Fatalf("November profit %d pool %d carried %d", r.Profit, r.DividendPool, r.LossCarriedOut)
		}

		// December: Olga's €5 000 cash day at 70% leaves €1 500 in the till;
		// the recount finds €30 missing. Profit 1 500 − 1 000 − 30 = €470.
		_, err = s.SaveReport(ctx, s.actor(s.olga), store.ReportEntry{Person: s.olga, Date: date(t, "2026-12-01"), Revenue: map[string]domain.Cents{"services": 500000}, Payments: map[string]domain.Cents{"cash": 500000}})
		must(t, err)
		_, err = s.Recount(ctx, s.admin, domain.KassaCash, 147000, "недостача")
		must(t, err)
		preview, err := s.Settlement(ctx, dec)
		must(t, err)
		if r := preview.Result; r.KassaDifference != -3000 || r.Profit != 47000 || r.LossCarriedIn != 100000 || r.DividendPool != 0 || r.LossCarriedOut != 53000 {
			t.Fatalf("December preview: difference %d profit %d in %d pool %d out %d", r.KassaDifference, r.Profit, r.LossCarriedIn, r.DividendPool, r.LossCarriedOut)
		}

		*s.clock = time.Date(2027, 1, 2, 12, 0, 0, 0, time.UTC)
		first, err := s.CloseMonth(ctx, s.admin, dec)
		must(t, err)
		_, err = s.ReopenMonth(ctx, s.admin, dec)
		must(t, err)
		again, err := s.CloseMonth(ctx, s.admin, dec)
		must(t, err)
		for name, st := range map[string]store.MonthState{"first close": first, "close again": again} {
			r := st.Snapshot.Result
			if r.KassaDifference != -3000 || r.LossCarriedIn != 100000 || r.DividendPool != 0 || r.LossCarriedOut != 53000 {
				t.Fatalf("%s: difference %d in %d pool %d out %d", name, r.KassaDifference, r.LossCarriedIn, r.DividendPool, r.LossCarriedOut)
			}
		}
		read, err := s.Month(ctx, dec)
		must(t, err)
		if read.Snapshot.Result.LossCarriedOut != 53000 || read.Snapshot.Result.KassaDifference != -3000 {
			t.Fatalf("saved snapshot lost the figures: %+v", read.Snapshot.Result)
		}
	})
}

func TestMonthsBeforeTheNewProfitRulesSettleLikeTheSheet(t *testing.T) {
	clock := time.Date(2026, 12, 2, 12, 0, 0, 0, time.UTC)
	st := store.NewMemory(store.WithClock(func() time.Time { return clock }), store.WithProfitRulesFrom(month(t, "2026-12")))
	admin, err := st.SetUp(ctx, defaultSetup())
	must(t, err)
	must(t, st.SetTerms(ctx, admin.Actor(), admin.ID, month(t, "2026-11"), domain.Terms{SelfPct: 5000, SalaryCents: 100000, DividendWeight: 1}, "старт"))
	closed, err := st.CloseMonth(ctx, admin.Actor(), month(t, "2026-11"))
	must(t, err)
	if r := closed.Snapshot.Result; r.Profit != -100000 || r.LossCarriedOut != 0 {
		t.Fatalf("a month before the rules carried a loss: profit %d out %d", r.Profit, r.LossCarriedOut)
	}
}

func TestARecountWaitsForPendingDepositsIntoThatAccount(t *testing.T) {
	eachSalon(t, func(t *testing.T, s *salon) {
		dep, err := s.RecordMovement(ctx, s.actor(s.olga), store.NewMovement{Type: domain.Deposit, Amount: 10000, From: domain.Person(s.olga), To: domain.Account(domain.KassaCash), Date: date(t, "2026-11-12")})
		must(t, err)
		_, err = s.Recount(ctx, s.admin, domain.KassaCash, 10000, "пересчёт")
		wantProblem(t, "a pending deposit into the till", err, store.ProblemPendingDeposits)
		_, err = s.Recount(ctx, s.admin, domain.KassaCard, 0, "")
		must(t, err) // the card account has nothing waiting
		_, err = s.ConfirmMovement(ctx, s.admin, dep.ID, dep.Version)
		must(t, err)
		got, err := s.Recount(ctx, s.admin, domain.KassaCash, 10000, "")
		must(t, err)
		if got.Adjustment != nil {
			t.Fatalf("a confirmed deposit still made an adjustment: %+v", got.Adjustment)
		}
	})
}

func TestAPreviewCarriesTheLossOfOpenMonthsInBetween(t *testing.T) {
	eachSalon(t, func(t *testing.T, s *salon) {
		// November (open) loses Anna's €1 000 salary; December, still open,
		// earns €1 500 at Olga's 70% less the same salary: €500, all of it
		// recovering November's loss once both close.
		*s.clock = time.Date(2027, 1, 2, 12, 0, 0, 0, time.UTC)
		_, err := s.SaveReport(ctx, s.actor(s.olga), store.ReportEntry{Person: s.olga, Date: date(t, "2026-12-01"), Revenue: map[string]domain.Cents{"services": 500000}, Payments: map[string]domain.Cents{"cash": 500000}})
		must(t, err)
		dec, err := s.Settlement(ctx, month(t, "2026-12"))
		must(t, err)
		if r := dec.Result; r.Profit != 50000 || r.LossCarriedIn != 100000 || r.DividendPool != 0 || r.LossCarriedOut != 50000 {
			t.Fatalf("December preview: profit %d in %d pool %d out %d", r.Profit, r.LossCarriedIn, r.DividendPool, r.LossCarriedOut)
		}
	})
}

func TestOnePersonsReportsForADayLeaveOutOtherDaysAndPeople(t *testing.T) {
	eachSalon(t, func(t *testing.T, s *salon) {
		for _, r := range []struct {
			person, day string
		}{{s.olga, "2026-11-10"}, {s.olga, "2026-11-11"}, {s.maria, "2026-11-10"}} {
			_, err := s.SaveReport(ctx, s.actor(r.person), store.ReportEntry{Person: r.person, Date: date(t, r.day), Revenue: map[string]domain.Cents{"services": 1000}, Payments: map[string]domain.Cents{"card": 1000}})
			must(t, err)
		}
		got, err := s.PersonReportsOn(ctx, s.olga, date(t, "2026-11-10"))
		must(t, err)
		if len(got) != 1 || got[0].Person != s.olga || got[0].Date != date(t, "2026-11-10") {
			t.Fatalf("got %+v", got)
		}
	})
}
