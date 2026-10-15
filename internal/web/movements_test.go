package web_test

import (
	"github.com/lieranderl/aestheticlab-income-app/internal/domain"
	"github.com/lieranderl/aestheticlab-income-app/internal/store"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

func TestStaffDepositWaitsForConfirmationAndStaysPrivate(t *testing.T) {
	s := newSite(t)
	z := s.login("olga", olgaPass)
	j := s.login("anna", annaPass)
	r := s.login("maria", mariaPass)
	form := url.Values{"type": {"deposit"}, "amount": {"100"}, "date": {"2026-11-20"}, "from": {"person:" + s.olga.ID}, "to": {"account:kassa_cash"}}
	wantRedirect(t, "deposit", s.post("/movements", form, z), "/my?saved=pending")
	es, err := s.st.PersonMovementsIn(ctx, s.olga.ID, s.st.StartMonth())
	if err != nil || len(es) != 1 {
		t.Fatalf("rows %+v %v", es, err)
	}
	e := es[0]
	if e.Status != domain.Pending {
		t.Fatal("staff immediately confirmed")
	}
	wantPage(t, "own", s.get("/my", z), http.StatusOK, "Ожидает подтверждения", "€100", "Текущий баланс")
	wantPage(t, "foreign void", s.get("/movements/"+e.ID+"/void", r), http.StatusForbidden, "Нет доступа")
	wantPage(t, "admin kassa", s.get("/admin/kassa", j), http.StatusOK, "Подтвердить", "Касса", "Пересчитать")
	wantRedirect(t, "confirm", s.post("/admin/movements/"+e.ID+"/confirm", url.Values{"version": {"1"}}, j), "/admin/kassa?saved=1")
	b, err := s.st.PersonBalance(ctx, s.olga.ID)
	if err != nil || b != 10000 {
		t.Fatalf("balance %d %v", b, err)
	}
	wantPage(t, "stale", s.post("/admin/movements/"+e.ID+"/confirm", url.Values{"version": {"1"}}, j), http.StatusUnprocessableEntity, "Запись изменена")
	form.Set("from", "person:"+s.maria.ID)
	wantPage(t, "forged", s.post("/movements", form, z), http.StatusForbidden, "Нет доступа")
	page := s.get("/movements/new", z)
	if strings.Contains(page.Body.String(), s.maria.ID) || strings.Contains(page.Body.String(), "Мария") {
		t.Fatal("form leaks another person")
	}
}

func TestRecountAcceptsZeroAndMovementHistoryValidatesFilters(t *testing.T) {
	s := newSite(t)
	j := s.login("anna", annaPass)
	z := s.login("olga", olgaPass)
	wantPage(t, "recount fallback", s.get("/admin/accounts/kassa_cash/recount", j), http.StatusOK, "По расчёту", "Фактически")
	wantRedirect(t, "zero recount", s.post("/admin/accounts/kassa_cash/recount", url.Values{"counted": {"0"}}, j), "/admin/kassa?saved=1")
	wantPage(t, "staff denied", s.get("/admin/accounts/kassa_cash/recount", z), http.StatusForbidden, "Нет доступа")
	wantPage(t, "history", s.get("/admin/history/movements?type=deposit&status=pending", j), http.StatusOK, "Движения", "Фильтры")
	for _, q := range []string{"type=unknown", "status=unknown", "person=unknown", "from=2026-11-20&to=2026-11-01"} {
		wantPage(t, q, s.get("/admin/history/movements?"+q, j), http.StatusBadRequest, "Проверьте")
	}
}

func TestMovementsRecomputeBalancesAndVoidsPreserveAudit(t *testing.T) {
	s := newSite(t)
	s.withTerms(s.anna, 5000)
	j := s.login("anna", annaPass)
	z := s.login("olga", olgaPass)
	_, err := s.st.RecordOpenings(ctx, s.anna.Actor(), []store.NewMovement{
		{Type: domain.Opening, Amount: 100000, From: domain.Salon, To: domain.Account("kassa_cash"), Date: s.st.StartMonth().FirstDay()},
		{Type: domain.Opening, Amount: 200000, From: domain.Salon, To: domain.Account("kassa_card"), Date: s.st.StartMonth().FirstDay()},
		{Type: domain.Opening, Amount: 5000, From: domain.Salon, To: domain.Certificates, Date: s.st.StartMonth().FirstDay()},
	})
	if err != nil {
		t.Fatal(err)
	}
	s.saveDay(s.anna, 20, map[string]domain.Cents{"services": 60000, "creams": 5000, "certificates": 10000}, map[string]domain.Cents{"card": 60000, "cash": 15000})
	submit := func(path, kind, amount, from, to, note string, cookies []*http.Cookie) {
		t.Helper()
		form := url.Values{"type": {kind}, "amount": {amount}, "date": {"2026-11-20"}, "from": {from}, "to": {to}, "note": {note}, "expenseCategory": {"rent"}}
		res := s.post(path, form, cookies)
		if res.Code != http.StatusSeeOther {
			t.Fatalf("%s: %d %s", kind, res.Code, res.Body.String())
		}
	}
	submit("/movements", "deposit", "100", "person:"+s.olga.ID, "account:kassa_cash", "", z)
	// Use an existing expense category rather than assuming its seed ID.
	cats, err := s.st.Categories(ctx)
	if err != nil {
		t.Fatal(err)
	}
	category := ""
	for _, c := range cats {
		if c.Kind == store.KindExpense {
			category = c.ID
			break
		}
	}
	expense := url.Values{"type": {"expense"}, "amount": {"20"}, "date": {"2026-11-20"}, "from": {"person:" + s.olga.ID}, "to": {"outside"}, "note": {"Материалы"}, "expenseCategory": {category}}
	wantRedirect(t, "own expense", s.post("/movements", expense, z), "/my?saved=pending")
	b, err := s.st.Balances(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if b.Accounts["kassa_cash"] != 100000 || b.Accounts["kassa_card"] != 260000 || b.Persons[s.anna.ID] != 17500 || b.Persons[s.olga.ID] != 0 || b.CertificatesOutstanding != 15000 {
		t.Fatalf("pending totals %+v", b)
	}
	rows, err := s.st.PersonMovementsIn(ctx, s.olga.ID, s.st.StartMonth())
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range rows {
		wantRedirect(t, "confirm", s.post("/admin/movements/"+e.ID+"/confirm", url.Values{"version": {"1"}}, j), "/admin/kassa?saved=1")
	}
	submit("/admin/movements", "payout", "50", "account:kassa_cash", "person:"+s.olga.ID, "", j)
	submit("/admin/movements", "transfer", "100", "account:kassa_cash", "account:kassa_card", "", j)
	expense.Set("from", "account:kassa_card")
	expense.Set("amount", "10")
	wantRedirect(t, "account expense", s.post("/admin/movements", expense, j), "/admin/kassa?saved=1")
	b, err = s.st.Balances(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if b.Accounts["kassa_cash"] != 95000 || b.Accounts["kassa_card"] != 269000 || b.Persons[s.olga.ID] != 7000 {
		t.Fatalf("confirmed %+v", b)
	}
	rows, err = s.st.PersonMovementsIn(ctx, s.olga.ID, s.st.StartMonth())
	if err != nil {
		t.Fatal(err)
	}
	var payout store.Entry
	for _, e := range rows {
		if e.Type == domain.Payout {
			payout = e
		}
	}
	wantRedirect(t, "void", s.post("/movements/"+payout.ID+"/void", url.Values{"version": {"1"}, "reason": {"Ошибка суммы"}}, j), "/admin/kassa?saved=1")
	after, err := s.st.Entry(ctx, payout.ID)
	if err != nil || after.Status != domain.Voided || after.VoidReason != "Ошибка суммы" {
		t.Fatalf("void row %+v %v", after, err)
	}
	b, err = s.st.Balances(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if b.Accounts["kassa_cash"] != 100000 || b.Persons[s.olga.ID] != 12000 {
		t.Fatalf("void totals %+v", b)
	}
	wantPage(t, "recount needs note", s.post("/admin/accounts/kassa_cash/recount", url.Values{"counted": {"0"}}, j), http.StatusUnprocessableEntity, "Укажите комментарий")
	wantRedirect(t, "zero adjustment", s.post("/admin/accounts/kassa_cash/recount", url.Values{"counted": {"0"}, "note": {"Проверено"}}, j), "/admin/kassa?saved=1")
	b, err = s.st.Balances(ctx)
	if err != nil || b.Accounts["kassa_cash"] != 0 || b.Accounts["kassa_card"] != 269000 {
		t.Fatalf("recount %+v %v", b, err)
	}
	audit, err := s.st.AuditBetween(ctx, s.st.StartMonth().FirstDay(), s.st.Today())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range audit {
		if e.EntityID == payout.ID && e.Action == store.ActionVoid {
			found = true
		}
	}
	if !found {
		t.Fatal("void missing audit")
	}
	wantPage(t, "audit recount values", s.get("/admin/audit", j), http.StatusOK, "По учёту", "При пересчёте")
}

func TestMovementFailuresAndBothResponseModesKeepFormsSafe(t *testing.T) {
	s := newSite(t)
	j := s.login("anna", annaPass)
	z := s.login("olga", olgaPass)
	rub := s.login("maria", mariaPass)
	base := url.Values{"type": {"deposit"}, "amount": {"50"}, "date": {"2026-11-20"}, "from": {"person:" + s.olga.ID}, "to": {"account:kassa_cash"}}
	for _, tc := range []struct{ key, value string }{{"amount", "0"}, {"amount", "-2"}, {"amount", "1 50"}, {"amount", ""}, {"date", "2026-11-21"}, {"date", "2026-10-31"}, {"date", "broken"}, {"to", "account:missing"}, {"to", "person:" + s.maria.ID}} {
		f := cloneValues(base)
		f.Set(tc.key, tc.value)
		wantPage(t, tc.key+tc.value, s.post("/admin/movements", f, j), http.StatusUnprocessableEntity, "Проверьте")
	}
	for _, kind := range []string{"opening", "adjustment", "salary", "unknown"} {
		f := cloneValues(base)
		f.Set("type", kind)
		wantPage(t, kind, s.post("/admin/movements", f, j), http.StatusBadRequest, "Проверьте")
	}
	transfer := cloneValues(base)
	transfer.Set("type", "transfer")
	transfer.Set("from", "account:kassa_cash")
	wantPage(t, "same account", s.post("/admin/movements", transfer, j), http.StatusUnprocessableEntity, "Проверьте")
	wantPage(t, "staff admin", s.post("/admin/movements", base, z), http.StatusForbidden, "Нет доступа")
	wantPage(t, "GET SSE type", s.do(request{method: http.MethodGet, path: "/movements/new?type=expense", cookies: z, datastar: true}), http.StatusOK, "datastar-patch-elements", "Категория расхода")
	wantPage(t, "SSE save", s.do(request{method: http.MethodPost, path: "/movements", form: base, cookies: z, datastar: true}), http.StatusOK, "redirectUrl", "/my?saved=pending")
	es, err := s.st.PersonMovementsIn(ctx, s.olga.ID, s.st.StartMonth())
	if err != nil || len(es) != 1 {
		t.Fatalf("rows %v %v", es, err)
	}
	e := es[0]
	path := "/movements/" + e.ID + "/void"
	wantPage(t, "foreign POST", s.post(path, url.Values{"version": {"1"}, "reason": {"wrong"}}, rub), http.StatusForbidden, "Нет доступа")
	wantPage(t, "void reason fallback", s.get(path, z), http.StatusOK, "Причина отмены")
	wantPage(t, "empty reason", s.post(path, url.Values{"version": {"1"}}, z), http.StatusUnprocessableEntity, "Укажите комментарий")
	wantPage(t, "SSE void stale", s.do(request{method: http.MethodPost, path: path, form: url.Values{"version": {"2"}, "reason": {"Ошибка"}}, cookies: z, datastar: true}), http.StatusOK, "datastar-patch-elements", "Запись изменена", "movement-action-"+e.ID+"-void")
	wantPage(t, "SSE void", s.do(request{method: http.MethodPost, path: path, form: url.Values{"version": {"1"}, "reason": {"Ошибка"}}, cookies: z, datastar: true}), http.StatusOK, "redirectUrl")
	wantPage(t, "void persists", s.get("/my", z), http.StatusOK, "Причина отмены: Ошибка", "Отменено")
}

func cloneValues(v url.Values) url.Values {
	out := url.Values{}
	for k, vs := range v {
		out[k] = append([]string{}, vs...)
	}
	return out
}

func TestMovementHistoryFiltersAndPagesDoNotLoseSelections(t *testing.T) {
	s := newSite(t)
	j := s.login("anna", annaPass)
	for i := 0; i < 60; i++ {
		_, err := s.st.RecordMovement(ctx, s.anna.Actor(), store.NewMovement{Type: domain.Deposit, Amount: 100, From: domain.Person(s.olga.ID), To: domain.Account("kassa_cash"), Date: domain.Date{Year: 2026, Mon: 11, Day: 20}, Note: "Ольга"})
		if err != nil {
			t.Fatal(err)
		}
	}
	_, err := s.st.RecordMovement(ctx, s.anna.Actor(), store.NewMovement{Type: domain.Deposit, Amount: 99900, From: domain.Person(s.maria.ID), To: domain.Account("kassa_card"), Date: domain.Date{Year: 2026, Mon: 11, Day: 19}, Note: "Исключено"})
	if err != nil {
		t.Fatal(err)
	}
	path := "/admin/history/movements?from=2026-11-20&to=2026-11-20&person=" + s.olga.ID + "&type=deposit&status=confirmed"
	first := s.get(path, j)
	wantPage(t, "first50", first, http.StatusOK, "Записи 1–50 из 60", "status=confirmed", "type=deposit")
	if strings.Contains(first.Body.String(), "Исключено") {
		t.Fatal("foreign date/person movement included")
	}
	wantPage(t, "last10", s.get(path+"&page=2", j), http.StatusOK, "Записи 51–60 из 60", "Предыдущие записи")
	wantPage(t, "pending filter", s.get("/admin/history/movements?status=pending", j), http.StatusOK, "По этим фильтрам движений нет")
}

func TestClosedMonthRejectsMovementWritesAndAdminOwnDepositsConfirmImmediately(t *testing.T) {
	s := newSite(t)
	s.withTerms(s.anna, 5000)
	j := s.login("anna", annaPass)
	form := url.Values{"type": {"deposit"}, "amount": {"25"}, "date": {"2026-11-20"}, "from": {"person:" + s.anna.ID}, "to": {"account:kassa_cash"}}
	wantRedirect(t, "admin own deposit", s.post("/movements", form, j), "/my?saved=1")
	es, err := s.st.PersonMovementsIn(ctx, s.anna.ID, s.st.StartMonth())
	if err != nil || len(es) != 1 || es[0].Status != domain.Confirmed {
		t.Fatalf("admin own %+v %v", es, err)
	}
	*s.clock = s.clock.AddDate(0, 1, 0)
	_, err = s.st.CloseMonth(ctx, s.anna.Actor(), s.st.StartMonth())
	if err != nil {
		t.Fatal(err)
	}
	j = s.login("anna", annaPass)
	z := s.login("olga", olgaPass)
	wantPage(t, "admin closed", s.post("/admin/movements", form, j), http.StatusUnprocessableEntity, "Месяц закрыт")
	form.Set("from", "person:"+s.olga.ID)
	wantPage(t, "staff closed", s.post("/movements", form, z), http.StatusUnprocessableEntity, "Месяц закрыт")
	wantPage(t, "closed void", s.post("/movements/"+es[0].ID+"/void", url.Values{"version": {"1"}, "reason": {"Ошибка"}}, j), http.StatusUnprocessableEntity, "Месяц закрыт")
	wantPage(t, "historical my current", s.get("/my?m=2026-11", j), http.StatusOK, "Текущий баланс", "20.12.2026")
}

func TestKassaWarnsWhenCertificateRedemptionsExceedSales(t *testing.T) {
	s := newSite(t)
	s.withTerms(s.olga, 7000)
	j := s.login("anna", annaPass)
	s.saveDay(s.olga, 20, map[string]domain.Cents{"services": 10000}, map[string]domain.Cents{"paid_by_certificate": 10000})
	wantPage(t, "certificate warning", s.get("/admin/kassa", j), http.StatusOK, "Проверьте сертификаты", "−€100", "начальный остаток", "продажи и использование сертификатов", "Открыть записи")
}

func TestMovementCommentRequirementMatchesTheSelectedOperation(t *testing.T) {
	s := newSite(t)
	j := s.login("anna", annaPass)
	for _, kind := range []string{"deposit", "transfer", "payout", "expense"} {
		label := "Комментарий (необязательно)"
		if kind == "expense" {
			label = "Комментарий (обязательно)"
		}
		page := s.get("/admin/movements/new?type="+kind, j)
		wantPage(t, kind, page, http.StatusOK, label, "Сумма, €")
		if strings.Contains(page.Body.String(), "Для расхода обязателен") {
			t.Fatalf("%s shows a generic expense instruction", kind)
		}
	}
}

func TestExpenseNoteAndSSEActionConflictsRemainActionable(t *testing.T) {
	s := newSite(t)
	j := s.login("anna", annaPass)
	z := s.login("olga", olgaPass)
	cats, err := s.st.Categories(ctx)
	if err != nil {
		t.Fatal(err)
	}
	category := ""
	for _, c := range cats {
		if c.Kind == store.KindExpense {
			category = c.ID
			break
		}
	}
	f := url.Values{"type": {"expense"}, "amount": {"12,50"}, "date": {"2026-11-20"}, "from": {"person:" + s.olga.ID}, "to": {"outside"}, "expenseCategory": {category}, "note": {""}}
	wantPage(t, "expense requires note", s.do(request{method: http.MethodPost, path: "/movements", cookies: z, form: f, datastar: true}), http.StatusOK, "datastar-patch-elements", "Укажите комментарий", `value="12,50"`)
	f.Set("note", "Материалы")
	f.Set("expenseCategory", "missing")
	wantPage(t, "unknown expense category", s.post("/movements", f, z), http.StatusUnprocessableEntity, "Проверьте")
	f.Set("expenseCategory", category)
	wantRedirect(t, "expense", s.post("/movements", f, z), "/my?saved=pending")
	rows, err := s.st.PersonMovementsIn(ctx, s.olga.ID, s.st.StartMonth())
	if err != nil || len(rows) != 1 {
		t.Fatalf("rows %+v %v", rows, err)
	}
	path := "/admin/movements/" + rows[0].ID + "/confirm"
	wantPage(t, "SSE confirmation", s.do(request{method: http.MethodPost, path: path, form: url.Values{"version": {"1"}}, cookies: j, datastar: true}), http.StatusOK, "redirectUrl")
	wantPage(t, "SSE stale confirmation", s.do(request{method: http.MethodPost, path: path, form: url.Values{"version": {"1"}}, cookies: j, datastar: true}), http.StatusOK, "datastar-patch-elements", "Запись изменена", "movement-action-"+rows[0].ID+"-confirm")
	wantPage(t, "double confirmation", s.post(path, url.Values{"version": {"2"}}, j), http.StatusUnprocessableEntity, "Движение уже обработано")
	wantPage(t, "SSE invalid recount", s.do(request{method: http.MethodPost, path: "/admin/accounts/kassa_cash/recount", form: url.Values{"counted": {"broken"}, "note": {"Пересчёт"}}, cookies: j, datastar: true}), http.StatusOK, "datastar-patch-elements", `value="broken"`, "Пересчёт")
	wantPage(t, "SSE exact recount", s.do(request{method: http.MethodPost, path: "/admin/accounts/kassa_cash/recount", form: url.Values{"counted": {"0"}}, cookies: j, datastar: true}), http.StatusOK, "redirectUrl")
	for _, kind := range []string{"deposit", "expense", "transfer", "payout"} {
		wantPage(t, "fallback "+kind, s.get("/admin/movements/new?type="+kind, j), http.StatusOK, `action="/admin/movements"`, `name="type" value="`+kind+`"`)
	}
	wantPage(t, "kassa native sheets", s.get("/admin/kassa", j), http.StatusOK, `id="recount-kassa_cash"`, `id="recount-kassa_card"`, `id="movement-form"`, `<dialog`)
}

func TestFinancialFormsNeverAutomaticallyRepeatPosts(t *testing.T) {
	s := newSite(t)
	j := s.login("anna", annaPass)
	z := s.login("olga", olgaPass)
	for _, p := range []struct {
		path    string
		cookies []*http.Cookie
	}{{"/admin/movements/new?type=payout", j}, {"/movements/new", z}, {"/admin/accounts/kassa_cash/recount", j}, {"/admin/kassa", j}} {
		page := s.get(p.path, p.cookies)
		wantPage(t, p.path, page, http.StatusOK, "retry: &#39;never&#39;", "retryMaxCount: 0", "Проверьте движения")
	}
	_, err := s.st.RecordMovement(ctx, s.olga.Actor(), store.NewMovement{Type: domain.Deposit, Amount: 2500, From: domain.Person(s.olga.ID), To: domain.Account("kassa_cash"), Date: s.st.Today()})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := s.st.PersonMovementsIn(ctx, s.olga.ID, s.st.StartMonth())
	if err != nil {
		t.Fatal(err)
	}
	wantPage(t, "void financial action", s.get("/movements/"+rows[0].ID+"/void", z), http.StatusOK, "retry: &#39;never&#39;", "retryMaxCount: 0", "Проверьте движения")
}

func TestOwnProcessedCancellationPreservesReasonAndOffersSafeNavigation(t *testing.T) {
	for _, processed := range []string{"confirmed", "voided"} {
		for _, sse := range []bool{false, true} {
			t.Run(processed+"/"+strconv.FormatBool(sse), func(t *testing.T) {
				s := newSite(t)
				z := s.login("olga", olgaPass)
				rub := s.login("maria", mariaPass)
				e, err := s.st.RecordMovement(ctx, s.olga.Actor(), store.NewMovement{Type: domain.Deposit, Amount: 2500, From: domain.Person(s.olga.ID), To: domain.Account("kassa_cash"), Date: s.st.Today()})
				if err != nil {
					t.Fatal(err)
				}
				path := "/movements/" + e.ID + "/void"
				wantPage(t, "opened pending", s.get(path, z), http.StatusOK, "Причина отмены")
				if processed == "confirmed" {
					_, err = s.st.ConfirmMovement(ctx, s.anna.Actor(), e.ID, e.Version)
				} else {
					_, err = s.st.VoidMovement(ctx, s.olga.Actor(), e.ID, e.Version, "Отмена в другой вкладке")
				}
				if err != nil {
					t.Fatal(err)
				}
				status := http.StatusConflict
				if sse {
					status = http.StatusOK
				}
				page := s.do(request{method: http.MethodPost, path: path, cookies: z, form: url.Values{"version": {"1"}, "reason": {"Сохранить мой комментарий"}}, datastar: sse})
				wantPage(t, "processed conflict", page, status, "Движение уже обработано", "Сохранить мой комментарий", `href="/my"`)
				if sse {
					wantPage(t, "SSE patches action", page, status, "datastar-patch-elements", "movement-action-"+e.ID+"-void")
				}
				after, err := s.st.Entry(ctx, e.ID)
				if err != nil {
					t.Fatal(err)
				}
				if after.Version != 2 || string(after.Status) != processed {
					t.Fatalf("race changed row %+v", after)
				}
				wantPage(t, "foreign remains denied", s.post(path, url.Values{"version": {"1"}, "reason": {"Скрыто"}}, rub), http.StatusForbidden, "Нет доступа")
			})
		}
	}
}

func TestLoadedMovementTypePatchesFormBeforeEnablingInput(t *testing.T) {
	s := newSite(t)
	z := s.login("olga", olgaPass)
	res := s.do(request{method: http.MethodGet, path: "/movements/new?type=expense", cookies: z, datastar: true})
	wantPage(t, "loaded expense", res, http.StatusOK, "datastar-patch-elements", `name="type" value="expense"`, "datastar-patch-signals", `"_movementready":true`, `"_movementloading":false`)
	if strings.Index(res.Body.String(), "datastar-patch-elements") >= strings.Index(res.Body.String(), "datastar-patch-signals") {
		t.Fatal("input enabled before loaded form patched")
	}
	wantPage(t, "no-JS expense page", s.get("/movements/new?type=expense", z), http.StatusOK, `action="/movements"`, `name="type" value="expense"`, `href="/movements/new?type=deposit"`)
	wantPage(t, "unknown type never enables", s.do(request{method: http.MethodGet, path: "/movements/new?type=opening", cookies: z, datastar: true}), http.StatusBadRequest, "Проверьте вид движения")
}
