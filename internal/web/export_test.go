package web_test

import (
	"bytes"
	"encoding/csv"
	"net/http"
	"strings"
	"testing"

	"github.com/lieranderl/aestheticlab-income-app/internal/domain"
	"github.com/lieranderl/aestheticlab-income-app/internal/store"
	"github.com/xuri/excelize/v2"
)

func TestExportAccessIsAdminOnly(t *testing.T) {
	s := newSite(t)
	staff := s.login("olga", olgaPass)
	for _, path := range []string{"/admin/export", "/admin/export/download"} {
		if got := s.get(path, staff).Code; got != http.StatusForbidden {
			t.Fatalf("staff access to %s: %d, want 403", path, got)
		}
	}
	admin := s.login("anna", annaPass)
	if got := s.get("/admin/export", admin); got.Code != http.StatusOK {
		t.Fatalf("admin access to /admin/export: %d, want 200", got.Code)
	}
}

func TestExportValidation(t *testing.T) {
	s := newSite(t)
	admin := s.login("anna", annaPass)

	cases := []struct {
		name string
		url  string
	}{
		{"invalid format", "/admin/export/download?from=2026-11-01&to=2026-11-10&format=invalid"},
		{"invalid from date", "/admin/export/download?from=bad-date&to=2026-11-10"},
		{"invalid to date", "/admin/export/download?from=2026-11-01&to=bad-date"},
		{"from after to", "/admin/export/download?from=2026-11-20&to=2026-11-10"},
		{"range exceeds 12 months", "/admin/export/download?from=2025-01-01&to=2026-11-20"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := s.get(tc.url, admin).Code; got != http.StatusBadRequest {
				t.Fatalf("GET %s returned %d, want 400", tc.url, got)
			}
		})
	}
}

func TestExportDownloadXLSX(t *testing.T) {
	s := newSite(t)
	adminUser, err := s.st.UserByUsername(ctx, "anna")
	if err != nil {
		t.Fatal(err)
	}
	actor := store.Actor{UID: adminUser.ID, Admin: true}
	if err := s.st.SetTerms(ctx, s.anna.Actor(), s.olga.ID, s.st.StartMonth(), domain.Terms{SelfPct: 5000}, ""); err != nil {
		t.Fatal(err)
	}

	// Create a client report
	day := domain.Date{Year: 2026, Mon: 11, Day: 10}
	clientEntry := store.ClientEntry{
		Token:  "test-client-token-12345",
		Person: s.olga.ID,
		Date:   day,
		Input: domain.ClientInput{
			Services:    10000,
			ServiceCash: 10000,
		},
	}
	if _, err := s.st.SaveClientReport(ctx, actor, clientEntry); err != nil {
		t.Fatal(err)
	}

	// Create a movement
	move := store.NewMovement{
		Type:            domain.Expense,
		Amount:          1500,
		From:            domain.Account(domain.KassaCard),
		To:              domain.Outside,
		ExpenseCategory: "materials",
		Date:            day,
		Note:            "Материалы для работы",
	}
	if _, err := s.st.RecordMovement(ctx, actor, move); err != nil {
		t.Fatal(err)
	}

	admin := s.login("anna", annaPass)
	r := s.get("/admin/export/download?from=2026-11-01&to=2026-11-20&format=xlsx", admin)
	if r.Code != http.StatusOK {
		t.Fatalf("download XLSX failed with status %d: %s", r.Code, r.Body.String())
	}

	ct := r.Header().Get("Content-Type")
	if ct != "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet" {
		t.Errorf("unexpected Content-Type: %s", ct)
	}
	cd := r.Header().Get("Content-Disposition")
	if !strings.Contains(cd, "aestheticlab-export-2026-11-01-2026-11-20.xlsx") {
		t.Errorf("unexpected Content-Disposition: %s", cd)
	}

	// Read Excel workbook
	f, err := excelize.OpenReader(bytes.NewReader(r.Body.Bytes()))
	if err != nil {
		t.Fatalf("excelize.OpenReader failed: %v", err)
	}
	defer func() { _ = f.Close() }()

	for _, sheet := range []string{"Записи", "Движения", "По сотрудникам", "Итоги месяца", "По месяцам"} {
		idx, err := f.GetSheetIndex(sheet)
		if err != nil || idx < 0 {
			t.Errorf("missing sheet %q", sheet)
		}
	}
}

func TestExportDownloadCSVRecordsAndMovements(t *testing.T) {
	s := newSite(t)
	adminUser, err := s.st.UserByUsername(ctx, "anna")
	if err != nil {
		t.Fatal(err)
	}
	actor := store.Actor{UID: adminUser.ID, Admin: true}
	if err := s.st.SetTerms(ctx, s.anna.Actor(), s.olga.ID, s.st.StartMonth(), domain.Terms{SelfPct: 5000}, ""); err != nil {
		t.Fatal(err)
	}

	day := domain.Date{Year: 2026, Mon: 11, Day: 10}
	clientEntry := store.ClientEntry{
		Token:  "test-client-token-12345",
		Person: s.olga.ID,
		Date:   day,
		Input: domain.ClientInput{
			Services:    8000,
			ServiceCard: 8000,
		},
	}
	if _, err := s.st.SaveClientReport(ctx, actor, clientEntry); err != nil {
		t.Fatal(err)
	}

	admin := s.login("anna", annaPass)

	// Test CSV Records
	r := s.get("/admin/export/download?from=2026-11-01&to=2026-11-20&format=csv_records", admin)
	if r.Code != http.StatusOK {
		t.Fatalf("CSV records status %d: %s", r.Code, r.Body.String())
	}
	if !strings.Contains(r.Header().Get("Content-Disposition"), "aestheticlab-records-") {
		t.Errorf("unexpected Content-Disposition: %s", r.Header().Get("Content-Disposition"))
	}
	bs := r.Body.Bytes()
	if !bytes.HasPrefix(bs, []byte{0xEF, 0xBB, 0xBF}) {
		t.Error("missing UTF-8 BOM in CSV records")
	}
	cr := csv.NewReader(bytes.NewReader(bs[3:]))
	cr.Comma = ';'
	records, err := cr.ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) < 2 {
		t.Fatalf("expected at least header + 1 record row, got %d", len(records))
	}

	// Test CSV Movements
	r = s.get("/admin/export/download?from=2026-11-01&to=2026-11-20&format=csv_movements", admin)
	if r.Code != http.StatusOK {
		t.Fatalf("CSV movements status %d: %s", r.Code, r.Body.String())
	}
	if !strings.Contains(r.Header().Get("Content-Disposition"), "aestheticlab-movements-") {
		t.Errorf("unexpected Content-Disposition: %s", r.Header().Get("Content-Disposition"))
	}
	bs = r.Body.Bytes()
	if !bytes.HasPrefix(bs, []byte{0xEF, 0xBB, 0xBF}) {
		t.Error("missing UTF-8 BOM in CSV movements")
	}
}

func TestExportMonthResultsCarryTheKassaBalance(t *testing.T) {
	s := newSite(t)
	actor := s.anna.Actor()
	nov := s.st.StartMonth()
	if err := s.st.SetTerms(ctx, actor, s.olga.ID, nov, domain.Terms{SelfPct: 5000}, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.st.RecordOpenings(ctx, actor, []store.NewMovement{
		{Type: domain.Opening, Amount: 100000, From: domain.Salon, To: domain.Account(domain.KassaCash), Date: nov.FirstDay()},
	}); err != nil {
		t.Fatal(err)
	}
	day := domain.Date{Year: 2026, Mon: 11, Day: 10}
	// A €200 service paid in cash: the cash goes to the kassa.
	clientEntry := store.ClientEntry{
		Token:  "test-client-token-kassa",
		Person: s.olga.ID,
		Date:   day,
		Input:  domain.ClientInput{Services: 20000, ServiceCash: 20000},
	}
	if _, err := s.st.SaveClientReport(ctx, actor, clientEntry); err != nil {
		t.Fatal(err)
	}
	if _, err := s.st.RecordMovement(ctx, actor, store.NewMovement{
		Type: domain.Expense, Amount: 5000, From: domain.Account(domain.KassaCash), To: domain.Outside,
		ExpenseCategory: "materials", Date: day, Note: "Материалы",
	}); err != nil {
		t.Fatal(err)
	}
	b, err := s.st.Balances(ctx)
	if err != nil {
		t.Fatal(err)
	}

	admin := s.login("anna", annaPass)
	r := s.get("/admin/export/download?from=2026-11-01&to=2026-11-30&format=xlsx", admin)
	if r.Code != http.StatusOK {
		t.Fatalf("download: %d %s", r.Code, r.Body.String())
	}
	f, err := excelize.OpenReader(bytes.NewReader(r.Body.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	got, err := f.GetCellValue("Итоги месяца", "I2", excelize.Options{RawCellValue: true})
	if err != nil {
		t.Fatal(err)
	}
	// Opening 1 000 + 200 − 50: the balance, not the month's 200 contribution.
	if b.Accounts[domain.KassaCash] != 115000 || got != "1150" {
		t.Fatalf("kassa cash: balance %d, export %q, want 115000 and 1150", b.Accounts[domain.KassaCash], got)
	}
}
