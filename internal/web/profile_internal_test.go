package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"github.com/lieranderl/aestheticlab-income-app/internal/store"
)

// A password change that started before an admin reset must not undo the
// reset: the handler holds the person as loaded with the request, and the
// reset lands before the change is written.
func TestAPasswordChangeCannotUndoAResetThatLandedMeanwhile(t *testing.T) {
	ctx := context.Background()
	hash := func(p string) string {
		h, err := bcrypt.GenerateFromPassword([]byte(p), bcrypt.MinCost)
		if err != nil {
			t.Fatal(err)
		}
		return string(h)
	}
	st := store.NewMemory()
	anna, err := st.SetUp(ctx, store.DefaultSetup(store.NewUser{Username: "anna", DisplayName: "Анна", Role: store.RoleAdmin, SubmitsReports: true, PasswordHash: hash("anna-password")}))
	if err != nil {
		t.Fatal(err)
	}
	olga, err := st.CreateUser(ctx, anna.Actor(), store.NewUser{Username: "olga", DisplayName: "Ольга", Role: store.RoleEmployee, SubmitsReports: true, PasswordHash: hash("olga-password")})
	if err != nil {
		t.Fatal(err)
	}
	a, err := newApp(Config{BaseURL: "https://kassa.aestheticlab.be", Store: st, PasswordCost: bcrypt.MinCost})
	if err != nil {
		t.Fatal(err)
	}
	loaded := olga // what signedIn read before the reset

	reset := hash("reset-temp-1")
	if _, err := st.UpdateUser(ctx, anna.Actor(), olga.ID, func(x *store.User) error {
		x.PasswordHash, x.MustChangePassword = reset, true
		x.SessionVersion++
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	form := url.Values{"current": {"olga-password"}, "new": {"attacker-pass"}, "repeat": {"attacker-pass"}}
	req := httptest.NewRequest(http.MethodPost, "/profile/password", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	a.handleChangePassword(rec, req, loaded)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status %d, want 422", rec.Code)
	}
	u, err := st.User(ctx, olga.ID)
	if err != nil {
		t.Fatal(err)
	}
	if u.PasswordHash != reset || !u.MustChangePassword {
		t.Fatal("the password change overwrote the admin's reset")
	}
}
