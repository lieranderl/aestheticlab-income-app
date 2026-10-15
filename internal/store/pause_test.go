package store_test

import (
	"errors"
	"testing"
	"time"

	"github.com/lieranderl/aestheticlab-income-app/internal/domain"
	"github.com/lieranderl/aestheticlab-income-app/internal/store"
)

func TestFinancialWritesPaused(t *testing.T) {
	eachStore(t, func(t *testing.T, open func(opts ...store.Option) *store.Store) {
		clock := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
		st := open(store.WithClock(func() time.Time { return clock }), store.WithFinancialWritesPaused(true))

		if !st.FinancialWritesPaused() {
			t.Fatal("expected FinancialWritesPaused() to return true")
		}

		// Initial setup and user administration are administrative, not financial writes.
		admin, err := st.SetUp(ctx, defaultSetup())
		must(t, err)

		nov := month(t, "2026-11")
		nov10 := date(t, "2026-11-10")
		nov1 := date(t, "2026-11-01")

		must(t, st.SetTerms(ctx, admin.Actor(), admin.ID, nov, domain.Terms{SelfPct: 5000, SalaryCents: 100000, DividendWeight: 1}, "старт"))

		// 1. SaveClientReport is refused with ErrFinancialWritesPaused.
		clientIn := store.ClientEntry{
			Person: admin.ID,
			Date:   nov10,
			Token:  "test-token-1234567890abcdef",
			Input: domain.ClientInput{
				Services:        10000,
				CertificatePaid: 10000,
			},
		}
		_, err = st.SaveClientReport(ctx, admin.Actor(), clientIn)
		if !errors.Is(err, store.ErrFinancialWritesPaused) {
			t.Errorf("SaveClientReport err = %v, want ErrFinancialWritesPaused", err)
		}

		if err := st.VoidClientReport(ctx, admin.Actor(), "client_x", 1, "ошибка"); !errors.Is(err, store.ErrFinancialWritesPaused) {
			t.Errorf("VoidClientReport err = %v, want ErrFinancialWritesPaused", err)
		}

		// 2. SaveReport (legacy daily report) is refused with ErrFinancialWritesPaused.
		reportIn := store.ReportEntry{
			Person: admin.ID,
			Date:   nov10,
			Revenue: map[string]domain.Cents{
				"nails": 10000,
			},
			Payments: map[string]domain.Cents{
				"cash": 10000,
			},
		}
		_, err = st.SaveReport(ctx, admin.Actor(), reportIn)
		if !errors.Is(err, store.ErrFinancialWritesPaused) {
			t.Errorf("SaveReport err = %v, want ErrFinancialWritesPaused", err)
		}

		// 3. RecordMovement is refused with ErrFinancialWritesPaused.
		movIn := store.NewMovement{
			Type:   domain.Deposit,
			Amount: 5000,
			From:   domain.Person(admin.ID),
			To:     domain.Account(domain.KassaCash),
			Date:   nov10,
		}
		_, err = st.RecordMovement(ctx, admin.Actor(), movIn)
		if !errors.Is(err, store.ErrFinancialWritesPaused) {
			t.Errorf("RecordMovement err = %v, want ErrFinancialWritesPaused", err)
		}

		// 4. RecordSettlementMovement is refused with ErrFinancialWritesPaused.
		_, err = st.RecordSettlementMovement(ctx, admin.Actor(), nov, admin.ID, "rev-1", movIn)
		if !errors.Is(err, store.ErrFinancialWritesPaused) {
			t.Errorf("RecordSettlementMovement err = %v, want ErrFinancialWritesPaused", err)
		}

		// 5. RecordOpenings is refused with ErrFinancialWritesPaused.
		openings := []store.NewMovement{
			{
				Type:   domain.Opening,
				Amount: 10000,
				From:   domain.Outside,
				To:     domain.Account(domain.KassaCash),
				Date:   nov1,
			},
		}
		_, err = st.RecordOpenings(ctx, admin.Actor(), openings)
		if !errors.Is(err, store.ErrFinancialWritesPaused) {
			t.Errorf("RecordOpenings err = %v, want ErrFinancialWritesPaused", err)
		}

		// 6. ConfirmMovement is refused with ErrFinancialWritesPaused.
		_, err = st.ConfirmMovement(ctx, admin.Actor(), "some-id", 1)
		if !errors.Is(err, store.ErrFinancialWritesPaused) {
			t.Errorf("ConfirmMovement err = %v, want ErrFinancialWritesPaused", err)
		}

		// 7. VoidMovement is refused with ErrFinancialWritesPaused.
		_, err = st.VoidMovement(ctx, admin.Actor(), "some-id", 1, "test mistake")
		if !errors.Is(err, store.ErrFinancialWritesPaused) {
			t.Errorf("VoidMovement err = %v, want ErrFinancialWritesPaused", err)
		}

		// 8. Recount is refused with ErrFinancialWritesPaused.
		_, err = st.Recount(ctx, admin.Actor(), domain.KassaCash, 5000, "test recount")
		if !errors.Is(err, store.ErrFinancialWritesPaused) {
			t.Errorf("Recount err = %v, want ErrFinancialWritesPaused", err)
		}

		// 9. CloseMonth / CloseMonthGuarded is refused with ErrFinancialWritesPaused.
		_, err = st.CloseMonth(ctx, admin.Actor(), nov)
		if !errors.Is(err, store.ErrFinancialWritesPaused) {
			t.Errorf("CloseMonth err = %v, want ErrFinancialWritesPaused", err)
		}

		// 10. ReopenMonth / ReopenMonthGuarded is refused with ErrFinancialWritesPaused.
		_, err = st.ReopenMonth(ctx, admin.Actor(), nov)
		if !errors.Is(err, store.ErrFinancialWritesPaused) {
			t.Errorf("ReopenMonth err = %v, want ErrFinancialWritesPaused", err)
		}

		// 11. Read operations succeed normally when paused.
		if _, err := st.Balances(ctx); err != nil {
			t.Errorf("Balances err = %v, want nil", err)
		}
		if _, err := st.Month(ctx, nov); err != nil {
			t.Errorf("Month err = %v, want nil", err)
		}
		if _, err := st.Ledger(ctx, nov); err != nil {
			t.Errorf("Ledger err = %v, want nil", err)
		}
	})
}
