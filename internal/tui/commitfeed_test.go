package tui

import (
	"strconv"
	"strings"
	"testing"

	"github.com/gigagit/gg/internal/git"
	"github.com/gigagit/gg/internal/gitexec"
	"github.com/gigagit/gg/internal/model"
)

func feedModel(n int, exhausted bool) Model {
	m := New(&git.Repo{Runner: gitexec.NewFakeRunner()})
	m.focus = panelCommits
	cs := make([]model.Commit, n)
	for i := range cs {
		cs[i] = model.Commit{Hash: "h" + strconv.Itoa(i), Subject: "s"}
	}
	m.commits = cs
	m.commitsExhausted = exhausted
	m.sel = map[panel]int{panelCommits: 0}
	return m
}

func TestCommitsLabel(t *testing.T) {
	if got := feedModel(37, true).panelLabel(panelCommits, "Commits"); !strings.Contains(got, "37") || strings.Contains(got, "+") {
		t.Fatalf("exhausted label = %q, want plain 37", got)
	}
	if got := feedModel(250, false).panelLabel(panelCommits, "Commits"); !strings.Contains(got, "250+") {
		t.Fatalf("non-exhausted label = %q, want 250+", got)
	}
}

func TestPagingFiresNearEnd(t *testing.T) {
	// New()'s feed has 0 commits and is not exhausted, so NeedsMore(sel) is
	// true for any sel within threshold of 0. Pressing down on the Commits
	// panel must fire a load-more cmd.
	m := feedModel(50, false)
	m.sel[panelCommits] = 49
	_, cmd := m.Update(keyMsg("down"))
	if cmd == nil {
		t.Fatal("scrolling near the end should fire a load-more cmd")
	}
}

func TestPagingSuppressedWhileFiltering(t *testing.T) {
	// filterActive(p) == (p == m.filterPanel && m.filterQuery != ""), so these
	// two fields make the commits filter active.
	m := feedModel(50, false)
	m.filterPanel = panelCommits
	m.filterQuery = "x"
	m.sel[panelCommits] = 49
	_, cmd := m.Update(keyMsg("down"))
	if cmd != nil {
		t.Fatal("an active commits filter must suppress auto-paging")
	}
}
