package web

import (
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/lieranderl/aestheticlab-income-app/internal/domain"
	"github.com/lieranderl/aestheticlab-income-app/internal/ru"
	"github.com/lieranderl/aestheticlab-income-app/internal/store"
	"github.com/lieranderl/aestheticlab-income-app/internal/views"
)

type historyFilter struct {
	from, to                 domain.Date
	person, category, entity string
	kind, status             string
	page                     int
}

// filterPage is one filtered list: «Записи», «Движения» or the change log.
// Each remembers the filter last used on it (2026-11-16 request), in a
// cookie tied to the person, so opening it from the menu shows that filter.
type filterPage struct {
	path, cookie string
	own          []string // its own filter keys besides the period and person
}

var (
	recordsPage   = filterPage{path: "/admin/history", cookie: "kassa_filter_records", own: []string{"category"}}
	movementsPage = filterPage{path: "/admin/history/movements", cookie: "kassa_filter_movements", own: []string{"type", "status"}}
	auditPage     = filterPage{path: "/admin/audit", cookie: "kassa_filter_audit", own: []string{"entity"}}
)

// sharedFilterKeys carry over between «Записи» and «Движения».
var sharedFilterKeys = []string{"from", "to", "person"}

const filterCookieAge = 90 * 24 * 60 * 60

// remembered is the filter u last used on p, limited to p's keys.
func (p filterPage) remembered(r *http.Request, u store.User) url.Values {
	q := url.Values{}
	c, err := r.Cookie(p.cookie)
	if err != nil {
		return q
	}
	uid, raw, ok := strings.Cut(c.Value, "|")
	if !ok || uid != u.ID {
		return q
	}
	saved, err := url.ParseQuery(raw)
	if err != nil {
		return q
	}
	for _, k := range append(slices.Clone(sharedFilterKeys), p.own...) {
		if v := saved.Get(k); v != "" {
			q.Set(k, v)
		}
	}
	return q
}

// readFilter reads p's filter from the address, or, when p is opened
// without one (menu, bottom bar), the filter last used on it.
func (a *app) readFilter(r *http.Request, u store.User, p filterPage) (historyFilter, bool) {
	q := r.URL.Query()
	if r.URL.RawQuery == "" {
		q = p.remembered(r, u)
	}
	return a.readHistoryFilter(q)
}

// rememberFilter keeps f as the filter last used on p. A period equal to
// the default (this month so far) is left out so it moves on with the
// calendar; a filter with nothing set forgets the cookie.
func (a *app) rememberFilter(w http.ResponseWriter, r *http.Request, u store.User, p filterPage, f historyFilter) {
	today := a.store.Today()
	all := f.values()
	q := url.Values{}
	for _, k := range append(slices.Clone(sharedFilterKeys), p.own...) {
		if v := all.Get(k); v != "" {
			q.Set(k, v)
		}
	}
	if f.from == today.Month().FirstDay() && f.to == today {
		q.Del("from")
		q.Del("to")
	}
	if len(q) == 0 {
		if _, err := r.Cookie(p.cookie); err == nil {
			a.forgetFilter(w, p)
		}
		return
	}
	value := u.ID + "|" + q.Encode()
	if c, err := r.Cookie(p.cookie); err == nil && c.Value == value {
		return
	}
	http.SetCookie(w, &http.Cookie{ // #nosec G124 -- Secure is set whenever the app is served over HTTPS (always in production)
		Name: p.cookie, Value: value, Path: "/admin", MaxAge: filterCookieAge,
		HttpOnly: true, Secure: a.secure(), SameSite: http.SameSiteLaxMode,
	})
}

func (a *app) forgetFilter(w http.ResponseWriter, p filterPage) {
	http.SetCookie(w, &http.Cookie{ // #nosec G124 -- Secure is set whenever the app is served over HTTPS (always in production)
		Name: p.cookie, Value: "", Path: "/admin", MaxAge: -1,
		HttpOnly: true, Secure: a.secure(), SameSite: http.SameSiteLaxMode,
	})
}

// badFilter answers a filter the page can't show and forgets it, so a
// remembered filter that went stale doesn't stick.
func (a *app) badFilter(w http.ResponseWriter, r *http.Request, p filterPage, message string) {
	a.forgetFilter(w, p)
	a.problem(w, r, http.StatusBadRequest, message)
}

// tabHref opens the other history tab with the same period and person,
// plus that tab's own filter last used.
func (a *app) tabHref(r *http.Request, u store.User, to filterPage, f historyFilter) string {
	q := to.remembered(r, u)
	all := f.values()
	for _, k := range sharedFilterKeys {
		q.Del(k)
		if v := all.Get(k); v != "" {
			q.Set(k, v)
		}
	}
	return to.path + "?" + q.Encode()
}

func (a *app) readHistoryFilter(q url.Values) (historyFilter, bool) {
	today := a.store.Today()
	f := historyFilter{from: today.Month().FirstDay(), to: today, page: 1}
	var err error
	if q.Get("from") != "" {
		f.from, err = domain.ParseDate(q.Get("from"))
		if err != nil {
			return f, false
		}
	}
	if q.Get("to") != "" {
		f.to, err = domain.ParseDate(q.Get("to"))
		if err != nil {
			return f, false
		}
	}
	if f.to.Before(f.from) || f.to.Time().Sub(f.from.Time()) > 366*24*time.Hour {
		return f, false
	}
	f.person, f.category, f.entity = q.Get("person"), q.Get("category"), q.Get("entity")
	f.kind, f.status = q.Get("type"), q.Get("status")
	if q.Get("page") != "" {
		f.page, err = strconv.Atoi(q.Get("page"))
		if err != nil || f.page < 1 || f.page > 1000000 {
			return f, false
		}
	}
	return f, true
}

func (f historyFilter) values() url.Values {
	q := url.Values{"from": {f.from.String()}, "to": {f.to.String()}}
	if f.person != "" {
		q.Set("person", f.person)
	}
	if f.category != "" {
		q.Set("category", f.category)
	}
	if f.entity != "" {
		q.Set("entity", f.entity)
	}
	if f.kind != "" {
		q.Set("type", f.kind)
	}
	if f.status != "" {
		q.Set("status", f.status)
	}
	return q
}

func (f historyFilter) view(path string, users []store.User, cats []store.Category) (views.HistoryFilters, bool) {
	p := views.HistoryFilters{Action: path, Reset: path + "?reset=1", From: f.from.String(), To: f.to.String(), Person: f.person, Category: f.category, Entity: f.entity}
	period := f.values()
	period.Del("from")
	period.Del("to")
	if len(period) == 0 {
		period.Set("reset", "1") // an address without a query would bring the remembered filter back
	}
	p.Chips = append(p.Chips, views.FilterChip{Label: ru.Date(f.from.Time()) + " — " + ru.Date(f.to.Time()), Href: path + "?" + period.Encode()})
	validPerson, validCategory := f.person == "", f.category == ""
	for _, u := range users {
		p.People = append(p.People, views.FilterOption{Value: u.ID, Label: u.DisplayName})
		if u.ID == f.person {
			validPerson = true
			q := f.values()
			q.Del("person")
			p.Chips = append(p.Chips, views.FilterChip{Label: u.DisplayName, Href: path + "?" + q.Encode()})
		}
	}
	for _, c := range cats {
		if c.Kind != store.KindRevenue {
			continue
		}
		p.Categories = append(p.Categories, views.FilterOption{Value: c.ID, Label: c.Name})
		if c.ID == f.category {
			validCategory = true
			q := f.values()
			q.Del("category")
			p.Chips = append(p.Chips, views.FilterChip{Label: c.Name, Href: path + "?" + q.Encode()})
		}
	}
	return p, validPerson && validCategory
}

func (f historyFilter) paging(path string, count int) (views.Paging, int, int) {
	pages := max(1, (count+49)/50)
	page := min(f.page, pages)
	start := (page - 1) * 50
	end := min(start+50, count)
	p := views.Paging{}
	if count > 0 {
		p.Label = fmt.Sprintf("Записи %d–%d из %d", start+1, end, count)
	}
	q := f.values()
	if page > 1 {
		q.Set("page", strconv.Itoa(page-1))
		p.Prev = path + "?" + q.Encode()
	}
	if end < count {
		q.Set("page", strconv.Itoa(page+1))
		p.Next = path + "?" + q.Encode()
	}
	return p, start, end
}

func (a *app) handleHistory(w http.ResponseWriter, r *http.Request, u store.User) {
	f, ok := a.readFilter(r, u, recordsPage)
	if !ok {
		a.badFilter(w, r, recordsPage, "Проверьте период: не больше года, начало не позже конца")
		return
	}
	users, err := a.store.Users(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	cats, err := a.store.Categories(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	filters, ok := f.view("/admin/history", users, cats)
	if !ok {
		a.badFilter(w, r, recordsPage, "Проверьте сотрудника и категорию")
		return
	}
	a.rememberFilter(w, r, u, recordsPage, f)
	filters.RecordsHref, filters.MovementsHref = recordsPage.path, a.tabHref(r, u, movementsPage, f)
	reports, err := a.store.ReportsBetween(r.Context(), f.from, f.to)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	reports = slices.DeleteFunc(reports, func(rep store.SavedReport) bool {
		return (f.person != "" && rep.Person != f.person) || (f.category != "" && rep.Revenue[f.category] == 0)
	})
	slices.Reverse(reports) // date and person are stable, including across pages
	paging, first, last := f.paging("/admin/history", len(reports))
	p := views.ReportHistory{Filters: filters, Paging: paging}
	for _, rep := range reports[first:last] {
		p.Reports = append(p.Reports, reportCard(rep, users, cats))
	}
	a.render(w, r, views.ReportHistoryPage(a.navFor(u, "/admin/history"), p))
}
