package notes

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

// Every FileState and Preview shape routes to exactly one part. A new
// FileState without a row here is a routing decision nobody made.
func TestPartOfRoutesEveryKind(t *testing.T) {
	t.Parallel()
	sha := strings.Repeat("a", 40)
	wt := WorktreePart("/repo")
	cases := []struct {
		name string
		addr model.FileAddress
		prev string
		want Part
	}{
		{"unstaged", model.FileAddress{State: model.StateUnstaged, Worktree: "/repo", Path: "a.go"}, "", wt},
		{"staged", model.FileAddress{State: model.StateStaged, Worktree: "/repo", Path: "a.go"}, "", wt},
		{"untracked", model.FileAddress{State: model.StateUntracked, Worktree: "/repo", Path: "a.go"}, "", wt},
		{"unscoped live", model.FileAddress{State: model.StateUnstaged, Path: "a.go"}, "", "wt-unscoped"},
		{"shelf", model.FileAddress{State: model.StateShelf, ShelfID: "e1", Path: "a.go"}, "", PartShelf},
		{"shelf entry", model.FileAddress{State: model.StateShelf, ShelfID: "e1"}, "", PartShelf},
		{"commit line", model.FileAddress{State: model.StateCommitted, Commit: sha, Path: "a.go"}, "", PartCommits},
		{"commit review", model.FileAddress{State: model.StateCommitted, Commit: sha}, "", PartCommits},
		{"pair note", model.FileAddress{State: model.StateCommitted, Commit: sha, Path: "a.go"}, "aaaaaaa..bbbbbbb", PartCommits},
		{"working review", model.FileAddress{State: model.StateUnstaged, Worktree: "/repo"}, "", wt},
		{"merge preview", model.FileAddress{State: model.StateCommitted, Commit: sha, Path: "a.go"}, "main...feat/x", PartPreviews},
	}
	for _, c := range cases {
		if got := PartOf(model.Note{Address: c.addr, Preview: c.prev}); got != c.want {
			t.Errorf("%s: PartOf = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestWorktreePartCleansThePath(t *testing.T) {
	t.Parallel()
	a := WorktreePart("/repo/wt")
	for _, spelling := range []string{"/repo/wt/", "/repo/x/../wt", "/repo//wt"} {
		if got := WorktreePart(spelling); got != a {
			t.Errorf("WorktreePart(%q) = %q, want %q", spelling, got, a)
		}
	}
	if WorktreePart("/repo/other") == a {
		t.Fatal("two worktrees share a part")
	}
	if !a.IsWorktree() || PartCommits.IsWorktree() {
		t.Fatal("IsWorktree misclassifies")
	}
	if len(strings.TrimPrefix(string(a), "wt-")) != 16 {
		t.Fatalf("key %q is not 8 hex bytes", a)
	}
}

func TestPartFileNamesRoundTrip(t *testing.T) {
	t.Parallel()
	root := filepath.FromSlash("/state/notes/k")
	if got := PartCommits.file(root); got != filepath.Join(root, "commits.toml") {
		t.Fatalf("commits file = %q", got)
	}
	wt := WorktreePart("/repo")
	f := wt.file(root)
	if filepath.Dir(f) != filepath.Join(root, "worktrees") {
		t.Fatalf("worktree file %q not under worktrees/", f)
	}
	back, ok := partOfFile(filepath.Base(f))
	if !ok || back != wt {
		t.Fatalf("partOfFile(%q) = %q, %v; want %q", filepath.Base(f), back, ok, wt)
	}
	if _, ok := partOfFile(".notes-123.tmp"); ok {
		t.Fatal("a temp file is not a part")
	}
	if _, ok := partOfFile("x.toml.lock"); ok {
		t.Fatal("a lock file is not a part")
	}
	if _, ok := partOfFile("x.toml.corrupt-17"); ok {
		t.Fatal("a quarantined file is not a part")
	}
}
