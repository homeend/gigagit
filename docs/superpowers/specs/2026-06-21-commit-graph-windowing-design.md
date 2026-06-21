# Commit-Graph Windowing — Design

**Date:** 2026-06-21
**Status:** Approved (brainstorm), pending implementation plan

## Problem

The Commits panel prepends the **entire** commit-graph cell string to every
row (`commitIdentRows`, `view.go:790`: `row = m.commitGraphRows[i] + " " + row`).
The engine pads all rows to `maxLanes*2` columns (`commitgraph.Lay`,
`graph.go:78`).

In a large monorepo with a deep merge history this is catastrophic. Measured
against the real Linux kernel (`git log --branches --date-order`, 2000 commits,
fed through the actual `commitgraph.Lay`):

| metric | value |
|--------|-------|
| global max lanes | **303** → graph string is **606 columns** wide |
| per-row lanes used | p50 = **222**, p90 = 279, p99 = 300, max = 303 |
| rows fitting in ≤ 8 lanes | **0.3 %** |
| rows fitting in ≤ 40 lanes | **2.2 %** |

This is **not** a few merge-heavy rows padding everything wide: in date-order
the kernel genuinely keeps ~222 branches concurrently open at the median row.
The 606-column graph pushes the actual commit text completely off-screen,
making both the graph and the commit list unusable (see
`big-repo-linux-commits.png`).

## Goal

Render only a horizontal **window** of the graph plane so the commit text is
always visible, and give the user runtime controls to widen/narrow that window,
pan it horizontally, and snap it to the selected commit's node.

## User decisions (brainstorm)

1. **Anchor at lane 0**, pan right to explore (not continuous follow-selection).
2. **Incremental** widen/narrow via two keys (`±` a step), not preset cycling.
3. **Generous plane cap (~320 lanes)** with a `⋯` overflow marker, not unbounded.
4. **Snap-to-node** key so the off-screen node is reachable instantly.
5. Width unit is **lanes** (1 lane = 2 display columns); default **8 lanes**.
6. The default lane count is **configurable** (wider monitors want a different
   default); runtime `>`/`<` override it for the session.
7. **No initial snap** — the view always starts at lane 0; `=` snaps on demand.

### Accepted browsing consequence

Because the view anchors at lane 0 and a selected commit's node usually sits at
a high lane in a large repo, **by default the graph shows a wall of
pass-through `│` lanes and the selected commit's `●` is off-screen until the
user presses `=`.** This is the explicitly chosen trade-off (simplicity over
follow-selection). Pressing `=` brings the node into view.

## Architecture

A **frontend-agnostic engine** stays pure; **all windowing is a view concern.**

```
commitgraph.Lay  → full per-row cell strings (lane logic UNCHANGED)
                   + width-only safety cap (MaxLanes) bounds the plane
        │
   internal/tui   → caches full cells (commitGraphRows, as today)
                   → graphWindow() slices a horizontal window at render time
                   → keys adjust window width / scroll / snap
                   → decorators read ONE windowed-prefix width
        │
   internal/config → UIConfig.CommitGraph* tunables (lanes/min/step/pan/max)
                     seed all window behavior; max clamped to the code ceiling
```

### 1. Engine (`internal/commitgraph/graph.go`) — lane logic unchanged

Lane **assignment** stays exactly as-is and remains correct. We do **not** fold
or merge lanes: folding would draw forks that never happened and would break
snap-to-node for any node beyond the fold point. Real histories are bounded by
the data (303 lanes for 2000 commits), so the assignment never needs capping.

The only addition is a **width-only safety cap** to bound memory on
pathological repos: a package const `MaxLanes = 320` that is the **absolute
code ceiling**. When the laid-out plane would exceed it, the rendered/cached
**cell string is truncated** at `MaxLanes*2` columns — the lane *assignments*
are untouched, so every node that fits keeps its true lane. The cap is generous
enough that normal histories (including Linux) never hit it; it is purely a
guard.

`Lay`'s signature stays `Lay([]Commit) ([]Row, int)` (callers unchanged). The
ceiling is applied internally during the final pad/width pass. A test feeds a
synthetic history wide enough to exceed `MaxLanes` and asserts the returned
width is clamped to `MaxLanes*2`.

**Configurable plane cap (clamped to the ceiling).** The user-facing plane cap
(`UIConfig.CommitGraphMaxLanes`, §6) is a *view-side* pan/widen clamp, **not**
the engine truncation. The engine always truncates at the hard `MaxLanes`
ceiling; the config value can only make the *reachable* plane **lower than or
equal to** that ceiling — never higher. The effective cap is
`effMax = min(configMax>0 ? configMax : MaxLanes, MaxLanes)`, and the view uses
`effMax` (not the raw config value) everywhere it clamps. This is the rule
"config max ≤ code max": code defines the absolute ceiling, config may only
reduce it.

### 2. Model state (`internal/tui/model.go`)

Two new fields, parallel to the existing `commitGraphRows []string` /
`commitGraphLanes []int`:

```go
commitGraphCols   int // graph window width in LANES (1 lane = 2 cols)
commitGraphScroll int // leftmost visible lane (0-based)
```

- `commitGraphCols` is seeded from config (`UIConfig.CommitGraphLanes`, default
  8) at model construction. It **persists** across j/k navigation and feed
  reloads (it is a width preference). Clamped to `[minLanes, min(planeLanes,
  effMax)]`.
- `commitGraphScroll` **resets to 0** whenever the feed reloads (the lane
  topology changed; a stale scroll into lane 200 of a different graph is
  meaningless). It defaults to 0.

All graph tunables come from config (resolved once at construction, §6):
`minLanes` (min window width), `step` (widen/narrow increment), `panStep` (pan
increment; `<=0` → derived `max(1, cols/2)`), and `effMax` (the clamped plane
cap). These are stored on the model (or read from the resolved config) so the
key handlers and clamps reference the configured values, not hard-coded
literals.

Both only matter when the graph is active
(`!m.commitListMode && m.commitGraphOn()`).

### 3. Window slice (`internal/tui`, new helper)

```go
// graphWindow slices the full padded graph cells `cells` to the current
// horizontal window and reports whether content exists beyond each edge.
//   visible = cells[scroll*2 : (scroll+cols)*2]  (rune-aware)
// leftMore/rightMore drive a single-column ⋯ overflow marker at that edge.
func (m Model) graphWindow(cells string) (visible string, leftMore, rightMore bool)
```

Behavior:
- Rune-slice (cells contain 3-byte glyphs — never byte-slice).
- Pad the slice to exactly `cols*2` columns so every row's prefix has identical
  width (alignment).
- `leftMore` = `scroll > 0`; when true the leftmost visible column is replaced
  with `⋯`.
- `rightMore` = there is any non-space lane content at lane ≥ `scroll+cols`;
  when true the rightmost visible column is replaced with `⋯`.
- The prefix the row gets is `visible` (with markers), then a single space, then
  the existing `tok + pills + subject`.

`commitIdentRows` changes its `graph` branch from
`row = m.commitGraphRows[i] + " " + row` to use `graphWindow`. **Result: the row
width is bounded by `cols*2 (+markers) + textwidth` regardless of repo size —
this alone fixes the reported bug.**

### 4. Decorator single-sourcing (`internal/tui/view.go`, `commitDecorators`)

Today `identStart` (dim-lineage column, `view.go:833`) computes
`identStart += lipgloss.Width(m.commitGraphRows[ci]) + 1` and `dotCol` (lane
color, `view.go:845`) computes `2 + 2*lane` independently. After windowing both
must derive from **one** value:

```go
prefixW := m.commitGraphCols*2 + 1 // window cols + the trailing space
// (leftMarker/rightMarker occupy columns already inside cols*2; they do not
//  add width — they replace an edge column.)
identStart := 2 + prefixW          // + the 2-col selection prefix
```

The node `●` is colored **only when it is inside the window**:
`scroll ≤ lane < scroll+cols`. Its column is:

```go
dotCol = 2 + (lane - scroll) * 2   // + the 2-col selection prefix
// when leftMore, lane==scroll is the ⋯ marker, not a node — suppress the dot
// for a node exactly at the scrolled-off-left edge.
```

When the node lane is outside the window, no dot is drawn (the row still dims
its lineage identity as before).

This path has known ANSI rune-index drift risk (see the commit-branch-column
work). A regression test renders a **scrolled** row (`scroll > 0`, so a left
`⋯` marker is present) and asserts the lane-color dot column and the subject
start column agree with the windowed prefix width.

### 5. Keys (Commits panel, graph active)

| key | action |
|-----|--------|
| `>` | widen graph: `commitGraphCols += step`, clamp ≤ `planeLanes` |
| `<` | narrow graph: `commitGraphCols -= step`, clamp ≥ `minLanes` |
| `shift+right` | pan graph right: `commitGraphScroll += panStep`, clamp so the window stays within the plane |
| `shift+left` | pan graph left: `commitGraphScroll -= panStep`, clamp ≥ 0 |
| `=` | snap: `commitGraphScroll = clamp(selectedLane - cols/2, 0, planeLanes - cols)` so the selected commit's node is centered-ish in the window |

`step`, `minLanes`, and `panStep` are the configured values (§6); `panStep`
defaults to `max(1, cols/2)` lanes (half-page feel) when its config is unset.

`shift+left`/`shift+right` already exist for the generic text horizontal scroll
(`model.go:564-575`, gated on `dispModes[focus] == modeScroll`). When the graph
is active in the Commits panel, those keys are **redirected to graph pan**;
elsewhere (or graph off) they keep their existing text-hscroll behavior. This
reuses the natural "horizontal scroll" keys rather than burning new ones.

`>` and `<` (shift+`.` / shift+`,`) are distinct from the `,` settings and `.`
actions bindings.

All five actions also appear as discoverable rows in the `.` action menu when
the graph is active: **Widen graph**, **Narrow graph**, **Pan graph left**,
**Pan graph right**, **Center on selected commit**. Per project convention every
new binding is added to `help.go` (the `?` pane) and the context-help footer.

`planeLanes` (the cap for clamping) = `min(lipgloss.Width(cells)/2, effMax)` of
the widest cached row, i.e. the true plane width bounded by the effective
(config-clamped) cap.

### 6. Config (`internal/config`)

**Every** graph tunable is configurable. Add to `UIConfig` (all `int`, all
`<=0 = unset` → fall back to the default, mirroring the existing
`WheelStep`/`HScrollStep` pattern):

```go
CommitGraphLanes    int `toml:"commit_graph_lanes"`     // default window width in lanes; <=0 = unset
CommitGraphMinLanes int `toml:"commit_graph_min_lanes"` // minimum window width (narrow floor); <=0 = unset
CommitGraphStep     int `toml:"commit_graph_step"`      // widen/narrow increment in lanes; <=0 = unset
CommitGraphPanStep  int `toml:"commit_graph_pan_step"`  // pan increment in lanes; <=0 = derived max(1, cols/2)
CommitGraphMaxLanes int `toml:"commit_graph_max_lanes"` // plane cap in lanes; <=0 = unset; clamped to MaxLanes
```

`Defaults()` sets: `CommitGraphLanes: 8`, `CommitGraphMinLanes: 2`,
`CommitGraphStep: 4`, `CommitGraphPanStep: 0` (→ derived), `CommitGraphMaxLanes:
0` (→ the hard `commitgraph.MaxLanes` ceiling, 320).

- The overlay in `Load` copies each field to `dst` when `> 0`.
- **Clamp rule (`config max ≤ code max`):** the engine's
  `commitgraph.MaxLanes = 320` is the absolute ceiling. The effective plane cap
  is `effMax = min(CommitGraphMaxLanes>0 ? CommitGraphMaxLanes : MaxLanes,
  MaxLanes)` — a config value **larger** than the ceiling is silently clamped
  down to it; the config can only lower the reachable plane, never raise it.
- The TUI resolves all five into model state at construction:
  `commitGraphCols = clamp(CommitGraphLanes, minLanes, effMax)`, and stores
  `minLanes`, `step`, `panStep`, `effMax` for the key handlers.
- A config value where `CommitGraphLanes < CommitGraphMinLanes` resolves by
  clamping `cols` up to `minLanes` (min wins).

## Edge cases

- **Graph off** (filter active or non-default sort → `commitGraphOn()==false`,
  or list mode): no windowing; rows render as today. The window keys are no-ops.
- **Plane narrower than the window** (small repo, e.g. 3 lanes, window 8): show
  all lanes, no markers, scroll forced to 0; widen/pan clamp to the plane.
- **Window wider than the panel**: the windowed prefix can still exceed the
  panel if the user widens a lot; that is the user's explicit choice (graph
  squeezes text), and the existing row truncation/`z` modes handle the overflow
  as for any long row.
- **Scroll past the end**: clamp `scroll ≤ max(0, planeLanes - cols)`.
- **Node exactly at the scrolled-off-left `⋯` column**: suppress its dot (the
  column is the marker, not the node).

## Testing

**Engine:**
- `MaxLanes` cap: synthetic history exceeding 320 lanes → returned width clamped
  to `MaxLanes*2`; lane assignments below the cap unchanged.

**View (core bug regression — Task 1):**
- Build a model with a synthetic wide graph (e.g. 200 lanes) and assert the
  rendered Commits line contains the commit subject text and its display width
  ≤ panel inner width. (Without windowing this fails — the subject is pushed
  off-screen.)
- `graphWindow` unit tests: slice bounds, padding to `cols*2`, `leftMore`
  (`scroll>0`) and `rightMore` (content beyond `scroll+cols`) markers, rune
  safety on multi-byte glyphs.

**View (later tasks):**
- Pan: `shift+right`/`shift+left` move the visible lane span; clamps at both
  ends; graph-off → keys retain text-hscroll behavior.
- Widen/narrow: `>`/`<` change `commitGraphCols` by the configured `step`; clamp
  at `minLanes` and plane width; config default seeds the initial value.
- Snap: `=` sets scroll so the selected node is in-window; dot colored at the
  correct windowed column after snap; dot absent when the node is scrolled off.
- Decorator alignment on a scrolled row (`scroll>0`, left `⋯` present): dot
  column and subject column agree with `prefixW`.
- Config: every field overlays (default applied, repo overrides global, `<=0`
  ignored). Specifically: `commit_graph_max_lanes` **above** `MaxLanes` is
  clamped down to `MaxLanes` (config-max-≤-code-max); `commit_graph_pan_step`
  `<=0` derives `max(1, cols/2)`; `commit_graph_lanes < commit_graph_min_lanes`
  resolves to `min_lanes`.

## Task staging (for the plan)

1. **Windowing + engine cap + config** — `graphWindow`, `commitIdentRows`
   slice, decorator single-sourcing, `MaxLanes` ceiling, and **all** config
   fields (lanes/min/step/pan-step/max) + their clamp rules + model seed.
   Independently shippable: fixes the reported bug. Default window = config (8).
2. **Pan** — `shift+left`/`shift+right` redirect + clamps + `.`-menu rows.
3. **Widen / narrow** — `>` / `<` + clamps + `.`-menu rows.
4. **Snap** — `=` + `.`-menu row; help/footer finalized.

## Non-goals

- Continuous follow-selection (explicitly rejected in favor of anchor-0 + snap).
- Changing the feed order (date-order multi-branch feed stays as-is).
- Lane folding / collapsing distant lanes into summaries.
- Per-row variable window width.
