// Command restorecheck is the restore drill's read-only comparison: it walks
// every collection and subcollection of a Firestore database, counts
// documents per collection pattern (users/{id}/terms)
// and prints one SHA-256 fingerprint over every document path and its fields.
// Run it on the live database at the snapshot time and on the restored copy;
// the outputs must be identical. It never writes.
//
//	go run ./cmd/restorecheck -project <project> aestheticlab-income 2026-10-14T18:21:00Z
//	go run ./cmd/restorecheck -project <project> aestheticlab-income-restore-test
//
// It uses the low-level API so that listing collections and documents, not
// only reading them, happens at the requested time, and it fingerprints the
// deterministic protobuf encoding of the fields, which keeps Firestore types
// apart (an integer 100 and a double 100 differ).
package main

import (
	"context"
	"crypto/sha256"
	"errors"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	vkit "cloud.google.com/go/firestore/apiv1"
	pb "cloud.google.com/go/firestore/apiv1/firestorepb"
	"google.golang.org/api/iterator"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var project string

func main() {
	flag.StringVar(&project, "project", os.Getenv("GOOGLE_CLOUD_PROJECT"), "the Google Cloud project ID")
	flag.Parse()
	args := flag.Args()
	if project == "" || len(args) < 1 || len(args) > 2 {
		fmt.Fprintln(os.Stderr, "usage: restorecheck -project <project> <database> [RFC3339 read time]")
		os.Exit(2)
	}
	if err := run(context.Background(), args[0], args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "restorecheck:", err)
		os.Exit(1)
	}
}

type walker struct {
	c        *vkit.Client
	root     string // projects/…/databases/…/documents
	readTime *timestamppb.Timestamp
	counts   map[string]int
	docs     map[string][]byte
}

func run(ctx context.Context, database string, at []string) error {
	c, err := vkit.NewClient(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()
	w := &walker{c: c, root: "projects/" + project + "/databases/" + database + "/documents", counts: map[string]int{}, docs: map[string][]byte{}}
	if len(at) == 1 {
		t, err := time.Parse(time.RFC3339, at[0])
		if err != nil {
			return err
		}
		w.readTime = timestamppb.New(t)
	}
	if err := w.walk(ctx, w.root); err != nil {
		return err
	}

	paths := make([]string, 0, len(w.docs))
	for p := range w.docs {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	h := sha256.New()
	for _, p := range paths {
		_, _ = fmt.Fprintf(h, "%d:%s%d:", len(p), p, len(w.docs[p])) // hash.Hash writes never fail
		h.Write(w.docs[p])
	}
	keys := make([]string, 0, len(w.counts))
	for k := range w.counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	total := 0
	for _, k := range keys {
		fmt.Printf("%-28s %d\n", k, w.counts[k])
		total += w.counts[k]
	}
	fmt.Printf("%-28s %d\nfingerprint %x\n", "TOTAL", total, h.Sum(nil))
	return nil
}

// walk visits every collection under parent (the database root or a
// document), then every document in it, existing or missing, since a
// missing document can still hold subcollections.
func (w *walker) walk(ctx context.Context, parent string) error {
	req := &pb.ListCollectionIdsRequest{Parent: parent}
	if w.readTime != nil {
		req.ConsistencySelector = &pb.ListCollectionIdsRequest_ReadTime{ReadTime: w.readTime}
	}
	ids := w.c.ListCollectionIds(ctx, req)
	for {
		id, err := ids.Next()
		if errors.Is(err, iterator.Done) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := w.collection(ctx, parent, id); err != nil {
			return err
		}
	}
}

func (w *walker) collection(ctx context.Context, parent, id string) error {
	req := &pb.ListDocumentsRequest{Parent: parent, CollectionId: id, ShowMissing: true}
	if w.readTime != nil {
		req.ConsistencySelector = &pb.ListDocumentsRequest_ReadTime{ReadTime: w.readTime}
	}
	docs := w.c.ListDocuments(ctx, req)
	for {
		d, err := docs.Next()
		if errors.Is(err, iterator.Done) {
			return nil
		}
		if err != nil {
			return err
		}
		rel := strings.TrimPrefix(d.GetName(), w.root+"/")
		if d.GetCreateTime() != nil { // missing documents have none
			fields, err := proto.MarshalOptions{Deterministic: true}.Marshal(&pb.MapValue{Fields: d.GetFields()})
			if err != nil {
				return err
			}
			w.counts[pattern(rel)]++
			w.docs[rel] = fields
		}
		if err := w.walk(ctx, d.GetName()); err != nil {
			return err
		}
	}
}

// pattern is a document's collection with IDs replaced: users/abc/terms/x →
// users/{id}/terms.
func pattern(doc string) string {
	parts := strings.Split(doc, "/")
	parts = parts[:len(parts)-1]
	for i := 1; i < len(parts); i += 2 {
		parts[i] = "{id}"
	}
	return strings.Join(parts, "/")
}
