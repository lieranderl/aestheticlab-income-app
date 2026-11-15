package web_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/lieranderl/aestheticlab-income-app/internal/domain"
	"github.com/lieranderl/aestheticlab-income-app/internal/store"
)

// 2026-11-15 design review: every view switcher (month, history, category
// kind, trend period, movement type) is the same full-width tab box.
func TestViewSwitchersShareOneTabStyle(t *testing.T) {
	s := newSite(t)
	admin := s.login("anna", annaPass)
	for _, path := range []string{"/admin/month", "/admin/history", "/admin/categories", "/admin/trends", "/admin/movements/new"} {
		body := s.get(path, admin).Body.String()
		if !strings.Contains(body, `class="tabs tabs-box w-full`) {
			t.Errorf("%s: no full-width tab box", path)
		}
		if strings.Contains(body, "join-item btn") {
			t.Errorf("%s: still uses joined buttons as a view switcher", path)
		}
	}
}

// 2026-11-15 design review: at lg: movement lists read as a table, laid out
// from the same elements, so each movement still has exactly one confirm
// form and one void dialog (a duplicated table would open two).
func TestMovementListIsATableOnDesktopWithoutDuplicates(t *testing.T) {
	s := newSite(t)
	if _, err := s.st.RecordMovement(ctx, s.olga.Actor(), store.NewMovement{Type: domain.Deposit, Amount: 100, From: domain.Person(s.olga.ID), To: domain.Account(domain.KassaCash), Date: domain.Date{Year: 2026, Mon: 11, Day: 20}}); err != nil {
		t.Fatal(err)
	}
	body := s.get("/admin/kassa", s.login("anna", annaPass)).Body.String()
	if !strings.Contains(body, `<span>Дата</span><span>Движение</span><span class="text-right">Сумма</span>`) {
		t.Error("no desktop column header over the movement list")
	}
	ids := regexp.MustCompile(`id="movement-action-([^"]+)-confirm"`).FindAllStringSubmatch(body, -1)
	if len(ids) != 1 {
		t.Fatalf("want one confirm form for the pending movement, got %d", len(ids))
	}
	if n := strings.Count(body, `class="modal modal-bottom sm:modal-middle" data-ledger="`+ids[0][1]+`"`); n != 1 {
		t.Errorf("the pending movement has %d void dialogs, want 1", n)
	}
}
