package web

import (
	"reflect"
	"testing"

	"github.com/lieranderl/aestheticlab-income-app/internal/views"
)

// 2026-11-15 mobile review: report rows start from «€0», not «—», so a
// creation must treat both as unset and drop rows that stay at €0.
func TestCreatedValuesTreatsEuroZeroAsUnset(t *testing.T) {
	got := createdValues([]views.MoneyRow{
		{Name: "Услуги", Value: "€0 → €105"},
		{Name: "Переводом на карту салона", Value: "€0 → €0"},
		{Name: "Выручка", Value: "— → €105"},
		{Name: "Название", Value: "— → Кофе"},
		{Name: "Удалена", Value: "— → Нет"},
		{Name: "Сумма", Value: "€10 → €12"},
	}, nil)
	want := []views.MoneyRow{
		{Name: "Услуги", Value: "€105"},
		{Name: "Выручка", Value: "€105"},
		{Name: "Название", Value: "Кофе"},
		{Name: "Сумма", Value: "€10 → €12"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("createdValues:\n got %v\nwant %v", got, want)
	}
}
