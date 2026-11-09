package store_test

import (
	"testing"

	"github.com/lieranderl/aestheticlab-income-app/internal/domain"
	"github.com/lieranderl/aestheticlab-income-app/internal/store"
)

func TestEditingAnExpenseVoidsTheOldRowAndRecordsTheCorrectedOne(t *testing.T) {
	eachSalon(t, func(t *testing.T, s *salon) {
		nov12 := date(t, "2026-11-12")
		old, err := s.RecordMovement(ctx, s.admin, store.NewMovement{Type: domain.Expense, Amount: 10000, From: domain.Account(domain.KassaCash), To: domain.Outside, ExpenseCategory: "rent", Date: nov12, Note: "аренда"})
		must(t, err)
		fix := store.NewMovement{Type: domain.Expense, Amount: 12550, From: domain.Account(domain.KassaCard), To: domain.Outside, ExpenseCategory: "materials", Date: date(t, "2026-11-10"), Note: "материалы"}

		_, err = s.EditMovement(ctx, s.admin, old.ID, old.Version+1, fix)
		wantErr(t, "stale edit", err, store.ErrVersionConflict)
		_, err = s.EditMovement(ctx, s.admin, old.ID, old.Version, store.NewMovement{Type: domain.Deposit, Amount: 1, From: domain.Person(s.olga), To: domain.Account(domain.KassaCash), Date: nov12})
		wantErr(t, "turn an expense into a deposit", err, store.ErrForbidden)
		_, err = s.EditMovement(ctx, s.actor(s.olga), old.ID, old.Version, fix)
		wantErr(t, "staff edits the salon's expense", err, store.ErrForbidden)

		got, err := s.EditMovement(ctx, s.admin, old.ID, old.Version, fix)
		must(t, err)
		if got.ID == old.ID || got.Amount != 12550 || got.From != domain.Account(domain.KassaCard) || got.ExpenseCategory != "materials" || got.Date != fix.Date || got.Status != domain.Confirmed || got.Note != "материалы" {
			t.Fatalf("corrected row = %+v", got)
		}
		was, err := s.Entry(ctx, old.ID)
		must(t, err)
		if was.Status != domain.Voided || was.VoidReason != store.EditedReason {
			t.Fatalf("old row = %+v, want voided as edited", was)
		}
		_, err = s.EditMovement(ctx, s.admin, old.ID, was.Version, fix)
		wantProblem(t, "edit a voided row", err, store.ProblemAlreadyVoided)

		b, err := s.Balances(ctx)
		must(t, err)
		if b.Accounts[domain.KassaCash] != 0 || b.Accounts[domain.KassaCard] != -12550 {
			t.Fatalf("balances after the edit = %+v", b.Accounts)
		}
		for id, action := range map[string]string{old.ID: store.ActionVoid, got.ID: store.ActionCreate} {
			audits, err := s.Audit(ctx, id)
			must(t, err)
			if len(audits) == 0 || audits[0].Action != action {
				t.Fatalf("audit for %s = %+v, want %s newest", id, audits, action)
			}
		}
	})
}

func TestStaffEditOnlyTheirOwnPendingExpense(t *testing.T) {
	eachSalon(t, func(t *testing.T, s *salon) {
		nov12 := date(t, "2026-11-12")
		own, err := s.RecordMovement(ctx, s.actor(s.olga), store.NewMovement{Type: domain.Expense, Amount: 5000, From: domain.Person(s.olga), To: domain.Outside, ExpenseCategory: "materials", Date: nov12, Note: "перчатки"})
		must(t, err)
		fix := store.NewMovement{Type: domain.Expense, Amount: 4500, From: domain.Person(s.olga), To: domain.Outside, ExpenseCategory: "materials", Date: nov12, Note: "перчатки"}
		_, err = s.EditMovement(ctx, s.actor(s.maria), own.ID, own.Version, store.NewMovement{Type: domain.Expense, Amount: 4500, From: domain.Person(s.maria), To: domain.Outside, ExpenseCategory: "materials", Date: nov12, Note: "x"})
		wantErr(t, "someone else's expense", err, store.ErrForbidden)
		forged := fix
		forged.From = domain.Person(s.maria)
		_, err = s.EditMovement(ctx, s.actor(s.olga), own.ID, own.Version, forged)
		wantErr(t, "move it to someone else", err, store.ErrForbidden)
		got, err := s.EditMovement(ctx, s.actor(s.olga), own.ID, own.Version, fix)
		must(t, err)
		if got.Status != domain.Pending || got.Amount != 4500 || got.PersonID != s.olga {
			t.Fatalf("staff correction = %+v", got)
		}
		ok, err := s.ConfirmMovement(ctx, s.admin, got.ID, got.Version)
		must(t, err)
		_, err = s.EditMovement(ctx, s.actor(s.olga), ok.ID, ok.Version, fix)
		wantErr(t, "edit after confirmation", err, store.ErrForbidden)
	})
}
