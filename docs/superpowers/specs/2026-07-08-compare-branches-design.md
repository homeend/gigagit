# Compare Branches in the Branches Panel — Design

**Date:** 2026-07-08
**Status:** Approved

## Problem

The Branches panel's `m`-mark pair machinery offers Merge / Rebase /
Interactive rebase on (marked, selected), but no way to *see* how two
branches differ. Users must drop to `gg compare A B` or mark commits in the
Commits panel. Add a compare action to the same pair-op popup.

## What it does (UX)

- Mark branch A with `m`, press `m` on branch B: the pair-op popup gains a
  4th row — **"Compare A ↔ B"** — below "Interactive rebase". Enter opens
  the existing compare files view (the tree-of-changed-files + per-file
  diff drill-in used by the Commits ◉ compare) showing the **full
  tip-to-tip diff** (`git diff A B`, exactly what `gg compare A B` prints).
  Left endpoint = marked, right endpoint = selected.
- The view title shows the **full branch names** (`feature/foo ↔ main`).
  This deliberately bypasses `Endpoint.Display()`, which truncates any
  hash-ish string to 7 chars and would render `feature/foo` as `feature`.
- New **`f` key** in the compare view cycles a scope filter:
  **all differences → only files A changed → only files B changed → all**.
  The title reflects the scope (e.g.
  `feature/foo ↔ main — only files main changed`), and the compare view's
  footer hint plus the `?` help advertise `[f]`.
  The diff content of each row is always the tip-to-tip difference; the
  filter only limits *which files* are listed.
- The filter exists **only for branch-pair compares** opened from this new
  row. Every other compare (commits ◉, bookmarks, shelf, WIP rows) is
  untouched and `f` stays inert there.
- If the branches share no common ancestor, pressing `f` reports
  `no common ancestor — filter unavailable` in the status line and the view
  stays on "all".
- Pressing `f` before the origin sets finish loading reports
  `origin filter loading…` and stays on "all".

## Origin sets (what "files A changed" means)

Let `M = git merge-base A B`. Then:

- files A changed = paths touched by `diff M..A`
- files B changed = paths touched by `diff M..B`

Loaded in the background immediately after the compare view opens — one new
domain query, 3 git invocations under one Read reservation. Properties:

- A file changed by **both** branches appears under both filters.
- A file a branch touched that ends up **identical at both tips** has no
  row under any scope — correct, since the view is the tip-to-tip diff,
  filtered.
- **Renames** match on either side: an origin set includes both the old and
  the new path of a rename, and a tip-to-tip rename row matches if either
  of its paths is in the active origin set.

## Implementation shape

### TUI (`internal/tui`)

- New `pairOp` row in `pairOpsFor(panelBranches)` (`mark.go`) using the
  existing `open` seam (the interactive-rebase pattern). It calls a new
  `Model.openBranchCompare(marked, selected)` which:
  1. calls `openCompareFiles(Endpoint{Commit, marked}, Endpoint{Commit,
     selected})` — branch names are commit-ish and work in every downstream
     path (diff, per-file `ShowFile`, history/blame context) today;
  2. overrides `m.filesTitle` with the full branch names;
  3. stores the branch-pair labels and dispatches the async origins load
     (tag-gated like `compareFilesMsg`).
- New Model state (all cleared in `closeFilesView`, and therefore by
  `reRoot`'s full reset):
  - the raw `[]model.CommitFile` compare list (today discarded after
    `commitFileLines` renders it) so scope changes rebuild rows locally
    with no git round-trip;
  - the two origin path sets + their branch labels + a loaded/error flag;
  - the scope enum (all / left-only / right-only).
- `f` in `updateFilesViewKey` cycles the scope when a branch-pair compare
  is showing and origins are loaded; rebuilds the tree lines from the
  retained list; resets tree selection to 0. (`f` is currently unbound in
  the files view; the global finder is capital `F`.)
- A stale `compareOriginsMsg` (tag mismatch or view closed) is dropped, the
  `compareFilesMsg` convention.

### Domain (`internal/domain`)

- New query `CompareOrigins(ctx, a, b string) (model.CompareOrigins,
  error)` under a Read reservation: `git.MergeBase(a, b)` (existing verb) +
  two path-list diffs (`M..A`, `M..B`) via the existing `git.DiffNumstat`
  verb with a range `DiffSpec` (`git diff --numstat -z` emits rename
  records with both old and new paths — exactly what the origin sets
  need). Returns the two path sets; a missing merge base returns a typed
  sentinel (e.g. `ErrNoMergeBase`) so the TUI can show the
  "filter unavailable" note without string matching.

### Model (`internal/model`)

- `CompareOrigins{APaths, BPaths map[string]bool}` shared between domain
  and TUI (sets, since the TUI's only use is membership tests while
  filtering).

### Not in scope

- **No engine op** — read-only feature.
- **No CLI change** — `gg compare A B` already accepts branch names; a
  `--only <branch>` CLI filter is deferred (YAGNI).
- **No config** — no new settings.
- Filter for non-branch compares (commit ◉ pairs would need endpoint→rev
  reasoning about WIP rows) — deferred.

## Error handling

- Compare load failure: existing `compareFilesMsg` error path (status
  message, retryable tag reset) — unchanged.
- Origins load failure (other than no-merge-base): status note on `f`
  (`origin filter unavailable: <err>`), view stays on "all"; the compare
  itself is unaffected.
- No merge base: the typed sentinel maps to the
  `no common ancestor — filter unavailable` note.

## Testing

- **Domain** (real git in `t.TempDir()`): `CompareOrigins` returns the
  correct sets for diverged branches (incl. a file changed by both, and a
  rename on one side); unrelated histories return `ErrNoMergeBase`.
- **TUI**:
  - `pairOpsFor(panelBranches)` includes the compare row; its label spells
    both names.
  - Enter on the row opens the files view in compare mode with branch-name
    endpoints and the full-name title; popup closed, mark cleared.
  - `f` cycles all → A-only → B-only → all, filtering the rows and
    updating the title.
  - `f` before origins arrive → loading note, scope unchanged.
  - No-merge-base origins result → unavailable note, scope stays "all".
  - `f` inert in a non-branch compare (e.g. commit-pair compare).
  - Render test: the filtered view draws (guards the
    green-unit/broken-render class).
