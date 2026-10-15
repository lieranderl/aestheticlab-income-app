package store

import (
	"cmp"
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/lieranderl/aestheticlab-income-app/internal/domain"
)

// CategoryKind says which list a category is in.
type CategoryKind string

var categoryIDPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,40}$`)

func validCategoryID(id string) bool { return categoryIDPattern.MatchString(id) }

// Category lists.
const (
	KindRevenue CategoryKind = "revenue" // «Услуги», «Кремы»…
	KindPayment CategoryKind = "payment" // «Карта», «Наличные», «Сертификат»
	KindExpense CategoryKind = "expense" // «Аренда», «Материалы»…
)

// Category is a revenue category, payment method or expense category.
// The ID never changes, so a rename shows in history too.
type Category struct {
	ID                string
	Version           int64
	Deleted           bool
	Name              string
	Kind              CategoryKind
	Order             int
	Active            bool // archived categories stay in history but leave the form
	UsedCount         int64
	CountsTowardSelf  bool               // revenue: part of the «Себе» base
	IsCertificateSale bool               // revenue: money held for a future service
	Destination       domain.Destination // payment: where the money lands
	CreatedAt         time.Time
}

type categoryRecord struct {
	Version           int64              `firestore:"version"               json:"version"`
	Deleted           bool               `firestore:"deleted"               json:"deleted"`
	Name              string             `firestore:"name"                  json:"name"`
	Kind              string             `firestore:"kind"                  json:"kind"`
	Order             int                `firestore:"order"                 json:"order"`
	Active            bool               `firestore:"active"                json:"active"`
	UsedCount         int64              `firestore:"usedCount"             json:"usedCount"`
	CountsTowardSelf  bool               `firestore:"countsTowardSelf"      json:"countsTowardSelf"`
	IsCertificateSale bool               `firestore:"isCertificateSale"     json:"isCertificateSale"`
	Destination       domain.Destination `firestore:"destination,omitempty" json:"destination,omitempty"`
	CreatedAt         time.Time          `firestore:"createdAt"             json:"createdAt"`
}

func (r categoryRecord) category(id string) Category {
	return Category{
		ID: id, Version: r.Version, Deleted: r.Deleted, Name: r.Name, Kind: CategoryKind(r.Kind), Order: r.Order, Active: r.Active,
		UsedCount: r.UsedCount, CountsTowardSelf: r.CountsTowardSelf, IsCertificateSale: r.IsCertificateSale,
		Destination: r.Destination, CreatedAt: r.CreatedAt.UTC(),
	}
}

func (c Category) record() categoryRecord {
	return categoryRecord{
		Version: c.Version, Deleted: c.Deleted, Name: c.Name, Kind: string(c.Kind), Order: c.Order, Active: c.Active, UsedCount: c.UsedCount,
		CountsTowardSelf: c.CountsTowardSelf, IsCertificateSale: c.IsCertificateSale,
		Destination: c.Destination, CreatedAt: c.CreatedAt.UTC(),
	}
}

func (c Category) check() error {
	if !validCategoryID(c.ID) {
		return ErrInvalid
	}
	if strings.TrimSpace(c.Name) == "" || len([]rune(c.Name)) > 120 {
		return fmt.Errorf("%w: category %s has no name", ErrInvalid, c.ID)
	}
	switch c.Kind {
	case KindRevenue:
		if c.Destination != "" || (c.IsCertificateSale && c.CountsTowardSelf) {
			return fmt.Errorf("%w: revenue category %s has invalid flags", ErrInvalid, c.ID)
		}
	case KindPayment:
		if c.CountsTowardSelf || c.IsCertificateSale {
			return ErrInvalid
		}
		switch c.Destination {
		case domain.ToKassaCard, domain.ToCash, domain.Prepaid:
		default:
			return fmt.Errorf("%w: payment method %s needs a destination", ErrInvalid, c.ID)
		}
	case KindExpense:
		if c.Destination != "" || c.CountsTowardSelf || c.IsCertificateSale {
			return fmt.Errorf("%w: expense category %s has revenue or payment flags", ErrInvalid, c.ID)
		}
	default:
		return fmt.Errorf("%w: category %s has kind %q", ErrInvalid, c.ID, c.Kind)
	}
	return nil
}

func (s *Store) createCategory(t tx, actorID string, c Category) (Category, error) {
	c.Active, c.UsedCount, c.CreatedAt = true, 0, s.stamp()
	c.Version, c.Deleted = 1, false
	rec := c.record()
	if err := t.set(categoryPath(c.ID), rec); err != nil {
		return Category{}, err
	}
	return c, s.audit(t, EntityCategory, c.ID, ActionCreate, actorID, nil, rec)
}

// writeCategory saves a changed category and its audit row.
func (s *Store) writeCategory(t tx, actorID string, before, after Category) error {
	after.Version = before.Version + 1
	rec := after.record()
	if err := t.set(categoryPath(after.ID), rec); err != nil {
		return err
	}
	return s.audit(t, EntityCategory, after.ID, ActionUpdate, actorID, before.record(), rec)
}

// CreateCategory adds a category with a fixed ID (admin only).
func (s *Store) CreateCategory(ctx context.Context, actor Actor, c Category) (Category, error) {
	if !actor.Admin {
		return Category{}, ErrForbidden
	}
	if err := c.check(); err != nil {
		return Category{}, err
	}
	var out Category
	err := s.db.run(ctx, false, func(t tx) error {
		found, err := t.get(categoryPath(c.ID), &categoryRecord{})
		if err != nil {
			return err
		}
		if found {
			return fmt.Errorf("%w: category %s exists", ErrInvalid, c.ID)
		}
		cats, err := readCategories(t)
		if err != nil {
			return err
		}
		c.Order = 1
		for _, existing := range cats {
			if existing.Kind == c.Kind && existing.Order >= c.Order {
				c.Order = existing.Order + 1
			}
		}
		out, err = s.createCategory(t, actor.UID, c)
		return err
	})
	return out, err
}

// Categories lists every category, archived ones included, by kind and order.
func (s *Store) Categories(ctx context.Context) ([]Category, error) {
	var out []Category
	err := s.db.run(ctx, true, func(t tx) error {
		var err error
		out, err = readCategories(t)
		return err
	})
	return out, err
}

func readCategories(t tx) ([]Category, error) {
	var out []Category
	err := t.query("categories", nil, func(id string, decode func(any) error) error {
		var rec categoryRecord
		if err := decode(&rec); err != nil {
			return err
		}
		if !rec.Deleted {
			out = append(out, rec.category(id))
		}
		return nil
	})
	slices.SortFunc(out, func(a, b Category) int {
		return cmp.Or(cmp.Compare(a.Kind, b.Kind), cmp.Compare(a.Order, b.Order), cmp.Compare(a.ID, b.ID))
	})
	return out, err
}

// CatalogOf is the domain view of the categories, archived ones included:
// a report may still hold an archived category.
func CatalogOf(cats []Category) domain.Catalog {
	c := domain.Catalog{Revenue: map[string]domain.RevenueCategory{}, Payments: map[string]domain.PaymentMethod{}}
	for _, cat := range cats {
		switch cat.Kind {
		case KindRevenue:
			c.Revenue[cat.ID] = domain.RevenueCategory{CountsTowardSelf: cat.CountsTowardSelf, IsCertificateSale: cat.IsCertificateSale}
		case KindPayment:
			c.Payments[cat.ID] = domain.PaymentMethod{Destination: cat.Destination}
		}
	}
	return c
}

// AccountKind is what a kassa account holds.
type AccountKind string

// Account kinds.
const (
	AccountCash AccountKind = "cash"
	AccountCard AccountKind = "card"
)

// Account is a kassa account: «Касса — наличные», «Касса — карта салона».
type Account struct {
	ID            string
	Name          string
	Kind          AccountKind
	Order         int
	Active        bool
	LastRecountAt time.Time // «Пересчёт кассы»; zero if never
	LastRecountBy string
}

type accountRecord struct {
	Name          string    `firestore:"name"          json:"name"`
	Kind          string    `firestore:"kind"          json:"kind"`
	Order         int       `firestore:"order"         json:"order"`
	Active        bool      `firestore:"active"        json:"active"`
	LastRecountAt time.Time `firestore:"lastRecountAt" json:"lastRecountAt"`
	LastRecountBy string    `firestore:"lastRecountBy" json:"lastRecountBy"`
}

func (r accountRecord) account(id string) Account {
	return Account{
		ID: id, Name: r.Name, Kind: AccountKind(r.Kind), Order: r.Order, Active: r.Active,
		LastRecountAt: r.LastRecountAt.UTC(), LastRecountBy: r.LastRecountBy,
	}
}

func (a Account) record() accountRecord {
	return accountRecord{
		Name: a.Name, Kind: string(a.Kind), Order: a.Order, Active: a.Active,
		LastRecountAt: a.LastRecountAt.UTC(), LastRecountBy: a.LastRecountBy,
	}
}

func (a Account) check() error {
	if err := checkID("account", a.ID); err != nil {
		return err
	}
	if strings.TrimSpace(a.Name) == "" || (a.Kind != AccountCash && a.Kind != AccountCard) {
		return fmt.Errorf("%w: account %s needs a name and a kind", ErrInvalid, a.ID)
	}
	return nil
}

func (s *Store) createAccount(t tx, actorID string, a Account) (Account, error) {
	a.Active, a.LastRecountAt, a.LastRecountBy = true, time.Time{}, ""
	rec := a.record()
	if err := t.set(accountPath(a.ID), rec); err != nil {
		return Account{}, err
	}
	return a, s.audit(t, EntityAccount, a.ID, ActionCreate, actorID, nil, rec)
}

// Accounts lists the kassa accounts in order.
func (s *Store) Accounts(ctx context.Context) ([]Account, error) {
	var out []Account
	err := s.db.run(ctx, true, func(t tx) error {
		var err error
		out, err = readAccounts(t)
		return err
	})
	return out, err
}

func readAccounts(t tx) ([]Account, error) {
	var out []Account
	err := t.query("accounts", nil, func(id string, decode func(any) error) error {
		var rec accountRecord
		if err := decode(&rec); err != nil {
			return err
		}
		out = append(out, rec.account(id))
		return nil
	})
	slices.SortFunc(out, func(a, b Account) int { return cmp.Or(cmp.Compare(a.Order, b.Order), cmp.Compare(a.ID, b.ID)) })
	return out, err
}

// Setup is the first data: Anna's admin account, the default categories
// and the kassa accounts (technical-solution §9).
type Setup struct {
	Admin      NewUser
	Categories []Category
	Accounts   []Account
}

// SetUp writes the first data in one transaction. It refuses to run once
// any user exists (ErrAlreadySetUp), so cmd/seed can't run twice.
func (s *Store) SetUp(ctx context.Context, setup Setup) (User, error) {
	admin, err := setup.Admin.check()
	if err != nil {
		return User{}, err
	}
	if admin.Role != RoleAdmin {
		return User{}, fmt.Errorf("%w: the first user must be the admin", ErrInvalid)
	}
	seen := map[string]bool{}
	for _, c := range setup.Categories {
		if err := c.check(); err != nil {
			return User{}, err
		}
		if seen["c:"+c.ID] {
			return User{}, fmt.Errorf("%w: category %s twice", ErrInvalid, c.ID)
		}
		seen["c:"+c.ID] = true
	}
	for _, a := range setup.Accounts {
		if err := a.check(); err != nil {
			return User{}, err
		}
		if seen["a:"+a.ID] {
			return User{}, fmt.Errorf("%w: account %s twice", ErrInvalid, a.ID)
		}
		seen["a:"+a.ID] = true
	}
	var u User
	err = s.db.run(ctx, false, func(t tx) error {
		users, err := readUsers(t)
		if err != nil {
			return err
		}
		if len(users) > 0 {
			return ErrAlreadySetUp
		}
		if u, err = s.createUser(t, "setup", admin); err != nil {
			return err
		}
		for _, c := range setup.Categories {
			if _, err := s.createCategory(t, u.ID, c); err != nil {
				return err
			}
		}
		for _, a := range setup.Accounts {
			if _, err := s.createAccount(t, u.ID, a); err != nil {
				return err
			}
		}
		return nil
	})
	return u, err
}
