package web

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/lieranderl/aestheticlab-income-app/internal/store"
	"github.com/lieranderl/aestheticlab-income-app/internal/views"
)

// Login throttling (technical-solution §6): after loginLimit wrong
// passwords in a row the account locks for loginLockFor.
const (
	loginLimit   = 5
	loginLockFor = 15 * time.Minute
	minPassword  = 8
)

// One answer for an unknown name, a wrong password and a locked account,
// so the login never reveals which usernames exist; it states the lockout
// rule up front instead.
const (
	msgWrongLogin = "Неверное имя пользователя или пароль. После 5 неудачных попыток вход блокируется на 15 минут — тогда подождите или попросите администратора сбросить пароль."
	msgLoginRetry = "Не удалось войти, попробуйте ещё раз"
	msgThrottled  = "Слишком много попыток входа с этого устройства. Подождите 15 минут и попробуйте снова."
)

func (a *app) handleLoginPage(w http.ResponseWriter, r *http.Request) {
	if _, _, err := a.currentUser(r); err == nil {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	a.render(w, r, views.LoginPage(views.LoginForm{}))
}

// handleLogin checks a username and password. Unknown names still run a
// bcrypt compare, so the answer takes as long whether the name exists. A
// source with too many failed logins is refused before any of that.
func (a *app) handleLogin(w http.ResponseWriter, r *http.Request) {
	if err := readForm(w, r); err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	username := strings.ToLower(strings.TrimSpace(r.PostFormValue("username")))
	password := r.PostFormValue("password")
	source := loginSource(r)
	show := func(status int, msg string) {
		f := views.LoginForm{Username: username, Error: msg}
		a.formFailedStatus(w, r, status, views.LoginFormView(f), views.LoginPage(f))
	}
	fail := func(msg string) { show(http.StatusUnprocessableEntity, msg) }
	wrong := func() { fail(msgWrongLogin) }
	if !a.throttle.admit(source) {
		slog.WarnContext(r.Context(), "login refused: too many failures from source", "source", source)
		show(http.StatusTooManyRequests, msgThrottled)
		return
	}
	// matches runs the bcrypt compare in a free slot; ok is false when the
	// app is too busy to check.
	matches := func(hash []byte) (match, ok bool) {
		ok = a.throttle.hash(r, func() { match = bcrypt.CompareHashAndPassword(hash, []byte(password)) == nil })
		return match, ok
	}
	busy := func() {
		a.throttle.release(source)
		slog.WarnContext(r.Context(), "login refused: password checks busy")
		show(http.StatusServiceUnavailable, msgLoginRetry)
	}

	u, err := a.store.UserByUsername(r.Context(), username)
	if errors.Is(err, store.ErrNotFound) {
		if _, ok := matches(a.dummyHash); !ok {
			busy()
			return
		}
		wrong()
		return
	}
	if err != nil {
		slog.ErrorContext(r.Context(), "login lookup", "error", err)
		a.throttle.release(source)
		fail(msgLoginRetry)
		return
	}
	if a.now().Before(u.LockedUntil) {
		if _, ok := matches(a.dummyHash); !ok {
			busy()
			return
		}
		slog.InfoContext(r.Context(), "login refused: account locked", "uid", u.ID)
		wrong()
		return
	}
	match, ok := matches([]byte(u.PasswordHash))
	if !ok {
		busy()
		return
	}
	if !match {
		after, err := a.store.RecordLoginFailure(r.Context(), u.ID, loginLimit, loginLockFor)
		switch {
		case err != nil:
			slog.ErrorContext(r.Context(), "record login failure", "error", err)
		case a.now().Before(after.LockedUntil):
			slog.WarnContext(r.Context(), "account locked after failed logins", "uid", u.ID, "source", source)
		}
		wrong()
		return
	}
	if !u.Active {
		wrong()
		return
	}
	a.throttle.succeeded(source)
	if _, err := a.store.RecordLoginSuccess(r.Context(), u.ID); err != nil {
		slog.ErrorContext(r.Context(), "record login success", "error", err)
	}
	if err := a.startSession(w, u); err != nil {
		slog.ErrorContext(r.Context(), "start session", "error", err)
		fail(msgLoginRetry)
		return
	}
	next := "/"
	if u.MustChangePassword {
		next = "/profile"
	}
	a.navigate(w, r, next)
}

// handleLogout ends the person's sessions on every device, so a copy of
// this cookie stops working too.
func (a *app) handleLogout(w http.ResponseWriter, r *http.Request) {
	if u, _, err := a.currentUser(r); err == nil {
		if _, err := a.store.UpdateUser(r.Context(), u.Actor(), u.ID, func(x *store.User) error {
			x.SessionVersion++
			return nil
		}); err != nil {
			slog.ErrorContext(r.Context(), "end sessions on logout", "error", err)
		}
	}
	a.endSession(w)
	a.navigate(w, r, "/login")
}

func (a *app) hashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), a.cfg.PasswordCost)
	return string(hash), err
}
