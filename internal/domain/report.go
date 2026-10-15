package domain

import (
	"fmt"
	"maps"
	"sort"
)

// Destination is where a payment method's money lands.
type Destination string

const (
	// ToKassaCard money is already on «Касса — карта салона».
	ToKassaCard Destination = "kassa_card"
	// ToCash money is split: part left in the till (cashToKassa), part kept.
	ToCash Destination = "cash"
	// Prepaid means a certificate sold earlier; the money is already in the kassa.
	Prepaid Destination = "prepaid"
)

// RevenueCategory carries the flags a report needs from a revenue category.
type RevenueCategory struct {
	CountsTowardSelf  bool // part of the «Себе» base
	IsCertificateSale bool // money held for a future service, not income yet
}

// PaymentMethod carries where a payment method's money lands.
type PaymentMethod struct {
	Destination Destination
}

// Catalog is the set of revenue categories and payment methods by ID, as
// they are when a report is saved (archived ones included).
type Catalog struct {
	Revenue  map[string]RevenueCategory
	Payments map[string]PaymentMethod
}

// ReportInput is what a person enters for one day. Nil pointers mean
// "use the default": for CashToKassa the suggestion less any transfer to
// the card, for TransferToCard nothing, for SelfOverride the computed share.
type ReportInput struct {
	Revenue        map[string]Cents
	Payments       map[string]Cents
	SelfPct        BasisPoints // from the person's terms in force for the month
	CashToKassa    *Cents      // «Наличными в кассу»: cash left in the till
	TransferToCard *Cents      // «Переводом на карту салона»: sent to the kassa card instead
	SelfOverride   *Cents
}

// Report is a day's figures, computed by the server and saved with the report.
type Report struct {
	Total            Cents
	SelfBase         Cents
	SelfPct          BasisPoints
	SelfAmount       Cents // «Себе»
	SelfOverridden   bool
	SalonShare       Cents // «Салону»
	CertificateSales Cents
	SalonIncome      Cents // «Салону» without certificate sales
	CardIn           Cents
	Prepaid          Cents
	Cash             Cents
	SuggestedToKassa Cents // what settles the day, in the till and on the card together
	CashToKassa      Cents // «Оставьте в кассе»: in the till
	TransferToCard   Cents // «Оставьте в кассе»: transferred to «Касса — карта салона»
	KassaCredit      Cents
	BalanceDelta     Cents // + the salon owes the person, − the person owes the salon
}

// Problem names why an input was rejected; the UI maps it to Russian text.
type Problem string

// Reasons an input can be rejected.
const (
	ProblemNegativeAmount        Problem = "negative_amount"
	ProblemUnknownCategory       Problem = "unknown_category"
	ProblemUnknownPaymentMethod  Problem = "unknown_payment_method"
	ProblemPaymentsMismatch      Problem = "payments_mismatch"
	ProblemCashToKassaOutOfRange Problem = "cash_to_kassa_out_of_range"
	ProblemSelfOutOfRange        Problem = "self_out_of_range"
	ProblemPercentOutOfRange     Problem = "percent_out_of_range"
	ProblemUnknownDestination    Problem = "unknown_destination"
)

// ValidationError is a rejected input: which field, why, and for a payment
// mismatch the difference (revenue − payments).
type ValidationError struct {
	Field      string
	Problem    Problem
	Difference Cents
}

func (e *ValidationError) Error() string {
	if e.Field == "" {
		return string(e.Problem)
	}
	return fmt.Sprintf("%s: %s", e.Field, e.Problem)
}

func invalid(field string, p Problem) *ValidationError {
	return &ValidationError{Field: field, Problem: p}
}

// ComputeReport validates a day's input and computes every derived figure
// (technical-solution §4). The result satisfies
// (Cash − CashToKassa − TransferToCard) + BalanceDelta == SelfAmount.
func ComputeReport(in ReportInput, cat Catalog) (Report, error) {
	if in.SelfPct < 0 || in.SelfPct > 10000 {
		return Report{}, invalid("selfPct", ProblemPercentOutOfRange)
	}
	r := Report{SelfPct: in.SelfPct}
	for _, id := range sortedKeys(in.Revenue) {
		amount := in.Revenue[id]
		c, ok := cat.Revenue[id]
		switch {
		case !ok:
			return Report{}, invalid("r_"+id, ProblemUnknownCategory)
		case amount < 0:
			return Report{}, invalid("r_"+id, ProblemNegativeAmount)
		}
		r.Total += amount
		if c.CountsTowardSelf && !c.IsCertificateSale {
			r.SelfBase += amount
		}
		if c.IsCertificateSale {
			r.CertificateSales += amount
		}
	}
	for _, id := range sortedKeys(in.Payments) {
		amount := in.Payments[id]
		m, ok := cat.Payments[id]
		switch {
		case !ok:
			return Report{}, invalid("p_"+id, ProblemUnknownPaymentMethod)
		case amount < 0:
			return Report{}, invalid("p_"+id, ProblemNegativeAmount)
		}
		switch m.Destination {
		case ToKassaCard:
			r.CardIn += amount
		case Prepaid:
			r.Prepaid += amount
		case ToCash:
			r.Cash += amount
		default: // misconfigured: never guess where money went
			return Report{}, invalid("p_"+id, ProblemUnknownDestination)
		}
	}
	if paid := r.CardIn + r.Prepaid + r.Cash; paid != r.Total {
		return Report{}, &ValidationError{Field: "payments", Problem: ProblemPaymentsMismatch, Difference: r.Total - paid}
	}

	r.SelfAmount = ApplyPercent(r.SelfBase, r.SelfPct)
	if in.SelfOverride != nil {
		if *in.SelfOverride < 0 || *in.SelfOverride > r.Total {
			return Report{}, invalid("selfOverride", ProblemSelfOutOfRange)
		}
		r.SelfAmount, r.SelfOverridden = *in.SelfOverride, true
	}
	r.SalonShare = r.Total - r.SelfAmount
	r.SalonIncome = r.SalonShare - r.CertificateSales

	r.SuggestedToKassa = clamp(r.SalonShare-r.CardIn-r.Prepaid, 0, r.Cash)
	if in.TransferToCard != nil {
		if *in.TransferToCard < 0 || *in.TransferToCard > r.Cash {
			return Report{}, invalid("transferToCard", ProblemCashToKassaOutOfRange)
		}
		r.TransferToCard = *in.TransferToCard
	}
	r.CashToKassa = max(0, r.SuggestedToKassa-r.TransferToCard)
	if in.CashToKassa != nil {
		if *in.CashToKassa < 0 || *in.CashToKassa+r.TransferToCard > r.Cash {
			return Report{}, invalid("cashToKassa", ProblemCashToKassaOutOfRange)
		}
		r.CashToKassa = *in.CashToKassa
	}
	r.KassaCredit = r.CardIn + r.Prepaid + r.CashToKassa + r.TransferToCard
	r.BalanceDelta = r.KassaCredit - r.SalonShare
	return r, nil
}

// NewPersonReport computes a person's report and freezes the flags of the
// categories and payment methods it used, so later catalog changes never
// alter or obscure how a saved report was computed.
func NewPersonReport(person string, day Date, in ReportInput, cat Catalog) (PersonReport, error) {
	r, err := ComputeReport(in, cat)
	if err != nil {
		return PersonReport{}, err
	}
	flags := Catalog{Revenue: map[string]RevenueCategory{}, Payments: map[string]PaymentMethod{}}
	for id := range in.Revenue {
		flags.Revenue[id] = cat.Revenue[id]
	}
	for id := range in.Payments {
		flags.Payments[id] = cat.Payments[id]
	}
	return PersonReport{
		Person: person, Date: day, Report: r, Flags: flags,
		Revenue: maps.Clone(in.Revenue), Payments: maps.Clone(in.Payments),
	}, nil
}

func clamp(v, lo, hi Cents) Cents {
	return max(lo, min(v, hi))
}

// sortedKeys gives a stable order, so the first invalid field reported is
// always the same one.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
