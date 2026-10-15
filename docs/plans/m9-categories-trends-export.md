# Milestone 9 — Categories, Trends, Export

Adapted for the client-entry model.

## Task 1: Categories management (`/admin/categories`)
- Store: `Category.Version`, `Category.Deleted`.
- Methods: `UpdateCategory`, `SetCategoryActive` (archive), `DeleteCategory` (only if unused), `ReorderCategory` (adjacent swap).
- Preserve frozen history: editing category settings or flags never rewrites past reports/snapshots.
- Routes:
  - `GET /admin/categories?kind=revenue|payment|expense`
  - `GET /admin/categories/new?kind=`
  - `GET /admin/categories/{id}`
  - `POST /admin/categories`
  - `POST /admin/categories/{id}`
  - `POST /admin/categories/{id}/reorder`
  - `POST /admin/categories/{id}/archive`
  - `POST /admin/categories/{id}/delete`
- UI: daisyUI tabs (Расходы, Выручка, Оплата), cards on phone with order buttons and edit sheet, 375 pt check in light/dark.
- Seams: `internal/store/categories_test.go`, `internal/web/categories_test.go`.

## Task 2: Trends (`/admin/trends`)
- Aggregate across 3, 6, 12 months ending in selected month (defaults to current salon month).
- Data sources:
  - Closed months: from `months/{YYYY-MM}` snapshot (`MonthResult`).
  - Open months: live preview via `domain.PreviewMonth`.
  - Imported months: from `importedMonths/{YYYY-MM}`.
- Metrics: total revenue, per-person revenue, services, creams, certificates, «Себе», salon income, expenses, profit, dividends, client count.
- Server-rendered SVG chart (responsive `viewBox`, CSP-compliant, no JS chart lib) + data table (cards on mobile, table on desktop `lg:`).
- Seams: `internal/domain/aggregate_test.go`, `internal/web/trends_test.go`.

## Task 3: Export (`/admin/export`)
- `GET /admin/export`: date range (from/to, default current month, up to 12 months), format (XLSX, CSV).
- `GET /admin/export/download`: binary download with `Content-Disposition: attachment; filename=...`.
- `internal/export`:
  - Pure Go XLSX via `github.com/xuri/excelize/v2`: sheets «Записи», «Движения», «По сотрудникам», «Итоги месяца», «По месяцам».
  - CSV: UTF-8 BOM, `;` delimiter, decimal comma, formula injection protection (`=`, `+`, `-`, `@`).
- Seams: `internal/export/export_test.go`, `internal/web/export_test.go`.

## Verification & Done when
- `make ci` green.
- Categories add/edit/archive/reorder works on phone viewports (375 pt light/dark).
- Trends SVG chart fits 375 pt without horizontal scroll.
- XLSX and CSV open cleanly with valid headers and formatted numbers.
