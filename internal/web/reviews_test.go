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
