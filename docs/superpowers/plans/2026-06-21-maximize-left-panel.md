# Maximize left-column panel (`t`) — implementation plan

> **For agentic workers:** Execute task-by-task, TDD, commit per task.

**Goal:** A `t` keybind that pins the focused small left-column TUI panel to fill
the whole left column height; `t` again restores the normal split.

**Architecture:** Drive entirely off `layout()` (`internal/tui/viewstate.go`) — the
single source of truth for panel geometry. Maximize = give the pinned panel
`boxH = bodyH` and hide the other left panels. A `leftColumnPanels()` helper
supplies the *logical* left-panel set so reachability/render membership never
derive from the maximize-zeroed `boxH`.

**Tech Stack:** Go 1.26, Bubble Tea TUI, lipgloss. Tests use the existing TUI
unit-test helpers (no svc / no `startOp`).

## Global Constraints

- Pin semantics (a specific focused panel), sticky until `t` toggles off.
- `panelCount` is the "no panel pinned" sentinel for `Model.leftMax`.
- Left panels only; `t` on Commits / narrow terminal / over the files/stash
  surface = no-op.
- Advertise `t` in BOTH footer.go and help.go (every keybind in both).
- TUI-only: no engine/git/CLI/e2e/agentskill changes.
- Tests avoid `startOp`/svc (nil-svc panic class) — pure predicates + a render
  end-to-end test.

---

### Task 1: `leftColumnPanels()` helper + `leftMax` field

**Files:**
- Modify: `internal/tui/model.go` (add `leftMax panel` field; init `leftMax: panelCount`; add `leftColumnPanels()`)
- Test: `internal/tui/maximize_left_test.go` (create)

**Interfaces:**
- Produces: `Model.leftMax panel`; `func (m Model) leftColumnPanels() []panel`.

- [ ] **Step 1: Write failing test** for `leftColumnPanels()`:
  - tall terminal (height 30) → `[activeLeftTab, middleTab(), panelStaged]`
  - short terminal (height 16, bodyH < 12) → `[activeLeftTab, middleTab()]` (no Staged)
  - reflects `activeLeftTab`/`activeFilesTab` (e.g. set `activeLeftTab=panelRemotes`, `activeFilesTab=panelTags` → first=panelRemotes, second=panelTags)
- [ ] **Step 2:** Run, verify it fails to compile (`leftColumnPanels` undefined).
- [ ] **Step 3:** Implement. Mirror the `bodyH >= 12` test in `layout()`:

```go
// leftColumnPanels returns the left-column panels that exist for the current
// terminal size, independent of any maximize. Staged is present only when the
// normal split is tall enough to show it.
func (m Model) leftColumnPanels() []panel {
	if m.width > 0 && m.width < 40 {
		return nil // single-column layout: no left column
	}
	ps := []panel{m.activeLeftTab, m.middleTab()}
	bodyH := m.height - 3
	if bodyH < 6 {
		bodyH = 6
	}
	if bodyH >= 12 {
		ps = append(ps, panelStaged)
	}
	return ps
}
```
  Add `leftMax panel` to the `Model` struct (near `focus`/`activeLeftTab`) with a
  comment: `// pinned full-column left panel; panelCount = none`. Add
  `leftMax: panelCount,` to the constructor (`New`/`initialModel`).
- [ ] **Step 4:** Run tests, verify pass.
- [ ] **Step 5:** Commit: `feat(tui): leftColumnPanels helper + leftMax field`.

---

### Task 2: `layout()` honors `leftMax`

**Files:**
- Modify: `internal/tui/viewstate.go` (`layout()`)
- Test: `internal/tui/maximize_left_test.go`

**Interfaces:**
- Consumes: `leftMax`, `leftColumnPanels()`.
- Produces: maximized geometry in `layoutGeom`.

- [ ] **Step 1: Write failing test:** set `m.leftMax = panelFiles` (with
  `activeFilesTab=panelFiles`), tall terminal. Assert:
  - `g.boxH[panelFiles] == g.bodyH`, `g.pos[panelFiles] == point{0,1}`
  - `g.boxH[m.activeLeftTab] == 0` and `g.boxH[panelStaged] == 0`
  - `g.boxH[panelCommits] == g.bodyH`, `g.pos[panelCommits] == point{g.leftW,1}` (unchanged)
  - control: with `leftMax = panelCount`, the normal three-box split is unchanged.
- [ ] **Step 2:** Run, verify fail.
- [ ] **Step 3:** Implement: after the normal left-column geometry is computed in
  `layout()` (the `bodyH>=12`/else block) and before `panelCommits` is set,
  insert a maximize override:

```go
// Maximize: a pinned left panel takes the whole left column; the others hide.
if m.leftMax != panelCount {
	for _, p := range m.leftColumnPanels() {
		if p == m.leftMax {
			g.boxH[p] = bodyH
			g.pos[p] = point{0, 1}
		} else {
			delete(g.boxH, p)
			delete(g.pos, p)
		}
	}
}
```
  (Place it inside the `w >= 40` path, after the staged geometry, before
  `g.boxH[panelCommits] = bodyH`.)
- [ ] **Step 4:** Run tests, verify pass.
- [ ] **Step 5:** Commit: `feat(tui): layout maximizes the pinned left panel`.

---

### Task 3: `focusOrder()` + `leftReturnTarget()` respect maximize

**Files:**
- Modify: `internal/tui/model.go` (`focusOrder`, `leftReturnTarget`)
- Test: `internal/tui/maximize_left_test.go`

**Interfaces:**
- Consumes: `leftMax`, `leftColumnPanels()`.

- [ ] **Step 1: Write failing tests:**
  - `focusOrder()` with `leftMax=panelFiles` → `[panelFiles, panelCommits]`.
  - `focusOrder()` with `leftMax=panelCount` (tall) → unchanged
    `[activeLeftTab, middleTab(), panelStaged, panelCommits]`.
  - `leftReturnTarget()` with `leftMax=panelBranches` → `panelBranches`.
- [ ] **Step 2:** Run, verify fail.
- [ ] **Step 3:** Implement:
  - `focusOrder()`: at the top, `if m.leftMax != panelCount { return []panel{m.leftMax, panelCommits} }`. Leave the existing body otherwise (its `boxH[panelStaged] > 0` check is correct when not maximized).
  - `leftReturnTarget()`: at the top, `if m.leftMax != panelCount { return m.leftMax }`.
- [ ] **Step 4:** Run tests, verify pass.
- [ ] **Step 5:** Commit: `feat(tui): focus order + return target honor maximize`.

---

### Task 4: render assembly skips hidden left panels

**Files:**
- Modify: `internal/tui/view.go` (`renderInterface`, the `m.filesView == nil` left-column branch, lines ~334–352)
- Test: `internal/tui/maximize_left_test.go`

**Interfaces:**
- Consumes: `leftColumnPanels()`, `layout().boxH`.

- [ ] **Step 1: Write a failing end-to-end render test:** build a Model with a
  real-ish snapshot (branches + files + staged + commits), `width=120 height=40`,
  `leftMax = m.activeLeftTab`, focus on `activeLeftTab`. Call `m.View()` (or
  `renderInterface`). Assert the output:
  - contains the pinned panel's label,
  - does NOT contain the Files or Staged labels (they're hidden),
  - the left column block height (count of lines belonging to the left box) spans
    the full body (i.e. no degenerate 0-height box rendered). A simple proxy:
    assert the render does not panic and the pinned box border line count ≈
    `bodyH`. Keep the assertion robust (label presence/absence is the load-bearing
    part).
- [ ] **Step 2:** Run, verify fail (today both top + middle boxes always render).
- [ ] **Step 3:** Implement: replace the unconditional `boxes := []string{...}` +
  `if g.boxH[panelStaged] > 0` block with a loop over `m.leftColumnPanels()`:

```go
var boxes []string
for _, p := range m.leftColumnPanels() {
	if g.boxH[p] <= 0 {
		continue // hidden (inactive in normal split, or maximized away)
	}
	rows, _ := m.panelView(p)
	boxes = append(boxes, m.renderPanel(p, m.leftPanelLabel(p), rows, nil, g.leftW, g.boxH[p]))
}
left = lipgloss.JoinVertical(lipgloss.Left, boxes...)
```
  Add a small `leftPanelLabel(p)` helper that returns the same label each panel
  used before (top tab → `tabBarLabel`, middle → `filesTabLabel`, Staged →
  `filesLabel(panelStaged,"Staged")`) so the labels are unchanged. Verify the
  helper reproduces the exact prior label strings.
- [ ] **Step 4:** Run the full `internal/tui` tests (catch any label regression),
  verify pass.
- [ ] **Step 5:** Commit: `feat(tui): render left column from the logical panel set`.

---

### Task 5: `t` key handler + `canMaximizeLeft()`

**Files:**
- Modify: `internal/tui/model.go` (key dispatch near `case "z"`; add `canMaximizeLeft`)
- Test: `internal/tui/maximize_left_test.go`

**Interfaces:**
- Consumes: `leftColumnPanels`, `leftMax`, `filesView`, `stashView`.
- Produces: `func (m Model) canMaximizeLeft() bool`; `case "t"`.

- [ ] **Step 1: Write failing tests** (drive `m.Update(tea.KeyMsg{...})` with `t`):
  - focus `panelBranches`, tall, `filesView==nil` → after `t`, `leftMax==panelBranches`; after another `t`, `leftMax==panelCount`.
  - focus `panelCommits` → `t` leaves `leftMax==panelCount`.
  - `filesView != nil` → `t` leaves `leftMax==panelCount`.
  - narrow terminal (`width=30`) → `t` is a no-op.
- [ ] **Step 2:** Run, verify fail.
- [ ] **Step 3:** Implement:

```go
// canMaximizeLeft reports whether t can pin the focused panel: focus is a
// small left-column panel and no full-screen surface owns the area.
func (m Model) canMaximizeLeft() bool {
	if m.filesView != nil {
		return false
	}
	return slices.Contains(m.leftColumnPanels(), m.focus)
}
```
  Key handler (next to `case "z"`):

```go
case "t": // toggle maximize of the focused left-column panel
	if m.canMaximizeLeft() {
		if m.leftMax == m.focus {
			m.leftMax = panelCount
		} else {
			m.leftMax = m.focus
		}
	}
	return m, nil
```
  (`slices` is already imported in the tui package via other files; add the
  import to model.go if absent.) Note: `stashView` keeps Staged a left panel, so
  it is intentionally not excluded; only `filesView` (which removes the small
  left panels) blocks.
- [ ] **Step 4:** Run tests, verify pass.
- [ ] **Step 5:** Commit: `feat(tui): t toggles maximize of the focused left panel`.

---

### Task 6: `ctrl+←/→` re-pins within the slot's tab group

**Files:**
- Modify: `internal/tui/model.go` (`case "ctrl+left", "ctrl+right"`, ~line 716)
- Test: `internal/tui/maximize_left_test.go`

**Interfaces:**
- Consumes: `leftMax`, existing tab-cycle logic.

- [ ] **Step 1: Write failing tests:**
  - pinned top slot: `leftMax=activeLeftTab=panelBranches`, focus=panelBranches; `ctrl+right` → `activeLeftTab` advances (panelRemotes) AND `leftMax==panelRemotes`, `focus==panelRemotes`.
  - pinned middle slot: `leftMax=panelFiles`, `activeFilesTab=panelFiles`, focus=panelFiles; `ctrl+right` → `activeFilesTab==panelTags`, `leftMax==panelTags`, `focus==panelTags`.
  - pinned Staged: `leftMax=panelStaged`, focus=panelStaged; `ctrl+right` → no change (`leftMax==panelStaged`, focus==panelStaged, activeLeftTab unchanged).
- [ ] **Step 2:** Run, verify fail.
- [ ] **Step 3:** Implement. In the `ctrl+left/right` case:
  - Add an early guard: `if m.leftMax == panelStaged { return m, nil }` (Staged
    has no tab group; don't move focus to a hidden top tab).
  - After the existing block sets `m.focus` (both the Files/Tags branch and the
    left-tab branch), if `m.leftMax != panelCount` set `m.leftMax = m.focus`.
    Place this so it covers both branches (e.g. just before the `return`).
- [ ] **Step 4:** Run tests, verify pass.
- [ ] **Step 5:** Commit: `feat(tui): ctrl+arrows re-pin the maximized tab slot`.

---

### Task 7: discoverability — footer + help

**Files:**
- Modify: `internal/tui/footer.go` (add a `t` entry near the `z` view entry)
- Modify: `internal/tui/help.go` (add `t` lines where `z` is listed for left panels)
- Test: `internal/tui/footer_test.go` / `internal/tui/help_test.go` if they assert specific entries; else rely on existing tests compiling.

- [ ] **Step 1:** Inspect the `z` footer entry (`{"view", "z", "[z] view", Model.opsIdle, scopeGlobal}`) and help `r("z", ...)` sites. Decide scope: `t` is meaningful only on left panels — use a guard predicate `Model.canMaximizeLeft` so the footer hint only shows when focus is a left panel.
- [ ] **Step 2:** Add footer entry: `{"maximize", "t", "[t] max", Model.canMaximizeLeft, scopeGlobal}` (verify the footer entry tuple shape + that a per-binding predicate is supported; match the existing field names). Keep the label `[t] max` short.
- [ ] **Step 3:** Add help lines: `r("t", "maximize the focused left panel to fill the left column")` in the Branches/Worktrees/Files/Staged/Tags help sections (wherever `z` appears for those panels). Do NOT add it to the Commits help section.
- [ ] **Step 4:** Run `go test ./internal/tui/...`, fix any help/footer assertion tests.
- [ ] **Step 5:** Commit: `docs(tui): advertise t maximize in footer + help`.

---

### Task 8: changelog + final verification

**Files:**
- Modify: `CHANGELOG.md`

- [ ] **Step 1:** Add a CHANGELOG entry under the current unreleased section:
  `- TUI: \`t\` maximizes the focused left-column panel (Branches/Remotes/Worktrees, Files/Tags, or Staged) to fill the whole left column; \`t\` again restores the split.`
- [ ] **Step 2:** `gofmt -l` the changed Go files (NOT CHANGELOG.md); `go vet ./internal/tui/...`.
- [ ] **Step 3:** `./test.sh race` (full suite).
- [ ] **Step 4:** Commit: `docs: changelog for maximize-left-panel`.

## Self-review

- Spec coverage: every spec behavior maps to a task (helper→T1, layout→T2,
  focus/return→T3, render→T4, key→T5, ctrl re-pin→T6, discoverability→T7,
  changelog/verify→T8).
- No placeholders; all code shown.
- Type consistency: `leftMax panel`, `panelCount` sentinel, `leftColumnPanels()
  []panel`, `canMaximizeLeft() bool` used consistently across tasks.
- Verify-in-code flags (resolve during execution): exact footer entry tuple
  shape + whether per-binding predicates are supported (T7); exact constructor
  name for the Model (`New` vs `initialModel`) (T1); confirm `slices` import in
  model.go (T5); confirm the prior left-panel label strings to reproduce in
  `leftPanelLabel` (T4).
