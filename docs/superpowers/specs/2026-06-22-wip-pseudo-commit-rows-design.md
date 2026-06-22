# WIP pseudo-commit rows (compare-trees Stage 3) — Design

**Date:** 2026-06-22
**Status:** Approved (brainstorm)
**Scope:** TUI only (`internal/tui`). No engine / domain / git / CLI / agentskill changes.
**Builds on:** the compare-trees arc (Stages 1/2/4/5 merged) — `model.Endpoint`
(`EndpointWorkTree` / `EndpointIndex` / commit hash), `openCompareFiles`,
the ◉ compare selection, and the single-line commit graph (`internal/commitgraph`).

## Problem

Uncommitted work is invisible in the Commits panel. To diff your working copy or
index against history you use the `.`-menu "Compare against working tree / staged"
actions — discoverable only via the menu, and disconnected from the graph. GitKraken
shows a **WIP node** at the top of the graph, parented to HEAD, that you select like
any commit. We want the same, refined to this project's split of *unstaged* vs
*staged* work.

## Behavior

Two **pseudo-rows** sit at the top of the Commits panel, shown only when the
working tree is dirty (a clean tree shows no pseudo-rows — today's behavior):

- `◇ Working tree (N)` — present when there are **unstaged** changes; N = unstaged
  file count (mirrors the left-column Files panel).
- `◇ Staged (N)` — present when there are **staged** changes; N = staged file count
  (mirrors the left-column Staged panel).

They are chained above HEAD: `Working tree → Staged → HEAD`. When only one side is
dirty, only that row shows, parented directly to HEAD:

| Tree state            | Rows shown (top→down)                 |
|-----------------------|---------------------------------------|
| staged + unstaged     | Working tree → Staged → HEAD          |
| only staged           | Staged → HEAD                         |
| only unstaged         | Working tree → HEAD                   |
| clean                 | (none)                                |

The commit graph draws the connecting `│` lane between the pseudo-rows and HEAD,
using a hollow `◇` node glyph to distinguish them from real `●` commits.

### Single-select diff (node vs its parent)

`enter` or `l` on a pseudo-row opens the files view as a whole-tree diff of that
node against **its parent in the chain**:

- **Working tree** row → working tree **vs index** (the unstaged diff). When there
  is no Staged row (nothing staged), vs HEAD — which equals the same files.
- **Staged** row → index **vs HEAD** (the staged diff).

`enter` on a file in that view diffs that path (reusing the existing compare files
view). This is the same machinery as commit single-select compare, with the
endpoints swapped for `EndpointWorkTree` / `EndpointIndex`.

### ◉ compare selection (Stage 2)

A pseudo-row can be toggled into the ◉ compare set. A 2-member selection diffs the
two endpoints (e.g. Working tree ↔ an old commit = that commit vs your working
copy; Working tree ↔ Staged = the unstaged diff). This **subsumes** the standalone
"Compare against working tree / staged" `.`-menu actions, which are retired (the
selection model replaces them). A pseudo-row is **not** allowed in a 3+ range
squash compare (a range `oldest^..newest` is meaningless for a non-commit); such a
selection refuses with a note.

### Commit-operations are refused on pseudo-rows

A pseudo-row is not a real commit, so every commit-only operation is unavailable on
it: rename/reword, move up/down, drop, cherry-pick, revert, reset, copy commit id,
mark (`m`) / compare-with-marked, create branch/worktree here, go-to-tip. The
`.`-menu simply omits these rows when a pseudo-row is selected; the status bar shows
a plain label (e.g. `working tree · 2 files`) instead of a commit id.

### Search / sort

`@` highlight and `/` filter treat pseudo-rows as ordinary rows whose searchable
text is their label ("Working tree" / "Staged"). Sorting by date/name leaves them
in place at the top is **not** required — when a non-default sort or a filter is
active the graph is already suppressed; pseudo-rows still render (they are part of
the list) but without graph connectors, consistent with how commits render then.

## Architecture (TUI-only)

**Unified Commits list = WIP rows (0–2) prepended to the pure `m.commits` feed.**

- `m.commits` stays the untouched domain feed (the read-model from `CommitFeed`).
  A new `m.wipRows []wipRow` (each `{kind wipKind; count int}`, `kind` ∈
  {`wipWorktree`, `wipStaged`}) is derived from `m.status` whenever status changes
  (cheap: counts already live on `model.WorkingTreeStatus`). `wipCount :=
  len(m.wipRows)` is the single offset seam.
- **Indexing.** The Commits panel's logical length becomes `wipCount +
  len(m.commits)`. Unified index `u`: `u < wipCount` → `m.wipRows[u]`; else the
  commit `m.commits[u-wipCount]`. A small accessor set centralizes this so call
  sites do not open-code the offset:
  - `commitsTotal() int`
  - `isWipRow(u int) bool`
  - `wipRowAt(u int) (wipRow, bool)`
- **Graph.** `rebuildCommitGraph` prepends the WIP nodes to the `commitgraph.Lay`
  input as synthetic `{Hash, Parents}` (Working tree's parent = Staged's synthetic
  hash if present else HEAD; Staged's parent = HEAD = `m.commits[0].Hash`). The
  resulting `commitGraphRows` / `commitGraphLanes` are length `commitsTotal`. The
  four `len(...) == len(m.commits)` invariant checks become `== m.commitsTotal()`.
- **Render.** `commitIdentRowAt` (and the per-index haystack/reveal helpers)
  dispatch on `isWipRow`: a WIP row renders `◇ Working tree (N)` / `◇ Staged (N)`
  with no identity-column/pills; a real commit renders as today. The node glyph in
  the graph cells for a WIP lane is `◇`. This stays O(visible) — one branch per
  visible row.
- **Selection / list.** `commitList` (the `panelList` behind filter/sort) length and
  `Row`/`Name`/`Date`/`Key`/`Haystack` include the WIP rows (Name/Date for a WIP
  row sort stably; Key is a stable sentinel). `m.sel[panelCommits]` and
  `displayIndices` already work in this unified space once the list reports the
  larger length.
- **`backingIndex(panelCommits)`** keeps returning the unified index; commit-op
  sites add an `if m.isWipRow(bi) { return … }` guard (refuse / omit the menu row).
  Endpoint resolution maps a WIP row to `EndpointWorkTree` (working tree) or
  `EndpointIndex` (staged).
- **Re-derivation.** `m.wipRows` is recomputed wherever `m.status` changes (the same
  points that already refresh the panels); `rebuildCommitGraph` is already called on
  every feed/status change, so the graph stays in sync. No new async paths.

### Files

- `internal/tui/model.go` — `wipRows` field; recompute on status change; graph
  invariant updates; commit-op `isWipRow` guards; status-bar label.
- New `internal/tui/wip_rows.go` — `wipRow`/`wipKind` types, `deriveWipRows(status)`,
  the `commitsTotal`/`isWipRow`/`wipRowAt` accessors, and WIP→endpoint mapping.
- `internal/tui/view.go` — `commitIdentRowAt` + haystack/reveal dispatch; graph node
  glyph; the four invariant sites.
- `internal/tui/model.go:rebuildCommitGraph` — prepend WIP nodes to `Lay`.
- `internal/tui/viewstate.go` — `commitList` length + `Row`/`Name`/`Date`/`Key`/
  `Haystack` include WIP rows.
- `internal/tui/files_view.go` / `internal/tui/commit_scope.go` — single-select
  diff for a WIP row (node-vs-parent endpoints); Stage 2 ◉ integration.
- `internal/tui/help.go`, `CHANGELOG.md` — docs.

## Staging

1. **Stage 1 — rows + graph + single-select diff + op-guards.** The unified list,
   the two chained dirty-only rows with graph connectors and the `◇` glyph,
   `l`/`enter` single-select diff (node-vs-parent), commit-ops refused on WIP rows,
   and search/filter treating them as ordinary rows. Ships visible value alone.
2. **Stage 2 — ◉ compare integration.** WIP rows join the 2-way ◉ selection;
   refuse a WIP row inside a 3+ range; retire the standalone "Compare against
   working tree / staged" `.`-menu actions in favor of the selection model.

This spec covers both; the implementation plan that follows targets **Stage 1**.

## Testing

- **`deriveWipRows`** (pure, table-driven): clean → none; only unstaged → one
  Working tree row; only staged → one Staged row; both → Working tree + Staged with
  correct counts.
- **Indexing accessors**: `commitsTotal`/`isWipRow`/`wipRowAt` for each tree state.
- **Graph chain** (real-git): with both dirty, `rebuildCommitGraph` produces
  `commitsTotal` rows and the WIP lanes connect to HEAD (oracle: the WIP node's lane
  equals HEAD's lane in a linear repo; a `│` connector is present).
- **Render** (real-git, `loadedModel`-style on a dirtied repo): the assembled
  `View()` shows `◇ Working tree` / `◇ Staged` at the top with their counts, above
  the real commits, and the graph still draws.
- **Single-select diff**: `l`/`enter` on the Working tree row opens the compare
  files view with `EndpointWorkTree` vs `EndpointIndex` (or HEAD when nothing
  staged); on the Staged row, `EndpointIndex` vs HEAD.
- **Op-guards**: with a WIP row selected, the rebase/cherry-pick/revert/reset/
  rename/copy-id rows are absent from the action menu and the direct handlers
  no-op; the status bar shows the plain label, not a commit id.
- **Clean tree**: no pseudo-rows; behavior identical to today (regression guard).

## Out of scope (v1 / Stage 1)

- ◉ compare integration (Stage 2).
- Staging/unstaging *from* a WIP row (that is the Files/Staged panels' job).
- A CLI surface (interactive navigation aid only).
