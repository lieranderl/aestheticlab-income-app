// Command import loads data from the «Aesthetic Lab Finance» sheet into the
// Kassa (technical-solution §5.6, implementation-plan M5 and M10).
//
//	import opening [-dry-run] FILE
//
// loads the opening balances on the start month's first day: the till, the
// salon money on the salon's card, outstanding certificates, and each person's
// balance after the previous month's settlement in the sheet. FILE is a CSV
// (never committed):
//
//	kind;id;amount;already_paid
//	kassa;kassa_cash;412,50;
//	kassa;kassa_card;1 830;
//	certificates;;500;
//	person;maria;265,13;0
//	person;anna;-42,70;1950
//
// kind is kassa (id = account), certificates (no id) or person (id =
// username; a negative amount means they owe the salon). already_paid is the
// sheet's «Already paid» for the month before the start, printed beside each
// person for checking.
// The delimiter is ; or , as the header uses. Run with -dry-run first and
// have Anna confirm the printed figures. The figures load in one
// transaction, whole or not at all. Rows already loaded with the same
// amount are skipped, so a later run can add the person rows.
//
// Run it locally with Application Default Credentials:
//
//	go run ./cmd/import opening -project <project> -database aestheticlab-income -dry-run opening.csv
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
	"strings"
	"text/tabwriter"
	"time"

	"github.com/lieranderl/aestheticlab-income-app/internal/domain"
	"github.com/lieranderl/aestheticlab-income-app/internal/store"
)

func main() { os.Exit(importCmd(os.Args[1:])) }

func importCmd(args []string) int {
	if len(args) == 0 || args[0] != "opening" {
		fmt.Fprintln(os.Stderr, "usage: import opening [-project P] [-database D] [-start-month YYYY-MM] [-admin USERNAME] [-dry-run] FILE")
		return 2
	}
	fs := flag.NewFlagSet("opening", flag.ContinueOnError)
	project := fs.String("project", os.Getenv("INCOME_FIRESTORE_PROJECT"), "Google Cloud project")
	database := fs.String("database", cmp.Or(os.Getenv("INCOME_FIRESTORE_DATABASE"), "aestheticlab-income"), "Firestore database")
	startMonth := fs.String("start-month", cmp.Or(os.Getenv("INCOME_START_MONTH"), store.DefaultStartMonth.String()), "the first month the app keeps")
	admin := fs.String("admin", "anna", "the admin the rows are recorded as")
	dryRun := fs.Bool("dry-run", false, "check and print the figures, write nothing")
	if err := fs.Parse(args[1:]); err != nil {
		return 2
	}
	if *project == "" || fs.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "import opening: -project and one FILE are required")
		return 2
	}
	start, err := domain.ParseMonth(*startMonth)
	if err != nil {
		fmt.Fprintln(os.Stderr, "import opening:", err)
		return 2
	}
	f, err := os.Open(fs.Arg(0))
	if err != nil {
		fmt.Fprintln(os.Stderr, "import opening:", err)
		return 1
	}
	defer func() { _ = f.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	st, err := store.NewFirestore(ctx, *project, *database, store.WithStartMonth(start))
	if err != nil {
		fmt.Fprintln(os.Stderr, "import opening:", err)
		return 1
	}
	defer func() { _ = st.Close() }()
	if err := runOpening(ctx, st, options{admin: *admin, dryRun: *dryRun}, f, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "import opening:", err)
		return 1
	}
	return 0
}

type options struct {
	admin  string // username of the admin the rows are recorded as
	dryRun bool
}

// openingRow is one figure of the file, checked against the store.
type openingRow struct {
	line        int
	label       string // what the figure is, for the printout
	party       domain.Party
	amount      domain.Cents
	alreadyPaid string // the sheet's «Already paid» for the month before the start, as written
	loaded      bool   // the same figure is already in the ledger
}

// runOpening reads the file, checks every row, prints the figures and,
// unless it is a dry run, records one opening row per figure not yet loaded.
// A file with any mistake writes nothing.
func runOpening(ctx context.Context, st *store.Store, opt options, in io.Reader, out io.Writer) error {
	admin, err := st.UserByUsername(ctx, opt.admin)
	if err != nil || admin.Role != store.RoleAdmin {
		return fmt.Errorf("admin %q not found", opt.admin)
	}
	rows, err := readOpening(ctx, st, in)
	if err != nil {
		return err
	}
	day := st.StartMonth().FirstDay()
	if err := markLoaded(ctx, st, rows); err != nil {
		return err
	}

	p := &printer{w: out}
	mode := "will be loaded"
	if opt.dryRun {
		mode = "dry run: nothing written"
	}
	p.printf("Opening balances dated %s (%s)\n", day, mode)
	if today := st.Today(); today.Before(day) {
		p.printf("Today is %s: they can be loaded from %s on.\n", today, day)
	}
	tw := &printer{w: tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)}
	var kassa domain.Cents
	for _, r := range rows {
		note := ""
		if r.alreadyPaid != "" {
			note = "already paid in " + day.Month().Prev().Mon.String() + " (sheet): " + r.alreadyPaid
		}
		if r.loaded {
			note = strings.TrimSpace("already loaded, skipped " + note)
		}
		tw.printf("  %s\t%s\t%s\n", r.label, domain.FormatEUR(r.amount), note)
		if r.party.Kind == domain.PartyAccount {
			kassa += r.amount
		}
	}
	if err := tw.flush(); err != nil {
		return err
	}
	p.printf("kassa total %s\n", domain.FormatEUR(kassa))
	if opt.dryRun || p.err != nil {
		return p.err
	}

	var batch []store.NewMovement
	for _, r := range rows {
		if !r.loaded {
			batch = append(batch, store.NewMovement{
				Type: domain.Opening, Amount: r.amount, From: domain.Salon, To: r.party, Date: day,
				Note: "Начальный остаток",
			})
		}
	}
	loaded := 0
	if len(batch) > 0 { // one transaction: the file loads whole or not at all
		if _, err := st.RecordOpenings(ctx, admin.Actor(), batch); err != nil {
			p.printf("loaded 0 of %d\n", len(batch))
			return err
		}
		loaded = len(batch)
	}
	p.printf("loaded %d\n", loaded)
	return p.err
}

// printer writes the report and keeps the first write error.
type printer struct {
	w   io.Writer
	err error
}

func (p *printer) printf(format string, args ...any) {
	if p.err == nil {
		_, p.err = fmt.Fprintf(p.w, format, args...)
	}
}

// flush writes out a tabwriter's table.
func (p *printer) flush() error {
	if tw, ok := p.w.(*tabwriter.Writer); ok && p.err == nil {
		p.err = tw.Flush()
	}
	return p.err
}

// readOpening parses and checks the whole file before anything is written.
func readOpening(ctx context.Context, st *store.Store, in io.Reader) ([]openingRow, error) {
	raw, err := io.ReadAll(io.LimitReader(in, 1<<20))
	if err != nil {
		return nil, err
	}
	text := strings.TrimPrefix(string(raw), "\ufeff") // Excel's BOM
	header, _, _ := strings.Cut(text, "\n")
	r := csv.NewReader(strings.NewReader(text))
	r.Comma = ','
	if strings.Contains(header, ";") {
		r.Comma = ';'
	}
	r.FieldsPerRecord = -1
	r.TrimLeadingSpace = true
	records, err := r.ReadAll()
	if err != nil {
		return nil, err
	}
	if len(records) == 0 || len(records[0]) < 3 || strings.ToLower(strings.TrimSpace(records[0][0])) != "kind" {
		return nil, errors.New("the first line must be the header kind;id;amount;already_paid")
	}

	accounts, err := st.Accounts(ctx)
	if err != nil {
		return nil, err
	}
	var rows []openingRow
	seen := map[domain.Party]int{}
	for i, rec := range records[1:] {
		line := i + 2
		if len(rec) > 4 {
			return nil, fmt.Errorf("line %d: %d columns, at most 4 (a decimal comma in a comma-separated file? use ; as the separator)", line, len(rec))
		}
		for len(rec) < 4 {
			rec = append(rec, "")
		}
		kind, id := strings.ToLower(strings.TrimSpace(rec[0])), strings.TrimSpace(rec[1])
		if kind == "" && id == "" && strings.TrimSpace(rec[2]) == "" {
			continue // blank line
		}
		amount, err := signedAmount(rec[2])
		if err != nil {
			return nil, fmt.Errorf("line %d: amount %q: %w", line, rec[2], err)
		}
		row := openingRow{line: line, amount: amount, alreadyPaid: strings.TrimSpace(rec[3])}
		switch kind {
		case "kassa":
			var found bool
			for _, a := range accounts {
				if a.ID == id && a.Active {
					row.label, row.party, found = a.Name, domain.Account(a.ID), true
				}
			}
			if !found {
				return nil, fmt.Errorf("line %d: no kassa account %q", line, id)
			}
		case "certificates":
			if id != "" {
				return nil, fmt.Errorf("line %d: certificates take no id", line)
			}
			row.label, row.party = "certificates sold, not yet used", domain.Certificates
		case "person":
			u, err := st.UserByUsername(ctx, id)
			if err != nil {
				return nil, fmt.Errorf("line %d: no person %q", line, id)
			}
			row.label, row.party = fmt.Sprintf("%s (%s)", u.DisplayName, u.Username), domain.Person(u.ID)
		default:
			return nil, fmt.Errorf("line %d: kind %q is not kassa, certificates or person", line, kind)
		}
		if row.alreadyPaid != "" && kind != "person" {
			return nil, fmt.Errorf("line %d: already_paid is for people only", line)
		}
		if row.alreadyPaid != "" {
			paid, err := domain.ParseAmount(row.alreadyPaid)
			if err != nil {
				return nil, fmt.Errorf("line %d: already_paid %q: %w", line, row.alreadyPaid, err)
			}
			row.alreadyPaid = domain.FormatEUR(paid)
		}
		if amount == 0 && kind == "person" {
			continue // nothing to carry over
		}
		if err := domain.ValidateMovement(domain.Movement{
			Type: domain.Opening, Status: domain.Confirmed, Amount: amount, From: domain.Salon, To: row.party,
		}); err != nil {
			return nil, fmt.Errorf("line %d (%s): %s must be above zero (only a person may start owing the salon)", line, row.label, domain.FormatEUR(amount))
		}
		if prev, dup := seen[row.party]; dup {
			return nil, fmt.Errorf("line %d: %s is already on line %d", line, row.label, prev)
		}
		seen[row.party] = line
		rows = append(rows, row)
	}
	if len(rows) == 0 {
		return nil, errors.New("no figures in the file")
	}
	return rows, nil
}

// markLoaded finds the figures already in the ledger. The same amount is
// skipped; a different one is refused, as the wrong row must be voided first.
func markLoaded(ctx context.Context, st *store.Store, rows []openingRow) error {
	entries, err := st.Ledger(ctx, st.StartMonth())
	if err != nil {
		return err
	}
	have := map[domain.Party]domain.Cents{}
	for _, e := range entries {
		if e.Type == domain.Opening && e.Status != domain.Voided {
			have[e.To] += e.Amount
		}
	}
	for i, r := range rows {
		got, ok := have[r.party]
		switch {
		case !ok:
		case got == r.amount:
			rows[i].loaded = true
		default:
			id := r.party.ID
			if id == "" {
				id = string(r.party.Kind)
			}
			return fmt.Errorf("line %d: %s (%s) already has an opening of %s, not %s — void it first", r.line, r.label, id, domain.FormatEUR(got), domain.FormatEUR(r.amount))
		}
	}
	return nil
}

// signedAmount reads an amount that may start with a minus sign (a person
// who owes the salon): "-42,70" or "−42,70".
func signedAmount(s string) (domain.Cents, error) {
	s = strings.TrimSpace(s)
	neg := strings.HasPrefix(s, "-") || strings.HasPrefix(s, "−")
	s = strings.TrimPrefix(strings.TrimPrefix(s, "-"), "−")
	c, err := domain.ParseAmount(s)
	if neg {
		c = -c
	}
	return c, err
}
