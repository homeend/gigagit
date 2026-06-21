# Move / drop a commit from the Commits panel — Design

**Date:** 2026-06-21
**Status:** Approved, ready for planning
**Scope:** TUI `.` menu, current branch only, one-shot

## Goal

Three `.`-action-menu entries on a Commits-panel commit that is in the
**checked-out branch's** linear history:

- **Move commit up** — make it one step newer (swap with its child).
- **Move commit down** — make it one step older (swap with its parent).
- **Drop commit** — remove it from the branch.

Each performs a single non-interactive rebase immediately (no editor), reusing
`engine.InteractiveRebase` to drive `git rebase -i` through the gg-as-editor
protocol with a pre-built plan.

## Decisions (from brainstorming)

- **One-shot direct actions** (not "open the editor").
- **Current branch (HEAD) only** — the only place reorder/drop is well-defined.

## Why an engine tweak is needed

`engine.InteractiveRebase` today validates that **both** `Branch` and `Onto` are
existing **branch names** (`interactive_rebase.go`). Moving/dropping a single
commit rebases the current branch onto a raw commit (`C^` or a grandparent SHA),
which the op currently rejects. The existing editor only ever launches from
Branches-panel pair-ops (branch→branch), so relaxing `Onto` to any commit-ish is
safe for existing callers.

### Engine change

1. **New git verb** `Repo.CommitExists(ctx, ref string) (bool, error)` —
   `git rev-parse -q --verify <ref>^{commit}`; exit 0 → true, exit 1 → false
   (mirrors the `CherryPickInProgress` exit-code pattern). Added to the `GitOps`
   interface (only `*git.Repo` implements it).
2. **Relax `Onto` validation** in `InteractiveRebase.Run`: `Branch` must still be
   a known branch; `Onto` may be a branch **or** any resolvable commit — if not
   in the branch set, require `CommitExists(Onto)`, else error
   `no such commit: <onto>`. The `Branch == Onto` guard and the `HasMergeCommits`
   guard are unchanged.

No other op behavior changes; the editor path (branch onto branch) still passes
the branch check and never reaches the new commit check.

## Topology, ordering, and the base (`Onto`)

`CommitRange(onto, branch)` runs `git log --reverse` → **oldest-first**:
`commits[0]` is the commit just after `onto`, `commits[len-1]` is the branch
tip. The plan's todo is this same oldest-first order. Earlier in the todo =
older in the resulting history.

For a selected commit **C** (SHA `s`) on the current branch, `Onto` is derived
with a git **revision expression** off C's SHA — `~N` follows first-parent, so it
agrees with `C.Parents[0]` on the non-merge histories we scope to, and
`git rebase -i`, `CommitExists`, and `HasMergeCommits` all accept revspecs. No
feed-walking for parents.

| Action     | `Onto`     | Loaded range (oldest-first) | C index | Transform        |
|------------|------------|------------------------------|---------|------------------|
| Drop       | `s~1` (C^) | `[C, …newer]`                | 0       | entry C → `drop` |
| Move up    | `s~1` (C^) | `[C, child, …]`              | 0       | swap C ↔ C+1     |
| Move down  | `s~2`      | `[P, C, …]`                  | 1       | swap C ↔ C−1     |

`ontoFor(sha string, e commitEdit) string` is a one-line pure helper:
`s~1` for drop/move-up, `s~2` for move-down.

- **Move up** (newer) moves C to a higher todo index → applied later → newer.
  Requires a child in range (refuse if C is the branch tip).
- **Move down** (older) needs C's parent in the range, so `Onto = s~2`. If `s~2`
  doesn't resolve (C's parent is the root commit), `CommitExists` fails and the
  op refuses cleanly (`no such commit: s~2`) — no special-casing.
- **Ancestry is checked implicitly:** with `Onto = s~1`, `C ∈ Onto..HEAD` iff C is
  an ancestor of HEAD. So "target SHA present in the loaded range" both locates C
  and proves it is on the current branch — no separate `IsAncestor` call.

## Gating

**Synchronous** (`.`-menu availability), cheap, feed-only:

- `m.focus == panelCommits && m.opsIdle()`.
- A current branch exists (`m.status.Branch != ""` — not detached HEAD).
- The selected commit is **non-merge and non-root**: `len(C.Parents) == 1`.

All three rows share this gate (the `~1`/`~2` derivation removes any need to
inspect the feed for parents). Move-down on the commit just above the root is
allowed to appear and is refused cleanly at run time when `s~2` can't resolve.

**Asynchronous** (in the load handler / op, clear refusals — never-trap):

- `Onto` (`s~2`) doesn't resolve → `no such commit` (op guard).
- Target SHA not in the loaded range → `commit is not on the current branch`.
- Move up with no newer commit → `already the newest commit on <branch>`.
- The op's own `HasMergeCommits` guard refuses a range that contains a merge.

## Components

### Pure transform — `internal/rebaseplan` or `internal/tui`

A pure function (no git) builds the plan and applies the single-commit edit:

```go
type commitEdit int
const ( editDrop commitEdit = iota; editMoveUp; editMoveDown )

// buildSingleEdit takes the oldest-first range, the target SHA, and the edit,
// and returns the rebase plan or an error describing why it can't apply.
func buildSingleEdit(commits []model.RangeCommit, target string, e commitEdit) (rebaseplan.Plan, error)
```

It seeds every entry `Pick` with `Orig = Message`, finds the target index, then:
`editDrop` → set that entry `Drop`; `editMoveUp` → swap with the next entry (err
if none); `editMoveDown` → swap with the previous entry (err if none). Lives in
`internal/tui` (it consumes `model.RangeCommit` + `rebaseplan`); kept pure and
table-tested.

### TUI accessors + command — `internal/tui/commit_rebase_ops.go`

- `commitMoveUpRow` / `commitMoveDownRow` / `commitDropRow` `(actionRow, bool)`:
  the synchronous gate above; each `run` calls a shared
  `m.startCommitEditCmd(sha, edit)` which computes `onto := ontoFor(sha, edit)`
  and dispatches the load command. `ontoFor` is the only Onto logic, so the
  integration tests exercise the real derivation by calling it (not a hardcoded
  `~1`/`~2`).
- `loadRebaseRangeCmd(branch, onto, target string, e commitEdit) tea.Cmd` —
  reads `CommitRange(onto, branch)` off the UI thread, yields
  `rebaseRangeLoadedMsg{branch, onto, target, edit, commits, err}`.
- Handler for `rebaseRangeLoadedMsg` (model.go): on error/refusal set the status
  message; else `plan, err := buildSingleEdit(...)`; on ok,
  `ggBin, _ := os.Executable()`, then
  `m.startOp(engine.InteractiveRebase{Branch: branch, Onto: onto, Plan: plan, GGBin: ggBin})`.

`engine.InteractiveRebase` already handles the dirty-worktree stash, the
other-worktree / switch rungs, and the `rebase-conflict` (keep/abort) fork, so
conflicts during the rebase surface through the existing modal.

Wired into `availableActions` after the branch rows.

## Testing

### Engine / git

- `CommitExists`: real-git — true for HEAD and a real SHA, false for a bogus ref.
- Relaxed `InteractiveRebase`: a real-git test that `Onto` may be a commit SHA
  (extend or add alongside the existing interactive-rebase tests) — drives a drop
  via a SHA `Onto` and asserts success; and that a bogus `Onto` errors with
  `no such commit`.

### Pure transform

- `buildSingleEdit` table tests over a 4-commit oldest-first range `[a,b,c,d]`:
  - drop `b` → entries keep order `[a,b,c,d]`, `b.Action == Drop`, the rest `Pick`.
  - move up `b` → entry order `[a,c,b,d]`, all `Pick` (swap b,c).
  - move down `c` → entry order `[a,c,b,d]` (swap b,c).
  - target SHA not in the range → error.
  - move up `d` (last/tip) → error (no newer neighbor).
  - move down `a` (first/oldest in range) → error (no older neighbor).
  Every entry carries `Orig` from `RangeCommit.Message`.

### Integration (real git, the discriminating proof)

A 4-commit linear branch `a→b→c→d` (d = HEAD). Each case computes `Onto` via the
real `ontoFor(sha, edit)` (NOT a hardcoded `~1`/`~2`), then
`CommitRange(onto, branch)` → `buildSingleEdit` → `engine.InteractiveRebase`,
and asserts the resulting `git log` **subject order**:

- **Drop c** (`ontoFor(c, editDrop) = c~1`, range `[c,d]`, drop c) → `a, b, d`.
- **Move c down** (`ontoFor(c, editMoveDown) = c~2`, range `[b,c,d]`, swap c,b) →
  `a, c, b, d`. This is the load-bearing case: it proves the `s~2` derivation,
  which the pure transform alone cannot.
- **Move b up** (`ontoFor(b, editMoveUp) = b~1`, range `[b,c,d]`, swap b,c) →
  `a, c, b, d`.
- **Conflict pauses cleanly:** an edit whose reorder/drop conflicts (e.g. two
  commits touching the same line) drives the `rebase-conflict` fork via
  `MapDecider{"rebase-conflict": "keep-conflicts"}` and asserts the op reports
  paused + leaves a rebase in progress (no silent corruption).

### TUI predicates

- The three rows appear on a non-merge commit of the current branch; absent on a
  merge commit (`len(Parents) != 1`), a root commit (`len(Parents) == 0`), a
  detached HEAD (`m.status.Branch == ""`), off the Commits panel, and while
  running. Move-down absent when the grandparent isn't derivable from the feed.

## Files

- **Create:** `internal/git/commit_exists.go` (+test)
- **Modify:** `internal/engine/gitops.go` (interface), `internal/engine/interactive_rebase.go` (relax Onto) (+test)
- **Create:** `internal/tui/commit_rebase_ops.go` (+test)
- **Modify:** `internal/tui/op.go` (msg + load cmd), `internal/tui/model.go` (handler), `internal/tui/action_menu.go` (wire), `internal/tui/help.go`, `CHANGELOG.md`

## Out of scope (v1)

- Any branch other than the checked-out HEAD.
- Merge or root commits.
- Moving a commit more than one step at a time (repeat the action).
- A CLI surface (this is interactive; `gg` has no single-commit rebase command).
- Squash/fixup from the menu (the editor still offers squash).
