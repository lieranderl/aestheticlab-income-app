// Package domain holds the Kassa money rules as pure functions: amounts,
// daily reports, balances, month settlement and aggregation. Values in,
// values out; no clock, storage or HTTP (see docs/technical-solution.md §4–5).
package domain

import (
	"errors"
	"strconv"
	"strings"
)

// Cents is an amount of euro cents. Money is never a float.
type Cents int64

// BasisPoints is a percentage in hundredths of a percent: 5000 is 50%.
type BasisPoints int64

// ErrInvalidAmount is returned for amounts that cannot be read as euros.
var ErrInvalidAmount = errors.New("invalid amount")

// maxIntegerDigits bounds typed amounts to €9 999 999, far above a salon day.
const maxIntegerDigits = 7

// ParseAmount reads an amount as typed on a phone: "600", "600,5", "600.50",
// "1 234,50" (any space kind as thousands separator), with or without "€".
// Empty means 0. Negative amounts and anything ambiguous are rejected.
func ParseAmount(s string) (Cents, error) {
	s = strings.TrimSpace(s)
	s = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(s, "€"), "€"))
	if s == "" {
		return 0, nil
	}
	intPart, fracPart, hasSep := cutDecimal(s)
	if strings.ContainsAny(fracPart, ",.") || (hasSep && (len(fracPart) == 0 || len(fracPart) > 2)) {
		return 0, ErrInvalidAmount
	}
	digits, ok := ungroup(intPart)
	if !ok || len(digits) > maxIntegerDigits || !allDigits(fracPart) {
		return 0, ErrInvalidAmount
	}
	if digits == "" {
		digits = "0"
	}
	euros, err := strconv.ParseInt(digits, 10, 64)
	if err != nil {
		return 0, ErrInvalidAmount
	}
	cents := int64(0)
	if fracPart != "" {
		if len(fracPart) == 1 {
			fracPart += "0"
		}
		cents, _ = strconv.ParseInt(fracPart, 10, 64) // two checked digits
	}
	return Cents(euros*100 + cents), nil
}

// cutDecimal splits at the first decimal separator, comma or point.
func cutDecimal(s string) (intPart, fracPart string, found bool) {
	if i := strings.IndexAny(s, ",."); i >= 0 {
		return s[:i], s[i+1:], true
	}
	return s, "", false
}

// ungroup removes thousands separators (space, no-break space, narrow
// no-break space) and accepts them only between groups of three digits.
func ungroup(s string) (string, bool) {
	groups := strings.FieldsFunc(s, func(r rune) bool { return r == ' ' || r == ' ' || r == ' ' })
	if len(groups) == 0 {
		return "", strings.TrimSpace(s) == ""
	}
	for i, g := range groups {
		if !allDigits(g) || g == "" || (i > 0 && len(g) != 3) || (len(groups) > 1 && i == 0 && len(g) > 3) {
			return "", false
		}
	}
	return strings.Join(groups, ""), true
}

func allDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// FormatEUR shows an amount the Russian way: "€1 234,50" with a narrow
// no-break space between thousands, "€600" for whole euros, "−€2,50" below 0.
func FormatEUR(c Cents) string {
	sign := ""
	if c < 0 {
		sign = "−"
		c = -c
	}
	euros := strconv.FormatInt(int64(c/100), 10)
	var b strings.Builder
	for i, r := range euros {
		if i > 0 && (len(euros)-i)%3 == 0 {
			b.WriteRune(' ')
		}
		b.WriteRune(r)
	}
	out := sign + "€" + b.String()
	if rest := c % 100; rest != 0 {
		out += "," + strconv.FormatInt(int64(rest)/10, 10) + strconv.FormatInt(int64(rest)%10, 10)
	}
	return out
}

// ApplyPercent returns pct of base, rounded half up to the cent. Both are
// non-negative in this domain (amounts and percentages are validated first).
func ApplyPercent(base Cents, pct BasisPoints) Cents {
	return Cents((int64(base)*int64(pct) + 5000) / 10000)
}
