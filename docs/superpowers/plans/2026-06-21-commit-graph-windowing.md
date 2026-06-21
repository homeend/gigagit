# Commit-Graph Windowing Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Render only a horizontal *window* of the commit graph (with widen/narrow, pan, and snap-to-node controls) so a deep merge history no longer pushes the commit text off-screen.

**Architecture:** The pure `commitgraph` engine stays unchanged except for a width-only safety ceiling. All windowing is a view concern in `internal/tui`: the full graph cells are still cached, and a new `graphWindow` helper slices a horizontal lane window at render time. Every tunable lives in `[ui]` config; the plane cap is clamped to the code ceiling.

**Tech Stack:** Go 1.26, Bubble Tea TUI (value-receiver `Model`), lipgloss, TOML config. Tests use Go's `testing` + the repo's existing TUI render-test helpers.

## Global Constraints

- Spec: `docs/superpowers/specs/2026-06-21-commit-graph-windowing-design.md`.
- `internal/tui` and `internal/cli` MUST NOT import `internal/git` (archtest-guarded). The graph window is TUI-only; it reads cached `m.commitGraphRows`/`m.commitGraphLanes`.
- Width unit is **lanes**; 1 lane = 2 display columns.
- Hard ceiling: `commitgraph.MaxLanes = 320`. The configurable plane cap can only **lower** the reachable plane: `effMax = min(cfg.CommitGraphMaxLanes>0 ? cfg.CommitGraphMaxLanes : MaxLanes, MaxLanes)`.
- Config field pattern: `int`, `<=0 = unset`, overlay copies only when `> 0` (mirrors `WheelStep`/`HScrollStep`).
- Defaults: lanes 8, min-lanes 2, step 4, pan-step derived (`max(1, cols/2)`), max-lanes = ceiling.
- The graph is only active when `!m.commitListMode && m.commitGraphOn() && len(m.commitGraphRows) == len(m.commits) && len(m.commits) > 0`.
- `commitGraphScroll` resets to 0 on every feed reload (`rebuildCommitGraph`); `commitGraphCols` (a width preference; `0` = use configured default) persists.
- Cells contain 3-byte glyphs — always rune-slice, never byte-slice.
- TUI `Model` is a value receiver; helpers that mutate return `Model`.
- Per project convention every new keybinding lands in `help.go` (the `?` pane) and the context-help footer.
- Run `./test.sh unit` (vet + gofmt + unit) before declaring the branch done; single-package runs use `go test ./internal/<pkg>/ -run <Name> -v`.

---

### Task 1: Engine width ceiling + config fields

Bounds the cached plane in the pure engine and adds all five config tunables. No behavior change yet — pure groundwork the view tasks consume.

**Files:**
- Modify: `internal/commitgraph/graph.go` (add `MaxLanes` const; clamp width in `Lay`; replace `pad` with a pad-or-truncate `fit`)
- Test: `internal/commitgraph/graph_test.go`
- Modify: `internal/config/config.go` (5 new `UIConfig` fields; `Defaults`; `Load` overlay)
- Test: `internal/config/config_test.go`

**Interfaces:**
- Consumes: existing `commitgraph.Lay([]Commit) ([]Row, int)`, `config.UIConfig`, `config.Defaults()`, `config.Load(...)`.
- Produces: `commitgraph.MaxLanes` (untyped const `320`); `Lay` now returns width ≤ `MaxLanes*2`. `UIConfig` fields `CommitGraphLanes`, `CommitGraphMinLanes`, `CommitGraphStep`, `CommitGraphPanStep`, `CommitGraphMaxLanes` (all `int`).

- [ ] **Step 1: Write the failing engine test**

Add to `internal/commitgraph/graph_test.go`:

```go
func TestLayClampsPlaneToMaxLanes(t *testing.T) {
	// One merge commit with far more parents than the ceiling forces a very
	// wide node row; every parent is a distinct root that frees its lane next.
	const parents = 400
	cs := make([]Commit, 0, parents+1)
	m := Commit{Hash: "M"}
	for i := 0; i < parents; i++ {
		p := "p" + strconv.Itoa(i)
		m.Parents = append(m.Parents, p)
	}
	cs = append(cs, m)
	for i := 0; i < parents; i++ {
		cs = append(cs, Commit{Hash: "p" + strconv.Itoa(i)})
	}

	rows, width := Lay(cs)
	if width != MaxLanes*2 {
		t.Fatalf("width = %d, want clamped to MaxLanes*2 = %d", width, MaxLanes*2)
	}
	for i, r := range rows {
		if r.Width != width {
			t.Fatalf("row %d Width = %d, want %d", i, r.Width, width)
		}
		if got := len([]rune(r.Cells)); got != width {
			t.Fatalf("row %d cell runes = %d, want %d", i, got, width)
		}
	}
}
```

Add `"strconv"` to that file's imports if not present.

- [ ] **Step 2: Run it to confirm it fails**

Run: `go test ./internal/commitgraph/ -run TestLayClampsPlaneToMaxLanes -v`
Expected: FAIL — `MaxLanes` undefined (build error), or width far exceeds `MaxLanes*2` once the const is added but the clamp is not.

- [ ] **Step 3: Add the ceiling and pad-or-truncate to the engine**

In `internal/commitgraph/graph.go`, add the const near the top (after the `Row` type):

```go
// MaxLanes is the absolute ceiling on rendered/cached plane width, in lanes.
// Lane *assignment* is never capped (it is bounded by the data); only the
// emitted cell string is clamped, to bound memory on pathological histories.
// A higher value is never reachable — config can only lower the cap.
const MaxLanes = 320
```

In `Lay`, replace the final width/pad block:

```go
	width := maxLanes * 2
	for i := range rows {
		rows[i].Width = width
		rows[i].Cells = pad(rows[i].Cells, width)
	}
	return rows, width
```

with:

```go
	if maxLanes > MaxLanes {
		maxLanes = MaxLanes
	}
	width := maxLanes * 2
	for i := range rows {
		rows[i].Width = width
		rows[i].Cells = fit(rows[i].Cells, width)
	}
	return rows, width
```

Replace the `pad` helper with `fit` (pads short strings, truncates long ones — both rune-aware):

```go
// fit pads s with spaces up to w runes, or truncates it to w runes.
func fit(s string, w int) string {
	r := []rune(s)
	if len(r) > w {
		return string(r[:w])
	}
	for len(r) < w {
		r = append(r, ' ')
	}
	return string(r)
}
```

(Delete the old `pad` function; `fit` replaces its single call site.)

- [ ] **Step 4: Run the engine test — expect PASS**

Run: `go test ./internal/commitgraph/ -run TestLayClampsPlaneToMaxLanes -v`
Expected: PASS. Also run the whole package to confirm no regression: `go test ./internal/commitgraph/` → `ok`.

- [ ] **Step 5: Write the failing config test**

Add to `internal/config/config_test.go`:

```go
func TestUIDefaultsCommitGraph(t *testing.T) {
	d := Defaults().UI
	if d.CommitGraphLanes != 8 || d.CommitGraphMinLanes != 2 || d.CommitGraphStep != 4 {
		t.Fatalf("defaults = %+v, want lanes 8 / min 2 / step 4", d)
	}
}

func TestLoadOverlaysCommitGraphFields(t *testing.T) {
	dir := t.TempDir()
	repo := filepath.Join(dir, "repo.toml")
	if err := os.WriteFile(repo, []byte("[ui]\ncommit_graph_lanes = 20\ncommit_graph_step = 6\ncommit_graph_max_lanes = 100\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load("", repo)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.UI.CommitGraphLanes != 20 {
		t.Errorf("lanes = %d, want 20 (repo overrides default)", cfg.UI.CommitGraphLanes)
	}
	if cfg.UI.CommitGraphStep != 6 {
		t.Errorf("step = %d, want 6", cfg.UI.CommitGraphStep)
	}
	if cfg.UI.CommitGraphMaxLanes != 100 {
		t.Errorf("max = %d, want 100", cfg.UI.CommitGraphMaxLanes)
	}
	if cfg.UI.CommitGraphMinLanes != 2 {
		t.Errorf("min = %d, want default 2 (unset field keeps default)", cfg.UI.CommitGraphMinLanes)
	}
}
```

Ensure `"os"`, `"path/filepath"` are imported in that test file (match existing style — other tests there likely already use them).

- [ ] **Step 6: Run it to confirm it fails**

Run: `go test ./internal/config/ -run 'TestUIDefaultsCommitGraph|TestLoadOverlaysCommitGraphFields' -v`
Expected: FAIL — fields undefined (build error).

- [ ] **Step 7: Add the config fields, defaults, and overlay**

In `internal/config/config.go`, add to `UIConfig` (after `MenuActions`):

```go
	CommitGraphLanes    int `toml:"commit_graph_lanes"`     // default graph window width in lanes; <=0 = unset
	CommitGraphMinLanes int `toml:"commit_graph_min_lanes"` // minimum window width (narrow floor); <=0 = unset
	CommitGraphStep     int `toml:"commit_graph_step"`      // widen/narrow increment in lanes; <=0 = unset
	CommitGraphPanStep  int `toml:"commit_graph_pan_step"`  // pan increment in lanes; <=0 = derived max(1, cols/2)
	CommitGraphMaxLanes int `toml:"commit_graph_max_lanes"` // plane cap in lanes; <=0 = unset; clamped to commitgraph.MaxLanes
```

In `Defaults()`, extend the `UI` literal:

```go
		UI: UIConfig{WheelStep: 3, HScrollStep: 8, CommitGraphLanes: 8, CommitGraphMinLanes: 2, CommitGraphStep: 4},
```

In `Load`'s overlay section (where `FooterActions`/`MenuActions` are copied), add:

```go
	if src.CommitGraphLanes > 0 {
		dst.CommitGraphLanes = src.CommitGraphLanes
	}
	if src.CommitGraphMinLanes > 0 {
		dst.CommitGraphMinLanes = src.CommitGraphMinLanes
	}
	if src.CommitGraphStep > 0 {
		dst.CommitGraphStep = src.CommitGraphStep
	}
	if src.CommitGraphPanStep > 0 {
		dst.CommitGraphPanStep = src.CommitGraphPanStep
	}
	if src.CommitGraphMaxLanes > 0 {
		dst.CommitGraphMaxLanes = src.CommitGraphMaxLanes
	}
```

Note: `dst`/`src` are the overlay helper's parameter names — match the names used by the existing `FooterActions`/`MenuActions` block in the same function.

- [ ] **Step 8: Run the config tests — expect PASS**

Run: `go test ./internal/config/ -run 'TestUIDefaultsCommitGraph|TestLoadOverlaysCommitGraphFields' -v`
Expected: PASS. Then `go test ./internal/config/ ./internal/commitgraph/` → `ok`.

- [ ] **Step 9: Commit**

```bash
git add internal/commitgraph/graph.go internal/commitgraph/graph_test.go internal/config/config.go internal/config/config_test.go
git commit -m "feat(commitgraph): width ceiling + commit-graph window config fields

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01TtVXjtxzdrhm5hPczG565F"
```

---

### Task 2: Window slice + decorator single-sourcing (the bug fix)

The independently-shippable fix: render only a lane window of the graph, with `⋯` edge markers, and route both decorator columns through one windowed-prefix width. Default window = configured lanes (8); no pan/widen keys yet (scroll stays 0).

**Files:**
- Create: `internal/tui/commit_graph_window.go` (model fields' helpers: config resolvers, `graphActive`, `graphCols`, `graphPlaneLanes`, `graphWindow`)
- Modify: `internal/tui/model.go` (two new `Model` fields; `commitGraphScroll = 0` reset in `rebuildCommitGraph`)
- Modify: `internal/tui/view.go` (`commitIdentRows` graph branch → `graphWindow`; `commitDecorators` `identStart`/`dotCol` single-sourced + window-gated dot)
- Test: `internal/tui/commit_graph_window_test.go`

**Interfaces:**
- Consumes: `m.commitGraphRows []string`, `m.commitGraphLanes []int`, `m.commitListMode`, `m.commitGraphOn()`, `commitgraph.MaxLanes`, `m.cfg.UI.CommitGraph*` (Task 1).
- Produces (used by Tasks 3-4):
  - `m.commitGraphCols int` (field; `0` = use configured default)
  - `m.commitGraphScroll int` (field)
  - `func (m Model) graphActive() bool`
  - `func (m Model) graphCols() int` (resolved, clamped current window width)
  - `func (m Model) graphMinLanes() int`, `graphStep() int`, `graphPanStep() int`, `graphMaxLanes() int`, `graphDefaultLanes() int`
  - `func (m Model) graphPlaneLanes() int`
  - `func (m Model) graphWindow(cells string) (visible string, leftMore, rightMore bool)`
  - `func (m Model) clampCols(c int) int`, `func (m Model) clampScroll(s int) int`

- [ ] **Step 1: Add the two model fields**

In `internal/tui/model.go`, in the `Model` struct next to `commitGraphRows`/`commitGraphLanes`/`commitListMode` (~line 70-72), add:

```go
	commitGraphCols     int // graph window width in LANES; 0 = use configured default
	commitGraphScroll   int // leftmost visible lane (0-based); resets on feed reload
```

- [ ] **Step 2: Create the window helpers (no test yet — compile target)**

Create `internal/tui/commit_graph_window.go`:

```go
package tui

import "github.com/gigagit/gg/internal/commitgraph"

// graphActive reports whether the windowed lane graph is currently drawn in the
// Commits panel (natural order, not list mode, cells cached and aligned).
func (m Model) graphActive() bool {
	return !m.commitListMode && m.commitGraphOn() &&
		len(m.commitGraphRows) == len(m.commits) && len(m.commits) > 0
}

// graphDefaultLanes is the configured startup window width ([ui]
// commit_graph_lanes), 8 until config loads.
func (m Model) graphDefaultLanes() int {
	if v := m.cfg.UI.CommitGraphLanes; v > 0 {
		return v
	}
	return 8
}

// graphMinLanes is the narrow floor ([ui] commit_graph_min_lanes), 2 by default.
func (m Model) graphMinLanes() int {
	if v := m.cfg.UI.CommitGraphMinLanes; v > 0 {
		return v
	}
	return 2
}

// graphStep is the widen/narrow increment in lanes ([ui] commit_graph_step), 4
// by default.
func (m Model) graphStep() int {
	if v := m.cfg.UI.CommitGraphStep; v > 0 {
		return v
	}
	return 4
}

// graphPanStep is the pan increment in lanes ([ui] commit_graph_pan_step); when
// unset it is derived as half the current window (min 1) for a half-page feel.
func (m Model) graphPanStep() int {
	if v := m.cfg.UI.CommitGraphPanStep; v > 0 {
		return v
	}
	if s := m.graphCols() / 2; s > 0 {
		return s
	}
	return 1
}

// graphMaxLanes is the effective plane cap: the configured value clamped to the
// hard code ceiling commitgraph.MaxLanes (config can only lower it).
func (m Model) graphMaxLanes() int {
	cap := commitgraph.MaxLanes
	if c := m.cfg.UI.CommitGraphMaxLanes; c > 0 && c < cap {
		cap = c
	}
	return cap
}

// graphPlaneLanes is the true lane width of the cached graph (all rows share the
// padded Width), bounded by the effective cap.
func (m Model) graphPlaneLanes() int {
	if len(m.commitGraphRows) == 0 {
		return 0
	}
	w := len([]rune(m.commitGraphRows[0])) / 2
	if cap := m.graphMaxLanes(); w > cap {
		w = cap
	}
	return w
}

// graphCols resolves the current window width in lanes: the stored preference
// (0 = configured default), floored at min and capped at the plane width.
func (m Model) graphCols() int {
	cols := m.commitGraphCols
	if cols <= 0 {
		cols = m.graphDefaultLanes()
	}
	return m.clampCols(cols)
}

// clampCols floors c at the configured min and caps it at the plane width.
func (m Model) clampCols(c int) int {
	if mn := m.graphMinLanes(); c < mn {
		c = mn
	}
	if pl := m.graphPlaneLanes(); pl > 0 && c > pl {
		c = pl
	}
	return c
}

// clampScroll keeps the horizontal offset within [0, planeLanes-cols].
func (m Model) clampScroll(s int) int {
	if s < 0 {
		return 0
	}
	max := m.graphPlaneLanes() - m.graphCols()
	if max < 0 {
		max = 0
	}
	if s > max {
		s = max
	}
	return s
}

// graphWindow slices the cached full graph cells to the current horizontal
// window [scroll, scroll+cols) lanes, pads to cols*2 columns, and reports
// whether content exists beyond each edge. A ⋯ marker replaces the edge column
// when there is more content past it. Rune-aware (cells hold 3-byte glyphs).
func (m Model) graphWindow(cells string) (visible string, leftMore, rightMore bool) {
	cols := m.graphCols()
	scroll := m.commitGraphScroll
	r := []rune(cells)
	start := scroll * 2
	end := start + cols*2

	for i := end; i < len(r); i++ {
		if r[i] != ' ' {
			rightMore = true
			break
		}
	}

	var win []rune
	if start < len(r) {
		e := end
		if e > len(r) {
			e = len(r)
		}
		win = append(win, r[start:e]...)
	}
	for len(win) < cols*2 {
		win = append(win, ' ')
	}

	leftMore = scroll > 0
	if leftMore && len(win) > 0 {
		win[0] = '⋯'
	}
	if rightMore && len(win) > 0 {
		win[len(win)-1] = '⋯'
	}
	return string(win), leftMore, rightMore
}
```

- [ ] **Step 3: Reset scroll on feed reload**

In `internal/tui/model.go`, in `rebuildCommitGraph` (~line 1173), after the rows are rebuilt (after the `for` loop that fills `commitGraphRows`/`commitGraphLanes`), add:

```go
	m.commitGraphScroll = 0
```

- [ ] **Step 4: Window the rendered row**

In `internal/tui/view.go`, `commitIdentRows` (~line 786-791), change the `case graph:` branch from:

```go
		case graph:
			row = m.commitGraphRows[i] + " " + row
```

to:

```go
		case graph:
			win, _, _ := m.graphWindow(m.commitGraphRows[i])
			row = win + " " + row
```

- [ ] **Step 5: Single-source the decorator columns**

In `internal/tui/view.go`, `commitDecorators` (~line 828-848), replace the `identStart` graph branch and the lane-color block.

Change the `identStart` computation:

```go
		} else if graphPrefix {
			identStart += lipgloss.Width(m.commitGraphRows[ci]) + 1
		}
```

to:

```go
		} else if graphPrefix {
			identStart += m.graphCols()*2 + 1 // window cols + the trailing space
		}
```

Change the lane-color block from:

```go
		if laneColorOn {
			lane := m.commitGraphLanes[ci]
			dotColor = laneColor(lane)
			if m.commitListMode {
				dotCol = 2 // ● at content col 0 + 2 prefix
			} else {
				dotCol = 2 + 2*lane
			}
			hasDot = true
		}
```

to:

```go
		if laneColorOn {
			lane := m.commitGraphLanes[ci]
			if m.commitListMode {
				dotCol = 2 // ● at content col 0 + 2 prefix
				dotColor = laneColor(lane)
				hasDot = true
			} else {
				// Graph mode: the node is drawn only when its lane is inside the
				// window. Suppress it when it lands exactly on the left ⋯ marker.
				cols := m.graphCols()
				scroll := m.commitGraphScroll
				if lane >= scroll && lane < scroll+cols && !(scroll > 0 && lane == scroll) {
					dotCol = 2 + (lane-scroll)*2
					dotColor = laneColor(lane)
					hasDot = true
				}
			}
		}
```

- [ ] **Step 6: Write the behavior tests**

Create `internal/tui/commit_graph_window_test.go`. (Use the package's existing model/render test helpers — search the test files for how other tests build a `Model` with commits and call the render path; mirror that. The asserts below are the contract.)

```go
package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/gigagit/gg/internal/model"
)

// graphWinModel builds a Model with a synthetically wide graph: `lanes` lanes,
// node on lane `nodeLane`, one commit selected, focus on the Commits panel. The
// commit subject is distinct so we can assert it survives windowing. Maps are
// initialized as New() does so Update can write to them; zero sortMode is
// sortDefault and no filter is set, so graphActive() is true.
func graphWinModel(lanes, nodeLane, cols, scroll int) Model {
	m := Model{
		sel:       map[panel]int{},
		sortModes: map[panel]sortMode{},
		dispModes: map[panel]dispMode{},
		hscroll:   map[panel]int{},
		focus:     panelCommits,
		width:     120,
		height:    40,
	}
	m.commits = []model.Commit{{Hash: "h0", Subject: "WINDOWED_SUBJECT"}}
	m.commitGraphRows = []string{cellsWithNode(lanes, nodeLane)}
	m.commitGraphLanes = []int{nodeLane}
	m.commitGraphCols = cols
	m.commitGraphScroll = scroll
	return m
}

// feedKey routes one key through Update and returns the resulting Model. If the
// package already has an equivalent helper, use that instead.
func feedKey(m Model, key string) Model {
	var msg tea.KeyMsg
	switch key {
	case "shift+left":
		msg = tea.KeyMsg{Type: tea.KeyShiftLeft}
	case "shift+right":
		msg = tea.KeyMsg{Type: tea.KeyShiftRight}
	default:
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
	}
	mm, _ := m.Update(msg)
	return mm.(Model)
}

func cellsWithNode(lanes, nodeLane int) string {
	r := make([]rune, lanes*2)
	for i := range r {
		r[i] = ' '
	}
	for l := 0; l < lanes; l++ {
		r[l*2] = '│'
	}
	r[nodeLane*2] = '●'
	return string(r)
}

func TestGraphWindowSlicesAndMarks(t *testing.T) {
	m := graphWinModel(50, 40, 8, 0) // window at lane 0, node off-screen right
	win, leftMore, rightMore := m.graphWindow(m.commitGraphRows[0])
	if lipgloss.Width(win) != 8*2 {
		t.Fatalf("window width = %d, want %d", lipgloss.Width(win), 16)
	}
	if leftMore {
		t.Error("leftMore should be false at scroll 0")
	}
	if !rightMore {
		t.Error("rightMore should be true (content beyond the window)")
	}
	if !strings.HasSuffix(win, "⋯") {
		t.Errorf("right edge should be the ⋯ marker, got %q", win)
	}
}

func TestGraphWindowLeftMarkerWhenScrolled(t *testing.T) {
	m := graphWinModel(50, 40, 8, 10) // scrolled right
	win, leftMore, _ := m.graphWindow(m.commitGraphRows[0])
	if !leftMore {
		t.Fatal("leftMore should be true when scrolled")
	}
	if !strings.HasPrefix(win, "⋯") {
		t.Errorf("left edge should be the ⋯ marker, got %q", win)
	}
}

func TestCommitRowWidthBoundedByWindow(t *testing.T) {
	// The core regression: with a 200-lane graph and an 8-lane window, the
	// rendered Commits line must still contain the subject and not be 400+ cols.
	m := graphWinModel(200, 150, 8, 0)
	rows := m.commitIdentRows(false)
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	if !strings.Contains(rows[0], "WINDOWED_SUBJECT") {
		t.Errorf("subject pushed off-screen by the graph: %q", rows[0])
	}
	if w := lipgloss.Width(rows[0]); w > 8*2+1+80 {
		t.Errorf("row width %d not bounded by the window", w)
	}
}
```

- [ ] **Step 7: Run tests to verify they fail, then implement the helper**

Run: `go test ./internal/tui/ -run 'TestGraphWindow|TestCommitRowWidthBounded' -v`
Expected: FAIL — `graphWindow`/`graphCols`/the windowed `commitIdentRows` are not yet wired (or the asserts fail). If the build breaks because a `model.Commit` field name differs, adjust `graphWinModel` to match the real `model.Commit` shape (grep an existing commit test). Once Steps 2-5 are implemented the three asserts pass.

- [ ] **Step 8: Run the tests — expect PASS**

Run: `go test ./internal/tui/ -run 'TestGraphWindow|TestCommitRowWidthBounded' -v`
Expected: PASS.

- [ ] **Step 9: Run the whole TUI package to confirm no decorator regression**

Run: `go test ./internal/tui/`
Expected: `ok`. (Existing graph/lane-color/commit-row tests must still pass; if a lane-color test assumed `dotCol = 2 + 2*lane` with a wide unwindowed prefix, update it to the windowed column — but only if it was asserting the *old* unwindowed math.)

- [ ] **Step 10: Update CHANGELOG and commit**

Add a bullet under the unreleased section of `CHANGELOG.md`:

```
- Commits panel: the commit graph now renders a fixed-width horizontal window
  (default 8 lanes, configurable via `[ui] commit_graph_lanes`) instead of the
  full plane, so a deep merge history no longer pushes the commit text
  off-screen. A `⋯` marker shows lanes beyond each edge.
```

```bash
git add internal/tui/commit_graph_window.go internal/tui/commit_graph_window_test.go internal/tui/model.go internal/tui/view.go CHANGELOG.md
git commit -m "feat(tui): windowed commit graph (default 8 lanes) — fixes off-screen text

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01TtVXjtxzdrhm5hPczG565F"
```

---

### Task 3: Pan + widen/narrow keys + action-menu rows

Adds the runtime controls: `shift+←/→` pan, `>`/`<` widen/narrow, redirected only when the graph is active in the Commits panel; plus discoverable `.`-menu rows.

**Files:**
- Modify: `internal/tui/model.go` (graph redirect in `shift+left`/`shift+right`; new `>`/`<` cases)
- Create/Modify: `internal/tui/commit_scope.go` (action rows) and `internal/tui/action_menu.go` (append them)
- Modify: `internal/tui/help.go` and `internal/tui/footer.go`
- Test: `internal/tui/commit_graph_window_test.go` (append)

**Interfaces:**
- Consumes from Task 2: `graphActive`, `graphCols`, `graphStep`, `graphPanStep`, `clampCols`, `clampScroll`, `m.commitGraphCols`, `m.commitGraphScroll`.
- Produces (used by Task 4): action-row append site in `availableActions`.

- [ ] **Step 1: Write the failing pan/width tests**

Append to `internal/tui/commit_graph_window_test.go`:

```go
func TestPanRightAdvancesScrollClamped(t *testing.T) {
	m := graphWinModel(50, 40, 8, 0)
	m.cfg.UI.CommitGraphPanStep = 5
	m = feedKey(m, "shift+right")
	if m.commitGraphScroll != 5 {
		t.Fatalf("scroll = %d, want 5", m.commitGraphScroll)
	}
	// Pan far right: clamp at planeLanes-cols = 50-8 = 42.
	for i := 0; i < 20; i++ {
		m = feedKey(m, "shift+right")
	}
	if m.commitGraphScroll != 42 {
		t.Fatalf("scroll = %d, want clamped to 42", m.commitGraphScroll)
	}
}

func TestPanLeftClampsAtZero(t *testing.T) {
	m := graphWinModel(50, 40, 8, 3)
	m.cfg.UI.CommitGraphPanStep = 5
	m = feedKey(m, "shift+left")
	if m.commitGraphScroll != 0 {
		t.Fatalf("scroll = %d, want 0", m.commitGraphScroll)
	}
}

func TestWidenNarrowAdjustCols(t *testing.T) {
	m := graphWinModel(50, 40, 8, 0)
	m.cfg.UI.CommitGraphStep = 4
	m = feedKey(m, ">")
	if m.graphCols() != 12 {
		t.Fatalf("cols = %d, want 12 after widen", m.graphCols())
	}
	m = feedKey(m, "<")
	m = feedKey(m, "<")
	if m.graphCols() != 4 {
		t.Fatalf("cols = %d, want 4 after two narrows", m.graphCols())
	}
	// Narrow floor = min (2): another narrow must not go below it.
	m = feedKey(m, "<")
	if m.graphCols() != 2 {
		t.Fatalf("cols = %d, want clamped to min 2", m.graphCols())
	}
}
```

`feedKey` is defined in Task 2 Step 6. If the package already has an equivalent key-feeding helper, prefer it; otherwise `feedKey` is self-contained.

- [ ] **Step 2: Run to confirm failure**

Run: `go test ./internal/tui/ -run 'TestPan|TestWidenNarrow' -v`
Expected: FAIL — keys are not handled (scroll/cols unchanged).

- [ ] **Step 3: Redirect shift+left/right to graph pan when active**

In `internal/tui/model.go`, replace the existing `shift+left` / `shift+right` cases (~line 564-575) with graph-aware versions:

```go
		case "shift+left":
			if m.focus == panelCommits && m.graphActive() {
				m.commitGraphScroll = m.clampScroll(m.commitGraphScroll - m.graphPanStep())
				return m, nil
			}
			if m.dispModes[m.focus] == modeScroll && m.hscroll[m.focus] > 0 {
				if m.hscroll[m.focus] -= m.hscrollStep(); m.hscroll[m.focus] < 0 {
					m.hscroll[m.focus] = 0
				}
			}
			return m, nil
		case "shift+right":
			if m.focus == panelCommits && m.graphActive() {
				m.commitGraphScroll = m.clampScroll(m.commitGraphScroll + m.graphPanStep())
				return m, nil
			}
			if m.dispModes[m.focus] == modeScroll {
				m.hscroll[m.focus] += m.hscrollStep()
			}
			return m, nil
```

- [ ] **Step 4: Add the widen/narrow cases**

In the same `switch` in `internal/tui/model.go`, add new cases (e.g. right after the `case "o":` block):

```go
		case ">":
			if m.focus == panelCommits && m.graphActive() {
				m.commitGraphCols = m.clampCols(m.graphCols() + m.graphStep())
				m.commitGraphScroll = m.clampScroll(m.commitGraphScroll)
				return m, nil
			}
		case "<":
			if m.focus == panelCommits && m.graphActive() {
				m.commitGraphCols = m.clampCols(m.graphCols() - m.graphStep())
				m.commitGraphScroll = m.clampScroll(m.commitGraphScroll)
				return m, nil
			}
```

- [ ] **Step 5: Run the pan/width tests — expect PASS**

Run: `go test ./internal/tui/ -run 'TestPan|TestWidenNarrow' -v`
Expected: PASS.

- [ ] **Step 6: Add the action-menu rows**

In `internal/tui/commit_scope.go`, add a helper returning the graph-window rows (empty unless graph active in the Commits panel):

```go
// graphWindowRows offers the commit-graph window controls in the . menu when the
// windowed lane graph is active in the Commits panel.
func (m Model) graphWindowRows() []actionRow {
	if m.focus != panelCommits || !m.graphActive() {
		return nil
	}
	return []actionRow{
		{id: "graph-widen", label: "Widen graph", run: func(m Model) (tea.Model, tea.Cmd) {
			m.commitGraphCols = m.clampCols(m.graphCols() + m.graphStep())
			m.commitGraphScroll = m.clampScroll(m.commitGraphScroll)
			return m, nil
		}},
		{id: "graph-narrow", label: "Narrow graph", run: func(m Model) (tea.Model, tea.Cmd) {
			m.commitGraphCols = m.clampCols(m.graphCols() - m.graphStep())
			m.commitGraphScroll = m.clampScroll(m.commitGraphScroll)
			return m, nil
		}},
		{id: "graph-pan-left", label: "Pan graph left", run: func(m Model) (tea.Model, tea.Cmd) {
			m.commitGraphScroll = m.clampScroll(m.commitGraphScroll - m.graphPanStep())
			return m, nil
		}},
		{id: "graph-pan-right", label: "Pan graph right", run: func(m Model) (tea.Model, tea.Cmd) {
			m.commitGraphScroll = m.clampScroll(m.commitGraphScroll + m.graphPanStep())
			return m, nil
		}},
	}
}
```

Add `"github.com/charmbracelet/bubbletea"` as `tea` to the imports if not already present in that file (match the existing alias used by `commitViewModeRow`).

In `internal/tui/action_menu.go`, in `availableActions` (near the other commit rows, ~line 123-129), append:

```go
	rows = append(rows, m.graphWindowRows()...)
```

(Use the actual local slice variable name in `availableActions` — read the function to confirm it is `rows`.)

- [ ] **Step 7: Write and run an action-row test**

Append to `internal/tui/commit_graph_window_test.go`:

```go
func TestGraphWindowMenuRowsPresentWhenActive(t *testing.T) {
	m := graphWinModel(50, 40, 8, 0)
	m.focus = panelCommits
	got := map[string]bool{}
	for _, r := range availableActions(m) {
		got[r.id] = true
	}
	for _, id := range []string{"graph-widen", "graph-narrow", "graph-pan-left", "graph-pan-right"} {
		if !got[id] {
			t.Errorf("menu missing %q when graph active", id)
		}
	}
}
```

Run: `go test ./internal/tui/ -run TestGraphWindowMenuRowsPresentWhenActive -v`
Expected: PASS.

- [ ] **Step 8: Help + footer entries**

In `internal/tui/help.go`, in the "Commits panel" section (~line 105-110), add a row after the existing graph description:

```go
		r("< / >", "narrow / widen the commit graph window (in lanes)"),
		r("shift+←/→", "pan the commit graph window left / right"),
```

In `internal/tui/footer.go`, add one context binding so the keys advertise while the graph is active. Read the `footerBinding` struct and the `contextBindings` slice, then append (matching the struct's exact field names — `when`, `label`, `id`):

```go
	{id: "graph-window", when: func(m Model) bool { return m.focus == panelCommits && m.graphActive() }, label: "[<>] graph [⇧←→] pan"},
```

- [ ] **Step 9: Run the full TUI package + footer id-uniqueness test**

Run: `go test ./internal/tui/`
Expected: `ok` (includes `TestFooterBindingIDsUniqueAndPresent` — the new `graph-window` id must be unique).

- [ ] **Step 10: Commit**

```bash
git add internal/tui/model.go internal/tui/commit_scope.go internal/tui/action_menu.go internal/tui/help.go internal/tui/footer.go internal/tui/commit_graph_window_test.go
git commit -m "feat(tui): commit-graph pan + widen/narrow keys and . menu rows

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01TtVXjtxzdrhm5hPczG565F"
```

---

### Task 4: Snap-to-node (`=`) + decorator alignment regression

Adds the `=` key (and a `.`-menu row) that snaps the horizontal scroll so the selected commit's node is in view, and locks the scrolled-row decorator alignment with a regression test.

**Files:**
- Create/Modify: `internal/tui/commit_graph_window.go` (`snapGraphToSelected`)
- Modify: `internal/tui/model.go` (`=` case)
- Modify: `internal/tui/commit_scope.go` (Center row)
- Modify: `internal/tui/help.go`
- Test: `internal/tui/commit_graph_window_test.go` (append)

**Interfaces:**
- Consumes: `m.backingIndex(panelCommits)`, `m.commitGraphLanes`, `graphCols`, `clampScroll`.
- Produces: `func (m Model) snapGraphToSelected() Model`.

- [ ] **Step 1: Write the failing snap + alignment tests**

Append to `internal/tui/commit_graph_window_test.go`:

```go
func TestSnapBringsNodeIntoWindow(t *testing.T) {
	m := graphWinModel(50, 40, 8, 0) // node at lane 40, window at 0
	m = feedKey(m, "=")
	cols := m.graphCols()
	if !(40 >= m.commitGraphScroll && 40 < m.commitGraphScroll+cols) {
		t.Fatalf("node lane 40 not in window [%d,%d)", m.commitGraphScroll, m.commitGraphScroll+cols)
	}
}

func TestDotColumnAlignsOnScrolledRow(t *testing.T) {
	// Scrolled so a left ⋯ marker is present; the lane-color dot column and the
	// windowed prefix width must agree (no ANSI rune-index drift).
	m := graphWinModel(50, 40, 8, 36) // window [36,44): node 40 visible, left marker on
	rows := m.commitIdentRows(false)
	decos := m.commitDecorators(rows, []int{0})
	if len(decos) != 1 || decos[0] == nil {
		t.Fatal("expected one decorator for the single commit row")
	}
	// The node sits at lane 40, window starts at 36 → dotCol = 2 + (40-36)*2 = 10.
	// Assert the decorated row colors exactly that column (the rune there is ●).
	out := decos[0]("●probe", 0, 0) // see note below; replace with the package's
	_ = out                          // decorator-call convention from existing tests.
}
```

Replace the final two lines with the package's actual convention for invoking a `rowDecorator` and asserting a colored column — grep the existing commit-branch-column / lane-color tests (e.g. `TestRenderPanel*` or `commitDecorators` tests) for how they assert a dot color at a column, and mirror it. The assertion to preserve: **on the scrolled row the colored node column equals `2 + (lane-scroll)*2` and the subject still begins at `2 + cols*2 + 1`.**

- [ ] **Step 2: Run to confirm failure**

Run: `go test ./internal/tui/ -run 'TestSnapBringsNodeIntoWindow|TestDotColumnAlignsOnScrolledRow' -v`
Expected: FAIL — `=` not handled (snap is a no-op).

- [ ] **Step 3: Implement snap**

Add to `internal/tui/commit_graph_window.go`:

```go
// snapGraphToSelected scrolls the graph window so the selected commit's node
// lane is centered-ish in view.
func (m Model) snapGraphToSelected() Model {
	bi, ok := m.backingIndex(panelCommits)
	if !ok || bi < 0 || bi >= len(m.commitGraphLanes) {
		return m
	}
	lane := m.commitGraphLanes[bi]
	m.commitGraphScroll = m.clampScroll(lane - m.graphCols()/2)
	return m
}
```

In `internal/tui/model.go`, add the `=` case alongside `>`/`<`:

```go
		case "=":
			if m.focus == panelCommits && m.graphActive() {
				m = m.snapGraphToSelected()
				return m, nil
			}
```

- [ ] **Step 4: Run snap/alignment tests — expect PASS**

Run: `go test ./internal/tui/ -run 'TestSnapBringsNodeIntoWindow|TestDotColumnAlignsOnScrolledRow' -v`
Expected: PASS.

- [ ] **Step 5: Add the Center action-menu row**

In `internal/tui/commit_scope.go`, append a fifth row to `graphWindowRows`'s returned slice:

```go
		{id: "graph-center", label: "Center on selected commit", run: func(m Model) (tea.Model, tea.Cmd) {
			return m.snapGraphToSelected(), nil
		}},
```

- [ ] **Step 6: Help entry for `=`**

In `internal/tui/help.go`, in the Commits panel section, add:

```go
		r("=", "snap the commit graph window to the selected commit's node"),
```

- [ ] **Step 7: Run the full TUI package**

Run: `go test ./internal/tui/`
Expected: `ok`.

- [ ] **Step 8: Update CHANGELOG and commit**

Extend the Task-2 CHANGELOG bullet (or add a follow-on bullet):

```
- Commit graph window controls: `<`/`>` narrow/widen, `shift+←/→` pan, `=` snap
  to the selected commit's node; all also in the `.` menu. Tunables:
  `[ui] commit_graph_min_lanes`, `commit_graph_step`, `commit_graph_pan_step`,
  `commit_graph_max_lanes` (clamped to the 320-lane ceiling).
```

```bash
git add internal/tui/commit_graph_window.go internal/tui/model.go internal/tui/commit_scope.go internal/tui/help.go internal/tui/commit_graph_window_test.go CHANGELOG.md
git commit -m "feat(tui): commit-graph snap-to-node (=) + alignment regression test

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01TtVXjtxzdrhm5hPczG565F"
```

- [ ] **Step 9: Full unit suite before finishing**

Run: `./test.sh unit`
Expected: vet + gofmt clean, all unit tests pass. (Then the finishing-a-development-branch flow: `./test.sh race` is the pre-merge gate for the human.)

---

## Notes for the implementer

- **`graphWinModel`/`feedKey` (Task 2 Step 6) are self-contained**, but verify the `model.Commit` field names and the `Model` map fields (`sel`/`sortModes`/`dispModes`/`hscroll`) against the real struct before relying on them — adjust if a name differs. If an existing test helper builds a Commits `Model` more faithfully, prefer it.
- **`backingIndex`** maps a panel's selection index to the underlying commit index (used in `model.go` ~line 789 for the `l` files view). Use it, not `m.sel[panelCommits]` directly, so snap respects filtering/sorting (though the graph is only active in natural order, this keeps it correct).
- **Do not** add graph keys to a Commits-panel-only sub-handler — there is none; the Commits panel is driven by the global key switch in `model.go`. Gate every graph key on `m.focus == panelCommits && m.graphActive()` and fall through otherwise.
- **`graphActive()` already encodes** the "natural order, not list mode, cells aligned" precondition, matching the existing `commitGraphOn()` gate the renderer uses — so the keys are inert exactly when the graph is not drawn.
