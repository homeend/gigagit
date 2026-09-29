package web

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
)

// webReviewDoc is a review document of newRepoDir's tip: one note on f.txt
// (the file the commit changes) and one on a file it does not touch, which the
// view lists under "Other notes".
const webReviewDoc = `{"version":1,"summary":"## Overview\nLooks fine.","meta":{"verdict":"approve"},"files":[
 {"path":"f.txt","summary":"adds A","annotations":[{"newRange":[1,1],"summary":"A is unused","rationale":"nothing calls it"}]},
 {"path":"zzz.go","annotations":[{"newRange":[1,1],"summary":"not in this commit"}]}]}`

type reviewHeadResp struct {
	ID      string `json:"id"`
	Commit  string `json:"commit"`
	Branch  string `json:"branch"`
	Agent   string `json:"agent"`
	Summary string `json:"summary"`
	Created string `json:"created"`
}

// reviewServer serves a two-commit repo holding one review (text) of its tip.
func reviewServer(t *testing.T, text string) (ts *httptest.Server, svc *domain.Service, sha, id string) {
	t.Helper()
	svc = domain.Open(newRepoDir(t, 2))
	svc.UseNotesDir(t.TempDir())
	ctx := context.Background()
	sha, _, err := svc.ResolveRev(ctx, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	sha = strings.TrimSpace(sha)
	tg := domain.ReviewTarget{Kind: domain.ReviewRange, Range: sha + "^.." + sha, Label: sha[:7]}
	id, _, err = svc.SaveReview(ctx, domain.SaveReview{Target: tg, Agent: "Claude", Text: text})
	if err != nil {
		t.Fatal(err)
	}
	return serve(t, New(svc)), svc, sha, id
}

func TestNoteCountsCarryReviews(t *testing.T) {
	t.Parallel()
	ts, _, sha, id := reviewServer(t, webReviewDoc)
	var got struct {
		Reviews []reviewHeadResp `json:"reviews"`
	}
	if code := getJSON(t, ts, "/api/notes/counts", &got); code != http.StatusOK {
		t.Fatalf("GET /api/notes/counts = %d", code)
	}
	if len(got.Reviews) != 1 {
		t.Fatalf("reviews = %+v, want one", got.Reviews)
	}
	r := got.Reviews[0]
	if r.ID != id || r.Commit != sha || r.Agent != "Claude" || r.Created == "" {
		t.Errorf("review head = %+v (want id %s, commit %s, agent Claude, a created time)", r, id, sha)
	}
}

func TestNoteCountsReviewsIsArrayWhenNone(t *testing.T) {
	t.Parallel()
	ts := notesServer(t)
	resp, err := http.Get(ts.URL + "/api/notes/counts")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(b), `"reviews":[]`) {
		t.Errorf("counts without reviews = %s, want \"reviews\":[]", b)
	}
}

func TestCommitFilesCarryReviews(t *testing.T) {
	t.Parallel()
	ts, _, sha, id := reviewServer(t, webReviewDoc)
	var got struct {
		Reviews []reviewHeadResp `json:"reviews"`
	}
	if code := getJSON(t, ts, "/api/commit/"+sha, &got); code != http.StatusOK {
		t.Fatalf("GET /api/commit = %d", code)
	}
	if len(got.Reviews) != 1 || got.Reviews[0].ID != id {
		t.Fatalf("commit reviews = %+v, want the one review %s", got.Reviews, id)
	}
	// A short sha names the same commit.
	getJSON(t, ts, "/api/commit/"+sha[:8], &got)
	if len(got.Reviews) != 1 {
		t.Errorf("a short sha lost the commit's review: %+v", got.Reviews)
	}
	resp, err := http.Get(ts.URL + "/api/commit/" + sha + "~1")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(b), `"reviews":[]`) {
		t.Errorf("the parent's body = %s, want \"reviews\":[]", b)
	}
}

type reviewViewResp struct {
	ID         string `json:"id"`
	Agent      string `json:"agent"`
	Label      string `json:"label"`
	Structured bool   `json:"structured"`
	Base       string `json:"base"`
	Tip        string `json:"tip"`
	Range      bool   `json:"range"`
	Files      []struct {
		Path string `json:"path"`
	} `json:"files"`
	Counts     map[string]int    `json:"counts"`
	Summaries  map[string]string `json:"summaries"`
	OverviewMd any               `json:"overviewMd"`
	Meta       string            `json:"meta"`
	Text       string            `json:"text"`
	Notes      int               `json:"notes"`
	NoteFiles  int               `json:"note_files"`
	Other      []struct {
		Path, Line, Summary string
	} `json:"other"`
}

func TestReviewViewShape(t *testing.T) {
	t.Parallel()
	ts, _, sha, id := reviewServer(t, webReviewDoc)
	var got reviewViewResp
	if code := getJSON(t, ts, "/api/review/"+id, &got); code != http.StatusOK {
		t.Fatalf("GET /api/review/{id} = %d", code)
	}
	if got.ID != id || !got.Structured || got.Range || got.Tip != sha || got.Agent != "Claude" {
		t.Fatalf("head fields = %+v", got)
	}
	if len(got.Files) != 1 || got.Files[0].Path != "f.txt" {
		t.Errorf("files = %+v, want the commit's f.txt", got.Files)
	}
	if got.Counts["f.txt"] != 1 || got.Summaries["f.txt"] != "adds A" {
		t.Errorf("counts %v / summaries %v", got.Counts, got.Summaries)
	}
	if len(got.Other) != 1 || got.Other[0].Path != "zzz.go" || got.Other[0].Line != "1" {
		t.Errorf("other notes = %+v, want zzz.go:1", got.Other)
	}
	if got.Notes != 2 || got.NoteFiles != 2 || got.OverviewMd == nil || got.Meta != "verdict: approve" {
		t.Errorf("notes %d on %d files, overview %v, meta %q", got.Notes, got.NoteFiles, got.OverviewMd, got.Meta)
	}
	if !strings.Contains(got.Text, `"summary"`) || got.Label != sha[:7] {
		t.Errorf("text %q / label %q", got.Text, got.Label)
	}
}

func TestReviewViewProse(t *testing.T) {
	t.Parallel()
	ts, _, _, id := reviewServer(t, "just prose")
	var got reviewViewResp
	if code := getJSON(t, ts, "/api/review/"+id, &got); code != http.StatusOK {
		t.Fatalf("GET = %d", code)
	}
	if got.Structured || len(got.Files) != 0 || got.Text != "just prose" || got.OverviewMd == nil {
		t.Errorf("prose review = %+v", got)
	}
}

func TestReviewViewUnknown404(t *testing.T) {
	t.Parallel()
	ts, _, _, _ := reviewServer(t, webReviewDoc)
	if code := getJSON(t, ts, "/api/review/nope", nil); code != http.StatusNotFound {
		t.Errorf("unknown review = %d, want 404", code)
	}
}

func TestReviewNotesReadOnlyOnFile(t *testing.T) {
	t.Parallel()
	ts, _, _, id := reviewServer(t, webReviewDoc)
	var got struct {
		Notes []struct {
			ID       string `json:"id"`
			Line     int    `json:"line"`
			Summary  string `json:"summary"`
			ReadOnly bool   `json:"read_only"`
		} `json:"notes"`
	}
	if code := getJSON(t, ts, "/api/review/notes?id="+id+"&path=f.txt&status=M", &got); code != http.StatusOK {
		t.Fatalf("GET /api/review/notes = %d", code)
	}
	if len(got.Notes) != 1 {
		t.Fatalf("notes = %+v, want one", got.Notes)
	}
	n := got.Notes[0]
	if !n.ReadOnly || !strings.HasPrefix(n.ID, "review:") || n.Line != 1 || n.Summary != "A is unused" {
		t.Errorf("note = %+v", n)
	}
	// Another path gets none of them (the fresh value: getJSON leaves its
	// target untouched on a non-200, which must read as "no notes" too).
	var other struct {
		Notes []struct{ ID string } `json:"notes"`
	}
	getJSON(t, ts, "/api/review/notes?id="+id+"&path=g.txt&status=A", &other)
	if len(other.Notes) != 0 {
		t.Errorf("another path's notes = %+v, want none", other.Notes)
	}
}

func TestReviewNotesRejectsUnsafePath(t *testing.T) {
	t.Parallel()
	ts, _, _, id := reviewServer(t, webReviewDoc)
	if code := getJSON(t, ts, "/api/review/notes?id="+id+"&path=-x", nil); code != http.StatusBadRequest {
		t.Errorf("unsafe path = %d, want 400", code)
	}
	if code := getJSON(t, ts, "/api/review/notes?id=nope&path=f.txt", nil); code != http.StatusNotFound {
		t.Errorf("unknown id = %d, want 404", code)
	}
}
