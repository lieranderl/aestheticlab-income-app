# AGENTS.md

**Aesthetic Lab Kassa** (`<app-host>`) is an internal, phone-first PWA for the Aesthetic Lab salon in Brussels. Four people use it (in these docs, fixtures and tests: Anna = admin, Maria, Olga, Vera — fictional names, not the real staff). It covers:
- per-client revenue entries and daily totals;
- the salon kassa (cash till + the salon's card) and each person's balance with the salon;
- the monthly settlement (salary, dividends, who pays whom).

It replaces the «Aesthetic Lab Finance» Google Sheet.

**Stack:** Go 1.27 · templ · Datastar v1 (SSE, no client framework) · Tailwind 4 + daisyUI 5 · Firestore (named DB `aestheticlab-income`) · Cloud Run (GCP project `<project>`, europe-west1). The UI is **Russian only**. Money is **int64 euro cents**.

**Production only:** service/database `aestheticlab-income` at `<app-host>`, start month `INCOME_START_MONTH=2026-10`.
- **October 2026 is the first real month (decision 2026-10-14).** It runs in parallel with the sheet, which stays the record until October reconciles. Openings are dated 2026-10-01, and 1–14 October is backfilled from the sheet.
- Treat all October production data as real: no test entries, and never delete October records. The earlier practice entries were voided in the app and stay.
- Trial resources were retired; do not recreate them. See `docs/MAINTAINING.md`.

`CLAUDE.md` and `GEMINI.md` are symlinks to this file. Skills live in `.agents/skills/` (`.claude/skills` is a symlink to it).

## Read first
| Need | Doc |
|---|---|
| What we build and why; money rules; security; backups | [docs/technical-solution.md](docs/technical-solution.md) |
| Data model, write paths, routes, screens, testing | [docs/system-design.md](docs/system-design.md) |
| Build order, milestones, "Done when" | [docs/implementation-plan.md](docs/implementation-plan.md) |
| Milestone status and follow-ups | [docs/progress.md](docs/progress.md) |
| Ops: setup, deploy, restore, secrets | [docs/MAINTAINING.md](docs/MAINTAINING.md) |
| How the AI tooling is set up | [docs/AI_AGENTS.md](docs/AI_AGENTS.md) |

**Client entries (2026-10-13):** services only earn «Себе»; cream/oil/certificate sales go fully to selected cash/card kassa. Optional certificate redemption is included in service value and creates no new cash/card inflow. Customer labels/time automatic. Preserve frozen legacy daily records/snapshots; see docs/plans/client-entries.md.

**Signed-in identity (PR #25):** all authenticated pages pass `views.Nav` from `navFor`; the phone header and desktop sidebar show the current actor's name/avatar and link to `/profile`. Admin edits retain the admin's identity. The login page has no badge.

## Non-negotiables
1. **Money correctness beats everything.**
   - Cents only (never floats); percentages in basis points.
   - All formulas live in `internal/domain`. The server recomputes; the client only previews.
   - Ledger rows are voided, never deleted.
   - Every financial write appends an audit row in the same transaction.
   - Load the **`money-rules`** skill before touching money.
2. **Access:** an employee only ever reads or writes their own data. Only the admin confirms movements, pays out, recounts the kassa, and closes or reopens months.
3. **Phone first.** Every screen works at 375 pt with no horizontal scroll, a bottom bar, ≥ 44 pt targets and ≥ 16 px inputs. Tables only at `lg:`. See the **`mobile-ui`** skill.
   - **Clarity:** separate inputs, optional explanations and read-only results. Visible labels include units; essential constraints/errors never depend on tooltips. Dashboards show period → primary totals → grouped breakdown → people/records. Distinguish period contributions, current balances and projected/frozen settlement figures. See `docs/system-design.md` §6.1a and the 2026-10-13 mobile UI audit.
4. **Datastar first.** No custom JS except `static/draft.js` and `static/sw.js`. No inline `style` (CSP `style-src 'self'`). Never interpolate data into Datastar expressions.
5. **DaisyUI first.** Semantic tokens only. Brand hex colours exist only in `frontend/app.css`.
6. **Russian copy** uses the glossary in `mobile-ui`: «Себе», «Салону», «Касса», «Оставьте в кассе», «Салон должен вам», and so on. Wording is gender-neutral.
7. **Cloud safety.** Other salon services share project `<project>`. Never touch Firestore `(default)` or other Cloud Run services. Read before you change, and ask before anything destructive or outward-facing. Load **`gcp-ops`** first.
8. **Secrets** never go in the repo: `.env.web` and the CSV exports from the sheet stay local.

## Work loop
For multi-session or delegated work, use [docs/agent-workflow.md](docs/agent-workflow.md) and its task goal template: one implementation owner, focused reviewer handoffs, recorded acceptance evidence and explicit stop/retry limits. The local `scripts/agent_loop.py` runner provides deduplication, atomic claims, isolated Claude dispatch and finite limits; dispatch requires owner approval of the current task revision. Check existing ownership before launching another writer. Use installed Jev tools for useful semantic decisions, following the guide's input boundaries and fallback. Exact checks stay deterministic.

1. **Plan** briefly. Name the test seams; list unresolved questions at the end.
2. **TDD in vertical slices** (the **`tdd`** skill): one failing test at a public seam, the minimum code, repeat.
3. **Verify:** `make ci`. For screens, also check the phone viewport in light and dark themes (the **`mobile-ui-reviewer`** agent). For money, use the **`money-auditor`** agent.
4. **Ship:**
   - Branch `feat/…` or `fix/…`, never commit to `main`.
   - Conventional Commit messages; open a PR with `gh pr create`.
   - Squash-merge a PR (`gh pr merge --squash --delete-branch`) only after its `CI passed` check is green.
   - Before merging, read every review, inline review thread, and discussion comment on the PR. Resolve all actionable issues and verify the final changes; a green CI check alone is insufficient. Never merge with unresolved review issues.
   - A quota/availability message from automated review is not a completed review. Perform an independent code review and applicable money/mobile reviews before merging.
   - A branch ruleset on `main` enforces this: changes arrive only by pull request, the `CI passed` check must be green, every review thread must be resolved, and `main` can't be deleted or force-pushed. The `.githooks/pre-push` hook also blocks direct pushes to `main` locally. `make hooks` installs it, and so does any `make assets`, `templ`, `test`, `web`, `web-dev`, `lint` or `ci` run.
   - Record progress in `docs/progress.md`.

Run a milestone end to end with **`/milestone M<N>`**.

## Commands
| Command | What it does |
|---|---|
| `make dev` | **The everyday local run:** Firestore emulator + first data (prints anna's temporary password once) + Air hot reload on http://localhost:8080. Always uses the emulator, whatever `.env.web` says (`.claude/launch.json` → `kassa-web`) |
| `make dev-october` / `make dev-memory` | Ready October testing on in-memory data without `.env.web` or the emulator; Anna's October terms ready, data/login reset on restart. `DEV_START_MONTH` / `DEV_PORT` customize local memory runs. |
| `make firestore-reset` / `make firestore-stop` | Wipe the emulator's data and seed again / stop the emulator |
| `make web-dev` / `make web` | App with the settings in `.env.web`, with / without Air |
| `make test` | `bun test` (frontend helpers) + `go test -race ./...` |
| `make firestore` / `make test-store` | Firestore emulator on :8086 via Apple `container` (keeps a running one) / store contract tests against it |
| `make mobile` | Playwright phone checks on `cmd/e2e` (memory store, seeded staff, fixed November clock): every screen at iPhone SE (3rd gen) / iPhone 15 / Pixel 7 in light and dark, the client/movement/payout flows and the 4G speed budget. First run: `bunx playwright install chromium webkit` |
| `make help` | Every target, grouped |
| `make lint` | templ check, assets check, `go vet`, staticcheck, golangci-lint, gofmt |
| `make security` | govulncheck + gosec |
| `make ci` | Everything CI runs: assets, templ, lint, test, security, build |
| `make templ` / `bun run assets` | Generate templ Go code / build CSS and icons |
| `make image` | Production image (distroless), built with Apple's `container` CLI locally (start its service once: `container system start`), Docker in CI |

## Skills (`.agents/skills/`)
- **Project skills:** `money-rules`, `mobile-ui`, `firestore-store`, `gcp-ops`, `tdd`, `milestone`.
- **Stack skills:** `go-backend`, `datastar-expert`, `daisyui-expert`.
- **General Go (MIT, samber/cc-skills-golang):** `golang-testing`, `golang-security`, `golang-error-handling`, `golang-code-style`, `golang-lint`, `golang-modernize`, `golang-design-patterns`, `golang-concurrency`.

## Plan mode
Keep plans very short; grammar can suffer for concision. End with unresolved questions, if any.
