package store_test

import (
	"github.com/lieranderl/aestheticlab-income-app/internal/domain"
	"github.com/lieranderl/aestheticlab-income-app/internal/store"
	"testing"
)

func TestPersonBalanceAndMovementsStayScopedAcrossSnapshots(t *testing.T) {
	eachSalon(t, func(t *testing.T, s *salon) {
		n := store.NewMovement{Type: domain.Deposit, Amount: 10000, From: domain.Person(s.olga), To: domain.Account("kassa_cash"), Date: date(t, "2026-11-20")}
		e, err := s.RecordMovement(ctx, s.actor(s.olga), n)
		must(t, err)
		b, err := s.PersonBalance(ctx, s.olga)
		must(t, err)
		if b != 0 {
			t.Fatalf("pending %d", b)
		}
		_, err = s.ConfirmMovement(ctx, s.admin, e.ID, e.Version)
		must(t, err)
		n.From = domain.Person(s.maria)
		n.Amount = 99999
		_, err = s.RecordMovement(ctx, s.admin, n)
		must(t, err)
		b, err = s.PersonBalance(ctx, s.olga)
		must(t, err)
		if b != 10000 {
			t.Fatalf("own %d", b)
		}
		es, err := s.PersonMovementsIn(ctx, s.olga, month(t, "2026-11"))
		must(t, err)
		if len(es) != 1 || es[0].PersonID != s.olga {
			t.Fatalf("rows %+v", es)
		}
		es, err = s.LedgerBetween(ctx, date(t, "2026-11-19"), date(t, "2026-11-19"))
		must(t, err)
		if len(es) != 0 {
			t.Fatal("period includes next day")
		}
		s.at("2026-12-20")
		_, err = s.CloseMonth(ctx, s.admin, month(t, "2026-11"))
		must(t, err)
		before, err := s.PersonBalance(ctx, s.olga)
		must(t, err)
		if before != 10000 {
			t.Fatalf("snapshot own balance %d", before)
		}
		n.From = domain.Person(s.olga)
		n.Amount = 5000
		n.Date = date(t, "2026-12-20")
		_, err = s.RecordMovement(ctx, s.admin, n)
		must(t, err)
		b, err = s.PersonBalance(ctx, s.olga)
		must(t, err)
		if b != before+5000 {
			t.Fatalf("snapshot %d -> %d", before, b)
		}
	})
}

func TestPersonBalanceExcludesFutureAndForeignRecords(t *testing.T) {
	eachSalon(t, func(t *testing.T, s *salon) {
		s.at("2026-12-20")
		_, err := s.SaveReport(ctx, s.actor(s.olga), store.ReportEntry{Person: s.olga, Date: date(t, "2026-12-20"), Revenue: map[string]domain.Cents{"services": 20000}, Payments: map[string]domain.Cents{"card": 20000}})
		must(t, err)
		_, err = s.RecordMovement(ctx, s.admin, store.NewMovement{Type: domain.Deposit, Amount: 5000, From: domain.Person(s.olga), To: domain.Account("kassa_cash"), Date: date(t, "2026-12-20")})
		must(t, err)
		_, err = s.RecordMovement(ctx, s.admin, store.NewMovement{Type: domain.Deposit, Amount: 99900, From: domain.Person(s.maria), To: domain.Account("kassa_cash"), Date: date(t, "2026-12-19")})
		must(t, err)
		b, err := s.PersonBalance(ctx, s.olga)
		must(t, err)
		if b != 19000 {
			t.Fatalf("today %d", b)
		}
		s.at("2026-12-19")
		b, err = s.PersonBalance(ctx, s.olga)
		must(t, err)
		if b != 0 {
			t.Fatalf("future included %d", b)
		}
	})
}
