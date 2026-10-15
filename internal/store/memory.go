package store

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"strings"
	"sync"
)

// memoryDB keeps documents as JSON, so values never alias between callers,
// and serialises transactions with one lock.
type memoryDB struct {
	mu   sync.Mutex
	docs map[string][]byte // full path → JSON
}

// NewMemory is a Store held in memory: for local runs without Firestore and
// for HTTP tests. Everything is lost on restart.
func NewMemory(opts ...Option) *Store {
	return newStore(&memoryDB{docs: map[string][]byte{}}, opts)
}

func (m *memoryDB) close() error { return nil }

func (m *memoryDB) run(_ context.Context, readOnly bool, fn func(tx) error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	t := &memoryTx{db: m, readOnly: readOnly, writes: map[string][]byte{}}
	if err := fn(t); err != nil {
		return err
	}
	maps.Copy(m.docs, t.writes)
	return nil
}

type memoryTx struct {
	db       *memoryDB
	readOnly bool
	writes   map[string][]byte
}

func (t *memoryTx) get(path string, dst any) (bool, error) {
	if len(t.writes) > 0 {
		return false, errReadAfterWrite
	}
	data, ok := t.db.docs[path]
	if !ok {
		return false, nil
	}
	return true, json.Unmarshal(data, dst)
}

func (t *memoryTx) query(collection string, conds []cond, each func(string, func(any) error) error) error {
	if len(t.writes) > 0 {
		return errReadAfterWrite
	}
	prefix := collection + "/"
	for path, data := range t.db.docs {
		id, ok := strings.CutPrefix(path, prefix)
		if !ok || strings.Contains(id, "/") {
			continue
		}
		var fields map[string]any
		if err := json.Unmarshal(data, &fields); err != nil {
			return err
		}
		if !matchAll(fields, conds) {
			continue
		}
		if err := each(id, func(dst any) error { return json.Unmarshal(data, dst) }); err != nil {
			return err
		}
	}
	return nil
}

func matchAll(fields map[string]any, conds []cond) bool {
	for _, c := range conds {
		v, ok := fields[c.field].(string)
		if !ok || !c.match(v) {
			return false
		}
	}
	return true
}

func (t *memoryTx) set(path string, v any) error {
	if t.readOnly {
		return fmt.Errorf("store: write to %s in a read-only transaction", path)
	}
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	t.writes[path] = data
	return nil
}
