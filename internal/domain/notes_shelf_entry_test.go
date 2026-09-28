package domain

import (
	"context"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/shelf"
)

// shelfNoteFixture is a real repo with a notes store, a shelf store and one
// shelved file set.
func shelfNoteFixture(t *testing.T) (*Service, model.ShelfEntry) {
	t.Helper()
	dir := noteSideRepo(t)
	svc := svcIn(t, dir)
	svc.SetShelfStore(shelf.NewFileStore(t.TempDir()))
	svc.UseNotesDir(t.TempDir())
	e, err := svc.ShelfAddFiles(context.Background(), []model.FileAddress{
		{State: model.StateUnstaged, Worktree: dir, Path: "a.go"},
	}, "WIP on main")
	if err != nil {
		t.Fatalf("ShelfAddFiles: %v", err)
	}
	return svc, e
}

func addShelfNote(t *testing.T, svc *Service, id string) model.Note {
	t.Helper()
	n, err := svc.NoteAdd(context.Background(), model.Note{
		Source: model.NoteSourceAgent, Author: "gg", Address: ShelfEntryNote(id),
		Summary: "Recycled from /x (main)", Rationale: "Deleted (not in this set):\n  gone.go",
	})
	if err != nil {
		t.Fatalf("NoteAdd: %v", err)
	}
	return n
}

func TestShelfNoteAddAndRead(t *testing.T) {
	t.Parallel()
	svc, e := shelfNoteFixture(t)
	ctx := context.Background()
	addShelfNote(t, svc, e.ID)

	got, err := svc.ShelfNotes(ctx, e.ID)
	if err != nil || len(got) != 1 {
		t.Fatalf("ShelfNotes = %v, %v; want one note", got, err)
	}
	if got[0].Status != model.NoteActive || !strings.Contains(got[0].Note.Rationale, "gone.go") {
		t.Fatalf("want an active note carrying its rationale, got %+v", got[0])
	}
	at, err := svc.NotesAt(ctx, ShelfEntryNote(e.ID))
	if err != nil || len(at) != 1 || at[0].Status != model.NoteActive {
		t.Fatalf("NotesAt(shelf entry) = %+v, %v; want the one active note", at, err)
	}
	c, err := svc.NoteCounts(ctx)
	if err != nil || c.ByShelf[e.ID] != 1 {
		t.Fatalf("NoteCounts.ByShelf = %v, %v; want 1 for %s", c.ByShelf, err, e.ID)
	}
}

func TestShelfNoteAddUnknownEntry(t *testing.T) {
	t.Parallel()
	svc, _ := shelfNoteFixture(t)
	_, err := svc.NoteAdd(context.Background(), model.Note{Address: ShelfEntryNote("nope"), Summary: "x"})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("want a not-found error, got %v", err)
	}
}

func TestShelfRemoveDropsItsNotes(t *testing.T) {
	t.Parallel()
	svc, e := shelfNoteFixture(t)
	ctx := context.Background()
	addShelfNote(t, svc, e.ID)
	if err := svc.ShelfRemove(ctx, e.ID); err != nil {
		t.Fatal(err)
	}
	all, _ := svc.notesStore(ctx).Load()
	for _, n := range all {
		if n.Address.ShelfID == e.ID {
			t.Fatalf("removing the entry must drop its notes, still have %+v", n)
		}
	}
	if c, _ := svc.NoteCounts(ctx); c.ByShelf[e.ID] != 0 {
		t.Fatalf("the count must be invalidated, got %d", c.ByShelf[e.ID])
	}
}

func TestSweepKeepsShelfNoteUntilItsEntryIsGone(t *testing.T) {
	t.Parallel()
	svc, e := shelfNoteFixture(t)
	ctx := context.Background()
	addShelfNote(t, svc, e.ID)
	if _, err := svc.sweepNotes(ctx); err != nil {
		t.Fatal(err)
	}
	if got, _ := svc.ShelfNotes(ctx, e.ID); len(got) != 1 {
		t.Fatalf("a shelf note whose entry exists must survive the sweep, got %d", len(got))
	}
	// Bypass the cascade: the entry disappears behind the notes store's back.
	if err := svc.shelfStore(ctx).Remove(e.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.sweepNotes(ctx); err != nil {
		t.Fatal(err)
	}
	all, _ := svc.notesStore(ctx).Load()
	if len(all) != 0 {
		t.Fatalf("the sweep must drop a shelf note whose entry is gone, got %+v", all)
	}
}

func TestOverviewListsShelfEntryNotes(t *testing.T) {
	t.Parallel()
	svc, e := shelfNoteFixture(t)
	addShelfNote(t, svc, e.ID)
	ov, err := svc.NotesOverview(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(ov.Shelves) != 1 || len(ov.Shelves[0].Entry) != 1 || len(ov.Shelves[0].Files) != 0 {
		t.Fatalf("want one shelf group with one entry note, got %+v", ov.Shelves)
	}
	if ov.Shelves[0].Label != "WIP on main" || ov.Count() != 1 {
		t.Fatalf("label %q count %d", ov.Shelves[0].Label, ov.Count())
	}
}

// An OLDER gg does not know shelf-level notes: its sweep resolves one like any
// shelf note — against the entry's whole stored blob — and drops it unless the
// fingerprint matches. The note therefore carries a real one (line 1 of the
// blob, which never changes), so a mixed-version machine keeps it.
func TestShelfNoteSurvivesAnOlderSweep(t *testing.T) {
	t.Parallel()
	svc, e := shelfNoteFixture(t)
	ctx := context.Background()
	n := addShelfNote(t, svc, e.ID)
	blob, err := svc.ShelfBlob(ctx, e.ID)
	if err != nil {
		t.Fatal(err)
	}
	// What the old sweep does: noteSideLines(StateShelf, new) = the blob's lines.
	if status, _ := resolveOne(n, splitLines(blob)); status != model.NoteActive {
		t.Fatalf("an older sweep would see %s (range %v, hash %q) and delete the note", status, n.Range, n.ContextHash)
	}
}
