package web

import (
	"net/http"

	"github.com/lieranderl/aestheticlab-income-app/internal/domain"
	"github.com/lieranderl/aestheticlab-income-app/internal/store"
	"github.com/lieranderl/aestheticlab-income-app/internal/views"
)

func (a *app) prefillSettlement(r *http.Request, f *views.MovementFormData) error {
	raw := r.URL.Query().Get("settlement")
	if raw == "" {
		return nil
	}
	if !f.Admin {
		return store.ErrForbidden
	}
	m, err := domain.ParseMonth(raw)
	if err != nil {
		return store.ErrInvalid
	}
	uid := r.URL.Query().Get("person")
	if _, err := a.store.User(r.Context(), uid); err != nil {
		return store.ErrInvalid
	}
	suggestion, err := a.store.SettlementSuggestion(r.Context(), uid, m)
	if err != nil {
		return err
	}
	action := domain.SuggestAction(suggestion.Balance)
	if (f.Type == "payout" && action.Kind != domain.SalonPays) || (f.Type == "deposit" && action.Kind != domain.PersonPays) || (f.Type != "payout" && f.Type != "deposit") {
		return store.ErrInvalid
	}
	f.SettlementMonth, f.SettlementPerson, f.SettlementRevision = m.String(), uid, suggestion.Revision
	f.Amount = plainEuros(action.Amount)
	f.ContextNotice = "Текущий остаток: " + balanceWords(suggestion.Balance) + ". Движение будет записано выбранной датой в открытом месяце."
	f.Back = settlementURL(m)
	if f.Type == "payout" {
		f.To = "person:" + uid
		for _, option := range f.Sources {
			if option.Value == "account:"+domain.KassaCard {
				f.From = option.Value
				break
			}
		}
	} else {
		f.From = "person:" + uid
	}
	return nil
}

func settlementContext(r *http.Request, f *views.MovementFormData) error {
	f.SettlementMonth = r.PostFormValue("settlement")
	f.SettlementPerson = r.PostFormValue("settlementPerson")
	f.SettlementRevision = r.PostFormValue("settlementRevision")
	if f.SettlementMonth == "" {
		if f.SettlementPerson != "" || f.SettlementRevision != "" {
			return store.ErrInvalid
		}
		return nil
	}
	if !f.Admin {
		return store.ErrForbidden
	}
	m, err := domain.ParseMonth(f.SettlementMonth)
	if err != nil || f.SettlementPerson == "" || f.SettlementRevision == "" {
		return store.ErrInvalid
	}
	f.Back = settlementURL(m)
	f.ContextNotice = "Расчёт по выбранному месяцу. Выплата или внесение уменьшает текущий остаток."
	return nil
}
