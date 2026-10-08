package tui

import (
	"path/filepath"
	"strings"
	"testing"
)

// A full load (loadCmd) launched on the viewed slot's service lands after
// the view went home: dropped, never written over home's live view.
func TestSwitchViewDropsAStaleFullLoad(t *testing.T) {
	m := loadedModel(t)
	m, other := addWorktree(t, m, "wt2")
	m, _ = m.switchView(other)
	m = landView(t, m)
	stale := m.loadCmd()
	m, _ = m.switchView(m.home)
	nm, _ := m.Update(stale())
	m = nm.(Model)
	if m.currentWorktree != m.home || m.viewed != m.home || m.svc != m.views[m.home].svc {
		t.Fatalf("a stale load landed: current=%q viewed=%q", m.currentWorktree, m.viewed)
	}
}

// Generations the handlers key on are Model-global and monotonic: a slot
// never restores an older one.
func TestSwitchGensAreMonotonic(t *testing.T) {
	m := loadedModel(t)
	m, other := addWorktree(t, m, "wt2")
	for i, to := range []string{other, m.home, other} {
		w, r, d := m.watchGen, m.workingReviewsGen, m.docWatch.gen
		m, _ = m.switchView(to)
		if m.watchGen <= w || m.workingReviewsGen <= r || m.docWatch.gen <= d {
			t.Fatalf("switch %d: gens went backwards or stalled: watch %d→%d reviews %d→%d doc %d→%d", i, w, m.watchGen, r, m.workingReviewsGen, d, m.docWatch.gen)
		}
	}
}

// gg's own worktree (home) is never deletable or recyclable while a console
// views another one; the viewed one is not either.
func TestOwnWorktreeGuardedWhileViewingAnother(t *testing.T) {
	m := loadedModel(t)
	m, other := addWorktree(t, m, "wt2")
	m, _ = m.switchView(other)
	for i, w := range m.worktrees {
		m.sel[panelWorktrees] = i
		if m.canDeleteWorktree() {
			t.Fatalf("%s is deletable while %q is viewed from home %q", w.Path, m.viewed, m.home)
		}
	}
	for _, w := range m.recycleCandidates() {
		if filepath.Clean(w.Path) == m.home || filepath.Clean(w.Path) == m.viewed {
			t.Fatalf("recycle offers %s", w.Path)
		}
	}
}

// The snapshot target resolved for an adopt lands even if a console has
// since put another slot's service on screen.
func TestSnapshotTargetLandsAfterAConsoleShow(t *testing.T) {
	m := loadedModel(t)
	m, a := addWorktree(t, m, "wtA")
	m, b := addWorktree(t, m, "wtB")
	nm, cmd := m.guardedReRoot(a, true)
	m = nm.(Model)
	m, _ = m.switchView(b) // a console show
	for _, msg := range drainBatch(cmd) {
		if st, ok := msg.(snapshotTargetMsg); ok {
			nm, _ := m.Update(st)
			m = nm.(Model)
		}
	}
	if filepath.Clean(m.snapshotWorktree) != filepath.Clean(a) {
		t.Fatalf("snapshotWorktree = %q, want %q", m.snapshotWorktree, a)
	}
}

// The hosted page is rooted at gg's own worktree even when started while a
// console views another one.
func TestWebHostStartsOnHome(t *testing.T) {
	m := loadedModel(t)
	m, other := addWorktree(t, m, "wt2")
	m, _ = m.switchView(other)
	if m.homeSvc() != m.views[m.home].svc || m.homeSvc() == m.svc {
		t.Fatal("homeSvc must be home's service, not the viewed one")
	}
}

// A legacy full load landing while a console views A keeps A on screen but
// publishes HOME as this gg's worktree.
func TestLegacyLoadWhileViewingKeepsPublishedHome(t *testing.T) {
	m := loadedModel(t)
	m, other := addWorktree(t, m, "wt2")
	m, _ = m.switchView(other)
	m = landView(t, m)
	nm, _ := m.Update(m.loadCmd()())
	m = nm.(Model)
	if m.currentWorktree != filepath.Clean(other) || publishedWorktree() != m.home {
		t.Fatalf("current=%q published=%q home=%q", m.currentWorktree, publishedWorktree(), m.home)
	}
}

// enter on home's row while A's console is docked: the panels go home, the
// console stays, its return is home, and the hint says the console works in
// another worktree than the panels show.
func TestUserSwitchUnderAConsoleSwapsAndLabelsTheHint(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 160, 40
	m, other := addWorktree(t, m, "wt2")
	installSessionManager(t)
	id := startSessionIn(t, m, other, "Shell")
	m, _ = m.showConsole(id, false)
	if row := m.withConsoleWorktree("", 120); !strings.HasPrefix(row, "showing: ") {
		t.Fatalf("console's worktree on screen: row = %q", row)
	}
	nm, _ := m.guardedReRoot(m.home, true)
	m = nm.(Model)
	if m.viewed != m.home || m.console == nil || m.console.ret.view != m.home {
		t.Fatalf("viewed=%q console=%+v", m.viewed, m.console)
	}
	if row := m.withConsoleWorktree("", 120); !strings.HasPrefix(row, "worktree: ") {
		t.Fatalf("console elsewhere than the panels: row = %q", row)
	}
}

// Closing a console while an operation runs cannot swap yet; the return is
// kept and happens when the operation ends.
func TestCloseConsoleDuringAnOpReturnsWhenItEnds(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	m, other := addWorktree(t, m, "wt2")
	installSessionManager(t)
	id := startSessionIn(t, m, other, "Shell")
	m, _ = m.showConsole(id, false)
	m.running = true
	m = m.closeConsole()
	if m.viewed != filepath.Clean(other) || m.pendingReturnView != m.home {
		t.Fatalf("viewed=%q pending=%q", m.viewed, m.pendingReturnView)
	}
	nm, _ := m.Update(opFinishedMsg{})
	m = nm.(Model)
	if m.viewed != m.home || m.pendingReturnView != "" {
		t.Fatalf("after the op: viewed=%q pending=%q", m.viewed, m.pendingReturnView)
	}
}
