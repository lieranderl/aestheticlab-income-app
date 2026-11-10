package store_test

import (
	"testing"

	"github.com/lieranderl/aestheticlab-income-app/internal/domain"
	"github.com/lieranderl/aestheticlab-income-app/internal/store"
)

func TestAMovementSentTwiceWithItsTokenIsRecordedOnce(t *testing.T) {
	eachSalon(t, func(t *testing.T, s *salon) {
		nov12 := date(t, "2026-11-12")
		pay := store.NewMovement{Token: "mv_repeatprotectiontoken01", Type: domain.Expense, Amount: 10000, From: domain.Account(domain.KassaCash), To: domain.Outside, ExpenseCategory: "rent", Date: nov12, Note: "аренда"}
		first, err := s.RecordMovement(ctx, s.admin, pay)
		must(t, err)
		again, err := s.RecordMovement(ctx, s.admin, pay)
		must(t, err)
		if again.ID != first.ID || again.Version != first.Version {
			t.Fatalf("repeat = %+v, want the first row %+v", again, first)
		}
		b, err := s.Balances(ctx)
		must(t, err)
		if b.Accounts[domain.KassaCash] != -10000 {
			t.Fatalf("kassa after a repeated expense = %d, want it booked once", b.Accounts[domain.KassaCash])
		}
		audits, err := s.Audit(ctx, first.ID)
		must(t, err)
		if len(audits) != 1 {
			t.Fatalf("a repeat wrote %d audit rows, want the one create", len(audits))
		}

		changed := pay
		changed.Amount = 12000
		_, err = s.RecordMovement(ctx, s.admin, changed)
		wantErr(t, "same token, other amount", err, store.ErrVersionConflict)
		_, err = s.RecordMovement(ctx, s.actor(s.olga), store.NewMovement{Token: pay.Token, Type: domain.Deposit, Amount: 10000, From: domain.Person(s.olga), To: domain.Account(domain.KassaCash), Date: nov12})
		wantErr(t, "someone else's token", err, store.ErrVersionConflict)

		_, err = s.VoidMovement(ctx, s.admin, first.ID, first.Version, "ошибка")
		must(t, err)
		_, err = s.RecordMovement(ctx, s.admin, pay)
		wantProblem(t, "a voided row is not acknowledged as saved", err, store.ProblemAlreadyVoided)

		plainMove := pay
		plainMove.Token = ""
		plain, err := s.RecordMovement(ctx, s.admin, plainMove)
		must(t, err) // a generated row ID is never a usable form token
		for _, bad := range []string{"mv_short", "mv_has/slash-in-the-token-01", "mv_UPPERCASEUPPERCASEUPPER", plain.ID} {
			n := pay
			n.Token = bad
			_, err = s.RecordMovement(ctx, s.admin, n)
			wantErr(t, "token "+bad, err, store.ErrInvalid)
		}
	})
}
