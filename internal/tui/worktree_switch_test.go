package tui

import (
	"path/filepath"
	"testing"
)

func TestEnterOnWorktreePanelSwitches(t *testing.T) {
	dir, repo := newRepoDir(t)
	m := New(repo)
	updated, _ := m.Update(m.loadCmd()())
	m = updated.(Model)

	wt := filepath.Join(filepath.Dir(dir), "wt-enter")
	runGit(t, dir, "worktree", "add", "-b", "feature/e", wt, "main")
	// Reload so the new worktree is in m.worktrees.
	updated, _ = m.Update(m.loadCmd()())
	m = updated.(Model)

	// Focus the Worktrees panel and select the non-current worktree.
	m.focus = panelWorktrees
	idx := -1
	for i, w := range m.worktrees {
		if w.Path != m.currentWorktree {
			idx = i
		}
	}
	if idx < 0 {
		t.Fatalf("expected a second worktree in %v", m.worktrees)
	}
	m.sel[panelWorktrees] = idx

	updated, cmd := m.Update(keyMsg("enter"))
	m = updated.(Model)
	resolvedWant, _ := filepath.EvalSymlinks(m.worktrees[idx].Path)
	resolvedGot, _ := filepath.EvalSymlinks(m.switchTarget)
	if resolvedGot != resolvedWant {
		t.Fatalf("switchTarget = %q, want %q", resolvedGot, resolvedWant)
	}
	if cmd == nil {
		t.Fatal("enter on a worktree should return a reload command")
	}
}
