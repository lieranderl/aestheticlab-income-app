package web

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/lieranderl/aestheticlab-income-app/internal/bookings"
	"github.com/lieranderl/aestheticlab-income-app/internal/domain"
	"github.com/lieranderl/aestheticlab-income-app/internal/ru"
	"github.com/lieranderl/aestheticlab-income-app/internal/store"
	"github.com/lieranderl/aestheticlab-income-app/internal/views"
)

// personBookings is what one person's booking section needs.
type personBookings struct {
	day   bookings.Day
	err   error
	links map[domain.BookingRef]store.BookingLink
}

// readBookings reads the day's bookings and their links. A calendar error
// leaves err set; the screen then says the bookings are unavailable.
func (a *app) readBookings(ctx context.Context, uid string, day domain.Date) personBookings {
	var pb personBookings
	pb.day, pb.err = a.cfg.Bookings.Day(ctx, uid, day)
	if pb.err != nil {
		if !errors.Is(pb.err, bookings.ErrNoCalendar) {
			slog.WarnContext(ctx, "bookings unavailable", "error", pb.err) // never event text
		}
		return pb
	}
	refs := make([]domain.BookingRef, 0, len(pb.day.Bookings))
	for _, b := range pb.day.Bookings {
		refs = append(refs, b.Ref)
	}
	pb.links, pb.err = a.store.BookingLinks(ctx, refs)
	return pb
}

// bookingsView builds one person's section. entries are the person's clients
// on the day; admin adds counts and notes on linked bookings missing from it.
func (a *app) bookingsView(ctx context.Context, actor store.User, uid string, day domain.Date, entries []store.SavedReport, locked, admin bool) views.BookingsView {
	if _, ok := a.cfg.Bookings.CalendarFor(uid); !ok {
		return views.BookingsView{}
	}
	pb := a.readBookings(ctx, uid, day)
	v := views.BookingsView{Enabled: true, Locked: locked}
	if pb.err != nil {
		v.Unavailable = true
		return v
	}
	v.Unreadable = pb.day.Unreadable
	var unlinked []store.SavedReport
	linkedHere := map[domain.BookingRef]bool{}
	for _, b := range pb.day.Bookings {
		linkedHere[b.Ref] = true
	}
	var missing []domain.BookingRef
	for _, e := range entries {
		if !e.Client || e.ClientInput == nil {
			continue
		}
		switch b := e.ClientInput.Booking; {
		case b == nil:
			unlinked = append(unlinked, e)
		case !linkedHere[*b]:
			missing = append(missing, *b)
		}
	}
	notLinked := 0
	for _, b := range pb.day.Bookings {
		c := views.BookingCard{Time: b.Start.In(a.store.Location()).Format("15:04"), Services: strings.Join(b.Booking.Services, " + "), Event: b.Ref.Event, Price: "Цена в записи не указана"}
		if b.Booking.PriceKnown {
			c.Price = "по записи " + domain.FormatEUR(b.Booking.Price)
		}
		if link, ok := pb.links[b.Ref]; ok {
			c.Linked = true
			c.EntryHref = "/clients/" + link.ReportID
			if link.Date != day {
				c.Elsewhere = "Связано с клиентом от " + ru.Date(link.Date.Time())
			}
		} else {
			notLinked++
			if actor.ID == uid && actor.SubmitsReports {
				c.NewHref = "/clients/new?date=" + day.String() + "&booking=" + url.QueryEscape(b.Ref.Event)
			}
			for _, e := range unlinked {
				c.Candidates = append(c.Candidates, views.LinkCandidate{Action: "/clients/" + e.ID + "/link", Label: "Клиент " + strconv.FormatInt(e.ClientNumber, 10) + " · " + domain.FormatEUR(e.Report.Total), Version: e.Version})
			}
		}
		v.Cards = append(v.Cards, c)
	}
	if admin {
		v.Counts = "Записей: " + strconv.Itoa(len(pb.day.Bookings)) + " · Клиентов: " + strconv.Itoa(countClients(entries)) + " · Записей без клиента: " + strconv.Itoa(notLinked) + " · Клиентов без записи: " + strconv.Itoa(len(unlinked))
	}
	if len(missing) > 0 {
		statuses := a.cfg.Bookings.Statuses(ctx, day, missing)
		unchecked := 0
		for _, ref := range missing {
			switch s := statuses[ref]; s.State {
			case bookings.LinkMoved:
				v.Notes = append(v.Notes, "Связанная запись перенесена на "+ru.Date(s.MovedTo.Time()))
			case bookings.LinkDeleted:
				v.Notes = append(v.Notes, "Связанная запись удалена в календаре")
			case bookings.LinkChanged:
				v.Notes = append(v.Notes, "Связанная запись изменена и не читается")
			case bookings.LinkUnknown:
				v.Notes = append(v.Notes, "Не удалось проверить связанную запись")
			case bookings.LinkUnchecked:
				unchecked++
			}
		}
		if unchecked > 0 {
			v.Notes = append(v.Notes, "Не проверено связанных записей: "+strconv.Itoa(unchecked))
		}
	}
	return v
}

func countClients(entries []store.SavedReport) int {
	n := 0
	for _, e := range entries {
		if e.Client {
			n++
		}
	}
	return n
}

// adminBookings is every mapped person's section for the admin day, read in
// parallel within the reader's timeout.
func (a *app) adminBookings(ctx context.Context, u store.User, users []store.User, day domain.Date, reports []store.SavedReport, locked bool) []views.BookingsView {
	if !a.cfg.Bookings.Enabled() {
		return nil
	}
	var people []store.User
	for _, p := range users {
		if _, ok := a.cfg.Bookings.CalendarFor(p.ID); ok && !p.Removed {
			people = append(people, p)
		}
	}
	out := make([]views.BookingsView, len(people))
	var wg sync.WaitGroup
	for i, p := range people {
		wg.Go(func() {
			var own []store.SavedReport
			for _, rep := range reports {
				if rep.Person == p.ID {
					own = append(own, rep)
				}
			}
			v := a.bookingsView(ctx, u, p.ID, day, own, locked, true)
			v.Name = p.DisplayName
			out[i] = v
		})
	}
	wg.Wait()
	return slices.DeleteFunc(out, func(v views.BookingsView) bool { return !v.Enabled })
}

// handleLinkClient links an entry to one of its person's bookings that day.
// A repeat of the link it already has is answered by the store alone.
func (a *app) handleLinkClient(w http.ResponseWriter, r *http.Request, u store.User) {
	if err := readForm(w, r); err != nil {
		http.Error(w, "Слишком большой запрос", http.StatusRequestEntityTooLarge)
		return
	}
	rep, err := a.store.ClientReport(r.Context(), u.Actor(), r.PathValue("id"))
	if err != nil {
		a.clientError(w, r, u, err)
		return
	}
	version, err := strconv.ParseInt(r.PostFormValue("version"), 10, 64)
	if err != nil {
		version = -1
	}
	event := r.PostFormValue("booking")
	cal, ok := a.cfg.Bookings.CalendarFor(rep.Person)
	if !ok {
		http.NotFound(w, r)
		return
	}
	ref := domain.BookingRef{Calendar: cal, Event: event}
	if b := rep.ClientInput.Booking; b == nil || *b != ref {
		// Fast refusals before asking Calendar; the store checks them all
		// again inside its transaction.
		if err := a.linkPrecheck(r.Context(), rep, version); err != nil {
			a.linkFailed(w, r, u, rep, err)
			return
		}
		if _, err := a.cfg.Bookings.Check(r.Context(), rep.Person, rep.Date, event); err != nil {
			a.linkFailed(w, r, u, rep, err)
			return
		}
	}
	if err := a.store.LinkClientBooking(r.Context(), u.Actor(), rep.ID, version, ref); err != nil {
		a.linkFailed(w, r, u, rep, err)
		return
	}
	a.navigate(w, r, dayURL(u, rep))
}

// handleUnlinkClient removes an entry's booking link; it never asks Calendar.
func (a *app) handleUnlinkClient(w http.ResponseWriter, r *http.Request, u store.User) {
	if err := readForm(w, r); err != nil {
		http.Error(w, "Слишком большой запрос", http.StatusRequestEntityTooLarge)
		return
	}
	rep, err := a.store.ClientReport(r.Context(), u.Actor(), r.PathValue("id"))
	if err != nil {
		a.clientError(w, r, u, err)
		return
	}
	version, err := strconv.ParseInt(r.PostFormValue("version"), 10, 64)
	if err != nil {
		version = -1
	}
	if err := a.store.UnlinkClientBooking(r.Context(), u.Actor(), rep.ID, version); err != nil {
		a.linkFailed(w, r, u, rep, err)
		return
	}
	a.navigate(w, r, "/clients/"+rep.ID)
}

func (a *app) linkPrecheck(ctx context.Context, rep store.SavedReport, version int64) error {
	if rep.ClientInput.Booking != nil {
		return store.ErrInvalid // unlink the other booking first
	}
	if m, err := a.store.Month(ctx, rep.Date.Month()); err == nil && m.Status == domain.MonthClosed {
		return store.ErrMonthClosed
	}
	if rep.Version != version {
		return store.ErrVersionConflict
	}
	return nil
}

func dayURL(u store.User, rep store.SavedReport) string {
	if rep.Person != u.ID {
		return "/admin/day?date=" + rep.Date.String()
	}
	return "/?date=" + rep.Date.String()
}

func (a *app) linkFailed(w http.ResponseWriter, r *http.Request, u store.User, rep store.SavedReport, err error) {
	var msg string
	switch {
	case errors.Is(err, store.ErrForbidden), errors.Is(err, store.ErrNotFound):
		a.clientError(w, r, u, err)
		return
	case errors.Is(err, bookings.ErrUnavailable):
		msg = "Записи недоступны — календарь не ответил. Попробуйте позже."
	case errors.Is(err, bookings.ErrNotABooking), errors.Is(err, bookings.ErrNoCalendar):
		msg = "Запись не найдена в календаре на этот день."
	case errors.Is(err, store.ErrBookingTaken):
		msg = "Эта запись уже связана с другим клиентом."
	case errors.Is(err, store.ErrInvalid):
		msg = "Клиент уже связан с другой записью — сначала отвяжите её."
	case errors.Is(err, store.ErrVersionConflict):
		msg = "Клиент изменён — обновите страницу и повторите."
	case errors.Is(err, store.ErrMonthClosed):
		msg = "Месяц закрыт — изменения недоступны"
	case errors.Is(err, store.ErrFinancialWritesPaused):
		msg = "Запись финансовых операций временно приостановлена"
	default:
		a.serverError(w, r, err)
		return
	}
	a.renderStatus(w, r, http.StatusUnprocessableEntity, views.LinkFailedPage(a.navFor(u, "/"), msg, dayURL(u, rep)))
}
