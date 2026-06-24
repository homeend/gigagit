package tui

import (
	"context"
	"testing"

	"github.com/gigagit/gg/internal/model"
)

// eagerModel builds a Commits-focused model with the given loaded commits and a
// real (FakeRunner-backed) feed in a known state.
func eagerModel(t *testing.T, commits []model.Commit) Model {
	t.Helper()
	m := newTestModelForReload(t)
	m.focus = panelCommits
	m.commits = commits
	m = m.rebuildCommitGraph()
	return m
}

func TestEagerAdvanceJumpsOnMatch(t *testing.T) {
	m := eagerModel(t, []model.Commit{{Hash: "a", Subject: "fix bug"}, {Hash: "b", Subject: "write docs"}})
	m.eager = eagerSearch{active: true, query: "docs", budget: 5}
	nm, _ := m.eagerAdvance()
	got := nm
	if got.eager.active {
		t.Fatal("a match must end the search")
	}
	if got.sel[panelCommits] != 1 {
		t.Fatalf("sel = %d, want 1 (the matching row)", got.sel[panelCommits])
	}
}

func TestEagerAdvancePagesWhenNoMatch(t *testing.T) {
	m := eagerModel(t, []model.Commit{{Hash: "a", Subject: "fix"}})
	// Fresh feed → CanLoadMore true.
	m.eager = eagerSearch{active: true, query: "zzz", budget: 5}
	nm, cmd := m.eagerAdvance()
	if cmd == nil {
		t.Fatal("no match + budget + loadable → should dispatch a page load")
	}
	if !nm.commitsLoading {
		t.Fatal("paging should set commitsLoading")
	}
	if nm.eager.budget != 4 {
		t.Fatalf("budget = %d, want 4 (decremented)", nm.eager.budget)
	}
}

func TestEagerAdvanceReportsExhausted(t *testing.T) {
	m := eagerModel(t, []model.Commit{{Hash: "a", Subject: "fix"}})
	// Exhaust the feed: short initial page (the fake serves 1 row < 50).
	m.feed.SetPageSizes(50, 50)
	m.feed.LoadInitial(context.Background())
	m.commits = m.feed.Snapshot().Commits
	m = m.rebuildCommitGraph()
	m.eager = eagerSearch{active: true, query: "zzz", budget: 5}
	nm, cmd := m.eagerAdvance()
	if cmd != nil || nm.eager.active {
		t.Fatal("exhausted feed with no match → stop, no load")
	}
	if nm.statusMsg == "" {
		t.Fatal("should report 'not found in full history'")
	}
}

