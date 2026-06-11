package model

import "testing"

func TestCountsFromStatus(t *testing.T) {
	st := WorkingTreeStatus{
		Files: []FileStatus{
			{Path: "a.go", Staged: 'M', Unstaged: '.'},
			{Path: "b.go", Staged: '.', Unstaged: 'M'},
			{Path: "c.go", Kind: KindUntracked},
			{Path: "d.go", Kind: KindUnmerged},
		},
	}
	c := st.Counts()
	if c.Staged != 1 {
		t.Errorf("Staged = %d, want 1", c.Staged)
	}
	if c.Unstaged != 1 {
		t.Errorf("Unstaged = %d, want 1", c.Unstaged)
	}
	if c.Untracked != 1 {
		t.Errorf("Untracked = %d, want 1", c.Untracked)
	}
	if c.Conflicted != 1 {
		t.Errorf("Conflicted = %d, want 1", c.Conflicted)
	}
}
