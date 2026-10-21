package web

import (
	"net/http"
	"strconv"

	"github.com/lieranderl/aestheticlab-income-app/internal/domain"
	"github.com/lieranderl/aestheticlab-income-app/internal/ru"
	"github.com/lieranderl/aestheticlab-income-app/internal/store"
	"github.com/lieranderl/aestheticlab-income-app/internal/views"
)

// handleMy is «Мои записи»: the person's own month only (system-design §6.4).
func (a *app) handleMy(w http.ResponseWriter, r *http.Request, u store.User) {
	current, start := a.store.Today().Month(), a.store.StartMonth()
	m, err := domain.ParseMonth(r.URL.Query().Get("m"))
	if err != nil || current.Before(m) {
		m = current
	}
	if m.Before(start) {
		m = start
	}
	reports, err := a.store.PersonReportsIn(r.Context(), u.ID, m)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	entries, err := a.store.Terms(r.Context(), u.ID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	sum := domain.Summarize(personReports(reports))
	page := views.MyMonth{
		Month: monthLabel(m),
		Total: domain.FormatEUR(sum.Total), Self: domain.FormatEUR(sum.Self), Salon: domain.FormatEUR(sum.SalonShare),
		WorkingDays: strconv.Itoa(sum.People[u.ID].WorkingDays), Details: summaryFigures(sum, false),
	}
	if len(reports) > 0 {
		page.Split = selfSplit(sum, "месяц")
	}
	if prev := m.Prev(); !prev.Before(start) {
		page.Prev = "/my?m=" + prev.String()
	}
	if next := m.Next(); !current.Before(next) {
		page.Next = "/my?m=" + next.String()
	}
	own, status, err := a.store.OwnMonthResult(r.Context(), u.ID, m)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if status == domain.MonthClosed {
		page.Details = nil
		if own.StatsVersion >= 1 {
			page.Details = append(clientSummary(domain.Summary{Clients: own.Clients, Total: own.Revenue, ByCategory: own.RevenueByCategory, CertificateSales: own.RevenueByCategory["certificates"], KassaCash: own.KassaCash, KassaCard: own.KassaCard}),
				views.MoneyRow{Name: "«Себе»", Value: domain.FormatEUR(own.Self)},
				views.MoneyRow{Name: "«Салону»", Value: domain.FormatEUR(own.SalonShare())},
				views.MoneyRow{Name: "Рабочие дни", Value: strconv.Itoa(own.WorkingDays)},
			)
		}
		page.ClosedFigures = []views.MoneyRow{{Name: "Зарплата", Value: domain.FormatEUR(own.Salary)}, {Name: "Доплата «Себе»", Value: domain.FormatEUR(own.SelfBonus)}, {Name: "Дивиденды", Value: domain.FormatEUR(own.Dividend)}, {Name: "Всего заработано", Value: domain.FormatEUR(own.Earned)}}
	}
	if terms, ok := history(entries).For(m); ok && status != domain.MonthClosed {
		if p, ok := domain.SelfProgressFor(sum.SelfBase, terms); ok {
			page.Progress, page.Reached = progressText(p, baseLabel(sum), anyHandSet(reports)), p.Reached
			if p.Reached && page.Split.Self != "" {
				page.Split.Note = "Без доплаты за ставку " + percent(p.HighPct) + ": она начислится при закрытии месяца."
			}
		}
	}
	for i := len(reports) - 1; i >= 0; i-- { // newest first
		rep := reports[i]
		card := views.MyReport{
			Day: ru.DayWithWeekday(rep.Date.Time()) + " · " + clientLabel(rep), Total: domain.FormatEUR(rep.Report.Total),
			Self: domain.FormatEUR(rep.Report.SelfAmount), Salon: domain.FormatEUR(rep.Report.SalonShare),
			Effect: clientEffect(rep), Explain: selfEquation(rep), Owed: sign(rep.Report.BalanceDelta),
		}
		if u.SubmitsReports {
			card.Href = "/reports/day?date=" + rep.Date.String()
			if rep.Client {
				card.Href = "/clients/" + rep.ID
			}
		}
		page.Reports = append(page.Reports, card)
	}
	bal, err := a.store.PersonBalance(r.Context(), u.ID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	page.BalanceSign = sign(bal)
	page.Balance = "Расчёт: 0"
	if bal > 0 {
		page.Balance = "Салон должен вам " + domain.FormatEUR(bal)
	} else if bal < 0 {
		page.Balance = "Вы должны салону " + domain.FormatEUR(-bal)
	}
	page.AsOf = ru.Date(a.store.Today().Time())
	if r.URL.Query().Get("saved") == "pending" {
		page.Notice = "Сохранено — ожидает подтверждения"
	} else if r.URL.Query().Get("saved") == "1" {
		page.Notice = "Сохранено ✓"
	}
	movements, err := a.store.PersonMovementsIn(r.Context(), u.ID, m)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	for i, j := 0, len(movements)-1; i < j; i, j = i+1, j-1 {
		movements[i], movements[j] = movements[j], movements[i]
	}
	page.Movements, err = a.movementCards(r, u, movements, false)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	page.MovementForm, err = a.movementForm(r, u, false, "deposit")
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	a.render(w, r, views.MyPage(a.navFor(u, "/my"), page))
}

// progressText is «Услуги за месяц €500 из €5 000 — ещё €4 500,01, и «Себе»
// за весь месяц станет 80%», or what passing the threshold means.
func progressText(p domain.SelfProgress, label string, handSet bool) string {
	scope := "за весь месяц"
	if handSet { // SelfBonusFor: a hand-set day keeps exactly what it was given
		scope = "за весь месяц (кроме дней, где «Себе» задано вручную)"
	}
	if p.Reached {
		return label + " " + domain.FormatEUR(p.Base) + " — больше " + domain.FormatEUR(p.Threshold) +
			": " + scope + " «Себе» " + percent(p.HighPct) + ", разница начислится при закрытии месяца"
	}
	return label + " " + domain.FormatEUR(p.Base) + " из " + domain.FormatEUR(p.Threshold) +
		" — ещё " + domain.FormatEUR(p.Remaining) + ", и «Себе» " + scope + " станет " + percent(p.HighPct)
}

// baseLabel names what the «Себе» base adds up: services only for client
// entries, but legacy days may count other categories too.
func baseLabel(s domain.Summary) string {
	if s.SelfBase == s.ByCategory["services"] {
		return "Услуги за месяц"
	}
	return "Услуги и продажи для «Себе» за месяц"
}

func anyHandSet(rs []store.SavedReport) bool {
	for _, r := range rs {
		if r.Report.SelfOverridden {
			return true
		}
	}
	return false
}

func sign(c domain.Cents) int {
	switch {
	case c > 0:
		return 1
	case c < 0:
		return -1
	}
	return 0
}

func personReports(rs []store.SavedReport) []domain.PersonReport {
	out := make([]domain.PersonReport, len(rs))
	for i, r := range rs {
		out[i] = r.PersonReport
	}
	return out
}
