package web

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

// A shelf entry gg annotated carries its note count on the list row, and its
// notes (text included) are readable by id.
func TestShelfNotesWire(t *testing.T) {
	isolateState(t)
	dir := newRepoDir(t, 1)
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	svc := domain.Open(dir)
	svc.UseNotesDir(t.TempDir())
	ts := serve(t, New(svc))
	if code := postJSON(t, ts, "/api/shelf", `{"path":"f.txt","state":"unstaged"}`, "application/json", "", nil); code != http.StatusOK {
		t.Fatalf("add: code = %d", code)
	}
	var list struct {
		Entries []struct {
			ID    string `json:"id"`
			Notes int    `json:"notes"`
		} `json:"entries"`
	}
	getJSON(t, ts, "/api/shelf", &list)
	if len(list.Entries) != 1 || list.Entries[0].Notes != 0 {
		t.Fatalf("before the note: %+v", list.Entries)
	}
	id := list.Entries[0].ID
	if _, err := svc.NoteAdd(context.Background(), model.Note{
		Source: model.NoteSourceAgent, Author: "gg", Address: domain.ShelfEntryNote(id),
		Summary: "Recycled from /x (main)", Rationale: "Deleted (not in this set):\n  gone.txt",
	}); err != nil {
		t.Fatal(err)
	}
	getJSON(t, ts, "/api/shelf", &list)
	if list.Entries[0].Notes != 1 {
		t.Fatalf("the row must count its note, got %+v", list.Entries)
	}
	var got struct {
		Notes []struct {
			Author    string `json:"author"`
			Summary   string `json:"summary"`
			Rationale string `json:"rationale"`
			Created   string `json:"created"`
		} `json:"notes"`
	}
	if code := getJSON(t, ts, "/api/shelf/notes?id="+id, &got); code != http.StatusOK {
		t.Fatalf("notes: code = %d", code)
	}
	if len(got.Notes) != 1 || got.Notes[0].Author != "gg" || got.Notes[0].Rationale == "" || got.Notes[0].Created == "" {
		t.Fatalf("notes = %+v", got.Notes)
	}
	if code := getJSON(t, ts, "/api/shelf/notes", &got); code != http.StatusBadRequest {
		t.Fatalf("no id: code = %d, want 400", code)
	}
}
