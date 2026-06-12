# Contextual Footer Key Hints Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the static TUI footer with a context-sensitive one that shows only the keys available given the focused panel, the selected row, and the current mode.

**Architecture:** A declarative binding registry (`footer.go`) is filtered per render by availability predicates (`avail.go`) that are shared with `Update`'s key dispatch — so the footer can never advertise a dead key. The existing key dispatch in `model.go` is rewired to call the predicates (which also deliberately tightens three cases that today spawn operations git rejects). The help drift guard switches from regexing the deleted `footerText` const to iterating the registry.

**Tech Stack:** Go 1.26, Bubble Tea v1 (value-receiver `Model`), table-driven Go tests (no real git needed — plain `Model` fixtures).

**Spec:** `docs/superpowers/specs/2026-06-12-footer-hints-design.md` — read it first; the "Governing rule" and "Deliberate behavior tightening" sections explain *why* the code below looks the way it does.

**Worktree/branch:** already created — `/mnt/t/others/gigagit/.claude/worktrees/footer-hints` on `feat/footer-hints`. Run everything from there.

---

## Codebase orientation (read before Task 1)

- `internal/tui/model.go` — root `Model` (value receiver). Key dispatch is one big `switch msg.String()` inside `Update` (~line 177). Relevant state: `focus panel`, `sel map[panel]int`, `running`, `loading`, `mark *markState`, `filterTyping`, `branches []model.Branch` (`IsHead` flags the checked-out branch), `worktrees []model.Worktree`, `currentWorktree string`, `status model.WorkingTreeStatus` (`.Branch`).
- `m.backingIndex(p panel) (int, bool)` (`viewstate.go:293`) resolves panel p's selection to an index into its backing slice through sort/filter transforms; ok=false when the visible list is empty or selection out of range.
- `m.listFor(p).Key(bi)` gives a row's stable identity (used by the mark).
- `m.markAlive()` (`mark.go`) — the marked row still exists in its panel.
- `internal/tui/view.go:161` — `const footerText` rendered at line 169 via `truncate(footerText, g.w)`. This const is deleted by Task 3.
- `internal/tui/help_test.go` — `TestHelpFooterCoverage` regexes `[x]` keys out of `footerText`; rewritten in Task 3.
- Test helper `keyMsg(s string)` in `model_test.go` builds `tea.KeyMsg` from a name ("enter", "tab", runes…).
- Run tests with `go test ./internal/tui/`. Final gate: `./test.sh race` from the worktree root.

---

### Task 1: Shared availability predicates + Update rewiring (with honesty tests)

**Files:**
- Create: `internal/tui/avail.go`
- Create: `internal/tui/footer_test.go` (fixture + honesty tests; footer rendering tests arrive in Task 2)
- Modify: `internal/tui/model.go` (the `case "s"/"b"/"B"/"w"/"W"/"d"/"enter"/"m"` arms, ~lines 193–302)

- [ ] **Step 1: Write the failing honesty tests**

Create `internal/tui/footer_test.go`:

```go
package tui

import (
	"testing"

	"github.com/gigagit/gg/internal/model"
)

// footerModel is an idle fixture: Branches focused (zero value), two branches
// (main is HEAD, selected by default), two worktrees ("/repo" is current,
// selected by default). Every panel except Status/Commits has rows.
func footerModel() Model {
	return Model{
		width:     120,
		height:    40,
		sel:       map[panel]int{},
		sortModes: map[panel]sortMode{},
		status:    model.WorkingTreeStatus{Branch: "main"},
		branches: []model.Branch{
			{Name: "main", IsHead: true},
			{Name: "feat/x"},
		},
		worktrees: []model.Worktree{
			{Path: "/repo", Branch: "main"},
			{Path: "/repo/wt-x", Branch: "feat/x"},
		},
		currentWorktree: "/repo",
	}
}

// The honesty tests pin the predicate-sharing contract: when a shared
// predicate is false, the key must be a complete no-op (no op spawned, no
// state change) — these three used to start operations git then rejects.

func TestSwitchKeyNoOpOnHeadBranch(t *testing.T) {
	m := footerModel() // sel 0 = main, the checked-out branch
	u, cmd := m.Update(keyMsg("s"))
	mm := u.(Model)
	if cmd != nil || mm.running {
		t.Fatal("s on the checked-out branch must be a no-op")
	}
}

func TestDeleteKeyNoOpOnHeadBranch(t *testing.T) {
	m := footerModel()
	u, cmd := m.Update(keyMsg("d"))
	mm := u.(Model)
	if cmd != nil || mm.running {
		t.Fatal("d on the checked-out branch must be a no-op")
	}
}

func TestDeleteKeyNoOpOnCurrentWorktree(t *testing.T) {
	m := footerModel()
	m.focus = panelWorktrees // sel 0 = /repo = currentWorktree
	u, cmd := m.Update(keyMsg("d"))
	mm := u.(Model)
	if cmd != nil || mm.running {
		t.Fatal("d on the current worktree must be a no-op")
	}
}

func TestEnterNoOpOnCurrentWorktree(t *testing.T) {
	m := footerModel()
	m.focus = panelWorktrees
	u, cmd := m.Update(keyMsg("enter"))
	mm := u.(Model)
	if cmd != nil || mm.loading {
		t.Fatal("enter on the current worktree must not re-root")
	}
}

func TestMarkKeyNoOpWhileRunning(t *testing.T) {
	m := footerModel()
	m.running = true
	u, _ := m.Update(keyMsg("m"))
	if u.(Model).mark != nil {
		t.Fatal("m while an op runs must not mark")
	}
}

func TestPredicatesOnSelectableRows(t *testing.T) {
	m := footerModel()
	m.sel[panelBranches] = 1 // feat/x (not HEAD)
	if !m.canSwitchBranch() || !m.canDeleteBranch() || !m.canOpenBranchPopup() || !m.canOpenWorktreePopup() {
		t.Error("branch predicates must hold on an idle model with a non-HEAD row selected")
	}
	m.sel[panelWorktrees] = 1 // /repo/wt-x (not current)
	if !m.canDeleteWorktree() || !m.canEnterWorktree() {
		t.Error("worktree predicates must hold on a non-current worktree row")
	}
	if !m.canMark() {
		t.Error("canMark must hold when the focused panel has a selected row")
	}
	m.running = true
	if m.opsIdle() || m.canSwitchBranch() || m.canMark() {
		t.Error("all op predicates must be false while running")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tui/ -run 'TestSwitchKeyNoOp|TestDeleteKeyNoOp|TestEnterNoOp|TestMarkKeyNoOp|TestPredicates' -v`

Expected: compile FAILURE — `m.canSwitchBranch undefined` etc. (the no-op tests would also fail behaviorally: today `s`/`d` on a HEAD row spawn an op).

- [ ] **Step 3: Create the predicates**

Create `internal/tui/avail.go`:

```go
package tui

import "github.com/gigagit/gg/internal/model"

// Availability predicates shared by Update's key dispatch (model.go) and the
// footer binding registry (footer.go). Sharing them keeps the footer honest:
// a key is advertised only through the same check that gates its handler.
// Footer bindings may add a focus check on top (stricter is fine); they must
// never be looser than the Update gate.

// opsIdle reports whether a new operation may start: nothing running and the
// initial load finished.
func (m Model) opsIdle() bool {
	return !m.running && !m.loading
}

// selectedBranch resolves the Branches panel selection through the view
// transforms. ok is false when the visible list is empty.
func (m Model) selectedBranch() (model.Branch, bool) {
	bi, ok := m.backingIndex(panelBranches)
	if !ok {
		return model.Branch{}, false
	}
	return m.branches[bi], true
}

// selectedWorktree resolves the Worktrees panel selection.
func (m Model) selectedWorktree() (model.Worktree, bool) {
	bi, ok := m.backingIndex(panelWorktrees)
	if !ok {
		return model.Worktree{}, false
	}
	return m.worktrees[bi], true
}

// canSwitchBranch gates s: SmartSwitch to the selected branch. Switching to
// the branch already checked out here would be a no-op git rejects.
func (m Model) canSwitchBranch() bool {
	b, ok := m.selectedBranch()
	return m.opsIdle() && ok && !b.IsHead
}

// canOpenBranchPopup gates b/B: a new branch from the selected one.
func (m Model) canOpenBranchPopup() bool {
	_, ok := m.selectedBranch()
	return m.opsIdle() && ok
}

// canOpenWorktreePopup gates w/W: a worktree from the selected branch.
func (m Model) canOpenWorktreePopup() bool {
	_, ok := m.selectedBranch()
	return m.opsIdle() && ok
}

// canDeleteBranch gates d on Branches: git refuses deleting the checked-out
// branch, so don't offer it.
func (m Model) canDeleteBranch() bool {
	b, ok := m.selectedBranch()
	return m.opsIdle() && ok && !b.IsHead
}

// canDeleteWorktree gates d on Worktrees: git refuses removing the current
// working tree, so don't offer it.
func (m Model) canDeleteWorktree() bool {
	wt, ok := m.selectedWorktree()
	return m.opsIdle() && ok && wt.Path != m.currentWorktree
}

// canEnterWorktree gates enter on Worktrees: re-root into another worktree.
func (m Model) canEnterWorktree() bool {
	wt, ok := m.selectedWorktree()
	return m.opsIdle() && ok && wt.Path != "" && wt.Path != m.currentWorktree
}

// canMark gates m: mark/unmark/pair needs a resolvable row in the focused
// panel (handleMarkKey re-checks and routes the three sub-cases).
func (m Model) canMark() bool {
	_, ok := m.backingIndex(m.focus)
	return m.opsIdle() && ok
}

// markOnFocusedPanel reports a live mark belonging to the focused panel.
func (m Model) markOnFocusedPanel() bool {
	return m.mark != nil && m.mark.panel == m.focus && m.markAlive()
}

// cursorOnMark reports whether the focused panel's selection is the marked row.
func (m Model) cursorOnMark() bool {
	if m.mark == nil {
		return false
	}
	bi, ok := m.backingIndex(m.focus)
	return ok && m.listFor(m.focus).Key(bi) == m.mark.key
}
```

(`markOnFocusedPanel`/`cursorOnMark` are consumed by the Task 2 registry —
they're availability helpers, so they live here.)

- [ ] **Step 4: Rewire Update's key arms to the predicates**

In `internal/tui/model.go`, replace these `case` arms inside `Update`'s key
switch (current code shown in the orientation; exact old bodies are at lines
193–302). The surrounding cases (`q`, `r`, `p`, `P`, `S`, `u`, `tab`,
`pgup/pgdown`, `o`, `/`, `R`, `,`, `?`, `esc`, `up/down`) stay untouched.

```go
		case "s":
			if m.canSwitchBranch() {
				b, _ := m.selectedBranch()
				return m.startOp(engine.SmartSwitch{Branch: b.Name})
			}
```

```go
		case "w": // worktree for the selected EXISTING branch
			if m.canOpenWorktreePopup() {
				if mm, ok := m.openWorktreePopup(true); ok {
					return mm, nil
				}
			}
		case "W": // worktree on a NEW branch from the selected one
			if m.canOpenWorktreePopup() {
				if mm, ok := m.openWorktreePopup(false); ok {
					return mm, nil
				}
			}
```

```go
		case "b":
			if m.focus == panelBranches && m.canOpenBranchPopup() {
				if mm, ok := m.openBranchPopup(false); ok {
					return mm, nil
				}
			}
		case "B":
			if m.focus == panelBranches && m.canOpenBranchPopup() {
				if mm, ok := m.openBranchPopup(true); ok {
					return mm, nil
				}
			}
```

```go
		case "d":
			switch m.focus {
			case panelWorktrees:
				if m.canDeleteWorktree() {
					wt, _ := m.selectedWorktree()
					return m.startOp(engine.RemoveWorktree{Path: wt.Path, Branch: wt.Branch})
				}
			case panelBranches:
				if m.canDeleteBranch() {
					b, _ := m.selectedBranch()
					return m.startOp(engine.DeleteBranch{Name: b.Name})
				}
			}
```

```go
		case "enter":
			if m.focus == panelWorktrees && m.canEnterWorktree() {
				wt, _ := m.selectedWorktree()
				return m.reRoot(wt.Path)
			}
```

```go
		case "m":
			if m.canMark() {
				return m.handleMarkKey()
			}
```

Behavior notes (all spec'd):
- `s`/`d` lose the inline `!m.running && !m.loading` + index plumbing; the
  predicate carries it, plus the new `!IsHead` / `!= currentWorktree` guards.
- `b`/`B` keep their explicit focus gate (footer mirrors it).
- `w`/`W` stay UN-focus-gated, exactly as before (they act on the Branches
  selection from any panel; only the footer is Branches-scoped).
- `enter` keeps its focus gate; the `target != ""`/`!= currentWorktree`
  checks move into the predicate.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/tui/ -run 'TestSwitchKeyNoOp|TestDeleteKeyNoOp|TestEnterNoOp|TestMarkKeyNoOp|TestPredicates' -v`
Expected: PASS (all 6).

- [ ] **Step 6: Run the whole TUI package**

Run: `go test ./internal/tui/`
Expected: PASS. The existing delete/switch tests select non-HEAD branches and
non-current worktrees explicitly (`branch_delete_test.go` walks the panel to
find `feat/doomed`; `worktree_delete_test.go`/`worktree_switch_test.go` skip
`m.currentWorktree`), so the tightening must not break them. If something
fails here, fix the PRODUCT or this task's wiring — do not loosen a predicate
to appease a test without checking the spec's tightening list first.

- [ ] **Step 7: Commit**

```bash
git add internal/tui/avail.go internal/tui/footer_test.go internal/tui/model.go
git commit -m "feat(tui): shared availability predicates gate s/b/w/d/enter/m"
```

---

### Task 2: Binding registry + footerLine renderer

**Files:**
- Create: `internal/tui/footer.go`
- Modify: `internal/tui/footer_test.go` (append rendering tests)

- [ ] **Step 1: Write the failing footer tests**

Append to `internal/tui/footer_test.go` (add `"strings"` to imports):

```go
func TestFooterBranchesContextNonHead(t *testing.T) {
	m := footerModel()
	m.sel[panelBranches] = 1 // feat/x
	f := m.footerLine()
	for _, want := range []string{
		"[s]witch", "[b]ranch", "[w]orktree", "[d]elete", "[m]ark",
		"•", "[p]ull", "[P]ush", "[q] quit",
	} {
		if !strings.Contains(f, want) {
			t.Errorf("footer %q must contain %q", f, want)
		}
	}
}

func TestFooterHeadBranchHidesSwitchAndDelete(t *testing.T) {
	m := footerModel() // sel 0 = main (HEAD)
	f := m.footerLine()
	if strings.Contains(f, "[s]witch") || strings.Contains(f, "[d]elete") {
		t.Errorf("HEAD branch row must not offer switch/delete: %q", f)
	}
	if !strings.Contains(f, "[b]ranch") || !strings.Contains(f, "[w]orktree") {
		t.Errorf("branch/worktree creation stays available on the HEAD row: %q", f)
	}
}

func TestFooterWorktreesContext(t *testing.T) {
	m := footerModel()
	m.focus = panelWorktrees
	m.sel[panelWorktrees] = 1 // not the current worktree
	f := m.footerLine()
	if !strings.Contains(f, "[enter] switch") || !strings.Contains(f, "[d]elete") {
		t.Errorf("other-worktree row must offer enter/delete: %q", f)
	}
	if strings.Contains(f, "[s]witch") || strings.Contains(f, "[b]ranch") {
		t.Errorf("branch actions must not show on Worktrees focus: %q", f)
	}
	m.sel[panelWorktrees] = 0 // the current worktree
	f = m.footerLine()
	if strings.Contains(f, "[enter] switch") || strings.Contains(f, "[d]elete") {
		t.Errorf("current-worktree row must not offer enter/delete: %q", f)
	}
}

func TestFooterStatusFocusHasNoContextSegment(t *testing.T) {
	m := footerModel()
	m.focus = panelStatus
	f := m.footerLine()
	if strings.Contains(f, "•") {
		t.Errorf("Status focus has no context actions, no separator: %q", f)
	}
	if !strings.HasPrefix(f, "[p]ull") {
		t.Errorf("global tail must lead when there is no context segment: %q", f)
	}
}

func TestFooterMarkStates(t *testing.T) {
	m := footerModel()
	m.sel[panelBranches] = 1 // feat/x
	if f := m.footerLine(); !strings.Contains(f, "[m]ark") || strings.Contains(f, "[m] pair") {
		t.Errorf("no mark yet: want [m]ark only, got %q", f)
	}
	u, _ := m.Update(keyMsg("m")) // mark feat/x
	m = u.(Model)
	if f := m.footerLine(); !strings.Contains(f, "[m] unmark") {
		t.Errorf("cursor on the marked row: want [m] unmark, got %q", f)
	}
	m.sel[panelBranches] = 0 // cursor to main; mark still on feat/x
	if f := m.footerLine(); !strings.Contains(f, "[m] pair") {
		t.Errorf("cursor on another row with a live mark: want [m] pair, got %q", f)
	}
}

func TestFooterRunningCollapses(t *testing.T) {
	m := footerModel()
	m.running = true
	want := "[tab] focus [?] help [q] quit"
	if f := m.footerLine(); f != want {
		t.Errorf("running footer = %q, want %q", f, want)
	}
}

func TestFooterFilterTypingOverride(t *testing.T) {
	m := footerModel()
	m.filterTyping = true
	want := "filter: type to search  [enter] keep  [esc] cancel"
	if f := m.footerLine(); f != want {
		t.Errorf("filter-typing footer = %q, want %q", f, want)
	}
}

func TestFooterEmptyPanelsHideRowActions(t *testing.T) {
	m := Model{width: 80, height: 24, sel: map[panel]int{}, sortModes: map[panel]sortMode{}}
	f := m.footerLine()
	for _, banned := range []string{"[s]witch", "[b]ranch", "[w]orktree", "[d]elete", "[m]ark", "[P]ush"} {
		if strings.Contains(f, banned) {
			t.Errorf("empty repo: %q must not appear in %q", banned, f)
		}
	}
	if !strings.Contains(f, "[p]ull") {
		t.Errorf("global tail must survive an empty repo: %q", f)
	}
}
```

Notes on the fixtures: `footerModel()` leaves `focus` at its zero value
(`panelBranches`) and `sel` defaults to row 0; the zero `sortModes` map means
`sortDefault`, so panel order equals backing order. `[P]ush` is absent in the
empty-model test because `status.Branch == ""`.

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/tui/ -run TestFooter -v`
Expected: compile FAILURE — `m.footerLine undefined`.

- [ ] **Step 3: Create the registry and renderer**

Create `internal/tui/footer.go`:

```go
package tui

import "strings"

// footerBinding is one advertised key: a canonical key name (consumed by the
// TestHelpFooterCoverage drift guard), the rendered label, and the
// availability predicate. The governing rule: the footer never shows an
// unavailable key; it may omit available ones for brevity (W, B, shift+tab,
// pgup/pgdn are usable but documented only in the ? help window). A when may
// therefore be stricter than the Update gate — never looser.
type footerBinding struct {
	key   string
	label string
	when  func(Model) bool
}

// contextBindings are the panel/row-specific actions, rendered first. The
// three m-bindings and two d-bindings have mutually exclusive predicates, so
// at most one of each key renders at a time.
var contextBindings = []footerBinding{
	{"s", "[s]witch", func(m Model) bool { return m.focus == panelBranches && m.canSwitchBranch() }},
	{"b", "[b]ranch", func(m Model) bool { return m.focus == panelBranches && m.canOpenBranchPopup() }},
	{"w", "[w]orktree", func(m Model) bool { return m.focus == panelBranches && m.canOpenWorktreePopup() }},
	{"d", "[d]elete", func(m Model) bool { return m.focus == panelBranches && m.canDeleteBranch() }},
	{"m", "[m]ark", func(m Model) bool {
		return m.focus == panelBranches && m.canMark() && !m.markOnFocusedPanel()
	}},
	{"m", "[m] unmark", func(m Model) bool {
		return m.focus == panelBranches && m.canMark() && m.markOnFocusedPanel() && m.cursorOnMark()
	}},
	{"m", "[m] pair", func(m Model) bool {
		return m.focus == panelBranches && m.canMark() && m.markOnFocusedPanel() && !m.cursorOnMark()
	}},
	{"enter", "[enter] switch", func(m Model) bool { return m.focus == panelWorktrees && m.canEnterWorktree() }},
	{"d", "[d]elete", func(m Model) bool { return m.focus == panelWorktrees && m.canDeleteWorktree() }},
}

// globalBindings are the always-relevant tail, still individually predicated
// (while an op runs everything gated on opsIdle drops out and the footer
// collapses to tab/help/quit).
var globalBindings = []footerBinding{
	{"p", "[p]ull", Model.opsIdle},
	{"P", "[P]ush", func(m Model) bool { return m.opsIdle() && m.status.Branch != "" }},
	{"S", "[S]tash", Model.opsIdle},
	{"u", "[u]ndo", Model.opsIdle},
	{"o", "[o]rder", Model.opsIdle},
	{"/", "[/]filter", Model.opsIdle},
	{"R", "[R]epo", Model.opsIdle},
	{",", "[,] settings", Model.opsIdle},
	{"tab", "[tab] focus", func(Model) bool { return true }},
	{"r", "[r] reload", func(m Model) bool { return !m.running }},
	{"?", "[?] help", func(Model) bool { return true }},
	{"q", "[q] quit", func(Model) bool { return true }},
}

// footerLine builds the context-sensitive footer: panel/row-specific actions,
// a separator, then the predicated global tail. Filter-input mode overrides
// everything because that mode captures every key.
func (m Model) footerLine() string {
	if m.filterTyping {
		return "filter: type to search  [enter] keep  [esc] cancel"
	}
	var ctx, glob []string
	for _, b := range contextBindings {
		if b.when(m) {
			ctx = append(ctx, b.label)
		}
	}
	for _, b := range globalBindings {
		if b.when(m) {
			glob = append(glob, b.label)
		}
	}
	line := strings.Join(glob, " ")
	if len(ctx) > 0 {
		line = strings.Join(ctx, " ") + "  •  " + line
	}
	return line
}
```

(`Model.opsIdle` as a method expression has exactly the `func(Model) bool`
type the field wants; spelling the closures out is fine too if the reviewer
prefers consistency.)

- [ ] **Step 4: Run to verify the new tests pass**

Run: `go test ./internal/tui/ -run TestFooter -v`
Expected: PASS (8 tests).

- [ ] **Step 5: Run the whole TUI package**

Run: `go test ./internal/tui/`
Expected: PASS — `footerLine` has no callers in the product yet, nothing else
moves.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/footer.go internal/tui/footer_test.go
git commit -m "feat(tui): contextual footer binding registry and renderer"
```

---

### Task 3: Render the contextual footer; rewrite the drift guard

**Files:**
- Modify: `internal/tui/view.go` (~lines 159–169: delete `footerText`, call `footerLine`)
- Modify: `internal/tui/help_test.go` (`TestHelpFooterCoverage`)
- Modify: `internal/tui/footer_test.go` (append the render integration test)

- [ ] **Step 1: Write the failing integration test**

Append to `internal/tui/footer_test.go` (add `"github.com/charmbracelet/x/ansi"` to imports):

```go
func TestFooterRenderedInInterface(t *testing.T) {
	m := footerModel()
	m.sel[panelBranches] = 1 // feat/x → full Branches context
	out := ansi.Strip(m.render())
	if !strings.Contains(out, "[s]witch") {
		t.Errorf("rendered interface must show the contextual footer:\n%s", out)
	}
	m.running = true
	out = ansi.Strip(m.render())
	if strings.Contains(out, "[s]witch") || strings.Contains(out, "[p]ull") {
		t.Errorf("running: gated keys must leave the rendered footer:\n%s", out)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/tui/ -run TestFooterRenderedInInterface -v`
Expected: FAIL — the static `footerText` contains `[s]witch` and `[p]ull`
unconditionally, so the first half passes but the *running* half fails (the
gated keys never leave the static footer). Both halves pass only with the
contextual footer.

- [ ] **Step 3: Swap view.go to footerLine**

In `internal/tui/view.go` delete the const block:

```go
// footerText abbreviates the global keys; TestHelpFooterCoverage enforces
// that every [x] key here has a row in helpContent.
const footerText = "[p]ull [P]ush [s]witch [b]ranch [S]tash [u]ndo [w]orktree [m]ark [d]elete [o]rder [/]filter [R]epo [,] settings  •  [tab] focus  [r] reload  [?] help  [q] quit"
```

and change the render call in `renderInterface`:

```go
	footer := truncate(m.footerLine(), g.w)
```

- [ ] **Step 4: Rewrite the drift guard**

In `internal/tui/help_test.go`, replace `TestHelpFooterCoverage` (and drop the
now-unused `"regexp"` import):

```go
// TestHelpFooterCoverage is the drift guard: every key in the footer binding
// registry (footer.go) must appear as the key column of some help row. The
// key column is the row's first whitespace-delimited field; alternates are
// /-separated (e.g. "q/ctrl+c").
func TestHelpFooterCoverage(t *testing.T) {
	var keys []string
	for _, b := range contextBindings {
		keys = append(keys, b.key)
	}
	for _, b := range globalBindings {
		keys = append(keys, b.key)
	}
	if len(keys) < 10 {
		t.Fatalf("binding registry looks broken, got keys %v", keys)
	}
	lines := helpContent()
	for _, k := range keys {
		found := false
		for _, l := range lines {
			if l.heading {
				continue
			}
			f := strings.Fields(l.text)
			if len(f) == 0 {
				continue
			}
			if f[0] == k {
				found = true
				continue
			}
			for _, alt := range strings.Split(f[0], "/") {
				if alt == k {
					found = true
				}
			}
		}
		if !found {
			t.Errorf("footer key %q has no help row (helpContent key column)", k)
		}
	}
}
```

The matching loop is byte-identical to the old one; only the key source
changed. All registry keys (`s b w d m enter p P S u o / R , tab r ? q`)
already have help rows — `enter` rows exist in the Worktrees-panel section of
`helpContent()` (help.go:38).

- [ ] **Step 5: Run the whole TUI package**

Run: `go test ./internal/tui/`
Expected: PASS, including `TestHelpFooterCoverage`,
`TestFooterRenderedInInterface`, and the fit tests (the footer is still one
truncated line, so layout invariants hold).

- [ ] **Step 6: Commit**

```bash
git add internal/tui/view.go internal/tui/help_test.go internal/tui/footer_test.go
git commit -m "feat(tui): render the contextual footer; drift guard reads the registry"
```

---

### Task 4: Docs + full gate

**Files:**
- Modify: `CHANGELOG.md` (top of `### Added` under `## [Unreleased]`)
- Modify: `README.md` (TUI keys section — find the `?` help row added by the
  help-window feature and describe the footer near the key table intro)
- Modify: `.claude/skills/adding-tui-windows/SKILL.md` (lines 44 and 85 both
  point at the deleted `footerText` const)

- [ ] **Step 1: CHANGELOG entry**

Insert under `### Added`, above `#### Mark-and-pair operations + SmartMerge`:

```markdown
#### Contextual footer
- TUI: the footer now shows only the keys that work right now — panel/row
  specific actions first (`[s]witch` hides on the checked-out branch,
  `[enter] switch`/`[d]elete` hide on the current worktree, the `m` hint
  reads mark/unmark/pair to match the mark state), then the global tail.
  While an operation runs it collapses to `[tab] [?] [q]`; filter input
  shows its own line. Availability predicates are shared between the key
  dispatch and the footer, so a shown key always works — and `s`/`d` on the
  checked-out branch or `d` on the current worktree are now clean no-ops
  instead of operations git rejects.
```

- [ ] **Step 2: README**

Locate the TUI key documentation (search for `[?]` or `help`). Add one
sentence where the keys are introduced, e.g.:

```markdown
The footer is contextual: it lists only the keys that apply to the focused
panel and selected row right now; `?` opens the full searchable reference.
```

Adjust placement to the file's actual structure — do not duplicate an
existing sentence about `?`.

- [ ] **Step 3: Update the adding-tui-windows skill**

In `.claude/skills/adding-tui-windows/SKILL.md` replace both stale pointers:

Line 44 (Keys row of the checklist table) — replace the sentence
"Footer hint: the `footerText` const in `view.go`." with:

```
Footer hint: add a `footerBinding` to `contextBindings`/`globalBindings` in `footer.go`, gated by a shared predicate from `avail.go` (the same predicate must gate the `Update` arm).
```

Line 85 (pitfalls table) — replace "The `footerText` const in `view.go`."
with:

```
The binding registry in `footer.go` (predicates in `avail.go`).
```

Keep the trailing "New global keys must also get a row in `helpContent()`
(`help.go`) — `TestHelpFooterCoverage` fails otherwise." in both places.

- [ ] **Step 4: Full gate**

Run from the worktree root: `./test.sh race`
Expected: `all green` (vet+gofmt → unit → e2e). The CLI surface did not
change, so no agentskill bump.

- [ ] **Step 5: Commit**

```bash
git add CHANGELOG.md README.md .claude/skills/adding-tui-windows/SKILL.md
git commit -m "docs: record the contextual footer; skill points at the binding registry"
```

---

## Self-review (done while writing)

- Spec coverage: registry+renderer (Task 2), shared predicates + tightening
  (Task 1), view swap + drift guard (Task 3), docs (Task 4). Per-context
  tests, honesty tests (predicate-level, per the spec's note), width
  truncation (unchanged `truncate` call, exercised by existing fit tests).
- Type consistency: `footerBinding{key,label,when}`, `footerLine()`,
  `can*` names match across tasks and the spec.
- No placeholders; every code step shows the code.
