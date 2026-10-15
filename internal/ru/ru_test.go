package ru_test

import (
	"testing"
	"time"

	"github.com/lieranderl/aestheticlab-income-app/internal/ru"
)

func TestMonthsAreNamedForHeaders(t *testing.T) {
	if got := ru.MonthYear(2026, time.October); got != "Октябрь 2026" {
		t.Errorf("MonthYear = %q", got)
	}
	if got := ru.MonthName(time.May); got != "Май" {
		t.Errorf("MonthName(May) = %q", got)
	}
	if got := ru.ShortMonthName(time.October); got != "Окт" {
		t.Errorf("ShortMonthName(October) = %q", got)
	}
	if got := ru.SinceMonth(2026, time.November); got != "с ноября 2026" {
		t.Errorf("SinceMonth = %q", got)
	}
	if got := ru.SinceMonth(2027, time.May); got != "с мая 2027" {
		t.Errorf("SinceMonth(May) = %q", got)
	}
}

func TestDatesUseDayMonthYearWithDots(t *testing.T) {
	d := time.Date(2026, time.October, 8, 0, 0, 0, 0, time.UTC)
	if got := ru.Date(d); got != "08.10.2026" {
		t.Errorf("Date = %q", got)
	}
	if got := ru.Weekday(d.Weekday()); got != "чт" {
		t.Errorf("Weekday = %q", got)
	}
	if got := ru.DayWithWeekday(d); got != "чт, 08.10" {
		t.Errorf("DayWithWeekday = %q", got)
	}
}

func TestCountsTakeTheRightRussianPlural(t *testing.T) {
	cases := map[int]string{
		0: "0 рабочих дней", 1: "1 рабочий день", 2: "2 рабочих дня", 4: "4 рабочих дня",
		5: "5 рабочих дней", 11: "11 рабочих дней", 12: "12 рабочих дней", 14: "14 рабочих дней",
		21: "21 рабочий день", 22: "22 рабочих дня", 25: "25 рабочих дней", 101: "101 рабочий день",
		111: "111 рабочих дней",
	}
	for n, want := range cases {
		if got := ru.Count(n, "рабочий день", "рабочих дня", "рабочих дней"); got != want {
			t.Errorf("Count(%d) = %q, want %q", n, got, want)
		}
	}
	if got := ru.Plural(-1, "a", "b", "c"); got != "a" {
		t.Errorf("Plural(-1) = %q, want the singular form as for 1", got)
	}
}
