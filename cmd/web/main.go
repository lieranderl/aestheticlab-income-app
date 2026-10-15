// Command web is the Aesthetic Lab Kassa web app, run on Cloud Run
// (Dockerfile) and locally with `make web` / `make web-dev`.
package main

import (
	"cmp"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/lieranderl/aestheticlab-income-app/internal/domain"
	"github.com/lieranderl/aestheticlab-income-app/internal/store"
	"github.com/lieranderl/aestheticlab-income-app/internal/web"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	if err := checkCloudRun(os.Getenv); err != nil {
		slog.Error("refusing to start on Cloud Run", "error", err)
		os.Exit(1)
	}
	st, err := openStore()
	if err != nil {
		slog.Error("store failed", "error", err)
		os.Exit(1)
	}

	// The session key comes from Secret Manager in production. Local runs
	// (memory store or the Firestore emulator) may go without one: they get
	// a random key, so sessions end when the process restarts.
	var sessionKey []byte
	switch key := os.Getenv("INCOME_SESSION_KEY"); {
	case key != "":
		sessionKey = []byte(key)
	case os.Getenv("INCOME_FIRESTORE_PROJECT") == "":
	case os.Getenv("FIRESTORE_EMULATOR_HOST") != "":
		slog.Warn("INCOME_SESSION_KEY unset: random key for the emulator; sign in again after each restart")
	default:
		slog.Error("INCOME_SESSION_KEY is required with Firestore")
		os.Exit(1)
	}
	cfg := web.Config{
		BaseURL: strings.TrimRight(cmp.Or(os.Getenv("INCOME_BASE_URL"), "http://localhost:8080"), "/"),
		Ready: func(r *http.Request) error {
			ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
			defer cancel()
			return st.Ping(ctx)
		},
		Store:      st,
		SessionKey: sessionKey,
	}
	handler, err := web.New(cfg)
	if err != nil {
		slog.Error("web config", "error", err)
		os.Exit(1)
	}

	srv := &http.Server{
		Addr:              ":" + cmp.Or(os.Getenv("PORT"), "8080"),
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	// Cloud Run sends SIGTERM and allows 10 seconds before it stops the instance.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	done := make(chan struct{})
	go func() {
		defer close(done)
		<-ctx.Done()
		slog.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			slog.Error("shutdown failed", "error", err)
		}
	}()

	slog.Info("kassa web listening", "addr", srv.Addr, "baseURL", cfg.BaseURL)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("listen failed", "error", err)
		os.Exit(1)
	}
	<-done
	if err := st.Close(); err != nil {
		slog.Error("store close failed", "error", err)
	}
	slog.Info("stopped cleanly")
}

// checkCloudRun refuses a Cloud Run start (K_SERVICE set) without the
// production settings, so a missing variable never falls back to the memory
// store (and its logged password), a random session key or plain HTTP
// (no Secure cookies, no HSTS).
func checkCloudRun(getenv func(string) string) error {
	if getenv("K_SERVICE") == "" {
		return nil
	}
	var missing []string
	for _, name := range []string{"INCOME_FIRESTORE_PROJECT", "INCOME_FIRESTORE_DATABASE", "INCOME_SESSION_KEY"} {
		if getenv(name) == "" {
			missing = append(missing, name)
		}
	}
	if !strings.HasPrefix(getenv("INCOME_BASE_URL"), "https://") {
		missing = append(missing, "INCOME_BASE_URL (https://…)")
	}
	if getenv("FIRESTORE_EMULATOR_HOST") != "" {
		return errors.New("FIRESTORE_EMULATOR_HOST is set")
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing %s", strings.Join(missing, ", "))
	}
	return nil
}

// openStore is Firestore when INCOME_FIRESTORE_PROJECT is set, otherwise
// memory (local runs; data is lost on restart).
func openStore() (*store.Store, error) {
	opts, err := storeOptions()
	if err != nil {
		return nil, err
	}
	project := os.Getenv("INCOME_FIRESTORE_PROJECT")
	if project == "" {
		slog.Warn("INCOME_FIRESTORE_PROJECT unset: in-memory store, data is lost on restart")
		st := store.NewMemory(opts...)
		return st, setUpLocal(st)
	}
	database := cmp.Or(os.Getenv("INCOME_FIRESTORE_DATABASE"), "aestheticlab-income")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return store.NewFirestore(ctx, project, database, opts...)
}

// storeOptions reads the salon timezone (INCOME_TZ), the first month the
// app keeps (INCOME_START_MONTH: 2026-11 for real use, 2026-10 during October production testing),
// and whether financial writes are paused (INCOME_PAUSE_FINANCIAL_WRITES / INCOME_PAUSE_WRITES).
func storeOptions() ([]store.Option, error) {
	loc, err := time.LoadLocation(cmp.Or(os.Getenv("INCOME_TZ"), "Europe/Brussels"))
	if err != nil {
		return nil, err
	}
	start := store.DefaultStartMonth
	if v := os.Getenv("INCOME_START_MONTH"); v != "" {
		if start, err = domain.ParseMonth(v); err != nil {
			return nil, err
		}
	}
	pauseVal := strings.ToLower(cmp.Or(os.Getenv("INCOME_PAUSE_FINANCIAL_WRITES"), os.Getenv("INCOME_PAUSE_WRITES")))
	paused := pauseVal == "true" || pauseVal == "1" || pauseVal == "yes"
	slog.Info("store settings", "timezone", loc.String(), "startMonth", start.String(), "financialWritesPaused", paused)
	return []store.Option{
		store.WithLocation(loc),
		store.WithStartMonth(start),
		store.WithFinancialWritesPaused(paused),
	}, nil
}

// setUpLocal gives the in-memory store the default data and an admin to
// sign in with: anna, with a random password printed to the log. Only
// ever for local runs; production is set up once by cmd/seed.
func setUpLocal(st *store.Store) error {
	password := strings.ToLower(rand.Text()[:12])
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	ctx := context.Background()
	admin, err := st.SetUp(ctx, store.DefaultSetup(store.NewUser{
		Username: "anna", DisplayName: "Анна", Role: store.RoleAdmin, SubmitsReports: true, PasswordHash: string(hash),
	}))
	if err != nil {
		return err
	}
	terms := domain.Terms{SelfPct: 4500, SalaryCents: 120000, DividendWeight: 1}
	if err := st.SetTerms(ctx, admin.Actor(), admin.ID, st.StartMonth(), terms, "local"); err != nil {
		return err
	}
	slog.Warn("local in-memory login (must be changed at first login)", "username", "anna", "password", password)
	return nil
}
