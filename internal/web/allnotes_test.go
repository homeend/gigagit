package web

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
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
