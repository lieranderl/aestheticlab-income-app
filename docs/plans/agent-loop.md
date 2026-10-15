# Local agent loop

The requested workflow uses the installed Jev MCP server to triage changed tasks before starting Claude. Implement a dependency-free Python 3.11+ CLI under `scripts/agent_loop.py`, with local SQLite state in `tmp/agent-loop/`.

- Intake: owner-authored JSON task files separate sanitized Jev text from the coding brief, explicit implementation authorization, allowed directories and observable acceptance criteria.
- Queue: content fingerprints prevent unchanged tasks from triggering another call; SQLite transactions claim tasks; an OS file lock serializes workers sharing the repository. Changed input invalidates previous approval and route. Failed/interrupted tasks need an explicit retry reason.
- Jev: reuse the `typesave` stdio MCP entry from the existing Codex TOML config, including its environment without logging secrets. Bound request time/count and reject malformed, truncated, uncertain or unexpected-model results. The protocol bridge sends only `jev_text`, not code, financial records or worker logs.
- Worker: isolated Git worktree, restricted Claude file tools, no shell/network/MCP tools, no permission bypass, per-attempt dollar cap, total reserved dollar cap, elapsed deadline and attempt cap. Preserve every attempt's output and session ID.
- Completion: execute trusted checks outside the agent with a deadline, record changed paths and scope violations. Passing implementation reaches `awaiting_review`; owner records required independent reviews before `completed`. Publishing remains separate.
- Operator commands: enqueue, list, cycle, bounded watch, approve, retry, recover and complete. No background schedule or production operation is enabled during setup.
- Tests: CLI/SQLite behavior, concurrent claiming, MCP protocol/process boundary, fake worker execution, timeout/process termination, budget reservation, explicit retry, stale decisions, scope and completion. Add tests to `make ci`.

Initial defaults: 2 attempts, 900 seconds per worker/check phase, $0.50 per attempt / $1 reserved total per task revision, 2 Jev calls per revision, 30-second MCP deadline. Dollar caps describe Claude's API-cost estimate (including subscription runs), not an invoice guarantee; request-count/text caps bound the Jev side. All caps are owner configuration, not issue-body instructions.

No unresolved design questions prevent local implementation. Full automatic dispatch from unreviewed external issues requires evaluated routing and separately authorized intake; the initial runner requires explicit owner approval of a current task revision.
