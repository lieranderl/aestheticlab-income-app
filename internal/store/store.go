// Package store keeps the Kassa's data: users and their money terms,
// categories, kassa accounts, daily reports, the ledger, months with their
// snapshots, imported history and the audit log (system-design §2–4).
//
// Every write is one transaction that also appends an audit row. Formulas
// come from internal/domain; this package only reads, checks and saves.
package store

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"strings"
	"time"
	_ "time/tzdata" // the salon's timezone must load in the distroless image

	"github.com/lieranderl/aestheticlab-income-app/internal/domain"
)

// Errors handlers turn into Russian messages. Rule violations from
// internal/domain come back as *domain.ValidationError.
var (
	ErrNotFound              = errors.New("not found")
	ErrVersionConflict       = errors.New("changed by someone else") // «Запись изменена другим пользователем — обновите страницу»
	ErrMonthClosed           = errors.New("month closed")            // «Месяц закрыт — сначала откройте его заново»
	ErrForbidden             = errors.New("forbidden")
	ErrUsernameTaken         = errors.New("username taken")
	ErrBalanceOpen           = errors.New("balance not settled")      // a person is removed only at a zero balance
	ErrOpenRecords           = errors.New("records in an open month") // reports in an unclosed month or pending movements
	ErrTermsInForce          = errors.New("salary or share in force") // terms still pay from the first open month on
	ErrAlreadySetUp          = errors.New("already set up")           // cmd/seed runs once
	ErrInvalid               = errors.New("invalid input")
	ErrFinancialWritesPaused = errors.New("financial writes paused") // «Запись финансовых операций временно приостановлена»
)

// Store is the Kassa's data, on Firestore or in memory.
type Store struct {
	db                    backend
	now                   func() time.Time
	loc                   *time.Location
	start                 domain.Month
	financialWritesPaused bool
}

// Option configures a Store.
type Option func(*Store)

// WithClock sets the clock that stamps writes and decides "today".
func WithClock(now func() time.Time) Option { return func(s *Store) { s.now = now } }

// WithLocation sets the salon's timezone (default Europe/Brussels).
func WithLocation(loc *time.Location) Option { return func(s *Store) { s.loc = loc } }

// WithStartMonth sets the first month the app keeps (INCOME_START_MONTH):
// 2026-11 in production, 2026-10 on the October trial.
func WithStartMonth(m domain.Month) Option { return func(s *Store) { s.start = m } }

// WithFinancialWritesPaused sets whether financial writes (reports, movements,
// recounts, month close/reopen, openings) are paused for maintenance or cutover.
func WithFinancialWritesPaused(paused bool) Option {
	return func(s *Store) { s.financialWritesPaused = paused }
}

// FinancialWritesPaused reports whether financial writes are currently paused.
func (s *Store) FinancialWritesPaused() bool { return s.financialWritesPaused }

// SetFinancialWritesPaused updates whether financial writes are paused.
func (s *Store) SetFinancialWritesPaused(paused bool) { s.financialWritesPaused = paused }

// DefaultStartMonth is production's go-live month.
var DefaultStartMonth = domain.Month{Year: 2026, Mon: time.November}

func newStore(db backend, opts []Option) *Store {
	s := &Store{db: db, now: time.Now, start: DefaultStartMonth}
	for _, o := range opts {
		o(s)
	}
	if s.loc == nil {
		loc, err := time.LoadLocation("Europe/Brussels")
		if err != nil {
			panic(err) // time/tzdata is embedded, so this cannot happen
		}
		s.loc = loc
	}
	return s
}

// Close releases the backend's connections.
func (s *Store) Close() error { return s.db.close() }

// StartMonth is the first month the app keeps.
func (s *Store) StartMonth() domain.Month { return s.start }

// Today is the salon's calendar day now.
func (s *Store) Today() domain.Date { return domain.Today(s.now(), s.loc) }

// Ping checks the backend answers, for /readyz.
func (s *Store) Ping(ctx context.Context) error {
	return s.db.run(ctx, true, func(t tx) error {
		_, err := t.get(monthPath(s.start), &monthRecord{})
		return err
	})
}

// Actor is who makes a change, from their session.
type Actor struct {
	UID   string
	Admin bool
}

// stamp is now at Firestore's precision (microseconds), so a value written
// reads back equal on both backends.
func (s *Store) stamp() time.Time { return s.now().UTC().Truncate(time.Microsecond) }

func trimmed(s string) string { return strings.TrimSpace(s) }

// newID is a random document ID.
func newID() string { return strings.ToLower(rand.Text()[:20]) }

// checkID refuses IDs that would escape their collection or that Firestore
// reserves (__name__), on both backends.
func checkID(kind, id string) error {
	if id == "" || strings.ContainsAny(id, "/.") || (strings.HasPrefix(id, "__") && strings.HasSuffix(id, "__")) {
		return fmt.Errorf("%w: %s ID %q", ErrInvalid, kind, id)
	}
	return nil
}

func userPath(uid string) string                  { return "users/" + uid }
func usernamePath(name string) string             { return "usernames/" + name }
func termsCollection(uid string) string           { return "users/" + uid + "/terms" }
func termsPath(uid string, m domain.Month) string { return termsCollection(uid) + "/" + m.String() }
func categoryPath(id string) string               { return "categories/" + id }
func accountPath(id string) string                { return "accounts/" + id }
func reportPath(uid string, d domain.Date) string { return "reports/" + uid + "_" + d.String() }
func ledgerPath(id string) string                 { return "ledger/" + id }
func monthPath(m domain.Month) string             { return "months/" + m.String() }
func importedPath(m domain.Month) string          { return "importedMonths/" + m.String() }
func auditPath(id string) string                  { return "audit/" + id }
