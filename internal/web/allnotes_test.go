package web

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

// The web "View all notes" inventory: one read of domain.NotesOverview,
// flattened for the page's tree (the TUI popup's own source).
func TestNotesOverviewEndpoint(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 2)
	svc := domain.Open(dir)
	svc.UseNotesDir(t.TempDir())
	ts := serve(t, New(svc))
	sha := gitRun(t, dir, "rev-parse", "HEAD")

	if code, b := postJSONRaw(t, ts, "/api/notes/add",
		`{"path":"f.txt","state":"unstaged","side":"new","line":1,"summary":"wt note"}`); code != http.StatusOK {
		t.Fatalf("add wt = %d (%v)", code, b)
	}
	if code, b := postJSONRaw(t, ts, "/api/notes/add",
		`{"path":"f.txt","rev":"`+sha+`","state":"commit","side":"new","line":1,"summary":"commit note"}`); code != http.StatusOK {
		t.Fatalf("add commit = %d (%v)", code, b)
	}
	tg := domain.ReviewTarget{Kind: domain.ReviewRange, Range: sha + "^.." + sha, Label: "c2"}
	if _, _, err := svc.SaveReview(context.Background(), domain.SaveReview{Target: tg, Agent: "Claude", Text: "prose review"}); err != nil {
		t.Fatal(err)
	}

	var got notesOverviewWire
	if code := getJSON(t, ts, "/api/notes/overview", &got); code != http.StatusOK {
		t.Fatalf("GET /api/notes/overview = %d", code)
	}
	if len(got.Unstaged) != 1 || got.Unstaged[0].Path != "f.txt" || got.Unstaged[0].State != "unstaged" ||
		len(got.Unstaged[0].Notes) != 1 || got.Unstaged[0].Notes[0].Summary != "wt note" {
		t.Fatalf("unstaged = %+v", got.Unstaged)
	}
	if len(got.Commits) != 1 {
		t.Fatalf("commits = %+v", got.Commits)
	}
	c := got.Commits[0]
	if c.Hash != sha || c.Subject != "c2" || c.Missing || c.Time == 0 {
		t.Fatalf("commit head = %+v", c)
	}
	if len(c.Files) != 1 || c.Files[0].Status != "M" || c.Files[0].State != "commit" ||
		len(c.Files[0].Notes) != 1 || c.Files[0].Notes[0].Summary != "commit note" {
		t.Fatalf("commit files = %+v", c.Files)
	}
	if len(c.Reviews) != 1 || c.Reviews[0].Agent != "Claude" || c.Reviews[0].Kind != "commit" || c.Reviews[0].ID == "" {
		t.Fatalf("reviews = %+v", c.Reviews)
	}
	if got.Count != 3 {
		t.Fatalf("count = %d, want 3", got.Count)
	}
}

// Empty groups are arrays, never null: the page iterates them unguarded.
func TestNotesOverviewEmptyIsArrays(t *testing.T) {
	t.Parallel()
	ts := notesServer(t)
	_, _, raw := getRaw(t, ts, "/api/notes/overview")
	for _, want := range []string{`"unstaged":[]`, `"staged":[]`, `"untracked":[]`, `"commits":[]`, `"shelves":[]`} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("body %s lacks %s", raw, want)
		}
	}
}

// A shelf entry's own notes (a recycle's) ride the overview as the shelf's
// entry list, so the page can show every thread the count includes.
func TestNotesOverviewCarriesShelfEntryNotes(t *testing.T) {
	isolateState(t)
	dir := newRepoDir(t, 1)
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	svc := domain.Open(dir)
	svc.UseNotesDir(t.TempDir())
	ts := serve(t, New(svc))
	if code := postJSON(t, ts, "/api/shelf", `{"path":"f.txt","state":"unstaged"}`, "application/json", "", nil); code != http.StatusOK {
		t.Fatalf("shelf add: code = %d", code)
	}
	var list struct {
		Entries []struct {
			ID string `json:"id"`
		} `json:"entries"`
	}
	getJSON(t, ts, "/api/shelf", &list)
	id := list.Entries[0].ID
	if _, err := svc.NoteAdd(context.Background(), model.Note{
		Source: model.NoteSourceAgent, Author: "gg", Address: domain.ShelfEntryNote(id),
		Summary: "Recycled from /x (main)", Rationale: "Deleted (not in this set):\n  gone.txt",
	}); err != nil {
		t.Fatal(err)
	}
	var got notesOverviewWire
	if code := getJSON(t, ts, "/api/notes/overview", &got); code != http.StatusOK {
		t.Fatalf("GET /api/notes/overview = %d", code)
	}
	if len(got.Shelves) != 1 || got.Shelves[0].ID != id {
		t.Fatalf("shelves = %+v", got.Shelves)
	}
	e := got.Shelves[0].Entry
	if len(e) != 1 || e[0].Summary != "Recycled from /x (main)" || e[0].ID == "" {
		t.Fatalf("entry notes = %+v", e)
	}
	if got.Count != 1 {
		t.Fatalf("count = %d, want 1", got.Count)
	}
}
