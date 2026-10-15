package store

import (
	"context"
	"strings"

	"github.com/lieranderl/aestheticlab-income-app/internal/domain"
)

// CategoryByID reads a category, including archived categories.
func (s *Store) CategoryByID(ctx context.Context, id string) (Category, error) {
	var c Category
	err := s.db.run(ctx, true, func(t tx) error { var err error; c, err = readCategory(t, id); return err })
	return c, err
}
func readCategory(t tx, id string) (Category, error) {
	if !validCategoryID(id) {
		return Category{}, ErrInvalid
	}
	var r categoryRecord
	found, err := t.get(categoryPath(id), &r)
	if err != nil {
		return Category{}, err
	}
	if !found || r.Deleted {
		return Category{}, ErrNotFound
	}
	return r.category(id), nil
}

// UpdateCategory changes future settings without changing kind, order or usage.
func (s *Store) UpdateCategory(ctx context.Context, actor Actor, c Category) error {
	if !actor.Admin {
		return ErrForbidden
	}
	if err := c.check(); err != nil {
		return err
	}
	return s.db.run(ctx, false, func(t tx) error {
		before, err := readCategory(t, c.ID)
		if err != nil {
			return err
		}
		if before.Version != c.Version {
			return ErrVersionConflict
		}
		if before.Kind != c.Kind {
			return ErrInvalid
		}
		after := before
		after.Name = strings.TrimSpace(c.Name)
		after.CountsTowardSelf = c.CountsTowardSelf
		after.IsCertificateSale = c.IsCertificateSale
		after.Destination = c.Destination
		return s.writeCategory(t, actor.UID, before, after)
	})
}

// IsFixedCategory reports whether the category is a system-fixed core category
// that cannot be archived or deleted.
func IsFixedCategory(id string) bool {
	switch id {
	case "services", "creams", "oil", "certificates", "card", "cash", "paid_by_certificate":
		return true
	default:
		return false
	}
}

// SetCategoryActive archives or reactivates the category.
func (s *Store) SetCategoryActive(ctx context.Context, actor Actor, id string, version int64, active bool) error {
	if !active && IsFixedCategory(id) {
		return &domain.ValidationError{Field: "category", Problem: "fixed_category"}
	}
	return s.mutateCategory(ctx, actor, id, version, func(c *Category) error { c.Active = active; return nil })
}

// DeleteCategory reserves an unused category's ID permanently.
func (s *Store) DeleteCategory(ctx context.Context, actor Actor, id string, version int64) error {
	if IsFixedCategory(id) {
		return &domain.ValidationError{Field: "category", Problem: "fixed_category"}
	}
	return s.mutateCategory(ctx, actor, id, version, func(c *Category) error {
		if c.UsedCount != 0 {
			return ErrInvalid
		}
		c.Deleted = true
		c.Active = false
		return nil
	})
}
func (s *Store) mutateCategory(ctx context.Context, actor Actor, id string, version int64, change func(*Category) error) error {
	if !actor.Admin {
		return ErrForbidden
	}
	return s.db.run(ctx, false, func(t tx) error {
		before, err := readCategory(t, id)
		if err != nil {
			return err
		}
		if before.Version != version {
			return ErrVersionConflict
		}
		after := before
		if err := change(&after); err != nil {
			return err
		}
		return s.writeCategory(t, actor.UID, before, after)
	})
}

// ReorderCategory swaps adjacent same-kind categories, checking both revisions.
func (s *Store) ReorderCategory(ctx context.Context, actor Actor, id string, version int64, neighborID string, neighborVersion int64) error {
	if !actor.Admin {
		return ErrForbidden
	}
	if !validCategoryID(id) || !validCategoryID(neighborID) {
		return ErrInvalid
	}
	return s.db.run(ctx, false, func(t tx) error {
		cats, err := readCategories(t)
		if err != nil {
			return err
		}
		i, j := -1, -1
		for k, c := range cats {
			if c.ID == id {
				i = k
			}
			if c.ID == neighborID {
				j = k
			}
		}
		if i < 0 || j < 0 {
			return ErrNotFound
		}
		a, b := cats[i], cats[j]
		if a.Version != version || b.Version != neighborVersion {
			return ErrVersionConflict
		}
		if a.Kind != b.Kind || (i-j != 1 && j-i != 1) {
			return ErrInvalid
		}
		// Normalize all same-kind positions when seed orders tie, preserving visible order.
		if a.Order == b.Order {
			return ErrInvalid
		}
		aa, bb := a, b
		aa.Order, bb.Order = b.Order, a.Order
		if err := s.writeCategory(t, actor.UID, a, aa); err != nil {
			return err
		}
		return s.writeCategory(t, actor.UID, b, bb)
	})
}
