package store_test

import (
	"slices"
	"testing"

	"github.com/lieranderl/aestheticlab-income-app/internal/domain"
	"github.com/lieranderl/aestheticlab-income-app/internal/store"
)

func TestCategoriesKeepDeletedIDsReserved(t *testing.T) {
	eachSalon(t, func(t *testing.T, s *salon) {
		c, err := s.CreateCategory(ctx, s.admin, store.Category{ID: "extra", Name: "Дополнительно", Kind: store.KindExpense})
		must(t, err)
		must(t, s.DeleteCategory(ctx, s.admin, c.ID, c.Version))
		_, err = s.CreateCategory(ctx, s.admin, c)
		wantErr(t, "reserved deleted ID", err, store.ErrInvalid)
		cats, err := s.Categories(ctx)
		must(t, err)
		for _, cat := range cats {
			if cat.ID == c.ID {
				t.Fatal("deleted category visible")
			}
		}
	})
}

func TestCategorySettingsCannotOverwriteLaterUse(t *testing.T) {
	eachSalon(t, func(t *testing.T, s *salon) {
		c, err := s.CategoryByID(ctx, "services")
		must(t, err)
		_, err = s.SaveReport(ctx, s.actor(s.olga), day(s.olga, "2026-11-10", t, 10000, 0))
		must(t, err)
		c.Name = "Новое имя"
		wantErr(t, "stale after first use", s.UpdateCategory(ctx, s.admin, c), store.ErrVersionConflict)
		c, err = s.CategoryByID(ctx, "services")
		must(t, err)
		wantProblem(t, "fixed delete", s.DeleteCategory(ctx, s.admin, c.ID, c.Version), "fixed_category")
		wantProblem(t, "fixed archive", s.SetCategoryActive(ctx, s.admin, c.ID, c.Version, false), "fixed_category")
		rent, err := s.CategoryByID(ctx, "rent")
		must(t, err)
		must(t, s.SetCategoryActive(ctx, s.admin, rent.ID, rent.Version, false))
	})
}

func TestCategoryIDsAndFlagsAreValidated(t *testing.T) {
	eachSalon(t, func(t *testing.T, s *salon) {
		for _, id := range []string{"A", "a-b", "a/b", "1abc", "", "abcdefghijklmnopqrstuvwxyzabcdefghijklmnooo"} {
			_, err := s.CreateCategory(ctx, s.admin, store.Category{ID: id, Name: "X", Kind: store.KindExpense})
			wantErr(t, id, err, store.ErrInvalid)
		}
		_, err := s.CreateCategory(ctx, s.admin, store.Category{ID: "bad_sale", Name: "X", Kind: store.KindRevenue, CountsTowardSelf: true, IsCertificateSale: true})
		wantErr(t, "sale/self", err, store.ErrInvalid)
	})
}

func TestCategoryEditsKeepSavedMoneyAndFlags(t *testing.T) {
	eachSalon(t, func(t *testing.T, s *salon) {
		e := store.ReportEntry{Person: s.anna, Date: date(t, "2026-11-10"), Revenue: map[string]domain.Cents{"services": 60000, "creams": 5000, "certificates": 10000}, Payments: map[string]domain.Cents{"card": 60000, "cash": 15000}}
		saved, err := s.SaveReport(ctx, s.admin, e)
		must(t, err)
		if saved.Report.SelfAmount != 32500 || saved.Report.SalonShare != 42500 || saved.Report.SalonIncome != 32500 {
			t.Fatalf("original figures: %+v", saved.Report)
		}
		creams, err := s.CategoryByID(ctx, "creams")
		must(t, err)
		creams.CountsTowardSelf = false
		must(t, s.UpdateCategory(ctx, s.admin, creams))
		card, err := s.CategoryByID(ctx, "card")
		must(t, err)
		card.Destination = domain.ToCash
		must(t, s.UpdateCategory(ctx, s.admin, card))
		loaded, err := s.Report(ctx, s.anna, e.Date)
		must(t, err)
		if loaded.Report.SelfAmount != 32500 || loaded.Report.SalonShare != 42500 || loaded.Report.SalonIncome != 32500 || !loaded.Flags.Revenue["creams"].CountsTowardSelf || loaded.Flags.Payments["card"].Destination != domain.ToKassaCard {
			t.Fatalf("history changed: %+v", loaded)
		}
		e.Version = loaded.Version
		again, err := s.SaveReport(ctx, s.admin, e)
		must(t, err)
		if again.Report.SelfAmount != 32500 || again.Report.CardIn != 60000 {
			t.Fatalf("editing historical report adopted later settings: %+v", again.Report)
		}
	})
}

func TestCategoryReorderAndAuditAreAtomic(t *testing.T) {
	eachSalon(t, func(t *testing.T, s *salon) {
		a, err := s.CategoryByID(ctx, "services")
		must(t, err)
		b, err := s.CategoryByID(ctx, "creams")
		must(t, err)
		wantErr(t, "nonadjacent", s.ReorderCategory(ctx, s.admin, a.ID, a.Version, "goods", 1), store.ErrInvalid)
		must(t, s.ReorderCategory(ctx, s.admin, a.ID, a.Version, b.ID, b.Version))
		fresh, err := s.Categories(ctx)
		must(t, err)
		var revenue []string
		for _, c := range fresh {
			if c.Kind == store.KindRevenue {
				revenue = append(revenue, c.ID)
			}
		}
		if revenue[0] != "creams" || revenue[1] != "services" {
			t.Fatalf("wrong order: %v", revenue)
		}
		wantErr(t, "stale neighbor", s.ReorderCategory(ctx, s.admin, a.ID, a.Version, b.ID, b.Version), store.ErrVersionConflict)
		for _, id := range []string{a.ID, b.ID} {
			if !slices.Contains(s.actions(t, id), store.ActionUpdate) {
				t.Fatalf("missing reorder audit for %s", id)
			}
		}
	})
}

func TestArchivedCategoryAllowsOnlyExistingSavedAmount(t *testing.T) {
	eachSalon(t, func(t *testing.T, s *salon) {
		e := day(s.olga, "2026-11-10", t, 0, 0)
		e.Revenue["other"] = 10000
		e.Payments["card"] = 10000
		saved, err := s.SaveReport(ctx, s.actor(s.olga), e)
		must(t, err)
		cat, err := s.CategoryByID(ctx, "other")
		must(t, err)
		must(t, s.SetCategoryActive(ctx, s.admin, cat.ID, cat.Version, false))
		e.Version = saved.Version
		e.Revenue["other"] = 11000
		e.Payments["card"] = 11000
		wantErr(t, "archived increase", func() error { _, err := s.SaveReport(ctx, s.actor(s.olga), e); return err }(), store.ErrInvalid)
		e.Revenue["other"] = 10000
		e.Payments["card"] = 10000
		_, err = s.SaveReport(ctx, s.actor(s.olga), e)
		must(t, err)
		mariaReport := day(s.maria, "2026-11-11", t, 0, 0)
		mariaReport.Revenue["other"] = 10000
		mariaReport.Payments["card"] = 10000
		_, err = s.SaveReport(ctx, s.actor(s.maria), mariaReport)
		wantErr(t, "new archived use", err, store.ErrInvalid)
	})
}
