# Rename / delete branch from a Commits-panel tip — Design

**Date:** 2026-06-21
**Status:** Approved, ready for planning
**Scope:** TUI-only (Commits panel `.` menu)

## Goal

When the selected Commits-panel commit is the tip of one or more **local**
branches, the `.` action menu offers **Rename branch ‹name›** and **Delete
branch ‹name›** for those branches — reusing the existing rename popup and
`engine.DeleteBranch` op. This brings branch rename/delete (today only on the
Branches panel) to the place you see a branch's tip in the commit graph.

## Why pure wiring

The backends already exist and are unchanged:

- `renameBranchPopup{old, name}` — a self-contained layer; on submit it runs
  `engine.RenameBranch{Old, New}`.
- `engine.DeleteBranch{Name}` — refuses the checked-out branch and any
  worktree-checked-out branch with clear errors, confirms via a Decider
  (`delete`/`abort`), and force-deletes an unmerged branch via a second fork.
- CLI `gg branch rename` / `gg branch delete` already cover the scriptable case.

So this feature adds **only** a new Commits-panel accessor + menu wiring. No
engine/git/domain/CLI change, no e2e, no agentskill bump.

## Tip detection

`model.Commit.Refs` carries the commit's ref decorations; a `model.Ref` with
`Kind == model.RefLocal` is a local branch whose tip is this commit, and
`Ref.Head == true` marks the checked-out branch. (This is the same data the
branch-name column's `commitIdentOf` already consumes, populated from the
`--decorate` `%D` feed.)

A commit can be the tip of **multiple** local branches, so the action emits one
pair of rows **per** local tip, disambiguated by the branch name in the label.

## Component

`commitBranchRows() []actionRow` in `internal/tui/commit_scope.go`:

```go
func (m Model) commitBranchRows() []actionRow {
    if m.focus != panelCommits || !m.opsIdle() {
        return nil
    }
    bi, ok := m.backingIndex(panelCommits)
    if !ok {
        return nil
    }
    checkedOut := map[string]bool{}
    for _, w := range m.worktrees {
        if w.Branch != "" {
            checkedOut[w.Branch] = true
        }
    }
    var rows []actionRow
    for _, r := range m.commits[bi].Refs {
        if r.Kind != model.RefLocal {
            continue
        }
        name := r.Name
        rows = append(rows, actionRow{
            id:    "rename-branch",
            label: "Rename branch " + name,
            run: func(m Model) (tea.Model, tea.Cmd) {
                return m.pushLayer(&renameBranchPopup{old: name, name: name}), nil
            },
        })
        if !r.Head && !checkedOut[name] { // a checked-out branch can't be deleted
            rows = append(rows, actionRow{
                id:    "delete-branch",
                label: "Delete branch " + name,
                run: func(m Model) (tea.Model, tea.Cmd) {
                    return m.startOp(engine.DeleteBranch{Name: name})
                },
            })
        }
    }
    return rows
}
```

- **Rename** is offered for every local tip, including the HEAD branch (renaming
  the current branch is valid).
- **Delete** is offered for every local tip **except** one that is checked out —
  either this worktree's HEAD (`r.Head`) or any other worktree
  (`checkedOut[name]`, built from `m.worktrees`). `engine.DeleteBranch` refuses
  both at run time, so suppressing the row avoids a menu entry that can never
  succeed (the same "no dead rows" rule the gitignore actions follow). `r.Head`
  covers the current branch even in unit fixtures with no worktree list; the
  worktree-set check covers branches checked out elsewhere.
- The reused ids (`rename-branch` / `delete-branch`) match the Branches-panel
  actions — same semantics, same config allowlist behavior. Duplicate ids in one
  menu (multi-tip) are harmless: dispatch is by row index, not id.

Wired into `availableActions` (action_menu.go):

```go
out = append(out, m.commitBranchRows()...)
```

placed among the other commit-row appenders (e.g. after `rewordRow`, before
`commitCreateBranchRow`, so branch identity ops group together).

## Confirm-modal option order (kept as-is)

`engine.DeleteBranch` leads its confirm Decider with `["delete", "abort"]` —
the destructive option at the modal's default cursor index. This is *pre-existing
shipped behavior* shared with the Branches-panel `d` path, and reaching delete
here already requires a deliberate menu selection first (not a single keypress),
so this feature keeps it unchanged rather than altering a shipped op. (Flipping
to `["abort", "delete"]` would be a separate, cross-cutting safety tweak.)

## After the op

`startOp` (delete) and the rename popup's submit both trigger the standard
post-op full refresh, so the renamed/deleted branch's ref decoration updates in
the Commits feed and Branches panel.

## Testing

Predicate tests for `commitBranchRows` (hand-built `model.Commit{Refs: …}`):

- A commit with one non-HEAD local tip → a `rename-branch` row **and** a
  `delete-branch` row, labels carry the branch name.
- A commit with the HEAD local tip → a `rename-branch` row but **no**
  `delete-branch` row.
- A non-HEAD local tip whose branch is checked out in another worktree
  (`m.worktrees = [{Branch: name}]`) → rename only, **no** delete row.
- A commit with two local tips (one HEAD, one not) → 2 rename rows + 1 delete
  row.
- A commit with only remote refs / no refs → no rows.
- Non-Commits panel and while `m.running` → no rows.
- `availableActions(m)` on a tip-commit selection contains `rename-branch`.

The rename popup (`renameBranchPopup` → `engine.RenameBranch`) and
`engine.DeleteBranch` (guards, confirm, force fork) are already covered by their
own tests; this feature does not re-test them.

## Files

- **Modify:** `internal/tui/commit_scope.go` (+ `commit_scope_test.go` or a new
  `commit_branch_ops_test.go`)
- **Modify:** `internal/tui/action_menu.go` (wire `commitBranchRows`)
- **Modify:** `internal/tui/help.go`, `CHANGELOG.md`

## Out of scope (v1)

- Flipping the delete confirm to `abort`-first (separate safety change).
- A branch picker UI for multi-tip commits (per-branch rows instead).
- Any new CLI surface (`gg branch rename/delete` already exist).
- Rename/delete from the commit graph lanes or other surfaces.
