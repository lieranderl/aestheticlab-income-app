package domain

import (
	"html"
	"regexp"
	"strconv"
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
	// <services> with <staff> (<Status>), after the "<client> - " prefix. The
	// staff name is after the last " with ".
	timelyRest = regexp.MustCompile(`^(.+) with .+ \(([^()]+)\)$`)
	// A line that looks like Timely's even if the client doesn't match.
	timelyLike  = regexp.MustCompile(`^.+ - .+ with .+ \([^()]+\)$`)
	timelyPrice = regexp.MustCompile(`^(.*?) Price: (.*)$`)
	// Timely's English amount: €30.00, whole euros €45, or €1,250.00 (comma
	// thousands only with cents, so "€1,250" is not guessed).
	timelyAmount = regexp.MustCompile(`^€(?:(0|[1-9]\d*)(?:\.(\d{2}))?|([1-9]\d{0,2}(?:,\d{3})+)\.(\d{2}))$`)
	htmlBreak    = regexp.MustCompile(`(?i)<br\s*/?>`)
)

// ClassifyEvent reads a synced Timely event. Only the description's first
// line is read. The title is compared, never returned: Timely sets it to the
// client's name, so the line must start with "<title> - " and exactly that
// prefix is cut off. Contact lines after the first are never looked at.
func ClassifyEvent(summary, description string) EventClass {
	summary = strings.TrimSpace(summary)
	if strings.HasPrefix(summary, "Busy Time with ") {
		return EventClass{Kind: EventUnrelated}
	}
	line := firstLine(description)
	afterClient, isClient := strings.CutPrefix(line, summary+" - ")
	m := timelyRest.FindStringSubmatch(afterClient)
	if summary == "" || !isClient || m == nil {
		if timelyLike.MatchString(line) || strings.Contains(strings.ToLower(line), "price") {
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
	if strings.Contains(strings.ToLower(rest), "price") || strings.Contains(rest, "€") {
		return EventClass{Kind: EventUnreadable} // a changed price label, not a service
	}
	if strings.Contains(rest, " - ") {
		return EventClass{Kind: EventUnreadable} // may be the rest of a client name
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

// parseTimelyPrice reads exactly Timely's format. Anything else is unreadable
// rather than guessed; ParseAmount is for phone input and accepts more.
func parseTimelyPrice(s string) (Cents, bool) {
	m := timelyAmount.FindStringSubmatch(s)
	if m == nil {
		return 0, false
	}
	euros, cents := m[1], m[2]
	if m[3] != "" {
		euros, cents = strings.ReplaceAll(m[3], ",", ""), m[4]
	}
	if len(euros) > maxIntegerDigits {
		return 0, false
	}
	e, err := strconv.ParseInt(euros, 10, 64)
	if err != nil {
		return 0, false
	}
	var c int64
	if cents != "" {
		c, _ = strconv.ParseInt(cents, 10, 64) // two checked digits
	}
	return Cents(e*100 + c), true
}
