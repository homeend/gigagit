package tui

import (
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/repos"
	"github.com/homeend/gigagit/internal/workspace"
)

func wsType(t *testing.T, m Model, s string) Model {
	t.Helper()
	for _, r := range s {
		nm, _ := m.Update(keyMsg(string(r)))
		m = nm.(Model)
	}
	return m
}

func wsPress(t *testing.T, m Model, key string) (Model, tea.Cmd) {
	t.Helper()
	nm, cmd := m.Update(keyMsg(key))
	return nm.(Model), cmd
}

// openWSPopup opens the Workspaces popup on a model with a fresh registry.
func openWSPopup(t *testing.T, m Model, ws []workspace.Entry) (Model, *workspacePopup) {
	t.Helper()
	m = withWorkspacesFile(t, m, ws)
	m.width, m.height = 120, 40
	nm, _ := m.openWorkspacePopup()
	p := layerOf[*workspacePopup](nm)
	if p == nil {
		t.Fatalf("top = %T", nm.topLayer())
	}
	return nm, p
}

// n creates, r renames, d deletes (after a confirm whose first option keeps
// it); every edit is on disk at once.
func TestWorkspacePopupCreateRenameDelete(t *testing.T) {
	m := loadedModel(t)
	m, _ = openWSPopup(t, m, nil)
	m, _ = wsPress(t, m, "n")
	m = wsType(t, m, "shop")
	m, _ = wsPress(t, m, "enter")
	if got := workspace.Load(m.workspacesPath()); len(got) != 1 || got[0].Name != "shop" {
		t.Fatalf("after create: %+v", got)
	}
	m, _ = wsPress(t, m, "r")
	for range 4 {
		m, _ = wsPress(t, m, "backspace")
	}
	m = wsType(t, m, "store")
	m, _ = wsPress(t, m, "enter")
	if got := workspace.Load(m.workspacesPath()); len(got) != 1 || got[0].Name != "store" {
		t.Fatalf("after rename: %+v", got)
	}
	m, _ = wsPress(t, m, "d")
	m, _ = wsPress(t, m, "enter") // the first option keeps it
	if got := workspace.Load(m.workspacesPath()); len(got) != 1 {
		t.Fatalf("the safe option deleted: %+v", got)
	}
	m, _ = wsPress(t, m, "d")
	m, _ = wsPress(t, m, "down")
	m, _ = wsPress(t, m, "enter")
	if got := workspace.Load(m.workspacesPath()); len(got) != 0 {
		t.Fatalf("after delete: %+v", got)
	}
}

// A bad name is refused inline; the box stays open until valid or esc.
func TestWorkspacePopupNameValidationInline(t *testing.T) {
	m := loadedModel(t)
	m, p := openWSPopup(t, m, nil)
	m, _ = wsPress(t, m, "n")
	m = wsType(t, m, "a/b")
	m, _ = wsPress(t, m, "enter")
	if p.inputErr == "" || p.level != wsLevelInput {
		t.Fatalf("err=%q level=%d", p.inputErr, p.level)
	}
	m, _ = wsPress(t, m, "esc")
	if p.level != wsLevelList || len(workspace.Load(m.workspacesPath())) != 0 {
		t.Fatal("esc did not cancel the name")
	}
}

// A full workspace's members list fits an 80x24 terminal: every member row
// and the key hints are on screen (the list is not windowed).
func TestWorkspacePopupFullMembersFit80x24(t *testing.T) {
	m := loadedModel(t)
	full := workspace.Entry{Name: "full"}
	for i := 1; i <= workspace.MaxRepos; i++ {
		full.Repos = append(full.Repos, "/r/member"+strconv.Itoa(i))
	}
	m, _ = openWSPopup(t, m, []workspace.Entry{full})
	m.width, m.height = 80, 24
	m, _ = wsPress(t, m, "enter") // members of "full"
	screen := m.View()
	if lines := strings.Count(screen, "\n") + 1; lines > 24 {
		t.Fatalf("view is %d lines tall", lines)
	}
	for i := 1; i <= workspace.MaxRepos; i++ {
		if !strings.Contains(screen, "member"+strconv.Itoa(i)+" ") {
			t.Fatalf("member %d not on screen:\n%s", i, screen)
		}
	}
	if !strings.Contains(screen, "[esc] back") {
		t.Fatalf("hints not on screen:\n%s", screen)
	}
}

// A member past the cap, or one already there, is refused with a status line.
func TestWorkspacePopupAddCapRefused(t *testing.T) {
	m := loadedModel(t)
	full := workspace.Entry{Name: "full"}
	for i := 1; i <= workspace.MaxRepos; i++ {
		full.Repos = append(full.Repos, "/r/"+strconv.Itoa(i))
	}
	m, p := openWSPopup(t, m, []workspace.Entry{full})
	m, _ = wsPress(t, m, "enter") // members of "full"
	nm, _ := m.Update(wsPathResolvedMsg{main: "/r/extra"})
	m = nm.(Model)
	if got := workspace.Load(m.workspacesPath()); len(got[0].Repos) != workspace.MaxRepos || !strings.Contains(m.statusMsg, strconv.Itoa(workspace.MaxRepos)) {
		t.Fatalf("cap: repos=%d status=%q", len(got[0].Repos), m.statusMsg)
	}
	nm, _ = m.Update(wsPathResolvedMsg{main: "/r/2"})
	m = nm.(Model)
	if !strings.Contains(m.statusMsg, "already") {
		t.Fatalf("duplicate: status=%q", m.statusMsg)
	}
	_ = p
}

// The add picker lists the repositories open now, then the registry's
// recents that are not among them, then Path….
func TestWorkspacePopupPickerSources(t *testing.T) {
	m := loadedModel(t)
	m.statePath = filepath.Join(t.TempDir(), "repos.toml")
	third := secondRepo(t)
	if err := repos.Touch(m.statePath, third, "", m.loadStart); err != nil {
		t.Fatal(err)
	}
	m, b := openTwo(t, m)
	if err := workspace.Save(m.workspacesPath(), []workspace.Entry{{Name: "shop", Repos: []string{m.homeRepo().mainPath, b.mainPath}}}); err != nil {
		t.Fatal(err)
	}
	m.width, m.height = 120, 40
	nm, probe := m.openWorkspacePopup()
	m = nm
	p := layerOf[*workspacePopup](m)
	m, _ = wsPress(t, m, "enter") // members
	m, _ = wsPress(t, m, "a")     // picker
	u, _ := m.Update(probe())     // the registry, read off-thread
	m = u.(Model)
	rows := p.pickerRows(m) // both open repositories are members already: not offered
	if len(rows) != 2 || rows[0].kind != pickRecent || rows[1].kind != pickPath {
		t.Fatalf("rows = %+v", rows)
	}
	if filepath.Clean(rows[0].path) != filepath.Clean(third) {
		t.Fatalf("recent = %q, want %q", rows[0].path, third)
	}
	m, _ = wsPress(t, m, "esc")
	m, _ = wsPress(t, m, "esc")
	m, _ = wsPress(t, m, "n") // a new, empty workspace offers the open ones first
	m = wsType(t, m, "fresh")
	m, _ = wsPress(t, m, "enter")
	m, _ = wsPress(t, m, "enter")
	m, _ = wsPress(t, m, "a")
	rows = p.pickerRows(m)
	if len(rows) != 4 || rows[0].kind != pickOpen || rows[1].kind != pickOpen || rows[2].kind != pickRecent || rows[3].kind != pickPath {
		t.Fatalf("fresh rows = %+v", rows)
	}
}

// K/J move a member; the open workspace's groups follow the new order.
func TestWorkspacePopupMoveReordersGroups(t *testing.T) {
	m := loadedModel(t)
	m, b := openTwo(t, m)
	home := m.homeRepo()
	m, p := openWSPopup(t, m, []workspace.Entry{{Name: "shop", Repos: []string{home.mainPath, b.mainPath}}})
	m, _ = wsPress(t, m, "enter")
	m, _ = wsPress(t, m, "down") // member 1
	m, _ = wsPress(t, m, "K")
	got := workspace.Load(m.workspacesPath())
	if filepath.Clean(got[0].Repos[0]) != b.mainPath || p.msel != 0 {
		t.Fatalf("file order = %v msel=%d", got[0].Repos, p.msel)
	}
	if m.ws.members[0] != b || m.ws.members[1] != home || m.liveRepo() != home {
		t.Fatalf("members not reordered: %v live ok=%v", m.ws.members, m.liveRepo() == home)
	}
}

// s marks the settings repository (✎) and applies its config; s again clears.
func TestWorkspacePopupSettingsRepoMarker(t *testing.T) {
	m := loadedModel(t)
	m, b := openTwo(t, m)
	home := m.homeRepo()
	m, p := openWSPopup(t, m, []workspace.Entry{{Name: "shop", Repos: []string{home.mainPath, b.mainPath}}})
	m, _ = wsPress(t, m, "enter")
	m, _ = wsPress(t, m, "down")
	m, _ = wsPress(t, m, "s")
	got := workspace.Load(m.workspacesPath())
	if filepath.Clean(got[0].SettingsRepo) != b.mainPath || m.ws.settings != b.mainPath || !strings.Contains(p.render(m, ""), "✎") {
		t.Fatalf("settings repo = %q ws=%q", got[0].SettingsRepo, m.ws.settings)
	}
	m, _ = wsPress(t, m, "s")
	if got := workspace.Load(m.workspacesPath()); got[0].SettingsRepo != "" || m.ws.settings != "" {
		t.Fatal("s again did not clear")
	}
}

// Removing the viewed member moves the view to the first remaining member
// before its slot goes (Review Focus 5).
func TestRemoveViewedMemberMovesView(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 160, 50
	m, b := openTwo(t, m)
	home := m.homeRepo()
	nm, ok := m.switchRepo(b, "", true)
	if !ok {
		t.Fatal(nm.statusMsg)
	}
	m = nm
	m, _ = openWSPopup(t, m, []workspace.Entry{{Name: "shop", Repos: []string{home.mainPath, b.mainPath}}})
	m, _ = wsPress(t, m, "enter")
	m, _ = wsPress(t, m, "down") // B
	m, _ = wsPress(t, m, "d")
	if m.liveRepo() != home || len(m.ws.members) != 1 || m.ws.members[0] != home {
		t.Fatalf("live ok=%v members=%d", m.liveRepo() == home, len(m.ws.members))
	}
	if got := workspace.Load(m.workspacesPath()); len(got[0].Repos) != 1 {
		t.Fatalf("file = %v", got[0].Repos)
	}
}

// A member with running sessions asks before it is dropped; the first
// option leaves them running and they stay in the manager.
func TestDropMemberWithSessionsAsks(t *testing.T) {
	m := loadedModel(t)
	m, b := openTwo(t, m)
	home := m.homeRepo()
	_ = startTestSession(t, m, "sleep 5")
	sb, err := domain.Sessions().Start(sessionSpecForTest("in-b", b.mainPath))
	if err != nil {
		t.Fatal(err)
	}
	m, _ = openWSPopup(t, m, []workspace.Entry{{Name: "shop", Repos: []string{home.mainPath, b.mainPath}}})
	m, _ = wsPress(t, m, "enter")
	m, _ = wsPress(t, m, "down")
	m, _ = wsPress(t, m, "d")
	dp, ok := m.topLayer().(*memberDropPopup)
	if !ok || !strings.Contains(dp.options()[0], "running") {
		t.Fatalf("top = %T options=%v", m.topLayer(), dp)
	}
	m, _ = wsPress(t, m, "enter")
	if len(m.ws.members) != 1 {
		t.Fatalf("members = %d", len(m.ws.members))
	}
	if _, still := domain.Sessions().Get(sb.Info().ID); !still {
		t.Fatal("leave running stopped the session")
	}
}

// Inside a workspace the Settings popup names the settings repository.
func TestSettingsTitleNamesSettingsRepo(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	m, _ = openTwo(t, m)
	nm, _ := m.openSettings()
	out := nm.topLayer().render(nm, "")
	if !strings.Contains(out, "Settings — "+m.settingsRepo().name) {
		t.Fatalf("render:\n%s", out)
	}
}
