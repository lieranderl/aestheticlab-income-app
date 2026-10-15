package web

import (
	"fmt"
	"net/http"
	"time"

	"github.com/lieranderl/aestheticlab-income-app/internal/domain"
	"github.com/lieranderl/aestheticlab-income-app/internal/export"
	"github.com/lieranderl/aestheticlab-income-app/internal/store"
	"github.com/lieranderl/aestheticlab-income-app/internal/views"
)

func (a *app) handleExport(w http.ResponseWriter, r *http.Request, u store.User) {
	today := a.store.Today()
	from := r.URL.Query().Get("from")
	if from == "" {
		from = today.Month().FirstDay().String()
	}
	to := r.URL.Query().Get("to")
	if to == "" {
		to = today.String()
	}
	format := r.URL.Query().Get("format")
	if format == "" {
		format = "xlsx"
	}

	data := views.ExportFormData{
		From:   from,
		To:     to,
		Format: format,
	}
	a.render(w, r, views.ExportPage(a.navFor(u, "/admin/export"), data))
}

func (a *app) handleExportDownload(w http.ResponseWriter, r *http.Request, _ store.User) {
	q := r.URL.Query()
	fromStr := q.Get("from")
	toStr := q.Get("to")
	format := q.Get("format")
	if format == "" {
		format = "xlsx"
	}
	if format != "xlsx" && format != "csv_records" && format != "csv_movements" {
		http.Error(w, "Проверьте формат файла", http.StatusBadRequest)
		return
	}

	fromDate, err := domain.ParseDate(fromStr)
	if err != nil {
		http.Error(w, "Проверьте дату начала", http.StatusBadRequest)
		return
	}
	toDate, err := domain.ParseDate(toStr)
	if err != nil {
		http.Error(w, "Проверьте дату окончания", http.StatusBadRequest)
		return
	}
	if toDate.Before(fromDate) {
		http.Error(w, "Начало периода не может быть позже окончания", http.StatusBadRequest)
		return
	}
	// Limit to at most 366 days (1 year)
	if toDate.Time().Sub(fromDate.Time()) > 366*24*time.Hour {
		http.Error(w, "Период не может превышать 12 месяцев", http.StatusBadRequest)
		return
	}

	usersList, err := a.store.Users(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	usersMap := make(map[string]store.User, len(usersList))
	for _, usr := range usersList {
		usersMap[usr.ID] = usr
	}

	accsList, err := a.store.Accounts(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	accsMap := make(map[string]store.Account, len(accsList))
	for _, acc := range accsList {
		accsMap[acc.ID] = acc
	}

	catsList, err := a.store.Categories(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	catsMap := make(map[string]store.Category, len(catsList))
	for _, cat := range catsList {
		catsMap[cat.ID] = cat
	}

	reports, err := a.store.ReportsBetween(r.Context(), fromDate, toDate)
	if err != nil {
		a.serverError(w, r, err)
		return
	}

	movements, err := a.store.LedgerBetween(r.Context(), fromDate, toDate)
	if err != nil {
		a.serverError(w, r, err)
		return
	}

	startMonth := fromDate.Month()
	endMonth := toDate.Month()
	var months []domain.Month
	for m := startMonth; !endMonth.Before(m); m = m.Next() {
		months = append(months, m)
	}

	monthResults := make(map[domain.Month]domain.MonthResult)
	closing := make(map[domain.Month]domain.Balances)
	closed := make(map[domain.Month]bool)
	for _, m := range months {
		if m.Before(a.store.StartMonth()) || a.store.Today().Month().Before(m) {
			continue
		}
		settlement, err := a.store.Settlement(r.Context(), m)
		if err == nil && settlement.PreviewError == nil {
			monthResults[m] = settlement.Result
			if settlement.Closing != nil {
				closing[m] = *settlement.Closing
			}
			closed[m] = settlement.State.Snapshot != nil
		}
	}

	imported, err := a.store.ImportedMonths(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}

	trendPoints := domain.Trend(months, monthResults, imported)

	exportData := export.Data{
		From:         fromDate,
		To:           toDate,
		Users:        usersMap,
		Accounts:     accsMap,
		Categories:   catsMap,
		Reports:      reports,
		Movements:    movements,
		MonthResults: monthResults,
		Closing:      closing,
		Closed:       closed,
		Today:        a.store.Today(),
		TrendPoints:  trendPoints,
	}

	switch format {
	case "xlsx":
		filename := fmt.Sprintf("aestheticlab-export-%s-%s.xlsx", fromDate.String(), toDate.String())
		w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
		if err := export.XLSX(w, exportData); err != nil {
			a.serverError(w, r, err)
			return
		}
	case "csv_records":
		filename := fmt.Sprintf("aestheticlab-records-%s-%s.csv", fromDate.String(), toDate.String())
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
		if err := export.CSV(w, "records", exportData); err != nil {
			a.serverError(w, r, err)
			return
		}
	case "csv_movements":
		filename := fmt.Sprintf("aestheticlab-movements-%s-%s.csv", fromDate.String(), toDate.String())
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
		if err := export.CSV(w, "movements", exportData); err != nil {
			a.serverError(w, r, err)
			return
		}
	}
}
