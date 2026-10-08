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
