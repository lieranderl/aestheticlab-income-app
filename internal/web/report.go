package web

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/starfederation/datastar-go/datastar"

	"github.com/lieranderl/aestheticlab-income-app/internal/domain"
	"github.com/lieranderl/aestheticlab-income-app/internal/ru"
	"github.com/lieranderl/aestheticlab-income-app/internal/store"
	"github.com/lieranderl/aestheticlab-income-app/internal/views"
)

// reportDay is the day the daily form opens: ?date=, else today. A future
// or unreadable date opens today; a day before the start month opens its
// first day (or today, while the app has not started yet).
func (a *app) reportDay(r *http.Request) domain.Date {
	today := a.store.Today()
	d, err := domain.ParseDate(r.URL.Query().Get("date"))
	switch {
	case err != nil || today.Before(d):
		return today
	case d.Month().Before(a.store.StartMonth()):
		if first := a.store.StartMonth().FirstDay(); !today.Before(first) {
			return first
		}
		return today
	}
	return d
}

// handleDaily is the daily form (system-design §6.3).
func (a *app) handleDaily(w http.ResponseWriter, r *http.Request, u store.User) {
	day := a.reportDay(r)
	dc, err := a.loadDay(r.Context(), u, day)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if dc.report == nil {
		http.NotFound(w, r)
		return
	}
	f := dc.form(nil)
	if r.URL.Query().Get("saved") == "1" && dc.report != nil {
		f.Done = "Сохранено ✓"
	}
	a.render(w, r, views.DailyPage(a.navFor(u, "/"), f))
}

// handleSaveReport saves the signed-in person's report for a day. A plain
// post answers 303 (or the page with its errors, 422); Datastar gets the
// form patched in place.
func (a *app) handleSaveReport(w http.ResponseWriter, r *http.Request, u store.User) {
	a.saveReportFor(w, r, u, u, false)
}

func (a *app) saveReportFor(w http.ResponseWriter, r *http.Request, actor, owner store.User, admin bool) {
	day, err := domain.ParseDate(r.PathValue("date"))
	if err != nil || (!admin && !owner.SubmitsReports) {
		http.NotFound(w, r)
		return
	}
	if err := readForm(w, r); err != nil {
		http.Error(w, "Слишком большой запрос", http.StatusRequestEntityTooLarge)
		return
	}
	dc, err := a.loadDay(r.Context(), owner, day)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if dc.report == nil {
		http.Error(w, "Отчёт за день отсутствует — добавьте клиента в списке за день", http.StatusUnprocessableEntity)
		return
	}
	dc.admin, dc.name = admin, owner.DisplayName
	navPath := "/"
	if admin {
		navPath = "/admin/day"
	}
	in := typedReport(r, dc.cats)
	fail := func(status int, f views.ReportForm) {
		a.formFailedStatus(w, r, status, views.ReportFormView(f), views.DailyPage(a.navFor(actor, navPath), f))
	}
	entry, bad := in.entry(owner.ID, day)
	if bad != "" {
		f := dc.form(&in)
		f.Error = "Проверьте суммы: введите число, например 600 или 600,50"
		markBad(&f, bad)
		fail(http.StatusUnprocessableEntity, f)
		return
	}
	saved, err := a.store.SaveReport(r.Context(), actor.Actor(), entry)
	if errors.Is(err, store.ErrVersionConflict) {
		// Someone saved the day meanwhile: show what is saved now, so a
		// second tap can never overwrite it unseen.
		fresh, lerr := a.loadDay(r.Context(), owner, day)
		if lerr != nil {
			a.serverError(w, r, lerr)
			return
		}
		fresh.admin, fresh.name = admin, owner.DisplayName
		f := fresh.form(nil)
		f.Error = "Запись изменена другим пользователем — показаны сохранённые суммы, проверьте и сохраните ещё раз"
		fail(http.StatusUnprocessableEntity, f)
		return
	}
	if err != nil {
		f := dc.form(&in)
		f.Error = reportProblem(r, err, &f)
		fail(http.StatusUnprocessableEntity, f)
		return
	}
	if !isDatastar(r) {
		href := "/reports/day?date=" + day.String() + "&saved=1"
		if admin {
			href = "/admin/reports/" + owner.ID + "/" + day.String() + "?saved=1"
		}
		http.Redirect(w, r, href, http.StatusSeeOther) // #nosec G710 -- a same-site path built from a stored user ID and parsed date
		return
	}
	dc.report = &saved
	f := dc.form(nil)
	f.Done = "Сохранено ✓"
	if err := datastar.NewSSE(w, r).PatchElementTempl(views.ReportFormView(f)); err != nil {
		slog.ErrorContext(r.Context(), "patch report form", "error", err)
	}
}

// dayContext is what the daily form needs about one person and day.
type dayContext struct {
	admin      bool
	name       string
	user       string
	day, today domain.Date
	start      domain.Month
	cats       []store.Category
	report     *store.SavedReport
	terms      domain.Terms
	hasTerms   bool
	closed     bool
}

func (a *app) loadDay(ctx context.Context, u store.User, day domain.Date) (dayContext, error) {
	dc := dayContext{user: u.ID, day: day, today: a.store.Today(), start: a.store.StartMonth()}
	var err error
	if dc.cats, err = a.store.Categories(ctx); err != nil {
		return dc, err
	}
	entries, err := a.store.Terms(ctx, u.ID)
	if err != nil {
		return dc, err
	}
	dc.terms, dc.hasTerms = history(entries).For(day.Month())
	month, err := a.store.Month(ctx, day.Month())
	if err != nil {
		return dc, err
	}
	dc.closed = month.Status == domain.MonthClosed
	switch saved, err := a.store.Report(ctx, u.ID, day); {
	case err == nil:
		dc.report = &saved
	case !errors.Is(err, store.ErrNotFound):
		return dc, err
	}
	return dc, nil
}

// typedReportForm is the daily form as posted, by field.
type typedReportForm struct {
	revenue, payments map[string]string // by category ID, as typed
	cashToKassa       string
	transferToCard    string
	selfManual        bool
	selfValue         string
	version           string
}

// typedReport reads the posted amounts of the categories the form shows.
func typedReport(r *http.Request, cats []store.Category) typedReportForm {
	in := typedReportForm{
		revenue: map[string]string{}, payments: map[string]string{},
		cashToKassa:    strings.TrimSpace(r.PostFormValue("cashToKassa")),
		transferToCard: strings.TrimSpace(r.PostFormValue("transferToCard")),
		selfManual:     r.PostFormValue("selfManual") == "1",
		selfValue:      strings.TrimSpace(r.PostFormValue("selfOverride")),
		version:        r.PostFormValue("version"),
	}
	for _, c := range cats {
		switch c.Kind {
		case store.KindRevenue:
			if v, ok := r.PostForm["r_"+c.ID]; ok {
				in.revenue[c.ID] = strings.TrimSpace(v[0])
			}
		case store.KindPayment:
			if v, ok := r.PostForm["p_"+c.ID]; ok {
				in.payments[c.ID] = strings.TrimSpace(v[0])
			}
		}
	}
	return in
}

// entry turns the typed form into a report entry, or names the first field
// that is not an amount.
func (in typedReportForm) entry(uid string, day domain.Date) (store.ReportEntry, string) {
	e := store.ReportEntry{Person: uid, Date: day, Revenue: map[string]domain.Cents{}, Payments: map[string]domain.Cents{}}
	for id, v := range in.revenue {
		c, err := domain.ParseAmount(v)
		if err != nil {
			return e, "r_" + id
		}
		e.Revenue[id] = c
	}
	for id, v := range in.payments {
		c, err := domain.ParseAmount(v)
		if err != nil {
			return e, "p_" + id
		}
		e.Payments[id] = c
	}
	if in.cashToKassa != "" {
		c, err := domain.ParseAmount(in.cashToKassa)
		if err != nil {
			return e, "cashToKassa"
		}
		e.CashToKassa = &c
	}
	if in.transferToCard != "" {
		c, err := domain.ParseAmount(in.transferToCard)
		if err != nil {
			return e, "transferToCard"
		}
		e.TransferToCard = &c
	}
	if in.selfManual {
		c, err := domain.ParseAmount(in.selfValue)
		if err != nil || in.selfValue == "" {
			return e, "selfOverride"
		}
		e.SelfOverride = &c
	}
	v, err := strconv.ParseInt(in.version, 10, 64)
	if err != nil || v < 0 {
		v = -1 // never matches a stored version: the form is refused as stale
	}
	e.Version = v
	return e, ""
}

// form builds the daily form: the typed values when in is set (a refused
// save), else the saved report, else an empty day.
func (dc dayContext) form(in *typedReportForm) views.ReportForm {
	f := views.ReportForm{
		User: dc.user, Date: dc.day.String(), DayLabel: ru.Date(dc.day.Time()), Weekday: weekday(dc.day, dc.today),
		Min: dc.start.FirstDay().String(), Max: dc.today.String(),
		SelfPct: int64(dc.terms.SelfPct), SelfPctText: percent(dc.terms.SelfPct),
	}
	if prev := dc.day.AddDays(-1); !prev.Month().Before(dc.start) {
		f.Prev = "/reports/day?date=" + prev.String()
	}
	if next := dc.day.AddDays(1); !dc.today.Before(next) {
		f.Next = "/reports/day?date=" + next.String()
	}
	if dc.admin {
		base := "/admin/reports/" + dc.user + "/"
		f.Action, f.PersonName, f.PickerAction = base+dc.day.String(), dc.name, "/admin/day"
		if f.Prev != "" {
			f.Prev = base + dc.day.AddDays(-1).String()
		}
		if f.Next != "" {
			f.Next = base + dc.day.AddDays(1).String()
		}
	}
	switch {
	case dc.day.Month().Before(dc.start):
		f.Locked = "Отчёты принимаются с " + ru.Date(dc.start.FirstDay().Time())
	case dc.closed:
		f.Locked = "Месяц закрыт — изменения недоступны"
	case !dc.hasTerms:
		f.Locked = "Ваши условия («Себе» %) ещё не заданы — попросите администратора задать их в разделе «Сотрудники»"
	}

	var saved *store.SavedReport
	if dc.report != nil {
		saved = dc.report
		f.Version, f.Editing = saved.Version, true
	}
	for _, c := range dc.cats {
		var amount domain.Cents
		var used bool
		if saved != nil {
			if c.Kind == store.KindRevenue {
				amount, used = saved.Revenue[c.ID]
			} else {
				amount, used = saved.Payments[c.ID]
			}
		}
		if !c.Active && !used {
			continue // archived categories stay only on reports that have them
		}
		row := views.AmountRow{ID: c.ID, Name: c.Name, Value: amountText(amount),
			CountsTowardSelf: c.CountsTowardSelf, IsCertificateSale: c.IsCertificateSale, Destination: string(c.Destination)}
		if saved != nil && used {
			if c.Kind == store.KindRevenue {
				if flags, ok := saved.Flags.Revenue[c.ID]; ok {
					row.CountsTowardSelf, row.IsCertificateSale = flags.CountsTowardSelf, flags.IsCertificateSale
				}
			} else if method, ok := saved.Flags.Payments[c.ID]; ok {
				row.Destination = string(method.Destination)
			}
		}
		switch c.Kind {
		case store.KindRevenue:
			if in != nil {
				row.Value = in.revenue[c.ID]
			}
			f.Revenue = append(f.Revenue, row)
		case store.KindPayment:
			if in != nil {
				row.Value = in.payments[c.ID]
			}
			f.Payments = append(f.Payments, row)
		}
	}

	var shown domain.Report
	if saved != nil {
		shown = saved.Report
		if shown.Cash > 0 {
			f.TransferToCard = amountText(shown.TransferToCard)
			// Empty means «the suggestion»: a till that differs from it shows,
			// a 0 included, so saving the day again keeps it.
			if shown.CashToKassa != max(0, shown.SuggestedToKassa-shown.TransferToCard) {
				f.CashToKassa = amountOrZero(shown.CashToKassa)
			}
		}
		if shown.SelfOverridden {
			f.SelfManual, f.SelfValue = true, amountOrZero(shown.SelfAmount)
		}
	}
	if in != nil {
		f.CashToKassa, f.TransferToCard, f.SelfManual, f.SelfValue = in.cashToKassa, in.transferToCard, in.selfManual, in.selfValue
		shown = dc.preview(in)
	}
	f.Total, f.Self, f.Salon = domain.FormatEUR(shown.Total), domain.FormatEUR(shown.SelfAmount), domain.FormatEUR(shown.SalonShare)
	f.HasCash = shown.Cash > 0 || (in != nil && dc.typedCash(in))
	f.Suggested = domain.FormatEUR(shown.SuggestedToKassa)
	f.KassaHint = amountOrZero(max(0, shown.SuggestedToKassa-shown.TransferToCard))
	f.Effect = effectLine(shown)
	return f
}

// preview computes the typed figures for a refused save, where they are
// valid; otherwise the figures show zero until the browser recomputes them.
func (dc dayContext) preview(in *typedReportForm) domain.Report {
	e, bad := in.entry("", dc.day)
	if bad != "" {
		return domain.Report{}
	}
	r, err := domain.ComputeReport(domain.ReportInput{
		Revenue: e.Revenue, Payments: e.Payments, SelfPct: dc.terms.SelfPct,
		CashToKassa: e.CashToKassa, TransferToCard: e.TransferToCard, SelfOverride: e.SelfOverride,
	}, dc.reportCatalog())
	if err != nil {
		return domain.Report{}
	}
	return r
}

// typedCash says whether a refused form had a cash payment, so its
// «Оставьте в кассе» fields stay on screen to be corrected.
func (dc dayContext) typedCash(in *typedReportForm) bool {
	for id, method := range dc.reportCatalog().Payments {
		if method.Destination != domain.ToCash {
			continue
		}
		if v, err := domain.ParseAmount(in.payments[id]); err == nil && v > 0 {
			return true
		}
	}
	return false
}

func (dc dayContext) reportCatalog() domain.Catalog {
	catalog := store.CatalogOf(dc.cats)
	if dc.report == nil {
		return catalog
	}
	for id := range dc.report.Revenue {
		if flags, ok := dc.report.Flags.Revenue[id]; ok {
			catalog.Revenue[id] = flags
		}
	}
	for id := range dc.report.Payments {
		if method, ok := dc.report.Flags.Payments[id]; ok {
			catalog.Payments[id] = method
		}
	}
	return catalog
}

// effectLine says in plain words what the day does to the person's balance:
// «Остальные €150 наличными — ваши. Салон должен вам €175».
func effectLine(r domain.Report) string {
	var b strings.Builder
	if kept := r.Cash - r.CashToKassa - r.TransferToCard; kept > 0 {
		b.WriteString("Остальные " + domain.FormatEUR(kept) + " наличными — ваши. ")
	}
	switch d := r.BalanceDelta; {
	case d > 0:
		b.WriteString("Салон должен вам " + domain.FormatEUR(d))
	case d < 0:
		b.WriteString("Вы должны салону " + domain.FormatEUR(-d))
	default:
		b.WriteString("Расчёт за день: 0")
	}
	return b.String()
}

// reportProblem is the Russian text for a refused save; it also marks the
// field at fault.
func reportProblem(r *http.Request, err error, f *views.ReportForm) string {
	var ve *domain.ValidationError
	if !errors.As(err, &ve) {
		if errors.Is(err, store.ErrFinancialWritesPaused) {
			return "Запись финансовых операций временно приостановлена"
		}
		if errors.Is(err, store.ErrMonthClosed) {
			return "Месяц закрыт — изменения недоступны"
		}
		return problemText(r, err)
	}
	markBad(f, ve.Field)
	switch ve.Problem {
	case domain.ProblemPaymentsMismatch:
		if ve.Difference > 0 {
			return "Сумма оплат меньше выручки на " + domain.FormatEUR(ve.Difference) + " — проверьте наличные"
		}
		return "Сумма оплат больше выручки на " + domain.FormatEUR(-ve.Difference) + " — проверьте суммы"
	case domain.ProblemCashToKassaOutOfRange:
		return "В кассу и на карту вместе — не больше суммы наличных"
	case domain.ProblemSelfOutOfRange:
		return "«Себе» — от €0 до суммы выручки за день"
	case domain.ProblemMissingTerms:
		return "Ваши условия («Себе» %) ещё не заданы — попросите администратора задать их"
	case domain.ProblemFutureDate:
		return "Отчёт за будущий день сохранить нельзя"
	case domain.ProblemUnknownCategory, domain.ProblemUnknownPaymentMethod:
		return "Список категорий изменился — обновите страницу"
	}
	return problemText(r, err)
}

func markBad(f *views.ReportForm, field string) {
	for i := range f.Revenue {
		f.Revenue[i].Bad = f.Revenue[i].Bad || field == "r_"+f.Revenue[i].ID
	}
	for i := range f.Payments {
		f.Payments[i].Bad = f.Payments[i].Bad || field == "p_"+f.Payments[i].ID
	}
	f.KassaBad = f.KassaBad || field == "cashToKassa"
	f.TransferBad = f.TransferBad || field == "transferToCard"
	f.SelfBad = f.SelfBad || field == "selfOverride"
}

// amountText is an amount as the form shows it: «600», «600,50»; empty for 0.
func amountText(c domain.Cents) string {
	if c == 0 {
		return ""
	}
	s := strconv.FormatInt(int64(c/100), 10)
	if frac := c % 100; frac != 0 {
		s += fmt.Sprintf(",%02d", frac)
	}
	return s
}

// amountOrZero is amountText, with «0» for zero.
func amountOrZero(c domain.Cents) string {
	if c == 0 {
		return "0"
	}
	return amountText(c)
}

// weekday is «пт» or «пт, сегодня».
func weekday(d, today domain.Date) string {
	w := ru.Weekday(d.Time().Weekday())
	if d == today {
		return w + ", сегодня"
	}
	return w
}
