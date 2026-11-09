package web

import (
	"cmp"
	"errors"
	"net/http"
	"slices"
	"strconv"

	"github.com/lieranderl/aestheticlab-income-app/internal/domain"
	"github.com/lieranderl/aestheticlab-income-app/internal/store"
	"github.com/lieranderl/aestheticlab-income-app/internal/views"
)

// newestEntered drops voided rows and puts the most recently entered first,
// so a list reads in the order people typed it in, whatever dates they chose.
func newestEntered(es []store.Entry) []store.Entry {
	es = slices.DeleteFunc(es, func(e store.Entry) bool { return e.Status == domain.Voided })
	byEntry(es)
	return es
}

// byEntry sorts rows newest entered first.
func byEntry(es []store.Entry) {
	slices.SortStableFunc(es, func(a, b store.Entry) int {
		return cmp.Or(b.CreatedAt.Compare(a.CreatedAt), cmp.Compare(b.ID, a.ID))
	})
}

// errProcessed: the author's own expense was confirmed or voided since they opened it.
var errProcessed = errors.New("movement already processed")

// editReadError answers the edit form's read failures.
func (a *app) editReadError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, errProcessed):
		http.Error(w, "Движение уже обработано — обновите страницу", http.StatusConflict)
	case !actionReadError(w, r, err):
		a.serverError(w, r, err)
	}
}

// mayEdit is who may correct a row: the admin any expense, the author only
// their own pending one (the same people who may void it).
func mayEdit(u store.User, e store.Entry) bool {
	return e.Status != domain.Voided && canEdit(u, e)
}

// canEdit is mayEdit without the voided check, so a repeated or stale edit of
// the admin's reads «Движение уже отменено» rather than «Нет доступа».
func canEdit(u store.User, e store.Entry) bool {
	return e.Type == domain.Expense &&
		(u.Actor().Admin || (e.CreatedBy == u.ID && e.PersonID == u.ID && e.Status == domain.Pending))
}

// editForm is the expense form filled with the row being corrected.
func (a *app) editForm(r *http.Request, u store.User) (views.MovementFormData, error) {
	e, err := a.store.Entry(r.Context(), r.PathValue("id"))
	if err != nil {
		return views.MovementFormData{}, err
	}
	if e.Type != domain.Expense {
		return views.MovementFormData{}, store.ErrNotFound
	}
	if !canEdit(u, e) {
		if e.Type == domain.Expense && e.CreatedBy == u.ID && e.PersonID == u.ID {
			return views.MovementFormData{}, errProcessed // confirmed or voided meanwhile
		}
		return views.MovementFormData{}, store.ErrForbidden
	}
	admin := u.Actor().Admin
	f, err := a.movementForm(r, u, admin, string(domain.Expense))
	if err != nil {
		return f, err
	}
	f.Edit = true
	f.Action = "/movements/" + e.ID + "/edit"
	f.Version = strconv.FormatInt(e.Version, 10)
	f.Amount = amountText(e.Amount)
	f.Date = e.Date.String()
	f.From = partyValue(e.From)
	f.To = partyValue(e.To)
	f.Category = e.ExpenseCategory
	f.Note = e.Note
	if e.Status == domain.Voided {
		f.Error = "Движение уже отменено — обновите страницу"
	}
	return f, nil
}

func (a *app) handleEditMovementPage(w http.ResponseWriter, r *http.Request, u store.User) {
	f, err := a.editForm(r, u)
	if err != nil {
		a.editReadError(w, r, err)
		return
	}
	a.render(w, r, views.MovementFormPage(a.navFor(u, f.Back), f))
}

func (a *app) handleEditMovement(w http.ResponseWriter, r *http.Request, u store.User) {
	f, err := a.editForm(r, u)
	if err != nil {
		a.editReadError(w, r, err)
		return
	}
	if err := readForm(w, r); err != nil {
		http.Error(w, "Проверьте поля формы", http.StatusBadRequest)
		return
	}
	f.Amount = r.PostFormValue("amount")
	f.Date = r.PostFormValue("date")
	f.From = r.PostFormValue("from")
	f.Category = r.PostFormValue("expenseCategory")
	f.Note = r.PostFormValue("note")
	f.Version = r.PostFormValue("version")
	version, err := strconv.ParseInt(f.Version, 10, 64)
	if err != nil || version < 1 {
		err = store.ErrInvalid
	}
	var amount domain.Cents
	if err == nil {
		amount, err = domain.ParseAmount(f.Amount)
	}
	var day domain.Date
	if err == nil {
		if day, err = domain.ParseDate(f.Date); err != nil {
			err = store.ErrInvalid
		}
	}
	if err == nil {
		_, err = a.store.EditMovement(r.Context(), u.Actor(), r.PathValue("id"), version, store.NewMovement{
			Type:            domain.Expense,
			Amount:          amount,
			Date:            day,
			From:            movementParty(f.From),
			To:              domain.Outside,
			ExpenseCategory: f.Category,
			Note:            f.Note,
		})
	}
	if err != nil {
		if errors.Is(err, store.ErrForbidden) { // e.g. confirmed between the read and the write
			// The form read passed, so for the author it can only have been processed since.
			if !u.Actor().Admin {
				err = errProcessed
			}
			a.editReadError(w, r, err)
			return
		}
		f.Error = movementProblem(r, err)
		a.formFailed(w, r, views.MovementForm(f), views.MovementFormPage(a.navFor(u, f.Back), f))
		return
	}
	saved := "1"
	if !u.Actor().Admin {
		saved = "pending"
	}
	a.navigate(w, r, f.Back+"?saved="+saved)
}
