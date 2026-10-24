package store

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/lieranderl/aestheticlab-income-app/internal/domain"
)

// Entity names an audited kind of document.
type Entity string

// Audited documents.
const (
	EntityReport   Entity = "report"
	EntityLedger   Entity = "ledger"
	EntityCategory Entity = "category"
	EntityAccount  Entity = "account"
	EntityUser     Entity = "user"
	EntityTerms    Entity = "terms"
	EntityMonth    Entity = "month"
	EntityImported Entity = "importedMonth"
)

// Audited actions.
const (
	ActionCreate  = "create"
	ActionUpdate  = "update"
	ActionConfirm = "confirm"
	ActionVoid    = "void"
	ActionRecount = "recount"
	ActionClose   = "close"
	ActionReopen  = "reopen"
	ActionSetUp   = "setup"
	ActionLock    = "lock"   // too many failed logins
	ActionRemove  = "remove" // a person removed; their records stay
)

// AuditEntry is one row of the change log («Журнал изменений»). Before and
// After are the document as JSON; empty when it didn't exist.
type AuditEntry struct {
	ID       string
	Entity   Entity
	EntityID string
	Action   string
	ActorID  string
	At       time.Time
	Before   string
	After    string
}

type auditRecord struct {
	Entity   string    `firestore:"entity"   json:"entity"`
	EntityID string    `firestore:"entityId" json:"entityId"`
	Action   string    `firestore:"action"   json:"action"`
	ActorID  string    `firestore:"actorId"  json:"actorId"`
	At       time.Time `firestore:"at"       json:"at"`
	Before   string    `firestore:"before"   json:"before"`
	After    string    `firestore:"after"    json:"after"`
}

// audit appends a change-log row in the same transaction as the change.
// before and after are records (nil when absent); secrets are the caller's
// job to leave out.
func (s *Store) audit(t tx, entity Entity, entityID, action, actorID string, before, after any) error {
	at := s.stamp()
	return t.set(auditPath(auditID(at)), auditRecord{
		Entity: string(entity), EntityID: entityID, Action: action, ActorID: actorID,
		At: at, Before: asJSON(before), After: asJSON(after),
	})
}

var auditSeq atomic.Uint64

// auditID sorts in the order rows were written, even within one clock tick.
func auditID(at time.Time) string {
	return fmt.Sprintf("%s-%09d-%s", at.Format("20060102T150405.000000000"), auditSeq.Add(1)%1e9, newID()[:8])
}

func asJSON(v any) string {
	if v == nil {
		return ""
	}
	data, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(data)
}

// Audit is the change log of one document, newest first.
func (s *Store) Audit(ctx context.Context, entityID string) ([]AuditEntry, error) {
	return s.queryAudit(ctx, where("entityId", "==", entityID))
}

// AuditBetween reads changes made during salon calendar days, inclusive.
func (s *Store) AuditBetween(ctx context.Context, from, to domain.Date) ([]AuditEntry, error) {
	first := time.Date(from.Year, from.Mon, from.Day, 0, 0, 0, 0, s.loc)
	last := time.Date(to.Year, to.Mon, to.Day, 0, 0, 0, 0, s.loc).AddDate(0, 0, 1).Add(-time.Nanosecond)
	return s.queryAudit(ctx, where("at", ">=", first), where("at", "<=", last))
}

// LocalTime formats audit timestamps in the same timezone as business dates.
func (s *Store) LocalTime(at time.Time) time.Time { return at.In(s.loc) }

// ExpectedReporters returns the active reporters at the end of a salon day.
// Audited user snapshots preserve old gaps when staff settings change later.
func (s *Store) ExpectedReporters(ctx context.Context, day domain.Date) ([]string, error) {
	rosters, err := s.ExpectedReportersBetween(ctx, day, day)
	return rosters[day], err
}

// ExpectedReportersBetween reads audited user history once and returns the
// expected reporters at the end of each inclusive salon calendar day.
func (s *Store) ExpectedReportersBetween(ctx context.Context, from, to domain.Date) (map[domain.Date][]string, error) {
	if to.Before(from) {
		return nil, ErrInvalid
	}
	end := time.Date(to.Year, to.Mon, to.Day, 0, 0, 0, 0, s.loc).AddDate(0, 0, 1).Add(-time.Nanosecond)
	rows, err := s.queryAudit(ctx, where("entity", "==", string(EntityUser)), where("at", "<=", end))
	if err != nil {
		return nil, err
	}
	// Audit IDs sort by timestamp and write sequence, including same-clock updates.
	slices.Reverse(rows)
	states := map[string]userRecord{}
	out := map[domain.Date][]string{}
	index := 0
	for day := from; !to.Before(day); day = day.AddDays(1) {
		cutoff := time.Date(day.Year, day.Mon, day.Day, 0, 0, 0, 0, s.loc).AddDate(0, 0, 1).Add(-time.Nanosecond)
		for index < len(rows) && !rows[index].At.After(cutoff) {
			row := rows[index]
			var user userRecord
			if err := json.Unmarshal([]byte(row.After), &user); err != nil {
				return nil, fmt.Errorf("decode user audit %s: %w", row.ID, err)
			}
			states[row.EntityID] = user
			index++
		}
		var expected []string
		for uid, user := range states {
			if user.Active && user.SubmitsReports && !user.CreatedAt.After(cutoff) {
				expected = append(expected, uid)
			}
		}
		slices.Sort(expected)
		out[day] = expected
	}
	return out, nil
}

func (s *Store) queryAudit(ctx context.Context, conds ...cond) ([]AuditEntry, error) {
	var out []AuditEntry
	err := s.db.run(ctx, true, func(t tx) error {
		out = out[:0]
		return t.query("audit", conds, func(id string, decode func(any) error) error {
			var r auditRecord
			if err := decode(&r); err != nil {
				return err
			}
			out = append(out, AuditEntry{
				ID: id, Entity: Entity(r.Entity), EntityID: r.EntityID, Action: r.Action,
				ActorID: r.ActorID, At: r.At.UTC(), Before: r.Before, After: r.After,
			})
			return nil
		})
	})
	slices.SortFunc(out, func(a, b AuditEntry) int { return strings.Compare(b.ID, a.ID) })
	return out, err
}
