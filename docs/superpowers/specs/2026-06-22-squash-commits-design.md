# Squash selected commits — design

**Date:** 2026-06-22
**Status:** approved (brainstorm)
**Branch:** `feat/squash-commits`

## Problem

On the Commits panel today, marking two commits (`m` then `m`) immediately
opens a whole-tree diff. There is also a separate multi-select set (`◉`, via the
`.`-menu "Add to compare selection") whose `.`-menu "Compare selection" diffs the
set (2 = tree diff, 3+ = combined range diff). Two parallel selection concepts,
and the only action reachable from marking is *compare*.

We want marking to be a pure selection step, and the action (compare **or
squash**) to be chosen afterward from the `.` context menu.

## Goals

1. Collapse the two Commits-panel selection concepts into **one selection set**.
2. From that selection, the `.` menu offers **Compare** (unchanged) and **Squash**.
3. Squash combines the selected commits into one, concatenating their messages.

## Non-goals

- No change to the Branches panel mark / pair-op machinery (`m` there keeps the
  single-mark merge/rebase/interactive-rebase popup).
- No squash of commits that are not on the current branch / not reachable from
  HEAD's first-parent range.
- No squash across merge commits (the engine already refuses these).
- Reorder-then-squash of non-adjacent commits is **Stage 2** (designed here, not
  built in Stage 1).

## Staging

- **Stage 1 (this spec's implementation target):** unified selection set + `.`-menu
  Compare/Squash, where Squash operates on **adjacent** selected commits and
  refuses gaps with a note.
- **Stage 2 (deferred, design recorded below):** when the selection has gaps,
  offer to reorder the commits adjacent first, then squash.

Each stage is its own branch and merge.

## Mark semantics change (Commits panel only)

This is a user-visible behavior change and is intentional:

- **`m` on a commit toggles its membership in the selection set `◉`** — the same
  toggle as today's `.`-menu "Add/Remove from compare selection". The second `m`
  no longer auto-opens a diff.
- The `.` context menu is the single place an action is chosen on the selection.
- **"Compare with marked" (the single-`◆` mark row) is removed from the Commits
  panel**; the set subsumes it. The single-mark `markState` machinery remains for
  the Branches panel.
- `handleMarkKey`'s Commits branch is rewritten: instead of single-mark +
  second-mark-auto-compare, it toggles `commitCompareSet` membership (reusing the
  existing toggle logic behind `commitCompareToggleRow`).

## Squash mechanics (Stage 1)

### Availability (the `.`-menu "Squash N commits" row)
- `m.focus == panelCommits` and ops idle.
- Current branch checked out (`m.status.Branch != ""`).
- 2 or more commits in the selection set.
- **No WIP rows** (working-tree / staged pseudo-rows) in the selection.
- Oldest selected commit is not a root commit (it needs a parent to rebase onto).

### Order and adjacency — from the range, never the feed
The squash Pick/Squash order and the adjacency test are derived from
`domain.CommitRange(oldestSelected + "^", branch)` — the real `onto..HEAD`
first-parent range, oldest-first. **They are never derived from the Commits feed
order or `compareKeyRank`**, because the feed is multi-branch and date/plain
ordered and would mis-order the plan.

- Resolve each selected commit's index within the loaded range.
- **Off-HEAD guard:** if any selected commit is absent from the range, refuse
  with *"can only squash commits on the current branch"*.
- **Adjacency:** the selected indices are adjacent iff `max-min+1 == len(targets)`.

### Plan and dispatch (adjacent case)
New pure builder `rebaseplan.BuildSquash(commits []model.RangeCommit, targets []string) (Plan, error)`:
- Validates all `targets` are present in `commits` and `len(targets) >= 2`.
- In range (oldest-first) order: the **oldest** target is `Pick`; every other
  target is `Squash`; all non-target commits stay `Pick`.
- Errors if targets are not adjacent (so the caller can detect and, in Stage 2,
  offer reorder). Stage 1 maps this error to the refusal note.

Dispatch the existing op:
`engine.InteractiveRebase{Branch: m.status.Branch, Onto: oldestSelected + "^", Plan: plan, GGBin: <self>}`,
mirroring the move/drop flow (`rebaseRangeLoadedMsg` → build plan → `startOp`).

### Non-adjacent case (Stage 1)
Refuse with: *"selected commits aren't adjacent — squash needs adjacent commits
(reorder coming soon)"*.

### Commit message
`rebaseplan.Message()` already returns the target's message followed by a blank
line and each squashed commit's message stacked in the body — exactly the
"concatenate all" behavior chosen. The user can reword afterward with the
existing reword action.

### Conflicts and merges
- Conflicts during the rebase ride `InteractiveRebase`'s existing
  `rebase-conflict` decision (keep-conflicts / abort).
- Merge commits in the range are already refused by `InteractiveRebase`.

## Reorder-then-squash (Stage 2 — deferred, design only)

When the selection has gaps, a confirm appears: *"These commits aren't adjacent.
Reorder them adjacent, then squash?"* → **Reorder & squash** / **Cancel**.

Resulting order — selecting **c1 and c3**, skipping **c2**, range oldest→newest
`[c1, c2, c3, c4]`:

```
before:  c1  c2  c3  c4          (c1, c3 selected)
after:   c1+c3 (squashed)  c2  c4
```

The selected commits collapse into the **oldest selected's slot**; the skipped
in-between commits (`c2`) **float to just after** the squashed commit, preserving
their relative order. Conflicts are possible and ride the same conflict decision.

Stage 2 adds a `reorder` variant to the builder (or a `BuildSquashReorder`) plus
the confirm modal, with placement unit tests for the before/after above.

## Testing

### rebaseplan (pure — the crux)
- `BuildSquash`: adjacent 2 commits, adjacent 3+, non-adjacent → error, missing
  target → error, oldest-as-`Pick`/rest-as-`Squash` ordering.
- `Groups()` / `Message()` concatenation over a built squash plan.
- (Stage 2) reorder placement tests for the §Stage 2 before/after.

### TUI
- `m` on Commits toggles the selection set (not auto-diff); second `m` does not
  open a diff.
- "Squash N commits" row appears only when eligible (branch, 2+, no WIP, non-root)
  and is absent otherwise.
- Refusals: off-HEAD selection, WIP-in-selection, non-adjacent.
- Real-repo integration on a **multi-commit linear history** (loaded model, not a
  clean 1-commit repo) proving the dispatched `Onto = oldestSelected^` and the
  Pick/Squash plan — mirroring the move/drop integration tests. Guards the
  display-vs-backing trap (selection resolved from the range, asserted on a
  non-trivial repo).

## Files touched (Stage 1)

- `internal/rebaseplan/` — new `BuildSquash` (new file `squash.go` + test).
- `internal/tui/mark.go` — Commits branch of `handleMarkKey` toggles the set.
- `internal/tui/commit_scope.go` — new "Squash N commits" row; remove
  "Compare with marked" for Commits.
- `internal/tui/op.go` / `internal/tui/model.go` — a squash range-load message +
  handler (or reuse `rebaseRangeLoadedMsg` with a squash variant), mirroring
  move/drop.
- `internal/tui/help.go` — help text for the changed `m` semantics + squash.
- `CHANGELOG.md` — entry.
