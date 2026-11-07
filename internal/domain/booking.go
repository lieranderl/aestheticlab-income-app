package domain

import (
	"html"
	"regexp"
	"strings"
)

// EventKind says what a synced calendar event is to the kassa.
type EventKind int

const (
	// EventUnrelated is not a Timely booking: a «Busy Time» block or a
	// personal event. It is ignored.
	EventUnrelated EventKind = iota
	// EventBooking is a confirmed Timely booking.
	EventBooking
	// EventUnreadable looks like a Timely booking but can't be read. It is
	// never counted as a booking; screens warn about it instead.
	EventUnreadable
)

// Booking is what the kassa takes from a Timely booking: never the client.
type Booking struct {
	Services   []string
	Price      Cents // Timely's list price: a suggestion, never money
	PriceKnown bool
}

// EventClass is the outcome of reading one calendar event.
type EventClass struct {
	Kind    EventKind
	Booking Booking // set only for EventBooking
}

// bookingStatus is the only status Timely writes (owner, 2026-11-07).
const bookingStatus = "Confirmed"

var (
	// <client> - <services> with <staff> (<Status>). The client is the text
	// before the first " - "; the staff after the last " with ".
	timelyLine  = regexp.MustCompile(`^.+? - (.+) with .+ \(([^()]+)\)$`)
	timelyPrice = regexp.MustCompile(`^(.*?)\s*Price:\s*(.*)$`)
	htmlBreak   = regexp.MustCompile(`(?i)<br\s*/?>`)
)

// ClassifyEvent reads a synced Timely event. Only the description's first
// line is read; the title is used only to recognise «Busy Time» blocks, and
// the contact lines after the first are never looked at.
func ClassifyEvent(summary, description string) EventClass {
	if strings.HasPrefix(strings.TrimSpace(summary), "Busy Time with ") {
		return EventClass{Kind: EventUnrelated}
	}
	line := firstLine(description)
	m := timelyLine.FindStringSubmatch(line)
	if m == nil {
		if strings.Contains(line, "Price:") {
			return EventClass{Kind: EventUnreadable}
		}
		return EventClass{Kind: EventUnrelated}
	}
	rest, status := m[1], m[2]
	if status != bookingStatus {
		return EventClass{Kind: EventUnreadable}
	}
	var b Booking
	if p := timelyPrice.FindStringSubmatch(rest); p != nil {
		price, ok := parseTimelyPrice(p[2])
		if !ok {
			return EventClass{Kind: EventUnreadable}
		}
		rest, b.Price, b.PriceKnown = p[1], price, true
	}
	rest = strings.TrimSpace(rest)
	if strings.HasPrefix(rest, "+") || strings.HasSuffix(rest, "+") {
		return EventClass{Kind: EventUnreadable} // a service is missing
	}
	for s := range strings.SplitSeq(rest, " + ") {
		s = strings.TrimSpace(s)
		if s == "" {
			return EventClass{Kind: EventUnreadable}
		}
		b.Services = append(b.Services, s)
	}
	return EventClass{Kind: EventBooking, Booking: b}
}

// firstLine is the description's first non-empty line, as plain text.
func firstLine(description string) string {
	text := html.UnescapeString(htmlBreak.ReplaceAllString(description, "\n"))
	for l := range strings.SplitSeq(text, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			return l
		}
	}
	return ""
}

// timelyThousands is Timely's English amount with comma thousands: €1,250.00.
var timelyThousands = regexp.MustCompile(`^\d{1,3}(,\d{3})+\.\d{2}$`)

// parseTimelyPrice reads "€30.00" or "€1,250.00". Anything else is unreadable
// rather than guessed.
func parseTimelyPrice(s string) (Cents, bool) {
	s, ok := strings.CutPrefix(s, "€")
	if !ok || s == "" {
		return 0, false
	}
	if timelyThousands.MatchString(s) {
		s = strings.ReplaceAll(s, ",", "")
	} else if strings.Contains(s, ",") {
		return 0, false // "30,00" or "1,250": ambiguous in Timely's format
	}
	c, err := ParseAmount(s)
	return c, err == nil
}
