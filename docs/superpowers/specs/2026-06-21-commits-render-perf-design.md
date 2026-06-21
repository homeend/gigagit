# Commits panel render performance — design

**Date:** 2026-06-21
**Status:** approved (brainstorm) → ready for plan
**Scope:** `internal/tui` only. No engine/domain/git/CLI/agentskill changes.

## Problem

On a large repository (reproduced against `/home/homeend/others/linux`) the TUI is
unusable: holding arrows / PageUp in the **Commits** panel lags badly, and the lag
grows the **wider** the commit graph is widened. After opening a commit's files
view (`l`), `j`/`k` also lags, even with a narrow graph, as does arrow navigation
in the file tree.

The user's first instinct was a key/read de-duplication gate. Investigation showed
that would not fix the reported case: `pgup` in the Commits panel and `>` (widen
graph) issue **zero reads**, yet both lag and the lag scales with graph width. The
dominant cost is **rendering**, not reads.

### Measured (root-cause confirmation)

A throwaway benchmark over the per-frame Commits render path (`panelView` +
`commitDecorators`), 303-lane graph, AMD Ryzen 8840HS:

| loaded commits | render time **per frame** | allocations |
|---:|---:|---:|
| 1,000  | 0.14 s | 1.0 M |
| 5,000  | 1.9 s  | 25 M |
| 20,000 | 28 s   | 400 M |

Allocations scale **exactly O(n²)**: (1k)²=1.0M, (5k)²=25M, (20k)²=400M. Because
Bubble Tea calls `View()` once per queued key message, every keystroke on a large
feed pays a multi-second, multi-hundred-MB render — held keys then drain long after
release.

### Root cause

Per-frame work is proportional to the **entire loaded feed**, though only ~panel
height rows are ever visible:

1. **O(commits²):** `commitDecorators` (`view.go:913`) calls `m.commitIdentWidth()`
   **inside its per-row loop over all rows**, and `commitIdentWidth` (`view.go:810`)
   itself scans every commit. Short branch labels never hit the `>= commitIdentW`
   early-out, so it is a full O(n) scan per row → O(n²).
2. **All-n styled-string materialization every frame:** `commitIdentRows`
   (`view.go:822`) builds a styled row string plus a `graphWindow` `[]rune`
   allocation over the full ~303-lane cached cells for **every** commit. Wider graph
   ⇒ larger per-row allocation ⇒ the width-scaling the user observed.
3. **Repeated rebuilds:** `listFor(panelCommits)` (`viewstate.go:345`) rebuilds
   **four** full-feed slices (`commitRows`, `commitFullRows`, `commitTextReveals`,
   `commitHaystacks`) on every call, and `panelView` (which calls `listFor`) runs
   several times per frame.

A separate, smaller cost: in the commit files view, **every** `j`/`k` that changes
the selected commit fires a `CommitFiles` read (`files_view.go:335`,
`moveListUnderFilesView`); held navigation queues a read per commit.

## Goals

- Commits-panel render cost per frame becomes **O(visible rows)** for the expensive
  (styling / graph-window / rune-alloc) work, with at most **O(n) cheap** work
  (filter haystack + sort key) — independent of graph width for the visible region.
- Navigation in the Commits panel on a 20k+ feed feels instant (sub-frame).
- Files-view / file-tree navigation does not issue a read per intermediate keypress
  while a key is held; the list loads for where navigation **settles**.
- No change to *what* is displayed: rendered output, filtering, sorting, selection,
  tooltips, mouse hit-testing, lane coloring, and the `⋯` window markers are all
  byte-for-byte unchanged for any given visible state.

## Non-goals

- Reworking the commit-feed paging/`domain` layer, the graph lane engine
  (`commitgraph`), or the cache. Untouched.
- The general read-dedup/in-flight gate as a cross-cutting framework. Part B is
  scoped narrowly to the files-view/file-tree read; broader gating is out of scope.
- Reducing the size of the loaded feed (windowed loading of the feed itself).

## Approach

Two parts, **A first** (the dominant win), then **B**.

### Part A — window-then-render + width memoization

The single source of truth contract stays: `panelView(p)` still returns
`(rows, idx)` and remains what selection, paging, clamping, tooltips, mouse
hit-testing, and action keys consume. The change is **where the expensive styling
happens**, not the contract.

1. **Memoize the identity-column width.** Compute `commitIdentWidth()` **once** per
   render pass and thread it into `commitDecorators` (and `commitIdentRows`) instead
   of calling it per row. This alone removes the O(n²) term. (`commitIdentWidth`
   already early-outs at `commitIdentW`; the defect is only that it is re-invoked
   per row.)

2. **Materialize expensive row strings for the visible window only.** The styled
   row string + per-commit `graphWindow` allocation must be produced only for the
   rows actually drawn (the `windowStart(…, cap, sel)` slice), not for the whole
   feed. The cheap parts that filtering and sorting genuinely need — the filter
   **haystack** and the **sort key/identity** — may remain O(n), since they do no
   lipgloss styling or wide-cell rune allocation.

   Mechanism (exact form finalized in the plan): make the commit row list produce
   styled rows lazily/by-index so that only the visible indices are styled, and/or
   have the renderer window the `idx` list first and style just that slice. The
   guiding invariant: **no lipgloss styling and no `graphWindow` allocation runs for
   an off-screen commit.**

3. **Decorators for visible rows only.** `commitDecorators` must compute decorators
   only for the windowed rows (it currently loops all rows). It already maps display
   row → backing index via `idx`; it just needs to run over the visible slice.

4. **Avoid redundant per-frame rebuilds.** Eliminate the repeated four-slice rebuild
   in `listFor`/`panelView` within a single frame — either by building only what a
   given call needs, or by memoizing the per-feed-generation list behind a pointer
   field (invalidated when the feed/selection inputs change). Decided empirically:
   if window-then-render already lands the frame budget, this is optional polish.

**Acceptance for A:** a benchmark over the per-frame render path shows
near-flat time vs. feed size for fixed panel height (e.g. n=1k vs n=20k within a
small constant factor, not 200×), and graph width no longer multiplies per-frame
cost beyond the visible window. The benchmark is committed as a regression guard.

### Part B — files-view / file-tree read settle

While navigating commits in the files view (and rows in the file tree), do not
issue the expensive per-row read for every intermediate keypress. Two acceptable
mechanisms (chosen in the plan; pure-drop preferred for its simplicity and its
direct match to "stop pressing = stop reacting"):

- **In-flight drop (pure-drop):** while a `CommitFiles` read is in flight, a further
  `j`/`k` that would issue another such read is dropped (selection move included),
  so navigation is paced by read completion. The selected commit's files load for
  where the user lands when reads catch up.
- **Settle debounce:** move the selection freely but defer the read via a short
  timer (`tea.Tick`), firing only after movement stops.

Scope: `moveListUnderFilesView` (`files_view.go:335`) and the file-tree navigation
path. The completion message (`commitFilesMsg`) clears the in-flight marker.

This is where the user's original read-dedup idea correctly applies. With Part A
done, this removes the residual files-view lag.

## Testing

- **Regression benchmark (Part A):** `BenchmarkCommitsRender` (kept, cleaned up)
  over `panelView` + the windowed render at several feed sizes and graph widths;
  asserts the visible-only invariant via near-flat scaling.
- **Render-equivalence tests (Part A):** golden assertions that the rendered
  Commits panel (rows, `⋯` markers, lane-colored `●`, dimmed lineage rows, identity
  column width) is identical before/after for representative states — selected row
  mid-feed, scrolled graph window, filter active, list mode. Drives out any
  off-by-one in the window-then-style change.
- **Unit tests (Part B):** pure-predicate tests for the in-flight marker — a held
  key issues one read then drops further reads until `commitFilesMsg` clears it;
  navigation still lands on the final commit; a non-read key is unaffected.

## Risks / edge cases

- **panelView is the single source of truth** for tooltips (`tooltip.go`), mouse
  hit-testing (`mouse.go`, `panelRowAt`), selection clamping (which uses
  `len(idx)`), and action keys. The refactor must keep `idx` semantics identical;
  only the cost of producing styled strings moves behind the window.
- **Filter/sort still need all-n cheap data.** Haystack-based filtering and sort
  ordering must continue to see the full feed; only styling is windowed.
- **Lazy/by-index styling must be deterministic** — the decorator's `identStart`,
  `dotCol`, and `⋯` math depend on `graphCols`/`scroll`; keep that math single-
  sourced exactly as today so windowed styling matches the previous full-feed
  output.
