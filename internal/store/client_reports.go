package store

import (
	"context"
	"maps"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/lieranderl/aestheticlab-income-app/internal/domain"
)

// ClientEntry is one client's amounts, with a stable token for safe retry.
type ClientEntry struct {
	Person  string
	Date    domain.Date
	Token   string
	Input   domain.ClientInput
	Version int64
}

type clientDayRecord struct {
	Last int64 `firestore:"last" json:"last"`
}

var clientTokenPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{16,64}$`)

// ProblemReasonTooLong refuses a removal reason longer than maxVoidReason.
const ProblemReasonTooLong domain.Problem = "reason_too_long"

const maxVoidReason = 200 // characters

func clientID(uid string, d domain.Date, token string) string {
	return "client_" + uid + "_" + d.String() + "_" + token
}

func sameClient(a, b domain.ClientInput) bool {
	return a.Services == b.Services && a.ServiceCash == b.ServiceCash && a.ServiceCard == b.ServiceCard && a.CertificatePaid == b.CertificatePaid && a.ExtrasDestination == b.ExtrasDestination && maps.Equal(a.Extras, b.Extras)
}

// SaveClientReport creates or edits one client. A repeated unchanged create
// acknowledges the first write, never allocates a second number or audit.
func (s *Store) SaveClientReport(ctx context.Context, actor Actor, e ClientEntry) (SavedReport, error) {
	if s.financialWritesPaused {
		return SavedReport{}, ErrFinancialWritesPaused
	}
	if !actor.Admin && actor.UID != e.Person {
		return SavedReport{}, ErrForbidden
	}
	if err := checkID("user", e.Person); err != nil {
		return SavedReport{}, err
	}
	if !clientTokenPattern.MatchString(e.Token) {
		return SavedReport{}, ErrInvalid
	}
	if err := checkID("client", e.Token); err != nil {
		return SavedReport{}, err
	}
	if err := domain.CheckDate(e.Date, s.Today(), s.start); err != nil {
		return SavedReport{}, err
	}
	id := clientID(e.Person, e.Date, e.Token)
	var out SavedReport
	err := s.db.run(ctx, false, func(t tx) error {
		u, err := readUser(t, e.Person)
		if err != nil {
			return err
		}
		if !actor.Admin && !u.SubmitsReports {
			return ErrForbidden
		}
		var before reportRecord
		found, err := t.get("reports/"+id, &before)
		if err != nil {
			return err
		}
		if found && before.Voided {
			return ErrNotFound
		}
		if found && e.Version == 0 && before.Version == 1 && before.ClientInput != nil && sameClient(*before.ClientInput, e.Input) {
			out, err = before.saved()
			return err
		}
		if err := requireOpen(t, e.Date.Month()); err != nil {
			return err
		}
		if before.Version != e.Version || (!found && e.Version != 0) {
			return ErrVersionConflict
		}
		h, err := readTermsHistory(t, e.Person)
		if err != nil {
			return err
		}
		terms, ok := h.For(e.Date.Month())
		if !ok {
			return &domain.ValidationError{Field: "terms", Problem: domain.ProblemMissingTerms}
		}
		pr, err := domain.NewClientReport(e.Person, e.Date, e.Input, terms.SelfPct)
		if err != nil {
			return err
		}
		var counter clientDayRecord
		counterPath := "clientDays/" + e.Person + "_" + e.Date.String()
		if !found {
			if _, err = t.get(counterPath, &counter); err != nil {
				return err
			}
		}
		cats, err := readCategories(t)
		if err != nil {
			return err
		}
		now := s.stamp()
		input := e.Input
		input.Extras = maps.Clone(e.Input.Extras)
		out = SavedReport{ID: id, ClientNumber: before.ClientNumber, ClientInput: &input, PersonReport: pr, Version: before.Version + 1, CreatedAt: now, UpdatedAt: now, UpdatedBy: actor.UID}
		if found {
			out.CreatedAt = before.CreatedAt.UTC()
		} else {
			out.ClientNumber = counter.Last + 1
		}
		oilFound := false
		for _, cat := range cats {
			if cat.ID == "oil" {
				oilFound = true
			}
		}
		if pr.Revenue["oil"] != 0 && !oilFound {
			oil, err := s.createCategory(t, actor.UID, Category{ID: "oil", Name: "Масло", Kind: KindRevenue, Order: 6})
			if err != nil {
				return err
			}
			used := oil
			used.UsedCount = 1
			if err := s.writeCategory(t, actor.UID, oil, used); err != nil {
				return err
			}
		}
		for _, cat := range cats {
			was := before.Revenue[cat.ID] != 0 || before.Payments[cat.ID] != 0
			used := pr.Revenue[cat.ID] != 0 || pr.Payments[cat.ID] != 0
			if used && !was {
				changed := cat
				changed.UsedCount++
				if err := s.writeCategory(t, actor.UID, cat, changed); err != nil {
					return err
				}
			}
		}
		if !found {
			counter.Last = out.ClientNumber
			if err := t.set(counterPath, counter); err != nil {
				return err
			}
		}
		rec := reportRecordOf(out)
		rec.ClientToken = e.Token
		if err := t.set("reports/"+id, rec); err != nil {
			return err
		}
		action, prev := ActionCreate, any(nil)
		if found {
			action, prev = ActionUpdate, before
		}
		return s.audit(t, EntityReport, id, action, actor.UID, prev, rec)
	})
	return out, err
}

// ClientReport returns one record after checking ownership.
func (s *Store) ClientReport(ctx context.Context, actor Actor, id string) (SavedReport, error) {
	if err := checkID("report", id); err != nil {
		return SavedReport{}, ErrNotFound
	}
	var out SavedReport
	err := s.db.run(ctx, true, func(t tx) error {
		var rec reportRecord
		found, err := t.get("reports/"+id, &rec)
		if err != nil {
			return err
		}
		if !found || rec.ClientInput == nil || rec.Voided {
			return ErrNotFound
		}
		if !actor.Admin && actor.UID != rec.UserID {
			return ErrForbidden
		}
		out, err = rec.saved()
		return err
	})
	return out, err
}

// VoidClientReport removes a client entered by mistake. The record stays,
// voided with its reason, so the change log keeps it; every read skips it.
// Its number is never reused and its create token cannot restore it.
func (s *Store) VoidClientReport(ctx context.Context, actor Actor, id string, version int64, reason string) error {
	if s.financialWritesPaused {
		return ErrFinancialWritesPaused
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return &domain.ValidationError{Field: "reason", Problem: ProblemNoteRequired}
	}
	if utf8.RuneCountInString(reason) > maxVoidReason {
		return &domain.ValidationError{Field: "reason", Problem: ProblemReasonTooLong}
	}
	if err := checkID("report", id); err != nil {
		return ErrNotFound
	}
	return s.db.run(ctx, false, func(t tx) error {
		var before reportRecord
		found, err := t.get("reports/"+id, &before)
		if err != nil {
			return err
		}
		if !found || before.ClientInput == nil || before.Voided {
			return ErrNotFound
		}
		if !actor.Admin {
			if actor.UID != before.UserID {
				return ErrForbidden
			}
			u, err := readUser(t, before.UserID)
			if err != nil {
				return err
			}
			if !u.SubmitsReports {
				return ErrForbidden
			}
		}
		if before.Version != version {
			return ErrVersionConflict
		}
		day, err := domain.ParseDate(before.Date)
		if err != nil {
			return err
		}
		// The same date rules as an edit: nothing before the start month.
		if err := domain.CheckDate(day, s.Today(), s.start); err != nil {
			return err
		}
		if err := requireOpen(t, day.Month()); err != nil {
			return err
		}
		after := before
		after.Voided, after.VoidReason = true, reason
		after.Version = before.Version + 1
		after.UpdatedAt, after.UpdatedBy = s.stamp(), actor.UID
		if err := t.set("reports/"+id, after); err != nil {
			return err
		}
		return s.audit(t, EntityReport, id, ActionVoid, actor.UID, before, after)
	})
}

// NextClientNumber is the number the person's next client on day d gets.
// Removed clients keep their numbers, so it reads the day counter.
func (s *Store) NextClientNumber(ctx context.Context, uid string, d domain.Date) (int64, error) {
	if err := checkID("user", uid); err != nil {
		return 0, err
	}
	var counter clientDayRecord
	err := s.db.run(ctx, true, func(t tx) error {
		_, err := t.get("clientDays/"+uid+"_"+d.String(), &counter)
		return err
	})
	return counter.Last + 1, err
}
