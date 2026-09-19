package domain

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

// pairNoteOn stores one new-side (or old-side) note for the pair tests.
func pairNoteOn(t *testing.T, svc *Service, commit, path string, side model.NoteSide, summary string) {
	t.Helper()
	if _, err := svc.NoteAdd(context.Background(), model.Note{
		Source: model.NoteSourceAgent, Author: "ada",
		Address: model.FileAddress{State: model.StateCommitted, Commit: commit, Path: path},
		Side:    side, Range: [2]int{1, 1}, Summary: summary,
	}); err != nil {
		t.Fatal(err)
	}
}

// ONE fixture, two scopes: the branch pair (feat → main) and the commit pair
// (merge-base, feat tip) cover the very same commits — and must still
// disagree about what they ARE, because every link and steer target built
// from them differs.
func TestPairNotesMatchesThePreviewSetButIsAPair(t *testing.T) {
	t.Parallel()
	svc, dir := newPreviewRepo(t)
	ctx := context.Background()
	a, b := revParse(t, dir, "main"), revParse(t, dir, "feat")

	pair, err := svc.PairNotes(ctx, a, b)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := svc.PreviewNotes(ctx, "feat", "main")
	if err != nil {
		t.Fatal(err)
	}
	if pair.Tip != preview.Tip || pair.Base != preview.Base || !reflect.DeepEqual(pair.Commits, preview.Commits) {
		t.Fatalf("same commits expected:\npair    %+v\npreview %+v", pair, preview)
	}
	if !pair.IsPair() || preview.IsPair() {
		t.Fatalf("IsPair: pair=%v preview=%v", pair.IsPair(), preview.IsPair())
	}
	if pair.Source != "" || pair.Target != "" {
		t.Fatalf("a pair has no names: %+v", pair)
	}
	if got := pair.DiffSpec().Rev; got != a+".."+b {
		t.Fatalf("DiffSpec = %q", got)
	}
	if (PreviewNoteSet{}).IsPair() {
		t.Fatal("the zero set is not a pair")
	}
}

func TestPairNotesAcceptsNamesAndFreezesThem(t *testing.T) {
	t.Parallel()
	svc, dir := newPreviewRepo(t)
	set, err := svc.PairNotes(context.Background(), "main", "feat")
	if err != nil {
		t.Fatal(err)
	}
	if set.Tip != revParse(t, dir, "feat") || set.Base != revParse(t, dir, "main") {
		t.Fatalf("names must resolve to full shas: %+v", set)
	}
}

// A REVERSED pair (the newer commit on the old side — what the Previews tab's
// swap saves): a..b is empty, yet b is the write target and its notes must be
// gathered.
func TestPairNotesReversedPairStillHoldsItsTip(t *testing.T) {
	t.Parallel()
	svc, dir := newPreviewRepo(t)
	ctx := context.Background()
	older, newer := revParse(t, dir, "main"), revParse(t, dir, "feat")
	pairNoteOn(t, svc, older, "a.txt", model.NoteSideNew, "on the reversed pair's tip")

	set, err := svc.PairNotes(ctx, newer, older)
	if err != nil {
		t.Fatal(err)
	}
	if !set.OK() || !reflect.DeepEqual(set.Commits, []string{older}) {
		t.Fatalf("Commits must hold the tip alone, got %+v", set)
	}
	_, total, err := svc.PreviewNoteCounts(ctx, set)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 {
		t.Fatalf("the note on the tip must count, total=%d", total)
	}
}

func TestPairNotesMissingCommitIsEmptyAndSilent(t *testing.T) {
	t.Parallel()
	svc, dir := newPreviewRepo(t)
	gone := strings.Repeat("0123456789", 4)
	for _, p := range [][2]string{{gone, revParse(t, dir, "feat")}, {revParse(t, dir, "main"), gone}} {
		set, err := svc.PairNotes(context.Background(), p[0], p[1])
		if err != nil {
			t.Fatalf("a missing commit must not be an error: %v", err)
		}
		if set.OK() {
			t.Fatalf("want the zero set, got %+v", set)
		}
	}
}

func TestPairNotesGathersNewSideOnly(t *testing.T) {
	t.Parallel()
	svc, dir := newPreviewRepo(t)
	ctx := context.Background()
	a, b := revParse(t, dir, "main"), revParse(t, dir, "feat")
	mid := revParse(t, dir, "feat~1")
	pairNoteOn(t, svc, b, "b.txt", model.NoteSideNew, "on B")
	pairNoteOn(t, svc, mid, "a.txt", model.NoteSideNew, "on the middle commit")
	pairNoteOn(t, svc, b, "a.txt", model.NoteSideOld, "old side of B")
	pairNoteOn(t, svc, a, "a.txt", model.NoteSideNew, "on A")

	set, err := svc.PairNotes(ctx, a, b)
	if err != nil {
		t.Fatal(err)
	}
	got, err := svc.PreviewNotesAll(ctx, set)
	if err != nil {
		t.Fatal(err)
	}
	var seen []string
	for _, p := range PreviewNotePaths(got) {
		for _, r := range got[p] {
			seen = append(seen, r.Note.Summary)
		}
	}
	want := []string{"on the middle commit", "on B"}
	if !reflect.DeepEqual(seen, want) {
		t.Fatalf("gathered %v, want %v", seen, want)
	}
}

func TestForgeNotesIgnoreAPairSet(t *testing.T) {
	t.Parallel()
	svc, dir := newPreviewRepo(t)
	set, err := svc.PairNotes(context.Background(), revParse(t, dir, "main"), revParse(t, dir, "feat"))
	if err != nil {
		t.Fatal(err)
	}
	if got := svc.forgeNotesFor(set, ""); len(got) != 0 {
		t.Fatalf("a commit pair is no pull request: %v", got)
	}
}

func TestNoteScopeResolve(t *testing.T) {
	t.Parallel()
	svc, dir := newPreviewRepo(t)
	ctx := context.Background()
	a, b := revParse(t, dir, "main"), revParse(t, dir, "feat")
	pv, err := svc.PreviewAdd(ctx, "feat", "main", "shared")
	if err != nil {
		t.Fatal(err)
	}
	pr, err := svc.PairAdd(ctx, "main", "feat", "attempt")
	if err != nil {
		t.Fatal(err)
	}
	// A pair whose label collides with the preview's: the preview wins, the
	// order `gg preview show/rm/rename` already follow.
	shadow, err := svc.PairAdd(ctx, "main", "feat~1", "shared")
	if err != nil {
		t.Fatal(err)
	}

	for _, c := range []struct {
		spec   string
		isPair bool
		tip    string
	}{
		{"main...feat", false, b},
		{"main..feat", true, b},
		{a + ".." + b, true, b},
		{pv.ID, false, b},
		{"shared", false, b},
		{pr.ID, true, b},
		{"attempt", true, b},
		{shadow.ID, true, revParse(t, dir, "feat~1")},
	} {
		set, err := svc.NoteScopeResolve(ctx, c.spec)
		if err != nil {
			t.Fatalf("%q: %v", c.spec, err)
		}
		if !set.OK() || set.IsPair() != c.isPair || set.Tip != c.tip {
			t.Fatalf("%q: got %+v, want pair=%v tip=%s", c.spec, set, c.isPair, c.tip)
		}
	}
}

func TestNoteScopeResolveRefusals(t *testing.T) {
	t.Parallel()
	svc, _ := newPreviewRepo(t)
	ctx := context.Background()
	gone := strings.Repeat("0123456789", 4)

	if _, err := svc.NoteScopeResolve(ctx, "nope"); !errors.Is(err, ErrPreviewNotFound) {
		t.Fatalf("unknown id: err = %v, want ErrPreviewNotFound", err)
	}
	for _, spec := range []string{"...feat", "main...", "..feat", "main.."} {
		if _, err := svc.NoteScopeResolve(ctx, spec); !errors.Is(err, errPreviewPairShape) {
			t.Fatalf("%q: err = %v, want errPreviewPairShape", spec, err)
		}
	}
	for spec, want := range map[string]string{
		"main...gone":   "preview: missing: gone",
		"main.." + gone: "preview: missing commit: " + gone,
		gone + "..main": "preview: missing commit: " + gone,
		"feat...feat":   "preview: feat → feat: ",
	} {
		_, err := svc.NoteScopeResolve(ctx, spec)
		if err == nil || !strings.HasPrefix(err.Error(), want) {
			t.Fatalf("%q: err = %v, want prefix %q", spec, err, want)
		}
	}
}
