package web_test

import (
	"encoding/json"
	"github.com/lieranderl/aestheticlab-income-app/internal/domain"
	"github.com/lieranderl/aestheticlab-income-app/internal/store"
	"golang.org/x/net/html"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"
)

func TestClientFormSavesServicesExtrasAndShowsCreationTime(t *testing.T) {
	s := newSite(t)
	s.withTerms(s.olga, 7000)
	z := s.login("olga", olgaPass)
	wantPage(t, "client form", s.get("/clients/new?date=2026-11-20", z), http.StatusOK, "Клиент 1", `name="services"`, `name="creams"`, `name="oil"`, `name="certificates"`, `name="certificatePaid"`, `name="serviceCash"`, `name="serviceCard"`)
	form := url.Values{"date": {"2026-11-20"}, "token": {"http-client-token"}, "version": {"0"}, "services": {"100"}, "creams": {"20"}, "extrasDestination": {"cash"}, "serviceCash": {"30"}}
	res := s.post("/clients", form, z)
	if res.Code != http.StatusSeeOther {
		t.Fatalf("save %d: %s", res.Code, res.Body.String())
	}
	wantPage(t, "daily clients", s.get("/?date=2026-11-20", z), http.StatusOK, "Клиент 1", "11:00", "€120", "€50")
	wantPage(t, "my clients", s.get("/my", z), http.StatusOK, "Клиент 1")
}

func TestClientRetryEditPrivacyAndTypedFailure(t *testing.T) {
	s := newSite(t)
	s.withTerms(s.olga, 7000)
	z := s.login("olga", olgaPass)
	form := url.Values{"date": {"2026-11-20"}, "token": {"http-client-retry-token"}, "version": {"0"}, "services": {"100"}, "creams": {"20"}, "extrasDestination": {"cash"}, "serviceCash": {"30"}}
	first := s.post("/clients", form, z)
	href := first.Header().Get("Location")
	retry := s.post("/clients", form, z)
	if retry.Header().Get("Location") != href {
		t.Fatalf("retry duplicated: %s %s", href, retry.Header().Get("Location"))
	}
	form.Set("creams", "30")
	wantPage(t, "changed replay", s.post("/clients", form, z), http.StatusUnprocessableEntity, "повтор отличается")
	other := s.login("maria", mariaPass)
	wantPage(t, "private client", s.get(href, other), http.StatusForbidden, "Нет доступа")
	form.Set("version", "1")
	form.Set("date", "2026-11-19")
	form.Set("token", "tampered-owner-token")
	form.Set("services", "oops")
	wantPage(t, "typed failure", s.post(strings.Split(href, "?")[0], form, z), http.StatusUnprocessableEntity, `value="oops"`)
	form.Set("services", "100")
	updated := s.post(strings.Split(href, "?")[0], form, z)
	if updated.Code != http.StatusSeeOther {
		t.Fatalf("edit %d %s", updated.Code, updated.Body.String())
	}
	wantPage(t, "stale", s.post(strings.Split(href, "?")[0], form, z), http.StatusUnprocessableEntity, "Запись изменена")
	wantPage(t, "creation preserved", s.get("/?date=2026-11-20", z), http.StatusOK, "Клиент 1", "11:00", "€130")
}

func TestClientZeroCertificatesValidationAdminAndClosedMonth(t *testing.T) {
	s := newSite(t)
	s.withTerms(s.olga, 7000)
	z := s.login("olga", olgaPass)
	admin := s.login("anna", annaPass)
	form := url.Values{"date": {"2026-11-20"}, "token": {"certificate-http-token"}, "version": {"0"}, "services": {"100"}, "certificatePaid": {"100"}, "extrasDestination": {"cash"}}
	res := s.do(request{method: http.MethodPost, path: "/clients", form: form, cookies: z, datastar: true})
	wantPage(t, "SSE create", res, http.StatusOK, "datastar-patch-signals", "redirectUrl")
	wantPage(t, "certificate daily", s.get("/?date=2026-11-20", z), http.StatusOK, "Салон должен вам €70", "Касса — наличные")
	form.Set("token", "invalid-http-client-token")
	form.Set("certificatePaid", "101")
	wantPage(t, "redemption over service", s.post("/clients", form, z), http.StatusUnprocessableEntity, "использование сертификата не больше услуг")
	form.Set("certificatePaid", "40")
	form.Set("serviceCash", "61")
	wantPage(t, "over contribution", s.post("/clients", form, z), http.StatusUnprocessableEntity, "взносы за услуги не больше услуг")
	form.Set("token", "all-zero-http-client-token")
	form.Set("services", "")
	form.Set("certificatePaid", "")
	form.Set("serviceCash", "")
	zero := s.post("/clients", form, z)
	if zero.Code != http.StatusSeeOther {
		t.Fatalf("zero %d %s", zero.Code, zero.Body.String())
	}
	href := strings.Split(zero.Header().Get("Location"), "?")[0]
	wantPage(t, "admin can edit", s.get(href, admin), http.StatusOK, "Клиент 2")
	form.Set("version", "1")
	form.Set("services", "100")
	form.Set("date", "2026-11-01")
	form.Set("token", "untrusted-edit-token")
	if got := s.post(href, form, admin); got.Code != http.StatusSeeOther {
		t.Fatalf("admin edit %d %s", got.Code, got.Body.String())
	}
	*s.clock = s.clock.AddDate(0, 0, 15)
	if _, err := s.st.CloseMonth(ctx, s.anna.Actor(), domain.Month{Year: 2026, Mon: 11}); err != nil {
		t.Fatal(err)
	}
	wantPage(t, "locked client", s.get(href, z), http.StatusOK, "Месяц закрыт", "disabled")
	form.Set("version", "2")
	wantPage(t, "locked post", s.post(href, form, z), http.StatusUnprocessableEntity, "Месяц закрыт")
}

func TestNewLegacyReportIsRefusedAndNotShownAsDailyForm(t *testing.T) {
	s := newSite(t)
	s.withTerms(s.olga, 7000)
	z := s.login("olga", olgaPass)
	wantPage(t, "new daily forbidden", s.post("/reports/2026-11-20", url.Values{"version": {"0"}, "r_services": {"100"}, "p_cash": {"100"}}, z), http.StatusUnprocessableEntity, "добавьте клиента")
	wantPage(t, "no legacy form", s.get("/reports/day?date=2026-11-20", z), http.StatusNotFound, "404")
	wantPage(t, "new home", s.get("/", z), http.StatusOK, "Добавить клиента")
}

func TestConcurrentSameClientCreatesOnlyOneHTTPRecord(t *testing.T) {
	s := newSite(t)
	s.withTerms(s.olga, 7000)
	z := s.login("olga", olgaPass)
	form := url.Values{"date": {"2026-11-20"}, "token": {"concurrent-http-client-token"}, "version": {"0"}, "services": {"100"}, "extrasDestination": {"cash"}}
	replies := make(chan string, 8)
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			res := s.post("/clients", form, z)
			if res.Code != http.StatusSeeOther {
				t.Errorf("concurrent save status %d", res.Code)
			}
			replies <- res.Header().Get("Location")
		})
	}
	wg.Wait()
	close(replies)
	href := ""
	for got := range replies {
		if href == "" {
			href = got
		}
		if got != href {
			t.Errorf("duplicate client %s %s", href, got)
		}
	}
	rows, err := s.st.PersonReportsIn(ctx, s.olga.ID, domain.Month{Year: 2026, Mon: 11})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ClientNumber != 1 || rows[0].Version != 1 {
		t.Fatalf("duplicate reports %+v", rows)
	}
}

func TestClientDayNavigationStaysOnTheClientList(t *testing.T) {
	s := newSite(t)
	s.withTerms(s.olga, 7000)
	z := s.login("olga", olgaPass)
	page := s.get("/?date=2026-11-02", z)
	wantPage(t, "client day arrows", page, http.StatusOK, `href="/?date=2026-11-01"`, `href="/?date=2026-11-03"`, `action="/"`, `href="/clients/new?date=2026-11-02"`)
	if strings.Contains(page.Body.String(), `href="/reports/day?date=`) {
		t.Fatal("empty client day navigation opens legacy report")
	}
}

func TestMalformedClientEditNeverUpgradesItsVersion(t *testing.T) {
	s := newSite(t)
	s.withTerms(s.olga, 7000)
	z := s.login("olga", olgaPass)
	form := url.Values{"date": {"2026-11-20"}, "token": {"stale-validation-client-token"}, "version": {"0"}, "services": {"100"}, "extrasDestination": {"cash"}}
	saved := s.post("/clients", form, z)
	href := strings.Split(saved.Header().Get("Location"), "?")[0]
	form.Set("version", "1")
	form.Set("services", "200")
	if got := s.post(href, form, z); got.Code != http.StatusSeeOther {
		t.Fatal(got.Body.String())
	}
	for _, version := range []string{"1", "oops", "-1"} {
		form.Set("version", version)
		form.Set("services", "typo")
		res := s.post(href, form, z)
		wantPage(t, "malformed stale edit", res, http.StatusUnprocessableEntity, `value="typo"`, `aria-invalid="true"`)
		if strings.Contains(res.Body.String(), `name="version" value="2"`) {
			t.Fatal("invalid edit granted latest stored version")
		}
		form.Set("services", "300")
		wantPage(t, "corrected stale edit refused", s.post(href, form, z), http.StatusUnprocessableEntity, "Запись изменена")
	}
	wantPage(t, "latest preserved", s.get(href, z), http.StatusOK, `name="services" value="200"`)
}

func TestIndependentNewClientURLsSaveIdenticalAmountsAsDistinctClients(t *testing.T) {
	s := newSite(t)
	s.withTerms(s.olga, 7000)
	z := s.login("olga", olgaPass)
	paths := []string{}
	for range 2 {
		res := s.do(request{method: http.MethodGet, path: "/clients/new?date=2026-11-20", cookies: z})
		if res.Code != http.StatusSeeOther {
			t.Fatalf("missing canonical identity redirect %d", res.Code)
		}
		paths = append(paths, res.Header().Get("Location"))
	}
	if paths[0] == paths[1] {
		t.Fatal("independent forms share canonical create identity")
	}
	for _, path := range paths {
		u, err := url.Parse(path)
		if err != nil {
			t.Fatal(err)
		}
		token := u.Query().Get("token")
		wantPage(t, "canonical reload", s.get(path, z), http.StatusOK, `name="token" value="`+token+`"`)
		form := url.Values{"date": {"2026-11-20"}, "token": {token}, "version": {"0"}, "services": {"100"}, "extrasDestination": {"cash"}}
		if res := s.post("/clients", form, z); res.Code != http.StatusSeeOther {
			t.Fatal(res.Body.String())
		}
	}
	rows, err := s.st.PersonReportsIn(ctx, s.olga.ID, domain.Month{Year: 2026, Mon: 11})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("identical real clients collapsed: %d", len(rows))
	}
}

func TestClientMonthAndAuditDescribeActualContributionsOnly(t *testing.T) {
	s := newSite(t)
	s.withTerms(s.olga, 7000)
	z := s.login("olga", olgaPass)
	admin := s.login("anna", annaPass)
	form := url.Values{"date": {"2026-11-20"}, "token": {"actual-stats-client-token"}, "version": {"0"}, "services": {"100"}, "extrasDestination": {"cash"}}
	saved := s.post("/clients", form, z)
	href := strings.Split(saved.Header().Get("Location"), "?")[0]
	form.Set("version", "1")
	form.Set("services", "200")
	s.post(href, form, z)
	body := s.get("/admin/month", admin).Body.String()
	if strings.Contains(body, "По способам оплаты") {
		t.Fatal("client month claims uncollected payment method breakdown")
	}
	desktop := strings.Split(body, `<div class="hidden overflow-x-auto rounded-box bg-base-100 lg:block">`)
	if len(desktop) != 2 || !strings.Contains(desktop[1], "Клиенты") || !strings.Contains(desktop[1], "Касса — карта салона") {
		t.Fatal("desktop omits client count or actual contribution details")
	}
	phone := strings.Split(strings.Split(body, `<div class="flex flex-col gap-3 lg:hidden">`)[1], `<div class="hidden overflow-x-auto`)[0]
	if strings.Count(phone, ">Выручка</dt>") != 1 {
		t.Fatal("person month revenue duplicated")
	}
	audit := s.get("/admin/audit?entity=report", admin).Body.String()
	if strings.Contains(audit, "Оплата · Наличные") {
		t.Fatal("audit claims change to uncollected customer cash payment")
	}
	day := s.get("/admin/day?date=2026-11-20", admin).Body.String()
	if strings.Contains(day, "Переводом на карту салона") {
		t.Fatal("client day shows obsolete transfer payment tile")
	}
}

func TestClientPreviewSignalsSurviveHTMLAttributeNormalization(t *testing.T) {
	s := newSite(t)
	s.withTerms(s.olga, 7000)
	z := s.login("olga", olgaPass)
	page := s.get("/clients/new?date=2026-11-20", z)
	tokenizer := html.NewTokenizer(strings.NewReader(page.Body.String()))
	definitions := map[string]bool{}
	var expressions []string
	inside := false
	for {
		kind := tokenizer.Next()
		if kind == html.ErrorToken {
			break
		}
		token := tokenizer.Token()
		if kind == html.EndTagToken && token.Data == "form" && inside {
			break
		}
		if kind != html.StartTagToken && kind != html.SelfClosingTagToken {
			continue
		}
		if token.Data == "form" {
			for _, a := range token.Attr {
				if a.Key == "id" && a.Val == "client-form" {
					inside = true
				}
			}
		}
		if !inside {
			continue
		}
		for _, a := range token.Attr {
			if a.Key == "data-signals" {
				var initial map[string]any
				if err := json.Unmarshal([]byte(a.Val), &initial); err != nil {
					t.Fatal(err)
				}
				for name := range initial {
					definitions[name] = true
				}
			}
			if strings.HasPrefix(a.Key, "data-computed:") {
				parts := strings.Split(strings.TrimPrefix(a.Key, "data-computed:"), "-")
				name := parts[0]
				for _, p := range parts[1:] {
					if p != "" {
						name += strings.ToUpper(p[:1]) + p[1:]
					}
				}
				definitions[name] = true
			}
			if strings.HasPrefix(a.Key, "data-") {
				expressions = append(expressions, a.Val)
			}
		}
	}
	refs := regexp.MustCompile(`\$(_[A-Za-z][A-Za-z0-9_]*)`)
	for _, expression := range expressions {
		for _, match := range refs.FindAllStringSubmatch(expression, -1) {
			if !definitions[match[1]] {
				t.Errorf("browser-normalized form never defines preview signal %s", match[1])
			}
		}
	}
	if !strings.Contains(page.Body.String(), `data-effect="if ([`) {
		t.Fatal("optional client fields are not connected to the draft visibility effect")
	}
	if len(definitions) < 10 {
		t.Fatal("client form signals not inspected")
	}
}

func TestClientServicesAutofocusAndBackLinkTouchTarget(t *testing.T) {
	s := newSite(t)
	s.withTerms(s.olga, 7000)
	z := s.login("olga", olgaPass)
	inspect := func(body string) (bool, string) {
		t.Helper()
		tokenizer := html.NewTokenizer(strings.NewReader(body))
		focus := false
		backClass := ""
		for {
			kind := tokenizer.Next()
			if kind == html.ErrorToken {
				break
			}
			token := tokenizer.Token()
			attrs := map[string]string{}
			for _, a := range token.Attr {
				attrs[a.Key] = a.Val
			}
			if token.Data == "input" && attrs["name"] == "services" {
				_, focus = attrs["autofocus"]
			}
			if token.Data == "a" && attrs["aria-label"] == "Назад" {
				backClass = attrs["class"]
			}
		}
		return focus, backClass
	}
	focus, back := inspect(s.get("/clients/new?date=2026-11-20", z).Body.String())
	if !focus {
		t.Fatal("new client services does not receive initial focus")
	}
	if !strings.Contains(back, "min-w-11") || !strings.Contains(back, "min-h-11") {
		t.Fatalf("back target is below44pt: %s", back)
	}
	form := url.Values{"date": {"2026-11-20"}, "token": {"autofocus-http-client-token"}, "version": {"0"}, "services": {"100"}, "extrasDestination": {"cash"}}
	res := s.post("/clients", form, z)
	focus, _ = inspect(s.get(res.Header().Get("Location"), z).Body.String())
	if focus {
		t.Fatal("saved client edit steals initial focus")
	}
}

func TestPrimaryDaySummaryAlwaysShowsActualKassaTotals(t *testing.T) {
	s := newSite(t)
	s.withTerms(s.olga, 7000)
	admin := s.login("anna", annaPass)
	primary := func(body string) string {
		t.Helper()
		tokens := html.NewTokenizer(strings.NewReader(body))
		active := false
		var content strings.Builder
		for {
			kind := tokens.Next()
			if kind == html.ErrorToken {
				break
			}
			token := tokens.Token()
			if kind == html.StartTagToken && token.Data == "section" {
				for _, a := range token.Attr {
					if a.Key == "aria-label" && a.Val == "Итоги" {
						active = true
					}
				}
			}
			if active && kind == html.EndTagToken && token.Data == "section" {
				break
			}
			if active && kind == html.TextToken {
				content.WriteString(token.Data)
				content.WriteByte('\n')
			}
		}
		return content.String()
	}
	check := func(label, cash string) {
		t.Helper()
		body := s.get("/admin/day?date=2026-11-20", admin).Body.String()
		text := primary(body)
		for _, old := range []string{"Наличные", "Карта", "Наличными в кассу", "Переводом на карту салона"} {
			if strings.Contains(text, "\n"+old+"\n") {
				t.Errorf("%s primary summary still shows obsolete %s", label, old)
			}
		}
		for _, current := range []string{"Выручка", "«Себе»", "«Салону»", "Клиенты"} {
			if !strings.Contains(text, current) {
				t.Errorf("%s lacks %s", label, current)
			}
		}
		for _, detail := range []string{"Касса — наличные", "Касса — карта салона", "Оплачено сертификатами"} {
			if strings.Contains(text, detail) {
				t.Errorf("%s primary totals contain contribution detail %s", label, detail)
			}
		}
		contributions := ""
		for _, section := range regexp.MustCompile(`(?s)<section\b.*?</section>`).FindAllString(body, -1) {
			if strings.Contains(section, "Внесено в кассу за день") {
				contributions = section
				break
			}
		}
		for _, value := range []string{"Касса — наличные", "Касса — карта салона", cash, "€0"} {
			if !strings.Contains(contributions, value) {
				t.Errorf("%s contribution section lacks %s", label, value)
			}
		}
		if !strings.Contains(body, ">Сертификаты</summary>") || !strings.Contains(body, "Оплачено сертификатами") {
			t.Errorf("%s lacks separate certificate activity", label)
		}
	}
	check("empty day", "€0")
	s.saveDay(s.olga, 20, map[string]domain.Cents{"services": 10000}, map[string]domain.Cents{"cash": 10000})
	check("legacy only", "€30")
	z := s.login("olga", olgaPass)
	s.post("/clients", url.Values{"date": {"2026-11-20"}, "token": {"primary-summary-client-token"}, "version": {"0"}, "services": {"100"}, "extrasDestination": {"cash"}, "serviceCash": {"30"}}, z)
	check("client and legacy", "€60")
}

func TestClientServiceInputsAreDistinctFromCombinedKassaTotals(t *testing.T) {
	s := newSite(t)
	s.withTerms(s.olga, 7000)
	z := s.login("olga", olgaPass)
	wantPage(t, "service inputs and combined totals", s.get("/clients/new?date=2026-11-20", z), http.StatusOK, "За услуги — наличными", "За услуги — на карту салона", "Итого внесено в кассу", "Взносы за услуги и дополнительные продажи.")
}

func TestClientFormExplainsShareAndRemainingContribution(t *testing.T) {
	s := newSite(t)
	s.withTerms(s.olga, 5000)
	z := s.login("olga", olgaPass)
	form := url.Values{
		"date": {"2026-11-20"}, "token": {"client-guidance-token"},
		"version": {"0"}, "services": {"60"},
	}
	saved := s.post("/clients", form, z)
	wantPage(t, "saved client", saved, http.StatusSeeOther)
	page := s.get(saved.Header().Get("Location"), z)
	wantPage(t, "share rule and outstanding contribution", page, http.StatusOK,
		"Распределение по вашим условиям", "Ваша доля от услуг — 50%.",
		"По введённым данным", "Оставьте в кассе €30",
		"Только по этой записи, без учёта общего баланса.")
	if strings.Contains(page.Body.String(), ">Результат</h2>") {
		t.Fatal("share rule is presented as a completed result")
	}
	admin := s.login("anna", annaPass)
	wantPage(t, "admin sees employee terms", s.get(saved.Header().Get("Location"), admin), http.StatusOK,
		"Распределение по условиям сотрудника", "Доля сотрудника от услуг — 50%.")
	for _, tc := range []struct {
		token, deposit, redemption, effect string
	}{
		{"client-guidance-paid", "30", "", "По этой записи расчёт с салоном — €0"},
		{"client-guidance-certificate", "", "60", "По этой записи салон должен вам €30"},
	} {
		form.Set("token", tc.token)
		form.Set("serviceCash", tc.deposit)
		form.Set("certificatePaid", tc.redemption)
		saved := s.post("/clients", form, z)
		wantPage(t, tc.token, s.get(saved.Header().Get("Location"), z), http.StatusOK, tc.effect)
	}
}

func TestClientWithoutTermsDoesNotInventShareOrContribution(t *testing.T) {
	s := newSite(t)
	z := s.login("olga", olgaPass)
	page := s.get("/clients/new?date=2026-11-20", z)
	wantPage(t, "missing terms", page, http.StatusOK, "Условия на этот месяц не настроены.")
	for _, invented := range []string{"Ваша доля от услуг — 0%", "Доля сотрудника от услуг — 0%", `id="client-result-values"`} {
		if strings.Contains(page.Body.String(), invented) {
			t.Errorf("missing terms present a fabricated calculation: %s", invented)
		}
	}
}

func TestInvalidClientRedemptionKeepsInputAndExplainsIncompleteResult(t *testing.T) {
	s := newSite(t)
	s.withTerms(s.olga, 7000)
	z := s.login("olga", olgaPass)
	form := url.Values{"date": {"2026-11-20"}, "token": {"invalid-preview-client-token"}, "version": {"0"}, "services": {"10"}, "certificatePaid": {"20"}, "extrasDestination": {"cash"}}
	res := s.post("/clients", form, z)
	wantPage(t, "incomplete client result", res, http.StatusUnprocessableEntity,
		`name="certificatePaid" value="20"`, `aria-describedby="client-certificatePaid-hint client-certificatePaid-error"`,
		`id="client-certificatePaid-error"`, "Использование сертификата не больше стоимости услуг.", "Расчёт не завершён", `id="client-result-values" class="hidden`,
		"Проверьте суммы: использование сертификата не больше услуг")
	tokens := html.NewTokenizer(strings.NewReader(res.Body.String()))
	summary := false
	for tokens.Next() != html.ErrorToken {
		token := tokens.Token()
		attrs := map[string]string{}
		for _, attr := range token.Attr {
			attrs[attr.Key] = attr.Val
		}
		if attrs["id"] == "client-validation-summary" && attrs["role"] == "alert" {
			summary = true
		}
	}
	if !summary {
		t.Fatal("invalid client input lacks an accessible validation summary")
	}
	form.Set("certificatePaid", "10")
	if corrected := s.post("/clients", form, z); corrected.Code != http.StatusSeeOther {
		t.Fatalf("corrected client retry %d: %s", corrected.Code, corrected.Body.String())
	}
}

func TestInvalidClientAmountsHideSavedResultsAndKeepCorrectionVersion(t *testing.T) {
	s := newSite(t)
	s.withTerms(s.olga, 7000)
	z := s.login("olga", olgaPass)
	form := url.Values{"date": {"2026-11-20"}, "token": {"invalid-saved-results-token"}, "version": {"0"}, "services": {"100"}, "extrasDestination": {"cash"}, "serviceCash": {"30"}}
	saved := s.post("/clients", form, z)
	href := strings.Split(saved.Header().Get("Location"), "?")[0]
	form.Set("version", "1")
	for _, invalid := range []struct {
		services, cash, message string
	}{
		{"oops", "30", "Введите сумму от €0"},
		{"10", "30", "Сумма взносов не больше стоимости услуг за вычетом сертификата."},
	} {
		form.Set("services", invalid.services)
		form.Set("serviceCash", invalid.cash)
		res := s.post(href, form, z)
		wantPage(t, "invalid saved client preview", res, http.StatusUnprocessableEntity,
			`name="services" value="`+invalid.services+`"`, `name="serviceCash" value="`+invalid.cash+`"`,
			`name="version" value="1"`, invalid.message, "Расчёт не завершён", `id="client-result-values" class="hidden`)
		if invalid.services == "10" && !strings.Contains(res.Body.String(), `id="client-serviceCard-error" class="text-sm text-error"`) {
			t.Fatal("contribution constraint is not visible beside both fields without JavaScript")
		}
	}
	form.Set("services", "100")
	if corrected := s.post(href, form, z); corrected.Code != http.StatusSeeOther {
		t.Fatalf("corrected saved client %d: %s", corrected.Code, corrected.Body.String())
	}
}

func TestClientFormSeparatesServiceRedemptionSalesAndHelp(t *testing.T) {
	s := newSite(t)
	s.withTerms(s.olga, 7000)
	z := s.login("olga", olgaPass)
	body := s.get("/clients/new?date=2026-11-20", z).Body.String()
	previous := -1
	for _, section := range []string{"Стоимость услуг, €", "Оплата сертификатом (необязательно)", `name="certificatePaid"`, "Дополнительные продажи (необязательно)", `name="creams"`, "Внесено в кассу за услуги", "Распределение по вашим условиям", "Как заполнить"} {
		index := strings.Index(body, section)
		if index <= previous {
			t.Fatalf("client entry section %q is missing or out of order", section)
		}
		previous = index
	}
	for _, field := range []string{"services", "creams", "oil", "certificates", "certificatePaid", "serviceCash", "serviceCard"} {
		if !strings.Contains(body, `aria-describedby="client-`+field+`-hint client-`+field+`-error"`) {
			t.Errorf("client amount %s lacks field-adjacent help association", field)
		}
	}
}

func TestInvalidExtraSalesDestinationExplainsRequiredChoice(t *testing.T) {
	s := newSite(t)
	s.withTerms(s.olga, 7000)
	z := s.login("olga", olgaPass)
	form := url.Values{"date": {"2026-11-20"}, "token": {"invalid-sales-destination-token"}, "version": {"0"}, "creams": {"20"}, "extrasDestination": {"unknown"}}
	res := s.post("/clients", form, z)
	wantPage(t, "invalid sales destination", res, http.StatusUnprocessableEntity,
		`name="creams" value="20"`, `aria-describedby="client-extrasDestination-error"`,
		`value="unknown" selected`, "Выберите кассу", "Выберите, куда внесены дополнительные продажи", `id="client-result-values" class="hidden`)
	form.Set("extrasDestination", "cash")
	if corrected := s.post("/clients", form, z); corrected.Code != http.StatusSeeOther {
		t.Fatalf("corrected extras-only client %d: %s", corrected.Code, corrected.Body.String())
	}
}

func TestClientDayLockingAndInstallBanner(t *testing.T) {
	s := newSite(t)
	s.withTerms(s.olga, 7000)
	z := s.login("olga", olgaPass)

	// 1. Normal open day for reporting staff shows install hint banner and "Добавить клиента"
	openDayRes := s.get("/?date=2026-11-20", z)
	wantPage(t, "open client day", openDayRes, http.StatusOK,
		"Добавить клиента",
		"Установите приложение",
		"standalone:hidden",
		"kassa_install_banner_dismissed",
		`data-show="!$_installDismissed"`,
	)

	// 2. Closed month shows lock notice and does NOT show "Добавить клиента"
	*s.clock = s.clock.AddDate(0, 0, 15)
	if _, err := s.st.CloseMonth(ctx, s.anna.Actor(), domain.Month{Year: 2026, Mon: 11}); err != nil {
		t.Fatal(err)
	}
	closedDayRes := s.get("/?date=2026-11-20", z)
	wantPage(t, "closed day shows lock notice", closedDayRes, http.StatusOK,
		"Месяц закрыт — изменения недоступны",
		`role="alert" class="alert alert-warning alert-soft`,
	)
	if strings.Contains(closedDayRes.Body.String(), "Добавить клиента") {
		t.Errorf("closed day unexpectedly showed 'Добавить клиента' button")
	}

	// 3. User without terms shows terms lock notice and does NOT show "Добавить клиента"
	r := s.login("maria", mariaPass)
	noTermsRes := s.get("/?date=2026-12-05", r)
	wantPage(t, "user without terms shows lock notice", noTermsRes, http.StatusOK,
		"Ваши условия («Себе» %) ещё не заданы",
	)
	if strings.Contains(noTermsRes.Body.String(), "Добавить клиента") {
		t.Errorf("user without terms unexpectedly showed 'Добавить клиента' button")
	}

	// 4. Non-reporting staff cannot create client and gets styled ForbiddenPage
	vera := s.person("vera", "Вера", "lerapass123")
	if _, err := s.st.UpdateUser(ctx, s.anna.Actor(), vera.ID, func(u *store.User) error {
		u.SubmitsReports = false
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	nonReporter := s.login("vera", "lerapass123")
	newClientRes := s.get("/clients/new?date=2026-12-05", nonReporter)
	wantPage(t, "non-reporting new client access", newClientRes, http.StatusForbidden,
		"Нет доступа",
	)
}

func TestClientEnteredByMistakeCanBeRemovedWithAReason(t *testing.T) {
	s := newSite(t)
	s.withTerms(s.olga, 7000)
	z := s.login("olga", olgaPass)
	form := url.Values{"date": {"2026-11-20"}, "token": {"http-removed-client-token"}, "version": {"0"}, "services": {"100"}, "creams": {"20"}, "extrasDestination": {"cash"}, "serviceCash": {"30"}}
	href := strings.Split(s.post("/clients", form, z).Header().Get("Location"), "?")[0]
	wantPage(t, "remove link", s.get(href, z), http.StatusOK, `href="`+href+`/remove"`, "Удалить клиента")
	wantPage(t, "confirmation", s.get(href+"/remove", z), http.StatusOK, "Удалить клиента?", "Клиент 1", "€120", `name="reason"`, `name="version" value="1"`, "журнале изменений у администратора", "Отмена", `enterkeyhint="done"`)
	wantPage(t, "other employee", s.get(href+"/remove", s.login("maria", mariaPass)), http.StatusForbidden, "Нет доступа")
	wantPage(t, "reason required", s.post(href+"/remove", url.Values{"date": {"2026-11-20"}, "version": {"1"}, "reason": {"  "}}, z), http.StatusUnprocessableEntity, "Укажите причину удаления")
	wantPage(t, "stale", s.post(href+"/remove", url.Values{"date": {"2026-11-20"}, "version": {"2"}, "reason": {"Ошибка"}}, z), http.StatusUnprocessableEntity, "Запись изменена")

	removed := s.post(href+"/remove", url.Values{"date": {"2026-11-20"}, "version": {"1"}, "reason": {"Добавлен по ошибке"}}, z)
	if removed.Code != http.StatusSeeOther || removed.Header().Get("Location") != "/?date=2026-11-20&removed=1" {
		t.Fatalf("remove %d %q %s", removed.Code, removed.Header().Get("Location"), removed.Body.String())
	}
	day := s.get("/?date=2026-11-20&removed=1", z)
	wantPage(t, "day after removal", day, http.StatusOK, "Клиент удалён", "За этот день записей нет.")
	if strings.Contains(day.Body.String(), "€120") {
		t.Fatal("removed client still counted in the day")
	}
	wantPage(t, "removed client page", s.get(href, z), http.StatusNotFound, "Клиент удалён или не найден", `href="/"`)
	wantPage(t, "back to the removal page", s.get(href+"/remove", z), http.StatusNotFound, "Клиент удалён или не найден")
	again := s.post(href+"/remove", url.Values{"date": {"2026-11-20"}, "version": {"1"}, "reason": {"Добавлен по ошибке"}}, z)
	if again.Code != http.StatusSeeOther || again.Header().Get("Location") != "/?date=2026-11-20" {
		t.Fatalf("repeated removal after a lost response %d %q", again.Code, again.Header().Get("Location"))
	}
	audit := s.get("/admin/audit", s.login("anna", annaPass))
	wantPage(t, "change log", audit, http.StatusOK, "Удаление", "Причина удаления", "Добавлен по ошибке")
	removal, _, _ := strings.Cut(audit.Body.String()[strings.Index(audit.Body.String(), "Удаление"):], "Создание")
	if !strings.Contains(removal, "€120") || strings.Contains(removal, `href="`+href+`"`) {
		t.Fatalf("removal card should show the removed amounts and no dead link: %s", removal)
	}
	wantPage(t, "next number counts the removed client", s.get("/clients/new?date=2026-11-20&token="+strings.Repeat("ab", 24), z), http.StatusOK, "Клиент 2")
	form.Set("token", "http-second-client-token")
	second := strings.Split(s.post("/clients", form, z).Header().Get("Location"), "?")[0]
	if !strings.Contains(s.get(second, z).Body.String(), "Клиент 2") {
		t.Fatal("second saved client should be Клиент 2")
	}
	wantPage(t, "long reason", s.post(second+"/remove", url.Values{"date": {"2026-11-20"}, "version": {"1"}, "reason": {strings.Repeat("я", 201)}}, z), http.StatusUnprocessableEntity, "не длиннее 200 символов")
	if removed := s.post(second+"/remove", url.Values{"date": {"2026-11-20"}, "version": {"1"}, "reason": {"Дубль"}}, z); removed.Code != http.StatusSeeOther {
		t.Fatalf("remove second %d", removed.Code)
	}
	wantPage(t, "next number after removing the last client", s.get("/clients/new?date=2026-11-20&token="+strings.Repeat("cd", 24), z), http.StatusOK, "Клиент 3")
}

func TestAdminRemovesAnEmployeeClientButNotInAClosedMonth(t *testing.T) {
	s := newSite(t)
	s.withTerms(s.olga, 7000)
	z := s.login("olga", olgaPass)
	admin := s.login("anna", annaPass)
	form := url.Values{"date": {"2026-11-20"}, "token": {"http-admin-removes-token"}, "version": {"0"}, "services": {"100"}, "extrasDestination": {"cash"}}
	href := strings.Split(s.post("/clients", form, z).Header().Get("Location"), "?")[0]
	form.Set("token", "http-closed-month-token")
	kept := strings.Split(s.post("/clients", form, z).Header().Get("Location"), "?")[0]
	removed := s.post(href+"/remove", url.Values{"date": {"2026-11-20"}, "version": {"1"}, "reason": {"Дубль"}}, admin)
	if removed.Code != http.StatusSeeOther || removed.Header().Get("Location") != "/admin/day?date=2026-11-20&removed=1" {
		t.Fatalf("admin remove %d %q %s", removed.Code, removed.Header().Get("Location"), removed.Body.String())
	}
	wantPage(t, "admin day notice", s.get("/admin/day?date=2026-11-20&removed=1", admin), http.StatusOK, "Клиент удалён")
	if again := s.post(href+"/remove", url.Values{"date": {"2026-11-20"}, "version": {"1"}, "reason": {"Дубль"}}, admin); again.Header().Get("Location") != "/admin/day?date=2026-11-20" {
		t.Fatalf("admin repeat lands on %q", again.Header().Get("Location"))
	}
	*s.clock = s.clock.AddDate(0, 0, 15)
	if _, err := s.st.CloseMonth(ctx, s.anna.Actor(), domain.Month{Year: 2026, Mon: 11}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(s.get(kept, z).Body.String(), "/remove") {
		t.Fatal("closed month offers removal")
	}
	wantPage(t, "closed removal", s.post(kept+"/remove", url.Values{"date": {"2026-11-20"}, "version": {"1"}, "reason": {"Поздно"}}, z), http.StatusUnprocessableEntity, "Месяц закрыт")
}
