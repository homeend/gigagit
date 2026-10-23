package tui

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/workspace"
)

// C1: a live-lane read launched in member A that lands after the switch
// to B is dropped — it never writes A's list into B's panels.
func TestInFlightLiveReadDoesNotLandAfterSwitch(t *testing.T) {
	m := loadedModel(t)
	m, b := openTwo(t, m)
	read := m.readSourceCmd(context.Background(), srcBranches, reloadOpts{manual: true}) // A's read, A's generation
	nm, ok := m.switchRepo(b, "", true)
	if !ok {
		t.Fatal(nm.statusMsg)
	}
	m = nm
	want := append([]model.Branch(nil), m.branches...)
	um, _ := m.Update(read())
	m = um.(Model)
	if len(m.branches) != len(want) || (len(want) > 0 && m.branches[0].Name != want[0].Name) {
		t.Fatalf("A's stale branches landed in B: %+v", m.branches)
	}
}

// C2: a member's service is its root service, whatever worktree was
// viewed there; leaving a member never rewrites it.
func TestMemberServiceStaysRooted(t *testing.T) {
	m := loadedModel(t)
	m, b := openTwo(t, m)
	root := b.svc
	nm, ok := m.switchRepo(b, "", true)
	if !ok {
		t.Fatal(nm.statusMsg)
	}
	nm, ok = nm.switchRepo(m.homeRepo(), "", true)
	if !ok {
		t.Fatal(nm.statusMsg)
	}
	if b.svc != root {
		t.Fatal("saveRepo replaced B's root service with the viewed slot's")
	}
}

// C2 (home): gg launched in a linked worktree seeds a home slot whose
// service is rooted at the MAIN checkout, not the launch worktree.
func TestHomeRepoServiceRootedAtMain(t *testing.T) {
	m := loadedModel(t)
	m, linked := addWorktree(t, m, "wt-linked")
	main := m.currentWorktree
	lm := loadedModelAt(t, linked)
	r := lm.homeRepo()
	if r == nil || model.KeyOf(r.mainPath) != model.KeyOf(main) {
		t.Fatalf("home repo main = %q, want %q", r.mainPath, main)
	}
	top, err := r.svc.TopLevel(context.Background())
	if err != nil || model.KeyOf(top) != model.KeyOf(main) {
		t.Fatalf("home repo service rooted at %q (%v), want %q", top, err, main)
	}
}

// C3: the first switch into a member reads its whole repository (remotes,
// tags, previews, health …), not only the two lists its group showed.
func TestFirstSwitchIntoMemberReadsEverything(t *testing.T) {
	m := loadedModel(t)
	m, b := openTwo(t, m)
	nm, ok := m.switchRepo(b, "", true)
	if !ok {
		t.Fatal(nm.statusMsg)
	}
	um, _ := nm.Update(tea.WindowSizeMsg{Width: 160, Height: 50}) // the Update tail runs the kick
	m = um.(Model)
	for _, s := range []sourceKey{srcRemotes, srcTags, srcPreviews} {
		if !m.srcInflight[s] {
			t.Fatalf("source %v not read on the first switch into B", s)
		}
	}
	if !b.everLive {
		t.Fatal("B not marked as having been live")
	}
	// The second time: no full read.
	nm, _ = m.switchRepo(m.homeRepo(), "", true)
	nm, _ = nm.switchRepo(b, "", true)
	nm.srcInflight[srcTags] = false
	um, _ = nm.Update(tea.WindowSizeMsg{Width: 160, Height: 50})
	if um.(Model).srcInflight[srcTags] {
		t.Fatal("a later switch into B re-read everything")
	}
}

// C3: a member load that lands after the member became live fills the
// live lists instead of being dropped.
func TestMemberLoadForLiveMemberApplies(t *testing.T) {
	m := loadedModel(t)
	m, b := openTwo(t, m)
	nm, ok := m.switchRepo(b, "", true)
	if !ok {
		t.Fatal(nm.statusMsg)
	}
	m = nm
	m.branches = nil
	b.loaded = false // the first load is still in flight
	um, _ := m.Update(memberLoadedMsg{svc: b.svc, branches: []model.Branch{{Name: "late"}}, wts: worktreesPayload{worktrees: b.worktrees, headTimes: map[string]int64{}}})
	m = um.(Model)
	if len(m.branches) != 1 || m.branches[0].Name != "late" {
		t.Fatalf("live lists = %+v", m.branches)
	}
}

// C4: only keys that act on the row switch members; global actions and
// focus moves do not.
func TestGlobalKeysNeverSwitchMember(t *testing.T) {
	for _, k := range []string{"u", "P", "c", "right", "left", "f", "o", ",", "g", "!", "F", "#", "<", ">", "=", "shift+left", "r", "t", "?"} {
		if memberSwitchKey(k) {
			t.Errorf("%q switches members", k)
		}
	}
	for _, k := range []string{"enter", ".", "s", "b", "w", "d", "m", "e", "x", "X", "ctrl+s", "ctrl+g", "alt+1", "alt+5"} {
		if !memberSwitchKey(k) {
			t.Errorf("%q does not switch members", k)
		}
	}
	m := loadedModel(t)
	m.width, m.height = 160, 50
	m, b := openTwo(t, m)
	m = m.activateTab(panelBranches)
	row, _ := rowOfMember(m, panelBranches, 1)
	m.sel[panelBranches] = row
	um, _ := m.Update(keyMsg("right"))
	if um.(Model).liveRepo() == b {
		t.Fatal("a focus move switched members")
	}
}

// I1: branch row keys are member-qualified (both members have main), and
// a switch-first keeps the cursor on the row it acted on.
func TestBranchKeysMemberQualifiedAndCursorKept(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 160, 50
	m, _ = openTwo(t, m)
	l := m.listFor(panelBranches)
	keys := map[string]int{}
	for i := 0; i < l.Len(); i++ {
		keys[l.Key(i)]++
	}
	for k, n := range keys {
		if n > 1 {
			t.Fatalf("row key %q is shared by %d rows", k, n)
		}
	}
	m = m.activateTab(panelBranches)
	row, _ := rowOfMember(m, panelBranches, 1)
	m.sel[panelBranches] = row
	um, _ := m.Update(keyMsg("m")) // mark: acts on the row, switches first
	m = um.(Model)
	ents := m.branchEntries()
	idx := m.displayIndices(panelBranches)
	sel := m.sel[panelBranches]
	if sel < 0 || sel >= len(idx) || ents[idx[sel]].member != 1 || ents[idx[sel]].heading {
		t.Fatalf("cursor at %d is no longer on B's row", sel)
	}
}

// I2: the home repository's identity comes from the health probe's common
// dir (a linked-worktree launch), and a probe landing before home is
// seeded leaves the question pending, not settled.
func TestHealthProbeSetsHomeRepoIdentity(t *testing.T) {
	m := loadedModel(t)
	home := m.homeRepo()
	home.mainPath, home.key = "/wrong", model.KeyOf("/wrong")
	common := filepath.Join(m.currentWorktree, ".git")
	um, _ := m.Update(repoHealthMsg{gen: m.noticeGen, health: model.RepoHealth{GitCommonDir: common}})
	m = um.(Model)
	if model.KeyOf(m.homeRepo().mainPath) != model.KeyOf(m.currentWorktree) {
		t.Fatalf("main = %q", m.homeRepo().mainPath)
	}
	unseeded := loadedModel(t)
	unseeded = withWorkspacesFile(t, unseeded, []workspace.Entry{{Name: "shop", Repos: []string{unseeded.homeRepo().mainPath, secondRepo(t)}}})
	unseeded.home = ""
	nm, _ := unseeded.maybeAskWorkspace()
	if nm.workspaceAsked || !nm.workspaceAskPending {
		t.Fatalf("asked=%v pending=%v", nm.workspaceAsked, nm.workspaceAskPending)
	}
}

// I3: a member's machine-private config file counts, and the Settings
// popup writes to the settings repository's active file.
func TestMemberConfigUsesPrivateFile(t *testing.T) {
	m := loadedModel(t)
	home := m.homeRepo()
	other := secondRepo(t)
	private := config.PrivateRepoPath(other)
	if err := os.MkdirAll(filepath.Dir(private), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(private, []byte("[ui]\nwheel_step = 9\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	nm, cmd := m.openWorkspace(workspace.Entry{Name: "shop", Repos: []string{home.mainPath, other}, SettingsRepo: other})
	m = settleWorkspace(t, nm, cmd)
	if m.cfg.UI.WheelStep != 9 {
		t.Fatalf("wheel_step = %d, want 9 from B's private file", m.cfg.UI.WheelStep)
	}
	if m.repoConfigPath != private {
		t.Fatalf("repoConfigPath = %q, want %q", m.repoConfigPath, private)
	}
}

// I4: leaving a workspace whose other member has running sessions asks
// first; the first option leaves them running.
func TestLeaveWorkspaceWithSessionsAsks(t *testing.T) {
	m := loadedModel(t)
	m, b := openTwo(t, m)
	_ = startTestSession(t, m, "sleep 5")
	sb, err := domain.Sessions().Start(sessionSpecForTest("in-b", b.mainPath))
	if err != nil {
		t.Fatal(err)
	}
	nm, _ := m.leaveWorkspace()
	m = nm
	if _, ok := m.topLayer().(*memberDropPopup); !ok || !m.inWorkspace() {
		t.Fatalf("top=%T ws=%v", m.topLayer(), m.inWorkspace())
	}
	um, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = um.(Model)
	if m.inWorkspace() {
		t.Fatal("leave did not proceed")
	}
	if _, still := domain.Sessions().Get(sb.Info().ID); !still {
		t.Fatal("the session was stopped")
	}
}

// I5: opening another workspace keeps the members both share (their
// slots, their windows); a home in a member that goes adopts the viewed
// worktree first.
func TestOpenAnotherWorkspaceKeepsSharedMembers(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 160, 50
	m, b := openTwo(t, m)
	third := secondRepo(t)
	nm, ok := m.switchRepo(b, "", true) // look into B; home stays in A
	if !ok {
		t.Fatal(nm.statusMsg)
	}
	m = nm
	m = m.pushLayer(&diffView{rev: "abc1"}) // a window in B
	nm2, cmd := m.openWorkspace(workspace.Entry{Name: "infra", Repos: []string{b.mainPath, third}})
	m = settleWorkspace(t, nm2, cmd)
	if m.ws.name != "infra" || len(m.ws.members) != 2 || m.ws.members[0] != b {
		t.Fatalf("members = %v (B kept=%v)", m.ws.members, len(m.ws.members) > 0 && m.ws.members[0] == b)
	}
	if m.diffLayer() == nil {
		t.Fatal("B's window was lost")
	}
	if m.home != b.key || m.homeRepo() != b {
		t.Fatalf("home did not adopt B: home=%q", m.home)
	}
}

// I6: a missing member is refused by switchRepo; once its path exists it
// is revived and switched into.
func TestSwitchIntoMissingMemberRefusedThenRevived(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 160, 50
	home := m.homeRepo()
	gone := filepath.Join(t.TempDir(), "later")
	nm, cmd := m.openWorkspace(workspace.Entry{Name: "shop", Repos: []string{home.mainPath, gone}})
	m = settleWorkspace(t, nm, cmd)
	r := m.ws.members[1]
	if nm2, ok := m.switchRepo(r, "", true); ok || nm2.statusMsg == "" {
		t.Fatal("a missing member was switched into")
	}
	if out, err := exec.Command("git", "init", "-b", "main", gone).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	nm3, ok := m.switchRepo(r, "", true)
	if !ok || nm3.liveRepo() != r || r.missing || r.svc == nil {
		t.Fatalf("revive: ok=%v status=%q missing=%v", ok, nm3.statusMsg, r.missing)
	}
}
