package store

import (
	"context"

	"github.com/lieranderl/aestheticlab-income-app/internal/domain"
)

// PersonMovementsIn reads only this person's rows in a month, including voided rows.
func (s *Store) PersonMovementsIn(ctx context.Context, uid string, m domain.Month) ([]Entry, error) {
	if err := checkID("user", uid); err != nil {
		return nil, err
	}
	return s.queryLedger(ctx, where("personId", "==", uid), where("month", "==", m.String()))
}

// LedgerBetween reads movement history within an inclusive date period.
func (s *Store) LedgerBetween(ctx context.Context, from, to domain.Date) ([]Entry, error) {
	if to.Before(from) {
		return nil, ErrInvalid
	}
	return s.queryLedger(ctx, where("date", ">=", from.String()), where("date", "<=", to.String()))
}

// PersonBalance folds only the person's open records onto their latest closed balance.
// It returns no salon accounts, certificates, or other person's financial data.
func (s *Store) PersonBalance(ctx context.Context, uid string) (domain.Cents, error) {
	if err := checkID("user", uid); err != nil {
		return 0, err
	}
	var out domain.Cents
	err := s.db.run(ctx, true, func(t tx) error {
		var err error
		out, err = s.personBalance(t, uid)
		return err
	})
	return out, err
}

// personBalance is PersonBalance inside a transaction.
func (s *Store) personBalance(t tx, uid string) (domain.Cents, error) {
	r, err := s.replayPerson(t, uid)
	return r.balance, err
}

// personReplay is one person's balance and the rows it was folded from: the
// latest closed snapshot, then their reports and ledger rows since, up to
// today. The settlement suggestion fingerprints the same rows.
type personReplay struct {
	balance domain.Cents
	months  map[domain.Month]monthRecord
	reports []SavedReport
	ledger  []Entry
}

func (s *Store) replayPerson(t tx, uid string) (personReplay, error) {
	months, err := readMonths(t)
	if err != nil {
		return personReplay{}, err
	}
	from := s.start
	start := domain.Balances{Persons: map[string]domain.Cents{}}
	if newest := newestClosed(months); newest != nil {
		snap, err := snapshotOf(months, *newest)
		if err != nil {
			return personReplay{}, err
		}
		start.Persons[uid] = snap.Balances.Persons[uid]
		from = newest.Next()
	}
	reports, err := readReports(t, where("userId", "==", uid), where("date", ">=", from.FirstDay().String()), where("date", "<=", s.Today().String()))
	if err != nil {
		return personReplay{}, err
	}
	ledger, err := readLedger(t, where("personId", "==", uid), where("date", ">=", from.FirstDay().String()), where("date", "<=", s.Today().String()))
	if err != nil {
		return personReplay{}, err
	}
	bal, err := domain.Fold(start, personReports(reports), movements(ledger))
	if err != nil {
		return personReplay{}, err
	}
	return personReplay{balance: bal.Persons[uid], months: months, reports: reports, ledger: ledger}, nil
}
