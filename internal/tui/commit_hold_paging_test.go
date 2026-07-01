package tui

import (
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

// Holding End (or PgDn / j) on the Commits panel must not queue a fresh page
// load while one is already in flight. The load chokepoint keys off the
// synchronous commitsLoading flag (set at dispatch), so held-key auto-repeat is
// paced by completion instead of firing a load per keystroke — the fix for
// "released End but the window kept loading commits".

func holdPagingModel(t *testing.T) Model {
	t.Helper()
	m := newTestModelForReload(t) // fresh feed → NeedsMore true, CanLoadMore true
	m.focus = panelCommits
	m.commits = []model.Commit{{Hash: "a", Subject: "one"}}
	m.sel[panelCommits] = len(m.commits) - 1
	return m
}

func TestMaybeLoadMoreDispatchesWhenIdle(t *testing.T) {
	m := holdPagingModel(t)
	mm, cmd := m.maybeLoadMoreCommits()
	if cmd == nil {
		t.Fatal("precondition: idle fresh feed at end should dispatch a page load")
	}
	if !mm.commitsLoading {
		t.Fatal("dispatching a page must set commitsLoading")
	}
}

func TestMaybeLoadMoreDroppedWhileLoading(t *testing.T) {
	m := holdPagingModel(t)
	m.commitsLoading = true // a page/reload is already in flight
	_, cmd := m.maybeLoadMoreCommits()
	if cmd != nil {
		t.Fatal("a load already in flight → must not dispatch another (held-key pacing)")
	}
}

func TestEndKeyDropsLoadWhileLoading(t *testing.T) {
	m := holdPagingModel(t)
	m.commitsLoading = true
	_, cmd := m.Update(keyMsg("end"))
	if cmd != nil {
		t.Fatal("End while a page is loading must not queue another load")
	}
}

func TestCtrlLDropsLoadWhileLoading(t *testing.T) {
	m := holdPagingModel(t)
	m.commitsLoading = true
	_, cmd := m.Update(keyMsg("ctrl+l"))
	if cmd != nil {
		t.Fatal("ctrl+l while a page is loading must not queue another load")
	}
}
