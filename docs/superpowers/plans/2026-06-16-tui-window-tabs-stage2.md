# TUI Tabbed Branches/Worktrees (Stage 2) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Merge the Branches and Worktrees panels into one tabbed left-column slot — `Ctrl+←/→` switches the active tab and focuses it; each tab keeps its own selection/sort/filter.

**Architecture:** Add a `Model.activeLeftTab panel` (panelBranches or panelWorktrees). The left column collapses from three stacked boxes to **two** — the tabbed slot (renders whichever tab is active) over Status. The tab bar lives in the panel's existing label line as ANSI-free text (`[Branches] Worktrees` / `Branches [Worktrees]`, active in brackets). The inactive tab is hidden (`layout()` gives it `boxH=0`) but its panel state (selection/sort/filter/marks) is untouched — the tab only chooses which renders. Focus walks a computed `focusOrder()` that skips the hidden tab.

**Tech Stack:** Go 1.26, Bubble Tea (value-receiver `Model`), lipgloss.

**Spec:** [`docs/superpowers/specs/2026-06-16-tui-window-framework-design.md`](../specs/2026-06-16-tui-window-framework-design.md) §"Stage 2 — Tabbed Branches/Worktrees". Stage 1a (the window primitive) is merged. Stage 3 (Files/Staged split) re-splits the left column afterward.

---

## File structure

| File | Responsibility | Action |
|---|---|---|
| `internal/tui/model.go` | `activeLeftTab` field + `New` init; `focusOrder`/`nextInOrder`/`leftReturnTarget` helpers; `tab`/`shift+tab`/`ctrl+left`/`ctrl+right`/`left` dispatch | Modify |
| `internal/tui/viewstate.go` | `layout()` left column → tabbed slot + Status (2 boxes) | Modify |
| `internal/tui/view.go` | `tabBarLabel` helper; `renderInterface` left column renders the tabbed slot | Modify |
| `internal/tui/model_test.go` | add `ctrl+left`/`ctrl+right` to the `keyMsg` helper | Modify |
| `internal/tui/help.go`, `internal/tui/footer.go` | advertise `ctrl+←/→` (switch tab) | Modify |
| `CHANGELOG.md`, `README.md` | document the tabbed slot | Modify |

**Conventions:** `internal/tui` must not import `internal/git`. `New(svc)` is safe with `nil` (existing tests use it). New TUI keys need: dispatch arm + footer binding + help row (gated by `TestHelpFooterCoverage`). Run `./test.sh race` before merging. Per-panel state (`sel`, `sortModes`, `dispModes`, filter, marks) is keyed by the `panel` enum and is unchanged — both tabs keep their own.

---

### Task 1: `activeLeftTab` field + focus-order helpers + tab/shift+tab walk the order

**Files:**
- Modify: `internal/tui/model.go` (struct fields near `focus`/`lastLeftPanel`; `New`; the `tab`/`shift+tab` cases ~`:505-510`)
- Test: `internal/tui/nav_test.go`

**Context:** Today `tab` does `m.focus = (m.focus + 1) % panelCount` over all four panels. With the tab slot, the inactive Branches/Worktrees panel must not be focusable, so focus walks a computed order.

- [ ] **Step 1: Write the failing test**

Add to `internal/tui/nav_test.go`:

```go
func TestTabCyclesActiveTabStatusCommits(t *testing.T) {
	m := New(nil)
	m.width, m.height = 80, 24
	m.focus = panelBranches // the active tab
	var got []panel
	for i := 0; i < 3; i++ {
		u, _ := m.Update(keyMsg("tab"))
		m = u.(Model)
		got = append(got, m.focus)
	}
	want := []panel{panelStatus, panelCommits, panelBranches}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("tab walk[%d] = %v, want %v (full: %v)", i, got[i], want[i], got)
		}
	}
}

func TestTabNeverFocusesInactiveTab(t *testing.T) {
	m := New(nil)
	m.width, m.height = 80, 24
	m.activeLeftTab = panelBranches
	m.focus = panelBranches
	for i := 0; i < 8; i++ {
		u, _ := m.Update(keyMsg("tab"))
		m = u.(Model)
		if m.focus == panelWorktrees {
			t.Fatal("tab focused the inactive Worktrees tab")
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui/ -run 'TestTabCyclesActiveTabStatusCommits|TestTabNeverFocusesInactiveTab'`
Expected: FAIL — `activeLeftTab` undefined / tab still cycles through Worktrees.

- [ ] **Step 3: Write minimal implementation**

In `model.go`, add the field next to `lastLeftPanel`:

```go
	focus         panel
	lastLeftPanel panel // ←'s return target; zero value = panelBranches
	activeLeftTab panel // which of Branches/Worktrees shows in the shared left tab slot; zero value = panelBranches
```

In `New`, no map needed (zero value `panelBranches` is correct), but set it explicitly for clarity:

```go
	return Model{
		svc:           svc,
		feed:          svc.CommitFeed(),
		loading:       true,
		sel:           map[panel]int{},
		sortModes:     map[panel]sortMode{panelBranches: sortDateDesc},
		dispModes:     map[panel]dispMode{},
		hscroll:       map[panel]int{},
		activeLeftTab: panelBranches,
	}
```

Add the helpers (near `rememberLeftFocus`):

```go
// focusOrder is the top-to-bottom sequence of focusable panels: the active
// Branches/Worktrees tab (the inactive one is not focusable), then Status, then
// Commits. tab/shift+tab walk this. Stage 3 inserts Files/Staged here.
func (m Model) focusOrder() []panel {
	return []panel{m.activeLeftTab, panelStatus, panelCommits}
}

// nextInOrder returns the panel dir steps from cur within order (wrapping). If
// cur is not in order (e.g. focus left on a now-hidden tab), it returns the
// first entry.
func nextInOrder(order []panel, cur panel, dir int) panel {
	for i, p := range order {
		if p == cur {
			n := len(order)
			return order[((i+dir)%n+n)%n]
		}
	}
	return order[0]
}
```

Replace the `tab`/`shift+tab` cases:

```go
		case "tab":
			m = m.rememberLeftFocus()
			m.focus = nextInOrder(m.focusOrder(), m.focus, +1)
		case "shift+tab":
			m = m.rememberLeftFocus()
			m.focus = nextInOrder(m.focusOrder(), m.focus, -1)
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tui/ -run 'TestTabCyclesActiveTabStatusCommits|TestTabNeverFocusesInactiveTab'`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/model.go internal/tui/nav_test.go
git commit -m "feat(tui): activeLeftTab field; tab/shift+tab walk the focus order"
```

---

### Task 2: `layout()` renders a tabbed left column (two boxes)

**Files:**
- Modify: `internal/tui/viewstate.go` (`layout()` ~`:61-81`)
- Test: `internal/tui/fit_test.go`

**Context:** Today the left column is three boxes (Branches/Worktrees/Status) when `bodyH>=9`, else two (Branches/Status). With the tab slot it is always two: the active tab over Status. The inactive tab gets no `boxH` entry (0 ⇒ hidden everywhere, since `panelAt`/render skip `boxH<=0`).

- [ ] **Step 1: Write the failing test**

```go
func TestLayoutTabbedLeftColumn(t *testing.T) {
	m := New(nil)
	m.width, m.height = 80, 24
	g := m.layout()
	if g.boxH[panelBranches] <= 0 {
		t.Error("active Branches tab box missing")
	}
	if g.boxH[panelStatus] <= 0 {
		t.Error("Status box missing")
	}
	if g.boxH[panelWorktrees] != 0 {
		t.Errorf("inactive Worktrees tab must be hidden, got boxH %d", g.boxH[panelWorktrees])
	}
	// Status sits directly below the tab slot.
	if g.pos[panelStatus].y != g.pos[panelBranches].y+g.boxH[panelBranches] {
		t.Error("Status is not positioned directly below the tab slot")
	}
	// Switching the tab moves the slot to Worktrees.
	m.activeLeftTab = panelWorktrees
	g = m.layout()
	if g.boxH[panelWorktrees] <= 0 || g.boxH[panelBranches] != 0 {
		t.Error("switching activeLeftTab did not move the visible box")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui/ -run TestLayoutTabbedLeftColumn`
Expected: FAIL — both Branches and Worktrees still get boxes.

- [ ] **Step 3: Write minimal implementation**

Replace the left-column block in `layout()` (the `if bodyH >= 9 { … } else { … }` that sets Branches/Worktrees/Status) with:

```go
	// Left column: the Branches/Worktrees tab slot over Status (two boxes).
	tabH := bodyH / 2
	g.boxH[m.activeLeftTab] = tabH
	g.boxH[panelStatus] = bodyH - tabH
	g.pos[m.activeLeftTab] = point{0, 1}
	g.pos[panelStatus] = point{0, 1 + tabH}
```

Leave the Commits assignment that follows (`g.boxH[panelCommits] = bodyH; g.pos[panelCommits] = point{leftW, 1}`) and the narrow `w < 40` early-return unchanged.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tui/ -run 'TestLayoutTabbedLeftColumn|TestFit'`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/viewstate.go internal/tui/fit_test.go
git commit -m "feat(tui): layout collapses Branches/Worktrees into one tabbed left slot"
```

---

### Task 3: Render the tab bar + the active tab in the slot

**Files:**
- Modify: `internal/tui/view.go` (`renderInterface` left column ~`:267-282`; add `tabBarLabel`)
- Test: `internal/tui/fit_test.go`

**Context:** The tab bar is the active panel's label line: `[Branches] Worktrees` when Branches is active, `Branches [Worktrees]` when Worktrees is. Plain ASCII so `renderPanel`'s `truncate` is safe; `panelLabel` still appends the active tab's sort/filter decorations. The inactive tab is not rendered.

- [ ] **Step 1: Write the failing test**

```go
func TestRenderShowsActiveTabBar(t *testing.T) {
	m := New(nil)
	m.width, m.height = 80, 24
	out := m.renderInterface()
	if !strings.Contains(out, "[Branches] Worktrees") {
		t.Errorf("tab bar (Branches active) missing:\n%s", out)
	}
	m.activeLeftTab = panelWorktrees
	out = m.renderInterface()
	if !strings.Contains(out, "Branches [Worktrees]") {
		t.Errorf("tab bar (Worktrees active) missing:\n%s", out)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui/ -run TestRenderShowsActiveTabBar`
Expected: FAIL — the old left column renders "Branches"/"Worktrees"/"Status" separately.

- [ ] **Step 3: Write minimal implementation**

Add `tabBarLabel` to `view.go`:

```go
// tabBarLabel is the shared left-slot header: both tab names with the active
// one bracketed. Plain ASCII so renderPanel's truncate stays safe.
func tabBarLabel(active panel) string {
	if active == panelWorktrees {
		return "Branches [Worktrees]"
	}
	return "[Branches] Worktrees"
}
```

In `renderInterface`, replace the left-column `switch` (the `case m.filesView != nil` / `case g.boxH[panelWorktrees] > 0` / `default` block that builds `left`) with:

```go
	var left string
	if m.filesView != nil {
		left = m.renderFilesView(g.leftW, g.bodyH)
	} else {
		active := m.activeLeftTab
		atRows, _ := m.panelView(active)
		stRows, _ := m.panelView(panelStatus)
		left = lipgloss.JoinVertical(lipgloss.Left,
			m.renderPanel(active, m.panelLabel(active, tabBarLabel(active)), atRows, g.leftW, g.boxH[active]),
			m.renderPanel(panelStatus, m.panelLabel(panelStatus, "Status"), stRows, g.leftW, g.boxH[panelStatus]),
		)
	}
```

Remove the now-unused `brRows`/`wtRows` locals if they were declared above the switch (keep `cmRows` for the right column).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tui/ -run 'TestRenderShowsActiveTabBar|TestFit'`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/view.go internal/tui/fit_test.go
git commit -m "feat(tui): render the Branches/Worktrees tab bar + active tab in the slot"
```

---

### Task 4: `Ctrl+←/→` switches the active tab and focuses it

**Files:**
- Modify: `internal/tui/model_test.go` (`keyMsg` helper — add `ctrl+left`/`ctrl+right`)
- Modify: `internal/tui/model.go` (new dispatch cases)
- Test: `internal/tui/nav_test.go`

- [ ] **Step 1: Write the failing test**

First extend the `keyMsg` helper in `model_test.go` (add to its switch, before `default`):

```go
	case "ctrl+left":
		return tea.KeyMsg{Type: tea.KeyCtrlLeft}
	case "ctrl+right":
		return tea.KeyMsg{Type: tea.KeyCtrlRight}
```

Then add to `nav_test.go`:

```go
func TestCtrlArrowSwitchesAndFocusesTab(t *testing.T) {
	m := New(nil)
	m.width, m.height = 80, 24
	m.focus = panelBranches
	if m.activeLeftTab != panelBranches {
		t.Fatal("default active tab should be Branches")
	}
	u, _ := m.Update(keyMsg("ctrl+right"))
	mm := u.(Model)
	if mm.activeLeftTab != panelWorktrees {
		t.Errorf("ctrl+right: active tab = %v, want Worktrees", mm.activeLeftTab)
	}
	if mm.focus != panelWorktrees {
		t.Errorf("ctrl+right: focus = %v, want Worktrees (focus follows the tab)", mm.focus)
	}
	u2, _ := mm.Update(keyMsg("ctrl+left"))
	if u2.(Model).activeLeftTab != panelBranches {
		t.Error("ctrl+left should switch back to Branches")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui/ -run TestCtrlArrowSwitchesAndFocusesTab`
Expected: FAIL — ctrl+left/right not handled (active tab unchanged).

- [ ] **Step 3: Write minimal implementation**

Add to the base-grid key switch in `model.go` (near the `tab` cases):

```go
		case "ctrl+left", "ctrl+right":
			// Two tabs, so either direction toggles. Switch and focus it.
			if m.activeLeftTab == panelBranches {
				m.activeLeftTab = panelWorktrees
			} else {
				m.activeLeftTab = panelBranches
			}
			m.focus = m.activeLeftTab
			m.lastLeftPanel = m.activeLeftTab
			return m, nil
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tui/ -run TestCtrlArrowSwitchesAndFocusesTab`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/model.go internal/tui/model_test.go internal/tui/nav_test.go
git commit -m "feat(tui): ctrl+left/right switches and focuses the Branches/Worktrees tab"
```

---

### Task 5: `←` never focuses a hidden tab

**Files:**
- Modify: `internal/tui/model.go` (`left` case + new `leftReturnTarget` helper)
- Test: `internal/tui/nav_test.go`

**Context:** `←` returns focus to `lastLeftPanel`. If that records the Branches tab but the user has since switched the slot to Worktrees, `←` would focus the now-hidden Branches panel. Guard it.

- [ ] **Step 1: Write the failing test**

```go
func TestLeftDoesNotFocusHiddenTab(t *testing.T) {
	m := New(nil)
	m.width, m.height = 80, 24
	m.focus = panelBranches
	m.lastLeftPanel = panelBranches
	// Switch the visible tab to Worktrees, then go to Commits and back.
	m.activeLeftTab = panelWorktrees
	m.focus = panelCommits
	u, _ := m.Update(keyMsg("left"))
	got := u.(Model).focus
	if got == panelBranches {
		t.Fatalf("← focused the hidden Branches tab; want the active tab or Status, got %v", got)
	}
	if got != panelWorktrees && got != panelStatus {
		t.Fatalf("← focus = %v, want the active Worktrees tab or Status", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui/ -run TestLeftDoesNotFocusHiddenTab`
Expected: FAIL — `←` focuses the hidden Branches panel.

- [ ] **Step 3: Write minimal implementation**

Add the helper near `rememberLeftFocus`:

```go
// leftReturnTarget is where ← lands: the remembered left panel, except a stale
// pointer at the now-inactive Branches/Worktrees tab is redirected to the
// active tab (which is the one actually visible).
func (m Model) leftReturnTarget() panel {
	p := m.lastLeftPanel
	if (p == panelBranches || p == panelWorktrees) && p != m.activeLeftTab {
		return m.activeLeftTab
	}
	return p
}
```

Update the `left` case:

```go
		case "left":
			// No-op when already in the left column, and when the narrow
			// layout has no left column to focus.
			if m.focus == panelCommits && (m.width <= 0 || m.width >= 40) {
				m.focus = m.leftReturnTarget()
			}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tui/ -run TestLeftDoesNotFocusHiddenTab`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/model.go internal/tui/nav_test.go
git commit -m "fix(tui): left-arrow never returns focus to a hidden tab"
```

---

### Task 6: Help + footer + docs

**Files:**
- Modify: `internal/tui/help.go`, `internal/tui/footer.go`
- Modify: `CHANGELOG.md`, `README.md`
- Test: `internal/tui/help_test.go` (coverage gate)

**Context:** `TestHelpFooterCoverage` requires every footer-binding key to have a help row whose first field matches. Add a `ctrl+←/→` footer binding and a matching help row. The footer-binding `key` and the help row key column must be byte-identical — use `ctrl+←/→` in both.

- [ ] **Step 1: Write the failing test**

Add to `help_test.go`:

```go
func TestHelpDocumentsTabSwitch(t *testing.T) {
	var b strings.Builder
	for _, l := range helpContent() {
		b.WriteString(l.text)
		b.WriteString("\n")
	}
	if !strings.Contains(b.String(), "switch the Branches/Worktrees tab") {
		t.Error("help does not document the ctrl+arrow tab switch")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui/ -run 'TestHelpDocumentsTabSwitch|TestHelpFooterCoverage'`
Expected: FAIL — no help row; once the footer binding is added, `TestHelpFooterCoverage` also fails until the help row exists.

- [ ] **Step 3: Write minimal implementation**

In `help.go`, add a row in the Global section (near the `←/→` row, line ~27):

```go
		r("ctrl+←/→", "switch the Branches/Worktrees tab"),
```

In `footer.go` `globalBindings`, add (after the `tab` binding):

```go
	{"ctrl+←/→", "[ctrl+←/→] tab", func(Model) bool { return true }},
```

> The footer-binding `key` is `ctrl+←/→`; `TestHelpFooterCoverage` splits the help key column on `/`, so the help row key `ctrl+←/→` must match. Keep both identical. The `when` is always-true (the tab is always switchable); like `tab`, it survives the running-op footer collapse.

In `CHANGELOG.md`, under the Unreleased "Added"/"TUI window display modes" area add a bullet:

```markdown
- TUI: the Branches and Worktrees panels are now one tabbed left-column slot —
  `ctrl+←/→` switches the active tab (and focuses it); each tab keeps its own
  selection, sort, and filter.
```

In `README.md`, add a keybinding row near the `tab` entry:

```markdown
| `ctrl+←/→` | switch the shared **Branches / Worktrees** left-column tab (and focus it) |
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tui/ -run 'TestHelp|TestFooter'`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/help.go internal/tui/footer.go internal/tui/help_test.go CHANGELOG.md README.md
git commit -m "docs(tui): advertise ctrl+arrow tab switch in help, footer, changelog, readme"
```

---

### Task 7: Full-suite verification

**Files:** none (verification only)

- [ ] **Step 1: Run the staged suite with the race detector**

Run: `./test.sh race`
Expected: vet+gofmt clean, all unit + e2e pass. Pre-existing focus/nav/mouse/fit tests must stay green (the tab slot reuses per-panel state).

- [ ] **Step 2: Build + manual smoke (document result)**

Run: `go build ./cmd/gg`
Expected: builds clean. Manually: open the TUI; confirm the left column shows a `[Branches] Worktrees` tab bar over Status; `ctrl+→`/`ctrl+←` flip the tab and focus it; `tab` cycles tab→Status→Commits (never the hidden tab); each tab remembers its own selection/sort/filter.

- [ ] **Step 3: Commit (only if a fix was needed)** — otherwise nothing to commit.

---

## Self-review notes (for the executor)

- **Spec coverage:** `activeLeftTab` + tab bar (Task 1/3), `Ctrl+←/→` switch+focus (Task 4), distinct per-tab state (preserved by keying on the `panel` enum — no code change needed), focus-order skip of the hidden tab (Task 1) and the `←` guard (Task 5), geometry 2-box left column (Task 2). **Deferred (note, not built):** clicking the inactive tab name to switch (mouse tab-switch) — `Ctrl+←/→` is the primary mechanism the user asked for; mouse-tab-switch can be a small follow-up. The short-terminal `bodyH<9` path is now the same two-box layout as the tall path (Task 2 unifies them), so no separate collapse rule is needed at this stage; Stage 3 will revisit collapse when it adds the Staged box.
- **Type consistency:** `activeLeftTab`, `focusOrder()`, `nextInOrder()`, `leftReturnTarget()`, `tabBarLabel()` are defined once and used consistently. `panelCount` and the per-panel maps are unchanged (both tabs remain real panels).
- **Risk — `ctrl+left/right` terminal support:** most modern terminals send these distinctly; if a terminal doesn't, the tab still switches via repeated `tab` landing on it is NOT possible (tab skips the inactive one) — so `ctrl+←/→` is the only switch path. Acceptable per the user's explicit request; mouse-tab-switch (deferred) would be the fallback.
- **Risk — narrow layout:** when `w<40` the layout early-returns a single Commits column (no left column, no tab slot); `ctrl+←/→` still toggles `activeLeftTab` harmlessly (it only shows once the terminal is wide enough). Confirm `TestFit`/reflow tests stay green.
