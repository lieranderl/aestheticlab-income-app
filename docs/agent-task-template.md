# Task goal

Copy to `tmp/agent-work/<task-id>/GOAL.md` and replace every placeholder before starting. This is an acceptance record, not an executable queue or permission grant.

## Identity and owner

- Task ID / source URL or plan section:
- Source revision / last update:
- Repository commit at start:
- Implementation owner / session:
- Branch / worktree:
- Status: ready
- Last update / new evidence:

## Jev decision, when useful

- Decision ID / unchanged-input check:
- Sanitized state sent / destination: existing TypeSafe integration.
- Tool / returned model / routing rubric version:
- Returned category or verdict / probabilities / confidence / margin:
- Tool recommendation / coordinator's route and reason:
- Token usage / result record path:
- Missing tool, withheld sensitive input or classifier failure: direct reasoning / specific clarification.

Leave this section as not applicable when deterministic checks or direct reasoning suffice. Jev's `auto` recommendation is a judgment about its answer, not permission to launch an agent or publish.

## Requested outcome and scope

- User's request and explicit implementation authorization:
- Observable outcome:
- Assigned paths / allowed changes:
- Dependencies / other owners:
- Excluded work:
- External actions specifically authorized, if any:

## Acceptance evidence

| Requirement | Required check | Observed evidence / path | Result |
|---|---|---|---|
| Requested behavior | Public seam or reproducible observation | | pending |
| Regression checks | `make ci` for implementation; path/link checks for docs only | | pending |
| Money, if applicable | Acceptance figures, store contracts, money review | | pending / not applicable |
| UI, if applicable | 375 px light/dark, interactions, screenshots, mobile review | | pending / not applicable |
| Independent code review, if applicable | Final diff reviewed; actionable findings resolved | | pending / not applicable |

## Limits and retry decisions

- Execution mode: attended or `scripts/agent_loop.py`; use its JSON intake for enforced limits.
- Maximum repeated attempts at the same failing approach: 2.
- Unattended runner caps, if applicable: attempts / elapsed time / spend; enforcement mechanism:
- Retry evidence or explicit owner request:
- Stop on: acceptance satisfied; missing decision/access; configured limit reached.

## Handoff / final result

- Changed paths / tested revision:
- Checks and reviews passed, failed or unavailable:
- Unresolved findings / specific blocker:
- Terminal status: completed / blocked / budget_exhausted.
- Publication state, if applicable: awaiting_publication / published; authorization and URL:
- Next action, owner and evidence required to resume:

Keep private logs/screenshots in the local task directory. Record sanitized durable progress in `docs/progress.md`. An unchanged source is not a new task, and a new session does not reset the retry history.
