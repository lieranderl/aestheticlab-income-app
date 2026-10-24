package web

import (
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/lieranderl/aestheticlab-income-app/internal/domain"
	"github.com/lieranderl/aestheticlab-income-app/internal/ru"
	"github.com/lieranderl/aestheticlab-income-app/internal/store"
	"github.com/lieranderl/aestheticlab-income-app/internal/views"
)

type auditField struct{ key, label string }

var auditEntities = []views.FilterOption{
	{Value: "report", Label: "Отчёт"}, {Value: "ledger", Label: "Движение"}, {Value: "user", Label: "Сотрудник"},
	{Value: "terms", Label: "Условия"}, {Value: "category", Label: "Категория"}, {Value: "account", Label: "Счёт кассы"},
	{Value: "month", Label: "Месяц"}, {Value: "importedMonth", Label: "Импорт месяца"},
}

func (a *app) handleAudit(w http.ResponseWriter, r *http.Request, u store.User) {
	f, ok := a.readHistoryFilter(r)
	if !ok {
		http.Error(w, "Проверьте период: не больше года, начало не позже конца", http.StatusBadRequest)
		return
	}
	users, err := a.store.Users(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	cats, err := a.store.Categories(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	f.category = ""
	filters, ok := f.view("/admin/audit", users, nil)
	if !ok {
		http.Error(w, "Проверьте сотрудника", http.StatusBadRequest)
		return
	}
	filters.Audit, filters.Entities = true, auditEntities
	valid := f.entity == ""
	for _, option := range auditEntities {
		if option.Value == f.entity {
			valid = true
			q := f.values()
			q.Del("entity")
			filters.Chips = append(filters.Chips, views.FilterChip{Label: option.Label, Href: "/admin/audit?" + q.Encode()})
		}
	}
	if !valid {
		http.Error(w, "Проверьте вид изменения", http.StatusBadRequest)
		return
	}
	rows, err := a.store.AuditBetween(r.Context(), f.from, f.to)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	rows = slices.DeleteFunc(rows, func(row store.AuditEntry) bool {
		return (f.person != "" && row.ActorID != f.person) || (f.entity != "" && string(row.Entity) != f.entity)
	})
	p := views.AuditHistory{Filters: filters}
	paging, first, last := f.paging("/admin/audit", len(rows))
	p.Paging = paging
	for _, row := range rows[first:last] {
		p.Rows = append(p.Rows, a.auditCard(row, users, cats))
	}
	a.render(w, r, views.AuditPage(a.navFor(u, "/admin/audit"), p))
}

func snapshotFields(s string) map[string]json.RawMessage {
	var fields map[string]json.RawMessage
	_ = json.Unmarshal([]byte(s), &fields)
	return fields
}

func auditString(fields map[string]json.RawMessage, key string) string {
	var s string
	_ = json.Unmarshal(fields[key], &s)
	return s
}

func (a *app) auditCard(row store.AuditEntry, users []store.User, cats []store.Category) views.AuditCard {
	p := views.AuditCard{Actor: personName(users, row.ActorID), At: a.store.LocalTime(row.At).Format("02.01.2006 · 15:04")}
	switch {
	case row.ActorID == "" || row.ActorID == "login":
		p.Actor = "Система"
	case strings.HasPrefix(row.ActorID, "cli:"): // cmd/resetpass, cmd/renameaccount…
		p.Actor = "Командная строка (" + strings.TrimPrefix(row.ActorID, "cli:") + ")"
	}
	for _, option := range auditEntities {
		if option.Value == string(row.Entity) {
			p.Entity = option.Label
		}
	}
	if p.Entity == "" {
		p.Entity = "Запись"
	}
	p.Action = map[string]string{
		store.ActionCreate:  "Создание",
		store.ActionUpdate:  "Изменение",
		store.ActionConfirm: "Подтверждение",
		store.ActionVoid:    "Отмена",
		store.ActionRecount: "Пересчёт кассы",
		store.ActionClose:   "Закрытие",
		store.ActionReopen:  "Открытие заново",
		store.ActionSetUp:   "Первоначальная настройка",
		store.ActionLock:    "Блокировка входа",
		store.ActionRemove:  "Удаление",
	}[row.Action]
	if p.Action == "" {
		p.Action = "Изменение"
	}
	if row.Entity == store.EntityReport && row.Action == store.ActionVoid {
		p.Action = "Удаление"
	}
	before, after := snapshotFields(row.Before), snapshotFields(row.After)
	if row.Entity == store.EntityReport {
		bclient, vclient := snapshotFields(string(before["clientInput"])), snapshotFields(string(after["clientInput"]))
		for _, field := range []auditField{{key: "services", label: "Услуги"}, {key: "serviceCash", label: "В кассу за услуги — наличные"}, {key: "serviceCard", label: "В кассу за услуги — карта"}, {key: "certificatePaid", label: "Оплачено сертификатом"}} {
			bc, bok := auditAmount(bclient[field.key])
			vc, vok := auditAmount(vclient[field.key])
			if (bok || vok) && bc != vc {
				p.Changes = append(p.Changes, views.MoneyRow{Name: field.label, Value: domain.FormatEUR(bc) + " → " + domain.FormatEUR(vc)})
			}
		}
		if b, v := auditString(bclient, "extrasDestination"), auditString(vclient, "extrasDestination"); b != v {
			label := func(s string) string {
				switch s {
				case "cash":
					return "Касса — наличные"
				case "kassa_card":
					return "Касса — карта салона"
				}
				return "—"
			}
			p.Changes = append(p.Changes, views.MoneyRow{Name: "Дополнительные продажи внесены", Value: label(b) + " → " + label(v)})
		}

		if len(vclient) > 0 {
			bc, _ := auditAmount(before["cardIn"])
			vc, _ := auditAmount(after["cardIn"])
			if bc != vc {
				p.Changes = append(p.Changes, views.MoneyRow{Name: "Касса — карта салона", Value: domain.FormatEUR(bc) + " → " + domain.FormatEUR(vc)})
			}
		}
		for _, group := range []struct {
			key, label string
			kind       store.CategoryKind
		}{{"revenue", "Выручка", store.KindRevenue}, {"payments", "Оплата", store.KindPayment}} {
			if group.key == "payments" && len(vclient) > 0 {
				continue
			}
			b, v := snapshotFields(string(before[group.key])), snapshotFields(string(after[group.key]))
			for _, cat := range cats {
				if cat.Kind != group.kind {
					continue
				}
				bc, _ := auditAmount(b[cat.ID])
				vc, _ := auditAmount(v[cat.ID])
				if bc != vc {
					p.Changes = append(p.Changes, views.MoneyRow{Name: group.label + " · " + cat.Name, Value: domain.FormatEUR(bc) + " → " + domain.FormatEUR(vc)})
				}
			}
		}
	}
	switch row.Entity {
	case store.EntityReport:
		uid, date := auditString(after, "userId"), auditString(after, "date")
		if d, err := domain.ParseDate(date); err == nil {
			p.Subject = personName(users, uid) + " · " + ru.Date(d.Time())
			p.Href = "/admin/reports/" + uid + "/" + d.String()
			if n, ok := auditAmount(after["clientNumber"]); ok && n > 0 {
				p.Subject += " · Клиент " + strconv.FormatInt(int64(n), 10)
				p.Href = "/clients/" + row.EntityID
				if row.Action == store.ActionVoid {
					// The removed client has no page; show what left the totals.
					p.Href = ""
					total, _ := auditAmount(before["total"])
					cash, _ := auditAmount(before["cashToKassa"])
					card, _ := auditAmount(before["cardIn"])
					transfer, _ := auditAmount(before["transferToCard"])
					p.Changes = append(p.Changes,
						views.MoneyRow{Name: "Выручка", Value: domain.FormatEUR(total)},
						views.MoneyRow{Name: "Касса — наличные", Value: domain.FormatEUR(cash)},
						views.MoneyRow{Name: "Касса — карта салона", Value: domain.FormatEUR(card + transfer)})
				}
				if created, err := time.Parse(time.RFC3339Nano, auditString(after, "createdAt")); err == nil {
					loc, _ := time.LoadLocation("Europe/Brussels")
					p.Subject += " · " + created.In(loc).Format("15:04")
				}
			}
		}
	case store.EntityUser:
		p.Subject = personName(users, row.EntityID)
	case store.EntityTerms:
		uid, month, _ := strings.Cut(row.EntityID, "/")
		p.Subject = personName(users, uid)
		if m, err := domain.ParseMonth(month); err == nil {
			p.Subject += " · " + monthLabel(m)
		}
	case store.EntityAccount:
		if row.Action == store.ActionRecount {
			for _, field := range []auditField{{key: "expected", label: "По учёту"}, {key: "counted", label: "При пересчёте"}} {
				amount, ok := auditAmount(after[field.key])
				p.Changes = append(p.Changes, views.MoneyRow{Name: field.label, Value: auditMoney(amount, ok)})
			}
			after = snapshotFields(string(after["account"]))
		}
		p.Subject = auditString(after, "name")
	case store.EntityCategory:
		p.Subject = auditString(after, "name")
		p.Href = "/admin/categories/" + row.EntityID
		if string(after["deleted"]) == "true" {
			p.Action = "Удаление"
			p.Href = ""
		} else if string(before["active"]) == "true" && string(after["active"]) == "false" {
			p.Action = "Архивирование"
		} else if string(before["active"]) == "false" && string(after["active"]) == "true" {
			p.Action = "Возврат из архива"
		} else if string(before["order"]) != "" && string(before["order"]) != string(after["order"]) {
			p.Action = "Изменение порядка"
		}
		for _, field := range []auditField{{"countsTowardSelf", "Входит в «Себе»"}, {"isCertificateSale", "Продажа сертификата"}, {"active", "В каталоге"}, {"deleted", "Удалена"}} {
			if string(before[field.key]) != string(after[field.key]) {
				p.Changes = append(p.Changes, views.MoneyRow{Name: field.label, Value: auditBool(before[field.key]) + " → " + auditBool(after[field.key])})
			}
		}
		if b, v := auditDestination(auditString(before, "destination")), auditDestination(auditString(after, "destination")); b != v {
			p.Changes = append(p.Changes, views.MoneyRow{Name: "Куда поступает оплата", Value: b + " → " + v})
		}
		if string(before["order"]) != "" && string(before["order"]) != string(after["order"]) {
			p.Changes = append(p.Changes, views.MoneyRow{Name: "Порядок", Value: string(before["order"]) + " → " + string(after["order"])})
		}
	case store.EntityMonth, store.EntityImported:
		if m, err := domain.ParseMonth(row.EntityID); err == nil {
			p.Subject = monthLabel(m)
		}
	}
	// A fixed allowlist keeps password/session fields and implementation data out of the log.
	for _, field := range []auditField{
		{key: "total", label: "Выручка"},
		{key: "selfAmount", label: "«Себе»"},
		{key: "salonShare", label: "«Салону»"},
		{key: "cashToKassa", label: "Наличными в кассу"},
		{key: "transferToCard", label: "Переводом на карту салона"},
		{key: "amount", label: "Сумма"},
		{key: "salaryCents", label: "Зарплата"},
		{key: "highSelfFromCents", label: "Порог повышенного «Себе»"},
	} {
		b, bok := auditAmount(before[field.key])
		v, vok := auditAmount(after[field.key])
		if !bok && !vok || bok && vok && b == v {
			continue
		}
		p.Changes = append(p.Changes, views.MoneyRow{Name: field.label, Value: auditMoney(b, bok) + " → " + auditMoney(v, vok)})
	}
	for _, field := range []auditField{
		{key: "selfPct", label: "«Себе» %"},
		{key: "highSelfPct", label: "Повышенное «Себе» %"},
		{key: "dividendWeight", label: "Доля в прибыли"},
	} {
		b, bok := auditAmount(before[field.key])
		v, vok := auditAmount(after[field.key])
		if !bok && !vok || bok && vok && b == v {
			continue
		}
		format := func(n domain.Cents, ok bool) string {
			if !ok {
				return "—"
			}
			if field.key == "dividendWeight" {
				return strconv.FormatInt(int64(n), 10)
			}
			return percent(domain.BasisPoints(n))
		}
		p.Changes = append(p.Changes, views.MoneyRow{Name: field.label, Value: format(b, bok) + " → " + format(v, vok)})
	}
	for _, field := range []auditField{
		{key: "active", label: "Доступ"},
		{key: "submitsReports", label: "Сдаёт отчёты"},
		{key: "mustChangePassword", label: "Нужна смена пароля"},
		{key: "countsTowardSelf", label: "Учитывать в «Себе»"},
		{key: "isCertificateSale", label: "Продажа сертификата"},
		{key: "selfOverridden", label: "«Себе» задано вручную"},
	} {
		b, bok := before[field.key]
		v, vok := after[field.key]
		if !bok && !vok || string(b) == string(v) {
			continue
		}
		format := func(raw json.RawMessage) string {
			if string(raw) == "true" {
				return "Да"
			}
			if string(raw) == "false" {
				return "Нет"
			}
			return "—"
		}
		p.Changes = append(p.Changes, views.MoneyRow{Name: field.label, Value: format(b) + " → " + format(v)})
	}
	if b, v := auditString(before, "role"), auditString(after, "role"); b != v {
		role := func(s string) string {
			switch store.Role(s) {
			case store.RoleAdmin:
				return "Администратор"
			case store.RoleEmployee:
				return "Сотрудник"
			}
			return "—"
		}
		p.Changes = append(p.Changes, views.MoneyRow{Name: "Роль", Value: role(b) + " → " + role(v)})
	}
	for _, field := range []auditField{
		{key: "displayName", label: "Имя"},
		{key: "name", label: "Название"},
		{key: "note", label: "Примечание"},
		{key: "voidReason", label: voidReasonLabel(row)},
	} {
		b, v := auditString(before, field.key), auditString(after, field.key)
		if b != v {
			p.Changes = append(p.Changes, views.MoneyRow{Name: field.label, Value: emptyDash(b) + " → " + emptyDash(v)})
		}
	}
	return p
}

func auditAmount(raw json.RawMessage) (domain.Cents, bool) {
	n, err := strconv.ParseInt(string(raw), 10, 64)
	return domain.Cents(n), err == nil
}
func auditMoney(c domain.Cents, ok bool) string {
	if !ok {
		return "—"
	}
	return domain.FormatEUR(c)
}
func emptyDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func auditBool(raw json.RawMessage) string {
	switch string(raw) {
	case "true":
		return "Да"
	case "false":
		return "Нет"
	default:
		return "—"
	}
}

func auditDestination(value string) string {
	switch domain.Destination(value) {
	case domain.ToKassaCard:
		return "Касса — карта салона"
	case domain.ToCash:
		return "Касса — наличные"
	case domain.Prepaid:
		return "Оплата сертификатом"
	default:
		return "—"
	}
}

func voidReasonLabel(row store.AuditEntry) string {
	if row.Entity == store.EntityReport {
		return "Причина удаления"
	}
	return "Причина отмены"
}
