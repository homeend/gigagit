package bookmark

import (
	"testing"

	"github.com/gigagit/gg/internal/model"
)

func newStore(t *testing.T) *FileStore { t.Helper(); return NewFileStore(t.TempDir()) }

func committed(path, sha string) model.Bookmark {
	return model.Bookmark{State: model.StateCommitted, Commit: "c0ffee", Path: path, SHA: sha}
}

func TestAddGetRoundTrip(t *testing.T) {
	s := newStore(t)
	b, err := s.Add(committed("a/b.go", "deadbeef"))
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if b.ID == "" {
		t.Fatalf("Add did not assign an ID: %+v", b)
	}
	got, err := s.Get(b.ID)
	if err != nil || got.Path != "a/b.go" || got.SHA != "deadbeef" {
		t.Fatalf("Get = %+v err %v", got, err)
	}
}

func TestIDFromAddressNotSHA(t *testing.T) {
	s := newStore(t)
	// Same content (SHA) but different paths → different bookmarks.
	b1, _ := s.Add(committed("x/.gitignore", "empty"))
	b2, _ := s.Add(committed("y/.gitignore", "empty"))
	if b1.ID == b2.ID {
		t.Fatalf("same SHA at different paths must be different bookmarks")
	}
	// Same address re-added → idempotent (same ID, one entry).
	b3, _ := s.Add(committed("x/.gitignore", "empty"))
	if b3.ID != b1.ID {
		t.Fatalf("same address must be idempotent: %s vs %s", b3.ID, b1.ID)
	}
	list, _ := s.List(0, 0)
	if len(list) != 2 {
		t.Fatalf("expected 2 distinct bookmarks, got %d", len(list))
	}
}

func TestListPagingAndRemove(t *testing.T) {
	s := newStore(t)
	for _, p := range []string{"a", "b", "c"} {
		if _, err := s.Add(committed(p, "s"+p)); err != nil {
			t.Fatal(err)
		}
	}
	page, _ := s.List(0, 2)
	if len(page) != 2 {
		t.Fatalf("page1 len = %d, want 2", len(page))
	}
	rest, _ := s.List(2, 2)
	if len(rest) != 1 {
		t.Fatalf("page2 len = %d, want 1", len(rest))
	}
	if err := s.Remove(page[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(page[0].ID); err != ErrNotFound {
		t.Fatalf("removed Get err = %v, want ErrNotFound", err)
	}
}

func TestAddressIDCommitBookmark(t *testing.T) {
	commit := model.Bookmark{State: model.StateCommitted, Commit: "c0ffee", Path: ""}
	id1 := AddressID(commit)
	if id1 == "" || id1 != AddressID(commit) {
		t.Fatalf("commit bookmark id not stable: %q", id1)
	}
	// Distinct from a FILE bookmark at the same commit.
	file := model.Bookmark{State: model.StateCommitted, Commit: "c0ffee", Path: "a.go"}
	if AddressID(file) == id1 {
		t.Fatal("commit and file bookmark at the same commit must have distinct ids")
	}
	// Distinct from another commit.
	other := model.Bookmark{State: model.StateCommitted, Commit: "deadbee", Path: ""}
	if AddressID(other) == id1 {
		t.Fatal("different commits must have distinct ids")
	}
	// Round-trips through the store.
	fs := NewFileStore(t.TempDir())
	stored, err := fs.Add(commit)
	if err != nil {
		t.Fatal(err)
	}
	got, err := fs.List(0, 100)
	if err != nil || len(got) != 1 || got[0].ID != stored.ID {
		t.Fatalf("commit bookmark did not round-trip: %+v err %v", got, err)
	}
}

func TestAddressIDCommitIgnoresBranch(t *testing.T) {
	// A commit bookmark's identity is the commit alone; the branch decoration is
	// volatile display sugar, so bookmarking the same commit when it is a branch
	// tip vs not must yield ONE row, not two.
	withBranch := model.Bookmark{State: model.StateCommitted, Commit: "c0ffee", Branch: "feat", Path: ""}
	noBranch := model.Bookmark{State: model.StateCommitted, Commit: "c0ffee", Branch: "", Path: ""}
	if AddressID(withBranch) != AddressID(noBranch) {
		t.Fatal("commit bookmark id must not depend on branch decoration")
	}
}
