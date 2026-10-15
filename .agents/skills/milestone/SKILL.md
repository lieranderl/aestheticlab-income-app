---
name: milestone
description: Drive one milestone of docs/implementation-plan.md from start to merged PR — read its tasks and "Done when", branch, TDD slices, verify (make ci, phone viewport), PR, and record progress. Use when the user says "start M3", "continue the milestone", "what's next", or invokes /milestone.
user-invocable: true
argument-hint: "[M0..M12]"
---

# /milestone: work through the implementation plan

## 1. Load context
1. **Find the milestone.**
   - Read `docs/implementation-plan.md`.
   - If an argument like `M4` is given, use that milestone.
   - Otherwise take the first milestone not marked ✅ in `docs/progress.md`. Create that file if it's missing.
2. **Read what the plan points to**: the sections of `docs/technical-solution.md` and `docs/system-design.md` that the milestone mentions.
3. **Load the skills it touches:**
   - `money-rules` for any money code;
   - `firestore-store` for the store;
   - `mobile-ui` for screens;
   - `gcp-ops` for cloud work;
   - `datastar-expert` and `daisyui-expert` for templates.

## 2. Plan in short
List the milestone's tasks as a checklist, plus the test seams you'll use (see the `tdd` skill), plus open questions. Keep it short. Ask only when a choice is really the user's to make. Otherwise pick the convention and say so.

## 3. Build
- **Branch** from `main`: `feat/m<N>-<slug>`. Never commit to `main`.
- **One TDD slice at a time.** After each green slice, make a focused commit (Conventional Commit message).
- **Before touching the cloud or GitHub settings,** follow `gcp-ops`: read first, and ask before anything destructive or outward-facing.

## 4. Verify against "Done when"
- `make ci` is green.
- **For screens:**
  - Start the dev server with `preview_start` (`.claude/launch.json`, name `kassa-web`).
  - Check at the mobile preset in light and dark themes.
  - Take screenshots.
- **For money:** the acceptance table in `money-rules` passes.
- **Go through each "Done when" bullet** and say ✅, or say what's missing, with evidence (command output, a screenshot, a URL).

## 5. Ship
- `git push -u origin HEAD`, then `gh pr create` with:
  - a Conventional Commit title;
  - the milestone checklist;
  - verification evidence;
  - new Firestore queries or indexes, if any.
- **Merging:** this repo has no auto-merge or branch rulesets (they need GitHub Pro for a private repo). Wait for the PR's `CI passed` check. When it's green, run `gh pr merge --squash --delete-branch`, but only with the user's go-ahead for that PR.
- Update `docs/progress.md`: milestone, status, PR link, date, notes and follow-ups.

## Rules
- **Don't widen scope.** Anything outside the milestone goes into `docs/progress.md` under "Follow-ups".
- **Report honestly.** If a "Done when" item can't be checked (for example it needs a real iPhone), say so; don't mark it ✅.
