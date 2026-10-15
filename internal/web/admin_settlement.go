package web

import (
	"errors"
	"net/http"

	"github.com/lieranderl/aestheticlab-income-app/internal/domain"
	"github.com/lieranderl/aestheticlab-income-app/internal/ru"
	"github.com/lieranderl/aestheticlab-income-app/internal/store"
	"github.com/lieranderl/aestheticlab-income-app/internal/views"
)

func settlementURL(m domain.Month) string { return "/admin/month?m=" + m.String() + "&tab=settlement" }

func settlementProblem(err error) string {
	if errors.Is(err, store.ErrFinancialWritesPaused) {
		return "Запись финансовых операций временно приостановлена"
	}
	var v *domain.ValidationError
	if errors.As(err, &v) {
		switch v.Problem {
		case domain.ProblemMissingTerms:
			return "Не хватает условий сотрудника — проверьте условия за выбранный месяц"
		case domain.ProblemSelfRateMismatch:
			return "Ставка «Себе» в отчётах отличается от условий месяца — проверьте отчёты и условия"
		case domain.ProblemPendingMovements:
			return "Подтвердите или отмените движения в кассе перед закрытием"
		case store.ProblemMonthNotOver:
			return "Месяц ещё не закончился"
		}
	}
	if errors.Is(err, store.ErrVersionConflict) {
		return "Расчёт изменился — обновите страницу и проверьте суммы"
	}
	return "Проверьте порядок закрытия и состояние месяца — обновите страницу"
}

func (a *app) settlementPage(r *http.Request, m domain.Month) (views.SettlementData, error) {
	selected, err := a.store.Settlement(r.Context(), m)
	if err != nil {
		return views.SettlementData{}, err
	}
	users, err := a.store.Users(r.Context())
	if err != nil {
		return views.SettlementData{}, err
	}
	cats, err := a.store.Categories(r.Context())
	if err != nil {
		return views.SettlementData{}, err
	}
	p := views.SettlementData{Key: m.String(), Month: monthLabel(m), AsOf: ru.Date(a.store.Today().Time()), Closed: selected.State.Status == domain.MonthClosed, Blockers: a.settlementBlockers(selected, m)}
	if !m.Prev().Before(a.store.StartMonth()) {
		p.Prev = settlementURL(m.Prev())
	}
	if !a.store.Today().Month().Before(m.Next()) {
		p.Next = settlementURL(m.Next())
	}
	result := selected.Result
	if selected.PreviewError == nil {
		p.Figures = settlementFigures(result)
		p.Expenses = categoryRows(cats, store.KindExpense, result.ExpensesByCategory)
		for _, u := range users {
			pr, ok := result.People[u.ID]
			if !ok {
				continue
			}
			b := selected.Breakdowns[u.ID]
			current, err := a.store.PersonBalance(r.Context(), u.ID)
			if err != nil {
				return p, err
			}
			c := views.SettlementPerson{Name: u.DisplayName, Balance: balanceWords(pr.Closing), Current: balanceWords(current), Earned: domain.FormatEUR(pr.Earned), Sign: sign(pr.Closing), CurrentSign: sign(current)}
			c.Breakdown = []views.MoneyRow{{Name: "Начальный остаток", Value: signedMoney(b.Opening)}, {Name: "Отчёты · изменение баланса", Value: signedMoney(b.Reports)}, {Name: "Внесения", Value: signedMoney(b.Deposits)}, {Name: "Расход за свой счёт", Value: signedMoney(b.StaffExpenses)}, {Name: "Зарплата", Value: signedMoney(b.Salary)}, {Name: "Доплата «Себе»", Value: signedMoney(b.SelfBonus)}, {Name: "Дивиденды", Value: signedMoney(b.Dividend)}, {Name: "Выплаты", Value: signedMoney(b.Payouts)}, {Name: "Итоговый баланс", Value: signedMoney(b.Closing)}}
			action := domain.SuggestAction(current)
			if action.Kind != domain.Settled {
				kind := "payout"
				c.Action = "Выплатить " + domain.FormatEUR(action.Amount)
				if action.Kind == domain.PersonPays {
					kind = "deposit"
					c.Action = "Внести в кассу " + domain.FormatEUR(action.Amount)
				}
				c.ActionURL = "/admin/movements/new?type=" + kind + "&settlement=" + m.String() + "&person=" + u.ID
			}
			c.CarryURL = settlementURL(m) + "&carry=1"
			p.People = append(p.People, c)
		}
	}
	action := a.monthActionData(selected, m, p.Closed)
	if !action.Blocked {
		p.Action = action
	}
	if r.URL.Query().Get("carry") == "1" {
		p.Notice = "Баланс переносится на следующий месяц. Выплата или внесение не записаны."
	}
	if r.URL.Query().Get("saved") == "1" {
		p.Notice = "Сохранено ✓"
	}
	// Historical gaps are warnings only, based on each elapsed day's audited roster.
	reports, err := a.store.ReportsIn(r.Context(), m)
	if err != nil {
		return p, err
	}
	lastElapsed := m.LastDay()
	if a.store.Today().Before(lastElapsed) {
		lastElapsed = a.store.Today()
	}
	rosters, err := a.store.ExpectedReportersBetween(r.Context(), m.FirstDay(), lastElapsed)
	if err != nil {
		return p, err
	}
	for day := m.FirstDay(); !lastElapsed.Before(day); day = day.AddDays(1) {
		expected := rosters[day]
		var rs []domain.PersonReport
		for _, rep := range reports {
			if rep.Date == day {
				rs = append(rs, rep.PersonReport)
			}
		}
		for _, id := range domain.MissingReporters(expected, rs) {
			p.Missing = append(p.Missing, views.SettlementWarning{Text: ru.Date(day.Time()) + " · " + personName(users, id), Href: "/admin/reports/" + id + "/" + day.String()})
		}
	}
	accounts, err := a.store.Accounts(r.Context())
	if err != nil {
		return p, err
	}
	for _, acc := range accounts {
		if !acc.Active || acc.Kind != store.AccountCash {
			continue
		}
		if acc.LastRecountAt.IsZero() || domain.Today(a.store.LocalTime(acc.LastRecountAt), a.store.LocalTime(acc.LastRecountAt).Location()).Before(a.store.Today().AddDays(-7)) {
			p.Warnings = append(p.Warnings, views.SettlementWarning{Text: "Пересчёта наличных нет или он старше семи дней", Href: "/admin/accounts/" + acc.ID + "/recount"})
		}
	}
	p.MovementForm, err = a.movementForm(r, store.User{}, true, "deposit")
	if err != nil {
		return p, err
	}
	return p, nil
}

func balanceWords(c domain.Cents) string {
	if c > 0 {
		return "Салон должен вам " + domain.FormatEUR(c)
	}
	if c < 0 {
		return "Вы должны салону " + domain.FormatEUR(-c)
	}
	return "Расчёт: 0"
}
func signedMoney(c domain.Cents) string {
	if c > 0 {
		return "+" + domain.FormatEUR(c)
	}
	return domain.FormatEUR(c)
}

func (a *app) handleAdminSettlement(w http.ResponseWriter, r *http.Request, u store.User) {
	m, err := domain.ParseMonth(r.URL.Query().Get("m"))
	if r.URL.Query().Get("m") == "" {
		m = a.store.Today().Month()
		err = nil
	}
	if err != nil || m.Before(a.store.StartMonth()) || a.store.Today().Month().Before(m) {
		http.Error(w, "Проверьте месяц", http.StatusBadRequest)
		return
	}
	p, err := a.settlementPage(r, m)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	a.render(w, r, views.SettlementPage(a.navFor(u, "/admin/month"), p))
}
