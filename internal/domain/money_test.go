package domain_test

import (
	"errors"
	"testing"

	"github.com/lieranderl/aestheticlab-income-app/internal/domain"
)

func TestAmountsTypedOnAPhoneAreReadAsCents(t *testing.T) {
	cases := map[string]domain.Cents{
		"":         0,
		"   ":      0,
		"0":        0,
		"600":      60000,
		"600,5":    60050,
		"600,50":   60050,
		"600.50":   60050,
		"0,05":     5,
		",5":       50,
		"1 234,50": 123450,
		"1 234":    123400, // no-break space from iOS number formatting
		"1 234":    123400, // narrow no-break space
		" 75 ":     7500,
		"€75":      7500,
		"75 €":     7500,
	}
	for in, want := range cases {
		got, err := domain.ParseAmount(in)
		if err != nil {
			t.Errorf("ParseAmount(%q) error: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("ParseAmount(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestUnreadableOrNegativeAmountsAreRejected(t *testing.T) {
	for _, in := range []string{"-5", "abc", "1,234,5", "1.2.3", "12,345", "5e3", "10,", "1 2", "99999999999"} {
		if _, err := domain.ParseAmount(in); !errors.Is(err, domain.ErrInvalidAmount) {
			t.Errorf("ParseAmount(%q) error = %v, want ErrInvalidAmount", in, err)
		}
	}
}

func TestAmountsAreShownTheRussianWay(t *testing.T) {
	cases := map[domain.Cents]string{
		0:         "€0",
		60000:     "€600",
		60050:     "€600,50",
		5:         "€0,05",
		123450:    "€1 234,50",
		100000000: "€1 000 000",
		-250:      "−€2,50",
	}
	for in, want := range cases {
		if got := domain.FormatEUR(in); got != want {
			t.Errorf("FormatEUR(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestPercentagesRoundHalfUpToTheCent(t *testing.T) {
	cases := []struct {
		base domain.Cents
		pct  domain.BasisPoints
		want domain.Cents
	}{
		{75000, 5000, 37500},   // 50% of €750
		{742100, 7000, 519470}, // 70% of €7 421
		{101, 5000, 51},        // €0,505 → €0,51
		{99, 5000, 50},         // €0,495 → €0,50
		{33, 3333, 11},         // 10,9989 → 11
		{0, 7000, 0},
		{12345, 0, 0},
		{12345, 10000, 12345},
	}
	for _, c := range cases {
		if got := domain.ApplyPercent(c.base, c.pct); got != c.want {
			t.Errorf("ApplyPercent(%d, %d) = %d, want %d", c.base, c.pct, got, c.want)
		}
	}
}
