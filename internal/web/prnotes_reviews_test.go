package web

import (
	"context"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

const prReviewDoc = `{"version":1,"summary":"pr review","files":[{"path":"pr7.txt","annotations":[{"newRange":[1,1],"summary":"here"}]}]}`

// savePRReviewWeb stores a review on PR #7's scope the way /gg-review does
// (plan 1's ScopeReviewTarget over the PR's note set).
func savePRReviewWeb(t *testing.T, srv *Server, doc string) string {
	t.Helper()
	svc := srv.service()
	ctx := context.Background()
	pr, ok := srv.cachedPR(svc, 7)
	if !ok {
		t.Fatal("PR #7 is not listed")
	}
	pair := svc.PRPair(ctx, pr)
	set, err := svc.PreviewNotes(ctx, pair.Head, pair.Base)
	if err != nil {
		t.Fatal(err)
	}
	id, _, err := svc.SaveReview(ctx, domain.SaveReview{Target: domain.ScopeReviewTarget(set), Agent: "claude", Text: doc})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// R4 / W1: a PR's stored reviews ride /api/pr/notes (the counts read the page
// makes after every open and send), the preview's reviewHead shape.
// Serial: sendServer.
func TestPRNotesListsThePRsReviews(t *testing.T) {
	ts, _, srv, _, _ := sendServerFull(t)
	id := savePRReviewWeb(t, srv, prReviewDoc)
	var out struct {
		Reviews []reviewHeadResp `json:"reviews"`
	}
	if code := getJSON(t, ts, "/api/pr/notes?n=7", &out); code != 200 || len(out.Reviews) != 1 || out.Reviews[0].ID != id || out.Reviews[0].Agent != "claude" || out.Reviews[0].Older {
		t.Fatalf("= %d %+v", code, out.Reviews)
	}
}

// R10: a PR's overviews ride /api/pr/notes only — never the counts' rows.
// Serial: sendServer.
func TestPRNotesListsThePRsOverviews(t *testing.T) {
	ts, _, srv, _, _ := sendServerFull(t)
	svc, ctx := srv.service(), context.Background()
	pr, ok := srv.cachedPR(svc, 7)
	if !ok {
		t.Fatal("PR #7 is not listed")
	}
	pair := svc.PRPair(ctx, pr)
	set, err := svc.PreviewNotes(ctx, pair.Head, pair.Base)
	if err != nil {
		t.Fatal(err)
	}
	tg, err := svc.LinkOverviewTarget(ctx, domain.Resolved{Preview: &set, Commit: set.Tip,
		Addr: model.FileAddress{State: model.StateCommitted, Commit: set.Tip}})
	if err != nil {
		t.Fatal(err)
	}
	got, _, err := svc.SaveOverview(ctx, domain.SaveOverview{Target: tg, Agent: "claude", Text: []byte(
		`{"version":1,"title":"How #7 works","overview":"[here](pr7.txt:1)","files":[]}`)})
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Overviews []overviewHeadWire `json:"overviews"`
	}
	if code := getJSON(t, ts, "/api/pr/notes?n=7", &out); code != 200 || len(out.Overviews) != 1 || out.Overviews[0].ID != got.ID || out.Overviews[0].Older {
		t.Fatalf("pr notes = %d %+v", code, out.Overviews)
	}
	var c struct {
		Overviews   []overviewHeadWire `json:"overviews"`
		OverviewIDs []string           `json:"overview_ids"`
	}
	if getJSON(t, ts, "/api/notes/counts", &c); len(c.Overviews) != 0 || len(c.OverviewIDs) != 1 {
		t.Fatalf("counts = %+v (a PR overview is not a row, but is an id)", c)
	}
}
