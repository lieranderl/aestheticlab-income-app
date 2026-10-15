// Command renameaccount changes a kassa account's display name (for example
// «Касса — карта салона»), with an audit row by "cli:renameaccount". The app
// has no screen for it; balances and records are untouched. Run with
// -dry-run first, then without it after approval.
//
// Run it locally with Application Default Credentials (docs/MAINTAINING.md):
//
//	go run ./cmd/renameaccount -project <project> -id kassa_card -name "Касса — карта салона" -dry-run
package main

import (
	"cmp"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/lieranderl/aestheticlab-income-app/internal/store"
)

func main() { os.Exit(renameaccount()) }

func renameaccount() int {
	project := flag.String("project", os.Getenv("INCOME_FIRESTORE_PROJECT"), "Google Cloud project")
	database := flag.String("database", cmp.Or(os.Getenv("INCOME_FIRESTORE_DATABASE"), "aestheticlab-income"), "Firestore database")
	id := flag.String("id", "", "the account ID (kassa_cash or kassa_card)")
	name := flag.String("name", "", "the new display name")
	dryRun := flag.Bool("dry-run", false, "print the change without writing it")
	flag.Parse()
	if *project == "" || *id == "" || strings.TrimSpace(*name) == "" {
		fmt.Fprintln(os.Stderr, "renameaccount: -project, -id and -name are required")
		return 2
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	s, err := store.NewFirestore(ctx, *project, *database)
	if err != nil {
		fmt.Fprintln(os.Stderr, "renameaccount:", err)
		return 1
	}
	defer func() { _ = s.Close() }()
	if err := run(ctx, s, os.Stdout, *id, *name, *dryRun); err != nil {
		fmt.Fprintln(os.Stderr, "renameaccount:", err)
		return 1
	}
	return 0
}

// cliActor is how the change log names this command.
var cliActor = store.Actor{UID: "cli:renameaccount", Admin: true}

func run(ctx context.Context, s *store.Store, out io.Writer, id, name string, dryRun bool) error {
	accounts, err := s.Accounts(ctx)
	if err != nil {
		return err
	}
	current := ""
	for _, a := range accounts {
		if a.ID == id {
			current = a.Name
		}
	}
	if current == "" {
		return fmt.Errorf("no account %q: %w", id, store.ErrNotFound)
	}
	name = strings.TrimSpace(name)
	if dryRun {
		_, err := fmt.Fprintf(out, "dry run: %s «%s» → «%s» (nothing written)\n", id, current, name)
		return err
	}
	a, err := s.RenameAccount(ctx, cliActor, id, name)
	if errors.Is(err, store.ErrInvalid) {
		return fmt.Errorf("check the name: %w", err)
	}
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(out, "renamed %s «%s» → «%s»\n", a.ID, current, a.Name)
	return err
}
