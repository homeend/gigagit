# Recycle a worktree — design

**Date:** 2026-09-28 · **Branch:** `feat/recycle-worktree` · **Status:** spec, awaiting review

## Goal

From the Branches tab, check a branch out **in an existing worktree that gg is
not running in**, replacing whatever that worktree has checked out now. If that
worktree has uncommitted work, the user picks what happens to it first. This
is the reuse counterpart of `W` (create a fresh worktree for a branch): on a
~100 GB monorepo a worktree is expensive, so an idle one should be recycled
rather than a new one added.

## What the user sees

### TUI

1. Branches tab, `.` on a **local** branch that **no worktree has checked
   out** (the exact inverse of the existing "Show in Worktrees" row; the two
   rows are mutually exclusive) shows a new row **"Recycle a worktree…"**.
   The row needs ops idle. Remote-only branches are out of scope (follow-up).
2. Picking it re-populates the same type-to-filter menu with **one row per
   worktree**: the worktree path elided in the middle (file-name rule) plus
   its current branch (or "detached"). Excluded: the worktree gg runs in
   (that case is "Switch to branch") and bare worktrees.
   Detached worktrees are listed. A worktree with a running agent session
   (the Worktrees-tab sub-rows) carries a marker and choosing it asks
   "An agent session is running in this worktree. Recycle anyway?"
   (Recycle / Cancel).
3. Enter runs the `RecycleWorktree` op. If the target worktree is clean,
   the branch is switched in place, no further prompt.
4. If the target has staged, unstaged **or untracked** changes, the op raises
   the `recycle.dirty` decision, rendered as the usual modal:

   ```
   <path> has uncommitted changes on <branch>
     commit     Commit everything (including untracked files) on <branch>
     discard    Throw everything away (untracked files are deleted; ignored files are kept)
     abort
   ```

   Esc = abort. Nothing is changed on abort.
5. After the switch gg stays where it is, refreshes Branches and Worktrees,
   and the status line reads `recycled <path>: <old-branch> → <branch>
   (committed|discarded|clean)`. The existing "Switch to branch → go there"
   path remains the way to jump into that worktree.

Refusals surface as the op's error in the status line, before anything is
touched:

- the target has a rebase/merge/cherry-pick/revert in progress;
- the target holds a git lock file;
- the branch is already checked out in some worktree (a background refresh
  raced the menu);
- the target is bare or does not exist.

### CLI

```
gg worktree recycle <path> <branch> [--on-dirty=commit|discard|abort]
```

`<path>` is the worktree top level (absolute or relative to the cwd).
`--on-dirty` pre-answers the `recycle.dirty` decision through the existing
decider policy map; without it an interactive terminal is prompted on stdin
and a pipeline gets `abort` (nothing is destroyed unseen). Exit codes follow
the other worktree verbs (`0` ok, `1` refused/failed, `2` usage). The op
summary is printed as usual.

## Engine

### The cross-worktree seam

Every op today runs on the current worktree through `deps.Repo` (a
`GitOps`); the only dir-targeted verbs are `ResetInDir` and `ShowFileInDir`.
Rather than add six more `XInDir` copies, `*git.Repo` gains

```go
// InDir returns a view of the repository whose every git invocation runs
// against the worktree at dir (git -C dir …). dir must be a worktree top
// level of this repository.
func (r *Repo) InDir(dir string) *Repo
```

returning a `*Repo` with `Root = dir` and a `Runner` wrapper that prefixes
the argv with `-C <dir>` (the wrapper lives in `internal/git`; `FakeRunner`
therefore sees the prefix in every recorded argv). Nothing else about the
verbs changes. `ResetInDir`/`ShowFileInDir` stay as they are (not in scope
to migrate).

Ops reach the view through a new `OpDeps` seam, in the style of
`HookRunner`/`CaptureRunner` (a `*git.Repo` method cannot return engine's
`GitOps`, so the seam is injected rather than declared on the interface):

```go
RepoAt func(dir string) GitOps // nil ⇒ repoAt returns ErrNoRepoAt
```

`domain.Execute` sets it to `s.repo.InDir`; engine tests inject the same.
`GitOps` itself stays single-worktree.

### `RecycleWorktree`

```go
type RecycleWorktree struct {
    Dir    string           // target worktree top level
    Branch string           // local branch to check out there
    Now    func() time.Time // clock for the commit message; nil = time.Now
}
```

Reservation: `TreeWrite` (the default). The gate is keyed by the git common
dir, so one reservation already covers every worktree of the repo.

Run:

1. **Pre-checks**, all before any mutation, each a plain error:
   - `Dir` is one of `Worktrees()` and not bare; otherwise
     `worktree <dir> not found`.
   - `Branch` is not checked out in any worktree (`Worktrees()` again;
     covers the race with a refresh), otherwise
     `<branch> is already checked out in <path>`.
   - No paused operation in the target (`git.PausedOpIn` on the target's
     git dir, resolved from `<dir>/.git`), otherwise
     `<path> has a <rebase|merge|…> in progress`.
   - No lock files in the target (`git.LockFiles`), otherwise
     `<path> is locked (<file>)`.
2. `wt := deps.Repo.InDir(op.Dir)`; `old := wt.CurrentBranch()` ("detached"
   when empty).
3. `st := wt.Status()`. Dirty means `Staged+Unstaged+Conflicted+Untracked > 0`
   (note `IsDirty` ignores untracked, so the op reads the counts itself).
4. If dirty: `decide(recycle.dirty, options [commit, discard, abort])`.
   - `commit`: `Progress{committing}`, `wt.StageAll()` (git add -A), then
     `wt.Commit("Committed changes due to worktree recycle <YYYY-MM-DD
     HH:MM>", all=false, amend=false)`. The timestamp is local time from
     a `Now func() time.Time` field on the op (nil = `time.Now`; tests set a
     fixed one). A detached target commits onto the detached HEAD; the
     summary then names the commit sha so it is findable in the reflog.
   - `discard`: `Progress{discarding}`, `wt.RestoreWorktree([":/"])` then
     `wt.CleanUntracked([":/"])` (the same repo-root pathspec `Discard{All}`
     uses; ignored files untouched because `clean` runs without `-x`). Both
     run even if the first fails; errors are joined.
   - `abort` (or any other answer, or a decider error): return
     `Result{}.WithSummary("recycle cancelled")`, nil. Nothing changed.
5. `Progress{switching}`, `wt.Switch(op.Branch)`. On failure after a commit
   the commit stays (it is the user's work, now safe); after a discard there
   is nothing to restore. The error is returned unchanged.
6. `Result{Changed: true}` with summary
   `recycled <path>: <old> → <branch>` plus `; committed <sha>`,
   `; changes discarded`, or nothing when it was clean.

Decision id constant: `RecycleDirtyDecisionID = "recycle.dirty"`. Option
values stay English (agent-facing protocol); only rendering is localized.

`opAffectedSources` maps the op to branches + worktrees (+ status, harmless).

## Domain

No new query. `Execute` runs the op as any other. The TUI's picker reads
`m.worktrees` (already refreshed by the worktree source) and the session
marker from `domain.Sessions()`.

## Follow-ups (recorded, not built)

- **Shelve** as a third answer, once the multi-file shelf lands (another
  session is adding it). It would be: shelve every changed file of the
  target, then discard, then switch — a domain step before the op, since the
  shelf store is domain-owned.
- **Remote-only branches** (`origin/foo` with no local branch): create the
  tracking branch, then recycle.
- **Web UI**: the same row on the branch context menu, a worktree picker, and
  the `recycle.dirty` decision through the parking web Decider.

## Testing

- `internal/git`: `InDir` prefixes `-C <dir>` (FakeRunner argv), and a real
  repo with two worktrees answers `Status`/`CurrentBranch` for the other one.
- `internal/engine` (real git, main + one extra worktree): clean switch;
  dirty + commit (message text with a fixed clock, untracked file included,
  branch switched); dirty + discard (untracked deleted, ignored file kept);
  dirty + abort (tree untouched, decision seen); refusals: paused merge,
  lock file, branch checked out elsewhere, unknown dir; detached target.
- `internal/tui`: row shown only when the branch is checked out nowhere and
  hidden when it is (mutual exclusion with "Show in Worktrees"); picker rows
  exclude the current and bare worktrees; enter dispatches
  `RecycleWorktree{Dir, Branch}`; the modal renders the three options; the
  session-marker confirm. i18n gate tests cover the new strings.
- `internal/cli`: `--on-dirty` maps to the policy; usage error on a bad value.
- `e2e/scenarios`: `worktree-recycle.toml` — dirty target, `--on-dirty=commit`,
  assert the commit and the checked-out branch.

## Docs to update at the end

`CHANGELOG.md`, `README.md` (worktrees section), `internal/agentskill/using-gg.md`
(+ `agentskill.Version` bump, `gg init --update`), `docs/CLAUDE-details.md`
(op semantics + decision id), the four i18n bundles.
