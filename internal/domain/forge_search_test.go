package domain

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/model"
)

func searchFixture() []model.PullRequest {
	day := func(d int) time.Time { return time.Date(2026, 8, d, 9, 0, 0, 0, time.UTC) }
	return []model.PullRequest{
		{Number: 3, Title: "Drop legacy login", State: model.PRStateClosed, Updated: day(10)},
		{Number: 5, Title: "Fix login redirect", State: model.PRStateMerged, Updated: day(20)},
	}
}

func TestPRSearchAsksTheProviderNormalized(t *testing.T) {
	t.Parallel()
	ff := &fakeForge{search: searchFixture(), searchMore: true}
	svc := newForgeSvc(t, ff)
	res, err := svc.PRSearch(context.Background(), PRQuery{Text: "  login "})
	if err != nil {
		t.Fatal(err)
	}
	want := PRQuery{State: PRStateFilterAll, Text: "login", Limit: PRSearchDefaultLimit}
	if len(ff.searchCalls) != 1 || ff.searchCalls[0] != want || res.Query != want {
		t.Fatalf("calls = %+v, result query = %+v", ff.searchCalls, res.Query)
	}
	if !res.More || len(res.PRs) != 2 || res.PRs[0].Number != 5 || res.PRs[1].Number != 3 {
		t.Errorf("result = %+v (want newest-updated first, more)", res)
	}
}

func TestPRSearchNumberLookup(t *testing.T) {
	t.Parallel()
	ff := &fakeForge{search: searchFixture(), byNum: map[int]model.PullRequest{
		5: {Number: 5, Title: "Fix login redirect", State: model.PRStateMerged, Body: "b"}}}
	svc := newForgeSvc(t, ff)
	ctx := context.Background()
	for _, text := range []string{"5", "#5", " #5 "} {
		res, err := svc.PRSearch(ctx, PRQuery{Text: text, State: PRStateFilterOpen})
		if err != nil || len(res.PRs) != 1 || res.PRs[0].Number != 5 || res.More {
			t.Fatalf("%q: %+v, %v", text, res, err)
		}
	}
	// Unknown to the forge: an empty answer, not a failure.
	res, err := svc.PRSearch(ctx, PRQuery{Text: "#404"})
	if err != nil || len(res.PRs) != 0 {
		t.Fatalf("#404: %+v, %v", res, err)
	}
	if len(ff.searchCalls) != 0 {
		t.Errorf("a number lookup must not search: %+v", ff.searchCalls)
	}
	// Not numbers: ordinary search text.
	for _, text := range []string{"0", "#", "5 login"} {
		if _, err := svc.PRSearch(ctx, PRQuery{Text: text}); err != nil {
			t.Fatal(err)
		}
	}
	if len(ff.searchCalls) != 3 {
		t.Errorf("search calls = %d, want 3", len(ff.searchCalls))
	}
}

func TestPRSearchDoesNotGrowTheList(t *testing.T) {
	t.Parallel()
	ff := &fakeForge{search: searchFixture()}
	svc := newForgeSvc(t, ff)
	ctx := context.Background()
	if _, err := svc.PRSearch(ctx, PRQuery{}); err != nil {
		t.Fatal(err)
	}
	prs, err := svc.PullRequests(ctx)
	if err != nil || len(prs) != 0 {
		t.Errorf("list after a search = %+v, %v (a search must not add rows)", prs, err)
	}
}

func TestPRSearchSeedsTheCache(t *testing.T) {
	t.Parallel()
	ff := &fakeForge{search: searchFixture()}
	svc := newForgeSvc(t, ff)
	if _, err := svc.PRSearch(context.Background(), PRQuery{}); err != nil {
		t.Fatal(err)
	}
	svc.forgeMu.Lock()
	e, ok := svc.takePRLocked(5)
	svc.forgeMu.Unlock()
	if !ok || e.full || e.pr.Title != "Fix login redirect" {
		t.Errorf("cache entry = %+v, %v", e, ok)
	}
}

func TestPRSearchLast(t *testing.T) {
	t.Parallel()
	ff := &fakeForge{search: searchFixture()}
	svc := newForgeSvc(t, ff)
	ctx := context.Background()
	if _, ok := svc.PRSearchLast(); ok {
		t.Fatal("a last result before any search")
	}
	if _, err := svc.PRSearch(ctx, PRQuery{Text: "login"}); err != nil {
		t.Fatal(err)
	}
	last, ok := svc.PRSearchLast()
	if !ok || last.Query.Text != "login" || len(last.PRs) != 2 || last.At.IsZero() {
		t.Fatalf("last = %+v, %v", last, ok)
	}
	last.PRs[0].Title = "scribbled" // the caller's copy
	if again, _ := svc.PRSearchLast(); again.PRs[0].Title == "scribbled" {
		t.Error("PRSearchLast handed out its own slice")
	}
	ff.mu.Lock()
	ff.searchErr = errors.New("HTTP 502")
	ff.mu.Unlock()
	if _, err := svc.PRSearch(ctx, PRQuery{Text: "other"}); err == nil {
		t.Fatal("want the forge error")
	}
	if last, _ := svc.PRSearchLast(); last.Query.Text != "login" {
		t.Errorf("a failed search replaced the last result: %+v", last.Query)
	}
}

func TestPRSearchBadQuery(t *testing.T) {
	t.Parallel()
	ff := &fakeForge{}
	svc := newForgeSvc(t, ff)
	if _, err := svc.PRSearch(context.Background(), PRQuery{State: "draft"}); err == nil {
		t.Fatal("want an error")
	}
	if _, err := svc.PRSearch(context.Background(), PRQuery{Limit: PRSearchMaxLimit + 1}); err == nil {
		t.Fatal("want an error")
	}
	if len(ff.searchCalls) != 0 {
		t.Errorf("a bad query reached the provider: %+v", ff.searchCalls)
	}
}

func TestPRSearchNoForge(t *testing.T) {
	t.Parallel()
	svc := newForgeSvc(t, &fakeForge{detectErr: errors.New("no gh")})
	if _, err := svc.PRSearch(context.Background(), PRQuery{}); !errors.Is(err, ErrForgeUnavailable) {
		t.Errorf("err = %v", err)
	}
}
