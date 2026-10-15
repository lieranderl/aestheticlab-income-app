package domain

import (
	"maps"
	"slices"
	"sort"
)

// Reasons a month cannot be settled.
const (
	ProblemOutsideMonth Problem = "outside_month"
	ProblemMissingTerms Problem = "missing_terms"
	// A report saved at another «Себе» rate than the month's terms: the
	// «Себе» bonus would pay the wrong difference.
	ProblemSelfRateMismatch Problem = "self_rate_mismatch"
)

// MonthInput is everything dated in one month, plus the balances it starts
// from (the previous month's snapshot, or zero in the start month, whose
// opening rows are among Movements).
type MonthInput struct {
	Month     Month
	Opening   Balances
	Reports   []PersonReport
	Movements []Movement
	Terms     map[string]TermsHistory // every person who may earn this month
}

// PersonResult is one person's month.
type PersonResult struct {
	StatsVersion      int
	RevenueByCategory map[string]Cents
	KassaCash         Cents
	KassaCard         Cents
	Clients           int
	Revenue           Cents
	Self              Cents // from the day reports
	SelfBase          Cents
	SelfBonus         Cents // the higher «Себе» rate's difference, above the threshold
	Salary            Cents
	Dividend          Cents
	Earned            Cents // «Себе» + bonus + salary + dividend
	WorkingDays       int
	Opening           Cents
	Closing           Cents
}

// SalonShare returns the salon's share of the person's revenue («Салону»).
func (p PersonResult) SalonShare() Cents {
	return p.Revenue - p.Self
}

// MonthResult replaces the sheet's Debit Credit tab (technical-solution §5.3).
type MonthResult struct {
	StatsVersion       int
	KassaCash          Cents
	KassaCard          Cents
	Clients            int
	Month              Month
	Revenue            Cents
	RevenueByCategory  map[string]Cents
	Self               Cents
	CertificateSales   Cents
	SalonIncome        Cents
	Expenses           Cents
	ExpensesByCategory map[string]Cents
	Salaries           Cents
	SelfBonuses        Cents
	Profit             Cents
	DividendPool       Cents
	WorkingDays        int
	People             map[string]PersonResult
}

// Closing is what closing a month writes: the result, the salary, «Себе»
// bonus and dividend accruals, the closing balances and the terms used.
type Closing struct {
	Result   MonthResult
	Accruals []Movement
	Balances Balances
	Terms    map[string]Terms
}

// CloseMonth settles one month: it refuses rows dated in other months and
// pending movements, applies the terms in force for the month, accrues
// salary, «Себе» bonuses and dividends on its last day and returns the closing balances
// (system-design §3.4).
func CloseMonth(in MonthInput) (Closing, error) {
	for _, r := range in.Reports {
		if r.Date.Month() != in.Month {
			return Closing{}, invalid("reports", ProblemOutsideMonth)
		}
	}
	for _, m := range in.Movements {
		if err := ValidateMovement(m); err != nil {
			return Closing{}, err
		}
		if m.Date.Month() != in.Month {
			return Closing{}, invalid("movements", ProblemOutsideMonth)
		}
		if m.Status == Pending {
			return Closing{}, invalid("movements", ProblemPendingMovements)
		}
	}
	return settle(in)
}

// PreviewMonth is the result an open month would have if it closed now,
// for the month screen and trends: pending rows are left out instead of
// refusing, and nothing is written. Rows from other months are ignored.
func PreviewMonth(in MonthInput) (MonthResult, error) {
	var reports []PersonReport
	for _, r := range in.Reports {
		if r.Date.Month() == in.Month {
			reports = append(reports, r)
		}
	}
	var moves []Movement
	for _, m := range in.Movements {
		if m.Date.Month() == in.Month && m.Status != Pending {
			moves = append(moves, m)
		}
	}
	in.Reports, in.Movements = reports, moves
	c, err := settle(in)
	return c.Result, err
}

// settle computes a month from rows already known to belong to it.
func settle(in MonthInput) (Closing, error) {
	terms := make(map[string]Terms, len(in.Terms))
	for _, p := range slices.Sorted(maps.Keys(in.Terms)) {
		t, ok := in.Terms[p].For(in.Month)
		if !ok {
			return Closing{}, invalid("terms:"+p, ProblemMissingTerms)
		}
		terms[p] = t
	}
	// Everyone with money in the month needs terms, or the balances would
	// hold their money while the settlement left them out.
	var involved []string
	for _, r := range in.Reports {
		involved = append(involved, r.Person)
	}
	for _, m := range in.Movements {
		if m.Status == Voided {
			continue
		}
		for _, p := range []Party{m.From, m.To} {
			if p.Kind == PartyPerson {
				involved = append(involved, p.ID)
			}
		}
	}
	for _, p := range slices.Sorted(maps.Keys(in.Opening.Persons)) {
		if in.Opening.Persons[p] != 0 {
			involved = append(involved, p)
		}
	}
	for _, p := range involved {
		if _, ok := terms[p]; !ok {
			return Closing{}, invalid("terms:"+p, ProblemMissingTerms)
		}
	}

	res := MonthResult{StatsVersion: 1, Month: in.Month, RevenueByCategory: map[string]Cents{}, ExpensesByCategory: map[string]Cents{}, People: map[string]PersonResult{}}
	days := map[Date]bool{}
	personDays := map[string]map[Date]bool{}
	bonusBase := map[string]Cents{}
	for _, r := range in.Reports {
		if r.Report.SelfPct != terms[r.Person].SelfPct {
			return Closing{}, invalid("reports", ProblemSelfRateMismatch)
		}
		if !r.Report.SelfOverridden {
			bonusBase[r.Person] += r.Report.SelfBase
		}
		res.KassaCash += r.Report.CashToKassa
		res.KassaCard += r.Report.CardIn + r.Report.TransferToCard
		res.Revenue += r.Report.Total
		res.Self += r.Report.SelfAmount
		for c, v := range r.Revenue {
			res.RevenueByCategory[c] += v
		}
		res.CertificateSales += r.Report.CertificateSales
		res.SalonIncome += r.Report.SalonIncome
		days[r.Date] = true
		if personDays[r.Person] == nil {
			personDays[r.Person] = map[Date]bool{}
		}
		personDays[r.Person][r.Date] = true
		p := res.People[r.Person]
		if p.RevenueByCategory == nil {
			p.RevenueByCategory = map[string]Cents{}
		}
		for id, v := range r.Revenue {
			p.RevenueByCategory[id] += v
		}
		p.KassaCash += r.Report.CashToKassa
		p.KassaCard += r.Report.CardIn + r.Report.TransferToCard
		if r.Client {
			res.Clients++
			p.Clients++
		}
		p.Revenue += r.Report.Total
		p.Self += r.Report.SelfAmount
		p.SelfBase += r.Report.SelfBase
		res.People[r.Person] = p
	}
	res.WorkingDays = len(days)
	for _, m := range in.Movements {
		if m.Status == Confirmed && m.Type == Expense {
			res.Expenses += m.Amount
			res.ExpensesByCategory[m.ExpenseCategory] += m.Amount
		}
	}

	weights := map[string]int64{}
	bonuses := map[string]Cents{}
	for p, t := range terms {
		res.Salaries += t.SalaryCents
		weights[p] = t.DividendWeight
		bonuses[p] = SelfBonusFor(res.People[p].SelfBase, bonusBase[p], t)
		res.SelfBonuses += bonuses[p]
	}
	res.Profit = res.SalonIncome - res.Expenses - res.Salaries - res.SelfBonuses
	res.DividendPool = max(0, res.Profit)
	dividends := SplitByWeight(res.DividendPool, weights)

	last := in.Month.LastDay()
	var accruals []Movement
	for _, p := range slices.Sorted(maps.Keys(terms)) {
		if s := terms[p].SalaryCents; s > 0 {
			accruals = append(accruals, Movement{Type: Salary, Status: Confirmed, Amount: s, From: Salon, To: Person(p), Date: last})
		}
		if b := bonuses[p]; b > 0 {
			accruals = append(accruals, Movement{Type: SelfBonus, Status: Confirmed, Amount: b, From: Salon, To: Person(p), Date: last})
		}
		if d := dividends[p]; d > 0 {
			accruals = append(accruals, Movement{Type: Dividend, Status: Confirmed, Amount: d, From: Salon, To: Person(p), Date: last})
		}
	}

	closing, err := Fold(in.Opening, in.Reports, append(slices.Clone(in.Movements), accruals...))
	if err != nil {
		return Closing{}, err
	}
	for p := range terms {
		pr := res.People[p]
		pr.StatsVersion = 1
		pr.Salary = terms[p].SalaryCents
		pr.SelfBonus = bonuses[p]
		pr.Dividend = dividends[p]
		pr.Earned = pr.Self + pr.SelfBonus + pr.Salary + pr.Dividend
		pr.WorkingDays = len(personDays[p])
		pr.Opening = in.Opening.Persons[p]
		pr.Closing = closing.Persons[p]
		res.People[p] = pr
	}
	return Closing{Result: res, Accruals: accruals, Balances: closing, Terms: terms}, nil
}

// SplitByWeight divides pool by integer weights with the largest-remainder
// method, so the parts always add up to pool exactly. Equal remainders go to
// the lowest ID first. Zero weights get nothing.
func SplitByWeight(pool Cents, weights map[string]int64) map[string]Cents {
	out := make(map[string]Cents, len(weights))
	var total int64
	for id, w := range weights {
		out[id] = 0
		if w > 0 {
			total += w
		}
	}
	if pool <= 0 || total == 0 {
		return out
	}
	type share struct {
		id  string
		rem int64
	}
	var shares []share
	var given Cents
	for id, w := range weights {
		if w <= 0 {
			continue
		}
		q := int64(pool) * w
		out[id] = Cents(q / total)
		given += out[id]
		shares = append(shares, share{id, q % total})
	}
	sort.Slice(shares, func(i, j int) bool {
		if shares[i].rem != shares[j].rem {
			return shares[i].rem > shares[j].rem
		}
		return shares[i].id < shares[j].id
	})
	for i := 0; given < pool; i++ {
		out[shares[i%len(shares)].id]++
		given++
	}
	return out
}

// ActionKind is what the settlement screen suggests for a person.
type ActionKind string

// Settlement suggestions (technical-solution §5.4).
const (
	SalonPays  ActionKind = "salon_pays"  // «Выплатить €X»
	PersonPays ActionKind = "person_pays" // «Внести в кассу €X»
	Settled    ActionKind = "settled"
)

// Action is a suggested settlement step; «Перенести» simply records none.
type Action struct {
	Kind   ActionKind
	Amount Cents
}

// SuggestAction turns a closing balance into the one action for the person.
func SuggestAction(balance Cents) Action {
	switch {
	case balance > 0:
		return Action{SalonPays, balance}
	case balance < 0:
		return Action{PersonPays, -balance}
	}
	return Action{Kind: Settled}
}
