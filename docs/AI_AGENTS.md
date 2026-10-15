# AI agent setup

This repo is set up so that Claude Code, Codex and Gemini CLI all work from the same instructions.

## Task coordination

Use [Bounded agent work](agent-workflow.md) for task intake, ownership, reviewer routing, evidence, stopping and resuming. `scripts/agent_loop.py` runs a local SQLite queue with direct installed-Jev triage, current-revision owner approval, isolated Claude dispatch, finite polling/time/attempt/spend caps and preserved evidence. Copy the [task goal template](agent-task-template.md) for attended sessions or the JSON example for runner intake. The setup adapts the [Loop Engineering + Jev Engineering article](https://x.com/beamnxw/status/2108101310515925217) to the project's review/release rules. No GitHub watcher, recurring schedule or automatic publication is enabled.

## Instructions
- `AGENTS.md` holds the instructions. `CLAUDE.md` and `GEMINI.md` are symlinks to it. Codex reads `AGENTS.md` directly.
- `.gemini/settings.json` also points Gemini at `AGENTS.md`.
- Read `docs/progress.md` for current work and `docs/plans/client-entries.md` for the reporting model introduced in PR #24. Older daily-form descriptions are legacy; new entries split services only, with extras fully credited to the selected kassa account.
- Shared signed-in identity lives in `views.Nav.User`, populated by `navFor` from the authenticated user. Use it in the phone header and desktop sidebar, including when an admin edits someone else's entry; `/login` has no user badge.
- Mobile information architecture follows `system-design.md` §6.1a: separate entry, help and results; group analytical metrics; distinguish contributions, current balances and settlement estimates. The [mobile audit](reviews/2026-10-13-mobile-ui/REVIEW.md) records the implementation, before/after screenshots and verification limits; deployment status is in `progress.md`.

## Skills
- **Location:** `.agents/skills/<name>/SKILL.md`. Codex reads this directory; Claude Code reads `.claude/skills`, which is a symlink to it.

| Skill | When it loads |
|---|---|
| `money-rules` | Any money maths, money storage or money UI. It includes the acceptance figures from the Google Sheet. |
| `mobile-ui` | Any screen or Russian copy (375 pt checklist, glossary, formats). |
| `firestore-store` | `internal/store`, seed, import, indexes. |
| `gcp-ops` | Before any gcloud command; lists who owns what in the shared project. |
| `tdd` (`/tdd`) | Building behaviour; the agreed test seams. |
| `milestone` (`/milestone M<N>`) | Running a milestone from the implementation plan through to a PR. |
| `go-backend`, `datastar-expert`, `daisyui-expert` | The stack, adapted from datastar-webapp. |
| `golang-*` | General Go practice (MIT, samber/cc-skills-golang; see `LICENSE-cc-skills-golang`). |

## Claude Code project config (`.claude/`)
- **`settings.json`:**
  - **Allowed without asking:** make, go test/vet/build, templ, bun, read-only git, gh PR status, and read-only gcloud on this app.
  - **Ask first:** deploy, traffic shifts, DNS changes, restore, `git push`, repo creation, PR merge.
  - **Denied:** force-push, pushing to `main`, deleting databases, services, secrets or the DNS zone, and reading `.env*`.
- **`hooks/format.sh`:** runs `gofmt` / `templ fmt` on every Go or templ file Claude edits.
- **Subagents** (`.claude/agents/`):
  - `money-auditor`: a read-only review of money changes.
  - `mobile-ui-reviewer`: static checks plus a render in the built-in browser at the phone preset.
  - These are Claude Code agent definitions. In other tools, delegate the same review instructions when delegation is available, or run the checks directly; report any render or review that could not run.
- **`launch.json`:** `kassa-web` runs `make dev` on port 8080 (Firestore emulator, seed, hot reload; October by default). `kassa-web-memory` runs `make dev-memory` (temporary local data/login, October terms ready). The legacy `kassa-web-trial` entry also runs memory only; it is not a cloud trial environment. Prefer `make dev-october` for a disposable October session; customize local runs with `DEV_START_MONTH` / `DEV_PORT`.
- **Configured runs:** `make web` / `make web-dev` read `.env.web` and can target its configured database. Use `make dev` or `make dev-memory` for local review/testing.
- **Personal overrides** go in `.claude/settings.local.json` (gitignored).

## MCP servers (`.mcp.json`, and the same list in `.gemini/settings.json`)
| Server | Why |
|---|---|
| `gopls` (`gopls mcp`, gopls ≥ v0.20) | Go-aware search, references, diagnostics and package API summaries. |
| `playwright` (`@playwright/mcp@0.0.82 --device "iPhone 15"`) | Phone-sized browser automation for agents without a built-in browser (Codex, Gemini). |

**Codex** has no project-level MCP file. Add the servers once:
```bash
codex mcp add gopls -- gopls mcp
```
```bash
codex mcp add playwright -- npx -y @playwright/mcp@0.0.82 --device "iPhone 15" --isolated
```

**Account-level integrations, when connected, are not repeated here:**
- **Jev / TypeSafe MCP:** installed and tested through `mcp__typesave__jev_classify`. Use the task-routing and evidence-check recipes in `agent-workflow.md`; authentication stays in the existing integration. Other harnesses must check their available tools; these docs do not install or connect an MCP server. A four-case fictional routing smoke test on 2026-10-14 returned the expected categories using `jev-1.13.0` (778 input tokens). This is connection validation, not a calibrated production evaluation.
- **Context7:** current library docs for Datastar, templ, daisyUI and Firestore.
- **Google Sheets:** read the «Aesthetic Lab Finance» sheet, id `14UskLYqMfY926IOmTlpBnA6YutSvxfb5uXQZs_OLB4I`.
- **The built-in browser** in the Claude desktop app.
- **gcloud and gh CLIs:** used instead of cloud MCP servers.

## Review and release
- Read PR reviews, inline threads and discussion comments before merge, and resolve every actionable finding. If automated review reports a quota limit, it has not reviewed the change: complete an independent code review plus the applicable money/mobile reviews before merge. Green CI alone is insufficient.
- `make ci` checks generated files, workflows, lint, Bun draft regressions, Go race tests, security and the app build. GitHub also tests the Firestore emulator, builds the container image and scans secrets/workflow security.
- On `main`, `.github/workflows/ci.yml` calls `web.yml` after `CI passed`; the deployment skips a commit superseded on `main`, checks the no-traffic candidate's `/readyz`, then promotes traffic. A successful PR run does not deploy. Documentation-only merges also deploy a new revision (pushes to `main` always run the full CI); a docs-only PR skips the lint/test/build, emulator and phone-check jobs, and «CI passed» accepts only that skip.
- Production remains `aestheticlab-income` only, with cutoff `2026-10`. October 2026 is the first real month (decision 2026-10-14), run in parallel with the sheet: no cleanup and no cut-off change; the earlier practice entries are voided and stay. Follow `MAINTAINING.md` → Real October.
- M0–M4, M6–M9 and M11 are done. M5 is not finished: it still needs the October openings and real-phone checks. The outstanding cutover work (openings, 1–14 October backfill, real-phone acceptance, April–September history import, October reconciliation with Anna's sign-off) is listed in `implementation-plan.md` §1.

## Editors
`.zed/settings.json` comes from datastar-webapp: format on save for Go and templ, gopls with staticcheck, and Tailwind IntelliSense inside templ files.
