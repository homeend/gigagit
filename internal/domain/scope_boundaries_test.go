package domain

import (
	"context"
	"os/exec"
	"strings"
	"testing"
)

// The bug this query exists for: feat-a adds one commit off main, then main
// MOVES ON past the fork. Main's tip is outside feat-a's history, so no ref
// decoration marks the fork commit — only merge-base can find it.
func TestScopeBoundariesFindsForkOfDivergedBase(t *testing.T) {
	t.Parallel()
	dir, svc := newRealRepo(t)
	ctx := context.Background()

	writeIn(t, dir, "f", "m1")
	gitIn(t, dir, "add", "f")
	gitIn(t, dir, "commit", "-m", "main 1")
	writeIn(t, dir, "f", "m2")
	gitIn(t, dir, "commit", "-am", "main 2")
	fork := revIn(t, dir, "HEAD") // main 2 = the fork commit
	gitIn(t, dir, "checkout", "-b", "feat-a")
	writeIn(t, dir, "g", "a1")
	gitIn(t, dir, "add", "g")
	gitIn(t, dir, "commit", "-m", "a 1")
	gitIn(t, dir, "checkout", "main")
	writeIn(t, dir, "f", "m3")
	gitIn(t, dir, "commit", "-am", "main 3") // main diverges past the fork
	gitIn(t, dir, "checkout", "feat-a")

	got, err := svc.ScopeBoundaries(ctx, []string{"feat-a"}, []string{"main"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != fork {
		t.Fatalf("boundaries = %v, want [%s]", got, fork)
	}
	if len(got[0]) != 40 {
		t.Fatalf("boundary must be a full sha for hash-set matching, got %q", got[0])
	}
}

// A base strictly behind the scope: merge-base(A, B) = B's tip — the same
// commit the decoration path already marks, so no behavior change there.
func TestScopeBoundariesBehindBaseIsItsTip(t *testing.T) {
	t.Parallel()
	dir, svc := newRealRepo(t)
	ctx := context.Background()

	writeIn(t, dir, "f", "m1")
	gitIn(t, dir, "add", "f")
	gitIn(t, dir, "commit", "-m", "main 1")
	tip := revIn(t, dir, "HEAD")
	gitIn(t, dir, "checkout", "-b", "feat-a")
	writeIn(t, dir, "g", "a1")
	gitIn(t, dir, "add", "g")
	gitIn(t, dir, "commit", "-m", "a 1")

	got, err := svc.ScopeBoundaries(ctx, []string{"feat-a"}, []string{"main"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != tip {
		t.Fatalf("boundaries = %v, want [%s]", got, tip)
	}
}

// Unrelated histories (no common ancestor) and bad refs are skipped, never an
// error: boundaries are best-effort decoration for a view.
func TestScopeBoundariesSkipsUnrelatedAndBadRefs(t *testing.T) {
	t.Parallel()
	dir, svc := newRealRepo(t)
	ctx := context.Background()

	writeIn(t, dir, "f", "m1")
	gitIn(t, dir, "add", "f")
	gitIn(t, dir, "commit", "-m", "main 1")
	gitIn(t, dir, "checkout", "--orphan", "island")
	gitIn(t, dir, "rm", "-rf", ".")
	writeIn(t, dir, "i", "i1")
	gitIn(t, dir, "add", "i")
	gitIn(t, dir, "commit", "-m", "island 1")
	gitIn(t, dir, "checkout", "main")

	got, err := svc.ScopeBoundaries(ctx, []string{"main"}, []string{"island", "no-such-branch"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("boundaries = %v, want none", got)
	}
}

// Two branches forked at the same commit dedupe to one boundary hash.
func TestScopeBoundariesDedupes(t *testing.T) {
	t.Parallel()
	dir, svc := newRealRepo(t)
	ctx := context.Background()

	writeIn(t, dir, "f", "m1")
	gitIn(t, dir, "add", "f")
	gitIn(t, dir, "commit", "-m", "main 1")
	fork := revIn(t, dir, "HEAD")
	gitIn(t, dir, "branch", "b1")
	gitIn(t, dir, "branch", "b2")
	gitIn(t, dir, "checkout", "-b", "feat-a")
	writeIn(t, dir, "g", "a1")
	gitIn(t, dir, "add", "g")
	gitIn(t, dir, "commit", "-m", "a 1")

	got, err := svc.ScopeBoundaries(ctx, []string{"feat-a"}, []string{"b1", "b2"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != fork {
		t.Fatalf("boundaries = %v, want deduped [%s]", got, fork)
	}
}

// revIn resolves a rev to its full sha in dir.
func revIn(t *testing.T, dir, rev string) string {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", rev)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git rev-parse %s: %v", rev, err)
	}
	return strings.TrimSpace(string(out))
}
