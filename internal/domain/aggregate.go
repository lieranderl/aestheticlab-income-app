package domain

import (
	"maps"
	"slices"
)

// PersonSummary is one person's part of a day or month summary.
type PersonSummary struct {
	ByCategory  map[string]Cents
	KassaCash   Cents
	KassaCard   Cents
	Clients     int
	Total       Cents
	SelfBase    Cents
	Self        Cents
	SalonShare  Cents
	WorkingDays int
}

// Summary adds reports up for the day and month screens.
type Summary struct {
	KassaCash        Cents
	KassaCard        Cents
	Clients          int
	Total            Cents
	SelfBase         Cents // what «Себе» is computed from
	BonusBase        Cents // SelfBase without hand-set days: what the higher rate applies to
	Self             Cents // «Себе»
	SalonShare       Cents // «Салону»
	SalonIncome      Cents
	CertificateSales Cents
	CardIn           Cents
	Cash             Cents
	CashToKassa      Cents
	TransferToCard   Cents // the salon's share sent to the kassa card instead of the till
	Prepaid          Cents // paid with certificates
	MoneyReceived    Cents // card + cash: new money, unlike certificate payments
	CashKept         Cents // cash the person kept; CashKept + BalanceDelta == Self
	BalanceDelta     Cents // + the salon owes the person, − the person owes the salon
	ByCategory       map[string]Cents
	ByPayment        map[string]Cents
	WorkingDays      int
	People           map[string]PersonSummary
}

// Summarize adds up reports by category, payment method and person.
func Summarize(reports []PersonReport) Summary {
	s := Summary{ByCategory: map[string]Cents{}, ByPayment: map[string]Cents{}, People: map[string]PersonSummary{}}
	days := map[Date]bool{}
	personDays := map[string]map[Date]bool{}
	for _, pr := range reports {
		r := pr.Report
		s.KassaCash += r.CashToKassa
		s.KassaCard += r.CardIn + r.TransferToCard
		s.Total += r.Total
		s.SelfBase += r.SelfBase
		if !r.SelfOverridden {
			s.BonusBase += r.SelfBase
		}
		s.Self += r.SelfAmount
		s.SalonShare += r.SalonShare
		s.SalonIncome += r.SalonIncome
		s.CertificateSales += r.CertificateSales
		s.CardIn += r.CardIn
		s.Cash += r.Cash
		s.CashToKassa += r.CashToKassa
		s.TransferToCard += r.TransferToCard
		s.Prepaid += r.Prepaid
		s.CashKept += r.CashKept()
		s.BalanceDelta += r.BalanceDelta
		for c, v := range pr.Revenue {
			if v != 0 {
				s.ByCategory[c] += v
			}
		}
		for m, v := range pr.Payments {
			if v != 0 {
				s.ByPayment[m] += v
			}
		}
		days[pr.Date] = true
		if personDays[pr.Person] == nil {
			personDays[pr.Person] = map[Date]bool{}
		}
		personDays[pr.Person][pr.Date] = true
		p := s.People[pr.Person]
		if p.ByCategory == nil {
			p.ByCategory = map[string]Cents{}
		}
		for id, v := range pr.Revenue {
			p.ByCategory[id] += v
		}
		p.KassaCash += r.CashToKassa
		p.KassaCard += r.CardIn + r.TransferToCard
		if pr.Client {
			s.Clients++
			p.Clients++
		}
		p.Total += r.Total
		p.SelfBase += r.SelfBase
		p.Self += r.SelfAmount
		p.SalonShare += r.SalonShare
		s.People[pr.Person] = p
	}
	s.MoneyReceived = s.CardIn + s.Cash
	s.WorkingDays = len(days)
	for id, p := range s.People {
		p.WorkingDays = len(personDays[id])
		s.People[id] = p
	}
	return s
}

// SelfProgress is where a month's «Себе» base stands against the threshold
// above which the whole month earns the higher rate.
type SelfProgress struct {
	Base      Cents
	Threshold Cents
	HighPct   BasisPoints
	Reached   bool  // the base is strictly above the threshold
	Remaining Cents // what the base still needs to get above it; 0 once reached
}

// SelfProgressFor is the month's progress to the higher «Себе» rate, when
// the terms have one (SelfBonusFor accrues it at close).
func SelfProgressFor(base Cents, t Terms) (SelfProgress, bool) {
	if t.HighSelfFromCents <= 0 || t.HighSelfPct <= t.SelfPct {
		return SelfProgress{}, false
	}
	p := SelfProgress{Base: base, Threshold: t.HighSelfFromCents, HighPct: t.HighSelfPct, Reached: base > t.HighSelfFromCents}
	if !p.Reached {
		p.Remaining = t.HighSelfFromCents - base + 1
	}
	return p, true
}

// MissingReporters are the people expected to report who have no report
// among reports, sorted («Не сдали отчёт»).
func MissingReporters(expected []string, reports []PersonReport) []string {
	done := map[string]bool{}
	for _, r := range reports {
		done[r.Person] = true
	}
	var missing []string
	for _, p := range expected {
		if !done[p] {
			missing = append(missing, p)
		}
	}
	slices.Sort(missing)
	return missing
}

// ImportedMonth is a month of history from the Google Sheet (April–October
// 2026): totals only, no categories, payments or working days.
type ImportedMonth struct {
	RevenueByPerson map[string]Cents
	RevenueTotal    Cents
	SalonIncome     Cents
	Expenses        Cents
	ManagerSalary   Cents
	Profit          Cents
	Dividends       Cents
}

// TrendPoint is one month on the «Динамика» chart.
type TrendPoint struct {
	Month             Month
	HasData           bool
	Imported          bool // from the sheet: shown with an «импорт» badge
	StatsVersion      int  // 0 for legacy snapshots without client/category stats
	Clients           int
	Revenue           Cents
	Self              Cents
	SalonIncome       Cents
	Expenses          Cents
	Salaries          Cents
	SelfBonuses       Cents // the higher «Себе» rate's difference, a cost before profit
	Profit            Cents
	Dividends         Cents
	RevenueByPerson   map[string]Cents
	RevenueByCategory map[string]Cents
}

// Window is the n months ending with end, oldest first (3, 6 or 12).
func Window(end Month, n int) []Month {
	months := make([]Month, n)
	for i := n - 1; i >= 0; i-- {
		months[i] = end
		end = end.Prev()
	}
	return months
}

// Trend builds the chart series for window: app months from results
// (closed snapshots or open-month previews), else imported history, else
// an empty point.
func Trend(window []Month, results map[Month]MonthResult, imported map[Month]ImportedMonth) []TrendPoint {
	points := make([]TrendPoint, 0, len(window))
	for _, m := range window {
		p := TrendPoint{Month: m, RevenueByPerson: map[string]Cents{}, RevenueByCategory: map[string]Cents{}}
		if r, ok := results[m]; ok {
			p.HasData = true
			p.StatsVersion = r.StatsVersion
			p.Clients = r.Clients
			p.Revenue, p.Self, p.SalonIncome = r.Revenue, r.Self, r.SalonIncome
			p.Expenses, p.Salaries, p.Profit, p.Dividends = r.Expenses, r.Salaries, r.Profit, r.DividendPool
			p.SelfBonuses = r.SelfBonuses
			for id, pr := range r.People {
				if pr.Revenue != 0 {
					p.RevenueByPerson[id] = pr.Revenue
				}
			}
			p.RevenueByCategory = maps.Clone(r.RevenueByCategory)
		} else if im, ok := imported[m]; ok {
			p.HasData, p.Imported = true, true
			p.Revenue, p.SalonIncome, p.Expenses = im.RevenueTotal, im.SalonIncome, im.Expenses
			p.Salaries, p.Profit, p.Dividends = im.ManagerSalary, im.Profit, im.Dividends
			p.RevenueByPerson = maps.Clone(im.RevenueByPerson)
		}
		points = append(points, p)
	}
	return points
}
