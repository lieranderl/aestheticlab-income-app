// Package ru formats month names, dates and counts the Russian way for the
// Kassa UI (the mobile-ui skill's formatting table).
package ru

import (
	"fmt"
	"strconv"
	"time"
)

var months = [...]string{
	"Январь", "Февраль", "Март", "Апрель", "Май", "Июнь",
	"Июль", "Август", "Сентябрь", "Октябрь", "Ноябрь", "Декабрь",
}

var shortMonths = [...]string{
	"Янв", "Фев", "Мар", "Апр", "Май", "Июн",
	"Июл", "Авг", "Сен", "Окт", "Ноя", "Дек",
}

// genitive month names, for «с ноября 2026».
var monthsGenitive = [...]string{
	"января", "февраля", "марта", "апреля", "мая", "июня",
	"июля", "августа", "сентября", "октября", "ноября", "декабря",
}

var weekdays = [...]string{"вс", "пн", "вт", "ср", "чт", "пт", "сб"}

// MonthName is the capitalised nominative month name: «Октябрь».
func MonthName(m time.Month) string { return months[m-1] }

// ShortMonthName is the short month name: «Окт».
func ShortMonthName(m time.Month) string { return shortMonths[m-1] }

// MonthYear is a month header: «Октябрь 2026».
func MonthYear(year int, m time.Month) string { return MonthName(m) + " " + strconv.Itoa(year) }

// SinceMonth is when something starts: «с ноября 2026».
func SinceMonth(year int, m time.Month) string {
	return "с " + monthsGenitive[m-1] + " " + strconv.Itoa(year)
}

// Date is DD.MM.YYYY: «08.10.2026».
func Date(t time.Time) string { return t.Format("02.01.2006") }

// DayWithWeekday is a short list label: «чт, 08.10».
func DayWithWeekday(t time.Time) string { return weekdays[t.Weekday()] + ", " + t.Format("02.01") }

// Weekday is the short weekday: «пт».
func Weekday(d time.Weekday) string { return weekdays[d] }

// Plural picks the form for n: one (1, 21, 101…), few (2–4, 22–24…) or
// many (0, 5–20, 25…, 11–14).
func Plural(n int, one, few, many string) string {
	if n < 0 {
		n = -n
	}
	switch n10, n100 := n%10, n%100; {
	case n10 == 1 && n100 != 11:
		return one
	case n10 >= 2 && n10 <= 4 && (n100 < 12 || n100 > 14):
		return few
	}
	return many
}

// Count is n with the matching form: «21 рабочий день».
func Count(n int, one, few, many string) string {
	return fmt.Sprintf("%d %s", n, Plural(n, one, few, many))
}
