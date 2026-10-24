// Package web is the Kassa web app's HTTP layer: routes, middleware and
// handlers. Money rules live in internal/domain, persistence in internal/store.
package web

import (
	"crypto/rand"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/lieranderl/aestheticlab-income-app/internal/store"
	"github.com/lieranderl/aestheticlab-income-app/internal/views"
	"github.com/lieranderl/aestheticlab-income-app/static"
)

// MinSessionKey is the shortest session signing key accepted (bytes).
const MinSessionKey = 32

// Config holds what the web app needs to serve requests.
type Config struct {
	// BaseURL is where people reach the app (https://kassa.example.com,
	// or http://localhost:8080 locally). An https URL turns on HSTS and
	// Secure cookies.
	BaseURL string
	// Ready reports whether the app's dependencies (Firestore) answer; nil
	// is always ready.
	Ready func(*http.Request) error
	// Store holds the data; nil means a fresh in-memory store.
	Store *store.Store
	// SessionKey signs session cookies (INCOME_SESSION_KEY, ≥ 32 bytes);
	// nil means a random key, so sessions end when the process does.
	SessionKey []byte
	// Now is the clock for sessions and lockouts; nil means time.Now.
	Now func() time.Time
	// PasswordCost is the bcrypt cost; 0 means 12. Tests use bcrypt.MinCost.
	PasswordCost int
}

type app struct {
	cfg       Config
	store     *store.Store
	signer    signer
	now       func() time.Time
	dummyHash []byte
	throttle  *loginThrottle
}

// newApp fills in the defaults of cfg.
func newApp(cfg Config) (*app, error) {
	if cfg.Store == nil {
		cfg.Store = store.NewMemory()
	}
	if cfg.SessionKey == nil {
		cfg.SessionKey = []byte(rand.Text() + rand.Text())
	}
	if len(cfg.SessionKey) < MinSessionKey {
		return nil, fmt.Errorf("session key has %d bytes, want at least %d", len(cfg.SessionKey), MinSessionKey)
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.PasswordCost == 0 {
		cfg.PasswordCost = 12
	}
	// Unknown usernames are compared against this, so a login takes as
	// long whether or not the name exists.
	dummy, err := bcrypt.GenerateFromPassword([]byte(rand.Text()), cfg.PasswordCost)
	if err != nil {
		return nil, err
	}
	return &app{cfg: cfg, store: cfg.Store, signer: signer{key: cfg.SessionKey}, now: cfg.Now, dummyHash: dummy, throttle: newLoginThrottle(cfg.Now)}, nil
}

// New returns the app's HTTP handler: panic recovery, security headers,
// request logs, no caching of private responses and cross-origin (CSRF) protection around the routes.
func New(cfg Config) (http.Handler, error) {
	a, err := newApp(cfg)
	if err != nil {
		return nil, err
	}
	cfg = a.cfg

	assets, err := staticHandler(static.Files)
	if err != nil {
		return nil, err
	}

	mux := http.NewServeMux()
	// /readyz, not /healthz: Cloud Run answers paths like /healthz itself.
	mux.HandleFunc("GET /readyz", a.handleReady)
	mux.Handle("GET /static/", assets)
	mux.HandleFunc("GET /manifest.webmanifest", handleManifest)
	mux.HandleFunc("GET /sw.js", handleServiceWorker)
	mux.HandleFunc("GET /offline", a.handleOffline)

	mux.HandleFunc("GET /login", a.handleLoginPage)
	mux.HandleFunc("POST /login", a.handleLogin)
	mux.HandleFunc("POST /logout", a.handleLogout)

	mux.HandleFunc("GET /{$}", a.signedIn(a.handleHome))
	mux.HandleFunc("GET /clients/new", a.signedIn(a.handleClient))
	mux.HandleFunc("GET /clients/{id}", a.signedIn(a.handleClient))
	mux.HandleFunc("POST /clients", a.signedIn(a.handleSaveClient))
	mux.HandleFunc("POST /clients/{id}", a.signedIn(a.handleSaveClient))
	mux.HandleFunc("GET /clients/{id}/remove", a.signedIn(a.handleRemoveClientPage))
	mux.HandleFunc("POST /clients/{id}/remove", a.signedIn(a.handleRemoveClient))
	mux.HandleFunc("GET /reports/day", a.signedIn(a.handleDaily))
	mux.HandleFunc("POST /reports/{date}", a.signedIn(a.handleSaveReport))
	mux.HandleFunc("GET /profile", a.signedIn(a.handleProfile))
	mux.HandleFunc("POST /profile/password", a.signedIn(a.handleChangePassword))
	mux.HandleFunc("POST /profile/theme", a.signedIn(a.handleSetTheme))
	mux.HandleFunc("GET /my", a.signedIn(a.handleMy))
	mux.HandleFunc("GET /admin/kassa", a.adminOnly(a.handleKassa))
	mux.HandleFunc("GET /admin/movements/new", a.adminOnly(a.handleMovementPage))
	mux.HandleFunc("POST /admin/movements", a.adminOnly(a.handleRecordMovement))
	mux.HandleFunc("GET /movements/new", a.signedIn(a.handleMovementPage))
	mux.HandleFunc("POST /movements", a.signedIn(a.handleRecordMovement))
	mux.HandleFunc("POST /admin/movements/{id}/confirm", a.adminOnly(a.handleConfirmMovement))
	mux.HandleFunc("GET /movements/{id}/void", a.signedIn(a.handleVoidPage))
	mux.HandleFunc("POST /movements/{id}/void", a.signedIn(a.handleVoidMovement))
	mux.HandleFunc("GET /admin/accounts/{id}/recount", a.adminOnly(a.handleRecountPage))
	mux.HandleFunc("POST /admin/accounts/{id}/recount", a.adminOnly(a.handleRecount))

	mux.HandleFunc("GET /more", a.adminOnly(a.handleMore))
	mux.HandleFunc("GET /admin/day", a.adminOnly(a.handleAdminDay))
	mux.HandleFunc("GET /admin/month/{month}/close", a.adminOnly(a.handleMonthAction))
	mux.HandleFunc("POST /admin/month/{month}/close", a.adminOnly(a.handleMonthAction))
	mux.HandleFunc("GET /admin/month/{month}/reopen", a.adminOnly(a.handleMonthAction))
	mux.HandleFunc("POST /admin/month/{month}/reopen", a.adminOnly(a.handleMonthAction))
	mux.HandleFunc("GET /admin/month", a.adminOnly(a.handleAdminMonth))
	mux.HandleFunc("GET /admin/history", a.adminOnly(a.handleHistory))
	mux.HandleFunc("GET /admin/history/movements", a.adminOnly(a.handleMovementHistory))
	mux.HandleFunc("GET /admin/audit", a.adminOnly(a.handleAudit))
	mux.HandleFunc("GET /admin/reports/{uid}/{date}", a.adminOnly(a.handleAdminReport))
	mux.HandleFunc("POST /admin/reports/{uid}/{date}", a.adminOnly(a.handleAdminSaveReport))
	mux.HandleFunc("GET /admin/users", a.adminOnly(a.handleStaff))
	mux.HandleFunc("GET /admin/users/new", a.adminOnly(a.handleNewStaffPage))
	mux.HandleFunc("POST /admin/users", a.adminOnly(a.handleCreateStaff))
	mux.HandleFunc("GET /admin/users/{uid}", a.adminOnly(a.handleStaffEdit))
	mux.HandleFunc("POST /admin/users/{uid}", a.adminOnly(a.handleUpdateStaff))
	mux.HandleFunc("POST /admin/users/{uid}/password", a.adminOnly(a.handleResetPassword))
	mux.HandleFunc("POST /admin/users/{uid}/terms", a.adminOnly(a.handleSetTerms))
	mux.HandleFunc("GET /admin/categories", a.adminOnly(a.handleCategories))
	mux.HandleFunc("GET /admin/categories/new", a.adminOnly(a.handleNewCategory))
	mux.HandleFunc("GET /admin/categories/{id}", a.adminOnly(a.handleCategory))
	mux.HandleFunc("POST /admin/categories", a.adminOnly(a.handleSaveCategory))
	mux.HandleFunc("POST /admin/categories/{id}", a.adminOnly(a.handleSaveCategory))
	mux.HandleFunc("POST /admin/categories/{id}/{action}", a.adminOnly(a.handleCategoryAction))
	mux.HandleFunc("GET /admin/trends", a.adminOnly(a.handleTrends))
	mux.HandleFunc("GET /admin/export", a.adminOnly(a.handleExport))
	mux.HandleFunc("GET /admin/export/download", a.adminOnly(a.handleExportDownload))
	// Anything else under /admin/ is the admin's too: 403 before 404.
	mux.HandleFunc("/admin/", a.adminOnly(func(w http.ResponseWriter, r *http.Request, _ store.User) { http.NotFound(w, r) }))

	secure := strings.HasPrefix(cfg.BaseURL, "https://")
	protected := http.NewCrossOriginProtection().Handler(withTheme(mux))
	return recovery(securityHeaders(secure, logging(noStore(protected)))), nil
}

// handleManifest serves the web app manifest, so phones install the app
// to the Home Screen (system-design §7). It needs no session.
func handleManifest(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/manifest+json")
	http.ServeFileFS(w, r, static.Files, "manifest.webmanifest")
}

// handleServiceWorker serves the service worker script from root, so its
// scope covers the whole app. It needs no session.
func handleServiceWorker(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Header().Set("Service-Worker-Allowed", "/")
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeFileFS(w, r, static.Files, "sw.js")
}

// handleOffline serves the offline fallback page (system-design §7).
func (a *app) handleOffline(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	a.render(w, r, views.OfflinePage())
}

func (a *app) handleReady(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if a.cfg.Ready != nil {
		if err := a.cfg.Ready(r); err != nil {
			slog.ErrorContext(r.Context(), "readiness check failed", "error", err)
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = io.WriteString(w, "not ready\n")
			return
		}
	}
	_, _ = io.WriteString(w, "ready\n")
}

// serverError logs an unexpected error and shows a short Russian message.
func (a *app) serverError(w http.ResponseWriter, r *http.Request, err error) {
	slog.ErrorContext(r.Context(), "request failed", "path", logValue(r.URL.Path), "error", err)
	http.Error(w, "Что-то пошло не так, попробуйте ещё раз", http.StatusInternalServerError)
}
