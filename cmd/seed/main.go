// Command seed writes the Kassa's first data once: Anna's admin account
// with a temporary password, the default categories and the two kassa
// accounts. It refuses to run when any user exists.
//
// Run it locally with Application Default Credentials (docs/MAINTAINING.md):
//
//	go run ./cmd/seed -project <project> -database aestheticlab-income
package main

import (
	"cmp"
	"context"
	"crypto/rand"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/lieranderl/aestheticlab-income-app/internal/store"
)

func main() { os.Exit(seed()) }

func seed() int {
	project := flag.String("project", os.Getenv("INCOME_FIRESTORE_PROJECT"), "Google Cloud project")
	database := flag.String("database", cmp.Or(os.Getenv("INCOME_FIRESTORE_DATABASE"), "aestheticlab-income"), "Firestore database")
	username := flag.String("username", "anna", "the admin's login name")
	name := flag.String("name", "Анна", "the admin's display name")
	flag.Parse()
	if *project == "" {
		fmt.Fprintln(os.Stderr, "seed: -project is required")
		return 2
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	s, err := store.NewFirestore(ctx, *project, *database)
	if err != nil {
		fmt.Fprintln(os.Stderr, "seed:", err)
		return 1
	}
	defer func() { _ = s.Close() }()
	err = run(ctx, s, os.Stdout, *username, *name)
	switch {
	case errors.Is(err, errAlreadySetUp):
		fmt.Fprintln(os.Stderr, "seed:", err)
		return exitAlreadySetUp
	case err != nil:
		fmt.Fprintln(os.Stderr, "seed:", err)
		return 1
	}
	return 0
}

// exitAlreadySetUp lets scripts (make seed-local) tell "nothing to do"
// from a failure.
const exitAlreadySetUp = 3

var errAlreadySetUp = errors.New("already set up: users exist, nothing written")

// run sets the store up and prints the admin's temporary password once.
func run(ctx context.Context, s *store.Store, out io.Writer, username, name string) error {
	password := tempPassword()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), 12) // the web app's cost
	if err != nil {
		return err
	}
	admin, err := s.SetUp(ctx, store.DefaultSetup(store.NewUser{
		Username: username, DisplayName: name, Role: store.RoleAdmin, SubmitsReports: true, PasswordHash: string(hash),
	}))
	if errors.Is(err, store.ErrAlreadySetUp) {
		return errAlreadySetUp
	}
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(out, "Set up. Admin %q (ID %s).\nTemporary password, shown once; it must be changed at first login:\n%s\n", admin.Username, admin.ID, password)
	return err
}

// tempPassword is 12 random lowercase letters and digits (60 bits).
func tempPassword() string { return strings.ToLower(rand.Text()[:12]) }
