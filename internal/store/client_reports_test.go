package store_test

import (
	"errors"
	"fmt"
	"github.com/lieranderl/aestheticlab-income-app/internal/domain"
	"github.com/lieranderl/aestheticlab-income-app/internal/store"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func TestTwoClientsInOneDayRemainSeparateAndRetryIsIdempotent(t *testing.T) {
	eachSalon(t, func(t *testing.T, s *salon) {
		day := date(t, "2026-11-20")
		in := store.ClientEntry{Person: s.olga, Date: day, Token: "first-client-token", Input: domain.ClientInput{Services: 10000, Extras: map[string]domain.Cents{"creams": 2000}, ExtrasDestination: domain.ToCash, ServiceCash: 3000}}
		first, err := s.SaveClientReport(ctx, s.actor(s.olga), in)
		must(t, err)
		retry, err := s.SaveClientReport(ctx, s.actor(s.olga), in)
		must(t, err)
		in.Token = "second-client-token"
		in.Input.Services = 5000
		in.Input.ServiceCash = 1500
		in.Input.Extras = nil
		second, err := s.SaveClientReport(ctx, s.actor(s.olga), in)
		must(t, err)
		if first.ID != retry.ID || first.ClientNumber != 1 || second.ClientNumber != 2 {
			t.Fatalf("clients %+v / %+v / %+v", first, retry, second)
		}
		rows, err := s.PersonReportsIn(ctx, s.olga, day.Month())
		must(t, err)
		if len(rows) != 2 {
			t.Fatalf("%d clients", len(rows))
		}
		bal, err := s.Balances(ctx)
		must(t, err)
		if bal.Accounts[domain.KassaCash] != 6500 || bal.Persons[s.olga] != 0 {
			t.Fatalf("balances %+v", bal)
		}
	})
}

func TestClientRecordsAppearInDayCountsAndFrozenClosing(t *testing.T) {
	eachSalon(t, func(t *testing.T, s *salon) {
		day := date(t, "2026-11-20")
		_, err := s.SaveClientReport(ctx, s.actor(s.olga), store.ClientEntry{Person: s.olga, Date: day, Token: "count-client-token", Input: domain.ClientInput{Services: 10000, CertificatePaid: 10000}})
		must(t, err)
		rows, err := s.ReportsOn(ctx, day)
		must(t, err)
		sum := domain.Summarize([]domain.PersonReport{rows[0].PersonReport})
		if sum.Clients != 1 || sum.People[s.olga].Clients != 1 {
			t.Fatalf("counts %+v", sum)
		}
		s.at("2026-12-01")
		closed, err := s.CloseMonth(ctx, s.admin, day.Month())
		must(t, err)
		if closed.Snapshot.Result.Clients != 1 || closed.Snapshot.Result.People[s.olga].Clients != 1 {
			t.Fatalf("closed %+v", closed.Snapshot.Result)
		}
		again, err := s.Month(ctx, day.Month())
		must(t, err)
		if again.Snapshot.Result.Clients != 1 {
			t.Fatal("count not frozen")
		}
	})
}

func TestClientEditsKeepNumberAndCreationTimeAndAreAudited(t *testing.T) {
	eachSalon(t, func(t *testing.T, s *salon) {
		in := store.ClientEntry{Person: s.olga, Date: date(t, "2026-11-20"), Token: "edit-client-token", Input: domain.ClientInput{Services: 10000, ServiceCard: 3000}}
		first, err := s.SaveClientReport(ctx, s.actor(s.olga), in)
		must(t, err)
		if _, err = s.ClientReport(ctx, s.actor(s.maria), first.ID); !errors.Is(err, store.ErrForbidden) {
			t.Fatalf("privacy %v", err)
		}
		audits, err := s.Audit(ctx, first.ID)
		must(t, err)
		if len(audits) != 1 {
			t.Fatalf("audit count%d", len(audits))
		}
		s.at("2026-11-21")
		in.Version = first.Version
		in.Input.ServiceCash = 2000
		updated, err := s.SaveClientReport(ctx, s.admin, in)
		must(t, err)
		if updated.ClientNumber != 1 || !updated.CreatedAt.Equal(first.CreatedAt) || !first.UpdatedAt.Before(updated.UpdatedAt) || updated.Version != 2 {
			t.Fatalf("edit %+v", updated)
		}
		if _, err = s.SaveClientReport(ctx, s.admin, in); !errors.Is(err, store.ErrVersionConflict) {
			t.Fatalf("stale %v", err)
		}
		audits, err = s.Audit(ctx, first.ID)
		must(t, err)
		if len(audits) != 2 {
			t.Fatalf("audit count%d", len(audits))
		}
		in.Version = 0
		in.Input.ServiceCard = 0
		if _, err = s.SaveClientReport(ctx, s.actor(s.olga), in); !errors.Is(err, store.ErrVersionConflict) {
			t.Fatalf("changed retry %v", err)
		}
		s.at("2026-12-01")
		_, err = s.CloseMonth(ctx, s.admin, in.Date.Month())
		must(t, err)
		in.Version = 2
		if _, err = s.SaveClientReport(ctx, s.admin, in); !errors.Is(err, store.ErrMonthClosed) {
			t.Fatalf("closed %v", err)
		}
	})
}

func TestConcurrentClientsHaveDistinctNumbers(t *testing.T) {
	eachSalon(t, func(t *testing.T, s *salon) {
		const n = 8
		errs := make(chan error, n)
		var wg sync.WaitGroup
		for i := range n {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, err := s.SaveClientReport(ctx, s.actor(s.olga), store.ClientEntry{Person: s.olga, Date: date(t, "2026-11-20"), Token: fmt.Sprintf("concurrent-client-%d", i), Input: domain.ClientInput{Services: 10000, ServiceCash: 3000}})
				errs <- err
			}()
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			must(t, err)
		}
		rows, err := s.ReportsIn(ctx, month(t, "2026-11"))
		must(t, err)
		seen := map[int64]bool{}
		for _, r := range rows {
			if seen[r.ClientNumber] {
				t.Fatal("duplicate number")
			}
			seen[r.ClientNumber] = true
		}
		if len(rows) != n {
			t.Fatalf("%d rows", len(rows))
		}
	})
}

func TestFirstOilSaleCreatesAuditedUsedCategory(t *testing.T) {
	eachSalon(t, func(t *testing.T, s *salon) {
		_, err := s.SaveClientReport(ctx, s.actor(s.olga), store.ClientEntry{Person: s.olga, Date: date(t, "2026-11-20"), Token: "first-oil-client-token", Input: domain.ClientInput{Services: 10000, ServiceCash: 3000, Extras: map[string]domain.Cents{"oil": 1000}, ExtrasDestination: domain.ToCash}})
		must(t, err)
		cats, err := s.Categories(ctx)
		must(t, err)
		found := false
		for _, cat := range cats {
			if cat.ID == "oil" {
				found = true
				if cat.UsedCount != 1 || cat.CountsTowardSelf {
					t.Fatalf("oil %+v", cat)
				}
			}
		}
		if !found {
			t.Fatal("oil missing")
		}
		audits, err := s.Audit(ctx, "oil")
		must(t, err)
		if len(audits) != 2 {
			t.Fatalf("oil audits%d", len(audits))
		}
	})
}

func TestClientCertificateSaleAndRedemptionFreezeStatisticsAndReclose(t *testing.T) {
	eachSalon(t, func(t *testing.T, s *salon) {
		nov := month(t, "2026-11")
		must(t, s.SetTerms(ctx, s.admin, s.anna, nov, domain.Terms{SelfPct: 5000, DividendWeight: 1}, ""))
		_, err := s.SaveClientReport(ctx, s.actor(s.olga), store.ClientEntry{Person: s.olga, Date: date(t, "2026-11-20"), Token: "certificate-sale-token", Input: domain.ClientInput{Extras: map[string]domain.Cents{"certificates": 10000}, ExtrasDestination: domain.ToKassaCard}})
		must(t, err)
		bal, err := s.Balances(ctx)
		must(t, err)
		if bal.CertificatesOutstanding != 10000 || bal.Accounts[domain.KassaCard] != 10000 || bal.Persons[s.olga] != 0 {
			t.Fatalf("sale %+v", bal)
		}
		s.at("2026-11-21")
		_, err = s.SaveClientReport(ctx, s.actor(s.olga), store.ClientEntry{Person: s.olga, Date: date(t, "2026-11-21"), Token: "certificate-use-token", Input: domain.ClientInput{Services: 10000, CertificatePaid: 10000}})
		must(t, err)
		bal, err = s.Balances(ctx)
		must(t, err)
		if bal.CertificatesOutstanding != 0 || bal.Accounts[domain.KassaCard] != 10000 || bal.Persons[s.olga] != 7000 {
			t.Fatalf("redemption %+v", bal)
		}
		s.at("2026-12-01")
		closed, err := s.CloseMonth(ctx, s.admin, nov)
		must(t, err)
		got := closed.Snapshot.Result
		p := got.People[s.olga]
		if got.StatsVersion != 1 || p.StatsVersion != 1 || got.Clients != 2 || got.KassaCard != 10000 || got.KassaCash != 0 || got.SalonIncome != 3000 || got.Profit != 3000 || p.Clients != 2 || p.KassaCard != 10000 || p.RevenueByCategory["services"] != 10000 || p.RevenueByCategory["certificates"] != 10000 || p.Closing != 8000 {
			t.Fatalf("snapshot %+v person %+v", got, p)
		}
		loaded, err := s.Month(ctx, nov)
		must(t, err)
		if !reflect.DeepEqual(got, loaded.Snapshot.Result) {
			t.Fatal("snapshot statistics changed on read")
		}
		_, err = s.ReopenMonth(ctx, s.admin, nov)
		must(t, err)
		closed, err = s.CloseMonth(ctx, s.admin, nov)
		must(t, err)
		if !reflect.DeepEqual(got, closed.Snapshot.Result) {
			t.Fatal("reclose changed client result")
		}
	})
}

func TestSameDayClientOrderUsesOrdinalBeforeRandomToken(t *testing.T) {
	eachSalon(t, func(t *testing.T, s *salon) {
		day := date(t, "2026-11-20")
		for _, token := range []string{"zzzz-client-order-token", "aaaa-client-order-token"} {
			_, err := s.SaveClientReport(ctx, s.actor(s.olga), store.ClientEntry{Person: s.olga, Date: day, Token: token, Input: domain.ClientInput{Services: 10000}})
			must(t, err)
		}
		rows, err := s.PersonReportsIn(ctx, s.olga, day.Month())
		must(t, err)
		if len(rows) != 2 || rows[0].ClientNumber != 1 || rows[1].ClientNumber != 2 {
			t.Fatalf("random token dictated client order: %+v", rows)
		}
	})
}

func TestOriginalClientRetryAcknowledgesClosedMonthWithoutFinancialWrites(t *testing.T) {
	eachSalon(t, func(t *testing.T, s *salon) {
		original := store.ClientEntry{ // #nosec G101 -- idempotency token, not credentials
			Person: s.olga,
			Date:   date(t, "2026-11-20"),
			Token:  "client-retry-attempt-1",
			Input:  domain.ClientInput{Services: 10000, ServiceCash: 3000},
		}
		first, err := s.SaveClientReport(ctx, s.actor(s.olga), original)
		must(t, err)
		s.at("2026-12-01")
		_, err = s.CloseMonth(ctx, s.admin, original.Date.Month())
		must(t, err)
		frozen, err := s.Month(ctx, original.Date.Month())
		must(t, err)
		audits, err := s.AuditBetween(ctx, original.Date, date(t, "2026-12-01"))
		must(t, err)
		balances, err := s.Balances(ctx)
		must(t, err)
		for _, actor := range []store.Actor{s.actor(s.olga), s.admin} {
			retry, err := s.SaveClientReport(ctx, actor, original)
			must(t, err)
			if !reflect.DeepEqual(first, retry) {
				t.Fatalf("acknowledgement changed original: %+v / %+v", first, retry)
			}
		}
		after, err := s.Month(ctx, original.Date.Month())
		must(t, err)
		afterAudits, err := s.AuditBetween(ctx, original.Date, date(t, "2026-12-01"))
		must(t, err)
		afterBalances, err := s.Balances(ctx)
		must(t, err)
		rows, err := s.ReportsOn(ctx, original.Date)
		must(t, err)
		if !reflect.DeepEqual(frozen, after) || !reflect.DeepEqual(audits, afterAudits) || !reflect.DeepEqual(balances, afterBalances) || len(rows) != 1 {
			t.Fatal("closed retry changed frozen financial state, audit or report count")
		}
		changed := original
		changed.Input.Services = 20000
		fresh := original
		fresh.Token = "client-new-attempt-2"
		edit := original
		edit.Version = 1
		for _, in := range []store.ClientEntry{changed, fresh, edit} {
			if _, err := s.SaveClientReport(ctx, s.actor(s.olga), in); !errors.Is(err, store.ErrMonthClosed) {
				t.Fatalf("closed mutation accepted: %+v %v", in, err)
			}
		}
		if _, err := s.SaveClientReport(ctx, s.actor(s.maria), original); !errors.Is(err, store.ErrForbidden) {
			t.Fatalf("closed retry bypasses ownership: %v", err)
		}
		_, err = s.UpdateUser(ctx, s.admin, s.olga, func(u *store.User) error { u.SubmitsReports = false; return nil })
		must(t, err)
		if _, err := s.SaveClientReport(ctx, s.actor(s.olga), original); !errors.Is(err, store.ErrForbidden) {
			t.Fatalf("retry bypasses reporting permission: %v", err)
		}
		_, err = s.UpdateUser(ctx, s.admin, s.olga, func(u *store.User) error { u.SubmitsReports = true; return nil })
		must(t, err)
		_, err = s.ReopenMonth(ctx, s.admin, original.Date.Month())
		must(t, err)
		second, err := s.SaveClientReport(ctx, s.actor(s.olga), fresh)
		must(t, err)
		if second.ClientNumber != 2 {
			t.Fatalf("acknowledgement or refused mutation advanced ordinal: %d", second.ClientNumber)
		}
	})
}

func TestRemovedClientLeavesTotalsBalancesAndNumberingButStaysAudited(t *testing.T) {
	eachSalon(t, func(t *testing.T, s *salon) {
		day := date(t, "2026-11-20")
		in := store.ClientEntry{Person: s.olga, Date: day, Token: "removed-client-token", Input: domain.ClientInput{Services: 10000, Extras: map[string]domain.Cents{"creams": 2000}, ExtrasDestination: domain.ToCash, ServiceCash: 3000}}
		mistake, err := s.SaveClientReport(ctx, s.actor(s.olga), in)
		must(t, err)
		kept, err := s.SaveClientReport(ctx, s.actor(s.olga), store.ClientEntry{Person: s.olga, Date: day, Token: "kept-client-token", Input: domain.ClientInput{Services: 5000, ServiceCash: 1500}})
		must(t, err)
		before, err := s.Settlement(ctx, day.Month())
		must(t, err)

		must(t, s.VoidClientReport(ctx, s.actor(s.olga), mistake.ID, mistake.Version, "  Добавлен по ошибке "))

		rows, err := s.PersonReportsIn(ctx, s.olga, day.Month())
		must(t, err)
		if len(rows) != 1 || rows[0].ID != kept.ID {
			t.Fatalf("rows after removal %+v", rows)
		}
		bal, err := s.Balances(ctx)
		must(t, err)
		if bal.Accounts[domain.KassaCash] != 1500 || bal.Persons[s.olga] != 0 {
			t.Fatalf("balances %+v", bal)
		}
		after, err := s.Settlement(ctx, day.Month())
		must(t, err)
		if after.Revision == before.Revision {
			t.Fatal("removal must invalidate a stale settlement revision")
		}
		if _, err := s.ClientReport(ctx, s.actor(s.olga), mistake.ID); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("removed client read %v", err)
		}
		if got := s.actions(t, mistake.ID); !reflect.DeepEqual(got, []string{store.ActionCreate, store.ActionVoid}) {
			t.Fatalf("audit %v", got)
		}
		rowsAudit, err := s.Audit(ctx, mistake.ID)
		must(t, err)
		if !strings.Contains(rowsAudit[0].After, `"voidReason":"Добавлен по ошибке"`) || rowsAudit[0].ActorID != s.olga {
			t.Fatalf("void audit %+v", rowsAudit[0])
		}
		if _, err := s.SaveClientReport(ctx, s.actor(s.olga), in); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("replaying the original create must not restore the client: %v", err)
		}
		third, err := s.SaveClientReport(ctx, s.actor(s.olga), store.ClientEntry{Person: s.olga, Date: day, Token: "third-client-token", Input: domain.ClientInput{Services: 1000}})
		must(t, err)
		if third.ClientNumber != 3 {
			t.Fatalf("client numbers are never reused, got %d", third.ClientNumber)
		}
	})
}

func TestClientRemovalNeedsOwnerOrAdminCurrentVersionReasonAndOpenMonth(t *testing.T) {
	eachSalon(t, func(t *testing.T, s *salon) {
		day := date(t, "2026-11-20")
		saved, err := s.SaveClientReport(ctx, s.actor(s.olga), store.ClientEntry{Person: s.olga, Date: day, Token: "guarded-client-token", Input: domain.ClientInput{Services: 10000}})
		must(t, err)
		if err := s.VoidClientReport(ctx, s.actor(s.maria), saved.ID, saved.Version, "чужой"); !errors.Is(err, store.ErrForbidden) {
			t.Fatalf("other employee %v", err)
		}
		if err := s.VoidClientReport(ctx, s.actor(s.olga), saved.ID, saved.Version+1, "устарело"); !errors.Is(err, store.ErrVersionConflict) {
			t.Fatalf("stale version %v", err)
		}
		wantProblem(t, "blank reason", s.VoidClientReport(ctx, s.actor(s.olga), saved.ID, saved.Version, "  "), store.ProblemNoteRequired)
		if err := s.VoidClientReport(ctx, s.actor(s.olga), "report_missing", 1, "нет"); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("missing %v", err)
		}
		other, err := s.SaveClientReport(ctx, s.actor(s.olga), store.ClientEntry{Person: s.olga, Date: day, Token: "closed-client-token", Input: domain.ClientInput{Services: 4000}})
		must(t, err)
		must(t, s.VoidClientReport(ctx, s.admin, saved.ID, saved.Version, "Администратор исправил"))
		if err := s.VoidClientReport(ctx, s.admin, saved.ID, saved.Version+1, "ещё раз"); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("second removal %v", err)
		}
		s.at("2026-12-01")
		_, err = s.CloseMonth(ctx, s.admin, day.Month())
		must(t, err)
		if err := s.VoidClientReport(ctx, s.admin, other.ID, other.Version, "после закрытия"); !errors.Is(err, store.ErrMonthClosed) {
			t.Fatalf("closed month %v", err)
		}
	})
}

func TestRemovingTheOnlyClientLetsNewTermsCoverThatMonth(t *testing.T) {
	eachSalon(t, func(t *testing.T, s *salon) {
		dec := month(t, "2026-12")
		s.at("2026-12-02")
		saved, err := s.SaveClientReport(ctx, s.actor(s.vera), store.ClientEntry{Person: s.vera, Date: date(t, "2026-12-02"), Token: "terms-client-token", Input: domain.ClientInput{Services: 1000}})
		must(t, err)
		if err := s.SetTerms(ctx, s.admin, s.vera, dec, domain.Terms{SelfPct: 6000}, ""); err == nil {
			t.Fatal("terms must not change under a live client record")
		}
		must(t, s.VoidClientReport(ctx, s.actor(s.vera), saved.ID, saved.Version, "Добавлен по ошибке"))
		must(t, s.SetTerms(ctx, s.admin, s.vera, dec, domain.Terms{SelfPct: 6000}, ""))
	})
}

func TestRemovalInAReopenedMonthIsLeftOutOfTheNewClose(t *testing.T) {
	eachSalon(t, func(t *testing.T, s *salon) {
		day := date(t, "2026-11-20")
		saved, err := s.SaveClientReport(ctx, s.actor(s.olga), store.ClientEntry{Person: s.olga, Date: day, Token: "reopened-mistake-token", Input: domain.ClientInput{Services: 10000, ServiceCash: 3000}})
		must(t, err)
		_, err = s.SaveClientReport(ctx, s.actor(s.olga), store.ClientEntry{Person: s.olga, Date: day, Token: "reopened-kept-token", Input: domain.ClientInput{Services: 5000, ServiceCash: 1500}})
		must(t, err)
		s.at("2026-12-01")
		_, err = s.CloseMonth(ctx, s.admin, day.Month())
		must(t, err)
		_, err = s.ReopenMonth(ctx, s.admin, day.Month())
		must(t, err)
		must(t, s.VoidClientReport(ctx, s.admin, saved.ID, saved.Version, "Дубль"))
		closed, err := s.CloseMonth(ctx, s.admin, day.Month())
		must(t, err)
		if closed.Snapshot.Result.Clients != 1 || closed.Snapshot.Result.Revenue != 5000 {
			t.Fatalf("reclosed snapshot %+v", closed.Snapshot.Result)
		}
	})
}

func TestRemovalReasonIsBoundedOnTheServer(t *testing.T) {
	eachSalon(t, func(t *testing.T, s *salon) {
		saved, err := s.SaveClientReport(ctx, s.actor(s.olga), store.ClientEntry{Person: s.olga, Date: date(t, "2026-11-20"), Token: "long-reason-client-token", Input: domain.ClientInput{Services: 1000}})
		must(t, err)
		wantProblem(t, "long reason", s.VoidClientReport(ctx, s.actor(s.olga), saved.ID, saved.Version, strings.Repeat("я", 201)), store.ProblemReasonTooLong)
		must(t, s.VoidClientReport(ctx, s.actor(s.olga), saved.ID, saved.Version, strings.Repeat("я", 200)))
	})
}

func TestNextClientNumberCountsRemovedClients(t *testing.T) {
	eachSalon(t, func(t *testing.T, s *salon) {
		day := date(t, "2026-11-20")
		if n, err := s.NextClientNumber(ctx, s.olga, day); err != nil || n != 1 {
			t.Fatalf("empty day next %d %v", n, err)
		}
		_, err := s.SaveClientReport(ctx, s.actor(s.olga), store.ClientEntry{Person: s.olga, Date: day, Token: "numbered-first-token", Input: domain.ClientInput{Services: 1000}})
		must(t, err)
		second, err := s.SaveClientReport(ctx, s.actor(s.olga), store.ClientEntry{Person: s.olga, Date: day, Token: "numbered-second-token", Input: domain.ClientInput{Services: 1000}})
		must(t, err)
		must(t, s.VoidClientReport(ctx, s.actor(s.olga), second.ID, second.Version, "Дубль"))
		if n, err := s.NextClientNumber(ctx, s.olga, day); err != nil || n != 3 {
			t.Fatalf("next after removal %d %v", n, err)
		}
	})
}
