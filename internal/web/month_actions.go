package web

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/lieranderl/aestheticlab-income-app/internal/domain"
	"github.com/lieranderl/aestheticlab-income-app/internal/store"
	"github.com/lieranderl/aestheticlab-income-app/internal/views"
)

func settlementFigures(result domain.MonthResult) []views.MoneyRow {
	return []views.MoneyRow{
		{Name: "Доход салона", Value: domain.FormatEUR(result.SalonIncome)},
		{Name: "Расходы", Value: domain.FormatEUR(result.Expenses)},
		{Name: "Зарплаты", Value: domain.FormatEUR(result.Salaries)},
		{Name: "Доплата «Себе»", Value: domain.FormatEUR(result.SelfBonuses)},
		{Name: "Прибыль", Value: domain.FormatEUR(result.Profit)},
		{Name: "Дивиденды", Value: domain.FormatEUR(result.DividendPool)},
	}
}

func (a *app) settlementBlockers(selected store.SettlementMonth, m domain.Month) []string {
	var blockers []string
	if selected.PreviewError != nil {
		blockers = append(blockers, settlementProblem(selected.PreviewError))
	}
	if selected.State.Status == domain.MonthClosed {
		return blockers
	}
	if selected.Pending > 0 {
		blockers = append(blockers, "Ожидают подтверждения: "+strconv.Itoa(selected.Pending)+" — откройте кассу")
	}
	if !m.Before(a.store.Today().Month()) {
		blockers = append(blockers, "Месяц ещё не закончился")
	}
	if !selected.PreviousClosed {
		blockers = append(blockers, "Сначала закройте предыдущий месяц")
	}
	return blockers
}

func (a *app) monthActionData(selected store.SettlementMonth, m domain.Month, reopen bool) views.MonthActionData {
	action := views.MonthActionData{Action: "/admin/month/" + m.String() + "/close", Back: settlementURL(m), Title: "Закрыть месяц", Revision: selected.Revision, Consequence: "Зарплаты, доплата «Себе» и дивиденды будут начислены. Отчёты и движения месяца станут недоступны для изменений."}
	if selected.PreviewError == nil {
		action.Figures = settlementFigures(selected.Result)
	}
	if reopen {
		action.Action = "/admin/month/" + m.String() + "/reopen"
		action.Title = "Открыть заново"
		action.Consequence = "Начисления зарплат, доплаты «Себе» и дивидендов будут отменены. Выплаты и внесения сохранятся. Изменения месяца снова станут доступны."
		action.Blocked = selected.State.Status != domain.MonthClosed || !selected.NewestClosed
	} else {
		blockers := a.settlementBlockers(selected, m)
		action.Blocked = selected.State.Status == domain.MonthClosed || len(blockers) > 0
		action.Error = strings.Join(blockers, ". ")
	}
	if action.Blocked && action.Error == "" {
		action.Error = "Проверьте порядок закрытия и состояние месяца — обновите расчёт"
	}
	return action
}

// monthConfirmation reads only the consistent selected-month calculation:
// no historical warnings, current payment balances, accounts or form catalogs.
func (a *app) monthConfirmation(r *http.Request, m domain.Month, reopen bool) (views.MonthActionData, error) {
	selected, err := a.store.Settlement(r.Context(), m)
	if err != nil {
		return views.MonthActionData{}, err
	}
	return a.monthActionData(selected, m, reopen), nil
}

func (a *app) handleMonthAction(w http.ResponseWriter, r *http.Request, u store.User) {
	m, err := domain.ParseMonth(r.PathValue("month"))
	if err != nil || m.Before(a.store.StartMonth()) || a.store.Today().Month().Before(m) {
		http.Error(w, "Проверьте месяц", http.StatusBadRequest)
		return
	}
	reopen := strings.HasSuffix(r.URL.Path, "/reopen")
	if r.Method == http.MethodGet {
		action, err := a.monthConfirmation(r, m, reopen)
		if err != nil {
			a.serverError(w, r, err)
			return
		}
		a.render(w, r, views.MonthActionPage(a.navFor(u, "/admin/month"), action))
		return
	}
	if err := readForm(w, r); err != nil {
		http.Error(w, "Проверьте поля формы", http.StatusBadRequest)
		return
	}
	revision := r.PostFormValue("revision")
	// A financial POST reaches the guarded transaction before preparing any view.
	if reopen {
		_, err = a.store.ReopenMonthGuarded(r.Context(), u.Actor(), m, revision)
	} else {
		_, err = a.store.CloseMonthGuarded(r.Context(), u.Actor(), m, revision)
	}
	if err == nil {
		a.navigate(w, r, settlementURL(m)+"&saved=1")
		return
	}
	action, readErr := a.monthConfirmation(r, m, reopen)
	if readErr != nil {
		a.serverError(w, r, readErr)
		return
	}
	action.Error = settlementProblem(err)
	action.Blocked = true
	status := http.StatusUnprocessableEntity
	if errors.Is(err, store.ErrVersionConflict) || errors.Is(err, store.ErrMonthClosed) || errors.Is(err, store.ErrFinancialWritesPaused) {
		status = http.StatusConflict
	}
	a.formFailedStatus(w, r, status, views.MonthActionForm(action), views.MonthActionPage(a.navFor(u, "/admin/month"), action))
}
