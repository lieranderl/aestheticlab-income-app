package store_test

import (
	"context"
	"crypto/rand"
	"errors"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lieranderl/aestheticlab-income-app/internal/domain"
	"github.com/lieranderl/aestheticlab-income-app/internal/store"
)

var ctx = context.Background()

// salon is a set-up store with four people and their November terms, at
// 20 November 2026 12:00 UTC, production start month 2026-11.
type salon struct {
	*store.Store
	clock                   *time.Time
	admin                   store.Actor
	anna, olga, maria, vera string
}

func (s *salon) at(day string) {
	d, err := time.Parse(time.DateOnly, day)
	if err != nil {
		panic(err)
	}
	*s.clock = d.Add(12*time.Hour + 987654321)
}

func (s *salon) actor(uid string) store.Actor { return store.Actor{UID: uid} }

// openers are the backends every behaviour is checked against: memory
// always, Firestore when its emulator runs (FIRESTORE_EMULATOR_HOST, as
// `make firestore` and CI start it). Each Firestore run gets its own
// emulator project, so runs never see each other's documents.
func openers(t *testing.T) map[string]func(opts ...store.Option) *store.Store {
	t.Helper()
	out := map[string]func(opts ...store.Option) *store.Store{
		"memory": func(opts ...store.Option) *store.Store { return store.NewMemory(opts...) },
	}
	if os.Getenv("FIRESTORE_EMULATOR_HOST") != "" {
		out["firestore"] = func(opts ...store.Option) *store.Store {
			project := "demo-kassa-" + strings.ToLower(rand.Text()[:12])
			fs, err := store.NewFirestore(ctx, project, "aestheticlab-income", opts...)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = fs.Close() })
			return fs
		}
	}
	return out
}

func eachStore(t *testing.T, check func(t *testing.T, open func(opts ...store.Option) *store.Store)) {
	t.Helper()
	if os.Getenv("FIRESTORE_EMULATOR_HOST") == "" {
		t.Log("Firestore not checked: set FIRESTORE_EMULATOR_HOST (make firestore)")
	}
	for name, open := range openers(t) {
		t.Run(name, func(t *testing.T) { check(t, open) })
	}
}

func eachSalon(t *testing.T, check func(t *testing.T, s *salon)) {
	t.Helper()
	eachStore(t, func(t *testing.T, open func(opts ...store.Option) *store.Store) {
		// Nanoseconds on the clock: Firestore keeps microseconds, so the store
		// must round the same way on both backends.
		clock := time.Date(2026, 11, 20, 12, 0, 0, 123456789, time.UTC)
		st := open(store.WithClock(func() time.Time { return clock }))
		s := &salon{Store: st, clock: &clock}
		admin, err := st.SetUp(ctx, defaultSetup())
		must(t, err)
		s.admin, s.anna = admin.Actor(), admin.ID
		s.olga = s.addPerson(t, "olga", "Ольга")
		s.maria = s.addPerson(t, "maria", "Мария")
		s.vera = s.addPerson(t, "vera", "Вера")
		nov := month(t, "2026-11")
		must(t, st.SetTerms(ctx, s.admin, s.anna, nov, domain.Terms{SelfPct: 5000, SalaryCents: 100000, DividendWeight: 1}, "старт"))
		must(t, st.SetTerms(ctx, s.admin, s.olga, nov, domain.Terms{SelfPct: 7000, DividendWeight: 1}, ""))
		must(t, st.SetTerms(ctx, s.admin, s.maria, nov, domain.Terms{SelfPct: 7000, DividendWeight: 1}, ""))
		must(t, st.SetTerms(ctx, s.admin, s.vera, nov, domain.Terms{SelfPct: 5000}, ""))
		check(t, s)
	})
}

func (s *salon) addPerson(t *testing.T, username, name string) string {
	t.Helper()
	u, err := s.CreateUser(ctx, s.admin, store.NewUser{Username: username, DisplayName: name, Role: store.RoleEmployee, SubmitsReports: true, PasswordHash: "x"})
	must(t, err)
	return u.ID
}

func defaultSetup() store.Setup {
	return store.DefaultSetup(store.NewUser{Username: "anna", DisplayName: "Анна", Role: store.RoleAdmin, SubmitsReports: true, PasswordHash: "hash"})
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func month(t *testing.T, s string) domain.Month {
	t.Helper()
	m, err := domain.ParseMonth(s)
	must(t, err)
	return m
}

func date(t *testing.T, s string) domain.Date {
	t.Helper()
	d, err := domain.ParseDate(s)
	must(t, err)
	return d
}

func cents(c domain.Cents) *domain.Cents { return &c }

func problem(err error) domain.Problem {
	var ve *domain.ValidationError
	if errors.As(err, &ve) {
		return ve.Problem
	}
	return ""
}

func wantErr(t *testing.T, what string, err, want error) {
	t.Helper()
	if !errors.Is(err, want) {
		t.Fatalf("%s: error %v, want %v", what, err, want)
	}
}

func wantProblem(t *testing.T, what string, err error, want domain.Problem) {
	t.Helper()
	if got := problem(err); got != want {
		t.Fatalf("%s: error %v (problem %q), want problem %q", what, err, got, want)
	}
}

// actions are the audit actions recorded for a document, oldest first.
func (s *salon) actions(t *testing.T, entityID string) []string {
	t.Helper()
	rows, err := s.Audit(ctx, entityID)
	must(t, err)
	out := make([]string, len(rows))
	for i, r := range rows {
		out[len(rows)-1-i] = r.Action
	}
	return out
}

// day is a simple report: services paid by card and cash.
func day(person, d string, t *testing.T, card, cash domain.Cents) store.ReportEntry {
	return store.ReportEntry{
		Person: person, Date: date(t, d),
		Revenue:  map[string]domain.Cents{"services": card + cash},
		Payments: map[string]domain.Cents{"card": card, "cash": cash},
	}
}

func TestSetUpRunsOnlyOnce(t *testing.T) {
	eachSalon(t, func(t *testing.T, s *salon) {
		_, err := s.SetUp(ctx, defaultSetup())
		wantErr(t, "second setup", err, store.ErrAlreadySetUp)

		anna, err := s.UserByUsername(ctx, " Anna ")
		must(t, err)
		if anna.ID != s.anna || anna.Role != store.RoleAdmin || !anna.MustChangePassword || !anna.Active {
			t.Errorf("admin = %+v", anna)
		}
		cats, err := s.Categories(ctx)
		must(t, err)
		accs, err := s.Accounts(ctx)
		must(t, err)
		if len(cats) != 16 || len(accs) != 2 || accs[0].ID != domain.KassaCash {
			t.Errorf("categories %d, accounts %+v", len(cats), accs)
		}
		if got := s.actions(t, "rent"); !reflect.DeepEqual(got, []string{store.ActionCreate}) {
			t.Errorf("rent audit = %v", got)
		}
		users, err := s.Users(ctx)
		must(t, err)
		if len(users) != 4 {
			t.Errorf("users = %d, want 4", len(users))
		}
	})
}

func TestUsernamesAreUniqueAndOnlyTheAdminCreatesAccounts(t *testing.T) {
	eachSalon(t, func(t *testing.T, s *salon) {
		_, err := s.CreateUser(ctx, s.admin, store.NewUser{Username: "OLGA", DisplayName: "Другая", Role: store.RoleEmployee, PasswordHash: "x"})
		wantErr(t, "duplicate username", err, store.ErrUsernameTaken)
		_, err = s.CreateUser(ctx, s.actor(s.olga), store.NewUser{Username: "new", DisplayName: "Новая", Role: store.RoleEmployee, PasswordHash: "x"})
		wantErr(t, "employee creates", err, store.ErrForbidden)

		audit, err := s.Audit(ctx, s.olga)
		must(t, err)
		if len(audit) != 1 || strings.Contains(audit[0].After, `"x"`) {
			t.Errorf("user audit must exist and leave out the password hash: %+v", audit)
		}
	})
}

func TestPeopleChangeOnlyTheirOwnLoginState(t *testing.T) {
	eachSalon(t, func(t *testing.T, s *salon) {
		u, err := s.UpdateUser(ctx, s.actor(s.olga), s.olga, func(u *store.User) error {
			u.PasswordHash, u.MustChangePassword = "new", false
			return nil
		})
		must(t, err)
		if u.MustChangePassword || u.PasswordHash != "new" || u.Username != "olga" {
			t.Errorf("updated = %+v", u)
		}
		_, err = s.UpdateUser(ctx, s.actor(s.olga), s.olga, func(u *store.User) error { u.Role = store.RoleAdmin; return nil })
		wantErr(t, "self-promotion", err, store.ErrForbidden)
		_, err = s.UpdateUser(ctx, s.actor(s.olga), s.maria, func(*store.User) error { return nil })
		wantErr(t, "someone else", err, store.ErrForbidden)
		if got := s.actions(t, s.olga); !reflect.DeepEqual(got, []string{store.ActionCreate, store.ActionUpdate}) {
			t.Errorf("audit = %v", got)
		}
	})
}

func TestTheAdminChangesALoginNameAndFreesTheOldOne(t *testing.T) {
	eachSalon(t, func(t *testing.T, s *salon) {
		u, err := s.RenameUser(ctx, s.admin, s.olga, " Olga2 ")
		must(t, err)
		if u.Username != "olga2" || u.DisplayName != "Ольга" {
			t.Errorf("renamed = %+v", u)
		}
		got, err := s.UserByUsername(ctx, "olga2")
		must(t, err)
		if got.ID != s.olga {
			t.Errorf("olga2 = %s, want %s", got.ID, s.olga)
		}
		_, err = s.UserByUsername(ctx, "olga")
		wantErr(t, "old name", err, store.ErrNotFound)

		_, err = s.RenameUser(ctx, s.admin, s.maria, "olga2")
		wantErr(t, "taken name", err, store.ErrUsernameTaken)
		_, err = s.RenameUser(ctx, s.actor(s.olga), s.olga, "olga3")
		wantErr(t, "employee renames", err, store.ErrForbidden)
		_, err = s.CreateUser(ctx, s.admin, store.NewUser{Username: "olga", DisplayName: "Новая Ольга", Role: store.RoleEmployee, PasswordHash: "x"})
		must(t, err)

		if got := s.actions(t, s.olga); !reflect.DeepEqual(got, []string{store.ActionCreate, store.ActionUpdate}) {
			t.Errorf("audit = %v", got)
		}
	})
}

func TestRemovedPeopleKeepTheirHistoryAndFreeTheirLoginName(t *testing.T) {
	eachSalon(t, func(t *testing.T, s *salon) {
		wantErr(t, "employee removes", s.RemoveUser(ctx, s.actor(s.maria), s.vera), store.ErrForbidden)
		wantErr(t, "admin removes self", s.RemoveUser(ctx, s.admin, s.anna), store.ErrForbidden)

		// Work in a month not yet closed would still accrue at its close.
		_, err := s.SaveReport(ctx, s.actor(s.olga), store.ReportEntry{Person: s.olga, Date: date(t, "2026-11-10"), Revenue: map[string]domain.Cents{"services": 20000}, Payments: map[string]domain.Cents{"card": 20000}})
		must(t, err)
		wantErr(t, "report in an open month", s.RemoveUser(ctx, s.admin, s.olga), store.ErrOpenRecords)

		// Terms never end on their own: a salary or profit share in force
		// would keep accruing at every later close.
		wantErr(t, "profit share in force", s.RemoveUser(ctx, s.admin, s.maria), store.ErrTermsInForce)
		must(t, s.SetTerms(ctx, s.admin, s.maria, month(t, "2026-11"), domain.Terms{SelfPct: 7000}, "уходит"))
		must(t, s.RemoveUser(ctx, s.admin, s.maria))

		// Vera has no salary or share; money still waiting or owed blocks removal.
		dep, err := s.RecordMovement(ctx, s.actor(s.vera), store.NewMovement{Type: domain.Deposit, Amount: 10000, From: domain.Person(s.vera), To: domain.Account("kassa_cash"), Date: date(t, "2026-11-20")})
		must(t, err)
		wantErr(t, "pending movement", s.RemoveUser(ctx, s.admin, s.vera), store.ErrOpenRecords)
		_, err = s.ConfirmMovement(ctx, s.admin, dep.ID, dep.Version)
		must(t, err)
		wantErr(t, "salon owes her", s.RemoveUser(ctx, s.admin, s.vera), store.ErrBalanceOpen)
		_, err = s.RecordMovement(ctx, s.admin, store.NewMovement{Type: domain.Payout, Amount: 10000, From: domain.Account("kassa_cash"), To: domain.Person(s.vera), Date: date(t, "2026-11-20")})
		must(t, err)

		before, err := s.User(ctx, s.vera)
		must(t, err)
		must(t, s.RemoveUser(ctx, s.admin, s.vera))
		gone, err := s.User(ctx, s.vera)
		must(t, err)
		if !gone.Removed || gone.Active || gone.SessionVersion != before.SessionVersion+1 || gone.DisplayName != "Вера" {
			t.Errorf("removed = %+v", gone)
		}
		_, err = s.UserByUsername(ctx, "vera")
		wantErr(t, "removed login", err, store.ErrNotFound)
		terms, err := s.Terms(ctx, s.vera)
		must(t, err)
		if len(terms) != 1 {
			t.Errorf("terms kept = %d, want 1", len(terms))
		}

		_, err = s.UpdateUser(ctx, s.admin, s.vera, func(u *store.User) error { u.Active, u.Removed = true, false; return nil })
		wantErr(t, "bring back", err, store.ErrInvalid)
		_, err = s.RenameUser(ctx, s.admin, s.vera, "vera2")
		wantErr(t, "rename removed", err, store.ErrInvalid)
		_, err = s.SaveReport(ctx, s.admin, store.ReportEntry{Person: s.vera, Date: date(t, "2026-11-20"), Revenue: map[string]domain.Cents{"services": 100}, Payments: map[string]domain.Cents{"card": 100}})
		wantErr(t, "report for a removed person", err, store.ErrForbidden)
		wantErr(t, "terms for a removed person", s.SetTerms(ctx, s.admin, s.vera, month(t, "2026-12"), domain.Terms{SelfPct: 5000, SalaryCents: 50000, DividendWeight: 1}, ""), store.ErrForbidden)
		again, err := s.CreateUser(ctx, s.admin, store.NewUser{Username: "vera", DisplayName: "Вера Н.", Role: store.RoleEmployee, PasswordHash: "x"})
		must(t, err)
		if again.ID == s.vera {
			t.Error("a new account must get a new ID")
		}
		if got := s.actions(t, s.vera); !reflect.DeepEqual(got, []string{store.ActionCreate, store.ActionRemove}) {
			t.Errorf("audit = %v", got)
		}
	})
}

func TestTheServerComputesAndFreezesTheReport(t *testing.T) {
	eachSalon(t, func(t *testing.T, s *salon) {
		saved, err := s.SaveReport(ctx, s.actor(s.anna), store.ReportEntry{
			Person: s.anna, Date: date(t, "2026-11-08"),
			Revenue:  map[string]domain.Cents{"services": 60000, "creams": 5000, "certificates": 10000, "unused": 0},
			Payments: map[string]domain.Cents{"card": 60000, "cash": 15000},
		})
		must(t, err)
		r := saved.Report
		if r.Total != 75000 || r.SelfBase != 65000 || r.SelfAmount != 32500 || r.SalonShare != 42500 ||
			r.SalonIncome != 32500 || r.CashToKassa != 0 || r.BalanceDelta != 17500 || saved.Version != 1 {
			t.Fatalf("saved = %+v v%d", r, saved.Version)
		}
		got, err := s.Report(ctx, s.anna, date(t, "2026-11-08"))
		must(t, err)
		if !reflect.DeepEqual(got, saved) {
			t.Fatalf("read back\n%+v\nwant\n%+v", got, saved)
		}
		if _, ok := got.Revenue["unused"]; ok || !got.Flags.Revenue["certificates"].IsCertificateSale {
			t.Errorf("zero amounts are dropped and flags frozen: %+v", got.PersonReport)
		}
		if got := s.actions(t, s.anna+"_2026-11-08"); !reflect.DeepEqual(got, []string{store.ActionCreate}) {
			t.Errorf("audit = %v", got)
		}
		cats, err := s.Categories(ctx)
		must(t, err)
		for _, c := range cats {
			want := int64(0)
			if c.ID == "services" || c.ID == "creams" || c.ID == "certificates" || c.ID == "card" || c.ID == "cash" {
				want = 1
			}
			if c.UsedCount != want {
				t.Errorf("%s used %d times, want %d", c.ID, c.UsedCount, want)
			}
		}
	})
}

func TestATransferToAnnasCardIsSavedAndFillsTheCard(t *testing.T) {
	eachSalon(t, func(t *testing.T, s *salon) {
		e := day(s.olga, "2026-11-08", t, 0, 20000) // Olga 70%: «Салону» €60
		e.TransferToCard = ptr(domain.Cents(2000))
		saved, err := s.SaveReport(ctx, s.actor(s.olga), e)
		must(t, err)
		if r := saved.Report; r.TransferToCard != 2000 || r.CashToKassa != 4000 || r.BalanceDelta != 0 {
			t.Fatalf("saved = %+v", r)
		}
		got, err := s.Report(ctx, s.olga, date(t, "2026-11-08"))
		must(t, err)
		if !reflect.DeepEqual(got, saved) {
			t.Fatalf("read back\n%+v\nwant\n%+v", got, saved)
		}
		b, err := s.Balances(ctx)
		must(t, err)
		if b.Accounts[domain.KassaCard] != 2000 || b.Accounts[domain.KassaCash] != 4000 {
			t.Fatalf("balances = %+v", b.Accounts)
		}
	})
}

func TestAReportChangedBySomeoneElseIsRejected(t *testing.T) {
	eachSalon(t, func(t *testing.T, s *salon) {
		e := day(s.olga, "2026-11-10", t, 0, 20000)
		_, err := s.SaveReport(ctx, s.actor(s.olga), e)
		must(t, err)
		_, err = s.SaveReport(ctx, s.admin, e) // still at version 0
		wantErr(t, "stale version", err, store.ErrVersionConflict)

		e.Version, e.CashToKassa = 1, cents(5000)
		saved, err := s.SaveReport(ctx, s.admin, e)
		must(t, err)
		if saved.Version != 2 || saved.UpdatedBy != s.anna || saved.Report.CashToKassa != 5000 {
			t.Errorf("saved = v%d by %s, cash to kassa %d", saved.Version, saved.UpdatedBy, saved.Report.CashToKassa)
		}
		fresh := day(s.olga, "2026-11-11", t, 100, 0)
		fresh.Version = 3
		_, err = s.SaveReport(ctx, s.actor(s.olga), fresh)
		wantErr(t, "new report with a version", err, store.ErrVersionConflict)
		if got := s.actions(t, s.olga+"_2026-11-10"); !reflect.DeepEqual(got, []string{store.ActionCreate, store.ActionUpdate}) {
			t.Errorf("audit = %v", got)
		}
	})
}

func TestStaffSaveOnlyTheirOwnReports(t *testing.T) {
	eachSalon(t, func(t *testing.T, s *salon) {
		_, err := s.SaveReport(ctx, s.actor(s.olga), day(s.maria, "2026-11-10", t, 100, 0))
		wantErr(t, "another person's report", err, store.ErrForbidden)
		_, err = s.SaveReport(ctx, s.admin, day(s.maria, "2026-11-10", t, 100, 0))
		must(t, err)
	})
}

func TestReportsNeedValidInputAndTermsForTheMonth(t *testing.T) {
	eachSalon(t, func(t *testing.T, s *salon) {
		bad := day(s.olga, "2026-11-10", t, 100, 0)
		bad.Payments["cash"] = 50
		_, err := s.SaveReport(ctx, s.actor(s.olga), bad)
		wantProblem(t, "payments mismatch", err, domain.ProblemPaymentsMismatch)

		u, err := s.CreateUser(ctx, s.admin, store.NewUser{Username: "new", DisplayName: "Новая", Role: store.RoleEmployee, SubmitsReports: true, PasswordHash: "x"})
		must(t, err)
		_, err = s.SaveReport(ctx, s.actor(u.ID), day(u.ID, "2026-11-10", t, 100, 0))
		wantProblem(t, "no terms", err, domain.ProblemMissingTerms)
	})
}

func TestNothingIsDatedBeforeTheStartMonthOrInTheFuture(t *testing.T) {
	eachSalon(t, func(t *testing.T, s *salon) {
		_, err := s.SaveReport(ctx, s.actor(s.olga), day(s.olga, "2026-10-31", t, 100, 0))
		wantProblem(t, "report before 1 November", err, domain.ProblemBeforeStartMonth)
		_, err = s.SaveReport(ctx, s.actor(s.olga), day(s.olga, "2026-11-21", t, 100, 0))
		wantProblem(t, "report tomorrow", err, domain.ProblemFutureDate)

		_, err = s.RecordMovement(ctx, s.admin, store.NewMovement{Type: domain.Opening, Amount: 1000, From: domain.Salon, To: domain.Account(domain.KassaCash), Date: date(t, "2026-10-31")})
		wantProblem(t, "opening before go-live", err, domain.ProblemBeforeStartMonth)
		_, err = s.RecordMovement(ctx, s.admin, store.NewMovement{Type: domain.Opening, Amount: 1000, From: domain.Salon, To: domain.Account(domain.KassaCash), Date: date(t, "2026-11-02")})
		wantProblem(t, "opening after the first day", err, store.ProblemOpeningDate)
		_, err = s.RecordMovement(ctx, s.admin, store.NewMovement{Type: domain.Opening, Amount: 1000, From: domain.Salon, To: domain.Account(domain.KassaCash), Date: date(t, "2026-11-01")})
		must(t, err)
		err = s.SetTerms(ctx, s.admin, s.olga, month(t, "2026-10"), domain.Terms{SelfPct: 7000}, "")
		wantProblem(t, "terms before go-live", err, domain.ProblemBeforeStartMonth)
	})
}

func TestTheOctoberTrialStartsAMonthEarlier(t *testing.T) {
	eachStore(t, func(t *testing.T, open func(opts ...store.Option) *store.Store) {
		clock := time.Date(2026, 10, 9, 9, 0, 0, 0, time.UTC)
		s := open(store.WithClock(func() time.Time { return clock }), store.WithStartMonth(month(t, "2026-10")))
		admin, err := s.SetUp(ctx, defaultSetup())
		must(t, err)
		must(t, s.SetTerms(ctx, admin.Actor(), admin.ID, month(t, "2026-10"), domain.Terms{SelfPct: 5000}, ""))
		_, err = s.SaveReport(ctx, admin.Actor(), day(admin.ID, "2026-10-08", t, 100, 0))
		must(t, err)
	})
}

func TestOpeningBalancesLoadWholeOrNotAtAll(t *testing.T) {
	eachSalon(t, func(t *testing.T, s *salon) {
		nov1 := date(t, "2026-11-01")
		opening := func(to domain.Party, amount domain.Cents) store.NewMovement {
			return store.NewMovement{Type: domain.Opening, Amount: amount, From: domain.Salon, To: to, Date: nov1, Note: "Начальный остаток"}
		}
		good := []store.NewMovement{opening(domain.Account(domain.KassaCash), 41250), opening(domain.Person(s.olga), -2613)}

		_, err := s.RecordOpenings(ctx, s.admin, append(good, opening(domain.Person("nobody"), 100)))
		wantProblem(t, "an unknown person in the file", err, store.ProblemUnknownParty)
		rows, err := s.Ledger(ctx, month(t, "2026-11"))
		must(t, err)
		if len(rows) != 0 {
			t.Fatalf("a refused file left %d rows", len(rows))
		}
		_, err = s.RecordOpenings(ctx, s.actor(s.olga), good)
		wantErr(t, "staff", err, store.ErrForbidden)
		_, err = s.RecordOpenings(ctx, s.admin, []store.NewMovement{{Type: domain.Deposit, Amount: 100, From: domain.Person(s.olga), To: domain.Account(domain.KassaCash), Date: nov1}})
		wantErr(t, "not an opening", err, store.ErrInvalid)

		entries, err := s.RecordOpenings(ctx, s.admin, good)
		must(t, err)
		b, err := s.Balances(ctx)
		must(t, err)
		if len(entries) != 2 || b.Accounts[domain.KassaCash] != 41250 || b.Persons[s.olga] != -2613 {
			t.Fatalf("entries %d, balances %+v", len(entries), b)
		}
		if got := s.actions(t, entries[0].ID); !reflect.DeepEqual(got, []string{store.ActionCreate}) {
			t.Errorf("audit = %v", got)
		}
	})
}

func TestEachPartyHasOneOpeningBalance(t *testing.T) {
	eachSalon(t, func(t *testing.T, s *salon) {
		nov1 := date(t, "2026-11-01")
		opening := func(to domain.Party, amount domain.Cents) store.NewMovement {
			return store.NewMovement{Type: domain.Opening, Amount: amount, From: domain.Salon, To: to, Date: nov1, Note: "Начальный остаток"}
		}
		cash := opening(domain.Account(domain.KassaCash), 30000)

		_, err := s.RecordOpenings(ctx, s.admin, []store.NewMovement{cash, cash})
		wantProblem(t, "the same party twice in one file", err, store.ProblemOpeningExists)

		// Two imports of the same file at once: one loads, the other is refused.
		var wg sync.WaitGroup
		errs := make([]error, 2)
		for i := range errs {
			wg.Go(func() { _, errs[i] = s.RecordOpenings(ctx, s.admin, []store.NewMovement{cash}) })
		}
		wg.Wait()
		if (errs[0] == nil) == (errs[1] == nil) {
			t.Fatalf("concurrent imports: errs = %v, want exactly one refused", errs)
		}
		for _, err := range errs {
			if err != nil {
				wantProblem(t, "the concurrent second import", err, store.ProblemOpeningExists)
			}
		}

		_, err = s.RecordOpenings(ctx, s.admin, []store.NewMovement{cash})
		wantProblem(t, "a second import", err, store.ProblemOpeningExists)
		_, err = s.RecordMovement(ctx, s.admin, cash)
		wantProblem(t, "a second opening by hand", err, store.ProblemOpeningExists)
		b, err := s.Balances(ctx)
		must(t, err)
		if got := b.Accounts[domain.KassaCash]; got != 30000 {
			t.Fatalf("kassa cash = %d, want 30000 (one opening)", got)
		}

		// A wrong opening is voided, then the right one goes in.
		rows, err := s.Ledger(ctx, month(t, "2026-11"))
		must(t, err)
		_, err = s.VoidMovement(ctx, s.admin, rows[0].ID, rows[0].Version, "не та сумма")
		must(t, err)
		_, err = s.RecordOpenings(ctx, s.admin, []store.NewMovement{opening(domain.Account(domain.KassaCash), 31000)})
		must(t, err)
		b, err = s.Balances(ctx)
		must(t, err)
		if got := b.Accounts[domain.KassaCash]; got != 31000 {
			t.Fatalf("kassa cash after the correction = %d, want 31000", got)
		}
	})
}

func TestOnlyTheAdminAndOwnersRecordMovements(t *testing.T) {
	eachSalon(t, func(t *testing.T, s *salon) {
		nov12 := date(t, "2026-11-12")
		dep, err := s.RecordMovement(ctx, s.actor(s.olga), store.NewMovement{Type: domain.Deposit, Amount: 5000, From: domain.Person(s.olga), To: domain.Account(domain.KassaCash), Date: nov12})
		must(t, err)
		if dep.Status != domain.Pending || dep.PersonID != s.olga {
			t.Errorf("staff deposit = %+v", dep)
		}
		_, err = s.RecordMovement(ctx, s.actor(s.olga), store.NewMovement{Type: domain.Deposit, Amount: 5000, From: domain.Person(s.maria), To: domain.Account(domain.KassaCash), Date: nov12})
		wantErr(t, "deposit for someone else", err, store.ErrForbidden)
		_, err = s.RecordMovement(ctx, s.actor(s.olga), store.NewMovement{Type: domain.Payout, Amount: 5000, From: domain.Account(domain.KassaCash), To: domain.Person(s.olga), Date: nov12})
		wantErr(t, "staff payout", err, store.ErrForbidden)
		_, err = s.RecordMovement(ctx, s.admin, store.NewMovement{Type: domain.Salary, Amount: 5000, From: domain.Salon, To: domain.Person(s.olga), Date: nov12})
		wantErr(t, "manual salary accrual", err, store.ErrForbidden)
		_, err = s.RecordMovement(ctx, s.admin, store.NewMovement{Type: domain.Expense, Amount: 5000, From: domain.Account(domain.KassaCash), To: domain.Outside, ExpenseCategory: "services", Date: nov12, Note: "не та категория"})
		wantProblem(t, "expense with a revenue category", err, store.ProblemUnknownParty)
		_, err = s.RecordMovement(ctx, s.admin, store.NewMovement{Type: domain.Payout, Amount: 5000, From: domain.Account("nope"), To: domain.Person(s.olga), Date: nov12})
		wantProblem(t, "unknown account", err, store.ProblemUnknownParty)
		_, err = s.RecordMovement(ctx, s.admin, store.NewMovement{Type: domain.Adjustment, Amount: 5000, From: domain.Outside, To: domain.Account(domain.KassaCash), Date: nov12})
		wantProblem(t, "adjustment without a note", err, store.ProblemNoteRequired)
		_, err = s.RecordMovement(ctx, s.actor(s.olga), store.NewMovement{Type: domain.Expense, Amount: 5000, From: domain.Person(s.olga), To: domain.Outside, ExpenseCategory: "materials", Date: nov12, Note: " "})
		wantProblem(t, "expense without a note", err, store.ProblemNoteRequired)

		_, err = s.ConfirmMovement(ctx, s.actor(s.olga), dep.ID, dep.Version)
		wantErr(t, "staff confirms", err, store.ErrForbidden)
		_, err = s.ConfirmMovement(ctx, s.admin, dep.ID, dep.Version+1)
		wantErr(t, "stale confirm", err, store.ErrVersionConflict)
		ok, err := s.ConfirmMovement(ctx, s.admin, dep.ID, dep.Version)
		must(t, err)
		if ok.Status != domain.Confirmed || ok.ConfirmedBy != s.anna || ok.Version != 2 {
			t.Errorf("confirmed = %+v", ok)
		}
		_, err = s.VoidMovement(ctx, s.actor(s.olga), ok.ID, ok.Version, "ошибка")
		wantErr(t, "author voids a confirmed row", err, store.ErrForbidden)
		_, err = s.ConfirmMovement(ctx, s.admin, ok.ID, ok.Version)
		wantProblem(t, "confirm twice", err, store.ProblemNotPending)
		if got := s.actions(t, dep.ID); !reflect.DeepEqual(got, []string{store.ActionCreate, store.ActionConfirm}) {
			t.Errorf("audit = %v", got)
		}
	})
}

func TestAVoidedRowStaysButNoLongerCounts(t *testing.T) {
	eachSalon(t, func(t *testing.T, s *salon) {
		exp, err := s.RecordMovement(ctx, s.actor(s.olga), store.NewMovement{Type: domain.Expense, Amount: 2900, From: domain.Person(s.olga), To: domain.Outside, ExpenseCategory: "materials", Date: date(t, "2026-11-12"), Note: "краска"})
		must(t, err)
		_, err = s.VoidMovement(ctx, s.actor(s.olga), exp.ID, exp.Version, "  ")
		wantProblem(t, "void without a reason", err, store.ProblemNoteRequired)
		v, err := s.VoidMovement(ctx, s.actor(s.olga), exp.ID, exp.Version, "дубль")
		must(t, err)
		if v.Status != domain.Voided || v.VoidReason != "дубль" {
			t.Errorf("voided = %+v", v)
		}
		_, err = s.VoidMovement(ctx, s.admin, v.ID, v.Version, "ещё раз")
		wantProblem(t, "void twice", err, store.ProblemAlreadyVoided)

		pay, err := s.RecordMovement(ctx, s.admin, store.NewMovement{Type: domain.Payout, Amount: 1000, From: domain.Account(domain.KassaCash), To: domain.Person(s.olga), Date: date(t, "2026-11-13")})
		must(t, err)
		_, err = s.VoidMovement(ctx, s.admin, pay.ID, pay.Version, "не выплачено")
		must(t, err)

		rows, err := s.Ledger(ctx, month(t, "2026-11"))
		must(t, err)
		bal, err := s.Balances(ctx)
		must(t, err)
		if len(rows) != 2 || bal.Persons[s.olga] != 0 || bal.Accounts[domain.KassaCash] != 0 {
			t.Errorf("ledger has %d rows; balances %+v", len(rows), bal)
		}
		pending, err := s.PendingMovements(ctx)
		must(t, err)
		if len(pending) != 0 {
			t.Errorf("pending = %+v", pending)
		}
	})
}

func TestAClosedMonthRejectsWritesFromEveryoneIncludingTheAdmin(t *testing.T) {
	eachSalon(t, func(t *testing.T, s *salon) {
		saved, err := s.SaveReport(ctx, s.actor(s.olga), day(s.olga, "2026-11-10", t, 0, 20000))
		must(t, err)
		dep, err := s.RecordMovement(ctx, s.admin, store.NewMovement{Type: domain.Deposit, Amount: 1000, From: domain.Person(s.olga), To: domain.Account(domain.KassaCash), Date: date(t, "2026-11-12")})
		must(t, err)
		s.at("2026-12-02")
		_, err = s.CloseMonth(ctx, s.admin, month(t, "2026-11"))
		must(t, err)

		e := day(s.olga, "2026-11-10", t, 0, 20000)
		e.Version = saved.Version
		_, err = s.SaveReport(ctx, s.actor(s.olga), e)
		wantErr(t, "staff edit", err, store.ErrMonthClosed)
		_, err = s.SaveReport(ctx, s.admin, e)
		wantErr(t, "admin edit", err, store.ErrMonthClosed)
		_, err = s.SaveReport(ctx, s.admin, day(s.maria, "2026-11-11", t, 100, 0))
		wantErr(t, "admin adds a report", err, store.ErrMonthClosed)
		_, err = s.RecordMovement(ctx, s.admin, store.NewMovement{Type: domain.Payout, Amount: 1000, From: domain.Account(domain.KassaCash), To: domain.Person(s.olga), Date: date(t, "2026-11-30")})
		wantErr(t, "admin movement", err, store.ErrMonthClosed)
		_, err = s.VoidMovement(ctx, s.admin, dep.ID, dep.Version, "ошибка")
		wantErr(t, "admin void", err, store.ErrMonthClosed)
		err = s.SetTerms(ctx, s.admin, s.olga, month(t, "2026-11"), domain.Terms{SelfPct: 6000}, "")
		wantErr(t, "terms", err, store.ErrMonthClosed)
		_, err = s.CloseMonth(ctx, s.admin, month(t, "2026-11"))
		wantErr(t, "close twice", err, store.ErrMonthClosed)

		_, err = s.SaveReport(ctx, s.actor(s.olga), day(s.olga, "2026-12-01", t, 100, 0))
		must(t, err) // December stays open
	})
}

func TestMonthsCloseInOrderAndOnlyTheNewestReopens(t *testing.T) {
	eachSalon(t, func(t *testing.T, s *salon) {
		nov, dec, jan := month(t, "2026-11"), month(t, "2026-12"), month(t, "2027-01")
		_, err := s.CloseMonth(ctx, s.actor(s.olga), nov)
		wantErr(t, "staff close", err, store.ErrForbidden)
		_, err = s.CloseMonth(ctx, s.admin, dec)
		wantProblem(t, "close December first", err, store.ProblemMonthNotOver)
		_, err = s.CloseMonth(ctx, s.admin, nov)
		wantProblem(t, "close November on the 20th", err, store.ProblemMonthNotOver)

		s.at("2027-01-05")
		_, err = s.CloseMonth(ctx, s.admin, dec)
		wantProblem(t, "close December before November", err, domain.ProblemPreviousMonthOpen)
		_, err = s.CloseMonth(ctx, s.admin, jan)
		wantProblem(t, "close the running month", err, store.ProblemMonthNotOver)
		_, err = s.ReopenMonth(ctx, s.admin, nov)
		wantProblem(t, "reopen an open month", err, domain.ProblemNotNewestClosed)

		_, err = s.CloseMonth(ctx, s.admin, nov)
		must(t, err)
		_, err = s.CloseMonth(ctx, s.admin, dec)
		must(t, err)
		_, err = s.ReopenMonth(ctx, s.admin, nov)
		wantProblem(t, "reopen November while December is closed", err, domain.ProblemNotNewestClosed)
		_, err = s.ReopenMonth(ctx, s.actor(s.olga), dec)
		wantErr(t, "staff reopen", err, store.ErrForbidden)
		st, err := s.ReopenMonth(ctx, s.admin, dec)
		must(t, err)
		if st.Status != domain.MonthOpen || st.Snapshot != nil {
			t.Errorf("reopened = %+v", st)
		}
		closed, err := s.ClosedMonths(ctx)
		must(t, err)
		if len(closed) != 1 || closed[0].Month != nov {
			t.Errorf("closed months = %+v", closed)
		}
		if got := s.actions(t, "2026-12"); !reflect.DeepEqual(got, []string{store.ActionClose, store.ActionReopen}) {
			t.Errorf("December audit = %v", got)
		}
	})
}

func TestAMonthWithMoneyForSomeoneWithoutTermsDoesNotClose(t *testing.T) {
	eachSalon(t, func(t *testing.T, s *salon) {
		u, err := s.CreateUser(ctx, s.admin, store.NewUser{Username: "new", DisplayName: "Новая", Role: store.RoleEmployee, PasswordHash: "x"})
		must(t, err)
		_, err = s.RecordMovement(ctx, s.admin, store.NewMovement{Type: domain.Deposit, Amount: 5000, From: domain.Person(u.ID), To: domain.Account(domain.KassaCash), Date: date(t, "2026-11-12")})
		must(t, err)
		s.at("2026-12-01")
		_, err = s.CloseMonth(ctx, s.admin, month(t, "2026-11"))
		wantProblem(t, "close with money for someone without terms", err, domain.ProblemMissingTerms)
		s.at("2026-11-30")
		must(t, s.SetTerms(ctx, s.admin, u.ID, month(t, "2026-11"), domain.Terms{}, "без процента"))
		s.at("2026-12-01")
		st, err := s.CloseMonth(ctx, s.admin, month(t, "2026-11"))
		must(t, err)
		if p, ok := st.Snapshot.Result.People[u.ID]; !ok || p.Closing != 5000 {
			t.Errorf("settlement line = %+v (present %v), want closing 5000", p, ok)
		}
	})
}

func TestPendingMovementsBlockClosing(t *testing.T) {
	eachSalon(t, func(t *testing.T, s *salon) {
		dep, err := s.RecordMovement(ctx, s.actor(s.olga), store.NewMovement{Type: domain.Deposit, Amount: 5000, From: domain.Person(s.olga), To: domain.Account(domain.KassaCash), Date: date(t, "2026-11-12")})
		must(t, err)
		s.at("2026-12-01")
		_, err = s.CloseMonth(ctx, s.admin, month(t, "2026-11"))
		wantProblem(t, "close with a pending deposit", err, domain.ProblemPendingMovements)
		_, err = s.ConfirmMovement(ctx, s.admin, dep.ID, dep.Version)
		must(t, err)
		_, err = s.CloseMonth(ctx, s.admin, month(t, "2026-11"))
		must(t, err)
	})
}

// november fills November with opening balances, reports, an expense and a
// payout, so a close has something to settle.
func (s *salon) november(t *testing.T) {
	t.Helper()
	first := date(t, "2026-11-01")
	for _, n := range []store.NewMovement{
		{Type: domain.Opening, Amount: 30000, From: domain.Salon, To: domain.Account(domain.KassaCash), Date: first},
		{Type: domain.Opening, Amount: 120000, From: domain.Salon, To: domain.Account(domain.KassaCard), Date: first},
		{Type: domain.Opening, Amount: 50000, From: domain.Salon, To: domain.Certificates, Date: first},
		{Type: domain.Opening, Amount: -250, From: domain.Salon, To: domain.Person(s.vera), Date: first},
		{Type: domain.Expense, Amount: 150000, From: domain.Account(domain.KassaCard), To: domain.Outside, ExpenseCategory: "rent", Date: date(t, "2026-11-03"), Note: "аренда за ноябрь"},
		{Type: domain.Payout, Amount: 10000, From: domain.Account(domain.KassaCash), To: domain.Person(s.maria), Date: date(t, "2026-11-15")},
	} {
		_, err := s.RecordMovement(ctx, s.admin, n)
		must(t, err)
	}
	for _, e := range []store.ReportEntry{
		day(s.olga, "2026-11-05", t, 200000, 50000),
		day(s.maria, "2026-11-05", t, 300000, 0),
		day(s.vera, "2026-11-06", t, 0, 40000),
		{Person: s.anna, Date: date(t, "2026-11-08"),
			Revenue:  map[string]domain.Cents{"services": 60000, "creams": 5000, "certificates": 10000},
			Payments: map[string]domain.Cents{"card": 60000, "cash": 15000}},
		{Person: s.olga, Date: date(t, "2026-11-09"),
			Revenue:  map[string]domain.Cents{"services": 10000},
			Payments: map[string]domain.Cents{"paid_by_certificate": 10000}},
	} {
		_, err := s.SaveReport(ctx, s.admin, e)
		must(t, err)
	}
}

func TestCloseReopenCloseGivesTheSameSnapshot(t *testing.T) {
	eachSalon(t, func(t *testing.T, s *salon) {
		s.november(t)
		nov, dec := month(t, "2026-11"), month(t, "2026-12")
		s.at("2026-12-01")
		first, err := s.CloseMonth(ctx, s.admin, nov)
		must(t, err)
		res := first.Snapshot.Result
		if res.Salaries != 100000 || res.Expenses != 150000 || res.Profit != res.SalonIncome-250000 {
			t.Fatalf("result = %+v", res)
		}
		var dividends domain.Cents
		for _, p := range res.People {
			dividends += p.Dividend
		}
		if dividends != res.DividendPool || res.People[s.vera].Dividend != 0 {
			t.Errorf("dividends %d of pool %d, Vera %d", dividends, res.DividendPool, res.People[s.vera].Dividend)
		}

		// New terms from December must not change November when it is redone.
		must(t, s.SetTerms(ctx, s.admin, s.anna, dec, domain.Terms{SelfPct: 5000, SalaryCents: 120000, DividendWeight: 1}, "повышение"))
		_, err = s.ReopenMonth(ctx, s.admin, nov)
		must(t, err)
		rows, err := s.Ledger(ctx, nov)
		must(t, err)
		for _, e := range rows {
			if (e.Type == domain.Salary || e.Type == domain.Dividend) && (e.Status != domain.Voided || e.VoidReason != store.ReasonMonthReopened) {
				t.Errorf("accrual after reopen = %+v", e)
			}
		}
		second, err := s.CloseMonth(ctx, s.admin, nov)
		must(t, err)
		if !reflect.DeepEqual(first.Snapshot, second.Snapshot) {
			t.Fatalf("snapshots differ:\nfirst  %+v\nsecond %+v", first.Snapshot, second.Snapshot)
		}
		read, err := s.Month(ctx, nov)
		must(t, err)
		if !reflect.DeepEqual(read.Snapshot, first.Snapshot) {
			t.Fatalf("stored snapshot differs:\n%+v\nwant\n%+v", read.Snapshot, first.Snapshot)
		}
		if got := s.actions(t, "2026-11"); !reflect.DeepEqual(got, []string{store.ActionClose, store.ActionReopen, store.ActionClose}) {
			t.Errorf("November audit = %v", got)
		}
	})
}

func TestAMonthAboveTheThresholdAccruesTheSelfBonusAtClose(t *testing.T) {
	eachSalon(t, func(t *testing.T, s *salon) {
		nov := month(t, "2026-11")
		wantErr(t, "threshold without a rate", s.SetTerms(ctx, s.admin, s.maria, nov, domain.Terms{SelfPct: 7000, HighSelfFromCents: 250000}, ""), store.ErrInvalid)
		// Maria: 70%, or 80% for the whole month above €2 500 (she makes €3 000).
		tiered := domain.Terms{SelfPct: 7000, DividendWeight: 1, HighSelfFromCents: 250000, HighSelfPct: 8000}
		must(t, s.SetTerms(ctx, s.admin, s.maria, nov, tiered, "80% свыше €2 500"))
		s.november(t)
		s.at("2026-12-01")
		closed, err := s.CloseMonth(ctx, s.admin, nov)
		must(t, err)
		snap := closed.Snapshot
		if got := snap.Result.People[s.maria]; got.SelfBase != 300000 || got.SelfBonus != 30000 || snap.Result.SelfBonuses != 30000 || snap.Terms[s.maria] != tiered {
			t.Fatalf("maria %+v, bonuses %d, terms %+v", got, snap.Result.SelfBonuses, snap.Terms[s.maria])
		}
		read, err := s.Month(ctx, nov)
		must(t, err)
		if !reflect.DeepEqual(read.Snapshot, snap) {
			t.Fatalf("stored snapshot differs:\n%+v\nwant\n%+v", read.Snapshot, snap)
		}
		bonus := func() []store.Entry {
			rows, err := s.Ledger(ctx, nov)
			must(t, err)
			var out []store.Entry
			for _, e := range rows {
				if e.Type == domain.SelfBonus {
					out = append(out, e)
				}
			}
			return out
		}
		if rows := bonus(); len(rows) != 1 || rows[0].Amount != 30000 || rows[0].To != domain.Person(s.maria) || rows[0].Status != domain.Confirmed {
			t.Fatalf("self bonus rows = %+v", rows)
		}
		_, err = s.RecordMovement(ctx, s.admin, store.NewMovement{Type: domain.SelfBonus, Amount: 100, From: domain.Salon, To: domain.Person(s.olga), Date: date(t, "2026-12-01")})
		wantErr(t, "a bonus by hand", err, store.ErrForbidden)
		row := bonus()[0]
		_, err = s.VoidMovement(ctx, s.admin, row.ID, row.Version, "вручную")
		wantErr(t, "voiding a bonus by hand", err, store.ErrForbidden)

		// A higher rate from December must not change November when it is redone.
		must(t, s.SetTerms(ctx, s.admin, s.maria, month(t, "2026-12"), domain.Terms{SelfPct: 7000, DividendWeight: 1, HighSelfFromCents: 100000, HighSelfPct: 9000}, ""))
		_, err = s.ReopenMonth(ctx, s.admin, nov)
		must(t, err)
		rows := bonus()
		if len(rows) != 1 || rows[0].Status != domain.Voided || rows[0].VoidReason != store.ReasonMonthReopened {
			t.Fatalf("self bonus after reopen = %+v", rows)
		}
		again, err := s.CloseMonth(ctx, s.admin, nov)
		must(t, err)
		if !reflect.DeepEqual(again.Snapshot, snap) {
			t.Fatalf("closed again:\n%+v\nwant\n%+v", again.Snapshot, snap)
		}
	})
}

func TestBalancesAreTheNewestSnapshotPlusTheOpenMonths(t *testing.T) {
	eachSalon(t, func(t *testing.T, s *salon) {
		s.november(t)
		s.at("2026-12-02")
		// A December report made before November closes must not enter
		// November's snapshot.
		dec, err := s.SaveReport(ctx, s.actor(s.olga), day(s.olga, "2026-12-01", t, 0, 10000))
		must(t, err)
		before, err := s.Balances(ctx)
		must(t, err)
		nov, err := s.CloseMonth(ctx, s.admin, month(t, "2026-11"))
		must(t, err)
		snap := nov.Snapshot.Balances
		if snap.Accounts[domain.KassaCash]+dec.Report.CashToKassa != before.Accounts[domain.KassaCash]+0 {
			t.Errorf("November cash %d + December %d vs before close %d", snap.Accounts[domain.KassaCash], dec.Report.CashToKassa, before.Accounts[domain.KassaCash])
		}
		if snap.CertificatesOutstanding != 50000+10000-10000 {
			t.Errorf("certificates outstanding = %d", snap.CertificatesOutstanding)
		}
		after, err := s.Balances(ctx)
		must(t, err)
		accrued := nov.Snapshot.Result.People[s.olga].Dividend
		if after.Persons[s.olga] != before.Persons[s.olga]+accrued {
			t.Errorf("Olga after close %d, want %d + dividend %d", after.Persons[s.olga], before.Persons[s.olga], accrued)
		}
		if after.Accounts[domain.KassaCash] != before.Accounts[domain.KassaCash] || after.Persons[s.vera] != snap.Persons[s.vera] {
			t.Errorf("after %+v, before %+v", after, before)
		}
	})
}

func TestARecountBooksTheDifferenceWithANote(t *testing.T) {
	eachSalon(t, func(t *testing.T, s *salon) {
		s.november(t)
		bal, err := s.Balances(ctx)
		must(t, err)
		want := bal.Accounts[domain.KassaCash]

		same, err := s.Recount(ctx, s.admin, domain.KassaCash, want, "")
		must(t, err)
		if same.Adjustment != nil || same.Expected != want {
			t.Errorf("matching count = %+v", same)
		}
		_, err = s.Recount(ctx, s.admin, domain.KassaCash, want-500, "")
		wantProblem(t, "difference without a note", err, store.ProblemNoteRequired)
		_, err = s.Recount(ctx, s.actor(s.olga), domain.KassaCash, want, "")
		wantErr(t, "staff recount", err, store.ErrForbidden)
		short, err := s.Recount(ctx, s.admin, domain.KassaCash, want-500, "не хватает")
		must(t, err)
		if a := short.Adjustment; a == nil || a.Amount != 500 || a.From != domain.Account(domain.KassaCash) || a.To != domain.Outside {
			t.Fatalf("adjustment = %+v", short.Adjustment)
		}
		bal, err = s.Balances(ctx)
		must(t, err)
		if bal.Accounts[domain.KassaCash] != want-500 {
			t.Errorf("cash after recount = %d, want %d", bal.Accounts[domain.KassaCash], want-500)
		}
		accs, err := s.Accounts(ctx)
		must(t, err)
		if !accs[0].LastRecountAt.Equal(s.clock.Truncate(time.Microsecond)) || accs[0].LastRecountBy != s.anna {
			t.Errorf("account = %+v", accs[0])
		}
		if got := s.actions(t, domain.KassaCash); !reflect.DeepEqual(got, []string{store.ActionCreate, store.ActionRecount, store.ActionRecount}) {
			t.Errorf("account audit = %v", got)
		}
	})
}

func TestNothingIsRecountedBeforeGoLive(t *testing.T) {
	eachSalon(t, func(t *testing.T, s *salon) {
		s.at("2026-10-08")
		_, err := s.Recount(ctx, s.admin, domain.KassaCash, 5000, "до запуска")
		wantProblem(t, "recount in October", err, domain.ProblemBeforeStartMonth)
	})
}

func TestCategoryUseIsCountedAndAudited(t *testing.T) {
	eachSalon(t, func(t *testing.T, s *salon) {
		_, err := s.RecordMovement(ctx, s.admin, store.NewMovement{Type: domain.Expense, Amount: 1000, From: domain.Account(domain.KassaCash), To: domain.Outside, ExpenseCategory: "rent", Date: date(t, "2026-11-03"), Note: "аренда за ноябрь"})
		must(t, err)
		_, err = s.SaveReport(ctx, s.actor(s.olga), day(s.olga, "2026-11-10", t, 100, 0))
		must(t, err)
		cats, err := s.Categories(ctx)
		must(t, err)
		for _, c := range cats {
			if c.ID == "rent" && c.UsedCount != 1 {
				t.Errorf("rent used %d times, want 1", c.UsedCount)
			}
		}
		for _, id := range []string{"rent", "services", "card"} {
			if got := s.actions(t, id); !reflect.DeepEqual(got, []string{store.ActionCreate, store.ActionUpdate}) {
				t.Errorf("%s audit = %v", id, got)
			}
		}
	})
}

func TestIDsFirestoreReservesAreRefusedEverywhere(t *testing.T) {
	eachSalon(t, func(t *testing.T, s *salon) {
		_, err := s.CreateCategory(ctx, s.admin, store.Category{ID: "__x__", Name: "X", Kind: store.KindExpense})
		wantErr(t, "reserved category ID", err, store.ErrInvalid)
		_, err = s.CreateUser(ctx, s.admin, store.NewUser{Username: "__admin__", DisplayName: "X", Role: store.RoleEmployee, PasswordHash: "x"})
		wantErr(t, "reserved username", err, store.ErrInvalid)
	})
}

func TestImportedHistoryComesBeforeTheStartMonth(t *testing.T) {
	eachSalon(t, func(t *testing.T, s *salon) {
		sep := domain.ImportedMonth{RevenueTotal: 1534100, Profit: 178885, RevenueByPerson: map[string]domain.Cents{s.olga: 568000}}
		err := s.SaveImportedMonth(ctx, s.admin, month(t, "2026-11"), sep, "sheet")
		wantProblem(t, "import into the app's months", err, store.ProblemNotHistory)
		err = s.SaveImportedMonth(ctx, s.actor(s.olga), month(t, "2026-09"), sep, "sheet")
		wantErr(t, "staff import", err, store.ErrForbidden)
		must(t, s.SaveImportedMonth(ctx, s.admin, month(t, "2026-09"), sep, "sheet"))
		got, err := s.ImportedMonths(ctx)
		must(t, err)
		if g := got[month(t, "2026-09")]; g.RevenueTotal != 1534100 || g.RevenueByPerson[s.olga] != 568000 || len(got) != 1 {
			t.Errorf("imported = %+v", got)
		}
		if got := s.actions(t, "2026-09"); !reflect.DeepEqual(got, []string{store.ActionUpdate}) {
			t.Errorf("audit = %v", got)
		}
	})
}

func TestImportedHistoryLoadsWholeOrNotAtAll(t *testing.T) {
	eachSalon(t, func(t *testing.T, s *salon) {
		aug := domain.ImportedMonth{RevenueTotal: 1368000, Profit: 153700}
		sep := domain.ImportedMonth{RevenueTotal: 1529500, Profit: 99790}
		err := s.SaveImportedMonths(ctx, s.admin, map[domain.Month]domain.ImportedMonth{
			month(t, "2026-08"): aug, month(t, "2026-11"): sep,
		}, "sheet")
		wantProblem(t, "a file reaching into the app's months", err, store.ProblemNotHistory)
		if got, err := s.ImportedMonths(ctx); err != nil || len(got) != 0 {
			t.Fatalf("a refused file wrote %v (err %v)", got, err)
		}

		must(t, s.SaveImportedMonths(ctx, s.admin, map[domain.Month]domain.ImportedMonth{
			month(t, "2026-08"): aug, month(t, "2026-09"): sep,
		}, "sheet"))
		got, err := s.ImportedMonths(ctx)
		must(t, err)
		if got[month(t, "2026-08")].Profit != 153700 || got[month(t, "2026-09")].RevenueTotal != 1529500 || len(got) != 2 {
			t.Errorf("imported = %+v", got)
		}
		for _, m := range []string{"2026-08", "2026-09"} {
			if got := s.actions(t, m); !reflect.DeepEqual(got, []string{store.ActionUpdate}) {
				t.Errorf("audit %s = %v", m, got)
			}
		}
	})
}

func TestTermsStartOnlyInAMonthWithoutReports(t *testing.T) {
	eachSalon(t, func(t *testing.T, s *salon) {
		_, err := s.SaveReport(ctx, s.actor(s.olga), day(s.olga, "2026-11-10", t, 100, 0))
		must(t, err)
		err = s.SetTerms(ctx, s.admin, s.olga, month(t, "2026-11"), domain.Terms{SelfPct: 6000}, "")
		wantProblem(t, "terms in a month with reports", err, domain.ProblemTermsMonthHasReports)
		err = s.SetTerms(ctx, s.actor(s.olga), s.olga, month(t, "2026-12"), domain.Terms{SelfPct: 9000}, "")
		wantErr(t, "staff sets terms", err, store.ErrForbidden)
		must(t, s.SetTerms(ctx, s.admin, s.olga, month(t, "2026-12"), domain.Terms{SelfPct: 6000}, "с декабря"))

		// Vera has no November reports but has December ones: terms from
		// November would change December's rate halfway through.
		s.at("2026-12-04")
		_, err = s.SaveReport(ctx, s.actor(s.vera), day(s.vera, "2026-12-03", t, 100, 0))
		must(t, err)
		err = s.SetTerms(ctx, s.admin, s.vera, month(t, "2026-11"), domain.Terms{SelfPct: 7000}, "")
		wantProblem(t, "terms covering a later month with reports", err, domain.ProblemTermsMonthHasReports)
		// Olga's December terms shield December from a November change.
		_, err = s.SaveReport(ctx, s.actor(s.olga), day(s.olga, "2026-12-03", t, 100, 0))
		must(t, err)
		_, err = s.SaveReport(ctx, s.admin, day(s.maria, "2026-12-03", t, 100, 0))
		must(t, err)
		must(t, s.SetTerms(ctx, s.admin, s.maria, month(t, "2027-01"), domain.Terms{SelfPct: 6500}, ""))

		terms, err := s.Terms(ctx, s.olga)
		must(t, err)
		if len(terms) != 2 || terms[1].From != month(t, "2026-12") || terms[1].Terms.SelfPct != 6000 || terms[1].Note != "с декабря" {
			t.Errorf("terms = %+v", terms)
		}
	})
}

func TestReadsByDayMonthAndPerson(t *testing.T) {
	eachSalon(t, func(t *testing.T, s *salon) {
		s.november(t)
		on, err := s.ReportsOn(ctx, date(t, "2026-11-05"))
		must(t, err)
		in, err := s.ReportsIn(ctx, month(t, "2026-11"))
		must(t, err)
		olga, err := s.PersonReportsIn(ctx, s.olga, month(t, "2026-11"))
		must(t, err)
		if len(on) != 2 || len(in) != 5 || len(olga) != 2 || olga[0].Date.String() != "2026-11-05" {
			t.Errorf("on %d, in %d, olga %d", len(on), len(in), len(olga))
		}
		_, err = s.Report(ctx, s.olga, date(t, "2026-11-20"))
		wantErr(t, "missing report", err, store.ErrNotFound)
		must(t, s.Ping(ctx))
	})
}

func TestConcurrentSavesOfTheSameVersionLetOnlyOneWin(t *testing.T) {
	eachSalon(t, func(t *testing.T, s *salon) {
		saved, err := s.SaveReport(ctx, s.actor(s.olga), day(s.olga, "2026-11-10", t, 100, 0))
		must(t, err)
		const writers = 4
		errs := make(chan error, writers)
		for i := range writers {
			go func() {
				e := day(s.olga, "2026-11-10", t, 100*domain.Cents(i+2), 0)
				e.Version = saved.Version
				_, err := s.SaveReport(ctx, s.admin, e)
				errs <- err
			}()
		}
		won := 0
		for range writers {
			switch err := <-errs; {
			case err == nil:
				won++
			case !errors.Is(err, store.ErrVersionConflict):
				t.Errorf("unexpected error: %v", err)
			}
		}
		got, err := s.Report(ctx, s.olga, date(t, "2026-11-10"))
		must(t, err)
		if won != 1 || got.Version != 2 {
			t.Fatalf("%d writers won, version %d; want exactly one and version 2", won, got.Version)
		}
	})
}

func TestTheDefaultDatabaseIsRefused(t *testing.T) {
	for _, db := range []string{"", "(default)"} {
		if _, err := store.NewFirestore(ctx, "demo-kassa", db); err == nil {
			t.Errorf("NewFirestore(%q) accepted", db)
		}
	}
}

func TestFailedLoginsLockTheAccountForAWhile(t *testing.T) {
	eachSalon(t, func(t *testing.T, s *salon) {
		const limit, lockFor = 3, 15 * time.Minute
		for i := 1; i < limit; i++ {
			u, err := s.RecordLoginFailure(ctx, s.olga, limit, lockFor)
			must(t, err)
			if u.FailedLogins != i || !u.LockedUntil.IsZero() {
				t.Fatalf("after %d failures: %+v", i, u)
			}
		}
		u, err := s.RecordLoginFailure(ctx, s.olga, limit, lockFor)
		must(t, err)
		if want := s.clock.Add(lockFor).Truncate(time.Microsecond); !u.LockedUntil.Equal(want) || u.FailedLogins != 0 {
			t.Fatalf("locked = %+v, want until %v", u, want)
		}
		if got := s.actions(t, s.olga); !reflect.DeepEqual(got, []string{store.ActionCreate, store.ActionLock}) {
			t.Errorf("audit = %v; only the lock is logged", got)
		}
		_, err = s.RecordLoginFailure(ctx, s.olga, limit, lockFor)
		must(t, err)
		u, err = s.RecordLoginSuccess(ctx, s.olga)
		must(t, err)
		if u.FailedLogins != 0 || !u.LockedUntil.IsZero() {
			t.Errorf("after success: %+v", u)
		}
		_, err = s.RecordLoginFailure(ctx, "nobody", limit, lockFor)
		wantErr(t, "unknown user", err, store.ErrNotFound)
	})
}

func ptr[T any](v T) *T { return &v }

// The admin can rename a kassa account (only its display name); the change
// is audited and staff can't do it.
func TestTheAdminRenamesAKassaAccount(t *testing.T) {
	eachSalon(t, func(t *testing.T, s *salon) {
		_, err := s.RenameAccount(ctx, s.actor(s.olga), domain.KassaCard, "Касса — карта салона")
		wantErr(t, "staff rename", err, store.ErrForbidden)
		_, err = s.RenameAccount(ctx, s.admin, domain.KassaCard, "  ")
		wantErr(t, "blank name", err, store.ErrInvalid)
		_, err = s.RenameAccount(ctx, s.admin, "nope", "Касса")
		wantErr(t, "unknown account", err, store.ErrNotFound)

		a, err := s.RenameAccount(ctx, s.admin, domain.KassaCard, "  Касса — карта салона ")
		must(t, err)
		if a.Name != "Касса — карта салона" || a.Kind != store.AccountCard || !a.Active {
			t.Fatalf("renamed account = %+v", a)
		}
		accounts, err := s.Accounts(ctx)
		must(t, err)
		if accounts[1].ID != domain.KassaCard || accounts[1].Name != "Касса — карта салона" {
			t.Fatalf("accounts = %+v", accounts)
		}
		entries, err := s.Audit(ctx, domain.KassaCard)
		must(t, err)
		renamed := false
		for _, e := range entries {
			renamed = renamed || (e.Action == store.ActionUpdate && e.Entity == store.EntityAccount && strings.Contains(e.After, "Касса — карта салона"))
		}
		if !renamed {
			t.Fatalf("no audited rename in %+v", entries)
		}
	})
}
