package tui

import (
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

// Held End (or PgDn / j) on the Commits panel pages per keystroke (progressive
// loading while held). The runaway "kept loading after release" was caused by
// the O(N) commit-graph rebuild running on every page: it blocked the event loop
// so a held-key auto-repeat backlog piled up and kept pulling pages long after
// release. The rebuild is now deferred (debounced) off the hot path — it runs
// once when paging settles — so the loop stays responsive, no backlog builds,
// and paging a burst is O(total) instead of O(total²).

func holdPagingModel(t *testing.T) Model {
	t.Helper()
	m := newTestModelForReload(t) // fresh feed → NeedsMore true, CanLoadMore true
	m.focus = panelCommits
	m.commits = []model.Commit{{Hash: "a", Subject: "one"}}
	m.sel[panelCommits] = len(m.commits) - 1
	return m
}

func TestMaybeLoadMoreDispatchesWhenIdle(t *testing.T) {
	t.Parallel()
	m := holdPagingModel(t)
	mm, cmd := m.maybeLoadMoreCommits()
	if cmd == nil {
		t.Fatal("idle fresh feed at end should dispatch a page load")
	}
	if !mm.commitsLoading {
		t.Fatal("dispatching a page must set commitsLoading")
	}
}

func TestMaybeLoadMoreDroppedWhileLoading(t *testing.T) {
	t.Parallel()
	m := holdPagingModel(t)
	m.commitsLoading = true // a page/reload is already in flight
	_, cmd := m.maybeLoadMoreCommits()
	if cmd != nil {
		t.Fatal("a load already in flight → must not dispatch another")
	}
}

func TestEndKeyDropsLoadWhileLoading(t *testing.T) {
	t.Parallel()
	m := holdPagingModel(t)
	m.commitsLoading = true
	_, cmd := m.Update(keyMsg("end"))
	if cmd != nil {
		t.Fatal("End while a page is loading must not queue another load")
	}
}

func TestCtrlLDropsLoadWhileLoading(t *testing.T) {
	t.Parallel()
	m := holdPagingModel(t)
	m.commitsLoading = true
	_, cmd := m.Update(keyMsg("ctrl+l"))
	if cmd != nil {
		t.Fatal("ctrl+l while a page is loading must not queue another load")
	}
}

func TestCommitsPagedRebuildsGraphInline(t *testing.T) {
	t.Parallel()
	m := holdPagingModel(t)
	nm, _ := m.Update(commitsPagedMsg{gen: m.feed.Gen()})
	got := nm.(Model)
	if got.commitsLoading {
		t.Fatal("a landed page clears commitsLoading")
	}
	if len(got.commitGraphRows) != got.commitsTotal() {
		t.Fatalf("a landed page must (incrementally) rebuild the graph inline: rows=%d, total=%d", len(got.commitGraphRows), got.commitsTotal())
	}
}

// The incremental append path (page in older commits) must produce byte-identical
// graph rows and lanes to a full re-lay of the same final commit list — the
// property that lets paging stay O(new) without changing what's drawn.
func TestGraphIncrementalMatchesFullRebuild(t *testing.T) {
	t.Parallel()
	all := []model.Commit{
		{Hash: "h", Parents: []string{"g", "f"}}, // merge
		{Hash: "g", Parents: []string{"e"}},
		{Hash: "f", Parents: []string{"d"}},
		{Hash: "e", Parents: []string{"c"}},
		{Hash: "d", Parents: []string{"c"}}, // fork point
		{Hash: "c", Parents: []string{"b"}},
		{Hash: "b", Parents: []string{"a"}},
		{Hash: "a"}, // root
	}

	// Incremental: lay the first 3, then "page in" the rest.
	inc := lazyModel()
	inc.commits = all[:3]
	inc = inc.rebuildCommitGraph()
	seededLayer := inc.graphLayer
	inc.commits = all
	inc = inc.rebuildCommitGraph()
	if inc.graphLayer != seededLayer {
		t.Fatal("paging in older commits must reuse the cached layer (incremental), not re-seed")
	}
	if inc.graphLaidReal != len(all) {
		t.Fatalf("graphLaidReal = %d, want %d", inc.graphLaidReal, len(all))
	}

	// Full: lay everything at once.
	full := lazyModel()
	full.commits = all
	full = full.rebuildCommitGraph()

	if len(inc.commitGraphRows) != len(full.commitGraphRows) {
		t.Fatalf("row count: incremental=%d full=%d", len(inc.commitGraphRows), len(full.commitGraphRows))
	}
	for i := range full.commitGraphRows {
		if inc.commitGraphRows[i] != full.commitGraphRows[i] {
			t.Fatalf("row %d cells differ:\n inc  %q\n full %q", i, inc.commitGraphRows[i], full.commitGraphRows[i])
		}
		if inc.commitGraphLanes[i] != full.commitGraphLanes[i] {
			t.Fatalf("row %d lane: incremental=%d full=%d", i, inc.commitGraphLanes[i], full.commitGraphLanes[i])
		}
	}
}

// A changed HEAD (commits[0]) must force a full re-lay, not an append onto stale
// lane state.
func TestGraphFullRebuildOnNewHead(t *testing.T) {
	t.Parallel()
	m := lazyModel()
	m.commits = []model.Commit{{Hash: "b", Parents: []string{"a"}}, {Hash: "a"}}
	m = m.rebuildCommitGraph()
	old := m.graphLayer
	// A new commit on top (new HEAD) — not an append at the tail.
	m.commits = []model.Commit{{Hash: "c", Parents: []string{"b"}}, {Hash: "b", Parents: []string{"a"}}, {Hash: "a"}}
	m = m.rebuildCommitGraph()
	if m.graphLayer == old {
		t.Fatal("a new HEAD must re-seed the layer (full rebuild), not append")
	}
	if len(m.commitGraphRows) != m.commitsTotal() {
		t.Fatalf("rows=%d, total=%d", len(m.commitGraphRows), m.commitsTotal())
	}
}
