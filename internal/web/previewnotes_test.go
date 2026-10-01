package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

// newPreviewServer builds main + a three-commit feat branch — the same shape
// as domain's newPreviewRepo (previewnotes_test.go): c1 adds DELTA (a.txt
// line 4), c2 rewrites that line to ECHO, c3 adds b.txt — with one note on
// feat~2 (c1), on the line c2 rewrites, so the preview reports it
// "outdated". The pair is also saved as a record so /api/preview lists it.
//
// No isolateState/XDG_STATE_HOME here: both stores this fixture touches are
// injected directly (UsePreviewsDir/UseNotesDir), the same posture
// notesServer (notes_test.go) documents as "parallel-safe" — so the tests
// built on this fixture call t.Parallel().
func newPreviewServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	dir := newRepoDir(t, 1)
	gitRun(t, dir, "checkout", "-q", "-b", "feat")
	write := func(s string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
		gitRun(t, dir, "add", "-A")
	}
	write("alpha\nbravo\ncharlie\nDELTA\n")
	gitRun(t, dir, "commit", "-m", "c1 adds DELTA")
	c1 := gitRun(t, dir, "rev-parse", "HEAD")
	write("alpha\nbravo\ncharlie\nECHO\n")
	gitRun(t, dir, "commit", "-m", "c2 rewrites DELTA")
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("bee\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "c3 adds b.txt")
	gitRun(t, dir, "checkout", "-q", "main")

	svc := domain.Open(dir)
	svc.UsePreviewsDir(t.TempDir())
	svc.UseNotesDir(t.TempDir())
	ctx := context.Background()
	if _, err := svc.NoteAdd(ctx, model.Note{
		Source: model.NoteSourceAgent, Author: "ada",
		Address: model.FileAddress{State: model.StateCommitted, Commit: c1, Path: "a.txt"},
		Side:    model.NoteSideNew, Range: [2]int{4, 4}, Summary: "why DELTA",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PreviewAdd(ctx, "feat", "main", "login"); err != nil {
		t.Fatal(err)
	}
	return serve(t, New(svc)), dir
}

func TestPreviewNotesEndpointReturnsTheGatheredSet(t *testing.T) {
	t.Parallel()
	ts, dir := newPreviewServer(t)
	_ = dir
	var body struct {
		Notes  []struct{ Status, Summary string } `json:"notes"`
		Tip    string                             `json:"tip"`
		Counts map[string]int                     `json:"counts"`
		Total  int                                `json:"total"`
	}
	if code := getJSON(t, ts, "/api/preview/notes?source=feat&target=main&path=a.txt", &body); code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	if len(body.Notes) != 1 {
		t.Fatalf("want the gathered note, got %d: %+v", len(body.Notes), body.Notes)
	}
	if body.Notes[0].Status != "outdated" {
		t.Fatalf("a note on a rewritten line is outdated, got %q", body.Notes[0].Status)
	}
	if len(body.Tip) != 40 {
		t.Fatalf("the payload must name the write target, got %q", body.Tip)
	}
	if body.Counts["a.txt"] != 1 || body.Total != 1 {
		t.Fatalf("counts/total must come from PreviewNoteCounts, got counts=%v total=%d", body.Counts, body.Total)
	}
}

// path="" is the counts-only form (the preview-open badge fetch): the
// response must still carry counts/total/tip, but `notes` must be an empty
// ARRAY, not null — PreviewNotesAt is never called, so a client that always
// does `for (const n of d.notes)` never sees a null crash it.
func TestPreviewNotesWithoutAPathIsCountsOnly(t *testing.T) {
	t.Parallel()
	ts, _ := newPreviewServer(t)
	var raw struct {
		Notes  json.RawMessage `json:"notes"`
		Tip    string          `json:"tip"`
		Counts map[string]int  `json:"counts"`
		Total  int             `json:"total"`
	}
	if code := getJSON(t, ts, "/api/preview/notes?source=feat&target=main", &raw); code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	if string(raw.Notes) != "[]" {
		t.Fatalf("notes must be an empty array (not null) on the counts-only form, got %s", raw.Notes)
	}
	if raw.Counts["a.txt"] != 1 || raw.Total != 1 {
		t.Fatalf("counts/total must still be filled, got counts=%v total=%d", raw.Counts, raw.Total)
	}
	if len(raw.Tip) != 40 {
		t.Fatalf("tip must still name the write target, got %q", raw.Tip)
	}
}

// An unknown branch is a 404, not an empty preview (the /api/compare posture).
func TestPreviewNotesRefusesAnUnknownBranch(t *testing.T) {
	t.Parallel()
	ts, _ := newPreviewServer(t)
	if code := getJSON(t, ts, "/api/preview/notes?source=nope&target=main&path=a.txt", nil); code != http.StatusNotFound {
		t.Fatalf("want 404, got %d", code)
	}
}

// A leading-dash name is git-argv-unsafe: reject it as a 400 before it ever
// reaches knownRefName's branch lookup. (The unknown-branch 404 above is also
// satisfied by a plain missing route, so this is the check that actually
// discriminates the validation branch.)
func TestPreviewNotesRejectsALeadingDashSource(t *testing.T) {
	t.Parallel()
	ts, _ := newPreviewServer(t)
	if code := getJSON(t, ts, "/api/preview/notes?source=-x&target=main&path=a.txt", nil); code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", code)
	}
}

func TestPreviewRowsCarryTheNoteTotal(t *testing.T) {
	t.Parallel()
	ts, _ := newPreviewServer(t) // the fixture also saves the pair as a record
	var body struct {
		Entries []struct {
			Notes int `json:"notes"`
		} `json:"entries"`
	}
	if code := getJSON(t, ts, "/api/preview", &body); code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	if len(body.Entries) != 1 || body.Entries[0].Notes != 1 {
		t.Fatalf("want notes:1 on the row, got %+v", body.Entries)
	}
}

// A note the page adds in a preview names it ("preview": target...source):
// the server stamps it only when that preview's tip is the note's commit, so
// a page cannot attach a preview the note was not written in.
func TestNoteAddInAPreviewRecordsThePreview(t *testing.T) {
	t.Parallel()
	ts, dir := newPreviewServer(t)
	tip := gitRun(t, dir, "rev-parse", "feat")
	older := gitRun(t, dir, "rev-parse", "feat~1") // on the branch, not its tip
	add := func(rev, preview string) string {
		t.Helper()
		code, b := postJSONRaw(t, ts, "/api/notes/add",
			`{"path":"a.txt","rev":"`+rev+`","state":"commit","side":"new","line":1,"summary":"s","preview":"`+preview+`"}`)
		if code != http.StatusOK {
			t.Fatalf("POST = %d (%v)", code, b)
		}
		return b["id"]
	}
	var got struct {
		Notes []struct {
			ID, Preview string
		} `json:"notes"`
	}
	stamped, foreign := add(tip, "main...feat"), add(older, "main...feat")
	if code := getJSON(t, ts, "/api/notes?path=a.txt&rev="+tip+"&state=commit", &got); code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	if len(got.Notes) != 1 || got.Notes[0].ID != stamped || got.Notes[0].Preview != "main...feat" {
		t.Fatalf("tip notes = %+v, want %s with preview main...feat", got.Notes, stamped)
	}
	got.Notes = nil
	if code := getJSON(t, ts, "/api/notes?path=a.txt&rev="+older+"&state=commit", &got); code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	if len(got.Notes) != 1 || got.Notes[0].ID != foreign || got.Notes[0].Preview != "" {
		t.Fatalf("a note off the preview's tip must not be stamped: %+v", got.Notes)
	}
}
