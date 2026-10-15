package web

import (
	"crypto/rand"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/lieranderl/aestheticlab-income-app/internal/domain"
	"github.com/lieranderl/aestheticlab-income-app/internal/store"
	"github.com/lieranderl/aestheticlab-income-app/internal/views"
)

func categoryKind(r *http.Request) store.CategoryKind {
	k := store.CategoryKind(r.URL.Query().Get("kind"))
	if k == "" {
		k = store.KindRevenue
	}
	return k
}

func validCategoryKind(k store.CategoryKind) bool {
	return k == store.KindRevenue || k == store.KindPayment || k == store.KindExpense
}

func categoryBack(k store.CategoryKind) string { return "/admin/categories?kind=" + string(k) }

func (a *app) handleCategories(w http.ResponseWriter, r *http.Request, u store.User) {
	k := categoryKind(r)
	if !validCategoryKind(k) {
		http.Error(w, "Проверьте вид категории", http.StatusBadRequest)
		return
	}
	cats, err := a.store.Categories(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	var rows []store.Category
	for _, c := range cats {
		if c.Kind == k {
			rows = append(rows, c)
		}
	}
	a.render(w, r, views.CategoriesPage(a.navFor(u, "/admin/categories"), k, rows))
}

func (a *app) handleNewCategory(w http.ResponseWriter, r *http.Request, u store.User) {
	k := categoryKind(r)
	if !validCategoryKind(k) {
		http.Error(w, "Проверьте вид категории", http.StatusBadRequest)
		return
	}
	f := views.CategoryFormData{Category: store.Category{Kind: k, CountsTowardSelf: k == store.KindRevenue}, Action: "/admin/categories", New: true}
	a.render(w, r, views.CategoryPage(a.navFor(u, "/admin/categories"), f))
}

func (a *app) handleCategory(w http.ResponseWriter, r *http.Request, u store.User) {
	c, err := a.store.CategoryByID(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) || errors.Is(err, store.ErrInvalid) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	a.render(w, r, views.CategoryPage(a.navFor(u, "/admin/categories"), views.CategoryFormData{Category: c, Action: "/admin/categories/" + c.ID}))
}

func (a *app) handleSaveCategory(w http.ResponseWriter, r *http.Request, u store.User) {
	if err := readForm(w, r); err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	isNew := r.PathValue("id") == ""
	var c store.Category
	var err error
	if isNew {
		kind := store.CategoryKind(r.PostFormValue("kind"))
		if !validCategoryKind(kind) {
			http.Error(w, "Проверьте вид категории", http.StatusBadRequest)
			return
		}
		c = store.Category{ID: "c_" + strings.ToLower(rand.Text()[:20]), Kind: kind}
	} else {
		c, err = a.store.CategoryByID(r.Context(), r.PathValue("id"))
		if errors.Is(err, store.ErrNotFound) || errors.Is(err, store.ErrInvalid) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			a.serverError(w, r, err)
			return
		}
	}
	c.Name = strings.TrimSpace(r.PostFormValue("name"))
	c.CountsTowardSelf = r.PostFormValue("countsTowardSelf") == "1"
	c.IsCertificateSale = r.PostFormValue("isCertificateSale") == "1"
	c.Destination = domain.Destination(r.PostFormValue("destination"))
	action := "/admin/categories"
	if !isNew {
		action += "/" + c.ID
	}
	f := views.CategoryFormData{Category: c, Action: action, New: isNew}
	fail := func(msg string) {
		f.Error = msg
		a.formFailed(w, r, views.CategoryForm(f), views.CategoryPage(a.navFor(u, "/admin/categories"), f))
	}
	if c.Name == "" || utf8.RuneCountInString(c.Name) > 120 {
		fail("Введите название (до 120 символов)")
		return
	}
	if !isNew {
		c.Version, err = strconv.ParseInt(r.PostFormValue("version"), 10, 64)
		if err != nil {
			fail("Обновите страницу и повторите сохранение")
			return
		}
		f.Category.Version = c.Version
	}
	if isNew {
		_, err = a.store.CreateCategory(r.Context(), u.Actor(), c)
	} else {
		err = a.store.UpdateCategory(r.Context(), u.Actor(), c)
	}
	if err != nil {
		fail(problemText(r, err))
		return
	}
	a.navigate(w, r, categoryBack(c.Kind))
}

func (a *app) handleCategoryAction(w http.ResponseWriter, r *http.Request, u store.User) {
	if err := readForm(w, r); err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	c, err := a.store.CategoryByID(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) || errors.Is(err, store.ErrInvalid) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	version, err := strconv.ParseInt(r.PostFormValue("version"), 10, 64)
	if err == nil {
		switch r.PathValue("action") {
		case "archive":
			if r.PostFormValue("confirm") != "1" {
				err = store.ErrInvalid
			} else if active := r.PostFormValue("active"); active != "0" && active != "1" {
				err = store.ErrInvalid
			} else {
				err = a.store.SetCategoryActive(r.Context(), u.Actor(), c.ID, version, r.PostFormValue("active") == "1")
			}
		case "delete":
			if r.PostFormValue("confirm") != "1" {
				err = store.ErrInvalid
			} else {
				err = a.store.DeleteCategory(r.Context(), u.Actor(), c.ID, version)
			}
		case "reorder":
			var nv int64
			nv, err = strconv.ParseInt(r.PostFormValue("neighborVersion"), 10, 64)
			if err == nil {
				err = a.store.ReorderCategory(r.Context(), u.Actor(), c.ID, version, r.PostFormValue("neighbor"), nv)
			}
		default:
			http.NotFound(w, r)
			return
		}
	}
	if err != nil {
		f := views.CategoryFormData{Category: c, Action: "/admin/categories/" + c.ID, Error: problemText(r, err)}
		a.formFailed(w, r, views.CategoryForm(f), views.CategoryPage(a.navFor(u, "/admin/categories"), f))
		return
	}
	a.navigate(w, r, categoryBack(c.Kind))
}
