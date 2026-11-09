package web_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/lieranderl/aestheticlab-income-app/internal/domain"
	"github.com/lieranderl/aestheticlab-income-app/internal/store"
)

func (s *site) adminExpense(amount, day, note string, cookies []*http.Cookie) store.Entry {
	s.t.Helper()
	*s.clock = s.clock.Add(time.Minute)
	form := url.Values{"type": {"expense"}, "amount": {amount}, "date": {day}, "from": {"account:kassa_cash"}, "to": {"outside"}, "expenseCategory": {"rent"}, "note": {note}}
	wantRedirect(s.t, note, s.post("/admin/movements", form, cookies), "/admin/kassa?saved=1")
	es, err := s.st.LedgerBetween(ctx, s.st.StartMonth().FirstDay(), s.st.Today())
	if err != nil {
		s.t.Fatal(err)
	}
	for _, e := range es {
		if e.Note == note && e.Status != domain.Voided {
			return e
		}
	}
	s.t.Fatalf("no expense %q", note)
	return store.Entry{}
}

func wantOrder(t *testing.T, what, body string, texts ...string) {
	t.Helper()
	at := -1
	for _, x := range texts {
		i := strings.Index(body, x)
		if i < 0 || i < at {
			t.Fatalf("%s: %q missing or out of order in %v", what, x, texts)
		}
		at = i
	}
}

func TestMovementListsShowNewestEnteredFirstAndHideVoided(t *testing.T) {
	s := newSite(t)
	j := s.login("anna", annaPass)
	s.adminExpense("10", "2026-11-18", "первый-расход", j)
	gone := s.adminExpense("30", "2026-11-19", "отменённый-расход", j)
	s.adminExpense("20", "2026-11-03", "второй-расход", j)
	wantRedirect(t, "void", s.post("/movements/"+gone.ID+"/void", url.Values{"version": {"1"}, "reason": {"дубль"}}, j), "/admin/kassa?saved=1")

	for _, path := range []string{"/admin/kassa", "/admin/history/movements"} {
		body := s.get(path, j).Body.String()
		wantOrder(t, path, body, "второй-расход", "первый-расход")
		if strings.Contains(body, "отменённый-расход") {
			t.Fatalf("%s shows a voided movement", path)
		}
	}
	wantPage(t, "voided on request", s.get("/admin/history/movements?status=voided", j), http.StatusOK, "отменённый-расход", "Причина отмены: дубль")

	z := s.login("olga", olgaPass)
	for _, note := range []string{"мой-первый", "мой-отменённый", "мой-второй"} {
		*s.clock = s.clock.Add(time.Minute)
		form := url.Values{"type": {"expense"}, "amount": {"5"}, "date": {"2026-11-10"}, "from": {"person:" + s.olga.ID}, "to": {"outside"}, "expenseCategory": {"materials"}, "note": {note}}
		wantRedirect(t, note, s.post("/movements", form, z), "/my?saved=pending")
	}
	mine, err := s.st.PersonMovementsIn(ctx, s.olga.ID, s.st.StartMonth())
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range mine {
		if e.Note == "мой-отменённый" {
			wantRedirect(t, "own void", s.post("/movements/"+e.ID+"/void", url.Values{"version": {"1"}, "reason": {"ошибка"}}, z), "/my?saved=1")
		}
	}
	body := s.get("/my", z).Body.String()
	wantOrder(t, "/my", body, "мой-второй", "мой-первый")
	if strings.Contains(body, "мой-отменённый") {
		t.Fatal("/my shows a voided movement")
	}
}

func TestAdminEditsAnExpenseInPlaceOfVoidingIt(t *testing.T) {
	s := newSite(t)
	j := s.login("anna", annaPass)
	old := s.adminExpense("100", "2026-11-03", "аренда гаража", j)
	wantPage(t, "edit link", s.get("/admin/kassa", j), http.StatusOK, "/movements/"+old.ID+"/edit", "Изменить")
	wantPage(t, "edit form", s.get("/movements/"+old.ID+"/edit", j), http.StatusOK, "Изменить расход", `value="100"`, `value="2026-11-03"`, "аренда гаража")

	form := url.Values{"version": {"1"}, "type": {"expense"}, "amount": {"120,50"}, "date": {"2026-11-04"}, "from": {"account:kassa_card"}, "to": {"outside"}, "expenseCategory": {"rent"}, "note": {"аренда гаража, ноябрь"}}
	wantRedirect(t, "edit", s.post("/movements/"+old.ID+"/edit", form, j), "/admin/kassa?saved=1")
	wantPage(t, "stale edit", s.post("/movements/"+old.ID+"/edit", form, j), http.StatusUnprocessableEntity, "Движение уже отменено")
	wantPage(t, "after edit", s.get("/admin/kassa", j), http.StatusOK, "€120,50", "аренда гаража, ноябрь")
	b, err := s.st.Balances(ctx)
	if err != nil || b.Accounts[domain.KassaCash] != 0 || b.Accounts[domain.KassaCard] != -12050 {
		t.Fatalf("balances %+v %v", b.Accounts, err)
	}
}

func TestStaffEditOnlyTheirOwnPendingExpenses(t *testing.T) {
	s := newSite(t)
	z := s.login("olga", olgaPass)
	r := s.login("maria", mariaPass)
	j := s.login("anna", annaPass)
	form := url.Values{"type": {"expense"}, "amount": {"50"}, "date": {"2026-11-10"}, "from": {"person:" + s.olga.ID}, "to": {"outside"}, "expenseCategory": {"materials"}, "note": {"перчатки"}}
	wantRedirect(t, "record", s.post("/movements", form, z), "/my?saved=pending")
	es, err := s.st.PersonMovementsIn(ctx, s.olga.ID, s.st.StartMonth())
	if err != nil || len(es) != 1 {
		t.Fatalf("rows %+v %v", es, err)
	}
	id := es[0].ID
	wantPage(t, "own edit link", s.get("/my", z), http.StatusOK, "/movements/"+id+"/edit")
	wantPage(t, "foreign edit page", s.get("/movements/"+id+"/edit", r), http.StatusForbidden, "Нет доступа")
	form.Set("version", "1")
	form.Set("amount", "45")
	wantPage(t, "foreign edit", s.post("/movements/"+id+"/edit", form, r), http.StatusForbidden, "Нет доступа")
	wantRedirect(t, "own edit", s.post("/movements/"+id+"/edit", form, z), "/my?saved=pending")
	wantPage(t, "after own edit", s.get("/my", z), http.StatusOK, "€45", "Ожидает подтверждения")

	deposit := url.Values{"type": {"deposit"}, "amount": {"10"}, "date": {"2026-11-10"}, "from": {"person:" + s.olga.ID}, "to": {"account:kassa_cash"}}
	wantRedirect(t, "deposit", s.post("/movements", deposit, z), "/my?saved=pending")
	es, _ = s.st.PersonMovementsIn(ctx, s.olga.ID, s.st.StartMonth())
	for _, e := range es {
		if e.Type == domain.Deposit {
			wantPage(t, "deposit edit", s.get("/movements/"+e.ID+"/edit", j), http.StatusNotFound)
		}
	}
}
