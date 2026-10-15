// Command e2e serves the app for the Playwright phone checks (e2e/): an
// in-memory store with a fixed November clock, seeded staff whose test
// passwords are in e2e/fixtures.ts, and a few records so every screen has
// something to show. It listens on loopback only and never touches Firestore;
// the production image builds cmd/web alone.
package main

import (
	"cmp"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/lieranderl/aestheticlab-income-app/internal/domain"
	"github.com/lieranderl/aestheticlab-income-app/internal/store"
	"github.com/lieranderl/aestheticlab-income-app/internal/web"
)

// Test-only sign-ins; e2e/fixtures.ts uses the same values.
var people = []struct {
	username, name, password string
	admin                    bool
	terms                    domain.Terms
}{
	{"anna", "Анна", "e2e-anna-pass", true, domain.Terms{SelfPct: 4500, SalaryCents: 120000, DividendWeight: 1}},
	{"olga", "Ольга", "e2e-olga-pass", false, domain.Terms{SelfPct: 6500, DividendWeight: 1}},
	{"maria", "Мария", "e2e-maria-pass", false, domain.Terms{SelfPct: 6500, DividendWeight: 1}},
}

func main() {
	if os.Getenv("INCOME_FIRESTORE_PROJECT") != "" || os.Getenv("FIRESTORE_EMULATOR_HOST") != "" {
		slog.Error("cmd/e2e only runs on the in-memory store; unset the Firestore settings")
		os.Exit(1)
	}
	loc, err := time.LoadLocation("Europe/Brussels")
	if err != nil {
		slog.Error("timezone", "error", err)
		os.Exit(1)
	}
	// 20 November 2026, 10:00 in Brussels, ticking from there so sessions work.
	base, started := time.Date(2026, time.November, 20, 10, 0, 0, 0, loc), time.Now()
	now := func() time.Time { return base.Add(time.Since(started)) }

	st := store.NewMemory(store.WithClock(now), store.WithLocation(loc), store.WithStartMonth(domain.Month{Year: 2026, Mon: time.November}))
	if err := seed(context.Background(), st); err != nil {
		slog.Error("seed", "error", err)
		os.Exit(1)
	}

	addr := "127.0.0.1:" + cmp.Or(os.Getenv("PORT"), "8097")
	handler, err := web.New(web.Config{BaseURL: "http://" + addr, Store: st, Now: now, PasswordCost: bcrypt.MinCost})
	if err != nil {
		slog.Error("web config", "error", err)
		os.Exit(1)
	}
	slog.Info("e2e server", "addr", addr)
	srv := &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("serve", "error", err)
		os.Exit(1)
	}
}

func seed(ctx context.Context, st *store.Store) error {
	users := map[string]store.User{}
	var admin store.Actor
	for _, p := range people {
		hash, err := bcrypt.GenerateFromPassword([]byte(p.password), bcrypt.MinCost)
		if err != nil {
			return err
		}
		n := store.NewUser{Username: p.username, DisplayName: p.name, Role: store.RoleEmployee, SubmitsReports: true, PasswordHash: string(hash)}
		var u store.User
		if p.admin {
			n.Role = store.RoleAdmin
			u, err = st.SetUp(ctx, store.DefaultSetup(n))
			admin = u.Actor()
		} else {
			u, err = st.CreateUser(ctx, admin, n)
		}
		if err != nil {
			return err
		}
		if u, err = st.UpdateUser(ctx, admin, u.ID, func(x *store.User) error { x.MustChangePassword = false; return nil }); err != nil {
			return err
		}
		if err := st.SetTerms(ctx, admin, u.ID, st.StartMonth(), p.terms, "e2e"); err != nil {
			return err
		}
		users[p.username] = u
	}

	day := func(d int) domain.Date { return domain.Date{Year: 2026, Mon: time.November, Day: d} }
	for i, c := range []struct {
		who string
		day int
		in  domain.ClientInput
	}{
		{"olga", 19, domain.ClientInput{Services: 12000, ServiceCash: 4000}},
		{"olga", 20, domain.ClientInput{Services: 8000, ServiceCard: 8000, Extras: map[string]domain.Cents{"creams": 2500}, ExtrasDestination: domain.ToCash}},
		{"anna", 20, domain.ClientInput{Services: 15000, ServiceCard: 7500}},
		{"maria", 18, domain.ClientInput{Services: 9000, ServiceCash: 2700}},
	} {
		u := users[c.who]
		token := "e2e-client-token-" + string(rune('a'+i)) + "-0000"
		if _, err := st.SaveClientReport(ctx, u.Actor(), store.ClientEntry{Person: u.ID, Date: day(c.day), Token: token, Input: c.in}); err != nil {
			return err
		}
	}
	// One confirmed salon expense and one staff expense waiting for the admin.
	if _, err := st.RecordMovement(ctx, admin, store.NewMovement{Type: domain.Expense, Amount: 4500, From: domain.Account(domain.KassaCard), To: domain.Outside, ExpenseCategory: "materials", Note: "Расходники", Date: day(17)}); err != nil {
		return err
	}
	olga := users["olga"]
	_, err := st.RecordMovement(ctx, olga.Actor(), store.NewMovement{Type: domain.Expense, Amount: 1200, From: domain.Person(olga.ID), To: domain.Outside, ExpenseCategory: "materials", Note: "Перчатки", Date: day(18)})
	return err
}
