package domain

// SettlementBreakdown explains a person's signed change during one month.
// Accrual ledger rows are excluded: the frozen or provisional result supplies them once.
// Reports is the sum of the report deltas; it splits into Self («Себе») and
// CashKept (≤ 0, the clients' cash the person kept), so Self + CashKept == Reports.
type SettlementBreakdown struct {
	Opening, Reports, Self, CashKept, Deposits, StaffExpenses, Salary, SelfBonus, Dividend, Payouts, Closing Cents
}

func BreakdownFor(uid string, opening Cents, reports []PersonReport, moves []Movement, result PersonResult) SettlementBreakdown {
	b := SettlementBreakdown{Opening: opening, Salary: result.Salary, SelfBonus: result.SelfBonus, Dividend: result.Dividend}
	for _, r := range reports {
		if r.Person == uid {
			b.Reports += r.Report.BalanceDelta
			b.Self += r.Report.SelfAmount
			b.CashKept -= r.Report.CashKept()
		}
	}
	for _, m := range moves {
		if m.Status != Confirmed {
			continue
		}
		switch m.Type {
		case Opening:
			if m.To == Person(uid) {
				b.Opening += m.Amount
			}
			if m.From == Person(uid) {
				b.Opening -= m.Amount
			}
		case Deposit:
			if m.From == Person(uid) {
				b.Deposits += m.Amount
			}
		case Expense:
			if m.From == Person(uid) {
				b.StaffExpenses += m.Amount
			}
		case Payout:
			if m.To == Person(uid) {
				b.Payouts -= m.Amount
			}
		}
	}
	b.Closing = b.Opening + b.Reports + b.Deposits + b.StaffExpenses + b.Salary + b.SelfBonus + b.Dividend + b.Payouts
	return b
}
