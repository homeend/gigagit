package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

// shelfWithNote shelves a.txt and annotates the entry the way a recycle does.
func shelfWithNote(t *testing.T, dir string) string {
	t.Helper()
	code, out, errb := runCLI(t, dir, "shelf", "add", "a.txt")
	if code != 0 {
		t.Fatalf("shelf add: %s", errb)
	}
	id := strings.TrimSpace(out)
	svc := openCLIService(t, dir)
	if _, err := svc.NoteAdd(context.Background(), model.Note{
		Source: model.NoteSourceAgent, Author: "gg", Address: domain.ShelfEntryNote(id),
		Summary:   "Recycled from /x (main)",
		Rationale: "Deleted (not in this set):\n  gone.txt",
	}); err != nil {
		t.Fatalf("NoteAdd: %v", err)
	}
	return id
}

func TestNoteListShelf(t *testing.T) {
	dir := noteRepo(t)
	id := shelfWithNote(t, dir)
	code, out, errb := runCLI(t, dir, "note", "list", "--shelf", id)
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errb)
	}
	for _, want := range []string{"shelf " + id, "[agent]", "Recycled from /x (main)", "\n    Deleted (not in this set):", "\n      gone.txt"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestNoteListShelfWithFileIsUsage(t *testing.T) {
	dir := noteRepo(t)
	id := shelfWithNote(t, dir)
	if code, _, errb := runCLI(t, dir, "note", "list", "--shelf", id, "--file", "a.txt"); code != 2 {
		t.Fatalf("exit=%d stderr=%s, want 2", code, errb)
	}
}
