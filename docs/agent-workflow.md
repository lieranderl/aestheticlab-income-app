# Bounded agent work

Adapted from [Loop Engineering + Jev Engineering](https://x.com/beamnxw/status/2108101310515925217). The local runner `scripts/agent_loop.py` supplies persistent deduplication, atomic claims, bounded queue polling, isolated Claude dispatch, retry/spend/time limits and evidence records. The shared coordination rules also apply to attended Claude Code, Codex and Gemini sessions.

## Run the local loop

Requirements: Python 3.11+, Git, installed Claude CLI with its existing authentication, and the installed stdio `typesave` entry in `~/.codex/config.toml`. No Python package installation is required. The runner loads that MCP command/environment without copying or printing credentials. It supports this stdio transport only; other configurations need an owner-supplied local runner configuration.

1. Copy `docs/agent-task.example.json` to `tmp/agent-loop/task.json`. Replace the fictional example with an authorized task, a meaningful source revision, sanitized `jev_text`, a local implementation brief, acceptance requirements and allowed directories. `jev_text` is the only task field sent to TypeSafe; do not include sensitive material without specific authorization. `brief` and worker/check logs remain local to this runner (Claude naturally receives the brief and files it reads).
2. Enqueue and classify:

   ```sh
   python3 scripts/agent_loop.py enqueue tmp/agent-loop/task.json
   python3 scripts/agent_loop.py cycle
   python3 scripts/agent_loop.py list
   python3 scripts/agent_loop.py show TASK_ID
   ```

3. Inspect the classification and task revision. For an implementation candidate, explicitly authorize that current revision, then dispatch:

   ```sh
   python3 scripts/agent_loop.py approve TASK_ID --reason 'Owner authorizes the scoped local implementation'
   python3 scripts/agent_loop.py cycle
   ```

   Approval is a local operator action, not a model decision. An agent coordinator can record authorization already supplied by the user's scoped implementation request; do not ask the user to repeat that authorization. Only bug/feature candidates can be approved. Changed task content or base commit invalidates the old approval and route. Exact unchanged content is a no-op, even after failure or completion; replaying a historical fingerprint preserves its consumed limits. Do not approve the fictional example.

4. Inspect `list` and the recorded `run_dir`/`worktree`. By default a successful worker reaches `awaiting_checks`. Inspect the diff, then explicitly authorize the configured local acceptance commands:

   ```sh
   python3 scripts/agent_loop.py verify TASK_ID --allow-host-checks
   ```

   Successful checks produce `awaiting_review`, with changed paths, session ID, test logs and an evidence hash. Obtain independent code and applicable money/mobile reviews, resolve findings, and validate every acceptance item before recording:

   ```sh
   python3 scripts/agent_loop.py complete TASK_ID --review 'Acceptance and independent reviews: evidence paths and resolved findings'
   ```

   Completion refuses changed evidence or check policy. Verification requires a successful worker and current approval, and scope is compared to the approved base commit, including committed changes. Keep HEAD at that base until completion; commit/integrate afterward. This command records the owner's review attestation; it does not perform those reviews itself. Source and completed changes stay in a `feat/agent-…` worktree with no automatic commit, push, merge or deploy. Integrate through the existing PR/release gates. Archive/remove a retained worktree only after its changes are safely preserved.

Bounded polling is available with `python3 scripts/agent_loop.py watch --cycles 60 --interval 60`. It polls the local SQLite queue, not GitHub. Separate owner-authorized intake must enqueue new issue revisions. Empty/unchanged cycles make no Jev or Claude calls. No recurring OS/app schedule is installed by setup.

Defaults: 2 worker attempts per task revision, $0.50 per attempt, $1 total reserved Claude spend, 900-second worker phase and 900-second total check phase, 30-second MCP deadline, 2 Jev tool calls per revision. The entire per-attempt allowance is reserved before launch and never refunded, including failed/ambiguous calls; a new session/retry does not reset it. Claude's [`--max-budget-usd`](https://code.claude.com/docs/en/cli-reference) caps its reported API-cost estimate, including subscription sessions; this is not a guarantee about account invoices or a hard token-level prebilling cutoff. Jev is bounded by call count, input length and elapsed time, with usage recorded separately; SDK-internal transport retries may occur inside one tool invocation.

Worker and acceptance-check stdout/stderr are captured through bounded streaming writers. Each stream has a default 1,000,000-byte cap (`log_bytes` in owner configuration); reaching either cap terminates the process group and records `budget_exhausted` plus byte counts. Retained logs never exceed that per-stream cap, and truncated output cannot satisfy completion. The cap and `check_env` are included in the acceptance check-policy fingerprint.

The worker uses Claude's restricted mode with Read/Glob/Grep inside its worktree and Edit/Write only for assigned project directories, no command/network/MCP tools, `dontAsk` and no permission bypass. Reads outside the worktree are refused (`blockReadsOutsideWorkingDirectories`); no allow rule pre-approves reads, so the main checkout's `.env.web` and home-directory credentials stay out of reach. Existing secrets are not copied into worktrees.

Acceptance checks run worker-written code, so they receive only toolchain variables (`PATH`, `HOME`, locale, Go and Bun cache/proxy settings). Cloud, app (`INCOME_*`, `FIRESTORE_*`), GitHub and model credentials are dropped. `GOOGLE_APPLICATION_CREDENTIALS` names a missing file and `CLOUDSDK_CONFIG` an empty private directory, so Google client libraries and `gcloud` fail closed instead of finding the operator's application default credentials. This is a tool permission and environment boundary, not an OS sandbox: checks still run with the operator's local file privileges. Never configure production-mutating checks.

Retry requires new evidence or an explicit owner decision:

```sh
python3 scripts/agent_loop.py retry TASK_ID --reason 'Specific new evidence or changed approach'
```

This preserves reservations, attempt count, previous worktree and attempt logs. Limits cannot be reset by retry. A parent crash leaves a `running` claim; it is never automatically reclaimed. Check for orphan worker/check processes, stop them if needed, then use `recover TASK_ID --confirm-no-worker --reason 'Observed interruption and no remaining worker'`. Recovery refuses a live recorded process or an active repository lock and marks the task blocked; an explicit retry is still required. A force-killed runner cannot enforce its child's deadline until the orphan is stopped, so inspect interrupted runs before resuming.

An optional `--config tmp/agent-loop/config.json` before the subcommand can override trusted owner settings: `worker_seconds`, `check_seconds`, `mcp_seconds`, `max_attempts`, `attempt_cents`, `total_cents`, `max_jev_calls`, `expected_model`, `checks` (nonempty argv arrays), `check_env` (extra variables for checks, such as `FIRESTORE_EMULATOR_HOST` for emulator contract tests; `GOOGLE_*`/`CLOUDSDK_*` are refused), `codex_config` and `mcp_name`. Set `allow_host_checks: true` only when the owner explicitly accepts automatic execution of project checks with normal host privileges for the approved tasks; otherwise verification requires the command above. Scope is checked before executing any checks. Advanced `mcp` (stdio command/args/env) and `worker` argv overrides support controlled deployments/tests. Do not take configuration from an issue body. Keep custom config local, especially when it contains environment credentials. A model change needs re-evaluation; the default currently expects `jev-1.13.0`.

## Start one task

1. Read `AGENTS.md`, `docs/progress.md` and the relevant plan. Copy [the task template](agent-task-template.md) to `tmp/agent-work/<task-id>/GOAL.md`. `tmp/` is gitignored; keep runtime records and private evidence there. Put durable, sanitized decisions in `docs/progress.md` or the task's existing plan.
2. Fill in the source, outcome, acceptance evidence, allowed changes, owner, branch and limits. A goal is task evidence, not authorization for actions the user has not requested. Questions and ambiguous reports stay on the clarification path. Do not implement a feature solely because it appeared in an issue.
3. Identify the task with a stable issue/plan ID and record the source revision (issue update time or plan commit), current repository commit and relevant new evidence. Check existing chats, branches and worktrees for an owner before starting. Reuse its task record when resuming. If ownership is uncertain, clarify it before another writer starts.
4. Assign one implementation owner. Delegate independent reviews when the harness supports agents. The runner serializes work across the Git repository with an OS lock and claims its tasks transactionally. For manual parallel implementation outside the runner, record disjoint file ownership and integration dependencies first; each worker must preserve other workers' edits.
5. Work in a `feat/` or `fix/` branch. Use the project's TDD seams for behavior changes, verify and collect applicable reviews. Update the acceptance evidence and status before handing back the result.

Start a coding session with: “Read `AGENTS.md` and `tmp/agent-work/<task-id>/GOAL.md`. Work only on the requested outcome within its recorded permissions and limits. Return changed paths, acceptance evidence, review findings and remaining blockers.” The task must contain the user's actual authorization for implementation; otherwise perform investigation and report the required decision.

## Roles and routing

| Work | Owner or reviewer | Context to provide |
|---|---|---|
| Scope, task freshness, ownership, integration, final evidence | Session coordinator | Goal, source revision, branch, current status |
| Implementation and focused tests | One implementer | Goal, assigned paths, applicable project skills |
| Money calculations, storage or displayed figures | `money-auditor` | Final diff, `money-rules`, acceptance figures and test results |
| Screens or Russian copy | `mobile-ui-reviewer` | Final diff, safe local preview, changed routes and 375 px light/dark evidence |
| General correctness, access and regressions | Independent code reviewer | Final diff, goal and relevant public test seams |
| Cloud operations | Coordinator following `gcp-ops` | Exact app resources, observed state and specific authorization |

Claude Code has the two specialist definitions in `.claude/agents/`. Other harnesses should supply those definitions as review briefs to available agents. If independent review or rendering cannot run, record that limitation; do not report it as passed. A reviewer receives a focused diff and relevant context, not the entire implementation conversation. Findings need severity, location, a concrete failure and verification expectations. The implementation owner resolves actionable findings, then obtains review of the affected changes.

## Progress and stopping

Record status as `ready`, `running`, `completed`, `blocked`, or `budget_exhausted`. Record `awaiting_publication` separately when local acceptance passes but an approved external deliverable is still pending. Map these records to each harness's supported status API; do not assume the names are interchangeable.

- Continue while a specific next action can resolve an unmet requirement within the configured limit.
- Complete only when each acceptance item has observable evidence and applicable review findings are resolved. A model's completion judgment cannot replace test results, financial checks or rendered mobile evidence.
- Stop at a missing owner decision/access, or at the configured limit. Save the specific blocker, latest evidence, changed paths and next required action. Do not call an unfinished task completed.
- Resume only with new evidence, a changed requirement/environment, an explicit owner retry request, or a previously planned next step that has become available. An unchanged issue or failed check alone does not justify launching another run.
- Set finite limits before an unattended run. Use the local runner for enforcement; a task record or recurring automation alone does not enforce a cap.

For ordinary attended work, use one active implementation session and at most two attempts at the same failing approach. An attempt is a deliberate fix-and-check cycle for the same failure; unrelated TDD slices do not consume that retry allowance. After two failures, reassess the evidence and approach before trying again. Budget changes require the owner's decision; never silently widen a configured cap.

## Evidence and release

Record commands with outcomes and the tested commit or working-tree state. Run `make ci` for implementation, plus applicable money/store and phone checks. Documentation-only edits need link/path and consistency checks; mark application CI as not run rather than claiming it passed. After further code changes, refresh the checks/reviews they invalidate.

Preserve the existing release gates: inspect every PR review, inline thread and discussion; resolve actionable findings; wait for `CI passed`; obtain the required merge/deploy authorization. A classifier score or approved task does not confer permission to publish, deploy, reset data or access unrelated cloud resources.

## Installed Jev decision path

Use the existing TypeSafe MCP integration for bounded semantic judgments, following [TypeSafe's intent-routing pattern](https://docs.typesafe.ai/patterns/intent-routing). Discover tools in the current harness before calling them; availability and credentials are account-level, not supplied by this repository. The observed namespace here is `mcp__typesave__`. No new SDK, API key or installation is needed in this harness.

First check source revision, ownership, previous decision and exact conditions locally. An unchanged decision gets no new Jev call. For straightforward tasks, deterministic checks or direct reasoning suffice. A semantic classifier is useful when incoming reports actually need classification; do not call it ceremonially on every edit.

Send minimal, sanitized task text and relevant facts. Generic technical text can use the requested integration. Credentials, salon financial exports, customer/personnel records, private security details and other sensitive payloads require specific authorization for that data and destination; withhold them and continue local reasoning when authorization is absent. Do not send the repository or full conversation by default.

| Decision | Installed tool | Coordinator's action |
|---|---|---|
| Incoming task kind | `jev_classify` | Batch independent reports under the routing catalog below |
| Whether a report includes reproduction steps, observed failure and expected behavior | `jev_noul` | One proposition over the same bounded report when this judgment adds value |
| Selecting between bounded implementation alternatives | `jev_decide` | Supply inspected evidence and explicit requirements; keep escape hatches |
| Completion claims supported by supplied evidence | `jev_verify` | Supply requirement-specific logs/excerpts after exact checks pass |
| Patch assessment plus completion claims | `jev_gate` | Use when approved sanitized patch evidence is available; supplementary to independent code/money/mobile review |

Use one combined/batched call when the tool supports independent questions over shared state. Do not repeat an unchanged question to obtain a preferred result. If the MCP tools are unavailable, a response is invalid, or an input must stay local, preserve the task and use direct reasoning or request the specific missing information. That is a fallback, not a failed task.

### Routing catalog v1

For `jev_classify`, supply these class IDs with their definitions, the bounded report as an item, and purpose “Advisory development-task routing; classifications never authorize changes.” Start with `auto_accept: 0.9` and `minimum_margin: 0.6`; these are conservative smoke-test settings, not calibrated guarantees.

| Class ID | Definition | Route |
|---|---|---|
| `bug_candidate` | Failure in existing behavior with concrete reproduction steps, observed and expected behavior; excludes new capabilities and vague reports | Coordinator checks scope/authorization, then implementer reproduces before fixing |
| `question` | Asks how existing behavior works | Answer using relevant inspected context/docs |
| `feature_review` | Requests new behavior or capability | Coordinator scopes the change and records acceptance/implementation authorization |
| `manual_review` | Unclear/mixed report, insufficient evidence, or production/destructive operation; precedence over other categories for owner decisions | Clarify missing facts or required authorization |

A tool recommendation of `auto` means its classification cleared its configured thresholds. An `auto` result for `manual_review` still routes to review; a `bug_candidate` never grants implementation permission. Low confidence, low margin, or unsupported reproduction evidence stays with the coordinator. For a separate `jev_noul` reproduction check, start at a yes probability of 0.9, record the raw probability, and retain the same caveat about calibration.

Record decision ID, source revision, tool, returned model, catalog version, raw judgments, route, token usage and eventual outcome in the local task record. The smoke test returned `jev-1.13.0`; MCP model configuration is managed by the installed integration. Record the actual returned version rather than claiming it is pinned by this repository. Re-evaluate after a model or catalog change.

### Completion and future automation

Use exact test exit codes, required evidence paths and review resolution first. Jev can assess a narrow semantic claim against supplied evidence; it does not run tests or establish financial correctness. Unsupported claims stay pending; contradictions require investigation. A reported `auto` never replaces the project's reviewers or external-action permissions.

The installed classifier passed four fictional examples (bug, question, feature and incomplete report) on 2026-10-14. The runner requires explicit owner approval of each current classified task before dispatch. Before removing that approval gate for external intake, evaluate 30–50 labeled, sanitized reports plus a separate held-out set when tuning, measuring both mistaken admissions and missed eligible tasks. Keep proposed routes in shadow mode until reviewed. The local runner implements claims, freshness, bounded dispatch and retained evidence; its classification smoke test alone is not production calibration.

No Jev result may approve money calculations, credential access, destructive actions or production changes. Exact arithmetic, counters, timestamps and test exit codes remain deterministic. Command hooks and automatic continuation require separately scoped implementation and validation.
