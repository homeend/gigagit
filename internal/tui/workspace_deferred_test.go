package tui

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/gittest"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/repos"
	"github.com/homeend/gigagit/internal/workspace"
)

// ctrl+\ on another member's session is an ADOPT (spec §4): home moves
// into that member, the workspace stays open, the console shows.
func TestOpenMemberSessionAdopts(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 160, 50
	m, b := openTwo(t, m)
	_ = startTestSession(t, m, "sleep 5")
	s, err := domain.Sessions().Start(sessionSpecForTest("in-b", b.mainPath))
	if err != nil {
		t.Fatal(err)
	}
	m, _ = m.openSessionAnywhere(s.Info().ID)
	if !m.inWorkspace() || m.liveRepo() != b || m.home != b.key {
		t.Fatalf("ws=%v live ok=%v home=%q want %q", m.inWorkspace(), m.liveRepo() == b, m.home, b.key)
	}
	if m.console == nil || m.console.id != s.Info().ID {
		t.Fatal("the console did not open")
	}
}

// A refresh of the member on screen replaces its own worktrees' head
// times and marks and keeps the sleeping members' (their date sort and ⚑
// survive until their own next read).
func TestLiveRefreshKeepsSleepingMembersDatesAndMarks(t *testing.T) {
	m := loadedModel(t)
	m, b := openTwo(t, m)
	gittest.Run(t, b.mainPath, "commit", "--allow-empty", "-m", "b-two") // B's own head, distinct from A's
	m = settleWorkspace(t, m, memberLoadCmd(b))
	if len(b.worktrees) == 0 || b.worktrees[0].Head == "" || b.worktrees[0].Head == m.worktrees[0].Head {
		t.Fatalf("B worktrees = %+v", b.worktrees)
	}
	bHead, bPath := b.worktrees[0].Head, filepath.Clean(b.worktrees[0].Path)
	m.headTimes[bHead] = 4242
	m.worktreeMarks[bPath] = domain.WorktreeMark{Reserved: true}
	nm, _ := m.Update(m.readSourceCmd(context.Background(), srcWorktrees, reloadOpts{manual: true})())
	m = nm.(Model)
	if m.headTimes[bHead] != 4242 {
		t.Fatalf("B's head time = %d after the live refresh", m.headTimes[bHead])
	}
	if !m.worktreeMarks[bPath].Reserved {
		t.Fatal("B's mark was dropped by the live refresh")
	}
	if _, ok := m.headTimes[m.worktrees[0].Head]; !ok {
		t.Fatal("the live member's own head time did not land")
	}
}

// The Workspaces popup reads the registry and stats member paths off the
// Update thread: the picker's recents and the (missing) marks come from
// the probe command's result, not from a read inside render.
func TestWorkspacePopupProbeOffThread(t *testing.T) {
	m := loadedModel(t)
	m.statePath = filepath.Join(t.TempDir(), "repos.toml")
	third := secondRepo(t)
	if err := repos.Touch(m.statePath, third, "", m.loadStart); err != nil {
		t.Fatal(err)
	}
	gone := filepath.Join(t.TempDir(), "gone")
	if err := workspace.Save(m.workspacesPath(), []workspace.Entry{{Name: "shop", Repos: []string{m.homeRepo().mainPath, gone}}}); err != nil {
		t.Fatal(err)
	}
	m.width, m.height = 120, 40
	nm, cmd := m.openWorkspacePopup()
	m = nm
	p := layerOf[*workspacePopup](m)
	m, _ = wsPress(t, m, "enter") // members
	if out := p.render(m, ""); strings.Contains(out, "(missing)") {
		t.Fatal("render stat'ed the path itself")
	}
	m, _ = wsPress(t, m, "a") // picker
	for _, r := range p.pickerRows(m) {
		if r.kind == pickRecent {
			t.Fatal("the picker read the registry inside the Update thread")
		}
	}
	msg := cmd()
	probe, ok := msg.(wsProbeMsg)
	if !ok {
		t.Fatalf("the popup's command returned %T", msg)
	}
	nm2, _ := m.Update(probe)
	m = nm2.(Model)
	var recent bool
	for _, r := range p.pickerRows(m) {
		if r.kind == pickRecent && filepath.Clean(r.path) == filepath.Clean(third) {
			recent = true
		}
	}
	if !recent {
		t.Fatal("the probe did not fill the recents")
	}
	m, _ = wsPress(t, m, "esc") // back to members
	if out := p.render(m, ""); !strings.Contains(out, "(missing)") {
		t.Fatalf("the probe did not mark the gone member:\n%s", out)
	}
}

// A member whose directory is gone is marked missing by its own load
// (off-thread), not by a stat inside openWorkspace.
func TestMissingMemberVerdictComesFromItsLoad(t *testing.T) {
	m := loadedModel(t)
	home := m.homeRepo()
	gone := filepath.Join(t.TempDir(), "unmounted", "repo")
	nm, cmd := m.openWorkspace(workspace.Entry{Name: "shop", Repos: []string{home.mainPath, gone}})
	if nm.ws.members[1].missing {
		t.Fatal("openWorkspace judged the path itself")
	}
	m = settleWorkspace(t, nm, cmd)
	if r := m.ws.members[1]; !r.missing || r.svc != nil {
		t.Fatalf("after the load: missing=%v svc=%v", r.missing, r.svc != nil)
	}
}

// Removing the member on screen while an operation runs is refused as a
// whole: the file and the open workspace both keep it, the status says why.
func TestRemoveViewedMemberRefusedKeepsFile(t *testing.T) {
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
	m, _ = wsPress(t, m, "down") // B, the viewed one
	m.running = true
	m, _ = wsPress(t, m, "d")
	if got := workspace.Load(m.workspacesPath()); len(got[0].Repos) != 2 {
		t.Fatalf("the file lost the member: %v", got[0].Repos)
	}
	if len(m.ws.members) != 2 || m.liveRepo() != b {
		t.Fatalf("members=%d live ok=%v", len(m.ws.members), m.liveRepo() == b)
	}
	if m.statusMsg != i18n.T("an operation is running — switch once it has finished") {
		t.Fatalf("status = %q", m.statusMsg)
	}
}

// An edit made in the popup is applied to the file as it is NOW, so a
// workspace another gg process added since the popup opened survives.
func TestWorkspacePopupEditMergesConcurrentChange(t *testing.T) {
	m := loadedModel(t)
	m, _ = openWSPopup(t, m, []workspace.Entry{{Name: "shop"}})
	if err := workspace.Save(m.workspacesPath(), []workspace.Entry{{Name: "shop"}, {Name: "other", Repos: []string{"/r/o"}}}); err != nil {
		t.Fatal(err)
	}
	m, _ = wsPress(t, m, "r")
	for range 4 {
		m, _ = wsPress(t, m, "backspace")
	}
	m = wsType(t, m, "store")
	m, _ = wsPress(t, m, "enter")
	got := workspace.Load(m.workspacesPath())
	if len(got) != 2 || got[0].Name != "store" || got[1].Name != "other" {
		t.Fatalf("file = %+v", got)
	}
}
