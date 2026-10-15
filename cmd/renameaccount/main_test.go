package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/lieranderl/aestheticlab-income-app/internal/domain"
	"github.com/lieranderl/aestheticlab-income-app/internal/store"
)

func TestRenameShowsTheChangeAndWritesItOnlyWhenAsked(t *testing.T) {
	ctx := context.Background()
	s := store.NewMemory()
	if _, err := s.SetUp(ctx, store.DefaultSetup(store.NewUser{Username: "anna", DisplayName: "Анна", Role: store.RoleAdmin, PasswordHash: "x"})); err != nil {
		t.Fatal(err)
	}
	name := func() string {
		accounts, err := s.Accounts(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, a := range accounts {
			if a.ID == domain.KassaCard {
				return a.Name
			}
		}
		return ""
	}
	before := name()

	var out bytes.Buffer
	if err := run(ctx, s, &out, domain.KassaCard, "Касса — карта студии", true); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "dry run") || !strings.Contains(out.String(), "Касса — карта студии") || name() != before {
		t.Fatalf("dry run wrote or said too little: %q, name %q", out.String(), name())
	}

	out.Reset()
	if err := run(ctx, s, &out, domain.KassaCard, "Касса — карта студии", false); err != nil {
		t.Fatal(err)
	}
	if name() != "Касса — карта студии" || !strings.Contains(out.String(), "renamed") {
		t.Fatalf("after rename: %q, output %q", name(), out.String())
	}
	entries, err := s.Audit(ctx, domain.KassaCard)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range entries {
		found = found || (e.ActorID == "cli:renameaccount" && e.Action == store.ActionUpdate)
	}
	if !found {
		t.Fatalf("no audited rename by the command: %+v", entries)
	}
	if err := run(ctx, s, &out, "nope", "x", false); err == nil {
		t.Fatal("an unknown account was renamed")
	}
}
