package web

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/lieranderl/aestheticlab-income-app/internal/store"
	"github.com/lieranderl/aestheticlab-income-app/internal/views"
)

const (
	sessionCookie = "kassa_session"
	sessionTTL    = 30 * 24 * time.Hour
	// A session is extended once a day at most, not on every request.
	sessionRenewAfter = 24 * time.Hour
	// However often it is used, a session ends this long after the login.
	sessionMaxAge = 90 * 24 * time.Hour
)

// signer signs cookie values with HMAC-SHA256, so nobody can forge or
// change them (from datastar-webapp internal/web/auth.go).
type signer struct{ key []byte }

// seal is value as a cookie carries it: its JSON and the JSON's signature.
func (s signer) seal(value any) (string, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	enc := base64.RawURLEncoding
	return enc.EncodeToString(payload) + "." + enc.EncodeToString(s.mac(payload)), nil
}

// open reads a cookie value sealed by seal into value.
func (s signer) open(sealed string, value any) error {
	enc := base64.RawURLEncoding
	p, m, ok := strings.Cut(sealed, ".")
	if !ok {
		return errors.New("not a sealed value")
	}
	payload, err := enc.DecodeString(p)
	if err != nil {
		return err
	}
	mac, err := enc.DecodeString(m)
	if err != nil {
		return err
	}
	if !hmac.Equal(mac, s.mac(payload)) {
		return errors.New("signature does not match")
	}
	return json.Unmarshal(payload, value)
}

func (s signer) mac(payload []byte) []byte {
	h := hmac.New(sha256.New, s.key)
	h.Write(payload)
	return h.Sum(nil)
}

// session is what the cookie carries: who, which session version (a
// password reset, deactivation or logout bumps it, logging out every
// device), when they logged in and until when.
type session struct {
	UID     string `json:"uid"`
	Version int64  `json:"sv"`
	Auth    int64  `json:"auth"`
	Expires int64  `json:"exp"`
}

// startSession signs the person in on this device.
func (a *app) startSession(w http.ResponseWriter, u store.User) error {
	return a.writeSession(w, u, a.now())
}

// writeSession sets a session for a login at auth: sessionTTL from now, but
// never past sessionMaxAge after the login.
func (a *app) writeSession(w http.ResponseWriter, u store.User, auth time.Time) error {
	expires := min(a.now().Add(sessionTTL).Unix(), auth.Add(sessionMaxAge).Unix())
	sealed, err := a.signer.seal(session{UID: u.ID, Version: u.SessionVersion, Auth: auth.Unix(), Expires: expires})
	if err != nil {
		return err
	}
	http.SetCookie(w, a.cookie(sealed, time.Unix(expires, 0).Sub(a.now())))
	return nil
}

// endSession forgets the session on this device.
func (a *app) endSession(w http.ResponseWriter) { http.SetCookie(w, a.cookie("", -1)) }

// cookie is the HttpOnly, SameSite=Lax session cookie, Secure over HTTPS;
// a negative maxAge deletes it.
func (a *app) cookie(value string, maxAge time.Duration) *http.Cookie {
	age := int(maxAge / time.Second)
	if maxAge < 0 {
		age = -1
	}
	return &http.Cookie{ // #nosec G124 -- Secure is set whenever the app is served over HTTPS (always in production)
		Name: sessionCookie, Value: value, Path: "/", MaxAge: age,
		HttpOnly: true, Secure: a.secure(), SameSite: http.SameSiteLaxMode,
	}
}

func (a *app) secure() bool { return strings.HasPrefix(a.cfg.BaseURL, "https://") }

// errNoSession means the visitor must sign in (again).
var errNoSession = errors.New("no valid session")

// currentUser is the signed-in person. errNoSession covers a missing,
// forged or expired cookie, and a person who was deactivated or whose
// sessions were ended; other errors are the store's.
func (a *app) currentUser(r *http.Request) (store.User, session, error) {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return store.User{}, session{}, errNoSession
	}
	var s session
	if err := a.signer.open(c.Value, &s); err != nil || a.now().Unix() >= s.Expires ||
		a.now().Unix() >= time.Unix(s.Auth, 0).Add(sessionMaxAge).Unix() {
		return store.User{}, session{}, errNoSession
	}
	u, err := a.store.User(r.Context(), s.UID)
	if errors.Is(err, store.ErrNotFound) {
		return store.User{}, session{}, errNoSession
	}
	if err != nil {
		return store.User{}, session{}, err
	}
	if !u.Active || u.SessionVersion != s.Version {
		return store.User{}, session{}, errNoSession
	}
	return u, s, nil
}

// userHandler is a handler for signed-in people.
type userHandler func(http.ResponseWriter, *http.Request, store.User)

// signedIn lets only signed-in people through, sends a person with a
// temporary password to /profile first, and extends the session.
func (a *app) signedIn(h userHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, s, err := a.currentUser(r)
		switch {
		case errors.Is(err, errNoSession):
			a.endSession(w)
			a.navigate(w, r, "/login")
			return
		case err != nil:
			slog.ErrorContext(r.Context(), "load session user", "error", err)
			http.Error(w, "Сервис временно недоступен, попробуйте ещё раз", http.StatusServiceUnavailable)
			return
		}
		if u.MustChangePassword && !strings.HasPrefix(r.URL.Path, "/profile") {
			a.navigate(w, r, "/profile")
			return
		}
		auth := time.Unix(s.Auth, 0)
		if time.Unix(s.Expires, 0).Sub(a.now()) < sessionTTL-sessionRenewAfter && s.Expires < auth.Add(sessionMaxAge).Unix() {
			if err := a.writeSession(w, u, auth); err != nil {
				slog.ErrorContext(r.Context(), "renew session", "error", err)
			}
		}
		h(w, r, u)
	}
}

// adminOnly lets only the admin through; others get «Нет доступа» (403).
func (a *app) adminOnly(h userHandler) http.HandlerFunc {
	return a.signedIn(func(w http.ResponseWriter, r *http.Request, u store.User) {
		if u.Role != store.RoleAdmin {
			a.renderStatus(w, r, http.StatusForbidden, views.ForbiddenPage(a.navFor(u, "")))
			return
		}
		h(w, r, u)
	})
}
