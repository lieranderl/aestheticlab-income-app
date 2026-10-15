package web

import (
	"log/slog"
	"net/http"
	"time"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"

	"github.com/lieranderl/aestheticlab-income-app/internal/store"
	"github.com/lieranderl/aestheticlab-income-app/internal/views"
)

const msgReauthLocked = "Слишком много неверных попыток ввода пароля. Подождите 15 минут или попросите администратора сбросить пароль."

func roleLabel(r store.Role) string {
	if r == store.RoleAdmin {
		return "Администратор"
	}
	return "Сотрудник"
}

func (a *app) profile(u store.User, f views.PasswordForm) views.Profile {
	return views.Profile{Name: u.DisplayName, Username: u.Username, Role: roleLabel(u.Role), Forced: u.MustChangePassword, Password: f}
}

func (a *app) handleProfile(w http.ResponseWriter, r *http.Request, u store.User) {
	var f views.PasswordForm
	if r.URL.Query().Get("saved") == "password" {
		f.Done = "Пароль изменён. На других устройствах нужно войти заново."
	}
	a.render(w, r, views.ProfilePage(a.navFor(u, "/profile"), a.profile(u, f)))
}

// handleChangePassword sets the person's own password. Every other device
// is logged out; this one gets a fresh session.
func (a *app) handleChangePassword(w http.ResponseWriter, r *http.Request, u store.User) {
	if err := readForm(w, r); err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	current, next, repeat := r.PostFormValue("current"), r.PostFormValue("new"), r.PostFormValue("repeat")
	show := func(status int, msg string) {
		f := views.PasswordForm{Error: msg}
		a.formFailedStatus(w, r, status, views.PasswordFormView(f), views.ProfilePage(a.navFor(u, "/profile"), a.profile(u, f)))
	}
	fail := func(msg string) { show(http.StatusUnprocessableEntity, msg) }
	// The current password is checked like a login: wrong guesses count
	// towards the same account lock, and the compare waits for a free slot,
	// so a stolen session cannot guess it or flood the CPU.
	if a.now().Before(u.LockedUntil) {
		show(http.StatusTooManyRequests, msgReauthLocked)
		return
	}
	var match bool
	if !a.throttle.hash(r, func() { match = bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(current)) == nil }) {
		show(http.StatusServiceUnavailable, msgLoginRetry)
		return
	}
	if !match {
		after, err := a.store.RecordLoginFailure(r.Context(), u.ID, loginLimit, loginLockFor)
		switch {
		case err != nil:
			slog.ErrorContext(r.Context(), "record reauth failure", "error", err)
		case a.now().Before(after.LockedUntil):
			slog.WarnContext(r.Context(), "account locked after wrong current passwords", "uid", u.ID)
		}
		fail("Текущий пароль неверный")
		return
	}
	switch {
	case utf8.RuneCountInString(next) < minPassword:
		fail("Новый пароль должен быть не короче 8 символов")
		return
	case next != repeat:
		fail("Пароли не совпадают")
		return
	case next == current:
		fail("Новый пароль должен отличаться от текущего")
		return
	}
	hash, err := a.hashPassword(next)
	if err != nil {
		fail(problemText(r, err))
		return
	}
	forced := u.MustChangePassword
	updated, err := a.store.UpdateUser(r.Context(), u.Actor(), u.ID, func(x *store.User) error {
		// The current password was checked against u as this request loaded
		// it; a reset or deactivation since then wins.
		if !x.Active || x.SessionVersion != u.SessionVersion || x.PasswordHash != u.PasswordHash {
			return store.ErrVersionConflict
		}
		x.PasswordHash, x.MustChangePassword = hash, false
		x.FailedLogins, x.LockedUntil = 0, time.Time{}
		x.SessionVersion++
		return nil
	})
	if err != nil {
		fail(problemText(r, err))
		return
	}
	if err := a.startSession(w, updated); err != nil {
		slog.ErrorContext(r.Context(), "start session", "error", err)
	}
	if forced {
		a.navigate(w, r, "/")
		return
	}
	a.navigate(w, r, "/profile?saved=password")
}
