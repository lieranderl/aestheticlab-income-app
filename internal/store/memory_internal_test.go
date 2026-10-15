package store

import (
	"context"
	"errors"
	"testing"
)

// The memory backend refuses what Firestore refuses, so a rule that reads
// after writing fails in the fast tests too.
func TestMemoryTransactionsFollowFirestoreRules(t *testing.T) {
	db := &memoryDB{docs: map[string][]byte{}}
	ctx := context.Background()
	err := db.run(ctx, false, func(t tx) error {
		if err := t.set("months/2026-11", monthRecord{Status: "closed"}); err != nil {
			return err
		}
		_, err := t.get("months/2026-11", &monthRecord{})
		return err
	})
	if !errors.Is(err, errReadAfterWrite) {
		t.Fatalf("get after set: %v", err)
	}
	err = db.run(ctx, false, func(t tx) error {
		if err := t.set("months/2026-11", monthRecord{}); err != nil {
			return err
		}
		return t.query("months", nil, func(string, func(any) error) error { return nil })
	})
	if !errors.Is(err, errReadAfterWrite) {
		t.Fatalf("query after set: %v", err)
	}
	if err := db.run(ctx, true, func(t tx) error { return t.set("months/2026-11", monthRecord{}) }); err == nil {
		t.Fatal("write in a read-only transaction accepted")
	}
	if len(db.docs) != 0 {
		t.Fatalf("failed transactions wrote %d docs", len(db.docs))
	}
}
