# Aesthetic Lab Income App — Technical Solution

Status: **approved for design** · Sources: `Aesthetic_Lab_TZ_internal_app.pdf` (ТЕХНИЧЕСКОЕ ЗАДАНИЕ) and the current Google Sheet «Aesthetic Lab Finance» (tabs Money Flow, Debit Credit, FutureValue Input, Dashboards)
Companion documents: [system-design.md](system-design.md) · [implementation-plan.md](implementation-plan.md)

## 1. Goal

An internal web app (PWA) that **replaces the «Aesthetic Lab Finance» Google Sheet** and covers the spec.

- **Per client:** each employee records service value, extra sales and actual cash/card contributions. «Клиент 1», «Клиент 2» and creation time are automatic; the day totals are aggregated.
- **Kassa:** the app always knows how much money is in the kassa (salon cash, and the salon's card). It also knows, for each person, whether the salon owes them or they owe the salon.
- **Monthly:** the app works out what the sheet does today: salon income, expenses, Anna's salary, the owners' dividends, and **one final action per person**.
- **Admin overview:** the admin sees the day, month and year, trends and history, and can export to Excel or CSV.

## 2. Decisions

| Topic | Decision |
|---|---|
| «Себе» / «Салону» | Each report's revenue is split in two. **«Себе»** is the person's share, worked out automatically as their % of the «Себе» base. **«Салону»** = Total − «Себе». The person can override «Себе» on a record. |
| Staff (fictional names and terms) | Anna (`anna`, admin and also submits reports): «Себе» 45%, salary €1,200/month, dividend share 1. Maria (`maria`): 65%, dividend share 1. Olga (`olga`): 65%, dividend share 1. Vera (`vera`): 40%, no dividends. The sheet's "Salon Split %" = 100% − «Себе» %. |
| Split base | New client entries split **services only**. Creams, oil and certificate sales go entirely to the salon. Certificate sales are liabilities until used for services; redemption earns the service share without another cash/card deposit. Legacy saved daily figures remain frozen. |
| Kassa | Two kassa accounts: **«Касса — наличные»** (the salon till) and **«Касса — карта салона»**. **Card payments always land in the kassa card.** Cash varies: each day the master records how much cash they left in the till. |
| Who pays whom | **Everything goes through the kassa; nobody pays another person directly.** Each person has a running balance with the salon. At month close the app suggests **one** payout (salon → person) or **one** deposit (person → kassa). Anything unpaid carries over. See §5. |
| Monthly settlement | Replaces the sheet's Debit Credit tab: salon income, expenses, manager salary (Anna €1,200), and **owner dividends = max(0, profit) split 1/3 between Anna, Olga and Maria**. These are added at month close. |
| History | The sheet's monthly figures for **April–September 2026** will be imported (milestone M10) as read-only monthly records marked «импорт», so trends start with data. Daily records start on **1 October 2026** (decision 2026-10-14), the first month the app tracks; nothing can be dated earlier. |
| Timezone | `Europe/Brussels`. |
| Domain | `kassa.aestheticlab.be`. |
| Accounts | The admin creates every account and gives out the first password. There is no self sign-up. |
| Payments | 3 payment methods: **Card** (goes into the kassa card), **Cash** (split between the till and the master), **Certificate** (a service paid with a certificate sold earlier; the money is already in the kassa). Payments must add up to Total. Payment methods are dynamic; each one has a «куда поступают деньги» setting. |
| Stack | Go + Datastar + templ + Tailwind 4 / daisyUI 5, on **Cloud Run** in GCP project `<project>` (europe-west1), with **Firestore**. |
| UI language | Russian only. |
| Devices | **Mobile-first for everyone, the admin included.** Every screen is designed for a 375 pt-wide phone (iPhone first, Android supported) and installed as a PWA. On desktop the same page uses a wider layout. See system-design §5. |
| Edit window | People edit their own records until the **admin closes the month**. A closed month is **locked for everyone, the admin included**. To correct it, the admin reopens it. Months are closed strictly in order, and only the most recently closed month can be reopened (§5.4). |
| Staff-entered money movements | When a person records a deposit or an expense they paid themselves, it shows as **«ожидает подтверждения»** until the admin confirms it with one tap. It doesn't affect any balance until then. |
| Production and the first real month (decisions 2026-10-12 and 2026-10-14) | Production only: **kassa.aestheticlab.be**, main service and main database `aestheticlab-income`, start month `2026-10`. Trial resources are retired after verified exports. **October 2026 is the first real month**, run in parallel with the sheet. The October practice entries were voided and stay as history. See MAINTAINING.md → Real October. |
| Money terms over time | «Себе» %, salary and dividend share are **effective-dated**: they apply from a given month, normally next month. A change can't start in a month where the person already has reports, so one month never mixes two rates. Every month is calculated with the terms in force that month, so changing someone's salary in December never changes November, even if November is reopened. |
| Environments | **Prod only.** Push to `main` → deploy with no traffic → check `/readyz` → shift traffic. Same pattern as the earlier `datastar-webapp`. |

## 3. Stack

Versions and conventions follow an earlier Go + Datastar web app (`datastar-webapp`).

| Layer | Choice | Why |
|---|---|---|
| Language | Go 1.27.2, `net/http` ServeMux with method patterns | One static binary; cold start on Cloud Run is under 1 s. |
| UI | templ v0.3.1020 (`go tool templ`) + Datastar (`datastar-go` v1.2.2, vendored `datastar.js` v1.0.4) | Server-rendered hypermedia, no React. Live form totals come from Datastar computed signals. |
| CSS | Tailwind 4.3.3 + daisyUI 5, built with Bun (`bunx @tailwindcss/cli`) | Custom daisyUI theme in the brand palette from qwik-aestheticlab: sage `#8b9687`, linen `#f7f5ef`, ink `#1c211b`, blush `#d9b3a6`, Onest font. |
| Icons | Lucide, generated server-side into `icons_gen.go` | Existing pattern. |
| Charts | **SVG drawn on the server by templ**, with a data table under each chart | Scales to the phone's width through its `viewBox`. No JS library, and it works under the strict CSP (`script-src 'self' 'unsafe-eval'`, needed because Datastar evaluates expressions; `style-src 'self'`). |
| DB | **Firestore Native, a separate named database `aestheticlab-income`** in europe-west1 | Serverless with no idle cost. Kept apart from the `(default)` DB, which holds the Telegram bot's data. |
| Excel / CSV | `excelize` (pure Go) for XLSX; `encoding/csv` for CSV | The CSV uses a UTF-8 BOM, `;` delimiter and decimal comma, so it opens correctly in Excel with a Belgian or Russian locale. |
| Auth | Custom: bcrypt password hashes + a session cookie signed with HMAC | Reuses the signer and the bcrypt "dummy hash" pattern from datastar-webapp. |
| Hosting | Cloud Run service `aestheticlab-income` (europe-west1) at **`https://kassa.aestheticlab.be`**. Image in its own Artifact Registry repo `europe-west1-docker.pkg.dev/<project>/aestheticlab-income/income`, separate from the website's, so the deployer can't touch other images | Same project and region as the other salon services. |
| CI/CD | GitHub Actions with Workload Identity Federation (no stored keys), repo `lieranderl/aestheticlab-income-app` (private) | Copies `datastar-webapp/.github/workflows/web.yml`. |

### Why Firestore is enough
- **The data is small.** Client entries grow with appointments; day reads are scoped to the employee/month or selected day, and monthly snapshots keep per-person aggregates rather than a client list.
- **Closed months are stored as snapshots.** Each closed month keeps its final balances, so a balance or settlement screen only reads the months still open.
- **Totals are worked out in Go.** Firestore has no GROUP BY, but at this size that doesn't matter.
- **Cost stays at cents.** Named databases get no Firestore free quota, so every read and write is billed, but at this volume that's well under €1 a month (§8).

### Rejected options
- **Cloud SQL Postgres:** at least ~€10 a month, plus connection management, for no real benefit at this scale.
- **SQLite + Litestream on Cloud Run:** fragile, because Cloud Run instances are ephemeral.
- **React / TanStack:** you prefer hypermedia, and the app is mostly forms and tables.

## 4. Business rules

### Current client-entry model (2026-10-13 user decision)
One entry per employee/client/day. Services includes the part paid with a previously sold certificate. Additional sales are cream, oil and certificate sales; their entire amount goes to the chosen cash/card kassa account. Service contributions are entered separately as actual cash and card amounts, defaulting to zero. Their sum cannot exceed services minus certificate redemption. Customer payment methods and personal client details are not collected.

«Себе» is the effective service percentage on service value only. Cash/card kassa inflows are actual contributions plus automatically allocated extra sales. Outstanding certificates increase on sale and decrease on redemption, which creates no additional cash/card inflow. Monthly salaries, higher service-share bonus and dividends keep their existing rules. Client labels are sequential within an employee/day; the server records immutable creation time, displayed in Brussels. Corrections are versioned, audited and locked in closed months. Repeating the same unchanged create token acknowledges one saved record rather than creating another.

Example: services €100, cream €20 to cash, service cash contribution €30, rate70% ⇒ Self€70, salon€50, cash kassa€50, held€70, balance change0. A service€100 fully paid with an earlier certificate deposits no cash/card, reduces outstanding by€100 and increases the employee balance by€70.

Old daily records and closed snapshots keep their old money. They remain inspectable/correctable and cannot be newly created from the web. Their customer count is unknown, not reconstructed. The following daily-form formulas document legacy calculations and the shared balance engine; new entries use `domain.NewClientReport` to supply the new service-only flags and actual contributions.

### Legacy daily report

The server is authoritative; the client only shows a preview.

### Money
- Stored as `int64` euro cents, never as floats.
- Input accepts `600`, `600,5` or `600.50`.
- Displayed as `€1 234,50`. Whole amounts drop the `,00`.
- Every amount must be ≥ 0.

### Totals and payments
- **Total** = the sum of the revenue-category amounts on the record.
- **Payment check:** the payment-method amounts must add up to Total. If not, the save is rejected with an inline message showing the difference.

### «Себе» and «Салону»
- **Base:** `selfBase` = the sum of revenue categories marked **«учитывать в “Себе”»**.
  - Every category counts by default, **except «Сертификаты» (certificate sales)**.
  - The admin can change the flag per category; no code change is needed.
- **«Себе»** = `round(selfBase × selfPct)`, using the person's own %.
- **«Салону»** = Total − «Себе».
- **Snapshots:** `selfPct` and `selfBase` are saved on each record. Changing a % or a flag later doesn't rewrite history.
- **Override:** the person can type a different «Себе» (between 0 and Total). The record is then marked `selfOverridden=true`.

### Where the money is: «Оставьте в кассе»
Each payment method has a destination:

| Method | Destination | Meaning |
|---|---|---|
| Карта | `kassa_card` | Already in «Касса — карта салона». |
| Наличные | `cash` | The master hands the salon's part over, in the till (`cashToKassa`) or by transfer to the salon's card (`transferToCard`), and keeps the rest. |
| Сертификат | `prepaid` | The money came into the kassa when the certificate was sold. |

**Recording cash:**
- When Cash > 0, the form shows **«Оставьте в кассе»** with the amount that settles the day to zero, `suggested = clamp(«Салону» − card − prepaid, 0, cash)`, and two fields:
  - **«Наличными в кассу»** (`cashToKassa`): cash left in the till.
  - **«Переводом на карту салона»** (`transferToCard`): the master transfers it to «Касса — карта салона» instead (and keeps that cash).
- Empty transfer means 0. Empty «Наличными в кассу» means `max(0, suggested − transfer)`, so the till follows the suggestion until it is typed (the form shows it as the field's placeholder).
- Both are ≥ 0 and together at most Cash.

**How the day settles:**
- **Money in the kassa from this report:** `kassaCredit = card + prepaid + cashToKassa + transferToCard`.
- **Effect on the person's balance:** `balanceDelta = kassaCredit − «Салону»`. A positive value means the salon owes the person; a negative value means the person owes the salon.
- **Check:** the cash the master kept (`cash − cashToKassa − transferToCard`) plus `balanceDelta` always equals «Себе».

**Spec example:**
- Revenue: services 600, creams 50, certificates 100, other 0, so Total = 750.
- «Себе» base: 650, because the certificate sale is excluded.
- At 50%: «Себе» 325 and «Салону» 425. (The spec's 375 / 375 assumed the certificate sale was split as well.)
- Payments: card 600 and cash 150.
- Card alone already covers «Салону», so «Оставьте в кассе» suggests **€0**.
- The master keeps €150 cash, and the salon owes them 600 − 425 = **€175**. Together: 150 + 175 = 325 = «Себе» ✓.

### Other daily rules
- **One record per person per day.** The doc ID is `{userId}_{YYYY-MM-DD}`. Opening a day that already has a record opens it for editing.
- **Date:** defaults to today in Europe/Brussels. Future dates are not allowed. Past dates are allowed while the month is open, and never before the **start month**: `INCOME_START_MONTH`. Production uses `2026-10`: October 2026 is the first real month (decision 2026-10-14). The code default `2026-11` applies only when the variable is unset. Every cutoff (report and movement dates, the first month that can close, opening rows) uses the configured start month; nothing hard-codes a month.
- **Categories and payment methods:**
  - They can be added, renamed and reordered.
  - Delete removes an item for good if it was never used; otherwise it is archived.
  - Renaming keeps the ID.
- **Working days:**
  - Salon: the number of distinct dates with at least one report.
  - Person: the number of their own reports.

## 5. Business rules: kassa, balances and settlement

### 5.1 Accounts and movements
**Kassa accounts:**
- «Касса — наличные» (`kassa_cash`).
- «Касса — карта салона» (`kassa_card`).

More accounts can be added later, for example a business bank account.

**Kassa movements** (the ledger; each has a date, amount, note and author):

| Type | From → To | Who records it | Effect |
|---|---|---|---|
| **Внесение** (deposit) | person → kassa (cash or card) | person (pending until confirmed) or admin | Kassa +, person's balance + |
| **Выплата** (payout) | kassa → person | admin | Kassa −, person's balance − |
| **Расход** (expense) | kassa → outside, **or** person → outside (the person paid for the salon) | admin; or a person for their own payment (pending) | Kassa − **or** person's balance +. Always counts as a salon expense. Needs an expense category and a note. |
| **Перевод** (transfer) | kassa cash ↔ kassa card | admin | Moves money between the two kassa accounts; total kassa unchanged. |
| **Корректировка** (adjustment) | ± a kassa account | admin, from «Пересчёт кассы»; note required | Fixes the difference between counted and expected money. |
| **Начальный остаток** (opening balance) | – | admin, once, dated **1 October 2026** | Kassa cash and card balances, each person's balance after the September settlement, and **outstanding certificates** (sold before 1 October, not yet used). See §5.6. |
| **Зарплата / Доплата «Себе» / Дивиденды** (salary / self_bonus / dividends) | salon → person (no money moves) | system, at month close; voided only by reopening the month | Person's balance + |

Daily reports also change balances without creating ledger rows:
- `kassa_card` += card + `transferToCard`.
- `kassa_cash` += `cashToKassa`.
- The person's balance += `balanceDelta`.

### 5.2 Balances
- **Kassa balance**, per account, at any moment:
  - opening balance
  - \+ money in from reports (card, or cash left in the till)
  - \+ money moved into the account
  - − money moved out of the account
- **Person's balance** («Салон должен вам» if positive, «Вы должны салону» if negative):
  - opening balance
  - \+ Σ `balanceDelta` from their reports
  - \+ Σ deposits they made
  - \+ Σ expenses they paid for the salon
  - \+ salary accruals
  - \+ dividend accruals
  - − Σ payouts to them

  This is the sheet's Settlement Balance formula: Returned + Staff-paid expenses + Salary + Dividend − Already paid − Salon share due. The one difference: card and certificate payments now count as "returned" automatically.
- **Outstanding certificates** (money in the kassa that belongs to future services):
  - opening outstanding (certificates sold before 1 October and not yet used) + Σ certificate sales − Σ certificate redemptions (`prepaid` payments).
  - Shown on the kassa screen. If it would go below zero, the screen shows «Проверьте сертификаты»: a redemption was entered for a certificate that the opening amount missed.

### 5.3 Monthly result (replaces the Debit Credit and Trend Dashboard tabs)
For month `m`:
- **Revenue:** Σ Total. The certificate-sale category is shown separately as well.
- **Salon income** = Σ «Салону» − Σ certificate sales.
  - A certificate sale is money held for a future service, not income yet. The salon's income from it is counted when the certificate is redeemed.
  - This matches the sheet's "Salon Retained Share".
- **Expenses:** Σ «Расход» entries in the month, from the kassa and paid by staff, grouped by expense category.
- **Manager salary:** Σ salaries (Anna €1,200).
- **Higher «Себе» above a threshold** (optional, per person in their terms, e.g. 50%, or 70% when the month's «Себе» base is above €5 000):
  - The «Себе» base is the revenue «Себе» is computed from (no certificate sales).
  - If the month's base is **strictly above** the threshold, the **whole month** counts at the higher rate.
  - Day reports always use the normal rate, so «Оставьте в кассе» and saved reports never change. Closing the month accrues the difference as one `self_bonus` row: `round(base × (higher % − normal %))`.
  - A day whose «Себе» was set by hand counts toward the threshold but is left out of the bonus base: it keeps exactly the «Себе» it was given.
  - Closing refuses a month whose reports were saved at another «Себе» rate than its terms (it cannot happen through the app, since terms never change inside a month with reports).
  - Threshold and rates are part of the effective-dated terms, so they can differ from month to month.
- **Profit** = salon income − expenses − salaries − «Себе» bonuses.
- **Dividends:**
  - Total = `max(0, profit)`, as in the sheet. A loss pays no dividends and is not carried over.
  - Split between Anna, Olga and Maria by dividend share (1 : 1 : 1). Leftover cents are spread by the largest-remainder method, so the parts add up exactly.
- **Per person:** revenue, «Себе», salary, dividend, «Итого заработано» (= «Себе» + «Себе» bonus + salary + dividend), plus their balance at the start and end of the month.

### 5.4 Month close and settlement («who pays whom»)
1. **Before closing**, the admin checks the month screen:
   - Missing daily reports (a warning).
   - Pending deposits and expenses: **closing is blocked** until each one is confirmed or voided.
   - A kassa recount («Пересчёт кассы»): enter the cash actually counted; any difference becomes an adjustment entry.
2. **«Закрыть месяц»** in one transaction:
   - Only allowed when the previous month is already closed (the first tracked month, the start month — October 2026 in production — has none).
   - Locks the month for everyone, the admin included.
   - Adds salary and dividend entries for each person.
   - Saves a **settlement snapshot**: the monthly result plus each person's closing balance.
3. The **settlement screen** shows one line per person:
   - **«Салон выплачивает €X»** (balance > 0). The «Выплатить» button records a payout from the kassa card (bank transfer) or the till. The amount is pre-filled and can be changed up to the current remaining balance; partial payments are allowed.
   - **«Внести в кассу €X»** (balance < 0). The «Получено» button records a deposit, capped at the current amount owed; partial deposits are allowed.
   - **«Перенести»** leaves the balance as it is to carry over into next month. A small negative balance then simply comes off next month's payout.
4. **Reopening a month:**
   - Only the most recently closed month can be reopened. To fix an older month, reopen the later months first, newest to oldest. That's rare, and it keeps every saved snapshot valid.
   - Reopening voids that month's salary and dividend entries and deletes its snapshot, with an audit record.
   - The admin corrects reports or movements, then closes the month again. Salary, dividends and closing balances are worked out afresh.
   - Payouts and deposits for a closed month's settlement are dated when the money actually moves, which is always in an open month. So settling up never needs a closed month reopened.

**Why this is simpler than today:**
- **Card money needs no transfers.**
- **Cash is settled on the spot.** The «Оставьте в кассе» suggestion zeroes the day, so most balances only grow when cards cover more than the salon's share, and then the salon owes the master.
- **One payment per person per month.** At month end there is usually just one payout to each person from the kassa card (their «Себе» not yet received + salary + dividends + their expenses), or nothing at all.
- **Problems surface weekly, not at month end.** The sheet shows a negative cash balance (−€968 for September), which means movements are missing. A weekly recount catches these early.

### 5.5 Who sees what
- **Everyone** sees their own balance, their own movements, and their monthly earnings («Себе», salary, dividend after close) on «Мои записи».
- **Only the admin** sees kassa balances, other people's figures, profit and the settlement screen.

### 5.6 Opening balances (go-live, 1 October 2026)
Decision 2026-10-14: the app tracks money from **1 October 2026**, the original plan having been 1 November. Anything earlier stays in the sheet. These figures are captured once and loaded as `opening` rows dated 1 October, before October is closed:

| Figure | When | Source |
|---|---|---|
| Cash in the till | the morning of 1 October, before the first client (reconstructed from the sheet if it wasn't counted then) | Anna |
| Salon money on the salon's card | the morning of 1 October | Anna's banking app: the card balance minus any of Anna's private money on it |
| Outstanding certificates | 1 October | certificates sold before 1 October and not yet used (Anna's certificate list / Timely) |
| Each person's balance | now: September is settled in the sheet | the sheet's September Debit Credit «Settlement Balance» |

- **The person openings are a snapshot from before 1 October:**
  - Each one is the sheet's September «Settlement Balance». Its "Already paid" counts only payouts dated **on or before 30 September**.
  - Every payout or deposit made **on or after 1 October**, including September's settlement payouts, goes into the app as an October movement with its real date.
  - So each payout is counted exactly once: in the sheet's September figure if it happened by 30 September, in the app if it happened later.
  - When loading, `cmd/import opening -start-month 2026-10 -dry-run` prints each person's figure next to the sheet's September "Already paid", for Anna to check.
- **Loading:** the opening figures are loaded in Release 1 (M5). Kassa balances, person balances and the outstanding-certificate figure are right from the first screen that shows them (M7).

## 6. Security

### Access
- Every route except `/login`, `/static/*` and `/readyz` requires a session.

### Session cookie
- HttpOnly, Secure, SameSite=Lax.
- Signed with HMAC-SHA256 using `INCOME_SESSION_KEY` from Secret Manager.
- Contains `uid`, `sessionVersion`, `auth` (login time) and `exp`. It lasts 30 days and is extended at most once a day, but never past 90 days after the login.
- Each request loads the user and checks `active` and `sessionVersion`.
- A password reset, deactivation or logout increments `sessionVersion`, which logs the person out on every device; a copied cookie stops working too.
- A password change checks, inside the write, that nothing reset the account since the request read it.
- The admin's own password changes only on `/profile` (which asks for the current one), never through the staff reset, so a stolen admin session cannot take over the account.

### Brute force
- `failedLogins` and `lockedUntil` are stored on the user doc.
- Unknown usernames still run a dummy bcrypt compare, so timing doesn't reveal which usernames exist.
- Per source: after 10 failed logins from one address within 15 minutes, logins from it get 429 «Слишком много попыток входа…» before any lookup or bcrypt. The source is the last `X-Forwarded-For` entry (Cloud Run appends it; earlier entries are the client's), IPv6 per /64. Counts live in instance memory (at most 10,000 sources; past that, new sources share one count).
- At most 4 bcrypt compares run at once per instance; a login that can't get a slot within 5 s answers 503 «Не удалось войти…», so a flood can't take all the CPU.
- Residual risk: someone using many addresses can still lock a known account for 15 minutes at a time. The lock is logged (`account locked after failed logins`, with the source); the admin can unlock by resetting the password.

### CSRF
- SameSite=Lax.
- `Origin` / `Sec-Fetch-Site` check on every non-GET request. Allowed origin: `https://kassa.aestheticlab.be`, plus localhost in development.

### Authorization
- Middleware plus a policy per handler.
- A person can only reach their own reports, movements and balance.
- `/admin/*` requires `role=admin`.
- Only the admin can confirm movements, record payouts, recount the kassa, and close or reopen months.

### Headers
- Strict CSP, using the existing `SecurityHeadersMiddleware`.

### Firestore access
- Rules are `allow read, write: if false`.
- Runtime service account `aestheticlab-income-runtime@`:
  - `roles/datastore.user`, limited by an IAM condition to the `aestheticlab-income` database.
  - `secretAccessor` on `INCOME_SESSION_KEY` only.

## 7. Backups and data safety

- **Point-in-time recovery:** 7 days.
- **Scheduled backups:** daily (kept 7 days) and weekly (kept 14 weeks).
- **Monthly export to GCS:** `gs://<project>-income-backups`, kept 3 years. Triggered by Cloud Scheduler with an OAuth token for the dedicated backup service account, which may only export this database.
- **Delete protection** is on for the database.
- **Audit log:** every change to reports, kassa movements, categories, staff settings and months is stored with before and after values (spec §11).
- **Movements are never hard-deleted.** A wrong movement is cancelled (`voided`, with a reason) and stays visible.
- **The admin XLSX export** also serves as a backup a person can read.

## 8. Cost estimate

Firestore's free quota doesn't cover named databases, so nothing below assumes it. Point-in-time recovery and backups are billed by storage. Usage assumed: 5 people, about 50 page views a day, ~50 document reads per view.

| Item | Usage | Per month |
|---|---|---|
| Firestore reads | ~75,000 | ≈ €0.03 |
| Firestore writes and deletes | ~3,000 | < €0.01 |
| Firestore storage | < 0.1 GiB | ≈ €0.02 |
| Point-in-time recovery | about the size of the data | ≈ €0.02 |
| Scheduled backups | ~21 copies kept (7 daily + 14 weekly) of < 0.1 GiB | ≈ €0.05–0.20 |
| Monthly export to GCS | < 0.1 GiB a month, kept 3 years | ≈ €0.01–0.05 |
| Cloud Run (min 0 instances) | a few hours of CPU | €0 within the account's free tier (shared with the other salon services), else ≈ €1 |
| Artifact Registry | images of ~20 MB; old ones pruned | ≈ €0.05 |
| **Total** | | **≈ €0.20–1.50** |

The €5/month budget alert from M1 catches any surprise. Check actual figures in Billing after the first full month and update this table.

## 9. Accounts

- **No public sign-up.**
  - The admin creates each account and sets a temporary password, which must be changed at first login.
  - Account settings: username, name, role, `submitsReports`, can log in.
  - Money terms («Себе» %, monthly salary, dividend share) are effective-dated: each change applies from a chosen open month, and earlier months keep their terms.
- **Passwords:**
  - Users change their own on the profile screen.
  - The admin can reset any password, which logs that person out everywhere.
- **First setup:** `cmd/seed` runs once and creates:
  - Anna's admin account;
  - the default revenue categories, payment methods and expense categories;
  - the two kassa accounts.

### Default lists
| List | Items |
|---|---|
| Revenue categories | Услуги, Кремы, Сертификаты (certificate sale, not split), Другие товары, Прочее |
| Payment methods | Карта → `kassa_card`, Наличные → `cash`, Сертификат → `prepaid` |
| Expense categories | Аренда, Материалы, Коммунальные (электричество, интернет), Реклама, Хозтовары, Оборудование и мебель, Софт и хостинг, Прочее |

## 10. Open items

None. All earlier questions are resolved in §2.
