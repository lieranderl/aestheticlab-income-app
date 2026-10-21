package web

import (
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/lieranderl/aestheticlab-income-app/internal/domain"
	"github.com/lieranderl/aestheticlab-income-app/internal/ru"
	"github.com/lieranderl/aestheticlab-income-app/internal/store"
	"github.com/lieranderl/aestheticlab-income-app/internal/views"
)

func (a *app) handleAdminMonth(w http.ResponseWriter, r *http.Request, u store.User) {
	if r.URL.Query().Get("tab") == "settlement" {
		a.handleAdminSettlement(w, r, u)
		return
	}
	current, start := a.store.Today().Month(), a.store.StartMonth()
	m, err := domain.ParseMonth(r.URL.Query().Get("m"))
	if err != nil || current.Before(m) {
		m = current
	}
	if m.Before(start) {
		m = start
	}
	reports, err := a.store.ReportsIn(r.Context(), m)
	if err != nil {
		a.serverError(w, r, err)
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
	month, err := a.store.Month(r.Context(), m)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	var legacy []store.SavedReport
	for _, rep := range reports {
		if !rep.Client {
			legacy = append(legacy, rep)
		}
	}
	legacySum := domain.Summarize(personReports(legacy))
	sum := domain.Summarize(personReports(reports))
	if month.Snapshot != nil {
		frozen := month.Snapshot.Result
		sum.Clients = frozen.Clients
		sum.KassaCash = frozen.KassaCash
		sum.KassaCard = frozen.KassaCard
	}
	p := views.AdminMonth{
		Key: m.String(), Month: monthLabel(m), Figures: summaryFigures(sum, false),
		RecordsURL: "/admin/history?from=" + m.FirstDay().String() + "&to=" + m.LastDay().String(),
		Categories: categoryRows(cats, store.KindRevenue, sum.ByCategory),
		Payments:   categoryRows(cats, store.KindPayment, legacySum.ByPayment),
		Closed:     month.Status == domain.MonthClosed,
	}
	p.Figures = append(withRevenueCategories(p.Figures, cats, sum.ByCategory), actualRows(sum.KassaCash+sum.KassaCard, sum.CashKept, 0, false)...)
	if month.Snapshot != nil && month.Snapshot.Result.StatsVersion < 1 {
		p.Figures = removeUnavailableClientStats(p.Figures)
	}
	if prev := m.Prev(); !prev.Before(start) {
		p.Prev = "/admin/month?m=" + prev.String()
	}
	if next := m.Next(); !current.Before(next) {
		p.Next = "/admin/month?m=" + next.String()
	}
	for _, person := range users {
		if s, ok := sum.People[person.ID]; ok {
			personSum := domain.Summarize(personReports(reportsForPerson(reports, person.ID)))
			detail := withRevenueCategories(summaryFigures(personSum, true), cats, personSum.ByCategory)
			detail = append(detail, actualRows(personSum.KassaCash+personSum.KassaCard, s.CashKept, s.BalanceDelta, true)...)
			actual := actualRows(personSum.KassaCash+personSum.KassaCard, s.CashKept, s.BalanceDelta, true)
			clients := strconv.Itoa(s.Clients)
			if month.Snapshot != nil {
				pr := month.Snapshot.Result.People[person.ID]
				if pr.StatsVersion < 1 {
					detail = nil
					clients = ""
				} else {
					detail = withRevenueCategories(clientSummary(domain.Summary{Clients: pr.Clients, Total: pr.Revenue, ByCategory: pr.RevenueByCategory, CertificateSales: pr.RevenueByCategory["certificates"], KassaCash: pr.KassaCash, KassaCard: pr.KassaCard}), cats, pr.RevenueByCategory)
					actual = actualRows(pr.KassaCash+pr.KassaCard, s.CashKept, s.BalanceDelta, true)
					detail = append(detail, actual...)
					clients = strconv.Itoa(pr.Clients)
				}
			}
			p.People = append(p.People, views.MonthPerson{
				Name: person.DisplayName, Total: domain.FormatEUR(s.Total),
				Self: domain.FormatEUR(s.Self), Salon: domain.FormatEUR(s.SalonShare), Days: strconv.Itoa(s.WorkingDays), Clients: clients, Details: detail, Actual: actual,
			})
		}
	}
	a.render(w, r, views.AdminMonthPage(a.navFor(u, "/admin/month"), p))
}

func (a *app) adminReportOwner(w http.ResponseWriter, r *http.Request) (store.User, bool) {
	u, err := a.store.User(r.Context(), r.PathValue("uid"))
	if errors.Is(err, store.ErrNotFound) || errors.Is(err, store.ErrInvalid) {
		http.NotFound(w, r)
		return u, false
	}
	if err != nil {
		a.serverError(w, r, err)
		return u, false
	}
	return u, true
}

func (a *app) handleAdminReport(w http.ResponseWriter, r *http.Request, admin store.User) {
	owner, ok := a.adminReportOwner(w, r)
	if !ok {
		return
	}
	day, err := domain.ParseDate(r.PathValue("date"))
	if err != nil || a.store.Today().Before(day) {
		http.NotFound(w, r)
		return
	}
	dc, err := a.loadDay(r.Context(), owner, day)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if dc.report == nil {
		http.NotFound(w, r)
		return
	}
	dc.admin, dc.name = true, owner.DisplayName
	f := dc.form(nil)
	if r.URL.Query().Get("saved") == "1" && dc.report != nil {
		f.Done = "Сохранено ✓"
	}
	a.render(w, r, views.DailyPage(a.navFor(admin, "/admin/day"), f))
}

func (a *app) handleAdminSaveReport(w http.ResponseWriter, r *http.Request, admin store.User) {
	owner, ok := a.adminReportOwner(w, r)
	if ok {
		a.saveReportFor(w, r, admin, owner, true)
	}
}

func (a *app) handleAdminDay(w http.ResponseWriter, r *http.Request, u store.User) {
	day := a.reportDay(r)
	reports, err := a.store.ReportsOn(r.Context(), day)
	if err != nil {
		a.serverError(w, r, err)
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
	sum := domain.Summarize(personReports(reports))
	dayForm := dayPicker(a, day, "/admin/day")
	month, err := a.store.Month(r.Context(), day.Month())
	if err == nil && month.Status == domain.MonthClosed {
		dayForm.Locked = "Месяц закрыт — изменения недоступны"
	} else if day.Month().Before(a.store.StartMonth()) {
		dayForm.Locked = "Отчёты принимаются с " + ru.Date(a.store.StartMonth().FirstDay().Time())
	}
	p := views.AdminDay{Date: dayForm, Figures: append(withRevenueCategories(summaryFigures(sum, true), cats, sum.ByCategory), actualRows(sum.KassaCash+sum.KassaCard, sum.CashKept, 0, false)...)}
	if r.URL.Query().Get("removed") == "1" {
		p.Notice = "Клиент удалён"
	}
	for _, person := range users {
		var own []store.SavedReport
		for _, rep := range reports {
			if rep.Person == person.ID {
				own = append(own, rep)
			}
		}
		if len(own) > 0 {
			ownSum := domain.Summarize(personReports(own))
			p.People = append(p.People, views.DayPerson{Name: person.DisplayName, Figures: append(withRevenueCategories(summaryFigures(ownSum, true), cats, ownSum.ByCategory), actualRows(ownSum.KassaCash+ownSum.KassaCard, ownSum.CashKept, ownSum.BalanceDelta, true)...)})
		}
	}
	for _, rep := range reports {
		p.Reports = append(p.Reports, reportCard(rep, users, cats))
	}
	if !day.Month().Before(a.store.StartMonth()) {
		expected, err := a.store.ExpectedReporters(r.Context(), day)
		if err != nil {
			a.serverError(w, r, err)
			return
		}
		for _, id := range domain.MissingReporters(expected, personReports(reports)) {
			p.Missing = append(p.Missing, personName(users, id))
		}
	}
	a.render(w, r, views.AdminDayPage(a.navFor(u, "/admin/day"), p))
}

func dayPicker(a *app, day domain.Date, path string) views.ReportForm {
	f := views.ReportForm{Date: day.String(), DayLabel: ru.Date(day.Time()), Weekday: weekday(day, a.store.Today()),
		Min: a.store.StartMonth().FirstDay().String(), Max: a.store.Today().String(), PickerAction: path}
	if prev := day.AddDays(-1); !prev.Month().Before(a.store.StartMonth()) {
		f.Prev = path + "?date=" + prev.String()
	}
	if next := day.AddDays(1); !a.store.Today().Before(next) {
		f.Next = path + "?date=" + next.String()
	}
	return f
}

func summaryFigures(s domain.Summary, day bool) []views.MoneyRow {
	rows := append(clientSummary(s), []views.MoneyRow{
		{Name: "«Себе»", Value: domain.FormatEUR(s.Self)},
		{Name: "«Салону»", Value: domain.FormatEUR(s.SalonShare)},
	}...)
	rows = append(rows, views.MoneyRow{Name: "Оплачено сертификатами", Value: domain.FormatEUR(s.Prepaid)})
	if !day {
		return append(rows, views.MoneyRow{Name: "Рабочие дни", Value: strconv.Itoa(s.WorkingDays)})
	}
	return rows
}

// actualRows are the money as it moved: paid into the kassa, kept in cash
// and, for one person, what their records leave to settle with the salon.
func actualRows(kassa, kept, delta domain.Cents, person bool) []views.MoneyRow {
	rows := []views.MoneyRow{{Name: "Внесено в кассу", Value: domain.FormatEUR(kassa)}, {Name: "Оставили себе наличными", Value: domain.FormatEUR(kept)}}
	if person {
		rows = append(rows, settleRow(delta))
	}
	return rows
}

// settleRow is what records leave between one person and the salon, in
// gender-neutral words for the admin.
func settleRow(delta domain.Cents) views.MoneyRow {
	switch {
	case delta > 0:
		return views.MoneyRow{Name: "По записям: салон должен", Value: domain.FormatEUR(delta)}
	case delta < 0:
		return views.MoneyRow{Name: "По записям: долг перед салоном", Value: domain.FormatEUR(-delta)}
	}
	return views.MoneyRow{Name: "По записям: расчёт", Value: "€0"}
}

func withRevenueCategories(rows []views.MoneyRow, cats []store.Category, sums map[string]domain.Cents) []views.MoneyRow {
	for _, c := range cats {
		if c.Kind != store.KindRevenue || sums[c.ID] == 0 {
			continue
		}
		defaultName, representedName := "", ""
		switch c.ID {
		case "services":
			defaultName, representedName = "Услуги", "Услуги"
		case "creams":
			defaultName, representedName = "Кремы", "Кремы"
		case "oil":
			defaultName, representedName = "Масло", "Масло"
		case "certificates":
			defaultName = "Сертификаты"
		}
		if c.Name == defaultName {
			continue
		}
		if representedName != "" {
			rows = slices.DeleteFunc(rows, func(row views.MoneyRow) bool { return row.Name == representedName })
		}
		rows = append(rows, views.MoneyRow{Name: "Категория · " + c.Name, Value: domain.FormatEUR(sums[c.ID])})
	}
	return rows
}

func personName(users []store.User, id string) string {
	for _, u := range users {
		if u.ID == id {
			return u.DisplayName
		}
	}
	return "Неизвестный сотрудник"
}

func reportCard(rep store.SavedReport, users []store.User, cats []store.Category) views.AdminReport {
	r := rep.Report
	card := views.AdminReport{
		Client: rep.Client, Name: personName(users, rep.Person), Day: ru.Date(rep.Date.Time()) + " · " + clientLabel(rep),
		Href:  "/admin/reports/" + rep.Person + "/" + rep.Date.String(),
		Total: domain.FormatEUR(r.Total), Self: domain.FormatEUR(r.SelfAmount), Salon: domain.FormatEUR(r.SalonShare),
		Details:     append(categoryRows(cats, store.KindRevenue, rep.Revenue), categoryRows(cats, store.KindPayment, rep.Payments)...),
		CashToKassa: domain.FormatEUR(r.CashToKassa), TransferToCard: domain.FormatEUR(r.TransferToCard),
		Actual: recordActual(r)}
	if rep.Client {
		card.Href = "/clients/" + rep.ID
	}
	if rep.Client {
		card.Details = clientSummary(domain.Summarize([]domain.PersonReport{rep.PersonReport}))
	}
	return card
}

// recordActual is one record's money as it moved: «В кассу €427 · себе
// наличными €100 · салон должен €113,50».
func recordActual(r domain.Report) string {
	settle := settleRow(r.BalanceDelta)
	return "В кассу " + domain.FormatEUR(r.CashToKassa+r.CardIn+r.TransferToCard) + " · себе наличными " + domain.FormatEUR(r.CashKept()) +
		" · " + strings.ToLower(strings.TrimPrefix(settle.Name, "По записям: ")) + " " + settle.Value
}

func categoryRows(cats []store.Category, kind store.CategoryKind, sums map[string]domain.Cents) []views.MoneyRow {
	var rows []views.MoneyRow
	for _, c := range cats {
		if c.Kind == kind && sums[c.ID] != 0 {
			rows = append(rows, views.MoneyRow{Name: c.Name, Value: domain.FormatEUR(sums[c.ID])})
		}
	}
	if kind == store.KindRevenue && sums["oil"] != 0 {
		found := false
		for _, c := range cats {
			if c.ID == "oil" {
				found = true
			}
		}
		if !found {
			rows = append(rows, views.MoneyRow{Name: "Масло", Value: domain.FormatEUR(sums["oil"])})
		}
	}
	return rows
}

func reportsForPerson(reports []store.SavedReport, id string) []store.SavedReport {
	var out []store.SavedReport
	for _, rep := range reports {
		if rep.Person == id {
			out = append(out, rep)
		}
	}
	return out
}

func removeUnavailableClientStats(rows []views.MoneyRow) []views.MoneyRow {
	var out []views.MoneyRow
	for _, row := range rows {
		if row.Name != "Клиенты" && row.Name != "Касса — наличные" && row.Name != "Касса — карта салона" {
			out = append(out, row)
		}
	}
	return out
}
