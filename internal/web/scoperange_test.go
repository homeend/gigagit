package web

import (
	"context"
	"net/http"
	"net/url"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

// A note written in a commit pair sits on the pair's newer commit; the counts
// name the range there and /api/scope-range turns it into its two commits.
func TestScopeRangeOpensARangeReviewFromItsCommit(t *testing.T) {
	t.Parallel()
	ts, svc, c := pairNotesRepo(t)
	scope := c[0][:7] + ".." + c[2][:7]
	if _, err := svc.NoteAdd(context.Background(), model.Note{
		Source: model.NoteSourceAgent, Author: "ada", Preview: scope,
		Address: model.FileAddress{State: model.StateCommitted, Commit: c[2], Path: "a.txt"},
		Side:    model.NoteSideNew, Range: [2]int{1, 1}, Summary: "in the range",
	}); err != nil {
		t.Fatal(err)
	}
	var counts struct {
		Scopes map[string][]struct {
			Scope, Label string
			N            int
		} `json:"scopes_by_commit"`
	}
	if code := getJSON(t, ts, "/api/notes/counts", &counts); code != http.StatusOK {
		t.Fatalf("counts = %d", code)
	}
	got := counts.Scopes[c[2]]
	if len(got) != 1 || got[0].Scope != scope || got[0].Label != scope || got[0].N != 1 {
		t.Fatalf("scopes on c2 = %+v", got)
	}
	if len(counts.Scopes[c[1]]) != 0 {
		t.Fatalf("c1 holds no range review: %+v", counts.Scopes[c[1]])
	}

	var rng struct{ A, B, Label string }
	q := url.Values{"commit": {c[2]}, "scope": {scope}}.Encode()
	if code := getJSON(t, ts, "/api/scope-range?"+q, &rng); code != http.StatusOK {
		t.Fatalf("scope-range = %d", code)
	}
	if rng.A != c[0] || rng.B != c[2] || rng.Label != scope {
		t.Fatalf("range = %+v, want %s..%s", rng, c[0], c[2])
	}
}

// The scope is a wire value headed for git: only one the commit's own notes
// name is resolved, and the commit must be a full id.
func TestScopeRangeGuardsItsWireValues(t *testing.T) {
	t.Parallel()
	ts, _, c := pairNotesRepo(t)
	for name, tc := range map[string]struct {
		q    url.Values
		code int
	}{
		"short commit":  {url.Values{"commit": {c[2][:7]}, "scope": {"x..y"}}, http.StatusBadRequest},
		"no scope":      {url.Values{"commit": {c[2]}}, http.StatusBadRequest},
		"foreign scope": {url.Values{"commit": {c[2]}, "scope": {"main...--upload-pack=x"}}, http.StatusNotFound},
	} {
		var out map[string]any
		if code := getJSON(t, ts, "/api/scope-range?"+tc.q.Encode(), &out); code != tc.code {
			t.Errorf("%s: %d, want %d", name, code, tc.code)
		}
	}
}
