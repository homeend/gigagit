package tui

import "testing"

func TestRightArrowFocusesCommitsFromEachLeftPanel(t *testing.T) {
	t.Parallel()
	for _, p := range []panel{panelBranches, panelWorktrees, panelFiles} {
		m := markModel()
		m.width, m.height = 80, 24
		m.focus = p
		u, _ := m.Update(keyMsg("right"))
		m = u.(Model)
		if m.focus != panelCommits {
			t.Fatalf("focus = %v after right from %v, want commits", m.focus, p)
		}
		if m.lastLeftPanel != p {
			t.Fatalf("lastLeftPanel = %v, want %v", m.lastLeftPanel, p)
		}
	}
}

func TestLeftArrowReturnsToLastLeftPanel(t *testing.T) {
	t.Parallel()
	m := markModel()
	m.width, m.height = 80, 24
	m.focus = panelFiles
	u, _ := m.Update(keyMsg("right"))
	m = u.(Model)
	u, _ = m.Update(keyMsg("left"))
	m = u.(Model)
	if m.focus != panelFiles {
		t.Fatalf("focus = %v, want status (the last left panel)", m.focus)
	}
}

func TestLeftArrowAfterTabRemembersLeftPanel(t *testing.T) {
	t.Parallel()
	m := markModel()
	m.width, m.height = 80, 24 // bodyH 21 >= 12 → Staged visible; order ends [Staged, Commits]
	m.focus = panelStaged
	u, _ := m.Update(keyMsg("tab")) // Staged -> Commits, must record Staged
	m = u.(Model)
	if m.focus != panelCommits {
		t.Fatalf("setup: focus = %v, want commits", m.focus)
	}
	u, _ = m.Update(keyMsg("left"))
	if got := u.(Model).focus; got != panelStaged {
		t.Fatalf("focus = %v, want Staged (recorded by tab)", got)
	}
}

func TestLeftArrowDefaultsToBranches(t *testing.T) {
	t.Parallel()
	m := markModel()
	m.width, m.height = 80, 24
	m.focus = panelCommits // never visited a left panel
	u, _ := m.Update(keyMsg("left"))
	if got := u.(Model).focus; got != panelBranches {
		t.Fatalf("focus = %v, want branches (zero-value default)", got)
	}
}

func TestArrowFocusEdgesNoOp(t *testing.T) {
	t.Parallel()
	m := markModel()
	m.width, m.height = 80, 24
	m.focus = panelCommits
	u, _ := m.Update(keyMsg("right")) // nothing right of commits
	if got := u.(Model).focus; got != panelCommits {
		t.Fatalf("focus = %v, right on commits must no-op", got)
	}
	m.focus = panelWorktrees
	u, _ = m.Update(keyMsg("left")) // nothing left of the left column
	if got := u.(Model).focus; got != panelWorktrees {
		t.Fatalf("focus = %v, left on a left panel must no-op", got)
	}
}

func TestLeftArrowNoOpOnNarrowTerminal(t *testing.T) {
	t.Parallel()
	m := markModel()
	m.width, m.height = 30, 24 // no left column below 40
	m.focus = panelCommits
	u, _ := m.Update(keyMsg("left"))
	if got := u.(Model).focus; got != panelCommits {
		t.Fatalf("focus = %v, left must no-op with no left column", got)
	}
}
