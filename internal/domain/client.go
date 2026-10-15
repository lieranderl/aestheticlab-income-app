package domain

// ClientInput records service value and actual contributions, without asking
// how the customer paid. CertificatePaid is included in Services.
type ClientInput struct {
	Services          Cents            `firestore:"services" json:"services"`
	Extras            map[string]Cents `firestore:"extras" json:"extras"`
	ExtrasDestination Destination      `firestore:"extrasDestination" json:"extrasDestination"`
	ServiceCash       Cents            `firestore:"serviceCash" json:"serviceCash"`
	ServiceCard       Cents            `firestore:"serviceCard" json:"serviceCard"`
	CertificatePaid   Cents            `firestore:"certificatePaid" json:"certificatePaid"`
}

// NewClientReport reuses the report/balance rules. Every extra sale goes to
// the salon; only services earn Self. A redemption is already funded.
func NewClientReport(person string, day Date, in ClientInput, pct BasisPoints) (PersonReport, error) {
	for field, amount := range map[string]Cents{"services": in.Services, "serviceCash": in.ServiceCash, "serviceCard": in.ServiceCard, "certificatePaid": in.CertificatePaid} {
		if amount < 0 || amount > 999999999 {
			return PersonReport{}, invalid(field, ProblemNegativeAmount)
		}
	}
	if in.CertificatePaid > in.Services {
		return PersonReport{}, invalid("certificatePaid", ProblemPaymentsMismatch)
	}
	if in.ServiceCash+in.ServiceCard > in.Services-in.CertificatePaid {
		return PersonReport{}, invalid("serviceCash", ProblemCashToKassaOutOfRange)
	}
	revenue := map[string]Cents{"services": in.Services}
	cat := Catalog{Revenue: map[string]RevenueCategory{"services": {CountsTowardSelf: true}}, Payments: map[string]PaymentMethod{"card": {Destination: ToKassaCard}, "cash": {Destination: ToCash}, "paid_by_certificate": {Destination: Prepaid}}}
	var extras Cents
	for _, id := range sortedKeys(in.Extras) {
		amount := in.Extras[id]
		if id != "creams" && id != "oil" && id != "certificates" {
			return PersonReport{}, invalid(id, ProblemUnknownCategory)
		}
		if amount < 0 || amount > 999999999 {
			return PersonReport{}, invalid(id, ProblemNegativeAmount)
		}
		revenue[id] = amount
		extras += amount
		cat.Revenue[id] = RevenueCategory{IsCertificateSale: id == "certificates"}
	}
	var extraCash, extraCard Cents
	switch in.ExtrasDestination {
	case ToCash:
		extraCash = extras
	case ToKassaCard:
		extraCard = extras
	case "":
		if extras != 0 {
			return PersonReport{}, invalid("extrasDestination", ProblemUnknownDestination)
		}
	default:
		return PersonReport{}, invalid("extrasDestination", ProblemUnknownDestination)
	}
	cashToKassa := in.ServiceCash + extraCash
	r, err := NewPersonReport(person, day, ReportInput{Revenue: revenue, Payments: map[string]Cents{"card": in.ServiceCard + extraCard, "cash": in.Services - in.CertificatePaid - in.ServiceCard + extraCash, "paid_by_certificate": in.CertificatePaid}, SelfPct: pct, CashToKassa: &cashToKassa}, cat)
	r.Client = true
	return r, err
}
