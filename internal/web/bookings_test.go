package web_test

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/lieranderl/aestheticlab-income-app/internal/bookings"
	"github.com/lieranderl/aestheticlab-income-app/internal/domain"
	"github.com/lieranderl/aestheticlab-income-app/internal/web"
)

const (
	olgaCal  = "olga-cal@group.calendar.google.com"
	mariaCal = "maria-cal@group.calendar.google.com"
	// Made-up client data: it must never reach a page.
	clientName = "Vera Example"
	clientTel  = "+32000000000"
)

// bookedSite is a site whose staff calendars are a memory source. The
// clock is 20 November 2026, 11:00 in Brussels.
func bookedSite(t *testing.T) (*site, *bookings.Memory) {
	t.Helper()
	s := newSite(t)
	s.withTerms(s.olga, 7000)
	s.withTerms(s.maria, 7000)
	src := bookings.NewMemory()
	brussels, err := time.LoadLocation("Europe/Brussels")
	if err != nil {
		t.Fatal(err)
	}
	reader, err := bookings.NewReader(src, olgaCal+"|"+s.olga.ID+";"+mariaCal+"|"+s.maria.ID, brussels)
	if err != nil {
		t.Fatal(err)
	}
	s.h = handler(t, web.Config{
		BaseURL: "https://kassa.example.com", Store: s.st, Now: func() time.Time { return *s.clock }, PasswordCost: bcrypt.MinCost,
		SessionKey: []byte("test-session-key-test-session-key-0123456789"), Bookings: reader,
	})
	return s, src
}

func timely(id, day, hm, line string) bookings.Event {
	brussels, _ := time.LoadLocation("Europe/Brussels")
	start, err := time.ParseInLocation("2006-01-02 15:04", day+" "+hm, brussels)
	if err != nil {
		panic(err)
	}
	return bookings.Event{ID: id, Summary: clientName, Description: clientName + " - " + line + "\nTel: " + clientTel + "\nEmail: client@example.com", Start: start}
}

func clientForm(token, services string) url.Values {
	return url.Values{"date": {"2026-11-20"}, "token": {token}, "version": {"0"}, "services": {services}, "extrasDestination": {"cash"}, "serviceCash": {"9"}}
}

func noClientData(t *testing.T, what, body string) {
	t.Helper()
	for _, secret := range []string{clientName, clientTel, "client@example.com"} {
		if strings.Contains(body, secret) {
			t.Fatalf("%s shows client data %q", what, secret)
		}
	}
}

func TestStaffEnterAClientFromTheirBookingOnce(t *testing.T) {
	s, src := bookedSite(t)
	src.Put(olgaCal, timely("b1", "2026-11-20", "10:00", "Lashes Coloring + Brows Coloring Price: €30.00 with Olga Test (Confirmed)"))
	src.Put(olgaCal, timely("b2", "2026-11-20", "15:30", "Upper Lip Wax Price: €10.00 with Olga Test (Confirmed)"))
	src.Put(mariaCal, timely("m1", "2026-11-20", "12:00", "Massage Price: €50.00 with Maria Test (Confirmed)"))
	z := s.login("olga", olgaPass)

	home := s.get("/?date=2026-11-20", z)
	wantPage(t, "home", home, http.StatusOK, `<details class="collapse collapse-arrow bg-base-100 shadow-sm" aria-label="Записи в календаре">`, `<summary class="collapse-title min-h-14 font-semibold">Записи в календаре</summary>`, "10:00", "Lashes Coloring + Brows Coloring", "по записи €30", "15:30", "Не связано", "/clients/new?date=2026-11-20&amp;booking=b1")
	noClientData(t, "home", home.Body.String())
	if strings.Contains(home.Body.String(), "Massage") {
		t.Fatal("another person's booking shown")
	}

	form := s.get("/clients/new?date=2026-11-20&booking=b1", z)
	wantPage(t, "form from booking", form, http.StatusOK, "По записи 10:00 · Lashes Coloring + Brows Coloring · €30", `name="booking" value="b1"`, `name="services" value="30"`)
	noClientData(t, "form", form.Body.String())

	in := clientForm("booked-http-client-1", "30")
	in.Set("booking", "b1")
	res := s.post("/clients", in, z)
	if res.Code != http.StatusSeeOther {
		t.Fatalf("save %d %s", res.Code, res.Body.String())
	}
	entry := strings.Split(res.Header().Get("Location"), "?")[0]
	wantPage(t, "linked home", s.get("/?date=2026-11-20", z), http.StatusOK, "Связано</span>", `href="`+entry+`"`)
	wantPage(t, "entry", s.get(entry, z), http.StatusOK, "Клиент связан с записью в календаре", "Отвязать от записи")

	twice := clientForm("booked-http-client-2", "30")
	twice.Set("booking", "b1")
	wantPage(t, "second entry", s.post("/clients", twice, z), http.StatusUnprocessableEntity, "уже связана с другим клиентом")
}

func TestARetryIsAnsweredWithoutAskingTheCalendar(t *testing.T) {
	s, src := bookedSite(t)
	src.Put(olgaCal, timely("b1", "2026-11-20", "10:00", "Wax Price: €30.00 with Olga Test (Confirmed)"))
	z := s.login("olga", olgaPass)
	in := clientForm("booked-retry-client-1", "30")
	in.Set("booking", "b1")
	first := s.post("/clients", in, z)
	if first.Code != http.StatusSeeOther {
		t.Fatalf("save %d %s", first.Code, first.Body.String())
	}
	src.Delete(olgaCal, "b1")
	src.Fail(errors.New("calendar down"))
	gets := src.Gets()
	for _, when := range []string{"after deletion and outage", "after the month closed"} {
		retry := s.post("/clients", in, z)
		if retry.Code != http.StatusSeeOther || retry.Header().Get("Location") != first.Header().Get("Location") {
			t.Fatalf("%s: retry %d %s", when, retry.Code, retry.Header().Get("Location"))
		}
		if src.Gets() != gets {
			t.Fatalf("%s: the calendar was asked", when)
		}
		*s.clock = time.Date(2026, 12, 1, 10, 0, 0, 0, time.UTC)
		if _, err := s.st.CloseMonth(ctx, s.anna.Actor(), domain.Month{Year: 2026, Mon: time.November}); err != nil && when == "after deletion and outage" {
			t.Fatal(err)
		}
	}
	changed := in
	changed.Set("booking", "b2")
	wantPage(t, "changed replay after close", s.post("/clients", changed, z), http.StatusUnprocessableEntity, "Месяц закрыт")
}

func TestABookingThatCantBeCheckedIsNeverDroppedSilently(t *testing.T) {
	s, src := bookedSite(t)
	src.Put(olgaCal, timely("b1", "2026-11-20", "10:00", "Wax Price: €30.00 with Olga Test (Confirmed)"))
	src.Put(mariaCal, timely("m1", "2026-11-20", "12:00", "Massage Price: €50.00 with Maria Test (Confirmed)"))
	src.Put(olgaCal, timely("y1", "2026-11-19", "12:00", "Wax Price: €30.00 with Olga Test (Confirmed)"))
	z := s.login("olga", olgaPass)
	for name, event := range map[string]string{"forged": "nope", "another person's": "m1", "another day's": "y1", "malformed": "../x"} {
		in := clientForm("refused-booking-"+strings.ReplaceAll(strings.ReplaceAll(name, "'", ""), " ", "-"), "30")
		in.Set("booking", event)
		wantPage(t, name, s.post("/clients", in, z), http.StatusUnprocessableEntity, "Запись не найдена в календаре", `name="nobooking"`)
	}
	src.Fail(errors.New("calendar down"))
	in := clientForm("booking-outage-client", "30")
	in.Set("booking", "b1")
	wantPage(t, "outage", s.post("/clients", in, z), http.StatusUnprocessableEntity, "Записи недоступны. Отметьте «Сохранить без привязки к записи»", `name="nobooking"`)
	in.Set("nobooking", "1")
	saved := s.post("/clients", in, z)
	if saved.Code != http.StatusSeeOther {
		t.Fatalf("without link %d %s", saved.Code, saved.Body.String())
	}
	page := s.get(strings.Split(saved.Header().Get("Location"), "?")[0], z)
	if strings.Contains(page.Body.String(), "Клиент связан с записью") {
		t.Fatal("saved with a link after choosing none")
	}
	wantPage(t, "home in an outage", s.get("/?date=2026-11-20", z), http.StatusOK, "Записи недоступны", "Добавить клиента")
}

func TestAClientEnteredByHandIsLinkedAndUnlinked(t *testing.T) {
	s, src := bookedSite(t)
	src.Put(olgaCal, timely("b1", "2026-11-20", "10:00", "Wax Price: €30.00 with Olga Test (Confirmed)"))
	z := s.login("olga", olgaPass)
	res := s.post("/clients", clientForm("manual-http-client-1", "30"), z)
	if res.Code != http.StatusSeeOther {
		t.Fatalf("save %d", res.Code)
	}
	entry := strings.Split(res.Header().Get("Location"), "?")[0]
	wantPage(t, "candidates", s.get("/?date=2026-11-20", z), http.StatusOK, "Связать с внесённым клиентом", `action="`+entry+`/link"`)

	maria := s.login("maria", mariaPass)
	wantPage(t, "someone else's", s.post(entry+"/link", url.Values{"booking": {"b1"}, "version": {"1"}}, maria), http.StatusForbidden)
	wantPage(t, "stale", s.post(entry+"/link", url.Values{"booking": {"b1"}, "version": {"7"}}, z), http.StatusUnprocessableEntity, "обновите страницу")
	if res := s.post(entry+"/link", url.Values{"booking": {"b1"}, "version": {"1"}}, z); res.Code != http.StatusSeeOther {
		t.Fatalf("link %d %s", res.Code, res.Body.String())
	}
	wantPage(t, "linked", s.get("/?date=2026-11-20", z), http.StatusOK, "Связано</span>")

	src.Delete(olgaCal, "b1")
	src.Fail(errors.New("calendar down"))
	gets := src.Gets()
	if res := s.post(entry+"/unlink", url.Values{"version": {"2"}}, z); res.Code != http.StatusSeeOther {
		t.Fatalf("unlink %d %s", res.Code, res.Body.String())
	}
	if src.Gets() != gets {
		t.Fatal("unlink asked the calendar")
	}
	if strings.Contains(s.get(entry, z).Body.String(), "Клиент связан с записью") {
		t.Fatal("still linked")
	}
}

func TestTheAdminDayReconcilesBookingsWithClients(t *testing.T) {
	s, src := bookedSite(t)
	src.Put(olgaCal, timely("b1", "2026-11-20", "10:00", "Wax Price: €30.00 with Olga Test (Confirmed)"))
	src.Put(olgaCal, timely("b2", "2026-11-20", "15:00", "Brows Price: €20.00 with Olga Test (Confirmed)"))
	src.Put(olgaCal, timely("odd", "2026-11-20", "16:00", "Brows Price: €20,00 with Olga Test (Confirmed)"))
	src.Put(mariaCal, timely("m1", "2026-11-20", "12:00", "Massage Price: €50.00 with Maria Test (Confirmed)"))
	z := s.login("olga", olgaPass)
	in := clientForm("admin-day-client-1", "30")
	in.Set("booking", "b1")
	if res := s.post("/clients", in, z); res.Code != http.StatusSeeOther {
		t.Fatalf("save %d", res.Code)
	}
	admin := s.login("anna", annaPass)
	page := s.get("/admin/day?date=2026-11-20", admin)
	wantPage(t, "admin day", page, http.StatusOK, `<details class="collapse collapse-arrow bg-base-100 shadow-sm" aria-label="Записи в календаре">`, ">Ольга</h3>", "Записей: 2 · Клиентов: 1 · Записей без клиента: 1 · Клиентов без записи: 0", ">Мария</h3>", "Записей: 1 · Клиентов: 0 · Записей без клиента: 1", "Не удалось прочитать записей в календаре: 1")
	noClientData(t, "admin day", page.Body.String())

	src.Put(olgaCal, timely("b1", "2026-11-19", "10:00", "Wax Price: €30.00 with Olga Test (Confirmed)")) // moved to the day before
	wantPage(t, "moved", s.get("/admin/day?date=2026-11-20", admin), http.StatusOK, "Связанная запись перенесена на")
	wantPage(t, "destination", s.get("/admin/day?date=2026-11-19", admin), http.StatusOK, "Связано с клиентом от", "Записей без клиента: 0")

	src.Delete(olgaCal, "b1")
	wantPage(t, "deleted", s.get("/admin/day?date=2026-11-20", admin), http.StatusOK, "Связанная запись удалена в календаре")
	src.Fail(errors.New("calendar down"))
	page = s.get("/admin/day?date=2026-11-20", admin)
	wantPage(t, "outage", page, http.StatusOK, "Записи недоступны")
	if strings.Contains(page.Body.String(), "удалена в календаре") {
		t.Fatal("an outage shown as a deletion")
	}
}

func TestLinkRulesAreCheckedBeforeAskingTheCalendar(t *testing.T) {
	s, src := bookedSite(t)
	src.Put(olgaCal, timely("b1", "2026-11-20", "10:00", "Wax Price: €30.00 with Olga Test (Confirmed)"))
	src.Put(olgaCal, timely("b2", "2026-11-20", "11:00", "Brows Price: €20.00 with Olga Test (Confirmed)"))
	src.Put(mariaCal, timely("m1", "2026-11-20", "12:00", "Massage Price: €50.00 with Maria Test (Confirmed)"))
	z := s.login("olga", olgaPass)
	res := s.post("/clients", clientForm("link-rules-client-1", "30"), z)
	entry := strings.Split(res.Header().Get("Location"), "?")[0]

	maria := s.login("maria", mariaPass)
	wantPage(t, "another person's entry with their event", s.post(entry+"/link", url.Values{"booking": {"m1"}, "version": {"1"}}, maria), http.StatusForbidden)
	wantPage(t, "own entry, another person's event", s.post(entry+"/link", url.Values{"booking": {"m1"}, "version": {"1"}}, z), http.StatusUnprocessableEntity, "Запись не найдена")

	admin := s.login("anna", annaPass)
	if res := s.post(entry+"/link", url.Values{"booking": {"b1"}, "version": {"1"}}, admin); res.Code != http.StatusSeeOther || res.Header().Get("Location") != "/admin/day?date=2026-11-20" {
		t.Fatalf("admin link %d %s", res.Code, res.Header().Get("Location"))
	}
	gets := src.Gets()
	if res := s.post(entry+"/link", url.Values{"booking": {"b1"}, "version": {"1"}}, z); res.Code != http.StatusSeeOther {
		t.Fatalf("repeat %d", res.Code)
	}
	wantPage(t, "other booking on a linked entry", s.post(entry+"/link", url.Values{"booking": {"b2"}, "version": {"2"}}, z), http.StatusUnprocessableEntity, "сначала отвяжите")
	wantPage(t, "stale version", s.post(entry+"/link", url.Values{"booking": {"b1"}, "version": {"1"}}, admin), http.StatusSeeOther)
	if src.Gets() != gets {
		t.Fatal("the calendar was asked for a repeat, a second booking or a stale link")
	}

	second := s.post("/clients", clientForm("link-rules-client-2", "20"), z)
	other := strings.Split(second.Header().Get("Location"), "?")[0]
	*s.clock = time.Date(2026, 12, 1, 10, 0, 0, 0, time.UTC)
	if _, err := s.st.CloseMonth(ctx, s.anna.Actor(), domain.Month{Year: 2026, Mon: time.November}); err != nil {
		t.Fatal(err)
	}
	src.Fail(errors.New("calendar down"))
	wantPage(t, "closed month link", s.post(other+"/link", url.Values{"booking": {"b2"}, "version": {"1"}}, z), http.StatusUnprocessableEntity, "Месяц закрыт")
	wantPage(t, "closed month unlink", s.post(entry+"/unlink", url.Values{"version": {"2"}}, admin), http.StatusUnprocessableEntity, "Месяц закрыт")
}

func TestStartingFromABookingAsksTheCalendarOnce(t *testing.T) {
	s, src := bookedSite(t)
	src.Put(olgaCal, timely("b1", "2026-11-20", "10:00", "Wax Price: €30.00 with Olga Test (Confirmed)"))
	z := s.login("olga", olgaPass)
	before := src.Gets()
	wantPage(t, "form", s.get("/clients/new?date=2026-11-20&booking=b1", z), http.StatusOK, `name="booking" value="b1"`)
	if src.Gets()-before != 1 {
		t.Fatalf("calendar asked %d times", src.Gets()-before)
	}
}

func TestTheBookingStaysOnTheFormThroughErrorsAndOutages(t *testing.T) {
	s, src := bookedSite(t)
	src.Put(olgaCal, timely("b1", "2026-11-20", "10:00", "Wax Price: €30.00 with Olga Test (Confirmed)"))
	z := s.login("olga", olgaPass)

	in := clientForm("kept-booking-client-1", "oops")
	in.Set("booking", "b1")
	wantPage(t, "amount error", s.post("/clients", in, z), http.StatusUnprocessableEntity, `value="oops"`, `name="booking" value="b1"`)

	src.Fail(errors.New("calendar down"))
	wantPage(t, "prefill outage", s.get("/clients/new?date=2026-11-20&booking=b1", z), http.StatusOK, `name="booking" value="b1"`, "Календарь не ответил")
	src.Fail(nil)
	wantPage(t, "unknown booking", s.get("/clients/new?date=2026-11-20&booking=nope", z), http.StatusOK, `name="booking" value="nope"`, "Запись не найдена в календаре")
	in.Set("services", "30")
	in.Set("booking", "nope")
	wantPage(t, "save with the unknown booking", s.post("/clients", in, z), http.StatusUnprocessableEntity, `name="nobooking"`)
}
