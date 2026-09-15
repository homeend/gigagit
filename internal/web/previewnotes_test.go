package web

import (
	"context"
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
func newPreviewServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	isolateState(t)
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

// An unknown branch is a 404, not an empty preview (the /api/compare posture).
func TestPreviewNotesRefusesAnUnknownBranch(t *testing.T) {
	ts, _ := newPreviewServer(t)
	if code := getJSON(t, ts, "/api/preview/notes?source=nope&target=main&path=a.txt", nil); code != http.StatusNotFound {
		t.Fatalf("want 404, got %d", code)
	}
}

func TestPreviewRowsCarryTheNoteTotal(t *testing.T) {
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
