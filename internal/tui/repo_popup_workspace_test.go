package tui

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/workspace"
)

// seededWorkspaces is seededModel plus a workspaces.toml beside its
// registry: "shop" (this repo + the other), "infra" (the other alone).
func seededWorkspaces(t *testing.T) (Model, string) {
	t.Helper()
	m, _, otherDir := seededModel(t)
	home := m.homeRepo().mainPath
	if err := workspace.Save(m.workspacesPath(), []workspace.Entry{
		{Name: "shop", Repos: []string{home, otherDir}},
		{Name: "infra", Repos: []string{otherDir}},
	}); err != nil {
		t.Fatal(err)
	}
	return m, otherDir
}

// The Workspaces group heads the list, in registry order, only when
// workspaces exist.
func TestRepoPopupListsWorkspaces(t *testing.T) {
	m, _ := seededWorkspaces(t)
	m.width, m.height = 120, 40
	u, _ := m.Update(keyMsg("R"))
	m = u.(Model)
	p := layerOf[*repoPopup](m)
	if p == nil {
		t.Fatal("R should open the repo popup")
	}
	ws := p.wsVisible()
	if len(ws) != 2 || ws[0].Name != "shop" || ws[1].Name != "infra" {
		t.Fatalf("workspaces = %+v", ws)
	}
	out := p.render(m, "")
	if !strings.Contains(out, "shop") || !strings.Contains(out, "infra") || !strings.Contains(out, "other-zebra") {
		t.Fatalf("render:\n%s", out)
	}
	// Without a registry file: no group, the list is today's.
	plain, _, _ := seededModel(t)
	plain.width, plain.height = 120, 40
	u, _ = plain.Update(keyMsg("R"))
	if pp := layerOf[*repoPopup](u.(Model)); pp == nil || len(pp.wsVisible()) != 0 || pp.sel != 0 {
		t.Fatal("a registry without workspaces grew a group")
	}
}

// Enter on a workspace row opens it; the popup closes.
func TestRepoPopupEnterOpensWorkspace(t *testing.T) {
	m, _ := seededWorkspaces(t)
	m.width, m.height = 120, 40
	u, _ := m.Update(keyMsg("R"))
	m = u.(Model)
	for range 2 { // the cursor opens on the current repository; the workspace rows are above it
		u, _ = m.Update(keyMsg("up"))
		m = u.(Model)
	}
	u, cmd := m.Update(keyMsg("enter")) // row 0 = shop
	m = settleWorkspace(t, u.(Model), cmd)
	if !m.inWorkspace() || m.ws.name != "shop" || layerOf[*repoPopup](m) != nil {
		t.Fatalf("ws=%v top=%T", m.inWorkspace(), m.topLayer())
	}
}

// Enter on the open workspace's own row just closes the popup.
func TestRepoPopupEnterOnOpenWorkspaceNoop(t *testing.T) {
	m, _ := seededWorkspaces(t)
	m.width, m.height = 120, 40
	e, _ := workspace.Find(workspace.Load(m.workspacesPath()), "shop")
	nm, cmd := m.openWorkspace(e)
	m = settleWorkspace(t, nm, cmd)
	ws := m.ws
	u, _ := m.Update(keyMsg("R"))
	m = u.(Model)
	for range 2 {
		u, _ = m.Update(keyMsg("up"))
		m = u.(Model)
	}
	if p := layerOf[*repoPopup](m); p == nil || p.sel != 0 {
		t.Fatal("the cursor is not on the open workspace's row")
	}
	u, _ = m.Update(keyMsg("enter"))
	m = u.(Model)
	if m.ws != ws || layerOf[*repoPopup](m) != nil {
		t.Fatal("enter on the open workspace re-opened it or kept the popup")
	}
}

// The filter matches a workspace by a member's directory name.
func TestRepoPopupFilterMatchesMemberName(t *testing.T) {
	m, _ := seededWorkspaces(t)
	m.width, m.height = 120, 40
	u, _ := m.Update(keyMsg("R"))
	m = u.(Model)
	p := layerOf[*repoPopup](m)
	for _, r := range "zebra" {
		u, _ = m.Update(keyMsg(string(r)))
		m = u.(Model)
	}
	if ws := p.wsVisible(); len(ws) != 2 {
		t.Fatalf("both workspaces hold other-zebra: %+v", ws)
	}
	for _, r := range "shop" {
		u, _ = m.Update(keyMsg(string(r)))
		m = u.(Model)
	}
	// "zebrashop" matches nothing.
	if ws := p.wsVisible(); len(ws) != 0 {
		t.Fatalf("workspaces = %+v", ws)
	}
}

// Inside a workspace the member repositories carry the ● marker.
func TestRepoPopupMemberMarked(t *testing.T) {
	m, otherDir := seededWorkspaces(t)
	m.width, m.height = 120, 40
	e, _ := workspace.Find(workspace.Load(m.workspacesPath()), "shop")
	nm, cmd := m.openWorkspace(e)
	m = settleWorkspace(t, nm, cmd)
	u, _ := m.Update(keyMsg("R"))
	m = u.(Model)
	p := layerOf[*repoPopup](m)
	out := p.render(m, "")
	marked := 0
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "● ") && (strings.Contains(line, filepath.Base(otherDir)) || strings.Contains(line, m.homeRepo().name)) {
			marked++
		}
	}
	if marked < 2 {
		t.Fatalf("members not marked:\n%s", out)
	}
}

// "Leave workspace" is in the palette only inside a workspace.
func TestPaletteLeaveWorkspaceVisibility(t *testing.T) {
	m, _ := seededWorkspaces(t)
	has := func(m Model) bool {
		for _, c := range m.availablePaletteCommands() {
			if c.label == "Leave workspace" {
				return true
			}
		}
		return false
	}
	if has(m) {
		t.Fatal("Leave workspace shown with no workspace open")
	}
	e, _ := workspace.Find(workspace.Load(m.workspacesPath()), "shop")
	nm, cmd := m.openWorkspace(e)
	m = settleWorkspace(t, nm, cmd)
	if !has(m) {
		t.Fatal("Leave workspace missing inside a workspace")
	}
}
