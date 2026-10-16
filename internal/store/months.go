package store

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/lieranderl/aestheticlab-income-app/internal/domain"
)

// MonthState is a month's status and, once closed, its snapshot.
type MonthState struct {
	Month    domain.Month
	Status   domain.MonthStatus
	ClosedBy string
	ClosedAt time.Time
	Snapshot *Snapshot // nil while open
}

// Snapshot is what closing a month froze: its result, the closing balances
// as of its last day (its own rows only) and the terms used.
type Snapshot struct {
	Result   domain.MonthResult
	Balances domain.Balances
	Terms    map[string]domain.Terms
}

// The snapshot is stored with explicit field names, so renaming a domain
// field can never make a stored snapshot read back as zero.
type monthRecord struct {
	Revision   int64           `firestore:"revision" json:"revision"`
	Status     string          `firestore:"status"     json:"status"`
	ClosedBy   string          `firestore:"closedBy"   json:"closedBy"`
	ClosedAt   time.Time       `firestore:"closedAt"   json:"closedAt"`
	ReopenedBy string          `firestore:"reopenedBy" json:"reopenedBy"`
	ReopenedAt time.Time       `firestore:"reopenedAt" json:"reopenedAt"`
	Snapshot   *snapshotRecord `firestore:"snapshot"   json:"snapshot"`
}

type snapshotRecord struct {
	Result   resultRecord                `firestore:"result"   json:"result"`
	Balances balancesRecord              `firestore:"balances" json:"balances"`
	Terms    map[string]termsValueRecord `firestore:"terms"    json:"terms"`
}

type resultRecord struct {
	StatsVersion       int                           `firestore:"statsVersion" json:"statsVersion"`
	KassaCash          domain.Cents                  `firestore:"kassaCash" json:"kassaCash"`
	KassaCard          domain.Cents                  `firestore:"kassaCard" json:"kassaCard"`
	Clients            int                           `firestore:"clients" json:"clients"`
	Revenue            domain.Cents                  `firestore:"revenue"            json:"revenue"`
	RevenueByCategory  map[string]domain.Cents       `firestore:"revenueByCategory"  json:"revenueByCategory"`
	Self               domain.Cents                  `firestore:"self"               json:"self"`
	CertificateSales   domain.Cents                  `firestore:"certificateSales"   json:"certificateSales"`
	SalonIncome        domain.Cents                  `firestore:"salonIncome"        json:"salonIncome"`
	Expenses           domain.Cents                  `firestore:"expenses"           json:"expenses"`
	ExpensesByCategory map[string]domain.Cents       `firestore:"expensesByCategory" json:"expensesByCategory"`
	Salaries           domain.Cents                  `firestore:"salaries"           json:"salaries"`
	SelfBonuses        domain.Cents                  `firestore:"selfBonuses"        json:"selfBonuses"`
	Profit             domain.Cents                  `firestore:"profit"             json:"profit"`
	DividendPool       domain.Cents                  `firestore:"dividendPool"       json:"dividendPool"`
	WorkingDays        int                           `firestore:"workingDays"        json:"workingDays"`
	People             map[string]personResultRecord `firestore:"people"             json:"people"`
}

type personResultRecord struct {
	StatsVersion      int                     `firestore:"statsVersion" json:"statsVersion"`
	RevenueByCategory map[string]domain.Cents `firestore:"revenueByCategory" json:"revenueByCategory"`
	KassaCash         domain.Cents            `firestore:"kassaCash" json:"kassaCash"`
	KassaCard         domain.Cents            `firestore:"kassaCard" json:"kassaCard"`
	Clients           int                     `firestore:"clients" json:"clients"`
	Revenue           domain.Cents            `firestore:"revenue"     json:"revenue"`
	Self              domain.Cents            `firestore:"self"        json:"self"`
	SelfBase          domain.Cents            `firestore:"selfBase"    json:"selfBase"`
	SelfBonus         domain.Cents            `firestore:"selfBonus"   json:"selfBonus"`
	Salary            domain.Cents            `firestore:"salary"      json:"salary"`
	Dividend          domain.Cents            `firestore:"dividend"    json:"dividend"`
	Earned            domain.Cents            `firestore:"earned"      json:"earned"`
	WorkingDays       int                     `firestore:"workingDays" json:"workingDays"`
	Opening           domain.Cents            `firestore:"opening"     json:"opening"`
	Closing           domain.Cents            `firestore:"closing"     json:"closing"`
}

type balancesRecord struct {
	Accounts                map[string]domain.Cents `firestore:"accounts"                json:"accounts"`
	Persons                 map[string]domain.Cents `firestore:"persons"                 json:"persons"`
	CertificatesOutstanding domain.Cents            `firestore:"certificatesOutstanding" json:"certificatesOutstanding"`
}

type termsValueRecord struct {
	SelfPct           domain.BasisPoints `firestore:"selfPct"           json:"selfPct"`
	SalaryCents       domain.Cents       `firestore:"salaryCents"       json:"salaryCents"`
	DividendWeight    int64              `firestore:"dividendWeight"    json:"dividendWeight"`
	HighSelfFromCents domain.Cents       `firestore:"highSelfFromCents" json:"highSelfFromCents"`
	HighSelfPct       domain.BasisPoints `firestore:"highSelfPct"       json:"highSelfPct"`
}

func snapshotRecordOf(s Snapshot) *snapshotRecord {
	r := s.Result
	rec := &snapshotRecord{
		Result: resultRecord{StatsVersion: r.StatsVersion, KassaCash: r.KassaCash, KassaCard: r.KassaCard, Clients: r.Clients,
			Revenue: r.Revenue, RevenueByCategory: nonNil(r.RevenueByCategory), Self: r.Self,
			CertificateSales: r.CertificateSales, SalonIncome: r.SalonIncome, Expenses: r.Expenses,
			ExpensesByCategory: nonNil(r.ExpensesByCategory), Salaries: r.Salaries, SelfBonuses: r.SelfBonuses, Profit: r.Profit,
			DividendPool: r.DividendPool, WorkingDays: r.WorkingDays, People: map[string]personResultRecord{},
		},
		Balances: balancesRecord{
			Accounts: nonNil(s.Balances.Accounts), Persons: nonNil(s.Balances.Persons),
			CertificatesOutstanding: s.Balances.CertificatesOutstanding,
		},
		Terms: map[string]termsValueRecord{},
	}
	for id, p := range r.People {
		rec.Result.People[id] = personResultRecord(p)
	}
	for id, t := range s.Terms {
		rec.Terms[id] = termsValueRecord(t)
	}
	return rec
}

func (rec *snapshotRecord) snapshot(m domain.Month) *Snapshot {
	if rec == nil {
		return nil
	}
	r := rec.Result
	s := &Snapshot{
		Result: domain.MonthResult{StatsVersion: r.StatsVersion, KassaCash: r.KassaCash, KassaCard: r.KassaCard, Clients: r.Clients,
			Month: m, Revenue: r.Revenue, RevenueByCategory: nonNil(r.RevenueByCategory), Self: r.Self,
			CertificateSales: r.CertificateSales, SalonIncome: r.SalonIncome, Expenses: r.Expenses,
			ExpensesByCategory: nonNil(r.ExpensesByCategory), Salaries: r.Salaries, SelfBonuses: r.SelfBonuses, Profit: r.Profit,
			DividendPool: r.DividendPool, WorkingDays: r.WorkingDays, People: map[string]domain.PersonResult{},
		},
		Balances: domain.Balances{
			Accounts: nonNil(rec.Balances.Accounts), Persons: nonNil(rec.Balances.Persons),
			CertificatesOutstanding: rec.Balances.CertificatesOutstanding,
		},
		Terms: map[string]domain.Terms{},
	}
	for id, p := range r.People {
		s.Result.People[id] = domain.PersonResult(p)
	}
	for id, t := range rec.Terms {
		s.Terms[id] = domain.Terms(t)
	}
	return s
}

func (rec monthRecord) state(m domain.Month) MonthState {
	st := MonthState{Month: m, Status: domain.MonthOpen}
	if rec.Status == string(domain.MonthClosed) {
		st.Status, st.ClosedBy, st.ClosedAt = domain.MonthClosed, rec.ClosedBy, rec.ClosedAt.UTC()
		st.Snapshot = rec.Snapshot.snapshot(m)
	}
	return st
}

// readMonths reads every months/{YYYY-MM} doc; there is one a month at most.
func readMonths(t tx) (map[domain.Month]monthRecord, error) {
	out := map[domain.Month]monthRecord{}
	err := t.query("months", nil, func(id string, decode func(any) error) error {
		m, err := domain.ParseMonth(id)
		if err != nil {
			return fmt.Errorf("months/%s: %w", id, err)
		}
		var rec monthRecord
		if err := decode(&rec); err != nil {
			return err
		}
		out[m] = rec
		return nil
	})
	return out, err
}

func readMonthStatus(t tx, m domain.Month) (domain.MonthStatus, error) {
	var rec monthRecord
	if _, err := t.get(monthPath(m), &rec); err != nil {
		return "", err
	}
	return rec.state(m).Status, nil
}

// requireOpen rejects any write dated in a closed month, the admin's too.
func requireOpen(t tx, m domain.Month) error {
	status, err := readMonthStatus(t, m)
	if err != nil {
		return err
	}
	if status == domain.MonthClosed {
		return ErrMonthClosed
	}
	return nil
}

// snapshotOf is a closed month's snapshot; a closed month always has one.
func snapshotOf(months map[domain.Month]monthRecord, m domain.Month) (*Snapshot, error) {
	snap := months[m].state(m).Snapshot
	if snap == nil {
		return nil, fmt.Errorf("store: month %s has no closed snapshot", m)
	}
	return snap, nil
}

// existing is the month's record for the audit log, nil if it had none.
func existing(months map[domain.Month]monthRecord, m domain.Month) any {
	if rec, ok := months[m]; ok {
		return rec
	}
	return nil
}

func newestClosed(months map[domain.Month]monthRecord) *domain.Month {
	var newest *domain.Month
	for m, rec := range months {
		if rec.Status == string(domain.MonthClosed) && (newest == nil || newest.Before(m)) {
			newest = &m
		}
	}
	return newest
}

// Month is a month's state; a month without a document is open.
func (s *Store) Month(ctx context.Context, m domain.Month) (MonthState, error) {
	var out MonthState
	err := s.db.run(ctx, true, func(t tx) error {
		var rec monthRecord
		if _, err := t.get(monthPath(m), &rec); err != nil {
			return err
		}
		out = rec.state(m)
		return nil
	})
	return out, err
}

// ClosedMonths are the closed months, oldest first.
func (s *Store) ClosedMonths(ctx context.Context) ([]MonthState, error) {
	var out []MonthState
	err := s.db.run(ctx, true, func(t tx) error {
		months, err := readMonths(t)
		if err != nil {
			return err
		}
		out = out[:0]
		for m, rec := range months {
			if st := rec.state(m); st.Status == domain.MonthClosed {
				out = append(out, st)
			}
		}
		return nil
	})
	slices.SortFunc(out, func(a, b MonthState) int { return cmp.Compare(a.Month.String(), b.Month.String()) })
	return out, err
}

// Balances are the kassa accounts, every person's balance and the
// outstanding certificates now: the newest closed month's snapshot plus the
// open months since (system-design §4.1).
func (s *Store) Balances(ctx context.Context) (domain.Balances, error) {
	var out domain.Balances
	err := s.db.run(ctx, true, func(t tx) error {
		var err error
		out, err = s.balances(t)
		return err
	})
	return out, err
}

func (s *Store) balances(t tx) (domain.Balances, error) {
	months, err := readMonths(t)
	if err != nil {
		return domain.Balances{}, err
	}
	start, from := domain.Balances{}, s.start
	if newest := newestClosed(months); newest != nil {
		snap, err := snapshotOf(months, *newest)
		if err != nil {
			return domain.Balances{}, err
		}
		start, from = snap.Balances, newest.Next()
	}
	reports, err := readReports(t, where("month", ">=", from.String()))
	if err != nil {
		return domain.Balances{}, err
	}
	ledger, err := readLedger(t, where("month", ">=", from.String()))
	if err != nil {
		return domain.Balances{}, err
	}
	return domain.Fold(start, personReports(reports), movements(ledger))
}

// CloseMonth settles month m (admin only, system-design §3.4): only once m
// is over, in order, with nothing pending; the terms in force for m give salary,
// «Себе» bonuses and dividends, accrued on m's last day; the snapshot holds m's rows only.
func (s *Store) CloseMonth(ctx context.Context, actor Actor, m domain.Month) (MonthState, error) {
	return s.closeMonth(ctx, actor, m, "")
}

// CloseMonthGuarded rejects a stale settlement confirmation inside the write transaction.
func (s *Store) CloseMonthGuarded(ctx context.Context, actor Actor, m domain.Month, revision string) (MonthState, error) {
	if revision == "" {
		return MonthState{}, ErrVersionConflict
	}
	return s.closeMonth(ctx, actor, m, revision)
}

func (s *Store) closeMonth(ctx context.Context, actor Actor, m domain.Month, revision string) (MonthState, error) {
	if s.financialWritesPaused {
		return MonthState{}, ErrFinancialWritesPaused
	}
	if !actor.Admin {
		return MonthState{}, ErrForbidden
	}
	if !m.Before(s.Today().Month()) {
		return MonthState{}, &domain.ValidationError{Field: "month", Problem: ProblemMonthNotOver}
	}
	var out MonthState
	err := s.db.run(ctx, false, func(t tx) error {
		if revision != "" {
			selected, err := s.settlement(t, m)
			if err != nil {
				return err
			}
			if selected.Revision != revision {
				return ErrVersionConflict
			}
		}
		months, err := readMonths(t)
		if err != nil {
			return err
		}
		if months[m].Status == string(domain.MonthClosed) {
			return ErrMonthClosed
		}
		ledger, err := readLedger(t, where("month", "==", m.String()))
		if err != nil {
			return err
		}
		pending := 0
		for _, e := range ledger {
			if e.Status == domain.Pending {
				pending++
			}
		}
		prevClosed := months[m.Prev()].Status == string(domain.MonthClosed)
		if err := domain.CheckCanClose(m, s.start, prevClosed, pending); err != nil {
			return err
		}
		opening := domain.Balances{}
		if m != s.start {
			snap, err := snapshotOf(months, m.Prev())
			if err != nil {
				return err
			}
			opening = snap.Balances
		}
		reports, err := readReports(t, where("month", "==", m.String()))
		if err != nil {
			return err
		}
		users, err := readUsers(t)
		if err != nil {
			return err
		}
		terms := map[string]domain.TermsHistory{}
		for _, u := range users {
			h, err := readTermsHistory(t, u.ID)
			if err != nil {
				return err
			}
			if _, ok := h.For(m); ok {
				terms[u.ID] = h
			}
		}
		closing, err := domain.CloseMonth(domain.MonthInput{
			Month: m, Opening: opening, Reports: personReports(reports), Movements: movements(ledger), Terms: terms,
		})
		if err != nil {
			return err
		}

		now := s.stamp()
		for _, a := range closing.Accruals {
			e := Entry{ID: newID(), Movement: a, PersonID: personOf(a), CreatedBy: actor.UID, ConfirmedBy: actor.UID, CreatedAt: now, Version: 1}
			if err := s.writeEntry(t, actor.UID, ActionCreate, nil, e); err != nil {
				return err
			}
		}
		snap := Snapshot{Result: closing.Result, Balances: closing.Balances, Terms: closing.Terms}
		rec := monthRecord{
			Revision: months[m].Revision + 1, Status: string(domain.MonthClosed), ClosedBy: actor.UID, ClosedAt: now,
			ReopenedBy: months[m].ReopenedBy, ReopenedAt: months[m].ReopenedAt, Snapshot: snapshotRecordOf(snap),
		}
		if err := t.set(monthPath(m), rec); err != nil {
			return err
		}
		out = rec.state(m)
		return s.audit(t, EntityMonth, m.String(), ActionClose, actor.UID, existing(months, m), rec)
	})
	return out, err
}

// ReopenMonth opens the newest closed month again (admin only,
// system-design §3.5): its accruals are voided and its snapshot dropped.
func (s *Store) ReopenMonth(ctx context.Context, actor Actor, m domain.Month) (MonthState, error) {
	return s.reopenMonth(ctx, actor, m, "")
}

// ReopenMonthGuarded rejects a stale confirmation, including a close/reopen cycle.
func (s *Store) ReopenMonthGuarded(ctx context.Context, actor Actor, m domain.Month, revision string) (MonthState, error) {
	if revision == "" {
		return MonthState{}, ErrVersionConflict
	}
	return s.reopenMonth(ctx, actor, m, revision)
}

func (s *Store) reopenMonth(ctx context.Context, actor Actor, m domain.Month, revision string) (MonthState, error) {
	if s.financialWritesPaused {
		return MonthState{}, ErrFinancialWritesPaused
	}
	if !actor.Admin {
		return MonthState{}, ErrForbidden
	}
	var out MonthState
	err := s.db.run(ctx, false, func(t tx) error {
		if revision != "" {
			selected, err := s.settlement(t, m)
			if err != nil {
				return err
			}
			if selected.Revision != revision {
				return ErrVersionConflict
			}
		}
		months, err := readMonths(t)
		if err != nil {
			return err
		}
		if err := domain.CheckCanReopen(m, newestClosed(months)); err != nil {
			return err
		}
		ledger, err := readLedger(t, where("month", "==", m.String()))
		if err != nil {
			return err
		}
		for _, before := range ledger {
			if !domain.IsAccrual(before.Type) || before.Status != domain.Confirmed {
				continue
			}
			e := before
			e.Status, e.VoidReason, e.Version = domain.Voided, ReasonMonthReopened, before.Version+1
			if err := s.writeEntry(t, actor.UID, ActionVoid, &before, e); err != nil {
				return err
			}
		}
		rec := monthRecord{Revision: months[m].Revision + 1, Status: string(domain.MonthOpen), ReopenedBy: actor.UID, ReopenedAt: s.stamp()}
		if err := t.set(monthPath(m), rec); err != nil {
			return err
		}
		out = rec.state(m)
		return s.audit(t, EntityMonth, m.String(), ActionReopen, actor.UID, existing(months, m), rec)
	})
	return out, err
}

// RecountResult is the outcome of «Пересчёт кассы».
type RecountResult struct {
	Expected   domain.Cents
	Counted    domain.Cents
	Adjustment *Entry // nil when the count matched
}

// Recount compares what the admin counted (or the card's bank balance) with
// the expected balance of the account; a difference is booked as an
// adjustment, which needs a note (system-design §3.3).
func (s *Store) Recount(ctx context.Context, actor Actor, accountID string, counted domain.Cents, note string) (RecountResult, error) {
	if s.financialWritesPaused {
		return RecountResult{}, ErrFinancialWritesPaused
	}
	if !actor.Admin {
		return RecountResult{}, ErrForbidden
	}
	if err := checkID("account", accountID); err != nil {
		return RecountResult{}, ErrNotFound
	}
	if counted < 0 {
		return RecountResult{}, &domain.ValidationError{Field: "counted", Problem: domain.ProblemNegativeAmount}
	}
	today := s.Today()
	if err := domain.CheckDate(today, today, s.start); err != nil {
		return RecountResult{}, err
	}
	var out RecountResult
	err := s.db.run(ctx, false, func(t tx) error {
		if err := requireOpen(t, today.Month()); err != nil {
			return err
		}
		var acc accountRecord
		found, err := t.get(accountPath(accountID), &acc)
		if err != nil {
			return err
		}
		if !found || !acc.Active {
			return ErrNotFound
		}
		bal, err := s.balances(t)
		if err != nil {
			return err
		}
		out = RecountResult{Expected: bal.Accounts[accountID], Counted: counted}
		diff := counted - out.Expected
		if diff != 0 && trimmed(note) == "" {
			return &domain.ValidationError{Field: "note", Problem: ProblemNoteRequired}
		}
		now := s.stamp()
		if diff != 0 {
			e := Entry{
				ID: newID(), Note: trimmed(note), CreatedBy: actor.UID, ConfirmedBy: actor.UID, CreatedAt: now, Version: 1,
				Movement: domain.Movement{
					Type: domain.Adjustment, Status: domain.Confirmed, Amount: diff,
					From: domain.Outside, To: domain.Account(accountID), Date: today,
				},
			}
			if diff < 0 {
				e.Amount, e.From, e.To = -diff, domain.Account(accountID), domain.Outside
			}
			if err := s.writeEntry(t, actor.UID, ActionCreate, nil, e); err != nil {
				return err
			}
			out.Adjustment = &e
		}
		after := acc
		after.LastRecountAt, after.LastRecountBy = now, actor.UID
		if err := t.set(accountPath(accountID), after); err != nil {
			return err
		}
		return s.audit(t, EntityAccount, accountID, ActionRecount, actor.UID, acc, map[string]any{
			"account": after, "expected": out.Expected, "counted": counted,
		})
	})
	return out, err
}

// ImportedMonths are the months of history from the Google Sheet.
func (s *Store) ImportedMonths(ctx context.Context) (map[domain.Month]domain.ImportedMonth, error) {
	out := map[domain.Month]domain.ImportedMonth{}
	err := s.db.run(ctx, true, func(t tx) error {
		clear(out)
		return t.query("importedMonths", nil, func(id string, decode func(any) error) error {
			m, err := domain.ParseMonth(id)
			if err != nil {
				return fmt.Errorf("importedMonths/%s: %w", id, err)
			}
			var rec importedRecord
			if err := decode(&rec); err != nil {
				return err
			}
			out[m] = rec.imported()
			return nil
		})
	})
	return out, err
}

type importedRecord struct {
	RevenueByPerson map[string]domain.Cents `firestore:"revenueByPerson" json:"revenueByPerson"`
	RevenueTotal    domain.Cents            `firestore:"revenueTotal"    json:"revenueTotal"`
	SalonIncome     domain.Cents            `firestore:"salonIncome"     json:"salonIncome"`
	Expenses        domain.Cents            `firestore:"expenses"        json:"expenses"`
	ManagerSalary   domain.Cents            `firestore:"managerSalary"   json:"managerSalary"`
	Profit          domain.Cents            `firestore:"profit"          json:"profit"`
	Dividends       domain.Cents            `firestore:"dividends"       json:"dividends"`
	Source          string                  `firestore:"source"          json:"source"`
}

func (r importedRecord) imported() domain.ImportedMonth {
	return domain.ImportedMonth{
		RevenueByPerson: nonNil(r.RevenueByPerson), RevenueTotal: r.RevenueTotal, SalonIncome: r.SalonIncome,
		Expenses: r.Expenses, ManagerSalary: r.ManagerSalary, Profit: r.Profit, Dividends: r.Dividends,
	}
}

// SaveImportedMonth stores a month of sheet history (admin only, cmd/import).
// Only months before the start month: history never overlaps app data.
func (s *Store) SaveImportedMonth(ctx context.Context, actor Actor, m domain.Month, im domain.ImportedMonth, source string) error {
	return s.SaveImportedMonths(ctx, actor, map[domain.Month]domain.ImportedMonth{m: im}, source)
}

// SaveImportedMonths stores months of sheet history in one transaction:
// all of them or, if any is refused, none. Each month gets its own audit row.
func (s *Store) SaveImportedMonths(ctx context.Context, actor Actor, months map[domain.Month]domain.ImportedMonth, source string) error {
	if !actor.Admin {
		return ErrForbidden
	}
	order := slices.SortedFunc(maps.Keys(months), func(a, b domain.Month) int { return cmp.Compare(a.String(), b.String()) })
	for _, m := range order {
		if !m.Before(s.start) {
			return &domain.ValidationError{Field: "month", Problem: ProblemNotHistory}
		}
	}
	return s.db.run(ctx, false, func(t tx) error {
		before := make([]any, len(order))
		for i, m := range order { // every read before any write
			var rec importedRecord
			existed, err := t.get(importedPath(m), &rec)
			if err != nil {
				return err
			}
			if existed {
				before[i] = rec
			}
		}
		for i, m := range order {
			im := months[m]
			rec := importedRecord{
				RevenueByPerson: maps.Clone(nonNil(im.RevenueByPerson)), RevenueTotal: im.RevenueTotal,
				SalonIncome: im.SalonIncome, Expenses: im.Expenses, ManagerSalary: im.ManagerSalary,
				Profit: im.Profit, Dividends: im.Dividends, Source: trimmed(source),
			}
			if err := t.set(importedPath(m), rec); err != nil {
				return err
			}
			if err := s.audit(t, EntityImported, m.String(), ActionUpdate, actor.UID, before[i], rec); err != nil {
				return err
			}
		}
		return nil
	})
}

// Reasons a month operation is rejected, beyond the domain's.
const (
	ProblemNotHistory   domain.Problem = "not_history"    // imported months come before the start month
	ProblemMonthNotOver domain.Problem = "month_not_over" // a month closes once it has ended
)
