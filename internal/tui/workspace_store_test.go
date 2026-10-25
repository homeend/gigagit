package tui

import (
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/workspace"
)

// Edits made in the popup are applied to the ENTRY as it is on disk now,
// so another gg process's edit of the same workspace survives.
func TestPopupMemberAddKeepsConcurrentAdd(t *testing.T) {
	m := loadedModel(t)
	home := m.homeRepo().mainPath
	m, _ = openWSPopup(t, m, []workspace.Entry{{Name: "shop", Repos: []string{home}}})
	m, _ = wsPress(t, m, "enter") // shop's members
	if _, err := workspace.Update(m.workspacesPath(), func(ws []workspace.Entry) []workspace.Entry {
		for i := range ws {
			if ws[i].Name == "shop" {
				_ = ws[i].Add("/other/process/x")
			}
		}
		return ws
	}); err != nil {
		t.Fatal(err)
	}
	nm, _ := m.Update(wsPathResolvedMsg{main: "/this/process/y"})
	m = nm.(Model)
	e, _ := workspace.Find(workspace.Load(m.workspacesPath()), "shop")
	if !e.Has("/this/process/y") || !e.Has("/other/process/x") {
		t.Fatalf("one process's member was lost: shop = %v", e.Repos)
	}
}

// n with a name another process created meanwhile is refused against the
// file as it is, never replacing that workspace with an empty one.
func TestPopupCreateChecksTheFile(t *testing.T) {
	m := loadedModel(t)
	m, p := openWSPopup(t, m, nil)
	if _, err := workspace.Update(m.workspacesPath(), func(ws []workspace.Entry) []workspace.Entry {
		return append(ws, workspace.Entry{Name: "infra", Repos: []string{"/a", "/b"}})
	}); err != nil {
		t.Fatal(err)
	}
	m, _ = wsPress(t, m, "n")
	m = wsType(t, m, "infra")
	m, _ = wsPress(t, m, "enter")
	e, _ := workspace.Find(workspace.Load(m.workspacesPath()), "infra")
	if len(e.Repos) != 2 {
		t.Fatalf("the other process's workspace was replaced: infra = %+v", e)
	}
	if p.inputErr == "" {
		t.Fatal("the duplicate was not refused inline")
	}
}

// Deleting the OPEN workspace asks about its members' running sessions
// FIRST; Cancel keeps the definition on disk.
func TestDeleteOpenWorkspaceCancelKeepsFile(t *testing.T) {
	m := loadedModel(t)
	m, b := openTwo(t, m)
	home := m.homeRepo()
	_ = startTestSession(t, m, "sleep 5")
	if _, err := domain.Sessions().Start(sessionSpecForTest("in-b", b.mainPath)); err != nil {
		t.Fatal(err)
	}
	m, _ = openWSPopup(t, m, []workspace.Entry{{Name: "shop", Repos: []string{home.mainPath, b.mainPath}}})
	m, _ = wsPress(t, m, "d")
	m, _ = wsPress(t, m, "down")
	m, _ = wsPress(t, m, "enter")
	if _, ok := m.topLayer().(*memberDropPopup); !ok {
		t.Fatalf("top = %T", m.topLayer())
	}
	m, _ = wsPress(t, m, "esc") // cancel
	if _, onDisk := workspace.Find(workspace.Load(m.workspacesPath()), "shop"); !onDisk || !m.inWorkspace() {
		t.Fatalf("cancel: onDisk=%v open=%v", onDisk, m.inWorkspace())
	}
	m, _ = wsPress(t, m, "d")
	m, _ = wsPress(t, m, "down")
	m, _ = wsPress(t, m, "enter")
	m, _ = wsPress(t, m, "enter") // leave the sessions running
	if _, onDisk := workspace.Find(workspace.Load(m.workspacesPath()), "shop"); onDisk || m.inWorkspace() {
		t.Fatalf("confirm: onDisk=%v open=%v", onDisk, m.inWorkspace())
	}
}

// A hand-edited definition is normalised on open: one repository spelled
// twice is one member, and the list is cut at the cap.
func TestOpenWorkspaceNormalisesHandEdits(t *testing.T) {
	m := loadedModel(t)
	home := m.homeRepo().mainPath
	nm, cmd := m.openWorkspace(workspace.Entry{Name: "dup", Repos: []string{home, home + "/", secondRepo(t)}})
	m = settleWorkspace(t, nm, cmd)
	if len(m.ws.members) != 2 || len(m.worktreeRing()) != 2 {
		t.Fatalf("members=%d ring=%d", len(m.ws.members), len(m.worktreeRing()))
	}
	m2 := loadedModel(t)
	rs := []string{m2.homeRepo().mainPath}
	for range workspace.MaxRepos + 1 {
		rs = append(rs, secondRepo(t))
	}
	nm, cmd = m2.openWorkspace(workspace.Entry{Name: "big", Repos: rs})
	m2 = settleWorkspace(t, nm, cmd)
	if len(m2.ws.members) != workspace.MaxRepos {
		t.Fatalf("members = %d, want the cap %d", len(m2.ws.members), workspace.MaxRepos)
	}
}
