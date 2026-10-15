package web_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/lieranderl/aestheticlab-income-app/internal/store"
)

func TestAdminCreatesCategoryWithoutJavaScript(t *testing.T) {
	s := newSite(t)
	cookies := s.login("anna", annaPass)
	r := s.post("/admin/categories", url.Values{"kind": {"expense"}, "name": {"Кофе и чай"}}, cookies)
	if r.Code != http.StatusSeeOther {
		t.Fatalf("status %d: %s", r.Code, r.Body.String())
	}
	r = s.get("/admin/categories?kind=expense", cookies)
	if !strings.Contains(r.Body.String(), "Кофе и чай") {
		t.Fatal("category missing from list")
	}
	r = s.get("/movements/new?type=expense", cookies)
	if !strings.Contains(r.Body.String(), "Кофе и чай") {
		t.Fatal("new expense category missing from movement form")
	}
}

func TestCategoryRoutesAreAdminOnlyAndValidatePlainForms(t *testing.T) {
	s := newSite(t)
	staff := s.login("olga", olgaPass)
	for _, path := range []string{"/admin/categories", "/admin/categories/new", "/admin/categories/services"} {
		if got := s.get(path, staff).Code; got != http.StatusForbidden {
			t.Fatalf("staff GET %s: %d", path, got)
		}
	}
	if got := s.post("/admin/categories", url.Values{"kind": {"expense"}, "name": {"X"}}, staff).Code; got != http.StatusForbidden {
		t.Fatalf("staff POST: %d", got)
	}
	admin := s.login("anna", annaPass)
	if got := s.post("/admin/categories", url.Values{"kind": {"payment"}, "name": {"Новая оплата"}, "destination": {"invalid"}}, admin); got.Code != http.StatusUnprocessableEntity || !strings.Contains(got.Body.String(), "Проверьте поля формы") {
		t.Fatalf("invalid payment: %d %s", got.Code, got.Body.String())
	}
	if got := s.get("/admin/categories?kind=payment", admin); got.Code != http.StatusOK || !strings.Contains(got.Body.String(), "aria-current=\"page\"") {
		t.Fatalf("payment tab: %d", got.Code)
	}
}

func TestCategoryDeleteRequiresConfirmationAndUsedCategoryCannotBeDeleted(t *testing.T) {
	s := newSite(t)
	admin := s.login("anna", annaPass)
	r := s.post("/admin/categories", url.Values{"kind": {"expense"}, "name": {"Временная"}}, admin)
	if r.Code != http.StatusSeeOther {
		t.Fatalf("create category: %d", r.Code)
	}
	cats, err := s.st.Categories(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var created store.Category
	for _, c := range cats {
		if c.Name == "Временная" {
			created = c
			break
		}
	}
	form := url.Values{"version": {"1"}}
	if got := s.post("/admin/categories/"+created.ID+"/delete", form, admin); got.Code != http.StatusUnprocessableEntity {
		t.Fatalf("without confirmation: %d", got.Code)
	}
	form.Set("confirm", "1")
	if got := s.post("/admin/categories/"+created.ID+"/delete", form, admin); got.Code != http.StatusSeeOther {
		t.Fatalf("delete unused: %d %s", got.Code, got.Body.String())
	}
	if _, err := s.st.CategoryByID(ctx, created.ID); err != store.ErrNotFound {
		t.Fatalf("deleted category: %v", err)
	}
	// Fixed category deletion refused
	if got := s.post("/admin/categories/services/delete", url.Values{"version": {"1"}, "confirm": {"1"}}, admin); got.Code != http.StatusUnprocessableEntity {
		t.Fatalf("delete fixed category: %d", got.Code)
	}
}

func TestCategoryAuditExplainsArchiveAndDestination(t *testing.T) {
	s := newSite(t)
	admin := s.login("anna", annaPass)
	if got := s.post("/admin/categories/card", url.Values{"version": {"1"}, "name": {"Карта"}, "destination": {"cash"}}, admin); got.Code != http.StatusSeeOther {
		t.Fatalf("edit: %d", got.Code)
	}
	if got := s.post("/admin/categories/rent/archive", url.Values{"version": {"1"}, "confirm": {"1"}, "active": {"0"}}, admin); got.Code != http.StatusSeeOther {
		t.Fatalf("archive: %d", got.Code)
	}
	audit := s.get("/admin/audit?from=2026-11-20&to=2026-11-20&entity=category", admin)
	for _, want := range []string{"Архивирование", "Куда поступает оплата", "Касса — карта салона", "Касса — наличные"} {
		if !strings.Contains(audit.Body.String(), want) {
			t.Fatalf("audit missing %q: %s", want, audit.Body.String())
		}
	}
}
