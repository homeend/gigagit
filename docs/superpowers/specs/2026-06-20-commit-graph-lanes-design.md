# Visual commit graph (Phase 2) — design

**Date:** 2026-06-20
**Status:** approved (brainstorm)
**Scope:** new pure pkg `internal/commitgraph` + `internal/tui` integration
(+ docs). No git/domain/CLI change. **Phase 2 of the 4-part GitKraken-style
commit graph** (Phase 1 = multi-branch feed + labels, merged `6c60fda`).

## Goal

Draw a single-line-per-commit **graph column** in the Commits panel —
Unicode rounded box-drawing, monochrome — showing which commits belong to which
line of descent (branches, forks, merges), laid out over the Phase-1
date-ordered all-branches feed.

## Background (what exists)

- `model.Commit{Hash, Parents []string, …, Refs []Ref}` — `Parents` is the graph
  input; merges have ≥2 parents. Already populated by Phase 1's `LogScoped`.
- `domain.CommitFeed` pages commits newest-first in `--date-order`; the TUI holds
  the loaded window in `m.commits`.
- The Commits panel renders via `m.commitRows()` → `commitList{rows}` →
  `panelView` (filters by `strings.Contains(Row(i), q)`, sorts via
  `sortIndices`) → `renderPanel` (prepends a 2-char selection cursor, lays out
  through `renderWindow`).

## Decisions (locked in brainstorm)

1. **Glyphs:** Unicode rounded box-drawing (`● │ ─ ╮ ╭ ╯ ╰ ├ ┤`), one line per
   commit.
2. **Monochrome** first cut — the engine emits plain strings (no lipgloss);
   per-lane color is deferred (the engine still returns lane indices so color
   slots in later).
3. **Pure engine** `internal/commitgraph` (mirrors `textdiff`/`hunkpick`): no
   git/tui/lipgloss imports.
4. **Natural-order only:** the graph renders only when the Commits panel is in
   feed order — `filterQuery == "" && sortMode == sortDefault`. A filtered or
   re-sorted list drops/reorders rows, which makes lane topology incoherent
   (edges to hidden rows) and would pollute the filter haystack; fall back to
   plain rows then.
5. **No hard lane cap** in v1 — a very wide graph truncates the subject under the
   panel's cutoff mode; a cap/overflow marker is a noted later refinement.

## A. Engine — `internal/commitgraph`

```go
package commitgraph

// Commit is the engine's minimal input: a node id and its parent ids, in the
// caller's display order (newest-first).
type Commit struct {
	Hash    string
	Parents []string
}

// Row is the laid-out graph for one commit.
type Row struct {
	Cells string // rendered graph cells (Unicode), padded to Width columns
	Lane  int    // the commit node's lane index (for future per-lane color)
	Width int    // display columns of Cells (= 2 * maxLanesThisRow); see Lay
}

// Lay folds the ordered commits into per-row graph cells. Pure; deterministic.
// Width across rows is normalized to the max simultaneous lane count so columns
// align (Lay returns that shared width too).
func Lay(commits []Commit) ([]Row, int)
```

**Algorithm — left-to-right per-commit fold.** Maintain `lanes []string`, where
`lanes[i]` is the commit hash that lane `i` is currently *waiting for* (set when
an earlier/newer commit named it as a parent). For each commit `C` at row `r`,
in input order:

1. **Node lane.** The leftmost lane whose target == `C.Hash` is `C`'s node lane.
   If no lane targets `C` (a branch tip / unreferenced head), allocate a new lane
   (leftmost free slot) for it.
2. **Merging children.** Any *other* lanes also targeting `C.Hash` converge into
   the node lane on this row (draw `╯`/`╰` + `─` connectors) and are freed.
3. **Parents → new targets.** `C`'s **first parent** keeps the node lane (its
   target becomes `parent[0]`). Each **additional parent** (a merge) takes a new
   lane (leftmost free, or just right of the node), targeting `parent[k]` (draw
   `╮`/`╭` + `─` forking off the node). A commit with no parents (root) frees its
   node lane.
4. **Pass-through.** Lanes not involved with `C` draw `│` straight down.
5. **Render the row's cells.** Two display columns per lane: the lane glyph plus
   a horizontal connector (`─`) or space, so fork/merge edges between the node
   column and the fork/merge columns are drawn on the same line. The node lane
   shows `●`.

The exact **glyph table** (each lane's cell as a function of: is-node /
pass-through / forking-right / merging-left / horizontal-crossing) is enumerated
in the implementation plan and pinned by the topology tests below. Lane
allocation prefers the leftmost free slot to keep the graph compact and
deterministic.

## B. TUI integration

- **Cache.** Add `m.commitGraphRows []string` (the rendered cell string per
  commit, parallel to `m.commits`) and `m.commitGraphWidth int`. Recompute via a
  helper `m.rebuildCommitGraph()` whenever `m.commits` changes — in the
  `dataLoadedMsg`, `commitsPagedMsg`, and `commitsReloadedMsg` handlers. Not per
  render (the fold needs rows 0..N). Converts `m.commits` → `[]commitgraph.Commit`
  and stores `Lay`'s output strings + shared width.
- **Render.** `commitRows()` prepends the cached graph cell + a space to each row
  **iff the panel is in natural order** (`m.filterQuery == "" ||
  m.filterPanel != panelCommits`) **and** (`m.sortModes[panelCommits] ==
  sortDefault`). Otherwise it returns today's plain `<hash> <labels><subject>`.
  When shown, the graph is left of the hash:
  `●─╮ a1b2c3d ‹*main› subject`.
- **Haystack/sort safety.** Because the graph is only added in natural order, the
  filter never matches against graph glyphs and sorting never sees them. The
  cached strings are indexed by backing position; `commitRows` reads
  `m.commitGraphRows[i]` aligned to `m.commits[i]` (commitList is built from the
  full `m.commits`, so indices match before `panelView`'s filter/sort — which are
  inactive in natural order).
- **Width.** The graph column is `m.commitGraphWidth` wide (padded by the engine
  for alignment); the subject takes the remainder and truncates under the panel's
  cutoff display mode (`z`). No hard cap v1.
- **Selection.** The selected row's reverse-video (applied by `renderPanel` to
  the whole row) already styles the graph cells; no special handling.

## C. Testing

**Engine (`internal/commitgraph`) — exact `Cells` + `Lane` per row on canonical
topologies:**
- linear chain (one lane, `●` every row, `│`-free single column);
- a branch + a 2-parent merge (fork `╮`, merge `╯`);
- a lane passing straight **through** a merge row (pass-through `│` beside a
  merge — the classic correctness trap);
- octopus (3-parent) merge (two forks on one row);
- criss-cross / interleaved merges (date-order can interleave branches);
- multiple roots (two independent histories);
- a branch tip with no children (new lane allocated at row 0);
- width normalization across rows.

**TUI:** `commitRows` carries the graph prefix in natural order; **absent** when
a Commits filter is active or `sortMode != sortDefault`; `rebuildCommitGraph`
runs on load/page/reload and aligns with `m.commits`; alignment/width at small
panel sizes (every line ≤ panel width).

## Docs

CHANGELOG (always); README (Commits panel now draws a graph in natural order);
CLAUDE.md (package map gains `commitgraph`; note the natural-order rule). No CLI
surface → no agentskill bump.

## Out of scope (later)

- **Per-lane color** (engine returns `Lane`; map to a lipgloss palette).
- **Lane-count cap / overflow marker** for pathological many-branch widths.
- **Phase 3:** branch-aware navigation (jump to a branch's divergence point,
  highlight a branch's commits) + the multi-branch "selected" set.
