# Aesthetic Lab Income App — Implementation Plan

Based on [technical-solution.md](technical-solution.md) and [system-design.md](system-design.md). This plan sets **the build order** and **how we know each step is done**.

## 1. Approach

- **Every milestone ships.** Each one ends with something that works and is deployed to production.
- **Deploy early.** `<app-host>` goes live in M1.
- **Staff first.** The daily form ships in M5 (Release 1), so real data builds up while the admin, kassa and settlement screens are built.
- **Test first where the money is.** Report maths, balances, settlement and dividends are written test-first. The test suite **reproduces the September 2026 numbers from the Google Sheet** before any screen relies on them.
- **Copy, don't reinvent.** Tooling, middleware, session signing, the Dockerfile and CI come from `~/Documents/Projects/datastar-webapp`.
- **Phone first.** Every screen is checked at 375 pt before it counts as done.
- **Cut over at a month boundary.** October 2026 is entered in both the app and the sheet (decision 2026-10-14; originally November). The sheet is retired once October's settlement matches.

### Done means
- `make ci` passes: lint, tests, security scan, build.
- The change is deployed through GitHub Actions.
- New screens are checked at 375 pt, in light and dark themes.
- Every change to financial data writes an audit record.

### Decision 2026-10-14: October is the first real month
- Real data starts on **1 October 2026**. Openings are dated 2026-10-01, and 1–14 October is backfilled from the sheet. The start month stays `2026-10`.
- **October is the parallel month** (it was November): the sheet stays the record, and October is closed in both and compared early November (M12).
- The October practice entries were already voided in the app; no cleanup or cut-off change is needed (MAINTAINING.md → Real October).
- M11 is done (#45, #46). M10 imports April–September 2026, since October lives in the app.

### Current release sequence (2026-10-13)
- M0–M8 implementation and per-client reporting (PR #24) are merged; the signed-in identity change (PR #25) passed CI and deployed. Keep the existing candidate/readiness/traffic deployment workflow. Neither change requires a financial reset for release.
- **Next: pre-November M11 readiness.** Provide and verify a pause of all financial writes, including admin writes, for the controlled cleanup/opening window. Check the client-entry and kassa flows on real phones, finish mobile CI/security/offline checks, and complete the restore drill.
- **UI clarity pass before onboarding:** [implemented and verified locally](reviews/2026-10-13-mobile-ui/REVIEW.md), pending release and real-phone comprehension check. Entry/help/results, redemption/sales, grouped analytical metrics, current/projected settlement and invalid-input previews follow system-design §6.1a. No deployment architecture change or data migration is needed.
- ~~**Before real November entries:**~~ superseded by the 2026-10-14 decision above. Was: while writes are paused, export, clear only approved practice financial records/counters and whole closed test-month state documents, preserve accounts/settings/November–December terms, set cutoff `2026-11`, and load confirmed openings using the dry-run procedure. Check balances before resuming writes. Till/card/certificates are captured on 1 November; person balances follow the final October settlement.
- M9 is paused: adapt its categories, trends and exports to the agreed client-entry model before resuming the preserved worktree. The original dynamic revenue/payment-form acceptance criterion below needs a new design decision; it must not restore the replaced daily payment model or change frozen legacy records. M9 can remain after cutover if needed.
- M10 imports April–September 2026. October is the parallel month; retire the sheet only after October reconciles and Anna signs off (M12).

## 2. Inputs

| # | Item | Status |
|---|---|---|
| 1 | GitHub repo `lieranderl/aestheticlab-income-app` (private) | ✅ Created in M0; stays private. |
| 2 | GCP access | ✅ `<owner-email>` has `roles/owner` on `<project>`. I run the setup with the gcloud CLI. |
| 3 | DNS | ✅ `<domain>` is in Cloud DNS zone `aestheticlab` in the same project. The domain is already verified, and two Cloud Run domain mappings already work. I add `kassa` myself. |
| 4 | Staff | ✅ Four staff (fictional here): Anna (admin, submits reports, 45%, €1,200 salary, dividend share 1), Maria (65%, 1), Olga (65%, 1), Vera (40%, no dividends). |
| 5 | Money rules | ✅ New client entries split services only. Cream/oil/certificate sales go fully to the selected cash/card kassa; redemption is included in services without new cash/card inflow. Legacy daily records and snapshots retain their frozen rules. See [plans/client-entries.md](plans/client-entries.md). |
| 6 | History | ⏳ «Aesthetic Lab Finance» sheet: monthly figures for **April–September 2026** (the months before the start month), imported in M10. Nothing has been imported yet. |
| 7 | **Opening balances on 1 October 2026** (decision 2026-10-14) | ⏳ See technical-solution §5.6. Anna provides: the till cash and the salon money on her card on the **morning of 1 October**; **outstanding certificates** sold before 1 October and not yet used; each person's balance after the September settlement in the sheet. |

## 3. Milestones

Estimates are working days, with Claude Code implementing and you reviewing.

### M0 — Repo and app skeleton (1 day)
- **Repo:**
  - Create the private repo with `gh repo create`.
  - Squash merges only. Branch rulesets need GitHub Pro for a private repo, so `.githooks/pre-push` (`make hooks`) blocks direct pushes to `main`, and PRs are merged only after `CI passed` is green.
- **Go module:** pin the same versions as datastar-webapp: Go 1.27.1, datastar-go v1.2.2, templ v0.3.1020 (as a `go tool`), Tailwind 4.3.3, daisyUI 5, Bun 1.4.2.
- **Tooling copied from datastar-webapp:**
  - `Makefile` (`web-dev`, `web`, `firestore`, `test`, `test-store`, `lint`, `security`, `ci`, `image`). Local containers use Apple's `container` CLI.
  - `.air.toml` and `.env.web.example`.
  - `Dockerfile.web` → `Dockerfile`.
- **Server shell:**
  - `cmd/web/main.go`.
  - `internal/web/server.go` with `/readyz`.
  - The middleware chain from `internal/handlers/handlers.go`.
- **Frontend:**
  - `frontend/app.css` with the brand daisyUI themes and the Onest font (Cyrillic and Latin only).
  - Lucide icons, the vendored `datastar.js`, and `static/assets.go`.
- **Mobile base layout:** `viewport-fit=cover`, safe areas, an empty `dock` bottom bar, and a sidebar at `lg`.
- **Project conventions:** `AGENTS.md` (with `CLAUDE.md` as a symlink to it), adapted from datastar-webapp.
- **CI:** `.github/workflows/ci.yml` (the Firestore emulator job is added in M3, when the store exists).

**Done when:** `make web-dev` serves the shell, it looks right at 375 pt, and CI is green.

### M1 — Infrastructure and first deploy (1 day)
Everything is scripted in `docs/MAINTAINING.md` so it can be repeated (system-design §9).
- **Firestore** `aestheticlab-income`: point-in-time recovery, delete protection, backup schedules, indexes, deny-all rules.
- **Service accounts:** runtime and deployer, with the database IAM condition.
- **Secret** `INCOME_SESSION_KEY`.
- **GitHub access:** a Workload Identity provider for the repo in the `aestheticlab-github` pool.
- **Deploy pipeline:** Cloud Run `aestheticlab-income` and `.github/workflows/web.yml` (no-traffic deploy → `/readyz` → shift traffic).
- **Domain:** a domain mapping for `<app-host>`, plus a CNAME `kassa → ghs.googlehosted.com.` in Cloud DNS zone `aestheticlab`.
- **Backups bucket and monthly export:** the bucket, plus a Cloud Scheduler export job.
- **Monitoring:** 5xx and `/readyz` alerts, and a €5/month budget alert.

**Done when:** `https://<app-host>/readyz` returns 200 with a valid certificate, and a push to `main` deploys automatically.

### M2 — Domain rules, pure Go (2.5 days)
`internal/domain`, written test-first.
- **Money:** parse and format; `int64` cents throughout.
- **Daily report:**
  - Total and the payment check.
  - `selfBase`, excluding certificate sales and categories without the flag.
  - «Себе» with the override limits; «Салону».
  - Suggested `cashToKassa`, `kassaCredit`, `balanceDelta`, `salonIncome`.
- **Balances:**
  - Per person and per account, from a snapshot plus reports and ledger rows.
  - Pending and voided rows are ignored.
- **Monthly result:**
  - Salon income, expenses, salaries, profit.
  - Dividends as `max(0, profit)` split by weight with largest-remainder cents.
  - Per-person settlement and the suggested action.
- **Aggregation:** day, month, trends (including imported months).
- **`ru` helpers:** month names, plural forms, dates.

**Done when:**
- **Invariant:** cash kept + `balanceDelta` = «Себе», tested across many generated inputs.
- **Spec example (a 50% master):** «Себе» 325, «Салону» 425, balance +175.
- **Certificates:** each euro is paid out only once.
- **Outstanding certificates:** opening 500 + sale 100 − redemption 150 = 450. Negative outstanding raises the warning.
- **Reference month (fictional, laid out like the sheet's settlement tab):**
  - Dividends: 3 × €596.28, with 1 cent to one owner by largest remainder.
  - Balances: Maria +96.93, Olga +333.28, Vera −88.50, Anna +489.78 (±1 cent).
- **Coverage:** ≥ 95% on `internal/domain`.

### M3 — Storage (2 days)
- **`store.go` interface:** users and usernames, categories, accounts, reports, ledger, months and snapshots, imported months, audit.
- **Implementations:** `memory.go` and `firestore.go`.
- **Transactions** (system-design §3): save report; record, confirm and void movements; recount the kassa; close a month (accruals and snapshot); reopen a month (void accruals, drop the snapshot). Months close only in order and only the newest closed month reopens, so snapshots never go stale.
- **`cmd/seed`:**
  - Creates Anna's admin account with a temporary password.
  - Seeds the default revenue, payment and expense categories and the two kassa accounts.
  - Refuses to run twice.

**Done when:** emulator tests pass for:
- a version conflict on a report;
- a closed month rejecting writes from staff **and** the admin;
- closing out of order, and reopening a month older than the newest closed one, both rejected;
- close → reopen → close giving identical snapshots;
- dates before the start month's first day rejected;
- every write producing an audit row.

### M4 — Login and staff (2 days)
- **Login:**
  - Phone-first login page.
  - bcrypt, with a dummy compare for unknown usernames.
  - Lockout after repeated failures.
- **Session:** HMAC-signed cookie (signer from datastar-webapp), 30-day sliding expiry, `sessionVersion` check.
- **Middleware:** session required, admin role, Origin / `Sec-Fetch-Site` check on writes.
- **Passwords:** forced change at first login; `/profile`.
- **«Сотрудники»** (admin):
  - Create, edit, reset password, deactivate.
  - Edit fields: role, `submitsReports`, can log in, plus effective-dated **terms** («Себе» %, salary, dividend share, «Действует с» an open month).

**Done when:**
- HTTP tests cover login, lockout, logout everywhere after a reset, and 403 on other people's data and on `/admin/*`.
- `cmd/seed` has been run against production, and Anna logs in on her phone.

### M5 — Daily form, opening balances, installable app ➜ **Release 1** (4 days)
- **Daily form** (system-design §6.3):
  - Date switcher; category and payment rows.
  - Live Total, «Себе» and «Салону».
  - Cash fills itself in.
  - **«Оставьте в кассе»**, with the suggestion and a plain-language effect line.
  - Pinned bar; server validation; draft kept by `draft.js`; closed-month banner.
- **«Мои записи»** (Release 1 part): month figures (revenue, «Себе», working days) and report cards. The balance card and the deposit and expense sheets come in M7, together with the admin confirmation they need. Showing a balance before deposits and payouts can be recorded would be misleading.
- **Opening balances** (technical-solution §5.6):
  - `cmd/import opening <file>` loads one `opening` row per kassa account, per person, and for outstanding certificates, all dated on the start month's first day (2026-10-01 since the 2026-10-14 decision).
  - `--dry-run` first, then Anna confirms the printed figures.
  - Till, card, certificates and the September person balances are loaded for 1 October.
- **Installable app (minimal PWA):**
  - `manifest.webmanifest` («Aesthetic Lab — Касса», short name «Касса», standalone, brand colours).
  - 192/512 px and maskable PNG icons generated from the brand SVG.
  - `apple-touch-icon` and the `apple-mobile-web-app-*` meta tags.
  - Install steps on the profile screen («Как установить на iPhone / Android»).
  - The service worker and offline page stay in M11.
- **Staff accounts:** create the accounts for Maria, Olga and Vera.
- **Production only:** `<app-host>`, main service/database `aestheticlab-income`, start month `2026-10`. Trial resources are retired. Since 2026-10-14, October is the first real month: no cleanup and no cut-off change; the earlier practice entries are voided and stay (docs/MAINTAINING.md → Real October). Automated tests remain on memory/emulator with injected clocks.

**Done when:**
- On a real iPhone, the spec example is entered one-handed in under 60 seconds.
- Reloading brings the draft back.
- The saved figures match the M2 tests.
- The app installs to the Home Screen on an iPhone and an Android phone, opens standalone with the brand icon, and stays logged in.
- The 1 October till, card, certificate and person figures are loaded and confirmed by Anna.

**➜ Release 1:** from the 2026-10-14 decision, staff enter real October data on **<app-host>**, from 1 October, in the app *and* the sheet. Until M7, deposits, expenses and payouts are still recorded only in the sheet. The admin enters November's movements into the app in M7, dated when they happened (November is still open).

### M6 — Admin: day, month (revenue), report editing, history, change log (3 days)
- **День:** salon card, a card per person, «Не сдали отчёт»; a table from `lg`.
- **Report editing:** «Изменить» reuses the M5 form with admin rights.
- **Месяц → «Выручка»:** totals by category, payment method and person; working days.
- **История → «Записи»:** filter sheet, chips, paging.
- **Журнал изменений.**
- **Admin bottom bar:** Отчёт · День · Месяц · Касса · Ещё.

**Done when:** the day and month figures match hand-calculated totals for the first November days.

### M7 — Kassa, movements, balances (3 days)
- **«Касса»:**
  - Cash and card balances.
  - Outstanding certificates.
  - «Ожидают подтверждения» with one-tap ✓.
  - The **«+ Движение»** sheet: payout, deposit, expense (from the kassa or paid by a person), transfer.
  - Void with a reason.
  - **«Пересчитать»** (recount, which writes an adjustment row).
- **История → «Движения»:** filters for type, status, person and period.
- **«Мои записи» for staff:** the balance card, plus the «+ Внести в кассу» and «+ Расход за свой счёт» sheets (pending until the admin confirms), and movement cards (system-design §6.4).
- **Backfill:** enter November's deposits, expenses and payouts so far from the sheet, each with its real date.

**Done when:**
- After the backfill, kassa balances match the till count and the salon's card.
- Outstanding certificates equal the opening amount + November sales − November redemptions.
- A person's pending expense only changes their balance after it is confirmed.

### M8 — Month result, settlement, close (2.5 days)
- **Месяц → «Итоги и расчёт»:**
  - Result card: income − expenses − salary = profit → dividends.
  - Expense breakdown.
  - Settlement cards with the full breakdown.
  - **«Выплатить» / «Получено» / «Перенести»** actions, which create payout or deposit rows with the amount pre-filled.
- **Close a month:**
  - Pre-checks (pending movements, missing reports, a recount older than 7 days).
  - A confirmation dialog.
  - Accruals and a snapshot.
- **Reopen a month:** only the newest closed month; voids its accruals and drops its snapshot. Closing is allowed only after the previous month is closed. A closed month is locked for the admin too.
- **Staff see their result:** salary and dividend appear in «Мои записи» once the month is closed.

**Done when:** an October-style dry run on test data matches the sheet's Debit Credit tab line by line.

### M9 — Categories, trends, export (2.5 days)
**Paused pending adaptation to client entries.** The scope below is the original daily-form proposal; revise its revenue/payment configuration and acceptance criteria before implementation. See the current release sequence above and [plans/client-entries.md](plans/client-entries.md).

- **«Категории»:** «Выручка», «Оплата» and «Расходы» tabs, with the «учитывать в “Себе”» flag, the certificate-sale flag, the payment destination, ↑ ↓ reordering, and archive or delete.
- **«Динамика»:** 3 / 6 / 12 months; metrics for revenue, per person, creams, certificates, «Себе», salon income, expenses, profit, dividends; an SVG chart plus a table.
- **«Экспорт»:**
  - XLSX sheets: «Записи», «Движения», «По сотрудникам», «Итоги месяца», «По месяцам».
  - CSV: «Записи» or «Движения».

**Done when:**
- A new category or payment method shows up in the form with no deploy.
- The chart fits at 375 pt.
- The XLSX and CSV open correctly in Excel (Belgian locale) and in iPhone Files.

### M10 — History import (0.5 day)
- **`cmd/import history <file>`** reads a CSV exported from the sheet (passed as a path, never committed).
  - From «4 Trend Dashboard» and «3 FutureValue Input»: monthly revenue per person, salon income, expenses, manager salary, profit and dividends for **April–September 2026**, written to `importedMonths`. Imported months must come before the start month (`2026-10`), so October is never imported: it lives in the app.
  - September is already settled in the sheet, so it can run any time.
  - Opening balances are a separate step (M5).
- **Check:** the trend view for April–September matches the sheet's Trend Dashboard.

**Done when:** imported totals equal the sheet's April–September totals.

### M11 — Offline support, mobile polish, hardening (1.5 days)
- **PWA (completing M5's minimal version):**
  - `sw.js`: caches versioned static assets only, with an «Нет соединения» offline page.
  - A one-time install hint at first login.
- **Playwright** (iPhone SE, iPhone 15, Pixel 7):
  - Every screen: no sideways scroll, the bottom bar and main button can be tapped, inputs ≥ 16 px.
  - Flows: daily form, movement sheet, settlement payout.
  - Screenshots in light and dark themes.
- **Speed:** check against the budget in system-design §6.7.
- **Security:** `make security`, a CSP review, an authorization test for every admin route.
- **Restore drill:** restore from point-in-time recovery into a scratch database, check it, then delete it.
- **`docs/MAINTAINING.md`:** deploy, setup, restore, users, rotating the session key, the month-close checklist.

**Done when:** Playwright passes in CI, and the restore drill is done and written up.

### M12 — Parallel month and cutover (1 day of work, spread over October and November)
Since the 2026-10-14 decision the parallel month is **October**.
- **During October:**
  - Each person is onboarded on their own phone (install, first login, password change).
  - 1–14 October is backfilled from the sheet; from then on, daily entries go into the app and the sheet.
  - A weekly kassa recount.
- **Early November:**
  - Close October in the sheet and in the app.
  - Compare the result per person: revenue, «Себе», salon income, expenses, profit, dividends, balances.
  - Fix and explain any differences.
- **Cutover is conditional, not a fixed date.** It happens on the day October reconciles: the figures match to the cent or every difference is explained, and Anna signs off. Target: the first half of November.
  - Until then the sheet stays the source of truth.
  - November days keep being entered in both, so nothing has to be backfilled at cutover.
  - If reconciliation slips, the parallel run simply continues.
- **At cutover:** the app becomes the only source of truth and the sheet becomes a read-only archive. October's payouts are made from the app's settlement screen. Trial resources were retired on 2026-10-12. October's voided practice entries stay as history; nothing is cleared.

**Done when:** October matches the sheet to the cent (or every difference is explained), and Anna has signed off. The cutover date is recorded in `docs/progress.md`.

## 4. Timeline

| Milestone | Days | Running total | Result |
|---|---|---|---|
| M0 Skeleton | 1 | 1 | Repo, CI, app shell |
| M1 Infrastructure | 1 | 2 | Live at <app-host> |
| M2 Domain rules | 2.5 | 4.5 | Money maths matches the sheet |
| M3 Storage | 2 | 6.5 | Firestore, transactions, audit |
| M4 Login and staff | 2 | 8.5 | Secure login, staff settings |
| M5 Daily form, opening balances, installable app | 4 | 12.5 | **Release 1: staff enter data** |
| M6 Admin revenue screens | 3 | 15.5 | Day, month, history, change log |
| M7 Kassa and balances | 3 | 18.5 | Balances, movements, recount, staff deposits and expenses |
| M8 Settlement | 2.5 | 21 | Who pays whom, month close |
| M9 Categories, trends, export | 2.5 | 23.5 | Everything in the spec |
| M10 History import | 0.5 | 24 | April–September trends |
| M11 Offline and hardening | 1.5 | 25.5 | Offline page, tests, restore drill |
| M12 Cutover | 1 | 26.5 | The sheet is retired (once November reconciles) |

### Target dates (starting Monday 12 October 2026)
- **Release 1 (daily form, opening balances, installable app):** in use from **14 October**, with real data from **1 October** (decision 2026-10-14). Staff enter daily data in both the app and the sheet; 1–14 October is backfilled.
- **Kassa and settlement ready:** done (M7, M8), in time to close October.
- **Cutover:** when October reconciles and Anna approves. Target: the first half of November 2026. The sheet stays the source of truth until then.

M2 can run in parallel with M1. M6 and M7 can overlap. M9 can move after cutover if time is short; it isn't needed for settlement.

## 5. Risks

| Risk | Impact | Mitigation |
|---|---|---|
| Missing kassa movements, as in the sheet's −€968 cash balance in September. | Balances wrong; settlement disputes. | Weekly recount with adjustment rows. Staff-entered movements need confirmation. Movements are voided, never deleted, with a full audit trail. |
| Staff forget «Оставьте в кассе», or leave a different amount. | Balances drift. | The suggestion is pre-filled. The effect line shows the result in plain words. The balance is always visible in «Мои записи». |
| Staff disagree about how certificates are paid out (the spec example changes from 375/375 to 325/425). | Confusion in the first month. | Explain it at onboarding. The certificate-sale flag is a setting if the owners change their minds. |
| Rounding differs from the sheet (floats vs cents). | ±1 cent differences. | Integer cents with largest-remainder dividends. Differences up to 1 cent per person are documented as expected. |
| A mistake found in an older closed month. | It has to be reopened. | Only the newest closed month can be reopened, so later months are reopened first (newest to oldest) and closed again in order. Rare, audited, needs a confirmation, and no snapshot can ever be stale. |
| Opening figures missed or wrong (especially outstanding certificates). | Kassa or certificate totals off from day one. | Captured on the morning of 1 November with a checklist (technical-solution §5.6). `--dry-run` confirmed by Anna. Negative outstanding certificates show a warning. A weekly recount catches cash differences. |
| Cloud Run domain mapping is still a preview feature. | Delay. | Already used for `<domain>` and `staging.<domain>` in this project. Fallback: an HTTPS load balancer with a serverless NEG. |
| iOS PWA quirks (keyboard, separate cookies). | Annoying daily form. | `sticky` bar, real-iPhone checks in M5 and M11, one login inside the installed app. |
| Staff don't build the habit. | Gaps in data. | Early Release 1, in-person onboarding, under 60 seconds per day, «Не сдали отчёт» on the admin's day screen. |

## 6. Not in this plan
- Reminders and notifications (for example «Не забудьте заполнить отчёт» through the existing Telegram bot).
- Importing Timely customer and booking statistics (customers, new customers, bookings, online bookings from «3 FutureValue Input»). This can be added later as an extra monthly import.
- Photos of expense receipts.
- Multiple salons, languages other than Russian, a staging environment.
