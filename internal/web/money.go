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
