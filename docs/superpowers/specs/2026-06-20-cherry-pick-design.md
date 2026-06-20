# Cherry-pick a commit (pipeline #7) — design

## Goal

Apply the changes of a selected commit onto the current branch as a new
commit, from the TUI Commits panel and the CLI, integrating any conflicts
into the existing conflict-resolution flow (resolve files → continue / abort).

## Scope

- Cherry-pick the **selected commit** onto the **current branch** in the
  **current worktree**. No "cherry-pick onto another branch" rung (v1) — the
  Commits-panel action means "land this commit on where I am now".
- Single commit only (no ranges) in v1.
- Structurally mirrors `engine.SmartMerge`: apply, and on conflict fork via a
  `Decider` (`keep-conflicts` / `abort`). The CLI answers the fork from a flag
  policy; the TUI answers it from the modal and then drives resolution.

## Dirty working tree

**Auto-stash then restore** (user decision, mirrors `SmartMerge`): if the tree
is dirty, `StashPush` before the cherry-pick and `StashPop` after a clean
finish. On a kept-conflict (or any failure) the stash is **not** popped — it
survives and the summary notes "your changes remain stashed", exactly as
`SmartMerge` does.

## Git verbs (`internal/git/cherrypick.go`)

One invocation each, built with `gitcmd`, honoring `dir` ("" = this worktree):

- `CherryPick(ctx, dir, commit string) error` → `git cherry-pick <commit>`
- `CherryPickContinue(ctx, dir string) error` → `git cherry-pick --continue`
  (carries `GIT_EDITOR=true` env so it never opens an editor for the message)
- `CherryPickAbort(ctx, dir string) error` → `git cherry-pick --abort`
- `CherryPickInProgress(ctx, dir string) (bool, error)` → exit-code probe of
  `git rev-parse -q --verify CHERRY_PICK_HEAD` (the `MergeInProgress` pattern:
  exit 0 = yes, exit 1 = no, other = error).

## Engine op (`internal/engine/cherry_pick.go`)

```
type CherryPick struct{ Commit string }
```

`Run`:
1. Require `Commit != ""`.
2. Autostash if `IsDirty` (emit `Progress{Step:"stashing"}`).
3. `emit Progress{Step:"cherry-picking", Detail: Commit}`; call `CherryPick`.
4. On success: pop the stash if stashed (a pop conflict surfaces like
   SmartMerge's), return `Result{Summary:"cherry-picked <commit>", Changed:true}`.
5. On error: probe `CherryPickInProgress`.
   - Not in progress → refused outright (bad ref, empty/redundant commit that
     git rejects before starting): return the error verbatim (legible), stash
     preserved.
   - In progress → conflict. `decide("cherry-pick-conflict", keep-conflicts/abort)`:
     - `keep-conflicts`: return `Changed:true` + a conflict summary and an
       error (`cherry-pick conflict: <commit>`); leave the tree for the
       resolver. If stashed, append "(your changes remain stashed)".
     - `abort`: `CherryPickAbort`; return aborted summary, `Changed:false`.

## Conflict integration (the part that makes it not a trap)

A cherry-pick conflict leaves unmerged index entries, so `status.Conflicts()`
already lists the files and the existing resolver can resolve them. But
`canContinue()`/`canAbort()` gate on the in-progress probe, which today only
knows merge/rebase — so without the additions below the user resolves every
file and is then **trapped** (no continue, no abort). Therefore:

- `domain.conflictState`: add a `CherryPickInProgress` branch →
  `ConflictState{Op: "cherry-pick", Source: <picked commit subject/hash>}`.
- `engine.ContinueOp` / `engine.AbortOp`: add a cherry-pick branch
  (`CherryPickContinue` / `CherryPickAbort`).
- TUI conflict-process probe (`loadInProgressCmd`): report `"cherry-pick"`.

**Probe ordering is load-bearing: merge → rebase → cherry-pick.** A paused
interactive-rebase *pick* also sets `CHERRY_PICK_HEAD`; if cherry-pick were
checked first, a rebase conflict would misroute to `git cherry-pick
--continue/--abort`. Cherry-pick is therefore checked **last** in all three
sites. A real-git test pauses a rebase on a conflict and asserts the probe
still reports "rebase".

## Empty / already-applied commit edge

Cherry-picking a commit whose changes are already in HEAD stops with
`CHERRY_PICK_HEAD` set but **zero** conflicted files. That makes
`canContinue()` true ("all resolved — [c] continue"), yet `git cherry-pick
--continue` errors on an empty pick. v1 requirement: the error is **legible**
and **abort works** — the user is never stuck. (No special-case auto-skip in
v1; surfacing git's own message is acceptable.)

## TUI (`internal/tui/commit_scope.go`)

`commitCherryPickRow() (actionRow, bool)`: gated `m.focus == panelCommits &&
m.opsIdle()`, commit via `backingIndex(panelCommits)` → `m.commits[bi].Hash`
(full SHA). `run` → `m.startOp(engine.CherryPick{Commit: hash})`. Wired into
`availableActions` near the other commit rows; advertised in `help.go`.

## CLI (`internal/cli/cherrypick.go`)

`gg cherry-pick <commit>` → `runOperation(ctx, svc, engine.CherryPick{Commit},
cliDecider{...}, stderr)`. On a conflict the `cliDecider` answers from its flag
policy (default: keep-conflicts, leaving the tree resolved-by-hand, mirroring
the CLI's existing merge/rebase conflict policy) or stdin when interactive.
Bump `agentskill.Version` and document the subcommand in `using-gg.md`.

## e2e (`e2e/scenarios/`)

- `cherry-pick-clean.toml`: commit on a side branch, switch to main,
  `gg cherry-pick <sha>`, assert the file content / commit landed on main.
- `cherry-pick-conflict.toml`: a conflicting commit; assert the op reports a
  conflict and the tree is left with unmerged paths (CLI keep-conflicts).

## Out of scope (v1)

Ranges, `--onto another branch`, `-x`/`--signoff` flags, mainline selection for
merge commits, auto-skip of empty picks. #8 revert is the same shape
(`REVERT_HEAD`, `revert --continue/--abort`) — the cherry-pick additions are
kept copy-paste-shaped so revert is nearly free, but not abstracted now (YAGNI).
