---
name: firestore-store
description: Firestore persistence patterns for the Kassa app — named database, store interface with memory and Firestore implementations, transactions with optimistic versions and audit rows, void-not-delete, indexes, emulator tests. Use when touching internal/store, cmd/seed, cmd/import, firestore.indexes.json, or any code that reads/writes reports, ledger, months, users or categories.
---

# Firestore store

Source: `docs/system-design.md` §2–4. Pattern copied from `~/Documents/Projects/datastar-webapp/internal/store`.

## Setup
- **Database:** named database `aestheticlab-income` in project `<project>`. **Never** touch `(default)`: it belongs to the Telegram bot (`calendar_telegram_*`).
- **Client:** `firestore.NewClientWithDatabase(ctx, project, database)`, configured by `INCOME_FIRESTORE_PROJECT` and `INCOME_FIRESTORE_DATABASE`. If they're unset, the app runs on the in-memory store (local dev and HTTP tests).
- **Access:** server-only. The security rules deny everything; the app uses the runtime service account. No client SDK, no Firebase Auth.

## Shape
- **The rules are written once.** `*store.Store` (`store.go`, plus `users.go`, `catalog.go`, `reports.go`, `ledger.go`, `months.go`, `audit.go`) holds every check and transaction, over a small `backend`/`tx` interface (`db.go`: `get`, `query` on string fields, `set`). Handlers use `*store.Store`.
- **Two backends hold the documents:**
  - `memory.go`: JSON documents in a map behind one lock. Used for HTTP tests and local runs. It refuses a read after a write in a transaction, as Firestore does, so such a bug fails in the fast tests too.
  - `firestore.go`: production. It refuses the `(default)` database.
- **Records** are private structs with `firestore` and `json` tags that match the field names in system-design §2. Snapshots have explicit records too, so renaming a domain field can never make a stored snapshot read back as zero.
- **Shared contract tests** (`store_test.go`) run against both backends: memory always, Firestore when `FIRESTORE_EMULATOR_HOST` is set (`make test-store`, and the `store` CI job). Each Firestore run uses its own emulator project.
- **Errors:**
  - `ErrNotFound`, `ErrVersionConflict`, `ErrMonthClosed`, `ErrForbidden`, `ErrUsernameTaken`, `ErrAlreadySetUp`, `ErrInvalid`.
  - Rule violations come back as `*domain.ValidationError` with a `Problem`: the domain's, plus the store's `opening_date`, `not_pending`, `already_voided`, `note_required`, `unknown_party` and `not_history`.
  - Handlers turn them into Russian messages.
- **Options:** `WithClock`, `WithLocation` (from `INCOME_TZ`), `WithStartMonth` (from `INCOME_START_MONTH`, default 2026-11).

## Write rules
1. **Every write runs inside `RunTransaction`. Do all reads before any write** (a Firestore rule).
2. **Optimistic concurrency:**
   - Reports and ledger rows carry `version`.
   - The caller passes the version it loaded.
   - A mismatch returns `ErrVersionConflict`.
   - Writes set `version+1`.
3. **Audit:** the same transaction appends `audit/{autoId}` with `entity`, `entityId`, `action`, `actorId`, `at`, and `before`/`after` snapshots.
4. **Never delete money.**
   - Ledger rows become `status=voided` with `voidReason`.
   - Reports are edited, not deleted. An admin "delete report" is a void flag plus an audit entry.
   - Categories are archived (`active=false`) once `usedCount > 0`.
5. **Month lock:** the report and ledger transactions read `months/{YYYY-MM}` first. A closed month rejects **every** write, the admin's included, with `ErrMonthClosed`. Months close in order and only the newest closed one reopens (system-design §3.4–3.5).
6. **Money terms are effective-dated** in `users/{uid}/terms/{YYYY-MM}`. Read the doc with the greatest ID ≤ the month being calculated, never today's values. Writing terms for a closed month is rejected.
7. **Use deterministic IDs where uniqueness matters:**
   - `reports/{uid}_{YYYY-MM-DD}`;
   - `usernames/{lowercase}` (written in the same transaction as the user);
   - `months/{YYYY-MM}`.
8. **Compute derived fields with `internal/domain` before writing.** The store never re-implements a formula.
9. **Transactions:** at most ~500 writes each. Month close writes about 8 accruals plus a snapshot, well under the limit.

## Reads
- **Balances:** start from the latest closed month's `snapshot`, then read the open months (system-design §4.1). Never scan all history on a request.
- **Indexes:** every new composite query needs an entry in `firestore.indexes.json`, deployed with `gcloud firestore indexes composite create` (see `docs/MAINTAINING.md`). The emulator doesn't enforce indexes, so list each new query in your PR description.
- **Timestamps** are UTC `time.Time`. Business dates are strings (`date "2026-10-08"`, `month "2026-10"`) worked out in `Europe/Brussels`.

## Testing
- `make firestore` starts the emulator on `localhost:8086` (image `google-cloud-cli:…-emulators`). Locally it runs with Apple's `container` CLI, so run `container system start` once per boot; CI uses Docker. Then run `make test-store`.
- **Clock:** inject it through `WithClock(func() time.Time)`; never call `time.Now()` directly in store code.
- **Required contract cases:**
  - a version conflict;
  - a closed month rejecting writes from both an employee and the admin;
  - closing out of order and reopening an older month both rejected;
  - an audit row for every write;
  - a voided row being ignored by balances;
  - close → reopen → close giving identical snapshots.

## Seed and import
- `cmd/seed`: runs once. `Store.SetUp` refuses (`ErrAlreadySetUp`) when any user exists. It writes `store.DefaultSetup`: Anna's admin account (temporary password printed once), the categories, payment methods, expense categories and kassa accounts.
- `cmd/import`: reads CSV files passed as arguments (never commit them) and writes `importedMonths/*` and the `opening` ledger rows. Always run it with `--dry-run` first and check the totals against the sheet.
