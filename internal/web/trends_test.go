package web_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/lieranderl/aestheticlab-income-app/internal/domain"
	"github.com/lieranderl/aestheticlab-income-app/internal/store"
)

func TestTrendsAccessIsAdminOnly(t *testing.T) {
	s := newSite(t)
	staff := s.login("olga", olgaPass)
	if got := s.get("/admin/trends", staff).Code; got != http.StatusForbidden {
		t.Fatalf("staff access to trends: %d, want 403", got)
	}
	admin := s.login("anna", annaPass)
	if got := s.get("/admin/trends", admin); got.Code != http.StatusOK {
		t.Fatalf("admin access to trends: %d, want 200", got.Code)
	}
}

func TestTrendsPeriodAndMetricSelection(t *testing.T) {
	s := newSite(t)
	admin := s.login("anna", annaPass)

	// Default: 6 months, revenue
	r := s.get("/admin/trends", admin)
	if r.Code != http.StatusOK {
		t.Fatalf("status %d", r.Code)
	}
	body := r.Body.String()
	if !strings.Contains(body, "Выручка") {
		t.Fatal("missing Выручка metric")
	}

	// 3 months, clients
	r = s.get("/admin/trends?period=3&metric=clients", admin)
	if r.Code != http.StatusOK {
		t.Fatalf("status %d", r.Code)
	}
	body = r.Body.String()
	if !strings.Contains(body, "Клиенты") {
		t.Fatal("missing Клиенты")
	}

	// 12 months, person_revenue with person=olga
	r = s.get("/admin/trends?period=12&metric=person_revenue&person="+s.olga.ID, admin)
	if r.Code != http.StatusOK {
		t.Fatalf("status %d", r.Code)
	}
	body = r.Body.String()
	if !strings.Contains(body, "Выручка: Ольга") {
		t.Fatalf("missing person header: %s", body)
	}

	// Invalid metric returns 400
	if got := s.get("/admin/trends?metric=invalid", admin).Code; got != http.StatusBadRequest {
		t.Fatalf("invalid metric: %d, want 400", got)
	}
}

func TestTrendsImportedMonthsAndNegativeProfit(t *testing.T) {
	s := newSite(t)
	adminUser, err := s.st.UserByUsername(ctx, "anna")
	if err != nil {
		t.Fatal(err)
	}
	adminActor := store.Actor{UID: adminUser.ID, Admin: true}

	// Save an imported month before start month (e.g. 2026-09)
	im := domain.ImportedMonth{
		RevenueTotal: 1200000,
		SalonIncome:  600000,
		Expenses:     700000,
		Profit:       -100000, // Negative profit
		Dividends:    0,
	}
	if err := s.st.SaveImportedMonth(ctx, adminActor, domain.Month{Year: 2026, Mon: 9}, im, "test sheet"); err != nil {
		t.Fatal(err)
	}

	admin := s.login("anna", annaPass)
	r := s.get("/admin/trends?period=6&metric=profit", admin)
	if r.Code != http.StatusOK {
		t.Fatalf("status %d: %s", r.Code, r.Body.String())
	}
	body := r.Body.String()
	if !strings.Contains(body, "импорт") {
		t.Fatal("missing import badge")
	}
	if !strings.Contains(body, domain.FormatEUR(im.Profit)) {
		t.Fatalf("missing negative profit %q in table: %s", domain.FormatEUR(im.Profit), body)
	}
}

// The default «Обзор» puts revenue, expenses and profit on one bar chart with a
// euro axis, and selects the latest month with data so its exact figures show.
func TestTrendsOverviewBarsAxisAndSelectedMonth(t *testing.T) {
	s := newSite(t)
	adminUser, err := s.st.UserByUsername(ctx, "anna")
	if err != nil {
		t.Fatal(err)
	}
	im := domain.ImportedMonth{RevenueTotal: 1200000, SalonIncome: 600000, Expenses: 700000, Profit: -100000}
	if err := s.st.SaveImportedMonth(ctx, store.Actor{UID: adminUser.ID, Admin: true}, domain.Month{Year: 2026, Mon: 9}, im, "test sheet"); err != nil {
		t.Fatal(err)
	}

	body := s.get("/admin/trends?period=3&m=2026-09", s.login("anna", annaPass)).Body.String()
	eur := func(c domain.Cents) string { return domain.FormatEUR(c) }
	for _, want := range []string{
		`aria-current="page">Обзор</a>`,
		// Legend and the selected month's readout.
		"Выручка", "Расходы", "Прибыль", "Сентябрь 2026",
		eur(1200000), eur(700000), eur(-100000),
		// Axis from −€5 000 to €15 000 in €5 000 steps.
		">" + eur(-500000) + "</text>", ">€0</text>", ">" + eur(500000) + "</text>", ">" + eur(1000000) + "</text>", ">" + eur(1500000) + "</text>",
		// Selection: September is the third month in the window (index 2). Indices
		// live in data-* attributes, never inside Datastar expressions.
		`data-selected="2"`, `data-month="2"`, `data-on:click="$_month = +el.dataset.month"`,
		`aria-label="Предыдущий месяц"`, `aria-label="Следующий месяц"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("trends page is missing %q", want)
		}
	}
	if strings.Contains(body, "{_month: ") || strings.Contains(body, "$_month = 2") || strings.Contains(body, "$_month !== 2") {
		t.Error("a month index is interpolated into a Datastar expression")
	}
	if strings.Contains(body, "<polyline") {
		t.Error("trends page still draws a line instead of bars")
	}
}

func TestTrendsUnknownPersonFallsBackToFirstPerson(t *testing.T) {
	s := newSite(t)
	admin := s.login("anna", annaPass)
	body := s.get("/admin/trends?metric=person_revenue&person=nobody%26period%3D12", admin).Body.String()
	if strings.Contains(body, "nobody") {
		t.Fatal("an unknown person id is echoed into the page links")
	}
	if want := s.get("/admin/trends?metric=person_revenue", admin).Body.String(); body != want {
		t.Fatal("an unknown person does not fall back to the default (first) person")
	}
}
