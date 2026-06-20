# Revert a commit (pipeline #8) — design

## Goal

Create a new commit that undoes the changes of a selected commit, from the TUI
Commits panel and the CLI, integrating any conflicts into the existing
conflict-resolution flow (resolve files → continue / abort).

This is the structural twin of #7 cherry-pick (`REVERT_HEAD`, `revert
--continue/--abort`): the additions are kept copy-paste-shaped.

## Scope

- Revert the **selected commit** onto the **current branch** in the **current
  worktree**. No "onto another branch" rung (v1).
- Single, non-merge commit only. Reverting a **merge commit** needs `-m
  <parent>` mainline selection — **refused in v1** (git itself refuses without
  `-m`; we also pre-check in the TUI for a clean message).
- Mirrors `engine.CherryPick`: apply, and on conflict fork via a `Decider`
  (`keep-conflicts` / `abort`). The CLI answers the fork from a flag policy.

## Dirty working tree

**Auto-stash then restore** (same as CherryPick/SmartMerge): stash if dirty,
revert, pop after a clean finish; on a kept-conflict (or failure) the stash
survives and the summary notes "your changes remain stashed".

## Git verbs (`internal/git/revert.go`)

One invocation each, honoring `dir` ("" = this worktree):

- `Revert(ctx, dir, commit) error` → `git revert --no-edit <commit>`
- `RevertContinue(ctx, dir) error` → `git revert --continue` (carries
  `GIT_EDITOR=true`)
- `RevertAbort(ctx, dir) error` → `git revert --abort`
- `RevertInProgress(ctx, dir) (bool, error)` → exit-code probe of `git rev-parse
  -q --verify REVERT_HEAD` (the MergeInProgress pattern)
- `RevertHeadSummary(ctx, dir) (string, error)` → `git log -1 --format=%h %s
  REVERT_HEAD` (attribute a conflict)

## Engine op (`internal/engine/revert.go`)

`type Revert struct{ Commit string }`, mirroring `CherryPick.Run`:
1. require `Commit != ""`.
2. autostash if `IsDirty`.
3. `git revert`; on success pop the stash, return `reverted <commit>`.
4. on error, probe `RevertInProgress`:
   - not in progress → refused outright (bad ref, or **a merge commit without
     `-m`**): return git's error verbatim (legible), stash preserved.
   - in progress with **0 conflicted files** → an empty/redundant revert (the
     change is already undone): **auto-abort** with a legible error (never trap
     the resolver), as CherryPick does.
   - in progress with conflicts → `decide("revert-conflict", keep-conflicts/
     abort)`: keep leaves the tree (error + summary), abort runs `RevertAbort`.

## Conflict integration (probed LAST, after cherry-pick)

`engine.ContinueOp`/`AbortOp`, `domain.conflictState`/`InProgressOp`, and the
e2e `in_progress` probe gain a **revert** branch, probed **after** cherry-pick:

  merge → rebase → cherry-pick → **revert**

(A paused rebase pick can set CHERRY_PICK_HEAD; revert has its own REVERT_HEAD,
but ordering revert last keeps the established "specific states win, generic
last" discipline and matches the e2e probe order.) `ConflictState.Describe`
gains "reverting <commit>".

## TUI (`internal/tui/commit_scope.go`)

`commitRevertRow() (actionRow, bool)`: gated `m.focus == panelCommits &&
m.opsIdle()`, commit via `backingIndex(panelCommits)` → `m.commits[bi]`. **Merge
guard**: if `len(commit.Parents) > 1`, the row still appears but its `run` sets
`statusMsg = "cannot revert a merge commit (v1)"` (clean message, never starts a
doomed op). Otherwise `run` → `m.startOp(engine.Revert{Commit: hash})`. Wired
into `availableActions` next to Cherry-pick; advertised in `help.go`.

## CLI (`internal/cli/revert.go`)

`gg revert [--on-conflict=keep|abort] <commit>` → `runOperation(... engine.Revert
{Commit}, cliDecider{...} ...)`. Mirrors `gg cherry-pick`. Bump
`agentskill.Version`, document in `using-gg.md`, refresh the dogfood SKILL.md
(`gg init --update`) — the `TestDogfoodSkillCopyInSync` gate fails otherwise.

## e2e (`e2e/scenarios/`)

- `revert_clean.toml`: a commit that added a file; `gg revert` removes it again,
  exit 0, a new "Revert …" commit on top.
- `revert_conflict_keep.toml`: a conflicting revert with `--on-conflict=keep`,
  exit 1, `in_progress = "revert"`.
- `revert_conflict_abort.toml`: `--on-conflict=abort`, exit 0, tree restored.

The e2e `in_progress` probe learns `REVERT_HEAD` (after the rebase-* and
CHERRY_PICK_HEAD probes).

## Out of scope (v1)

Reverting merge commits (`-m`), ranges, `--no-commit`. #9 reset is the last
pipeline item.
