package main

import (
	"cmp"
	"context"
	"encoding/csv"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/lieranderl/aestheticlab-income-app/internal/domain"
	"github.com/lieranderl/aestheticlab-income-app/internal/store"
)

// History: the «4 Trend Dashboard» tab of the sheet, downloaded as CSV
// (File → Download → CSV, current sheet), loaded into importedMonths for the
// months before the start month (implementation-plan M10).
//
//	import history [-dry-run] [-person "Sheet name=username"]... FILE
//
// The tab has a header row starting with «Month» and the figure columns
// below; to its right a second «Month» column starts the staff block, one
// column per person, ending at a blank column or a «Total…» column (checked
// against Total Revenue). Rows whose first cell is a month (YYYY-MM) are loaded,
// except blank ones; the «Total» row must equal the sum of the loaded months.

// historyCmd is `import history`: flags, the file and the Firestore store.
//
//	go run ./cmd/import history -project <project> -database aestheticlab-income -dry-run trend.csv
func historyCmd(args []string) int {
	fs := flag.NewFlagSet("history", flag.ContinueOnError)
	project := fs.String("project", os.Getenv("INCOME_FIRESTORE_PROJECT"), "Google Cloud project")
	database := fs.String("database", cmp.Or(os.Getenv("INCOME_FIRESTORE_DATABASE"), "aestheticlab-income"), "Firestore database")
	startMonth := fs.String("start-month", os.Getenv("INCOME_START_MONTH"), "the first month the app keeps (required; production: 2026-10)")
	admin := fs.String("admin", "", "username of the admin the months are recorded as (default: the only active admin)")
	dryRun := fs.Bool("dry-run", false, "check and print the figures, write nothing")
	people := personFlag{}
	fs.Var(people, "person", "a staff column and its username, NAME=USERNAME (repeatable)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *project == "" || fs.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "import history: -project and one FILE are required")
		return 2
	}
	if *startMonth == "" { // the code default would let the app's own months in
		fmt.Fprintln(os.Stderr, "import history: name the start month with -start-month (production: 2026-10)")
		return 2
	}
	start, err := domain.ParseMonth(*startMonth)
	if err != nil {
		fmt.Fprintln(os.Stderr, "import history:", err)
		return 2
	}
	f, err := os.Open(fs.Arg(0))
	if err != nil {
		fmt.Fprintln(os.Stderr, "import history:", err)
		return 1
	}
	defer func() { _ = f.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	st, err := store.NewFirestore(ctx, *project, *database, store.WithStartMonth(start))
	if err != nil {
		fmt.Fprintln(os.Stderr, "import history:", err)
		return 1
	}
	defer func() { _ = st.Close() }()
	opt := historyOptions{admin: *admin, dryRun: *dryRun, source: filepath.Base(fs.Arg(0)), people: people}
	if err := runHistory(ctx, st, opt, f, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "import history:", err)
		return 1
	}
	return 0
}

// personFlag collects -person NAME=USERNAME.
type personFlag map[string]string

func (p personFlag) String() string { return "" }

func (p personFlag) Set(v string) error {
	name, username, ok := strings.Cut(v, "=")
	name, username = strings.TrimSpace(name), strings.TrimSpace(username)
	if !ok || name == "" || username == "" {
		return errors.New("want NAME=USERNAME")
	}
	p[name] = username
	return nil
}

type historyOptions struct {
	admin  string            // username of the admin the rows are recorded as
	dryRun bool              // check and print, write nothing
	source string            // the file, recorded on each month
	people map[string]string // sheet column name → username, beyond the automatic match
}

// The figure columns, by their header in the sheet.
const (
	colMonth    = "Month"
	colRevenue  = "Total Revenue"
	colSalon    = "Salon Retained Share"
	colOverhead = "Overhead Expenses"
	colSalary   = "Manager Salary"
	colExpenses = "Total Salon Expenses" // optional: overhead + salary
	colProfit   = "Net Operating Profit"
	colDividend = "Owner Dividends"
)

// sheetMonth is one month row of the file, checked.
type sheetMonth struct {
	month  domain.Month
	figure domain.ImportedMonth
	state  string // "", "already loaded" or "replaces the imported figures"
}

// runHistory reads the file, checks every month, prints the figures and,
// unless it is a dry run, stores the months that changed in one transaction.
func runHistory(ctx context.Context, st *store.Store, opt historyOptions, in io.Reader, out io.Writer) error {
	admin, err := recordingAdmin(ctx, st, opt.admin)
	if err != nil {
		return err
	}
	records, err := readCSV(in)
	if err != nil {
		return err
	}
	months, people, err := parseTrend(ctx, st, records, opt.people)
	if err != nil {
		return err
	}
	have, err := st.ImportedMonths(ctx)
	if err != nil {
		return err
	}
	changed := map[domain.Month]domain.ImportedMonth{}
	for i, m := range months {
		prev, ok := have[m.month]
		switch {
		case !ok:
			changed[m.month] = m.figure
		case reflect.DeepEqual(prev, m.figure):
			months[i].state = "already loaded"
		default:
			months[i].state = "replaces the imported figures"
			changed[m.month] = m.figure
		}
	}

	p := &printer{w: out}
	mode := "will be loaded"
	if opt.dryRun {
		mode = "dry run: nothing written"
	}
	p.printf("Sheet history before %s (%s)\n", st.StartMonth(), mode)
	tw := &printer{w: tabwriter.NewWriter(out, 0, 4, 2, ' ', tabwriter.AlignRight)}
	tw.printf("month\trevenue\tsalon\texpenses\tmanager salary\tprofit\tdividends\t\n")
	var sum domain.ImportedMonth
	for _, m := range months {
		f := m.figure
		tw.printf("%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", m.month, domain.FormatEUR(f.RevenueTotal), domain.FormatEUR(f.SalonIncome),
			domain.FormatEUR(f.Expenses), domain.FormatEUR(f.ManagerSalary), domain.FormatEUR(f.Profit), domain.FormatEUR(f.Dividends), m.state)
		sum = addMonth(sum, f)
	}
	tw.printf("total\t%s\t%s\t%s\t%s\t%s\t%s\t\n", domain.FormatEUR(sum.RevenueTotal), domain.FormatEUR(sum.SalonIncome),
		domain.FormatEUR(sum.Expenses), domain.FormatEUR(sum.ManagerSalary), domain.FormatEUR(sum.Profit), domain.FormatEUR(sum.Dividends))
	if err := tw.flush(); err != nil {
		return err
	}
	p.printf("The sum matches the sheet's Total row.\n")
	p.printf("Revenue per person:\n")
	pw := &printer{w: tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)}
	for _, person := range people {
		var all domain.Cents
		var line strings.Builder
		for _, m := range months {
			c := m.figure.RevenueByPerson[person.ID]
			all += c
			fmt.Fprintf(&line, "\t%s %s", m.month, domain.FormatEUR(c))
		}
		pw.printf("  %s (%s)%s\ttotal %s\n", person.DisplayName, person.Username, line.String(), domain.FormatEUR(all))
	}
	if err := pw.flush(); err != nil {
		return err
	}
	if opt.dryRun || p.err != nil {
		return p.err
	}
	if len(changed) > 0 {
		if err := st.SaveImportedMonths(ctx, admin.Actor(), changed, "sheet «4 Trend Dashboard» "+opt.source); err != nil {
			p.printf("loaded 0 of %d\n", len(changed))
			return err
		}
	}
	p.printf("loaded %d\n", len(changed))
	return p.err
}

// readCSV reads a whole file of at most 1 MiB, comma or semicolon separated.
func readCSV(in io.Reader) ([][]string, error) {
	raw, err := io.ReadAll(io.LimitReader(in, 1<<20+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > 1<<20 {
		return nil, errors.New("the file is larger than 1 MiB: is it the «4 Trend Dashboard» tab?")
	}
	text := strings.TrimPrefix(string(raw), "\ufeff") // Excel's BOM
	r := csv.NewReader(strings.NewReader(text))
	r.Comma = ','
	if strings.Count(text, ";") > strings.Count(text, ",") {
		r.Comma = ';'
	}
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	return r.ReadAll()
}

// parseTrend finds the header, reads every month row and checks that the
// sheet's figures add up, the Total row included. It returns the months in
// file order and the people of the staff block.
func parseTrend(ctx context.Context, st *store.Store, records [][]string, names map[string]string) ([]sheetMonth, []store.User, error) {
	head := -1
	for i, rec := range records {
		if len(rec) > 0 && strings.EqualFold(strings.TrimSpace(rec[0]), colMonth) {
			head = i
			break
		}
	}
	if head < 0 {
		return nil, nil, errors.New("no header row: the first column of one row must be «Month» (download the «4 Trend Dashboard» tab)")
	}
	header := records[head]
	col := map[string]int{}
	staffAt := -1
	for i, h := range header {
		h = strings.TrimSpace(h)
		if i > 0 && strings.EqualFold(h, colMonth) {
			staffAt = i
			break
		}
		col[strings.ToLower(h)] = i
	}
	for _, name := range []string{colRevenue, colSalon, colOverhead, colSalary, colProfit, colDividend} {
		if _, ok := col[strings.ToLower(name)]; !ok {
			return nil, nil, fmt.Errorf("the header has no %q column", name)
		}
	}
	if staffAt < 0 {
		return nil, nil, errors.New("the header has no staff block (a second «Month» column followed by one column per person)")
	}
	// The staff block ends at a blank column or at its own «Total…» column,
	// which must equal the month's Total Revenue.
	staffHeader, staffTotalAt := header[staffAt+1:], -1
	for i, h := range staffHeader {
		h = strings.TrimSpace(h)
		if h == "" {
			staffHeader = staffHeader[:i]
			break
		}
		if strings.HasPrefix(strings.ToLower(h), "total") {
			staffHeader, staffTotalAt = staffHeader[:i], staffAt+1+i
			break
		}
	}
	people, err := staffColumns(ctx, st, staffHeader, names)
	if err != nil {
		return nil, nil, err
	}

	cell := func(rec []string, i int) string {
		if i < len(rec) {
			return strings.TrimSpace(rec[i])
		}
		return ""
	}
	figure := func(rec []string, line int, name string, signed bool) (domain.Cents, error) {
		raw := cell(rec, col[strings.ToLower(name)])
		if blankCell(raw) {
			return 0, fmt.Errorf("line %d: %s is blank", line, name)
		}
		c, err := sheetAmount(raw)
		if err != nil {
			return 0, fmt.Errorf("line %d: %s %q: %w", line, name, raw, err)
		}
		if c < 0 && !signed {
			return 0, fmt.Errorf("line %d: %s %s is below zero", line, name, domain.FormatEUR(c))
		}
		return c, nil
	}

	var months []sheetMonth
	seen := map[domain.Month]int{}
	var totalRow []string
	totalLine := 0
	for i, rec := range records[head+1:] {
		line := head + i + 2
		first := cell(rec, 0)
		if strings.EqualFold(first, "Total") {
			if totalRow != nil {
				return nil, nil, fmt.Errorf("two Total rows (lines %d and %d)", totalLine, line)
			}
			totalRow, totalLine = rec, line
			continue
		}
		m, err := domain.ParseMonth(first)
		if err != nil {
			continue // a title, a blank line, the «Average» row
		}
		if prev, dup := seen[m]; dup {
			return nil, nil, fmt.Errorf("line %d: %s is in the file twice (also line %d)", line, m, prev)
		}
		if blankRow(rec[1:]) {
			continue // a month the sheet hasn't filled in
		}
		seen[m] = line
		if !m.Before(st.StartMonth()) {
			return nil, nil, fmt.Errorf("line %d: %s is kept by the app; only months before %s are history", line, m, st.StartMonth())
		}
		if got := cell(rec, staffAt); got != m.String() {
			return nil, nil, fmt.Errorf("line %d: the staff block is for %q, not %s", line, got, m)
		}
		f, err := monthFigures(rec, line, figure)
		if err != nil {
			return nil, nil, err
		}
		f.RevenueByPerson = map[string]domain.Cents{}
		var staff domain.Cents
		for j, person := range people {
			raw := cell(rec, staffAt+1+j)
			if blankCell(raw) {
				return nil, nil, fmt.Errorf("line %d: %s's revenue is blank", line, person.DisplayName)
			}
			c, err := sheetAmount(raw)
			if err != nil {
				return nil, nil, fmt.Errorf("line %d: %s's revenue %q: %w", line, person.DisplayName, raw, err)
			}
			if c < 0 {
				return nil, nil, fmt.Errorf("line %d: %s's revenue %s is below zero", line, person.DisplayName, domain.FormatEUR(c))
			}
			if c != 0 {
				f.RevenueByPerson[person.ID] += c
			}
			staff += c
		}
		if staffTotalAt >= 0 {
			raw := cell(rec, staffTotalAt)
			got, err := sheetAmount(raw)
			if blankCell(raw) || err != nil {
				return nil, nil, fmt.Errorf("line %d: the staff block's Total Revenue %q is not an amount", line, raw)
			}
			if got != f.RevenueTotal {
				return nil, nil, fmt.Errorf("line %d: %s the staff block's Total Revenue %s is not the Total Revenue %s", line, m, domain.FormatEUR(got), domain.FormatEUR(f.RevenueTotal))
			}
		}
		if staff != f.RevenueTotal {
			return nil, nil, fmt.Errorf("line %d: %s staff revenue adds up to %s, not the Total Revenue %s", line, m, domain.FormatEUR(staff), domain.FormatEUR(f.RevenueTotal))
		}
		if want := f.SalonIncome - f.Expenses - f.ManagerSalary; f.Profit != want {
			return nil, nil, fmt.Errorf("line %d: %s profit %s is not salon share − overhead − manager salary = %s", line, m, domain.FormatEUR(f.Profit), domain.FormatEUR(want))
		}
		if _, ok := col[strings.ToLower(colExpenses)]; ok {
			got, err := figure(rec, line, colExpenses, false)
			if err != nil {
				return nil, nil, err
			}
			if want := f.Expenses + f.ManagerSalary; got != want {
				return nil, nil, fmt.Errorf("line %d: %s Total Salon Expenses %s is not overhead + manager salary = %s", line, m, domain.FormatEUR(got), domain.FormatEUR(want))
			}
		}
		months = append(months, sheetMonth{month: m, figure: f})
	}
	if len(months) == 0 {
		return nil, nil, errors.New("no months in the file (rows whose first cell is YYYY-MM)")
	}
	if totalRow == nil {
		return nil, nil, errors.New("no Total row: the sum of the months is checked against it")
	}
	want, err := monthFigures(totalRow, totalLine, figure)
	if err != nil {
		return nil, nil, err
	}
	var sum domain.ImportedMonth
	for _, m := range months {
		sum = addMonth(sum, m.figure)
	}
	if want.RevenueByPerson = nil; !reflect.DeepEqual(sum, want) {
		return nil, nil, fmt.Errorf("line %d: the months add up to revenue %s, salon %s, overhead %s, salary %s, profit %s, dividends %s — not the sheet's Total row (%s, %s, %s, %s, %s, %s)",
			totalLine, domain.FormatEUR(sum.RevenueTotal), domain.FormatEUR(sum.SalonIncome), domain.FormatEUR(sum.Expenses), domain.FormatEUR(sum.ManagerSalary), domain.FormatEUR(sum.Profit), domain.FormatEUR(sum.Dividends),
			domain.FormatEUR(want.RevenueTotal), domain.FormatEUR(want.SalonIncome), domain.FormatEUR(want.Expenses), domain.FormatEUR(want.ManagerSalary), domain.FormatEUR(want.Profit), domain.FormatEUR(want.Dividends))
	}
	return months, people, nil
}

// monthFigures reads a row's totals (no staff block).
func monthFigures(rec []string, line int, figure func([]string, int, string, bool) (domain.Cents, error)) (domain.ImportedMonth, error) {
	var f domain.ImportedMonth
	var err error
	for _, c := range []struct {
		name   string
		dst    *domain.Cents
		signed bool
	}{
		{colRevenue, &f.RevenueTotal, false}, {colSalon, &f.SalonIncome, false}, {colOverhead, &f.Expenses, false},
		{colSalary, &f.ManagerSalary, false}, {colProfit, &f.Profit, true}, {colDividend, &f.Dividends, false},
	} {
		if *c.dst, err = figure(rec, line, c.name, c.signed); err != nil {
			return f, err
		}
	}
	return f, nil
}

// addMonth adds the totals of b to a (not the people).
func addMonth(a, b domain.ImportedMonth) domain.ImportedMonth {
	a.RevenueTotal += b.RevenueTotal
	a.SalonIncome += b.SalonIncome
	a.Expenses += b.Expenses
	a.ManagerSalary += b.ManagerSalary
	a.Profit += b.Profit
	a.Dividends += b.Dividends
	a.RevenueByPerson = nil
	return a
}

// staffColumns matches each staff column to a person: by -person, else by
// username, display name or its first word, ignoring case. A column that
// matches nobody, or more than one person, is refused.
func staffColumns(ctx context.Context, st *store.Store, header []string, names map[string]string) ([]store.User, error) {
	users, err := st.Users(ctx)
	if err != nil {
		return nil, fmt.Errorf("list the people: %w", err)
	}
	var people []store.User
	seen := map[string]string{}
	for _, h := range header {
		h = strings.TrimSpace(h)
		if h == "" {
			break // the end of the staff block
		}
		var match []store.User
		if username, ok := names[h]; ok {
			for _, u := range users {
				if strings.EqualFold(u.Username, username) {
					match = append(match, u)
				}
			}
			if len(match) == 0 {
				return nil, fmt.Errorf("-person %s=%s: no user %q", h, username, username)
			}
		} else {
			for _, u := range users {
				first, _, _ := strings.Cut(u.DisplayName, " ")
				if strings.EqualFold(u.Username, h) || strings.EqualFold(u.DisplayName, h) || strings.EqualFold(first, h) {
					match = append(match, u)
				}
			}
		}
		switch len(match) {
		case 0:
			return nil, fmt.Errorf("the staff column %q matches nobody: name the person with -person %q", h, h+"=username")
		case 1:
		default:
			return nil, fmt.Errorf("the staff column %q matches %d people: name one with -person %q", h, len(match), h+"=username")
		}
		u := match[0]
		if prev, dup := seen[u.ID]; dup {
			return nil, fmt.Errorf("the staff columns %q and %q are both %s", prev, h, u.Username)
		}
		seen[u.ID] = h
		people = append(people, u)
	}
	if len(people) == 0 {
		return nil, errors.New("the staff block names nobody")
	}
	return people, nil
}

// sheetAmount reads a figure as the sheet shows it, in either locale:
// "€12,428.00", "€699.00", "12 428,50 €", "-€2.50". When a separator is
// followed by one or two digits at the end it is the decimal one; every
// other separator groups thousands.
func sheetAmount(s string) (domain.Cents, error) {
	s = strings.Map(func(r rune) rune {
		switch r {
		case '€':
			return -1
		case '\u00a0', '\u202f':
			return ' '
		case '−':
			return '-'
		}
		return r
	}, s)
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimSpace(strings.TrimPrefix(s, "-"))
	if s == "" {
		return 0, domain.ErrInvalidAmount
	}
	whole, frac := s, ""
	if i := strings.LastIndexAny(s, ",."); i >= 0 && len(s)-i-1 < 3 {
		// A decimal separator: two digits after it, and it groups nothing.
		if len(s)-i-1 != 2 || strings.IndexByte(s[:i], s[i]) >= 0 {
			return 0, domain.ErrInvalidAmount
		}
		whole, frac = s[:i], s[i+1:]
	}
	if strings.Contains(whole, ",") && strings.Contains(whole, ".") {
		return 0, domain.ErrInvalidAmount // one thousands separator, not two
	}
	groups := strings.Split(strings.NewReplacer(",", " ", ".", " ").Replace(whole), " ")
	for i, g := range groups {
		if g == "" || i > 0 && len(g) != 3 || len(groups) > 1 && i == 0 && len(g) > 3 {
			return 0, domain.ErrInvalidAmount
		}
	}
	plain := strings.Join(groups, "")
	if frac != "" {
		plain += "," + frac
	}
	c, err := domain.ParseAmount(plain)
	if err != nil {
		return 0, err
	}
	if neg {
		c = -c
	}
	return c, nil
}

// blankCell is an empty figure, or one the sheet shows as a dash.
func blankCell(s string) bool {
	return strings.Trim(s, "€-− \u00a0\u202f") == ""
}

// blankRow is a row with no figure at all after its month.
func blankRow(rec []string) bool {
	for _, c := range rec {
		if c = strings.TrimSpace(c); c != "" && !blankCell(c) {
			if _, err := domain.ParseMonth(c); err != nil {
				return false
			}
		}
	}
	return true
}
