package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/homeend/gigagit/internal/model"
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
	t.Parallel()
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
	t.Parallel()
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

func TestCtrlFFromCommittedFilterStartsEager(t *testing.T) {
	t.Parallel()
	m := eagerModel(t, []model.Commit{{Hash: "a", Subject: "fix"}})
	m.filterPanel = panelCommits
	m.filterQuery = "zzz" // committed /-filter, no loaded match
	nm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlF})
	got := nm.(Model)
	// No match + fresh feed (CanLoadMore) → eager active and a load dispatched.
	if !got.eager.active || cmd == nil {
		t.Fatalf("ctrl+f should start eager search (active=%v cmd=%v)", got.eager.active, cmd != nil)
	}
}

func TestCtrlFWhileTypingCommitsAndSearches(t *testing.T) {
	t.Parallel()
	m := eagerModel(t, []model.Commit{{Hash: "a", Subject: "fix"}})
	m.filterTyping = true
	m.filterPanel = panelCommits
	m.filterQuery = "zzz"
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlF})
	got := nm.(Model)
	if got.filterTyping {
		t.Fatal("ctrl+f should commit the /-filter (stop typing)")
	}
	if !got.eager.active {
		t.Fatal("ctrl+f while typing should start eager search")
	}
}

func TestCtrlFFromHighlightStartsEager(t *testing.T) {
	t.Parallel()
	m := eagerModel(t, []model.Commit{{Hash: "a", Subject: "fix"}})
	m.highlightQuery = "zzz" // committed @-highlight, no loaded match
	nm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlF})
	got := nm.(Model)
	if !got.eager.active || cmd == nil {
		t.Fatalf("ctrl+f should start eager search from @ (active=%v cmd=%v)", got.eager.active, cmd != nil)
	}
	if got.highlightQuery != "zzz" {
		t.Fatal("@-sourced eager search must keep the highlight active")
	}
	if got.filterQuery != "" {
		t.Fatal("@-sourced eager search must not introduce a / filter")
	}
}

func TestEagerAdvanceReportsExhausted(t *testing.T) {
	t.Parallel()
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

func TestEagerAdvanceOpensPromptAtCap(t *testing.T) {
	t.Parallel()
	m := eagerModel(t, []model.Commit{{Hash: "a", Subject: "fix"}})
	// budget exhausted, no match, feed loadable → prompt.
	m.eager = eagerSearch{active: true, query: "zzz", budget: 0}
	nm, _ := m.eagerAdvance()
	got := nm
	if got.eager.active {
		t.Fatal("at the cap the search pauses (inactive) pending the dialog")
	}
	if _, ok := got.topLayer().(*eagerPrompt); !ok {
		t.Fatalf("expected an eagerPrompt on top, got %T", got.topLayer())
	}
}

func TestEagerPromptSearchMoreResumes(t *testing.T) {
	t.Parallel()
	m := eagerModel(t, []model.Commit{{Hash: "a", Subject: "fix"}})
	m = m.pushLayer(&eagerPrompt{query: "zzz", scanned: 1, sel: 0})
	p := m.topLayer().(*eagerPrompt)
	nm, cmd := p.update(m, tea.KeyMsg{Type: tea.KeyEnter})
	got := nm
	if _, ok := got.topLayer().(*eagerPrompt); ok {
		t.Fatal("choosing 'search more' should pop the prompt")
	}
	if !got.eager.active || cmd == nil {
		t.Fatal("'search more' should resume eager search with a fresh budget")
	}
}

func TestEagerPromptCancelStops(t *testing.T) {
	t.Parallel()
	m := eagerModel(t, []model.Commit{{Hash: "a", Subject: "fix"}})
	m = m.pushLayer(&eagerPrompt{query: "zzz", scanned: 1, sel: 1}) // sel 1 = Cancel
	p := m.topLayer().(*eagerPrompt)
	nm, _ := p.update(m, tea.KeyMsg{Type: tea.KeyEnter})
	if nm.eager.active {
		t.Fatal("cancel must not resume the search")
	}
	if _, ok := nm.topLayer().(*eagerPrompt); ok {
		t.Fatal("cancel should pop the prompt")
	}
}

func TestEagerClearedOnExternalReload(t *testing.T) {
	t.Parallel()
	m := eagerModel(t, []model.Commit{{Hash: "a", Subject: "fix"}})
	m.eager = eagerSearch{active: true, query: "zzz", budget: 3}
	// A scope-toggle reload arrives (e.g. user cleared a filter mid eager-search).
	nm, _ := m.Update(commitsReloadedMsg{gen: m.feed.Gen(), state: m.feed.Snapshot()})
	if nm.(Model).eager.active {
		t.Fatal("an external feed reload must abort an in-flight eager search")
	}
}

func TestCtrlFKeepsCommitFilter(t *testing.T) {
	t.Parallel()
	m := eagerModel(t, []model.Commit{{Hash: "a", Subject: "fix docs"}})
	m.filterPanel = panelCommits
	m.filterQuery = "docs"
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlF})
	got := nm.(Model)
	if got.filterQuery != "docs" || got.filterPanel != panelCommits {
		t.Fatalf("ctrl+f must keep the / filter engaged like @ (query=%q panel=%v)", got.filterQuery, got.filterPanel)
	}
	if !got.eager.active {
		t.Fatal("the deeper search should still start")
	}
}

func TestCtrlFWhileTypingKeepsFilter(t *testing.T) {
	t.Parallel()
	m := eagerModel(t, []model.Commit{{Hash: "a", Subject: "fix docs"}})
	m.filterTyping = true
	m.filterPanel = panelCommits
	m.filterQuery = "docs"
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlF})
	got := nm.(Model)
	if got.filterTyping {
		t.Fatal("ctrl+f should commit the /-filter (stop typing)")
	}
	if got.filterQuery != "docs" {
		t.Fatalf("filterQuery = %q, want kept (same as the @ highlight)", got.filterQuery)
	}
}

func TestEagerAdvanceJumpsWithinFilteredView(t *testing.T) {
	t.Parallel()
	m := eagerModel(t, []model.Commit{
		{Hash: "a", Subject: "docs one"},
		{Hash: "b", Subject: "noise"},
		{Hash: "c", Subject: "docs two"},
	})
	m.filterPanel = panelCommits
	m.filterQuery = "docs"
	m.eager = eagerSearch{active: true, query: "docs", budget: 5, from: 2} // "c" newly loaded
	nm, _ := m.eagerAdvance()
	if nm.eager.active {
		t.Fatal("a match at/after from must end the search")
	}
	// Filtered display = [a, c]; the jump must land on c (display index 1).
	if nm.sel[panelCommits] != 1 {
		t.Fatalf("sel = %d, want 1 (the new match within the filtered view)", nm.sel[panelCommits])
	}
	if nm.filterQuery != "docs" {
		t.Fatalf("filter must stay engaged through the jump, got %q", nm.filterQuery)
	}
}

func TestCtrlFWithLoadedMatchStillPagesDeeper(t *testing.T) {
	t.Parallel()
	m := eagerModel(t, []model.Commit{{Hash: "a", Subject: "fix docs"}})
	m.filterPanel = panelCommits
	m.filterQuery = "docs" // committed /-filter WITH a match already loaded
	nm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlF})
	got := nm.(Model)
	if !got.eager.active || cmd == nil {
		t.Fatalf("ctrl+f must page deeper even with a loaded match (active=%v cmd=%v)", got.eager.active, cmd != nil)
	}
	if got.eager.from != 1 {
		t.Fatalf("from = %d, want 1 (skip the already-loaded commits)", got.eager.from)
	}
}

func TestEagerAdvanceSkipsMatchesBelowFrom(t *testing.T) {
	t.Parallel()
	m := eagerModel(t, []model.Commit{{Hash: "a", Subject: "docs one"}, {Hash: "b", Subject: "docs two"}})
	m.eager = eagerSearch{active: true, query: "docs", budget: 5, from: 1}
	nm, _ := m.eagerAdvance()
	if nm.eager.active {
		t.Fatal("a match at/after from must end the search")
	}
	if nm.sel[panelCommits] != 1 {
		t.Fatalf("sel = %d, want 1 (first match at feed index >= from, not the earlier one)", nm.sel[panelCommits])
	}
}

func TestEagerJumpRetainsQuery(t *testing.T) {
	t.Parallel()
	m := eagerModel(t, []model.Commit{{Hash: "a", Subject: "fix bug"}, {Hash: "b", Subject: "write docs"}})
	m.eager = eagerSearch{active: true, query: "docs", budget: 5}
	nm, _ := m.eagerAdvance()
	if nm.eager.active {
		t.Fatal("a match must end the search")
	}
	if nm.eager.query != "docs" {
		t.Fatalf("query = %q, want %q retained for a repeat ctrl+f", nm.eager.query, "docs")
	}
}

func TestCtrlFReusesRetainedQuery(t *testing.T) {
	t.Parallel()
	m := eagerModel(t, []model.Commit{{Hash: "a", Subject: "write docs"}})
	m.eager = eagerSearch{query: "docs"} // retained from a finished jump; no / or @ active
	nm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlF})
	got := nm.(Model)
	if !got.eager.active || cmd == nil {
		t.Fatalf("ctrl+f must restart the eager cycle from the retained query (active=%v cmd=%v)", got.eager.active, cmd != nil)
	}
	if got.eager.from != 1 {
		t.Fatalf("from = %d, want 1 (dig past the loaded commits)", got.eager.from)
	}
}

func TestEagerPromptResumeKeepsFrom(t *testing.T) {
	t.Parallel()
	m := eagerModel(t, []model.Commit{{Hash: "a", Subject: "fix"}})
	m = m.pushLayer(&eagerPrompt{query: "zzz", scanned: 1, from: 1, sel: 0})
	p := m.topLayer().(*eagerPrompt)
	nm, _ := p.update(m, tea.KeyMsg{Type: tea.KeyEnter})
	if !nm.eager.active || nm.eager.from != 1 {
		t.Fatalf("resume must keep the search floor (active=%v from=%d, want true/1)", nm.eager.active, nm.eager.from)
	}
}

func TestEagerDeeperExhaustedReportsNoFurther(t *testing.T) {
	t.Parallel()
	m := eagerModel(t, []model.Commit{{Hash: "a", Subject: "docs"}})
	// Exhaust the feed: short initial page (the fake serves 1 row < 50).
	m.feed.SetPageSizes(50, 50)
	m.feed.LoadInitial(context.Background())
	m.commits = m.feed.Snapshot().Commits
	m = m.rebuildCommitGraph()
	m.eager = eagerSearch{active: true, query: "docs", budget: 5, from: len(m.commits)}
	nm, cmd := m.eagerAdvance()
	if cmd != nil || nm.eager.active {
		t.Fatal("exhausted feed with no further match → stop, no load")
	}
	if !strings.Contains(nm.statusMsg, "no further match") {
		t.Fatalf("statusMsg = %q, want a 'no further match' report", nm.statusMsg)
	}
	if nm.eager.query != "docs" {
		t.Fatal("the query must stay retained even on exhaustion")
	}
}
