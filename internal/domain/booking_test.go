package domain_test

import (
	"fmt"
	"html"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/lieranderl/aestheticlab-income-app/internal/domain"
)

// Fixtures copy the layout Timely writes into a synced staff calendar; every
// name, phone and address in them is made up.
const timelyContacts = "\nTel: +32000000000 / SMS: 000000000\nEmail: client@example.com\nLocation: Aesthetic Lab"

func TestATimelyBookingGivesItsServicesAndPrice(t *testing.T) {
	got := domain.ClassifyEvent("Olga Example",
		"Olga Example - Lashes Coloring + Brows Coloring Price: €30.00 with Maria Test (Confirmed)"+timelyContacts)
	if got.Kind != domain.EventBooking {
		t.Fatalf("kind = %v, want booking", got.Kind)
	}
	want := []string{"Lashes Coloring", "Brows Coloring"}
	if len(got.Booking.Services) != 2 || got.Booking.Services[0] != want[0] || got.Booking.Services[1] != want[1] {
		t.Fatalf("services = %q, want %q", got.Booking.Services, want)
	}
	if !got.Booking.PriceKnown || got.Booking.Price != 3000 {
		t.Fatalf("price = %d known=%v, want 3000 known", got.Booking.Price, got.Booking.PriceKnown)
	}
}

func TestTimelyEventsAreBookingsUnrelatedOrUnreadable(t *testing.T) {
	const client = "Olga Example"
	cases := []struct {
		name        string
		summary     string
		description string
		kind        domain.EventKind
		services    []string
		price       domain.Cents
		priceKnown  bool
	}{
		{"one service", client, client + " - Upper Lip Wax Price: €10.00 with Maria Test (Confirmed)" + timelyContacts,
			domain.EventBooking, []string{"Upper Lip Wax"}, 1000, true},
		{"price with thousands", client, client + " - Full Course Price: €1,250.00 with Maria Test (Confirmed)",
			domain.EventBooking, []string{"Full Course"}, 125000, true},
		{"whole euros", client, client + " - Consultation Price: €45 with Maria Test (Confirmed)",
			domain.EventBooking, []string{"Consultation"}, 4500, true},
		{"free service", client, client + " - Touch-up Price: €0.00 with Maria Test (Confirmed)",
			domain.EventBooking, []string{"Touch-up"}, 0, true},
		{"no price still counts", client, client + " - Consultation with Maria Test (Confirmed)",
			domain.EventBooking, []string{"Consultation"}, 0, false},
		{"html line breaks", client, client + " - Upper Lip Wax Price: €10.00 with Maria Test (Confirmed)<br>Tel: +32000000000",
			domain.EventBooking, []string{"Upper Lip Wax"}, 1000, true},
		{"html entities", client, client + " - Wax &amp; Tint Price: €20.00 with Maria Test (Confirmed)",
			domain.EventBooking, []string{"Wax & Tint"}, 2000, true},
		{"service named with 'with'", client, client + " - Peel with Mask Price: €50.00 with Maria Test (Confirmed)",
			domain.EventBooking, []string{"Peel with Mask"}, 5000, true},
		{"leading blank lines and CRLF", client, "\r\n" + client + " - Upper Lip Wax Price: €10.00 with Maria Test (Confirmed)\r\nTel: +32000000000",
			domain.EventBooking, []string{"Upper Lip Wax"}, 1000, true},

		{"busy block", "Busy Time with Maria Test", "", domain.EventUnrelated, nil, 0, false},
		{"busy block with text", "Busy Time with Maria Test", "Lunch Price: €0.00 with Maria Test (Confirmed)", domain.EventUnrelated, nil, 0, false},
		{"personal event", "Репетитор", "", domain.EventUnrelated, nil, 0, false},
		{"personal note", "Стрижка на 13:30", "Позвонить заранее", domain.EventUnrelated, nil, 0, false},

		{"other status", client, client + " - Upper Lip Wax Price: €10.00 with Maria Test (Cancelled)",
			domain.EventUnreadable, nil, 0, false},
		{"malformed price", client, client + " - Upper Lip Wax Price: €10,00 with Maria Test (Confirmed)",
			domain.EventUnreadable, nil, 0, false},
		{"price without euro", client, client + " - Upper Lip Wax Price: 10.00 with Maria Test (Confirmed)",
			domain.EventUnreadable, nil, 0, false},
		{"empty price", client, client + " - Upper Lip Wax Price: with Maria Test (Confirmed)",
			domain.EventUnreadable, nil, 0, false},
		{"empty service", client, client + " - Upper Lip Wax +  Price: €10.00 with Maria Test (Confirmed)",
			domain.EventUnreadable, nil, 0, false},
		{"no services", client, client + " - Price: €10.00 with Maria Test (Confirmed)",
			domain.EventUnreadable, nil, 0, false},
		{"price but not the Timely line", client, "Price: €10.00",
			domain.EventUnreadable, nil, 0, false},

		// Prices are read in Timely's one format or not at all, never guessed.
		{"blank price after euro", client, client + " - Wax Price: €  with Maria Test (Confirmed)", domain.EventUnreadable, nil, 0, false},
		{"double euro", client, client + " - Wax Price: €€ with Maria Test (Confirmed)", domain.EventUnreadable, nil, 0, false},
		{"spaced euros", client, client + " - Wax Price: € € with Maria Test (Confirmed)", domain.EventUnreadable, nil, 0, false},
		{"no-break space after euro", client, client + " - Wax Price: €  with Maria Test (Confirmed)", domain.EventUnreadable, nil, 0, false},
		{"cents without euros", client, client + " - Wax Price: €.50 with Maria Test (Confirmed)", domain.EventUnreadable, nil, 0, false},
		{"one decimal", client, client + " - Wax Price: €30.5 with Maria Test (Confirmed)", domain.EventUnreadable, nil, 0, false},
		{"space thousands", client, client + " - Wax Price: €1 250.00 with Maria Test (Confirmed)", domain.EventUnreadable, nil, 0, false},
		{"leading zero group", client, client + " - Wax Price: €0,250.00 with Maria Test (Confirmed)", domain.EventUnreadable, nil, 0, false},
		{"space after euro", client, client + " - Wax Price: € 30.00 with Maria Test (Confirmed)", domain.EventUnreadable, nil, 0, false},
		{"trailing euro", client, client + " - Wax Price: €30.00€ with Maria Test (Confirmed)", domain.EventUnreadable, nil, 0, false},
		{"European decimals", client, client + " - Wax Price: €1.250,00 with Maria Test (Confirmed)", domain.EventUnreadable, nil, 0, false},
		{"European thousands", client, client + " - Wax Price: €1.250 with Maria Test (Confirmed)", domain.EventUnreadable, nil, 0, false},
		{"comma thousands without cents", client, client + " - Wax Price: €1,250 with Maria Test (Confirmed)", domain.EventUnreadable, nil, 0, false},
		{"negative", client, client + " - Wax Price: €-5.00 with Maria Test (Confirmed)", domain.EventUnreadable, nil, 0, false},
		{"too large", client, client + " - Wax Price: €12345678.00 with Maria Test (Confirmed)", domain.EventUnreadable, nil, 0, false},

		// A changed price label is warned about, not absorbed into a service name.
		{"price without colon", client, client + " - Wax Price €30.00 with Maria Test (Confirmed)", domain.EventUnreadable, nil, 0, false},
		{"lowercase price", client, client + " - Wax price: €30.00 with Maria Test (Confirmed)", domain.EventUnreadable, nil, 0, false},
		{"euro amount in a service", client, client + " - Wax €30 with Maria Test (Confirmed)", domain.EventUnreadable, nil, 0, false},

		// Only a price label is a warning sign, not the letters "price".
		{"personal event mentioning price", "Встреча", "Обсудить price list", domain.EventUnrelated, nil, 0, false},
		{"personal event with an amount", "Купить крем", "Крем €20", domain.EventUnrelated, nil, 0, false},
		{"service containing price letters", client, client + " - Caprice Massage Price: €40.00 with Maria Test (Confirmed)",
			domain.EventBooking, []string{"Caprice Massage"}, 4000, true},
		{"price label off the Timely line", "Заметка", "Price: €10.00", domain.EventUnreadable, nil, 0, false},

		// The client is exactly the title; nothing of it may reach a service.
		{"client name with a dash", "Olga - VIP", "Olga - VIP - Lashes Price: €30.00 with Maria Test (Confirmed)",
			domain.EventBooking, []string{"Lashes"}, 3000, true},
		{"title is not the client", "Olga", "Olga - VIP - Lashes Price: €30.00 with Maria Test (Confirmed)",
			domain.EventUnreadable, nil, 0, false},
		{"title differs from the line", "Someone Else", client + " - Wax Price: €10.00 with Maria Test (Confirmed)",
			domain.EventUnreadable, nil, 0, false},
		{"empty title", "", client + " - Wax Price: €10.00 with Maria Test (Confirmed)",
			domain.EventUnreadable, nil, 0, false},
		{"title with spaces around", "  " + client + " ", client + " - Wax Price: €10.00 with Maria Test (Confirmed)",
			domain.EventBooking, []string{"Wax"}, 1000, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := domain.ClassifyEvent(tc.summary, tc.description)
			if got.Kind != tc.kind {
				t.Fatalf("kind = %v, want %v", got.Kind, tc.kind)
			}
			if !slices.Equal(got.Booking.Services, tc.services) {
				t.Errorf("services = %q, want %q", got.Booking.Services, tc.services)
			}
			if got.Booking.Price != tc.price || got.Booking.PriceKnown != tc.priceKnown {
				t.Errorf("price = %d known=%v, want %d known=%v", got.Booking.Price, got.Booking.PriceKnown, tc.price, tc.priceKnown)
			}
		})
	}
}

func TestOnlyTheFirstLineOfABookingIsRead(t *testing.T) {
	// The contact lines may contain anything, even another Timely-looking line.
	got := domain.ClassifyEvent("Olga Example",
		"Olga Example - Upper Lip Wax Price: €10.00 with Maria Test (Confirmed)\nOther - Fake Price: €99.00 with X (Confirmed)")
	if got.Kind != domain.EventBooking || got.Booking.Price != 1000 || !slices.Equal(got.Booking.Services, []string{"Upper Lip Wax"}) {
		t.Fatalf("got %+v", got)
	}
}

func TestASalonDayRunsFromMidnightToMidnightInBrussels(t *testing.T) {
	brussels, err := time.LoadLocation("Europe/Brussels")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		day        string
		start, end string // UTC
	}{
		{"2026-11-07", "2026-11-06T23:00:00Z", "2026-11-07T23:00:00Z"}, // winter, 24 h
		{"2026-07-15", "2026-07-14T22:00:00Z", "2026-07-15T22:00:00Z"}, // summer, 24 h
		{"2026-10-25", "2026-10-24T22:00:00Z", "2026-10-25T23:00:00Z"}, // clocks go back, 25 h
		{"2026-03-29", "2026-03-28T23:00:00Z", "2026-03-29T22:00:00Z"}, // clocks go forward, 23 h
	}
	for _, tc := range cases {
		start, end := date(t, tc.day).Window(brussels)
		if got := start.UTC().Format(time.RFC3339); got != tc.start {
			t.Errorf("%s start = %s, want %s", tc.day, got, tc.start)
		}
		if got := end.UTC().Format(time.RFC3339); got != tc.end {
			t.Errorf("%s end = %s, want %s", tc.day, got, tc.end)
		}
	}
}

func TestABookingBelongsToTheDayItStartsOnInBrussels(t *testing.T) {
	brussels, err := time.LoadLocation("Europe/Brussels")
	if err != nil {
		t.Fatal(err)
	}
	// Overlaps 7 November but starts on the 6th at 23:30 local time.
	start := time.Date(2026, 11, 6, 22, 30, 0, 0, time.UTC)
	if got := domain.Today(start, brussels); got != date(t, "2026-11-06") {
		t.Fatalf("day = %s, want 2026-11-06", got)
	}
	// 23:30 UTC on 7 November is already the 8th in Brussels.
	if got := domain.Today(time.Date(2026, 11, 7, 23, 30, 0, 0, time.UTC), brussels); got != date(t, "2026-11-08") {
		t.Fatalf("day = %s, want 2026-11-08", got)
	}
}

// Whatever a calendar holds, reading it never panics; a booking always has
// services, none of them carrying a " - " (a client name), a price label or €,
// and a known price is exactly the amount written after "Price: €".
func FuzzClassifyEvent(f *testing.F) {
	f.Add("Olga Example", "Olga Example - Upper Lip Wax Price: €10.00 with Maria Test (Confirmed)")
	f.Add("Olga Example", "Olga Example - Full Course Price: €1,250.00 with Maria Test (Confirmed)")
	f.Add("Olga", "Olga - Wax Price: €45 with Maria Test (Confirmed)")
	f.Add("Busy Time with Maria Test", "")
	f.Add("x", " -  with  ()")
	f.Add("a", "a - + Price: €1,0,0.00 with b (Confirmed)<br>")
	f.Add("a", "a - Wax Price: €  with b (Confirmed)")
	f.Add("Olga Example", "Olga Example - Upper Lip Wax Price: &#8364;10.00 with Maria Test (Confirmed)")
	f.Fuzz(func(t *testing.T, summary, description string) {
		got := domain.ClassifyEvent(summary, description)
		if got.Kind != domain.EventBooking {
			return
		}
		b := got.Booking
		if len(b.Services) == 0 || b.Price < 0 {
			t.Fatalf("booking %+v", b)
		}
		for _, s := range b.Services {
			if s == "" || strings.Contains(s, " - ") || priceLabel.MatchString(s) || strings.Contains(s, "€") {
				t.Fatalf("service %q in %q", s, b.Services)
			}
		}
		if b.PriceKnown && !writtenAs(description, b.Price) {
			t.Fatalf("price %d not written as such in %q", b.Price, description)
		}
	})
}

// writtenAs says the description's first line, read as plain text the way
// Timely's HTML is decoded, holds c as "Price: €1,250.00", "€1250.00" or, for
// whole euros, "€1250".
func writtenAs(description string, c domain.Cents) bool {
	line := ""
	for l := range strings.SplitSeq(html.UnescapeString(htmlBreak.ReplaceAllString(description, "\n")), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			line = l
			break
		}
	}
	forms := []string{timelyAmount(c), fmt.Sprintf("%d.%02d", c/100, c%100)}
	if c%100 == 0 {
		forms = append(forms, fmt.Sprint(c/100))
	}
	for _, a := range forms {
		if strings.Contains(line, "Price: €"+a+" ") {
			return true
		}
	}
	return false
}

var htmlBreak = regexp.MustCompile(`(?i)<br\s*/?>`)

// priceLabel is a price label left in a service name: "Price:" or "price €".
var priceLabel = regexp.MustCompile(`(?i)\bprice\b\s*(?::|€)`)

// timelyAmount writes cents with comma thousands: 1,250.00.
func timelyAmount(c domain.Cents) string {
	e := fmt.Sprint(c / 100)
	for i := len(e) - 3; i > 0; i -= 3 {
		e = e[:i] + "," + e[i:]
	}
	return fmt.Sprintf("%s.%02d", e, c%100)
}
