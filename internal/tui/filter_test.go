package tui

import (
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func typeRunes(t *testing.T, m Model, s string) Model {
	t.Helper()
	for _, r := range s {
		u, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = u.(Model)
	}
	return m
}

func TestSlashFilterLifecycle(t *testing.T) {
	dir, repo := newRepoDir(t)
	runGit(t, dir, "branch", "fix-1")
	runGit(t, dir, "branch", "fix-2")
	runGit(t, dir, "branch", "feat-x")
	m := New(repo)
	u, _ := m.Update(m.loadCmd()())
	m = u.(Model)
	m.focus = panelBranches

	u, _ = m.Update(keyMsg("/"))
	m = u.(Model)
	if !m.filterTyping || m.filterPanel != panelBranches {
		t.Fatalf("/ should start filter input on the focused panel")
	}
	m = typeRunes(t, m, "fix")
	if n := m.panelLen(panelBranches); n != 2 {
		t.Fatalf("filtered len = %d, want 2 (fix-1, fix-2)", n)
	}
	u, _ = m.Update(keyMsg("backspace"))
	m = u.(Model)
	if m.filterQuery != "fi" {
		t.Fatalf("query = %q, want fi", m.filterQuery)
	}
	u, _ = m.Update(keyMsg("enter"))
	m = u.(Model)
	if m.filterTyping || m.filterQuery != "fi" {
		t.Fatalf("enter must commit and keep the filter (typing=%v q=%q)", m.filterTyping, m.filterQuery)
	}
	// esc in normal mode clears the committed filter.
	u, _ = m.Update(keyMsg("esc"))
	m = u.(Model)
	if m.filterQuery != "" {
		t.Fatalf("esc should clear the filter, query = %q", m.filterQuery)
	}
	if n := m.panelLen(panelBranches); n != 4 {
		t.Fatalf("unfiltered len = %d, want 4", n)
	}
}

func TestEscDuringTypingCancelsAndClears(t *testing.T) {
	m := loadedModel(t)
	m.focus = panelBranches
	u, _ := m.Update(keyMsg("/"))
	m = u.(Model)
	m = typeRunes(t, m, "xyz")
	u, _ = m.Update(keyMsg("esc"))
	m = u.(Model)
	if m.filterTyping || m.filterQuery != "" {
		t.Fatalf("esc while typing must cancel (typing=%v q=%q)", m.filterTyping, m.filterQuery)
	}
}

func TestFilterTypingSwallowsGlobalKeys(t *testing.T) {
	m := loadedModel(t)
	m.focus = panelBranches
	u, _ := m.Update(keyMsg("/"))
	m = u.(Model)
	u, _ = m.Update(keyMsg("p")) // would start SmartPull in normal mode
	m = u.(Model)
	if m.running {
		t.Fatal("global key leaked through filter input")
	}
	if m.filterQuery != "p" {
		t.Fatalf("query = %q, want p", m.filterQuery)
	}
	u, _ = m.Update(keyMsg("q")) // would quit in normal mode
	m = u.(Model)
	if m.filterQuery != "pq" {
		t.Fatalf("query = %q, want pq", m.filterQuery)
	}
}

func TestFilteredEnterSwitchesToVisibleWorktree(t *testing.T) {
	dir, repo := newRepoDir(t)
	wtA := filepath.Join(filepath.Dir(dir), "wt-aaa")
	wtB := filepath.Join(filepath.Dir(dir), "wt-bbb")
	runGit(t, dir, "worktree", "add", "-b", "feature/aaa", wtA, "main")
	runGit(t, dir, "worktree", "add", "-b", "feature/bbb", wtB, "main")
	m := New(repo)
	u, _ := m.Update(m.loadCmd()())
	m = u.(Model)
	m.focus = panelWorktrees

	u, _ = m.Update(keyMsg("/"))
	m = u.(Model)
	m = typeRunes(t, m, "bbb")
	u, _ = m.Update(keyMsg("enter")) // commit the filter
	m = u.(Model)
	if n := m.panelLen(panelWorktrees); n != 1 {
		t.Fatalf("filtered len = %d, want 1", n)
	}
	u, _ = m.Update(keyMsg("enter")) // act on the visible row
	m = u.(Model)
	wantR, _ := filepath.EvalSymlinks(wtB)
	gotR, _ := filepath.EvalSymlinks(m.switchTarget)
	if gotR != wantR {
		t.Fatalf("switchTarget = %q, want %q — action hit the wrong backing row", m.switchTarget, wtB)
	}
}

func TestFilterSurvivesReloadAndMovesBetweenPanels(t *testing.T) {
	dir, repo := newRepoDir(t)
	runGit(t, dir, "branch", "fix-1")
	m := New(repo)
	u, _ := m.Update(m.loadCmd()())
	m = u.(Model)
	m.focus = panelBranches
	u, _ = m.Update(keyMsg("/"))
	m = u.(Model)
	m = typeRunes(t, m, "fix")
	u, _ = m.Update(keyMsg("enter"))
	m = u.(Model)

	// Reload: the filter re-applies over fresh data.
	u, _ = m.Update(m.loadCmd()())
	m = u.(Model)
	if m.filterQuery != "fix" || m.panelLen(panelBranches) != 1 {
		t.Fatalf("filter lost on reload: q=%q len=%d", m.filterQuery, m.panelLen(panelBranches))
	}

	// Starting / on another panel moves the filter (the old one clears).
	m.focus = panelCommits
	u, _ = m.Update(keyMsg("/"))
	m = u.(Model)
	if m.filterPanel != panelCommits || m.filterQuery != "" {
		t.Fatalf("new / must rebind the filter: panel=%v q=%q", m.filterPanel, m.filterQuery)
	}
	if m.panelLen(panelBranches) < 2 {
		t.Fatal("old panel must be unfiltered after the filter moved")
	}
}

func TestFilterLabelRendering(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 100, 30
	m.focus = panelBranches
	m.sortModes[panelBranches] = sortDateDesc
	u, _ := m.Update(keyMsg("/"))
	m = u.(Model)
	m = typeRunes(t, m, "fi")
	out := m.View()
	if !strings.Contains(out, "Branches ·date↓ /fi█") {
		t.Fatalf("label missing sort+filter+cursor decoration:\n%s", out)
	}
}
