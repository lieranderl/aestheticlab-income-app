package store

import (
	"context"
	"errors"
	"strings"
	"time"
)

// The rules live once, in Store. A backend only holds documents and runs
// transactions; Firestore in production, a map in memory for local runs and
// HTTP tests. So the two can never disagree about a rule.

// backend runs fn as one transaction: all of its writes apply together, or
// none do when fn fails. readOnly transactions may not write.
type backend interface {
	run(ctx context.Context, readOnly bool, fn func(tx) error) error
	close() error
}

// tx is what a transaction may do. As in Firestore, every read comes before
// the first write; the memory backend enforces it, so a rule that breaks it
// fails in the fast tests too.
type tx interface {
	// get decodes the document into dst; found is false if it doesn't exist.
	get(path string, dst any) (found bool, err error)
	// query calls each for every document in collection that matches all
	// conds, in no particular order.
	query(collection string, conds []cond, each func(id string, decode func(dst any) error) error) error
	// set creates or replaces a document.
	set(path string, v any) error
}

// cond is a filter on a string field: "==", ">=" or "<=".
type cond struct {
	field string
	op    string
	value any // string or time.Time (Firestore Timestamp)
}

func where(field, op string, value any) cond { return cond{field, op, value} }

// errReadAfterWrite is a programming error: Firestore refuses reads after
// writes in a transaction.
var errReadAfterWrite = errors.New("store: read after write in a transaction")

func (c cond) match(v string) bool {
	var order int
	switch value := c.value.(type) {
	case string:
		order = strings.Compare(v, value)
	case time.Time:
		at, err := time.Parse(time.RFC3339Nano, v)
		if err != nil {
			return false
		}
		order = at.Compare(value)
	default:
		return false
	}
	switch c.op {
	case "==":
		return order == 0
	case ">=":
		return order >= 0
	case "<=":
		return order <= 0
	}
	return false
}
