package tui

import "testing"

func TestCommitCompareRowsPresentOnCommit(t *testing.T) {
	t.Parallel()
	m := loadedModel(t)
	if len(m.commits) == 0 {
		t.Skip("no commits")
	}
	m.focus = panelCommits

	if _, ok := m.commitCompareWorktreeRow(); !ok {
		t.Error("Compare against working tree row must be available on a commit")
	}
	if _, ok := m.commitCompareStagedRow(); !ok {
		t.Error("Compare against staged row must be available on a commit")
	}

	// run the worktree row → opens compare mode
	r, _ := m.commitCompareWorktreeRow()
	u, cmd := r.run(m)
	if !u.(Model).inCompareMode() {
		t.Error("running the row must enter compare mode")
	}
	if cmd == nil {
		t.Error("running the row must kick off the file-list load")
	}
}

func TestCommitCompareRowsAbsentOffCommits(t *testing.T) {
	t.Parallel()
	m := loadedModel(t)
	m.focus = panelBranches
	if _, ok := m.commitCompareWorktreeRow(); ok {
		t.Error("compare row must not appear off the Commits panel")
	}
}
