package web

import (
	"github.com/lieranderl/aestheticlab-income-app/internal/domain"
	"github.com/lieranderl/aestheticlab-income-app/internal/views"
)

// audience is who a money view speaks to: the person about their own
// records, the admin about one person, or the admin about the whole salon.
type audience int

const (
	ownMoney audience = iota
	staffMoney
	salonMoney
)

// moneyView splits a period into its records' result, the money that
// actually moved, and the shares the rates expect. period is «дня» or
// «месяца».
func moneyView(s domain.Summary, period string, who audience) views.MoneyView {
	v := views.MoneyView{StatusLabel: "Итог " + period + " по записям"}
	v.Status, v.Tone = statusText(s.BalanceDelta, who)
	kept := "Осталось у вас наличными"
	switch who {
	case staffMoney:
		kept = "Наличными у сотрудника"
	case salonMoney:
		kept = "Наличными у сотрудников"
	}
	v.Fact = []views.MoneyLine{{Name: "Получено от клиентов", Value: domain.FormatEUR(s.MoneyReceived)}}
	if who == salonMoney {
		v.Fact = append(v.Fact, views.MoneyLine{Name: "Внесено в кассу", Value: domain.FormatEUR(s.KassaCash + s.KassaCard), Strong: true})
	}
	v.Fact = append(v.Fact,
		views.MoneyLine{Name: "В кассу — наличные", Value: domain.FormatEUR(s.KassaCash)},
		views.MoneyLine{Name: "В кассу — карта салона", Value: domain.FormatEUR(s.KassaCard)})
	if s.Prepaid > 0 {
		v.Fact = append(v.Fact, views.MoneyLine{Name: "Оплачено сертификатом", Value: domain.FormatEUR(s.Prepaid)})
	}
	v.Fact = append(v.Fact, views.MoneyLine{Name: kept, Value: domain.FormatEUR(s.CashKept), Strong: true})

	self := "«Себе»"
	switch who {
	case ownMoney:
		self = "Ваши «Себе»"
	case salonMoney:
		self = "«Себе» сотрудников"
	}
	v.Expected = []views.MoneyLine{{Name: "Выручка", Value: domain.FormatEUR(s.Total)}}
	if who == salonMoney {
		v.Expected = append(v.Expected, views.MoneyLine{Name: "«Салону»", Value: domain.FormatEUR(s.SalonShare)}, views.MoneyLine{Name: self, Value: domain.FormatEUR(s.Self)})
		v.StatusLabel = "Сверка " + period + " по записям"
		v.Status, v.Tone = salonStatus(s.OpenDebts())
		v.StatusNote = "Ожидалось салону " + domain.FormatEUR(s.SalonShare) + " · зачтено " + domain.FormatEUR(s.KassaCredit)
		if s.Prepaid > 0 {
			v.StatusNote += " (из них сертификатами " + domain.FormatEUR(s.Prepaid) + ")"
		}
		return v
	}
	v.Expected = append(v.Expected, views.MoneyLine{Name: self, Value: domain.FormatEUR(s.Self)}, views.MoneyLine{Name: "«Салону»", Value: domain.FormatEUR(s.SalonShare)})
	v.Explain = selfEquation(s.Self, s.CashKept, s.BalanceDelta, who)
	if who == ownMoney {
		v.StatusNote = "Взносы и выплаты здесь не учтены — они в балансе."
	}
	return v
}

// splitFact rewrites a month's «Факт» so the money received and the money
// paid into the kassa each show what they were for: services, certificates
// and other sales, then their total.
func splitFact(v *views.MoneyView, s domain.Summary, who audience) {
	if v.Status == "" {
		return
	}
	kept := "Наличными у сотрудника"
	if who == salonMoney {
		kept = "Наличными у сотрудников"
	}
	split := func(name string, m domain.MoneySplit, newGroup bool) []views.MoneyLine {
		return []views.MoneyLine{
			{Name: name + " (услуги)", Value: domain.FormatEUR(m.Services), NewGroup: newGroup},
			{Name: name + " (сертификаты)", Value: domain.FormatEUR(m.Certificates)},
			{Name: name + " (продажи)", Value: domain.FormatEUR(m.Sales)},
			{Name: name, Value: domain.FormatEUR(m.Total()), Strong: true},
		}
	}
	fact := split("Получено от клиентов", s.Received, false)
	fact = append(fact, split("Внесено в кассу", s.Deposited, true)...)
	fact = append(fact,
		views.MoneyLine{Name: "В кассу — наличные", Value: domain.FormatEUR(s.KassaCash), Sub: true},
		views.MoneyLine{Name: "В кассу — карта салона", Value: domain.FormatEUR(s.KassaCard), Sub: true})
	if s.Prepaid > 0 {
		fact = append(fact, views.MoneyLine{Name: "Оплачено сертификатом", Value: domain.FormatEUR(s.Prepaid), NewGroup: true})
	}
	fact = append(fact, views.MoneyLine{Name: kept, Value: domain.FormatEUR(s.CashKept), Strong: true, NewGroup: s.Prepaid == 0})
	v.Fact = fact
}

// freezeShares replaces the expected shares of a closed month with the
// figures its snapshot froze.
func freezeShares(v *views.MoneyView, revenue, self, salon domain.Cents, who audience) {
	label := "«Себе»"
	switch who {
	case ownMoney:
		label = "Ваши «Себе»"
	case salonMoney:
		label = "«Себе» сотрудников"
	}
	v.Expected = []views.MoneyLine{{Name: "Выручка", Value: domain.FormatEUR(revenue)}, {Name: label, Value: domain.FormatEUR(self)}, {Name: "«Салону»", Value: domain.FormatEUR(salon)}}
	v.Explain, v.Frozen = "", true
}

// salonStatus states both sides of the salon's open debts; opposite debts
// of different people never cancel out.
func salonStatus(toStaff, toSalon domain.Cents) (string, int) {
	switch {
	case toStaff > 0 && toSalon > 0:
		return "Салон должен сотрудникам " + domain.FormatEUR(toStaff) + " · сотрудники должны салону " + domain.FormatEUR(toSalon), 0
	case toStaff > 0:
		return "Салон должен сотрудникам " + domain.FormatEUR(toStaff), 1
	case toSalon > 0:
		return "Сотрудники должны салону " + domain.FormatEUR(toSalon), -1
	}
	return "Всё сдано: долгов нет", 0
}

// statusText is what records leave between a person and the salon, worded
// for who is reading.
func statusText(delta domain.Cents, who audience) (string, int) {
	switch who {
	case ownMoney:
		switch {
		case delta > 0:
			return "Салон должен вам " + domain.FormatEUR(delta), 1
		case delta < 0:
			return "Вы должны салону " + domain.FormatEUR(-delta), -1
		}
		return "Расчёт с салоном: €0", 0
	}
	switch {
	case delta > 0:
		return "Салон должен " + domain.FormatEUR(delta), 1
	case delta < 0:
		return "Долг перед салоном " + domain.FormatEUR(-delta), -1
	}
	return "Без долга", 0
}

// selfEquation is how the expected «Себе» meets the money:
// «Себе» €213,50 = €100 у вас + €113,50 от салона.
func selfEquation(self, kept, delta domain.Cents, who audience) string {
	where := " у вас"
	if who != ownMoney {
		where = " у сотрудника"
	}
	s := "«Себе» " + domain.FormatEUR(self) + " = " + domain.FormatEUR(kept) + where
	switch {
	case delta > 0:
		s += " + " + domain.FormatEUR(delta) + " от салона"
	case delta < 0:
		s += " − " + domain.FormatEUR(-delta) + " салону"
	}
	return s
}

// recordLines are one record's fact and expected lines.
func recordLines(r domain.Report, who audience) (fact, expected string) {
	where := " · у вас "
	if who != ownMoney {
		where = " · у сотрудника "
	}
	fact = "Факт: в кассу " + domain.FormatEUR(r.CashToKassa+r.CardIn+r.TransferToCard) + where + domain.FormatEUR(r.CashKept())
	if r.Prepaid > 0 {
		fact += " · сертификатом " + domain.FormatEUR(r.Prepaid)
	}
	return fact, "Ожидается: «Себе» " + domain.FormatEUR(r.SelfAmount) + " · «Салону» " + domain.FormatEUR(r.SalonShare)
}

// atHigherRate shows a person's month at the higher «Себе» rate once its
// base is above the threshold: «Себе», «Салону» and the result move by the
// bonus that close accrues (SelfBonusFor, a self_bonus row), so the card
// reads «Себе» за ставку 70% instead of a separate bonus line. The records
// themselves keep the rate they were saved with.
// handSet days keep what they were given, and the label says so.
func atHigherRate(v *views.MoneyView, bonus, self, salon, delta domain.Cents, highPct domain.BasisPoints, handSet bool, who audience) {
	if bonus <= 0 || v.Status == "" {
		return
	}
	name := "«Себе»"
	if who == ownMoney {
		name = "Ваши «Себе»"
	}
	label := name
	if highPct > 0 {
		label += " за ставку " + percent(highPct)
		if handSet {
			label += " (кроме дней, заданных вручную)"
		}
	}
	setShares(v, name, label, self+bonus, salon-bonus)
	v.Status, v.Tone = statusText(delta+bonus, who)
	if v.Explain != "" {
		v.Explain = selfEquation(self+bonus, self-delta, delta+bonus, who)
	}
}

// salonAtHigherRate does the same for the salon's card, with each person's
// bonus kept on their own side of the open debts.
func salonAtHigherRate(v *views.MoneyView, s domain.Summary, self, salon domain.Cents, bonuses map[string]domain.Cents) {
	var total domain.Cents
	for _, b := range bonuses {
		total += b
	}
	if total <= 0 || v.Status == "" {
		return
	}
	setShares(v, "«Себе» сотрудников", "«Себе» сотрудников", self+total, salon-total)
	v.Status, v.Tone = salonStatus(s.OpenDebtsWith(bonuses))
	v.StatusNote = "Ожидалось салону " + domain.FormatEUR(salon-total) + " · зачтено " + domain.FormatEUR(s.KassaCredit)
	if s.Prepaid > 0 {
		v.StatusNote += " (из них сертификатами " + domain.FormatEUR(s.Prepaid) + ")"
	}
}

// setShares rewrites the «Себе» and «Салону» lines of the expected shares.
func setShares(v *views.MoneyView, selfName, selfLabel string, self, salon domain.Cents) {
	for i, l := range v.Expected {
		switch l.Name {
		case selfName:
			v.Expected[i].Name, v.Expected[i].Value = selfLabel, domain.FormatEUR(self)
		case "«Салону»":
			v.Expected[i].Value = domain.FormatEUR(salon)
		}
	}
}
