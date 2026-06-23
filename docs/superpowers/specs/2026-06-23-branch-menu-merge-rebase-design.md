# Design: Branches `.` menu Merge/Rebase (parity with Remotes & Tags)

**Status:** Design spec (approved to build).
**Date:** 2026-06-23.
**Branch:** `feat/branch-menu-merge-rebase` (off `main` @ `f904156`).

## Goal

Give the **Branches** tab `.` menu the same one-click **Merge** and **Rebase**
actions the **Remotes** and **Tags** tabs already have, so a local branch can be
merged into / rebased onto the current branch without the two-step `m`-mark
gesture. Closes the parity gap surfaced after the remote-row leak fix
(`f904156`).

## What exists (context)

- Remotes: `remoteMergeRow` / `remoteRebaseRow` (`remote_actions.go`).
- Tags: `tagMergeRow` / `tagRebaseRow` (`tags_actions.go`).
- Branches: **no** merge/rebase row — only the two-step `m`-mark pair gesture
  (`mark.go`, `pairOpsFor(panelBranches)`).
- Engine: `SmartMerge{Source, Target}` (Target defaults to current),
  `SmartRebase{Branch, Onto}` (Branch defaults to current) — fully sufficient.

## Design

Two new row builders in a new `internal/tui/branch_actions.go`, mirroring the tag
versions exactly, appended in `availableActions` alongside the remote/tag rows.

```
branchMergeRow():  "Merge <branch> into current (<cur>)"  -> SmartMerge{Source: b.Name}
branchRebaseRow(): "Rebase current (<cur>) onto <branch>" -> SmartRebase{Onto: b.Name}
```

**Gating** (both rows):
- `m.focus == panelBranches` — tab-scoped, applying the leak-fix lesson so they
  never appear on Remotes/Worktrees/Tags.
- a branch selected via `selectedBranch()`.
- **`!b.IsHead`** — the one difference from the remote/tag versions: a local
  selection can be the current branch, and merging/rebasing a branch with itself
  is degenerate. (The remote/tag rows omit this because a remote ref/tag can never
  equal the current local branch.)
- attached HEAD via `remoteCurrentBranch()` (for the "into current (`cur`)" label;
  hidden on detached HEAD — same as the remote/tag rows).

Conflicts/dirty-tree resolve through the existing `SmartMerge`/`SmartRebase`
Decider ladder mapped to the TUI modal — no new handling.

## Explicitly out of scope

- **Not** making the menus identical: `rename`/`pull` stay Branches-only,
  `prune`/`checkout` stay Remotes-only (tab-appropriate). Only the merge/rebase
  pair reaches parity.
- **TUI-only**, matching the remote/tag rows (no `gg merge`/`gg rebase` CLI exists
  for those either).
- Interactive rebase stays on the `m`-mark gesture (already there); not duplicated
  as a one-click row.

## Testing

New `branch_actions_test.go`, mirroring `tags_actions_test.go`:
- `branch-merge` / `branch-rebase` present with a **non-current** branch selected
  on the Branches tab.
- **Absent on the current branch** (`IsHead`).
- **Absent when another tab is focused** (the cross-tab guard — same shape as the
  leak regression test).
- Absent on detached HEAD (`status.Branch == ""`).
- Rows' `run` dispatches `SmartMerge` / `SmartRebase` (non-nil cmd).

## Acceptance

- Branches `.` menu shows Merge/Rebase for a non-current selected branch; running
  them starts `SmartMerge`/`SmartRebase` against the current branch.
- No leak to other tabs; no degenerate self rows; hidden on detached HEAD.
- Full suite + `./test.sh race` green; CHANGELOG updated.
