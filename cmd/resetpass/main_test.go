package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/lieranderl/aestheticlab-income-app/internal/store"
)

func TestResetGivesAWorkingTemporaryPasswordAndEndsSessions(t *testing.T) {
	ctx := context.Background()
	s := store.NewMemory()
	admin, err := s.SetUp(ctx, store.DefaultSetup(store.NewUser{Username: "anna", DisplayName: "Анна", Role: store.RoleAdmin, PasswordHash: "old"}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.RecordLoginFailure(ctx, admin.ID, 1, time.Hour); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := run(ctx, s, &out, " Anna "); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	password := lines[len(lines)-1]
	u, err := s.User(ctx, admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) != nil || len(password) != 12 {
		t.Fatalf("printed password %q does not match the stored hash", password)
	}
	if !u.MustChangePassword || u.SessionVersion != admin.SessionVersion+1 || !u.LockedUntil.IsZero() || u.FailedLogins != 0 {
		t.Errorf("after reset: %+v", u)
	}
	audit, err := s.Audit(ctx, admin.ID)
	if err != nil || len(audit) == 0 || audit[0].ActorID != "cli:resetpass" || strings.Contains(audit[0].After, password) {
		t.Errorf("audit = %+v, %v", audit, err)
	}

	if err := run(ctx, s, &out, "nobody"); err == nil {
		t.Error("an unknown username was accepted")
	}
}
