# Unstage Picker Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `H` on the Staged panel opens the region/line picker over index ↔ HEAD to deselect staged changes (`git reset -p` analog), applying through the existing `StageHunks` op; plus the five small carry-list items from sub-project 1.

**Architecture:** Pure reuse of the `hunkPicker` surface. New TUI lane only: `canUnstageHunks` gate → `loadUnstageHunksCmd` (index + HEAD blobs via existing `svc.ShowFile`) → `unstageHunksLoadedMsg` handler → `newUnstagePicker` (labels staged/HEAD, default keep-staged) → `engine.StageHunks`. Zero engine/domain changes.

**Tech Stack:** Go 1.26, Bubble Tea, existing test helpers (`keyMsg`, `pickerDoc`, `FakeRunner`-backed model tests where present).

**Spec:** `docs/superpowers/specs/2026-08-14-unstage-picker-design.md`

## Global Constraints

- Work in the worktree `.claude/worktrees/unstage-picker` on branch `feat/unstage-picker`. Prefix every build/test command with `cd /mnt/t/others/gigagit/.claude/worktrees/unstage-picker &&`; Write/Edit only absolute paths under that worktree.
- TUI `Model` is a value receiver; `hunkPicker` methods are pointer receivers.
- Every new user-visible TUI string: `i18n.T` with a **literal** key present in ALL FOUR bundles (`internal/i18n/lang/{ja,ko,zh,ru}.toml`). Existing keys to REUSE (never re-add — duplicate TOML keys fail parsing): `"[H] hunks"`, `"staged"`. Keys that do NOT exist yet and must be added ×4: listed per task below. When an English key is removed from Go code entirely, remove it from all four bundles too (the AST gate flags orphans in neither direction, but stale keys rot — and the changed help-header key in Task 4 MUST be swapped, not duplicated).
- Run tests in the FOREGROUND with generous timeouts (600000 ms); NEVER background a test run — a subagent's background process dies at turn end.
- gofmt-clean; TDD (failing test → red → implement → green → commit).
- Behavior decisions from the spec (do not "simplify"): new-in-HEAD staged file refused with the hint message; left column = staged (default kept); apply = existing `StageHunks`; suffix wording changes "none" → "empty"; sub-3-row output pane hides.

---

### Task 1: hunkpick carry items — LinePicked guard + clear-order test

**Files:**
- Modify: `internal/hunkpick/hunkpick.go` (the `LinePicked` method)
- Test: `internal/hunkpick/hunkpick_test.go`

**Interfaces:**
- Consumes: existing `Block.LinePicked(s Side, line int) bool`, `ToggleSide`, `ToggleLine`, `EnsurePicks`.
- Produces: no signature changes — behavior-hardening only.

- [ ] **Step 1: Write the failing/locking tests**

Append to `internal/hunkpick/hunkpick_test.go` (match the file's existing fixture idiom — read a couple of neighboring tests first and reuse their block-construction helper if one exists):

```go
func TestLinePickedZeroLineSide(t *testing.T) {
	// A pure-insertion block: current side empty. LinePicked on the empty
	// side must be false for any index, mirroring SideState's guard.
	b := &Block{Current: nil, Incoming: []string{"a", "b"}}
	b.EnsurePicks()
	b.ToggleSide(Incoming)
	if b.LinePicked(Current, 0) {
		t.Fatalf("LinePicked on a zero-line side must be false")
	}
}

func TestToggleSideClearPreservesInterleavedOrder(t *testing.T) {
	// space-pick i0, c0, i1 then ToggleSide(Current) clears ONLY the current
	// picks; the incoming picks keep their original relative order.
	b := &Block{Current: []string{"c0"}, Incoming: []string{"i0", "i1"}}
	b.EnsurePicks()
	b.ToggleLine(Incoming, 0)
	b.ToggleLine(Current, 0)
	b.ToggleLine(Incoming, 1)
	b.ToggleSide(Current) // all current picked → clears the current side
	got, ok := b.ResolvedLines()
	if !ok || len(got) != 2 || got[0] != "i0" || got[1] != "i1" {
		t.Fatalf("after clearing current: lines=%v ok=%v, want [i0 i1]", got, ok)
	}
}
```

Adjust ONLY construction mechanics to the real API (e.g. if `Block` literals aren't used in tests, build via `ParseConflict`/`FromDiff` with a fixture that yields the same shape); the assertions' meaning is fixed. If `EnsurePicks` on an Undecided block already implies something different, read `hunkpick.go` first and keep the test faithful to the intended scenario (a LineByLine block with interleaved picks).

- [ ] **Step 2: Run to see the state**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/unstage-picker && go test ./internal/hunkpick/ -run 'TestLinePickedZeroLineSide|TestToggleSideClearPreserves' -v`
Expected: the clear-order test may already PASS (it locks existing behavior); `TestLinePickedZeroLineSide` may PASS or FAIL depending on `LinePicked`'s current bounds handling — read its body. If BOTH already pass, the guard step below is still required (parity with `SideState`), and these tests become regression locks.

- [ ] **Step 3: Add the guard**

In `internal/hunkpick/hunkpick.go`, at the top of `LinePicked`, mirror `SideState`'s zero-line-side guard:

```go
	if len(b.lines(s)) == 0 {
		return false
	}
```

(Read `SideState` first and copy its exact guard idiom/comment style.)

- [ ] **Step 4: Run green + package**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/unstage-picker && go test ./internal/hunkpick/ -v 2>&1 | tail -15`
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/unstage-picker && git add internal/hunkpick/hunkpick.go internal/hunkpick/hunkpick_test.go && git commit -m "fix(hunkpick): LinePicked zero-line-side guard + interleaved clear-order lock"
```

---

### Task 2: the TUI unstage lane

**Files:**
- Modify: `internal/tui/avail.go` (after `canStageHunks`, ~line 52)
- Modify: `internal/tui/op.go` (after the stage-hunks block, ~line 296)
- Modify: `internal/tui/model.go` (the `"H"` key case ~line 1379; the `stageHunksLoadedMsg` handler ~line 2655)
- Modify: `internal/tui/conflict_picker.go` (after `newStagePicker`, ~line 103)
- Modify: `internal/i18n/lang/{ja,ko,zh,ru}.toml`
- Test: `internal/tui/conflict_picker_test.go`

**Interfaces:**
- Consumes: `svc.ShowFile(ctx, rev, path)` (rev `""` = index, `"HEAD"` = head — exactly how `loadStageHunksCmd` and the staged-diff lane use it), `hunkpick.FromDiff`, `engine.StageHunks`, `model.FileStatus.Staged` porcelain byte.
- Produces: `canUnstageHunks()` (Task 4's footer gate), `newUnstagePicker`, `unstageHunksLoadedMsg`, `loadUnstageHunksCmd`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/tui/conflict_picker_test.go`:

```go
func TestUnstageHunksLoadedPushesPicker(t *testing.T) {
	m := Model{width: 80, height: 24}
	mm, _ := m.Update(unstageHunksLoadedMsg{path: "f.txt",
		index: []byte("a\nX\nc\n"), head: []byte("a\nb\nc\n")})
	m = mm.(Model)
	e, ok := m.topLayer().(*hunkPicker)
	if !ok {
		t.Fatalf("unstage load did not push a picker, top = %T", m.topLayer())
	}
	out := e.render(m, "")
	if !strings.Contains(out, "Unstage hunks: f.txt") {
		t.Fatalf("title missing:\n%s", out)
	}
	if !strings.Contains(out, "staged") || !strings.Contains(out, "HEAD") {
		t.Fatalf("column labels missing:\n%s", out)
	}
	// Default: everything kept staged — resolves to the index bytes.
	outBytes, ok2 := e.doc.Resolved()
	if !ok2 || string(outBytes) != "a\nX\nc\n" {
		t.Fatalf("default resolve = %q ok=%v, want index bytes", outBytes, ok2)
	}
}

func TestUnstageHunksBinaryAndEmptyRefusals(t *testing.T) {
	m := Model{width: 80, height: 24}
	mm, _ := m.Update(unstageHunksLoadedMsg{path: "f.bin",
		index: []byte("x\x00y"), head: []byte("a\n")})
	m = mm.(Model)
	if m.topLayer() != nil || !strings.Contains(m.statusMsg, i18n.T("unstage hunks: binary file")) {
		t.Fatalf("binary refusal missing: layer=%v msg=%q", m.topLayer(), m.statusMsg)
	}
	m2 := Model{width: 80, height: 24}
	mm2, _ := m2.Update(unstageHunksLoadedMsg{path: "same.txt",
		index: []byte("a\n"), head: []byte("a\n")})
	m2 = mm2.(Model)
	if m2.topLayer() != nil || !strings.Contains(m2.statusMsg, i18n.T("unstage hunks: nothing to unstage")) {
		t.Fatalf("empty refusal missing: layer=%v msg=%q", m2.topLayer(), m2.statusMsg)
	}
}

func TestUnstagePickerApplyDispatchesStageHunks(t *testing.T) {
	doc := hunkpick.FromDiff([]byte("a\nX\nc\n"), []byte("a\nb\nc\n"))
	doc.SetAll(hunkpick.TakeCurrent)
	e := newUnstagePicker("f.txt", doc)
	// Revert the changed region to HEAD: incoming on, current off.
	m := Model{layers: &layerStack{entries: []layer{e}}, width: 80, height: 24}
	m, _ = e.update(m, keyMsg("i"))
	m, _ = e.update(m, keyMsg("c"))
	out, ok := e.doc.Resolved()
	if !ok || string(out) != "a\nb\nc\n" {
		t.Fatalf("resolved = %q ok=%v, want HEAD content", out, ok)
	}
}

func TestCanUnstageHunksGate(t *testing.T) {
	st := model.WorkingTreeStatus{Files: []model.FileStatus{
		{Path: "mod.txt", Staged: 'M', Kind: model.KindTracked},
		{Path: "new.txt", Staged: 'A', Kind: model.KindTracked},
	}}
	m := Model{status: st, focus: panelStaged, width: 80, height: 24}
	m.sel = map[panel]int{panelStaged: 0}
	if !m.canUnstageHunks() {
		t.Fatalf("gate false for a staged modification")
	}
	m.sel[panelStaged] = 1 // the added file
	if m.canUnstageHunks() {
		t.Fatalf("gate must refuse a newly added (not-in-HEAD) file")
	}
	m.focus = panelFiles
	m.sel[panelFiles] = 0
	if m.canUnstageHunks() {
		t.Fatalf("gate must be Staged-panel only")
	}
}
```

Setup notes for the implementer: match how existing tests construct a Model with panel selection (read `TestConflictFileLoadedPushesPicker` and any `canStageHunks`/avail tests for the exact fields — `sel`, `opsIdle` prerequisites like `running/loading` zero-values, `backingIndex` needs). If `m.Update` message plumbing differs (e.g. handlers switch in `updateMsg`), mirror `TestStageHunksLoadedPushesPicker` exactly — it is the template for the loaded-msg tests. Keep assertion meanings fixed.

- [ ] **Step 2: Run to verify compile failure**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/unstage-picker && go test ./internal/tui/ -run TestUnstage -v`
Expected: compile FAIL (`unstageHunksLoadedMsg`, `newUnstagePicker`, `canUnstageHunks` undefined).

- [ ] **Step 3: Implement the lane**

`internal/tui/avail.go`, after `canStageHunks`:

```go
// canUnstageHunks reports whether the Staged panel's selected row is a
// tracked, non-conflicted file the hunk-unstaging picker can open. A file
// not yet in HEAD (staged 'A') is excluded: StageHunks can only set index
// content, never remove the entry — space unstages such a file whole.
func (m Model) canUnstageHunks() bool {
	if m.focus != panelStaged || !m.opsIdle() {
		return false
	}
	bi, ok := m.backingIndex(panelStaged)
	if !ok {
		return false
	}
	f := m.status.Files[bi]
	return f.Kind != model.KindUntracked && f.Kind != model.KindUnmerged && f.Staged != 'A'
}
```

`internal/tui/op.go`, after the stage-hunks block:

```go
// unstageHunksLoadedMsg carries the two sides for the unstaging picker.
type unstageHunksLoadedMsg struct {
	path        string
	index, head []byte
	err         error
}

// loadUnstageHunksCmd reads the index blob and the HEAD blob off the UI
// thread; the resulting msg builds the diff and pushes the unstaging picker.
func (m Model) loadUnstageHunksCmd(path string) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		idx, err := svc.ShowFile(context.Background(), "", path)
		if err != nil {
			return unstageHunksLoadedMsg{path: path, err: err}
		}
		head, herr := svc.ShowFile(context.Background(), "HEAD", path)
		if herr != nil {
			return unstageHunksLoadedMsg{path: path, err: herr}
		}
		return unstageHunksLoadedMsg{path: path, index: idx, head: head}
	}
}
```

`internal/tui/model.go` — extend the `"H"` key case:

```go
		case "H":
			if m.canStageHunks() {
				bi, _ := m.backingIndex(panelFiles)
				return m, m.loadStageHunksCmd(m.status.Files[bi].Path)
			}
			if m.canUnstageHunks() {
				bi, _ := m.backingIndex(panelStaged)
				return m, m.loadUnstageHunksCmd(m.status.Files[bi].Path)
			}
			if m.focus == panelStaged && m.opsIdle() {
				if bi, ok := m.backingIndex(panelStaged); ok && m.status.Files[bi].Staged == 'A' {
					m.statusMsg = i18n.T("new file — space unstages it whole")
					return m, nil
				}
			}
```

`internal/tui/model.go` — new msg case directly after the `stageHunksLoadedMsg` handler, mirroring it exactly:

```go
	case unstageHunksLoadedMsg:
		if msg.err != nil {
			m.statusMsg = i18n.T("unstage hunks: %s", msg.err.Error())
			return m, nil
		}
		if textdiff.IsBinary(msg.index) || textdiff.IsBinary(msg.head) {
			m.statusMsg = i18n.T("unstage hunks: binary file")
			return m, nil
		}
		doc := hunkpick.FromDiff(msg.index, msg.head)
		doc.SetAll(hunkpick.TakeCurrent) // default: everything stays staged
		if len(doc.Blocks()) == 0 {
			m.statusMsg = i18n.T("unstage hunks: nothing to unstage")
			return m, nil
		}
		m = m.pushLayer(newUnstagePicker(msg.path, doc))
		return m, nil
```

`internal/tui/conflict_picker.go`, after `newStagePicker`:

```go
// newUnstagePicker wires the hunk-unstaging params: the grid runs index ↔
// HEAD and the assembled content goes back through StageHunks, so taking the
// HEAD side of a region reverts that region of the index (git reset -p).
func newUnstagePicker(path string, doc *hunkpick.Doc) *hunkPicker {
	return &hunkPicker{
		title:      i18n.T("Unstage hunks: %s", path),
		leftLabel:  i18n.T("staged"),
		rightLabel: i18n.T("HEAD"),
		requireAll: false,
		apply: func(m Model, content []byte) (Model, tea.Cmd) {
			m = m.popLayer()
			return m.startOp(engine.StageHunks{Path: path, Content: content})
		},
		doc: doc, blocks: doc.Blocks(), side: hunkpick.Current, mode: modeScroll,
	}
}
```

- [ ] **Step 4: Add the new i18n keys ×4 bundles**

New keys (place near the existing stage-hunks keys — lines ~303 and ~920 in each bundle; `"staged"` already exists, do NOT re-add):

ja.toml:
```toml
"Unstage hunks: %s" = "ハンクをアンステージ: %s"
"HEAD" = "HEAD"
"unstage hunks: %s" = "ハンクをアンステージ: %s"
"unstage hunks: binary file" = "ハンクをアンステージ: バイナリファイル"
"unstage hunks: nothing to unstage" = "ハンクをアンステージ: アンステージする内容がありません"
"new file — space unstages it whole" = "新規ファイル — space でファイルごとアンステージします"
```

ko.toml:
```toml
"Unstage hunks: %s" = "헝크 언스테이지: %s"
"HEAD" = "HEAD"
"unstage hunks: %s" = "헝크 언스테이지: %s"
"unstage hunks: binary file" = "헝크 언스테이지: 바이너리 파일"
"unstage hunks: nothing to unstage" = "헝크 언스테이지: 언스테이지할 내용이 없습니다"
"new file — space unstages it whole" = "새 파일 — space로 파일 전체를 언스테이지합니다"
```

zh.toml:
```toml
"Unstage hunks: %s" = "取消暂存代码块: %s"
"HEAD" = "HEAD"
"unstage hunks: %s" = "取消暂存代码块: %s"
"unstage hunks: binary file" = "取消暂存代码块: 二进制文件"
"unstage hunks: nothing to unstage" = "取消暂存代码块: 没有可取消暂存的内容"
"new file — space unstages it whole" = "新文件 — 用 space 取消暂存整个文件"
```

ru.toml:
```toml
"Unstage hunks: %s" = "Убрать хунки из индекса: %s"
"HEAD" = "HEAD"
"unstage hunks: %s" = "убрать хунки из индекса: %s"
"unstage hunks: binary file" = "убрать хунки из индекса: двоичный файл"
"unstage hunks: nothing to unstage" = "убрать хунки из индекса: нечего убирать"
"new file — space unstages it whole" = "новый файл — space убирает его из индекса целиком"
```

- [ ] **Step 5: Run green**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/unstage-picker && go test ./internal/tui/ -run 'TestUnstage|TestCanUnstage' -v && go test ./internal/tui/ ./internal/i18n/ 2>&1 | tail -3`
Expected: new tests PASS; both packages green (i18n gates confirm the keys).

- [ ] **Step 6: Commit**

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/unstage-picker && git add internal/tui/avail.go internal/tui/op.go internal/tui/model.go internal/tui/conflict_picker.go internal/tui/conflict_picker_test.go internal/i18n/lang/ja.toml internal/i18n/lang/ko.toml internal/i18n/lang/zh.toml internal/i18n/lang/ru.toml && git commit -m "feat(tui): unstage picker — H on the Staged panel runs index↔HEAD through StageHunks"
```

---

### Task 3: picker polish — "empty" suffix, suffix render tests, tiny-overlay hide

**Files:**
- Modify: `internal/tui/conflict_picker.go` (`stateSuffix` ~line 151; the `outH` split math in `render` ~line 377)
- Modify: `internal/i18n/lang/{ja,ko,zh,ru}.toml` (add `"empty"`; `"none"` stays — other users)
- Test: `internal/tui/conflict_picker_test.go`

**Interfaces:**
- Consumes: Task 2 nothing; independent of it.
- Produces: nothing for later tasks.

- [ ] **Step 1: Failing tests**

```go
func TestPickerSuffixEmptyAndFirst(t *testing.T) {
	e := newConflictPicker("f.txt", pickerDoc())
	m := Model{layers: &layerStack{entries: []layer{e}}, width: 100, height: 30}
	// Region 0: both sides on, current toggled first → " — current first".
	m, _ = e.update(m, keyMsg("c"))
	m, _ = e.update(m, keyMsg("i"))
	out := e.render(m, "")
	if !strings.Contains(out, i18n.T("%s first", i18n.T("current"))) {
		t.Fatalf("'current first' suffix missing:\n%s", out)
	}
	// Region 0: clear both → touched-empty → " — empty" (not "none").
	m, _ = e.update(m, keyMsg("c"))
	m, _ = e.update(m, keyMsg("i"))
	out = e.render(m, "")
	if !strings.Contains(out, " — "+i18n.T("empty")) {
		t.Fatalf("'empty' suffix missing:\n%s", out)
	}
}

func TestPickerTinyOverlayHidesOutputPane(t *testing.T) {
	e := newConflictPicker("f.txt", pickerDoc())
	// Height so small the pane cannot get its 3-line minimum after the
	// ≥3-grid-lines cap: it must hide entirely (no rule), not degrade.
	m := Model{layers: &layerStack{entries: []layer{e}}, width: 80, height: 10}
	out := e.render(m, "")
	if strings.Contains(out, "── ") {
		t.Fatalf("tiny overlay must hide the output pane:\n%s", out)
	}
	if got := len(splitLinesTest(out)); got != 10 {
		t.Fatalf("render produced %d lines, want 10", got)
	}
}
```

Before finalizing the tiny test, compute the actual `bodyH` for height 10 at width 80 (hint lines wrap!) and pick a height where the current code yields `0 < outH < 3` — print `e.render` in a scratch run if needed; the point is to pin a height where the OLD code shows a 1–2 line pane and the NEW code hides it. If height 10 doesn't produce that shape at width 80, adjust height/width until it does and note the choice in a comment.

- [ ] **Step 2: Run red**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/unstage-picker && go test ./internal/tui/ -run 'TestPickerSuffix|TestPickerTiny' -v`
Expected: FAIL — "empty" key/text missing; tiny overlay still shows a degraded pane (verify this is actually the old behavior; if the old code already hides at your chosen size, pick a different size that degrades).

- [ ] **Step 3: Implement**

`stateSuffix`: change `i18n.T("none")` → `i18n.T("empty")` (comment stays accurate: touched-empty region).

`render` split math — replace the block:

```go
	if !e.outCollapsed && !e.zoomed {
		outH = bodyH / 3
		if outH < 3 {
			outH = 3
		}
		if outH > bodyH-4 {
			outH = bodyH - 4 // keep ≥3 grid lines + the rule
		}
		if outH < 1 {
			outH = 0 // too small to show a pane at all
		} else {
			gridH = bodyH - outH - 1
		}
	}
```

with:

```go
	if !e.outCollapsed && !e.zoomed {
		outH = bodyH / 3
		if outH < 3 {
			outH = 3
		}
		if outH > bodyH-4 {
			outH = bodyH - 4 // keep ≥3 grid lines + the rule
		}
		if outH < 3 {
			outH = 0 // can't meet the 3-line minimum: hide rather than degrade
		} else {
			gridH = bodyH - outH - 1
		}
	}
```

Bundles — add near the picker keys (`"none"` untouched):

- ja: `"empty" = "空"`
- ko: `"empty" = "비움"`
- zh: `"empty" = "空"`
- ru: `"empty" = "пусто"`

- [ ] **Step 4: Run green + full picker suite**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/unstage-picker && go test ./internal/tui/ -run 'TestPicker|TestHunkPicker|TestConflictPicker' -v 2>&1 | tail -5 && go test ./internal/tui/ ./internal/i18n/ 2>&1 | tail -3`
Expected: all green (watch for existing tests asserting the old `"none"` suffix or a degraded pane — update any such assertion to the new spec'd behavior and say so in the report).

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/unstage-picker && git add internal/tui/conflict_picker.go internal/tui/conflict_picker_test.go internal/i18n/lang/ja.toml internal/i18n/lang/ko.toml internal/i18n/lang/zh.toml internal/i18n/lang/ru.toml && git commit -m "fix(tui): picker 'empty' suffix key, suffix render coverage, tiny-overlay pane hides"
```

---

### Task 4: footer, help, README, CHANGELOG, remaining i18n

**Files:**
- Modify: `internal/tui/footer.go` (~line 77, after the stage-hunks entry)
- Modify: `internal/tui/help.go` (Staged panel section ~line 115; the Hunk picker header ~line 129)
- Modify: `internal/i18n/lang/{ja,ko,zh,ru}.toml`
- Modify: `README.md`, `CHANGELOG.md`

**Interfaces:**
- Consumes: `canUnstageHunks` from Task 2.
- Produces: nothing.

- [ ] **Step 1: Footer**

After the `{"unstage", "space", ...}` entry add:

```go
		{"unstage-hunks", "H", i18n.T("[H] hunks"), func(m Model) bool { return m.canUnstageHunks() }, scopeRow},
```

(`"[H] hunks"` key exists — reuse.)

- [ ] **Step 2: Help**

In the **Staged panel** section (after its `space` row):

```go
		r("H", i18n.T("unstage hunks: open the region/line picker over the staged change (staged ↔ HEAD) — taking HEAD reverts that region of the index; the working tree is untouched")),
```

Change the Hunk picker section header key from
`"Hunk picker (x conflict resolve / H stage)"` to
`"Hunk picker (x conflict resolve / H stage / H unstage)"` — swap the key in help.go AND in all four bundles (remove the old key from each bundle, add the new one; a leftover old key is dead weight, a duplicate new key breaks TOML).

New keys ×4:

ja.toml:
```toml
"unstage hunks: open the region/line picker over the staged change (staged ↔ HEAD) — taking HEAD reverts that region of the index; the working tree is untouched" = "ハンクをアンステージ: ステージ済みの変更 (staged ↔ HEAD) をリージョン/行単位で選ぶピッカーを開く — HEAD 側を選ぶとそのリージョンのインデックスが HEAD に戻る。ワーキングツリーは変更されない"
"Hunk picker (x conflict resolve / H stage / H unstage)" = "ハンクピッカー (x 競合解決 / H ステージ / H アンステージ)"
```

ko.toml:
```toml
"unstage hunks: open the region/line picker over the staged change (staged ↔ HEAD) — taking HEAD reverts that region of the index; the working tree is untouched" = "헝크 언스테이지: 스테이지된 변경(staged ↔ HEAD)에 대한 영역/행 피커를 엽니다 — HEAD 쪽을 선택하면 해당 영역의 인덱스가 HEAD로 되돌아갑니다. 워킹 트리는 그대로입니다"
"Hunk picker (x conflict resolve / H stage / H unstage)" = "헝크 피커 (x 충돌 해결 / H 스테이지 / H 언스테이지)"
```

zh.toml:
```toml
"unstage hunks: open the region/line picker over the staged change (staged ↔ HEAD) — taking HEAD reverts that region of the index; the working tree is untouched" = "取消暂存代码块: 打开针对已暂存更改 (staged ↔ HEAD) 的区域/行选择器 — 选择 HEAD 侧会将该区域的索引恢复为 HEAD。工作区不受影响"
"Hunk picker (x conflict resolve / H stage / H unstage)" = "代码块选择器 (x 解决冲突 / H 暂存 / H 取消暂存)"
```

ru.toml:
```toml
"unstage hunks: open the region/line picker over the staged change (staged ↔ HEAD) — taking HEAD reverts that region of the index; the working tree is untouched" = "убрать хунки из индекса: открыть пикер по областям/строкам для индексированного изменения (staged ↔ HEAD) — выбор стороны HEAD возвращает эту область индекса к HEAD; рабочее дерево не меняется"
"Hunk picker (x conflict resolve / H stage / H unstage)" = "Пикер хунков (x разрешение конфликтов / H в индекс / H из индекса)"
```

- [ ] **Step 3: README + CHANGELOG**

README: find the Staged-panel `space` row (grep `restore --staged`). Append inside that row's cell (before the closing `|`), matching its prose style:

```
`H` opens the region/line **unstage picker** over the staged change (staged ↔ HEAD, the same picker surface as staging) — taking the HEAD side reverts that region of the index, the working tree is untouched (`git reset -p` style); a newly added file is refused (`space` unstages it whole)
```

Also extend the `H` row (~line 75, the staging-picker row): after "open the region/line **staging picker** for the selected tracked file", add a parenthetical "(on the **Staged** panel, `H` opens the mirror **unstage picker**, staged ↔ HEAD)". Keep both rows single lines.

CHANGELOG, top of `## [Unreleased]`, matching neighbor style:

```markdown
- **TUI: `H` on the Staged panel opens the unstage picker.** The same
  region/line picker surface now runs staged ↔ HEAD: taking the HEAD side of
  a region reverts that part of the index (`git reset -p` analog) while the
  working tree stays untouched; apply goes through the existing StageHunks
  op. A newly added file is refused with a hint (`space` unstages it whole).
  Also: the picker's touched-empty suffix now reads "empty" (own i18n key),
  and an output pane that cannot get its 3-line minimum hides instead of
  degrading.
```

- [ ] **Step 4: Gates + full unit stage**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/unstage-picker && gofmt -l internal/ && go test ./internal/tui/ ./internal/i18n/ 2>&1 | tail -3 && ./test.sh unit 2>&1 | tail -3`
Expected: gofmt silent; all green. Run in the FOREGROUND with timeout 600000.

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/unstage-picker && git add internal/tui/footer.go internal/tui/help.go internal/i18n/lang/ja.toml internal/i18n/lang/ko.toml internal/i18n/lang/zh.toml internal/i18n/lang/ru.toml README.md CHANGELOG.md && git commit -m "docs(tui): advertise the unstage picker — footer, help, i18n, README, changelog"
```
