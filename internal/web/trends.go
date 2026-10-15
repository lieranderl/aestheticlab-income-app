package web

import (
	"net/http"
	"slices"
	"strconv"

	"github.com/lieranderl/aestheticlab-income-app/internal/domain"
	"github.com/lieranderl/aestheticlab-income-app/internal/ru"
	"github.com/lieranderl/aestheticlab-income-app/internal/store"
	"github.com/lieranderl/aestheticlab-income-app/internal/views"
)

var trendMetrics = []views.TrendMetric{
	{ID: "overview", Label: "Обзор"},
	{ID: "revenue", Label: "Выручка"},
	{ID: "person_revenue", Label: "По сотрудникам"},
	{ID: "services", Label: "Услуги"},
	{ID: "creams", Label: "Кремы"},
	{ID: "certificates", Label: "Сертификаты"},
	{ID: "self", Label: "«Себе»"},
	{ID: "salon_income", Label: "Доход салона"},
	{ID: "expenses", Label: "Расходы"},
	{ID: "profit", Label: "Прибыль"},
	{ID: "dividends", Label: "Дивиденды"},
	{ID: "clients", Label: "Клиенты"},
}

func (a *app) handleTrends(w http.ResponseWriter, r *http.Request, u store.User) {
	period, _ := strconv.Atoi(r.URL.Query().Get("period"))
	if period != 3 && period != 6 && period != 12 {
		period = 6
	}

	metric := r.URL.Query().Get("metric")
	if metric == "" {
		metric = "overview"
	}
	metricValid := false
	metricLabel := ""
	for _, m := range trendMetrics {
		if m.ID == metric {
			metricValid = true
			metricLabel = m.Label
			break
		}
	}
	if !metricValid {
		http.Error(w, "Проверьте показатель", http.StatusBadRequest)
		return
	}

	endMonth := a.store.Today().Month()
	if qm := r.URL.Query().Get("m"); qm != "" {
		if m, err := domain.ParseMonth(qm); err == nil && !a.store.Today().Month().Before(m) {
			endMonth = m
		}
	}

	users, err := a.store.Users(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	var people []views.TrendPerson
	for _, u := range users {
		people = append(people, views.TrendPerson{ID: u.ID, Name: u.DisplayName})
	}

	personID := r.URL.Query().Get("person")
	if !slices.ContainsFunc(people, func(p views.TrendPerson) bool { return p.ID == personID }) {
		personID = ""
		if len(people) > 0 {
			personID = people[0].ID
		}
	}
	if metric == "person_revenue" {
		for _, p := range people {
			if p.ID == personID {
				metricLabel = "Выручка: " + p.Name
				break
			}
		}
	}

	window := domain.Window(endMonth, period)
	imported, err := a.store.ImportedMonths(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}

	results := make(map[domain.Month]domain.MonthResult)
	for _, m := range window {
		if m.Before(a.store.StartMonth()) || a.store.Today().Month().Before(m) {
			continue
		}
		settlement, err := a.store.Settlement(r.Context(), m)
		if err == nil && settlement.PreviewError == nil {
			results[m] = settlement.Result
		}
	}

	points := domain.Trend(window, results, imported)

	data := a.buildTrendData(period, metric, metricLabel, personID, people, points)
	a.render(w, r, views.TrendsPage(a.navFor(u, "/admin/trends"), data))
}

// trendValue is one series' figure for one month; ok is false when the month
// has no figure for it (no data, or an imported/legacy month without the stat).
func trendValue(metric, personID string, pt domain.TrendPoint) (raw int64, ok bool) {
	if !pt.HasData {
		return 0, false
	}
	stats := !pt.Imported && pt.StatsVersion > 0
	switch metric {
	case "revenue":
		return int64(pt.Revenue), true
	case "person_revenue":
		return int64(pt.RevenueByPerson[personID]), true
	case "services", "creams", "certificates":
		return int64(pt.RevenueByCategory[metric]), stats
	case "self":
		return int64(pt.Self), !pt.Imported
	case "salon_income":
		return int64(pt.SalonIncome), true
	case "expenses":
		return int64(pt.Expenses), true
	case "profit":
		return int64(pt.Profit), true
	case "dividends":
		return int64(pt.Dividends), true
	case "clients":
		return int64(pt.Clients), stats
	}
	return 0, false
}

// trendSeries are the bars drawn per month: three for «Обзор», otherwise one.
func trendSeries(metric, metricLabel string) []views.TrendSeries {
	if metric == "overview" {
		return []views.TrendSeries{{ID: "revenue", Label: "Выручка"}, {ID: "expenses", Label: "Расходы"}, {ID: "profit", Label: "Прибыль"}}
	}
	return []views.TrendSeries{{ID: metric, Label: metricLabel}}
}

// chartAxis rounds [lo, hi] out to a "nice" step (1, 2 or 5 × 10ⁿ units) so the
// axis has about three intervals and always includes zero. unit is the smallest
// step: 100 cents for money (whole euros), 1 for counts.
func chartAxis(lo, hi, unit int64) (axisLo, axisHi, step int64) {
	lo, hi = min(lo, 0), max(hi, 0)
	if lo == hi {
		return 0, unit, unit
	}
	raw := (hi - lo + 2) / 3
	step = unit
	for mag := unit; step < raw; mag *= 10 {
		for _, m := range []int64{1, 2, 5} {
			if step = m * mag; step >= raw {
				break
			}
		}
	}
	axisLo = -((-lo + step - 1) / step) * step
	axisHi = (hi + step - 1) / step * step
	return axisLo, axisHi, step
}

// Chart geometry in viewBox units (the SVG is 340 × 190 and scales to the card).
const (
	chartLeft   = 52
	chartRight  = 334
	chartTop    = 12
	chartBottom = 160
)

func (a *app) buildTrendData(period int, metric, metricLabel, personID string, people []views.TrendPerson, points []domain.TrendPoint) views.TrendData {
	d := views.TrendData{
		Period:      period,
		Metric:      metric,
		MetricLabel: metricLabel,
		PersonID:    personID,
		People:      people,
		Metrics:     trendMetrics,
		Series:      trendSeries(metric, metricLabel),
	}
	format := func(raw int64) string { return domain.FormatEUR(domain.Cents(raw)) }
	unit := int64(100)
	if metric == "clients" {
		format = func(raw int64) string { return strconv.FormatInt(raw, 10) }
		unit = 1
	}

	type cell struct {
		raw int64
		ok  bool
	}
	cells := make([][]cell, len(points))
	var lo, hi int64
	d.Selected = -1
	for i, pt := range points {
		row := views.TrendRow{
			MonthLabel: ru.MonthYear(pt.Month.Year, pt.Month.Mon),
			ShortMonth: ru.ShortMonthName(pt.Month.Mon),
			Imported:   pt.Imported,
		}
		for _, s := range d.Series {
			raw, ok := trendValue(s.ID, personID, pt)
			cells[i] = append(cells[i], cell{raw, ok})
			text := ""
			if ok {
				row.HasData, d.HasData = true, true
				text = format(raw)
				lo, hi = min(lo, raw), max(hi, raw)
				d.HasImported = d.HasImported || pt.Imported
			}
			row.Values = append(row.Values, text)
		}
		if row.HasData {
			d.Selected = i
		}
		d.Rows = append(d.Rows, row)
	}
	if !d.HasData {
		return d
	}

	axisLo, axisHi, step := chartAxis(lo, hi, unit)
	y := func(v int64) int {
		return chartBottom - int((v-axisLo)*(chartBottom-chartTop)/(axisHi-axisLo))
	}
	for v := axisLo; v <= axisHi; v += step {
		d.Ticks = append(d.Ticks, views.TrendTick{Y: y(v), Label: format(v)})
	}
	d.ZeroY = y(0)

	n, k := len(points), len(d.Series)
	slot := (chartRight - chartLeft) / n
	group := slot * 4 / 5
	if k == 1 {
		group = min(slot*3/5, 28)
	}
	barW := max(group/k, 2)
	for i := range points {
		x := chartLeft + i*slot
		col := views.TrendColumn{X: x, W: slot, LabelX: x + slot/2}
		start := x + (slot-barW*k)/2
		for j, c := range cells[i] {
			if !c.ok {
				continue
			}
			top, bottom := y(max(c.raw, 0)), y(min(c.raw, 0))
			if bottom-top < 1 && c.raw != 0 {
				if c.raw > 0 {
					top = bottom - 1
				} else {
					bottom = top + 1
				}
			}
			col.Bars = append(col.Bars, views.TrendBar{Series: j, X: start + j*barW, Y: top, W: barW, H: bottom - top})
		}
		d.Rows[i].Column = col
	}
	return d
}
