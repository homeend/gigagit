package tui

import (
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
)

// TestDeleteKeyRemovesWorktreeThroughModal presses `d` on a linked worktree and
// answers the remove-scope modal with "worktree-only", then drives the op to
// completion and asserts the worktree is gone from disk and the panel.
func TestDeleteKeyRemovesWorktreeThroughModal(t *testing.T) {
	dir, repo := newRepoDir(t)
	wt := filepath.Join(filepath.Dir(dir), "wt-del")
	runGit(t, dir, "worktree", "add", "-b", "feature/del", wt, "main")

	m := New(domain.New(repo))
	loaded, _ := m.Update(m.loadCmd()())
	m = loaded.(Model)

	// Focus the Worktrees panel and select the linked (non-current) worktree.
	m.focus = panelWorktrees
	idx := -1
	for i, w := range m.worktrees {
		if w.Path != m.currentWorktree {
			idx = i
		}
	}
	if idx < 0 {
		t.Fatalf("expected a linked worktree in %v", m.worktrees)
	}
	m.sel[panelWorktrees] = idx

	updated, cmd := m.Update(keyMsg("d"))
	m = updated.(Model)

	answered := false
	for i := 0; i < 100 && m.running; i++ {
		if m.modal != nil {
			if m.modal.req.ID != "remove-scope" {
				t.Fatalf("unexpected decision %q", m.modal.req.ID)
			}
			// selection 0 == "worktree-only"
			u, _ := m.Update(keyMsg("enter"))
			m = u.(Model)
			answered = true
			continue
		}
		if cmd == nil {
			t.Fatal("no command but op still running")
		}
		u, next := m.Update(cmd())
		m = u.(Model)
		cmd = next
	}
	if m.running {
		t.Fatal("operation did not finish")
	}
	if !answered {
		t.Fatal("expected a remove-scope decision modal")
	}
	// Apply the post-op reload. op completion now calls reloadSourcesCmd which
	// returns a tea.Batch of per-source reads; drive each sub-command through Update.
	if cmd != nil {
		msg := cmd()
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, subCmd := range batch {
				u, _ := m.Update(subCmd())
				m = u.(Model)
			}
		} else {
			u, _ := m.Update(msg)
			m = u.(Model)
		}
	}
	if _, err := os.Stat(wt); !os.IsNotExist(err) {
		t.Fatalf("worktree dir still present: %v", err)
	}
	for _, w := range m.worktrees {
		if w.Path == wt {
			t.Fatal("removed worktree still in panel after reload")
		}
	}
}

// TestSelectionClampAfterWorktreeReload deletes the last worktree out-of-band
// and reloads; the stale selection index must be clamped so subsequent indexing
// does not panic.
func TestSelectionClampAfterWorktreeReload(t *testing.T) {
	dir, repo := newRepoDir(t)
	wt := filepath.Join(filepath.Dir(dir), "wt-clamp")
	runGit(t, dir, "worktree", "add", "-b", "feature/clamp", wt, "main")

	m := New(domain.New(repo))
	loaded, _ := m.Update(m.loadCmd()())
	m = loaded.(Model)
	if len(m.worktrees) < 2 {
		t.Fatalf("expected 2 worktrees, got %d", len(m.worktrees))
	}
	m.focus = panelWorktrees
	m.sel[panelWorktrees] = len(m.worktrees) - 1 // select the last row

	// Remove the worktree out-of-band, then reload.
	runGit(t, dir, "worktree", "remove", wt)
	reloaded, _ := m.Update(m.loadCmd()())
	m = reloaded.(Model)

	if got := m.sel[panelWorktrees]; got > len(m.worktrees)-1 {
		t.Fatalf("sel[panelWorktrees] = %d not clamped to <= %d", got, len(m.worktrees)-1)
	}
}
