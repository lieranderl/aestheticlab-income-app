package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"regexp"

	"github.com/lieranderl/aestheticlab-income-app/internal/domain"
)

// bookingLinkRecord is the lock that keeps one active client per Timely
// booking: bookingLinks/{sha256(calendar|event)}. It is written in the same
// transaction as the client record's booking field.
type bookingLinkRecord struct {
	ReportID string `firestore:"reportId" json:"reportId"`
	Person   string `firestore:"person"   json:"person"`
	Date     string `firestore:"date"     json:"date"`
	Active   bool   `firestore:"active"   json:"active"`
}

// BookingLink is an active link: which client entry a booking was entered as.
type BookingLink struct {
	ReportID string
	Person   string
	Date     domain.Date
}

var (
	bookingCalendar = regexp.MustCompile(`^[A-Za-z0-9._@-]{1,254}$`)
	bookingEvent    = regexp.MustCompile(`^[A-Za-z0-9_-]{1,256}$`)
)

func bookingLockPath(ref domain.BookingRef) (string, error) {
	if !bookingCalendar.MatchString(ref.Calendar) || !bookingEvent.MatchString(ref.Event) {
		return "", ErrInvalid
	}
	sum := sha256.Sum256([]byte(ref.Calendar + "|" + ref.Event))
	return "bookingLinks/" + hex.EncodeToString(sum[:]), nil
}

func sameBooking(a, b *domain.BookingRef) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && *a == *b)
}

func cloneBooking(b *domain.BookingRef) *domain.BookingRef {
	if b == nil {
		return nil
	}
	c := *b
	return &c
}

// readFreeLock reads the booking's lock and refuses it when another live
// client holds it. It returns the lock's path for the write.
func readFreeLock(t tx, ref domain.BookingRef, reportID string) (string, error) {
	path, err := bookingLockPath(ref)
	if err != nil {
		return "", err
	}
	var lock bookingLinkRecord
	found, err := t.get(path, &lock)
	if err != nil {
		return "", err
	}
	if found && lock.Active && lock.ReportID != reportID {
		return "", ErrBookingTaken
	}
	return path, nil
}

// readOwnLock returns the path of the lock reportID holds for ref, or "" when
// it holds none (no booking, or a lock left over from elsewhere).
func readOwnLock(t tx, ref *domain.BookingRef, reportID string) (string, error) {
	if ref == nil {
		return "", nil
	}
	path, err := bookingLockPath(*ref)
	if err != nil {
		return "", nil // an old malformed ref holds no lock
	}
	var lock bookingLinkRecord
	found, err := t.get(path, &lock)
	if err != nil || !found || !lock.Active || lock.ReportID != reportID {
		return "", err
	}
	return path, nil
}

// readLinkable reads a live client record for a link change and checks who
// may change it: the owner who still submits reports, or the admin.
func (s *Store) readLinkable(t tx, actor Actor, id string) (reportRecord, error) {
	var rec reportRecord
	found, err := t.get("reports/"+id, &rec)
	if err != nil {
		return rec, err
	}
	if !found || rec.ClientInput == nil || rec.Voided {
		return rec, ErrNotFound
	}
	if !actor.Admin && actor.UID != rec.UserID {
		return rec, ErrForbidden
	}
	u, err := readUser(t, rec.UserID)
	if err != nil {
		return rec, err
	}
	if u.Removed || (!actor.Admin && !u.SubmitsReports) { // as SaveClientReport
		return rec, ErrForbidden
	}
	return rec, nil
}

// LinkClientBooking links a client entered by hand to its booking. The web
// layer checks the booking against the calendar first; every rule that can
// change meanwhile is checked again here. Repeating a link the entry already
// has is acknowledged without any other check, even in a closed month.
func (s *Store) LinkClientBooking(ctx context.Context, actor Actor, id string, version int64, ref domain.BookingRef) error {
	if s.financialWritesPaused {
		return ErrFinancialWritesPaused
	}
	if err := checkID("report", id); err != nil {
		return ErrNotFound
	}
	if _, err := bookingLockPath(ref); err != nil {
		return err
	}
	return s.db.run(ctx, false, func(t tx) error {
		before, err := s.readLinkable(t, actor, id)
		if err != nil {
			return err
		}
		if before.ClientInput.Booking != nil && *before.ClientInput.Booking == ref {
			if own, err := readOwnLock(t, &ref, id); err != nil || own != "" {
				return err // already linked: acknowledged
			}
		}
		day, err := domain.ParseDate(before.Date)
		if err != nil {
			return err
		}
		if err := domain.CheckDate(day, s.Today(), s.start); err != nil {
			return err
		}
		if err := requireOpen(t, day.Month()); err != nil {
			return err
		}
		if before.Version != version {
			return ErrVersionConflict
		}
		if before.ClientInput.Booking != nil && *before.ClientInput.Booking != ref {
			return ErrInvalid // unlink the other booking first
		}
		lockPath, err := readFreeLock(t, ref, id)
		if err != nil {
			return err
		}
		after := before
		in := *before.ClientInput
		in.Booking = cloneBooking(&ref)
		after.ClientInput = &in
		after.Version = before.Version + 1
		after.UpdatedAt, after.UpdatedBy = s.stamp(), actor.UID
		if err := t.set("reports/"+id, after); err != nil {
			return err
		}
		if err := t.set(lockPath, bookingLinkRecord{ReportID: id, Person: before.UserID, Date: before.Date, Active: true}); err != nil {
			return err
		}
		return s.audit(t, EntityReport, id, ActionUpdate, actor.UID, before, after)
	})
}

// UnlinkClientBooking removes an entry's booking link. It never needs the
// calendar, so a deleted, moved or unreadable booking can always be
// unlinked. An entry without a link is acknowledged.
func (s *Store) UnlinkClientBooking(ctx context.Context, actor Actor, id string, version int64) error {
	if s.financialWritesPaused {
		return ErrFinancialWritesPaused
	}
	if err := checkID("report", id); err != nil {
		return ErrNotFound
	}
	return s.db.run(ctx, false, func(t tx) error {
		before, err := s.readLinkable(t, actor, id)
		if err != nil {
			return err
		}
		if before.ClientInput.Booking == nil {
			return nil
		}
		if before.Version != version {
			return ErrVersionConflict
		}
		day, err := domain.ParseDate(before.Date)
		if err != nil {
			return err
		}
		if err := domain.CheckDate(day, s.Today(), s.start); err != nil {
			return err
		}
		if err := requireOpen(t, day.Month()); err != nil {
			return err
		}
		release, err := readOwnLock(t, before.ClientInput.Booking, id)
		if err != nil {
			return err
		}
		after := before
		in := *before.ClientInput
		in.Booking = nil
		after.ClientInput = &in
		after.Version = before.Version + 1
		after.UpdatedAt, after.UpdatedBy = s.stamp(), actor.UID
		if err := t.set("reports/"+id, after); err != nil {
			return err
		}
		if release != "" {
			if err := t.set(release, bookingLinkRecord{ReportID: id, Person: before.UserID, Date: before.Date, Active: false}); err != nil {
				return err
			}
		}
		return s.audit(t, EntityReport, id, ActionUpdate, actor.UID, before, after)
	})
}

// BookingLinks returns the active links among refs, read by ID.
func (s *Store) BookingLinks(ctx context.Context, refs []domain.BookingRef) (map[domain.BookingRef]BookingLink, error) {
	out := map[domain.BookingRef]BookingLink{}
	err := s.db.run(ctx, true, func(t tx) error {
		for _, ref := range refs {
			path, err := bookingLockPath(ref)
			if err != nil {
				continue
			}
			var lock bookingLinkRecord
			found, err := t.get(path, &lock)
			if err != nil {
				return err
			}
			if !found || !lock.Active {
				continue
			}
			d, err := domain.ParseDate(lock.Date)
			if err != nil {
				continue // a malformed lock never hides the others
			}
			out[ref] = BookingLink{ReportID: lock.ReportID, Person: lock.Person, Date: d}
		}
		return nil
	})
	return out, err
}

// ClientSaved says whether a client record exists for this creation token,
// so a retry is answered by the store without asking the calendar again.
func (s *Store) ClientSaved(ctx context.Context, actor Actor, person string, d domain.Date, token string) (bool, error) {
	if !actor.Admin && actor.UID != person {
		return false, ErrForbidden
	}
	if checkID("user", person) != nil || !clientTokenPattern.MatchString(token) {
		return false, ErrInvalid
	}
	var found bool
	err := s.db.run(ctx, true, func(t tx) error {
		var rec reportRecord
		var err error
		found, err = t.get("reports/"+clientID(person, d, token), &rec)
		return err
	})
	return found, err
}
