package tui

import (
	"fmt"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

func pressSessionWalk(t *testing.T, m Model, scoped bool) Model {
	t.Helper()
	if scoped {
		return pressAlt(t, m, 'S')
	}
	return pressAlt(t, m, 's')
}

// subRowsOf is the Branches tab's session sub-rows of the worktree at dir,
// top to bottom: the order alt+s walks them in.
func subRowsOf(m Model, dir string) []domain.SessionID {
	var out []domain.SessionID
	for _, e := range m.branchEntries() {
		if e.sess == "" {
			continue
		}
		if s, ok := domain.Sessions().Get(e.sess); ok && model.SamePath(s.Info().Dir, dir) {
			out = append(out, e.sess)
		}
	}
	return out
}

func wantPlainOf(t *testing.T, m Model, step, dir string) {
	t.Helper()
	if m.viewed != model.KeyOf(dir) {
		t.Fatalf("%s: viewed=%q (%s), want %s", step, m.viewed, m.statusMsg, dir)
	}
	wantPlain(t, m, step)
}

// Serial (session manager). alt+s walks every worktree that has a running
// session, in the Branches tab's order: its plain worktree, then its
// sessions as their sub-rows list them (agents and terminals mixed), then
// the next such worktree; past the last, the first one's plain worktree.
// A worktree without a session is no stop.
func TestAltSWalksEveryWorktreeWithSessions(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 160, 40
	m, wt2 := addWorktree(t, m, "wt2")
	m, wt3 := addWorktree(t, m, "wt3")
	installSessionManager(t)
	startSessionIn(t, m, wt2, "A2")
	startTerminalIn(t, m, wt2, "T2")
	startTerminalIn(t, m, wt3, "T3")
	startSessionIn(t, m, wt3, "A3")
	m = walkSelectBranch(t, m, m.worktreeBranch(m.homeWorktree()))

	var order []string // the worktrees with sessions, in the ring's order
	for _, st := range m.worktreeRing() {
		if !model.SamePath(st.path, m.homeWorktree()) {
			order = append(order, st.path)
		}
	}
	if len(order) != 2 {
		t.Fatalf("ring=%+v, want two other worktrees", m.worktreeRing())
	}
	for round := range 2 {
		for _, dir := range order {
			m = pressSessionWalk(t, m, false)
			wantPlainOf(t, m, fmt.Sprintf("round %d: %s", round, dir), dir)
			rows := subRowsOf(m, dir)
			if len(rows) != 2 {
				t.Fatalf("%s: sub-rows=%v, want 2", dir, rows)
			}
			for i, id := range rows {
				m = pressSessionWalk(t, m, false)
				wantSession(t, m, fmt.Sprintf("round %d: %s #%d", round, dir, i+1), id, false)
				if e, ok := m.selectedBranchEntry(); !ok || e.sess != id {
					t.Fatalf("%s #%d: cursor on %+v, want its sub-row", dir, i+1, e)
				}
			}
		}
	}
}

// Serial. Arriving at another worktree its windows are put aside (its
// plain stop), then come back as its "your windows" stop, then its
// sessions show over them; past its last session the walk moves on.
func TestAltSParksTheArrivingWorktreesWindows(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 160, 40
	home := m.homeWorktree()
	m, wt2 := addWorktree(t, m, "wt2")
	installSessionManager(t)
	a1 := startSessionIn(t, m, home, "A1")
	b1 := startSessionIn(t, m, wt2, "B1")
	m, ok := m.switchView(wt2)
	if !ok {
		t.Fatalf("switch: %s", m.statusMsg)
	}
	dv := &diffView{title: "a.go", rev: "abc123"}
	m = m.pushLayer(dv)
	if m, ok = m.switchView(home); !ok {
		t.Fatalf("switch back: %s", m.statusMsg)
	}
	m = walkSelectBranch(t, m, m.worktreeBranch(home))
	if m.ringIndex(home) > m.ringIndex(wt2) {
		t.Fatal("fixture: home must come before wt2 in the ring")
	}

	m = pressSessionWalk(t, m, false)
	wantSession(t, m, "home's agent", a1, false)
	m = pressSessionWalk(t, m, false)
	wantPlainOf(t, m, "wt2's plain worktree", wt2)
	if m.windowState.cyclePark == nil {
		t.Fatal("wt2's diff was not put aside")
	}
	m = pressSessionWalk(t, m, false)
	wantWindows(t, m, "wt2's windows", dv)
	m = pressSessionWalk(t, m, false)
	wantSession(t, m, "wt2's agent", b1, true)
	m = pressSessionWalk(t, m, false)
	wantPlainOf(t, m, "around to home", home)
}

// Serial. From an open window with no session shown (alt+q's order): the
// viewed worktree's first session, not its plain worktree.
func TestAltSFromYourWindowsShowsTheFirstSession(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 160, 40
	installSessionManager(t)
	a1 := startSessionIn(t, m, m.homeWorktree(), "A1")
	dv := &diffView{title: "a.go", rev: "abc123"}
	m = m.pushLayer(dv)
	m = pressSessionWalk(t, m, false)
	wantSession(t, m, "1st", a1, true)
	m = pressSessionWalk(t, m, false)
	wantPlainOf(t, m, "2nd", m.homeWorktree())
	m = pressSessionWalk(t, m, false)
	wantWindows(t, m, "3rd", dv)
}

// Serial. The viewed worktree has no session: the next worktree that has
// one, in the ring's order, wrapping.
func TestAltSFromAWorktreeWithoutSessionsGoesToTheNext(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 160, 40
	home := m.homeWorktree()
	m, wt2 := addWorktree(t, m, "wt2")
	installSessionManager(t)
	startSessionIn(t, m, home, "A1")
	m, ok := m.switchView(wt2)
	if !ok {
		t.Fatalf("switch: %s", m.statusMsg)
	}
	m = walkSelectBranch(t, m, "wt2")
	m = pressSessionWalk(t, m, false)
	wantPlainOf(t, m, "home", home)
}

// Serial. No running session anywhere: a status line, nothing moves.
func TestAltSWithNoSessionSaysSo(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 160, 40
	installSessionManager(t)
	dv := &diffView{title: "a.go", rev: "abc123"}
	m = m.pushLayer(dv)
	m.statusMsg = ""
	m = pressSessionWalk(t, m, false)
	if m.topLayer() != dv || m.windowState.cyclePark != nil || m.statusMsg == "" {
		t.Fatalf("top=%T park=%v msg=%q, want the diff untouched and a status line", m.topLayer(), m.windowState.cyclePark != nil, m.statusMsg)
	}
}

// Serial. A popup on screen: alt+s does nothing.
func TestAltSUnderAPopupDoesNothing(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 160, 40
	installSessionManager(t)
	startSessionIn(t, m, m.homeWorktree(), "A1")
	pp := &gotoCommitPopup{}
	m = m.pushLayer(pp)
	m = pressSessionWalk(t, m, false)
	if m.console != nil || m.topLayer() != pp {
		t.Fatalf("console=%v top=%T, want the popup alone", m.console != nil, m.topLayer())
	}
}

// Serial. The footer offers alt+s while a session runs.
func TestFooterOffersAltSWithASession(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 160, 40
	installSessionManager(t)
	b, ok := bindingByID("walk-sessions")
	if !ok {
		t.Fatal("no walk-sessions footer binding")
	}
	if b.when(m) {
		t.Fatal("the footer offers alt+s with no session")
	}
	startSessionIn(t, m, m.homeWorktree(), "A1")
	if !b.when(m) {
		t.Fatal("a running session: no hint")
	}
}

// Serial. In a workspace alt+s walks every member's worktrees, crossing
// into member B; alt+S stays with the live member's.
func TestAltSWalksEveryMemberAndAltShiftSTheLiveOne(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 160, 50
	m, b := workspaceOf(t, m)
	a := m.homeRepo()
	sa := startTestSession(t, m, "sleep 5")
	sb, err := domain.Sessions().Start(sessionSpecForTest("in-b", b.mainPath))
	if err != nil {
		t.Fatal(err)
	}
	m = m.activateTab(panelBranches).selectWorktreeBranch(m.homeWorktree())

	s := pressSessionWalk(t, m, true)
	wantSession(t, s, "alt+S 1st", sa.Info().ID, false)
	s = pressSessionWalk(t, s, true)
	if s.liveRepo() != a {
		t.Fatal("alt+S left the live member")
	}
	wantPlainOf(t, s, "alt+S 2nd", m.homeWorktree())
	s = pressSessionWalk(t, s, true)
	wantSession(t, s, "alt+S 3rd", sa.Info().ID, false)

	m = pressSessionWalk(t, m, false)
	wantSession(t, m, "1st", sa.Info().ID, false)
	m = pressSessionWalk(t, m, false)
	if m.liveRepo() != b || m.viewed != b.key {
		t.Fatalf("2nd: live ok=%v viewed=%q (%s), want member B", m.liveRepo() == b, m.viewed, m.statusMsg)
	}
	wantPlain(t, m, "2nd")
	m = pressSessionWalk(t, m, false)
	wantSession(t, m, "3rd", sb.Info().ID, false)
	m = pressSessionWalk(t, m, false)
	if m.liveRepo() != a {
		t.Fatalf("4th: live ok=%v (%s), want member A again", m.liveRepo() == a, m.statusMsg)
	}
	wantPlain(t, m, "4th")
}
