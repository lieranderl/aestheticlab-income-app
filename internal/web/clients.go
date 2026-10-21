package web

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/lieranderl/aestheticlab-income-app/internal/domain"
	"github.com/lieranderl/aestheticlab-income-app/internal/ru"
	"github.com/lieranderl/aestheticlab-income-app/internal/store"
	"github.com/lieranderl/aestheticlab-income-app/internal/views"
)

func clientLabel(rep store.SavedReport) string {
	if !rep.Client {
		return "Отчёт за день"
	}
	loc, _ := time.LoadLocation("Europe/Brussels")
	return "Клиент " + strconv.FormatInt(rep.ClientNumber, 10) + " · " + rep.CreatedAt.In(loc).Format("15:04")
}

func (a *app) handleClientsDay(w http.ResponseWriter, r *http.Request, u store.User) {
	day := a.reportDay(r)
	rows, err := a.store.PersonReportsIn(r.Context(), u.ID, day.Month())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	var selected []store.SavedReport
	var cards []views.MyReport
	for _, rep := range rows {
		if rep.Date != day {
			continue
		}
		selected = append(selected, rep)
		href := "/reports/day?date=" + day.String()
		if rep.Client {
			href = "/clients/" + rep.ID
		}
		cards = append(cards, views.MyReport{Day: clientLabel(rep), Href: href, Total: domain.FormatEUR(rep.Report.Total), Self: domain.FormatEUR(rep.Report.SelfAmount), Salon: domain.FormatEUR(rep.Report.SalonShare), Effect: clientEffect(rep), Explain: selfEquation(rep), Owed: sign(rep.Report.BalanceDelta)})
	}
	sum := domain.Summarize(personReports(selected))
	figures := append(clientSummary(sum), []views.MoneyRow{
		{Name: "«Себе»", Value: domain.FormatEUR(sum.Self)},
		{Name: "«Салону»", Value: domain.FormatEUR(sum.SalonShare)},
		{Name: "Оплачено сертификатами", Value: domain.FormatEUR(sum.Prepaid)},
	}...)
	var daySplit views.SelfSplit
	if len(selected) > 0 {
		daySplit = selfSplit(sum, "день")
	}
	picker := dayPicker(a, day, "/")
	if dc, err := a.loadDay(r.Context(), u, day); err == nil {
		picker.Locked = dc.form(nil).Locked
	}
	notice := ""
	if r.URL.Query().Get("removed") == "1" {
		notice = "Клиент удалён"
	}
	a.render(w, r, views.ClientDayPage(a.navFor(u, "/"), picker, cards, figures, daySplit, notice))
}

func clientSummary(s domain.Summary) []views.MoneyRow {
	return []views.MoneyRow{{Name: "Клиенты", Value: strconv.Itoa(s.Clients)}, {Name: "Услуги", Value: domain.FormatEUR(s.ByCategory["services"])}, {Name: "Кремы", Value: domain.FormatEUR(s.ByCategory["creams"])}, {Name: "Масло", Value: domain.FormatEUR(s.ByCategory["oil"])}, {Name: "Продажа сертификатов", Value: domain.FormatEUR(s.CertificateSales)}, {Name: "Выручка", Value: domain.FormatEUR(s.Total)}, {Name: "Касса — наличные", Value: domain.FormatEUR(s.KassaCash)}, {Name: "Касса — карта салона", Value: domain.FormatEUR(s.KassaCard)}}
}

func (a *app) handleClient(w http.ResponseWriter, r *http.Request, u store.User) {
	f, err := a.clientForm(r, u)
	if err != nil {
		a.clientError(w, r, u, err)
		return
	}
	if r.PathValue("id") == "" && r.URL.Query().Get("token") == "" {
		a.navigate(w, r, "/clients/new?date="+f.Date+"&token="+f.Token)
		return
	}
	a.render(w, r, views.ClientPage(a.navFor(u, "/"), f))
}

func (a *app) clientError(w http.ResponseWriter, r *http.Request, u store.User, err error) {
	if errors.Is(err, store.ErrForbidden) {
		a.renderStatus(w, r, http.StatusForbidden, views.ForbiddenPage(a.navFor(u, "/")))
		return
	}
	if errors.Is(err, store.ErrNotFound) || errors.Is(err, store.ErrInvalid) {
		a.renderStatus(w, r, http.StatusNotFound, views.ClientGonePage(a.navFor(u, "/")))
		return
	}
	a.serverError(w, r, err)
}

func (a *app) clientForm(r *http.Request, u store.User) (views.ClientForm, error) {
	owner := u
	day := a.reportDay(r)
	if r.Method == http.MethodPost && r.PathValue("id") == "" {
		var err error
		day, err = domain.ParseDate(r.PostFormValue("date"))
		if err != nil {
			return views.ClientForm{}, store.ErrInvalid
		}
	}
	f := views.ClientForm{Action: "/clients", User: u.ID, Date: day.String(), Values: map[string]string{"services": "", "creams": "", "oil": "", "certificates": "", "certificatePaid": "", "serviceCash": "", "serviceCard": ""}, Number: 1}
	if id := r.PathValue("id"); id != "" {
		rep, err := a.store.ClientReport(r.Context(), u.Actor(), id)
		if err != nil {
			return f, err
		}
		owner, err = a.store.User(r.Context(), rep.Person)
		if err != nil {
			return f, err
		}
		day = rep.Date
		f.User = rep.Person
		f.Date = day.String()
		f.Action = "/clients/" + id
		f.Entry = id
		f.Version = rep.Version
		f.Number = rep.ClientNumber
		f.Title = clientLabel(rep)
		f.Token = strings.TrimPrefix(id, "client_"+rep.Person+"_"+day.String()+"_")
		in := rep.ClientInput
		f.Values = map[string]string{"services": amountText(in.Services), "creams": amountText(in.Extras["creams"]), "oil": amountText(in.Extras["oil"]), "certificates": amountText(in.Extras["certificates"]), "certificatePaid": amountText(in.CertificatePaid), "serviceCash": amountText(in.ServiceCash), "serviceCard": amountText(in.ServiceCard), "extrasDestination": string(in.ExtrasDestination)}
		if r.URL.Query().Get("saved") == "1" {
			f.Done = "Сохранено ✓"
			f.Acknowledged = f.Token
		}
	} else {
		if !u.SubmitsReports {
			return f, store.ErrForbidden
		}
		token := make([]byte, 24)
		if _, err := rand.Read(token); err != nil {
			return f, err
		}
		f.Token = hex.EncodeToString(token)
		if canonical := r.URL.Query().Get("token"); canonical != "" && r.Method == http.MethodGet {
			raw, err := hex.DecodeString(canonical)
			if err != nil || len(raw) != 24 {
				return f, store.ErrInvalid
			}
			f.Token = canonical
		}
		next, err := a.store.NextClientNumber(r.Context(), u.ID, day)
		if err != nil {
			return f, err
		}
		f.Number = next
		f.Title = "Клиент " + strconv.FormatInt(f.Number, 10)
		f.Entry = "new"
		f.Values["extrasDestination"] = "cash"
	}
	dc, err := a.loadDay(r.Context(), owner, day)
	if err != nil {
		return f, err
	}
	f.Pct = int64(dc.terms.SelfPct)
	if dc.hasTerms {
		f.SharePct = percent(dc.terms.SelfPct)
	}
	f.OtherEmployee = owner.ID != u.ID
	f.CanNext = owner.ID == u.ID && u.SubmitsReports
	f.Back = "/?date=" + day.String()
	if owner.ID != u.ID {
		f.Back = "/admin/day?date=" + day.String()
	}
	f.DayLabel = ru.Date(day.Time())
	show := domain.Report{}
	if r.PathValue("id") != "" {
		saved, err := a.store.ClientReport(r.Context(), u.Actor(), r.PathValue("id"))
		if err != nil {
			return f, err
		}
		show = saved.Report
	}
	f.Self = domain.FormatEUR(show.SelfAmount)
	f.Salon = domain.FormatEUR(show.SalonShare)
	f.Cash = domain.FormatEUR(show.CashToKassa)
	f.Card = domain.FormatEUR(show.CardIn + show.TransferToCard)
	f.Held = domain.FormatEUR(show.CashKept())
	f.Effect = "По этой записи расчёт с салоном — €0"
	switch {
	case show.BalanceDelta < 0:
		f.Effect = "Оставьте в кассе " + domain.FormatEUR(-show.BalanceDelta)
	case show.BalanceDelta > 0:
		f.Effect = "По этой записи салон должен вам " + domain.FormatEUR(show.BalanceDelta)
	}
	f.Locked = dc.form(nil).Locked
	return f, nil
}

func (a *app) handleSaveClient(w http.ResponseWriter, r *http.Request, u store.User) {
	if err := readForm(w, r); err != nil {
		http.Error(w, "Слишком большой запрос", http.StatusRequestEntityTooLarge)
		return
	}
	f, err := a.clientForm(r, u)
	if err != nil {
		a.clientError(w, r, u, err)
		return
	}
	e := store.ClientEntry{Person: f.User, Token: f.Token, Input: domain.ClientInput{Extras: map[string]domain.Cents{}}}
	if r.PathValue("id") == "" {
		e.Token = r.PostFormValue("token")
		f.Token = e.Token
		f.Date = r.PostFormValue("date")
	}
	e.Date, err = domain.ParseDate(f.Date)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	e.Version, err = strconv.ParseInt(r.PostFormValue("version"), 10, 64)
	if err != nil {
		e.Version = -1
	}
	f.Version = e.Version
	f.Bad = map[string]bool{}
	f.FieldErrors = map[string]string{}
	var bad bool
	for _, name := range []string{"services", "creams", "oil", "certificates", "certificatePaid", "serviceCash", "serviceCard", "extrasDestination"} {
		f.Values[name] = strings.TrimSpace(r.PostFormValue(name))
	}
	for _, name := range []string{"services", "creams", "oil", "certificates", "certificatePaid", "serviceCash", "serviceCard"} {
		c, err := domain.ParseAmount(f.Values[name])
		if err != nil {
			bad = true
			f.Bad[name] = true
			f.FieldErrors[name] = "Введите сумму от €0 до €9 999 999,99, например 600,50."
			continue
		}
		switch name {
		case "services":
			e.Input.Services = c
		case "certificatePaid":
			e.Input.CertificatePaid = c
		case "serviceCash":
			e.Input.ServiceCash = c
		case "serviceCard":
			e.Input.ServiceCard = c
		default:
			e.Input.Extras[name] = c
		}
	}
	e.Input.ExtrasDestination = domain.Destination(f.Values["extrasDestination"])
	if bad {
		f.Error = "Проверьте суммы: введите число, например 600 или 600,50"
	} else {
		saved, err := a.store.SaveClientReport(r.Context(), u.Actor(), e)
		if err == nil {
			a.navigate(w, r, "/clients/"+saved.ID+"?saved=1")
			return
		}
		switch {
		case errors.Is(err, store.ErrVersionConflict):
			if r.PathValue("id") != "" {
				fresh, ferr := a.clientForm(r, u)
				if ferr != nil {
					a.clientError(w, r, u, ferr)
					return
				}
				f = fresh
			}
			f.Error = "Запись изменена или повтор отличается — проверьте список клиентов и обновите страницу"
		case errors.Is(err, store.ErrFinancialWritesPaused):
			f.Error = "Запись финансовых операций временно приостановлена"
		case errors.Is(err, store.ErrMonthClosed):
			f.Error = "Месяц закрыт — изменения недоступны"
		default:
			f.Error = clientProblem(r, err)
			var ve *domain.ValidationError
			if errors.As(err, &ve) {
				f.Bad[ve.Field] = true
				switch ve.Problem {
				case domain.ProblemPaymentsMismatch:
					f.FieldErrors[ve.Field] = "Использование сертификата не больше стоимости услуг."
				case domain.ProblemCashToKassaOutOfRange:
					f.FieldErrors[ve.Field] = "Сумма взносов не больше стоимости услуг за вычетом сертификата."
					f.Bad["serviceCard"] = true
					f.FieldErrors["serviceCard"] = f.FieldErrors[ve.Field]
				default:
					f.FieldErrors[ve.Field] = f.Error
				}
			}
		}
	}
	for name := range f.Values {
		if f.Bad[name] {
			f.Incomplete = true
		}
	}
	a.formFailed(w, r, views.ClientFormView(f), views.ClientPage(a.navFor(u, "/"), f))
}

func clientEffect(rep store.SavedReport) string {
	if !rep.Client {
		return effectLine(rep.Report)
	}
	switch d := rep.Report.BalanceDelta; {
	case d > 0:
		return "По записи: салон должен вам " + domain.FormatEUR(d)
	case d < 0:
		return "По записи: вы должны салону " + domain.FormatEUR(-d)
	}
	return "По записи: расчёт с салоном €0"
}

// selfEquation spells out how a client record's «Себе» is made up:
// «Себе» €213,50 = €100 оставили себе наличными + €113,50 от салона.
func selfEquation(rep store.SavedReport) string {
	if !rep.Client {
		return ""
	}
	r := rep.Report
	s := "«Себе» " + domain.FormatEUR(r.SelfAmount) + " = " + domain.FormatEUR(r.CashKept()) + " оставили себе наличными"
	switch {
	case r.BalanceDelta > 0:
		s += " + " + domain.FormatEUR(r.BalanceDelta) + " от салона"
	case r.BalanceDelta < 0:
		s += " − " + domain.FormatEUR(-r.BalanceDelta) + " салону"
	}
	return s
}

// selfSplit is the «Ваши «Себе»» card of a person's own day or month.
func selfSplit(s domain.Summary, period string) views.SelfSplit {
	settle := views.MoneyRow{Name: "По записям расчёт с салоном", Value: "€0"}
	switch {
	case s.BalanceDelta > 0:
		settle = views.MoneyRow{Name: "По записям салон должен вам", Value: signedMoney(s.BalanceDelta)}
	case s.BalanceDelta < 0:
		settle = views.MoneyRow{Name: "По записям вы должны салону", Value: signedMoney(s.BalanceDelta)}
	}
	return views.SelfSplit{Period: period, Self: domain.FormatEUR(s.Self), Owed: sign(s.BalanceDelta),
		Rows: []views.MoneyRow{{Name: "Оставили себе наличными", Value: domain.FormatEUR(s.CashKept)}, settle}}
}

func clientProblem(r *http.Request, err error) string {
	var ve *domain.ValidationError
	if errors.As(err, &ve) {
		switch ve.Problem {
		case domain.ProblemPaymentsMismatch:
			return "Проверьте суммы: использование сертификата не больше услуг"
		case domain.ProblemCashToKassaOutOfRange:
			return "Проверьте суммы: взносы за услуги не больше услуг за вычетом сертификата"
		case domain.ProblemMissingTerms:
			return "Ваши условия («Себе» %) ещё не заданы — попросите администратора задать их"
		case domain.ProblemFutureDate:
			return "Клиента за будущий день сохранить нельзя"
		case domain.ProblemNegativeAmount:
			return "Введите сумму от €0 до €9 999 999,99"
		case domain.ProblemUnknownDestination:
			return "Выберите, куда внесены дополнительные продажи"
		}
	}
	return problemText(r, err)
}

// clientListURL is where an actor returns after removing a client: their own
// day, or the admin's salon day for someone else's record.
func clientListURL(u store.User, person string, day domain.Date) string {
	if person != u.ID {
		return "/admin/day?date=" + day.String() + "&removed=1"
	}
	return "/?date=" + day.String() + "&removed=1"
}

func (a *app) removeClientForm(r *http.Request, u store.User) (views.ClientRemoveForm, error) {
	id := r.PathValue("id")
	rep, err := a.store.ClientReport(r.Context(), u.Actor(), id)
	if err != nil {
		return views.ClientRemoveForm{}, err
	}
	owner, err := a.store.User(r.Context(), rep.Person)
	if err != nil {
		return views.ClientRemoveForm{}, err
	}
	dc, err := a.loadDay(r.Context(), owner, rep.Date)
	if err != nil {
		return views.ClientRemoveForm{}, err
	}
	return views.ClientRemoveForm{
		Action: "/clients/" + id + "/remove", Cancel: "/clients/" + id, Date: rep.Date.String(),
		Title: clientLabel(rep), DayLabel: ru.Date(rep.Date.Time()), Version: rep.Version,
		Total: domain.FormatEUR(rep.Report.Total), Cash: domain.FormatEUR(rep.Report.CashToKassa),
		Card:   domain.FormatEUR(rep.Report.CardIn + rep.Report.TransferToCard),
		Locked: dc.form(nil).Locked, Done: clientListURL(u, rep.Person, rep.Date),
	}, nil
}

func (a *app) handleRemoveClientPage(w http.ResponseWriter, r *http.Request, u store.User) {
	f, err := a.removeClientForm(r, u)
	if err != nil {
		a.clientError(w, r, u, err)
		return
	}
	a.render(w, r, views.ClientRemovePage(a.navFor(u, "/"), f))
}

func (a *app) handleRemoveClient(w http.ResponseWriter, r *http.Request, u store.User) {
	if err := readForm(w, r); err != nil {
		http.Error(w, "Слишком большой запрос", http.StatusRequestEntityTooLarge)
		return
	}
	f, err := a.removeClientForm(r, u)
	if errors.Is(err, store.ErrNotFound) {
		// Already removed (a repeated tap or a lost response): return to the
		// list without claiming this request removed anything.
		day, derr := domain.ParseDate(r.PostFormValue("date"))
		if derr != nil {
			http.NotFound(w, r)
			return
		}
		back := "/?date=" + day.String()
		if u.Actor().Admin {
			back = "/admin/day?date=" + day.String()
		}
		a.navigate(w, r, back)
		return
	}
	if err != nil {
		a.clientError(w, r, u, err)
		return
	}
	version, err := strconv.ParseInt(r.PostFormValue("version"), 10, 64)
	if err != nil {
		version = -1
	}
	f.Reason = strings.TrimSpace(r.PostFormValue("reason"))
	err = a.store.VoidClientReport(r.Context(), u.Actor(), r.PathValue("id"), version, f.Reason)
	var ve *domain.ValidationError
	switch {
	case err == nil:
		a.navigate(w, r, f.Done)
		return
	case errors.Is(err, store.ErrForbidden):
		a.clientError(w, r, u, err)
		return
	case errors.Is(err, store.ErrNotFound):
		a.navigate(w, r, f.Done)
		return
	case errors.Is(err, store.ErrVersionConflict):
		f.Error = "Запись изменена — проверьте суммы и подтвердите удаление снова"
	case errors.Is(err, store.ErrFinancialWritesPaused):
		f.Error = "Запись финансовых операций временно приостановлена"
	case errors.Is(err, store.ErrMonthClosed):
		f.Error = "Месяц закрыт — изменения недоступны"
	case errors.As(err, &ve) && ve.Problem == store.ProblemReasonTooLong:
		f.Error = "Причина — не длиннее 200 символов"
		f.BadReason = true
	case errors.As(err, &ve) && ve.Field == "reason":
		f.Error = "Укажите причину удаления"
		f.BadReason = true
	case errors.As(err, &ve):
		f.Error = clientProblem(r, err)
	default:
		a.serverError(w, r, err)
		return
	}
	a.renderStatus(w, r, http.StatusUnprocessableEntity, views.ClientRemovePage(a.navFor(u, "/"), f))
}
