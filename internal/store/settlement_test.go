package store_test

import (
	"errors"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/lieranderl/aestheticlab-income-app/internal/domain"
	"github.com/lieranderl/aestheticlab-income-app/internal/store"
)

func TestSettlementBoundsSelectedMonthAndRejectsStaleConfirmations(t *testing.T) {
	eachSalon(t, func(t *testing.T, s *salon) {
		nov := month(t, "2026-11")
		_, err := s.RecordMovement(ctx, s.admin, store.NewMovement{Type: domain.Opening, Amount: 12300, From: domain.Salon, To: domain.Person(s.olga), Date: nov.FirstDay()})
		must(t, err)
		_, err = s.RecordMovement(ctx, s.admin, store.NewMovement{Type: domain.Deposit, Amount: 5000, From: domain.Person(s.olga), To: domain.Account(domain.KassaCard), Date: date(t, "2026-11-12")})
		must(t, err)
		preview, err := s.Settlement(ctx, nov)
		must(t, err)
		if b := preview.Breakdowns[s.olga]; b.Opening != 12300 || b.Deposits != 5000 || b.Closing != 17300 {
			t.Fatalf("breakdown %+v", b)
		}
		s.at("2026-12-20")
		_, err = s.RecordMovement(ctx, s.admin, store.NewMovement{Type: domain.Deposit, Amount: 90000, From: domain.Person(s.olga), To: domain.Account(domain.KassaCard), Date: date(t, "2026-12-02")})
		must(t, err)
		selected, err := s.Settlement(ctx, nov)
		must(t, err)
		if selected.Result.People[s.olga].Closing != 17300 {
			t.Fatalf("later rows leaked: %+v", selected.Result.People[s.olga])
		}
		_, err = s.RecordMovement(ctx, s.admin, store.NewMovement{Type: domain.Deposit, Amount: 100, From: domain.Person(s.olga), To: domain.Account(domain.KassaCard), Date: date(t, "2026-11-13")})
		must(t, err)
		_, err = s.CloseMonthGuarded(ctx, s.admin, nov, selected.Revision)
		wantErr(t, "stale confirmation", err, store.ErrVersionConflict)
		fresh, err := s.Settlement(ctx, nov)
		must(t, err)
		closed, err := s.CloseMonthGuarded(ctx, s.admin, nov, fresh.Revision)
		must(t, err)
		if closed.Snapshot.Result.People[s.olga].Closing != 17400 {
			t.Fatal("wrong closing")
		}
		own, status, err := s.OwnMonthResult(ctx, s.olga, nov)
		must(t, err)
		if status != domain.MonthClosed || own.Closing != 17400 {
			t.Fatalf("own %+v %v", own, status)
		}
		_, err = s.CloseMonthGuarded(ctx, s.admin, nov, fresh.Revision)
		wantErr(t, "stale confirmation", err, store.ErrVersionConflict)
		frozen, err := s.Settlement(ctx, nov)
		must(t, err)
		_, err = s.ReopenMonthGuarded(ctx, s.admin, nov, frozen.Revision)
		must(t, err)
		reopened, err := s.Settlement(ctx, nov)
		must(t, err)
		_, err = s.CloseMonthGuarded(ctx, s.admin, nov, fresh.Revision)
		wantErr(t, "stale confirmation", err, store.ErrVersionConflict)
		again, err := s.CloseMonthGuarded(ctx, s.admin, nov, reopened.Revision)
		must(t, err)
		if !reflect.DeepEqual(closed.Snapshot, again.Snapshot) {
			t.Fatal("reclosing changed snapshot")
		}
		_, err = s.ReopenMonthGuarded(ctx, s.admin, nov, frozen.Revision)
		wantErr(t, "stale confirmation", err, store.ErrVersionConflict)
	})
}

func TestUnclosedEarlierMonthContributesRealizedOpeningOnly(t *testing.T) {
	eachSalon(t, func(t *testing.T, s *salon) {
		_, err := s.RecordMovement(ctx, s.admin, store.NewMovement{Type: domain.Deposit, Amount: 5000, From: domain.Person(s.anna), To: domain.Account(domain.KassaCard), Date: date(t, "2026-11-12")})
		must(t, err)
		s.at("2026-12-20")
		selected, err := s.Settlement(ctx, month(t, "2026-12"))
		must(t, err)
		if selected.Result.People[s.anna].Opening != 5000 {
			t.Fatalf("invented November salary: %+v", selected.Result.People[s.anna])
		}
		if selected.PreviousClosed {
			t.Fatal("unclosed previous month accepted")
		}
	})
}

func TestGuardedMonthOperationsPreserveAuthorizationAndEmptyRevisionSafety(t *testing.T) {
	eachSalon(t, func(t *testing.T, s *salon) {
		s.at("2026-12-20")
		p, err := s.Settlement(ctx, month(t, "2026-11"))
		must(t, err)
		_, err = s.CloseMonthGuarded(ctx, s.actor(s.olga), month(t, "2026-11"), p.Revision)
		if !errors.Is(err, store.ErrForbidden) {
			t.Fatal(err)
		}
		_, err = s.CloseMonthGuarded(ctx, s.admin, month(t, "2026-11"), "")
		wantErr(t, "stale confirmation", err, store.ErrVersionConflict)
	})
}

func TestSettlementPaymentUsesCurrentBalanceAndRejectsRepeatedSuggestion(t *testing.T) {
	eachSalon(t, func(t *testing.T, s *salon) {
		nov := month(t, "2026-11")
		_, err := s.RecordMovement(ctx, s.admin, store.NewMovement{Type: domain.Deposit, Amount: 5000, From: domain.Person(s.olga), To: domain.Account(domain.KassaCard), Date: date(t, "2026-11-12")})
		must(t, err)
		s.at("2026-12-20")
		closed, err := s.CloseMonth(ctx, s.admin, nov)
		must(t, err)
		suggestion, err := s.SettlementSuggestion(ctx, s.olga, nov)
		must(t, err)
		if suggestion.Balance != 5000 {
			t.Fatal(suggestion.Balance)
		}
		n := store.NewMovement{Type: domain.Payout, Amount: 2000, From: domain.Account(domain.KassaCard), To: domain.Person(s.olga), Date: s.Today()}
		_, err = s.RecordSettlementMovement(ctx, s.admin, nov, s.olga, suggestion.Revision, n)
		must(t, err)
		_, err = s.RecordSettlementMovement(ctx, s.admin, nov, s.olga, suggestion.Revision, n)
		wantErr(t, "repeated payment", err, store.ErrVersionConflict)
		next, err := s.SettlementSuggestion(ctx, s.olga, nov)
		must(t, err)
		if next.Balance != 3000 {
			t.Fatal(next.Balance)
		}
		state, err := s.Month(ctx, nov)
		must(t, err)
		if !reflect.DeepEqual(closed.Snapshot, state.Snapshot) {
			t.Fatal("later payout changed snapshot")
		}
		audits, err := s.Audit(ctx, nov.String())
		must(t, err)
		if len(audits) != 1 {
			t.Fatal("payment rewrote month")
		}
	})
}

func TestClosedSettlementKeepsFrozenSalaryAfterFutureTermsAndPayments(t *testing.T) {
	eachSalon(t, func(t *testing.T, s *salon) {
		s.at("2026-12-20")
		nov := month(t, "2026-11")
		closed, err := s.CloseMonth(ctx, s.admin, nov)
		must(t, err)
		must(t, s.SetTerms(ctx, s.admin, s.anna, month(t, "2026-12"), domain.Terms{SelfPct: 5000, SalaryCents: 120000, DividendWeight: 1}, ""))
		_, err = s.RecordMovement(ctx, s.admin, store.NewMovement{Type: domain.Payout, Amount: 30000, From: domain.Account(domain.KassaCard), To: domain.Person(s.anna), Date: s.Today()})
		must(t, err)
		p, err := s.Settlement(ctx, nov)
		must(t, err)
		if p.Result.Salaries != 100000 || p.Result.People[s.anna].Salary != 100000 || p.Breakdowns[s.anna].Salary != 100000 {
			t.Fatalf("future terms leaked: %+v", p)
		}
		if !reflect.DeepEqual(p.Result, closed.Snapshot.Result) {
			t.Fatal("frozen result changed")
		}
		own, _, err := s.OwnMonthResult(ctx, s.anna, nov)
		must(t, err)
		if own.Salary != 100000 {
			t.Fatal("own salary changed")
		}
	})
}

func TestOctoberSpreadsheetSettlementContract(t *testing.T) {
	eachStore(t, func(t *testing.T, open func(...store.Option) *store.Store) {
		now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
		st := open(store.WithStartMonth(month(t, "2026-10")), store.WithClock(func() time.Time { return now }))
		admin, err := st.SetUp(ctx, defaultSetup())
		must(t, err)
		type row struct {
			uid                                               string
			rate                                              domain.BasisPoints
			revenue, returned, expense, paid, salary, closing domain.Cents
			weight                                            int64
		}
		rows := []row{{uid: admin.ID, rate: 4500, revenue: 143000, returned: 143000, paid: 195000, salary: 120000, closing: 48978, weight: 1}}
		for _, r := range []struct {
			name                                      string
			rate                                      domain.BasisPoints
			revenue, returned, expense, paid, closing domain.Cents
			weight                                    int64
		}{
			{"maria", 6500, 742100, 205000, 4800, 0, 9693, 1}, {"olga", 6500, 568000, 151000, 21500, 0, 33328, 1}, {"vera", 4000, 81000, 81000, 0, 41250, -8850, 0},
		} {
			u, err := st.CreateUser(ctx, admin.Actor(), store.NewUser{Username: r.name, DisplayName: r.name, Role: store.RoleEmployee, PasswordHash: "x", SubmitsReports: true})
			must(t, err)
			rows = append(rows, row{uid: u.ID, rate: r.rate, revenue: r.revenue, returned: r.returned, expense: r.expense, paid: r.paid, closing: r.closing, weight: r.weight})
		}
		oct := month(t, "2026-10")
		day := date(t, "2026-10-20")
		zero := domain.Cents(0)
		for _, r := range rows {
			must(t, st.SetTerms(ctx, admin.Actor(), r.uid, oct, domain.Terms{SelfPct: r.rate, SalaryCents: r.salary, DividendWeight: r.weight}, ""))
			_, err := st.SaveReport(ctx, admin.Actor(), store.ReportEntry{Person: r.uid, Date: day, Revenue: map[string]domain.Cents{"services": r.revenue}, Payments: map[string]domain.Cents{"cash": r.revenue}, CashToKassa: &zero})
			must(t, err)
			for _, n := range []store.NewMovement{
				{Type: domain.Deposit, Amount: r.returned, From: domain.Person(r.uid), To: domain.Account(domain.KassaCard), Date: day},
				{Type: domain.Expense, Amount: r.expense, From: domain.Person(r.uid), To: domain.Outside, ExpenseCategory: "materials", Note: "Расход", Date: day},
				{Type: domain.Payout, Amount: r.paid, From: domain.Account(domain.KassaCard), To: domain.Person(r.uid), Date: day},
			} {
				if n.Amount > 0 {
					_, err := st.RecordMovement(ctx, admin.Actor(), n)
					must(t, err)
				}
			}
		}
		_, err = st.RecordMovement(ctx, admin.Actor(), store.NewMovement{Type: domain.Expense, Amount: 260600, From: domain.Account(domain.KassaCard), To: domain.Outside, ExpenseCategory: "rent", Note: "Расходы салона", Date: day})
		must(t, err)
		preview, err := st.Settlement(ctx, oct)
		must(t, err)
		if r := preview.Result; r.SalonIncome != 585785 || r.Expenses != 286900 || r.Salaries != 120000 || r.Profit != 178885 || r.DividendPool != 178885 {
			t.Fatalf("literal sheet result %+v", r)
		}
		extra := 0
		for _, r := range rows {
			p := preview.Result.People[r.uid]
			if p.Closing == r.closing+1 && r.weight > 0 {
				extra++
			} else if p.Closing != r.closing {
				t.Fatalf("%s closing %d", r.uid, p.Closing)
			}
			if preview.Breakdowns[r.uid].Closing != p.Closing {
				t.Fatal("breakdown differs")
			}
		}
		if extra != 1 {
			t.Fatalf("extra cents %d", extra)
		}
		closed, err := st.CloseMonthGuarded(ctx, admin.Actor(), oct, preview.Revision)
		must(t, err)
		if !reflect.DeepEqual(preview.Result, closed.Snapshot.Result) {
			t.Fatal("preview/close differ")
		}
		latest, err := st.Settlement(ctx, oct)
		must(t, err)
		_, err = st.ReopenMonthGuarded(ctx, admin.Actor(), oct, latest.Revision)
		must(t, err)
		latest, err = st.Settlement(ctx, oct)
		must(t, err)
		again, err := st.CloseMonthGuarded(ctx, admin.Actor(), oct, latest.Revision)
		must(t, err)
		if !reflect.DeepEqual(closed.Snapshot, again.Snapshot) {
			t.Fatal("reopen duplicated accruals")
		}
	})
}

func TestBatchedRosterAndRecountWarningsRemainOutsideFinancialCloseGuard(t *testing.T) {
	eachSalon(t, func(t *testing.T, s *salon) {
		nov := month(t, "2026-11")
		selected, err := s.Settlement(ctx, nov)
		must(t, err)
		s.at("2026-11-21")
		_, err = s.UpdateUser(ctx, s.admin, s.maria, func(u *store.User) error { u.Active = false; return nil })
		must(t, err)
		rosters, err := s.ExpectedReportersBetween(ctx, nov.FirstDay(), nov.LastDay())
		must(t, err)
		if len(rosters) != 30 || !slices.Contains(rosters[date(t, "2026-11-20")], s.maria) || slices.Contains(rosters[date(t, "2026-11-21")], s.maria) {
			t.Fatal("wrong full-month roster")
		}
		s.at("2026-12-20")
		_, err = s.Recount(ctx, s.admin, domain.KassaCash, 0, "")
		must(t, err)
		closed, err := s.CloseMonthGuarded(ctx, s.admin, nov, selected.Revision)
		must(t, err)
		if closed.Status != domain.MonthClosed || !reflect.DeepEqual(closed.Snapshot.Result, selected.Result) {
			t.Fatal("warning state changed financial result or blocked close")
		}
	})
}

func TestSettlementAmountCannotCreateReverseDebtButAllowsPartialAndFullPayment(t *testing.T) {
	eachSalon(t, func(t *testing.T, s *salon) {
		for _, kind := range []domain.MovementType{domain.Payout, domain.Deposit} {
			t.Run(string(kind), func(t *testing.T) {
				uid := s.addPerson(t, "cap-"+string(kind), "Расчёт")
				m := month(t, "2026-11")
				initial := store.NewMovement{Type: domain.Deposit, Amount: 5000, From: domain.Person(uid), To: domain.Account(domain.KassaCard), Date: s.Today()}
				n := store.NewMovement{Type: domain.Payout, Amount: 6000, From: domain.Account(domain.KassaCard), To: domain.Person(uid), Date: s.Today()}
				expected := domain.Cents(5000)
				if kind == domain.Deposit {
					initial, n = n, initial
					initial.Amount = 5000
					n.Amount = 6000
					expected = -5000
				}
				_, err := s.RecordMovement(ctx, s.admin, initial)
				must(t, err)
				suggestion, err := s.SettlementSuggestion(ctx, uid, m)
				must(t, err)
				beforeLedger, err := s.PersonMovementsIn(ctx, uid, m)
				must(t, err)
				beforeAudit, err := s.AuditBetween(ctx, s.Today(), s.Today())
				must(t, err)
				_, err = s.RecordSettlementMovement(ctx, s.admin, m, uid, suggestion.Revision, n)
				var invalid *domain.ValidationError
				if !errors.As(err, &invalid) || invalid.Field != "amount" || invalid.Problem != store.ProblemSettlementAmount {
					t.Fatalf("oversized settlement must fail on amount: %v", err)
				}
				balance, err := s.PersonBalance(ctx, uid)
				must(t, err)
				if balance != expected {
					t.Fatalf("rejection changed balance %d", balance)
				}
				afterLedger, err := s.PersonMovementsIn(ctx, uid, m)
				must(t, err)
				afterAudit, err := s.AuditBetween(ctx, s.Today(), s.Today())
				must(t, err)
				if !reflect.DeepEqual(beforeLedger, afterLedger) || !reflect.DeepEqual(beforeAudit, afterAudit) {
					t.Fatal("rejected settlement wrote financial/audit rows")
				}
				n.Amount = 2000
				_, err = s.RecordSettlementMovement(ctx, s.admin, m, uid, suggestion.Revision, n)
				must(t, err)
				_, err = s.RecordSettlementMovement(ctx, s.admin, m, uid, suggestion.Revision, n)
				wantErr(t, "stale partial settlement", err, store.ErrVersionConflict)
				remaining, err := s.SettlementSuggestion(ctx, uid, m)
				must(t, err)
				want := domain.Cents(3000)
				if kind == domain.Deposit {
					want = -3000
				}
				if remaining.Balance != want {
					t.Fatalf("partial leaves %d", remaining.Balance)
				}
				n.Amount = 3000
				_, err = s.RecordSettlementMovement(ctx, s.admin, m, uid, remaining.Revision, n)
				must(t, err)
				balance, err = s.PersonBalance(ctx, uid)
				must(t, err)
				if balance != 0 {
					t.Fatalf("full settlement leaves %d", balance)
				}
				// Generic movements intentionally retain their broader accounting behavior.
				n.Amount = 6000
				_, err = s.RecordMovement(ctx, s.admin, n)
				must(t, err)
				balance, err = s.PersonBalance(ctx, uid)
				must(t, err)
				want = -6000
				if kind == domain.Deposit {
					want = 6000
				}
				if balance != want {
					t.Fatalf("generic movement changed behavior %d", balance)
				}
			})
		}
	})
}

func TestSettlementGivesTheBalancesAtMonthEnd(t *testing.T) {
	eachSalon(t, func(t *testing.T, s *salon) {
		nov := month(t, "2026-11")
		kassaCash := func(what string, want domain.Cents) {
			t.Helper()
			got, err := s.Settlement(ctx, nov)
			must(t, err)
			if got.Closing == nil || got.Closing.Accounts[domain.KassaCash] != want {
				t.Fatalf("%s: closing %+v, want kassa cash %d", what, got.Closing, want)
			}
		}
		_, err := s.RecordMovement(ctx, s.admin, store.NewMovement{Type: domain.Opening, Amount: 100000, From: domain.Salon, To: domain.Account(domain.KassaCash), Date: nov.FirstDay()})
		must(t, err)
		_, err = s.RecordMovement(ctx, s.admin, store.NewMovement{Type: domain.Deposit, Amount: 10000, From: domain.Person(s.olga), To: domain.Account(domain.KassaCash), Date: date(t, "2026-11-12")})
		must(t, err)
		_, err = s.RecordMovement(ctx, s.admin, store.NewMovement{Type: domain.Expense, Amount: 5000, From: domain.Account(domain.KassaCash), To: domain.Outside, ExpenseCategory: "rent", Date: date(t, "2026-11-13"), Note: "аренда"})
		must(t, err)
		_, err = s.RecordMovement(ctx, s.actor(s.olga), store.NewMovement{Type: domain.Deposit, Amount: 7000, From: domain.Person(s.olga), To: domain.Account(domain.KassaCash), Date: date(t, "2026-11-14")})
		must(t, err) // pending: not in the kassa yet
		kassaCash("open month", 105000)

		s.at("2026-12-20")
		_, err = s.RecordMovement(ctx, s.admin, store.NewMovement{Type: domain.Deposit, Amount: 90000, From: domain.Person(s.olga), To: domain.Account(domain.KassaCash), Date: date(t, "2026-12-02")})
		must(t, err)
		kassaCash("with a later month's rows", 105000)

		pending, err := s.PendingMovements(ctx)
		must(t, err)
		_, err = s.VoidMovement(ctx, s.admin, pending[0].ID, pending[0].Version, "не было")
		must(t, err)
		fresh, err := s.Settlement(ctx, nov)
		must(t, err)
		_, err = s.CloseMonthGuarded(ctx, s.admin, nov, fresh.Revision)
		must(t, err)
		kassaCash("closed month", 105000)
	})
}

func TestOpenMonthBalancesStartFromTheClosedMonth(t *testing.T) {
	eachSalon(t, func(t *testing.T, s *salon) {
		nov, dec := month(t, "2026-11"), month(t, "2026-12")
		_, err := s.RecordOpenings(ctx, s.admin, []store.NewMovement{
			{Type: domain.Opening, Amount: 100000, From: domain.Salon, To: domain.Account(domain.KassaCash), Date: nov.FirstDay()},
			{Type: domain.Opening, Amount: 20000, From: domain.Salon, To: domain.Account(domain.KassaCard), Date: nov.FirstDay()},
		})
		must(t, err)
		s.at("2026-12-10")
		fresh, err := s.Settlement(ctx, nov)
		must(t, err)
		_, err = s.CloseMonthGuarded(ctx, s.admin, nov, fresh.Revision)
		must(t, err)
		_, err = s.RecordMovement(ctx, s.admin, store.NewMovement{Type: domain.Transfer, Amount: 30000, From: domain.Account(domain.KassaCash), To: domain.Account(domain.KassaCard), Date: date(t, "2026-12-03")})
		must(t, err)
		_, err = s.RecordMovement(ctx, s.admin, store.NewMovement{Type: domain.Expense, Amount: 4000, From: domain.Account(domain.KassaCard), To: domain.Outside, ExpenseCategory: "rent", Date: date(t, "2026-12-04"), Note: "аренда"})
		must(t, err)

		got, err := s.Settlement(ctx, dec)
		must(t, err)
		if got.Closing == nil || got.Closing.Accounts[domain.KassaCash] != 70000 || got.Closing.Accounts[domain.KassaCard] != 46000 {
			t.Fatalf("December closing %+v, want cash 70000 and card 46000", got.Closing)
		}
	})
}
