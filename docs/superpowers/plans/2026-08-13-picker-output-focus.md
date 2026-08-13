# Picker Tab Focus + Scrollable Output Pane Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Tab toggles focus between the picker's grid and its output pane; a focused output scrolls with plain ↑/↓ end to end, selection keys stay inert until Tab returns to the grid, and Tab-back resumes cursor-following.

**Architecture:** Two new `hunkPicker` fields — `outFocused bool` and `oshift int` — where `oshift` mirrors the proven `vshift` pattern: a display-line delta from the follow-anchor window start, applied and clamped inside `renderOutput` with the effective value stored back (render has a pointer receiver). Key routing is a pre-block in `update` ahead of the existing alt/vshift handling; rendering swaps the hint row and highlights the pane rule by focus.

**Tech Stack:** Go 1.26, Bubble Tea, lipgloss. Plain `go test ./internal/tui/`.

**Spec:** `docs/superpowers/specs/2026-08-13-picker-output-focus-design.md`

## Global Constraints

- Work happens in the worktree `/mnt/t/others/gigagit/.claude/worktrees/picker-output-focus` on branch `feat/picker-output-focus`. Prefix every Bash command with `cd /mnt/t/others/gigagit/.claude/worktrees/picker-output-focus && ` and give Write/Edit that worktree's absolute paths.
- Under **output focus**: `up`/`k`/`down`/`j` scroll the pane; `o` collapses + unfocuses + resets; `esc`, `enter`, `z`, `shift+left`, `shift+right`, `alt+up`, `alt+down` keep their existing meanings; **every other key is consumed** (inert). Under **grid focus** everything behaves exactly as today.
- Focus leaving the output (Tab back, or `o` collapse) always resets `oshift = 0` (follow resumes).
- Every user-visible TUI string goes through `i18n.T` with a **literal** key present in ALL FOUR bundles (`internal/i18n/lang/{ja,ko,zh,ru}.toml`); each task that adds keys updates the bundles in the same task. Translation values given here are final — do not improve them.
- `gofmt -l internal/` must print nothing before every commit.
- TDD: failing test → see it fail → implement → see it pass → commit, per task.

---

### Task 1: Focus state + key routing

**Files:**
- Modify: `internal/tui/conflict_picker.go` (`hunkPicker` struct, `update`)
- Test: `internal/tui/conflict_picker_test.go`

**Interfaces:**
- Consumes: the existing `update` layout — ctrl+c check, then the alt/vshift pre-switch, then the main switch (do not reorder those).
- Produces: `hunkPicker.outFocused bool` and `hunkPicker.oshift int` — Task 2 renders from exactly these names.

- [ ] **Step 1: Write the failing tests**

Append to `internal/tui/conflict_picker_test.go` (the file already imports `tea`, `strings`, `fmt`, `hunkpick`; `keyMsg("tab")` maps to `tea.KeyTab`):

```go
func TestPickerTabTogglesOutputFocus(t *testing.T) {
	e := newConflictPicker("f.txt", pickerDoc())
	m := Model{layers: &layerStack{entries: []layer{e}}, width: 80, height: 30}
	m, _ = e.update(m, keyMsg("tab"))
	if !e.outFocused {
		t.Fatal("tab must focus the output")
	}
	m, _ = e.update(m, keyMsg("down"))
	m, _ = e.update(m, keyMsg("down"))
	m, _ = e.update(m, keyMsg("up"))
	if e.oshift != 1 || e.bi != 0 || e.line != 0 {
		t.Fatalf("output arrows must scroll the pane only: oshift=%d bi=%d line=%d", e.oshift, e.bi, e.line)
	}
	m, _ = e.update(m, tea.KeyMsg{Type: tea.KeyDown, Alt: true})
	if e.vshift != 1 {
		t.Fatal("alt+↓ must keep free-scrolling the grid under output focus")
	}
	m, _ = e.update(m, keyMsg("tab"))
	if e.outFocused || e.oshift != 0 {
		t.Fatalf("tab back must return to the grid and resume follow: focused=%v oshift=%d", e.outFocused, e.oshift)
	}
}

func TestPickerOutputFocusInertSelectionKeys(t *testing.T) {
	e := newConflictPicker("f.txt", pickerDoc())
	m := Model{layers: &layerStack{entries: []layer{e}}, width: 80, height: 30}
	m, _ = e.update(m, keyMsg("tab"))
	m, _ = e.update(m, key("c"))
	m, _ = e.update(m, keyMsg("space"))
	m, _ = e.update(m, key("n"))
	m, _ = e.update(m, keyMsg("right"))
	b := e.doc.Blocks()[0]
	if b.Mode != hunkpick.Undecided || e.bi != 0 || e.side != hunkpick.Current {
		t.Fatalf("selection keys must be inert under output focus: mode=%v bi=%d side=%v", b.Mode, e.bi, e.side)
	}
	m, _ = e.update(m, keyMsg("esc"))
	if m.topLayer() != nil {
		t.Fatal("esc must still cancel from output focus")
	}
}

func TestPickerTabExpandsCollapsedPane(t *testing.T) {
	e := newConflictPicker("f.txt", pickerDoc())
	m := Model{layers: &layerStack{entries: []layer{e}}, width: 80, height: 30}
	m, _ = e.update(m, key("o")) // collapse under grid focus, as today
	if !e.outCollapsed {
		t.Fatal("o must collapse")
	}
	m, _ = e.update(m, keyMsg("tab"))
	if e.outCollapsed || !e.outFocused {
		t.Fatal("tab on a collapsed pane must expand AND focus it")
	}
	m, _ = e.update(m, keyMsg("down"))
	m, _ = e.update(m, key("o")) // collapse from output focus
	if !e.outCollapsed || e.outFocused || e.oshift != 0 {
		t.Fatalf("o under output focus must collapse, unfocus, and reset: collapsed=%v focused=%v oshift=%d",
			e.outCollapsed, e.outFocused, e.oshift)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail to compile**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/picker-output-focus && go test ./internal/tui/ -run 'TestPickerTab|TestPickerOutputFocus' -v`
Expected: compile error — `outFocused`/`oshift` undefined.

- [ ] **Step 3: Implement**

In `internal/tui/conflict_picker.go`:

1. Add the fields to `hunkPicker` (after `outCollapsed bool`):

```go
	outFocused bool // tab moves the arrows to the output pane
	oshift     int  // output free-scroll: display-line delta from the follow-anchor window
```

2. In `update`, insert between the `tea.KeyCtrlC` check and the existing `switch msg.String()` alt pre-switch:

```go
	if msg.String() == "tab" {
		switch {
		case e.outCollapsed:
			e.outCollapsed, e.outFocused = false, true
		case e.outFocused:
			e.outFocused, e.oshift = false, 0
		default:
			e.outFocused = true
		}
		return m, nil
	}
	if e.outFocused {
		// The output owns the plain arrows; global keys fall through, every
		// selection key waits until tab returns focus to the grid.
		switch msg.String() {
		case "up", "k":
			e.oshift--
			return m, nil
		case "down", "j":
			e.oshift++
			return m, nil
		case "o":
			e.outCollapsed, e.outFocused, e.oshift = true, false, 0
			return m, nil
		case "esc", "enter", "z", "shift+left", "shift+right", "alt+up", "alt+down":
		default:
			return m, nil
		}
	}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/picker-output-focus && go test ./internal/tui/ -run TestPicker -v && go test ./internal/tui/ -run TestConflictPicker`
Expected: all new `TestPicker*` PASS and every pre-existing `TestConflictPicker*` stays green (grid focus is the default, so today's flows are untouched).

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/picker-output-focus
git add internal/tui/conflict_picker.go internal/tui/conflict_picker_test.go
git commit -m "feat(tui): tab focuses the picker output pane (arrows scroll, selection keys wait)"
```

---

### Task 2: Rendering — oshift window, focused rule, focus-adaptive hints, i18n

**Files:**
- Modify: `internal/tui/conflict_picker.go` (`render`, `renderOutput`, `outputRule`)
- Modify: `internal/i18n/lang/ja.toml`, `ko.toml`, `zh.toml`, `ru.toml`
- Test: `internal/tui/conflict_picker_test.go`

**Interfaces:**
- Consumes: Task 1's `outFocused`/`oshift` and key routing.
- Produces: new i18n keys `"[tab] output"`, `"[tab] grid"`, `"[↑/↓] scroll"`, `"[o] hide"` (bundle entries below). No API changes.

- [ ] **Step 1: Write the failing tests**

Append to `internal/tui/conflict_picker_test.go`:

```go
func TestPickerOutputScrollMovesPaneWindow(t *testing.T) {
	var sb strings.Builder
	for i := 0; i < 30; i++ {
		fmt.Fprintf(&sb, "line%02d\n", i)
	}
	sb.WriteString("<<<<<<< HEAD\nfoo\n=======\nbar\n>>>>>>> x\n")
	d, err := hunkpick.ParseConflict([]byte(sb.String()))
	if err != nil {
		t.Fatal(err)
	}
	e := newConflictPicker("f.txt", d)
	m := Model{layers: &layerStack{entries: []layer{e}}, width: 80, height: 30}
	// Follow mode pins the pane near the focused (EOF) region: top not visible.
	if out := plain(e.render(m, "")); strings.Contains(out, "line00") {
		t.Fatalf("follow mode should sit at the focused region, not the top:\n%s", out)
	}
	m, _ = e.update(m, keyMsg("tab"))
	for i := 0; i < 50; i++ {
		m, _ = e.update(m, keyMsg("down"))
	}
	_ = e.render(m, "")
	if e.oshift < 0 || e.oshift > 10 {
		t.Fatalf("downward scroll past the end must clamp near 0, got %d", e.oshift)
	}
	for i := 0; i < 100; i++ {
		m, _ = e.update(m, keyMsg("up"))
	}
	out := plain(e.render(m, ""))
	if !strings.Contains(out, "line00") {
		t.Fatalf("scrolled-up pane must reach the top of the result:\n%s", out)
	}
	if e.oshift <= -100 || e.oshift >= 0 {
		t.Fatalf("render must store the clamped effective shift back, got %d", e.oshift)
	}
	m, _ = e.update(m, keyMsg("tab")) // back to grid: follow resumes
	if out := plain(e.render(m, "")); strings.Contains(out, "line00") {
		t.Fatalf("tab back must resume following the cursor:\n%s", out)
	}
}

func TestPickerOutputRuleShowsFocus(t *testing.T) {
	e := newConflictPicker("f.txt", pickerDoc())
	m := Model{layers: &layerStack{entries: []layer{e}}, width: 80, height: 30}
	if strings.Contains(plain(e.render(m, "")), "▶ output") {
		t.Fatal("unfocused rule must not carry the focus marker")
	}
	m, _ = e.update(m, keyMsg("tab"))
	if !strings.Contains(plain(e.render(m, "")), "▶ output") {
		t.Fatal("focused rule must carry the focus marker")
	}
}

func TestPickerHintsSwapWithFocus(t *testing.T) {
	e := newConflictPicker("f.txt", pickerDoc())
	m := Model{layers: &layerStack{entries: []layer{e}}, width: 80, height: 30}
	out := plain(e.render(m, ""))
	if !strings.Contains(out, "[tab] output") || strings.Contains(out, "[tab] grid") {
		t.Fatalf("grid-focus hints wrong:\n%s", out)
	}
	m, _ = e.update(m, keyMsg("tab"))
	out = plain(e.render(m, ""))
	if !strings.Contains(out, "[tab] grid") || !strings.Contains(out, "[↑/↓] scroll") || strings.Contains(out, "[space] pick") {
		t.Fatalf("output-focus hints wrong:\n%s", out)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/picker-output-focus && go test ./internal/tui/ -run 'TestPickerOutput|TestPickerHints' -v`
Expected: FAIL — no oshift windowing, no marker, no `[tab]` hints yet.

- [ ] **Step 3: Implement**

In `internal/tui/conflict_picker.go`:

1. `renderOutput`: after the existing `start := windowStart(len(dl), h, anchor)` line, insert the oshift application (same shape as `renderTwoCol`'s vshift):

```go
	if e.oshift != 0 {
		maxStart := len(dl) - h
		if maxStart < 0 {
			maxStart = 0
		}
		s := start + e.oshift
		if s > maxStart {
			s = maxStart
		}
		if s < 0 {
			s = 0
		}
		e.oshift = s - start
		start = s
	}
```

2. Replace the free function `outputRule(w int)` with a method carrying the focus marker (update its one call site in `render` to `e.outputRule(w)`):

```go
// outputRule is the pane's titled separator line; the title carries the
// focus marker while the pane owns the arrows.
func (e *hunkPicker) outputRule(w int) string {
	label, style := "── "+i18n.T("output")+" ", pickerDim
	if e.outFocused {
		label, style = "── ▶ "+i18n.T("output")+" ", pickerFocus
	}
	fill := w - lipgloss.Width(label)
	if fill < 0 {
		fill = 0
	}
	return style.Render(padRight(label+strings.Repeat("─", fill), w))
}
```

3. In `render`, replace the hint construction

```go
	// The hint wraps instead of truncating so no command is ever cut off.
	hintLines := wrapParts([]string{
		i18n.T("[←/→] side"), i18n.T("[shift+←/→] scroll"), i18n.T("[z] mode"), i18n.T("[↑/↓] line"), i18n.T("[alt+↑/↓] view"), i18n.T("[space] pick"),
		"[c] " + e.leftLabel, "[i] " + e.rightLabel, i18n.T("[C/I] all"), i18n.T("[n/p] hunk"), i18n.T("[o] output"),
		i18n.T("[enter] apply"), i18n.T("[esc] cancel"),
	}, w, "  ")
```

with

```go
	// The hint wraps instead of truncating so no command is ever cut off;
	// the set follows the focus so only live keys are advertised.
	hintParts := []string{
		i18n.T("[←/→] side"), i18n.T("[shift+←/→] scroll"), i18n.T("[z] mode"), i18n.T("[↑/↓] line"), i18n.T("[alt+↑/↓] view"), i18n.T("[space] pick"),
		"[c] " + e.leftLabel, "[i] " + e.rightLabel, i18n.T("[C/I] all"), i18n.T("[n/p] hunk"), i18n.T("[o] output"), i18n.T("[tab] output"),
		i18n.T("[enter] apply"), i18n.T("[esc] cancel"),
	}
	if e.outFocused {
		hintParts = []string{
			i18n.T("[↑/↓] scroll"), i18n.T("[tab] grid"), i18n.T("[o] hide"), i18n.T("[z] mode"), i18n.T("[shift+←/→] scroll"), i18n.T("[alt+↑/↓] view"),
			i18n.T("[enter] apply"), i18n.T("[esc] cancel"),
		}
	}
	hintLines := wrapParts(hintParts, w, "  ")
```

4. Bundle entries — add next to the existing picker-hint keys (anchor: `"[o] output"`) in each file, values final:

`ja.toml`:
```toml
"[tab] output" = "[tab] 出力"
"[tab] grid" = "[tab] グリッド"
"[↑/↓] scroll" = "[↑/↓] スクロール"
"[o] hide" = "[o] 隠す"
```

`ko.toml`:
```toml
"[tab] output" = "[tab] 출력"
"[tab] grid" = "[tab] 그리드"
"[↑/↓] scroll" = "[↑/↓] 스크롤"
"[o] hide" = "[o] 숨기기"
```

`zh.toml`:
```toml
"[tab] output" = "[tab] 输出"
"[tab] grid" = "[tab] 网格"
"[↑/↓] scroll" = "[↑/↓] 滚动"
"[o] hide" = "[o] 隐藏"
```

`ru.toml`:
```toml
"[tab] output" = "[tab] результат"
"[tab] grid" = "[tab] сетка"
"[↑/↓] scroll" = "[↑/↓] прокрутка"
"[o] hide" = "[o] скрыть"
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/picker-output-focus && go test ./internal/tui/ ./internal/i18n/ && gofmt -l internal/`
Expected: PASS incl. the i18n AST gates; gofmt prints nothing.

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/picker-output-focus
git add internal/tui/conflict_picker.go internal/tui/conflict_picker_test.go internal/i18n/lang/ja.toml internal/i18n/lang/ko.toml internal/i18n/lang/zh.toml internal/i18n/lang/ru.toml
git commit -m "feat(tui): scrollable output pane with focus rule marker and adaptive hints"
```

---

### Task 3: Help, README, CHANGELOG, full suite

**Files:**
- Modify: `internal/tui/help.go` (hunk-picker section), the four bundles, `README.md`, `CHANGELOG.md`

**Interfaces:**
- Consumes: Tasks 1–2 behavior. Nothing downstream.

- [ ] **Step 1: Help rows**

In `internal/tui/help.go`'s hunk-picker section, insert after the `r("o", ...)` row:

```go
		r("tab", i18n.T("switch focus between the selection grid and the output pane — Tab on a collapsed pane expands and focuses it")),
		r("", i18n.T("while the output pane is focused: ↑/↓ scroll the result end to end; selection keys wait until Tab returns focus to the grid; esc/enter keep cancel/apply")),
```

- [ ] **Step 2: Bundles**

Add to each bundle (anchor: next to the help-row entries added by the toggles feature):

`ja.toml`:
```toml
"switch focus between the selection grid and the output pane — Tab on a collapsed pane expands and focuses it" = "選択グリッドと出力ペインのフォーカスを切り替える — 折りたたまれたペインで Tab を押すと展開してフォーカスする"
"while the output pane is focused: ↑/↓ scroll the result end to end; selection keys wait until Tab returns focus to the grid; esc/enter keep cancel/apply" = "出力ペインにフォーカスがある間: ↑/↓ で結果全体をスクロール。選択キーは Tab でグリッドに戻るまで無効。esc/enter は従来どおりキャンセル/適用"
```

`ko.toml`:
```toml
"switch focus between the selection grid and the output pane — Tab on a collapsed pane expands and focuses it" = "선택 그리드와 출력 패널 사이의 포커스를 전환 — 접힌 패널에서 Tab 을 누르면 펼쳐지며 포커스됨"
"while the output pane is focused: ↑/↓ scroll the result end to end; selection keys wait until Tab returns focus to the grid; esc/enter keep cancel/apply" = "출력 패널에 포커스가 있는 동안: ↑/↓ 로 결과 전체를 스크롤. 선택 키는 Tab 으로 그리드에 돌아갈 때까지 비활성. esc/enter 는 그대로 취소/적용"
```

`zh.toml`:
```toml
"switch focus between the selection grid and the output pane — Tab on a collapsed pane expands and focuses it" = "在选择网格与输出面板之间切换焦点 — 面板折叠时按 Tab 会展开并获得焦点"
"while the output pane is focused: ↑/↓ scroll the result end to end; selection keys wait until Tab returns focus to the grid; esc/enter keep cancel/apply" = "输出面板获得焦点时：↑/↓ 滚动整个结果；选择键在 Tab 返回网格前无效；esc/enter 仍为取消/应用"
```

`ru.toml`:
```toml
"switch focus between the selection grid and the output pane — Tab on a collapsed pane expands and focuses it" = "переключает фокус между сеткой выбора и панелью результата — Tab на свёрнутой панели разворачивает её и передаёт ей фокус"
"while the output pane is focused: ↑/↓ scroll the result end to end; selection keys wait until Tab returns focus to the grid; esc/enter keep cancel/apply" = "пока панель результата в фокусе: ↑/↓ прокручивают весь результат; клавиши выбора не действуют до возврата фокуса в сетку по Tab; esc/enter по-прежнему отмена/применение"
```

- [ ] **Step 3: README + CHANGELOG**

`README.md` — both fragments verified verbatim against the current file; each row stays one line.

`H` row: replace

```
and `alt+↑/↓` scrolls the view freely without moving the cursor (the first plain `↑`/`↓` snaps back)
```

with

```
and `alt+↑/↓` scrolls the view freely without moving the cursor (the first plain `↑`/`↓` snaps back); `tab` focuses the bottom **output** pane so `↑`/`↓` scroll the assembled result (`tab` again returns to the grid)
```

`x` row: replace

```
; `alt+↑/↓` scrolls the view without moving the line cursor, and the first plain `↑`/`↓` snaps the view back to the selection
```

with

```
; `alt+↑/↓` scrolls the view without moving the line cursor, and the first plain `↑`/`↓` snaps the view back to the selection; `tab` switches focus to the **output** pane (`▶` on its rule) where `↑`/`↓` scroll the whole result end to end — selection keys wait until `tab` returns to the grid, a collapsed pane expands on `tab`, and leaving the pane resumes cursor-following
```

`CHANGELOG.md` — first bullet under `## [Unreleased]`:

```markdown
- **TUI: the picker's output pane is now focusable and scrollable.** With
  a long assembled result only the window around the focused region was
  visible — nothing scrolled the output. `tab` now toggles focus between
  the selection grid and the output pane (a collapsed pane expands and
  takes focus in one press; the pane's rule shows `▶` while focused):
  under output focus `↑`/`↓` scroll the result end to end, the selection
  keys wait until `tab` returns to the grid, and `esc`/`enter`/`z`/
  `shift+←/→`/`alt+↑/↓` keep their meanings. Leaving the pane discards
  the manual scroll and resumes following the cursor. The hint row adapts
  to the focus so only live keys are advertised.
```

- [ ] **Step 4: Full staged test run**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/picker-output-focus && ./test.sh 2>&1 | tee /tmp/claude-1000/-mnt-t-others-gigagit/ed682707-e3c5-49ce-bd93-696f3d7932fe/scratchpad/output-focus-testsh.log; echo "EXIT=$?"`
Expected: vet+gofmt clean, unit green, e2e green, `all green`, `EXIT=0`. Paste the REAL tail of the log (stage headers + last lines + exit code) into your report — a paraphrase is not evidence. On any failure: report BLOCKED with the verbatim output; do not fix unrelated breakage.

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/picker-output-focus
git add internal/tui/help.go internal/i18n/lang/ja.toml internal/i18n/lang/ko.toml internal/i18n/lang/zh.toml internal/i18n/lang/ru.toml README.md CHANGELOG.md
git commit -m "docs(tui): help/README/CHANGELOG for the focusable output pane"
```

---

## Out of scope (from the spec)

- Page jumps, home/end, mouse focus/scroll, persistence across sessions.
- No `internal/agentskill` update (CLI surface unchanged). Merge into `main` is the human's call.
