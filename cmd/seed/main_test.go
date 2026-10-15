package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"github.com/lieranderl/aestheticlab-income-app/internal/store"
)

func TestSeedSetsUpOnceAndPrintsAWorkingPassword(t *testing.T) {
	ctx := context.Background()
	s := store.NewMemory()
	var out bytes.Buffer
	if err := run(ctx, s, &out, "Anna", "Анна"); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	password := lines[len(lines)-1]
	admin, err := s.UserByUsername(ctx, "anna")
	if err != nil {
		t.Fatal(err)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(admin.PasswordHash), []byte(password)); err != nil || len(password) != 12 {
		t.Fatalf("printed password %q does not match the stored hash: %v", password, err)
	}
	if admin.Role != store.RoleAdmin || !admin.SubmitsReports || !admin.MustChangePassword {
		t.Errorf("admin = %+v", admin)
	}
	cats, err := s.Categories(ctx)
	if err != nil || len(cats) != 16 {
		t.Fatalf("categories = %d, %v", len(cats), err)
	}

	out.Reset()
	if err := run(ctx, s, &out, "anna", "Анна"); !errors.Is(err, errAlreadySetUp) || out.Len() != 0 {
		t.Fatalf("second run: %v, printed %q", err, out.String())
	}
}
