package tui

import (
	"os/exec"
	"path/filepath"
	"testing"
)

// addWorktree adds a second worktree on a new branch and returns its path.
func addWorktree(t *testing.T, m Model, name string) string {
	t.Helper()
	other := filepath.Join(t.TempDir(), name)
	if out, err := exec.Command("git", "-C", m.currentWorktree, "worktree", "add", "-b", name, other).CombinedOutput(); err != nil {
		t.Fatalf("worktree add: %v\n%s", err, out)
	}
	return other
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
	other := m.ensureView(addWorktree(t, m, "wt2"))
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
	other := addWorktree(t, m, "wt2")
	a := m.ensureView(other + string(filepath.Separator))
	b := m.ensureView(other)
	if a != b || a.path != filepath.Clean(other) {
		t.Fatalf("slots differ: %p %p path=%q", a, b, a.path)
	}
}
