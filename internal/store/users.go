package store

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/lieranderl/aestheticlab-income-app/internal/domain"
)

// Role is what a person may do in the app.
type Role string

// Roles.
const (
	RoleAdmin    Role = "admin"
	RoleEmployee Role = "employee"
)

// User is a person with an account. Money terms are separate (Terms).
type User struct {
	ID                 string
	Username           string // unique, lowercase
	DisplayName        string
	Role               Role
	SubmitsReports     bool
	Active             bool // may log in
	Removed            bool // gone from the staff list; their records stay
	PasswordHash       string
	MustChangePassword bool
	SessionVersion     int64
	FailedLogins       int
	LockedUntil        time.Time
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// Actor is the user acting, for a store call.
func (u User) Actor() Actor { return Actor{UID: u.ID, Admin: u.Role == RoleAdmin} }

// NewUser is an account the admin creates.
type NewUser struct {
	Username       string
	DisplayName    string
	Role           Role
	SubmitsReports bool
	PasswordHash   string // bcrypt; the person changes it at first login
}

type userRecord struct {
	Username           string    `firestore:"username"           json:"username"`
	DisplayName        string    `firestore:"displayName"        json:"displayName"`
	Role               string    `firestore:"role"               json:"role"`
	SubmitsReports     bool      `firestore:"submitsReports"     json:"submitsReports"`
	Active             bool      `firestore:"active"             json:"active"`
	Removed            bool      `firestore:"removed"            json:"removed"`
	PasswordHash       string    `firestore:"passwordHash"       json:"passwordHash"`
	MustChangePassword bool      `firestore:"mustChangePassword" json:"mustChangePassword"`
	SessionVersion     int64     `firestore:"sessionVersion"     json:"sessionVersion"`
	FailedLogins       int       `firestore:"failedLogins"       json:"failedLogins"`
	LockedUntil        time.Time `firestore:"lockedUntil"        json:"lockedUntil"`
	CreatedAt          time.Time `firestore:"createdAt"          json:"createdAt"`
	UpdatedAt          time.Time `firestore:"updatedAt"          json:"updatedAt"`
}

// usernameRecord claims a login name for one account. A changed or
// removed account's old claim is kept with no UID: the name is free again.
type usernameRecord struct {
	UID string `firestore:"uid" json:"uid"`
}

// claimed reads who holds a login name; "" when nobody does.
func claimed(t tx, name string) (string, error) {
	var claim usernameRecord
	if _, err := t.get(usernamePath(name), &claim); err != nil {
		return "", err
	}
	return claim.UID, nil
}

func (r userRecord) user(id string) User {
	return User{
		ID: id, Username: r.Username, DisplayName: r.DisplayName, Role: Role(r.Role),
		SubmitsReports: r.SubmitsReports, Active: r.Active, Removed: r.Removed, PasswordHash: r.PasswordHash,
		MustChangePassword: r.MustChangePassword, SessionVersion: r.SessionVersion,
		FailedLogins: r.FailedLogins, LockedUntil: r.LockedUntil.UTC(),
		CreatedAt: r.CreatedAt.UTC(), UpdatedAt: r.UpdatedAt.UTC(),
	}
}

func recordOf(u User) userRecord {
	return userRecord{
		Username: u.Username, DisplayName: u.DisplayName, Role: string(u.Role),
		SubmitsReports: u.SubmitsReports, Active: u.Active, Removed: u.Removed, PasswordHash: u.PasswordHash,
		MustChangePassword: u.MustChangePassword, SessionVersion: u.SessionVersion,
		FailedLogins: u.FailedLogins, LockedUntil: u.LockedUntil.UTC(),
		CreatedAt: u.CreatedAt.UTC(), UpdatedAt: u.UpdatedAt.UTC(),
	}
}

// audited is the user as the change log shows it: never the password hash.
func (r userRecord) audited() userRecord {
	r.PasswordHash = ""
	return r
}

func normalizeUsername(name string) (string, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if err := checkID("username", name); err != nil {
		return "", err
	}
	return name, nil
}

func (n NewUser) check() (NewUser, error) {
	name, err := normalizeUsername(n.Username)
	if err != nil {
		return n, err
	}
	n.Username = name
	if strings.TrimSpace(n.DisplayName) == "" || (n.Role != RoleAdmin && n.Role != RoleEmployee) || n.PasswordHash == "" {
		return n, fmt.Errorf("%w: user needs a name, a role and a password", ErrInvalid)
	}
	return n, nil
}

// createUser writes the user and its username claim; reads come first.
func (s *Store) createUser(t tx, actorID string, n NewUser) (User, error) {
	holder, err := claimed(t, n.Username)
	if err != nil {
		return User{}, err
	}
	if holder != "" {
		return User{}, ErrUsernameTaken
	}
	now := s.stamp()
	u := User{
		ID: newID(), Username: n.Username, DisplayName: strings.TrimSpace(n.DisplayName), Role: n.Role,
		SubmitsReports: n.SubmitsReports, Active: true, PasswordHash: n.PasswordHash,
		MustChangePassword: true, SessionVersion: 1, CreatedAt: now, UpdatedAt: now,
	}
	rec := recordOf(u)
	if err := t.set(userPath(u.ID), rec); err != nil {
		return User{}, err
	}
	if err := t.set(usernamePath(u.Username), usernameRecord{UID: u.ID}); err != nil {
		return User{}, err
	}
	return u, s.audit(t, EntityUser, u.ID, ActionCreate, actorID, nil, rec.audited())
}

// CreateUser adds an account (admin only). It must change its password at
// first login.
func (s *Store) CreateUser(ctx context.Context, actor Actor, n NewUser) (User, error) {
	if !actor.Admin {
		return User{}, ErrForbidden
	}
	n, err := n.check()
	if err != nil {
		return User{}, err
	}
	var u User
	err = s.db.run(ctx, false, func(t tx) error {
		u, err = s.createUser(t, actor.UID, n)
		return err
	})
	return u, err
}

// User reads one account.
func (s *Store) User(ctx context.Context, uid string) (User, error) {
	if err := checkID("user", uid); err != nil {
		return User{}, ErrNotFound
	}
	var u User
	err := s.db.run(ctx, true, func(t tx) error {
		var err error
		u, err = readUser(t, uid)
		return err
	})
	return u, err
}

func readUser(t tx, uid string) (User, error) {
	var rec userRecord
	found, err := t.get(userPath(uid), &rec)
	if err != nil {
		return User{}, err
	}
	if !found {
		return User{}, ErrNotFound
	}
	return rec.user(uid), nil
}

// UserByUsername finds an account by its login name, in any letter case.
func (s *Store) UserByUsername(ctx context.Context, username string) (User, error) {
	name, err := normalizeUsername(username)
	if err != nil {
		return User{}, ErrNotFound
	}
	var u User
	err = s.db.run(ctx, true, func(t tx) error {
		uid, err := claimed(t, name)
		if err != nil {
			return err
		}
		if uid == "" {
			return ErrNotFound
		}
		u, err = readUser(t, uid)
		return err
	})
	return u, err
}

// Users lists every account, by display name.
func (s *Store) Users(ctx context.Context) ([]User, error) {
	var out []User
	err := s.db.run(ctx, true, func(t tx) error {
		var err error
		out, err = readUsers(t)
		return err
	})
	return out, err
}

func readUsers(t tx) ([]User, error) {
	var out []User
	err := t.query("users", nil, func(id string, decode func(any) error) error {
		var rec userRecord
		if err := decode(&rec); err != nil {
			return err
		}
		out = append(out, rec.user(id))
		return nil
	})
	slices.SortFunc(out, func(a, b User) int { return strings.Compare(a.DisplayName, b.DisplayName) })
	return out, err
}

// UpdateUser changes an account through change (admin, or the person
// themselves for their own password and login state). The username, the ID
// and removal change only through RenameUser and RemoveUser; a removed
// person never gets access back.
func (s *Store) UpdateUser(ctx context.Context, actor Actor, uid string, change func(*User) error) (User, error) {
	if !actor.Admin && actor.UID != uid {
		return User{}, ErrForbidden
	}
	var u User
	err := s.db.run(ctx, false, func(t tx) error {
		before, err := readUser(t, uid)
		if err != nil {
			return err
		}
		u = before
		if err := change(&u); err != nil {
			return err
		}
		if !actor.Admin && (u.Role != before.Role || u.Active != before.Active || u.SubmitsReports != before.SubmitsReports) {
			return ErrForbidden
		}
		if before.Removed && u.Active {
			return fmt.Errorf("%w: removed user", ErrInvalid)
		}
		u.ID, u.Username, u.Removed, u.CreatedAt, u.UpdatedAt = before.ID, before.Username, before.Removed, before.CreatedAt, s.stamp()
		rec := recordOf(u)
		if err := t.set(userPath(uid), rec); err != nil {
			return err
		}
		return s.audit(t, EntityUser, uid, ActionUpdate, actor.UID, recordOf(before).audited(), rec.audited())
	})
	return u, err
}

// RenameUser changes a person's login name (admin only) and frees the old
// one. Sessions stay: they name the account, not the login.
func (s *Store) RenameUser(ctx context.Context, actor Actor, uid, username string) (User, error) {
	if !actor.Admin {
		return User{}, ErrForbidden
	}
	name, err := normalizeUsername(username)
	if err != nil {
		return User{}, err
	}
	var u User
	err = s.db.run(ctx, false, func(t tx) error {
		before, err := readUser(t, uid)
		if err != nil {
			return err
		}
		if before.Removed {
			return fmt.Errorf("%w: removed user", ErrInvalid)
		}
		u = before
		if name == before.Username {
			return nil
		}
		holder, err := claimed(t, name)
		if err != nil {
			return err
		}
		if holder != "" {
			return ErrUsernameTaken
		}
		u.Username, u.UpdatedAt = name, s.stamp()
		rec := recordOf(u)
		if err := t.set(userPath(uid), rec); err != nil {
			return err
		}
		if err := t.set(usernamePath(before.Username), usernameRecord{}); err != nil {
			return err
		}
		if err := t.set(usernamePath(name), usernameRecord{UID: uid}); err != nil {
			return err
		}
		return s.audit(t, EntityUser, uid, ActionUpdate, actor.UID, recordOf(before).audited(), rec.audited())
	})
	return u, err
}

// RemoveUser takes a person off the staff (admin only, never the admin
// themselves): they are logged out everywhere, can't log in again, and their
// login name is free. Their reports, movements, terms and audit rows stay,
// so past months still add up. Nothing may accrue to them afterwards, so it
// needs: no reports in a month not yet closed and no pending movements
// (ErrOpenRecords), no salary or profit share in force from the first open
// month on — terms never end on their own (ErrTermsInForce) — and a zero
// balance with the salon (ErrBalanceOpen).
func (s *Store) RemoveUser(ctx context.Context, actor Actor, uid string) error {
	if !actor.Admin || actor.UID == uid {
		return ErrForbidden
	}
	if err := checkID("user", uid); err != nil {
		return ErrNotFound
	}
	return s.db.run(ctx, false, func(t tx) error {
		before, err := readUser(t, uid)
		if err != nil {
			return err
		}
		if before.Removed {
			return nil
		}
		if err := s.checkNothingAccrues(t, uid); err != nil {
			return err
		}
		balance, err := s.personBalance(t, uid)
		if err != nil {
			return err
		}
		if balance != 0 {
			return ErrBalanceOpen
		}
		u := before
		u.Removed, u.Active, u.UpdatedAt = true, false, s.stamp()
		u.SessionVersion++ // out on every device at once
		rec := recordOf(u)
		if err := t.set(userPath(uid), rec); err != nil {
			return err
		}
		if err := t.set(usernamePath(before.Username), usernameRecord{}); err != nil {
			return err
		}
		return s.audit(t, EntityUser, uid, ActionRemove, actor.UID, recordOf(before).audited(), rec.audited())
	})
}

// checkNothingAccrues refuses while a month close could still post money to
// the person: work or pending movements in open months, or terms that pay.
func (s *Store) checkNothingAccrues(t tx, uid string) error {
	months, err := readMonths(t)
	if err != nil {
		return err
	}
	firstOpen := s.start
	if newest := newestClosed(months); newest != nil {
		firstOpen = newest.Next()
	}
	reports, err := readReports(t, where("userId", "==", uid), where("month", ">=", firstOpen.String()))
	if err != nil {
		return err
	}
	if len(reports) > 0 {
		return ErrOpenRecords
	}
	pending, err := readLedger(t, where("personId", "==", uid), where("status", "==", string(domain.Pending)))
	if err != nil {
		return err
	}
	if len(pending) > 0 {
		return ErrOpenRecords
	}
	history, err := readTermsHistory(t, uid)
	if err != nil {
		return err
	}
	pays := func(x domain.Terms) bool { return x.SalaryCents != 0 || x.DividendWeight != 0 }
	if current, ok := history.For(firstOpen); ok && pays(current) {
		return ErrTermsInForce
	}
	for from, x := range history {
		if firstOpen.Before(from) && pays(x) {
			return ErrTermsInForce
		}
	}
	return nil
}

// RecordLoginFailure counts a wrong password. At limit failures the account
// locks until lockFor from now and the count starts again; only the lock is
// audited, not every attempt.
func (s *Store) RecordLoginFailure(ctx context.Context, uid string, limit int, lockFor time.Duration) (User, error) {
	if checkID("user", uid) != nil {
		return User{}, ErrNotFound
	}
	var u User
	err := s.db.run(ctx, false, func(t tx) error {
		before, err := readUser(t, uid)
		if err != nil {
			return err
		}
		u = before
		u.FailedLogins++
		locked := u.FailedLogins >= limit
		if locked {
			u.FailedLogins, u.LockedUntil = 0, s.stamp().Add(lockFor)
		}
		if err := t.set(userPath(uid), recordOf(u)); err != nil {
			return err
		}
		if !locked {
			return nil
		}
		return s.audit(t, EntityUser, uid, ActionLock, "login", recordOf(before).audited(), recordOf(u).audited())
	})
	return u, err
}

// RecordLoginSuccess clears failed attempts and any lock. It writes only
// when there is something to clear.
func (s *Store) RecordLoginSuccess(ctx context.Context, uid string) (User, error) {
	if checkID("user", uid) != nil {
		return User{}, ErrNotFound
	}
	var u User
	err := s.db.run(ctx, false, func(t tx) error {
		var err error
		if u, err = readUser(t, uid); err != nil {
			return err
		}
		if u.FailedLogins == 0 && u.LockedUntil.IsZero() {
			return nil
		}
		u.FailedLogins, u.LockedUntil = 0, time.Time{}
		return t.set(userPath(uid), recordOf(u))
	})
	return u, err
}

// TermsEntry is a person's money terms from a month on, as set.
type TermsEntry struct {
	From  domain.Month
	Terms domain.Terms
	SetBy string
	SetAt time.Time
	Note  string
}

type termsRecord struct {
	SelfPct        domain.BasisPoints `firestore:"selfPct"        json:"selfPct"`
	SalaryCents    domain.Cents       `firestore:"salaryCents"    json:"salaryCents"`
	DividendWeight int64              `firestore:"dividendWeight" json:"dividendWeight"`
	// The whole month's «Себе» rate when its base is above the threshold (0 = none).
	HighSelfFromCents domain.Cents       `firestore:"highSelfFromCents" json:"highSelfFromCents"`
	HighSelfPct       domain.BasisPoints `firestore:"highSelfPct"       json:"highSelfPct"`
	SetBy             string             `firestore:"setBy"          json:"setBy"`
	SetAt             time.Time          `firestore:"setAt"          json:"setAt"`
	Note              string             `firestore:"note"           json:"note"`
}

func (r termsRecord) terms() domain.Terms {
	return domain.Terms{
		SelfPct: r.SelfPct, SalaryCents: r.SalaryCents, DividendWeight: r.DividendWeight,
		HighSelfFromCents: r.HighSelfFromCents, HighSelfPct: r.HighSelfPct,
	}
}

// SetTerms starts new money terms for a person from month m (admin only).
// m must be open and not before the start month, and the person must have
// no reports in any month the new terms would cover (m up to their next
// terms), so a month never mixes two rates.
func (s *Store) SetTerms(ctx context.Context, actor Actor, uid string, m domain.Month, terms domain.Terms, note string) error {
	if !actor.Admin {
		return ErrForbidden
	}
	if err := checkID("user", uid); err != nil {
		return err
	}
	if err := domain.ValidateTerms(terms); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	if m.Before(s.start) {
		return &domain.ValidationError{Field: "month", Problem: domain.ProblemBeforeStartMonth}
	}
	return s.db.run(ctx, false, func(t tx) error {
		if _, err := readUser(t, uid); err != nil {
			return err
		}
		status, err := readMonthStatus(t, m)
		if err != nil {
			return err
		}
		history, err := readTermsHistory(t, uid)
		if err != nil {
			return err
		}
		var next *domain.Month // the person's next terms after m end these
		for from := range history {
			if m.Before(from) && (next == nil || from.Before(*next)) {
				next = &from
			}
		}
		hasReports := false
		err = t.query("reports", []cond{where("userId", "==", uid), where("month", ">=", m.String())}, func(_ string, decode func(any) error) error {
			var rec reportRecord
			if err := decode(&rec); err != nil {
				return err
			}
			if !rec.Voided && (next == nil || rec.Month < next.String()) {
				hasReports = true
			}
			return nil
		})
		if err != nil {
			return err
		}
		if err := domain.CheckCanStartTerms(status, hasReports); err != nil {
			if status == domain.MonthClosed {
				return ErrMonthClosed
			}
			return err
		}
		var before termsRecord
		existed, err := t.get(termsPath(uid, m), &before)
		if err != nil {
			return err
		}
		rec := termsRecord{
			SelfPct: terms.SelfPct, SalaryCents: terms.SalaryCents, DividendWeight: terms.DividendWeight,
			HighSelfFromCents: terms.HighSelfFromCents, HighSelfPct: terms.HighSelfPct,
			SetBy: actor.UID, SetAt: s.stamp(), Note: strings.TrimSpace(note),
		}
		if err := t.set(termsPath(uid, m), rec); err != nil {
			return err
		}
		var prev any
		if existed {
			prev = before
		}
		return s.audit(t, EntityTerms, uid+"/"+m.String(), ActionUpdate, actor.UID, prev, rec)
	})
}

// Terms is a person's terms history, every entry by its start month.
func (s *Store) Terms(ctx context.Context, uid string) ([]TermsEntry, error) {
	if err := checkID("user", uid); err != nil {
		return nil, ErrNotFound
	}
	var out []TermsEntry
	err := s.db.run(ctx, true, func(t tx) error {
		out = out[:0]
		return t.query(termsCollection(uid), nil, func(id string, decode func(any) error) error {
			m, err := domain.ParseMonth(id)
			if err != nil {
				return fmt.Errorf("terms %s/%s: %w", uid, id, err)
			}
			var rec termsRecord
			if err := decode(&rec); err != nil {
				return err
			}
			out = append(out, TermsEntry{From: m, Terms: rec.terms(), SetBy: rec.SetBy, SetAt: rec.SetAt.UTC(), Note: rec.Note})
			return nil
		})
	})
	slices.SortFunc(out, func(a, b TermsEntry) int { return strings.Compare(a.From.String(), b.From.String()) })
	return out, err
}

// readTermsHistory reads one person's terms as a domain history.
func readTermsHistory(t tx, uid string) (domain.TermsHistory, error) {
	h := domain.TermsHistory{}
	err := t.query(termsCollection(uid), nil, func(id string, decode func(any) error) error {
		m, err := domain.ParseMonth(id)
		if err != nil {
			return fmt.Errorf("terms %s/%s: %w", uid, id, err)
		}
		var rec termsRecord
		if err := decode(&rec); err != nil {
			return err
		}
		h[m] = rec.terms()
		return nil
	})
	return h, err
}
