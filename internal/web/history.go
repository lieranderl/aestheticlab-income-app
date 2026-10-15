package web

import (
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
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

func (a *app) readHistoryFilter(r *http.Request) (historyFilter, bool) {
	today := a.store.Today()
	f := historyFilter{from: today.Month().FirstDay(), to: today, page: 1}
	q := r.URL.Query()
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
	p := views.HistoryFilters{Action: path, From: f.from.String(), To: f.to.String(), Person: f.person, Category: f.category, Entity: f.entity}
	p.Chips = append(p.Chips, views.FilterChip{Label: ru.Date(f.from.Time()) + " — " + ru.Date(f.to.Time()), Href: path})
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
	f, ok := a.readHistoryFilter(r)
	if !ok {
		http.Error(w, "Проверьте период: не больше года, начало не позже конца", http.StatusBadRequest)
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
		http.Error(w, "Проверьте сотрудника и категорию", http.StatusBadRequest)
		return
	}
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
