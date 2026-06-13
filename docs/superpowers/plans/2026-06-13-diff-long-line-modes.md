# Diff view long-line modes (scroll/wrap/truncate) — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `w` cycles the diff view's long-line handling through scroll → wrap → truncate (scroll = default); scroll mode pans long lines with `←`/`→`/`0` and `‹`/`›` edge markers.

**Architecture:** A tri-state `longMode` replaces the `wrap bool`. `scroll`/`truncate` reuse the existing 1:1 display stream; `wrap` keeps the expanded stream. Scroll mode renders each line through a horizontal window (`hOffset`) via a new `scrollCell`, which delegates to `diffCell` for fitting lines so the default flip to scroll is byte-identical at rest. Pure TUI + one `[ui] hscroll_step` config entry.

**Tech Stack:** Go 1.26, Bubble Tea, lipgloss. Config via `internal/config`.

**Reference:** spec `docs/superpowers/specs/2026-06-13-diff-long-line-modes-design.md`. Build/test: `go build ./cmd/gg`, `./test.sh race`. The `adding-config-entries` skill (`.claude/skills/`) is the reference for Task 1.

**Task order:** 1 (config) → 2 (scrollCell, pure) → 3 (longMode plumbing + render + w-cycle) → 4 (pan keys + hint + help + docs).

---

### Task 1: `[ui] hscroll_step` config

**Files:**
- Modify: `internal/config/config.go`
- Test: `internal/config/config_test.go`
- Modify: `internal/tui/model.go` (the `hscrollStep()` accessor)

- [ ] **Step 1: Write the failing test**

Add to `internal/config/config_test.go`:

```go
func TestHScrollStepDefaultAndOverlay(t *testing.T) {
	if got := Defaults().UI.HScrollStep; got != 8 {
		t.Fatalf("default hscroll_step = %d, want 8", got)
	}
	dst := Defaults().UI
	overlayUI(&dst, UIConfig{HScrollStep: 12})
	if dst.HScrollStep != 12 {
		t.Fatalf("overlay set hscroll_step = %d, want 12", dst.HScrollStep)
	}
	overlayUI(&dst, UIConfig{HScrollStep: 0}) // <=0 is unset, must not clobber
	if dst.HScrollStep != 12 {
		t.Fatalf("unset (0) must not overwrite; got %d", dst.HScrollStep)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -run TestHScrollStep`
Expected: FAIL — `UIConfig` has no `HScrollStep`.

- [ ] **Step 3: Add the config field, default, and overlay**

In `internal/config/config.go`:

- `UIConfig` gains a field (after `WheelStep`):

```go
	HScrollStep int `toml:"hscroll_step"` // diff scroll-mode pan columns per ←/→; <=0 = unset
```

- `Defaults()` UI block becomes:

```go
		UI: UIConfig{WheelStep: 3, HScrollStep: 8},
```

- `overlayUI` gains (after the `WheelStep` block):

```go
	if src.HScrollStep > 0 {
		dst.HScrollStep = src.HScrollStep
	}
```

- [ ] **Step 4: Add the Model accessor**

In `internal/tui/model.go`, after `func (m Model) wheelStep() int { … }`:

```go
// hscrollStep is the diff scroll-mode horizontal pan distance (columns per
// ←/→), from [ui] hscroll_step; 8 until config loads.
func (m Model) hscrollStep() int {
	if s := m.cfg.UI.HScrollStep; s > 0 {
		return s
	}
	return 8
}
```

- [ ] **Step 5: Run tests**

Run: `go test ./internal/config/ ./internal/tui/` and `go build ./cmd/gg`
Expected: PASS; build clean.

- [ ] **Step 6: Commit**

```bash
git add internal/config/ internal/tui/model.go
git commit -m "feat(config): [ui] hscroll_step for diff horizontal scroll

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 2: `scrollCell` — the horizontal-window renderer

**Files:**
- Modify: `internal/tui/diff_render.go` (add `scrollCell` + `maxCellWidth`)
- Test: `internal/tui/diff_render_test.go`

- [ ] **Step 1: Write the failing tests**

Add to `internal/tui/diff_render_test.go`:

```go
func TestScrollCellFitsDelegatesToDiffCell(t *testing.T) {
	// hOffset 0 and a line that fits ⇒ byte-identical to the truncate path.
	got := scrollCell(3, "hello", nil, 0, 3, 20, false, false, diffDelCell)
	want := diffCell(3, "hello", 3, 20, false, false, diffDelCell, nil)
	if got != want {
		t.Fatalf("fitting scrollCell must equal diffCell:\n got %q\nwant %q", got, want)
	}
}

func TestScrollCellWidthAlwaysExact(t *testing.T) {
	long := strings.Repeat("abcdefghij ", 8) // ~88 cols
	for _, hOff := range []int{0, 5, 40, 200} {
		cell := scrollCell(1, long, nil, hOff, 3, 20, false, false, diffDelCell)
		if w := lipgloss.Width(cell); w != 20 {
			t.Fatalf("hOffset %d: cell width %d, want 20", hOff, w)
		}
	}
}

func TestScrollCellRightMarkerWhenMore(t *testing.T) {
	long := strings.Repeat("x", 100)
	cell := ansi.Strip(scrollCell(1, long, nil, 0, 3, 20, false, false, diffDelCell))
	if !strings.Contains(cell, "›") {
		t.Fatalf("a line past the window must show ›: %q", cell)
	}
	if strings.Contains(cell, "‹") {
		t.Fatalf("at hOffset 0 there is nothing to the left: %q", cell)
	}
}

func TestScrollCellLeftMarkerWhenScrolled(t *testing.T) {
	long := strings.Repeat("x", 100)
	cell := ansi.Strip(scrollCell(1, long, nil, 30, 3, 20, false, false, diffDelCell))
	if !strings.Contains(cell, "‹") {
		t.Fatalf("scrolled right, ‹ must show on the left: %q", cell)
	}
}

func TestScrollCellGapFiller(t *testing.T) {
	cell := ansi.Strip(scrollCell(0, "", nil, 0, 3, 20, true, false, diffDelCell))
	if strings.TrimRight(cell, "·") != "" {
		t.Fatalf("gap side must be all · filler: %q", cell)
	}
}

func TestMaxCellWidthIgnoresGapSides(t *testing.T) {
	lines := []textdiff.Line{
		{Row: textdiff.Row{Kind: textdiff.Same, Left: "ab", Right: "ab"}},
		{Row: textdiff.Row{Kind: textdiff.Add, Right: "longer right side here"}}, // left is a gap
		{Fold: 4},
	}
	if got := maxCellWidth(lines); got != lipgloss.Width("longer right side here") {
		t.Fatalf("maxCellWidth = %d, want %d", got, lipgloss.Width("longer right side here"))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/ -run 'ScrollCell|MaxCellWidth'`
Expected: FAIL — `scrollCell`/`maxCellWidth` undefined.

- [ ] **Step 3: Implement `scrollCell` and `maxCellWidth`**

In `internal/tui/diff_render.go`, add after `segCell`:

```go
// scrollCell renders one pane's line through a horizontal window starting at
// hOffset display columns. At hOffset==0 with a line that fits the pane it
// delegates to diffCell (byte-identical to truncate at rest). Otherwise it
// shows the column slice, with ‹ in the first column when hOffset>0 and › in
// the last when text extends past the window. Emphasis rides in seg.emph.
func scrollCell(no int, text string, spans []textdiff.Span, hOffset, gut, width int, gap, hot bool, hotStyle lipgloss.Style) string {
	if gap {
		return diffGapCell.Render(strings.Repeat("·", width))
	}
	if gut > width-2 { // degenerate pane: keep the cell inside its width
		gut = width - 2
		if gut < 1 {
			gut = 1
		}
	}
	tw := width - gut - 1
	if tw < 1 {
		tw = 1
	}
	disp, emph := sanitizeSpans(text, spans)
	full := lipgloss.Width(string(disp))
	if hOffset <= 0 && full <= tw {
		return diffCell(no, text, gut, width, false, hot, hotStyle, spans)
	}
	hasLeft := hOffset > 0
	hasRight := full > hOffset+tw
	// Content occupies the window minus any marker columns.
	contentStart, contentEnd := hOffset, hOffset+tw
	if hasLeft {
		contentStart++
	}
	if hasRight {
		contentEnd--
	}
	var wdisp []rune
	var wemph []bool
	col := 0
	for i, r := range disp {
		rw := lipgloss.Width(string(r))
		if col >= contentStart && col+rw <= contentEnd {
			wdisp = append(wdisp, r)
			wemph = append(wemph, emph[i])
		}
		col += rw
	}
	base := lipgloss.NewStyle()
	if hot {
		base = hotStyle
	}
	var b strings.Builder
	if hasLeft {
		b.WriteString(diffGutter.Render("‹"))
	}
	b.WriteString(styledRuns(wdisp, wemph, base))
	inner := tw
	if hasLeft {
		inner--
	}
	if hasRight {
		inner--
	}
	if pad := inner - lipgloss.Width(string(wdisp)); pad > 0 {
		b.WriteString(base.Render(strings.Repeat(" ", pad)))
	}
	if hasRight {
		b.WriteString(diffGutter.Render("›"))
	}
	num := fmt.Sprintf("%*d ", gut, no)
	return diffGutter.Render(truncate(num, gut+1)) + b.String()
}

// maxCellWidth is the widest single cell (either side, gap sides skipped)
// across the logical lines — the horizontal extent scroll mode can pan to.
func maxCellWidth(lines []textdiff.Line) int {
	max := 0
	for _, ln := range lines {
		if ln.Fold > 0 {
			continue
		}
		r := ln.Row
		if r.Kind != textdiff.Add {
			if w := lipgloss.Width(sanitizeLine(r.Left)); w > max {
				max = w
			}
		}
		if r.Kind != textdiff.Del {
			if w := lipgloss.Width(sanitizeLine(r.Right)); w > max {
				max = w
			}
		}
	}
	return max
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/tui/ -run 'ScrollCell|MaxCellWidth'` then `gofmt -l internal/tui/diff_render.go`
Expected: PASS; gofmt silent.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/diff_render.go internal/tui/diff_render_test.go
git commit -m "feat(tui): scrollCell horizontal-window renderer + maxCellWidth

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 3: tri-state `longMode` — plumbing, render branch, `w` cycle

**Files:**
- Modify: `internal/tui/diff_view.go` (`longMode`, `diffView` fields, `relayout`, `clampHOffset`, loaders, `w` handler)
- Modify: `internal/tui/diff_render.go` (`diffPaneLines` branches on `long`)
- Modify: `internal/tui/model.go` (`Model.diffLong`, status loading-stub)
- Modify: `internal/tui/files_view.go` (tree loading-stub)
- Test: `internal/tui/diff_view_test.go` (migrate wrap tests; add cycle/inherit tests)

- [ ] **Step 1: Migrate the existing wrap tests and add new ones**

In `internal/tui/diff_view_test.go`:

a) Replace every `v.wrap = true` with `v.long = longWrap` (in `TestRelayoutWrapOnExpandsAndRemapsBlocks`, `TestRelayoutWrapOnGapSideHasNilSegments`, `TestDiffPaneLinesWrappedRowWidthAndCount` (this one is in `diff_render_test.go`), `TestDiffResizeReanchorsToTopLine`, `TestDiffNextBlockWhenWrappedLandsOnChangeRow`).

b) Replace `TestDiffWToggleFlipsWrapAndSession` entirely with the 3-way cycle test:

```go
func TestDiffWCyclesLongMode(t *testing.T) {
	rows := sameRowsTUI(40, 20)
	m := diffModel()
	m.width, m.height = 80, 24
	m.diffView = diffViewWith(rows, []int{20})
	m.diffView.width = 80
	m.diffView.rebuild()
	m.diffTag = "status:x"
	// default scroll → wrap → truncate → scroll
	wantSeq := []longMode{longWrap, longTruncate, longScroll}
	cur := tea.Model(m)
	for i, want := range wantSeq {
		u, _ := cur.(Model).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("w")})
		mm := u.(Model)
		if mm.diffView.long != want {
			t.Fatalf("press %d: long = %d, want %d", i+1, mm.diffView.long, want)
		}
		if mm.diffLong != want {
			t.Fatalf("press %d: session diffLong = %d, want %d", i+1, mm.diffLong, want)
		}
		cur = mm
	}
}
```

c) Replace `TestDiffOpenInheritsSessionWrap` with the long-mode inherit version:

```go
func TestDiffOpenInheritsSessionLongMode(t *testing.T) {
	dir, repo := newRepoDir(t)
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("a\nb\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", "f.txt")
	gitIn(t, dir, "commit", "-m", "base")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("a\nB\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := diffModel()
	m.svc = domain.New(repo)
	m.currentWorktree = dir
	m.diffLong = longWrap
	msg := m.loadStatusDiffCmd(model.FileStatus{Path: "f.txt", Staged: '.', Unstaged: 'M'})().(diffMsg)
	if msg.view.long != longWrap {
		t.Fatalf("a new diff must inherit the session long mode, got %d", msg.view.long)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/ -run 'DiffWCycles|InheritsSessionLong'`
Expected: FAIL — `longMode`/`longWrap`/`m.diffLong`/`v.long` undefined.

- [ ] **Step 3: Define `longMode` and update the `diffView` fields**

In `internal/tui/diff_view.go`, add the type (after the `diffContext`/`diffLead` const block):

```go
// longMode is how the diff view shows lines wider than a pane. The zero value
// (scroll) is the default. w cycles scroll → wrap → truncate → scroll.
type longMode int

const (
	longScroll   longMode = iota // horizontal pan (←/→), the default
	longWrap                     // word-wrap across display rows
	longTruncate                 // cut with a trailing …
)
```

In the `diffView` struct, replace the `wrap bool` line with:

```go
	long       longMode        // mode: how lines wider than a pane are shown (default scroll)
	hOffset    int             // scroll mode: horizontal pan column (0 = left edge)
	maxCell    int             // scroll mode: widest cell width (pan clamp); set by relayout
```

- [ ] **Step 4: Update `relayout` and add `clampHOffset`**

In `relayout`, change the 1:1 case condition `case !v.wrap || width <= 0:` to:

```go
		case v.long != longWrap || width <= 0:
```

and the wrap case is now the `default:` (unchanged body). After the block that builds `dispBlocks` and before `v.scroll(0, 1)`, insert the scroll-mode extent + clamp:

```go
	if v.long == longScroll {
		v.maxCell = maxCellWidth(v.lines)
		v.clampHOffset()
	}
```

Add the clamp helper (near `scroll`):

```go
// clampHOffset keeps the horizontal pan within [0, maxCell - tw].
func (v *diffView) clampHOffset() {
	paneW := (v.width - 1) / 2
	if paneW < 4 {
		paneW = 4
	}
	tw := paneW - gutterWidth(v.full) - 1
	if tw < 1 {
		tw = 1
	}
	max := v.maxCell - tw
	if max < 0 {
		max = 0
	}
	if v.hOffset > max {
		v.hOffset = max
	}
	if v.hOffset < 0 {
		v.hOffset = 0
	}
}
```

- [ ] **Step 5: Branch `diffPaneLines` on `long`**

In `internal/tui/diff_render.go`, replace the body of the `diffPaneLines` loop (from `r := dr.row` through the end of the loop iteration — i.e. the `if !v.wrap { … } … segCell` block) with a `switch v.long`:

```go
		r := dr.row
		switch v.long {
		case longWrap:
			leftGap := r.Kind == textdiff.Add
			rightGap := r.Kind == textdiff.Del
			leftNo, rightNo := 0, 0
			if dr.first && !leftGap {
				leftNo = r.LeftNo
			}
			if dr.first && !rightGap {
				rightNo = r.RightNo
			}
			left := segCell(leftNo, dr.left, gut, paneW, leftGap,
				r.Kind == textdiff.Del || r.Kind == textdiff.Changed, diffDelCell)
			right := segCell(rightNo, dr.right, gut, paneW, rightGap,
				r.Kind == textdiff.Add || r.Kind == textdiff.Changed, diffAddCell)
			out = append(out, left+"│"+right)
		case longTruncate:
			left := diffCell(r.LeftNo, r.Left, gut, paneW,
				r.Kind == textdiff.Add,
				r.Kind == textdiff.Del || r.Kind == textdiff.Changed, diffDelCell, r.LeftSpans)
			right := diffCell(r.RightNo, r.Right, gut, paneW,
				r.Kind == textdiff.Del,
				r.Kind == textdiff.Add || r.Kind == textdiff.Changed, diffAddCell, r.RightSpans)
			out = append(out, left+"│"+right)
		default: // longScroll
			left := scrollCell(r.LeftNo, r.Left, r.LeftSpans, v.hOffset, gut, paneW,
				r.Kind == textdiff.Add,
				r.Kind == textdiff.Del || r.Kind == textdiff.Changed, diffDelCell)
			right := scrollCell(r.RightNo, r.Right, r.RightSpans, v.hOffset, gut, paneW,
				r.Kind == textdiff.Del,
				r.Kind == textdiff.Add || r.Kind == textdiff.Changed, diffAddCell)
			out = append(out, left+"│"+right)
		}
```

- [ ] **Step 6: `Model.diffLong` + creation sites + `w` cycle**

In `internal/tui/model.go`, replace the `diffWrap bool` field line with (the
`longMode` type lives in `diff_view.go`, same package):

```go
	diffLong longMode // session: long-line mode for new diffs (0 = scroll); w cycles
```

Update the status loading-stub: replace `wrap: m.diffWrap` with `long: m.diffLong`.

In `internal/tui/files_view.go` tree loading-stub: replace `wrap:    m.diffWrap,` with `long:    m.diffLong,`.

In `internal/tui/diff_view.go`, both loaders: replace `wrap: m.diffWrap` with `long: m.diffLong` in the `v := &diffView{…}` literals (keep `width: width`).

Replace the `w` key case in `updateDiffViewKey`:

```go
	case "w":
		ord := v.currentBlockOrdinal()
		v.long = (v.long + 1) % 3
		v.hOffset = 0
		v.relayout(v.width)
		m.diffLong = v.long
		if len(v.dispBlocks) > 0 {
			if ord >= len(v.dispBlocks) {
				ord = len(v.dispBlocks) - 1
			}
			v.jumpTo(v.dispBlocks[ord], m.diffBodyRows())
		} else {
			v.offset = 0
		}
```

- [ ] **Step 7: Run tests**

Run: `go test ./internal/tui/` then `go build ./cmd/gg` and `gofmt -l internal/tui/`
Expected: PASS — the migrated wrap tests, the new cycle/inherit tests, AND every other pre-existing diff test (default scroll at hOffset 0 on fitting lines delegates to `diffCell` ⇒ byte-identical; wrap/truncate untouched). Build clean; gofmt silent.

> If a pre-existing render golden fails, a fitting line is NOT delegating to
> `diffCell` — STOP and fix `scrollCell`'s delegation, not the golden.

- [ ] **Step 8: Commit**

```bash
git add internal/tui/diff_view.go internal/tui/diff_render.go internal/tui/model.go internal/tui/files_view.go internal/tui/diff_view_test.go internal/tui/diff_render_test.go
git commit -m "feat(tui): tri-state diff long-line mode (scroll default) + w cycle

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 4: pan keys, resize re-clamp, mode-aware hint, help, docs

**Files:**
- Modify: `internal/tui/diff_view.go` (`left`/`right`/`0` key cases)
- Modify: `internal/tui/model.go` (`WindowSizeMsg` re-clamp)
- Modify: `internal/tui/diff_render.go` (mode-aware hint function)
- Modify: `internal/tui/help.go` (Diff-view rows)
- Modify: `CHANGELOG.md`, `README.md`
- Test: `internal/tui/diff_view_test.go`

- [ ] **Step 1: Write the failing tests**

Add to `internal/tui/diff_view_test.go`:

```go
func TestDiffScrollPanKeys(t *testing.T) {
	rows := make([]textdiff.Row, 5)
	wide := strings.Repeat("word ", 40) // 200 cols, far wider than any pane
	for i := range rows {
		rows[i] = textdiff.Row{Kind: textdiff.Same, Left: wide, Right: wide, LeftNo: i + 1, RightNo: i + 1}
	}
	m := diffModel()
	m.width, m.height = 80, 24
	v := diffViewWith(rows, nil) // default long == longScroll
	v.width = 80
	v.rebuild()
	m.diffView = v
	m.diffTag = "status:x"

	// → pans right by hscrollStep (default 8); 0 resets; ← clamps at 0.
	u, _ := m.Update(tea.KeyMsg{Type: tea.KeyRight})
	v1 := u.(Model).diffView
	if v1.hOffset != m.hscrollStep() {
		t.Fatalf("→ should pan by %d, got hOffset %d", m.hscrollStep(), v1.hOffset)
	}
	u2, _ := u.(Model).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("0")})
	if u2.(Model).diffView.hOffset != 0 {
		t.Fatal("0 must reset hOffset")
	}
	u3, _ := u2.(Model).Update(tea.KeyMsg{Type: tea.KeyLeft})
	if u3.(Model).diffView.hOffset != 0 {
		t.Fatal("← at column 0 must clamp to 0")
	}
}

func TestDiffPanNoOpWhenNotScroll(t *testing.T) {
	rows := sameRowsTUI(5, 2)
	m := diffModel()
	m.width, m.height = 80, 24
	v := diffViewWith(rows, []int{2})
	v.long = longWrap
	v.width = 80
	v.rebuild()
	m.diffView = v
	m.diffTag = "status:x"
	u, _ := m.Update(tea.KeyMsg{Type: tea.KeyRight})
	if u.(Model).diffView.hOffset != 0 {
		t.Fatal("→ must be a no-op outside scroll mode")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/ -run 'DiffScrollPanKeys|DiffPanNoOp'`
Expected: FAIL — pan keys unhandled (hOffset stays 0 on `→`).

- [ ] **Step 3: Add the pan key cases**

In `internal/tui/diff_view.go` `updateDiffViewKey`, add after the `w` case:

```go
	case "left":
		if v.long == longScroll {
			v.hOffset -= m.hscrollStep()
			v.clampHOffset()
		}
	case "right":
		if v.long == longScroll {
			v.hOffset += m.hscrollStep()
			v.clampHOffset()
		}
	case "0":
		if v.long == longScroll {
			v.hOffset = 0
		}
```

- [ ] **Step 4: Re-clamp `hOffset` on resize**

In `internal/tui/model.go`, in the `WindowSizeMsg` diff branch, inside the `else` (the `msg.Width >= 60` re-wrap block), after `v.relayout(w)` add a clamp (relayout already re-clamps in scroll mode, but the explicit call keeps it correct if `maxCell` was stale):

```go
				v.clampHOffset()
```

(Place it right after `v.relayout(w)` and before the `topLine`/`v.scroll` lines.)

- [ ] **Step 5: Mode-aware hint**

In `internal/tui/diff_render.go`, delete the `const diffHint = …` line and add a function:

```go
// diffHintFor builds the diff-view hint for the current long-line mode. Kept
// short enough that [esc] close survives truncation at width 100
// (TestRenderDiffViewPanes). The scroll variant appends the pan keys.
func diffHintFor(long longMode) string {
	mode := "scroll"
	switch long {
	case longWrap:
		mode = "wrap"
	case longTruncate:
		mode = "trunc"
	}
	pan := ""
	if long == longScroll {
		pan = "  [←→/0] pan"
	}
	return "[↑↓] scroll  [n/p] change  [f] partial  [w] lines:" + mode + pan + "  [h] history  [esc] close  [q] quit"
}
```

In `renderDiffView`, change `lines = append(lines, truncate(diffHint, w))` to:

```go
	lines = append(lines, truncate(diffHintFor(v.long), w))
```

- [ ] **Step 6: Help rows**

In `internal/tui/help.go`, in the Diff-view section, replace the `r("w", …)` row with two rows:

```go
		r("w", "cycle long lines: scroll / wrap / truncate"),
		r("← → 0", "scroll mode: pan left / right / reset"),
```

- [ ] **Step 7: Run tests**

Run: `go test ./internal/tui/` then `go build ./cmd/gg` and `gofmt -l internal/tui/`
Expected: PASS (incl. the pan tests and `TestRenderDiffViewPanes` — the hint still shows `[esc] close`). Build clean; gofmt silent.

- [ ] **Step 8: Commit**

```bash
git add internal/tui/diff_view.go internal/tui/model.go internal/tui/diff_render.go internal/tui/help.go internal/tui/diff_view_test.go
git commit -m "feat(tui): diff scroll-mode pan keys (←/→/0), mode-aware hint, help

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

- [ ] **Step 9: Docs**

In `CHANGELOG.md`, under `### Added` (near the other diff entries):

```markdown
#### Diff view: long-line modes (scroll / wrap / truncate)
- The diff view now opens in **horizontal-scroll** mode by default: long lines
  pan with `←`/`→` (`0` resets; `‹`/`›` mark off-screen text), step from the new
  `[ui] hscroll_step` (default 8). `w` cycles scroll → wrap → truncate; the mode
  shows in the hint and is remembered for the session.
```

In `README.md`, update the diff keymap (the `enter` row): change the `w` mention from "wraps long lines" to "cycles long-line mode (scroll/wrap/truncate)" and add "`←`/`→`/`0` pan in scroll mode". If the config section lists `[ui] wheel_step`, add `[ui] hscroll_step` (default 8) beside it.

Run: `go build ./cmd/gg` → clean.

```bash
git add CHANGELOG.md README.md
git commit -m "docs: diff long-line modes + [ui] hscroll_step (changelog, readme)

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Final verification (after all tasks)

- [ ] `./test.sh race` — all stages green.
- [ ] Manual smoke: `go build ./cmd/gg && ./gg`, open a diff with a long line — it opens in scroll mode, `→`/`←` pan with `‹`/`›` markers, `0` resets; `w` cycles to wrap, then truncate, then back; the mode shows in the hint.
- [ ] Final holistic review of the branch.

## Notes for the executor

- **No CLI surface, no new window kind** ⇒ no agentskill bump, no footer-registry/avail change (the diff view draws its own hint; `TestHelpFooterCoverage` is uninvolved). The new config entry follows the `adding-config-entries` skill.
- **Byte-identity safety net:** default is now scroll, but `scrollCell` delegates to `diffCell` for fitting lines at `hOffset 0`, so existing render goldens stay green. If one breaks, fix the delegation, not the golden.
- Keep `textdiff`/`domain`/`cache` untouched.
- Commit after every task; never commit in the shared checkout (work stays in this worktree on `feat/diff-hscroll`).
```
