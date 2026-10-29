package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/homeend/gigagit/internal/model"
)

// wsFilterModel is a two-member workspace whose Branches tab, sorted
// newest first, holds one "feat" branch in each member:
//
//	0 heading A, 1 main, 2 alpha, 3 feat-one, 4 beta, 5 gamma, 6 delta, 7 eps,
//	8 heading B, 9 main, 10 feat-two, 11 zeta
func wsFilterModel(t *testing.T) Model {
	t.Helper()
	m := loadedModel(t)
	m.width, m.height = 160, 50
	m, b := workspaceOf(t, m)
	m.branches = []model.Branch{
		{Name: "main", UnixTime: 100}, {Name: "alpha", UnixTime: 90}, {Name: "feat-one", UnixTime: 80},
		{Name: "beta", UnixTime: 70}, {Name: "gamma", UnixTime: 60}, {Name: "delta", UnixTime: 50},
		{Name: "eps", UnixTime: 40},
	}
	b.branches = []model.Branch{{Name: "main", UnixTime: 100}, {Name: "feat-two", UnixTime: 90}, {Name: "zeta", UnixTime: 80}}
	m.focus = panelBranches
	m.sortModes[panelBranches] = sortDateDesc
	if n := m.panelLen(panelBranches); n != 12 {
		t.Fatalf("fixture: %d branch rows, want 12", n)
	}
	return m
}

// Typing a /-filter on a workspace's grouped Branches tab, with the cursor
// low in the list and a query both members match, re-seats the cursor on the
// next match below it — it panicked indexing the shrunken grouped list with
// the old row number (t:\others\gg-panic.txt, 2026-10-29).
func TestWorkspaceBranchFilterSnapsAcrossMembers(t *testing.T) {
	t.Parallel()
	m := wsFilterModel(t)
	m.filterTyping = true
	m.filterPanel = panelBranches
	m.sel[panelBranches] = 7 // eps, A's last row
	nm, _ := m.Update(key("feat"))
	got := nm.(Model)
	l := got.listFor(panelBranches)
	if l.Len() != 4 {
		t.Fatalf("filtered rows = %d, want 4 (two headings + feat-one + feat-two)", l.Len())
	}
	if s := got.sel[panelBranches]; s != 3 || l.Name(s) != "feat-two" {
		t.Fatalf("sel = %d (%q), want 3 (feat-two, the match below eps)", s, l.Name(s))
	}
}

// Esc out of the filter keeps the cursor on the same row in the full grouped
// list, even when that row belongs to a later member.
func TestWorkspaceBranchFilterEscKeepsRow(t *testing.T) {
	t.Parallel()
	m := wsFilterModel(t)
	m.filterTyping = true
	m.filterPanel = panelBranches
	m.filterQuery = "feat"
	m.sel[panelBranches] = 3 // feat-two in [heading A, feat-one, heading B, feat-two]
	nm, _ := m.Update(keyType(tea.KeyEsc))
	got := nm.(Model)
	l := got.listFor(panelBranches)
	if s := got.sel[panelBranches]; s != 10 || l.Name(s) != "feat-two" {
		t.Fatalf("sel = %d (%q), want 10 (feat-two in the full list)", s, l.Name(s))
	}
}

// A query matching nothing below the cursor wraps to the first match, never
// onto a member heading.
func TestWorkspaceBranchFilterWrapsPastHeading(t *testing.T) {
	t.Parallel()
	m := wsFilterModel(t)
	m.filterTyping = true
	m.filterPanel = panelBranches
	m.sel[panelBranches] = 11 // zeta, the last row
	nm, _ := m.Update(key("feat"))
	got := nm.(Model)
	l := got.listFor(panelBranches)
	if s := got.sel[panelBranches]; s != 1 || l.Name(s) != "feat-one" {
		t.Fatalf("sel = %d (%q), want 1 (feat-one, the first match)", s, l.Name(s))
	}
}

// The grouped Worktrees tab takes the same path: sorted by name, filtering
// from A's last worktree lands on B's match.
func TestWorkspaceWorktreeFilterSnapsAcrossMembers(t *testing.T) {
	t.Parallel()
	m := loadedModel(t)
	m.width, m.height = 160, 50
	m, _ = addWorktree(t, m, "feat-one")
	m, _ = addWorktree(t, m, "yankee")
	m, _ = addWorktree(t, m, "zulu")
	m, b := workspaceOf(t, m)
	b.worktrees = append(b.worktrees, model.Worktree{Path: b.mainPath + "-feat-two", Branch: "feat-two"})
	m.focus = panelWorktrees
	m.sortModes[panelWorktrees] = sortNameAsc
	l := m.listFor(panelWorktrees)
	last := -1
	for i := range l.Len() {
		if l.Name(i) == "zulu" {
			last = i
		}
	}
	if last < 0 {
		t.Fatalf("fixture: no zulu row in %d rows", l.Len())
	}
	m.filterTyping = true
	m.filterPanel = panelWorktrees
	m.sel[panelWorktrees] = last
	nm, _ := m.Update(key("feat"))
	got := nm.(Model)
	gl := got.listFor(panelWorktrees)
	s := got.sel[panelWorktrees]
	if s < 0 || s >= gl.Len() || gl.Name(s) != "feat-two" {
		t.Fatalf("sel = %d of %d rows, want B's feat-two", s, gl.Len())
	}
}
