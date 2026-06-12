# Truncation Tooltip Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** When the focused panel's selected row is ellipsized, float its full text in a styled strip directly above the row (below when there's no room), in all four list panels, automatically.

**Architecture:** Three units in `internal/tui` per the spec (`docs/superpowers/specs/2026-06-12-truncation-tooltip-design.md`): (1) `layout()` additionally exposes each panel's screen **origin** so render and tooltip share one geometry source; (2) `overlayCenter` is generalized into a position-aware `overlayAt` (pure refactor); (3) a new `tooltip.go` computes the strip's lines+position from the same `panelView`/`windowRows` data the renderer uses, and `render()` composites it only in the plain (no modal/popup) state.

**Tech Stack:** Go 1.26, Bubble Tea/lipgloss/`charmbracelet/x/ansi` (already in use). No new dependencies.

---

## Ground-truth contracts (verified against the code)

- `layout()` (`internal/tui/viewstate.go`) returns `layoutGeom{w,h,bodyH,leftW,rightW,boxH map[panel]int}`. Header is screen row 0; panels start at row 1. Left column x=0; Commits x=`leftW` (narrow mode: Commits x=0, width `rightW=w`). Left stack: Branches at y=1, Worktrees at y=1+boxH[Branches] (when visible), Status below whichever precedes it.
- `renderPanel` truncates each row as `truncate(prefix+row, innerW)` where `innerW = boxW-4` and `prefix` is 2 cols; data rows start 2 lines below the panel's top border (border + label). `windowRows(rows, boxH-3, sel)` returns the window plus `selInWin`.
- `panelView(p)` is the single source of truth for visible rows (sort+filter applied). `m.sel[p]` indexes into it.
- `render()` (`internal/tui/view.go`) returns the modal early; otherwise composites whichever popup pointer is non-nil over `renderInterface()` via `overlayCenter`. **The popup pointer list may have grown on main — read `render()` first and treat "plain state" as: `m.modal == nil` and every popup pointer nil.**
- Tests construct models either via `loadedModel(t)` (real repo, full `Update` cycle — `nav_test.go`) or direct `Model{...}` literals with `sel: map[panel]int{}` (`worktree_view_test.go`). Nil `sortModes`/filter fields are safe zero values.
- `padRight`/`truncate` are display-width-aware; use them, never `len()`.

## File structure

```
internal/tui/viewstate.go    # + point type, layoutGeom.pos, origin computation
internal/tui/view.go         # overlayAt extraction; render() tooltip wiring
internal/tui/tooltip.go      # NEW: tooltip(), wrapWidth(), tooltipY(), style
internal/tui/viewstate_test.go  # + origin tests (or layout_test.go if absent)
internal/tui/view_test.go?      # overlayAt tests — put beside existing overlay tests (grep for overlayCenter in *_test.go and append there)
internal/tui/tooltip_test.go    # NEW: tooltip behavior tests
internal/tui/fit_test.go        # + tooltip-active bounds invariant
```

---

### Task 1: Panel origins in `layout()`

**Files:**
- Modify: `internal/tui/viewstate.go` (layoutGeom + layout())
- Test: append to `internal/tui/viewstate_test.go` (create the test func wherever the existing layout/viewstate tests live — grep `func TestLayout` first; if none, add to `pgnav_test.go` which exercises `layout()` geometry)

- [ ] **Step 1: Write the failing test**

```go
func TestLayoutOrigins(t *testing.T) {
	// Wide terminal: three left panels + commits column.
	m := Model{width: 90, height: 30}
	g := m.layout()
	if got, want := g.pos[panelBranches], (point{0, 1}); got != want {
		t.Errorf("branches origin = %v, want %v", got, want)
	}
	if got, want := g.pos[panelWorktrees], (point{0, 1 + g.boxH[panelBranches]}); got != want {
		t.Errorf("worktrees origin = %v, want %v", got, want)
	}
	if got, want := g.pos[panelStatus], (point{0, 1 + g.boxH[panelBranches] + g.boxH[panelWorktrees]}); got != want {
		t.Errorf("status origin = %v, want %v", got, want)
	}
	if got, want := g.pos[panelCommits], (point{g.leftW, 1}); got != want {
		t.Errorf("commits origin = %v, want %v", got, want)
	}

	// Short terminal: worktrees hidden, status sits right under branches.
	m = Model{width: 90, height: 10}
	g = m.layout()
	if _, visible := g.pos[panelWorktrees]; visible {
		t.Error("worktrees should have no origin when hidden")
	}
	if got, want := g.pos[panelStatus], (point{0, 1 + g.boxH[panelBranches]}); got != want {
		t.Errorf("short status origin = %v, want %v", got, want)
	}

	// Narrow terminal: single commits column at the left edge.
	m = Model{width: 30, height: 24}
	g = m.layout()
	if got, want := g.pos[panelCommits], (point{0, 1}); got != want {
		t.Errorf("narrow commits origin = %v, want %v", got, want)
	}
}
```

- [ ] **Step 2: Run it**

Run: `go test ./internal/tui/ -run TestLayoutOrigins -v`
Expected: FAIL — `undefined: point` / `g.pos undefined` (compile error).

- [ ] **Step 3: Implement**

In `viewstate.go`, extend the geometry:

```go
// point is a cell coordinate on the rendered screen (x = column, y = row).
type point struct{ x, y int }

// layoutGeom is the panel geometry renderInterface draws with. boxH holds each
// panel's box height under the current layout; a panel missing from the map
// (or 0) is not visible at this terminal size. pos holds each visible panel's
// top-left corner (the header occupies screen row 0).
type layoutGeom struct {
	w, h, bodyH   int
	leftW, rightW int
	boxH          map[panel]int
	pos           map[panel]point
}
```

In `layout()`, initialize `pos: map[panel]point{}` alongside `boxH`, then set origins exactly where the heights are assigned:

```go
	// narrow branch:
	g.rightW = w
	g.boxH[panelCommits] = bodyH
	g.pos[panelCommits] = point{0, 1}
	return g
```

```go
	// wide branches:
	if bodyH >= 9 {
		h1 := bodyH / 3
		h2 := bodyH / 3
		g.boxH[panelBranches] = h1
		g.boxH[panelWorktrees] = h2
		g.boxH[panelStatus] = bodyH - h1 - h2
		g.pos[panelBranches] = point{0, 1}
		g.pos[panelWorktrees] = point{0, 1 + h1}
		g.pos[panelStatus] = point{0, 1 + h1 + h2}
	} else {
		bh := bodyH / 2
		g.boxH[panelBranches] = bh
		g.boxH[panelStatus] = bodyH - bh
		g.pos[panelBranches] = point{0, 1}
		g.pos[panelStatus] = point{0, 1 + bh}
	}
	g.boxH[panelCommits] = bodyH
	g.pos[panelCommits] = point{leftW, 1}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/tui/ -run TestLayoutOrigins -v` → PASS, then `go test ./internal/tui/` → all PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -l internal/tui/ && go vet ./internal/tui/
git add internal/tui/viewstate.go internal/tui/*_test.go
git commit -m "feat(tui): layout() exposes panel screen origins"
```

---

### Task 2: `overlayAt` extraction

**Files:**
- Modify: `internal/tui/view.go:11-57` (overlayCenter → overlayAt + wrapper)
- Test: append beside the existing overlay tests (grep `overlayCenter` in `internal/tui/*_test.go` for the right file; if only indirect popup tests exist, create the test in `internal/tui/tooltip_test.go`... no — keep overlay tests with the compositor: append to the file where popup overlay tests live, or `view_overlay_test.go` if none names it)

- [ ] **Step 1: Write the failing test**

```go
func TestOverlayAtPlacesAtCoordinates(t *testing.T) {
	bg := strings.Join([]string{
		"aaaaaaaaaa",
		"bbbbbbbbbb",
		"cccccccccc",
		"dddddddddd",
	}, "\n")
	out := overlayAt(bg, "XX", 3, 1, 10, 4)
	lines := strings.Split(out, "\n")
	if lines[1] != "bbbXXbbbbb" {
		t.Errorf("row 1 = %q, want XX at col 3", lines[1])
	}
	if lines[0] != "aaaaaaaaaa" || lines[2] != "cccccccccc" {
		t.Error("rows outside the overlay must be untouched")
	}
}

func TestOverlayAtClampsNegativeAndOverflow(t *testing.T) {
	bg := "aaaa\nbbbb"
	out := overlayAt(bg, "XY", -5, -5, 4, 2)
	if lines := strings.Split(out, "\n"); lines[0] != "XYaa" {
		t.Errorf("negative coords must clamp to 0,0; row 0 = %q", lines[0])
	}
	// A row beyond the grid is dropped, not panicking.
	out = overlayAt(bg, "XY", 0, 5, 4, 2)
	if lines := strings.Split(out, "\n"); lines[0] != "aaaa" || lines[1] != "bbbb" {
		t.Errorf("out-of-grid overlay must leave bg unchanged: %q", out)
	}
}
```

- [ ] **Step 2: Run it**

Run: `go test ./internal/tui/ -run TestOverlayAt -v`
Expected: FAIL — `undefined: overlayAt`.

- [ ] **Step 3: Implement** — replace `overlayCenter` in `view.go` with:

```go
// overlayAt composites fg on top of bg with fg's top-left corner at cell
// (left, top), replacing the cells fg covers while keeping the surrounding bg
// visible. Both are treated as a grid of termW×termH cells; negative
// coordinates clamp to 0 and rows outside the grid are dropped. ANSI styling
// in both layers is preserved (slicing is width-aware).
func overlayAt(bg, fg string, left, top, termW, termH int) string {
	bgLines := strings.Split(bg, "\n")
	for len(bgLines) < termH {
		bgLines = append(bgLines, "")
	}
	fgLines := strings.Split(fg, "\n")

	fgW := 0
	for _, l := range fgLines {
		if w := ansi.StringWidth(l); w > fgW {
			fgW = w
		}
	}
	if top < 0 {
		top = 0
	}
	if left < 0 {
		left = 0
	}

	for i, fl := range fgLines {
		row := top + i
		if row < 0 || row >= len(bgLines) {
			continue
		}
		bgLine := bgLines[row]
		// Left slice of the background, padded out to the overlay's left edge.
		leftPart := ansi.Truncate(bgLine, left, "")
		if w := ansi.StringWidth(leftPart); w < left {
			leftPart += strings.Repeat(" ", left-w)
		}
		// Pad the overlay line to a clean rectangle so its right edge is straight.
		if w := ansi.StringWidth(fl); w < fgW {
			fl += strings.Repeat(" ", fgW-w)
		}
		// Background to the right of the overlay (empty if the bg line is shorter).
		rightPart := ansi.TruncateLeft(bgLine, left+fgW, "")
		bgLines[row] = leftPart + fl + rightPart
	}
	return strings.Join(bgLines, "\n")
}

// overlayCenter composites fg centered on top of bg (see overlayAt).
func overlayCenter(bg, fg string, termW, termH int) string {
	fgLines := strings.Split(fg, "\n")
	fgW := 0
	for _, l := range fgLines {
		if w := ansi.StringWidth(l); w > fgW {
			fgW = w
		}
	}
	return overlayAt(bg, fg, (termW-fgW)/2, (termH-len(fgLines))/2, termW, termH)
}
```

Note: `overlayCenter`'s old body clamped negative top/left to 0 — `overlayAt` keeps that clamp, so centering behavior is identical, including for oversized fg.

- [ ] **Step 4: Run the whole package** (popup overlay tests must pass unchanged)

Run: `go test ./internal/tui/` → ALL PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -l internal/tui/ && go vet ./internal/tui/
git add internal/tui/view.go internal/tui/*_test.go
git commit -m "refactor(tui): extract position-aware overlayAt from overlayCenter"
```

---

### Task 3: Tooltip helper + render wiring

**Files:**
- Create: `internal/tui/tooltip.go`
- Modify: `internal/tui/view.go` (render() wiring)
- Test: `internal/tui/tooltip_test.go` (new), `internal/tui/fit_test.go` (append)

- [ ] **Step 1: Write the failing tests** (`internal/tui/tooltip_test.go`)

```go
package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/gigagit/gg/internal/model"
)

const longPath = "/very/long/path/that/will/definitely/not/fit/in/a/narrow/panel"

// tooltipModel: 50-col terminal → left panels ~16 cols inner, so the second
// worktree row is guaranteed to truncate.
func tooltipModel() Model {
	return Model{
		width: 50, height: 24,
		focus:           panelWorktrees,
		sel:             map[panel]int{panelWorktrees: 1},
		currentWorktree: "/repo",
		worktrees: []model.Worktree{
			{Path: "/repo", Branch: "main"},
			{Path: longPath, Branch: "feature/login"},
		},
	}
}

func TestTooltipShowsFullRowAboveSelection(t *testing.T) {
	m := tooltipModel()
	lines, _, y, ok := m.tooltip()
	if !ok {
		t.Fatal("want a tooltip for the truncated selected row")
	}
	plain := ansi.Strip(strings.Join(lines, "\n"))
	if !strings.Contains(plain, longPath) {
		t.Fatalf("tooltip must contain the full path, got %q", plain)
	}
	g := m.layout()
	_, selInWin := windowRows(mustRows(t, m, panelWorktrees), g.boxH[panelWorktrees]-3, 1)
	rowY := g.pos[panelWorktrees].y + 2 + selInWin
	if want := rowY - len(lines); y != want {
		t.Errorf("tooltip y = %d, want %d (directly above row line %d)", y, want, rowY)
	}
}

func mustRows(t *testing.T, m Model, p panel) []string {
	t.Helper()
	rows, _ := m.panelView(p)
	if len(rows) == 0 {
		t.Fatal("expected rows")
	}
	return rows
}

func TestTooltipHiddenWhenRowFits(t *testing.T) {
	m := tooltipModel()
	m.sel[panelWorktrees] = 0 // "* main  /repo" fits comfortably
	if _, _, _, ok := m.tooltip(); ok {
		t.Fatal("no tooltip when the selected row fits")
	}
}

func TestTooltipHiddenOnEmptyPanel(t *testing.T) {
	m := tooltipModel()
	m.worktrees = nil
	if _, _, _, ok := m.tooltip(); ok {
		t.Fatal("no tooltip for an empty panel")
	}
}

func TestTooltipWrapsAndCaps(t *testing.T) {
	m := tooltipModel()
	m.worktrees[1].Path = strings.Repeat("x", 500) // wider than 3 terminal lines
	lines, _, _, ok := m.tooltip()
	if !ok {
		t.Fatal("want a tooltip")
	}
	if len(lines) != 3 {
		t.Fatalf("want 3 wrapped lines, got %d", len(lines))
	}
	last := ansi.Strip(lines[len(lines)-1])
	if !strings.HasSuffix(strings.TrimRight(last, " "), "…") {
		t.Errorf("capped tooltip must end with …, got %q", last)
	}
	for i, l := range lines {
		if w := ansi.StringWidth(l); w > m.width {
			t.Errorf("line %d is %d cols, wider than the terminal (%d)", i, w, m.width)
		}
	}
}

func TestTooltipRenderedInView(t *testing.T) {
	m := tooltipModel()
	out := ansi.Strip(m.render())
	if !strings.Contains(out, longPath) {
		t.Fatal("rendered view must contain the tooltip's full path")
	}
}

func TestTooltipSuppressedByModal(t *testing.T) {
	m := tooltipModel()
	m.modal = &decisionModal{} // any non-nil modal takes over the screen
	out := ansi.Strip(m.render())
	if strings.Contains(out, longPath) {
		t.Fatal("modal view must not contain the tooltip")
	}
}

func TestWrapWidth(t *testing.T) {
	got := wrapWidth("abcdef", 3, 3)
	if len(got) != 2 || got[0] != "abc" || got[1] != "def" {
		t.Fatalf("wrapWidth = %q", got)
	}
	capped := wrapWidth(strings.Repeat("a", 10), 3, 2)
	if len(capped) != 2 || !strings.HasSuffix(capped[1], "…") {
		t.Fatalf("capped wrap = %q", capped)
	}
}

func TestTooltipY(t *testing.T) {
	if y := tooltipY(5, 2); y != 3 {
		t.Errorf("tooltipY(5,2) = %d, want 3 (above)", y)
	}
	if y := tooltipY(1, 3); y != 2 {
		t.Errorf("tooltipY(1,3) = %d, want 2 (flips below)", y)
	}
}
```

Adaptation notes for the implementer:
- `decisionModal` is the modal struct's likely name — READ `internal/tui/modal_test.go`/`model.go` first and use the real type + minimal valid value (existing modal tests show how; the render must not panic, so populate required fields the way `renderModal` needs — at minimum `req` with empty options is rendered fine since it only ranges).
- `ansi.Strip` is in `charmbracelet/x/ansi`; if the function is named differently in the pinned version (check existing tests for prior usage of stripping), use that.
- The y-assertion in the first test mirrors the implementation's formula on purpose: it pins the contract "directly above the selected row" against future drift of either side.

- [ ] **Step 2: Run them**

Run: `go test ./internal/tui/ -run 'TestTooltip|TestWrapWidth' -v`
Expected: FAIL — `undefined: tooltip` etc. (compile errors).

- [ ] **Step 3: Implement `internal/tui/tooltip.go`**

```go
package tui

import (
	"github.com/charmbracelet/lipgloss"
)

// tooltipStyle is the floating strip showing a truncated row's full text.
// Distinct from selectedRow (reverse video): black on yellow reads as an
// annotation layered over the UI.
var tooltipStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(lipgloss.Color("11"))

// tooltipMaxLines caps the strip's height; longer content re-truncates with …
const tooltipMaxLines = 3

// tooltip returns the styled lines and overlay position of the full-text
// strip for the focused panel's selected row, when that row is truncated in
// its panel. ok is false when nothing should be shown. Geometry comes from
// the same layout()/panelView/windowRows sources the renderer uses, so the
// two cannot drift.
func (m Model) tooltip() (lines []string, x, y int, ok bool) {
	g := m.layout()
	p := m.focus
	boxH := g.boxH[p]
	if boxH <= 0 {
		return nil, 0, 0, false
	}
	rows, _ := m.panelView(p)
	sel := m.sel[p]
	if len(rows) == 0 || sel < 0 || sel >= len(rows) {
		return nil, 0, 0, false
	}
	boxW := g.leftW
	if p == panelCommits {
		boxW = g.rightW
	}
	innerW := boxW - 4 // mirrors renderPanel: border (2) + padding (2)
	if lipgloss.Width("> "+rows[sel]) <= innerW {
		return nil, 0, 0, false // renderPanel shows it in full — nothing to add
	}
	rowsCap := boxH - 3 // mirrors renderPanel: borders + label line
	if rowsCap < 1 {
		return nil, 0, 0, false
	}
	_, selInWin := windowRows(rows, rowsCap, sel)
	origin := g.pos[p]
	rowY := origin.y + 2 + selInWin // top border + label line

	raw := wrapWidth(rows[sel], g.w, tooltipMaxLines)
	width := 0
	for _, l := range raw {
		if w := lipgloss.Width(l); w > width {
			width = w
		}
	}
	x = origin.x + 2 // the panel's content edge
	if x+width > g.w {
		x = g.w - width
	}
	if x < 0 {
		x = 0
	}
	y = tooltipY(rowY, len(raw))

	lines = make([]string, len(raw))
	for i, l := range raw {
		lines[i] = tooltipStyle.Render(padRight(l, width))
	}
	return lines, x, y, true
}

// tooltipY places an n-line strip directly above the row at rowY, flipping to
// directly below when there is no room above.
func tooltipY(rowY, n int) int {
	if y := rowY - n; y >= 0 {
		return y
	}
	return rowY + 1
}

// wrapWidth greedily wraps s into display-width-aware lines of at most w
// columns, capped at maxLines. If content remains past the cap, the last
// line is re-truncated to end in …
func wrapWidth(s string, w, maxLines int) []string {
	if w < 1 {
		w = 1
	}
	var out []string
	r := []rune(s)
	for len(r) > 0 && len(out) < maxLines {
		n, width := 0, 0
		for n < len(r) {
			cw := lipgloss.Width(string(r[n]))
			if width+cw > w {
				break
			}
			width += cw
			n++
		}
		if n == 0 {
			n = 1 // a single glyph wider than w: emit it rather than loop forever
		}
		out = append(out, string(r[:n]))
		r = r[n:]
	}
	if len(r) > 0 && len(out) > 0 {
		// Remainder exists: force an ellipsis onto the (full-width) last line.
		out[len(out)-1] = truncate(out[len(out)-1]+string(r), w)
	}
	return out
}
```

- [ ] **Step 4: Wire `render()` in `view.go`**

Right after `bg := m.renderInterface()`, BEFORE the popup branches (read the current popup pointer list in `render()` and include every one of them in the nil-check):

```go
	bg := m.renderInterface()
	if m.popup == nil && m.repoPopup == nil && m.settings == nil && m.branchPopup == nil {
		if lines, x, y, ok := m.tooltip(); ok {
			w, h := m.overlayDims()
			bg = overlayAt(bg, strings.Join(lines, "\n"), x, y, w, h)
		}
	}
```

(The modal case already returned earlier in `render()`. If main has gained additional popup pointers since this plan was written, add them to the condition — the invariant is: tooltip only in the plain interface state.)

- [ ] **Step 5: Extend the fit invariant** (`internal/tui/fit_test.go`, append)

```go
// A visible tooltip must not push the frame beyond the terminal bounds.
func TestRenderWithTooltipStaysInBounds(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 50, 24
	m.focus = panelWorktrees
	m.worktrees = []model.Worktree{
		{Path: "/repo", Branch: "main"},
		{Path: "/" + strings.Repeat("deep/", 40) + "end", Branch: "feature/x"},
	}
	m.sel[panelWorktrees] = 1

	out := m.View()
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) > m.height {
		t.Fatalf("render produced %d lines, want <= %d", len(lines), m.height)
	}
	for i, ln := range lines {
		if w := lipgloss.Width(ln); w > m.width {
			t.Fatalf("line %d is %d cols wide, want <= %d: %q", i, w, m.width, ln)
		}
	}
}
```

- [ ] **Step 6: Run the whole package**

Run: `go test ./internal/tui/ -v -count=1`
Expected: ALL PASS (new tooltip tests + every pre-existing test).

- [ ] **Step 7: Commit**

```bash
gofmt -l internal/tui/ && go vet ./internal/tui/
git add internal/tui/tooltip.go internal/tui/tooltip_test.go internal/tui/view.go internal/tui/fit_test.go
git commit -m "feat(tui): tooltip shows a truncated row's full text above the selection"
```

---

### Task 4: Docs + final gates

**Files:**
- Modify: `CHANGELOG.md` (current Added section)
- Modify: `README.md` (TUI feature list, only if one exists — read first)
- Modify: `.claude/skills/adding-tui-windows/SKILL.md` (surface taxonomy)

- [ ] **Step 1: CHANGELOG.md** — add under the current `### Added` (mirror existing style):

```markdown
#### Truncation tooltip
- TUI: when the focused panel's selected row is too wide and ellipsized, a
  floating strip directly above the row shows its full text (all four list
  panels; wraps up to 3 lines; suppressed under modals/popups).
```

- [ ] **Step 2: README.md** — if the TUI/features section lists interactions, add one line mirroring the CHANGELOG entry; otherwise skip (don't invent a section).

- [ ] **Step 3: adding-tui-windows skill** — in the surface taxonomy (panel/popup/modal), add the tooltip as a fourth, read-only surface kind: positioned via `layoutGeom.pos` + `overlayAt`, no key routing, no state — so future agents don't reinvent or misuse it for interactive surfaces. Two or three sentences in the existing style.

- [ ] **Step 4: Final gates**

```bash
./test.sh race
```
Expected: gates clean, all unit packages PASS, e2e PASS, exit 0.

- [ ] **Step 5: Commit**

```bash
git add CHANGELOG.md README.md .claude/skills/adding-tui-windows/SKILL.md
git commit -m "docs: record the truncation tooltip"
```

---

## Self-review notes (already applied)

- Spec coverage: trigger+scope (Task 3 tooltip()), geometry origins (Task 1), overlayAt refactor (Task 2), styling/wrap/cap (Task 3), suppression (Task 3 wiring + tests), flip-below (tooltipY + unit test — the flip is unreachable through real layouts today since the smallest rowY is 3 and the cap is 3 lines, so it's pinned by the pure-function test instead of a render test), fit invariant (Task 3 step 5), docs (Task 4). No gaps.
- Type consistency: `point{x,y}`, `layoutGeom.pos map[panel]point`, `overlayAt(bg, fg string, left, top, termW, termH int)`, `tooltip() ([]string, int, int, bool)`, `wrapWidth(s string, w, maxLines int) []string`, `tooltipY(rowY, n int) int` — used identically across tasks.
- Known soft spots flagged inline: the modal struct's real name in TestTooltipSuppressedByModal; the popup pointer list in render() (main moves fast); the ansi strip helper's exact name.
```
