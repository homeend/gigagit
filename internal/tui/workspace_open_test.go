package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/workspace"
)

// settleWorkspace feeds every message of cmd (and what they chain) back
// into the model until nothing is left — the member loads landing.
func settleWorkspace(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	queue := drainTimed(cmd)
	for i := 0; i < 200 && len(queue) > 0; i++ {
		msg := queue[0]
		queue = queue[1:]
		if msg == nil {
			continue
		}
		nm, next := m.Update(msg)
		m = nm.(Model)
		queue = append(queue, drainTimed(next)...)
	}
	return m
}

// drainTimed is drainBatch with a deadline per command: one that does not
// answer within a second (a watcher's listener, a poll tick) is left
// behind — the test drives those by hand.
func drainTimed(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	ch := make(chan tea.Msg, 1)
	go func() { ch <- cmd() }()
	var msg tea.Msg
	select {
	case msg = <-ch:
	case <-time.After(time.Second):
		return nil
	}
	if b, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range b {
			out = append(out, drainTimed(c)...)
		}
		return out
	}
	return []tea.Msg{msg}
}

// secondRepo is another real repository on disk, for a member.
func secondRepo(t *testing.T) string {
	t.Helper()
	dir, _ := newRepoDir(t)
	return dir
}

// Opening a workspace from its home member loads every other member in
// the background: both slots loaded, home still live, nothing re-rooted.
func TestOpenWorkspaceLoadsMembers(t *testing.T) {
	m := loadedModel(t)
	home := m.homeRepo()
	other := secondRepo(t)
	e := workspace.Entry{Name: "shop", Repos: []string{home.mainPath, other}}
	nm, cmd := m.openWorkspace(e)
	if !nm.inWorkspace() || nm.ws.name != "shop" || len(nm.ws.members) != 2 || nm.liveRepo() != home {
		t.Fatalf("after open: ws=%v live ok=%v", nm.inWorkspace(), nm.liveRepo() == home)
	}
	if nm.loading {
		t.Fatal("opening a workspace set m.loading")
	}
	if !strings.Contains(nm.statusMsg, "workspace shop") || !strings.Contains(nm.statusMsg, "2") {
		t.Fatalf("status = %q", nm.statusMsg)
	}
	m = settleWorkspace(t, nm, cmd)
	b := m.ws.members[1]
	if !b.loaded || len(b.branches) == 0 || len(b.worktrees) != 1 || b.svc == nil {
		t.Fatalf("member B not loaded: loaded=%v branches=%d worktrees=%d", b.loaded, len(b.branches), len(b.worktrees))
	}
	if m.liveRepo() != home || m.loading {
		t.Fatal("a member load touched the live member or m.loading")
	}
	// The grouped tabs show B's rows now.
	ents := m.worktreeEntries()
	seen := false
	for _, e := range ents {
		if e.member == 1 && !e.heading {
			seen = true
		}
	}
	if !seen {
		t.Fatalf("B has no worktree row: %+v", ents)
	}
}

// A member whose path is gone is kept as a greyed, empty group; the rest
// load; nothing hangs.
func TestOpenWorkspaceSkipsMissingMember(t *testing.T) {
	m := loadedModel(t)
	home := m.homeRepo()
	gone := filepath.Join(t.TempDir(), "unmounted", "repo")
	other := secondRepo(t)
	e := workspace.Entry{Name: "shop", Repos: []string{home.mainPath, gone, other}}
	nm, cmd := m.openWorkspace(e)
	m = settleWorkspace(t, nm, cmd)
	if len(m.ws.members) != 3 || !m.ws.members[1].missing || m.ws.members[1].svc != nil {
		t.Fatalf("members = %+v", m.ws.members)
	}
	if !m.ws.members[2].loaded {
		t.Fatal("the member after the missing one did not load")
	}
	ents := m.branchEntries()
	for i, e := range ents {
		if e.heading && e.member == 1 {
			if i+1 < len(ents) && ents[i+1].member == 1 {
				t.Fatal("the missing member has rows")
			}
			if !strings.Contains(m.listFor(panelBranches).Row(i), "(missing)") {
				t.Fatalf("heading = %q", m.listFor(panelBranches).Row(i))
			}
		}
	}
	if len(m.worktreeRing()) != 2 {
		t.Fatalf("ring = %+v, want A and the third member only", m.worktreeRing())
	}
}

// Leaving keeps the viewed member as the single repository and drops the
// rest; a workspace whose home lies elsewhere adopts the viewed worktree.
func TestLeaveWorkspaceKeepsLive(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 160, 50
	home := m.homeRepo()
	other := secondRepo(t)
	nm, cmd := m.openWorkspace(workspace.Entry{Name: "shop", Repos: []string{home.mainPath, other}})
	m = settleWorkspace(t, nm, cmd)
	b := m.ws.members[1]
	nm, ok := m.switchRepo(b, "", true) // a look into B
	if !ok {
		t.Fatalf("switch: %s", nm.statusMsg)
	}
	m = nm
	nm, _ = m.leaveWorkspace()
	m = nm
	if m.inWorkspace() || m.homeRepoView != b || m.liveRepo() != b {
		t.Fatalf("after leave: ws=%v home ok=%v live ok=%v", m.inWorkspace(), m.homeRepoView == b, m.liveRepo() == b)
	}
	if m.home != b.key || m.svc != b.svc {
		t.Fatalf("home did not adopt the viewed worktree: home=%q", m.home)
	}
	if m.statusMsg != "left workspace shop" {
		t.Fatalf("status = %q", m.statusMsg)
	}
}

// The process-wide config comes from the settings repository and stays
// while another member is viewed; the member's own sections follow it.
func TestApplyWorkspaceConfigUsesSettingsRepo(t *testing.T) {
	m := loadedModel(t)
	home := m.homeRepo()
	other := secondRepo(t)
	if err := os.WriteFile(filepath.Join(other, ".gg.toml"), []byte("[ui]\nwheel_step = 7\n[worktree]\npath_template = \"/b/{branch}\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home.mainPath, ".gg.toml"), []byte("[ui]\nwheel_step = 3\n[worktree]\npath_template = \"/a/{branch}\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	e := workspace.Entry{Name: "shop", Repos: []string{home.mainPath, other}, SettingsRepo: other}
	nm, cmd := m.openWorkspace(e)
	m = settleWorkspace(t, nm, cmd)
	if m.cfg.UI.WheelStep != 7 || m.cfg.Worktree.PathTemplate != "/a/{branch}" {
		t.Fatalf("live cfg: wheel=%d tmpl=%q (want B's [ui], A's [worktree])", m.cfg.UI.WheelStep, m.cfg.Worktree.PathTemplate)
	}
	b := m.ws.members[1]
	if b.cfg.UI.WheelStep != 7 || b.cfg.Worktree.PathTemplate != "/b/{branch}" {
		t.Fatalf("B cfg: wheel=%d tmpl=%q", b.cfg.UI.WheelStep, b.cfg.Worktree.PathTemplate)
	}
	nm, ok := m.switchRepo(b, "", true)
	if !ok {
		t.Fatal(nm.statusMsg)
	}
	if nm.cfg.UI.WheelStep != 7 || nm.cfg.Worktree.PathTemplate != "/b/{branch}" {
		t.Fatalf("after switch: wheel=%d tmpl=%q", nm.cfg.UI.WheelStep, nm.cfg.Worktree.PathTemplate)
	}
}

// --workspace names a workspace the opened repository must belong to.
func TestResolveStartWorkspace(t *testing.T) {
	m := loadedModel(t)
	home := m.homeRepo()
	p := filepath.Join(t.TempDir(), "workspaces.toml")
	if err := workspace.Save(p, []workspace.Entry{
		{Name: "mine", Repos: []string{home.mainPath}},
		{Name: "other", Repos: []string{filepath.Join(t.TempDir(), "x")}},
	}); err != nil {
		t.Fatal(err)
	}
	if e, err := resolveStartWorkspace(m.svc, p, "MINE"); err != nil || e.Name != "mine" {
		t.Fatalf("mine: %+v %v", e, err)
	}
	if _, err := resolveStartWorkspace(m.svc, p, "other"); err == nil || !strings.Contains(err.Error(), "not a member") {
		t.Fatalf("other: %v", err)
	}
	if _, err := resolveStartWorkspace(m.svc, p, "nope"); err == nil || !strings.Contains(err.Error(), "no workspace named") {
		t.Fatalf("nope: %v", err)
	}
}
