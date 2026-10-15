package export

import (
	"encoding/csv"
	"fmt"
	"io"
	"sort"
	"strconv"
	"time"

	"github.com/lieranderl/aestheticlab-income-app/internal/domain"
	"github.com/lieranderl/aestheticlab-income-app/internal/ru"
	"github.com/lieranderl/aestheticlab-income-app/internal/store"
	"github.com/xuri/excelize/v2"
)

var brusselsLoc *time.Location

func init() {
	brusselsLoc, _ = time.LoadLocation("Europe/Brussels")
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	if brusselsLoc != nil {
		t = t.In(brusselsLoc)
	}
	return t.Format("15:04")
}

// Data contains everything needed to render the XLSX and CSV exports.
type Data struct {
	From         domain.Date
	To           domain.Date
	Users        map[string]store.User
	Accounts     map[string]store.Account
	Categories   map[string]store.Category
	Reports      []store.SavedReport
	Movements    []store.Entry
	MonthResults map[domain.Month]domain.MonthResult
	Closing      map[domain.Month]domain.Balances // the balances at each month's end (now, for the open month)
	TrendPoints  []domain.TrendPoint
}

// unknown fills a cell whose figure the app doesn't have, as the web view does.
const unknown = "—"

// XLSX generates a multi-sheet Excel workbook:
// «Записи», «Движения», «По сотрудникам», «Итоги месяца», «По месяцам».
func XLSX(w io.Writer, d Data) error {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()

	headerStyle, err := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{Bold: true, Color: "000000"},
		Fill: excelize.Fill{Type: "pattern", Color: []string{"F2F4F8"}, Pattern: 1},
	})
	if err != nil {
		return err
	}

	currencyStyle, err := f.NewStyle(&excelize.Style{
		CustomNumFmt: &[]string{"#,##0.00 €"}[0],
	})
	if err != nil {
		return err
	}

	sheets := []string{"Записи", "Движения", "По сотрудникам", "Итоги месяца", "По месяцам"}
	for _, sheet := range sheets {
		if _, err := f.NewSheet(sheet); err != nil {
			return err
		}
	}
	if err := f.DeleteSheet("Sheet1"); err != nil {
		return err
	}

	writeRecordsSheet(f, d, headerStyle, currencyStyle)
	writeMovementsSheet(f, d, headerStyle, currencyStyle)
	writeEmployeesSheet(f, d, headerStyle, currencyStyle)
	writeMonthResultsSheet(f, d, headerStyle, currencyStyle)
	writeTrendsSheet(f, d, headerStyle, currencyStyle)

	return f.Write(w)
}

func writeRecordsSheet(f *excelize.File, d Data, hStyle, cStyle int) {
	sheet := "Записи"
	headers := []string{
		"Дата", "Время", "Мастер", "№ записи",
		"Услуги", "Кремы", "Масло", "Сертификаты продажа",
		"Выручка итого", "Наличные", "Карта", "Сертификат оплата",
		"В кассу наличными", "В кассу на карту", "Себе", "Салону",
	}
	writeHeaderRow(f, sheet, headers, hStyle)

	for i, r := range d.Reports {
		row := i + 2
		dateStr := fmt.Sprintf("%02d.%02d.%04d", r.Date.Day, int(r.Date.Mon), r.Date.Year)
		timeStr := formatTime(r.CreatedAt)
		masterStr := userName(r.Person, d.Users)
		numStr := "день"
		if r.Client {
			numStr = strconv.FormatInt(r.ClientNumber, 10)
		}

		_ = f.SetCellValue(sheet, cell(1, row), dateStr)
		_ = f.SetCellValue(sheet, cell(2, row), timeStr)
		_ = f.SetCellValue(sheet, cell(3, row), masterStr)
		_ = f.SetCellValue(sheet, cell(4, row), numStr)

		setCur(f, sheet, 5, row, r.Revenue["services"], cStyle)
		setCur(f, sheet, 6, row, r.Revenue["creams"], cStyle)
		setCur(f, sheet, 7, row, r.Revenue["oil"], cStyle)
		setCur(f, sheet, 8, row, r.Report.CertificateSales, cStyle)
		setCur(f, sheet, 9, row, r.Report.Total, cStyle)
		setCur(f, sheet, 10, row, r.Report.Cash, cStyle)
		setCur(f, sheet, 11, row, r.Report.CardIn, cStyle)
		setCur(f, sheet, 12, row, r.Report.Prepaid, cStyle)
		setCur(f, sheet, 13, row, r.Report.CashToKassa, cStyle)
		setCur(f, sheet, 14, row, r.Report.TransferToCard, cStyle)
		setCur(f, sheet, 15, row, r.Report.SelfAmount, cStyle)
		setCur(f, sheet, 16, row, r.Report.SalonShare, cStyle)
	}
}

func writeMovementsSheet(f *excelize.File, d Data, hStyle, cStyle int) {
	sheet := "Движения"
	headers := []string{
		"Дата", "Время", "Тип", "Сумма",
		"Откуда", "Куда", "Категория расхода",
		"Статус", "Примечание", "Причина отмены",
	}
	writeHeaderRow(f, sheet, headers, hStyle)

	for i, m := range d.Movements {
		row := i + 2
		dateStr := fmt.Sprintf("%02d.%02d.%04d", m.Date.Day, int(m.Date.Mon), m.Date.Year)
		timeStr := formatTime(m.CreatedAt)
		typeStr := movementKindTitle(m.Type)
		fromStr := partyName(m.From, d.Users, d.Accounts)
		toStr := partyName(m.To, d.Users, d.Accounts)
		catStr := "—"
		if m.ExpenseCategory != "" {
			if c, ok := d.Categories[m.ExpenseCategory]; ok {
				catStr = c.Name
			} else {
				catStr = m.ExpenseCategory
			}
		}
		statusStr := movementStatusTitle(m.Status)

		_ = f.SetCellValue(sheet, cell(1, row), dateStr)
		_ = f.SetCellValue(sheet, cell(2, row), timeStr)
		_ = f.SetCellValue(sheet, cell(3, row), typeStr)
		setCur(f, sheet, 4, row, m.Amount, cStyle)
		_ = f.SetCellValue(sheet, cell(5, row), fromStr)
		_ = f.SetCellValue(sheet, cell(6, row), toStr)
		_ = f.SetCellValue(sheet, cell(7, row), catStr)
		_ = f.SetCellValue(sheet, cell(8, row), statusStr)
		_ = f.SetCellValue(sheet, cell(9, row), m.Note)
		_ = f.SetCellValue(sheet, cell(10, row), m.VoidReason)
	}
}

type empSummary struct {
	ID           string
	Name         string
	Clients      int
	LegacyDays   bool // a legacy day report has no client count
	WorkingDays  int
	Revenue      domain.Cents
	Services     domain.Cents
	Creams       domain.Cents
	Oil          domain.Cents
	Certificates domain.Cents
	Self         domain.Cents
	SelfBonus    domain.Cents
	Salary       domain.Cents
	Dividend     domain.Cents
}

func writeEmployeesSheet(f *excelize.File, d Data, hStyle, cStyle int) {
	sheet := "По сотрудникам"
	headers := []string{
		"Сотрудник", "Клиентов", "Рабочих дней",
		"Выручка итого", "Услуги", "Кремы", "Масло", "Сертификаты",
		"Себе", "Доплата «Себе»", "Зарплата", "Дивиденды", "Начислено итого",
	}
	writeHeaderRow(f, sheet, headers, hStyle)

	empMap := make(map[string]*empSummary)
	personDays := make(map[string]map[domain.Date]bool)

	for _, r := range d.Reports {
		s, ok := empMap[r.Person]
		if !ok {
			s = &empSummary{ID: r.Person, Name: userName(r.Person, d.Users)}
			empMap[r.Person] = s
			personDays[r.Person] = make(map[domain.Date]bool)
		}
		if r.Client {
			s.Clients++
		} else {
			s.LegacyDays = true
		}
		personDays[r.Person][r.Date] = true
		s.Revenue += r.Report.Total
		s.Services += r.Revenue["services"]
		s.Creams += r.Revenue["creams"]
		s.Oil += r.Revenue["oil"]
		s.Certificates += r.Report.CertificateSales
		s.Self += r.Report.SelfAmount
	}

	for id, days := range personDays {
		if s, ok := empMap[id]; ok {
			s.WorkingDays = len(days)
		}
	}

	for m, res := range d.MonthResults {
		if !isFullMonth(m, d.From, d.To) {
			continue
		}
		for id, pr := range res.People {
			s, ok := empMap[id]
			if !ok {
				s = &empSummary{ID: id, Name: userName(id, d.Users)}
				empMap[id] = s
			}
			s.SelfBonus += pr.SelfBonus
			s.Salary += pr.Salary
			s.Dividend += pr.Dividend
		}
	}

	var emps []*empSummary
	for _, s := range empMap {
		emps = append(emps, s)
	}
	sort.Slice(emps, func(i, j int) bool { return emps[i].Name < emps[j].Name })

	for i, s := range emps {
		row := i + 2
		totalEarned := s.Self + s.SelfBonus + s.Salary + s.Dividend

		_ = f.SetCellValue(sheet, cell(1, row), s.Name)
		setCount(f, sheet, 2, row, s.Clients, !s.LegacyDays)
		_ = f.SetCellValue(sheet, cell(3, row), s.WorkingDays)
		setCur(f, sheet, 4, row, s.Revenue, cStyle)
		setCur(f, sheet, 5, row, s.Services, cStyle)
		setCur(f, sheet, 6, row, s.Creams, cStyle)
		setCur(f, sheet, 7, row, s.Oil, cStyle)
		setCur(f, sheet, 8, row, s.Certificates, cStyle)
		setCur(f, sheet, 9, row, s.Self, cStyle)
		setCur(f, sheet, 10, row, s.SelfBonus, cStyle)
		setCur(f, sheet, 11, row, s.Salary, cStyle)
		setCur(f, sheet, 12, row, s.Dividend, cStyle)
		setCur(f, sheet, 13, row, totalEarned, cStyle)
	}
}

func isFullMonth(m domain.Month, from, to domain.Date) bool {
	return !m.FirstDay().Before(from) && !to.Before(m.LastDay())
}

func writeMonthResultsSheet(f *excelize.File, d Data, hStyle, cStyle int) {
	sheet := "Итоги месяца"
	headers := []string{
		"Месяц", "Выручка", "Себе мастера",
		"Доход салона", "Расходы салона", "Зарплаты",
		"Прибыль", "Дивиденды", "Касса наличные, остаток на конец",
		"Касса карта, остаток на конец", "Клиентов всего",
	}
	writeHeaderRow(f, sheet, headers, hStyle)

	var months []domain.Month
	for m := range d.MonthResults {
		if isFullMonth(m, d.From, d.To) {
			months = append(months, m)
		}
	}
	sort.Slice(months, func(i, j int) bool { return months[i].Before(months[j]) })

	for i, m := range months {
		row := i + 2
		res := d.MonthResults[m]
		monthStr := ru.MonthYear(m.Year, m.Mon)

		_ = f.SetCellValue(sheet, cell(1, row), monthStr)
		setCur(f, sheet, 2, row, res.Revenue, cStyle)
		setCur(f, sheet, 3, row, res.Self, cStyle)
		setCur(f, sheet, 4, row, res.SalonIncome, cStyle)
		setCur(f, sheet, 5, row, res.Expenses, cStyle)
		setCur(f, sheet, 6, row, res.Salaries, cStyle)
		setCur(f, sheet, 7, row, res.Profit, cStyle)
		setCur(f, sheet, 8, row, res.DividendPool, cStyle)
		if b, ok := d.Closing[m]; ok {
			setCur(f, sheet, 9, row, b.Accounts[domain.KassaCash], cStyle)
			setCur(f, sheet, 10, row, b.Accounts[domain.KassaCard], cStyle)
		} else {
			_ = f.SetCellValue(sheet, cell(9, row), unknown)
			_ = f.SetCellValue(sheet, cell(10, row), unknown)
		}
		setCount(f, sheet, 11, row, res.Clients, res.StatsVersion > 0)
	}
}

func writeTrendsSheet(f *excelize.File, d Data, hStyle, cStyle int) {
	sheet := "По месяцам"
	headers := []string{
		"Месяц", "Источник", "Выручка",
		"Себе", "Доход салона", "Расходы",
		"Прибыль", "Дивиденды", "Клиентов",
	}
	writeHeaderRow(f, sheet, headers, hStyle)

	for i, pt := range d.TrendPoints {
		row := i + 2
		monthStr := ru.MonthYear(pt.Month.Year, pt.Month.Mon)
		sourceStr := "Нет данных"
		if pt.Imported {
			sourceStr = "Импорт из таблицы"
		} else if pt.HasData {
			sourceStr = "Учёт"
		}

		_ = f.SetCellValue(sheet, cell(1, row), monthStr)
		_ = f.SetCellValue(sheet, cell(2, row), sourceStr)
		// The same availability as the «Динамика» screen: imported months
		// have no «Себе» or clients, legacy snapshots no clients.
		has := pt.HasData
		setCurIf(f, sheet, 3, row, pt.Revenue, cStyle, has)
		setCurIf(f, sheet, 4, row, pt.Self, cStyle, has && !pt.Imported)
		setCurIf(f, sheet, 5, row, pt.SalonIncome, cStyle, has)
		setCurIf(f, sheet, 6, row, pt.Expenses, cStyle, has)
		setCurIf(f, sheet, 7, row, pt.Profit, cStyle, has)
		setCurIf(f, sheet, 8, row, pt.Dividends, cStyle, has)
		setCount(f, sheet, 9, row, pt.Clients, has && !pt.Imported && pt.StatsVersion > 0)
	}
}

func writeHeaderRow(f *excelize.File, sheet string, headers []string, style int) {
	for colIdx, h := range headers {
		c := cell(colIdx+1, 1)
		_ = f.SetCellValue(sheet, c, h)
		_ = f.SetCellStyle(sheet, c, c, style)
	}
}

func setCur(f *excelize.File, sheet string, col, row int, c domain.Cents, style int) {
	target := cell(col, row)
	_ = f.SetCellValue(sheet, target, float64(c)/100.0)
	_ = f.SetCellStyle(sheet, target, target, style)
}

// setCurIf writes the amount, or «—» when the figure isn't known.
func setCurIf(f *excelize.File, sheet string, col, row int, c domain.Cents, style int, known bool) {
	if !known {
		_ = f.SetCellValue(sheet, cell(col, row), unknown)
		return
	}
	setCur(f, sheet, col, row, c, style)
}

// setCount writes a count, or «—» when the figure isn't known.
func setCount(f *excelize.File, sheet string, col, row, n int, known bool) {
	if !known {
		_ = f.SetCellValue(sheet, cell(col, row), unknown)
		return
	}
	_ = f.SetCellValue(sheet, cell(col, row), n)
}

func cell(col, row int) string {
	name, _ := excelize.CoordinatesToCellName(col, row)
	return name
}

// CSV generates a CSV file with UTF-8 BOM, semicolon delimiter,
// Belgian comma decimals, and formula injection protection.
func CSV(w io.Writer, kind string, d Data) error {
	// Write UTF-8 BOM so Excel opens with proper Russian characters
	if _, err := w.Write([]byte{0xEF, 0xBB, 0xBF}); err != nil {
		return err
	}

	cw := csv.NewWriter(w)
	cw.Comma = ';'

	if kind == "movements" {
		headers := []string{
			"Дата", "Время", "Тип", "Сумма",
			"Откуда", "Куда", "Категория расхода",
			"Статус", "Примечание", "Причина отмены",
		}
		if err := cw.Write(headers); err != nil {
			return err
		}
		for _, m := range d.Movements {
			timeStr := formatTime(m.CreatedAt)
			catStr := "—"
			if m.ExpenseCategory != "" {
				if c, ok := d.Categories[m.ExpenseCategory]; ok {
					catStr = c.Name
				} else {
					catStr = m.ExpenseCategory
				}
			}
			row := []string{
				fmt.Sprintf("%02d.%02d.%04d", m.Date.Day, int(m.Date.Mon), m.Date.Year),
				timeStr,
				movementKindTitle(m.Type),
				centsBelgian(m.Amount),
				sanitizeCSV(partyName(m.From, d.Users, d.Accounts)),
				sanitizeCSV(partyName(m.To, d.Users, d.Accounts)),
				sanitizeCSV(catStr),
				movementStatusTitle(m.Status),
				sanitizeCSV(m.Note),
				sanitizeCSV(m.VoidReason),
			}
			if err := cw.Write(row); err != nil {
				return err
			}
		}
	} else {
		// default: records
		headers := []string{
			"Дата", "Время", "Мастер", "№ записи",
			"Услуги", "Кремы", "Масло", "Сертификаты продажа",
			"Выручка итого", "Наличные", "Карта", "Сертификат оплата",
			"В кассу наличными", "В кассу на карту", "Себе", "Салону",
		}
		if err := cw.Write(headers); err != nil {
			return err
		}
		for _, r := range d.Reports {
			timeStr := formatTime(r.CreatedAt)
			numStr := "день"
			if r.Client {
				numStr = strconv.FormatInt(r.ClientNumber, 10)
			}
			row := []string{
				fmt.Sprintf("%02d.%02d.%04d", r.Date.Day, int(r.Date.Mon), r.Date.Year),
				timeStr,
				sanitizeCSV(userName(r.Person, d.Users)),
				numStr,
				centsBelgian(r.Revenue["services"]),
				centsBelgian(r.Revenue["creams"]),
				centsBelgian(r.Revenue["oil"]),
				centsBelgian(r.Report.CertificateSales),
				centsBelgian(r.Report.Total),
				centsBelgian(r.Report.Cash),
				centsBelgian(r.Report.CardIn),
				centsBelgian(r.Report.Prepaid),
				centsBelgian(r.Report.CashToKassa),
				centsBelgian(r.Report.TransferToCard),
				centsBelgian(r.Report.SelfAmount),
				centsBelgian(r.Report.SalonShare),
			}
			if err := cw.Write(row); err != nil {
				return err
			}
		}
	}

	cw.Flush()
	return cw.Error()
}

func centsBelgian(c domain.Cents) string {
	sign := ""
	if c < 0 {
		sign = "-"
		c = -c
	}
	return fmt.Sprintf("%s%d,%02d", sign, c/100, c%100)
}

func sanitizeCSV(s string) string {
	if s == "" {
		return ""
	}
	switch s[0] {
	case '=', '+', '-', '@', '\t', '\r':
		return "'" + s
	}
	return s
}

func userName(id string, users map[string]store.User) string {
	if u, ok := users[id]; ok && u.DisplayName != "" {
		return u.DisplayName
	}
	return id
}

func partyName(p domain.Party, users map[string]store.User, accounts map[string]store.Account) string {
	switch p.Kind {
	case domain.PartyAccount:
		if a, ok := accounts[p.ID]; ok {
			return a.Name
		}
		return "Касса"
	case domain.PartyPerson:
		return userName(p.ID, users)
	case domain.PartyOutside:
		return "Расход"
	case domain.PartySalon:
		return "Салон"
	case domain.PartyCertificates:
		return "Сертификаты"
	default:
		return string(p.Kind)
	}
}

func movementKindTitle(t domain.MovementType) string {
	switch t {
	case domain.Deposit:
		return "Внесено в кассу"
	case domain.Payout:
		return "Выдано из кассы"
	case domain.Expense:
		return "Расход"
	case domain.Transfer:
		return "Перевод между счетами"
	case domain.Adjustment:
		return "Пересчёт кассы"
	case domain.Opening:
		return "Начальный остаток"
	case domain.Salary:
		return "Зарплата"
	case domain.SelfBonus:
		return "Доплата «Себе»"
	case domain.Dividend:
		return "Дивиденды"
	default:
		return "Движение"
	}
}

func movementStatusTitle(s domain.MovementStatus) string {
	switch s {
	case domain.Pending:
		return "Ожидает подтверждения"
	case domain.Voided:
		return "Отменено"
	default:
		return "Подтверждено"
	}
}
