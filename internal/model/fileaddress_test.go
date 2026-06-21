package model

import "testing"

func TestFileAddressDisplay(t *testing.T) {
	cases := []struct {
		name string
		a    FileAddress
		want string
	}{
		{"committed", FileAddress{State: StateCommitted, Branch: "feat", Commit: "a1b2c3d4e5", Path: "src/x.go"}, "feat / a1b2c3d / src/x.go"},
		{"committed-no-branch", FileAddress{State: StateCommitted, Commit: "a1b2c3d4e5", Path: "x.go"}, "commit / a1b2c3d / x.go"},
		{"shelf", FileAddress{State: StateShelf, ShelfID: "id1", Path: "x.go"}, "shelf / shelf / x.go"},
		{"unstaged", FileAddress{State: StateUnstaged, Worktree: "/home/u/repo", Path: "a/b.go"}, "wt:repo / unstaged / a/b.go"},
		{"staged", FileAddress{State: StateStaged, Worktree: "/home/u/repo", Path: "a/b.go"}, "wt:repo / staged / a/b.go"},
	}
	for _, c := range cases {
		if got := c.a.Display(); got != c.want {
			t.Errorf("%s: Display()=%q want %q", c.name, got, c.want)
		}
	}
}

func TestFileAddressDisplayCommitPointer(t *testing.T) {
	cases := []struct {
		name string
		a    FileAddress
		want string
	}{
		{"branch", FileAddress{State: StateCommitted, Branch: "feat", Commit: "a1b2c3d4e5", Path: ""}, "feat / a1b2c3d"},
		{"no-branch", FileAddress{State: StateCommitted, Commit: "a1b2c3d4e5", Path: ""}, "commit / a1b2c3d"},
		{"file-unchanged", FileAddress{State: StateCommitted, Branch: "feat", Commit: "a1b2c3d4e5", Path: "src/x.go"}, "feat / a1b2c3d / src/x.go"},
	}
	for _, c := range cases {
		if got := c.a.Display(); got != c.want {
			t.Errorf("%s: Display()=%q want %q", c.name, got, c.want)
		}
	}
}

func TestBookmarkIsCommit(t *testing.T) {
	if !(Bookmark{State: StateCommitted, Commit: "abc", Path: ""}).IsCommit() {
		t.Error("path-less committed bookmark should be a commit bookmark")
	}
	if (Bookmark{State: StateCommitted, Commit: "abc", Path: "x.go"}).IsCommit() {
		t.Error("a committed FILE bookmark is not a commit bookmark")
	}
	if (Bookmark{State: StateUnstaged, Worktree: "/wt", Path: ""}).IsCommit() {
		t.Error("a non-committed state is not a commit bookmark")
	}
}

func TestFileAddressFileRef(t *testing.T) {
	cases := []struct {
		a    FileAddress
		want FileRef
	}{
		{FileAddress{State: StateUnstaged, Path: "p"}, FileRef{Source: SourceUnstaged, Path: "p"}},
		{FileAddress{State: StateUntracked, Path: "p"}, FileRef{Source: SourceUnstaged, Path: "p"}},
		{FileAddress{State: StateStaged, Path: "p"}, FileRef{Source: SourceStaged, Path: "p"}},
		{FileAddress{State: StateCommitted, Commit: "abc", Path: "p"}, FileRef{Source: SourceCommit, Locator: "abc", Path: "p"}},
		{FileAddress{State: StateShelf, ShelfID: "id", Path: "p"}, FileRef{Source: SourceShelf, Locator: "id", Path: "p"}},
	}
	for _, c := range cases {
		if got := c.a.FileRef(); got != c.want {
			t.Errorf("FileRef(%+v)=%+v want %+v", c.a, got, c.want)
		}
	}
}

func TestBookmarkAddressRoundTrip(t *testing.T) {
	b := Bookmark{Worktree: "/wt", Branch: "b", Commit: "c", ShelfID: "s", Path: "p", State: StateCommitted}
	a := b.Address()
	if a.Worktree != "/wt" || a.Branch != "b" || a.Commit != "c" || a.ShelfID != "s" || a.Path != "p" || a.State != StateCommitted {
		t.Fatalf("Address() lost a field: %+v", a)
	}
}
