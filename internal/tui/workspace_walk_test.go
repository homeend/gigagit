package tui

import (
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

// alt+w from the last worktree of member A shows the first worktree of
// member B: a look (home stays), the Branches tab's cursor on B's row.
func TestAltWCrossesMember(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 160, 50
	m, b := workspaceOf(t, m)
	home := m.home
	ring := m.worktreeRing()
	if len(ring) != 2 || ring[1].member != 1 {
		t.Fatalf("ring = %+v", ring)
	}
	m = pressAlt(t, m, 'w') // the first hit: Branches on the viewed worktree
	m = pressAlt(t, m, 'w') // the move: into B
	if m.liveRepo() != b || m.viewed != b.key {
		t.Fatalf("after alt+w: live ok=%v viewed=%q", m.liveRepo() == b, m.viewed)
	}
	if m.home != home {
		t.Fatalf("alt+w moved home to %q", m.home)
	}
	if !m.inWorkspace() || m.loading {
		t.Fatalf("ws=%v loading=%v", m.inWorkspace(), m.loading)
	}
	ents := m.branchEntries()
	idx := m.displayIndices(panelBranches)
	sel := m.sel[panelBranches]
	if sel < 0 || sel >= len(idx) || ents[idx[sel]].member != 1 || ents[idx[sel]].heading {
		t.Fatalf("cursor at %d is not on B's branch row: %+v", sel, ents)
	}
	if m.statusMsg != shortWorktreeName(b.mainPath)+" — 2 of 2 worktrees" {
		t.Fatalf("status = %q", m.statusMsg)
	}
}

// Past the last member the ring comes back to the first: the second press
// returns to A's worktree.
func TestAltWWrapsToFirstMember(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 160, 50
	m, b := workspaceOf(t, m)
	a := m.homeRepo()
	m = pressAlt(t, m, 'w')
	m = pressAlt(t, m, 'w')
	if m.liveRepo() != b {
		t.Fatal("first move did not reach B")
	}
	m = pressAlt(t, m, 'w')
	if m.liveRepo() != a || m.viewed != m.home {
		t.Fatalf("wrap: live ok=%v viewed=%q home=%q", m.liveRepo() == a, m.viewed, m.home)
	}
}

// alt+a from A's bound console binds B's agent next: the walk crosses the
// member boundary and the panels follow the session's worktree.
func TestAltAWalksAcrossMembers(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 160, 50
	m, b := workspaceOf(t, m)
	sa := startTestSession(t, m, "sleep 5")
	sb, err := domain.Sessions().Start(sessionSpecForTest("in-b", b.mainPath))
	if err != nil {
		t.Fatal(err)
	}
	aID, bID := sa.Info().ID, sb.Info().ID
	m = pressAlt(t, m, 'a') // the viewed worktree's own agent first
	if m.console == nil || m.console.id != aID || !m.console.focused {
		t.Fatalf("first alt+a did not bind A's agent: %+v", m.console)
	}
	m = pressAlt(t, m, 'a') // the next one: B's
	if m.console == nil || m.console.id != bID {
		t.Fatalf("second alt+a did not bind B's agent: %+v", m.console)
	}
	if m.liveRepo() != b || model.KeyOf(m.currentWorktree) != b.key {
		t.Fatalf("the panels did not follow into B: live ok=%v current=%q", m.liveRepo() == b, m.currentWorktree)
	}
}

// alt+A (shift) stays with the viewed worktree's own sessions: B's agent
// is not in its ring while A is on screen.
func TestAltShiftAStaysInViewedWorktree(t *testing.T) {
	m := loadedModel(t)
	m, b := workspaceOf(t, m)
	_ = startTestSession(t, m, "sleep 5")
	if _, err := domain.Sessions().Start(sessionSpecForTest("in-b", b.mainPath)); err != nil {
		t.Fatal(err)
	}
	all := m.sessionRing(false)
	scoped := m.sessionRingIn(false, m.viewPath(m.viewed))
	if len(all) != 2 || len(scoped) != 1 || !model.SamePath(scoped[0].Dir, m.currentWorktree) {
		t.Fatalf("all=%d scoped=%d", len(all), len(scoped))
	}
}

// alt+q walks whatever worktree alt+w left on screen — B's after a cross.
func TestAltQUnchangedAcrossMembers(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 160, 50
	m, b := workspaceOf(t, m)
	m = pressAlt(t, m, 'w')
	m = pressAlt(t, m, 'w')
	if m.liveRepo() != b {
		t.Fatal("not in B")
	}
	m = m.pushLayer(&diffView{title: "a.go", rev: "abc123"}) // a window in B
	nm, _ := m.walkWorktree(false)                           // the plain stop: windows aside
	if nm.liveRepo() != b || nm.windowState.cyclePark == nil || nm.diffLayer() != nil {
		t.Fatalf("alt+q in B: live ok=%v park=%v diff=%v", nm.liveRepo() == b, nm.windowState.cyclePark != nil, nm.diffLayer() != nil)
	}
}
