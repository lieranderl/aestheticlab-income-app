package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"strings"

	"github.com/lieranderl/aestheticlab-income-app/internal/domain"
)

// ProblemSettlementAmount rejects a contextual payment above the realized remaining balance.
const ProblemSettlementAmount domain.Problem = "settlement_amount_exceeds_balance"

// SettlementPayment is a realized balance and a revision of its source records.
type SettlementPayment struct {
	Balance  domain.Cents
	Revision string
}

func (s *Store) SettlementSuggestion(ctx context.Context, uid string, m domain.Month) (SettlementPayment, error) {
	if checkID("user", uid) != nil || m.Before(s.start) || s.Today().Month().Before(m) {
		return SettlementPayment{}, ErrInvalid
	}
	var out SettlementPayment
	err := s.db.run(ctx, true, func(t tx) error {
		var err error
		out, err = s.settlementSuggestion(t, uid, m)
		return err
	})
	return out, err
}

func (s *Store) settlementSuggestion(t tx, uid string, m domain.Month) (SettlementPayment, error) {
	months, err := readMonths(t)
	if err != nil {
		return SettlementPayment{}, err
	}
	from := s.start
	start := domain.Balances{Persons: map[string]domain.Cents{}}
	if newest := newestClosed(months); newest != nil {
		snap, e := snapshotOf(months, *newest)
		if e != nil {
			return SettlementPayment{}, e
		}
		start.Persons[uid] = snap.Balances.Persons[uid]
		from = newest.Next()
	}
	reports, err := readReports(t, where("userId", "==", uid), where("date", ">=", from.FirstDay().String()), where("date", "<=", s.Today().String()))
	if err != nil {
		return SettlementPayment{}, err
	}
	ledger, err := readLedger(t, where("personId", "==", uid), where("date", ">=", from.FirstDay().String()), where("date", "<=", s.Today().String()))
	if err != nil {
		return SettlementPayment{}, err
	}
	bal, err := domain.Fold(start, personReports(reports), movements(ledger))
	if err != nil {
		return SettlementPayment{}, err
	}
	slices.SortFunc(reports, compareReport)
	slices.SortFunc(ledger, func(a, b Entry) int { return strings.Compare(a.ID, b.ID) })
	monthKeys := map[string]monthRecord{}
	for key, rec := range months {
		monthKeys[key.String()] = rec
	}
	bytes, err := json.Marshal(struct {
		Month, UID string
		Months     map[string]monthRecord
		Reports    []SavedReport
		Ledger     []Entry
		Today      string
	}{m.String(), uid, monthKeys, reports, ledger, s.Today().String()})
	if err != nil {
		return SettlementPayment{}, err
	}
	hash := sha256.Sum256(bytes)
	return SettlementPayment{Balance: bal.Persons[uid], Revision: hex.EncodeToString(hash[:])}, nil
}

// RecordSettlementMovement rechecks the actual suggestion in the same audited movement transaction.
func (s *Store) RecordSettlementMovement(ctx context.Context, actor Actor, m domain.Month, uid, revision string, n NewMovement) (Entry, error) {
	if !actor.Admin {
		return Entry{}, ErrForbidden
	}
	if revision == "" || checkID("user", uid) != nil || m.Before(s.start) || s.Today().Month().Before(m) {
		return Entry{}, ErrInvalid
	}
	validTarget := (n.Type == domain.Payout && n.To == domain.Person(uid)) || (n.Type == domain.Deposit && n.From == domain.Person(uid))
	if !validTarget {
		return Entry{}, ErrInvalid
	}
	return s.recordMovement(ctx, actor, n, func(t tx) error {
		suggestion, err := s.settlementSuggestion(t, uid, m)
		if err != nil {
			return err
		}
		if suggestion.Revision != revision {
			return ErrVersionConflict
		}
		action := domain.SuggestAction(suggestion.Balance)
		if (n.Type == domain.Payout && action.Kind != domain.SalonPays) || (n.Type == domain.Deposit && action.Kind != domain.PersonPays) {
			return ErrVersionConflict
		}
		if n.Amount > action.Amount {
			return &domain.ValidationError{Field: "amount", Problem: ProblemSettlementAmount}
		}
		return nil
	})
}
