package web

import (
	"github.com/lieranderl/aestheticlab-income-app/internal/domain"
	"github.com/lieranderl/aestheticlab-income-app/internal/store"
	"github.com/lieranderl/aestheticlab-income-app/internal/views"
	"net/http"
	"slices"
)

func (a *app) handleMovementHistory(w http.ResponseWriter, r *http.Request, u store.User) {
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
	filters, ok := f.view("/admin/history/movements", users, nil)
	if !ok || f.category != "" || f.entity != "" {
		http.Error(w, "Проверьте сотрудника и фильтры", http.StatusBadRequest)
		return
	}
	filters.Movements = true
	filters.Type, filters.Status = f.kind, f.status
	types := []domain.MovementType{domain.Deposit, domain.Payout, domain.Expense, domain.Transfer, domain.Adjustment, domain.Opening, domain.Salary, domain.SelfBonus, domain.Dividend}
	validType, validStatus := f.kind == "", f.status == ""
	for _, k := range types {
		filters.Types = append(filters.Types, views.FilterOption{Value: string(k), Label: movementTitle(k)})
		if f.kind == string(k) {
			validType = true
			q := f.values()
			q.Del("type")
			filters.Chips = append(filters.Chips, views.FilterChip{Label: movementTitle(k), Href: "/admin/history/movements?" + q.Encode()})
		}
	}
	for _, s := range []domain.MovementStatus{domain.Pending, domain.Confirmed, domain.Voided} {
		filters.Statuses = append(filters.Statuses, views.FilterOption{Value: string(s), Label: movementStatus(s)})
		if f.status == string(s) {
			validStatus = true
			q := f.values()
			q.Del("status")
			filters.Chips = append(filters.Chips, views.FilterChip{Label: movementStatus(s), Href: "/admin/history/movements?" + q.Encode()})
		}
	}
	if !validType || !validStatus {
		http.Error(w, "Проверьте вид и статус движения", http.StatusBadRequest)
		return
	}
	es, err := a.store.LedgerBetween(r.Context(), f.from, f.to)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	es = slices.DeleteFunc(es, func(e store.Entry) bool {
		return (f.person != "" && e.PersonID != f.person) || (f.kind != "" && string(e.Type) != f.kind) || (f.status != "" && string(e.Status) != f.status)
	})
	slices.Reverse(es)
	paging, first, last := f.paging("/admin/history/movements", len(es))
	cards, err := a.movementCards(r, u, es[first:last], true)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	a.render(w, r, views.MovementHistoryPage(a.navFor(u, "/admin/history"), views.MovementHistoryData{Filters: filters, Paging: paging, Cards: cards}))
}
