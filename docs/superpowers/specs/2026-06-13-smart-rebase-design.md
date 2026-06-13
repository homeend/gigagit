# SmartRebase — design

**Date:** 2026-06-13
**Status:** approved (brainstorm)
**Feature branch:** `feat/smart-rebase` off `main`

## Goal

Implement the pair-operation **Rebase** that is currently a disabled
"not implemented yet" stub in the Branches mark-and-pair popup. A new
`engine.SmartRebase` operation replays one branch's commits onto a new base,
choosing the simplest correct worktree path, and modelling the mid-replay
rebase state explicitly. Exposed in all three frontends the same way merge is:
TUI pair-op, CLI `gg rebase`, and (later) MCP via the shared `Operation`
contract.

## Why it is not just "merge with a different verb"

`SmartRebase` mirrors `SmartMerge`'s *structure* (fail-fast guards → a
3-rung worktree-aware ladder → a conflict decision) but differs in two
load-bearing ways:

1. **Direction is inverted.** Merge moves `Source` *into* `Target` and leaves
   both branches' histories intact. Rebase **rewrites** the branch being
   rebased by replaying its commits onto a new base. The branch that *moves*
   is the one being rebased, so the worktree-ladder pivots on **that** branch,
   not on the base.
2. **Conflict is a state, not an event.** A merge conflict is a single
   conflicted tree: keep-or-abort. A rebase replays N commits and can stop on
   **any** one of them, leaving the repo **mid-rebase with a detached HEAD**.
   "Keep" therefore does not hand back a conflicted tree to commit — it hands
   back a *paused replay* the user must `git rebase --continue` themselves.
   This is written out explicitly below; it must not silently inherit merge's
   semantics.

## Direction mapping (settled once, pinned by a test)

```
engine.SmartRebase{ Branch, Onto }
  Branch — the branch whose commits are replayed (gets rewritten)
  Onto   — the new base they are replayed onto
```

From the TUI stub label *"Rebase {selected} onto {marked}"*:

| popup role | engine field |
|------------|--------------|
| `selected` | `Branch`     |
| `marked`   | `Onto`       |

CLI default: `Branch` = the current branch, so the bare form matches
`git rebase <newbase>`.

A single engine test asserts this mapping (mark `main`, select `feature` →
`SmartRebase{Branch: "feature", Onto: "main"}`) so the direction can never
silently invert.

## Fail-fast guards (mirror SmartMerge, order preserved)

`Run` returns an error before touching the tree when:

1. **Branch resolution** (mirrors how `SmartMerge` resolves `Target`): when
   `Branch == ""` the op resolves it to `CurrentBranch()`. If that is also
   empty (detached HEAD), error:
   `smart rebase: detached HEAD — specify a branch to rebase`. So the bare
   CLI form `gg rebase <newbase>` passes `Branch: ""` and the op fills in the
   current branch.
2. `Onto == ""` → `smart rebase: Onto is required`.
3. `Branch == Onto` → `smart rebase: branch and base are both <name>`.
4. Either name is not in `Branches()` → `smart rebase: no such branch: <name>`.

## Worktree-aware ladder (pivots on `Branch`)

Let `cur` = `CurrentBranch()`.

**Rung 1 — `Branch` is checked out here (`Branch == cur`):**
rebase in place: `rebaseAt(ctx, deps, "", Branch, Onto)`.

**Rung 2 — `Branch` lives in another linked worktree:**
`WorktreeForBranch(ctx, Branch)` returns non-nil → rebase there
(`rebaseAt(ctx, deps, wt.Path, Branch, Onto)`); you stay put on `cur`.

**Rung 3 — `Branch` is not checked out anywhere:**
autostash-if-dirty → switch to `Branch` → rebase → end on `Branch`.
This is `SmartMerge`'s rung 3 verbatim, retargeted:

- `IsDirty()` → if dirty, `emit(Progress{Step:"stashing"})` +
  `StashPush(ctx, "gg-autostash:"+Branch)`, remember `stashed`.
- `emit(Progress{Step:"switching", Detail: Branch})` + `Switch(ctx, Branch)`.
  On switch failure, best-effort `StashPop` to restore on the original branch,
  return the error.
- `res, rebaseErr := rebaseAt(ctx, deps, "", Branch, Onto)`.
- If `rebaseErr != nil`: the tree is paused mid-rebase (or rebase refused
  outright). **Do not pop** — popping onto a half-replayed/detached tree
  compounds the mess. If `stashed`, append
  `" (your changes remain stashed)"` to `res.Summary`. Return `res, rebaseErr`.
- Else if `stashed`: `emit(Progress{Step:"restoring changes"})` +
  `StashPop`. On pop conflict, emit a `stash-pop-conflict` decision
  (`["keep","abort"]`, same as merge's tail) and return a wrapped error noting
  the changes are preserved in the stash.

## `rebaseAt` — the conflict / rebase-state core

```
rebaseAt(ctx, deps, dir, branch, onto) (Result, error)
```

- `where` = `""` or `" in worktree "+dir` (for summaries), as in `mergeAt`.
- `emit(Progress{Step:"rebasing", Detail: branch + " onto " + onto + where})`.
- `rebaseErr := deps.Repo.Rebase(ctx, dir, onto)`.
- **Clean replay** (`rebaseErr == nil`): return
  `Result{Summary: "rebased "+branch+" onto "+onto+where, Changed: true}, nil`.
- **Otherwise** probe state: `inRebase, stateErr := deps.Repo.RebaseInProgress(ctx, dir)`.
  - `stateErr != nil` → wrap both errors (mirror `mergeAt`'s state-check wrap).
  - `!inRebase` → rebase **refused outright** (e.g. unrelated histories,
    nothing to replay): nothing to resolve, return the wrapped `rebaseErr`.
  - `inRebase` → a replay stopped on a conflict. Decision:

    ```
    DecisionRequest{
      ID:      "rebase-conflict",
      Prompt:  "Rebasing "+branch+" onto "+onto+" hit conflicts",
      Options: []string{"keep-conflicts", "abort"},
    }
    ```

    - **`keep-conflicts`** → leave the paused rebase in place. Return
      `Result{Summary: "rebase of "+branch+" onto "+onto+where+" paused on a
      conflict (resolve, then `git rebase --continue`)", Changed: true}` and a
      non-nil error `fmt.Errorf("rebase conflict: %s onto %s", branch, onto)`.
      **Does not return cleanly on a branch** — HEAD is detached mid-rebase.
    - **`abort`** → `deps.Repo.RebaseAbort(ctx, dir)`; on its failure wrap
      `smart rebase: abort failed: %w`. On success return
      `Result{Summary: "aborted: rebasing "+branch+" onto "+onto, Changed: false}, nil`.

## New git verbs (`internal/git/rebase.go`, dir-aware)

All take `dir` (`""` = this repo's own worktree) exactly like the merge verbs,
built with `gitcmd` and run via `r.Runner.Run`.

- `Rebase(ctx, dir, onto string) error` — `git [-C dir] rebase <onto>` (plain; `--no-edit` is a merge-only flag).
  Non-interactive replay of the current branch's commits onto `onto`.
- `RebaseAbort(ctx, dir string) error` — `git [-C dir] rebase --abort`.
- `RebaseInProgress(ctx, dir string) (bool, error)` — **exit-code probe**,
  mirroring `MergeInProgress`'s shape. `git [-C dir] rebase
  --show-current-patch` exits **0** when a rebase is paused on a patch
  (i.e. stopped on a conflict → in progress) and **128** ("No rebase in
  progress?") when none. So `err == nil` → `true`; any non-zero exit →
  `false, nil`. Empirically verified in this repo's git, and honors `-C dir`
  for the linked-worktree rung. This is preferred over a `REBASE_HEAD` ref
  probe (unreliable: set only at some stops, version-dependent) and over a
  `--git-path rebase-merge` + `os.Stat` directory probe (the `--git-path`
  output is relative to git's cwd, which would need re-resolving against
  `dir` — the exit-code probe sidesteps that entirely). The op only calls
  this right after `Rebase` returns an error, so "in progress" cleanly means
  "stopped on a conflict" and any non-zero means "refused outright."

These three verbs are added to the `engine.GitOps` interface (next to
`Merge`/`MergeAbort`/`MergeInProgress`); the `var _ GitOps = (*git.Repo)(nil)`
assertion proves `*git.Repo` still satisfies it.

## TUI wiring (`internal/tui/mark.go`)

Enable the existing stub in `pairOpsFor(panelBranches)`:

```go
{
    label: func(marked, selected string) string { return "Rebase " + selected + " onto " + marked },
    build: func(marked, selected string) engine.Operation {
        return engine.SmartRebase{Branch: selected, Onto: marked}
    },
    enabled: true,
},
```

No other TUI change: the pair-popup, modal Decider, and async-op machinery
already drive any `engine.Operation` and render any `DecisionRequest`'s
option list. The `rebase-conflict` and `stash-pop-conflict` decisions surface
through the existing modal unchanged.

## CLI wiring (`internal/cli/rebase.go`, mirror `merge.go`)

```
gg rebase [--branch <b>] [--on-conflict=keep|abort] <newbase>
```

- Flags precede the positional `<newbase>` (the new base, = `Onto`).
- `--branch` (default `""`) selects the branch to rebase; when empty the op
  resolves it to the current branch. (Asymmetry with merge's `--into` is
  intentional: rebase's positional is the base, merge's positional is the
  source.)
- `--on-conflict` pre-answers the `rebase-conflict` fork:
  `keep` → `keep-conflicts`, `abort` → `abort`, invalid → exit 2.
- Build `cliDecider{policy, ...}` and run
  `engine.SmartRebase{Branch: *branch, Onto: fs.Arg(0)}` through
  `runOperation` / `finish`, exactly as `cmdMerge` does.
- Register `cmdRebase` in the CLI dispatch next to `merge`.

Exit codes follow the established contract: success / chosen-abort = 0;
failed op or unanswered decision = 1; usage error = 2.

## e2e scenarios (`e2e/scenarios/`)

Author from the operation contract, never by running and copying. Cover at
least:

1. **Clean rebase, current branch (rung 1).** `feature` ahead of `main` with
   disjoint changes; `gg rebase main` (from `feature`) → exit 0, linear
   history, `feature`'s commit(s) replayed on top of `main`'s tip,
   `in_progress=none`, end on `feature`.
2. **Rebase a non-checked-out branch (rung 3).** On `main`, `gg rebase
   --branch feature main`-equivalent base → ends **on `feature`**, replayed.
   (Pick a base/branch pair that does not conflict.)
3. **Conflict, keep.** Overlapping change → `gg rebase --on-conflict=keep
   <base>` → **exit 1**, `in_progress="rebase"`, **do not assert `branch`**
   (detached HEAD mid-rebase), conflicted file present.
4. **Conflict, abort.** Same setup → `gg rebase --on-conflict=abort <base>`
   → exit 0, `in_progress=none`, history untouched (pre-rebase tip), end on
   the rebased branch.

The conflict contract matches the already-documented pull-rebase-conflict
contract in `writing-e2e-scenarios` ("exit 1, `in_progress="rebase"`, don't
assert `branch`").

## Scope guards (explicit non-goals)

- **No `--skip`** — silently drops a commit; out of scope.
- **No interactive rebase (`-i`)** — that is an M3 heavy-op per the roadmap.
- **No 3-arg `--onto <newbase> <upstream> <branch>` form** — YAGNI.
- **No git native `--autostash`** — manual stash everywhere (rung 3 switches
  branches, where `--autostash` cannot help, and merge's
  stash-survives-on-failure logic transfers directly).

## Docs to update on completion

- `CHANGELOG.md` (always).
- `README.md` if the user-facing surface list changed (new `gg rebase`).
- `internal/agentskill/using-gg.md` — document `gg rebase`; bump
  `agentskill.Version`; then `gg init --update` to refresh installed copies.
- `CLAUDE.md` engine row — add `SmartRebase` to the smart-ops list (the
  package map already lists `SmartMerge`).

## Files touched

| File | Change |
|------|--------|
| `internal/git/rebase.go` | Create: `Rebase`, `RebaseAbort`, `RebaseInProgress` verbs |
| `internal/git/rebase_test.go` | Create: argv assertions + a real-repo conflict/abort/probe test |
| `internal/engine/gitops.go` | Modify: add the 3 verbs to `GitOps` |
| `internal/engine/smart_rebase.go` | Create: `SmartRebase` op + `rebaseAt` |
| `internal/engine/smart_rebase_test.go` | Create: guards, ladder, conflict decision, direction-mapping test |
| `internal/tui/mark.go` | Modify: enable the Rebase pair-op stub |
| `internal/cli/rebase.go` | Create: `cmdRebase` (mirror `merge.go`) |
| `internal/cli/rebase_test.go` | Create: flag parsing, policy mapping, usage errors |
| `internal/cli/*` (dispatch) | Modify: register `rebase` |
| `e2e/scenarios/rebase-*.toml` | Create: the four scenarios above |
| `internal/agentskill/using-gg.md` + `version` | Modify: document `gg rebase`, bump version |
| `CHANGELOG.md`, `README.md`, `CLAUDE.md` | Modify: on completion |
