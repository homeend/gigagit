package tui

import (
	"strconv"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/domain"
	"github.com/gigagit/gg/internal/git"
	"github.com/gigagit/gg/internal/gitexec"
	"github.com/gigagit/gg/internal/model"
)

// TestWheelOverCommitsPages: mouse-wheeling the commit list toward the end
// pages in more, like keyboard movement does.
func TestWheelOverCommitsPages(t *testing.T) {
	m := mouseModel() // markModel + 80x24; its feed is nil
	m.svc = domain.New(&git.Repo{Runner: gitexec.NewFakeRunner()})
	m.feed = m.svc.CommitFeed() // 0 commits, not exhausted → NeedsMore true
	_, cmd := m.Update(mouseMsg(30, 5, tea.MouseButtonWheelDown))
	if cmd == nil {
		t.Fatal("wheel over the commits panel near the end should page in more")
	}
}

func feedModel(n int, exhausted bool) Model {
	m := New(domain.New(&git.Repo{Runner: gitexec.NewFakeRunner()}))
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

func TestCommitsLabelShowsLoadingIndicator(t *testing.T) {
	m := feedModel(250, false)
	if got := m.panelLabel(panelCommits, "Commits"); strings.Contains(got, commitsLoadingGlyph) {
		t.Fatalf("idle label must not show the loading glyph: %q", got)
	}
	m.commitsLoading = true
	if got := m.panelLabel(panelCommits, "Commits"); !strings.Contains(got, commitsLoadingGlyph) {
		t.Fatalf("loading label must show %q: %q", commitsLoadingGlyph, got)
	}
	// The indicator is the Commits panel's alone.
	if got := m.panelLabel(panelBranches, "Branches"); strings.Contains(got, commitsLoadingGlyph) {
		t.Fatalf("non-Commits panel must not show the loading glyph: %q", got)
	}
}

func TestPagingSetsCommitsLoading(t *testing.T) {
	m := feedModel(50, false)
	m.sel[panelCommits] = 49
	mm, cmd := m.Update(keyMsg("down"))
	if cmd == nil {
		t.Fatal("scrolling near the end should fire a load-more cmd")
	}
	if !mm.(Model).commitsLoading {
		t.Fatal("dispatching a load-more must set commitsLoading for the indicator")
	}
}

func TestCommitsPagedClearsLoading(t *testing.T) {
	m := feedModel(50, false)
	m.commitsLoading = true
	mm, _ := m.Update(commitsPagedMsg{gen: m.feed.Gen()})
	if mm.(Model).commitsLoading {
		t.Fatal("a completed page load must clear commitsLoading")
	}
}

func TestScopeReloadShowsAndClearsLoading(t *testing.T) {
	m := feedModel(50, true)
	m2, cmd := m.startFeedReload()
	if cmd == nil {
		t.Fatal("startFeedReload must return a reload cmd")
	}
	if !m2.commitsLoading {
		t.Fatal("startFeedReload must set commitsLoading for the indicator")
	}
	gen := m2.feed.Gen()
	mm, _ := m2.Update(commitsReloadedMsg{gen: gen, state: domain.FeedState{Gen: gen}})
	if mm.(Model).commitsLoading {
		t.Fatal("a matching-gen commitsReloadedMsg must clear commitsLoading")
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
