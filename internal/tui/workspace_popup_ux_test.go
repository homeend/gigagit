package tui

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/repos"
	"github.com/homeend/gigagit/internal/workspace"
)

// A path picked for workspace a resolves off-thread; by the time it lands
// the user may have moved on: it still joins a, and the screen (another
// workspace's members, a half-typed name) is left as it is.
func TestLateResolveLandsInItsWorkspace(t *testing.T) {
	m := loadedModel(t)
	m, p := openWSPopup(t, m, []workspace.Entry{{Name: "a"}, {Name: "b"}})
	m, _ = wsPress(t, m, "enter") // a's members
	m, _ = wsPress(t, m, "a")     // picker
	m, cmd := wsPress(t, m, "enter")
	if cmd == nil {
		t.Fatalf("no resolve cmd; level=%d", p.level)
	}
	m, _ = wsPress(t, m, "esc") // picker -> members
	m, _ = wsPress(t, m, "esc") // members -> list
	m, _ = wsPress(t, m, "down")
	m, _ = wsPress(t, m, "enter") // b's members
	nm, _ := m.Update(cmd())
	m = nm.(Model)
	ws := workspace.Load(m.workspacesPath())
	a, _ := workspace.Find(ws, "a")
	b, _ := workspace.Find(ws, "b")
	if len(a.Repos) != 1 || len(b.Repos) != 0 {
		t.Fatalf("the repository picked for a landed in: a=%v b=%v", a.Repos, b.Repos)
	}
	if p.level != wsLevelMembers || !strings.EqualFold(p.ws[p.cur].Name, "b") {
		t.Fatalf("the screen moved: level=%d cur=%q", p.level, p.ws[p.cur].Name)
	}
	m, _ = wsPress(t, m, "esc")
	m, _ = wsPress(t, m, "n")
	m = wsType(t, m, "half")
	nm, _ = m.Update(wsPathResolvedMsg{main: "/late/two", workspace: "a"})
	m = nm.(Model)
	if p.level != wsLevelInput || p.input.Value() != "half" {
		t.Fatalf("the half-typed name box was left: level=%d value=%q", p.level, p.input.Value())
	}
	if a, _ = workspace.Find(workspace.Load(m.workspacesPath()), "a"); len(a.Repos) != 2 {
		t.Fatalf("a = %v", a.Repos)
	}
}

// The R switcher's slow-filesystem warning follows the cursor's REPOSITORY
// row, below the workspace rows.
func TestRepoPopupSlowTooltipFollowsRepositoryRow(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	p := &repoPopup{
		workspaces: []workspace.Entry{{Name: "shop", Repos: []string{"/l/local"}}},
		entries:    []repos.Entry{{Path: "/l/local"}, {Path: "/mnt/c/slow"}},
		foreign:    map[string]bool{"/mnt/c/slow": true},
	}
	box := p.box(m)
	p.sel = 1 // the /l/local repository row (row 0 is the workspace)
	if _, _, _, ok := p.slowTooltip(m, box, 120, 40); ok {
		t.Errorf("cursor on the LOCAL repository: the foreign-filesystem warning shows")
	}
	p.sel = 2 // the /mnt/c/slow row
	if _, _, _, ok := p.slowTooltip(m, box, 120, 40); !ok {
		t.Errorf("cursor on the FOREIGN repository: no warning")
	}
}

// R opens with the cursor on the current repository's row (the habit "R,
// enter" stays a no-op); the workspace rows above it are reached with up.
func TestRepoPopupOpensOnTheCurrentRepository(t *testing.T) {
	m, _ := seededWorkspaces(t)
	m.width, m.height = 120, 40
	u, _ := m.Update(keyMsg("R"))
	m = u.(Model)
	p := layerOf[*repoPopup](m)
	if ri := p.repoSel(); ri < 0 || !samePathTUI(p.visible()[ri].Path, m.currentWorktree) {
		t.Fatalf("cursor sel=%d repoSel=%d, want the current repository's row", p.sel, ri)
	}
}

// The Workspaces popup marks every workspace the viewed repository belongs
// to (spec §2), not only the open one.
func TestWorkspacePopupMarksWorkspacesOfViewedRepo(t *testing.T) {
	m := loadedModel(t)
	home := m.homeRepo().mainPath
	m, p := openWSPopup(t, m, []workspace.Entry{{Name: "shop", Repos: []string{home, "/x/b"}}, {Name: "other", Repos: []string{"/x/b"}}})
	out := p.render(m, "")
	if !strings.Contains(out, "● shop") || strings.Contains(out, "● other") {
		t.Fatalf("marks:\n%s", out)
	}
}

// The add picker does not offer repositories that are members already.
func TestWorkspacePickerSkipsMembers(t *testing.T) {
	m := loadedModel(t)
	m.statePath = filepath.Join(t.TempDir(), "repos.toml")
	third := secondRepo(t)
	if err := repos.Touch(m.statePath, third, "", m.loadStart); err != nil {
		t.Fatal(err)
	}
	m, b := openTwo(t, m)
	home := m.homeRepo().mainPath
	if err := workspace.Save(m.workspacesPath(), []workspace.Entry{{Name: "shop", Repos: []string{home, b.mainPath}}}); err != nil {
		t.Fatal(err)
	}
	m.width, m.height = 120, 40
	nm, probe := m.openWorkspacePopup()
	m = nm
	p := layerOf[*workspacePopup](m)
	m, _ = wsPress(t, m, "enter")
	m, _ = wsPress(t, m, "a")
	u, _ := m.Update(probe())
	m = u.(Model)
	for _, r := range p.pickerRows(m) {
		if r.kind != pickPath && (samePathTUI(r.path, home) || samePathTUI(r.path, b.mainPath)) {
			t.Fatalf("a member is offered: %+v", r)
		}
	}
}
