package tui

import (
	"slices"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

// alt+w walks the Worktrees panel's list: from the viewed worktree to the
// next row, past the last one back to the first — the fast switch, no
// console needed, and gg's own worktree (home) stays where it was.
func TestAltWCyclesTheWorktreesAndWraps(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	m, _ = addWorktree(t, m, "wtA")
	m, _ = addWorktree(t, m, "wtB")
	if len(m.worktrees) != 3 {
		t.Fatalf("worktrees = %d, want 3", len(m.worktrees))
	}
	home := m.home
	start := -1
	for i, w := range m.worktrees {
		if model.KeyOf(w.Path) == home {
			start = i
		}
	}
	if start < 0 {
		t.Fatalf("home %q is not in the list", home)
	}
	for step := 1; step <= 3; step++ {
		m = pressAlt(t, m, 'w')
		want := model.KeyOf(m.worktrees[(start+step)%3].Path)
		if m.viewed != want {
			t.Fatalf("press %d: viewed = %q, want %q", step, m.viewed, want)
		}
		if m.home != home {
			t.Fatalf("press %d: home moved to %q", step, m.home)
		}
		if !strings.Contains(m.statusMsg, "of 3") {
			t.Fatalf("press %d: status = %q, want the position in the ring", step, m.statusMsg)
		}
		m = landView(t, m)
	}
	if m.viewed != home {
		t.Fatalf("after a full ring: viewed = %q, want home %q", m.viewed, home)
	}
}

// One worktree: nothing to cycle, the status line says so.
func TestAltWWithOneWorktreeSaysSo(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	m = pressAlt(t, m, 'w')
	if m.viewed != m.home || m.statusMsg == "" {
		t.Fatalf("viewed=%q status=%q", m.viewed, m.statusMsg)
	}
}

// alt+w under a shown console is a first hit: the console hides (the
// session keeps running), the panels stay on its worktree, Branches takes
// the keyboard with its cursor on that worktree's branch. The next press
// moves on.
func TestAltWFirstHidesTheConsoleAndFocusesBranches(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 160, 40
	m, other := addWorktree(t, m, "wt2")
	installSessionManager(t)
	id := startSessionIn(t, m, other, "Shell")
	m, _ = m.showConsole(id, false)
	if m.viewed != model.KeyOf(other) || m.focus != panelCommits {
		t.Fatalf("precondition: viewed=%q focus=%v", m.viewed, m.focus)
	}
	m = pressAlt(t, m, 'w')
	if m.console != nil || m.viewed != model.KeyOf(other) || m.focus != panelBranches || m.activeLeftTab != panelBranches {
		t.Fatalf("first hit: console=%v viewed=%q focus=%v tab=%v", m.console != nil, m.viewed, m.focus, m.activeLeftTab)
	}
	if b, ok := m.selectedBranch(); !ok || b.Name != m.worktreeBranch(other) {
		t.Fatalf("Branches cursor on %+v, want the branch of %s", b, other)
	}
	m = pressAlt(t, m, 'w')
	if m.console != nil || m.viewed == model.KeyOf(other) {
		t.Fatalf("second hit: console=%v viewed=%q, want the next worktree", m.console != nil, m.viewed)
	}
}

// The Branches panel's head marker (*) is worktree-specific: a fast
// switch moves it to the viewed worktree's branch AT ONCE, from the
// worktree list — no git read.
func TestFastSwitchMovesTheHeadMarkerToTheViewedBranch(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	m, other := addWorktree(t, m, "wtA")
	m, ok := m.switchView(other)
	if !ok {
		t.Fatalf("switch refused: %s", m.statusMsg)
	}
	var heads []string
	for _, b := range m.branches {
		if b.IsHead {
			heads = append(heads, b.Name)
		}
	}
	if len(heads) != 1 || heads[0] != "wtA" {
		t.Fatalf("head branches after the switch = %v, want [wtA]", heads)
	}
}

// A fast switch never blocks: its reads are silent (no ⏳ reloading, no
// guard trips), so a second alt+w right after the first moves on without
// waiting for anything to land.
func TestAltWNeverBlocksOnItsOwnReads(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	m, _ = addWorktree(t, m, "wtA")
	m, _ = addWorktree(t, m, "wtB")
	m = pressAlt(t, m, 'w')
	first := m.viewed
	if m.anySourceLoading() || !m.opsIdle() {
		t.Fatalf("after alt+w: a source is marked loading (the ⏳ gate), opsIdle=%v", m.opsIdle())
	}
	m = pressAlt(t, m, 'w')
	if m.viewed == first {
		t.Fatalf("second alt+w refused: %q (viewed %q)", m.statusMsg, m.viewed)
	}
}

// A bound console: the first alt+w hides it and hands the keyboard to
// Branches — the next keys are gg's, not the agent's — without moving on.
func TestAltWHidesABoundConsoleAndFocusesBranches(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 160, 40
	m, other := addWorktree(t, m, "wt2")
	installSessionManager(t)
	id := startSessionIn(t, m, other, "Shell")
	m.activeLeftTab, m.focus = panelWorktrees, panelWorktrees
	m, _ = m.showConsole(id, true)
	if m.console == nil || !m.console.focused || m.focus != panelCommits {
		t.Fatalf("precondition: console=%+v focus=%v", m.console, m.focus)
	}
	m = pressAlt(t, m, 'w')
	if m.console != nil {
		t.Fatal("the console must hide")
	}
	if m.focus != panelBranches || m.activeLeftTab != panelBranches || !m.panelFocused(panelBranches) {
		t.Fatalf("focus=%v tab=%v, want the Branches panel", m.focus, m.activeLeftTab)
	}
	if m.viewed != model.KeyOf(other) {
		t.Fatalf("viewed=%q, want the console's worktree kept", m.viewed)
	}
}

// Without a console, alt+w from another panel only takes the keyboard to
// Branches; the worktree stays. The press after it moves on.
func TestAltWFromAnotherPanelFocusesBranchesFirst(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 160, 40
	m, _ = addWorktree(t, m, "wt2")
	m.focus = panelFiles
	start := m.viewed
	m = pressAlt(t, m, 'w')
	if m.focus != panelBranches || m.activeLeftTab != panelBranches || m.viewed != start {
		t.Fatalf("focus=%v tab=%v viewed=%q, want Branches on %q", m.focus, m.activeLeftTab, m.viewed, start)
	}
	m = pressAlt(t, m, 'w')
	if m.viewed == start {
		t.Fatal("second press must move on")
	}
}

// A ctrl+t-maximised docked console hides like a docked one.
func TestAltWHidesAMaximisedConsoleToo(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 160, 40
	m, other := addWorktree(t, m, "wt2")
	installSessionManager(t)
	id := startSessionIn(t, m, other, "Shell")
	m, _ = m.showConsole(id, true)
	m.console.maximized = true
	m = pressAlt(t, m, 'w')
	if m.console != nil || m.focus != panelBranches || m.viewed != model.KeyOf(other) {
		t.Fatalf("console=%v focus=%v viewed=%q", m.console != nil, m.focus, m.viewed)
	}
}

// A console over a parked diff view: alt+w hides it and the diff comes
// back on top, as esc would; Branches has the keyboard beneath it.
func TestAltWUnderAFullScreenConsoleHidesItAndBringsTheViewBack(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 160, 40
	m, other := addWorktree(t, m, "wt2")
	installSessionManager(t)
	id := startSessionIn(t, m, other, "Shell")
	dv := &diffView{title: "a.go", rev: "abc123"}
	m = m.pushLayer(dv)
	m, _ = m.showConsole(id, true)
	if !m.consoleFull() || m.topLayer() != nil {
		t.Fatalf("precondition: console=%+v top=%T", m.console, m.topLayer())
	}
	m = pressAlt(t, m, 'w')
	if m.console != nil || m.topLayer() != layer(dv) || m.focus != panelBranches {
		t.Fatalf("console=%v top=%T focus=%v", m.console != nil, m.topLayer(), m.focus)
	}
}

// alt+w after alt+a: the console hides on its own worktree, Branches has
// the keyboard with its cursor on that worktree's branch; the next alt+w
// moves to the worktree below.
func TestAltWAfterAltAHidesTheConsoleOnItsWorktree(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 160, 40
	m, wtA := addWorktree(t, m, "wtA")
	m, wtB := addWorktree(t, m, "wtB")
	installSessionManager(t)
	startSessionIn(t, m, wtA, "A")
	startSessionIn(t, m, wtB, "B")
	m.focus = panelFiles
	m = pressAlt(t, m, 'a')
	if m.console == nil || !m.console.focused || m.focus != panelCommits {
		t.Fatalf("after alt+a: console=%+v focus=%v", m.console, m.focus)
	}
	at := m.viewed
	m = pressAlt(t, m, 'w')
	if m.console != nil || m.focus != panelBranches || m.viewed != at {
		t.Fatalf("alt+w: console=%v focus=%v viewed=%q, want hidden, Branches, %q", m.console != nil, m.focus, m.viewed, at)
	}
	if b, ok := m.selectedBranch(); !ok || b.Name != m.worktreeBranch(m.viewPath(at)) {
		t.Fatalf("Branches cursor on %+v, want the branch of %s", b, at)
	}
	m = pressAlt(t, m, 'w')
	if m.viewed == at || m.console != nil {
		t.Fatalf("second alt+w: viewed=%q console=%v, want the next worktree", m.viewed, m.console != nil)
	}
	if b, ok := m.selectedBranch(); !ok || b.Name != m.worktreeBranch(m.viewPath(m.viewed)) {
		t.Fatalf("Branches cursor on %+v, want the branch of %s", b, m.viewed)
	}
}

// alt+w walks the BRANCHES tab's order — its branch rows with a checkout,
// top to bottom under its sort — not the worktree list's: with the tab
// sorted by name descending the next worktree is the row below in that
// order.
func TestAltWFollowsTheBranchesTabsSortOrder(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 160, 40
	m, _ = addWorktree(t, m, "wtA")
	m, _ = addWorktree(t, m, "wtB")
	m.sortModes[panelBranches] = sortNameAsc
	asc := append([]int(nil), m.worktreeOrder()...)
	m.sortModes[panelBranches] = sortNameDesc
	sorted := m.worktreeOrder()
	if len(sorted) != 3 || slices.Equal(sorted, asc) {
		t.Fatalf("precondition: the sort must reorder the tab: asc=%v desc=%v", asc, sorted)
	}
	for i := range 3 { // the rows top to bottom, as the tab draws them
		if name := m.branches[m.branchEntries()[m.displayIndices(panelBranches)[i]].br].Name; m.worktrees[sorted[i]].Branch != name {
			t.Fatalf("row %d is %s, order says %s", i, name, m.worktrees[sorted[i]].Branch)
		}
	}
	var visited []model.CheckoutKey
	for range 3 {
		m = pressAlt(t, m, 'w')
		visited = append(visited, m.viewed)
	}
	var want []model.CheckoutKey
	at := m.worktreeIndex(m.homeWorktree())
	for i := 1; i <= 3; i++ {
		want = append(want, model.KeyOf(m.worktrees[sorted[(at+i)%3]].Path))
	}
	if !slices.Equal(visited, want) {
		t.Fatalf("visited %v, want the tab's order %v", visited, want)
	}
}
