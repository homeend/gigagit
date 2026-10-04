package notes

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

var sha40 = strings.Repeat("c", 40)

func liveNote(id, wt string, sec int) model.Note {
	n := noteAt(id, sec)
	n.Address = model.FileAddress{State: model.StateUnstaged, Worktree: wt, Path: "a.go"}
	return n
}

func commitNote(id, preview string, sec int) model.Note {
	n := noteAt(id, sec)
	n.Address = model.FileAddress{State: model.StateCommitted, Commit: sha40, Path: "a.go"}
	n.Preview = preview
	return n
}

func TestPutRoutesEachNoteToItsPartFile(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	fs := NewFileStore(root)
	shelf := noteAt("s0000000", 4)
	shelf.Address = model.FileAddress{State: model.StateShelf, ShelfID: "e1", Path: "a.go"}
	for _, n := range []model.Note{
		liveNote("w0000000", "/repo", 1), commitNote("c0000000", "", 2),
		commitNote("p0000000", "main...feat", 3), shelf,
	} {
		if err := fs.Put(n); err != nil {
			t.Fatalf("Put %s: %v", n.ID, err)
		}
	}
	for part, id := range map[Part]string{
		WorktreePart("/repo"): "w0000000", PartCommits: "c0000000",
		PartPreviews: "p0000000", PartShelf: "s0000000",
	} {
		got, err := fs.Load(part)
		if err != nil || len(got) != 1 || got[0].ID != id {
			t.Errorf("Load(%s) = %+v, %v; want only %s", part, got, err, id)
		}
		if _, err := os.Stat(part.file(root)); err != nil {
			t.Errorf("%s: file missing: %v", part, err)
		}
	}
	all, err := fs.LoadAll()
	if err != nil || len(all) != 4 {
		t.Fatalf("LoadAll = %d notes, %v; want 4", len(all), err)
	}
	if _, err := os.Stat(filepath.Join(root, "notes.toml")); !os.IsNotExist(err) {
		t.Fatal("the split store must never write notes.toml")
	}
}

// A reply carries no Preview of its own: it must still land with its
// merge-preview root, or the preview would lose its threads.
func TestReplyFollowsItsRootsPart(t *testing.T) {
	t.Parallel()
	fs := NewFileStore(t.TempDir())
	root := commitNote("r0000000", "main...feat", 1)
	reply := commitNote("p0000000", "", 2)
	reply.ParentID = root.ID
	for _, n := range []model.Note{root, reply} {
		if err := fs.Put(n); err != nil {
			t.Fatal(err)
		}
	}
	prev, _ := fs.Load(PartPreviews)
	if len(prev) != 2 {
		t.Fatalf("previews part = %+v, want root + reply", prev)
	}
	if com, _ := fs.Load(PartCommits); len(com) != 0 {
		t.Fatalf("the reply leaked into commits: %+v", com)
	}
}

// A reply whose root may sit in an unreadable part is refused rather than
// filed by guess, where the orphan prune would silently drop it.
func TestReplyPutFailsWhenItsRootMayBeInACorruptPart(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	fs := NewFileStore(root)
	parent := commitNote("r0000000", "main...feat", 1)
	if err := fs.Put(parent); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(PartPreviews.file(root), []byte("notes = [[["), 0o644); err != nil {
		t.Fatal(err)
	}
	reply := commitNote("p0000000", "", 2)
	reply.ParentID = parent.ID
	if err := fs.Put(reply); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("Put(reply) = %v, want ErrCorrupt", err)
	}
	if com, _ := fs.Load(PartCommits); len(com) != 0 {
		t.Fatalf("the reply was filed by guess: %+v", com)
	}
}

func TestRemoveFindsTheIdInAnyPart(t *testing.T) {
	t.Parallel()
	fs := NewFileStore(t.TempDir())
	root := liveNote("r0000000", "/repo", 1)
	reply := liveNote("p0000000", "/repo", 2)
	reply.ParentID = root.ID
	for _, n := range []model.Note{root, reply, commitNote("c0000000", "", 3)} {
		if err := fs.Put(n); err != nil {
			t.Fatal(err)
		}
	}
	if err := fs.Remove(root.ID); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	all, _ := fs.LoadAll()
	if len(all) != 1 || all[0].ID != "c0000000" {
		t.Fatalf("left %+v, want only the commit note", all)
	}
	if err := fs.Remove("nosuch00"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Remove(unknown) = %v, want ErrNotFound", err)
	}
}

func TestPutRefusesToMoveANoteToAnotherPart(t *testing.T) {
	t.Parallel()
	fs := NewFileStore(t.TempDir())
	n := liveNote("m0000000", "/repo", 1)
	if err := fs.Put(n); err != nil {
		t.Fatal(err)
	}
	n.Address = model.FileAddress{State: model.StateCommitted, Commit: sha40, Path: "a.go"}
	if err := fs.Put(n); !errors.Is(err, ErrPartChange) {
		t.Fatalf("Put across parts = %v, want ErrPartChange", err)
	}
}

func TestCorruptPartLeavesOtherPartsWorking(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	fs := NewFileStore(root)
	if err := fs.Put(commitNote("c0000000", "", 1)); err != nil {
		t.Fatal(err)
	}
	if err := fs.Put(liveNote("w0000000", "/repo", 2)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(PartCommits.file(root), []byte("notes = [[["), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := fs.Load(WorktreePart("/repo")); err != nil || len(got) != 1 {
		t.Fatalf("a healthy part must stay readable: %+v, %v", got, err)
	}
	if _, err := fs.LoadAll(); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("LoadAll = %v, want ErrCorrupt naming the bad part", err)
	}
	if err := fs.Remove("w0000000"); err != nil {
		t.Fatalf("removing from a healthy part must not fail on a corrupt one: %v", err)
	}
	moved, err := fs.Quarantine()
	if err != nil || !strings.Contains(moved, "commits.toml.corrupt-") {
		t.Fatalf("Quarantine = %q, %v", moved, err)
	}
	if _, err := fs.LoadAll(); err != nil {
		t.Fatalf("after quarantine LoadAll = %v", err)
	}
}

func TestCapAppliesPerPart(t *testing.T) {
	t.Parallel()
	fs := NewFileStore(t.TempDir())
	fs.SetPolicy(Policy{MaxEntries: 2})
	for i, n := range []model.Note{
		commitNote("c1000000", "", 1), commitNote("c2000000", "", 2),
		liveNote("w1000000", "/repo", 3), liveNote("w2000000", "/repo", 4),
	} {
		if err := fs.Put(n); err != nil {
			t.Fatalf("Put %d: %v", i, err)
		}
	}
	if all, _ := fs.LoadAll(); len(all) != 4 {
		t.Fatalf("a cap of 2 per part must keep 2+2, got %d", len(all))
	}
	if err := fs.Put(commitNote("c3000000", "", 5)); err != nil {
		t.Fatal(err)
	}
	com, _ := fs.Load(PartCommits)
	if len(com) != 2 || com[0].ID == "c1000000" || com[1].ID == "c1000000" {
		t.Fatalf("commits part = %+v, want the two newest", com)
	}
}

func TestSweepVisitsEveryPart(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	fs := NewFileStore(root)
	for _, n := range []model.Note{commitNote("c0000000", "", 1), liveNote("w0000000", "/gone", 2)} {
		if err := fs.Put(n); err != nil {
			t.Fatal(err)
		}
	}
	dropped, err := fs.Sweep(func(n model.Note) bool { return n.ID != "w0000000" })
	if err != nil || dropped != 1 {
		t.Fatalf("Sweep = %d, %v", dropped, err)
	}
	if _, err := os.Stat(WorktreePart("/gone").file(root)); !os.IsNotExist(err) {
		t.Fatal("the emptied worktree file must be removed")
	}
}

func TestEmptyStoreHasNoPartsAndCreatesNothing(t *testing.T) {
	t.Parallel()
	root := filepath.Join(t.TempDir(), "state")
	fs := NewFileStore(root)
	if ps, err := fs.Parts(); err != nil || len(ps) != 0 {
		t.Fatalf("Parts = %v, %v", ps, err)
	}
	if ns, err := fs.LoadAll(); err != nil || len(ns) != 0 {
		t.Fatalf("LoadAll = %v, %v", ns, err)
	}
	if n, err := fs.Sweep(func(model.Note) bool { return false }); err != nil || n != 0 {
		t.Fatalf("Sweep = %d, %v", n, err)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("reads and an empty sweep must not create the state directory")
	}
}
