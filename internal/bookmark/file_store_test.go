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
