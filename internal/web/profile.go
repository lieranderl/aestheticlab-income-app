package web

import (
	"log/slog"
	"net/http"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"

	"github.com/lieranderl/aestheticlab-income-app/internal/store"
	"github.com/lieranderl/aestheticlab-income-app/internal/views"
)

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
	fail := func(msg string) {
		f := views.PasswordForm{Error: msg}
		a.formFailed(w, r, views.PasswordFormView(f), views.ProfilePage(a.navFor(u, "/profile"), a.profile(u, f)))
	}
	switch {
	case bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(current)) != nil:
		fail("Текущий пароль неверный")
		return
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
