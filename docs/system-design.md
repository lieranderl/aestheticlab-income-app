# Aesthetic Lab Income App — System Design

Companion to [technical-solution.md](technical-solution.md), which holds the stack, business rules, money model, security and backups. The build order is in [implementation-plan.md](implementation-plan.md).

## 1. Architecture

```
iPhone PWA / desktop browser
   │  HTTPS (HTML + Datastar SSE patches, cookie session)
   ▼
Cloud Run: aestheticlab-income (Go binary, distroless, 0–2 instances)
   ├─ web     handlers, auth middleware, SSE
   ├─ domain  money, report calc, balances, settlement, aggregation   ← pure, fully unit-tested
   ├─ store   interface → Firestore impl / in-memory impl for tests
   └─ export  CSV, XLSX
   │  gRPC, runtime service account
   ▼
Firestore named DB "aestheticlab-income" (PITR + scheduled backups)
   ▲
Cloud Scheduler ──(monthly exportDocuments)──► GCS backup bucket
Secret Manager: INCOME_SESSION_KEY
```

## Client-entry update (2026-10-13)

New web records are per client rather than per day. Existing daily reports and old closed snapshots keep their values; legacy web routes only correct existing daily rows.

- New IDs: `reports/client_{uid}_{YYYY-MM-DD}_{token}`. Fields `clientToken`, `clientNumber`, `clientInput` distinguish new records; absent fields mean legacy. `clientInput` stores services (including redeemed amount), cream/oil/certificate sales, extra destination, service cash/card contributions and certificate redemption. Derived report fields/flags use the shared domain money engine.
- `clientDays/{uid}_{YYYY-MM-DD}` stores `last` ordinal. Creation reads/updates this counter and writes the report/audit in one transaction. Unchanged version0 token replay acknowledges only the first version1 record; changed replay/stale edits are refused. Number and createdAt never change on edit.
- Snapshots persist client count, report kassaCash/kassaCard and per-person revenueByCategory. `statsVersion=1` means these fields are available; absent/0 in old snapshots means unknown, which the UI hides. Actual cash/card statistics exclude the virtual prepaid redemption credit.
- Home aggregates employee/day entries; admin day aggregates all entries and by person, then shows individual clients. Month/my/history include client labels/time and totals. Legacy daily rows are labelled separately and do not manufacture a client count.
- GET `/clients/new?date=`, POST `/clients`, GET/POST `/clients/{id}`: own creation, own/admin correction. GET/POST `/clients/{id}/remove`: the owner (while submitting reports) or the admin removes a client entered by mistake, in an open month, with a required reason and the current version. The record is voided, never deleted (`voided`, `voidReason`, version+1, audit «Удаление» in the same transaction); every report read skips it, so day/month totals, kassa, balances, settlement revision, close snapshots, history and exports exclude it. Its number is never reused, its create token cannot restore it, and a voided record no longer blocks new terms. GET `/reports/day?date=`: existing own legacy daily report. Existing POST `/reports/{date}` and admin report routes refuse new legacy creation.
- New-client drafts are scoped by person/date/new entry and retain the creation token through reload/lost response. Saved-entry drafts use the stable report ID/version. Success clears only the matching create token. Financial requests never auto-retry.
- Oil is added as an audited revenue category on its first client sale if absent, with first use recorded in the same transaction. Existing category settings and saved flags are not rewritten.

## 2. Data model (Firestore)

All amounts are `int64` cents. Percentages are basis points (5000 = 50%).

| Collection / doc ID | Fields | Notes |
|---|---|---|
| `users/{uid}` | `username` (unique, lowercase), `displayName`, `role` (`admin` \| `employee`), `submitsReports`, `passwordHash`, `mustChangePassword`, `sessionVersion`, `active` (may log in), `failedLogins`, `lockedUntil`, `createdAt`, `updatedAt` | Usernames stay unique through a `usernames/{username}` → `{uid}` doc written in the same transaction. Money terms are **not** stored here; see `terms`. |
| `users/{uid}/terms/{YYYY-MM}` | `selfPct`, `salaryCents` (monthly), `dividendWeight` (int, 0 = none), `highSelfFromCents` + `highSelfPct` (optional: the whole month's «Себе» rate when the month's «Себе» base is above the threshold; 0 = none), `setBy`, `setAt`, `note` | **Effective-dated money terms.** The doc ID is the first month the terms apply. The terms in force for month `m` are the doc with the greatest ID ≤ `m`. Terms for a person can only start in an open month when that person has **no reports in any month the new terms would cover** (from that month up to their next terms), normally next month. A month therefore never mixes two rates, and a closed month's terms never change. Before the first report, the start month's terms are entered once at setup. Leaving the salon = new terms with salary 0 and weight 0 from that month. |
| `categories/{id}` | `name`, `kind` (`revenue` \| `payment` \| `expense`), `order`, `active`, `usedCount`, `createdAt`. Revenue only: `countsTowardSelf`, `isCertificateSale`. Payment only: `destination` (`kassa_card` \| `cash` \| `prepaid`). | Default lists are in technical-solution §9. |
| `accounts/{id}` | `name`, `kind` (`cash` \| `card`), `order`, `active`, `lastRecountAt`, `lastRecountBy` | Seeded with `kassa_cash` «Касса — наличные» and `kassa_card` «Касса — карта салона». |
| `reports/{uid}_{YYYY-MM-DD}` | `userId`, `date`, `month`, `revenue` {catId: cents}, `payments` {methodId: cents}, `cashToKassa`, `transferToCard`, plus values the server computes: `total`, `selfBase`, `selfPct`, `selfAmount`, `selfOverridden`, `salonShare`, `certificateSales`, `salonIncome` (= `salonShare − certificateSales`), `cardIn`, `prepaid`, `kassaCredit`, `balanceDelta`. Also `version`, `createdAt`, `updatedAt`, `updatedBy`. | Computed values and category flags are saved at the moment the report is saved. Zero amounts are left out of the maps. |
| `ledger/{autoId}` | `date`, `month`, `type` (`deposit` \| `payout` \| `expense` \| `transfer` \| `adjustment` \| `opening` \| `salary` \| `self_bonus` \| `dividend`), `amount`, `from`, `to` (`account:<id>` \| `person:<uid>` \| `outside` \| `salon` \| `certificates`), `personId`, `expenseCategoryId`, `note`, `status` (`pending` \| `confirmed` \| `voided`), `voidReason`, `createdBy`, `confirmedBy`, `createdAt`, `version` | One row per movement of money. Rows are never deleted: a mistake is voided instead. Only `confirmed` rows count toward balances. |
| `months/{YYYY-MM}` | `status` (`open` \| `closed`), `closedBy`, `closedAt`, `reopenedBy`, `reopenedAt`, `revision`, `snapshot` | A missing doc means the month is open. Revision defaults to zero on older records and increases on every close/reopen to reject stale confirmation cycles. `snapshot` is written at close: the monthly result (technical-solution §5.3), closing balances per person and per account, outstanding certificates. |
| `importedMonths/{YYYY-MM}` | `revenueByPerson` {uid: cents}, `revenueTotal`, `salonIncome`, `expenses`, `managerSalary`, `profit`, `dividends`, `source` | Read-only history from the Google Sheet (April–September 2026, the months before the start month), loaded in M10. Used only by trends and the month view, never by balances. |
| `audit/{id}` | `entity` (`report` \| `ledger` \| `category` \| `account` \| `user` \| `terms` \| `month` \| `importedMonth`), `entityId`, `action`, `actorId`, `at`, `before`, `after` (the document as JSON; never a password hash) | Append-only change log. The ID starts with the time, so IDs sort in the order rows were written. |

### Example: the spec's day for a 50% master
```
reports/anna_2026-10-08
  revenue:  { services: 60000, creams: 5000, certificates: 10000 }   # certificates = sale, not split
  payments: { card: 60000, cash: 15000 }
  cashToKassa: 0                      # suggested 0, because the card already covers the salon's share
  total: 75000   selfBase: 65000   selfPct: 5000   selfAmount: 32500
  salonShare: 42500   certificateSales: 10000   salonIncome: 32500
  cardIn: 60000   prepaid: 0   kassaCredit: 60000   balanceDelta: +17500   # the salon owes Anna €175
```

### Composite indexes (`firestore.indexes.json`)
- `reports(userId ASC, date ASC)`
- `reports(userId ASC, month ASC)`
- `ledger(month ASC, date ASC)`
- `ledger(personId ASC, month ASC)`
- `ledger(status ASC, createdAt ASC)`
- `audit(entityId ASC, at DESC)`

## 3. Write paths

Every write path is one Firestore transaction that also appends an `audit` doc.

### 3.1 Save a daily report
1. Read `months/{month}`. If the month is closed, reject for **everyone, the admin included** (`ErrMonthClosed`, «Месяц закрыт — сначала откройте его заново»). Dates before the start month `S` (`INCOME_START_MONTH`; `2026-11` for real use, `2026-10` for October practice on the main URL) are rejected too.
2. Read the existing report. If its `version` differs from the version the form was loaded with, reject with «Запись изменена другим пользователем — обновите страницу».
3. Read the current categories and the person's `selfPct` **from the terms in force for the report's month** (`users/{uid}/terms`, greatest ID ≤ month).
4. Validate:
   - amounts ≥ 0;
   - Σ payments = Total;
   - `cashToKassa`, `transferToCard` ≥ 0 and `cashToKassa + transferToCard` ≤ cash;
   - «Себе» override between 0 and Total.
5. Compute all derived fields (technical-solution §4).
6. Write the report with `version + 1`, and increment `usedCount` on newly used categories.

### 3.2 Record a movement (`ledger`)
- **Who can record what:**
  - The admin can record any type. It is `confirmed` immediately.
  - A person can record two types, `deposit` and `expense` (paid by them), with `from = person:<self>`. These are `pending`.
- **The movement's month must be open, for everyone.** Settlement payouts and deposits for a closed month are dated when the money actually moves, which is always in an open month, so no exception is needed.
- **Go-live cutoff:** a movement's date must be on or after the first day of the start month `S`, the same rule as reports. `opening` rows are dated exactly on that day (2026-10-01 in production since the 2026-10-14 decision). Without this, a missing `months/{YYYY-MM}` doc would count as open and let pre-go-live rows in.
- **«Подтвердить»** (admin): `pending` → `confirmed`.
- **«Отменить»** (admin, or the author while the row is still pending): status becomes `voided`, with a reason.

### 3.3 Kassa recount
1. The admin enters the counted cash, or the card account's bank balance. Today must be in an open month on or after the start month `S`.
2. The server works out the expected balance (§4.1).
3. If they differ, it writes an `adjustment` row (note required) for the difference.

### 3.4 Close a month
0. **Order:** the month must be over (closing the running month would lock its remaining days), and the month before must already be closed (except the start month `S`). Otherwise reject with «Сначала закройте предыдущий месяц». So closed months always form an unbroken run from `S`, and every open month comes after the last closed one.
1. **Pre-checks:**
   - **Blocking:** no `pending` movements dated in the month. The admin must confirm or void each one first. After close they could no longer be resolved, and the snapshot would silently leave them out.
   - **Warnings only:** days with no report for people who submit reports; the last kassa recount is older than 7 days.
2. **Read the terms in force for `m`** for every person (`users/{uid}/terms`, greatest ID ≤ `m`). Everyone with money in the month (a report, a non-voided movement or a carried balance) must have terms, or the close is refused; someone without a share gets terms of 0. Never read today's values, so reopening and closing November again after December terms changed gives the same salary and dividends.
3. **Compute the monthly result:**
   - salon income, expenses, salaries;
   - each person's «Себе» bonus: when their month's «Себе» base is above `highSelfFromCents`, `round(base × (highSelfPct − selfPct))`;
   - profit (after salaries and bonuses), and dividends split by `dividendWeight` using largest-remainder cents.
4. **Write the accruals:** one `salary` row (salary > 0), one `self_bonus` row (bonus > 0) and one `dividend` row (weight > 0) per person, using those terms, dated the last day of the month, status `confirmed`.
5. **Compute closing balances as of the end of month `m` only:** start from month `m−1`'s snapshot (or zero plus the `opening` rows when `m` is `S`), then apply reports and `confirmed` ledger rows **with `month == m`**, including the accruals from step 4. The snapshot also records the terms used, for display and audit. Later open months (for example December entries made before November is closed) are never part of `m`'s snapshot; they stay open-month activity in §4.1. Then write `months/{m}` with `status = closed` and the `snapshot`.

### 3.5 Reopen a month
0. **Only the most recently closed month** can be reopened. If a later month is closed, reject with «Сначала откройте заново <следующий месяц>». To fix an older month, reopen newest-first, correct it, then close again oldest-first.
1. Void the month's `salary`, `self_bonus` and `dividend` rows (reason «месяц открыт заново»).
2. Remove the `snapshot` and set `status = open`.

Because only the newest closed month can change, no remaining snapshot ever depended on it. Snapshots are never stale, and balance reads (§4.1) can always trust the latest one.

## 4. Reads and aggregation

Each request reads categories, accounts and users once and keeps them in memory for that request. All totals are computed in Go.

### 4.1 Balances
- **Starting point:** the `snapshot` of the most recent closed month, taken as the opening balance. If no month is closed yet, start from zero and apply the `opening` rows (dated the first day of `S`). Closing and reopening only in order (§3.4–3.5) guarantees this snapshot is current, and because a snapshot only contains its own month (§3.4 step 4), every later month is applied exactly once.
- **Then add the open months since:**
  - **Person's balance:** + Σ `balanceDelta` from their reports, then apply their `confirmed` ledger rows (credits in, debits out).
  - **Account balance:**
    - `kassa_card` += Σ `cardIn` + Σ `transferToCard` from reports.
    - `kassa_cash` += Σ `cashToKassa` from reports.
    - Apply the `confirmed` ledger rows to/from the account.
- **Cost of a read:** at most ~2 months of reports plus ledger rows, so a few hundred docs.

### 4.2 Day report (spec §5)
- **Query:** `reports where date == D`, plus that day's ledger rows.
- **Shows:**
  - Salon totals for the day.
  - Per person: revenue, «Себе», «Салону», card, cash and cash left in the kassa.
  - Who hasn't reported yet.

### 4.3 Month report and settlement (spec §6 and the sheet's Debit Credit tab)
- **Query:** `reports where month == M` and `ledger where month == M`. A closed month reads its `snapshot`. For April–September 2026 (once imported in M10), `importedMonths/{M}` is shown, labelled «импорт».
- **Shows:**
  - **Revenue:** totals by category, by payment method and by person, plus working days.
  - **Salon result:** salon income, expenses by category, manager salary, profit, dividends.
  - **Settlement, per person:**
    - opening balance;
    - kassa credit from reports;
    - deposits;
    - expenses they paid;
    - salary, «Себе» bonus and dividend;
    - payouts;
    - closing balance and the suggested action.

**Missing-report warnings:** read audited staff history once through the last elapsed selected day, then derive each day’s expected reporters in memory. Confirmation submission calls the guarded write directly; warning/payment-card assembly is not part of the write path.

**Presentation and payment context:** an open result is explicitly provisional; closed result and terms come from the saved snapshot. Salary, «Себе» bonus and dividend appear on an employee’s page only after close, through a read that returns only their own result. Settlement cards show the selected-month closing figure separately from the current realized balance. Payment suggestions use the current balance and a per-person source fingerprint checked in the movement transaction; a later payment updates the suggestion without rewriting the old snapshot. Amounts are editable up to the current remaining balance; both payout and deposit ceilings are checked inside the movement transaction, while partial payments are allowed. Movements are dated in an open month when money actually moves. «Перенести» acknowledges carry-forward without a movement. Financial requests never automatically retry.

### 4.4 Trends (spec §7 and the sheet's Trend Dashboard tab)
- **Window:** 3, 6 or 12 months. Closed months come from their snapshots, open months are worked out live, and April–September 2026 come from `importedMonths` once M10 has loaded them.
- **Series:**
  - total revenue;
  - revenue per person;
  - creams;
  - certificates;
  - «Себе»;
  - salon income;
  - expenses;
  - profit;
  - dividends.

### 4.5 History (spec §9)
- **Reports:** filter by period, person and category (the category filter is applied in Go). Paged 50 at a time.
- **Movements:** a «Движения» tab with the same period and person filters, plus type and status.

### 4.6 Export (spec §10)
- **XLSX sheets:**
  - «Записи»: one row per report, a column per category and per payment method.
  - «Движения»: kassa movements.
  - «По сотрудникам»: totals per person.
  - «Итоги месяца»: monthly result and settlement.
  - «По месяцам»: totals per month.
- **CSV:** «Записи» or «Движения», chosen in the form.

## 5. Routes and screens

- **Changes** are `POST` forms that work without JS. Datastar enhances them (`@post` with `contentType: 'form'`). The server answers a Datastar request with SSE patches: the form patched back with its errors, or a `redirectUrl` signal on success. A plain post gets the page again (422), or a 303.
- **Pages** are plain `GET`s.

| Route | Who | Purpose |
|---|---|---|
| `GET /login`, `POST /login`, `POST /logout` | all | Log in. |
| `GET /` | people with `submitsReports` | **Daily form** (§6.3). |
| `POST /reports/{date}` | owner | Save a report. |
| `GET` / `POST /clients/{id}/remove` | owner, admin | Remove a client entered by mistake (voided with a reason, open month only). |
| `GET /my?m=` | all | «Мои записи»: balance card, month earnings, reports, movements. |
| `GET /movements/new`, `POST /movements`, `GET` / `POST /movements/{id}/void` | all | Own deposit/expense (pending for staff); staff can cancel only their own pending movement. Admin can cancel non-accrual movements in open months. |
| `GET /profile`, `POST /profile/password` | all | Change own password. Forced at first login. Logs out every other device. |
| `GET /more` | admin | «Ещё»: the screens that don't fit the bottom bar, and «Выйти». |
| `GET /admin/day?date=` | admin | Day report. |
| `GET /admin/month?m=` | admin | Month: «Выручка» and «Итоги и расчёт» tabs, with close/reopen. |
| `GET` / `POST /admin/month/{month}/close`, `GET` / `POST /admin/month/{month}/reopen` | admin | Native confirmation sheets and plain pages; submitted source fingerprint and month revision checked inside the audited transaction. |
| `GET /admin/kassa` | admin | Kassa balances, outstanding certificates, latest movements, pending movements to confirm. |
| `POST /admin/movements`, `POST /admin/movements/{id}/confirm` | admin | Record, confirm or void a movement: payout, deposit, expense, transfer. |
| `GET` / `POST /admin/accounts/{id}/recount` | admin | Recount the selected account, with a native sheet and plain page. |
| `GET /admin/trends?period=3\|6\|12&metric=` | admin | Trends. |
| `GET /admin/history?…`, `GET /admin/history/movements?…` | admin | Report/movement history with filters. |
| `GET /admin/reports/{uid}/{date}`, `POST …` | admin | Edit anyone's report using their effective terms, with version and closed-month checks. |
| `GET /admin/export?…` | admin | Download an export. |
| `GET/POST/PUT/DELETE /admin/categories…` | admin | Revenue, payment and expense categories, with their flags and destinations. |
| `GET /admin/users`, `GET /admin/users/new`, `POST /admin/users`, `GET /admin/users/{uid}`, `POST /admin/users/{uid}`, `POST /admin/users/{uid}/password`, `POST /admin/users/{uid}/terms` | admin | Staff: add, edit name, role, `submitsReports` and access (deactivating logs out everywhere), reset the password (temporary, logs out everywhere, unlocks), and effective-dated terms («Себе» %, salary, dividend share). |
| `GET /admin/audit?…` | admin | Change log. |
| `GET /readyz` | public | Health check that pings Firestore. |
| `/static/*`, `/manifest.webmanifest`, `/sw.js` | public | Static assets and PWA files. |

The same routes serve phone and desktop; each template is laid out for the phone first (§6).

## 6. Mobile-first UI

The phone is the main device for **everyone**, the admin included. Every screen is designed at **375 pt wide** first; desktop is the same page with more room.

### 6.1 Principles
- **One column on phones.** Wide tables only from `lg` (≥ 1024 px); below that, cards.
- **Thumb reach.** Navigation and the main action sit at the bottom.
- **Touch targets:** at least 44 × 44 pt, at least 8 pt apart. No hover-only interactions.
- **Text size:** inputs use at least 16 px text, so iOS doesn't zoom in. Pinch-zoom stays on.
- **Safe areas:** `viewport-fit=cover` and `env(safe-area-inset-bottom)` padding.
- **No drag and drop:** reordering uses ↑ ↓ buttons, and editing opens a bottom sheet (native `<dialog>`).
- **Themes:** light and dark brand themes, following the phone's system setting.
- **Orientation:** portrait first; landscape must not break.

### 6.1a Information clarity contract (2026-10-13)

These UX requirements are implemented locally; before/after screenshots, checks and remaining device verification are recorded in [the mobile audit](reviews/2026-10-13-mobile-ui/REVIEW.md). See `progress.md` for deployment status.

- **Entry, help and results are separate.** Inputs use visible short labels with units and required/optional status. Essential constraints stay visible and are associated with their field; examples and longer explanations live in a separate «Как заполнить» disclosure. No essential instruction depends on hover tooltips or placeholder text.
- **Client entry follows the money meaning.** Group services and optional service certificate redemption together; group additional sales and their kassa destination separately; show actual service contributions in their own block. Finish with a read-only result. Redemption is already included in services; extras are not included in service contribution inputs.
- **Invalid values are not financial results.** Preserve entered values, show errors by the relevant field, and make the first error reachable. Mark affected previews incomplete until inputs are valid; authoritative calculations and save validation remain on the server.
- **Dashboard order:** selected period, primary totals, grouped breakdown, employee comparison, underlying records. Revenue, «Себе», «Салону» and client count lead; category detail does not compete equally or repeat across every section. Phone employee cards show comparable summary fields before expandable details; desktop tables use consistent columns.
- **Financial scopes are explicit.** Day/month cash/card contributions are labelled as «Внесено в кассу» for that period, separately from current account balances. Current balances carry an as-of date. Open-month projections and frozen closing figures are labelled separately from current realized balances; payout/deposit actions show the suggested amount from the current balance.
- **Help follows the task.** Required expense comments are labelled only for expenses. Warnings state what to check next in a distinct block. No inferred opening balances, changed money rules or invented statistics for old snapshots.
- **Verification:** 375 pt in both themes, real-phone keyboard/Next/save, optional sections and drafts, field-error recovery, service/extra/certificate examples, current-versus-frozen balances, employee/admin access and readable person comparison. Screenshots alone do not prove completion speed or accessibility compliance.

### 6.2 Navigation
A bottom tab bar (daisyUI `dock`) holds at most 5 items. On desktop the same items move to a sidebar.

| Role | Bottom bar |
|---|---|
| Employee | Сегодня · Мои записи · Профиль |
| Admin who submits reports (Anna) | Отчёт · День · Месяц · Касса · Ещё |
| Admin who doesn't submit reports | День · Месяц · Касса · История · Ещё |

«Ещё» opens a list: Динамика, История, Мои записи, Категории, Сотрудники, Журнал изменений, Экспорт, Профиль, Выйти.

Dates switch with «‹ 08.10.2026 ›» (tap the date for the native picker). Months switch with «‹ Октябрь 2026 ›».

### 6.3 Daily form (target: under 60 seconds)
- **Layout:** one screen, in order:
  1. date;
  2. revenue categories;
  3. payments;
  4. **«Оставьте в кассе»** (only when cash > 0);
  5. «Себе» / «Салону».

  A bar pinned at the bottom shows the live Total and **Сохранить**.
- **Inputs:**
  - `inputmode="decimal"` and `enterkeyhint="next"`.
  - Each row is full width; the first field gets focus. Empty counts as 0.
- **Live calculation:** Datastar computed signals (`r_<categoryId>`, `p_<methodId>`) give the Total, «Себе» and «Салону».
- **Cash fills itself in** as Total − card − certificate, until the person edits it by hand.
- **«Оставьте в кассе»** (подсказка €X, the amount that settles the day, technical-solution §4), in two fields:
  - «Наличными в кассу»: empty means the suggestion less any transfer, which the field shows as its placeholder; a typed amount (0 included) is kept as typed;
  - «Переводом на карту салона», empty (0) unless the master transfers the salon's part to the salon's card.
  - Underneath, one line explains the effect in plain words: «Остальные €150 наличными — ваши. Салон должен вам €175» or «Расчёт за день: 0».
- **«Себе» / «Салону»:** shown already worked out, for example «Себе €325 (50%) · Салону €425». The «изменить» toggle lets the person override «Себе».
- **Keyboard:** the pinned bar uses `position: sticky`, so the iOS keyboard never covers it.
- **Saving:**
  - Errors from the server are patched in next to the fields.
  - Success shows a «Сохранено ✓» toast and switches the form to edit mode.
  - A closed month shows a read-only banner.

### 6.4 «Мои записи» (everyone)
- **Balance card on top:** «Салон должен вам €X» or «Вы должны салону €X», with the date the balance was last changed.
- **This month:** revenue, «Себе», working days. Salary and dividend appear once the month is closed.
- **Two buttons open a bottom sheet:**
  - **«+ Внести в кассу»** (deposit): amount, account, note.
  - **«+ Расход за свой счёт»** (own-paid expense): amount, category, note.

  Both show «ожидает подтверждения» until the admin confirms them.
- **Below:** a list of the person's reports and movements, as cards.

### 6.5 Admin screens on a phone
| Screen | Phone layout | Desktop (`lg` and up) |
|---|---|---|
| День | Salon card (revenue, «Салону», «Себе», card / cash / kassa). One card per person, which expands to show details and «Изменить». A «Не сдали отчёт» list. | Table of people × categories. |
| Месяц → «Выручка» | 2-column figure cards (revenue, «Себе», «Салону», working days). Lists by category and payment method. Cards per person. | Tables. |
| Месяц → «Итоги и расчёт» | **Result card:** salon income − expenses − salary − «Себе» bonuses = profit → dividends. **Expense list** by category. **Settlement cards**, one per person: closing balance in large type, then «Выплатить €X» / «Получено €X» / «Перенести». Tapping a card shows the full breakdown: opening, reports, deposits, expenses, salary, «Себе» bonus, dividend, payouts. **Pre-checks**, then the «Закрыть месяц» button with a confirmation dialog. | Tables side by side. |
| Касса | **Two balance cards:** «Наличные» and «Карта Анны», each with «Пересчитать». An «Ожидают подтверждения» list with a ✓ button per item. A **«+ Движение»** sheet: type, amount, from/to, category, note. **Latest movements** as cards. **Outstanding certificates.** | Same, in two columns. |
| Динамика | 3 / 6 / 12 segmented control. Metric chips; «Обзор» (default) shows Выручка, Расходы and Прибыль side by side. A server-drawn SVG bar chart scaled by `viewBox`, with a € (or count) axis that always includes €0. The selected month's exact figures sit above the chart: the latest month with data by default, changed with ‹ › or by tapping a month (a local Datastar signal; no custom JS). Imported months are drawn lighter. A table underneath. | Wider chart. |
| История | «Записи» / «Движения» tabs, filters in a bottom sheet, chips for active filters, «Показать ещё». | Table. |
| Категории | Three tabs: «Выручка», «Оплата», «Расходы». Bottom-sheet editing, ↑ ↓ reordering. | Same. |
| Сотрудники | A list. The bottom-sheet edit has: role, reports on/off, can log in, password reset, and **«Условия»**: «Себе» %, optional higher «Себе» (threshold € and the whole month's %), salary, «Доля в прибыли» (a whole number 0–1000; the profit is split in proportion to everyone's shares), plus «Действует с» (defaults to next month; the earliest allowed month is the first open month with no reports from that person), with the history of earlier terms underneath. As the share is typed, the form names everyone else's shares in force in the chosen month and says what this one gets: «Получит 33,3% прибыли месяца, остальные — 66,7%». | Same. |
| Журнал изменений, Экспорт | Cards / a single form with a large «Скачать» button. | Table / same. |

**Exports on iPhone:** files are served with `Content-Disposition: attachment` and an ASCII file name. Safari's preview offers Share → «Сохранить в Файлы».

### 6.6 Weak or missing connection
- **Daily form drafts:** `static/draft.js` keeps the unsaved form in `localStorage` per user and date, and clears it after a successful save.
- **Failed save:** the values stay on screen with a «Нет сети — данные сохранены на телефоне, нажмите ещё раз» message.
- **Money figures are never cached**, so nobody sees stale amounts.

### 6.7 Speed
- **Target:** under 1.5 s from opening the app to a usable form, on 4G.
- **What the phone loads:** Datastar (~11 KB gzipped), one CSS file (≤ ~30 KB gzipped), and one font (Onest, Cyrillic and Latin only, `font-display: swap`).
- **Compression:** Cloud Run does not compress, so the app gzips CSS/JS/SVG/manifest once at start-up and serves them gzipped to browsers that accept it.
- **No chart library and no framework** are sent to the phone.
- **Checked in CI** (`e2e/speed.spec.ts`, Chromium, empty cache, Lighthouse slow 4G: 150 ms RTT, 1.6 Mbit/s): sign-in and new-client forms usable under 1.5 s; CSS ≤ 30 KB, JS ≤ 20 KB, fonts ≤ 60 KB over the wire.
- **Cold starts:** under 1 s with min 0 instances. Setting min 1 instance costs a few euros a month if that is ever needed.

## 7. PWA

- **Install hint:** shown once at first login. On iPhone it's Share → «На экран Домой»; on Android, the browser's install prompt.
- **Manifest:** «Aesthetic Lab — Касса», short name «Касса», standalone, `start_url: /`, `theme_color #8b9687`, `background_color #fffaf3`, 192 and 512 px icons plus maskable versions.
- **iOS:**
  - `apple-touch-icon` and the `apple-mobile-web-app-*` meta tags.
  - The installed app has its own cookies, so each person logs in once inside it.
  - The login cookie is set by the server, so Safari's 7-day limit on storage written by scripts doesn't apply to it.
- **`sw.js`:**
  - Caches versioned static assets only.
  - HTML and data always come from the network.
  - Offline, it shows an «Нет соединения» page.

## 8. Code layout

The layout mirrors datastar-webapp:

```
cmd/web/            main: config, Firestore client, server, graceful shutdown
cmd/seed/           one-off: admin user, default categories, kassa accounts (run locally with ADC)
cmd/import/         one-off: sheet history → importedMonths; opening balances → ledger `opening` rows
internal/domain/    money, report calc & validation, balances, settlement & dividends, aggregation (pure)
internal/store/     Store: every rule and transaction once (store.go, users, catalog, reports, ledger, months, audit),
                    over a tiny backend (db.go): firestore.go in production, memory.go for local runs and HTTP tests
internal/web/       server.go (routes), auth.go / session.go, handlers per screen, middleware
internal/views/     *.templ (layout, form, admin pages, SVG charts), icons_gen.go
internal/export/    csv.go, xlsx.go
frontend/           app.css (Tailwind + daisyUI theme), icons.mjs
static/             built CSS, datastar.js, draft.js, font, icons, manifest, sw.js (go:embed)
docs/               technical-solution.md, system-design.md, implementation-plan.md, MAINTAINING.md
Dockerfile, Makefile, .air.toml, .env.web.example, firestore.indexes.json
.github/workflows/  ci.yml, web.yml
```

- **Strings:** Russian, written directly in the templ files.
- **`ru` helper:** month names, plural forms, dates.
- **`cmd/import` input:** CSV files exported from the sheet, passed as file paths at run time. They are **never committed**.

**What comes from datastar-webapp:**
- the HMAC signer (`internal/web/auth.go`);
- the bcrypt accounts pattern (`internal/auth/accounts.go`);
- the middleware chain (`internal/handlers/handlers.go`);
- the store interface with in-memory and Firestore implementations (`internal/store`);
- `Dockerfile.web`;
- the Makefile targets;
- `.github/workflows/web.yml` and `ci.yml`.

## 9. Infrastructure (one-time setup, scripted in `docs/MAINTAINING.md`)

1. **Firestore database `aestheticlab-income`:**
   - Native mode, europe-west1.
   - Point-in-time recovery and delete protection on.
   - Daily backups (kept 7 days) and weekly backups (kept 14 weeks).
   - Indexes from `firestore.indexes.json`.
2. **Service accounts:**
   - `aestheticlab-income-runtime@`: `datastore.user` with the database condition, plus `secretAccessor`.
   - `aestheticlab-income-deployer@`: `run.developer`, `artifactregistry.writer`, and `iam.serviceAccountUser` on the runtime account.
3. **GitHub access:** a Workload Identity provider for `lieranderl/aestheticlab-income-app` in the existing `aestheticlab-github` pool («Aesthetic Lab GitHub Actions»).
4. **Secret Manager:** `INCOME_SESSION_KEY`.
5. **Backups bucket:** `<project>-income-backups` (3-year lifecycle), plus a monthly Cloud Scheduler export job (OAuth token, dedicated `aestheticlab-income-backup@` account).
   - **Image repository:** a separate Artifact Registry repo `aestheticlab-income`, so the deployer can only push this app's images.
6. **Cloud Run `aestheticlab-income`:** 1 vCPU, 512 MiB, 0–2 instances, concurrency 80, gen2, CPU throttling on.
7. **Domain `<app-host>`:** a Cloud Run domain mapping, plus a CNAME `kassa → ghs.googlehosted.com.` at DNS. Google manages the TLS certificate.
8. **Monitoring:** alerts on 5xx errors and failed `/readyz` checks, and a €5/month budget alert.
9. **October testing — production only (decision 2026-10-12):**
   - Use `<app-host>`, Cloud Run `aestheticlab-income` and the main database `aestheticlab-income`, with start month `2026-10` during testing. Preserve existing accounts/settings and November/December terms; add October-only terms as needed.
   - Trial service/database/account/secret and temporary IAM grants were removed after verified exports. Never recreate or deploy a trial environment.
   - **Decision 2026-10-14:** October 2026 is the first real month, run in parallel with the sheet, with openings dated 2026-10-01 and the start month kept at `2026-10`. The October practice entries were already voided and stay as history; no cleanup or cut-off change (docs/MAINTAINING.md → Real October).

## 10. Testing strategy

### Domain (unit tests, table-driven)
- **Money:** parsing and formatting.
- **Daily report:**
  - Total and the payment check.
  - «Себе» base, excluding certificate sales; the %; override bounds.
  - Suggested `cashToKassa`; `balanceDelta`.
  - Invariant: **cash kept + `balanceDelta` = «Себе»**.
- **Spec example** (a 50% master): «Себе» 325, «Салону» 425, suggested deposit 0, balance +175.
- **Certificates:** selling one by card gives a balance change of 0. A €100 service paid by certificate at 70% gives the salon owing the master €70. The salon is paid out once per euro.
- **Settlement, reproducing a reference month laid out like the sheet:**
  - Fictional inputs in the sheet's layout: revenue, salon share, amounts returned, staff-paid expenses, salary, already paid.
  - Expected: dividends 3 × €596.28 (+1 cent to one owner by largest remainder); balances Maria +96.93, Olga +333.28, Vera −88.50, Anna +489.78 (±1 cent).
- **Dividends:** a loss month pays none; the parts always add up exactly to the total.
- **Balances:** combining snapshots with open months; voided and pending rows are ignored.
- **Outstanding certificates:** opening 500 + sale 100 − redemption 150 = 450. A redemption with no opening amount and no sale shows the «Проверьте сертификаты» warning (negative outstanding).

### HTTP (`httptest` with the in-memory store)
- Login, lockout, session invalidation.
- Role isolation: a person can't reach other people's reports, movements or balance, or `/admin`.
- A person's movements stay pending until confirmed.
- A closed month rejects writes from everyone, the admin included.
- Months close only in order. Only the newest closed month reopens.
- Closing November while December already has reports and movements: November's snapshot excludes December, and current balances count December exactly once.
- Closing is rejected while a pending movement is dated in the month.
- Movements dated before the start month are rejected.
- **Settings history:** change Anna's salary to €1,200 from December, then reopen and close November again. November still accrues €1,000. Terms can't start in a closed month, or in a month where the person already has reports (rejected with «Условия можно изменить со следующего месяца»).
- **Start month is configuration, used everywhere:** with `INCOME_START_MONTH=2026-10` (October tests on production), October reports and movements are accepted, October is the first month that can close without a previous one, and openings are dated 2026-10-01. With the production default `2026-11`, the same October writes are rejected. No code hard-codes a month.
- Version conflicts are rejected.
- Close → reopen → close gives the same result.

### Store (Firestore emulator via `make firestore`)
- Transactions for report save, movement, close and reopen.
- Audit writes.
- Indexes.

### Export
- Reopen the XLSX with excelize and check every sheet.
- CSV: BOM and delimiter.

### Mobile (Playwright with Bun)
- Profiles: iPhone SE, iPhone 15, Pixel 7.
- Every screen: no sideways scroll at 375 pt, the bottom bar and main button can be tapped, inputs ≥ 16 px.
- The daily form, the movement sheet and the settlement «Выплатить» flow all work with taps only.
- Screenshots in light and dark themes.

### Manual
- **Real iPhone and Android phone:** install the app and fill in the daily form one-handed in under 60 s.
- **Admin flows on a phone:** confirm a movement, recount the kassa, close a month, record payouts.
- **Offline:** check that the daily draft survives losing the network.
- **Backups:** one restore from point-in-time recovery into a scratch database.

## 11. Traceability

| Requirement | Where it's covered |
|---|---|
| Spec §1, §13: 1–2 minutes per day | §6.3 |
| Spec §2–3: daily form and example | §6.3; technical-solution §4 (the example changes because certificate sales aren't split) |
| Spec §4, §11: roles and access | technical-solution §5.5, §6; §5 routes |
| Spec §5: day report | §4.2 |
| Spec §6: month report | §4.3 |
| Spec §7: trends | §4.4 |
| Spec §8: dynamic categories | `categories`; technical-solution §4 |
| Spec §9: history | §4.5 |
| Spec §10: export | §4.6 |
| Spec §11: change history | `audit`; movements are voided, never deleted |
| Spec §12: PWA, iPhone, auth, EUR, DB, backups | §6–7; technical-solution §3, §6, §7 |
| Sheet: Money Flow | `ledger` + reports (§3.2, §4.1) |
| Sheet: Debit Credit | §3.4, §4.3; technical-solution §5.3–5.4 |
| Sheet: FutureValue Input, Trend Dashboard | `importedMonths`, §4.4 |
