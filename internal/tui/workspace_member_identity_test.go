package tui

import (
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/forge/forgetest"
	"github.com/homeend/gigagit/internal/gittest"
	"github.com/homeend/gigagit/internal/workspace"
)

// runTail runs one Update (the tail: memberFirstLive, the kicks) and
// settles what it chains.
func runTail(t *testing.T, m Model) Model {
	t.Helper()
	nm, cmd := m.Update(tea.WindowSizeMsg{Width: 160, Height: 50})
	return settleWorkspace(t, nm.(Model), cmd)
}

// A member other than the one gg was opened in runs the forge probe on
// its first visit: its first visit reads every source, and the probe is
// what makes its Pull requests tab (and the palette's forge entries)
// appear. Serial: lifts the package-wide ForgeDisabled seam.
func TestMemberForgeIsProbedOnFirstVisit(t *testing.T) {
	m := loadedModel(t)
	m, b := openTwo(t, m)
	t.Setenv(forgetest.EnvBin, filepath.Join(t.TempDir(), "no-such-gh"))
	t.Setenv(forgetest.EnvFixtures, "")
	domain.ForgeDisabled = false
	t.Cleanup(func() { domain.ForgeDisabled = true })
	nm, ok := m.switchRepo(b, "", true)
	if !ok {
		t.Fatal(nm.statusMsg)
	}
	m = runTail(t, nm)
	if m.liveRepo() != b {
		t.Fatal("fixture: B is not live")
	}
	if !m.forgeProbeKicked && !m.prsInflight && !m.prsAnswered {
		t.Fatal("member B's first visit never probed its forge: its Pull requests tab can never appear")
	}
}

// A member's gg:// link name (its remote repository name) is read with
// its load: links copied while viewing member B name the repository the
// way gg opened in B alone does, not by the absolute path.
func TestMemberLinksUseItsRemoteName(t *testing.T) {
	m := loadedModel(t)
	home := m.homeRepo()
	other := secondRepo(t)
	gittest.Run(t, other, "remote", "add", "origin", "https://github.com/acme/widgets.git")
	alone := loadedModelAt(t, other)
	want, _ := alone.linkRepoFor("")
	if want.Name == "" {
		t.Fatal("fixture: gg opened in B alone has no link name")
	}
	nm, cmd := m.openWorkspace(workspace.Entry{Name: "shop", Repos: []string{home.mainPath, other}})
	m = settleWorkspace(t, nm, cmd)
	b := m.ws.members[1]
	nm, ok := m.switchRepo(b, "", true)
	if !ok {
		t.Fatal(nm.statusMsg)
	}
	m = runTail(t, nm)
	if got, _ := m.linkRepoFor(""); got.Name != want.Name {
		t.Fatalf("a link copied while viewing member B names %+v; gg opened in B alone names %q", got, want.Name)
	}
}
