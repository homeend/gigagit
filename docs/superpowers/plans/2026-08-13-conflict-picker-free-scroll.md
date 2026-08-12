# Hunk-Picker Free View-Scroll Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let the user scroll the hunk-picker viewport (conflict resolve / hunk staging) with Alt+↑/↓ without moving the selection cursor; the first plain ↑/↓ snaps the view back to the cursor and is consumed.

**Architecture:** The free scroll is a display-line *delta from the anchored window start* (`vshift`), applied and clamped inside `renderTwoCol` (the only place wrap-aware display lines exist). `renderTwoCol` returns the effective (clamped) shift and the picker's `render` (pointer receiver) stores it back, so the stored value never drifts past the content. Key handling lives entirely in `hunkPicker.update`.

**Tech Stack:** Go 1.26, Bubble Tea, lipgloss. Tests: plain `go test` in `internal/tui` (no git needed for these surfaces).

**Spec:** `docs/superpowers/specs/2026-08-13-conflict-picker-free-scroll-design.md`

## Global Constraints

- Work happens in the worktree `/mnt/t/others/gigagit/.claude/worktrees/conflict-picker-free-scroll` on branch `feat/conflict-picker-free-scroll`. **Every** build/test/edit command runs there — prefix Bash commands with `cd /mnt/t/others/gigagit/.claude/worktrees/conflict-picker-free-scroll &&` and give Write/Edit the worktree's absolute paths.
- Every user-visible TUI string goes through `i18n.T` with a **literal** key present in ALL FOUR bundles (`internal/i18n/lang/{ja,ko,zh,ru}.toml`); AST-gate tests in `internal/tui` fail otherwise.
- One line per Alt+↑/↓ press; the first plain ↑/↓ after a free scroll only snaps back (consumed); every other cursor/pick/mode key resets the scroll and then acts normally; `enter`/`esc` semantics unchanged (they may reset the scroll — harmless).
- Both picker flavors (conflict resolve and `H` hunk staging) share this surface; no flavor-specific code.
- TDD: write the failing test first, see it fail, implement, see it pass, commit.

---

### Task 1: `renderTwoCol` vertical shift + effective-shift return

**Files:**
- Modify: `internal/tui/twocol.go` (`twoColOpts`, `renderTwoCol`)
- Test: `internal/tui/twocol_test.go`

**Interfaces:**
- Produces: `twoColOpts.vshift int` (display-line delta from the anchored start) and the new signature `renderTwoCol(rows []colRow, o twoColOpts) ([]string, int)` — the `int` is the *effective* shift after clamping (`0` when `vshift == 0` or content fits the height). Task 2 relies on exactly this signature.

- [ ] **Step 1: Write the failing test**

Append to `internal/tui/twocol_test.go`:

```go
func TestTwoColVShiftSlidesAndClamps(t *testing.T) {
	var rows []colRow
	for i := 0; i < 20; i++ {
		rows = append(rows, colRow{full: &winCell{body: string(rune('A' + i))}})
	}
	o := twoColOpts{w: 20, h: 5, sep: " | ", mode: modeCutoff, anchor: 0}

	o.vshift = 3
	out, eff := renderTwoCol(rows, o)
	joined := plain(strings.Join(out, "\n"))
	if eff != 3 {
		t.Fatalf("eff = %d, want 3", eff)
	}
	if strings.Contains(joined, "A") || !strings.Contains(joined, "D") || !strings.Contains(joined, "H") {
		t.Fatalf("vshift 3 should show D..H: %q", joined)
	}

	o.vshift = 999
	_, eff = renderTwoCol(rows, o)
	if eff != 15 { // clamped to the last page: start 15 of len 20, h 5
		t.Fatalf("eff = %d, want 15", eff)
	}

	o.anchor, o.vshift = 18, -999
	out, eff = renderTwoCol(rows, o)
	if eff != -15 { // anchored start 15 (windowStart(20,5,18)), clamped to 0
		t.Fatalf("eff = %d, want -15", eff)
	}
	if !strings.Contains(plain(out[0]), "A") {
		t.Fatalf("clamped to top must show A: %q", plain(out[0]))
	}

	o.anchor, o.vshift = 0, 7
	_, eff = renderTwoCol(rows[:3], o)
	if eff != 0 { // content shorter than h: nothing to shift
		t.Fatalf("short content: eff = %d, want 0", eff)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails to compile**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/conflict-picker-free-scroll && go test ./internal/tui/ -run TestTwoColVShift -v`
Expected: compile error — `twoColOpts` has no field `vshift`, `renderTwoCol` returns 1 value.

- [ ] **Step 3: Implement**

In `internal/tui/twocol.go`:

1. Add the field (keep the existing comment style):

```go
// twoColOpts configures renderTwoCol. anchor is the colRow index kept visible.
// vshift slides the anchored window by that many display lines (free
// view-scroll); the render clamps it to the content and reports the
// effective shift back so callers can store the clamped value.
type twoColOpts struct {
	w, h    int
	sep     string
	mode    dispMode
	hscroll int
	anchor  int
	vshift  int
}
```

2. Change the signature and apply the shift after the existing `start := windowStart(len(dl), h, anchorLine)` line:

```go
func renderTwoCol(rows []colRow, o twoColOpts) ([]string, int) {
```

```go
	start := windowStart(len(dl), h, anchorLine)
	eff := 0
	if o.vshift != 0 {
		maxStart := len(dl) - h
		if maxStart < 0 {
			maxStart = 0
		}
		s := start + o.vshift
		if s > maxStart {
			s = maxStart
		}
		if s < 0 {
			s = 0
		}
		eff = s - start
		start = s
	}
```

and return `out, eff` at the end.

3. Fix the one production caller so the package compiles — in `internal/tui/conflict_picker.go` (`render`), change

```go
	body := renderTwoCol(rows, twoColOpts{
		w: w, h: bodyH, sep: pickerColSep, mode: e.mode, hscroll: e.hscroll, anchor: anchor,
	})
```

to

```go
	body, _ := renderTwoCol(rows, twoColOpts{
		w: w, h: bodyH, sep: pickerColSep, mode: e.mode, hscroll: e.hscroll, anchor: anchor,
	})
```

(Task 2 replaces the `_` with the store-back; this task only keeps the build green.)

4. Update the four existing test call sites in `twocol_test.go` (`TestTwoColCutoffTruncates`, `TestTwoColScrollReveals`, `TestTwoColWrapAlignsPairsAndGutterOnlyFirst`, `TestTwoColVerticalWindowKeepsAnchor`): each `out := renderTwoCol(...)` / `out = renderTwoCol(...)` becomes `out, _ := renderTwoCol(...)` / `out, _ = renderTwoCol(...)`.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/conflict-picker-free-scroll && go test ./internal/tui/ -run TestTwoCol -v`
Expected: all `TestTwoCol*` PASS (4 old + 1 new).

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/conflict-picker-free-scroll
git add internal/tui/twocol.go internal/tui/twocol_test.go internal/tui/conflict_picker.go
git commit -m "feat(tui): renderTwoCol vertical shift with clamped-shift feedback"
```

---

### Task 2: Picker key handling — Alt+↑/↓ scroll, snap-back, resets

**Files:**
- Modify: `internal/tui/conflict_picker.go` (`hunkPicker` struct, `update`, `render`)
- Test: `internal/tui/conflict_picker_test.go`

**Interfaces:**
- Consumes: `twoColOpts.vshift` + `renderTwoCol(...) ([]string, int)` from Task 1.
- Produces: `hunkPicker.vshift int` field (Task 3's hint row documents the behavior; no other task touches it).

- [ ] **Step 1: Write the failing tests**

Append to `internal/tui/conflict_picker_test.go`. The file currently imports only `strings`, `testing`, and `hunkpick` — add `tea "github.com/charmbracelet/bubbletea"` to its import block (the `key(...)`/`keyMsg(...)` helpers already live elsewhere in the package; `tea.KeyMsg{Type: tea.KeyDown, Alt: true}.String()` is `"alt+down"`).

```go
func TestConflictPickerAltScrollMovesViewNotCursor(t *testing.T) {
	e := newConflictPicker("f.txt", pickerDoc())
	m := Model{layers: &layerStack{entries: []layer{e}}, width: 80, height: 24}
	m, _ = e.update(m, tea.KeyMsg{Type: tea.KeyDown, Alt: true})
	m, _ = e.update(m, tea.KeyMsg{Type: tea.KeyDown, Alt: true})
	m, _ = e.update(m, tea.KeyMsg{Type: tea.KeyUp, Alt: true})
	if e.vshift != 1 {
		t.Fatalf("vshift = %d, want 1", e.vshift)
	}
	if e.bi != 0 || e.line != 0 || e.side != hunkpick.Current {
		t.Fatal("alt+scroll must not move the cursor")
	}
	if e.doc.Blocks()[0].Mode != hunkpick.Undecided {
		t.Fatal("alt+scroll must not touch picks")
	}
}

func TestConflictPickerPlainArrowSnapsBackConsumed(t *testing.T) {
	e := newConflictPicker("f.txt", pickerDoc())
	m := Model{layers: &layerStack{entries: []layer{e}}, width: 80, height: 24}
	m, _ = e.update(m, tea.KeyMsg{Type: tea.KeyDown, Alt: true})
	m, _ = e.update(m, keyMsg("down"))
	if e.vshift != 0 {
		t.Fatalf("plain ↓ must reset vshift, got %d", e.vshift)
	}
	if e.bi != 0 || e.line != 0 {
		t.Fatal("first plain ↓ after a free scroll must not move the cursor")
	}
	// region 0's current side has one line, so the next ↓ steps to block 1.
	m, _ = e.update(m, keyMsg("down"))
	if e.bi != 1 || e.line != 0 {
		t.Fatalf("second ↓ must move the cursor: bi=%d line=%d", e.bi, e.line)
	}
}

func TestConflictPickerOtherKeysResetViewScroll(t *testing.T) {
	e := newConflictPicker("f.txt", pickerDoc())
	m := Model{layers: &layerStack{entries: []layer{e}}, width: 80, height: 24}
	m, _ = e.update(m, tea.KeyMsg{Type: tea.KeyDown, Alt: true})
	m, _ = e.update(m, key("c")) // pick key resets AND acts
	if e.vshift != 0 {
		t.Fatalf("c must reset vshift, got %d", e.vshift)
	}
	if e.doc.Blocks()[0].Mode != hunkpick.TakeCurrent {
		t.Fatal("c must still take current")
	}
	m, _ = e.update(m, tea.KeyMsg{Type: tea.KeyDown, Alt: true})
	m, _ = e.update(m, key("n"))
	if e.vshift != 0 || e.bi != 1 {
		t.Fatalf("n must reset vshift and move block: vshift=%d bi=%d", e.vshift, e.bi)
	}
}

func TestConflictPickerRenderStoresClampedShift(t *testing.T) {
	e := newConflictPicker("f.txt", pickerDoc())
	m := Model{layers: &layerStack{entries: []layer{e}}, width: 80, height: 24}
	// The doc's display lines fit the 24-row overlay, so any shift clamps to 0.
	e.vshift = 9999
	_ = e.render(m, "")
	if e.vshift != 0 {
		t.Fatalf("render must store the clamped shift back, got %d", e.vshift)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/conflict-picker-free-scroll && go test ./internal/tui/ -run TestConflictPicker -v`
Expected: compile error — `hunkPicker` has no field `vshift`.

- [ ] **Step 3: Implement**

In `internal/tui/conflict_picker.go`:

1. Add the field to `hunkPicker` (after `hscroll int`):

```go
	vshift  int // free view-scroll: display-line delta from the cursor-anchored window
```

2. In `update`, insert between the existing `tea.KeyCtrlC` check and the `b := e.cur()` line:

```go
	switch msg.String() {
	case "alt+up":
		e.vshift--
		return m, nil
	case "alt+down":
		e.vshift++
		return m, nil
	}
	if e.vshift != 0 {
		// Any other key returns the viewport to the cursor first; a bare
		// up/down is consumed by that snap-back so the cursor stays put.
		e.vshift = 0
		switch msg.String() {
		case "up", "k", "down", "j":
			return m, nil
		}
	}
```

3. In `render`, pass the shift through and store the clamped value back (replacing Task 1's `body, _ :=` stopgap):

```go
	body, eff := renderTwoCol(rows, twoColOpts{
		w: w, h: bodyH, sep: pickerColSep, mode: e.mode, hscroll: e.hscroll, anchor: anchor, vshift: e.vshift,
	})
	e.vshift = eff
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/conflict-picker-free-scroll && go test ./internal/tui/ -run 'TestConflictPicker|TestTwoCol' -v`
Expected: all PASS (existing picker tests must stay green too).

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/conflict-picker-free-scroll
git add internal/tui/conflict_picker.go internal/tui/conflict_picker_test.go
git commit -m "feat(tui): free view-scroll in the hunk picker (alt+up/down, snap-back on plain arrows)"
```

---

### Task 3: Advertise it — picker hint row, help window, four bundles

**Files:**
- Modify: `internal/tui/conflict_picker.go` (the `wrapParts` hint list in `render`)
- Modify: `internal/tui/help.go` (the "Hunk picker (x conflict resolve / H stage)" section, around line 129)
- Modify: `internal/i18n/lang/ja.toml`, `internal/i18n/lang/ko.toml`, `internal/i18n/lang/zh.toml`, `internal/i18n/lang/ru.toml`

**Interfaces:**
- Consumes: the behavior implemented in Task 2 (documentation only; no code contract).
- Produces: two new i18n keys (exact strings below) present in all four bundles.

- [ ] **Step 1: Add the hint-row entry**

In `internal/tui/conflict_picker.go`'s `render`, in the `wrapParts([]string{...})` list, insert after `i18n.T("[↑/↓] line"),`:

```go
		i18n.T("[alt+↑/↓] view"),
```

- [ ] **Step 2: Add the help-window row**

In `internal/tui/help.go`, in the hunk-picker section, insert after the `r("↑/k ↓/j", i18n.T("move the line cursor (steps across regions)")),` row:

```go
		r("alt+↑/↓", i18n.T("scroll the view without moving the line cursor (e.g. to inspect the result: preview); the first plain ↑/↓ snaps the view back to the cursor")),
```

- [ ] **Step 3: Run the i18n gate to see the missing-key failure**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/conflict-picker-free-scroll && go test ./internal/tui/ -run TestI18n -v`
Expected: FAIL naming the two new keys as missing from ja/ko/zh/ru (this is the AST gate proving the keys are literal and tracked).

- [ ] **Step 4: Add the keys to all four bundles**

In each bundle, place the entries next to the existing picker-hint keys (search for `"[shift+←/→] scroll"` — e.g. `ja.toml:314`) and next to the neighboring help-row prose respectively, keeping the file's `"english" = "translation"` format:

`internal/i18n/lang/ja.toml`:

```toml
"[alt+↑/↓] view" = "[alt+↑/↓] 表示"
"scroll the view without moving the line cursor (e.g. to inspect the result: preview); the first plain ↑/↓ snaps the view back to the cursor" = "行カーソルを動かさずに表示だけをスクロール（result: プレビューの確認などに）。修飾なしの ↑/↓ を一度押すと表示がカーソル位置に戻る"
```

`internal/i18n/lang/ko.toml`:

```toml
"[alt+↑/↓] view" = "[alt+↑/↓] 보기"
"scroll the view without moving the line cursor (e.g. to inspect the result: preview); the first plain ↑/↓ snaps the view back to the cursor" = "줄 커서를 움직이지 않고 화면만 스크롤 (result: 미리보기 확인 등). 일반 ↑/↓ 를 한 번 누르면 화면이 커서 위치로 돌아감"
```

`internal/i18n/lang/zh.toml`:

```toml
"[alt+↑/↓] view" = "[alt+↑/↓] 视图"
"scroll the view without moving the line cursor (e.g. to inspect the result: preview); the first plain ↑/↓ snaps the view back to the cursor" = "滚动视图而不移动行光标（例如查看 result: 预览）；按一次普通 ↑/↓ 视图即回到光标处"
```

`internal/i18n/lang/ru.toml`:

```toml
"[alt+↑/↓] view" = "[alt+↑/↓] вид"
"scroll the view without moving the line cursor (e.g. to inspect the result: preview); the first plain ↑/↓ snaps the view back to the cursor" = "прокрутка вида без перемещения курсора строки (например, чтобы увидеть предпросмотр result:); первое нажатие обычных ↑/↓ возвращает вид к курсору"
```

- [ ] **Step 5: Run the full tui + i18n test packages**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/conflict-picker-free-scroll && go test ./internal/tui/ ./internal/i18n/`
Expected: PASS (AST gates satisfied, no other regressions).

- [ ] **Step 6: Commit**

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/conflict-picker-free-scroll
git add internal/tui/conflict_picker.go internal/tui/help.go internal/i18n/lang/ja.toml internal/i18n/lang/ko.toml internal/i18n/lang/zh.toml internal/i18n/lang/ru.toml
git commit -m "feat(tui): advertise the picker view-scroll in hints, help, and all bundles"
```

---

### Task 4: Docs + full verification

**Files:**
- Modify: `CHANGELOG.md` (top of `## [Unreleased]`)
- Modify: `README.md` (the `H` staging-picker row ~line 75 and the `x` conflict row ~line 85)

**Interfaces:**
- Consumes: the shipped behavior from Tasks 1–3. Nothing downstream.

- [ ] **Step 1: CHANGELOG entry**

Add as the FIRST bullet under `## [Unreleased]` in `CHANGELOG.md`:

```markdown
- **TUI: free view-scroll in the hunk picker.** In the conflict resolver
  (`x` → enter) and the hunk-staging picker (`H`), the viewport followed
  the line cursor and nothing else — with many differences on screen there
  was no way to look at the line-by-line `result:` preview without moving
  the selection. `alt+↑/↓` now scrolls the view freely without touching
  the cursor or the picks; the first plain `↑/↓` afterwards snaps the view
  back to the selected line (that keypress is consumed), and any pick/nav
  key re-anchors and acts as usual. The scroll is a clamped delta from the
  cursor-anchored window, so it survives wrap-mode toggles and never
  drifts past the content.
```

- [ ] **Step 2: README rows**

In the `H` row (~line 75), extend the closing sentence "the picker scrolls vertically to keep the cursor in view" to:

```
the picker scrolls vertically to keep the cursor in view, and `alt+↑/↓` scrolls the view freely without moving the cursor (the first plain `↑`/`↓` snaps back)
```

In the `x` row (~line 85), after "`shift+←/→` pans long lines (the action hint wraps across lines so no command is cut off)", append:

```
; `alt+↑/↓` scrolls the view without moving the line cursor — e.g. to inspect the `result:` preview — and the first plain `↑`/`↓` snaps the view back to the selection
```

- [ ] **Step 3: Full staged test run**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/conflict-picker-free-scroll && ./test.sh`
Expected: vet+gofmt clean, unit tests pass, e2e pass. (The race gate `./test.sh race` runs at merge time on a quiet machine, per project convention — not part of this task.)

- [ ] **Step 4: Commit**

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/conflict-picker-free-scroll
git add CHANGELOG.md README.md
git commit -m "docs(tui): changelog + README for the hunk-picker free view-scroll"
```

---

## Out of scope (from the spec)

- Page-size jumps (Alt+PgUp/PgDn), mouse-wheel view-scroll, other pickers/diff view, persistence of the scroll position.
- No `internal/agentskill` update: the CLI surface did not change.
- Merging into `main` is the human's call (ask first; `--no-ff` with the project's merge-message convention).
