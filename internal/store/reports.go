package store

import (
	"cmp"
	"context"
	"maps"
	"slices"
	"time"

	"github.com/lieranderl/aestheticlab-income-app/internal/domain"
)

// ReportEntry is a day's report as a person submits it.
type ReportEntry struct {
	Person         string
	Date           domain.Date
	Revenue        map[string]domain.Cents
	Payments       map[string]domain.Cents
	CashToKassa    *domain.Cents // nil: the suggested «Оставьте в кассе», less any transfer to the card
	TransferToCard *domain.Cents // nil: nothing transferred to «Касса — карта салона»
	SelfOverride   *domain.Cents // nil: «Себе» from the person's terms
	Version        int64         // the version the form was loaded with; 0 for a new report
}

// SavedReport is a stored report with the figures computed at save time.
type SavedReport struct {
	domain.PersonReport
	ID           string
	ClientNumber int64
	ClientInput  *domain.ClientInput
	Version      int64
	CreatedAt    time.Time
	UpdatedAt    time.Time
	UpdatedBy    string
}

type revenueFlagsRecord struct {
	CountsTowardSelf  bool `firestore:"countsTowardSelf"  json:"countsTowardSelf"`
	IsCertificateSale bool `firestore:"isCertificateSale" json:"isCertificateSale"`
}

type reportRecord struct {
	ClientToken         string                        `firestore:"clientToken,omitempty" json:"clientToken,omitempty"`
	ClientNumber        int64                         `firestore:"clientNumber,omitempty" json:"clientNumber,omitempty"`
	ClientInput         *domain.ClientInput           `firestore:"clientInput,omitempty" json:"clientInput,omitempty"`
	UserID              string                        `firestore:"userId"              json:"userId"`
	Date                string                        `firestore:"date"                json:"date"`
	Month               string                        `firestore:"month"               json:"month"`
	Revenue             map[string]domain.Cents       `firestore:"revenue"             json:"revenue"`
	Payments            map[string]domain.Cents       `firestore:"payments"            json:"payments"`
	RevenueFlags        map[string]revenueFlagsRecord `firestore:"revenueFlags"        json:"revenueFlags"`
	PaymentDestinations map[string]domain.Destination `firestore:"paymentDestinations" json:"paymentDestinations"`
	CashToKassa         domain.Cents                  `firestore:"cashToKassa"         json:"cashToKassa"`
	TransferToCard      domain.Cents                  `firestore:"transferToCard"      json:"transferToCard"`
	Total               domain.Cents                  `firestore:"total"               json:"total"`
	SelfBase            domain.Cents                  `firestore:"selfBase"            json:"selfBase"`
	SelfPct             domain.BasisPoints            `firestore:"selfPct"             json:"selfPct"`
	SelfAmount          domain.Cents                  `firestore:"selfAmount"          json:"selfAmount"`
	SelfOverridden      bool                          `firestore:"selfOverridden"      json:"selfOverridden"`
	SalonShare          domain.Cents                  `firestore:"salonShare"          json:"salonShare"`
	CertificateSales    domain.Cents                  `firestore:"certificateSales"    json:"certificateSales"`
	SalonIncome         domain.Cents                  `firestore:"salonIncome"         json:"salonIncome"`
	CardIn              domain.Cents                  `firestore:"cardIn"              json:"cardIn"`
	Prepaid             domain.Cents                  `firestore:"prepaid"             json:"prepaid"`
	Cash                domain.Cents                  `firestore:"cash"                json:"cash"`
	SuggestedToKassa    domain.Cents                  `firestore:"suggestedToKassa"    json:"suggestedToKassa"`
	KassaCredit         domain.Cents                  `firestore:"kassaCredit"         json:"kassaCredit"`
	BalanceDelta        domain.Cents                  `firestore:"balanceDelta"        json:"balanceDelta"`
	Version             int64                         `firestore:"version"             json:"version"`
	CreatedAt           time.Time                     `firestore:"createdAt"           json:"createdAt"`
	UpdatedAt           time.Time                     `firestore:"updatedAt"           json:"updatedAt"`
	UpdatedBy           string                        `firestore:"updatedBy"           json:"updatedBy"`
	// A client entered by mistake is voided, never deleted; every read skips it.
	Voided     bool   `firestore:"voided,omitempty"     json:"voided,omitempty"`
	VoidReason string `firestore:"voidReason,omitempty" json:"voidReason,omitempty"`
}

func reportRecordOf(r SavedReport) reportRecord {
	rec := reportRecord{
		ClientNumber: r.ClientNumber, ClientInput: r.ClientInput,
		UserID: r.Person, Date: r.Date.String(), Month: r.Date.Month().String(),
		Revenue: maps.Clone(r.Revenue), Payments: maps.Clone(r.Payments),
		RevenueFlags: map[string]revenueFlagsRecord{}, PaymentDestinations: map[string]domain.Destination{},
		CashToKassa: r.Report.CashToKassa, TransferToCard: r.Report.TransferToCard, Total: r.Report.Total, SelfBase: r.Report.SelfBase,
		SelfPct: r.Report.SelfPct, SelfAmount: r.Report.SelfAmount, SelfOverridden: r.Report.SelfOverridden,
		SalonShare: r.Report.SalonShare, CertificateSales: r.Report.CertificateSales,
		SalonIncome: r.Report.SalonIncome, CardIn: r.Report.CardIn, Prepaid: r.Report.Prepaid,
		Cash: r.Report.Cash, SuggestedToKassa: r.Report.SuggestedToKassa,
		KassaCredit: r.Report.KassaCredit, BalanceDelta: r.Report.BalanceDelta,
		Version: r.Version, CreatedAt: r.CreatedAt.UTC(), UpdatedAt: r.UpdatedAt.UTC(), UpdatedBy: r.UpdatedBy,
	}
	for id, f := range r.Flags.Revenue {
		rec.RevenueFlags[id] = revenueFlagsRecord(f)
	}
	for id, m := range r.Flags.Payments {
		rec.PaymentDestinations[id] = m.Destination
	}
	return rec
}

func (rec reportRecord) saved() (SavedReport, error) {
	day, err := domain.ParseDate(rec.Date)
	if err != nil {
		return SavedReport{}, err
	}
	flags := domain.Catalog{Revenue: map[string]domain.RevenueCategory{}, Payments: map[string]domain.PaymentMethod{}}
	for id, f := range rec.RevenueFlags {
		flags.Revenue[id] = domain.RevenueCategory(f)
	}
	for id, d := range rec.PaymentDestinations {
		flags.Payments[id] = domain.PaymentMethod{Destination: d}
	}
	id := reportID(rec.UserID, day)
	if rec.ClientToken != "" {
		id = clientID(rec.UserID, day, rec.ClientToken)
	}
	return SavedReport{
		ID: id, ClientNumber: rec.ClientNumber, ClientInput: rec.ClientInput,
		PersonReport: domain.PersonReport{
			Person: rec.UserID, Date: day, Flags: flags, Client: rec.ClientInput != nil,
			Revenue: nonNil(rec.Revenue), Payments: nonNil(rec.Payments),
			Report: domain.Report{
				Total: rec.Total, SelfBase: rec.SelfBase, SelfPct: rec.SelfPct, SelfAmount: rec.SelfAmount,
				SelfOverridden: rec.SelfOverridden, SalonShare: rec.SalonShare,
				CertificateSales: rec.CertificateSales, SalonIncome: rec.SalonIncome,
				CardIn: rec.CardIn, Prepaid: rec.Prepaid, Cash: rec.Cash,
				SuggestedToKassa: rec.SuggestedToKassa, CashToKassa: rec.CashToKassa, TransferToCard: rec.TransferToCard,
				KassaCredit: rec.KassaCredit, BalanceDelta: rec.BalanceDelta,
			},
		},
		Version: rec.Version, CreatedAt: rec.CreatedAt.UTC(), UpdatedAt: rec.UpdatedAt.UTC(), UpdatedBy: rec.UpdatedBy,
	}, nil
}

func nonNil(m map[string]domain.Cents) map[string]domain.Cents {
	if m == nil {
		return map[string]domain.Cents{}
	}
	return m
}

// withoutZeros drops empty amounts: zero amounts are left out of the maps.
func withoutZeros(m map[string]domain.Cents) map[string]domain.Cents {
	out := map[string]domain.Cents{}
	for id, v := range m {
		if v != 0 {
			out[id] = v
		}
	}
	return out
}

// SaveReport creates or updates a person's report for a day
// (system-design §3.1). People save only their own; the admin may save
// anyone's. The month must be open for everyone, the date between the
// start month and today, and Version the one the form was loaded with.
// The server computes every figure from the person's terms for the month.
func (s *Store) SaveReport(ctx context.Context, actor Actor, e ReportEntry) (SavedReport, error) {
	if s.financialWritesPaused {
		return SavedReport{}, ErrFinancialWritesPaused
	}
	if !actor.Admin && actor.UID != e.Person {
		return SavedReport{}, ErrForbidden
	}
	if err := checkID("user", e.Person); err != nil {
		return SavedReport{}, err
	}
	if err := domain.CheckDate(e.Date, s.Today(), s.start); err != nil {
		return SavedReport{}, err
	}
	revenue, payments := withoutZeros(e.Revenue), withoutZeros(e.Payments)
	m := e.Date.Month()
	var out SavedReport
	err := s.db.run(ctx, false, func(t tx) error {
		if err := requireOpen(t, m); err != nil {
			return err
		}
		u, err := readUser(t, e.Person)
		if err != nil {
			return err
		}
		if u.Removed { // nothing accrues to a removed person
			return ErrForbidden
		}
		var before reportRecord
		existed, err := t.get(reportPath(e.Person, e.Date), &before)
		if err != nil {
			return err
		}
		if before.Version != e.Version || (!existed && e.Version != 0) {
			return ErrVersionConflict
		}
		cats, err := readCategories(t)
		if err != nil {
			return err
		}
		history, err := readTermsHistory(t, e.Person)
		if err != nil {
			return err
		}
		terms, ok := history.For(m)
		if !ok {
			return &domain.ValidationError{Field: "terms", Problem: domain.ProblemMissingTerms}
		}
		// Archived categories leave the form; only a report that already
		// holds one may keep it.
		var usable []Category
		for _, c := range cats {
			_, wasRevenue := before.Revenue[c.ID]
			_, wasPayment := before.Payments[c.ID]
			if !c.Active && (revenue[c.ID] > before.Revenue[c.ID] || payments[c.ID] > before.Payments[c.ID]) {
				return ErrInvalid
			}
			if c.Active || wasRevenue || wasPayment {
				usable = append(usable, c)
			}
		}
		catalog := CatalogOf(usable)
		for id := range before.Revenue {
			if flags, ok := before.RevenueFlags[id]; ok {
				catalog.Revenue[id] = domain.RevenueCategory(flags)
			}
		}
		for id := range before.Payments {
			if destination, ok := before.PaymentDestinations[id]; ok {
				catalog.Payments[id] = domain.PaymentMethod{Destination: destination}
			}
		}
		pr, err := domain.NewPersonReport(e.Person, e.Date, domain.ReportInput{
			Revenue: revenue, Payments: payments, SelfPct: terms.SelfPct,
			CashToKassa: e.CashToKassa, TransferToCard: e.TransferToCard, SelfOverride: e.SelfOverride,
		}, catalog)
		if err != nil {
			return err
		}

		now := s.stamp()
		out = SavedReport{ID: reportID(e.Person, e.Date), PersonReport: pr, Version: before.Version + 1, CreatedAt: now, UpdatedAt: now, UpdatedBy: actor.UID}
		if existed {
			out.CreatedAt = before.CreatedAt.UTC()
		}
		for _, c := range usable {
			_, nowRevenue := revenue[c.ID]
			_, nowPayment := payments[c.ID]
			_, wasRevenue := before.Revenue[c.ID]
			_, wasPayment := before.Payments[c.ID]
			if (nowRevenue || nowPayment) && !wasRevenue && !wasPayment {
				used := c
				used.UsedCount++
				if err := s.writeCategory(t, actor.UID, c, used); err != nil {
					return err
				}
			}
		}
		rec := reportRecordOf(out)
		if err := t.set(reportPath(e.Person, e.Date), rec); err != nil {
			return err
		}
		action, prev := ActionCreate, any(nil)
		if existed {
			action, prev = ActionUpdate, before
		}
		return s.audit(t, EntityReport, reportID(e.Person, e.Date), action, actor.UID, prev, rec)
	})
	return out, err
}

func reportID(uid string, d domain.Date) string { return uid + "_" + d.String() }

// Report reads a person's report for a day.
func (s *Store) Report(ctx context.Context, uid string, d domain.Date) (SavedReport, error) {
	if err := checkID("user", uid); err != nil {
		return SavedReport{}, ErrNotFound
	}
	var out SavedReport
	err := s.db.run(ctx, true, func(t tx) error {
		var rec reportRecord
		found, err := t.get(reportPath(uid, d), &rec)
		if err != nil {
			return err
		}
		if !found {
			return ErrNotFound
		}
		out, err = rec.saved()
		return err
	})
	return out, err
}

// ReportsOn are everyone's reports for a day, by person.
func (s *Store) ReportsOn(ctx context.Context, d domain.Date) ([]SavedReport, error) {
	return s.queryReports(ctx, where("date", "==", d.String()))
}

// ReportsIn are everyone's reports in a month, by date and person.
func (s *Store) ReportsIn(ctx context.Context, m domain.Month) ([]SavedReport, error) {
	return s.queryReports(ctx, where("month", "==", m.String()))
}

// ReportsBetween reads a bounded, inclusive period for admin history.
func (s *Store) ReportsBetween(ctx context.Context, from, to domain.Date) ([]SavedReport, error) {
	return s.queryReports(ctx, where("date", ">=", from.String()), where("date", "<=", to.String()))
}

// PersonReportsIn are one person's reports in a month, by date.
func (s *Store) PersonReportsIn(ctx context.Context, uid string, m domain.Month) ([]SavedReport, error) {
	return s.queryReports(ctx, where("userId", "==", uid), where("month", "==", m.String()))
}

func (s *Store) queryReports(ctx context.Context, conds ...cond) ([]SavedReport, error) {
	var out []SavedReport
	err := s.db.run(ctx, true, func(t tx) error {
		var err error
		out, err = readReports(t, conds...)
		return err
	})
	return out, err
}

func readReports(t tx, conds ...cond) ([]SavedReport, error) {
	var out []SavedReport
	err := t.query("reports", conds, func(_ string, decode func(any) error) error {
		var rec reportRecord
		if err := decode(&rec); err != nil {
			return err
		}
		if rec.Voided {
			return nil
		}
		r, err := rec.saved()
		if err != nil {
			return err
		}
		out = append(out, r)
		return nil
	})
	slices.SortFunc(out, func(a, b SavedReport) int {
		return cmp.Or(cmp.Compare(a.Date.String(), b.Date.String()), cmp.Compare(a.Person, b.Person), cmp.Compare(a.ClientNumber, b.ClientNumber), a.CreatedAt.Compare(b.CreatedAt), cmp.Compare(a.ID, b.ID))
	})
	return out, err
}

func personReports(rs []SavedReport) []domain.PersonReport {
	out := make([]domain.PersonReport, len(rs))
	for i, r := range rs {
		out[i] = r.PersonReport
	}
	return out
}
