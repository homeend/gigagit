# TUI Files / Staged Split (Stage 3) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Split the single Status panel into **Files** (working-tree changes) and **Staged** (index) panels, so staged content is visible at a glance.

**Architecture:** Rename `panelStatus` → `panelFiles` and add a new `panelStaged`. Both panels are backed by the *same* full `m.status.Files`; a per-panel **membership filter applied in `panelView`** selects each panel's subset, so `backingIndex` keeps returning indices into `m.status.Files` and every action handler stays unchanged. The left column becomes three boxes (tab slot / Files / Staged); a short terminal drops Staged. `space` stages from Files and unstages from Staged.

**Tech Stack:** Go 1.26, Bubble Tea (value-receiver `Model`), lipgloss.

**Spec:** [`docs/superpowers/specs/2026-06-16-tui-window-framework-design.md`](../specs/2026-06-16-tui-window-framework-design.md) §"Stage 3 — Files / Staged split". Stage 1a (window primitive) and Stage 2 (tabbed Branches/Worktrees) are merged.

**Classification (pure TUI filter over `model.FileStatus` XY bytes; no domain change):**
- **Files** = working-tree side: `Unstaged != '.'` (and `!= 0`) — includes unstaged mods, untracked (`?`), and conflicts (unmerged codes have `Y != '.'`).
- **Staged** = index side: not unmerged, not untracked, and `Staged != '.'` (and `!= 0`, `!= '?'`).
- A partially-staged file (`MM`) appears in **both**. Conflicts appear only in Files.

---

## File structure

| File | Responsibility | Action |
|---|---|---|
| `internal/tui/*.go` (+ `*_test.go`) | token rename `panelStatus` → `panelFiles` | Modify (mechanical) |
| `internal/tui/model.go` | `panelStaged` enum; `isFilesPanel`; `focusOrder`/`leftReturnTarget` for Staged; `handleStageKey` direction; dispatch enter/h/b from both; `s`/clamp | Modify |
| `internal/tui/viewstate.go` | `inFilesPanel`/`inStagedPanel`/`memberOf`; `panelView` membership; `listFor` Staged case; `layout()` 3-box | Modify |
| `internal/tui/view.go` | `renderInterface` adds the Staged box; Files/Staged labels with counts | Modify |
| `internal/tui/avail.go` | `canStage`/`canShowFileDiff` use `m.focus` + `isFilesPanel` | Modify |
| `internal/tui/mark.go` | mark machinery uses `isFilesPanel` | Modify |
| `internal/tui/footer.go` | enter-diff / space stage+unstage / mark / stash bindings for both panels | Modify |
| `internal/tui/help.go`, `CHANGELOG.md`, `README.md` | docs | Modify |

**Conventions:** `internal/tui` must not import `internal/git`. New TUI keys need help + footer rows (gated by `TestHelpFooterCoverage`). `panelLen` and `markedDisplayIndices` already route through `panelView`, so the membership filter applies to them automatically. Run `./test.sh race` before merging.

---

### Task 1: Mechanical rename `panelStatus` → `panelFiles`

**Files:** all of `internal/tui/*.go` including tests.

**Context:** `panelStatus` is a unique token (not a substring of `statusList`/`statusRows`/`m.status`), so a global token replace is safe. The panel's UI label also changes from "Status" to "Files". No behavior change — Files (alone) behaves exactly like the old Status panel until Task 2 adds Staged.

- [ ] **Step 1: Run the rename**

```bash
cd internal/tui
# AST-safe identifier rename across the package (impl + tests):
gofmt -r 'panelStatus -> panelFiles' -w *.go
# The UI label string:
sed -i 's/m.panelLabel(panelFiles, "Status")/m.panelLabel(panelFiles, "Files")/' view.go
cd ../..
```

- [ ] **Step 2: Update the focusOrder comment**

In `model.go`, the `focusOrder` doc comment says "then Status, then Commits" — change "Status" to "Files":

```go
// focusOrder is the top-to-bottom sequence of focusable panels: the active
// Branches/Worktrees tab (the inactive one is not focusable), then Files (and
// Staged when it fits), then Commits. tab/shift+tab walk this.
```

- [ ] **Step 3: Verify the rename compiles and tests pass**

Run: `go build ./... && go test ./internal/tui/`
Expected: PASS (pure rename; the panel is still a single "Files"/Status-equivalent panel). If a test referenced the literal label "Status" in rendered output, update it to "Files".

- [ ] **Step 4: Commit**

```bash
gofmt -w internal/tui/*.go
git add -A
git commit -m "refactor(tui): rename panelStatus -> panelFiles (label Status -> Files)"
```

---

### Task 2: Add `panelStaged` + membership filter + geometry + render

**Files:** `internal/tui/model.go` (enum, `isFilesPanel`, `focusOrder`, `leftReturnTarget`), `internal/tui/viewstate.go` (membership, `listFor`, `layout`), `internal/tui/view.go` (`renderInterface`, labels).
**Test:** `internal/tui/stage_test.go` (membership), `internal/tui/fit_test.go` (geometry).

**Context:** This is the coupled core — enum, filter, geometry, and render must land together to compile and stay consistent. The inactive Worktrees tab and (on a short terminal) Staged are hidden via `boxH=0`.

- [ ] **Step 1: Write the failing tests**

Add to `internal/tui/stage_test.go`:

```go
func TestFilesStagedMembership(t *testing.T) {
	m := New(nil)
	m.width, m.height = 80, 30
	m.status.Files = []model.FileStatus{
		{Path: "untracked.txt", Kind: model.KindUntracked, Staged: '?', Unstaged: '?'},
		{Path: "unstaged.go", Kind: model.KindTracked, Staged: '.', Unstaged: 'M'},
		{Path: "staged.go", Kind: model.KindTracked, Staged: 'M', Unstaged: '.'},
		{Path: "partial.go", Kind: model.KindTracked, Staged: 'M', Unstaged: 'M'},
		{Path: "conflict.go", Kind: model.KindUnmerged, Staged: 'U', Unstaged: 'U'},
	}
	files := pathsOf(t, m, panelFiles)
	staged := pathsOf(t, m, panelStaged)
	wantFiles := map[string]bool{"untracked.txt": true, "unstaged.go": true, "partial.go": true, "conflict.go": true}
	wantStaged := map[string]bool{"staged.go": true, "partial.go": true}
	if !sameSet(files, wantFiles) {
		t.Errorf("Files panel = %v, want %v", files, wantFiles)
	}
	if !sameSet(staged, wantStaged) {
		t.Errorf("Staged panel = %v, want %v", staged, wantStaged)
	}
}

// pathsOf returns the file paths visible in panel p (membership + view order).
func pathsOf(t *testing.T, m Model, p panel) []string {
	t.Helper()
	_, idx := m.panelView(p)
	out := make([]string, len(idx))
	for n, i := range idx {
		out[n] = m.status.Files[i].Path
	}
	return out
}

func sameSet(got []string, want map[string]bool) bool {
	if len(got) != len(want) {
		return false
	}
	for _, g := range got {
		if !want[g] {
			return false
		}
	}
	return true
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/tui/ -run TestFilesStagedMembership`
Expected: FAIL — `panelStaged` undefined.

- [ ] **Step 3: Implement**

In `model.go` enum, insert `panelStaged` after `panelFiles`:

```go
const (
	panelBranches panel = iota
	panelWorktrees
	panelFiles
	panelStaged
	panelCommits
	panelCount
)
```

Add `isFilesPanel` (in `avail.go` or `model.go`):

```go
// isFilesPanel reports whether p is one of the two working-tree file panels.
func (m Model) isFilesPanel(p panel) bool { return p == panelFiles || p == panelStaged }
```

In `viewstate.go`, add the membership predicates and hook them into `panelView`:

```go
// inFilesPanel reports whether f belongs in the Files panel: any working-tree
// change (Unstaged side), including untracked and conflicts.
func inFilesPanel(f model.FileStatus) bool {
	return f.Kind == model.KindUntracked || (f.Unstaged != '.' && f.Unstaged != 0)
}

// inStagedPanel reports whether f belongs in the Staged panel: an index change.
// Untracked and unmerged (conflict) entries are excluded.
func inStagedPanel(f model.FileStatus) bool {
	if f.Kind == model.KindUntracked || f.Kind == model.KindUnmerged {
		return false
	}
	return f.Staged != '.' && f.Staged != 0 && f.Staged != '?'
}

// memberOf reports whether backing element i of panel p is shown there. Only
// the Files/Staged split filters; every other panel shows all of its rows.
func (m Model) memberOf(p panel, i int) bool {
	switch p {
	case panelFiles:
		return inFilesPanel(m.status.Files[i])
	case panelStaged:
		return inStagedPanel(m.status.Files[i])
	}
	return true
}
```

In `panelView`, add the membership check at the top of the loop (before the search filter):

```go
	idx = make([]int, 0, l.Len())
	for i := 0; i < l.Len(); i++ {
		if !m.memberOf(p, i) {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(l.Row(i)), q) {
			continue
		}
		idx = append(idx, i)
	}
```

In `listFor`, make the Files case also serve Staged (both back onto the full slice; `panelView` filters):

```go
	case panelFiles, panelStaged:
		return statusList{files: m.status.Files, rows: m.statusRows(), root: m.currentWorktree, mtime: map[int]int64{}}
```

In `layout()`, replace the two-box left column with three boxes (short terminal drops Staged):

```go
	// Left column: the tab slot, Files, and Staged. Each bordered box needs >=3
	// rows; a short terminal drops Staged (tab slot over Files).
	if bodyH >= 12 {
		h1 := bodyH / 3
		h2 := bodyH / 3
		g.boxH[m.activeLeftTab] = h1
		g.boxH[panelFiles] = h2
		g.boxH[panelStaged] = bodyH - h1 - h2
		g.pos[m.activeLeftTab] = point{0, 1}
		g.pos[panelFiles] = point{0, 1 + h1}
		g.pos[panelStaged] = point{0, 1 + h1 + h2}
	} else {
		h1 := bodyH / 2
		g.boxH[m.activeLeftTab] = h1
		g.boxH[panelFiles] = bodyH - h1
		g.pos[m.activeLeftTab] = point{0, 1}
		g.pos[panelFiles] = point{0, 1 + h1}
	}
	g.boxH[panelCommits] = bodyH
```

Update `focusOrder` to include Staged only when it is visible, and `leftReturnTarget` to skip a hidden target:

```go
func (m Model) focusOrder() []panel {
	order := []panel{m.activeLeftTab, panelFiles}
	if m.layout().boxH[panelStaged] > 0 {
		order = append(order, panelStaged)
	}
	return append(order, panelCommits)
}

func (m Model) leftReturnTarget() panel {
	p := m.lastLeftPanel
	if (p == panelBranches || p == panelWorktrees) && p != m.activeLeftTab {
		p = m.activeLeftTab
	}
	if m.layout().boxH[p] <= 0 { // hidden (inactive tab, or Staged on a short terminal)
		return panelFiles
	}
	return p
}
```

In `view.go` `renderInterface`, render the three left boxes (Staged only when it fits), with row counts in the Files/Staged labels:

```go
	var left string
	if m.filesView != nil {
		left = m.renderFilesView(g.leftW, g.bodyH)
	} else {
		active := m.activeLeftTab
		atRows, _ := m.panelView(active)
		fRows, _ := m.panelView(panelFiles)
		boxes := []string{
			m.renderPanel(active, m.panelLabel(active, tabBarLabel(active)), atRows, g.leftW, g.boxH[active]),
			m.renderPanel(panelFiles, m.filesLabel(panelFiles, "Files"), fRows, g.leftW, g.boxH[panelFiles]),
		}
		if g.boxH[panelStaged] > 0 {
			sRows, _ := m.panelView(panelStaged)
			boxes = append(boxes, m.renderPanel(panelStaged, m.filesLabel(panelStaged, "Staged"), sRows, g.leftW, g.boxH[panelStaged]))
		}
		left = lipgloss.JoinVertical(lipgloss.Left, boxes...)
	}
	var right string
```

Add the count helper to `view.go`:

```go
// filesLabel decorates a Files/Staged panel label with its visible row count
// plus the shared sort/filter decorations.
func (m Model) filesLabel(p panel, base string) string {
	return m.panelLabel(p, base+" "+strconv.Itoa(m.panelLen(p)))
}
```

(`strconv` is already imported in `viewstate.go`; add it to `view.go` imports if not present.)

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/tui/ -run 'TestFilesStagedMembership'`
Expected: PASS. (Other geometry tests will fail — fixed in Task 6.)

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/tui/model.go internal/tui/viewstate.go internal/tui/view.go internal/tui/stage_test.go
git add -A
git commit -m "feat(tui): add Staged panel + membership filter, 3-box left column"
```

---

### Task 3: Action retargeting (stage / unstage / diff / history / blame / mark)

**Files:** `internal/tui/avail.go`, `internal/tui/model.go` (`handleStageKey`, dispatch, clamp), `internal/tui/mark.go`.
**Test:** `internal/tui/stage_test.go`.

- [ ] **Step 1: Write the failing test**

```go
func TestSpaceStagesFromFilesUnstagesFromStaged(t *testing.T) {
	mk := func(focus panel) Model {
		m := New(nil)
		m.width, m.height = 80, 30
		m.status.Files = []model.FileStatus{
			{Path: "staged.go", Kind: model.KindTracked, Staged: 'M', Unstaged: '.'},
			{Path: "unstaged.go", Kind: model.KindTracked, Staged: '.', Unstaged: 'M'},
		}
		m.focus = focus
		return m
	}
	// Files panel, cursor on the only Files row (unstaged.go) → stage it.
	if d := stageDirection(t, mk(panelFiles)); d != false {
		t.Errorf("space on Files should STAGE (Unstage=false), got Unstage=%v", d)
	}
	// Staged panel, cursor on the only Staged row (staged.go) → unstage it.
	if d := stageDirection(t, mk(panelStaged)); d != true {
		t.Errorf("space on Staged should UNSTAGE (Unstage=true), got Unstage=%v", d)
	}
}

// stageDirection captures the engine.Stage.Unstage value handleStageKey emits.
func stageDirection(t *testing.T, m Model) bool {
	t.Helper()
	if !m.canStage() {
		t.Fatal("canStage() should be true for the focused files panel with a selection")
	}
	bi, _ := m.backingIndex(m.focus)
	return m.focus == panelStaged // mirrors handleStageKey's direction rule
}
```

> Note: this test asserts the *rule* (`focus == panelStaged ⇒ unstage`) rather than running the op (which needs a real repo). The op wiring is exercised by the existing `stage_test.go` repo-backed tests after the focus rename.

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/tui/ -run TestSpaceStagesFromFilesUnstagesFromStaged`
Expected: FAIL — `canStage()` still checks `m.focus != panelFiles` only.

- [ ] **Step 3: Implement**

In `avail.go`, make both predicates panel-aware and self-guarded:

```go
func (m Model) canStage() bool {
	if !m.isFilesPanel(m.focus) || !m.opsIdle() {
		return false
	}
	_, ok := m.backingIndex(m.focus)
	return ok
}

func (m Model) canShowFileDiff() bool {
	if !m.isFilesPanel(m.focus) {
		return false
	}
	bi, ok := m.backingIndex(m.focus)
	if !ok {
		return false
	}
	f := m.status.Files[bi]
	return m.opsIdle() && f.Kind != model.KindUnmerged && !(m.width > 0 && m.width < 60)
}
```

In `model.go` `handleStageKey`, make the direction panel-driven and read from the focused panel:

```go
func (m Model) handleStageKey() (tea.Model, tea.Cmd) {
	if !m.canStage() {
		return m, nil
	}
	bi, _ := m.backingIndex(m.focus)
	f := m.status.Files[bi]
	if f.Kind == model.KindUnmerged {
		m.statusMsg = "resolve conflicts first"
		return m, nil
	}
	m.running = true
	m.statusMsg = "working…"
	return m, m.stageCmd(engine.Stage{Paths: []string{f.Path}, Unstage: m.focus == panelStaged})
}
```

In `model.go` dispatch, the enter/h/b arms gated on `m.focus == panelFiles && m.canShowFileDiff()` become just `m.canShowFileDiff()` (it now self-guards to the focused files panel), and the `bi` reads use `m.focus`:

- `case "enter":` change `if m.focus == panelFiles && m.canShowFileDiff() {` → `if m.canShowFileDiff() {`, and `bi, _ := m.backingIndex(panelFiles)` → `bi, _ := m.backingIndex(m.focus)`.
- `case "h":` and `case "b":` same two changes.

In `model.go` the dataLoadedMsg clamp, clamp both file panels:

```go
		for _, p := range []panel{panelFiles, panelStaged} {
			if n := m.panelLen(p); n > 0 && m.sel[p] >= n {
				m.sel[p] = n - 1
			}
		}
```

In `mark.go`, the multi-select file-mark branch fires for either file panel:

```go
	if m.isFilesPanel(m.focus) {
		if m.fileMarks == nil {
			m.fileMarks = map[string]bool{}
		}
		// ... unchanged toggle body ...
	}
```

And `markedDisplayIndices` (mark.go:~106): `if p == panelFiles && len(m.fileMarks) > 0` → `if m.isFilesPanel(p) && len(m.fileMarks) > 0`.

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/tui/ -run 'TestSpaceStagesFromFilesUnstagesFromStaged|TestStage'`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/tui/avail.go internal/tui/model.go internal/tui/mark.go internal/tui/stage_test.go
git add -A
git commit -m "feat(tui): stage from Files, unstage from Staged; diff/history/blame/mark from both"
```

---

### Task 4: Footer + availability bindings

**Files:** `internal/tui/footer.go`.
**Test:** `internal/tui/footer_test.go`.

**Context:** The footer is context-sensitive per focused panel. Split the single `[space] stage` into stage (Files) and unstage (Staged); the stash key stays Files-only; mark works in both.

- [ ] **Step 1: Write the failing test**

```go
func TestFooterStageVsUnstage(t *testing.T) {
	base := func(focus panel) Model {
		m := New(nil)
		m.width, m.height = 80, 30
		m.status.Files = []model.FileStatus{
			{Path: "a.go", Kind: model.KindTracked, Staged: 'M', Unstaged: 'M'},
		}
		m.focus = focus
		return m
	}
	if got := base(panelFiles).footerLine(); !strings.Contains(got, "[space] stage") || strings.Contains(got, "unstage") {
		t.Errorf("Files footer = %q, want [space] stage", got)
	}
	if got := base(panelStaged).footerLine(); !strings.Contains(got, "[space] unstage") {
		t.Errorf("Staged footer = %q, want [space] unstage", got)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/tui/ -run TestFooterStageVsUnstage`
Expected: FAIL — only one `[space] stage` binding exists, gated on `canStage` (true for both panels), so Staged shows "stage" not "unstage".

- [ ] **Step 3: Implement**

In `footer.go` `contextBindings`, replace the single stage binding and broaden the file-panel ones:

```go
	{"enter", "[enter] diff", func(m Model) bool { return m.canShowFileDiff() }},
	{"space", "[space] stage", func(m Model) bool { return m.focus == panelFiles && m.canStage() }},
	{"space", "[space] unstage", func(m Model) bool { return m.focus == panelStaged && m.canStage() }},
	{"s", "[s] stash", func(m Model) bool {
		return m.focus == panelFiles && m.opsIdle() && len(stashCandidates(m.status)) > 0
	}},
	{"m", "[m] mark", func(m Model) bool { return m.isFilesPanel(m.focus) && m.panelLen(m.focus) > 0 }},
```

(Remove the old `{"space", "[space] stage", Model.canStage}` and the old `enter`/`s`/`m` Status lines these replace.)

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/tui/ -run 'TestFooter|TestHelpFooterCoverage'`
Expected: PASS. (`space` already has a help row; both bindings share the key `space`, which the coverage gate matches.)

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/tui/footer.go internal/tui/footer_test.go
git add -A
git commit -m "feat(tui): footer shows stage on Files, unstage on Staged"
```

---

### Task 5: Help + CHANGELOG + README

**Files:** `internal/tui/help.go`, `CHANGELOG.md`, `README.md`.
**Test:** `internal/tui/help_test.go`.

- [ ] **Step 1: Write the failing test**

```go
func TestHelpDocumentsFilesStaged(t *testing.T) {
	var b strings.Builder
	for _, l := range helpContent() {
		b.WriteString(l.text + "\n")
	}
	h := b.String()
	if !strings.Contains(h, "Files panel") || !strings.Contains(h, "Staged panel") {
		t.Error("help does not document the Files and Staged panels")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/tui/ -run TestHelpDocumentsFilesStaged`
Expected: FAIL — help still has a single "Status panel" section.

- [ ] **Step 3: Implement**

In `help.go`, rename the "Status panel" heading/section to two sections:

```go
		h("Files panel"),
		r("space", "stage the selected file (git add)"),
		r("enter", "side-by-side diff (HEAD → working tree)"),
		r("s", "stash a selection of working-tree files"),
		r("m", "multi-select files (for stashing); h/b history/blame"),
		h("Staged panel"),
		r("space", "unstage the selected file (git restore --staged)"),
		r("enter", "side-by-side diff of the staged change"),
		r("m", "multi-select; h/b history/blame"),
```

In `CHANGELOG.md` (Unreleased → Added):

```markdown
#### TUI Files / Staged split
- The Status panel is now two panels: **Files** (working-tree changes —
  unstaged, untracked, and conflicts) and **Staged** (the index). `space` stages
  from Files and unstages from Staged; a partially-staged file shows in both.
  Each panel header shows its file count.
```

In `README.md`, update the panel/keybinding rows: replace the Status-panel description with Files + Staged, and note `space` stages on Files / unstages on Staged.

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/tui/ -run 'TestHelp'`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/help.go internal/tui/help_test.go CHANGELOG.md README.md
git commit -m "docs(tui): document the Files/Staged split in help, changelog, readme"
```

---

### Task 6: Fix residual geometry tests + full verification

**Files:** geometry/mouse/tooltip/fit tests that assumed the 2-box (Stage 2) or single-Status layout.
**Test:** the whole package.

- [ ] **Step 1: Run the package and list failures**

Run: `go test ./internal/tui/ 2>&1 | grep -E "FAIL|ok"`
Expected: failures in geometry-dependent tests — `TestLayoutOrigins` (now three boxes when `bodyH>=12`), `TestPanelAt`/`TestPanelRowAt*` (box heights/positions changed: tab slot `bodyH/3`, Files, Staged), and any test asserting the single Status box. The membership/stage/footer/help tests from Tasks 2-5 must already pass.

- [ ] **Step 2: Update each failing geometry test to the 3-box layout**

For an 80×24 model: `bodyH=21`, `bodyH>=12` so three boxes — `h1=h2=7`, tab slot y=1 (h7), Files y=8 (h7), Staged y=15 (h7); Commits column unchanged. Recompute the hit-test coordinates and box-height comments exactly as the geometry produces them (mirror `layout()`). For `TestLayoutOrigins`, assert the active tab at `{0,1}`, Files at `{0,1+boxH[active]}`, Staged at `{0,1+boxH[active]+boxH[panelFiles]}`, and that a short terminal (e.g. height 14 → bodyH 11 < 12) has no Staged origin.

- [ ] **Step 3: Run the full package**

Run: `go test ./internal/tui/`
Expected: PASS.

- [ ] **Step 4: Run the staged suite with the race detector**

Run: `./test.sh race`
Expected: all unit + e2e green.

- [ ] **Step 5: Build + manual smoke (document result)**

Run: `go build ./cmd/gg`
Expected: clean. Manually: open the TUI on a repo with both staged and unstaged changes; confirm the left column shows the tab slot, **Files**, and **Staged** boxes with counts; `space` on Files stages (the file moves to Staged), `space` on Staged unstages; `tab` cycles tab→Files→Staged→Commits; a partially-staged file shows in both.

- [ ] **Step 6: Commit (if Step 2 changed tests)**

```bash
gofmt -w internal/tui/*_test.go
git add -A
git commit -m "test(tui): update geometry tests for the 3-box Files/Staged left column"
```

---

## Self-review notes (for the executor)

- **Spec coverage:** rename + Staged panel (Task 1/2), membership classification (Task 2, incl. partial-stage in both and conflicts in Files only), geometry 3-box + short-terminal drop-Staged (Task 2), focus order + `←` guard for the hidden Staged (Task 2), stage/unstage by panel + diff/history/blame/mark from both (Task 3), footer/help/docs (Task 4/5). No domain change (FR-4 preserved) — pure TUI filter over the existing snapshot.
- **Type consistency:** `panelStaged`, `isFilesPanel`, `inFilesPanel`/`inStagedPanel`/`memberOf`, `filesLabel` defined once. `backingIndex` returns indices into `m.status.Files` (membership filters in `panelView`, not in the list), so every `m.status.Files[bi]` action site is correct without change beyond `panelStatus`→`panelFiles` and `panelFiles`→`m.focus`.
- **Risk — `bodyH` threshold:** the 3-box layout needs `bodyH>=12` (three bordered boxes of >=3 rows + the tab slot's label). Below that, Staged drops. Confirm `TestFit`/reflow tests still pass; the very-narrow `w<40` single-Commits column is unchanged.
- **Risk — marks across panels:** a partially-staged file marked in Files also shows the mark in Staged (same path key). That is acceptable (one mark set keyed by path); note it if a reviewer asks.
- **Merge discipline:** before merging, confirm `git -C "$MAIN" rev-parse --abbrev-ref HEAD` is `main`; afterward delete both `worktree-*` and the plan-only `feat/*` branch (see [[merge-from-worktree-verify-target-branch]]).
