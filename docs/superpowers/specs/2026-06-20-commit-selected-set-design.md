# Multi-branch "selected" set — design

**Phase 3 (this round) of the 4-part GitKraken-style commit graph effort.**
Phases 1 (multi-branch feed + branch/HEAD labels + one-tap Solo, merge `6c60fda`)
and 2 (visual single-line lane graph, merge `a31c1f0`) are merged. This round
ships the **multi-branch selected set**: build a custom set of branches and scope
the Commits feed to just those — the additive counterpart to the existing one-tap
Solo. Branch-aware **navigation** (jump-to-divergence, per-branch commit
highlighting) is explicitly deferred to a separate later feature.

## Goal

From the Branches panel, toggle any branch in or out of the Commits-feed scope, so
the feed shows commits from exactly the chosen branches (e.g. `feat-x` + `feat-y` +
`main`). Removing the last branch returns to "all". This completes the
solo/scope story: Solo replaces the set with one branch, the new toggle builds a
multi-branch set, Show all clears it.

## Why there is almost no new plumbing

The scope was deliberately built list-shaped in Phase 1. The data path already is:

```
m.commitScopeBranches []string   (empty = all local branches)
        │  reloadFeedCmd()
        ▼
domain.LogScope{Branches: …}
        ▼
git log <names|--branches HEAD> --date-order --decorate
        ▼
feed.SetScope + LoadInitial  (gen bump → stale-page drop + supersede-cancel)
        ▼
commitsReloadedMsg → painted Commits panel
```

`commitScopeLabel()` already renders the three cases: `all` (len 0), `solo: X`
(len 1), `N branches` (len ≥ 2). So this feature is a new menu action that mutates
the slice and one render extension to the Branches-panel marker — no engine,
domain, or git changes.

## Components

### 1. The toggle menu action — `commit_scope.go`

A new `actionRow` helper `commitToggleRow()` alongside the existing
`commitSoloRow` / `commitShowAllRow`, with the same guards (`focus ==
panelBranches && m.opsIdle()`, a selectable branch):

- **Dynamic label.** `Remove from commit view` when the selected branch is already
  in `commitScopeBranches`; `Add to commit view` otherwise.
- **`run`.** Append the branch if absent, else remove it (preserving the order of
  the remaining entries); then `m.reloadFeedCmd()`. Removing the last entry leaves
  the slice empty → the feed reverts to all branches.
- **Stable id** `commits-toggle` (for the `.`-menu allowlist / replay machinery).

Membership and removal use a small helper kept local to the file:

```go
func contains(ss []string, s string) bool { … }
func without(ss []string, s string) []string { … } // returns a new slice
```

### 2. Wiring into the action menu — `action_menu.go` / `availableActions`

`commitToggleRow()` is appended right after `commitSoloRow()` so the Branches-row
`.` menu reads, in order:

```
Solo this branch          → set = [this]
Add to commit view        → set += this      (or "Remove from commit view")
Show all branches         → set = []         (only present when scoped)
```

All three already coexist; this adds one conditional row. No new global key —
turning the mode off is itself the menu (Remove the last branch, or Show all).

### 3. Marker extension — the Branches panel

Today `branchRows()` appends `◉` to the row string when the branch is the *sole*
scope entry (`len(commitScopeBranches) == 1 && [0] == b.Name`). Change that test
to "is this branch a member of `commitScopeBranches`" (`contains(...)`), so a
3-branch set shows three `◉` rows.

The marker is appended to the row string, exactly like the existing `(↓N)`
behind-indicator and the worktree-path suffix on the same line. Per the
single-sourced-row convention (a panel row string feeds display + filter haystack
+ tooltip), the `◉` therefore already participates in filtering today when a
branch is soloed; extending it to all set members keeps that same, already-
accepted behavior. No change to where the marker lives — only to which rows get
it.

### 4. Header label — unchanged

`commitScopeLabel()` already returns `N branches` for len ≥ 2 and `solo: X` for
len 1. A set toggled down to a single branch therefore reads `solo: X` — accepted
as consistent (size-1 set == solo). No label change in this round.

## Data flow (toggle add)

1. User opens the `.` menu on a Branches row, runs **Add to commit view**.
2. `run` appends the branch name to `m.commitScopeBranches`, returns
   `m.reloadFeedCmd()`.
3. `reloadFeedCmd` copies the scope into `domain.LogScope`, calls
   `feed.SetScope` + `feed.LoadInitial` off the UI thread (gen bump drops stale
   pages, cancels any superseded in-flight walk), returns `commitsReloadedMsg`.
4. `Update` paints page 0; header shows `Commits (N branches)`; the toggled
   branch row now carries `◉`.

## Error handling / edge cases

- **Toggle while an op is running** — guarded out (`m.opsIdle()`), same as Solo.
- **Toggle a branch that was deleted between render and run** — the name is simply
  added/removed as a string; a stale name in the scope makes `git log` ignore an
  unknown ref for that walk (already the feed's behavior). No crash; next reload
  with a corrected set fixes it. Not specially handled (YAGNI).
- **Remove last branch** — empty slice → `commitScopeLabel()` → `all`; feed walks
  `--branches HEAD` again.
- **Solo then toggle** — Solo sets `[X]`; a subsequent Add makes `[X, Y]`; the
  label flips `solo: X` → `2 branches`. Consistent with the set model.

## Testing (TDD)

Unit tests mirroring `commit_scope_test.go`, against the existing FakeRunner feed
harness:

1. `commitToggleRow` on a branch not in scope → label `Add to commit view`; run
   grows the set by that branch.
2. `commitToggleRow` on a branch already in scope → label `Remove from commit
   view`; run shrinks the set, preserving the order of the rest.
3. Toggling the only scoped branch off → empty scope (`commitScopeLabel()` ==
   `all`).
4. Marker: with `commitScopeBranches = [a, c]`, `branchRows()` marks rows `a` and
   `c` with `◉` and leaves `b` unmarked.
5. End-to-end: menu row → `reloadFeedCmd` → `commitsReloadedMsg` → `Update` →
   header reads `2 branches` and the feed is painted (reuses the Phase-1 e2e TUI
   test shape).

No CLI surface and no git/argv change → no e2e scenario or real-git test needed
this round (the scope already had its real-git coverage in Phase 1).

## Out of scope (deferred)

- **Branch-aware navigation** — jump to a branch's divergence/merge-base point and
  highlight which commits belong to a branch. Needs new git verbs (merge-base
  returning a hash; per-commit branch membership) + render changes; its own
  spec/plan/branch.
- **Per-lane graph color** — separate later enhancement (the `commitgraph` engine
  already returns `Row.Lane`).
- **Listing the set in the header** beyond `N branches` — kept opaque for now to
  avoid overflow; revisit if users ask.
