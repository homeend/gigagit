package tui

import (
	"path/filepath"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

// memberB is a loaded second member of a workspace: a real repository with
// its one worktree listed, so memberAt finds it.
func memberB(t *testing.T) *repoView {
	t.Helper()
	dir, repo := newRepoDir(t)
	r := newRepoView(dir, domain.New(repo))
	r.worktrees = []model.Worktree{{Path: dir, Branch: "main"}}
	r.branches = []model.Branch{{Name: "main"}}
	r.loaded = true
	return r
}

// workspaceOf puts the loaded model into a two-member workspace with its own
// repository as the first (home) member.
func workspaceOf(t *testing.T, m Model) (Model, *repoView) {
	t.Helper()
	b := memberB(t)
	m.ws = &workspaceState{name: "shop", members: []*repoView{m.homeRepo(), b}, live: 0}
	return m, b
}

// A look into another member keeps the leaving member's windows in its
// worktree slot and brings them back on return; gg's home does not move.
func TestSwitchRepoKeepsWindows(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	m, b := workspaceOf(t, m)
	homeKey, homeWT := m.home, m.currentWorktree
	a := m.homeRepo()
	m = m.pushLayer(&diffView{rev: "abc1"})
	nm, ok := m.switchRepo(b, "", true)
	if !ok {
		t.Fatalf("switchRepo refused: %s", nm.statusMsg)
	}
	m = nm
	if m.liveRepo() != b || m.svc != b.svc || m.currentWorktree != b.mainPath {
		t.Fatalf("B is not live: live=%p svc ok=%v current=%q", m.liveRepo(), m.svc == b.svc, m.currentWorktree)
	}
	if m.home != homeKey || m.homeWorktree() != homeWT {
		t.Fatalf("a look moved home: home=%q wt=%q", m.home, m.homeWorktree())
	}
	if m.diffLayer() != nil || len(a.views[homeKey].windows.layers.entries) != 1 {
		t.Fatalf("the diff did not wait in A: live=%v parked=%d", m.diffLayer() != nil, len(a.views[homeKey].windows.layers.entries))
	}
	if m.loading || !m.ready {
		t.Fatal("a member switch must not reload")
	}
	nm, ok = m.switchRepo(a, "", true)
	if !ok {
		t.Fatalf("switch back refused: %s", nm.statusMsg)
	}
	m = nm
	if m.liveRepo() != a || m.diffLayer() == nil || m.currentWorktree != homeWT {
		t.Fatalf("A not restored with its diff: live ok=%v diff=%v current=%q", m.liveRepo() == a, m.diffLayer() != nil, m.currentWorktree)
	}
}

// Every switch is refused while an operation runs, members included.
func TestSwitchRepoRefusedWhileRunning(t *testing.T) {
	m := loadedModel(t)
	m, b := workspaceOf(t, m)
	m.running = true
	nm, ok := m.switchRepo(b, "", true)
	if ok || nm.liveRepo() != m.homeRepo() {
		t.Fatal("switchRepo ran during an operation")
	}
	if nm.statusMsg != "an operation is running — switch once it has finished" {
		t.Fatalf("status = %q", nm.statusMsg)
	}
}

// Enter on another member's worktree row adopts: home moves there, the exit
// directory follows, the workspace stays open and A keeps its windows.
func TestEnterOnOtherMemberWorktreeAdopts(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	m, b := workspaceOf(t, m)
	a := m.homeRepo()
	oldHome := m.home
	m = m.pushLayer(&diffView{rev: "abc1"})
	nm, _ := m.guardedReRoot(b.mainPath, true, true)
	m = nm.(Model)
	if !m.inWorkspace() {
		t.Fatal("the adopt re-rooted and left the workspace")
	}
	if m.home != model.KeyOf(b.mainPath) || m.viewed != m.home || m.switchTarget != filepath.Clean(b.mainPath) {
		t.Fatalf("home=%q viewed=%q target=%q", m.home, m.viewed, m.switchTarget)
	}
	if m.homeRepo() != b || m.homeSvc() != b.svc {
		t.Fatal("homeRepo/homeSvc did not follow the adopt")
	}
	if m.loading || len(a.views[oldHome].windows.layers.entries) != 1 {
		t.Fatalf("A lost its windows or gg reloaded: loading=%v parked=%d", m.loading, len(a.views[oldHome].windows.layers.entries))
	}
	if m.snapshotPath != "" {
		t.Fatal("the old snapshot target must be disabled until the new one resolves")
	}
}

// A target outside every member re-roots as today and leaves the workspace.
func TestGuardedReRootNonMemberLeaves(t *testing.T) {
	m := loadedModel(t)
	m, _ = workspaceOf(t, m)
	dir, _ := newRepoDir(t)
	nm, _ := m.guardedReRoot(dir, false, true)
	m = nm.(Model)
	if m.inWorkspace() {
		t.Fatal("a non-member target kept the workspace")
	}
	if m.statusMsg != "left workspace shop" {
		t.Fatalf("status = %q", m.statusMsg)
	}
}

// switchView itself (a console's look, a tour, ctrl+\) reaches another
// member's worktree without leaving the workspace.
func TestSwitchViewIntoMemberStaysInWorkspace(t *testing.T) {
	m := loadedModel(t)
	m, b := workspaceOf(t, m)
	nm, ok := m.switchView(b.mainPath)
	if !ok {
		t.Fatalf("switchView refused: %s", nm.statusMsg)
	}
	if !nm.inWorkspace() || nm.liveRepo() != b || nm.home != m.home {
		t.Fatalf("ws=%v live ok=%v home moved=%v", nm.inWorkspace(), nm.liveRepo() == b, nm.home != m.home)
	}
}

// memberAt finds a member by a worktree path and by its main path; a
// stranger is nobody's.
func TestMemberAt(t *testing.T) {
	m := loadedModel(t)
	m, b := workspaceOf(t, m)
	if r, ok := m.memberAt(b.mainPath); !ok || r != b {
		t.Fatal("main path not found")
	}
	if r, ok := m.memberAt(m.currentWorktree); !ok || r != m.homeRepo() {
		t.Fatal("home worktree not found")
	}
	if _, ok := m.memberAt(filepath.Join(t.TempDir(), "x")); ok {
		t.Fatal("a stranger matched")
	}
}
