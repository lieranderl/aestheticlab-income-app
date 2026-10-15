package web

import "testing"

func TestChartAxis(t *testing.T) {
	for _, tc := range []struct {
		name           string
		lo, hi, unit   int64
		wantLo, wantHi int64
		wantStep       int64
	}{
		{"revenue and a loss", -100000, 1200000, 100, -500000, 1500000, 500000},
		{"only positive", 0, 1234500, 100, 0, 1500000, 500000},
		{"all zero", 0, 0, 100, 0, 100, 100},
		{"small euros never step below €1", 0, 250, 100, 0, 300, 100},
		{"client counts", 0, 37, 1, 0, 40, 20},
		{"exact top", 0, 1000000, 100, 0, 1000000, 500000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lo, hi, step := chartAxis(tc.lo, tc.hi, tc.unit)
			if lo != tc.wantLo || hi != tc.wantHi || step != tc.wantStep {
				t.Fatalf("chartAxis(%d, %d, %d) = %d, %d, %d; want %d, %d, %d", tc.lo, tc.hi, tc.unit, lo, hi, step, tc.wantLo, tc.wantHi, tc.wantStep)
			}
		})
	}
}
