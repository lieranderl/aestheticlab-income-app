package web_test

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/lieranderl/aestheticlab-income-app/internal/domain"
	"github.com/lieranderl/aestheticlab-income-app/internal/store"
	"github.com/lieranderl/aestheticlab-income-app/internal/web"
	"golang.org/x/crypto/bcrypt"
)

func TestDashboardSeparatesPeriodContributionsFromCurrentSettlementBalance(t *testing.T) {
	s := newSite(t)
	s.withTerms(s.olga, 7000)
	s.withTerms(s.maria, 7000)
	if err := s.st.SetTerms(ctx, s.anna.Actor(), s.anna.ID, s.st.StartMonth(), domain.Terms{SelfPct: 5000, SalaryCents: 100000}, ""); err != nil {
		t.Fatal(err)
	}
	cookie := s.login("anna", annaPass)
	saved := s.post("/clients", url.Values{
		"date": {"2026-11-20"}, "token": {"dashboard-clarity-example"}, "version": {"0"},
		"services": {"100"}, "creams": {"20"}, "certificatePaid": {"40"},
		"serviceCash": {"30"}, "extrasDestination": {"cash"},
	}, cookie)
	if saved.Code != http.StatusSeeOther {
		t.Fatalf("save example: %d %s", saved.Code, saved.Body.String())
	}
	page := s.get("/admin/month?m=2026-11", cookie)
	wantPage(t, "grouped month", page, http.StatusOK, "Внесено в кассу за месяц", "Сертификаты", "€120", "€50", "€70", "€40")
	primary := regexp.MustCompile(`(?s)<section[^>]*aria-label="Итоги"[^>]*>(.*?)</section>`).FindStringSubmatch(page.Body.String())
	if len(primary) != 2 {
		t.Fatal("missing primary totals region")
	}
	previous := -1
	for _, label := range []string{"Выручка", "«Себе»", "«Салону»", "Клиенты"} {
		index := strings.Index(primary[1], label)
		if index <= previous {
			t.Fatalf("primary totals are not ordered: %s", label)
		}
		previous = index
	}
	if strings.Contains(primary[1], "Кремы") || strings.Contains(primary[1], "Касса") {
		t.Fatal("category or contribution detail competes with primary totals")
	}
	settlement := s.get("/admin/month?m=2026-11&tab=settlement", cookie)
	wantPage(t, "realized action", settlement, http.StatusOK, "Текущий остаток", "Выплатить €20", "Предварительный итог месяца", "€1\u202f020")
	body := ""
	for _, card := range regexp.MustCompile(`(?s)<article\b.*?</article>`).FindAllString(settlement.Body.String(), -1) {
		if strings.Contains(card, "Выплатить €20") {
			body = card
			break
		}
	}
	current, action, projected := strings.Index(body, "Текущий остаток"), strings.Index(body, "Выплатить €20"), strings.Index(body, "Предварительный итог месяца")
	if current < 0 || action < current || projected < action {
		t.Fatal("current balance and its action must precede the projected month balance")
	}
}

func TestLegacyCategoryNamedLikeATotalRemainsInRevenueDetails(t *testing.T) {
	clock := time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)
	now := func() time.Time { return clock }
	st := store.NewMemory(store.WithClock(now))
	s := &site{t: t, st: st, clock: &clock}
	setup := store.DefaultSetup(store.NewUser{
		Username: "anna", DisplayName: "Анна", Role: store.RoleAdmin,
		SubmitsReports: true, PasswordHash: hash(t, annaPass),
	})
	for i := range setup.Categories {
		if setup.Categories[i].ID == "services" {
			setup.Categories[i].Name = "Касса — наличные"
		}
	}
	setup.Categories = append(setup.Categories, store.Category{
		ID: "legacy_total", Name: "Выручка", Kind: store.KindRevenue,
		Active: true, CountsTowardSelf: true,
	})
	anna, err := st.SetUp(ctx, setup)
	if err != nil {
		t.Fatal(err)
	}
	s.anna = s.settle(anna)
	s.olga = s.person("olga", "Ольга", olgaPass)
	s.h = handler(t, web.Config{
		BaseURL: "https://kassa.example.com", Store: st, Now: now, PasswordCost: bcrypt.MinCost,
		SessionKey: []byte("test-session-key-test-session-key-0123456789"),
	})
	s.withTerms(s.olga, 7000)
	s.saveDay(s.olga, 20, map[string]domain.Cents{"services": 1000, "legacy_total": 2000}, map[string]domain.Cents{"cash": 3000})
	cookie := s.login("anna", annaPass)
	for _, path := range []string{"/admin/day?date=2026-11-20", "/admin/month?m=2026-11"} {
		page := s.get(path, cookie)
		wantPage(t, "legacy category detail", page, http.StatusOK,
			"Услуги и продажи", "Категория · Выручка", ">€20</dd>", "Категория · Касса — наличные", ">€10</dd>")
	}
	*s.clock = s.clock.AddDate(0, 0, 15)
	if _, err := s.st.CloseMonth(ctx, s.anna.Actor(), domain.Month{Year: 2026, Mon: 11}); err != nil {
		t.Fatal(err)
	}
	wantPage(t, "frozen legacy category detail", s.get("/admin/month?m=2026-11", cookie), http.StatusOK,
		"Категория · Выручка", ">€20</dd>", "Категория · Касса — наличные", ">€10</dd>")
}

func TestMobileTouchTargetCompliance(t *testing.T) {
	s := newSite(t)
	olga := s.login("olga", olgaPass)
	dayPage := s.get("/", olga)
	if !strings.Contains(dayPage.Body.String(), "btn-sm btn-primary min-h-11") || !strings.Contains(dayPage.Body.String(), "min-h-11 min-w-11") {
		t.Fatal("install banner does not meet >=44pt touch target standard")
	}

	admin := s.login("anna", annaPass)
	trendsPage := s.get("/admin/trends", admin)
	if !strings.Contains(trendsPage.Body.String(), "btn btn-sm min-h-11") {
		t.Fatal("trends metric selector does not meet >=44pt touch target standard")
	}

	exportPage := s.get("/admin/export", admin)
	if strings.Contains(exportPage.Body.String(), "input-bordered") {
		t.Fatal("export page uses deprecated input-bordered")
	}
	if !strings.Contains(exportPage.Body.String(), "input input-lg w-full min-h-11 text-base") {
		t.Fatal("export page inputs missing standard daisyUI 5 input input-lg")
	}
}

func TestCategoryPageShowsGuidanceForFixedCategories(t *testing.T) {
	s := newSite(t)
	admin := s.login("anna", annaPass)
	page := s.get("/admin/categories/services", admin)
	wantPage(t, "fixed category notice", page, http.StatusOK,
		"Базовую категорию нельзя архивировать или удалить. Новое название видно в категориях и отчётах за день, но не в форме записи клиента.")
}

// iOS WebKit (Safari and Chrome alike) ignores showPicker() on a hidden date
// input, so the input itself must sit on top and take the tap.
func TestDateSwitcherDateInputTakesTheTap(t *testing.T) {
	s := newSite(t)
	olga := s.login("olga", olgaPass)
	page := s.get("/", olga)
	if page.Code != http.StatusOK {
		t.Fatalf("get /: %d", page.Code)
	}
	body := page.Body.String()
	if !strings.Contains(body, `type="date" name="date"`) || !strings.Contains(body, `class="date-overlay peer absolute inset-0 z-10 h-full w-full cursor-pointer appearance-none text-base opacity-0" aria-label="Выбрать день"`) {
		t.Fatal("date switcher input is not the tappable overlay")
	}
	for _, stale := range []string{"pointer-events-none absolute inset-0 -z-10", "showPicker", `tabindex="-1" aria-label="Выбрать день"`} {
		if strings.Contains(body, stale) {
			t.Fatalf("date switcher still hides the input behind the label: %q", stale)
		}
	}
	if !strings.Contains(body, `data-on:input="el.value && el.form.submit()" data-on:change="el.value && el.form.submit()"`) {
		t.Fatal("date switcher input missing input/change submission handlers")
	}
}
