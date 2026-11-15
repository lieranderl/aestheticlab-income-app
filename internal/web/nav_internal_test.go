package web

import (
	"slices"
	"strings"
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

// 2026-11-15 production review: the owner's card said «Условия не заданы»,
// which reads as a problem for someone who doesn't report.
func TestNoTermsLineFitsWhetherThePersonReports(t *testing.T) {
	if got := noTermsLine(store.User{}); got != "Без условий — не сдаёт отчёты" {
		t.Errorf("non-reporter: %q", got)
	}
	if got := noTermsLine(store.User{SubmitsReports: true}); got != "Условия не заданы" {
		t.Errorf("reporter: %q", got)
	}
}

// Third production review: «Откуда»/«Куда» listed the owner (who doesn't
// report or hold money) among staff. Reporters come first, others last.
func TestMovementPeopleListReportersFirst(t *testing.T) {
	got := movementPeople([]store.User{
		{ID: "owner", DisplayName: "Evgenii"},
		{ID: "j", DisplayName: "Julia", SubmitsReports: true},
		{ID: "l", DisplayName: "Lera", SubmitsReports: true},
	})
	var labels []string
	for _, o := range got {
		labels = append(labels, o.Label)
	}
	if strings.Join(labels, ",") != "Julia,Lera,Evgenii" {
		t.Errorf("order = %v", labels)
	}
}
