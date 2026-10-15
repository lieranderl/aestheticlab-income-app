// Command resetpass gives an account a new temporary password when nobody
// can reset it in the app (the admin lost theirs or is locked out). The
// person must change it at the next login; every session ends, the lock is
// cleared and the change is audited.
//
// Run it locally with Application Default Credentials (docs/MAINTAINING.md):
//
//	go run ./cmd/resetpass -project <project> -username anna
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

func main() { os.Exit(resetpass()) }

func resetpass() int {
	project := flag.String("project", os.Getenv("INCOME_FIRESTORE_PROJECT"), "Google Cloud project")
	database := flag.String("database", cmp.Or(os.Getenv("INCOME_FIRESTORE_DATABASE"), "aestheticlab-income"), "Firestore database")
	username := flag.String("username", "", "whose password to reset")
	flag.Parse()
	if *project == "" || *username == "" {
		fmt.Fprintln(os.Stderr, "resetpass: -project and -username are required")
		return 2
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	s, err := store.NewFirestore(ctx, *project, *database)
	if err != nil {
		fmt.Fprintln(os.Stderr, "resetpass:", err)
		return 1
	}
	defer func() { _ = s.Close() }()
	if err := run(ctx, s, os.Stdout, *username); err != nil {
		fmt.Fprintln(os.Stderr, "resetpass:", err)
		return 1
	}
	return 0
}

// cliActor is how the change log names this command.
var cliActor = store.Actor{UID: "cli:resetpass", Admin: true}

// run sets a new temporary password and prints it once.
func run(ctx context.Context, s *store.Store, out io.Writer, username string) error {
	u, err := s.UserByUsername(ctx, username)
	if errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("no account %q", strings.ToLower(strings.TrimSpace(username)))
	}
	if err != nil {
		return err
	}
	password := strings.ToLower(rand.Text()[:12])
	hash, err := bcrypt.GenerateFromPassword([]byte(password), 12) // the web app's cost
	if err != nil {
		return err
	}
	_, err = s.UpdateUser(ctx, cliActor, u.ID, func(x *store.User) error {
		x.PasswordHash, x.MustChangePassword = string(hash), true
		x.SessionVersion++ // signed out everywhere
		x.FailedLogins, x.LockedUntil = 0, time.Time{}
		return nil
	})
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(out, "New temporary password for %q, shown once; it must be changed at the next login:\n%s\n", u.Username, password)
	return err
}
