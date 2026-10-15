# Local agent-loop verification

Scope: `scripts/agent_loop.py`, process-boundary fixtures/tests, Makefile integration and workflow instructions. Application behavior, money logic and screens are unchanged.

Independent read-only review found and re-reviewed fixes for:

- MCP pipe-write deadlock and malformed result shapes stranding claims.
- Descendants surviving process-leader exit and ignoring SIGTERM.
- Untracked symlink retargets escaping evidence invalidation.
- Historical task revision replay resetting consumed limits.
- Partial failed worker output reaching completion through verification.
- Committed changes disappearing from scope checks against HEAD.
- Changed check policy leaving stale acceptance valid.
- GitHub P2: unbounded worker/check stdout and stderr could exhaust disk before a deadline. Streaming per-file caps now stop the group, retain bounded output and block completion; the cap is part of the verified policy.

Host check execution is an explicit owner choice: default dispatch stops at `awaiting_checks`, scope is inspected before checks, and `verify --allow-host-checks` or trusted owner configuration authorizes project-code execution with local privileges. Worker tool restrictions do not constitute an OS sandbox for those checks. Completion records independent review/acceptance; it does not fabricate a review.

Final independent verdict, including the log-cap follow-up: no remaining blocking findings within that documented trust boundary. All 16 regression tests pass, and final `make ci` passes including generated assets, lint, race tests, security and build. Additional review probes confirmed simultaneous stdin/stdout/stderr backpressure does not deadlock and closed output pipes do not bypass deadlines. No additional money/mobile review is applicable to these tooling-only changes.

Live test: disposable fictional Git repository under `/private/tmp`, installed Jev triage → explicit current task approval → installed restricted Claude → isolated worktree output → explicit acceptance command → controller inspection → completion. Jev returned `jev-1.13.0`; Claude produced only `docs/result.md` with `READY` plus newline, reported $0.019808 against a $0.20 allowance, and returned a recorded session ID. Two empty polling cycles processed zero tasks. No application/cloud data was used or modified.

Scope limits: local queue intake, no GitHub polling or installed schedule, no automatic publication; Claude dollar limits refer to its reported API-cost estimate. A force-killed parent needs explicit orphan inspection/recovery. These limits are documented in `docs/agent-workflow.md`.
