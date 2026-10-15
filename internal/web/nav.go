package web

import (
	"net/http"
	"strings"

	"github.com/lieranderl/aestheticlab-income-app/internal/store"
	"github.com/lieranderl/aestheticlab-income-app/internal/views"
)

// navFor is the bottom bar and user header/sidebar context for a person,
// with the item for current marked active.
func navFor(u store.User, current string) views.Nav {
	var items []views.NavItem
	switch {
	case u.Role == store.RoleAdmin && u.SubmitsReports:
		items = []views.NavItem{
			{Href: "/", Label: "Отчёт", Icon: "calendar-check"},
			{Href: "/admin/day", Label: "День", Icon: "calendar-days"},
			{Href: "/admin/month", Label: "Месяц", Icon: "calendar-range"},
			{Href: "/admin/kassa", Label: "Касса", Icon: "landmark"},
			{Href: "/more", Label: "Ещё", Icon: "ellipsis"},
		}
	case u.Role == store.RoleAdmin:
		items = []views.NavItem{
			{Href: "/admin/day", Label: "День", Icon: "calendar-days"},
			{Href: "/admin/month", Label: "Месяц", Icon: "calendar-range"},
			{Href: "/admin/kassa", Label: "Касса", Icon: "landmark"},
			{Href: "/admin/history", Label: "История", Icon: "history"},
			{Href: "/more", Label: "Ещё", Icon: "ellipsis"},
		}
	case u.SubmitsReports:
		items = []views.NavItem{
			{Href: "/", Label: "Сегодня", Icon: "calendar-check"},
			{Href: "/my", Label: "Мои записи", Icon: "wallet"},
			{Href: "/profile", Label: "Профиль", Icon: "user-round"},
		}
	default:
		items = []views.NavItem{
			{Href: "/my", Label: "Мои записи", Icon: "wallet"},
			{Href: "/profile", Label: "Профиль", Icon: "user-round"},
		}
	}
	active := activeHref(items, current)
	for i := range items {
		items[i].Active = items[i].Href == active
	}
	name := u.DisplayName
	if name == "" {
		name = u.Username
	}
	return views.Nav{
		Items: items,
		User: views.UserBadge{
			Name:      name,
			Username:  u.Username,
			RoleLabel: roleLabel(u.Role),
		},
	}
}

func (a *app) navFor(u store.User, current string) views.Nav {
	nav := navFor(u, current)
	nav.FinancialWritesPaused = a.store.FinancialWritesPaused()
	return nav
}

// activeHref picks the bar item a page belongs to: its own item, or «Ещё»
// for pages listed there.
func activeHref(items []views.NavItem, current string) string {
	best := ""
	for _, it := range items {
		if it.Href == current || (it.Href != "/" && strings.HasPrefix(current, it.Href+"/")) {
			if len(it.Href) > len(best) {
				best = it.Href
			}
		}
	}
	if best == "" && current != "" && current != "/" {
		for _, it := range items {
			if it.Href == "/more" {
				return "/more"
			}
		}
	}
	return best
}

// moreLinks is the admin's «Ещё» list.
func moreLinks() []views.NavItem {
	return []views.NavItem{
		{Href: "/admin/trends", Label: "Динамика", Icon: "chart-line"},
		{Href: "/admin/history", Label: "История", Icon: "history"},
		{Href: "/my", Label: "Мои записи", Icon: "wallet"},
		{Href: "/admin/categories", Label: "Категории", Icon: "tags"},
		{Href: "/admin/users", Label: "Сотрудники", Icon: "users"},
		{Href: "/admin/audit", Label: "Журнал изменений", Icon: "scroll-text"},
		{Href: "/admin/export", Label: "Экспорт", Icon: "file-down"},
		{Href: "/profile", Label: "Профиль", Icon: "user-round"},
	}
}

// handleHome is the daily form for people who report; the admin who
// doesn't report starts on the day screen, others on «Мои записи».
func (a *app) handleHome(w http.ResponseWriter, r *http.Request, u store.User) {
	switch {
	case u.SubmitsReports:
		a.handleClientsDay(w, r, u)
	case u.Role == store.RoleAdmin:
		http.Redirect(w, r, "/admin/day", http.StatusSeeOther)
	default:
		http.Redirect(w, r, "/my", http.StatusSeeOther)
	}
}

func (a *app) handleMore(w http.ResponseWriter, r *http.Request, u store.User) {
	a.render(w, r, views.MorePage(a.navFor(u, "/more"), moreLinks()))
}
