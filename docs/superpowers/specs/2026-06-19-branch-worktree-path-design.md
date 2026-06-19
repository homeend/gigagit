# Branch row: show the worktree path — design

Date: 2026-06-19
Status: approved (brainstorm)

## Problem

In the TUI **Branches** panel, a branch checked out in a worktree is currently
marked only with a compact `◫` glyph (`branchRows()` in `internal/tui/view.go`).
That tells you the branch lives in *a* worktree but not *where*. Show the
worktree's path instead, so the user can see and locate it.

## Design

In `branchRows()`, for every branch that is the HEAD of a worktree, append
` (` + the worktree's path + `)` and **drop the `◫` glyph** (the path supersedes
it). This applies to **every** worktree-backed branch, including the current
one (which shows the current worktree's path) — uniform and literal.

Row format (in order): `<head-marker><name>[ (↓N)][ (<worktree-path>)]`

- `<head-marker>` — `* ` for the checked-out branch, `  ` otherwise (unchanged).
- `(↓N)` — existing behind-upstream indicator (unchanged), kept *before* the
  path so the compact count stays near the name; the longer path trails and is
  the first thing the `z` cutoff mode clips.
- `(<worktree-path>)` — the **full** path (matches the Worktrees tab's
  `worktreeRows()` format). The Branches panel already supports `z`
  cutoff/wrap/scroll display modes, so long paths are handled.

Examples:

```
* main       (/mnt/t/others/gigagit)
  feature/x  (/mnt/t/others/gigagit/.claude/worktrees/feature-x)
  behind-br (↓3) (/path/to/wt)
  old-branch
```

### Path lookup

`branchRows` needs branch-name → worktree-path for *any* worktree (the existing
`worktreeForBranch` deliberately excludes the current worktree, so it can't be
reused). Add a small helper:

```go
func (m Model) worktreePathOf(branch string) (string, bool)
```

returning the `Path` of the first worktree whose `Branch == branch` (git allows
a branch in at most one worktree), `ok=false` when none. `branchRows` uses it;
the old `worktreeBranchSet()` call (only used for the `◫` glyph) is removed if it
has no other consumers.

## Scope decisions

- Path shown for **every** worktree-backed branch (incl. current).
- **Full** path (not basename); overflow handled by the panel's `z` modes.
- `◫` glyph **removed** (superseded by the path).
- Branches/Worktrees panels only; no CLI change.

## Testing

`branchRows()` is a pure function of `m.branches` + `m.worktrees`:

- A branch that is a worktree HEAD gets ` (<path>)`; the path matches that
  worktree's `Path`.
- The current branch shows the current worktree's path.
- A branch in no worktree gets no path.
- `* ` head-marker and `(↓N)` behind-indicator are preserved.
- The `◫` glyph no longer appears.

## Out of scope

- No truncation/abbreviation of the path (rely on `z` modes).
- No change to the Worktrees tab or any CLI output.
