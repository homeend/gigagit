package savedcompare

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

func mustLink(t *testing.T, s string) model.Link {
	t.Helper()
	l, err := model.ParseLink(s)
	if err != nil {
		t.Fatalf("ParseLink(%q): %v", s, err)
	}
	return l
}

// ID is ONE derivation for both shapes. A set-shaped entry hashes its empty
// right half rather than taking a different code path: a hash that branches
// on shape is the two-arms defect this feature keeps producing.
func TestIDIsOneDerivationForBothShapes(t *testing.T) {
	t.Parallel()
	set := ID("gg://r@main...feat/x", "")
	pair := ID("gg://r@main...feat/x", "gg://r@abc1234")
	if set == "" || pair == "" {
		t.Fatal("ID returned empty")
	}
	if set == pair {
		t.Fatal("a set and a pair sharing a left half collided")
	}
	// Direction-sensitive, like preview.ID before it.
	if ID("gg://r@a", "gg://r@b") == ID("gg://r@b", "gg://r@a") {
		t.Fatal("ID is not direction-sensitive")
	}
}

// Add fills the id, defaults the label, dedups on the PAIR (not the id), and
// returns the existing record with ErrExists so a caller can focus it.
func TestAddDedupsOnThePairAndReturnsTheExisting(t *testing.T) {
	t.Parallel()
	fs := NewFileStore(t.TempDir())
	left, right := mustLink(t, "gg://r@main...feat/x"), mustLink(t, "gg://r@abc1234")

	first, err := fs.Add(Entry{Left: left, Right: &right, Label: "mine"})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if first.ID == "" || first.Created.IsZero() {
		t.Fatalf("Add did not fill ID/Created: %+v", first)
	}

	again, err := fs.Add(Entry{Left: left, Right: &right, Label: "different label"})
	if !errors.Is(err, ErrExists) {
		t.Fatalf("second Add err = %v, want ErrExists", err)
	}
	if again.ID != first.ID || again.Label != "mine" {
		t.Fatalf("ErrExists returned %+v, want the stored record %+v", again, first)
	}

	list, err := fs.List()
	if err != nil || len(list) != 1 {
		t.Fatalf("List = %v (%d entries), want 1", err, len(list))
	}
}

// The dedup is on the PAIR, and this is the fixture that proves it rather
// than merely agreeing with it. A converted entry carries a LEGACY id, so a
// later Add of the same pair derives a DIFFERENT id — an id-keyed check would
// see two distinct ids, miss the duplicate, and store the row twice.
//
// Without this case the dedup test passes against a build that keys on the
// id, because in every other fixture the same pair also yields the same id:
// the assertion cannot see its own subject.
func TestAddDedupsOnThePairEvenWhenTheIDsDiffer(t *testing.T) {
	t.Parallel()
	fs := NewFileStore(t.TempDir())
	left := mustLink(t, "gg://r@main...feat/x")

	// As ConvertLegacy stores it: the pair's own id is never derived.
	converted, err := fs.Add(Entry{ID: "abcd1234", Left: left, Label: "converted"})
	if err != nil {
		t.Fatalf("Add converted: %v", err)
	}
	if converted.ID != "abcd1234" {
		t.Fatalf("an explicit ID was overwritten: %q", converted.ID)
	}
	if derived := ID(left.String(), ""); derived == converted.ID {
		t.Fatalf("fixture is useless: the derived id %q equals the legacy one", derived)
	}

	again, err := fs.Add(Entry{Left: left, Label: "the same pair again"})
	if !errors.Is(err, ErrExists) {
		t.Fatalf("re-adding the same pair under a derived id = %v, want ErrExists", err)
	}
	if again.ID != "abcd1234" {
		t.Fatalf("ErrExists returned id %q, want the stored legacy id", again.ID)
	}
	list, err := fs.List()
	if err != nil || len(list) != 1 {
		t.Fatalf("List = %v (%d entries), want 1 — the duplicate was stored", err, len(list))
	}
}

// A set-shaped entry (Right nil) and a pair-shaped entry with the SAME left
// half are two different rows, and each reads back in its own shape. This is
// the shared fixture that makes the two arms disagree.
func TestSetAndPairWithTheSameLeftAreDistinctRows(t *testing.T) {
	t.Parallel()
	fs := NewFileStore(t.TempDir())
	left := mustLink(t, "gg://r@main...feat/x")
	right := mustLink(t, "gg://r@abc1234")

	set, err := fs.Add(Entry{Left: left, Label: "the preview"})
	if err != nil {
		t.Fatalf("Add set: %v", err)
	}
	pair, err := fs.Add(Entry{Left: left, Right: &right, Label: "the comparison"})
	if err != nil {
		t.Fatalf("Add pair: %v", err)
	}
	if set.ID == pair.ID {
		t.Fatal("the set and the pair share an id")
	}

	gotSet, err := fs.Get(set.ID)
	if err != nil {
		t.Fatalf("Get set: %v", err)
	}
	if !gotSet.IsSet() || gotSet.Right != nil {
		t.Fatalf("set read back as a pair: %+v", gotSet)
	}
	gotPair, err := fs.Get(pair.ID)
	if err != nil {
		t.Fatalf("Get pair: %v", err)
	}
	if gotPair.IsSet() || gotPair.Right == nil {
		t.Fatalf("pair read back as a set: %+v", gotPair)
	}
	if gotPair.Right.String() != right.String() {
		t.Fatalf("right half = %q, want %q", gotPair.Right.String(), right.String())
	}
}

// The file on disk holds LINK TEXT, so it stays readable and every stored row
// is a String(Parse(s)) == s witness.
func TestTheFileHoldsLinkText(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fs := NewFileStore(dir)
	if _, err := fs.Add(Entry{Left: mustLink(t, "gg://r@main...feat/x"), Label: "x"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "savedcompare.toml"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(data), "gg://r@main...feat/x") {
		t.Fatalf("stored file does not hold the link text:\n%s", data)
	}
}

// A corrupt file is an ERROR, never "empty" — swallowing it would let the
// next write destroy the store (the rule internal/preview already follows).
func TestACorruptFileIsAnError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "savedcompare.toml"), []byte("this is not toml ["), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := NewFileStore(dir).List(); err == nil {
		t.Fatal("List read a corrupt file as empty")
	}
}

func TestRenameAndRemove(t *testing.T) {
	t.Parallel()
	fs := NewFileStore(t.TempDir())
	e, err := fs.Add(Entry{Left: mustLink(t, "gg://r@main...feat/x")})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if e.Label != "main...feat/x" {
		t.Fatalf("default label = %q, want %q", e.Label, "main...feat/x")
	}
	if err := fs.Rename(e.ID, "renamed"); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	got, err := fs.Get(e.ID)
	if err != nil || got.Label != "renamed" {
		t.Fatalf("after Rename: %+v, %v", got, err)
	}
	if err := fs.Remove(e.ID); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := fs.Get(e.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get after Remove = %v, want ErrNotFound", err)
	}
	if err := fs.Remove(e.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second Remove = %v, want ErrNotFound", err)
	}
}
