package tui

import (
	"context"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/agentdocs"
	"github.com/homeend/gigagit/internal/agentsession"
	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/gittest"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/workspace"
)

// Switches that must stay INSIDE the workspace: a worktree a member just
// gained (not in its list yet), another workspace sharing a member, a
// console's or a tour's way back, a pruned slot's fallback.

// A switch to a worktree of a member that is not in the member's list yet
// (created a moment ago; a link, a session navigate, a move) is a member
// switch, never a re-root out of the workspace.
func TestFreshWorktreeOfMemberStaysInWorkspace(t *testing.T) {
	m := loadedModel(t)
	m, b := openTwo(t, m)
	wt := filepath.Join(t.TempDir(), "fresh-wt")
	gittest.Run(t, b.mainPath, "worktree", "add", "-b", "freshwt", wt)
	tm, _ := m.guardedReRoot(wt, false, true)
	m = tm.(Model)
	if m.ws == nil || m.liveRepo() != b || !model.SamePath(m.currentWorktree, wt) {
		t.Fatalf("ws=%v live ok=%v current=%q status=%q", m.ws != nil, m.liveRepo() == b, m.currentWorktree, m.statusMsg)
	}
}

func TestFreshWorktreeOfLiveMemberStaysInWorkspace(t *testing.T) {
	m := loadedModel(t)
	m, _ = openTwo(t, m)
	wt := filepath.Join(t.TempDir(), "fresh-wt")
	gittest.Run(t, m.homeRepo().mainPath, "worktree", "add", "-b", "freshwt", wt)
	tm, _ := m.guardedReRoot(wt, false, true)
	m = tm.(Model)
	if m.ws == nil || !model.SamePath(m.currentWorktree, wt) {
		t.Fatalf("ws=%v current=%q status=%q", m.ws != nil, m.currentWorktree, m.statusMsg)
	}
}

// Opening workspace B from inside A while viewing X ∉ B, when B's first
// member Y is also in A: the view moves to Y within the workspace and B
// opens at once — no dangling request that opens B later on its own.
func TestOpenOverlappingWorkspaceSwitchesWithin(t *testing.T) {
	m := loadedModel(t)
	x := m.homeRepo()
	y, z := secondRepo(t), secondRepo(t)
	a := workspace.Entry{Name: "A", Repos: []string{x.mainPath, y}}
	bws := workspace.Entry{Name: "B", Repos: []string{y, z}}
	m = withWorkspacesFile(t, m, []workspace.Entry{a, bws})
	nm, cmd := m.openWorkspace(a)
	m = settleWorkspace(t, nm, cmd)
	nm, cmd = m.openWorkspace(bws)
	m = settleWorkspace(t, nm, cmd)
	if m.ws == nil || m.ws.name != "B" || m.startWorkspace != nil || !model.SamePath(m.liveRepo().mainPath, y) {
		t.Fatalf("ws=%v status=%q start=%v", m.ws != nil, m.statusMsg, m.startWorkspace != nil)
	}
	nm, cmd = m.leaveWorkspace()
	m = settleWorkspace(t, nm, cmd)
	u, c2 := m.Update(tea.WindowSizeMsg{Width: 160, Height: 50})
	m = settleWorkspace(t, u.(Model), c2)
	if m.ws != nil {
		t.Fatalf("after Leave a workspace is open again: %s (%q)", m.ws.name, m.statusMsg)
	}
}

// The viewed worktree of member B is removed while home lives in A: the
// panels fall back to B's main checkout (never a dead slot), and B stays
// reachable by default afterwards.
func TestPrunedViewedWorktreeInMemberFallsBackToItsMain(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 160, 50
	m, b := openTwo(t, m)
	wt := filepath.Join(t.TempDir(), "bwt")
	gittest.Run(t, b.mainPath, "worktree", "add", "-b", "bwt", wt)
	m = settleWorkspace(t, m, memberLoadCmd(b))
	nm, ok := m.switchRepo(b, wt, true)
	if !ok {
		t.Fatal(nm.statusMsg)
	}
	m = nm
	gittest.Run(t, b.mainPath, "worktree", "remove", "--force", wt)
	um, _ := m.Update(m.readSourceCmd(context.Background(), srcWorktrees, reloadOpts{manual: true})())
	m = um.(Model)
	if m.views[m.viewed] == nil || !model.SamePath(m.currentWorktree, b.mainPath) || m.liveRepo() != b {
		t.Fatalf("after the prune: viewed=%q current=%q live ok=%v status=%q", m.viewed, m.currentWorktree, m.liveRepo() == b, m.statusMsg)
	}
	_ = m.View()
	nm, _ = m.switchRepo(m.homeRepo(), "", true)
	nm2, ok2 := nm.switchRepo(b, "", true)
	if !ok2 || !model.SamePath(nm2.currentWorktree, b.mainPath) {
		t.Fatalf("back into B: ok=%v current=%q status=%q", ok2, nm2.currentWorktree, nm2.statusMsg)
	}
}

// Closing a console that crossed into B returns to A's worktree.
func TestConsoleReturnCrossesMembers(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 160, 50
	m, b := openTwo(t, m)
	a := m.homeRepo()
	homeKey := m.home
	_ = startTestSession(t, m, "sleep 5")
	sb, err := domain.Sessions().Start(sessionSpecForTest("in-b", b.mainPath))
	if err != nil {
		t.Fatal(err)
	}
	m, _ = m.showConsoleBy(sb.Info().ID, true, true)
	if m.liveRepo() != b {
		t.Fatalf("show did not cross into B: %s", m.statusMsg)
	}
	m = m.closeConsole()
	if m.liveRepo() != a || m.viewed != homeKey {
		t.Fatalf("close did not return to A: live==a %v viewed=%q home=%q status=%q", m.liveRepo() == a, m.viewed, homeKey, m.statusMsg)
	}
}

// Open brief / Open report on a worker in another member is a look there
// with the tour carried — not an adopt that loses it.
func TestOpenTourInOtherMemberIsALook(t *testing.T) {
	restore := domain.UseSessionManager(agentsession.NewManager())
	t.Cleanup(func() { domain.Sessions().KillAll(context.Background()); restore() })
	t.Cleanup(domain.UseSessionStates(domain.NewStaticStates(nil)))
	m := loadedModel(t)
	m.width, m.height = 160, 50
	m, b := openTwo(t, m)
	home := m.home
	tc := config.ToolCommand{Category: "session", Name: "Sh", Mode: "session", Command: "sh -c 'sleep 600'"}
	s, _, err := domain.Open(b.mainPath).StartAgentSession(context.Background(), tc, b.mainPath, "", 80, 24, nil, "",
		domain.SpawnRecord{Parent: "x/ov", Brief: "# Task\nstart at [a](a.go:1)", Worktree: b.mainPath, Spawned: true}, "")
	if err != nil {
		t.Fatal(err)
	}
	m.docs = agentdocs.New()
	id := s.Info().ID
	m, _ = m.openTour(id, "brief")
	ovID, _ := m.docs.TourID("brief:" + domain.FullSessionID(id))
	if m.liveRepo() != b || (m.tour != ovID && topTour(m) != ovID) {
		t.Fatalf("live ok=%v tour=%q top=%q want %q status=%q", m.liveRepo() == b, m.tour, topTour(m), ovID, m.statusMsg)
	}
	if m.home != home {
		t.Fatal("opening a tour moved gg's home into B")
	}
}
