package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/lieranderl/aestheticlab-income-app/internal/domain"
	"github.com/lieranderl/aestheticlab-income-app/internal/store"
)

var ctx = context.Background()

// salon is the store on the morning of 1 November 2026 with Anna (admin)
// and Maria.
func salon(t *testing.T, now time.Time) *store.Store {
	t.Helper()
	st := store.NewMemory(store.WithClock(func() time.Time { return now }))
	anna, err := st.SetUp(ctx, store.DefaultSetup(store.NewUser{Username: "anna", DisplayName: "Анна", Role: store.RoleAdmin, SubmitsReports: true, PasswordHash: "x"}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateUser(ctx, anna.Actor(), store.NewUser{Username: "maria", DisplayName: "Мария", Role: store.RoleEmployee, SubmitsReports: true, PasswordHash: "x"}); err != nil {
		t.Fatal(err)
	}
	return st
}

var firstMorning = time.Date(2026, 11, 1, 8, 0, 0, 0, time.UTC)

const openingFile = `kind;id;amount;already_paid
kassa;kassa_cash;412,50;
kassa;kassa_card;1 830;
certificates;;500;
person;maria;265,13;0
person;anna;-42,70;1950
`

func TestOpeningBalancesArePrintedFirstThenLoadedOnce(t *testing.T) {
	st := salon(t, firstMorning)
	var out bytes.Buffer
	if err := runOpening(ctx, st, options{admin: "anna", dryRun: true}, strings.NewReader(openingFile), &out); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"2026-11-01", "dry run", "Касса — наличные", "€412,50", "Касса — карта салона", "€1\u202f830",
		"certificates", "€500", "Мария (maria)", "€265,13", "Анна (anna)", "−€42,70", "€1\u202f950",
		"kassa total €2\u202f242,50",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("dry run lacks %q:\n%s", want, out.String())
		}
	}
	if b := balances(t, st); b.KassaTotal() != 0 || b.CertificatesOutstanding != 0 || len(b.Persons) != 0 {
		t.Fatalf("a dry run wrote: %+v", b)
	}

	out.Reset()
	if err := runOpening(ctx, st, options{admin: "anna"}, strings.NewReader(openingFile), &out); err != nil {
		t.Fatal(err)
	}
	b := balances(t, st)
	maria, anna := user(t, st, "maria"), user(t, st, "anna")
	if b.Accounts[domain.KassaCash] != 41250 || b.Accounts[domain.KassaCard] != 183000 || b.CertificatesOutstanding != 50000 ||
		b.Persons[maria.ID] != 26513 || b.Persons[anna.ID] != -4270 {
		t.Fatalf("after loading: %+v", b)
	}
	if !strings.Contains(out.String(), "loaded 5") {
		t.Errorf("real run says:\n%s", out.String())
	}

	// Running it again (say, with the person rows added later) skips what
	// is already there, so nothing is counted twice.
	out.Reset()
	if err := runOpening(ctx, st, options{admin: "anna"}, strings.NewReader(openingFile), &out); err != nil {
		t.Fatal(err)
	}
	if again := balances(t, st); again.KassaTotal() != b.KassaTotal() || again.Persons[maria.ID] != 26513 {
		t.Fatalf("a second run changed the balances: %+v", again)
	}
	if !strings.Contains(out.String(), "already loaded") || !strings.Contains(out.String(), "loaded 0") {
		t.Errorf("second run says:\n%s", out.String())
	}

	// A different figure for a party that already has one is refused: the
	// wrong row has to be voided first.
	changed := strings.Replace(openingFile, "412,50", "400", 1)
	if err := runOpening(ctx, st, options{admin: "anna"}, strings.NewReader(changed), &out); err == nil || !strings.Contains(err.Error(), "kassa_cash") {
		t.Fatalf("a changed opening figure: %v", err)
	}
}

func TestAnOpeningFileWithMistakesIsRefusedWhole(t *testing.T) {
	st := salon(t, firstMorning)
	for name, file := range map[string]string{
		"no header":                 "kassa;kassa_cash;100\n",
		"unknown kind":              "kind;id;amount\nbank;x;100\n",
		"unknown account":           "kind;id;amount\nkassa;kassa_gold;100\n",
		"unknown person":            "kind;id;amount\nperson;nobody;100\n",
		"negative kassa":            "kind;id;amount\nkassa;kassa_cash;-100\n",
		"unreadable amount":         "kind;id;amount\nkassa;kassa_cash;1.234,5\n",
		"the same party twice":      "kind;id;amount\nperson;maria;10\nperson;maria;20\n",
		"certificates with an id":   "kind;id;amount\ncertificates;x;10\n",
		"a comma inside the amount": "kind,id,amount,already_paid\nperson,maria,265,13,0\n",
	} {
		var out bytes.Buffer
		if err := runOpening(ctx, st, options{admin: "anna"}, strings.NewReader(file), &out); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if b := balances(t, st); b.KassaTotal() != 0 || len(b.Persons) != 0 {
		t.Fatalf("a refused file wrote: %+v", b)
	}
}

func TestOpeningBalancesLoadOnlyFromTheStartDay(t *testing.T) {
	st := salon(t, firstMorning.AddDate(0, 0, -1)) // 31 October
	var out bytes.Buffer
	if err := runOpening(ctx, st, options{admin: "anna", dryRun: true}, strings.NewReader(openingFile), &out); err != nil {
		t.Fatalf("a dry run the day before: %v", err)
	}
	if !strings.Contains(out.String(), "from 2026-11-01") {
		t.Errorf("the dry run does not say when it can be loaded:\n%s", out.String())
	}
	if err := runOpening(ctx, st, options{admin: "anna"}, strings.NewReader(openingFile), &out); err == nil {
		t.Fatal("loaded before the start day")
	}
}

func balances(t *testing.T, st *store.Store) domain.Balances {
	t.Helper()
	b, err := st.Balances(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func user(t *testing.T, st *store.Store, username string) store.User {
	t.Helper()
	u, err := st.UserByUsername(ctx, username)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

// With an October start the person figures come from the September
// settlement, and the dry run says so.
func TestTheDryRunNamesTheMonthBeforeTheStart(t *testing.T) {
	now := time.Date(2026, 10, 14, 8, 0, 0, 0, time.UTC)
	st := store.NewMemory(store.WithClock(func() time.Time { return now }), store.WithStartMonth(domain.Month{Year: 2026, Mon: time.October}))
	if _, err := st.SetUp(ctx, store.DefaultSetup(store.NewUser{Username: "anna", DisplayName: "Анна", Role: store.RoleAdmin, SubmitsReports: true, PasswordHash: "x"})); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	file := "kind;id;amount;already_paid\nperson;anna;-42,70;1950\n"
	if err := runOpening(ctx, st, options{admin: "anna", dryRun: true}, strings.NewReader(file), &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "2026-10-01") || !strings.Contains(out.String(), "already paid in September (sheet): €1\u202f950") {
		t.Fatalf("dry run:\n%s", out.String())
	}
}

func TestWithoutAnAdminNameTheOnlyAdminRecordsTheRows(t *testing.T) {
	st := salon(t, firstMorning)
	var out bytes.Buffer
	if err := runOpening(ctx, st, options{dryRun: true}, strings.NewReader(openingFile), &out); err != nil {
		t.Fatalf("no -admin with one admin: %v", err)
	}
}

func TestTheAdminMustBeAnActiveAdmin(t *testing.T) {
	st := salon(t, firstMorning)
	var out bytes.Buffer
	for name, want := range map[string]string{"maria": `"maria" is not an admin`, "nobody": `no user "nobody"`} {
		err := runOpening(ctx, st, options{admin: name, dryRun: true}, strings.NewReader(openingFile), &out)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("-admin %s: %v, want %q", name, err, want)
		}
	}
}

func TestWithTwoAdminsTheAdminMustBeNamed(t *testing.T) {
	st := salon(t, firstMorning)
	anna, err := st.UserByUsername(ctx, "anna")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateUser(ctx, anna.Actor(), store.NewUser{Username: "olga", DisplayName: "Ольга", Role: store.RoleAdmin, PasswordHash: "x"}); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := runOpening(ctx, st, options{dryRun: true}, strings.NewReader(openingFile), &out); err == nil || !strings.Contains(err.Error(), "-admin") {
		t.Fatalf("two admins, no -admin: %v, want a request for -admin", err)
	}
}
