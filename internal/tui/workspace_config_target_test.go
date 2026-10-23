package tui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/workspace"
)

// Probe round 2: the config file a write or a reload targets while a
// member other than the settings repository is on screen.

// moving the VIEWED member's repo config (private <-> committed)
// while looking at member B must not rebind the process-wide write target
// (the settings repository A's file) to B's.
func TestRepoConfigMoveInMemberKeepsSettingsTarget(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 160, 50
	m, b := openTwo(t, m)
	a := m.homeRepo()
	want := memberConfigPath(a.mainPath)
	if m.repoConfigPath != want {
		t.Fatalf("precondition: target %q, want %q", m.repoConfigPath, want)
	}
	if err := os.WriteFile(filepath.Join(b.mainPath, ".gg.toml"), []byte("[refresh]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	nm, ok := m.switchRepo(b, "", true)
	if !ok {
		t.Fatal(nm.statusMsg)
	}
	m = settleTail(t, nm)
	m = m.openRepoConfigLocation()
	p, _ := m.topLayer().(*repoConfigPopup)
	if p == nil || len(p.actions) == 0 {
		t.Fatalf("no repo-config popup/actions")
	}
	m, _ = p.run(m, p.actions[0])
	if m.repoConfigPath != want {
		t.Fatalf("process-wide write target rebound to %q, want settings repo %q", m.repoConfigPath, want)
	}
}

// a first-run reload of the config (the hosted page's session
// start; Start agent's and the task launch's detect do the same) reads
// m.repoConfigPath — the SETTINGS repository's file inside a workspace —
// and assigns it whole: viewing member B, B's own per-member sections
// (branch filters, tools, refresh, versions) are replaced by A's.
func TestFirstRunConfigReloadKeepsMemberSections(t *testing.T) {
	m := loadedModel(t).ensureWeb()
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
	if len(m.cfg.Branches.Filter) == 0 {
		t.Fatal("fixture: B's own filter is not in the live config")
	}
	global := filepath.Join(t.TempDir(), "config.toml")
	old := agentGlobalConfigPath
	agentGlobalConfigPath = func() string { return global }
	t.Cleanup(func() { agentGlobalConfigPath = old })
	reply := make(chan webSessionReply, 1)
	m, _, why := m.webSessionStartLeaf(webSessionRequestMsg{req: domain.SessionStartRequest{Worktree: b.mainPath, Terminal: true, Cols: 80, Rows: 24}, reply: reply})
	if why != "" {
		t.Fatal(why)
	}
	if len(m.cfg.Branches.Filter) == 0 || m.cfg.Branches.Filter[0].Name != "bfilter" {
		t.Fatalf("viewing B, the first-run reload replaced B's own sections with the settings repository's: filters=%+v", m.cfg.Branches.Filter)
	}
}
