package domain

import (
	"fmt"
	"maps"
)

// Kassa account IDs seeded in production.
const (
	KassaCash = "kassa_cash" // «Касса — наличные»
	KassaCard = "kassa_card" // «Касса — карта салона»
)

// PartyKind is what a ledger row's From/To refers to.
type PartyKind string

// Kinds of ledger parties.
const (
	PartyAccount      PartyKind = "account"
	PartyPerson       PartyKind = "person"
	PartyOutside      PartyKind = "outside"
	PartySalon        PartyKind = "salon"
	PartyCertificates PartyKind = "certificates"
)

// Party is one side of a ledger row.
type Party struct {
	Kind PartyKind
	ID   string
}

// Account is a kassa account party.
func Account(id string) Party { return Party{PartyAccount, id} }

// Person is a staff member party.
func Person(uid string) Party { return Party{PartyPerson, uid} }

// Fixed parties.
var (
	Outside      = Party{Kind: PartyOutside}
	Salon        = Party{Kind: PartySalon}
	Certificates = Party{Kind: PartyCertificates}
)

// MovementType is the kind of ledger row (technical-solution §5.1).
type MovementType string

// Ledger row types.
const (
	Deposit    MovementType = "deposit"    // «Внесение»: person → kassa
	Payout     MovementType = "payout"     // «Выплата»: kassa → person
	Expense    MovementType = "expense"    // «Расход»: kassa or person → outside
	Transfer   MovementType = "transfer"   // «Перевод»: kassa ↔ kassa
	Adjustment MovementType = "adjustment" // «Корректировка»: outside ↔ kassa
	Opening    MovementType = "opening"    // «Начальный остаток»
	Salary     MovementType = "salary"     // accrual at month close
	Dividend   MovementType = "dividend"   // accrual at month close
	SelfBonus  MovementType = "self_bonus" // accrual at month close: the higher «Себе» rate's difference
)

// IsAccrual says whether rows of type t are written only by closing a month
// (and voided only by reopening it).
func IsAccrual(t MovementType) bool { return t == Salary || t == Dividend || t == SelfBonus }

// MovementStatus says whether a ledger row counts.
type MovementStatus string

// Only confirmed rows count toward balances.
const (
	Pending   MovementStatus = "pending"
	Confirmed MovementStatus = "confirmed"
	Voided    MovementStatus = "voided"
)

// Movement is one ledger row.
type Movement struct {
	Type            MovementType
	Status          MovementStatus
	Amount          Cents
	From, To        Party
	ExpenseCategory string
	Date            Date
}

// PersonReport is a saved report with its author, day, the amounts as
// entered and the figures computed from them.
type PersonReport struct {
	Client   bool // per-client entry; legacy daily reports do not invent a client count
	Person   string
	Date     Date
	Report   Report
	Revenue  map[string]Cents
	Payments map[string]Cents
	Flags    Catalog // the category flags and payment destinations used, frozen at save time
}

// Balances are the kassa accounts, each person's balance with the salon
// (+ the salon owes them) and the certificates sold but not yet used.
type Balances struct {
	Accounts                map[string]Cents
	Persons                 map[string]Cents
	CertificatesOutstanding Cents
}

// KassaTotal is the money in all kassa accounts together.
func (b Balances) KassaTotal() Cents {
	var total Cents
	for _, v := range b.Accounts {
		total += v
	}
	return total
}

// CertificatesNeedChecking is true when more certificates were used than
// sold plus the opening amount: «Проверьте сертификаты».
func (b Balances) CertificatesNeedChecking() bool { return b.CertificatesOutstanding < 0 }

func (b Balances) clone() Balances {
	out := Balances{
		Accounts:                maps.Clone(b.Accounts),
		Persons:                 maps.Clone(b.Persons),
		CertificatesOutstanding: b.CertificatesOutstanding,
	}
	if out.Accounts == nil {
		out.Accounts = map[string]Cents{}
	}
	if out.Persons == nil {
		out.Persons = map[string]Cents{}
	}
	return out
}

// Fold applies reports and confirmed ledger rows to start and returns the
// new balances; start itself is not changed. Pending and voided rows are
// ignored. A malformed row is an error, not silently skipped.
func Fold(start Balances, reports []PersonReport, movements []Movement) (Balances, error) {
	b := start.clone()
	for _, pr := range reports {
		r := pr.Report
		b.Accounts[KassaCard] += r.CardIn + r.TransferToCard
		b.Accounts[KassaCash] += r.CashToKassa
		b.Persons[pr.Person] += r.BalanceDelta
		b.CertificatesOutstanding += r.CertificateSales - r.Prepaid
	}
	for i, m := range movements {
		if err := ValidateMovement(m); err != nil {
			return Balances{}, fmt.Errorf("movement %d: %w", i, err)
		}
		if m.Status == Confirmed {
			b.apply(m)
		}
	}
	return b, nil
}

// apply books one valid, confirmed row.
func (b *Balances) apply(m Movement) {
	switch m.Type {
	case Deposit:
		b.Accounts[m.To.ID] += m.Amount
		b.Persons[m.From.ID] += m.Amount // she gave the salon money
	case Payout:
		b.Accounts[m.From.ID] -= m.Amount
		b.Persons[m.To.ID] -= m.Amount // the salon settled what it owed
	case Expense:
		if m.From.Kind == PartyAccount {
			b.Accounts[m.From.ID] -= m.Amount
		} else {
			b.Persons[m.From.ID] += m.Amount // she paid for the salon
		}
	case Transfer:
		b.Accounts[m.From.ID] -= m.Amount
		b.Accounts[m.To.ID] += m.Amount
	case Adjustment:
		if m.To.Kind == PartyAccount {
			b.Accounts[m.To.ID] += m.Amount
		} else {
			b.Accounts[m.From.ID] -= m.Amount
		}
	case Opening:
		switch m.To.Kind {
		case PartyAccount:
			b.Accounts[m.To.ID] += m.Amount
		case PartyPerson:
			b.Persons[m.To.ID] += m.Amount
		default:
			b.CertificatesOutstanding += m.Amount
		}
	case Salary, Dividend, SelfBonus:
		b.Persons[m.To.ID] += m.Amount // the salon now owes them this
	}
}

// ValidateMovement checks a ledger row's shape: positive amount (a person's
// opening may be negative), the right parties for its type, and an expense
// category on expenses.
func ValidateMovement(m Movement) error {
	switch m.Status {
	case Pending, Confirmed, Voided:
	default: // an unknown status must not be silently skipped by Fold or close
		return invalid("status", ProblemUnknownStatus)
	}
	from, to := m.From.Kind, m.To.Kind
	var ok bool
	switch m.Type {
	case Deposit:
		ok = from == PartyPerson && to == PartyAccount
	case Payout:
		ok = from == PartyAccount && to == PartyPerson
	case Expense:
		ok = (from == PartyAccount || from == PartyPerson) && to == PartyOutside && m.ExpenseCategory != ""
	case Transfer:
		ok = from == PartyAccount && to == PartyAccount && m.From.ID != m.To.ID
	case Adjustment:
		ok = (from == PartyOutside && to == PartyAccount) || (from == PartyAccount && to == PartyOutside)
	case Opening:
		ok = from == PartySalon && (to == PartyAccount || to == PartyPerson || to == PartyCertificates)
		if ok && m.Amount != 0 && (to == PartyPerson || m.Amount > 0) {
			return nil // a person may start owing the salon
		}
		return invalid("movement", ProblemNegativeAmount)
	case Salary, Dividend, SelfBonus:
		ok = from == PartySalon && to == PartyPerson
	default:
		return invalid("type", ProblemUnknownMovement)
	}
	switch {
	case !ok:
		return invalid("movement", ProblemMalformedMovement)
	case m.Amount <= 0:
		return invalid("amount", ProblemNegativeAmount)
	}
	return nil
}

// Reasons a ledger row is rejected.
const (
	ProblemUnknownMovement   Problem = "unknown_movement"
	ProblemMalformedMovement Problem = "malformed_movement"
	ProblemUnknownStatus     Problem = "unknown_status"
)
