package tui

import (
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/workspace"
)

// withWorkspacesFile points the model at a fresh state dir holding ws.
func withWorkspacesFile(t *testing.T, m Model, ws []workspace.Entry) Model {
	t.Helper()
	m.statePath = filepath.Join(t.TempDir(), "repos.toml")
	if err := workspace.Save(m.workspacesPath(), ws); err != nil {
		t.Fatal(err)
	}
	return m
}

// The dialog appears only when the opened repository belongs to a
// workspace; a registry without it leaves the single-repository screen.
func TestLaunchDialogOnlyForMembers(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	home := m.homeRepo().mainPath
	m = withWorkspacesFile(t, m, []workspace.Entry{{Name: "shop", Repos: []string{home, secondRepo(t)}}})
	nm, _ := m.maybeAskWorkspace()
	if _, ok := nm.topLayer().(*workspaceLaunchPopup); !ok {
		t.Fatalf("top = %T, want the launch dialog", nm.topLayer())
	}
	other := loadedModel(t)
	other = withWorkspacesFile(t, other, []workspace.Entry{{Name: "shop", Repos: []string{secondRepo(t)}}})
	no, _ := other.maybeAskWorkspace()
	if no.topLayer() != nil || !no.workspaceAsked {
		t.Fatalf("a non-member got a dialog: %T", no.topLayer())
	}
}

// esc opens the repository alone: no workspace, no popup, nothing lost.
func TestLaunchDialogEscOpensAlone(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	m = withWorkspacesFile(t, m, []workspace.Entry{{Name: "shop", Repos: []string{m.homeRepo().mainPath, secondRepo(t)}}})
	nm, _ := m.maybeAskWorkspace()
	m = nm
	nm2, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = nm2.(Model)
	if m.topLayer() != nil || m.inWorkspace() {
		t.Fatalf("esc: layer=%T ws=%v", m.topLayer(), m.inWorkspace())
	}
	again, _ := m.maybeAskWorkspace()
	if again.topLayer() != nil {
		t.Fatal("the dialog asked twice")
	}
}

// enter on a workspace row opens it.
func TestLaunchDialogEnterOpensWorkspace(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	m = withWorkspacesFile(t, m, []workspace.Entry{{Name: "shop", Repos: []string{m.homeRepo().mainPath, secondRepo(t)}}})
	nm, _ := m.maybeAskWorkspace()
	m = nm
	nm2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = settleWorkspace(t, nm2.(Model), cmd)
	if !m.inWorkspace() || m.ws.name != "shop" || m.topLayer() != nil {
		t.Fatalf("enter: ws=%v top=%T", m.inWorkspace(), m.topLayer())
	}
}

// Every workspace the repository belongs to is a row, then "alone".
func TestLaunchDialogListsEveryWorkspace(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	home := m.homeRepo().mainPath
	m = withWorkspacesFile(t, m, []workspace.Entry{
		{Name: "shop", Repos: []string{home, secondRepo(t)}},
		{Name: "solo", Repos: []string{secondRepo(t)}},
		{Name: "infra", Repos: []string{secondRepo(t), home}},
	})
	nm, _ := m.maybeAskWorkspace()
	p, ok := nm.topLayer().(*workspaceLaunchPopup)
	if !ok {
		t.Fatalf("top = %T", nm.topLayer())
	}
	if len(p.rows) != 3 || p.rows[0].entry == nil || p.rows[0].entry.Name != "shop" || p.rows[1].entry.Name != "infra" || p.rows[2].entry != nil {
		t.Fatalf("rows = %+v", p.rows)
	}
	out := p.render(nm, "")
	if !strings.Contains(out, "Open workspace shop") || !strings.Contains(out, "alone") {
		t.Fatalf("render:\n%s", out)
	}
}

// --workspace already chose: no dialog.
func TestNoDialogWithWorkspaceFlag(t *testing.T) {
	m := loadedModel(t)
	e := workspace.Entry{Name: "shop", Repos: []string{m.homeRepo().mainPath, secondRepo(t)}}
	m = withWorkspacesFile(t, m, []workspace.Entry{e})
	m.startWorkspace = &e
	nm, _ := m.maybeAskWorkspace()
	if nm.topLayer() != nil {
		t.Fatal("the flag and the dialog both fired")
	}
}
