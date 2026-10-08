package tui

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// addWorktree adds a second worktree on a new branch, reloads the Worktrees
// list so the model knows it, and returns its path.
func addWorktree(t *testing.T, m Model, name string) (Model, string) {
	t.Helper()
	other := filepath.Join(t.TempDir(), name)
	if out, err := exec.Command("git", "-C", m.currentWorktree, "worktree", "add", "-b", name, other).CombinedOutput(); err != nil {
		t.Fatalf("worktree add: %v\n%s", err, out)
	}
	nm, _ := m.Update(m.readSourceCmd(context.Background(), srcWorktrees, reloadOpts{manual: true})())
	return nm.(Model), other
}

// The first load seeds home: one slot, viewed = home = the worktree gg runs in.
func TestFirstLoadSeedsTheHomeSlot(t *testing.T) {
	m := loadedModel(t)
	if m.home == "" || m.viewed != m.home || m.home != filepath.Clean(m.currentWorktree) {
		t.Fatalf("home=%q viewed=%q current=%q", m.home, m.viewed, m.currentWorktree)
	}
	if v, ok := m.views[m.home]; !ok || v.svc != m.svc {
		t.Fatalf("views[home] = %+v, want the live service", v)
	}
}

// saveView then loadView is a round trip of the Status panel, its cursor and
// its marks; the live service is the slot's.
func TestSaveAndLoadViewRoundTrip(t *testing.T) {
	m := loadedModel(t)
	m.sel[panelFiles] = 3
	m.fileMarks = map[string]bool{"a.txt": true}
	m = m.saveView()
	home := m.views[m.home]
	m, otherPath := addWorktree(t, m, "wt2")
	other := m.ensureView(otherPath)
	m = m.loadView(other)
	if m.viewed != other.path || m.svc != other.svc || m.sel[panelFiles] != 0 || len(m.fileMarks) != 0 || m.currentWorktree != other.path {
		t.Fatalf("after load: viewed=%q sel=%d marks=%v current=%q", m.viewed, m.sel[panelFiles], m.fileMarks, m.currentWorktree)
	}
	m = m.loadView(home)
	if m.sel[panelFiles] != 3 || !m.fileMarks["a.txt"] || m.svc != home.svc {
		t.Fatalf("home not restored: sel=%d marks=%v", m.sel[panelFiles], m.fileMarks)
	}
}

// ensureView is keyed by the cleaned path and reuses the slot.
func TestEnsureViewIsKeyedByCleanPath(t *testing.T) {
	m := loadedModel(t)
	m, other := addWorktree(t, m, "wt2")
	a := m.ensureView(other + string(filepath.Separator))
	b := m.ensureView(other)
	if a != b || a.path != filepath.Clean(other) {
		t.Fatalf("slots differ: %p %p path=%q", a, b, a.path)
	}
}

// landView runs the swap's refresh kick (what the Update tail launches) and
// applies its status read, as the runtime would.
func landView(t *testing.T, m Model) Model {
	t.Helper()
	cmd := m.viewKickCmd()
	m.viewKick = false
	if cmd == nil {
		return m
	}
	for _, msg := range drainBatch(cmd) {
		if _, ok := msg.(dataAvailableMsg); !ok {
			continue // the watcher/docs results need a live loop; the status read is what we assert
		}
		nm, _ := m.Update(msg)
		m = nm.(Model)
	}
	return m
}

// drainBatch runs a cmd (a tea.Batch or a single cmd) and collects the
// messages its leaves return.
func drainBatch(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if b, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range b {
			out = append(out, drainBatch(c)...)
		}
		return out
	}
	return []tea.Msg{msg}
}

// Switching to another worktree shows ITS files; switching back restores
// home's cursor and marks without a reload.
func TestSwitchViewShowsTheOtherWorktreesStatusAndRestoresHome(t *testing.T) {
	m := loadedModel(t)
	m, other := addWorktree(t, m, "wt2")
	if err := os.WriteFile(filepath.Join(other, "only-there.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.sel[panelFiles] = 1
	m.fileMarks = map[string]bool{"home.txt": true}
	m, ok := m.switchView(other)
	if !ok || m.viewed != filepath.Clean(other) || !m.viewKick {
		t.Fatalf("switch: ok=%v viewed=%q kick=%v msg=%q", ok, m.viewed, m.viewKick, m.statusMsg)
	}
	if m.home == m.viewed {
		t.Fatal("home must stay the worktree gg runs in")
	}
	m = landView(t, m)
	var seen bool
	for _, f := range m.status.Files {
		seen = seen || f.Path == "only-there.txt"
	}
	if !seen {
		t.Fatalf("status after switch = %+v, want wt2's untracked file", m.status.Files)
	}
	m, _ = m.switchView(m.home)
	if m.sel[panelFiles] != 1 || !m.fileMarks["home.txt"] || m.viewed != m.home {
		t.Fatalf("home not restored: sel=%d marks=%v viewed=%q", m.sel[panelFiles], m.fileMarks, m.viewed)
	}
	for _, f := range m.status.Files {
		if f.Path == "only-there.txt" {
			t.Fatal("home shows wt2's file")
		}
	}
}

// A status read launched for the old slot lands after the swap: dropped.
func TestSwitchViewDropsAStaleStatusRead(t *testing.T) {
	m := loadedModel(t)
	m, other := addWorktree(t, m, "wt2")
	stale := m.readSourceCmd(context.Background(), srcStatus, reloadOpts{manual: true})
	m, _ = m.switchView(other)
	m = landView(t, m)
	before := m.status
	nm, _ := m.Update(stale())
	m = nm.(Model)
	if len(m.status.Files) != len(before.Files) || m.viewed != filepath.Clean(other) {
		t.Fatalf("a stale read landed: %+v", m.status.Files)
	}
}

// The swap is a no-op for the viewed path, refused for a path that is not a
// worktree of this repository, and refused while an operation runs.
func TestSwitchViewRefusals(t *testing.T) {
	m := loadedModel(t)
	m, other := addWorktree(t, m, "wt2")
	if nm, ok := m.switchView(m.viewed); !ok || nm.viewKick {
		t.Fatalf("same path: ok=%v kick=%v", ok, nm.viewKick)
	}
	if _, ok := m.switchView(t.TempDir()); ok {
		t.Fatal("a foreign path must be refused")
	}
	m.running = true
	if nm, ok := m.switchView(other); ok || nm.statusMsg == "" {
		t.Fatalf("running op: ok=%v msg=%q", ok, nm.statusMsg)
	}
}

// Only the live slot is watched: the slot left behind has no watcher.
func TestSwitchViewPutsTheLeavingSlotToSleep(t *testing.T) {
	m := loadedModel(t)
	m, other := addWorktree(t, m, "wt2")
	m.watchSupported = true
	m, _ = m.switchView(other)
	if v := m.views[m.home]; v.watcher != nil || v.docWatch.w != nil {
		t.Fatalf("home slot still awake: %+v", v)
	}
	if m.watcher != nil || m.watchSupported {
		t.Fatal("the arriving slot starts with no watcher until its kick lands")
	}
}

// The Update tail launches the kick once and clears the flag.
func TestUpdateTailLaunchesTheViewKick(t *testing.T) {
	m := loadedModel(t)
	m, other := addWorktree(t, m, "wt2")
	m, _ = m.switchView(other)
	nm, cmd := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	if nm.(Model).viewKick || cmd == nil {
		t.Fatalf("kick=%v cmd=%v", nm.(Model).viewKick, cmd)
	}
}
