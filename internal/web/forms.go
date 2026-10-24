package web

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/a-h/templ"
	"github.com/starfederation/datastar-go/datastar"

	"github.com/lieranderl/aestheticlab-income-app/internal/domain"
	"github.com/lieranderl/aestheticlab-income-app/internal/store"
)

// maxFormBytes bounds a form post; the largest form is a few hundred bytes.
const maxFormBytes = 64 << 10

// isDatastar says whether Datastar sent the request (it then expects SSE).
func isDatastar(r *http.Request) bool { return r.Header.Get("Datastar-Request") == "true" }

// readForm parses a posted form, at most maxFormBytes.
func readForm(w http.ResponseWriter, r *http.Request) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
	return r.ParseForm()
}

// navigate sends the browser to url: a Datastar request through the
// redirectUrl signal (the CSP forbids the inline script Datastar's own
// redirect uses), a plain request with a 303.
func (a *app) navigate(w http.ResponseWriter, r *http.Request, url string) {
	if !strings.HasPrefix(url, "/") || strings.HasPrefix(url, "//") || strings.ContainsAny(url, "\\\r\n") {
		a.serverError(w, r, store.ErrInvalid)
		return
	}
	if isDatastar(r) {
		sse := datastar.NewSSE(w, r)
		if err := sse.MarshalAndPatchSignals(map[string]any{"redirectUrl": url}); err != nil {
			slog.ErrorContext(r.Context(), "send redirect", "error", err)
		}
		return
	}
	// #nosec G710 -- Server-built local paths only; the guard rejects external and protocol-relative targets.
	http.Redirect(w, r, url, http.StatusSeeOther)
}

// formFailed shows a form again with its errors: Datastar gets the form
// patched in place (by its id), a plain post gets the whole page (422).
func (a *app) formFailed(w http.ResponseWriter, r *http.Request, form, page templ.Component) {
	a.formFailedStatus(w, r, http.StatusUnprocessableEntity, form, page)
}

// formFailedStatus is formFailed with the status a page answers with.
func (a *app) formFailedStatus(w http.ResponseWriter, r *http.Request, status int, form, page templ.Component) {
	if isDatastar(r) {
		sse := datastar.NewSSE(w, r)
		if err := sse.PatchElementTempl(form); err != nil {
			slog.ErrorContext(r.Context(), "patch form", "error", err)
		}
		return
	}
	a.renderStatus(w, r, status, page)
}

func (a *app) render(w http.ResponseWriter, r *http.Request, c templ.Component) {
	a.renderStatus(w, r, http.StatusOK, c)
}

func (a *app) renderStatus(w http.ResponseWriter, r *http.Request, status int, c templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := c.Render(r.Context(), w); err != nil {
		slog.ErrorContext(r.Context(), "render failed", "path", logValue(r.URL.Path), "error", err)
	}
}

// problemText turns a store or domain error into what the person reads;
// unexpected errors are logged and shown as a generic retry message.
func problemText(r *http.Request, err error) string {
	var ve *domain.ValidationError
	switch {
	case errors.Is(err, store.ErrUsernameTaken):
		return "Это имя пользователя уже занято"
	case errors.Is(err, store.ErrBalanceOpen):
		return "Удалить можно только при нулевом балансе с салоном — сначала рассчитайтесь"
	case errors.Is(err, store.ErrOpenRecords):
		return "Есть отчёты в незакрытом месяце или движения, которые ждут подтверждения — сначала закройте месяц и подтвердите или отмените движения"
	case errors.Is(err, store.ErrTermsInForce):
		return "Зарплата или доля в прибыли продолжат начисляться — сначала задайте условия с зарплатой 0 и долей 0 с первого открытого месяца"
	case errors.Is(err, store.ErrFinancialWritesPaused):
		return "Запись финансовых операций временно приостановлена"
	case errors.Is(err, store.ErrMonthClosed):
		return "Месяц закрыт — выберите открытый месяц"
	case errors.Is(err, store.ErrVersionConflict):
		return "Запись изменена другим пользователем — обновите страницу"
	case errors.Is(err, store.ErrForbidden):
		return "Нет доступа"
	case errors.Is(err, store.ErrInvalid):
		return "Проверьте поля формы"
	case errors.As(err, &ve):
		switch ve.Problem {
		case domain.ProblemTermsMonthHasReports:
			return "У сотрудника уже есть отчёты в месяцах, на которые распространятся эти условия — выберите более поздний месяц"
		case domain.ProblemBeforeStartMonth:
			return "Этот месяц раньше начала работы приложения"
		case domain.ProblemMonthClosed:
			return "Месяц закрыт — выберите открытый месяц"
		case "fixed_category":
			return "Базовые категории нельзя архивировать или удалить"
		}
	}
	slog.ErrorContext(r.Context(), "request failed", "path", logValue(r.URL.Path), "error", err)
	return "Не удалось сохранить, попробуйте ещё раз"
}
