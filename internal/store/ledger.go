package store

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/lieranderl/aestheticlab-income-app/internal/domain"
)

// Reasons the store rejects a ledger write, beyond the domain's.
const (
	ProblemOpeningDate   domain.Problem = "opening_date"   // opening rows are dated the start month's first day
	ProblemNotPending    domain.Problem = "not_pending"    // only a pending row can be confirmed
	ProblemAlreadyVoided domain.Problem = "already_voided" // a voided row stays voided
	ProblemNoteRequired  domain.Problem = "note_required"  // expenses, adjustments and voids need a note
	ProblemUnknownParty  domain.Problem = "unknown_party"  // the account, person or expense category doesn't exist
)

// ReasonMonthReopened is the void reason of accruals when a month reopens.
const ReasonMonthReopened = "месяц открыт заново"

// NewMovement is a movement of money as someone records it.
type NewMovement struct {
	Type            domain.MovementType
	Amount          domain.Cents
	From, To        domain.Party
	ExpenseCategory string
	Date            domain.Date
	Note            string
}

// Entry is a stored ledger row.
type Entry struct {
	ID string
	domain.Movement
	Note        string
	VoidReason  string
	PersonID    string // the person on either side, if any
	CreatedBy   string
	ConfirmedBy string
	CreatedAt   time.Time
	Version     int64
}

type ledgerRecord struct {
	Date              string                `firestore:"date"              json:"date"`
	Month             string                `firestore:"month"             json:"month"`
	Type              domain.MovementType   `firestore:"type"              json:"type"`
	Amount            domain.Cents          `firestore:"amount"            json:"amount"`
	From              string                `firestore:"from"              json:"from"`
	To                string                `firestore:"to"                json:"to"`
	PersonID          string                `firestore:"personId"          json:"personId"`
	ExpenseCategoryID string                `firestore:"expenseCategoryId" json:"expenseCategoryId"`
	Note              string                `firestore:"note"              json:"note"`
	Status            domain.MovementStatus `firestore:"status"            json:"status"`
	VoidReason        string                `firestore:"voidReason"        json:"voidReason"`
	CreatedBy         string                `firestore:"createdBy"         json:"createdBy"`
	ConfirmedBy       string                `firestore:"confirmedBy"       json:"confirmedBy"`
	CreatedAt         time.Time             `firestore:"createdAt"         json:"createdAt"`
	Version           int64                 `firestore:"version"           json:"version"`
}

// partyString writes a party as "account:<id>", "person:<uid>", "outside",
// "salon" or "certificates".
func partyString(p domain.Party) string {
	if p.ID == "" {
		return string(p.Kind)
	}
	return string(p.Kind) + ":" + p.ID
}

func parseParty(s string) domain.Party {
	kind, id, _ := strings.Cut(s, ":")
	return domain.Party{Kind: domain.PartyKind(kind), ID: id}
}

func personOf(m domain.Movement) string {
	switch {
	case m.From.Kind == domain.PartyPerson:
		return m.From.ID
	case m.To.Kind == domain.PartyPerson:
		return m.To.ID
	}
	return ""
}

func ledgerRecordOf(e Entry) ledgerRecord {
	return ledgerRecord{
		Date: e.Date.String(), Month: e.Date.Month().String(), Type: e.Type, Amount: e.Amount,
		From: partyString(e.From), To: partyString(e.To), PersonID: personOf(e.Movement),
		ExpenseCategoryID: e.ExpenseCategory, Note: e.Note, Status: e.Status, VoidReason: e.VoidReason,
		CreatedBy: e.CreatedBy, ConfirmedBy: e.ConfirmedBy, CreatedAt: e.CreatedAt.UTC(), Version: e.Version,
	}
}

func (r ledgerRecord) entry(id string) (Entry, error) {
	day, err := domain.ParseDate(r.Date)
	if err != nil {
		return Entry{}, fmt.Errorf("ledger %s: %w", id, err)
	}
	return Entry{
		ID: id,
		Movement: domain.Movement{
			Type: r.Type, Status: r.Status, Amount: r.Amount, From: parseParty(r.From), To: parseParty(r.To),
			ExpenseCategory: r.ExpenseCategoryID, Date: day,
		},
		Note: r.Note, VoidReason: r.VoidReason, PersonID: r.PersonID, CreatedBy: r.CreatedBy,
		ConfirmedBy: r.ConfirmedBy, CreatedAt: r.CreatedAt.UTC(), Version: r.Version,
	}, nil
}

func movements(es []Entry) []domain.Movement {
	out := make([]domain.Movement, len(es))
	for i, e := range es {
		out[i] = e.Movement
	}
	return out
}

func readLedger(t tx, conds ...cond) ([]Entry, error) {
	var out []Entry
	err := t.query("ledger", conds, func(id string, decode func(any) error) error {
		var rec ledgerRecord
		if err := decode(&rec); err != nil {
			return err
		}
		e, err := rec.entry(id)
		if err != nil {
			return err
		}
		out = append(out, e)
		return nil
	})
	slices.SortFunc(out, func(a, b Entry) int {
		return cmp.Or(cmp.Compare(a.Date.String(), b.Date.String()), a.CreatedAt.Compare(b.CreatedAt), cmp.Compare(a.ID, b.ID))
	})
	return out, err
}

func readEntry(t tx, id string) (Entry, error) {
	if err := checkID("ledger", id); err != nil {
		return Entry{}, ErrNotFound
	}
	var rec ledgerRecord
	found, err := t.get(ledgerPath(id), &rec)
	if err != nil {
		return Entry{}, err
	}
	if !found {
		return Entry{}, ErrNotFound
	}
	return rec.entry(id)
}

// writeEntry stores a ledger row and its audit row.
func (s *Store) writeEntry(t tx, actorID, action string, before *Entry, e Entry) error {
	rec := ledgerRecordOf(e)
	if err := t.set(ledgerPath(e.ID), rec); err != nil {
		return err
	}
	var prev any
	if before != nil {
		prev = ledgerRecordOf(*before)
	}
	return s.audit(t, EntityLedger, e.ID, action, actorID, prev, rec)
}

// RecordMovement adds a ledger row (system-design §3.2). The admin records
// any type except accruals, confirmed at once. Anyone else records only a
// deposit or an expense they paid themselves, pending until the admin
// confirms it. The row's month must be open and its date between the
// start month and today; opening rows are dated the start month's first day.
func (s *Store) RecordMovement(ctx context.Context, actor Actor, n NewMovement) (Entry, error) {
	return s.recordMovement(ctx, actor, n, nil)
}

func (s *Store) recordMovement(ctx context.Context, actor Actor, n NewMovement, guard func(tx) error) (Entry, error) {
	if s.financialWritesPaused {
		return Entry{}, ErrFinancialWritesPaused
	}
	e, err := s.newEntry(actor, n)
	if err != nil {
		return Entry{}, err
	}
	err = s.db.run(ctx, false, func(t tx) error {
		if err := requireOpen(t, n.Date.Month()); err != nil {
			return err
		}
		if guard != nil {
			if err := guard(t); err != nil {
				return err
			}
		}
		category, err := checkParties(t, e.Movement)
		if err != nil {
			return err
		}
		if category != nil {
			before := *category
			category.UsedCount++
			if err := s.writeCategory(t, actor.UID, before, *category); err != nil {
				return err
			}
		}
		return s.writeEntry(t, actor.UID, ActionCreate, nil, e)
	})
	return e, err
}

// newEntry checks a movement as someone records it, before any read: who
// may record it, its date, note and shape.
func (s *Store) newEntry(actor Actor, n NewMovement) (Entry, error) {
	e := Entry{
		ID: newID(),
		Movement: domain.Movement{
			Type: n.Type, Amount: n.Amount, From: n.From, To: n.To,
			ExpenseCategory: n.ExpenseCategory, Date: n.Date, Status: domain.Confirmed,
		},
		Note: strings.TrimSpace(n.Note), CreatedBy: actor.UID, CreatedAt: s.stamp(), Version: 1,
	}
	e.PersonID = personOf(e.Movement)
	switch {
	case domain.IsAccrual(n.Type):
		return Entry{}, ErrForbidden // only closing a month accrues them
	case actor.Admin:
		e.ConfirmedBy = actor.UID
	case (n.Type == domain.Deposit || n.Type == domain.Expense) && n.From == domain.Person(actor.UID):
		e.Status = domain.Pending
	default:
		return Entry{}, ErrForbidden
	}
	if err := domain.CheckDate(n.Date, s.Today(), s.start); err != nil {
		return Entry{}, err
	}
	if n.Type == domain.Opening && n.Date != s.start.FirstDay() {
		return Entry{}, &domain.ValidationError{Field: "date", Problem: ProblemOpeningDate}
	}
	if (n.Type == domain.Adjustment || n.Type == domain.Expense) && e.Note == "" {
		return Entry{}, &domain.ValidationError{Field: "note", Problem: ProblemNoteRequired}
	}
	if err := domain.ValidateMovement(e.Movement); err != nil {
		return Entry{}, err
	}
	return e, nil
}

// RecordOpenings records the start month's opening rows (admin only) in one
// transaction, so a file of opening balances loads whole or not at all.
func (s *Store) RecordOpenings(ctx context.Context, actor Actor, ns []NewMovement) ([]Entry, error) {
	if s.financialWritesPaused {
		return nil, ErrFinancialWritesPaused
	}
	if !actor.Admin {
		return nil, ErrForbidden
	}
	entries := make([]Entry, len(ns))
	for i, n := range ns {
		if n.Type != domain.Opening {
			return nil, fmt.Errorf("%w: row %d is a %s, not an opening", ErrInvalid, i+1, n.Type)
		}
		e, err := s.newEntry(actor, n)
		if err != nil {
			return nil, err
		}
		entries[i] = e
	}
	err := s.db.run(ctx, false, func(t tx) error {
		if err := requireOpen(t, s.start); err != nil {
			return err
		}
		for _, e := range entries { // every read before any write
			if _, err := checkParties(t, e.Movement); err != nil {
				return err
			}
		}
		for _, e := range entries {
			if err := s.writeEntry(t, actor.UID, ActionCreate, nil, e); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return entries, nil
}

// checkParties makes sure the accounts, person and expense category exist;
// it returns an expense's category.
func checkParties(t tx, m domain.Movement) (*Category, error) {
	for _, p := range []domain.Party{m.From, m.To} {
		if (p.Kind == domain.PartyAccount || p.Kind == domain.PartyPerson) && checkID("party", p.ID) != nil {
			return nil, &domain.ValidationError{Field: "party", Problem: ProblemUnknownParty}
		}
		var found bool
		var err error
		switch p.Kind {
		case domain.PartyAccount:
			var rec accountRecord
			found, err = t.get(accountPath(p.ID), &rec)
			found = found && rec.Active
		case domain.PartyPerson:
			found, err = t.get(userPath(p.ID), &userRecord{})
		default:
			continue
		}
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, &domain.ValidationError{Field: "party", Problem: ProblemUnknownParty}
		}
	}
	if m.Type != domain.Expense {
		return nil, nil //nolint:nilnil // only expenses have a category
	}
	if checkID("category", m.ExpenseCategory) != nil {
		return nil, &domain.ValidationError{Field: "expenseCategory", Problem: ProblemUnknownParty}
	}
	var rec categoryRecord
	found, err := t.get(categoryPath(m.ExpenseCategory), &rec)
	if err != nil {
		return nil, err
	}
	if !found || rec.Kind != string(KindExpense) || !rec.Active {
		return nil, &domain.ValidationError{Field: "expenseCategory", Problem: ProblemUnknownParty}
	}
	c := rec.category(m.ExpenseCategory)
	return &c, nil
}

// ConfirmMovement turns a pending row into a confirmed one (admin only).
func (s *Store) ConfirmMovement(ctx context.Context, actor Actor, id string, version int64) (Entry, error) {
	if s.financialWritesPaused {
		return Entry{}, ErrFinancialWritesPaused
	}
	if !actor.Admin {
		return Entry{}, ErrForbidden
	}
	var out Entry
	err := s.db.run(ctx, false, func(t tx) error {
		before, err := readEntry(t, id)
		if err != nil {
			return err
		}
		if before.Version != version {
			return ErrVersionConflict
		}
		if before.Status != domain.Pending {
			return &domain.ValidationError{Field: "status", Problem: ProblemNotPending}
		}
		if err := requireOpen(t, before.Date.Month()); err != nil {
			return err
		}
		out = before
		out.Status, out.ConfirmedBy, out.Version = domain.Confirmed, actor.UID, before.Version+1
		return s.writeEntry(t, actor.UID, ActionConfirm, &before, out)
	})
	return out, err
}

// VoidMovement cancels a row with a reason; it stays in the ledger but no
// longer counts. The admin voids any row in an open month, the author only
// their own pending row. Accruals are voided only by reopening the month.
func (s *Store) VoidMovement(ctx context.Context, actor Actor, id string, version int64, reason string) (Entry, error) {
	if s.financialWritesPaused {
		return Entry{}, ErrFinancialWritesPaused
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return Entry{}, &domain.ValidationError{Field: "reason", Problem: ProblemNoteRequired}
	}
	var out Entry
	err := s.db.run(ctx, false, func(t tx) error {
		before, err := readEntry(t, id)
		if err != nil {
			return err
		}
		switch {
		case domain.IsAccrual(before.Type):
			return ErrForbidden
		case !actor.Admin && (before.CreatedBy != actor.UID || before.Status != domain.Pending):
			return ErrForbidden
		case before.Version != version:
			return ErrVersionConflict
		case before.Status == domain.Voided:
			return &domain.ValidationError{Field: "status", Problem: ProblemAlreadyVoided}
		}
		if err := requireOpen(t, before.Date.Month()); err != nil {
			return err
		}
		out = before
		out.Status, out.VoidReason, out.Version = domain.Voided, reason, before.Version+1
		return s.writeEntry(t, actor.UID, ActionVoid, &before, out)
	})
	return out, err
}

// Entry reads one ledger row.
func (s *Store) Entry(ctx context.Context, id string) (Entry, error) {
	var out Entry
	err := s.db.run(ctx, true, func(t tx) error {
		var err error
		out, err = readEntry(t, id)
		return err
	})
	return out, err
}

// Ledger is every row dated in a month, voided ones included, by date.
func (s *Store) Ledger(ctx context.Context, m domain.Month) ([]Entry, error) {
	return s.queryLedger(ctx, where("month", "==", m.String()))
}

// PendingMovements are the rows waiting for the admin («Ждут подтверждения»).
func (s *Store) PendingMovements(ctx context.Context) ([]Entry, error) {
	return s.queryLedger(ctx, where("status", "==", string(domain.Pending)))
}

func (s *Store) queryLedger(ctx context.Context, conds ...cond) ([]Entry, error) {
	var out []Entry
	err := s.db.run(ctx, true, func(t tx) error {
		var err error
		out, err = readLedger(t, conds...)
		return err
	})
	return out, err
}
