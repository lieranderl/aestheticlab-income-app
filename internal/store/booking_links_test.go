package store_test

import (
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/lieranderl/aestheticlab-income-app/internal/domain"
	"github.com/lieranderl/aestheticlab-income-app/internal/store"
)

func ref(id string) *domain.BookingRef {
	return &domain.BookingRef{Calendar: "olga-cal@group.calendar.google.com", Event: id}
}

func bookedClient(s *salon, t *testing.T, token string, booking *domain.BookingRef) store.ClientEntry {
	return store.ClientEntry{Person: s.olga, Date: date(t, "2026-11-20"), Token: token, Input: domain.ClientInput{Services: 3000, ServiceCash: 900, Booking: booking}}
}

func TestOneBookingHasOneActiveClient(t *testing.T) {
	eachSalon(t, func(t *testing.T, s *salon) {
		first := bookedClient(s, t, "booked-client-first", ref("b1"))
		saved, err := s.SaveClientReport(ctx, s.actor(s.olga), first)
		must(t, err)
		if saved.ClientInput.Booking == nil || *saved.ClientInput.Booking != *ref("b1") {
			t.Fatalf("booking %+v", saved.ClientInput.Booking)
		}
		_, err = s.SaveClientReport(ctx, s.actor(s.olga), bookedClient(s, t, "booked-client-second", ref("b1")))
		wantErr(t, "second entry for the booking", err, store.ErrBookingTaken)

		retry, err := s.SaveClientReport(ctx, s.actor(s.olga), first)
		must(t, err)
		if retry.ID != saved.ID || retry.Version != 1 {
			t.Fatalf("retry %+v", retry)
		}
		changed := first
		changed.Input.Booking = ref("b2")
		_, err = s.SaveClientReport(ctx, s.actor(s.olga), changed)
		wantErr(t, "replay with another booking", err, store.ErrVersionConflict)
		changed.Input.Booking = nil
		_, err = s.SaveClientReport(ctx, s.actor(s.olga), changed)
		wantErr(t, "replay without the booking", err, store.ErrVersionConflict)

		links, err := s.BookingLinks(ctx, []domain.BookingRef{*ref("b1"), *ref("b2")})
		must(t, err)
		if l, ok := links[*ref("b1")]; !ok || l.ReportID != saved.ID || l.Date != date(t, "2026-11-20") || l.Person != s.olga {
			t.Fatalf("links %+v", links)
		}
		if _, ok := links[*ref("b2")]; ok {
			t.Fatal("b2 is not linked")
		}
	})
}

func TestConcurrentEntriesForOneBookingLetExactlyOneWin(t *testing.T) {
	eachSalon(t, func(t *testing.T, s *salon) {
		const n = 6
		errs := make(chan error, n)
		var wg sync.WaitGroup
		for i := range n {
			wg.Go(func() {
				_, err := s.SaveClientReport(ctx, s.actor(s.olga), bookedClient(s, t, fmt.Sprintf("booked-concurrent-%d", i), ref("b1")))
				errs <- err
			})
		}
		wg.Wait()
		close(errs)
		won := 0
		for err := range errs {
			switch {
			case err == nil:
				won++
			case !errors.Is(err, store.ErrBookingTaken):
				t.Fatalf("unexpected %v", err)
			}
		}
		rows, err := s.PersonReportsIn(ctx, s.olga, month(t, "2026-11"))
		must(t, err)
		if won != 1 || len(rows) != 1 {
			t.Fatalf("won %d, rows %d", won, len(rows))
		}
	})
}

func TestRemovingAClientReleasesItsBooking(t *testing.T) {
	eachSalon(t, func(t *testing.T, s *salon) {
		saved, err := s.SaveClientReport(ctx, s.actor(s.olga), bookedClient(s, t, "booked-to-remove", ref("b1")))
		must(t, err)
		must(t, s.VoidClientReport(ctx, s.actor(s.olga), saved.ID, saved.Version, "ошибка"))
		links, err := s.BookingLinks(ctx, []domain.BookingRef{*ref("b1")})
		must(t, err)
		if len(links) != 0 {
			t.Fatalf("still linked %+v", links)
		}
		again, err := s.SaveClientReport(ctx, s.actor(s.olga), bookedClient(s, t, "booked-after-remove", ref("b1")))
		must(t, err)
		if again.ClientInput.Booking == nil {
			t.Fatal("relink lost")
		}
	})
}

func TestEditsKeepTheStoredBooking(t *testing.T) {
	eachSalon(t, func(t *testing.T, s *salon) {
		in := bookedClient(s, t, "booked-client-to-edit", ref("b1"))
		saved, err := s.SaveClientReport(ctx, s.actor(s.olga), in)
		must(t, err)
		for i, other := range []*domain.BookingRef{nil, ref("b2")} {
			in.Version = saved.Version
			in.Input.Booking = other
			in.Input.ServiceCash = domain.Cents(1000 + i)
			saved, err = s.SaveClientReport(ctx, s.actor(s.olga), in)
			must(t, err)
			if saved.ClientInput.Booking == nil || *saved.ClientInput.Booking != *ref("b1") {
				t.Fatalf("edit %d changed the booking to %+v", i, saved.ClientInput.Booking)
			}
		}
	})
}

func TestAManualClientIsLinkedAndUnlinkedWithAudit(t *testing.T) {
	eachSalon(t, func(t *testing.T, s *salon) {
		olga := s.actor(s.olga)
		manual, err := s.SaveClientReport(ctx, olga, bookedClient(s, t, "manual-client-entry-x", nil))
		must(t, err)
		other, err := s.SaveClientReport(ctx, olga, bookedClient(s, t, "manual-client-entry-y", nil))
		must(t, err)

		wantErr(t, "stale link", s.LinkClientBooking(ctx, olga, manual.ID, manual.Version+1, *ref("b1")), store.ErrVersionConflict)
		wantErr(t, "someone else's entry", s.LinkClientBooking(ctx, s.actor(s.maria), manual.ID, manual.Version, *ref("b1")), store.ErrForbidden)
		must(t, s.LinkClientBooking(ctx, olga, manual.ID, manual.Version, *ref("b1")))
		must(t, s.LinkClientBooking(ctx, olga, manual.ID, manual.Version, *ref("b1"))) // a repeated tap
		wantErr(t, "taken", s.LinkClientBooking(ctx, olga, other.ID, other.Version, *ref("b1")), store.ErrBookingTaken)

		linked, err := s.ClientReport(ctx, olga, manual.ID)
		must(t, err)
		if linked.Version != 2 || linked.ClientInput.Booking == nil || *linked.ClientInput.Booking != *ref("b1") {
			t.Fatalf("linked %+v", linked)
		}
		wantErr(t, "relink to another booking", s.LinkClientBooking(ctx, olga, manual.ID, linked.Version, *ref("b2")), store.ErrInvalid)

		wantErr(t, "stale unlink", s.UnlinkClientBooking(ctx, olga, manual.ID, 1), store.ErrVersionConflict)
		must(t, s.UnlinkClientBooking(ctx, olga, manual.ID, linked.Version))
		must(t, s.UnlinkClientBooking(ctx, olga, manual.ID, linked.Version)) // a repeated tap
		unlinked, err := s.ClientReport(ctx, olga, manual.ID)
		must(t, err)
		if unlinked.Version != 3 || unlinked.ClientInput.Booking != nil || unlinked.Report.Total != manual.Report.Total {
			t.Fatalf("unlinked %+v", unlinked)
		}
		must(t, s.LinkClientBooking(ctx, s.admin, other.ID, other.Version, *ref("b1"))) // free again; the admin may link
		if got := s.actions(t, manual.ID); len(got) != 3 {
			t.Fatalf("audit %v", got)
		}
	})
}

func TestLinksInAClosedMonthAreRefusedButRepeatsAreAcknowledged(t *testing.T) {
	eachSalon(t, func(t *testing.T, s *salon) {
		olga := s.actor(s.olga)
		manual, err := s.SaveClientReport(ctx, olga, bookedClient(s, t, "closed-month-link-x", nil))
		must(t, err)
		linked, err := s.SaveClientReport(ctx, olga, bookedClient(s, t, "closed-month-link-y", ref("b1")))
		must(t, err)
		s.at("2026-12-01")
		_, err = s.CloseMonth(ctx, s.admin, month(t, "2026-11"))
		must(t, err)
		wantErr(t, "link", s.LinkClientBooking(ctx, olga, manual.ID, manual.Version, *ref("b2")), store.ErrMonthClosed)
		wantErr(t, "unlink", s.UnlinkClientBooking(ctx, olga, linked.ID, linked.Version), store.ErrMonthClosed)
		must(t, s.LinkClientBooking(ctx, olga, linked.ID, linked.Version, *ref("b1")))
		must(t, s.UnlinkClientBooking(ctx, olga, manual.ID, manual.Version))
		_, err = s.SaveClientReport(ctx, olga, bookedClient(s, t, "closed-month-link-y", ref("b1")))
		must(t, err) // the lost-response retry after close
		exists, err := s.ClientSaved(ctx, olga, s.olga, date(t, "2026-11-20"), "closed-month-link-y")
		must(t, err)
		if !exists {
			t.Fatal("saved client not found")
		}
		exists, err = s.ClientSaved(ctx, olga, s.olga, date(t, "2026-11-20"), "never-saved-token")
		must(t, err)
		if exists {
			t.Fatal("unsaved client found")
		}
	})
}
