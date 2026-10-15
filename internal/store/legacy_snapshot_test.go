package store_test

import (
	"crypto/rand"
	"os"
	"strings"
	"testing"

	"cloud.google.com/go/firestore"
	"github.com/lieranderl/aestheticlab-income-app/internal/store"
)

func TestPreviousSnapshotKeepsMoneyAndMarksNewStatisticsUnavailable(t *testing.T) {
	if os.Getenv("FIRESTORE_EMULATOR_HOST") == "" {
		t.Skip("Firestore emulator old-schema fixture")
	}
	project := "demo-client-old-" + strings.ToLower(rand.Text()[:8])
	raw, err := firestore.NewClientWithDatabase(ctx, project, "aestheticlab-income")
	must(t, err)
	t.Cleanup(func() { _ = raw.Close() })
	_, err = raw.Doc("months/2026-11").Set(ctx, map[string]any{"status": "closed", "snapshot": map[string]any{"result": map[string]any{"revenue": 12300, "salonIncome": 4000, "people": map[string]any{"old_user": map[string]any{"revenue": 12300, "closing": 5000}}}, "balances": map[string]any{"persons": map[string]any{"old_user": 5000}}}})
	must(t, err)
	s, err := store.NewFirestore(ctx, project, "aestheticlab-income")
	must(t, err)
	t.Cleanup(func() { _ = s.Close() })
	old, err := s.Month(ctx, month(t, "2026-11"))
	must(t, err)
	got := old.Snapshot.Result
	p := got.People["old_user"]
	if got.Revenue != 12300 || got.SalonIncome != 4000 || p.Closing != 5000 || got.StatsVersion != 0 || p.StatsVersion != 0 {
		t.Fatalf("old result changed %+v", got)
	}
}
