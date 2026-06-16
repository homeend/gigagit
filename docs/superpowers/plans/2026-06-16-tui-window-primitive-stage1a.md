# TUI Window Primitive + Grid/Surface Unification (Stage 1a) Implementation Plan

> **Status: IMPLEMENTED (2026-06-16)** on branch `worktree-tui-window-framework`.
> Full `./test.sh race` (unit + e2e) green.
>
> **Execution deviation — display-mode key is `z`, not `w`.** During Task 4 it
> was found that **both** `w` (existing-branch worktree, `model.go:419`) and `W`
> (new-branch worktree, `:425`) are taken, so `w` could not be freed. Per user
> decision the unified display-mode key is **`z`** everywhere (the diff view's
> long-line cycle was migrated `w`→`z`); both worktree keys are unchanged.
> Wherever the tasks below say `w`/`W`, read `z` (and "no worktree remap"). The
> spec is updated to match. Also: the user requested a separate `.` action
> context-menu feature (config-driven footer vs. menu placement) — captured in
> the spec as a follow-up, not built here.

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Introduce a reusable list/text window primitive with three switchable display modes (cutoff / wrap / scroll, cycled with `w`) and route the base panels, the stash list, the files tree, the history and blame surfaces, and the repo switcher popup through it.

**Architecture:** A new stateless `renderWindow(rows []winRow, winOpts) []string` lays rows out into a `w×h` box under a `dispMode`, applying each row's style *after* truncation/wrapping (ANSI-safe). Callers keep building their decorated row strings (cursor/mark prefixes, heading/selection styles) exactly as today; they stop calling `truncate`/`padRight`/`windowRows` inline and call the primitive instead. Per-window mode lives in caller-owned state (a `Model` map for panels; a struct field for popups/surfaces). Compositing is untouched — this is content rendering only.

**Tech Stack:** Go 1.26, Bubble Tea (value-receiver `Model`), lipgloss, `github.com/charmbracelet/x/ansi`. Tests use Go's `testing` with the existing TUI helpers.

**Spec:** [`docs/superpowers/specs/2026-06-16-tui-window-framework-design.md`](../specs/2026-06-16-tui-window-framework-design.md) — this plan is the first half of that spec's Stage 1 ("content unification"). The remaining popups (settings/branch/conflict/content/pairop/stash-action) plus the generalized popup reveal are Plan 1b; tabs are Stage 2; Files/Staged is Stage 3.

---

## File structure

| File | Responsibility | Action |
|---|---|---|
| `internal/tui/window.go` | The `dispMode` enum + `winRow`/`winOpts` types + `renderWindow` + `hslice`/`rowTruncated` helpers | Create |
| `internal/tui/window_test.go` | Unit tests for the primitive | Create |
| `internal/tui/model.go` | Add `dispModes`/`hscroll` maps; cycle key; `w`→mode, worktree-create `w`→`W` | Modify |
| `internal/tui/view.go` | `renderPanel` + `renderListBox` route through `renderWindow` | Modify |
| `internal/tui/files_view.go` | `renderFilesView` tree body routes through `renderWindow`; `w`/`shift+left/right` | Modify |
| `internal/tui/history_view.go` | history list body routes through `renderWindow`; `w`/`shift+left/right` | Modify |
| `internal/tui/blame_view.go` | blame body routes through `renderWindow`; `w`/`shift+left/right` | Modify |
| `internal/tui/repo_popup.go` | repo popup body routes through `renderWindow`; add `mode` field + `w` | Modify |
| `internal/tui/help.go`, `internal/tui/footer.go` | advertise `w` (display mode), `W` (worktree) | Modify |
| `CHANGELOG.md`, `README.md` | document the modes + `W` remap | Modify |

**Convention reminders:** `main` is trunk; this plan runs on a feature branch (already `feat/tui-window-framework`). Run `./test.sh race` before the final merge. `internal/tui` must not import `internal/git`. lipgloss strips color in the non-TTY test environment — to assert styling, set `lipgloss.SetColorProfile(termenv.TrueColor)` in the test and compare `ansi.Strip(...)` for text; default tests assert plain text.

---

### Task 1: `dispMode` + `renderWindow` (cutoff mode)

**Files:**
- Create: `internal/tui/window.go`
- Create: `internal/tui/window_test.go`

- [ ] **Step 1: Write the failing test**

```go
package tui

import (
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestRenderWindowCutoff(t *testing.T) {
	rows := []winRow{
		{text: "short"},
		{text: "this row is far too long to fit in the narrow box"},
		{text: "tail"},
	}
	out := renderWindow(rows, winOpts{w: 10, h: 3, mode: modeCutoff, anchor: 0})
	if len(out) != 3 {
		t.Fatalf("want 3 lines, got %d", len(out))
	}
	// Each line is exactly w display columns.
	for i, l := range out {
		if w := ansi.StringWidth(l); w != 10 {
			t.Errorf("line %d width = %d, want 10", i, w)
		}
	}
	// The over-long row is truncated with an ellipsis.
	if got := ansi.Strip(out[1]); got != "this row …" {
		t.Errorf("row 1 = %q, want %q", got, "this row …")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui/ -run TestRenderWindowCutoff`
Expected: FAIL — `undefined: winRow` / `renderWindow`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/tui/window.go`:

```go
package tui

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// dispMode is how a window lays out rows that are wider than its box. It is
// cycled with the `w` key and generalizes the diff view's long-line modes to
// every list/text window.
type dispMode int

const (
	modeCutoff dispMode = iota // truncate each row to width (one line) + reveal
	modeWrap                   // wrap each row onto multiple lines
	modeScroll                 // keep rows full; reveal via horizontal scroll
	dispModeCount
)

// next returns the following mode, wrapping around.
func (d dispMode) next() dispMode { return (d + 1) % dispModeCount }

// winRow is one logical row before layout: raw (unstyled) text plus an optional
// style applied AFTER truncation/wrapping. Callers bake any cursor/mark prefix
// into text and set style for the selected row (selectedRow) or headings
// (titleStyle); the primitive never adds prefixes itself.
type winRow struct {
	text  string
	style lipgloss.Style // zero value renders the text unchanged
}

// winOpts is everything renderWindow needs besides the rows. anchor is the
// logical row kept visible by the vertical window (typically the selection).
type winOpts struct {
	w, h    int
	mode    dispMode
	anchor  int
	hscroll int // modeScroll horizontal offset (display columns)
}

// renderWindow lays rows out under o and returns exactly o.h display lines,
// each padded to o.w columns. Row styling is applied only after truncation or
// wrapping, so it can never corrupt the width-based slicing (ANSI-safety).
func renderWindow(rows []winRow, o winOpts) []string {
	w, h := o.w, o.h
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}

	type dline struct {
		text  string
		style lipgloss.Style
		row   int
	}
	var dl []dline
	for ri, r := range rows {
		var segs []string
		switch o.mode {
		case modeWrap:
			segs = wrapWidth(r.text, w, 1<<20) // huge cap => clean full wrap, no ellipsis
		case modeScroll:
			segs = []string{hslice(r.text, o.hscroll, w)}
		default:
			segs = []string{truncate(r.text, w)}
		}
		if len(segs) == 0 {
			segs = []string{""}
		}
		for _, s := range segs {
			dl = append(dl, dline{text: s, style: r.style, row: ri})
		}
	}

	anchorLine := 0
	for i, d := range dl {
		if d.row == o.anchor {
			anchorLine = i
			break
		}
	}
	start := windowStart(len(dl), h, anchorLine)

	out := make([]string, 0, h)
	for i := 0; i < h; i++ {
		idx := start + i
		if idx >= len(dl) {
			out = append(out, padRight("", w))
			continue
		}
		out = append(out, dl[idx].style.Render(padRight(dl[idx].text, w)))
	}
	return out
}

// hslice returns the display-column window [off, off+w) of raw text s, padded
// implicitly by the caller (padRight). Width-aware so wide glyphs never split.
func hslice(s string, off, w int) string {
	if off > 0 {
		s = ansi.TruncateLeft(s, off, "")
	}
	return ansi.Truncate(s, w, "")
}

// rowTruncated reports whether s would be cut off in a w-wide cutoff window
// (drives the truncated-row reveal).
func rowTruncated(s string, w int) bool { return lipgloss.Width(s) > w }
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tui/ -run TestRenderWindowCutoff`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/window.go internal/tui/window_test.go
git commit -m "feat(tui): window primitive with cutoff display mode"
```

---

### Task 2: `renderWindow` wrap mode

**Files:**
- Modify: `internal/tui/window.go` (already handles wrap via `wrapWidth`; this task proves + locks it)
- Test: `internal/tui/window_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestRenderWindowWrap(t *testing.T) {
	// One 12-col row in an 6-wide box wraps to two display lines.
	rows := []winRow{{text: "aaaaaabbbbbb"}}
	out := renderWindow(rows, winOpts{w: 6, h: 3, mode: modeWrap, anchor: 0})
	if len(out) != 3 {
		t.Fatalf("want 3 lines, got %d", len(out))
	}
	if got := ansi.Strip(out[0]); got != "aaaaaa" {
		t.Errorf("line 0 = %q, want %q", got, "aaaaaa")
	}
	if got := ansi.Strip(out[1]); got != "bbbbbb" {
		t.Errorf("line 1 = %q, want %q", got, "bbbbbb")
	}
	if got := ansi.Strip(out[2]); got != "      " { // blank pad row
		t.Errorf("line 2 = %q, want 6 spaces", got)
	}
}

func TestRenderWindowWrapStylesAllSegments(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(termenv.Ascii) })
	rows := []winRow{{text: "aaaaaabbbbbb", style: selectedRow}}
	out := renderWindow(rows, winOpts{w: 6, h: 2, mode: modeWrap, anchor: 0})
	for i, l := range out {
		if !strings.Contains(l, "\x1b[") { // both wrapped lines carry the style
			t.Errorf("wrapped line %d not styled: %q", i, l)
		}
	}
}
```

Add imports `"strings"`, `"github.com/charmbracelet/lipgloss"`, `"github.com/muesli/termenv"` to the test file.

- [ ] **Step 2: Run test to verify it fails (or passes)**

Run: `go test ./internal/tui/ -run TestRenderWindowWrap`
Expected: PASS if Task 1's `wrapWidth` branch is correct; if `TestRenderWindowWrapStylesAllSegments` fails, the style is being applied before wrap — fix per Step 3.

- [ ] **Step 3: Confirm implementation**

No code change expected (Task 1 already wraps and styles per display line). If the style test fails, ensure `renderWindow` applies `dl[idx].style.Render(...)` per produced line (it does). Keep as-is.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tui/ -run TestRenderWindowWrap`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/window_test.go
git commit -m "test(tui): lock window wrap mode + per-segment styling"
```

---

### Task 3: `renderWindow` scroll mode (horizontal offset)

**Files:**
- Modify: `internal/tui/window.go` (scroll branch already present; lock + test)
- Test: `internal/tui/window_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestRenderWindowScroll(t *testing.T) {
	rows := []winRow{{text: "0123456789ABCDEF"}}
	// No offset: first w columns.
	out := renderWindow(rows, winOpts{w: 5, h: 1, mode: modeScroll, anchor: 0})
	if got := ansi.Strip(out[0]); got != "01234" {
		t.Errorf("hscroll 0 = %q, want %q", got, "01234")
	}
	// Offset 5: reveals later columns.
	out = renderWindow(rows, winOpts{w: 5, h: 1, mode: modeScroll, anchor: 0, hscroll: 5})
	if got := ansi.Strip(out[0]); got != "56789" {
		t.Errorf("hscroll 5 = %q, want %q", got, "56789")
	}
}
```

- [ ] **Step 2: Run test to verify it fails (or passes)**

Run: `go test ./internal/tui/ -run TestRenderWindowScroll`
Expected: PASS (the `hslice` branch from Task 1 implements this). If it fails, verify `hslice` uses `ansi.TruncateLeft` then `ansi.Truncate`.

- [ ] **Step 3: Confirm implementation** — no change expected.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tui/ -run TestRenderWindowScroll`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/window_test.go
git commit -m "test(tui): lock window scroll mode horizontal offset"
```

---

### Task 4: `Model` per-panel mode state + cycle the focused panel with `w`, remap worktree-create to `W`

**Files:**
- Modify: `internal/tui/model.go:82-90` (struct fields), `:104-111` (`New`), `:419` (`case "w"`)
- Test: `internal/tui/keys_test.go` (new test func)

**Context:** `model.go:419` currently is `case "w": // worktree for the selected EXISTING branch`. We move that action to `W` and make `w` cycle the focused panel's display mode. The panel maps mirror `sel`/`sortModes`.

- [ ] **Step 1: Write the failing test**

Add to `internal/tui/keys_test.go`:

```go
func TestWCyclesFocusedPanelMode(t *testing.T) {
	// Build the Model with a literal: New(svc) dereferences svc (svc.CommitFeed()),
	// so it cannot take nil. Existing TUI tests use Model{...} literals.
	m := Model{width: 80, height: 24, focus: panelCommits,
		sel: map[panel]int{}, dispModes: map[panel]dispMode{}, hscroll: map[panel]int{}}
	if m.dispModes[panelCommits] != modeCutoff {
		t.Fatalf("default mode = %v, want modeCutoff", m.dispModes[panelCommits])
	}
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("w")})
	if got := m2.(Model).dispModes[panelCommits]; got != modeWrap {
		t.Errorf("after w, mode = %v, want modeWrap", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui/ -run TestWCyclesFocusedPanelMode`
Expected: FAIL — `m.dispModes` is nil / `w` does not cycle.

- [ ] **Step 3: Write minimal implementation**

In `model.go` struct (after `sortModes`):

```go
	sortModes     map[panel]sortMode // per-panel display order (zero value = default)
	dispModes     map[panel]dispMode // per-panel text display mode (zero value = modeCutoff); w cycles
	hscroll       map[panel]int      // per-panel horizontal scroll (modeScroll); shift+←/→
	headTimes     map[string]int64   // worktree HEAD sha -> committer time (date sort)
```

In `New`:

```go
	return Model{
		svc:       svc,
		feed:      svc.CommitFeed(),
		loading:   true,
		sel:       map[panel]int{},
		sortModes: map[panel]sortMode{panelBranches: sortDateDesc},
		dispModes: map[panel]dispMode{},
		hscroll:   map[panel]int{},
	}
```

At `model.go:419`, change the existing worktree case key from `"w"` to `"W"` (leave its body unchanged), and add a new `w` case that cycles the focused panel mode. Place the new `w` case in the same base-grid key switch (the one guarded against filter-typing):

```go
		case "W": // worktree for the selected EXISTING branch (was w)
			// ... existing body unchanged ...
		case "w": // cycle the focused panel's text display mode
			m.dispModes[m.focus] = m.dispModes[m.focus].next()
			m.hscroll[m.focus] = 0
			return m, nil
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tui/ -run TestWCyclesFocusedPanelMode`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/model.go internal/tui/keys_test.go
git commit -m "feat(tui): w cycles focused panel display mode; worktree-create moves to W"
```

---

### Task 5: Route `renderPanel` through `renderWindow`

**Files:**
- Modify: `internal/tui/view.go:306-355` (`renderPanel`)
- Test: existing `internal/tui/fit_test.go` / `focus_test.go` / `nav_test.go` must stay green; add one mode test.

**Context:** Keep all the prefix/mark/selection logic; only replace the inline `windowRows`+`truncate`+`padRight` body with a `renderWindow` call. The label line and border framing stay in `renderPanel`.

- [ ] **Step 1: Write the failing test**

Add to `internal/tui/fit_test.go`:

```go
func TestRenderPanelWrapModeExpandsRow(t *testing.T) {
	m := Model{width: 80, height: 24, focus: panelBranches,
		sel: map[panel]int{}, dispModes: map[panel]dispMode{panelBranches: modeWrap}, hscroll: map[panel]int{}}
	m.branches = []model.Branch{{Name: strings.Repeat("x", 60)}}
	out := m.renderPanel(panelBranches, "Branches", m.branchRows(), 20, 6)
	// In wrap mode the 60-char branch name occupies more than one body line.
	if strings.Count(out, "x") < 30 {
		t.Errorf("wrap mode did not expand the long row:\n%s", out)
	}
}
```

(Import `strings` and `github.com/gigagit/gg/internal/model` if not present.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui/ -run TestRenderPanelWrapModeExpandsRow`
Expected: FAIL — `renderPanel` ignores `dispModes`, truncates to one line.

- [ ] **Step 3: Write minimal implementation**

Replace the data-row block of `renderPanel` (the `else` branch that loops `win`) with a `renderWindow` call. Full new body of `renderPanel`:

```go
func (m Model) renderPanel(p panel, label string, rows []string, boxW, boxH int) string {
	contentH := boxH - 2
	if contentH < 1 {
		contentH = 1
	}
	innerW := boxW - 4
	if innerW < 1 {
		innerW = 1
	}
	rowsCap := contentH - 1
	if rowsCap < 0 {
		rowsCap = 0
	}

	lines := make([]string, 0, contentH)
	lines = append(lines, padRight(truncate(label, innerW), innerW))

	if rowsCap < 1 {
		// No room for data rows below the label.
	} else if len(rows) == 0 {
		lines = append(lines, padRight(truncate("  (none)", innerW), innerW))
	} else {
		marked := m.markedDisplayIndices(p)
		sel := m.sel[p]
		wr := make([]winRow, len(rows))
		for i, row := range rows {
			prefix := "  "
			style := lipgloss.Style{}
			focused := i == sel && m.panelFocused(p)
			if marked[i] {
				prefix = "◆ "
			} else if focused {
				prefix = "> "
			}
			if focused {
				style = selectedRow
			}
			wr[i] = winRow{text: prefix + row, style: style}
		}
		body := renderWindow(wr, winOpts{
			w: innerW, h: rowsCap, mode: m.dispModes[p], anchor: sel, hscroll: m.hscroll[p],
		})
		lines = append(lines, body...)
	}
	for len(lines) < contentH {
		lines = append(lines, padRight("", innerW))
	}

	style := bluredPanel
	if m.panelFocused(p) {
		style = focusedPanel
	}
	return style.Render(strings.Join(lines, "\n"))
}
```

Add `"github.com/charmbracelet/lipgloss"` usage is already imported in view.go.

Note: `markedDisplayIndices(p)` previously was indexed by `start+i` (window-relative). Now we build `winRow`s for *all* rows and let `renderWindow` window them, so mark lookup is by absolute display index `i`. Verify `markedDisplayIndices` returns a map keyed by display-row index (it does — see `mark.go`); if it is keyed differently, adjust the lookup to match.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tui/ -run 'TestRenderPanelWrapModeExpandsRow|TestFit|TestFocus|TestNav'`
Expected: PASS (cutoff default reproduces prior output; wrap test passes).

- [ ] **Step 5: Commit**

```bash
git add internal/tui/view.go internal/tui/fit_test.go
git commit -m "refactor(tui): renderPanel renders rows through the window primitive"
```

---

### Task 6: Route `renderListBox` (stash) through `renderWindow` + `w`/hscroll in the stash view

**Files:**
- Modify: `internal/tui/view.go:359-395` (`renderListBox`)
- Modify: `internal/tui/stash_view.go` (`stashView` gets `mode`/`hscroll`; `updateStashViewKey` handles `w`, `shift+left/right`; `renderStashList` passes them)
- Test: `internal/tui/stash_view_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestStashListWrapMode(t *testing.T) {
	m := Model{width: 80, height: 24, focus: panelCommits, sel: map[panel]int{}}
	m.stashView = &stashView{entries: []model.StashEntry{{Ref: "stash@{0}", Subject: strings.Repeat("z", 60)}}, mode: modeWrap}
	out := m.renderStashList(20, 6)
	if strings.Count(out, "z") < 30 {
		t.Errorf("stash wrap mode did not expand the long subject:\n%s", out)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui/ -run TestStashListWrapMode`
Expected: FAIL — `stashView` has no `mode` field / `renderListBox` truncates.

- [ ] **Step 3: Write minimal implementation**

Add fields to `stashView` (stash_view.go):

```go
type stashView struct {
	entries []model.StashEntry
	sel     int
	mode    dispMode // text display mode; w cycles
	hscroll int      // modeScroll horizontal offset
	loading bool
	err     error
	tag     string
}
```

Change `renderListBox` signature to take mode + hscroll, routing the body through `renderWindow`:

```go
func (m Model) renderListBox(label string, rows []string, sel, boxW, boxH int, focused bool, mode dispMode, hscroll int) string {
	contentH := boxH - 2
	if contentH < 1 {
		contentH = 1
	}
	innerW := boxW - 4
	if innerW < 1 {
		innerW = 1
	}
	rowsCap := contentH - 1
	if rowsCap < 0 {
		rowsCap = 0
	}
	lines := []string{padRight(truncate(label, innerW), innerW)}
	if rowsCap >= 1 && len(rows) > 0 {
		wr := make([]winRow, len(rows))
		for i, row := range rows {
			prefix := "  "
			style := lipgloss.Style{}
			if i == sel && focused {
				prefix = "> "
				style = selectedRow
			}
			wr[i] = winRow{text: prefix + row, style: style}
		}
		body := renderWindow(wr, winOpts{w: innerW, h: rowsCap, mode: mode, anchor: sel, hscroll: hscroll})
		lines = append(lines, body...)
	}
	for len(lines) < contentH {
		lines = append(lines, padRight("", innerW))
	}
	style := bluredPanel
	if focused {
		style = focusedPanel
	}
	return style.Render(strings.Join(lines, "\n"))
}
```

Update the caller in `stash_view.go` `renderStashList`:

```go
	return m.renderListBox("Stashes", rows, v.sel, boxW, boxH, focused, v.mode, v.hscroll)
```

In `updateStashViewKey`, add (alongside the existing cases):

```go
	case "w":
		v.mode = v.mode.next()
		v.hscroll = 0
		return m, nil
	case "shift+left":
		if v.mode == modeScroll && v.hscroll > 0 {
			v.hscroll -= m.hscrollStep()
			if v.hscroll < 0 {
				v.hscroll = 0
			}
		}
		return m, nil
	case "shift+right":
		if v.mode == modeScroll {
			v.hscroll += m.hscrollStep()
		}
		return m, nil
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tui/ -run 'TestStashListWrapMode|TestStash'`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/view.go internal/tui/stash_view.go internal/tui/stash_view_test.go
git commit -m "refactor(tui): stash list renders through the window primitive with w-cycle"
```

---

### Task 7: Route `renderFilesView` tree through `renderWindow` + `w`/hscroll in the files view

**Files:**
- Modify: `internal/tui/files_view.go:280-338` (`renderFilesView`), and `updateFilesViewKey` (add `w`, `shift+left/right`); the `contentPopup` struct (the files tree state) gets `mode`/`hscroll` fields.
- Test: `internal/tui/files_view_test.go`

**Context:** The files tree state is a `*contentPopup` (`m.filesView`). Add `mode`/`hscroll` there. The tree rows carry a heading style; preserve "cursor wins over heading."

- [ ] **Step 1: Write the failing test**

```go
func TestFilesViewWrapMode(t *testing.T) {
	m := Model{width: 100, height: 30, sel: map[panel]int{}}
	p := &contentPopup{lines: []contentLine{{text: strings.Repeat("q", 80), path: "x"}}, mode: modeWrap}
	m.filesView = p
	m.filesTitle = "Files"
	out := m.renderFilesView(20, 12)
	if strings.Count(out, "q") < 40 {
		t.Errorf("files view wrap mode did not expand the long row:\n%s", out)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui/ -run TestFilesViewWrapMode`
Expected: FAIL — `contentPopup` has no `mode`; tree truncates.

- [ ] **Step 3: Write minimal implementation**

Add `mode dispMode` and `hscroll int` fields to the `contentPopup` struct (find it in `content_popup.go`).

In `renderFilesView`, replace the `rows`+`windowRows` block with `renderWindow`. New body of the row section (keep title/hint framing):

```go
	vis := p.visible()
	wr := make([]winRow, len(vis))
	for i, l := range vis {
		prefix := "  "
		style := lipgloss.Style{}
		switch {
		case i == p.sel:
			prefix = "> "
			style = selectedRow // cursor wins over heading
		case l.heading:
			prefix = ""
			style = titleStyle
		}
		wr[i] = winRow{text: prefix + l.text, style: style}
	}
	win := renderWindow(wr, winOpts{w: innerW, h: rowsCap, mode: p.mode, anchor: p.sel, hscroll: p.hscroll})

	lines := make([]string, 0, contentH)
	lines = append(lines, padRight(truncate(title, innerW), innerW))
	if len(vis) == 0 {
		lines = append(lines, padRight(truncate("  (no match)", innerW), innerW))
	} else {
		lines = append(lines, win...)
	}
	for len(lines) < contentH-1 {
		lines = append(lines, padRight("", innerW))
	}
```

Keep the existing hint line and `style.Render(...)` framing below.

In `updateFilesViewKey`, add (outside the `p.typing` capture block, in the main switch):

```go
	case "w":
		p.mode = p.mode.next()
		p.hscroll = 0
		return m, nil
	case "shift+left":
		if p.mode == modeScroll && p.hscroll > 0 {
			p.hscroll -= m.hscrollStep()
			if p.hscroll < 0 {
				p.hscroll = 0
			}
		}
		return m, nil
	case "shift+right":
		if p.mode == modeScroll {
			p.hscroll += m.hscrollStep()
		}
		return m, nil
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tui/ -run 'TestFilesView'`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/files_view.go internal/tui/content_popup.go internal/tui/files_view_test.go
git commit -m "refactor(tui): files tree renders through the window primitive with w-cycle"
```

---

### Task 8: Route history + blame surfaces through `renderWindow`

**Files:**
- Modify: `internal/tui/history_view.go:115-173` (list body), add `mode`/`hscroll` to `historyView`, `w`/`shift+left/right` in `update`
- Modify: `internal/tui/blame_view.go` (body render + `mode`/`hscroll` + keys)
- Test: `internal/tui/history_view_test.go`, `internal/tui/blame_view_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/tui/history_view_test.go`:

```go
func TestHistoryViewWrapMode(t *testing.T) {
	h := &historyView{
		ctx:     navContext{path: "x"},
		commits: []model.FileCommit{{Hash: "abcdef0", Subject: strings.Repeat("w", 80), Status: "M"}},
		mode:    modeWrap,
	}
	m := Model{width: 50, height: 20} // < 60 => list-only, easier to assert
	out := h.render(m)
	if strings.Count(out, "w") < 30 {
		t.Errorf("history wrap mode did not expand the subject:\n%s", out)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui/ -run TestHistoryViewWrapMode`
Expected: FAIL — `historyView` has no `mode`; list truncates.

- [ ] **Step 3: Write minimal implementation**

Add `mode dispMode` and `hscroll int` to `historyView`. In `historyView.render`, replace the manual `rows`/`windowRows` list construction with `renderWindow`:

```go
	wr := make([]winRow, len(h.commits))
	for i, fc := range h.commits {
		line := shortHash(fc.Hash) + "  " + fc.Status + "  " + fc.Subject
		prefix := "  "
		style := lipgloss.Style{}
		if i == h.sel {
			prefix = "> "
			style = selectedRow
		}
		wr[i] = winRow{text: prefix + line, style: style}
	}
	win := renderWindow(wr, winOpts{w: listW, h: body, mode: h.mode, anchor: h.sel, hscroll: h.hscroll})
	if h.loading {
		win = padLines("  (loading…)", listW, body)
	} else if h.err != nil {
		win = padLines("  error: "+h.err.Error(), listW, body)
	} else if len(h.commits) == 0 {
		win = padLines("  (no history)", listW, body)
	}
```

Add a small helper to `history_view.go` (or reuse `padBox`):

```go
// padLines returns a body-high block whose first line is s (truncated to w).
func padLines(s string, w, body int) []string {
	out := make([]string, body)
	out[0] = padRight(truncate(s, w), w)
	for i := 1; i < body; i++ {
		out[i] = padRight("", w)
	}
	return out
}
```

In `historyView.update`, add the `w`/`shift+left`/`shift+right` cases (same shape as Task 6/7, cycling `h.mode`/`h.hscroll`).

Apply the analogous change to `blame_view.go` (add `mode`/`hscroll`, route its body through `renderWindow`, add the keys). Write a parallel `TestBlameViewWrapMode` in `blame_view_test.go` mirroring the history test against the blame view's row source.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tui/ -run 'TestHistoryView|TestBlameView'`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/history_view.go internal/tui/blame_view.go internal/tui/history_view_test.go internal/tui/blame_view_test.go
git commit -m "refactor(tui): history and blame render through the window primitive with w-cycle"
```

---

### Task 9: Route the repo switcher popup through `renderWindow` (fix wrapping) + `w`

**Files:**
- Modify: `internal/tui/repo_popup.go` (`repoPopup` gets `mode`/`hscroll`; `renderRepoPopup` builds rows via `renderWindow`; `updateRepoPopupKey` handles `w`/`shift+left/right`)
- Test: `internal/tui/repo_popup_test.go`

**Context:** Today `renderRepoPopup` lets `modalStyle.Width(inner).Render` wrap long paths. We render the entry rows through `renderWindow` (cutoff default => single-line rows, no ugly wrap) inside a fixed-width box. The header/hint stay as plain lines.

- [ ] **Step 1: Write the failing test**

```go
func TestRepoPopupDoesNotWrapLongPath(t *testing.T) {
	m := Model{width: 80, height: 24}
	long := "/very/deeply/nested/path/that/is/way/longer/than/the/popup/box/repo"
	m.repoPopup = &repoPopup{
		entries: []repos.Entry{{Path: long, LastOpened: time.Now()}},
		now:     time.Now(),
	}
	out := m.renderRepoPopup()
	// Each rendered line must fit the popup inner width: no entry spills onto a
	// second wrapped line. Count body lines that contain a path separator run.
	for _, line := range strings.Split(out, "\n") {
		if ansi.StringWidth(line) > m.width {
			t.Errorf("popup line exceeds width: %q", ansi.Strip(line))
		}
	}
	// The single entry occupies exactly one display row (truncated, not wrapped).
	bodyRows := 0
	for _, line := range strings.Split(ansi.Strip(out), "\n") {
		if strings.Contains(line, "…") || strings.Contains(line, "repo") {
			bodyRows++
		}
	}
	if bodyRows > 1 {
		t.Errorf("entry wrapped onto %d rows, want 1", bodyRows)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui/ -run TestRepoPopupDoesNotWrapLongPath`
Expected: FAIL — the long path wraps onto multiple lines.

- [ ] **Step 3: Write minimal implementation**

Add `mode dispMode` and `hscroll int` to `repoPopup`. Rewrite `renderRepoPopup` so the entry rows go through `renderWindow` at a fixed inner width:

```go
func (m Model) renderRepoPopup() string {
	p := m.repoPopup
	w, _ := m.overlayDims()
	inner := popupInnerWidth(w)

	header := "Switch repository"
	if p.query != "" {
		header += "  /" + p.query
	}

	vis := m.popupVisible()
	var bodyLines []string
	if len(vis) == 0 {
		bodyLines = []string{padRight("  (no match)", inner)}
	} else {
		wr := make([]winRow, len(vis))
		for i, e := range vis {
			marker := "  "
			if samePathTUI(e.Path, m.currentWorktree) {
				marker = "● "
			}
			prefix := "  "
			style := lipgloss.Style{}
			if i == p.sel {
				prefix = "> "
				style = selectedRow
			}
			row := prefix + marker + repos.Name(e) + "  " + e.Path + "  (" + ageString(p.now, e.LastOpened) + ")"
			wr[i] = winRow{text: row, style: style}
		}
		// Cap the visible body to a reasonable height; renderWindow scrolls to sel.
		h := len(vis)
		if h > 12 {
			h = 12
		}
		bodyLines = renderWindow(wr, winOpts{w: inner, h: h, mode: p.mode, anchor: p.sel, hscroll: p.hscroll})
	}

	hint := "[enter] switch  [ctrl+d] forget  [w] wrap  [esc] cancel"
	parts := []string{header, ""}
	parts = append(parts, bodyLines...)
	parts = append(parts, "", hint)
	return modalStyle.Width(inner).Render(strings.Join(parts, "\n")) + "\n"
}
```

Add `lipgloss` import to repo_popup.go if missing. In `updateRepoPopupKey`, add (before the `tea.KeyRunes` catch-all, since `w` must cycle the mode rather than type into the query):

```go
	case tea.KeyRunes:
		if string(msg.Runes) == "w" {
			p.mode = p.mode.next()
			p.hscroll = 0
			return m, nil
		}
		p.query += string(msg.Runes)
		p.sel = 0
		return m, nil
```

> Design note: the repo popup has a typing query, so `w` is overloaded. Here `w` is taken as the mode key (the picker filters by `/`-prefixed substring already; losing literal `w` in the query is acceptable, matching how the diff/panel surfaces treat `w`). If a future requirement needs literal `w` in a query, switch the mode key to a non-letter in popups.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tui/ -run 'TestRepoPopup'`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/repo_popup.go internal/tui/repo_popup_test.go
git commit -m "fix(tui): repo switcher renders through window primitive (no more wrapping)"
```

---

### Task 10: Help + footer + docs

**Files:**
- Modify: `internal/tui/help.go` (add `w` display-mode row; update worktree row to `W`)
- Modify: `internal/tui/footer.go` (advertise `w`; the panel footer's worktree hint → `W`)
- Modify: `CHANGELOG.md`, `README.md`
- Test: `internal/tui/help_test.go` (the help↔footer coverage gate), `internal/tui/footer_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/tui/help_test.go`:

```go
func TestHelpAdvertisesDisplayMode(t *testing.T) {
	// help is built as []contentLine by helpContent(); join the text to search.
	var b strings.Builder
	for _, l := range helpContent() {
		b.WriteString(l.text)
		b.WriteString("\n")
	}
	h := b.String()
	if !strings.Contains(h, "display mode") {
		t.Errorf("help does not document the display-mode cycle key")
	}
}
```

The existing `TestHelpFooterCoverage` gate enforces that every footer key also
appears in help, so adding `w`/`W` to both surfaces (Step 3) is what makes that
gate stay green; this added test only checks the human-readable description is
present.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui/ -run 'TestHelp'`
Expected: FAIL — neither key advertised; also the existing `TestHelpFooterCoverage` may fail until both surfaces list `w`/`W`.

- [ ] **Step 3: Write minimal implementation**

- In `help.go`, add a row: `w  cycle text display mode (cutoff / wrap / scroll)`; change the worktree row's key from `w` to `W`.
- In `footer.go`, add `w wrap` to the context footer (keep it tight — footer truncates to width); change the Branches-panel worktree hint key from `w` to `W`.
- `CHANGELOG.md` — under a new Unreleased "Added"/"Changed": "TUI windows now support three text display modes (cutoff / wrap / scroll) cycled with `w`; horizontal scroll via `shift+←/→`. Worktree-create on the Branches panel moves from `w` to `W`."
- `README.md` — update the keybinding list: add `w` (display mode) and `shift+←/→`; change worktree to `W`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tui/ -run 'TestHelp|TestFooter'`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/help.go internal/tui/footer.go internal/tui/help_test.go internal/tui/footer_test.go CHANGELOG.md README.md
git commit -m "docs(tui): advertise w display-mode + W worktree remap in help, footer, changelog, readme"
```

---

### Task 11: Full-suite verification

**Files:** none (verification only)

- [ ] **Step 1: Run the staged suite with the race detector**

Run: `./test.sh race`
Expected: vet+gofmt clean, all unit tests pass, e2e passes. In particular the pre-existing panel/stash/files-view/history/blame tests must be green (cutoff default reproduces prior output).

- [ ] **Step 2: Manual smoke (optional, document result)**

Build and open the TUI on `/mnt/t/others/test-1`; confirm: `w` cycles modes on each focused panel; `W` creates a worktree from a branch; the repo switcher (`R`) no longer wraps a long path; `shift+←/→` pans in scroll mode.

Run: `go build ./cmd/gg`
Expected: builds clean.

- [ ] **Step 3: Commit (only if Step 1/2 required a fix)** — otherwise nothing to commit.

---

## Self-review notes (for the executor)

- **Spec coverage:** This plan covers the primitive (spec §"The window primitive"), the `w` key + `W` remap (§"Display-mode key"), and the grid/surface consumers (panels, stash, files-tree, history, blame) + the repo popup. **Deferred to Plan 1b:** the remaining popups (settings/branch/conflict/content/pairop/stash-action) and the generalized truncated-row *reveal* inside popups (Task 9 fixes wrapping but does not add a reveal strip). Tabs = Stage 2; Files/Staged = Stage 3.
- **Type consistency:** `dispMode`/`winRow`/`winOpts`/`renderWindow`/`hslice`/`rowTruncated` are defined once in Task 1 and consumed unchanged thereafter. `renderListBox`'s signature gains `(mode dispMode, hscroll int)` in Task 6 — its only caller is `renderStashList`, updated in the same task.
- **Confirmed — `markedDisplayIndices` keying:** verified keyed by absolute
  display-row index (`mark.go:101` — `out[md]` from `markDisplayIndex`, and the
  Status `out[n]` loop over display order). Task 5's lookup by absolute `i` is
  correct.
- **Confirmed — `shift+left/right` free:** `grep -rn '"shift+left"\|"shift+right"
  ' internal/tui` returns nothing, so the hscroll binding does not collide.
- **Confirmed — `Model` construction:** `New(svc)` dereferences `svc`
  (`svc.CommitFeed()`), so tests build `Model{...}` literals and must initialize
  the maps they touch (`sel`, `dispModes`, `hscroll`).
- **Watch — `New` map init:** Task 4 adds `dispModes`/`hscroll` to `New`; any
  production path that constructs `Model` without `New` and then cycles a mode
  must also init the maps (the panel render reads them with the comma-ok zero
  value, so reads are safe; only writes via `w` need a non-nil map — the `w`
  handler runs only after `New`).
