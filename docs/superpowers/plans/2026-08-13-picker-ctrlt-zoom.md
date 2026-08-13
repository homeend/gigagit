# Hunk-picker ctrl+t zoom Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** ctrl+t inside the hunk picker zooms the tab-focused half (selection grid or output pane) to occupy the whole picker body; ctrl+t or esc restores the split, and Tab swaps which half is zoomed.

**Architecture:** One `zoomed bool` on `hunkPicker`. The zoomed half is never stored — render always shows the *focused* half when zoomed, so "zoom follows focus" falls out of the existing Tab handling. Keys are handled in the picker's own `update` (the central ctrl+t handler in model.go only serves `maximizableLayer` popups; full-screen surfaces fall through). The conflict *process* intercepts esc before its embedded picker, so it needs a one-line zoom guard.

**Tech Stack:** Go 1.26, Bubble Tea, existing `internal/tui` test helpers (`key`, `keyMsg`, `pickerDoc`).

**Spec:** `docs/superpowers/specs/2026-08-13-picker-ctrlt-zoom-design.md`

## Global Constraints

- Work happens in the worktree `.claude/worktrees/picker-ctrlt-zoom` on branch `feat/picker-ctrlt-zoom`. Run every build/test command with `cd /mnt/t/others/gigagit/.claude/worktrees/picker-ctrlt-zoom &&` prefixed, and Write/Edit only files under that absolute worktree path.
- TUI `Model` is a value receiver; `hunkPicker` methods are pointer receivers (state persists on the layer entry). Do not change receiver kinds.
- Every user-visible TUI string goes through `i18n.T` with a **literal** key present in ALL FOUR bundles (`internal/i18n/lang/{ja,ko,zh,ru}.toml`) — AST-gate tests fail otherwise. The hint key `"[ctrl+t] full"` already exists in all four bundles (used by other popups): reuse it, do NOT re-add it (TOML rejects duplicate keys).
- gofmt-clean (`gofmt -l internal/` must print nothing); no double blank lines left by edits.
- TDD: write the failing test, see it fail, implement, see it pass, commit.
- esc-restores-first and zoom-follows-focus are user decisions from the spec — do not "simplify" them away.

---

### Task 1: Zoom state, keys, and rendering

**Files:**
- Modify: `internal/tui/conflict_picker.go`
- Test: `internal/tui/conflict_picker_test.go`

**Interfaces:**
- Consumes: existing `hunkPicker` fields (`outFocused`, `outCollapsed`, `oshift`, `vshift`), `renderTwoCol`, `renderOutput`, `outputRule`.
- Produces: `hunkPicker.zoomed bool` field — Task 2's process guard reads/writes it; Task 3 documents the behavior.

- [ ] **Step 1: Write the failing key-handling tests**

Append to `internal/tui/conflict_picker_test.go`:

```go
func TestPickerCtrlTTogglesZoom(t *testing.T) {
	e := newConflictPicker("f.txt", pickerDoc())
	m := Model{layers: &layerStack{entries: []layer{e}}, width: 80, height: 24}
	m, _ = e.update(m, keyMsg("ctrl+t"))
	if !e.zoomed {
		t.Fatalf("ctrl+t did not zoom")
	}
	m, _ = e.update(m, keyMsg("ctrl+t"))
	if e.zoomed {
		t.Fatalf("second ctrl+t did not restore the split")
	}
}

func TestPickerEscRestoresZoomFirst(t *testing.T) {
	e := newConflictPicker("f.txt", pickerDoc())
	m := Model{layers: &layerStack{entries: []layer{e}}, width: 80, height: 24}
	m, _ = e.update(m, keyMsg("ctrl+t"))
	m, _ = e.update(m, keyMsg("esc"))
	if e.zoomed {
		t.Fatalf("esc under zoom did not restore the split")
	}
	if len(m.layers.entries) != 1 {
		t.Fatalf("esc under zoom closed the picker (layers = %d, want 1)", len(m.layers.entries))
	}
	m, _ = e.update(m, keyMsg("esc"))
	if len(m.layers.entries) != 0 {
		t.Fatalf("second esc did not close the picker")
	}
}

func TestPickerEscRestoresZoomWhileOutputFocused(t *testing.T) {
	e := newConflictPicker("f.txt", pickerDoc())
	m := Model{layers: &layerStack{entries: []layer{e}}, width: 80, height: 24}
	m, _ = e.update(m, keyMsg("tab")) // focus output
	m, _ = e.update(m, keyMsg("ctrl+t"))
	m, _ = e.update(m, keyMsg("esc"))
	if e.zoomed || len(m.layers.entries) != 1 {
		t.Fatalf("esc under output-zoom: zoomed=%v layers=%d, want false/1", e.zoomed, len(m.layers.entries))
	}
}

func TestPickerODropsZoom(t *testing.T) {
	// Grid-focused: o unzooms AND collapses the pane.
	e := newConflictPicker("f.txt", pickerDoc())
	m := Model{layers: &layerStack{entries: []layer{e}}, width: 80, height: 24}
	m, _ = e.update(m, keyMsg("ctrl+t"))
	m, _ = e.update(m, keyMsg("o"))
	if e.zoomed || !e.outCollapsed {
		t.Fatalf("grid o under zoom: zoomed=%v collapsed=%v, want false/true", e.zoomed, e.outCollapsed)
	}
	// Output-focused: o unzooms, collapses, and returns focus to the grid.
	e2 := newConflictPicker("f.txt", pickerDoc())
	m2 := Model{layers: &layerStack{entries: []layer{e2}}, width: 80, height: 24}
	m2, _ = e2.update(m2, keyMsg("tab"))
	m2, _ = e2.update(m2, keyMsg("ctrl+t"))
	m2, _ = e2.update(m2, keyMsg("o"))
	if e2.zoomed || !e2.outCollapsed || e2.outFocused {
		t.Fatalf("output o under zoom: zoomed=%v collapsed=%v focused=%v", e2.zoomed, e2.outCollapsed, e2.outFocused)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/picker-ctrlt-zoom && go test ./internal/tui/ -run 'TestPicker(CtrlT|Esc|O)' -v`
Expected: compile FAIL — `e.zoomed undefined`.

- [ ] **Step 3: Implement the field and key handling**

In `internal/tui/conflict_picker.go`, add the field after `oshift` (line ~46):

```go
	zoomed bool // ctrl+t: the tab-focused half owns the whole body; zoom follows focus
```

In `update`, insert BETWEEN the tab block (ends `return m, nil` after the tab switch, line ~189) and the `if e.outFocused {` block:

```go
	// ctrl+t zooms the tab-focused half (grid or output) to the whole body;
	// the zoomed half is not stored — render shows the focused one, so tab
	// swaps the zoom for free. esc restores the split before it can cancel.
	if msg.String() == "ctrl+t" {
		e.zoomed = !e.zoomed
		return m, nil
	}
	if e.zoomed && msg.String() == "esc" {
		e.zoomed = false
		return m, nil
	}
```

(Placement is load-bearing: both keys must be consumed before the `outFocused` inert-default and before the main switch's `esc` → `popLayer`.)

In the `outFocused` block's `"o"` case, drop the zoom too:

```go
		case "o":
			e.outCollapsed, e.outFocused, e.oshift = true, false, 0
			e.zoomed = false
			return m, nil
```

In the main switch's `"o"` case:

```go
	case "o":
		e.outCollapsed = !e.outCollapsed
		e.zoomed = false
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/picker-ctrlt-zoom && go test ./internal/tui/ -run 'TestPicker(CtrlT|Esc|O)' -v`
Expected: PASS (render is untouched so far — these tests only assert state/layers).

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/picker-ctrlt-zoom && git add internal/tui/conflict_picker.go internal/tui/conflict_picker_test.go && git commit -m "feat(tui): picker ctrl+t zoom state + key handling"
```

- [ ] **Step 6: Write the failing render tests**

Append to `internal/tui/conflict_picker_test.go`:

```go
func TestPickerZoomGridHidesOutput(t *testing.T) {
	e := newConflictPicker("f.txt", pickerDoc())
	m := Model{layers: &layerStack{entries: []layer{e}}, width: 80, height: 24}
	m, _ = e.update(m, keyMsg("ctrl+t")) // grid focused → grid-zoom
	out := e.render(m, "")
	if strings.Contains(out, "── ") {
		t.Fatalf("grid-zoom still shows the output rule:\n%s", out)
	}
	if !strings.Contains(out, "region 1/2") {
		t.Fatalf("grid-zoom lost the grid rows:\n%s", out)
	}
}

func TestPickerZoomOutputHidesGrid(t *testing.T) {
	e := newConflictPicker("f.txt", pickerDoc())
	m := Model{layers: &layerStack{entries: []layer{e}}, width: 80, height: 24}
	m, _ = e.update(m, keyMsg("tab"))    // focus output
	m, _ = e.update(m, keyMsg("ctrl+t")) // output-zoom
	out := e.render(m, "")
	if !strings.Contains(out, "── ") {
		t.Fatalf("output-zoom lost the rule:\n%s", out)
	}
	if strings.Contains(out, "region 1/2") {
		t.Fatalf("output-zoom still shows grid rows:\n%s", out)
	}
}

func TestPickerTabSwapsZoomedHalf(t *testing.T) {
	e := newConflictPicker("f.txt", pickerDoc())
	m := Model{layers: &layerStack{entries: []layer{e}}, width: 80, height: 24}
	m, _ = e.update(m, keyMsg("ctrl+t")) // grid-zoom
	m, _ = e.update(m, keyMsg("tab"))    // focus output → zoom follows
	if !e.zoomed {
		t.Fatalf("tab dropped the zoom")
	}
	out := e.render(m, "")
	if strings.Contains(out, "region 1/2") || !strings.Contains(out, "── ") {
		t.Fatalf("after tab, zoom did not swap to the output half:\n%s", out)
	}
}

func TestPickerZoomRestoreShowsSplit(t *testing.T) {
	e := newConflictPicker("f.txt", pickerDoc())
	m := Model{layers: &layerStack{entries: []layer{e}}, width: 80, height: 24}
	m, _ = e.update(m, keyMsg("ctrl+t"))
	m, _ = e.update(m, keyMsg("ctrl+t"))
	out := e.render(m, "")
	if !strings.Contains(out, "── ") || !strings.Contains(out, "region 1/2") {
		t.Fatalf("restore did not bring back the split view:\n%s", out)
	}
}
```

Note: the rule line renders as `── output ───…` (or `── ▶ output` when focused) — `"── "` is the presence marker because the bare word "output" also appears in the grid hints (`[o] output`, `[tab] output`). Existing render tests in this file assert on bare `e.render(m, "")` output with `strings.Contains` — no ANSI stripping; keep that idiom.

- [ ] **Step 7: Run the render tests to verify they fail**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/picker-ctrlt-zoom && go test ./internal/tui/ -run 'TestPickerZoom|TestPickerTabSwaps' -v`
Expected: FAIL — zoomed renders still show both halves.

- [ ] **Step 8: Implement the render changes**

In `render` (conflict_picker.go, after `bodyH` is clamped at line ~375):

Replace:

```go
	gridH, outH := bodyH, 0
	if !e.outCollapsed {
```

with:

```go
	// Output-zoom: the rule replaces the column-labels row and the pane gets
	// the full body; no grid rows are built at all.
	if e.zoomed && e.outFocused {
		lines := []string{header, e.outputRule(w)}
		lines = append(lines, e.renderOutput(w, bodyH)...)
		lines = append(lines, "")
		lines = append(lines, hintLines...)
		return strings.Join(lines, "\n")
	}

	gridH, outH := bodyH, 0
	if !e.outCollapsed && !e.zoomed {
```

(The second change makes grid-zoom skip the split math: `gridH` stays `bodyH`, `outH` stays 0, so the rule+pane block at the bottom is skipped by the existing `if outH > 0` guard. Grid-zoom keeps the column-labels row.)

- [ ] **Step 9: Run the render tests to verify they pass, then the whole picker suite**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/picker-ctrlt-zoom && go test ./internal/tui/ -run 'TestPickerZoom|TestPickerTabSwaps' -v && go test ./internal/tui/`
Expected: PASS, and the full `internal/tui` package green.

- [ ] **Step 10: Commit**

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/picker-ctrlt-zoom && git add internal/tui/conflict_picker.go internal/tui/conflict_picker_test.go && git commit -m "feat(tui): picker zoom rendering — focused half owns the body"
```

---

### Task 2: Conflict-process esc guard

**Files:**
- Modify: `internal/tui/conflict_process.go:93-97`
- Test: `internal/tui/conflict_process_test.go` (or wherever `confPicking` tests live — grep `confPicking` in `internal/tui/*_test.go` and add alongside)

**Interfaces:**
- Consumes: `hunkPicker.zoomed` from Task 1; `conflictProcess` states `confPicking`/`confListing`.
- Produces: nothing new — behavioral fix only.

Background: the conflict process intercepts esc BEFORE its embedded picker's `update` (it drops the picker back to the file list). Without this guard, esc under zoom in the process-owned picker would discard the picker instead of restoring the split — violating the spec's esc-restores-first decision.

- [ ] **Step 1: Write the failing test**

Find the existing test file driving `conflictProcess` in `confPicking` state (grep `confPicking` under `internal/tui/*_test.go`) and mirror its setup idiom:

```go
func TestConflictProcessEscRestoresPickerZoom(t *testing.T) {
	p := &conflictProcess{st: confPicking, picker: newProcessConflictPicker("f.txt", pickerDoc())}
	m := Model{proc: p, width: 80, height: 24}
	m, _ = p.update(m, keyMsg("ctrl+t"))
	if !p.picker.zoomed {
		t.Fatalf("ctrl+t did not reach the process picker")
	}
	m, _ = p.update(m, keyMsg("esc"))
	if p.picker == nil || p.st != confPicking {
		t.Fatalf("esc under zoom left the picker (st=%v)", p.st)
	}
	if p.picker.zoomed {
		t.Fatalf("esc under zoom did not restore the split")
	}
	m, _ = p.update(m, keyMsg("esc"))
	if p.picker != nil || p.st != confListing {
		t.Fatalf("second esc did not leave the picker (st=%v)", p.st)
	}
}
```

Adjust the `conflictProcess` literal to whatever fields its existing tests construct (if they build it through a helper, use the helper). If `p.update` is not the method name the process exposes to tests, mirror the existing tests' call path exactly.

- [ ] **Step 2: Run it to verify it fails**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/picker-ctrlt-zoom && go test ./internal/tui/ -run TestConflictProcessEscRestoresPickerZoom -v`
Expected: FAIL — the first esc sets `p.picker = nil`.

- [ ] **Step 3: Implement the guard**

In `conflict_process.go`, the `confPicking` case:

```go
		if msg.String() == "esc" { // leave the editor without applying → back to the list
			if p.picker.zoomed { // esc restores the zoomed split before it can leave
				p.picker.zoomed = false
				return m, nil
			}
			p.picker = nil
			p.st = confListing
			return m, nil
		}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/picker-ctrlt-zoom && go test ./internal/tui/ -run TestConflictProcessEscRestoresPickerZoom -v && go test ./internal/tui/`
Expected: PASS, package green.

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/picker-ctrlt-zoom && git add internal/tui/conflict_process.go internal/tui/conflict_process_test.go && git commit -m "fix(tui): conflict-process esc restores picker zoom before leaving"
```

(Substitute the actual test file name if the test landed elsewhere.)

---

### Task 3: Hints, help, i18n, docs

**Files:**
- Modify: `internal/tui/conflict_picker.go` (hint rows, lines ~358-368)
- Modify: `internal/tui/help.go` (Hunk picker section, lines ~129-141)
- Modify: `internal/i18n/lang/ja.toml`, `internal/i18n/lang/ko.toml`, `internal/i18n/lang/zh.toml`, `internal/i18n/lang/ru.toml`
- Modify: `README.md` (rows for `H` ~line 75 and `x` ~line 85), `CHANGELOG.md`

**Interfaces:**
- Consumes: the behavior from Tasks 1-2. Nothing produced for later tasks.

- [ ] **Step 1: Add `[ctrl+t] full` to both hint sets**

In `render`, grid-focused `hintParts`: insert `i18n.T("[ctrl+t] full")` before `i18n.T("[enter] apply")`. Output-focused `hintParts`: same position. The key `"[ctrl+t] full"` already exists in all four bundles — do NOT add it again.

- [ ] **Step 2: Add the help row**

In `help.go`, inside the "Hunk picker" section, after the `tab` row (line ~137-138) add:

```go
		r("ctrl+t", i18n.T("zoom the focused half (grid or output) to the whole body — Tab swaps which half is zoomed, ctrl+t or esc restores the split")),
```

- [ ] **Step 3: Add the new key to all four bundles**

The literal English text from Step 2 is the key. Append to each bundle (alphabetical/nearby-section placement per the file's existing grouping; exact section does not matter to the gate, duplicate keys do):

`ja.toml`:
```toml
"zoom the focused half (grid or output) to the whole body — Tab swaps which half is zoomed, ctrl+t or esc restores the split" = "フォーカス中の半分（グリッドまたは出力）を本体全体にズーム — Tab でズーム対象を切り替え、ctrl+t か esc で分割表示に戻る"
```

`ko.toml`:
```toml
"zoom the focused half (grid or output) to the whole body — Tab swaps which half is zoomed, ctrl+t or esc restores the split" = "포커스된 절반(그리드 또는 출력)을 본문 전체로 확대 — Tab으로 확대 대상을 전환, ctrl+t 또는 esc로 분할 복원"
```

`zh.toml`:
```toml
"zoom the focused half (grid or output) to the whole body — Tab swaps which half is zoomed, ctrl+t or esc restores the split" = "将聚焦的半屏（网格或输出）放大到整个主体 — Tab 切换放大的一半，ctrl+t 或 esc 恢复分栏"
```

`ru.toml`:
```toml
"zoom the focused half (grid or output) to the whole body — Tab swaps which half is zoomed, ctrl+t or esc restores the split" = "развернуть активную половину (сетку или вывод) на всё окно — Tab переключает развёрнутую половину, ctrl+t или esc возвращает разделение"
```

- [ ] **Step 4: Run the i18n gates and the TUI suite**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/picker-ctrlt-zoom && go test ./internal/tui/ ./internal/i18n/`
Expected: PASS (the AST-gate tests `i18n_scan_test.go` etc. confirm literal keys + four-bundle coverage).

- [ ] **Step 5: Update README and CHANGELOG**

README: in the `H` row (~line 75) and the `x` row (~line 85), extend the existing tab/output-pane sentence with: `` `ctrl+t` zooms the focused half (grid or output) to the whole box — Tab swaps which half is zoomed, `ctrl+t`/`esc` restores the split ``. Keep each row one table cell — append inside the cell, matching the row's prose style.

CHANGELOG: add a bullet at the top of the current unreleased/topmost section, matching the neighboring bullets' style:

```markdown
- Hunk picker: `ctrl+t` zooms the tab-focused half (selection grid or output pane) to the whole body; Tab swaps which half is zoomed, `ctrl+t`/`esc` restores the split (esc restores before it cancels — including in the conflict process's picker).
```

- [ ] **Step 6: Full verification and commit**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/picker-ctrlt-zoom && gofmt -l internal/ && ./test.sh unit`
Expected: gofmt prints nothing; unit stage green.

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/picker-ctrlt-zoom && git add internal/tui/conflict_picker.go internal/tui/help.go internal/i18n/lang/ja.toml internal/i18n/lang/ko.toml internal/i18n/lang/zh.toml internal/i18n/lang/ru.toml README.md CHANGELOG.md && git commit -m "docs(tui): advertise picker ctrl+t zoom — hints, help, i18n, README, changelog"
```
