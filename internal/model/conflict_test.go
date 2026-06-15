package model

import "testing"

func uf(staged, unstaged byte) FileStatus {
	return FileStatus{Path: "f", Kind: KindUnmerged, Staged: staged, Unstaged: unstaged}
}

func TestConflictClass(t *testing.T) {
	cases := []struct {
		name               string
		f                  FileStatus
		wantClass          ConflictClass
		ours, theirs, base bool
	}{
		{"UU both", uf('U', 'U'), ConflictBothSides, true, true, true},
		{"AA both-added", uf('A', 'A'), ConflictBothSides, true, true, false},
		{"DU deleted-by-us", uf('D', 'U'), ConflictModifyDelete, false, true, true},
		{"UD deleted-by-them", uf('U', 'D'), ConflictModifyDelete, true, false, true},
		{"AU added-by-us", uf('A', 'U'), ConflictModifyDelete, true, false, false},
		{"UA added-by-them", uf('U', 'A'), ConflictModifyDelete, false, true, false},
		{"DD both-deleted", uf('D', 'D'), ConflictModifyDelete, false, false, true},
	}
	for _, c := range cases {
		if got := c.f.ConflictClass(); got != c.wantClass {
			t.Errorf("%s: class = %v, want %v", c.name, got, c.wantClass)
		}
		if c.f.ConflictHasOurs() != c.ours || c.f.ConflictHasTheirs() != c.theirs || c.f.ConflictHasBase() != c.base {
			t.Errorf("%s: ours/theirs/base = %v/%v/%v, want %v/%v/%v", c.name,
				c.f.ConflictHasOurs(), c.f.ConflictHasTheirs(), c.f.ConflictHasBase(), c.ours, c.theirs, c.base)
		}
	}
}

func TestConflictsHelper(t *testing.T) {
	st := WorkingTreeStatus{Files: []FileStatus{
		{Path: "a", Kind: KindTracked, Unstaged: 'M'},
		uf('U', 'U'),
		{Path: "u", Kind: KindUntracked},
	}}
	if c := st.Conflicts(); len(c) != 1 || c[0].Staged != 'U' {
		t.Fatalf("Conflicts() = %+v, want the single unmerged file", c)
	}
}
