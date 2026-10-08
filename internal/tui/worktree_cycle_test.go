package tui

import (
	"path/filepath"
	"strings"
	"testing"
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
		if filepath.Clean(w.Path) == home {
			start = i
		}
	}
	if start < 0 {
		t.Fatalf("home %q is not in the list", home)
	}
	for step := 1; step <= 3; step++ {
		m = pressAlt(t, m, 'w')
		want := filepath.Clean(m.worktrees[(start+step)%3].Path)
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

// A docked console stays open while alt+w moves the panels away from its
// worktree; the status row then names the console's worktree.
func TestAltWKeepsADockedConsole(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 160, 40
	m, other := addWorktree(t, m, "wt2")
	installSessionManager(t)
	id := startSessionIn(t, m, other, "Shell")
	m, _ = m.showConsole(id, false)
	if m.viewed != filepath.Clean(other) {
		t.Fatalf("precondition: viewed = %q", m.viewed)
	}
	m = pressAlt(t, m, 'w')
	if m.console == nil || m.viewed == filepath.Clean(other) {
		t.Fatalf("console=%v viewed=%q", m.console != nil, m.viewed)
	}
	if got := m.consoleWorktreeHint(); got != filepath.Clean(other) {
		t.Fatalf("hint = %q, want %q", got, other)
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

// alt+w picks the base worktree: alt+a over a console in B, then alt+w to
// C under it, then alt+a round to the return stop lands on C — the last
// alt+w — not on the worktree the cycle started in.
func TestAltAReturnStopIsTheLastAltWWorktree(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 160, 40
	m, wtB := addWorktree(t, m, "wtB")
	m, _ = addWorktree(t, m, "wtC")
	installSessionManager(t)
	startSessionIn(t, m, wtB, "B")
	start := m.viewed
	m = pressAlt(t, m, 'a')
	if m.console == nil || m.viewed != filepath.Clean(wtB) {
		t.Fatalf("alt+a: console=%v viewed=%q", m.console != nil, m.viewed)
	}
	m = pressAlt(t, m, 'w')
	base := m.viewed
	if m.console == nil || base == filepath.Clean(wtB) || base == start {
		t.Fatalf("alt+w under the console: console=%v viewed=%q", m.console != nil, m.viewed)
	}
	m = pressAlt(t, m, 'a') // the ring's return stop: the console closes
	if m.console != nil {
		t.Fatal("return stop: console still shown")
	}
	if m.viewed != base {
		t.Fatalf("return stop landed on %q, want the last alt+w worktree %q", m.viewed, base)
	}
}

// alt+w inside a bound console unbinds it (the title hints come back, no
// cursor) and moves the keyboard to the Branches panel: the next keys are
// gg's, not the agent's.
func TestAltWUnbindsAFocusedConsoleAndFocusesBranches(t *testing.T) {
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
	if m.console == nil || m.console.focused {
		t.Fatalf("console=%+v, want shown and unbound", m.console)
	}
	if m.focus != panelBranches || m.activeLeftTab != panelBranches || !m.panelFocused(panelBranches) {
		t.Fatalf("focus=%v tab=%v, want the Branches panel", m.focus, m.activeLeftTab)
	}
	if m.console.ret.focus != panelBranches {
		t.Fatalf("return focus = %v, want Branches (the alt+a return stop lands there)", m.console.ret.focus)
	}
}

// Without a console alt+w also lands on Branches: the switch and the
// keyboard go together.
func TestAltWFocusesBranchesWithoutAConsole(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 160, 40
	m, _ = addWorktree(t, m, "wt2")
	m.focus = panelFiles
	m = pressAlt(t, m, 'w')
	if m.focus != panelBranches || m.activeLeftTab != panelBranches {
		t.Fatalf("focus=%v tab=%v, want Branches", m.focus, m.activeLeftTab)
	}
}

// A ctrl+t-maximised docked console docks again on alt+w, so the Branches
// panel it hands the keyboard to is on screen.
func TestAltWRedocksAMaximisedConsole(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 160, 40
	m, other := addWorktree(t, m, "wt2")
	installSessionManager(t)
	id := startSessionIn(t, m, other, "Shell")
	m, _ = m.showConsole(id, true)
	m.console.maximized = true
	m = pressAlt(t, m, 'w')
	if m.console == nil || m.console.maximized || m.console.focused || m.focus != panelBranches {
		t.Fatalf("console=%+v focus=%v", m.console, m.focus)
	}
}

// Over a full-screen return point the console stays full (Branches is not
// on screen): it is unbound and keeps the keyboard; the panels beneath
// still switched.
func TestAltWUnderAFullScreenConsoleKeepsItFull(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 160, 40
	m, other := addWorktree(t, m, "wt2")
	installSessionManager(t)
	id := startSessionIn(t, m, other, "Shell")
	m, _ = m.showConsole(id, true)
	m.console.ret.full, m.console.maximized = true, true
	m = pressAlt(t, m, 'w')
	if m.console == nil || !m.console.maximized || m.console.focused || m.focus != panelCommits {
		t.Fatalf("console=%+v focus=%v", m.console, m.focus)
	}
	if m.viewed == filepath.Clean(other) {
		t.Fatalf("viewed = %q, want the next worktree", m.viewed)
	}
}

// alt+a / alt+t show the next session unbound with the console column
// focused; the return stop hands the keyboard back to the panel focused
// before the first show — Branches once alt+w was pressed in between.
func TestAltAReturnStopRestoresTheFocusAltWSet(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 160, 40
	m, wtA := addWorktree(t, m, "wtA")
	m, wtB := addWorktree(t, m, "wtB")
	installSessionManager(t)
	startSessionIn(t, m, wtA, "A")
	startSessionIn(t, m, wtB, "B")
	m.focus = panelFiles
	m = pressAlt(t, m, 'a')
	if m.console == nil || m.console.focused || m.focus != panelCommits {
		t.Fatalf("after alt+a: console=%+v focus=%v", m.console, m.focus)
	}
	m = pressKey(t, m, "enter") // bind it
	if !m.console.focused {
		t.Fatal("enter did not bind the console")
	}
	m = pressAlt(t, m, 'a') // from a bound console: the next one, unbound, column focused
	if m.console == nil || m.console.focused || m.focus != panelCommits {
		t.Fatalf("after the second alt+a: console=%+v focus=%v", m.console, m.focus)
	}
	m = pressAlt(t, m, 'w')
	if m.focus != panelBranches {
		t.Fatalf("after alt+w: focus=%v", m.focus)
	}
	m.focus = panelCommits  // the user clicked back onto the console column
	m = pressAlt(t, m, 'a') // the return stop
	if m.console != nil || m.focus != panelBranches {
		t.Fatalf("return stop: console=%v focus=%v, want no console and Branches", m.console != nil, m.focus)
	}
}
