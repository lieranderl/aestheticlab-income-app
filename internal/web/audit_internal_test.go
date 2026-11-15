package web

import (
	"reflect"
	"testing"

	"github.com/lieranderl/aestheticlab-income-app/internal/store"
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

// Money audit follow-up: usageOnly never hides a real category change.
func TestUsageOnlyHidesOnlyTheCounter(t *testing.T) {
	base := `{"version":1,"name":"Услуги","kind":"revenue","order":1,"active":true,"usedCount":3,"countsTowardSelf":true}`
	row := func(after string) store.AuditEntry {
		return store.AuditEntry{Entity: store.EntityCategory, Action: store.ActionUpdate, Before: base, After: after}
	}
	if !usageOnly(row(`{"version":2,"name":"Услуги","kind":"revenue","order":1,"active":true,"usedCount":4,"countsTowardSelf":true}`)) {
		t.Error("a counter bump is not hidden")
	}
	for _, real := range []string{
		`{"version":2,"name":"Услуги и уход","kind":"revenue","order":1,"active":true,"usedCount":3,"countsTowardSelf":true}`,
		`{"version":2,"name":"Услуги","kind":"revenue","order":1,"active":true,"usedCount":3,"countsTowardSelf":false}`,
		`{"version":2,"name":"Услуги","kind":"revenue","order":2,"active":true,"usedCount":3,"countsTowardSelf":true}`,
		`{"version":2,"name":"Услуги","kind":"revenue","order":1,"active":false,"usedCount":3,"countsTowardSelf":true}`,
		`{"version":2,"name":"Услуги","kind":"revenue","order":1,"active":true,"usedCount":3,"countsTowardSelf":true,"deleted":true}`,
	} {
		if usageOnly(row(real)) {
			t.Errorf("a real change is hidden: %s", real)
		}
	}
}
