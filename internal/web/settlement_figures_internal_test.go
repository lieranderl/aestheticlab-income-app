package web

import (
	"slices"
	"testing"

	"github.com/lieranderl/aestheticlab-income-app/internal/domain"
)

func TestTheMonthResultShowsTheRecountAndTheCarriedLoss(t *testing.T) {
	rows := func(r domain.MonthResult) []string {
		var out []string
		for _, row := range settlementFigures(r) {
			out = append(out, row.Name+" "+row.Value)
		}
		return out
	}
	plain := rows(domain.MonthResult{SalonIncome: 150000, Salaries: 100000, Profit: 50000, DividendPool: 50000})
	for _, absent := range []string{"Расхождение кассы", "Убыток прошлых месяцев", "Убыток к покрытию"} {
		for _, row := range plain {
			if len(row) >= len(absent) && row[:len(absent)] == absent {
				t.Fatalf("a month without it shows %q", row)
			}
		}
	}
	got := rows(domain.MonthResult{SalonIncome: 150000, Salaries: 100000, KassaDifference: -3000, Profit: 47000, LossCarriedIn: 100000, LossCarriedOut: 53000})
	want := []string{"Доход салона €1 500", "Расходы €0", "Зарплаты €1 000", "Доплата «Себе» €0", "Расхождение кассы −€30", "Прибыль за месяц €470", "Убыток прошлых месяцев −€1 000", "Дивиденды €0", "Убыток к покрытию €530"}
	if !slices.Equal(got, want) {
		t.Fatalf("rows\n got %q\nwant %q", got, want)
	}
	if surplus := rows(domain.MonthResult{KassaDifference: 1000}); !slices.Contains(surplus, "Расхождение кассы +€10") {
		t.Fatalf("a surplus is not signed: %q", surplus)
	}
}
