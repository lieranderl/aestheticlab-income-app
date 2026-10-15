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
	"github.com/starfederation/datastar-go/datastar"
)

func movementKind(k string, admin bool) bool {
	return k == string(domain.Deposit) || k == string(domain.Expense) || (admin && (k == string(domain.Payout) || k == string(domain.Transfer)))
}
func movementBack(admin bool) string {
	if admin {
		return "/admin/kassa"
	}
	return "/my"
}
func accountMovementName(c store.Account, admin bool) string {
	if admin {
		return c.Name
	}
	if c.Kind == store.AccountCash {
		return "Касса — наличные"
	}
	return "Касса — карта"
}

func partyValue(p domain.Party) string {
	if p.ID == "" {
		return string(p.Kind)
	}
	return string(p.Kind) + ":" + p.ID
}
func movementParty(v string) domain.Party {
	k, id, _ := strings.Cut(v, ":")
	return domain.Party{Kind: domain.PartyKind(k), ID: id}
}

func (a *app) movementForm(r *http.Request, u store.User, admin bool, kind string) (views.MovementFormData, error) {
	f := views.MovementFormData{
		Action:  "/movements",
		Back:    movementBack(admin),
		Admin:   admin,
		Type:    kind,
		Date:    a.store.Today().String(),
		MinDate: a.store.StartMonth().FirstDay().String(),
		MaxDate: a.store.Today().String(),
	}
	if admin {
		f.Action = "/admin/movements"
	}
	accounts, err := a.store.Accounts(r.Context())
	if err != nil {
		return f, err
	}
	var accs, people []views.FilterOption
	for _, c := range accounts {
		if c.Active {
			accs = append(accs, views.FilterOption{Value: "account:" + c.ID, Label: accountMovementName(c, admin)})
		}
	}
	if admin {
		users, err := a.store.Users(r.Context())
		if err != nil {
			return f, err
		}
		for _, p := range users {
			people = append(people, views.FilterOption{Value: "person:" + p.ID, Label: p.DisplayName})
		}
	} else {
		people = []views.FilterOption{{Value: "person:" + u.ID, Label: "За свой счёт"}}
	}
	switch domain.MovementType(kind) {
	case domain.Payout:
		f.Sources, f.Destinations = accs, people
	case domain.Deposit:
		f.Sources, f.Destinations = people, accs
	case domain.Transfer:
		f.Sources, f.Destinations = accs, accs
	case domain.Expense:
		f.Sources = people
		if admin {
			f.Sources = append(accs, people...)
		}
		f.Destinations = []views.FilterOption{{Value: "outside", Label: "Расход"}}
		cats, err := a.store.Categories(r.Context())
		if err != nil {
			return f, err
		}
		for _, c := range cats {
			if c.Active && c.Kind == store.KindExpense {
				f.Categories = append(f.Categories, views.FilterOption{Value: c.ID, Label: c.Name})
			}
		}
	}
	if len(f.Sources) > 0 {
		f.From = f.Sources[0].Value
	}
	if len(f.Destinations) > 0 {
		f.To = f.Destinations[0].Value
	}
	if kind == string(domain.Transfer) && len(f.Destinations) > 1 {
		f.To = f.Destinations[1].Value
	}
	return f, nil
}
func (a *app) handleMovementPage(w http.ResponseWriter, r *http.Request, u store.User) {
	admin := strings.HasPrefix(r.URL.Path, "/admin/")
	kind := r.URL.Query().Get("type")
	if kind == "" {
		kind = "deposit"
	}
	if !movementKind(kind, admin) {
		http.Error(w, "Проверьте вид движения", http.StatusBadRequest)
		return
	}
	f, err := a.movementForm(r, u, admin, kind)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if err := a.prefillSettlement(r, &f); err != nil {
		http.Error(w, "Проверьте месяц, сотрудника и текущий остаток — обновите расчёт", http.StatusBadRequest)
		return
	}
	if isDatastar(r) {
		sse := datastar.NewSSE(w, r)
		if err := sse.PatchElementTempl(views.MovementForm(f)); err != nil {
			a.serverError(w, r, err)
			return
		}
		if err := sse.MarshalAndPatchSignals(map[string]any{"_movementready": true, "_movementloading": false}); err != nil {
			a.serverError(w, r, err)
		}
		return
	}
	a.render(w, r, views.MovementFormPage(a.navFor(u, f.Back), f))
}
func (a *app) handleRecordMovement(w http.ResponseWriter, r *http.Request, u store.User) {
	if err := readForm(w, r); err != nil {
		http.Error(w, "Проверьте поля формы", http.StatusBadRequest)
		return
	}
	admin := strings.HasPrefix(r.URL.Path, "/admin/")
	kind := r.PostFormValue("type")
	if !movementKind(kind, admin) {
		http.Error(w, "Проверьте вид движения", http.StatusBadRequest)
		return
	}
	if !admin {
		for _, key := range []string{"person", "personId", "uid"} {
			if v := r.PostFormValue(key); v != "" && v != u.ID {
				http.Error(w, "Нет доступа", http.StatusForbidden)
				return
			}
		}
		if movementParty(r.PostFormValue("from")) != domain.Person(u.ID) {
			http.Error(w, "Нет доступа", http.StatusForbidden)
			return
		}
	}
	f, err := a.movementForm(r, u, admin, kind)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if err := settlementContext(r, &f); err != nil {
		http.Error(w, "Проверьте контекст расчёта", http.StatusBadRequest)
		return
	}
	f.Amount = r.PostFormValue("amount")
	f.Date = r.PostFormValue("date")
	f.From = r.PostFormValue("from")
	f.To = r.PostFormValue("to")
	f.Category = r.PostFormValue("expenseCategory")
	f.Note = r.PostFormValue("note")
	amount, err := domain.ParseAmount(f.Amount)
	if err == nil && strings.TrimSpace(f.Amount) == "" {
		err = store.ErrInvalid
	}
	var day domain.Date
	if err == nil {
		day, err = domain.ParseDate(f.Date)
		if err != nil {
			err = store.ErrInvalid
		}
	}
	if err == nil {
		n := store.NewMovement{
			Type:            domain.MovementType(kind),
			Amount:          amount,
			Date:            day,
			From:            movementParty(f.From),
			To:              movementParty(f.To),
			ExpenseCategory: f.Category,
			Note:            f.Note,
		}
		if f.SettlementMonth != "" {
			m, _ := domain.ParseMonth(f.SettlementMonth)
			_, err = a.store.RecordSettlementMovement(r.Context(), u.Actor(), m, f.SettlementPerson, f.SettlementRevision, n)
		} else {
			_, err = a.store.RecordMovement(r.Context(), u.Actor(), n)
		}
	}
	if err != nil {
		f.Error = movementProblem(r, err)
		status := http.StatusUnprocessableEntity
		if errors.Is(err, store.ErrVersionConflict) {
			status = http.StatusConflict
		}
		a.formFailedStatus(w, r, status, views.MovementForm(f), views.MovementFormPage(a.navFor(u, f.Back), f))
		return
	}
	saved := "1"
	if !u.Actor().Admin {
		saved = "pending"
	}
	separator := "?"
	if strings.Contains(f.Back, "?") {
		separator = "&"
	}
	a.navigate(w, r, f.Back+separator+"saved="+saved)
}

func movementProblem(r *http.Request, err error) string {
	var v *domain.ValidationError
	if errors.Is(err, domain.ErrInvalidAmount) {
		return "Проверьте сумму — например, 100,50"
	}
	if errors.As(err, &v) {
		switch v.Problem {
		case store.ProblemSettlementAmount:
			return "Сумма больше текущего остатка — уменьшите сумму или обновите расчёт"
		case store.ProblemNoteRequired:
			return "Укажите комментарий или причину отмены"
		case store.ProblemNotPending:
			return "Движение уже обработано — обновите страницу"
		case store.ProblemAlreadyVoided:
			return "Движение уже отменено"
		default:
			return "Проверьте сумму, дату, вид движения и участников"
		}
	}
	return problemText(r, err)
}
func movementTitle(t domain.MovementType) string {
	switch t {
	case domain.Deposit:
		return "Внесено в кассу"
	case domain.Payout:
		return "Выдано из кассы"
	case domain.Expense:
		return "Расход"
	case domain.Transfer:
		return "Перевод между счетами"
	case domain.Adjustment:
		return "Пересчёт кассы"
	case domain.Opening:
		return "Начальный остаток"
	case domain.Salary:
		return "Зарплата"
	case domain.SelfBonus:
		return "Доплата «Себе»"
	case domain.Dividend:
		return "Дивиденды"
	}
	return "Движение"
}
func movementStatus(s domain.MovementStatus) string {
	switch s {
	case domain.Pending:
		return "Ожидает подтверждения"
	case domain.Voided:
		return "Отменено"
	}
	return "Подтверждено"
}
func (a *app) movementCards(r *http.Request, u store.User, es []store.Entry, admin bool) ([]views.MovementCard, error) {
	names := map[string]string{"outside": "Расход", "salon": "Салон", "certificates": "Сертификаты"}
	accs, err := a.store.Accounts(r.Context())
	if err != nil {
		return nil, err
	}
	for _, c := range accs {
		names["account:"+c.ID] = accountMovementName(c, admin)
	}
	if admin {
		users, err := a.store.Users(r.Context())
		if err != nil {
			return nil, err
		}
		for _, p := range users {
			names["person:"+p.ID] = p.DisplayName
		}
	} else {
		names["person:"+u.ID] = "За свой счёт"
	}
	states := map[domain.Month]domain.MonthStatus{}
	out := make([]views.MovementCard, 0, len(es))
	for _, e := range es {
		c := views.MovementCard{
			ID:         e.ID,
			Title:      movementTitle(e.Type),
			Date:       ru.Date(e.Date.Time()),
			Amount:     domain.FormatEUR(e.Amount),
			Parties:    names[partyValue(e.From)] + " → " + names[partyValue(e.To)],
			Note:       e.Note,
			Status:     movementStatus(e.Status),
			VoidReason: e.VoidReason,
			Version:    e.Version,
		}
		status, ok := states[e.Date.Month()]
		if !ok {
			m, err := a.store.Month(r.Context(), e.Date.Month())
			if err != nil {
				return nil, err
			}
			status = m.Status
			states[e.Date.Month()] = status
		}
		mayVoid := u.Actor().Admin || (e.CreatedBy == u.ID && e.Status == domain.Pending)
		if status != domain.MonthClosed && !domain.IsAccrual(e.Type) && e.Status != domain.Voided && mayVoid {
			c.VoidURL = "/movements/" + e.ID + "/void"
		}
		if admin && status != domain.MonthClosed && e.Status == domain.Pending {
			c.ConfirmURL = "/admin/movements/" + e.ID + "/confirm"
		}
		out = append(out, c)
	}
	return out, nil
}
func (a *app) handleKassa(w http.ResponseWriter, r *http.Request, u store.User) {
	bal, err := a.store.Balances(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	accs, err := a.store.Accounts(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	p := views.KassaData{
		AsOf:               ru.Date(a.store.Today().Time()),
		Certificates:       domain.FormatEUR(bal.CertificatesOutstanding),
		CertificateWarning: bal.CertificatesOutstanding < 0,
	}
	if r.URL.Query().Get("saved") == "1" {
		p.Notice = "Сохранено ✓"
	}
	for _, c := range accs {
		if !c.Active {
			continue
		}
		last := "Пересчёта ещё не было"
		if !c.LastRecountAt.IsZero() {
			last = "Последний пересчёт: " + ru.Date(a.store.LocalTime(c.LastRecountAt))
		}
		path := "/admin/accounts/" + c.ID + "/recount"
		p.Accounts = append(p.Accounts, views.AccountCard{
			Name:    c.Name,
			Balance: domain.FormatEUR(bal.Accounts[c.ID]),
			Last:    last,
			Recount: path,
			Form:    views.RecountFormData{ID: c.ID, Action: path, Name: c.Name, Expected: domain.FormatEUR(bal.Accounts[c.ID])},
		})
	}
	pending, err := a.store.PendingMovements(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	p.Pending, err = a.movementCards(r, u, pending, true)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	es, err := a.store.LedgerBetween(r.Context(), a.store.StartMonth().FirstDay(), a.store.Today())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	es = slices.DeleteFunc(es, func(e store.Entry) bool { return e.Status == domain.Pending })
	slices.Reverse(es)
	if len(es) > 10 {
		es = es[:10]
	}
	p.Recent, err = a.movementCards(r, u, es, true)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	p.Form, err = a.movementForm(r, u, true, "deposit")
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	a.render(w, r, views.KassaPage(a.navFor(u, "/admin/kassa"), p))
}

func (a *app) movementAction(r *http.Request, u store.User, void bool) (views.MovementActionData, error) {
	e, err := a.store.Entry(r.Context(), r.PathValue("id"))
	if err != nil {
		return views.MovementActionData{}, err
	}
	if !u.Actor().Admin && (e.CreatedBy != u.ID || e.PersonID != u.ID) {
		return views.MovementActionData{}, store.ErrForbidden
	}
	if domain.IsAccrual(e.Type) {
		return views.MovementActionData{}, store.ErrForbidden
	}
	p := views.MovementActionData{
		ID:      e.ID,
		Version: e.Version,
		Void:    void,
		Title:   "Подтвердить",
		Action:  "/admin/movements/" + e.ID + "/confirm",
		Back:    movementBack(u.Actor().Admin),
	}
	if void {
		p.Title = "Отменить движение"
		p.Action = "/movements/" + e.ID + "/void"
	}
	cs, err := a.movementCards(r, u, []store.Entry{e}, u.Actor().Admin)
	if err != nil {
		return p, err
	}
	p.Card = cs[0]
	p.Card.VoidURL, p.Card.ConfirmURL = "", ""
	p.Processed = !u.Actor().Admin && e.Status != domain.Pending
	if p.Processed {
		p.Error = "Движение уже обработано — проверьте текущие движения"
	}
	return p, nil
}
func actionReadError(w http.ResponseWriter, r *http.Request, err error) bool {
	if errors.Is(err, store.ErrForbidden) {
		http.Error(w, "Нет доступа", http.StatusForbidden)
		return true
	}
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return true
	}
	return false
}
func (a *app) handleVoidPage(w http.ResponseWriter, r *http.Request, u store.User) {
	p, err := a.movementAction(r, u, true)
	if err != nil {
		if !actionReadError(w, r, err) {
			a.serverError(w, r, err)
		}
		return
	}
	if p.Processed {
		a.renderStatus(w, r, http.StatusConflict, views.MovementActionPage(a.navFor(u, p.Back), p))
		return
	}
	a.render(w, r, views.MovementActionPage(a.navFor(u, p.Back), p))
}
func (a *app) handleConfirmMovement(w http.ResponseWriter, r *http.Request, u store.User) {
	a.handleMovementAction(w, r, u, false)
}
func (a *app) handleVoidMovement(w http.ResponseWriter, r *http.Request, u store.User) {
	a.handleMovementAction(w, r, u, true)
}
func (a *app) handleMovementAction(w http.ResponseWriter, r *http.Request, u store.User, void bool) {
	p, err := a.movementAction(r, u, void)
	if err != nil {
		if !actionReadError(w, r, err) {
			a.serverError(w, r, err)
		}
		return
	}
	if err = readForm(w, r); err != nil {
		http.Error(w, "Проверьте поля формы", http.StatusBadRequest)
		return
	}
	p.Reason = r.PostFormValue("reason")
	if p.Processed {
		a.formFailedStatus(w, r, http.StatusConflict, views.MovementActionForm(p), views.MovementActionPage(a.navFor(u, p.Back), p))
		return
	}
	version, err := strconv.ParseInt(r.PostFormValue("version"), 10, 64)
	if err != nil || version < 1 {
		err = store.ErrInvalid
	}
	if err == nil {
		if void {
			_, err = a.store.VoidMovement(r.Context(), u.Actor(), p.ID, version, p.Reason)
		} else {
			_, err = a.store.ConfirmMovement(r.Context(), u.Actor(), p.ID, version)
		}
	}
	if err != nil {
		// Confirmation can race the read above; keep an owned employee's reason on the conflict form.
		if void && !u.Actor().Admin && errors.Is(err, store.ErrForbidden) {
			latest, readErr := a.movementAction(r, u, true)
			if readErr != nil {
				if !actionReadError(w, r, readErr) {
					a.serverError(w, r, readErr)
				}
				return
			}
			if latest.Processed {
				latest.Reason = p.Reason
				a.formFailedStatus(w, r, http.StatusConflict, views.MovementActionForm(latest), views.MovementActionPage(a.navFor(u, latest.Back), latest))
				return
			}
		}
		p.Error = movementProblem(r, err)
		// Patch a stable action container present on the source cards as well as the fallback page.
		a.formFailed(w, r, views.MovementActionForm(p), views.MovementActionPage(a.navFor(u, p.Back), p))
		return
	}
	a.navigate(w, r, p.Back+"?saved=1")
}

func (a *app) recountForm(r *http.Request) (views.RecountFormData, error) {
	id := r.PathValue("id")
	accs, err := a.store.Accounts(r.Context())
	if err != nil {
		return views.RecountFormData{}, err
	}
	for _, c := range accs {
		if c.ID == id && c.Active {
			b, err := a.store.Balances(r.Context())
			return views.RecountFormData{
				ID:       id,
				Action:   "/admin/accounts/" + id + "/recount",
				Name:     c.Name,
				Expected: domain.FormatEUR(b.Accounts[id]),
			}, err
		}
	}
	return views.RecountFormData{}, store.ErrNotFound
}
func (a *app) handleRecountPage(w http.ResponseWriter, r *http.Request, u store.User) {
	p, err := a.recountForm(r)
	if err != nil {
		if !actionReadError(w, r, err) {
			a.serverError(w, r, err)
		}
		return
	}
	a.render(w, r, views.RecountPage(a.navFor(u, "/admin/kassa"), p))
}
func (a *app) handleRecount(w http.ResponseWriter, r *http.Request, u store.User) {
	if err := readForm(w, r); err != nil {
		http.Error(w, "Проверьте поля формы", http.StatusBadRequest)
		return
	}
	p, err := a.recountForm(r)
	if err != nil {
		if !actionReadError(w, r, err) {
			a.serverError(w, r, err)
		}
		return
	}
	p.Counted, p.Note = r.PostFormValue("counted"), r.PostFormValue("note")
	c, err := domain.ParseAmount(p.Counted)
	if err == nil && strings.TrimSpace(p.Counted) == "" {
		err = store.ErrInvalid
	}
	if err == nil {
		_, err = a.store.Recount(r.Context(), u.Actor(), p.ID, c, p.Note)
	}
	if err != nil {
		p.Error = movementProblem(r, err)
		a.formFailed(w, r, views.RecountForm(p), views.RecountPage(a.navFor(u, "/admin/kassa"), p))
		return
	}
	a.navigate(w, r, "/admin/kassa?saved=1")
}
