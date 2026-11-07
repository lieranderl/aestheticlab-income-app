// Package bookings reads Timely bookings from the staff's Google calendars,
// read-only and on demand (docs/plans/booking-sync.md). Nothing is stored:
// each screen asks for the day it shows. Client names and contact details
// pass through only as far as domain.ClassifyEvent and are never kept.
package bookings

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/lieranderl/aestheticlab-income-app/internal/domain"
)

var (
	// ErrUnavailable means a calendar could not be read completely in time.
	ErrUnavailable = errors.New("bookings unavailable")
	// ErrNoCalendar means the person has no mapped calendar.
	ErrNoCalendar = errors.New("no booking calendar")
	// ErrNotABooking means the event is missing, not a readable confirmed
	// booking, or not on the given day.
	ErrNotABooking = errors.New("not a booking")
)

// Event is one calendar event as the source returns it.
type Event struct {
	ID          string
	Summary     string
	Description string
	Start       time.Time   // zero for all-day events
	AllDay      domain.Date // the date of an all-day event
	Cancelled   bool
}

// Source is a calendar backend: Google in production, Memory in tests.
type Source interface {
	// List returns every event overlapping [from, to), all pages or an error.
	List(ctx context.Context, calendar string, from, to time.Time) ([]Event, error)
	// Get returns one event; found is false when it doesn't exist.
	Get(ctx context.Context, calendar, id string) (ev Event, found bool, err error)
}

// Item is one booking on a day.
type Item struct {
	Ref     domain.BookingRef
	Start   time.Time
	Booking domain.Booking
}

// Day is a person's bookings for one day.
type Day struct {
	Bookings   []Item
	Unreadable int // Timely-looking events that couldn't be read
}

// Timeout bounds one screen's calendar reads.
const Timeout = 3 * time.Second

// MaxLinkChecks bounds the extra event reads one screen makes.
const MaxLinkChecks = 20

// Reader maps people to their calendars and reads bookings for them.
type Reader struct {
	src      Source
	loc      *time.Location
	byPerson map[string]string
}

var eventID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,256}$`)

// NewReader parses BOOKING_CALENDARS ("<calendarId>|<uid>;…"). An empty
// config gives a reader with the feature off.
func NewReader(src Source, config string, loc *time.Location) (*Reader, error) {
	r := &Reader{src: src, loc: loc, byPerson: map[string]string{}}
	seen := map[string]bool{}
	for pair := range strings.SplitSeq(config, ";") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		cal, uid, ok := strings.Cut(pair, "|")
		cal, uid = strings.TrimSpace(cal), strings.TrimSpace(uid)
		if !ok || cal == "" || uid == "" {
			return nil, fmt.Errorf("booking calendars: %q is not <calendarId>|<uid>", pair)
		}
		if seen[cal] || r.byPerson[uid] != "" {
			return nil, fmt.Errorf("booking calendars: %q repeats a calendar or person", pair)
		}
		seen[cal], r.byPerson[uid] = true, cal
	}
	return r, nil
}

// Enabled says whether any calendar is mapped.
func (r *Reader) Enabled() bool { return r != nil && len(r.byPerson) > 0 }

// CalendarFor is the person's calendar.
func (r *Reader) CalendarFor(uid string) (string, bool) {
	if r == nil {
		return "", false
	}
	cal, ok := r.byPerson[uid]
	return cal, ok
}

// Day reads the person's bookings that start on day in Brussels, in start
// order. A failed or partial read is ErrUnavailable, never a short list.
func (r *Reader) Day(ctx context.Context, uid string, day domain.Date) (Day, error) {
	cal, ok := r.CalendarFor(uid)
	if !ok {
		return Day{}, ErrNoCalendar
	}
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	from, to := day.Window(r.loc)
	events, err := r.src.List(ctx, cal, from, to)
	if err != nil {
		return Day{}, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	var out Day
	for _, ev := range events {
		if ev.Cancelled {
			continue
		}
		c := domain.ClassifyEvent(ev.Summary, ev.Description)
		if ev.Start.IsZero() { // all day, or a time that didn't parse
			if c.Kind != domain.EventUnrelated && (ev.AllDay == day || ev.AllDay == domain.Date{}) {
				out.Unreadable++ // a Timely booking always has a time
			}
			continue
		}
		if domain.Today(ev.Start, r.loc) != day {
			continue
		}
		switch c.Kind {
		case domain.EventBooking:
			out.Bookings = append(out.Bookings, Item{Ref: domain.BookingRef{Calendar: cal, Event: ev.ID}, Start: ev.Start, Booking: c.Booking})
		case domain.EventUnreadable:
			out.Unreadable++
		}
	}
	slices.SortStableFunc(out.Bookings, func(a, b Item) int {
		return cmp.Or(a.Start.Compare(b.Start), strings.Compare(a.Ref.Event, b.Ref.Event))
	})
	return out, nil
}

// Check resolves an event ID against the person's own calendar before it is
// linked: it must exist, be a readable confirmed booking and start on day.
// Nothing about another calendar can pass, so a forged ID is refused.
func (r *Reader) Check(ctx context.Context, uid string, day domain.Date, id string) (Item, error) {
	cal, ok := r.CalendarFor(uid)
	if !ok {
		return Item{}, ErrNoCalendar
	}
	if !eventID.MatchString(id) {
		return Item{}, ErrNotABooking
	}
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	ev, found, err := r.src.Get(ctx, cal, id)
	if err != nil {
		return Item{}, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	if !found || ev.Cancelled || ev.Start.IsZero() || domain.Today(ev.Start, r.loc) != day {
		return Item{}, ErrNotABooking
	}
	c := domain.ClassifyEvent(ev.Summary, ev.Description)
	if c.Kind != domain.EventBooking {
		return Item{}, ErrNotABooking
	}
	return Item{Ref: domain.BookingRef{Calendar: cal, Event: id}, Start: ev.Start, Booking: c.Booking}, nil
}

// LinkState is what became of a linked booking that is missing from the day
// of its entry.
type LinkState int

const (
	LinkUnchecked LinkState = iota // over MaxLinkChecks: not looked at
	LinkUnknown                    // the calendar didn't answer: never shown as deleted
	LinkDeleted                    // confirmed: gone or cancelled
	LinkMoved                      // a booking on another day
	LinkChanged                    // still there, but no longer a readable booking
	LinkHere                       // still a booking on the entry's day
)

// LinkStatus is one linked booking's state.
type LinkStatus struct {
	State   LinkState
	MovedTo domain.Date
}

// Statuses looks up linked bookings that are absent from the entry's day,
// at most MaxLinkChecks of them, in parallel within one Timeout.
func (r *Reader) Statuses(ctx context.Context, day domain.Date, refs []domain.BookingRef) map[domain.BookingRef]LinkStatus {
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	out := make(map[domain.BookingRef]LinkStatus, len(refs))
	// A 404 means deleted only on a calendar we can still read: a lost share
	// or a remapped calendar is an outage, never a deletion.
	readable := map[string]bool{}
	from, to := day.Window(r.loc)
	for _, ref := range refs {
		if _, probed := readable[ref.Calendar]; !probed {
			readable[ref.Calendar] = false
			if r.mapped(ref.Calendar) {
				_, err := r.src.List(ctx, ref.Calendar, from, to)
				readable[ref.Calendar] = err == nil
			}
		}
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i, ref := range refs {
		if i >= MaxLinkChecks {
			mu.Lock()
			out[ref] = LinkStatus{State: LinkUnchecked}
			mu.Unlock()
			continue
		}
		if !readable[ref.Calendar] {
			mu.Lock()
			out[ref] = LinkStatus{State: LinkUnknown}
			mu.Unlock()
			continue
		}
		wg.Go(func() {
			s := r.status(ctx, day, ref)
			mu.Lock()
			out[ref] = s
			mu.Unlock()
		})
	}
	wg.Wait()
	return out
}

func (r *Reader) mapped(cal string) bool {
	for _, c := range r.byPerson {
		if c == cal {
			return true
		}
	}
	return false
}

func (r *Reader) status(ctx context.Context, day domain.Date, ref domain.BookingRef) LinkStatus {
	if !eventID.MatchString(ref.Event) {
		return LinkStatus{State: LinkChanged}
	}
	ev, found, err := r.src.Get(ctx, ref.Calendar, ref.Event)
	switch {
	case err != nil:
		return LinkStatus{State: LinkUnknown}
	case !found || ev.Cancelled:
		return LinkStatus{State: LinkDeleted}
	case ev.Start.IsZero() || domain.ClassifyEvent(ev.Summary, ev.Description).Kind != domain.EventBooking:
		return LinkStatus{State: LinkChanged}
	}
	if d := domain.Today(ev.Start, r.loc); d != day {
		return LinkStatus{State: LinkMoved, MovedTo: d}
	}
	return LinkStatus{State: LinkHere}
}
