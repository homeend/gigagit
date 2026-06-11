package git

import "testing"

func TestParseStatusV2(t *testing.T) {
	// Fields within an entry are space-separated; entries are NUL-terminated.
	entries := []string{
		"# branch.oid abc123",
		"# branch.head main",
		"# branch.upstream origin/main",
		"# branch.ab +2 -1",
		"1 M. N... 100644 100644 100644 hhh iii staged.go",
		"1 .M N... 100644 100644 100644 hhh iii unstaged.go",
		"u UU N... 100644 100644 100644 000000 h1 h2 h3 conflict.go",
		"? untracked.txt",
	}
	var data []byte
	for _, e := range entries {
		data = append(data, []byte(e)...)
		data = append(data, 0)
	}

	st, err := ParseStatusV2(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if st.Branch != "main" {
		t.Errorf("branch = %q, want main", st.Branch)
	}
	if st.Upstream != "origin/main" {
		t.Errorf("upstream = %q, want origin/main", st.Upstream)
	}
	if st.Ahead != 2 || st.Behind != 1 {
		t.Errorf("ahead/behind = %d/%d, want 2/1", st.Ahead, st.Behind)
	}
	c := st.Counts()
	if c.Staged != 1 || c.Unstaged != 1 || c.Conflicted != 1 || c.Untracked != 1 {
		t.Fatalf("counts = %+v, want staged1 unstaged1 conflicted1 untracked1", c)
	}
}

func TestParseStatusV2Rename(t *testing.T) {
	// A "2" entry encodes a rename; with -z the original path is the NEXT token.
	entries := []string{
		"# branch.head main",
		"2 R. N... 100644 100644 100644 hhh iii R100 new.go",
		"old.go", // original path follows immediately for the rename entry
	}
	var data []byte
	for _, e := range entries {
		data = append(data, []byte(e)...)
		data = append(data, 0)
	}
	st, err := ParseStatusV2(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(st.Files) != 1 {
		t.Fatalf("files = %d, want 1", len(st.Files))
	}
	f := st.Files[0]
	if f.Path != "new.go" || f.OrigPath != "old.go" {
		t.Fatalf("rename parse: path=%q orig=%q, want new.go/old.go", f.Path, f.OrigPath)
	}
	if f.Staged != 'R' {
		t.Fatalf("staged code = %q, want R", string(f.Staged))
	}
}
