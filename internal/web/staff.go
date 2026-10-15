package web

import (
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/a-h/templ"

	"github.com/lieranderl/aestheticlab-income-app/internal/domain"
	"github.com/lieranderl/aestheticlab-income-app/internal/ru"
	"github.com/lieranderl/aestheticlab-income-app/internal/store"
	"github.com/lieranderl/aestheticlab-income-app/internal/views"
)

var usernamePattern = regexp.MustCompile(`^[a-z0-9_-]{2,32}$`)

const maxNameRunes = 60

// The staff screens' notices after a redirect (?saved=…).
var staffNotices = map[string]string{ // #nosec G101 -- notice texts, not credentials
	"created":  "Сотрудник добавлен. Задайте условия: «Себе», зарплату и долю дивидендов.",
	"account":  "Учётная запись сохранена.",
	"password": "Пароль сброшен. Передайте временный пароль сотруднику лично.",
	"terms":    "Условия сохранены.",
}

func (a *app) handleStaff(w http.ResponseWriter, r *http.Request, admin store.User) {
	users, err := a.store.Users(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	month := a.termsMonthNow()
	rows := make([]views.StaffRow, 0, len(users))
	for _, u := range users {
		entries, err := a.store.Terms(r.Context(), u.ID)
		if err != nil {
			a.serverError(w, r, err)
			return
		}
		terms := "Условия не заданы"
		if t, ok := history(entries).For(month); ok {
			terms = termsLine(t)
		}
		rows = append(rows, views.StaffRow{
			ID: u.ID, Name: u.DisplayName, Username: u.Username, Role: roleLabel(u.Role),
			Reporter: u.SubmitsReports, Active: u.Active, Terms: terms,
		})
	}
	a.render(w, r, views.StaffPage(a.navFor(admin, "/admin/users"), rows, staffNotices[r.URL.Query().Get("saved")]))
}

func (a *app) handleNewStaffPage(w http.ResponseWriter, r *http.Request, admin store.User) {
	a.render(w, r, views.NewStaffPage(a.navFor(admin, "/admin/users"), views.NewStaffForm{Reporter: true}))
}

func (a *app) handleCreateStaff(w http.ResponseWriter, r *http.Request, admin store.User) {
	if err := readForm(w, r); err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	f := views.NewStaffForm{
		Username: strings.ToLower(strings.TrimSpace(r.PostFormValue("username"))),
		Name:     strings.TrimSpace(r.PostFormValue("name")),
		Admin:    r.PostFormValue("admin") == "1",
		Reporter: r.PostFormValue("reporter") == "1",
	}
	password := r.PostFormValue("password")
	fail := func(msg string) {
		f.Error = msg
		a.formFailed(w, r, views.NewStaffFormView(f), views.NewStaffPage(a.navFor(admin, "/admin/users"), f))
	}
	switch {
	case f.Name == "" || utf8.RuneCountInString(f.Name) > maxNameRunes:
		fail("Введите имя (до 60 символов)")
		return
	case !usernamePattern.MatchString(f.Username):
		fail("Имя пользователя: 2–32 символа, латиница в нижнем регистре, цифры, «-» или «_»")
		return
	case utf8.RuneCountInString(password) < minPassword:
		fail("Временный пароль должен быть не короче 8 символов")
		return
	}
	hash, err := a.hashPassword(password)
	if err != nil {
		fail(problemText(r, err))
		return
	}
	role := store.RoleEmployee
	if f.Admin {
		role = store.RoleAdmin
	}
	u, err := a.store.CreateUser(r.Context(), admin.Actor(), store.NewUser{
		Username: f.Username, DisplayName: f.Name, Role: role, SubmitsReports: f.Reporter, PasswordHash: hash,
	})
	if err != nil {
		fail(problemText(r, err))
		return
	}
	a.navigate(w, r, "/admin/users/"+u.ID+"?saved=created")
}

// staffEdit is the edit screen's state for person u, with forms as given.
func (a *app) staffEdit(r *http.Request, admin, u store.User) (views.StaffEdit, error) {
	entries, err := a.store.Terms(r.Context(), u.ID)
	if err != nil {
		return views.StaffEdit{}, err
	}
	months, err := a.termsMonths(r, u)
	if err != nil {
		return views.StaffEdit{}, err
	}
	now := a.termsMonthNow()
	h := history(entries)
	current, hasCurrent := h.For(now)
	var currentFrom domain.Month
	rows := make([]views.TermsRow, 0, len(entries))
	for _, e := range entries {
		if hasCurrent && !now.Before(e.From) {
			currentFrom = e.From
		}
	}
	for i := len(entries) - 1; i >= 0; i-- { // newest first
		e := entries[i]
		rows = append(rows, views.TermsRow{
			From: ru.SinceMonth(e.From.Year, e.From.Mon), SelfPct: percent(e.Terms.SelfPct), Salary: domain.FormatEUR(e.Terms.SalaryCents),
			Share: strconv.FormatInt(e.Terms.DividendWeight, 10), High: highSelfText(e.Terms), Note: e.Note,
			Current: hasCurrent && e.From == currentFrom,
		})
	}
	form := views.TermsForm{Months: months, Month: a.nextTermsMonth(months)}
	if hasCurrent {
		form.SelfPct, form.Salary = plainPercent(current.SelfPct), plainEuros(current.SalaryCents)
		form.Share = strconv.FormatInt(current.DividendWeight, 10)
		if current.HighSelfFromCents > 0 {
			form.HighFrom, form.HighPct = plainEuros(current.HighSelfFromCents), plainPercent(current.HighSelfPct)
		}
	}
	form.ShareNote, _ = shareNote(form)
	return views.StaffEdit{
		ID: u.ID, Name: u.DisplayName, Username: u.Username, Self: u.ID == admin.ID,
		Account:  views.AccountForm{Name: u.DisplayName, Admin: u.Role == store.RoleAdmin, Reporter: u.SubmitsReports, Active: u.Active},
		Terms:    rows,
		NewTerms: form,
	}, nil
}

// staffTarget reads the person a /admin/users/{uid} route names.
func (a *app) staffTarget(w http.ResponseWriter, r *http.Request) (store.User, bool) {
	u, err := a.store.User(r.Context(), r.PathValue("uid"))
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return store.User{}, false
	}
	if err != nil {
		a.serverError(w, r, err)
		return store.User{}, false
	}
	return u, true
}

func (a *app) handleStaffEdit(w http.ResponseWriter, r *http.Request, admin store.User) {
	u, ok := a.staffTarget(w, r)
	if !ok {
		return
	}
	s, err := a.staffEdit(r, admin, u)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	s.Notice = staffNotices[r.URL.Query().Get("saved")]
	a.render(w, r, views.StaffEditPage(a.navFor(admin, "/admin/users"), s))
}

func (a *app) handleUpdateStaff(w http.ResponseWriter, r *http.Request, admin store.User) {
	u, ok := a.staffTarget(w, r)
	if !ok {
		return
	}
	if err := readForm(w, r); err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	f := views.AccountForm{
		Name:     strings.TrimSpace(r.PostFormValue("name")),
		Admin:    r.PostFormValue("admin") == "1",
		Reporter: r.PostFormValue("reporter") == "1",
		Active:   r.PostFormValue("active") == "1",
	}
	self := u.ID == admin.ID
	fail := func(msg string) {
		f.Error = msg
		a.staffFormFailed(w, r, admin, u, views.AccountFormView(u.ID, self, f), func(s *views.StaffEdit) { s.Account = f })
	}
	switch {
	case f.Name == "" || utf8.RuneCountInString(f.Name) > maxNameRunes:
		fail("Введите имя (до 60 символов)")
		return
	case self && (!f.Admin || !f.Active):
		fail("Свои права администратора и доступ отключить нельзя")
		return
	}
	_, err := a.store.UpdateUser(r.Context(), admin.Actor(), u.ID, func(x *store.User) error {
		x.DisplayName, x.SubmitsReports = f.Name, f.Reporter
		x.Role = store.RoleEmployee
		if f.Admin {
			x.Role = store.RoleAdmin
		}
		if x.Active && !f.Active {
			x.SessionVersion++ // out on every device at once
		}
		x.Active = f.Active
		return nil
	})
	if err != nil {
		fail(problemText(r, err))
		return
	}
	a.navigate(w, r, "/admin/users/"+u.ID+"?saved=account")
}

// handleResetPassword sets a temporary password: the person is logged out
// everywhere, unlocked, and must choose a new password at the next login.
func (a *app) handleResetPassword(w http.ResponseWriter, r *http.Request, admin store.User) {
	u, ok := a.staffTarget(w, r)
	if !ok {
		return
	}
	if err := readForm(w, r); err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	fail := func(msg string) {
		f := views.ResetForm{Error: msg}
		a.staffFormFailed(w, r, admin, u, views.ResetFormView(u.ID, u.Username, f), func(s *views.StaffEdit) { s.Password = f })
	}
	// A reset needs no current password, so a stolen admin session must not
	// be able to take over the admin's own account with it.
	if u.ID == admin.ID {
		fail("Свой пароль меняйте в профиле")
		return
	}
	password := r.PostFormValue("password")
	if utf8.RuneCountInString(password) < minPassword {
		fail("Временный пароль должен быть не короче 8 символов")
		return
	}
	hash, err := a.hashPassword(password)
	if err != nil {
		fail(problemText(r, err))
		return
	}
	_, err = a.store.UpdateUser(r.Context(), admin.Actor(), u.ID, func(x *store.User) error {
		x.PasswordHash, x.MustChangePassword = hash, true
		x.SessionVersion++
		x.FailedLogins, x.LockedUntil = 0, time.Time{}
		return nil
	})
	if err != nil {
		fail(problemText(r, err))
		return
	}
	a.navigate(w, r, "/admin/users/"+u.ID+"?saved=password")
}

func (a *app) handleSetTerms(w http.ResponseWriter, r *http.Request, admin store.User) {
	u, ok := a.staffTarget(w, r)
	if !ok {
		return
	}
	if err := readForm(w, r); err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	months, err := a.termsMonths(r, u)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	f := views.TermsForm{
		Months: months, Month: r.PostFormValue("month"),
		SelfPct: strings.TrimSpace(r.PostFormValue("selfPct")), Salary: strings.TrimSpace(r.PostFormValue("salary")),
		Share: strings.TrimSpace(r.PostFormValue("share")), Note: strings.TrimSpace(r.PostFormValue("note")),
		HighFrom: strings.TrimSpace(r.PostFormValue("highFrom")), HighPct: strings.TrimSpace(r.PostFormValue("highPct")),
	}
	f.ShareNote, f.ShareBad = shareNote(f)
	fail := func(msg string) {
		f.Error = msg
		a.staffFormFailed(w, r, admin, u, views.TermsFormView(u.ID, f), func(s *views.StaffEdit) { s.NewTerms = f })
	}
	m, err := domain.ParseMonth(f.Month)
	if err != nil {
		fail("Выберите месяц")
		return
	}
	pct, err := domain.ParseAmount(f.SelfPct) // "50" or "62,5" → basis points
	if err != nil || pct > 10000 {
		fail("«Себе» — число от 0 до 100")
		return
	}
	salary, err := domain.ParseAmount(f.Salary)
	if err != nil {
		fail("Зарплата — сумма в евро, например 1000 или 1000,50")
		return
	}
	share, ok := parseShare(f.Share)
	if !ok {
		fail("Доля в прибыли — целое число от 0 до 1000")
		return
	}
	highFrom, err := domain.ParseAmount(f.HighFrom)
	if err != nil {
		fail("Порог — сумма в евро за месяц, например 5000")
		return
	}
	highPct, err := domain.ParseAmount(f.HighPct)
	switch {
	case err != nil || highPct > 10000:
		fail("Повышенный «Себе» — число от 0 до 100")
		return
	case (highFrom > 0) != (highPct > 0):
		fail("Для повышенного «Себе» укажите и порог, и процент — или оставьте оба поля пустыми")
		return
	case highFrom > 0 && highPct <= pct:
		fail("Повышенный «Себе» должен быть больше обычного")
		return
	}
	if utf8.RuneCountInString(f.Note) > 200 {
		fail("Комментарий — до 200 символов")
		return
	}
	terms := domain.Terms{
		SelfPct: domain.BasisPoints(pct), SalaryCents: salary, DividendWeight: share,
		HighSelfFromCents: highFrom, HighSelfPct: domain.BasisPoints(highPct),
	}
	if err := a.store.SetTerms(r.Context(), admin.Actor(), u.ID, m, terms, f.Note); err != nil {
		fail(problemText(r, err))
		return
	}
	a.navigate(w, r, "/admin/users/"+u.ID+"?saved=terms")
}

// staffFormFailed shows one of the edit screen's forms again with errors.
func (a *app) staffFormFailed(w http.ResponseWriter, r *http.Request, admin, u store.User, form templ.Component, apply func(*views.StaffEdit)) {
	s, err := a.staffEdit(r, admin, u)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	apply(&s)
	a.formFailed(w, r, form, views.StaffEditPage(a.navFor(admin, "/admin/users"), s))
}

// termsMonthNow is the month whose terms are «Сейчас»: this month, or the
// start month before go-live.
func (a *app) termsMonthNow() domain.Month {
	m := a.store.Today().Month()
	if start := a.store.StartMonth(); m.Before(start) {
		return start
	}
	return m
}

// termsMonths are the months new terms for u may start in: the first open
// month up to two months after this one. Each names everyone else's profit
// share in force that month, for the form's preview.
func (a *app) termsMonths(r *http.Request, u store.User) ([]views.MonthOption, error) {
	closed, err := a.store.ClosedMonths(r.Context())
	if err != nil {
		return nil, err
	}
	users, err := a.store.Users(r.Context())
	if err != nil {
		return nil, err
	}
	type person struct {
		name  string
		terms domain.TermsHistory
	}
	var others []person
	for _, o := range users {
		if o.ID == u.ID {
			continue
		}
		entries, err := a.store.Terms(r.Context(), o.ID)
		if err != nil {
			return nil, err
		}
		others = append(others, person{o.DisplayName, history(entries)})
	}
	slices.SortFunc(others, func(x, y person) int { return strings.Compare(x.name, y.name) })
	from := a.store.StartMonth()
	if len(closed) > 0 {
		from = closed[len(closed)-1].Month.Next()
	}
	to := a.termsMonthNow()
	if to.Before(from) {
		to = from
	}
	to = to.Next().Next()
	var out []views.MonthOption
	for m := from; !to.Before(m); m = m.Next() {
		opt := views.MonthOption{Value: m.String(), Label: monthLabel(m)}
		var names []string
		for _, o := range others {
			if t, ok := o.terms.For(m); ok && t.DividendWeight > 0 {
				opt.OthersShare += t.DividendWeight
				names = append(names, fmt.Sprintf("%s %d", o.name, t.DividendWeight))
			}
		}
		opt.Others = "ни у кого нет доли"
		if len(names) > 0 {
			opt.Others = strings.Join(names, " · ")
		}
		out = append(out, opt)
	}
	return out, nil
}

// nextTermsMonth is the month the terms form starts on: next month, or the
// first month on offer when that one is already closed.
func (a *app) nextTermsMonth(months []views.MonthOption) string {
	next := a.store.Today().Month().Next().String()
	for _, m := range months {
		if m.Value >= next { // YYYY-MM sorts as text
			return m.Value
		}
	}
	if len(months) > 0 {
		return months[len(months)-1].Value
	}
	return ""
}

var sharePattern = regexp.MustCompile(`^[0-9]{1,4}$`)

// parseShare reads a profit share: a whole number from 0 to 1000.
func parseShare(s string) (int64, bool) {
	if !sharePattern.MatchString(s) {
		return 0, false
	}
	n, err := strconv.ParseInt(s, 10, 64)
	return n, err == nil && n <= 1000
}

// shareNote says what the form's share means against everyone else's in the
// chosen month, and whether the share is invalid. views.shareNoteExpr says
// the same in the browser as the admin types.
func shareNote(f views.TermsForm) (string, bool) {
	var others int64
	for _, m := range f.Months {
		if m.Value == f.Month {
			others = m.OthersShare
		}
	}
	if f.Share == "" {
		return "Целое число от 0 до 1000; 0 — без дивидендов.", false
	}
	share, ok := parseShare(f.Share)
	switch {
	case !ok:
		return "Введите целое число от 0 до 1000.", true
	case share == 0 && others > 0:
		return "Без дивидендов: прибыль делится между остальными.", false
	case share == 0:
		return "Ни у кого нет доли: прибыль останется в салоне.", false
	case others == 0:
		return "Получит всю прибыль месяца.", false
	}
	total := share + others
	tenths := (share*2000 + total) / (2 * total) // share/total in tenths of a percent, half up
	return fmt.Sprintf("Получит %s%% прибыли месяца, остальные — %s%%.", tenthsText(tenths), tenthsText(1000-tenths)), false
}

// tenthsText shows tenths of a percent as «66,7» or «50».
func tenthsText(n int64) string {
	s := strconv.FormatInt(n/10, 10)
	if n%10 != 0 {
		s += "," + strconv.FormatInt(n%10, 10)
	}
	return s
}

func history(entries []store.TermsEntry) domain.TermsHistory {
	h := domain.TermsHistory{}
	for _, e := range entries {
		h[e.From] = e.Terms
	}
	return h
}

func monthLabel(m domain.Month) string { return ru.MonthYear(m.Year, m.Mon) }

func termsLine(t domain.Terms) string {
	self := percent(t.SelfPct)
	if h := highSelfText(t); h != "" {
		self += " (" + h + ")"
	}
	return fmt.Sprintf("«Себе» %s · зарплата %s · доля %d", self, domain.FormatEUR(t.SalaryCents), t.DividendWeight)
}

// highSelfText is the higher «Себе» rate: «80% при базе больше €5 000», or "".
func highSelfText(t domain.Terms) string {
	if t.HighSelfFromCents <= 0 {
		return ""
	}
	return percent(t.HighSelfPct) + " при базе больше " + domain.FormatEUR(t.HighSelfFromCents)
}

// percent shows basis points as «50%» or «62,5%».
func percent(bp domain.BasisPoints) string { return plainPercent(bp) + "%" }

// plainPercent is the number a form field shows: «50» or «62,5».
func plainPercent(bp domain.BasisPoints) string { return plainEuros(domain.Cents(bp)) }

// plainEuros is an amount as typed: «1000» or «1000,5».
func plainEuros(c domain.Cents) string {
	s := strconv.FormatInt(int64(c/100), 10)
	if frac := c % 100; frac != 0 {
		s += "," + strings.TrimRight(fmt.Sprintf("%02d", frac), "0")
	}
	return s
}
