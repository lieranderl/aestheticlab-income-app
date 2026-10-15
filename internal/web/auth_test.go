package web_test

import (
	"context"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/lieranderl/aestheticlab-income-app/internal/domain"
	"github.com/lieranderl/aestheticlab-income-app/internal/store"
	"github.com/lieranderl/aestheticlab-income-app/internal/web"
)

var ctx = context.Background()

// site is the app on the in-memory store at 20 November 2026, with Anna
// (admin, reports), Olga and Maria (staff), all with their own passwords.
type site struct {
	t                 *testing.T
	h                 http.Handler
	st                *store.Store
	clock             *time.Time
	anna, olga, maria store.User
}

const (
	annaPass  = "anna-password"
	olgaPass  = "olga-password"
	mariaPass = "maria-password"
)

func newSite(t *testing.T) *site { return newSiteOptions(t) }

func newSiteOptions(t *testing.T, opts ...store.Option) *site {
	t.Helper()
	clock := time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)
	now := func() time.Time { return clock }
	st := store.NewMemory(append(opts, store.WithClock(now))...)
	s := &site{t: t, st: st, clock: &clock}
	anna, err := st.SetUp(ctx, store.DefaultSetup(store.NewUser{
		Username: "anna", DisplayName: "Анна", Role: store.RoleAdmin, SubmitsReports: true, PasswordHash: hash(t, annaPass),
	}))
	if err != nil {
		t.Fatal(err)
	}
	s.anna = s.settle(anna)
	s.olga = s.person("olga", "Ольга", olgaPass)
	s.maria = s.person("maria", "Мария", mariaPass)
	s.h = handler(t, web.Config{
		BaseURL: "https://kassa.example.com", Store: st, Now: now, PasswordCost: bcrypt.MinCost,
		SessionKey: []byte("test-session-key-test-session-key-0123456789"),
	})
	return s
}

func hash(t *testing.T, password string) string {
	t.Helper()
	h, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	return string(h)
}

// person adds a staff member who already chose their own password.
func (s *site) person(username, name, password string) store.User {
	s.t.Helper()
	u, err := s.st.CreateUser(ctx, s.anna.Actor(), store.NewUser{Username: username, DisplayName: name, Role: store.RoleEmployee, SubmitsReports: true, PasswordHash: hash(s.t, password)})
	if err != nil {
		s.t.Fatal(err)
	}
	return s.settle(u)
}

// settle clears the first-login password change.
func (s *site) settle(u store.User) store.User {
	s.t.Helper()
	u, err := s.st.UpdateUser(ctx, store.Actor{UID: u.ID, Admin: true}, u.ID, func(x *store.User) error {
		x.MustChangePassword = false
		return nil
	})
	if err != nil {
		s.t.Fatal(err)
	}
	return u
}

// request is one browser request; datastar marks it as sent by Datastar.
type request struct {
	method   string
	path     string
	form     url.Values
	cookies  []*http.Cookie
	datastar bool
	from     string // X-Forwarded-For, as Cloud Run passes it on
}

func (s *site) do(r request) *httptest.ResponseRecorder {
	s.t.Helper()
	var body *strings.Reader
	if r.form != nil {
		body = strings.NewReader(r.form.Encode())
	} else {
		body = strings.NewReader("")
	}
	req := httptest.NewRequest(r.method, r.path, body)
	if r.form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if r.datastar {
		req.Header.Set("Datastar-Request", "true")
	}
	if r.from != "" {
		req.Header.Set("X-Forwarded-For", r.from)
	}
	for _, c := range r.cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	s.h.ServeHTTP(rec, req)
	return rec
}

func (s *site) get(path string, cookies []*http.Cookie) *httptest.ResponseRecorder {
	s.t.Helper()
	res := s.do(request{method: http.MethodGet, path: path, cookies: cookies})
	if strings.HasPrefix(path, "/clients/new") && res.Code == http.StatusSeeOther {
		target := res.Header().Get("Location")
		u, err := url.Parse(target)
		if err != nil || u.Path != "/clients/new" || u.Host != "" || u.Scheme != "" || u.Query().Get("token") == "" {
			s.t.Fatalf("invalid canonical client URL %q", target)
		}
		raw, err := hex.DecodeString(u.Query().Get("token"))
		if err != nil || len(raw) != 24 {
			s.t.Fatalf("invalid canonical create token %q", target)
		}
		if _, err := domain.ParseDate(u.Query().Get("date")); err != nil {
			s.t.Fatalf("invalid canonical create date %q", target)
		}
		return s.do(request{method: http.MethodGet, path: target, cookies: cookies})
	}
	return res
}

func (s *site) post(path string, form url.Values, cookies []*http.Cookie) *httptest.ResponseRecorder {
	s.t.Helper()
	return s.do(request{method: http.MethodPost, path: path, form: form, cookies: cookies})
}

// login signs in and returns the session cookie.
func (s *site) login(username, password string) []*http.Cookie {
	s.t.Helper()
	res := s.post("/login", url.Values{"username": {username}, "password": {password}}, nil)
	if res.Code != http.StatusSeeOther {
		s.t.Fatalf("login %s: status %d, body %s", username, res.Code, res.Body.String())
	}
	return sessionCookies(res)
}

func sessionCookies(res *httptest.ResponseRecorder) []*http.Cookie {
	var out []*http.Cookie
	for _, c := range res.Result().Cookies() {
		if c.Name == "kassa_session" {
			out = append(out, c)
		}
	}
	return out
}

func wantRedirect(t *testing.T, what string, res *httptest.ResponseRecorder, to string) {
	t.Helper()
	if res.Code != http.StatusSeeOther || res.Header().Get("Location") != to {
		t.Fatalf("%s: status %d → %q, want 303 → %q", what, res.Code, res.Header().Get("Location"), to)
	}
}

func wantPage(t *testing.T, what string, res *httptest.ResponseRecorder, status int, texts ...string) {
	t.Helper()
	if res.Code != status {
		t.Fatalf("%s: status %d, want %d; body %.300s", what, res.Code, status, res.Body.String())
	}
	for _, text := range texts {
		if !strings.Contains(res.Body.String(), text) {
			t.Errorf("%s: page lacks %q", what, text)
		}
	}
}

func TestPagesNeedASession(t *testing.T) {
	s := newSite(t)
	for _, path := range []string{"/", "/profile", "/my", "/more", "/admin/users", "/admin/day"} {
		wantRedirect(t, path, s.get(path, nil), "/login")
	}
	res := s.do(request{method: http.MethodPost, path: "/profile/password", form: url.Values{}, datastar: true})
	wantPage(t, "Datastar post without a session", res, http.StatusOK, `redirectUrl`, `/login`)
	if ct := res.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Errorf("Content-Type = %q, want SSE", ct)
	}
}

func TestLoginWorksWithoutJavaScript(t *testing.T) {
	s := newSite(t)
	wantPage(t, "login page", s.get("/login", nil), http.StatusOK,
		`action="/login"`, `autocomplete="username"`, `autocomplete="current-password"`, `Войти`)

	res := s.post("/login", url.Values{"username": {"Olga "}, "password": {"wrong"}}, nil)
	wantPage(t, "wrong password", res, http.StatusUnprocessableEntity, "Неверное имя пользователя или пароль", `value="olga"`)
	if len(sessionCookies(res)) != 0 {
		t.Error("a failed login set a session cookie")
	}
	res = s.post("/login", url.Values{"username": {"nobody"}, "password": {"wrong"}}, nil)
	wantPage(t, "unknown user", res, http.StatusUnprocessableEntity, "Неверное имя пользователя или пароль")

	res = s.post("/login", url.Values{"username": {"OLGA"}, "password": {olgaPass}}, nil)
	wantRedirect(t, "login", res, "/")
	cookies := sessionCookies(res)
	if len(cookies) != 1 || !cookies[0].HttpOnly || !cookies[0].Secure || cookies[0].SameSite != http.SameSiteLaxMode || cookies[0].MaxAge != 30*24*3600 {
		t.Fatalf("session cookie = %+v", cookies)
	}
	start := s.get("/", cookies)
	wantPage(t, "start page", start, http.StatusOK, `<html lang="ru">`, `viewport-fit=cover`, `class="dock`, "Сегодня", "Мои записи", "Профиль", "/static/datastar.js")
	if body := start.Body.String(); strings.Contains(body, `style="`) || strings.Contains(body, `focusable="false"></svg>`) {
		t.Error("inline style (CSP) or an empty icon (run `bun run icons`) on the start page")
	}
	wantRedirect(t, "login page when signed in", s.get("/login", cookies), "/")

	res = s.post("/logout", nil, cookies)
	wantRedirect(t, "logout", res, "/login")
	if c := sessionCookies(res); len(c) != 1 || c[0].MaxAge >= 0 {
		t.Errorf("logout must delete the cookie: %+v", c)
	}
}

func TestLoginWithDatastarPatchesTheFormOrNavigates(t *testing.T) {
	s := newSite(t)
	res := s.do(request{method: http.MethodPost, path: "/login", form: url.Values{"username": {"olga"}, "password": {"wrong"}}, datastar: true})
	wantPage(t, "wrong password", res, http.StatusOK, `id="login-form"`, "Неверное имя пользователя или пароль", "datastar-patch-elements")
	res = s.do(request{method: http.MethodPost, path: "/login", form: url.Values{"username": {"olga"}, "password": {olgaPass}}, datastar: true})
	wantPage(t, "success", res, http.StatusOK, "datastar-patch-signals", `"redirectUrl":"/"`)
	if len(sessionCookies(res)) != 1 {
		t.Error("no session cookie on a Datastar login")
	}
}

func TestRepeatedWrongPasswordsLockTheAccount(t *testing.T) {
	s := newSite(t)
	for range 4 {
		s.post("/login", url.Values{"username": {"olga"}, "password": {"wrong"}}, nil)
	}
	res := s.post("/login", url.Values{"username": {"olga"}, "password": {"wrong"}}, nil)
	wantPage(t, "fifth failure", res, http.StatusUnprocessableEntity, "Неверное имя пользователя или пароль")
	res = s.post("/login", url.Values{"username": {"olga"}, "password": {olgaPass}}, nil)
	wantPage(t, "right password while locked", res, http.StatusUnprocessableEntity, "Неверное имя пользователя или пароль")
	if len(sessionCookies(res)) != 0 {
		t.Fatal("signed in while locked")
	}
	// A locked account must answer exactly like an unknown name, or the
	// lock would tell an attacker which usernames exist.
	unknown := s.post("/login", url.Values{"username": {"zaro"}, "password": {"wrong"}}, nil)
	if strings.ReplaceAll(res.Body.String(), "olga", "zaro") != unknown.Body.String() || res.Code != unknown.Code {
		t.Fatal("a locked account answers differently from an unknown username")
	}

	*s.clock = s.clock.Add(16 * time.Minute)
	s.login("olga", olgaPass)
	u, err := s.st.User(ctx, s.olga.ID)
	if err != nil || u.FailedLogins != 0 || !u.LockedUntil.IsZero() {
		t.Fatalf("after a good login: %+v, %v", u, err)
	}
}

func TestATemporaryPasswordMustBeChangedFirst(t *testing.T) {
	s := newSite(t)
	u, err := s.st.CreateUser(ctx, s.anna.Actor(), store.NewUser{Username: "vera", DisplayName: "Вера", Role: store.RoleEmployee, SubmitsReports: true, PasswordHash: hash(t, "temporary1")})
	if err != nil {
		t.Fatal(err)
	}
	res := s.post("/login", url.Values{"username": {"vera"}, "password": {"temporary1"}}, nil)
	wantRedirect(t, "first login", res, "/profile")
	first := sessionCookies(res)
	wantRedirect(t, "start page before the change", s.get("/", first), "/profile")
	wantPage(t, "profile", s.get("/profile", first), http.StatusOK, "Это временный пароль", `autocomplete="new-password"`)

	for msg, form := range map[string]url.Values{
		"Текущий пароль неверный":       {"current": {"nope"}, "new": {"vera-own-pass"}, "repeat": {"vera-own-pass"}},
		"не короче 8 символов":          {"current": {"temporary1"}, "new": {"short"}, "repeat": {"short"}},
		"Пароли не совпадают":           {"current": {"temporary1"}, "new": {"vera-own-pass"}, "repeat": {"vera-own-pas"}},
		"должен отличаться от текущего": {"current": {"temporary1"}, "new": {"temporary1"}, "repeat": {"temporary1"}},
	} {
		wantPage(t, msg, s.post("/profile/password", form, first), http.StatusUnprocessableEntity, msg)
	}
	res = s.post("/profile/password", url.Values{"current": {"temporary1"}, "new": {"vera-own-pass"}, "repeat": {"vera-own-pass"}}, first)
	wantRedirect(t, "password changed", res, "/")
	fresh := sessionCookies(res)
	wantPage(t, "start page after the change", s.get("/", fresh), http.StatusOK, "Сегодня")
	wantRedirect(t, "the old session ended", s.get("/", first), "/login")
	if u, _ = s.st.User(ctx, u.ID); u.MustChangePassword {
		t.Error("still must change the password")
	}
}

func TestAResetOrDeactivationLogsOutEverywhere(t *testing.T) {
	s := newSite(t)
	admin := s.login("anna", annaPass)
	phone, laptop := s.login("olga", olgaPass), s.login("olga", olgaPass)

	res := s.post("/admin/users/"+s.olga.ID+"/password", url.Values{"password": {"new-temp-1"}}, admin)
	wantRedirect(t, "reset", res, "/admin/users/"+s.olga.ID+"?saved=password")
	wantRedirect(t, "phone after reset", s.get("/", phone), "/login")
	wantRedirect(t, "laptop after reset", s.get("/", laptop), "/login")
	wantPage(t, "old password", s.post("/login", url.Values{"username": {"olga"}, "password": {olgaPass}}, nil), http.StatusUnprocessableEntity)
	wantRedirect(t, "temporary password", s.post("/login", url.Values{"username": {"olga"}, "password": {"new-temp-1"}}, nil), "/profile")

	maria := s.login("maria", mariaPass)
	res = s.post("/admin/users/"+s.maria.ID, url.Values{"name": {"Мария"}, "reporter": {"1"}}, admin)
	wantRedirect(t, "deactivate", res, "/admin/users/"+s.maria.ID+"?saved=account")
	wantRedirect(t, "deactivated session", s.get("/", maria), "/login")
	wantPage(t, "deactivated login", s.post("/login", url.Values{"username": {"maria"}, "password": {mariaPass}}, nil), http.StatusUnprocessableEntity, "Неверное имя пользователя или пароль")
}

func TestStaffCannotOpenAdminScreensOrOtherPeoplesData(t *testing.T) {
	s := newSite(t)
	olga := s.login("olga", olgaPass)
	for _, path := range []string{"/admin/users", "/admin/users/" + s.maria.ID, "/admin/users/new", "/admin/day", "/admin/anything", "/more"} {
		wantPage(t, path, s.get(path, olga), http.StatusForbidden, "Нет доступа")
	}
	for _, path := range []string{"/admin/users/" + s.maria.ID + "/password", "/admin/users/" + s.maria.ID + "/terms", "/admin/users/" + s.maria.ID, "/admin/users"} {
		res := s.post(path, url.Values{"password": {"hijacked-1"}, "name": {"x"}, "admin": {"1"}, "active": {"1"}}, olga)
		wantPage(t, "POST "+path, res, http.StatusForbidden)
	}
	if u, _ := s.st.User(ctx, s.maria.ID); u.MustChangePassword || u.DisplayName != "Мария" {
		t.Fatalf("staff changed someone else's account: %+v", u)
	}
	admin := s.login("anna", annaPass)
	if res := s.get("/admin/anything", admin); res.Code != http.StatusNotFound {
		t.Errorf("admin on an unknown admin page: %d, want 404", res.Code)
	}
}

func TestForgedAndExpiredSessionsAreRefused(t *testing.T) {
	s := newSite(t)
	cookies := s.login("olga", olgaPass)
	forged := *cookies[0] //nolint:gosec // a copy of the real cookie, tampered with on purpose
	forged.Value = strings.Replace(forged.Value, forged.Value[2:4], "xx", 1)
	wantRedirect(t, "forged cookie", s.get("/", []*http.Cookie{&forged}), "/login")

	*s.clock = s.clock.Add(31 * 24 * time.Hour)
	wantRedirect(t, "expired cookie", s.get("/", cookies), "/login")
}

func TestASessionIsExtendedOnceADay(t *testing.T) {
	s := newSite(t)
	cookies := s.login("olga", olgaPass)
	if res := s.get("/", cookies); len(sessionCookies(res)) != 0 {
		t.Error("renewed within the same day")
	}
	*s.clock = s.clock.Add(25 * time.Hour)
	res := s.get("/", cookies)
	if len(sessionCookies(res)) != 1 {
		t.Fatal("not renewed after a day")
	}
	*s.clock = s.clock.Add(29 * 24 * time.Hour) // 30 days after login, 29 after renewal
	wantPage(t, "renewed session", s.get("/", sessionCookies(res)), http.StatusOK)
}

func TestTheBottomBarFollowsTheRole(t *testing.T) {
	s := newSite(t)
	wantPage(t, "admin who reports", s.get("/", s.login("anna", annaPass)), http.StatusOK, "Отчёт", "День", "Месяц", "Касса", "Ещё")
	wantPage(t, "staff", s.get("/", s.login("olga", olgaPass)), http.StatusOK, "Сегодня", "Мои записи", "Профиль")
	wantPage(t, "«Ещё»", s.get("/more", s.login("anna", annaPass)), http.StatusOK, "Сотрудники", "Журнал изменений", "Выйти")
}

func TestTheAdminManagesStaffAndTheirTerms(t *testing.T) {
	s := newSite(t)
	admin := s.login("anna", annaPass)
	wantPage(t, "new staff form", s.get("/admin/users/new", admin), http.StatusOK, `name="username"`, "Временный пароль")

	res := s.post("/admin/users", url.Values{"name": {"Вера"}, "username": {"Vera"}, "password": {"temp-pass-1"}, "reporter": {"1"}}, admin)
	if res.Code != http.StatusSeeOther || !strings.HasSuffix(res.Header().Get("Location"), "?saved=created") {
		t.Fatalf("create: %d → %q", res.Code, res.Header().Get("Location"))
	}
	vera, err := s.st.UserByUsername(ctx, "vera")
	if err != nil || vera.Role != store.RoleEmployee || !vera.SubmitsReports || !vera.MustChangePassword {
		t.Fatalf("created %+v, %v", vera, err)
	}
	wantPage(t, "duplicate", s.post("/admin/users", url.Values{"name": {"Ещё Вера"}, "username": {"vera"}, "password": {"temp-pass-1"}}, admin),
		http.StatusUnprocessableEntity, "уже занято")
	wantPage(t, "bad username", s.post("/admin/users", url.Values{"name": {"X"}, "username": {"ле ра"}, "password": {"temp-pass-1"}}, admin),
		http.StatusUnprocessableEntity, "латиница")

	page := "/admin/users/" + vera.ID
	wantPage(t, "edit page", s.get(page+"?saved=created", admin), http.StatusOK, "Сотрудник добавлен", "Условия не заданы", "Ноябрь 2026", "Декабрь 2026", "Январь 2027")
	wantRedirect(t, "terms", s.post(page+"/terms", url.Values{"month": {"2026-11"}, "selfPct": {"62,5"}, "salary": {""}, "share": {"0"}, "note": {"старт"}}, admin), page+"?saved=terms")
	entries, err := s.st.Terms(ctx, vera.ID)
	if err != nil || len(entries) != 1 || entries[0].Terms != (domain.Terms{SelfPct: 6250}) || entries[0].Note != "старт" {
		t.Fatalf("terms = %+v, %v", entries, err)
	}
	wantPage(t, "terms shown", s.get(page, admin), http.StatusOK, "«Себе» 62,5%", "Сейчас", "с ноября 2026", `value="62,5"`)
	wantPage(t, "list", s.get("/admin/users", admin), http.StatusOK, "Вера", "«Себе» 62,5% · зарплата €0 · доля 0")

	wantPage(t, "percent above 100", s.post(page+"/terms", url.Values{"month": {"2026-12"}, "selfPct": {"120"}, "share": {"0"}}, admin),
		http.StatusUnprocessableEntity, "от 0 до 100")
	if _, err := s.st.SaveReport(ctx, vera.Actor(), store.ReportEntry{
		Person: vera.ID, Date: domain.Date{Year: 2026, Mon: time.November, Day: 20},
		Revenue: map[string]domain.Cents{"services": 100}, Payments: map[string]domain.Cents{"card": 100},
	}); err != nil {
		t.Fatal(err)
	}
	res = s.do(request{method: http.MethodPost, path: page + "/terms", form: url.Values{"month": {"2026-11"}, "selfPct": {"70"}, "share": {"0"}}, cookies: admin, datastar: true})
	wantPage(t, "terms over a month with reports", res, http.StatusOK, `id="terms-form"`, "уже есть отчёты")

	wantPage(t, "admin removes own rights", s.post("/admin/users/"+s.anna.ID, url.Values{"name": {"Анна"}, "active": {"1"}}, admin),
		http.StatusUnprocessableEntity, "нельзя")
	if u, _ := s.st.User(ctx, s.anna.ID); u.Role != store.RoleAdmin {
		t.Fatal("the admin demoted themselves")
	}
	wantRedirect(t, "promote", s.post(page, url.Values{"name": {"Вера"}, "admin": {"1"}, "reporter": {"1"}, "active": {"1"}}, admin), page+"?saved=account")
	if u, _ := s.st.User(ctx, vera.ID); u.Role != store.RoleAdmin || !u.Active {
		t.Fatalf("after update: %+v", u)
	}
}

func TestTheTermsFormExplainsTheProfitShare(t *testing.T) {
	s := newSite(t)
	admin := s.login("anna", annaPass)
	nov := domain.Month{Year: 2026, Mon: time.November}
	for _, p := range []struct {
		u      store.User
		weight int64
	}{{s.anna, 1}, {s.olga, 2}} {
		if err := s.st.SetTerms(ctx, s.anna.Actor(), p.u.ID, nov, domain.Terms{SelfPct: 5000, DividendWeight: p.weight}, ""); err != nil {
			t.Fatal(err)
		}
	}

	// Maria has no terms yet: new terms start next month by default, and the
	// form names everyone else's share.
	wantPage(t, "first terms", s.get("/admin/users/"+s.maria.ID, admin), http.StatusOK,
		`value="2026-12" selected`, "Доля в прибыли", "пропорционально долям", "У остальных:", ">Анна 1 · Ольга 2<", "Целое число от 0 до 1000")
	// Olga's current share 2 against Anna's 1 is two thirds of the profit.
	wantPage(t, "split", s.get("/admin/users/"+s.olga.ID, admin), http.StatusOK,
		`value="2026-12" selected`, ">Анна 1<", "Получит 66,7% прибыли месяца, остальные — 33,3%")

	page := "/admin/users/" + s.maria.ID
	for _, bad := range []string{"1,5", "-1", "1001", "один"} {
		wantPage(t, "share "+bad, s.post(page+"/terms", url.Values{"month": {"2026-12"}, "selfPct": {"70"}, "share": {bad}}, admin),
			http.StatusUnprocessableEntity, "Доля в прибыли — целое число от 0 до 1000", "input-error", `aria-invalid="true"`)
	}
	wantRedirect(t, "share with spaces", s.post(page+"/terms", url.Values{"month": {"2026-12"}, "selfPct": {"70"}, "share": {" 1 "}}, admin), page+"?saved=terms")
	entries, err := s.st.Terms(ctx, s.maria.ID)
	if err != nil || len(entries) != 1 || entries[0].Terms.DividendWeight != 1 || entries[0].From != nov.Next() {
		t.Fatalf("terms = %+v, %v", entries, err)
	}
}

func TestTermsCanRaiseSelfAboveAMonthlyThreshold(t *testing.T) {
	s := newSite(t)
	admin := s.login("anna", annaPass)
	page := "/admin/users/" + s.maria.ID
	terms := func(highFrom, highPct string) url.Values {
		return url.Values{"month": {"2026-11"}, "selfPct": {"50"}, "share": {"1"}, "highFrom": {highFrom}, "highPct": {highPct}}
	}
	for _, c := range []struct{ from, pct, want string }{
		{"5000", "", "укажите и порог, и процент"},
		{"", "70", "укажите и порог, и процент"},
		{"5000", "50", "должен быть больше обычного"},
		{"5000", "120", "от 0 до 100"},
		{"пять", "70", "Порог — сумма в евро"},
	} {
		wantPage(t, c.from+"/"+c.pct, s.post(page+"/terms", terms(c.from, c.pct), admin), http.StatusUnprocessableEntity, c.want)
	}
	wantRedirect(t, "tiered", s.post(page+"/terms", terms("5000", "70"), admin), page+"?saved=terms")
	entries, err := s.st.Terms(ctx, s.maria.ID)
	if err != nil || len(entries) != 1 || entries[0].Terms != (domain.Terms{SelfPct: 5000, DividendWeight: 1, HighSelfFromCents: 500000, HighSelfPct: 7000}) {
		t.Fatalf("terms = %+v, %v", entries, err)
	}
	wantPage(t, "shown", s.get(page, admin), http.StatusOK, "(70% при базе больше €5\u202f000)", `name="highFrom" value="5000"`, `name="highPct" value="70"`)
	wantPage(t, "list", s.get("/admin/users", admin), http.StatusOK, "«Себе» 50% (70% при базе больше €5\u202f000) · зарплата €0 · доля 1")
}

func TestEveryScreenRendersItsIconsAndNoInlineStyles(t *testing.T) {
	s := newSite(t)
	admin := s.login("anna", annaPass)
	for _, path := range []string{"/", "/profile", "/more", "/my", "/admin/day", "/admin/users", "/admin/users/new", "/admin/users/" + s.olga.ID, "/admin/users/" + s.olga.ID + "?saved=terms"} {
		body := s.get(path, admin).Body.String()
		if strings.Contains(body, `style="`) {
			t.Errorf("%s: inline style breaks the CSP", path)
		}
		if strings.Contains(body, `focusable="false"></svg>`) {
			t.Errorf("%s: an icon rendered empty; run `bun run icons`", path)
		}
	}
	login := s.get("/login", nil).Body.String()
	if strings.Contains(login, `style="`) {
		t.Error("login: inline style")
	}
}

func TestLogoutEndsTheSessionEvenForACopiedCookie(t *testing.T) {
	s := newSite(t)
	phone := s.login("olga", olgaPass)
	copied := s.login("olga", olgaPass)
	wantRedirect(t, "logout", s.post("/logout", url.Values{}, phone), "/login")
	wantRedirect(t, "the logged-out cookie", s.get("/", phone), "/login")
	wantRedirect(t, "another copy of the session", s.get("/", copied), "/login")
	s.login("olga", olgaPass)
}

func TestASessionEndsNinetyDaysAfterLoginEvenWhenUsedDaily(t *testing.T) {
	s := newSite(t)
	cookies := s.login("olga", olgaPass)
	for day := 1; day < 90; day++ {
		*s.clock = s.clock.Add(24*time.Hour + time.Minute)
		res := s.get("/", cookies)
		if res.Code != http.StatusOK {
			t.Fatalf("day %d: status %d", day, res.Code)
		}
		if renewed := sessionCookies(res); len(renewed) == 1 {
			cookies = renewed
		}
	}
	*s.clock = s.clock.Add(2 * 24 * time.Hour)
	wantRedirect(t, "91 days after login", s.get("/", cookies), "/login")
}

func TestTheAdminChangesTheirOwnPasswordOnlyInTheProfile(t *testing.T) {
	s := newSite(t)
	admin := s.login("anna", annaPass)
	page := s.get("/admin/users/"+s.anna.ID, admin)
	wantPage(t, "own staff page", page, http.StatusOK, `href="/profile"`)
	if strings.Contains(page.Body.String(), `id="reset-form"`) {
		t.Error("own staff page offers a password reset")
	}
	res := s.post("/admin/users/"+s.anna.ID+"/password", url.Values{"password": {"taken-over-1"}}, admin)
	wantPage(t, "own reset", res, http.StatusUnprocessableEntity, "Свой пароль меняйте в профиле")
	wantPage(t, "still signed in", s.get("/", admin), http.StatusOK)
	s.login("anna", annaPass)
}

func TestManyWrongLoginsFromOneSourceAreSlowedBeforeAnyPasswordCheck(t *testing.T) {
	s := newSite(t)
	attempt := func(from, username, password string) *httptest.ResponseRecorder {
		return s.do(request{method: http.MethodPost, path: "/login", from: from, form: url.Values{"username": {username}, "password": {password}}})
	}
	// Unknown names, so no account locks: only the source is counted.
	for i := range 10 {
		wantPage(t, "guess", attempt("198.51.100.7", "guess"+string(rune('a'+i)), "wrong"), http.StatusUnprocessableEntity, "Неверное имя пользователя или пароль")
	}
	res := attempt("198.51.100.7", "olga", olgaPass)
	wantPage(t, "11th attempt", res, http.StatusTooManyRequests, "Слишком много попыток входа")
	if len(sessionCookies(res)) != 0 {
		t.Fatal("signed in while throttled")
	}
	// A client cannot dodge the limit by sending its own X-Forwarded-For:
	// Cloud Run appends the real address last.
	wantPage(t, "spoofed header", attempt("203.0.113.1, 198.51.100.7", "olga", olgaPass), http.StatusTooManyRequests)
	wantRedirect(t, "another source", attempt("198.51.100.8", "olga", olgaPass), "/")

	*s.clock = s.clock.Add(16 * time.Minute)
	wantRedirect(t, "after the wait", attempt("198.51.100.7", "olga", olgaPass), "/")
}

func TestUserIndicationShowsNameAndAvatarAcrossScreens(t *testing.T) {
	s := newSite(t)
	// Login page has no avatar or user indication
	loginPage := s.get("/login", nil)
	if strings.Contains(loginPage.Body.String(), `aria-label="Профиль: Анна"`) || strings.Contains(loginPage.Body.String(), `aria-label="Профиль: Ольга"`) {
		t.Fatal("login page has user indication")
	}

	// Signed in as Anna (admin): check topbar and sidebar
	admin := s.login("anna", annaPass)
	for _, path := range []string{"/", "/admin/day", "/my"} {
		page := s.get(path, admin)
		if page.Code != http.StatusOK {
			t.Fatalf("GET %s: %d", path, page.Code)
		}
		body := page.Body.String()
		if !strings.Contains(body, `aria-label="Профиль: Анна"`) {
			t.Fatalf("GET %s missing profile link for Anna: %s", path, body)
		}
		if !strings.Contains(body, "Анна") {
			t.Fatalf("GET %s missing Anna's name: %s", path, body)
		}
		if !strings.Contains(body, "<span>А</span>") {
			t.Fatalf("GET %s missing Anna's avatar initial: %s", path, body)
		}
	}

	// Signed in as Olga (staff): check topbar
	staff := s.login("olga", olgaPass)
	page := s.get("/", staff)
	if page.Code != http.StatusOK {
		t.Fatalf("GET /: %d", page.Code)
	}
	body := page.Body.String()
	if !strings.Contains(body, `aria-label="Профиль: Ольга"`) {
		t.Fatalf("GET / missing profile link for Olga: %s", body)
	}
	if !strings.Contains(body, "Ольга") {
		t.Fatalf("GET / missing Olga's name: %s", body)
	}
	if !strings.Contains(body, "<span>О</span>") {
		t.Fatalf("GET / missing Olga's avatar initial: %s", body)
	}
}

// Drafts on the phone hold names and money: the login page right after a
// logout tells draft.js to forget them, once; an ordinary visit does not.
func TestLogoutClearsDraftsOnThePhone(t *testing.T) {
	s := newSite(t)
	if body := s.get("/login", nil).Body.String(); strings.Contains(body, "data-drafts-clear") {
		t.Fatal("plain login page clears drafts")
	}
	res := s.post("/logout", url.Values{}, s.login("olga", olgaPass))
	wantRedirect(t, "logout", res, "/login")
	var marker []*http.Cookie
	for _, c := range res.Result().Cookies() {
		if c.Name == "kassa_signed_out" {
			marker = append(marker, c)
		}
	}
	if len(marker) != 1 || !marker[0].HttpOnly || !marker[0].Secure || marker[0].MaxAge <= 0 {
		t.Fatalf("logout marker cookie: %+v", marker)
	}
	page := s.get("/login", marker)
	wantPage(t, "login after logout", page, http.StatusOK, `<script type="module" src="/static/draft.js" data-drafts-clear></script>`)
	for _, c := range page.Result().Cookies() {
		if c.Name == "kassa_signed_out" && c.MaxAge >= 0 {
			t.Errorf("login page must delete the marker: %+v", c)
		}
	}
}
