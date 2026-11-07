package bookings_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lieranderl/aestheticlab-income-app/internal/bookings"
	"github.com/lieranderl/aestheticlab-income-app/internal/domain"
)

var brussels = func() *time.Location {
	loc, err := time.LoadLocation("Europe/Brussels")
	if err != nil {
		panic(err)
	}
	return loc
}()

func at(day string, hm string) time.Time {
	t, err := time.ParseInLocation("2006-01-02 15:04", day+" "+hm, brussels)
	if err != nil {
		panic(err)
	}
	return t
}

func mustDate(t *testing.T, s string) domain.Date {
	t.Helper()
	d, err := domain.ParseDate(s)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

const mariaCal = "maria-cal@group.calendar.google.com"

func booking(id string, start time.Time, line string) bookings.Event {
	return bookings.Event{ID: id, Summary: "Olga Example", Description: "Olga Example - " + line + "\nTel: +32000000000", Start: start}
}

func newReader(t *testing.T, src bookings.Source) *bookings.Reader {
	t.Helper()
	r, err := bookings.NewReader(src, mariaCal+"|maria", brussels)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestADayListsOnlyBookingsThatStartThatDayInBrussels(t *testing.T) {
	src := bookings.NewMemory()
	src.Put(mariaCal, booking("late", at("2026-11-07", "17:00"), "Upper Lip Wax Price: €10.00 with Maria Test (Confirmed)"))
	src.Put(mariaCal, booking("early", at("2026-11-07", "09:30"), "Lashes Coloring + Brows Coloring Price: €30.00 with Maria Test (Confirmed)"))
	src.Put(mariaCal, booking("yesterday", at("2026-11-06", "23:30"), "Wax Price: €10.00 with Maria Test (Confirmed)")) // overlaps the 7th
	src.Put(mariaCal, booking("tomorrow", at("2026-11-08", "09:00"), "Wax Price: €10.00 with Maria Test (Confirmed)"))
	src.Put(mariaCal, bookings.Event{ID: "busy", Summary: "Busy Time with Maria Test", Start: at("2026-11-07", "12:00")})
	src.Put(mariaCal, bookings.Event{ID: "own", Summary: "Репетитор", Start: at("2026-11-07", "13:00")})
	src.Put(mariaCal, booking("odd", at("2026-11-07", "14:00"), "Wax Price: €10,00 with Maria Test (Confirmed)"))

	day, err := newReader(t, src).Day(context.Background(), "maria", mustDate(t, "2026-11-07"))
	if err != nil {
		t.Fatal(err)
	}
	if len(day.Bookings) != 2 || day.Bookings[0].Ref.Event != "early" || day.Bookings[1].Ref.Event != "late" {
		t.Fatalf("bookings = %+v, want early then late", day.Bookings)
	}
	first := day.Bookings[0]
	if first.Ref.Calendar != mariaCal || first.Booking.Price != 3000 || !first.Start.Equal(at("2026-11-07", "09:30")) {
		t.Fatalf("first = %+v", first)
	}
	if day.Unreadable != 1 {
		t.Fatalf("unreadable = %d, want 1", day.Unreadable)
	}
}

func TestADayIsUnavailableWhenTheCalendarFails(t *testing.T) {
	src := bookings.NewMemory()
	src.Fail(errors.New("boom"))
	_, err := newReader(t, src).Day(context.Background(), "maria", mustDate(t, "2026-11-07"))
	if !errors.Is(err, bookings.ErrUnavailable) {
		t.Fatalf("err = %v, want ErrUnavailable", err)
	}
}

func TestAPersonWithoutACalendarHasNoBookings(t *testing.T) {
	r := newReader(t, bookings.NewMemory())
	if _, ok := r.CalendarFor("olga"); ok {
		t.Fatal("olga has no calendar")
	}
	if _, err := r.Day(context.Background(), "olga", mustDate(t, "2026-11-07")); !errors.Is(err, bookings.ErrNoCalendar) {
		t.Fatalf("err = %v, want ErrNoCalendar", err)
	}
}

func TestCalendarsAreMappedFromConfig(t *testing.T) {
	r, err := bookings.NewReader(bookings.NewMemory(), " a@x|maria ; b@x|olga ", brussels)
	if err != nil {
		t.Fatal(err)
	}
	if c, _ := r.CalendarFor("olga"); c != "b@x" {
		t.Fatalf("olga = %q", c)
	}
	for _, bad := range []string{"a@x", "a@x|maria;a@x|olga", "a@x|maria;b@x|maria", "|maria", "a@x|"} {
		if _, err := bookings.NewReader(bookings.NewMemory(), bad, brussels); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	off, err := bookings.NewReader(bookings.NewMemory(), "", brussels)
	if err != nil || off.Enabled() {
		t.Fatalf("empty config: enabled=%v err=%v", off.Enabled(), err)
	}
}

func TestABookingIsCheckedAgainstThePersonsOwnCalendar(t *testing.T) {
	src := bookings.NewMemory()
	src.Put(mariaCal, booking("b1", at("2026-11-07", "10:00"), "Wax Price: €10.00 with Maria Test (Confirmed)"))
	src.Put(mariaCal, bookings.Event{ID: "busy", Summary: "Busy Time with Maria Test", Start: at("2026-11-07", "12:00")})
	src.Put("other@x", booking("theirs", at("2026-11-07", "10:00"), "Wax Price: €10.00 with Olga Test (Confirmed)"))
	r := newReader(t, src)
	day := mustDate(t, "2026-11-07")

	got, err := r.Check(context.Background(), "maria", day, "b1")
	if err != nil || got.Ref != (domain.BookingRef{Calendar: mariaCal, Event: "b1"}) || got.Booking.Price != 1000 {
		t.Fatalf("check = %+v, %v", got, err)
	}
	for name, tc := range map[string]struct {
		uid, event string
		day        domain.Date
		want       error
	}{
		"forged id":        {"maria", "nope", day, bookings.ErrNotABooking},
		"another calendar": {"maria", "theirs", day, bookings.ErrNotABooking},
		"not a booking":    {"maria", "busy", day, bookings.ErrNotABooking},
		"another day":      {"maria", "b1", mustDate(t, "2026-11-08"), bookings.ErrNotABooking},
		"no calendar":      {"olga", "b1", day, bookings.ErrNoCalendar},
		"malformed id":     {"maria", "../x", day, bookings.ErrNotABooking},
	} {
		if _, err := r.Check(context.Background(), tc.uid, tc.day, tc.event); !errors.Is(err, tc.want) {
			t.Errorf("%s: err = %v, want %v", name, err, tc.want)
		}
	}
	src.Fail(errors.New("down"))
	if _, err := r.Check(context.Background(), "maria", day, "b1"); !errors.Is(err, bookings.ErrUnavailable) {
		t.Fatalf("outage: err = %v", err)
	}
}

func TestALinkedBookingMissingFromItsDayIsMovedDeletedChangedOrUnknown(t *testing.T) {
	src := bookings.NewMemory()
	src.Put(mariaCal, booking("moved", at("2026-11-12", "10:00"), "Wax Price: €10.00 with Maria Test (Confirmed)"))
	src.Put(mariaCal, bookings.Event{ID: "cancelled", Summary: "", Start: at("2026-11-07", "10:00"), Cancelled: true})
	src.Put(mariaCal, bookings.Event{ID: "changed", Summary: "Olga Example", Description: "Olga Example - Wax Price: €10.00 with Maria Test (Rescheduled)", Start: at("2026-11-07", "11:00")})
	r := newReader(t, src)
	day := mustDate(t, "2026-11-07")
	ref := func(id string) domain.BookingRef { return domain.BookingRef{Calendar: mariaCal, Event: id} }

	got := r.Statuses(context.Background(), day, []domain.BookingRef{ref("moved"), ref("cancelled"), ref("gone"), ref("changed")})
	want := map[string]bookings.LinkState{"moved": bookings.LinkMoved, "cancelled": bookings.LinkDeleted, "gone": bookings.LinkDeleted, "changed": bookings.LinkChanged}
	for id, state := range want {
		if got[ref(id)].State != state {
			t.Errorf("%s: state = %v, want %v", id, got[ref(id)].State, state)
		}
	}
	if got[ref("moved")].MovedTo != mustDate(t, "2026-11-12") {
		t.Errorf("moved to %v", got[ref("moved")].MovedTo)
	}

	src.Fail(errors.New("down"))
	got = r.Statuses(context.Background(), day, []domain.BookingRef{ref("moved")})
	if got[ref("moved")].State != bookings.LinkUnknown {
		t.Fatalf("outage: state = %v, want unknown (never deleted)", got[ref("moved")].State)
	}
}

func TestLinkChecksAreBounded(t *testing.T) {
	src := bookings.NewMemory()
	var refs []domain.BookingRef
	for i := range 25 {
		refs = append(refs, domain.BookingRef{Calendar: mariaCal, Event: "e" + string(rune('a'+i))})
	}
	got := newReader(t, src).Statuses(context.Background(), mustDate(t, "2026-11-07"), refs)
	unchecked := 0
	for _, s := range got {
		if s.State == bookings.LinkUnchecked {
			unchecked++
		}
	}
	if unchecked != 5 || src.Gets() != bookings.MaxLinkChecks {
		t.Fatalf("unchecked = %d, gets = %d; want 5 and %d", unchecked, src.Gets(), bookings.MaxLinkChecks)
	}
}

func TestALostShareOrRemappedCalendarIsNeverShownAsDeleted(t *testing.T) {
	src := bookings.NewMemory()
	src.Put(mariaCal, booking("here", at("2026-11-07", "10:00"), "Wax Price: €10.00 with Maria Test (Confirmed)"))
	r := newReader(t, src)
	day := mustDate(t, "2026-11-07")
	old := domain.BookingRef{Calendar: "old-cal@group.calendar.google.com", Event: "e1"}
	got := r.Statuses(context.Background(), day, []domain.BookingRef{old})
	if got[old].State != bookings.LinkUnknown {
		t.Fatalf("unmapped calendar: state = %v, want unknown", got[old].State)
	}

	src.FailLists(errors.New("forbidden")) // the share is gone: listing fails, gets say 404
	gone := domain.BookingRef{Calendar: mariaCal, Event: "missing"}
	got = r.Statuses(context.Background(), day, []domain.BookingRef{gone})
	if got[gone].State != bookings.LinkUnknown {
		t.Fatalf("unreadable calendar: state = %v, want unknown", got[gone].State)
	}
}

func TestATimelyEventWithoutATimeCountsAsUnreadable(t *testing.T) {
	src := bookings.NewMemory()
	ev := booking("allday", at("2026-11-07", "00:00"), "Wax Price: €10.00 with Maria Test (Confirmed)")
	ev.Start, ev.AllDay = time.Time{}, mustDate(t, "2026-11-07")
	src.Put(mariaCal, ev)
	src.Put(mariaCal, bookings.Event{ID: "holiday", Summary: "Выходной", AllDay: mustDate(t, "2026-11-07")})
	day, err := newReader(t, src).Day(context.Background(), "maria", mustDate(t, "2026-11-07"))
	if err != nil {
		t.Fatal(err)
	}
	if len(day.Bookings) != 0 || day.Unreadable != 1 {
		t.Fatalf("day = %+v, want one unreadable", day)
	}
}
