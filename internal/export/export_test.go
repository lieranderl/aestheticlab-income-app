package export_test

import (
	"bytes"
	"encoding/csv"
	"strings"
	"testing"
	"time"

	"github.com/lieranderl/aestheticlab-income-app/internal/domain"
	"github.com/lieranderl/aestheticlab-income-app/internal/export"
	"github.com/lieranderl/aestheticlab-income-app/internal/store"
	"github.com/xuri/excelize/v2"
)

func sampleExportData() export.Data {
	m := domain.Month{Year: 2026, Mon: 11}
	d1 := m.FirstDay()
	d2 := m.LastDay()

	users := map[string]store.User{
		"olga": {ID: "olga", Username: "olga", DisplayName: "Ольга"},
		"anna": {ID: "anna", Username: "anna", DisplayName: "Анна"},
	}
	accounts := map[string]store.Account{
		"cash": {ID: "cash", Name: "Касса — наличные", Kind: store.AccountCash},
		"card": {ID: "card", Name: "Касса — карта салона", Kind: store.AccountCard},
	}
	categories := map[string]store.Category{
		"tea": {ID: "tea", Name: "Чай и кофе", Kind: store.KindExpense},
	}

	reports := []store.SavedReport{
		{
			PersonReport: domain.PersonReport{
				Client: true,
				Person: "olga",
				Date:   d1,
				Revenue: map[string]domain.Cents{
					"services": 12000,
					"creams":   3000,
				},
				Payments: map[string]domain.Cents{
					"cash": 15000,
				},
				Report: domain.Report{
					Total:       15000,
					SelfAmount:  6000,
					SalonShare:  9000,
					Cash:        15000,
					CashToKassa: 9000,
				},
			},
			ClientNumber: 1,
			CreatedAt:    time.Date(2026, 11, 10, 14, 30, 0, 0, time.UTC),
		},
	}

	movements := []store.Entry{
		{
			ID: "m1",
			Movement: domain.Movement{
				Date:            d2,
				Type:            domain.Expense,
				Amount:          2500,
				From:            domain.Account("cash"),
				To:              domain.Outside,
				ExpenseCategory: "tea",
				Status:          domain.Confirmed,
			},
			Note:      "Покупка чая в салон",
			CreatedAt: time.Date(2026, 11, 15, 11, 0, 0, 0, time.UTC),
		},
		{
			ID: "m2",
			Movement: domain.Movement{
				Date:   d2,
				Type:   domain.Deposit,
				Amount: 5000,
				From:   domain.Person("olga"),
				To:     domain.Account("cash"),
				Status: domain.Confirmed,
			},
			Note:      "=HYPERLINK(\"http://evil.com\")", // Injection attempt
			CreatedAt: time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC),
		},
	}

	monthResults := map[domain.Month]domain.MonthResult{
		m: {
			Month:        m,
			Revenue:      150000,
			Self:         60000,
			SalonIncome:  90000,
			Expenses:     20000,
			Salaries:     30000,
			Profit:       40000,
			DividendPool: 40000,
			KassaCash:    15000,
			KassaCard:    25000,
			Clients:      12,
			People: map[string]domain.PersonResult{
				"olga": {
					Revenue:     150000,
					Self:        60000,
					Clients:     12,
					WorkingDays: 5,
				},
			},
		},
	}

	trendPoints := []domain.TrendPoint{
		{
			Month:       m,
			HasData:     true,
			Revenue:     150000,
			SalonIncome: 90000,
			Expenses:    20000,
			Profit:      40000,
			Dividends:   40000,
			Clients:     12,
		},
	}

	return export.Data{
		From:         d1,
		To:           d2,
		Users:        users,
		Accounts:     accounts,
		Categories:   categories,
		Reports:      reports,
		Movements:    movements,
		MonthResults: monthResults,
		TrendPoints:  trendPoints,
	}
}

func TestExportXLSX(t *testing.T) {
	data := sampleExportData()
	var buf bytes.Buffer
	if err := export.XLSX(&buf, data); err != nil {
		t.Fatalf("export.XLSX failed: %v", err)
	}

	f, err := excelize.OpenReader(&buf)
	if err != nil {
		t.Fatalf("excelize.OpenReader failed: %v", err)
	}
	defer func() { _ = f.Close() }()

	expectedSheets := []string{"Записи", "Движения", "По сотрудникам", "Итоги месяца", "По месяцам"}
	for _, sheet := range expectedSheets {
		idx, err := f.GetSheetIndex(sheet)
		if err != nil || idx < 0 {
			t.Errorf("missing sheet %q", sheet)
		}
	}

	// Verify sheet "Записи" has rows
	rows, err := f.GetRows("Записи")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) < 2 {
		t.Fatalf("expected at least 2 rows in Записи, got %d", len(rows))
	}
	header := rows[0]
	if header[0] != "Дата" || header[2] != "Мастер" {
		t.Errorf("unexpected header in Записи: %v", header)
	}
	firstDataRow := rows[1]
	if firstDataRow[2] != "Ольга" {
		t.Errorf("expected master Ольга, got %v", firstDataRow[2])
	}
	// Timestamp 14:30 UTC converted to Brussels time (UTC+1 in November) is 15:30
	if firstDataRow[1] != "15:30" {
		t.Errorf("expected Brussels time 15:30, got %v", firstDataRow[1])
	}

	// Verify sheet "Движения"
	mRows, err := f.GetRows("Движения")
	if err != nil {
		t.Fatal(err)
	}
	if len(mRows) < 3 {
		t.Fatalf("expected header + 2 movement rows, got %d", len(mRows))
	}
}

func TestExportPartialMonthDoesNotAddMonthlySalary(t *testing.T) {
	data := sampleExportData()
	data.From = domain.Date{Year: 2026, Mon: 11, Day: 10}
	data.To = domain.Date{Year: 2026, Mon: 11, Day: 15}

	var buf bytes.Buffer
	if err := export.XLSX(&buf, data); err != nil {
		t.Fatalf("export.XLSX failed: %v", err)
	}

	f, err := excelize.OpenReader(&buf)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	// Sheet "Итоги месяца" should have only the header row because Nov 2026 is not full
	mRows, err := f.GetRows("Итоги месяца")
	if err != nil {
		t.Fatal(err)
	}
	if len(mRows) != 1 {
		t.Errorf("expected only header row for partial month in Итоги месяца, got %d rows", len(mRows))
	}

	// Sheet "По сотрудникам" should have totalEarned equal to Self (60,00 €) without whole-month salary
	empRows, err := f.GetRows("По сотрудникам")
	if err != nil {
		t.Fatal(err)
	}
	if len(empRows) < 2 {
		t.Fatalf("expected header + employee row, got %d", len(empRows))
	}
	olgaRow := empRows[1]
	// Salary should be 0 (not 300)
	if olgaRow[10] != "0.00 €" && olgaRow[10] != "" {
		t.Errorf("expected no whole-month salary for partial period, got %s", olgaRow[10])
	}
}

func TestExportCSVRecords(t *testing.T) {
	data := sampleExportData()
	var buf bytes.Buffer
	if err := export.CSV(&buf, "records", data); err != nil {
		t.Fatalf("export.CSV failed: %v", err)
	}

	bs := buf.Bytes()
	if !bytes.HasPrefix(bs, []byte{0xEF, 0xBB, 0xBF}) {
		t.Fatal("missing UTF-8 BOM in CSV")
	}

	r := csv.NewReader(bytes.NewReader(bs[3:]))
	r.Comma = ';'
	records, err := r.ReadAll()
	if err != nil {
		t.Fatalf("csv.ReadAll failed: %v", err)
	}
	if len(records) < 2 {
		t.Fatalf("expected at least 2 rows, got %d", len(records))
	}
	if records[0][0] != "Дата" {
		t.Errorf("header[0] = %q, want Дата", records[0][0])
	}
	// Check decimal comma format e.g. 150,00 or 120,00
	row := records[1]
	foundCommaDecimal := false
	for _, col := range row {
		if strings.Contains(col, ",00") {
			foundCommaDecimal = true
			break
		}
	}
	if !foundCommaDecimal {
		t.Errorf("expected Belgian comma decimal in row %v", row)
	}
}

func TestExportCSVMovementsAndInjectionEscaping(t *testing.T) {
	data := sampleExportData()
	var buf bytes.Buffer
	if err := export.CSV(&buf, "movements", data); err != nil {
		t.Fatalf("export.CSV failed: %v", err)
	}

	bs := buf.Bytes()
	r := csv.NewReader(bytes.NewReader(bs[3:]))
	r.Comma = ';'
	records, err := r.ReadAll()
	if err != nil {
		t.Fatalf("csv.ReadAll failed: %v", err)
	}
	if len(records) < 3 {
		t.Fatalf("expected header + 2 movement rows, got %d", len(records))
	}

	// Movement 2 had "=HYPERLINK(...)" in Note, which must be escaped as "'=HYPERLINK(...)"
	foundEscaped := false
	for _, row := range records {
		for _, col := range row {
			if strings.HasPrefix(col, "'=HYPERLINK") {
				foundEscaped = true
				break
			}
		}
	}
	if !foundEscaped {
		t.Error("formula injection '=HYPERLINK' was not escaped with leading single quote")
	}
}
