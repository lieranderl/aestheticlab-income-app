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

// 2026-11-15 production review: 36 of 50 change-log cards were empty. A
// client save bumps each category's usage counter, which is not a change
// anyone made; and a booking link is a real change the card did not show.
func TestChangeLogHasNoEmptyCards(t *testing.T) {
	s := newSite(t)
	if err := s.st.SetTerms(ctx, s.anna.Actor(), s.olga.ID, s.st.StartMonth(), domain.Terms{SelfPct: 5000, DividendWeight: 1}, ""); err != nil {
		t.Fatal(err)
	}
	saved, err := s.st.SaveClientReport(ctx, s.anna.Actor(), store.ClientEntry{
		Token: "test-client-token-change-log", Person: s.olga.ID, Date: domain.Date{Year: 2026, Mon: 11, Day: 20},
		Input: domain.ClientInput{Services: 10000, ServiceCash: 5000, Extras: map[string]domain.Cents{"creams": 1200}, ExtrasDestination: domain.ToCash},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.st.LinkClientBooking(ctx, s.anna.Actor(), saved.ID, saved.Version, domain.BookingRef{Calendar: "cal", Event: "ev"}); err != nil {
		t.Fatal(err)
	}
	body := s.get("/admin/audit?from=2026-11-20&to=2026-11-20", s.login("anna", annaPass)).Body.String()
	for _, card := range regexp.MustCompile(`(?s)<article\b.*?</article>`).FindAllString(body, -1) {
		if !strings.Contains(card, "<dl") && !strings.Contains(card, "<details") {
			t.Errorf("empty change-log card: %s", regexp.MustCompile(`<[^>]+>`).ReplaceAllString(card, " "))
		}
	}
	if !strings.Contains(body, "Запись в календаре") {
		t.Error("the booking link is not shown")
	}
}
