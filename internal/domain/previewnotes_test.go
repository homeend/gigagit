package domain

import (
	"context"
	"testing"
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
