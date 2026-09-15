package domain

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

// TestPreviewNotesGathersTheBranch: main + feat (three commits). The set's tip
// is feat's tip and Commits is exactly the three commits merge-base..feat.
func TestPreviewNotesGathersTheBranch(t *testing.T) {
	svc, repoDir := newPreviewRepo(t) // helper at the bottom of this file
	ctx := context.Background()
	_ = repoDir

	set, err := svc.PreviewNotes(ctx, "feat", "main")
	if err != nil {
		t.Fatal(err)
	}
	if !set.OK() {
		t.Fatalf("want an ok set, got %+v", set)
	}
	if len(set.Commits) != 3 {
		t.Fatalf("want 3 commits on feat, got %d: %v", len(set.Commits), set.Commits)
	}
	if set.Commits[0] != set.Tip {
		t.Fatalf("newest first: Commits[0]=%s Tip=%s", set.Commits[0], set.Tip)
	}
	if set.Source != "feat" || set.Target != "main" {
		t.Fatalf("set must remember its pair, got %+v", set)
	}
}

// Ruling 6: a non-ok state yields an EMPTY set and NO error.
func TestPreviewNotesOnANonOKPairIsEmptyAndSilent(t *testing.T) {
	svc, _ := newPreviewRepo(t)
	ctx := context.Background()

	set, err := svc.PreviewNotes(ctx, "no-such-branch", "main")
	if err != nil {
		t.Fatalf("a missing source must not be an error: %v", err)
	}
	if set.OK() || set.Tip != "" {
		t.Fatalf("want an empty set, got %+v", set)
	}
}

func TestPreviewResolveAcceptsTheThreeDotForm(t *testing.T) {
	svc, _ := newPreviewRepo(t)
	src, tgt, err := svc.PreviewResolve(context.Background(), "main...feat")
	if err != nil {
		t.Fatal(err)
	}
	if src != "feat" || tgt != "main" {
		t.Fatalf("`<target>...<source>`: want source=feat target=main, got %s/%s", src, tgt)
	}
}

func TestPreviewResolveAcceptsASavedIDAndLabel(t *testing.T) {
	svc, _ := newPreviewRepo(t)
	ctx := context.Background()
	p, err := svc.PreviewAdd(ctx, "feat", "main", "login")
	if err != nil {
		t.Fatal(err)
	}
	for _, spec := range []string{p.ID, "login"} {
		src, tgt, err := svc.PreviewResolve(ctx, spec)
		if err != nil {
			t.Fatalf("%s: %v", spec, err)
		}
		if src != "feat" || tgt != "main" {
			t.Fatalf("%s: got %s/%s", spec, src, tgt)
		}
	}
}

// TestPreviewNotesCacheFollowsSummaryCache (spec §1.6): the rev-list cache is
// keyed on the same (srcHash, tgtHash) pair as PreviewSummary, so a new commit
// on source (a new hash pair) is a new cache key — never a stale hit.
func TestPreviewNotesCacheFollowsSummaryCache(t *testing.T) {
	svc, dir := newPreviewRepo(t)
	ctx := context.Background()

	first, err := svc.PreviewNotes(ctx, "feat", "main")
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Commits) != 3 {
		t.Fatalf("want 3 commits before the new commit, got %d: %v", len(first.Commits), first.Commits)
	}

	gitRun(t, dir, "checkout", "feat")
	writeFile(t, dir, "c.txt", "charlie\n")
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "c4 adds c.txt")
	gitRun(t, dir, "checkout", "main")

	second, err := svc.PreviewNotes(ctx, "feat", "main")
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Commits) != 4 {
		t.Fatalf("want 4 commits after the new commit (no stale cache), got %d: %v", len(second.Commits), second.Commits)
	}
	if second.Tip == first.Tip {
		t.Fatalf("want a new tip after the new commit, got the same %s", second.Tip)
	}
	if second.Commits[0] != second.Tip {
		t.Fatalf("newest first: Commits[0]=%s Tip=%s", second.Commits[0], second.Tip)
	}
}

// newPreviewRepo builds a real repo: main with one commit, then feat with
// three commits, the second of which rewrites a line the first added (so a
// note on the first commit goes outdated on the tip).
func newPreviewRepo(t *testing.T) (*Service, string) {
	t.Helper()
	dir, svc := newRealRepo(t) // the existing domain test helper
	run := func(args ...string) {
		t.Helper()
		gitRun(t, dir, args...)
	}
	writeFile(t, dir, "a.txt", "alpha\nbravo\ncharlie\n")
	run("add", ".")
	run("commit", "-m", "seed")
	run("checkout", "-b", "feat")
	writeFile(t, dir, "a.txt", "alpha\nbravo\ncharlie\nDELTA\n")
	run("add", ".")
	run("commit", "-m", "c1 adds DELTA")
	writeFile(t, dir, "a.txt", "alpha\nbravo\ncharlie\nECHO\n")
	run("add", ".")
	run("commit", "-m", "c2 rewrites DELTA")
	writeFile(t, dir, "b.txt", "bee\n")
	run("add", ".")
	run("commit", "-m", "c3 adds b.txt")
	run("checkout", "main")
	return svc, dir
}

// revParse is the test's own rev resolver: full sha for a rev in dir.
func revParse(t *testing.T, dir, rev string) string {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", rev)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("rev-parse %s: %v", rev, err)
	}
	return strings.TrimSpace(string(out))
}

// A note on the FIRST feat commit, on a line the SECOND commit rewrote, must
// still be listed on the preview — as stale (rendered "outdated"). A note on
// a line that survived is active.
func TestPreviewNotesForGathersOlderCommitsAndMarksThemStale(t *testing.T) {
	svc, dir := newPreviewRepo(t)
	ctx := context.Background()
	c1 := revParse(t, dir, "feat~2") // "c1 adds DELTA"

	// On c1, line 4 is "DELTA"; c2 rewrote it to "ECHO" → stale on the tip.
	stale, err := svc.NoteAdd(ctx, model.Note{
		Source: model.NoteSourceAgent, Author: "ada",
		Address: model.FileAddress{State: model.StateCommitted, Commit: c1, Path: "a.txt"},
		Side:    model.NoteSideNew, Range: [2]int{4, 4}, Summary: "why DELTA",
	})
	if err != nil {
		t.Fatal(err)
	}
	// On c1, line 1 is "alpha" and still is on the tip → active.
	live, err := svc.NoteAdd(ctx, model.Note{
		Source: model.NoteSourceAgent, Author: "ada",
		Address: model.FileAddress{State: model.StateCommitted, Commit: c1, Path: "a.txt"},
		Side:    model.NoteSideNew, Range: [2]int{1, 1}, Summary: "alpha stands",
	})
	if err != nil {
		t.Fatal(err)
	}

	set, err := svc.PreviewNotes(ctx, "feat", "main")
	if err != nil {
		t.Fatal(err)
	}
	got, err := svc.PreviewNotesAt(ctx, set, "a.txt")
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]ResolvedNote{}
	for _, r := range got {
		byID[r.Note.ID] = r
	}
	if len(byID) != 2 {
		t.Fatalf("want both notes gathered from the older commit, got %d", len(byID))
	}
	if s := byID[stale.ID].Status; s != model.NoteStale {
		t.Fatalf("a note on a rewritten line must be stale, got %q", s)
	}
	if s := byID[live.ID].Status; s != model.NoteActive {
		t.Fatalf("a note on a surviving line must be active, got %q", s)
	}
	if w := PreviewStatus(model.NoteStale); w != "outdated" {
		t.Fatalf("the preview word for stale is outdated, got %q", w)
	}
}

// Controller ruling (spec amended): a note whose commit is no longer in
// base..tip (branch rebased) CANNOT be attributed to this preview and is NOT
// counted. Only a path-gone note — its commit stays on the branch, but a
// later commit removed the path from the tip — is hidden from the listing
// yet still counted (spec §1.2, the "retired file" rule).
func TestPreviewNoteCountsIncludeHiddenNotes(t *testing.T) {
	svc, dir := newPreviewRepo(t)
	ctx := context.Background()
	c3 := revParse(t, dir, "feat") // "c3 adds b.txt", tip before the removal
	if _, err := svc.NoteAdd(ctx, model.Note{
		Source: model.NoteSourceAgent, Author: "ada",
		Address: model.FileAddress{State: model.StateCommitted, Commit: c3, Path: "b.txt"},
		Side:    model.NoteSideNew, Range: [2]int{1, 1}, Summary: "on a file the tip drops",
	}); err != nil {
		t.Fatal(err)
	}
	// A later commit on feat removes b.txt: the note's commit STAYS on the
	// branch, but the tip no longer has the path.
	gitRun(t, dir, "checkout", "feat")
	gitRun(t, dir, "rm", "b.txt")
	gitRun(t, dir, "commit", "-m", "c4 removes b.txt")
	gitRun(t, dir, "checkout", "main")

	// A note on a commit that is NOT on feat at all (main's seed commit) —
	// rebased away / never on the branch — must not be attributed.
	seed := revParse(t, dir, "main")
	offBranch, err := svc.NoteAdd(ctx, model.Note{
		Source: model.NoteSourceAgent, Author: "ada",
		Address: model.FileAddress{State: model.StateCommitted, Commit: seed, Path: "a.txt"},
		Side:    model.NoteSideNew, Range: [2]int{1, 1}, Summary: "off the branch",
	})
	if err != nil {
		t.Fatal(err)
	}

	set, err := svc.PreviewNotes(ctx, "feat", "main")
	if err != nil {
		t.Fatal(err)
	}
	byPath, total, err := svc.PreviewNoteCounts(ctx, set)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || byPath["b.txt"] != 1 {
		t.Fatalf("the path-gone note counts, the off-branch note does not: total=%d byPath=%v", total, byPath)
	}

	// The counted half is only half the rule: the path-gone note must also be
	// HIDDEN from the listing (its file is gone from the tip, so nothing can
	// anchor it), and the rebased-away note must never appear at all — on
	// ANY path, since it is not even gathered.
	gone, err := svc.PreviewNotesAt(ctx, set, "b.txt")
	if err != nil {
		t.Fatal(err)
	}
	if len(gone) != 0 {
		t.Fatalf("a path-gone note must be hidden from the listing, got %d: %+v", len(gone), gone)
	}
	onA, err := svc.PreviewNotesAt(ctx, set, "a.txt")
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range onA {
		if r.Note.ID == offBranch.ID {
			t.Fatalf("a rebased-away commit's note must never be listed, got %+v", r)
		}
	}
}

// SetNotesStore points a Service at a different store (the brief predicted
// this): a preview count cached against the OLD store must not survive.
func TestSetNotesStoreClearsPreviewCounts(t *testing.T) {
	svc, dir := newPreviewRepo(t)
	ctx := context.Background()
	set, err := svc.PreviewNotes(ctx, "feat", "main")
	if err != nil {
		t.Fatal(err)
	}
	if _, total, err := svc.PreviewNoteCounts(ctx, set); err != nil || total != 0 {
		t.Fatalf("cold counts: total=%d err=%v", total, err)
	}
	svc.UseNotesDir(t.TempDir())
	if _, err := svc.NoteAdd(ctx, model.Note{
		Source: model.NoteSourceAgent, Author: "ada",
		Address: model.FileAddress{State: model.StateCommitted, Commit: revParse(t, dir, "feat"), Path: "b.txt"},
		Side:    model.NoteSideNew, Range: [2]int{1, 1}, Summary: "in the new store",
	}); err != nil {
		t.Fatal(err)
	}
	_, total, err := svc.PreviewNoteCounts(ctx, set)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 {
		t.Fatalf("SetNotesStore must drop the cached preview counts, got total=%d", total)
	}
}

// Ruling 7: the counts follow the notes store, not only the summary cache.
func TestPreviewNoteCountsInvalidateOnAMutation(t *testing.T) {
	svc, dir := newPreviewRepo(t)
	ctx := context.Background()
	set, err := svc.PreviewNotes(ctx, "feat", "main")
	if err != nil {
		t.Fatal(err)
	}
	if _, total, err := svc.PreviewNoteCounts(ctx, set); err != nil || total != 0 {
		t.Fatalf("cold counts: total=%d err=%v", total, err)
	}
	if _, err := svc.NoteAdd(ctx, model.Note{
		Source: model.NoteSourceAgent, Author: "ada",
		Address: model.FileAddress{State: model.StateCommitted, Commit: revParse(t, dir, "feat"), Path: "b.txt"},
		Side:    model.NoteSideNew, Range: [2]int{1, 1}, Summary: "fresh",
	}); err != nil {
		t.Fatal(err)
	}
	_, total, err := svc.PreviewNoteCounts(ctx, set)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 {
		t.Fatalf("a note mutation must drop the cached preview counts, got total=%d", total)
	}
}

// Ruling 6: a non-ok set answers empty, silently.
func TestPreviewNotesOnAnEmptySetAreSilent(t *testing.T) {
	svc, _ := newPreviewRepo(t)
	ctx := context.Background()
	got, err := svc.PreviewNotesAt(ctx, PreviewNoteSet{}, "a.txt")
	if err != nil || got != nil {
		t.Fatalf("want (nil, nil), got (%v, %v)", got, err)
	}
	byPath, total, err := svc.PreviewNoteCounts(ctx, PreviewNoteSet{})
	if err != nil || total != 0 || len(byPath) != 0 {
		t.Fatalf("want empty counts and no error, got (%v, %d, %v)", byPath, total, err)
	}
}

// PreviewHunkAnchor numbers hunks over the PREVIEW's patch (merge-base → tip),
// not over the tip commit's own parent→tip diff (ruling 1). The tip commit
// (c3) only adds b.txt, so its own patch has NO hunk for a.txt at all: an
// anchor that resolves for a.txt can only have come from the preview patch.
func TestPreviewHunkAnchorUsesThePreviewPatch(t *testing.T) {
	svc, _ := newPreviewRepo(t)
	ctx := context.Background()
	set, err := svc.PreviewNotes(ctx, "feat", "main")
	if err != nil {
		t.Fatal(err)
	}
	side, rng, err := svc.PreviewHunkAnchor(ctx, set, "a.txt", 1)
	if err != nil {
		t.Fatal(err)
	}
	if side != model.NoteSideNew {
		t.Fatalf("a preview anchors on the new side, got %s", side)
	}
	if rng[1] < 4 {
		t.Fatalf("the range must come from the preview patch (reaching ECHO on line 4), got %v", rng)
	}
}

// A hunk that only deletes has no new side to anchor on: refused, not silently
// stored as an old-side note the preview could never show.
func TestPreviewHunkAnchorRefusesADeleteOnlyHunk(t *testing.T) {
	svc, dir := newDeletedFilePreviewRepo(t)
	ctx := context.Background()
	set, err := svc.PreviewNotes(ctx, "feat", "main")
	if err != nil {
		t.Fatal(err)
	}
	_ = dir
	if _, _, err := svc.PreviewHunkAnchor(ctx, set, "c.txt", 1); !errors.Is(err, ErrPreviewOldSide) {
		t.Fatalf("want ErrPreviewOldSide, got %v", err)
	}
}

// newDeletedFilePreviewRepo builds main (c.txt) and a feat branch that DELETES
// it — the one shape that yields a hunk with no new side at all (@@ -1,4 +0,0 @@).
func newDeletedFilePreviewRepo(t *testing.T) (*Service, string) {
	t.Helper()
	dir, svc := newRealRepo(t)
	writeFile(t, dir, "c.txt", "l1\nl2\nl3\nl4\n")
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "seed c.txt")
	gitRun(t, dir, "checkout", "-b", "feat")
	gitRun(t, dir, "rm", "-q", "c.txt")
	gitRun(t, dir, "commit", "-m", "feat drops c.txt")
	gitRun(t, dir, "checkout", "main")
	return svc, dir
}
