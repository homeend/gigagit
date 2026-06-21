package tui

import (
	"strings"
	"testing"
)

func TestCommitGraphWrittenMsgClearsAndReloadsNearTop(t *testing.T) {
	m := loadedModel(t)
	m.commitGraphIndexing = true
	m.commitsPaged = false
	u, cmd := m.Update(commitGraphWrittenMsg{})
	if u.(Model).commitGraphIndexing {
		t.Error("notice must clear on completion")
	}
	if cmd == nil {
		t.Error("near-top completion must reload the feed")
	}
}

func TestCommitGraphWrittenMsgSkipsReloadWhenPaged(t *testing.T) {
	m := loadedModel(t)
	m.commitGraphIndexing = true
	m.commitsPaged = true
	u, cmd := m.Update(commitGraphWrittenMsg{})
	if u.(Model).commitGraphIndexing {
		t.Error("notice must clear even when reload is skipped")
	}
	if cmd != nil {
		t.Error("a deep-scrolled feed must not be yanked to the top")
	}
}

func TestCommitGraphWriteFailureIsNonFatal(t *testing.T) {
	m := loadedModel(t)
	m.commitGraphIndexing = true
	u, cmd := m.Update(commitGraphWrittenMsg{err: cgErr("boom")})
	if u.(Model).commitGraphIndexing {
		t.Error("notice must clear on failure")
	}
	if cmd != nil {
		t.Error("a failed write must not reload")
	}
}

type cgErr string

func (e cgErr) Error() string { return string(e) }

func TestIndexingNoticeRendersInCommitsLabel(t *testing.T) {
	m := loadedModel(t)
	m.commitGraphIndexing = true
	if !strings.Contains(m.panelLabel(panelCommits, "Commits"), "indexing") {
		t.Error("Commits label must show the indexing notice")
	}
}

// TestDataLoadedTriggersGraphWriteOnce: under the graph pager with no
// commit-graph, dataLoadedMsg dispatches the write once + sets the notice; a
// second identical load does not re-fire.
func TestDataLoadedTriggersGraphWriteOnce(t *testing.T) {
	m := loadedModel(t) // default pager = "graph"
	// loadedModel's own initial load (fresh repo, no graph) already fires the
	// one-shot trigger; reset to observe a fresh dispatch.
	m.commitGraphTried = false
	m.commitGraphIndexing = false
	msg := dataLoadedMsg{gen: m.loadGen, hasCommitGraph: false, cfg: m.cfg}
	u, cmd := m.Update(msg)
	mm := u.(Model)
	if !mm.commitGraphIndexing || cmd == nil {
		t.Fatal("graph pager + no graph must dispatch the write and show the notice")
	}
	if !mm.commitGraphTried {
		t.Fatal("the once-guard flag must be set")
	}
	u2, cmd2 := mm.Update(dataLoadedMsg{gen: mm.loadGen, hasCommitGraph: false, cfg: mm.cfg})
	if cmd2 != nil {
		t.Error("the write must fire at most once per session")
	}
	_ = u2
}

// TestDataLoadedSkipsWriteWhenGraphPresent locks the steady state (every launch
// after the first): graph pager + an existing commit-graph → no write, no notice.
func TestDataLoadedSkipsWriteWhenGraphPresent(t *testing.T) {
	m := loadedModel(t) // default pager = "graph"
	m.commitGraphTried = false
	m.commitGraphIndexing = false
	u, cmd := m.Update(dataLoadedMsg{gen: m.loadGen, hasCommitGraph: true, cfg: m.cfg})
	if u.(Model).commitGraphIndexing || cmd != nil {
		t.Error("a present commit-graph must not trigger a write or notice")
	}
}

func TestDataLoadedSkipsGraphWriteUnderDateOrderPager(t *testing.T) {
	t.Setenv("GG_COMMIT_PAGER", "date-order")
	m := loadedModel(t) // pager = "date-order"
	u, cmd := m.Update(dataLoadedMsg{gen: m.loadGen, hasCommitGraph: false, cfg: m.cfg})
	if u.(Model).commitGraphIndexing || cmd != nil {
		t.Error("legacy pager must not auto-write the commit-graph")
	}
}
