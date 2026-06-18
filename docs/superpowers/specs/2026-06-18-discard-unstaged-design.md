# Discard unstaged changes (`d` / `D`)

**Date:** 2026-06-18
**Status:** Approved (brainstorm)
**Scope:** TUI-only (engine op reusable by a future `gg discard` CLI)

## Problem

The Files panel shows unstaged working-tree changes, but there is no way to
throw them away. Users need a fast, GitKraken-style "discard" that covers both
**edits to tracked files** and **new (untracked) files**, gated by a
confirmation because the operation is destructive and irreversible.

## Behaviour

Two keys, both active **only on the Files panel** (`panelFiles`) and inert on
the Staged panel:

| Key | Target |
|-----|--------|
| `d` | The **marked** files (`m.fileMarks`). If nothing is marked, the **cursor row**. |
| `D` | **All** unstaged changes in the working tree. |

Both open a **confirmation modal** before doing anything. On confirm, the
discard runs as an engine operation; on cancel, nothing happens.

### What "discard" means per file kind

- **Tracked edit / deletion** (`KindTracked`, including a deleted tracked
  file): `git restore --worktree -- <path>` — resets the working tree to the
  **index**, so a partially-staged file keeps its staged hunks and loses only
  the unstaged portion. A deleted tracked file is restored.
- **Untracked / new file** (`KindUntracked`): `git clean -f -d -- <path>` —
  removes the file. `-d` is required so a brand-new untracked **directory** is
  removed too (`clean -f` alone no-ops on directories).

### Conflicted files

Conflicted rows (`KindUnmerged`) are **excluded** from `d`'s target set,
consistent with `canShowFileDiff` and `canStageHunks`, which already skip
unmerged paths. `git restore`/`git clean` misbehave on unmerged paths, and
conflict resolution is the `x` editor's job. If `D` is pressed while any
conflict exists, it **refuses** with a status message rather than touching the
conflicted tree. If `d`'s resolved target set is empty after dropping unmerged
rows (e.g. the cursor rests on a conflicted file with nothing marked), `d`
no-ops with a brief status message and opens no modal.

### Discard-all scale

`D` does not enumerate paths — on a ~100GB monorepo the dirty set can be huge
and blow the argv limit. It uses a whole-tree pathspec instead:
`git restore --worktree -- :/` (repo root regardless of the runner's cwd) plus
`git clean -f -d`.

## Architecture

### Engine operation — `engine.Discard`

```go
// Discard throws away unstaged working-tree changes. Restore holds tracked
// paths to reset from the index; Remove holds untracked paths to delete. When
// All is set, both lists are ignored and the entire working tree is discarded
// (whole-tree pathspec, avoiding argv blowup on large monorepos).
type Discard struct {
    Restore []string
    Remove  []string
    All     bool
}
```

`Run`:
1. Emit `Progress{Step: "discarding", ...}`.
2. If `All`: `RestoreWorktree(ctx, []string{":/"})` then `CleanUntracked(ctx, nil)`.
   Else: `RestoreWorktree(ctx, Restore)` (skip when empty) then
   `CleanUntracked(ctx, Remove)` (skip when empty).
3. **Partial failure:** run both steps even if the first errors; collect and
   return the first error (or a joined error) — never leave a silent
   half-discard unreported.
4. Return `Result{Summary: "...", Changed: true}` so the frontend reloads the
   status panel like every other mutating op. Emit `Done{Result: ...}`.

Default `TreeWrite` reservation (touches the working tree); no `LockMode`
override needed — same as `Stage`.

### Git verbs (one invocation each, on `*git.Repo`)

Both added to the `GitOps` interface (`internal/engine/gitops.go`).

```go
// RestoreWorktree resets the given paths in the working tree to the index
// (git restore --worktree), discarding unstaged changes while keeping staged
// hunks. Pass ":/" to restore the whole tree from the repo root.
func (r *Repo) RestoreWorktree(ctx context.Context, paths []string) error
//   git restore --worktree -- <paths>

// CleanUntracked deletes untracked files and directories (git clean -f -d).
// Empty paths cleans the whole working tree.
func (r *Repo) CleanUntracked(ctx context.Context, paths []string) error
//   git clean -f -d -- <paths>     (paths omitted when empty)
```

Built with `gitcmd`, run via `r.Runner.Run`. `restore` is already a dependency
(`UnstagePaths` uses `restore --staged`).

### TUI wiring (`internal/tui`)

- **Classification** — a helper turns the target file set into
  `engine.Discard{Restore, Remove}` by `model.FileStatus.Kind`
  (`KindUntracked` → `Remove`, other non-unmerged → `Restore`).
- **`d` handler** (in `model.go` key dispatch, `case "d"` under `panelFiles`):
  build the target set (marked files, else cursor row; drop unmerged), open the
  confirmation modal.
- **`D` handler** (new `case "D"`): if conflicts exist, set a refusing
  `statusMsg`; else open the confirmation modal for `engine.Discard{All: true}`.
- **Confirmation modal** — reuse the established pre-op `decisionState` pattern
  (the switch-to-worktree confirm at `model.go:448`), not a new surface:
  - `d`, single file: `Prompt: "Discard changes to <name>? This cannot be undone."`
  - `d`, N files: `Prompt: "Discard changes to N files? This cannot be undone."`
  - `D`: `Prompt: "Discard ALL unstaged changes? This cannot be undone."`
  - `Options: ["Discard", "Cancel"]`; `onResolve` → `startOp(engine.Discard{...})`
    on "Discard", no-op on "Cancel".
- **Availability** — `canDiscard()` predicate in `avail.go`: focus is
  `panelFiles`, ops idle, and at least one discardable (non-unmerged) row
  exists. Shared by the key dispatch and the footer binding. `D` reuses the
  same predicate.
- **Footer** — bindings in `footer.go`: `[d] discard` (scopeRow) and
  `[D] discard all`, gated by `canDiscard`.
- **Help** — entries in `help.go` for `d` and `D`.

## Testing

- **engine** (`discard_test.go`, `FakeRunner` argv assertions): targeted
  restore-only, clean-only, mixed; `All` whole-tree (`:/` + `clean -f -d`);
  partial-failure propagates the error; empty lists skip the invocation.
- **git** (`mutate`/`stage`-style real-`git` tests): `RestoreWorktree` reverts
  a tracked edit and keeps a staged hunk; restores a deleted tracked file;
  `CleanUntracked` removes a new file and a new directory.
- **tui**: `canDiscard` gating (panel/idle/empty/unmerged-only); `d` targets
  marked-vs-cursor and drops unmerged; `D` refuses on conflicts; the modal
  Prompt text and `onResolve` dispatch the expected `engine.Discard`.

## Out of scope

- `gg discard` CLI verb (engine op is reusable; natural follow-up).
- Discarding staged changes / `d` on the Staged panel.
- Hunk/line-level discard (the hunk picker covers staging; discard is
  whole-file here).
- Any undo/trash safety net beyond the confirmation dialog.
