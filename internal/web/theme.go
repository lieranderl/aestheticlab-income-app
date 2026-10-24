package web

import (
	"net/http"

	"github.com/lieranderl/aestheticlab-income-app/internal/store"
	"github.com/lieranderl/aestheticlab-income-app/internal/views"
)

// themeCookie keeps this phone's light/dark choice. It is a display
// preference, not a secret, and stays a year.
const themeCookie = "kassa_theme"

const themeMaxAge = 365 * 24 * 60 * 60

// withTheme hands the phone's chosen theme to the page layout. Public pages
// (the cached offline page) always follow the phone's setting.
func withTheme(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie(themeCookie); err == nil && !publicPath(r.URL.Path) {
			if t, ok := views.ParseTheme(c.Value); ok && t != views.ThemeSystem {
				r = r.WithContext(views.WithTheme(r.Context(), t))
			}
		}
		next.ServeHTTP(w, r)
	})
}

// handleSetTheme saves the light/dark/system choice for this phone;
// «Как в системе» forgets the cookie.
func (a *app) handleSetTheme(w http.ResponseWriter, r *http.Request, _ store.User) {
	if err := readForm(w, r); err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	t, ok := views.ParseTheme(r.PostFormValue("theme"))
	if !ok {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	age := themeMaxAge
	if t == views.ThemeSystem {
		age = -1
	}
	http.SetCookie(w, &http.Cookie{ // #nosec G124 -- Secure is set whenever the app is served over HTTPS (always in production)
		Name: themeCookie, Value: string(t), Path: "/", MaxAge: age,
		HttpOnly: true, Secure: a.secure(), SameSite: http.SameSiteLaxMode,
	})
	a.navigate(w, r, "/profile")
}
