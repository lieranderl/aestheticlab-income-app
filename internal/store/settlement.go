package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"

	"github.com/lieranderl/aestheticlab-income-app/internal/domain"
)

// SettlementMonth contains selected-month figures, never later movements.
type SettlementMonth struct {
	State                        MonthState
	Result                       domain.MonthResult
	Breakdowns                   map[string]domain.SettlementBreakdown
	Revision                     string
	Pending                      int
	PreviousClosed, NewestClosed bool
	PreviewError                 error
}

func (s *Store) Settlement(ctx context.Context, m domain.Month) (SettlementMonth, error) {
	if m.Before(s.start) || s.Today().Month().Before(m) {
		return SettlementMonth{}, ErrInvalid
	}
	var out SettlementMonth
	err := s.db.run(ctx, true, func(t tx) error {
		var err error
		out, err = s.settlement(t, m)
		return err
	})
	return out, err
}

func (s *Store) settlement(t tx, m domain.Month) (SettlementMonth, error) {
	months, err := readMonths(t)
	if err != nil {
		return SettlementMonth{}, err
	}
	reports, err := readReports(t, where("month", "==", m.String()))
	if err != nil {
		return SettlementMonth{}, err
	}
	ledger, err := readLedger(t, where("month", "==", m.String()))
	if err != nil {
		return SettlementMonth{}, err
	}
	users, err := readUsers(t)
	if err != nil {
		return SettlementMonth{}, err
	}
	terms := map[string]domain.TermsHistory{}
	if frozen := months[m].state(m).Snapshot; frozen != nil {
		for uid, term := range frozen.Terms {
			terms[uid] = domain.TermsHistory{m: term}
		}
	} else {
		for _, u := range users {
			h, e := readTermsHistory(t, u.ID)
			if e != nil {
				return SettlementMonth{}, e
			}
			if _, ok := h.For(m); ok {
				terms[u.ID] = h
			}
		}
	}
	opening := domain.Balances{}
	from := s.start
	var earlier *domain.Month
	for month, rec := range months {
		if month.Before(m) && rec.Status == string(domain.MonthClosed) && (earlier == nil || earlier.Before(month)) {
			copyMonth := month
			earlier = &copyMonth
		}
	}
	if earlier != nil {
		snap, e := snapshotOf(months, *earlier)
		if e != nil {
			return SettlementMonth{}, e
		}
		opening = snap.Balances
		from = earlier.Next()
	}
	if from.Before(m) {
		rs, e := readReports(t, where("month", ">=", from.String()), where("month", "<=", m.Prev().String()))
		if e != nil {
			return SettlementMonth{}, e
		}
		es, e := readLedger(t, where("month", ">=", from.String()), where("month", "<=", m.Prev().String()))
		if e != nil {
			return SettlementMonth{}, e
		}
		opening, e = domain.Fold(opening, personReports(rs), movements(es))
		if e != nil {
			return SettlementMonth{}, e
		}
	}
	out := SettlementMonth{State: months[m].state(m), Breakdowns: map[string]domain.SettlementBreakdown{}, PreviousClosed: m == s.start || months[m.Prev()].Status == string(domain.MonthClosed)}
	newest := newestClosed(months)
	out.NewestClosed = newest != nil && *newest == m
	for _, e := range ledger {
		if e.Status == domain.Pending {
			out.Pending++
		}
	}
	moves := movements(ledger)
	moves = slices.DeleteFunc(moves, func(e domain.Movement) bool { return domain.IsAccrual(e.Type) })
	in := domain.MonthInput{Month: m, Opening: opening, Reports: personReports(reports), Movements: moves, Terms: terms}
	if out.State.Snapshot != nil {
		out.Result = out.State.Snapshot.Result
	} else {
		out.Result, out.PreviewError = domain.PreviewMonth(in)
	}
	for id, p := range out.Result.People {
		out.Breakdowns[id] = domain.BreakdownFor(id, opening.Persons[id], in.Reports, moves, p)
	}
	// JSON encodes map keys in order; query slices must also have a stable order.
	slices.SortFunc(reports, func(a, b SavedReport) int { return compareReport(a, b) })
	slices.SortFunc(ledger, func(a, b Entry) int {
		if a.ID < b.ID {
			return -1
		}
		if a.ID > b.ID {
			return 1
		}
		return 0
	})
	effective := map[string]domain.Terms{}
	for id, h := range terms {
		effective[id], _ = h.For(m)
	}
	monthKeys := map[string]monthRecord{}
	for key, rec := range months {
		monthKeys[key.String()] = rec
	}
	bytes, e := json.Marshal(struct {
		Month   string
		Months  map[string]monthRecord
		Reports []SavedReport
		Ledger  []Entry
		Terms   map[string]domain.Terms
		Opening domain.Balances
	}{m.String(), monthKeys, reports, ledger, effective, opening})
	if e != nil {
		return SettlementMonth{}, e
	}
	hash := sha256.Sum256(bytes)
	out.Revision = hex.EncodeToString(hash[:])
	return out, nil
}

func compareReport(a, b SavedReport) int {
	if a.Person < b.Person {
		return -1
	}
	if a.Person > b.Person {
		return 1
	}
	if a.Date.Before(b.Date) {
		return -1
	}
	if b.Date.Before(a.Date) {
		return 1
	}
	if a.ID < b.ID {
		return -1
	}
	if a.ID > b.ID {
		return 1
	}
	return 0
}

// OwnMonthResult returns only the caller-selected person's frozen result and status.
func (s *Store) OwnMonthResult(ctx context.Context, uid string, m domain.Month) (domain.PersonResult, domain.MonthStatus, error) {
	if err := checkID("user", uid); err != nil {
		return domain.PersonResult{}, "", err
	}
	var out domain.PersonResult
	status := domain.MonthOpen
	err := s.db.run(ctx, true, func(t tx) error {
		var rec monthRecord
		if _, err := t.get(monthPath(m), &rec); err != nil {
			return err
		}
		if rec.Status == string(domain.MonthClosed) {
			status = domain.MonthClosed
		}
		if rec.Snapshot != nil && status == domain.MonthClosed {
			out = domain.PersonResult(rec.Snapshot.Result.People[uid])
		}
		return nil
	})
	return out, status, err
}
