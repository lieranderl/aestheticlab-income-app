package store

import (
	"context"
	"errors"
	"fmt"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// firestoreDB is the production backend: the named database
// aestheticlab-income, never (default), which belongs to the Telegram bot.
type firestoreDB struct {
	client *firestore.Client
}

// NewFirestore opens database in the Google Cloud project. On Cloud Run it
// signs in as the service's account; with FIRESTORE_EMULATOR_HOST set it
// talks to that emulator instead.
func NewFirestore(ctx context.Context, project, database string, opts ...Option) (*Store, error) {
	if database == "" || database == "(default)" {
		return nil, fmt.Errorf("store: refusing database %q; the Kassa uses its own named database", database)
	}
	client, err := firestore.NewClientWithDatabase(ctx, project, database)
	if err != nil {
		return nil, fmt.Errorf("open firestore %s/%s: %w", project, database, err)
	}
	return newStore(&firestoreDB{client: client}, opts), nil
}

func (f *firestoreDB) close() error { return f.client.Close() }

func (f *firestoreDB) run(ctx context.Context, readOnly bool, fn func(tx) error) error {
	var opts []firestore.TransactionOption
	if readOnly {
		opts = append(opts, firestore.ReadOnly)
	}
	return f.client.RunTransaction(ctx, func(ctx context.Context, t *firestore.Transaction) error {
		return fn(&firestoreTx{ctx: ctx, client: f.client, t: t})
	}, opts...)
}

type firestoreTx struct {
	ctx    context.Context //nolint:containedctx // scoped to one transaction callback
	client *firestore.Client
	t      *firestore.Transaction
}

func (t *firestoreTx) get(path string, dst any) (bool, error) {
	snap, err := t.t.Get(t.client.Doc(path))
	if notFound(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read %s: %w", path, err)
	}
	if err := snap.DataTo(dst); err != nil {
		return false, fmt.Errorf("decode %s: %w", path, err)
	}
	return true, nil
}

func (t *firestoreTx) query(collection string, conds []cond, each func(string, func(any) error) error) error {
	q := t.client.Collection(collection).Query
	for _, c := range conds {
		q = q.Where(c.field, c.op, c.value)
	}
	it := t.t.Documents(q)
	defer it.Stop()
	for {
		snap, err := it.Next()
		if errors.Is(err, iterator.Done) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("query %s: %w", collection, err)
		}
		if err := each(snap.Ref.ID, snap.DataTo); err != nil {
			return err
		}
	}
}

func (t *firestoreTx) set(path string, v any) error {
	if err := t.t.Set(t.client.Doc(path), v); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func notFound(err error) bool {
	var se interface{ GRPCStatus() *status.Status }
	return errors.As(err, &se) && se.GRPCStatus().Code() == codes.NotFound
}
