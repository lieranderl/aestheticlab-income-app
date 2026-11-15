package web

import (
	"slices"
	"testing"

	"github.com/lieranderl/aestheticlab-income-app/internal/store"
)

// 2026-11-15 production review: an admin who doesn't report saw «Мои
// записи» (an empty page) in «Ещё», and «История» twice (bar and «Ещё»).
func TestMoreLinksFitTheAdmin(t *testing.T) {
	hrefs := func(u store.User) []string {
		var out []string
		for _, it := range moreLinks(u) {
			out = append(out, it.Href)
		}
		return out
	}
	owner := hrefs(store.User{Role: store.RoleAdmin})
	if slices.Contains(owner, "/my") || slices.Contains(owner, "/admin/history") {
		t.Errorf("non-reporting admin's «Ещё»: %v", owner)
	}
	reporter := hrefs(store.User{Role: store.RoleAdmin, SubmitsReports: true})
	if !slices.Contains(reporter, "/my") || !slices.Contains(reporter, "/admin/history") {
		t.Errorf("reporting admin's «Ещё» lost an item: %v", reporter)
	}
}
