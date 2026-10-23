package tui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/homeend/gigagit/internal/agentdocs"
	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/gittest"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/steer"
	"github.com/homeend/gigagit/internal/workspace"
)

// Repository-wide state that must follow the member on screen (spec §6: the
// per-member sections, the <seq> counters, the session-docs directory) or
// stay with gg's home (the published snapshot) or the settings repository
// (the process-wide write target) — never the single-repository "the one
// repo" shortcut.

func writeTestFile(t *testing.T, p, s string) {
	t.Helper()
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The branch-filter popup's repo scope, viewed in B, is B's own config;
// the reload after the write keeps the workspace merge (B's rules stay).
func TestBranchFilterWritesGoToViewedMember(t *testing.T) {
	dirA := gittest.BasicRepo(t, "hi\n")
	writeTestFile(t, filepath.Join(dirA, ".gg.toml"), "[[branches.filter]]\nslot = 1\nname = \"afilter\"\nprefix = \"a/\"\n")
	m := loadedModelAt(t, dirA)
	home := m.homeRepo()
	other := secondRepo(t)
	writeTestFile(t, filepath.Join(other, ".gg.toml"), "[[branches.filter]]\nslot = 1\nname = \"bfilter\"\nprefix = \"b/\"\n")
	nm, cmd := m.openWorkspace(workspace.Entry{Name: "shop", Repos: []string{home.mainPath, other}})
	m = settleWorkspace(t, nm, cmd)
	b := m.ws.members[1]
	nm, ok := m.switchRepo(b, "", true)
	if !ok {
		t.Fatal(nm.statusMsg)
	}
	m = nm
	v := &branchFilterView{scope: bfScopeRepo}
	if wp := v.writePath(m); wp != memberConfigPath(other) {
		t.Errorf("repo-scope write path while viewing B = %q, want B's %q", wp, memberConfigPath(other))
	}
	m = m.reloadConfigAfterBranchFilterWrite()
	if m.branchFilters[0].Name != "bfilter" {
		t.Errorf("after a branch-filter write while viewing B, slot 1 = %q, want B's 'bfilter'", m.branchFilters[0].Name)
	}
	m = m.reloadToolConfig()
	if m.branchFilters[0].Name != "bfilter" {
		t.Errorf("after a tools write while viewing B, slot 1 = %q", m.branchFilters[0].Name)
	}
}

// A full load while B is viewed (a conflict tool's end, an AI task) keeps
// the Settings write target on the settings repository and home in place.
func TestReloadInMemberKeepsSettingsTarget(t *testing.T) {
	m := loadedModel(t)
	m, b := openTwo(t, m)
	a := m.homeRepo()
	aHome := m.homeWorktree()
	want := m.repoConfigPath
	if want != memberConfigPath(a.mainPath) {
		t.Fatalf("precondition: repoConfigPath=%q want %q", want, memberConfigPath(a.mainPath))
	}
	nm, ok := m.switchRepo(b, "", true)
	if !ok {
		t.Fatal(nm.statusMsg)
	}
	m = nm
	um, _ := m.Update(m.loadCmd()())
	m = um.(Model)
	if m.homeRepo() != a || m.homeWorktree() != aHome {
		t.Errorf("home moved: homeRepo A=%v homeWorktree=%q want %q", m.homeRepo() == a, m.homeWorktree(), aHome)
	}
	if m.repoConfigPath != want {
		t.Errorf("after a reload in B the Settings write target is %q, want the settings repository's %q", m.repoConfigPath, want)
	}
	nm, _ = m.switchRepo(m.homeRepo(), "", true)
	if nm.repoConfigPath != want {
		t.Errorf("back on A the Settings write target is %q", nm.repoConfigPath)
	}
}

// The <seq> counters' home (gitCommonDir) is the viewed member's.
func TestGitCommonDirFollowsMember(t *testing.T) {
	m := loadedModel(t)
	m, b := openTwo(t, m)
	before := m.gitCommonDir
	nm, ok := m.switchRepo(b, "", true)
	if !ok {
		t.Fatal(nm.statusMsg)
	}
	m = nm
	if want := filepath.Join(b.mainPath, ".git"); !model.SamePath(m.gitCommonDir, want) {
		t.Fatalf("gitCommonDir after switch to B = %q (was %q), want %q", m.gitCommonDir, before, want)
	}
	nm, _ = m.switchRepo(m.homeRepo(), "", true)
	if !model.SamePath(nm.gitCommonDir, before) || before == "" {
		t.Fatalf("back on A gitCommonDir = %q, want %q", nm.gitCommonDir, before)
	}
}

// With no settings_repo the settings repository is the member gg was
// OPENED in, fixed while the workspace is open: an adopt into B and a
// later edit (a reorder) keep A's process-wide config.
func TestSettingsRepoStaysOpenedInMember(t *testing.T) {
	m := loadedModel(t)
	home := m.homeRepo()
	other := secondRepo(t)
	writeTestFile(t, filepath.Join(home.mainPath, ".gg.toml"), "[ui]\nwheel_step = 3\n")
	writeTestFile(t, filepath.Join(other, ".gg.toml"), "[ui]\nwheel_step = 7\n")
	e := workspace.Entry{Name: "shop", Repos: []string{home.mainPath, other}}
	m = withWorkspacesFile(t, m, []workspace.Entry{e})
	nm, cmd := m.openWorkspace(e)
	m = settleWorkspace(t, nm, cmd)
	if m.wsCfg.UI.WheelStep != 3 {
		t.Fatalf("wsCfg wheel=%d", m.wsCfg.UI.WheelStep)
	}
	b := m.ws.members[1]
	tm, c := m.guardedReRoot(b.mainPath, false, true) // an adopt
	m = settleWorkspace(t, tm.(Model), c)
	if m.homeRepo() != b {
		t.Fatal("adopt did not move home")
	}
	if m.settingsRepo() != home {
		t.Fatalf("settings repository after the adopt = %s, want the opened-in %s", m.settingsRepo().name, home.name)
	}
	e2 := workspace.Entry{Name: "shop", Repos: []string{other, home.mainPath}}
	m, c = m.applyWorkspaceEdit(e2)
	m = settleWorkspace(t, m, c)
	if m.wsCfg.UI.WheelStep != 3 || m.repoConfigPath != memberConfigPath(home.mainPath) {
		t.Errorf("after adopt + reorder: wheel_step=%d target=%q, want the opened-in member's", m.wsCfg.UI.WheelStep, m.repoConfigPath)
	}
}

// A looked-at member's worktree attaches its session documents where the
// CLI in that repository writes them: under ITS common dir, not home's.
func TestSessionDocsDirIsTheMembersOwn(t *testing.T) {
	m := loadedModel(t)
	m.docs = agentdocs.New()
	um, _ := m.Update(snapshotTargetCmd(m.homeSvc())())
	m = um.(Model)
	if m.snapshotCommonDir == "" {
		t.Fatal("no snapshot common dir")
	}
	m, b := openTwo(t, m)
	nm, ok := m.switchRepo(b, "", true)
	if !ok {
		t.Fatal(nm.statusMsg)
	}
	m = nm
	want := steer.DocsDir(config.SessionSteerDir(filepath.Join(b.mainPath, ".git"), b.mainPath))
	if got := m.docs.Attached(domain.CheckoutKey(b.mainPath)); got != want {
		t.Fatalf("B's docs attached at\n  %q\nthe CLI in B writes to\n  %q", got, want)
	}
}

// The published snapshot is gg's OWN identity: looking at B, its branch
// and head are home's (A's), never B's.
func TestSnapshotIdentityStaysHomeWhileLookingAtMember(t *testing.T) {
	m := loadedModel(t)
	um, _ := m.Update(snapshotTargetCmd(m.homeSvc())())
	m = um.(Model)
	m, b := openTwo(t, m)
	gittest.Run(t, b.mainPath, "checkout", "-q", "-b", "only-in-b")
	m = settleWorkspace(t, m, memberLoadCmd(b))
	aBranch, aHead := m.status.Branch, m.currentBranchTipHash()
	nm, ok := m.switchRepo(b, "", true)
	if !ok {
		t.Fatal(nm.statusMsg)
	}
	m = nm
	um, _ = m.Update(m.readSourceCmd(t.Context(), srcStatus, reloadOpts{manual: true})())
	m = um.(Model)
	id := m.snapRepoIdentity()
	if id.Branch != aBranch || id.Head != aHead {
		t.Fatalf("snapshot = %+v, want A's branch %q head %q (B's is %q)", id, aBranch, aHead, m.status.Branch)
	}
	if !model.SamePath(id.Viewed, b.mainPath) {
		t.Fatalf("snapshot viewed = %q, want B's %q", id.Viewed, b.mainPath)
	}
}

// A session the hosted page starts in A's worktree while B is viewed is
// recorded under A.
func TestWebSessionStartLabelsTheWorktreesRepo(t *testing.T) {
	m := loadedModel(t)
	_ = startTestSession(t, m, "sleep 5") // installs a manager
	m, b := openTwo(t, m)
	aWT := m.homeWorktree()
	nm, ok := m.switchRepo(b, "", true)
	if !ok {
		t.Fatal(nm.statusMsg)
	}
	m = nm
	reply := make(chan webSessionReply, 1)
	req := domain.SessionStartRequest{Worktree: aWT, Command: config.ToolCommand{Category: "session", Name: "Sh", Mode: "session", Command: "sh -c 'sleep 5'"}, Cols: 40, Rows: 10}
	_, start, why := m.webSessionStartLeaf(webSessionRequestMsg{req: req, reply: reply})
	if why != "" {
		t.Fatal(why)
	}
	out := start().(webSessionStartedMsg)
	if out.err != nil {
		t.Fatal(out.err)
	}
	s, _ := domain.Sessions().Get(out.id)
	if s.Info().Repo != filepath.Base(m.homeRepo().mainPath) {
		t.Fatalf("session in %s recorded under repo %q (B is %q)", aWT, s.Info().Repo, filepath.Base(b.mainPath))
	}
}
