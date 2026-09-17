package domain

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/homeend/gigagit/internal/gittest"
	"github.com/homeend/gigagit/internal/model"
)

// compareFixture is a repo with three commits:
//
//	c1: a.txt = "one"
//	c2: a.txt = "two",  b.txt = "bee"   (c1..c2 changes a.txt and ADDS b.txt)
//	c3: a.txt = "three", b.txt DELETED  (c2..c3 changes a.txt and DELETES b.txt)
//
// The deletion is load-bearing, not decoration: the pair c2..c3 ENUMERATES
// b.txt while having no bytes for it at c3, which is the case ruling R6
// exists for. A fixture with no deletions lets a comparison that blindly
// reads every member pass.
//
// newRealRepo seeds README.md as well, so every tree here also holds
// README.md — the assertions below never depend on a tree's size, only on the
// BOUNDED side's key set, which is the whole point.
type compareFixture struct {
	dir        string
	svc        *Service
	c1, c2, c3 string
}

func newCompareFixture(t *testing.T) compareFixture {
	t.Helper()
	dir, svc := newRealRepo(t)
	ctx := context.Background()
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	rev := func() string {
		t.Helper()
		sha, ok, err := svc.ResolveRev(ctx, "HEAD")
		if err != nil || !ok {
			t.Fatalf("ResolveRev(HEAD): %v ok=%v", err, ok)
		}
		return sha
	}

	write("a.txt", "one\n")
	gittest.Run(t, dir, "add", "a.txt")
	gittest.Run(t, dir, "commit", "-m", "c1")
	c1 := rev()

	write("a.txt", "two\n")
	write("b.txt", "bee\n")
	gittest.Run(t, dir, "add", "a.txt", "b.txt")
	gittest.Run(t, dir, "commit", "-m", "c2")
	c2 := rev()

	write("a.txt", "three\n")
	gittest.Run(t, dir, "rm", "-f", "b.txt")
	gittest.Run(t, dir, "add", "a.txt")
	gittest.Run(t, dir, "commit", "-m", "c3")
	c3 := rev()

	return compareFixture{dir: dir, svc: svc, c1: c1, c2: c2, c3: c3}
}

func (f compareFixture) eval(t *testing.T, e model.Endpoint) FileSet {
	t.Helper()
	fs, err := f.svc.EvalEndpoint(context.Background(), e)
	if err != nil {
		t.Fatalf("EvalEndpoint: %v", err)
	}
	return fs
}

func statuses(files []model.CommitFile) map[string]string {
	m := make(map[string]string, len(files))
	for _, f := range files {
		m[f.Path] = f.Status
	}
	return m
}

func mustTestCommit(t *testing.T, h string) model.Endpoint {
	t.Helper()
	e, err := model.CommitEndpoint(h)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func mustTestPair(t *testing.T, a, b string) model.Endpoint {
	t.Helper()
	e, err := model.PairEndpoint(a, b)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

// The three lanes of spec §3.5, each asserted on real data, and each asserted
// to yield a BOUNDED result (compare is CLOSED — that closure is what makes
// "compare everything with everything" terminate).
func TestCompareSetsMatrix(t *testing.T) {
	t.Parallel()
	f := newCompareFixture(t)
	ctx := context.Background()

	t.Run("unbounded x unbounded: the differing paths", func(t *testing.T) {
		got, err := f.svc.CompareSets(ctx, f.eval(t, mustTestCommit(t, f.c1)), f.eval(t, mustTestCommit(t, f.c3)))
		if err != nil {
			t.Fatal(err)
		}
		st := statuses(got)
		// b.txt was added at c2 and deleted at c3, so it is absent from BOTH
		// trees and never appears. README.md is identical in both trees, so it
		// is omitted too.
		if st["a.txt"] != "M" || len(st) != 1 {
			t.Fatalf("c1 vs c3 = %v, want exactly a.txt M", st)
		}
	})

	t.Run("bounded x bounded: symmetric over the union", func(t *testing.T) {
		// c1..c2 enumerates {a.txt (M), b.txt (A)}; c2..c3 enumerates
		// {a.txt (M), b.txt (D)}. BOTH sets contain b.txt — but the right set
		// has no BYTES for it (it is the file c3 deleted), which is exactly
		// ruling R6. A comparison that read every member would hard-error
		// here on `git show <c3>:b.txt`.
		got, err := f.svc.CompareSets(ctx, f.eval(t, mustTestPair(t, f.c1, f.c2)), f.eval(t, mustTestPair(t, f.c2, f.c3)))
		if err != nil {
			t.Fatalf("a deleted member must not be read: %v", err)
		}
		st := statuses(got)
		// a.txt is in both sets with bytes on both, differing → M.
		if st["a.txt"] != "M" {
			t.Fatalf("a.txt = %q, want M; full result %v", st["a.txt"], st)
		}
		// b.txt: bytes on the left (it exists at c2), none on the right ⇒ D.
		if st["b.txt"] != "D" {
			t.Fatalf("b.txt = %q, want D (no bytes on the right); full result %v", st["b.txt"], st)
		}
	})

	t.Run("unbounded x bounded: projected onto the bounded side", func(t *testing.T) {
		// The pair c1..c2 is {a.txt, b.txt}. The result must be keyed on the
		// PAIR's paths — never on the whole tree (which also holds README.md).
		got, err := f.svc.CompareSets(ctx, f.eval(t, mustTestCommit(t, f.c1)), f.eval(t, mustTestPair(t, f.c1, f.c2)))
		if err != nil {
			t.Fatal(err)
		}
		st := statuses(got)
		if len(st) != 2 {
			t.Fatalf("the result must be keyed on the BOUNDED side's 2 paths, got %v", st)
		}
		if st["a.txt"] != "M" {
			t.Fatalf("a.txt = %q, want M", st["a.txt"])
		}
		// b.txt does not exist at c1, and the bounded side is the RIGHT one,
		// so its absence on the left reads as ADDED.
		if st["b.txt"] != "A" {
			t.Fatalf("b.txt = %q, want A (absent on the unbounded LEFT)", st["b.txt"])
		}
	})

	t.Run("bounded x unbounded: the direction flips", func(t *testing.T) {
		// The mirror of the previous case. This is the generalization of
		// shelfCommitCompare's shelfIsRight flag: which side is bounded
		// decides whether a missing key reads A or D.
		got, err := f.svc.CompareSets(ctx, f.eval(t, mustTestPair(t, f.c1, f.c2)), f.eval(t, mustTestCommit(t, f.c1)))
		if err != nil {
			t.Fatal(err)
		}
		st := statuses(got)
		if st["b.txt"] != "D" {
			t.Fatalf("b.txt = %q, want D (absent on the unbounded RIGHT)", st["b.txt"])
		}
	})
}

// DISJOINT bounded sets are a result, never an error (spec §6). Disjoint does
// NOT mean empty: every member of one side is absent from the other, so the
// result is all A and D. (Two EMPTY sets are the separate, degenerate case,
// and they do compare to nothing.)
func TestCompareSetsDisjointBoundedIsAResultNotAnError(t *testing.T) {
	t.Parallel()
	f := newCompareFixture(t)
	ctx := context.Background()
	c1 := mustTestCommit(t, f.c1)
	c2 := mustTestCommit(t, f.c2)
	left := boundedSetWith(c1, []string{"a.txt"}, map[string]bool{"a.txt": true})
	right := boundedSetWith(c2, []string{"b.txt"}, map[string]bool{"b.txt": true})

	got, err := f.svc.CompareSets(ctx, left, right)
	if err != nil {
		t.Fatalf("disjoint sets must not error: %v", err)
	}
	st := statuses(got)
	if st["a.txt"] != "D" || st["b.txt"] != "A" || len(st) != 2 {
		t.Fatalf("disjoint sets compare to all A/D, got %v", st)
	}

	empty, err := f.svc.CompareSets(ctx,
		boundedSetWith(c1, nil, nil), boundedSetWith(c1, nil, nil))
	if err != nil {
		t.Fatalf("two empty sets must not error: %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("two empty sets compare to nothing, got %v", empty)
	}
}

// Compare is CLOSED: the result is a bounded set, so it can be an endpoint of
// the next comparison. This is the property that makes "compare everything
// with everything" terminate (spec §3.5).
func TestCompareSetsResultIsBounded(t *testing.T) {
	t.Parallel()
	f := newCompareFixture(t)
	got, err := f.svc.CompareSets(context.Background(),
		f.eval(t, mustTestCommit(t, f.c1)), f.eval(t, mustTestCommit(t, f.c3)))
	if err != nil {
		t.Fatal(err)
	}
	// "Bounded" here means: the caller can enumerate it. A []CommitFile IS
	// the enumeration, so the assertion is that it is finite and complete,
	// which the matrix test already pins. What this test adds is the
	// round trip: feeding the result's paths back in as a bounded set works.
	paths := make([]string, 0, len(got))
	for _, cf := range got {
		paths = append(paths, cf.Path)
	}
	again := boundedSetWith(mustTestCommit(t, f.c3), paths, nil)
	if !again.Bounded() {
		t.Fatal("a comparison result must be re-usable as a bounded set")
	}
}

// A pair endpoint reaching the LIVE patch lane must refuse, not silently
// render `git diff` (index → working tree). livePairSpec's old default arm
// would have done exactly that.
func TestComparePatchRefusesAnUnsupportedPair(t *testing.T) {
	t.Parallel()
	f := newCompareFixture(t)
	pair := mustTestPair(t, f.c1, f.c2)
	if _, err := f.svc.ComparePatch(context.Background(), pair, model.WorkTreeEndpoint()); err == nil {
		t.Fatal("a pair endpoint must not reach the live patch lane silently")
	}
}
