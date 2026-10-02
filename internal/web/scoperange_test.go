package web

import (
	"context"
	"net/http"
	"net/url"
	"strings"
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

// A range review's notes belong to the review: the commit's own diff
// (/api/notes) leaves them out and its file badges (plain_by_commit_path) do
// not count them. View all notes opens a note where it is stored, so it asks
// for every note (scoped=1).
func TestCommitNotesLeaveOutRangeReviewNotes(t *testing.T) {
	t.Parallel()
	ts, svc, c := pairNotesRepo(t) // one plain note on a.txt at each commit
	scope := c[0][:7] + ".." + c[2][:7]
	if _, err := svc.NoteAdd(context.Background(), model.Note{
		Source: model.NoteSourceAgent, Author: "ada", Preview: scope,
		Address: model.FileAddress{State: model.StateCommitted, Commit: c[2], Path: "a.txt"},
		Side:    model.NoteSideNew, Range: [2]int{1, 1}, Summary: "in the range",
	}); err != nil {
		t.Fatal(err)
	}
	summaries := func(extra string) []string {
		t.Helper()
		var got struct {
			Notes []struct{ Summary string } `json:"notes"`
		}
		q := url.Values{"path": {"a.txt"}, "rev": {c[2]}, "state": {"commit"}}.Encode()
		if code := getJSON(t, ts, "/api/notes?"+q+extra, &got); code != http.StatusOK {
			t.Fatalf("notes = %d", code)
		}
		var out []string
		for _, n := range got.Notes {
			out = append(out, n.Summary)
		}
		return out
	}
	if got := summaries(""); len(got) != 1 || got[0] != "on c2" {
		t.Fatalf("the commit's own notes = %q, want the plain one only", got)
	}
	if got := summaries("&scoped=1"); len(got) != 2 {
		t.Fatalf("scoped=1 must return every note at the address: %q", got)
	}
	// The review opened from its row reads ITS notes only: the pair's read
	// narrowed by scope leaves the three plain notes out.
	var pair struct {
		Total int `json:"total"`
	}
	pq := url.Values{"a": {c[0]}, "b": {c[2]}}
	if code := getJSON(t, ts, "/api/pair/notes?"+pq.Encode(), &pair); code != http.StatusOK || pair.Total != 3 {
		t.Fatalf("the whole pair = %d notes (status %d), want 3", pair.Total, code)
	}
	pq.Set("scope", scope)
	if code := getJSON(t, ts, "/api/pair/notes?"+pq.Encode(), &pair); code != http.StatusOK || pair.Total != 1 {
		t.Fatalf("the review alone = %d notes (status %d), want 1", pair.Total, code)
	}
	var counts struct {
		All   map[string]int `json:"by_commit_path"`
		Plain map[string]int `json:"plain_by_commit_path"`
	}
	if code := getJSON(t, ts, "/api/notes/counts", &counts); code != http.StatusOK {
		t.Fatalf("counts = %d", code)
	}
	k := c[2] + ":a.txt"
	if counts.All[k] != 2 || counts.Plain[k] != 1 {
		t.Fatalf("counts at %s: all %d plain %d, want 2 and 1", k, counts.All[k], counts.Plain[k])
	}
}

// The page's commit file badge reads the plain count, and only View all
// notes asks for the range notes too.
func TestCommitFileBadgeCountsPlainNotes(t *testing.T) {
	t.Parallel()
	files := staticSrc(t, "files.js")
	if !strings.Contains(files, `noteBadgeHTML(state.noteCounts.plain_by_commit_path[(f.sha || state.fileSha) + ":" + f.path])`) {
		t.Fatal("files.js: a commit's file badge must count plain_by_commit_path")
	}
	if strings.Contains(files, "noteBadgeHTML(state.noteCounts.by_commit_path[") {
		t.Fatal("files.js: a commit's file badge still counts the range reviews' notes")
	}
	if n := strings.Count(files, `q.set("scoped", "1")`); n != 1 {
		t.Fatalf("files.js: %d scoped=1 sites, want the one in notesFor", n)
	}
	// Armed BEFORE the diff opens, so its first notes read is the scoped one.
	an := staticSrc(t, "allnotes.js")
	if arm, open := strings.Index(an, "armRangeNotes();"), strings.Index(an, "await openFile(i);"); arm < 0 || open < arm {
		t.Fatal("allnotes.js: a commit note must arm the range notes before its diff opens")
	}
	if !strings.Contains(an, "showRangeNotes()") {
		t.Fatal("allnotes.js: a note opened from View all notes must ask for the range notes")
	}
}
