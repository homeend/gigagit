package tui

import (
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

func rebaseOpsModel(parents []string) Model {
	m := New(nil)
	m.width, m.height = 80, 30
	m.loading = false
	if m.sel == nil {
		m.sel = map[panel]int{}
	}
	m.focus = panelCommits
	m.status = model.WorkingTreeStatus{Branch: "main"}
	m.commits = []model.Commit{{Hash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Subject: "x", Parents: parents}}
	m.sel[panelCommits] = 0
	return m
}

func TestCommitMoveDropRowsPresent(t *testing.T) {
	m := rebaseOpsModel([]string{"p"}) // non-merge, non-root
	for _, id := range []string{"commit-drop", "commit-move-up", "commit-move-down"} {
		if _, ok := findRow(availableActions(m), id); !ok {
			t.Fatalf("availableActions missing %q on a non-merge commit", id)
		}
	}
}

func TestCommitMoveDropRowsGating(t *testing.T) {
	// Merge commit → absent.
	if _, ok := rebaseOpsModel([]string{"p1", "p2"}).commitDropRow(); ok {
		t.Fatal("no drop row on a merge commit")
	}
	// Root commit (no parent) → absent.
	if _, ok := rebaseOpsModel(nil).commitDropRow(); ok {
		t.Fatal("no drop row on a root commit")
	}
	// Detached HEAD → absent.
	m := rebaseOpsModel([]string{"p"})
	m.status.Branch = ""
	if _, ok := m.commitDropRow(); ok {
		t.Fatal("no drop row on a detached HEAD")
	}
	// Off the Commits panel → absent.
	m2 := rebaseOpsModel([]string{"p"})
	m2.focus = panelBranches
	if _, ok := m2.commitMoveUpRow(); ok {
		t.Fatal("no row off the Commits panel")
	}
	// While running → absent.
	m3 := rebaseOpsModel([]string{"p"})
	m3.running = true
	if _, ok := m3.commitMoveDownRow(); ok {
		t.Fatal("no row while running")
	}
}
