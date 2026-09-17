package domain

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/gittest"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/repogate"
)

// EvalEndpoint is the spec's §3.1 rule made executable: a POINT evaluates to
// an unbounded set (every file in the repo), a PAIR to a bounded one (the
// enumerated paths it changed).
func TestEvalEndpointBoundedness(t *testing.T) {
	t.Parallel()
	dir, svc := newRealRepo(t)
	ctx := context.Background()

	head, ok, err := svc.ResolveRev(ctx, "HEAD")
	if err != nil || !ok {
		t.Fatalf("ResolveRev(HEAD): %v ok=%v", err, ok)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gittest.Run(t, dir, "add", "b.txt")
	gittest.Run(t, dir, "commit", "-m", "second")
	second, _, err := svc.ResolveRev(ctx, "HEAD")
	if err != nil {
		t.Fatal(err)
	}

	commit, err := model.CommitEndpoint(second)
	if err != nil {
		t.Fatal(err)
	}
	pair, err := model.PairEndpoint(head, second)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := model.RefEndpoint("main")
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name    string
		ep      model.Endpoint
		bounded bool
		paths   []string
	}{
		{"worktree", model.WorkTreeEndpoint(), false, nil},
		{"index", model.IndexEndpoint(), false, nil},
		{"commit", commit, false, nil},
		{"ref", ref, false, nil},
		{"pair", pair, true, []string{"b.txt"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fs, err := svc.EvalEndpoint(ctx, tc.ep)
			if err != nil {
				t.Fatalf("EvalEndpoint: %v", err)
			}
			if fs.Bounded() != tc.bounded {
				t.Fatalf("Bounded() = %v, want %v", fs.Bounded(), tc.bounded)
			}
			if tc.paths != nil {
				got := fs.Paths()
				if len(got) != len(tc.paths) || got[0] != tc.paths[0] {
					t.Fatalf("Paths() = %v, want %v", got, tc.paths)
				}
			}
			if !tc.bounded && fs.Paths() != nil {
				t.Fatalf("an unbounded set must enumerate nothing, got %v", fs.Paths())
			}
		})
	}
}

// A ref endpoint MUST be resolved to a commit before it leaves EvalEndpoint:
// its CacheTag panics by design (plan 1b ruling R3), so a FileSet that still
// held one would blow up the moment anything keyed a cache on it.
func TestEvalEndpointResolvesARefToACommit(t *testing.T) {
	t.Parallel()
	_, svc := newRealRepo(t)

	ref, err := model.RefEndpoint("main")
	if err != nil {
		t.Fatal(err)
	}
	fs, err := svc.EvalEndpoint(context.Background(), ref)
	if err != nil {
		t.Fatalf("EvalEndpoint: %v", err)
	}
	if fs.Endpoint().Kind() != model.EndpointCommit {
		t.Fatalf("a ref must evaluate to a COMMIT endpoint, got kind %d", fs.Endpoint().Kind())
	}
	// The proof: this would panic if the ref survived.
	if tag := fs.Endpoint().CacheTag(); tag == "main" || tag == "" {
		t.Fatalf("CacheTag = %q — a moving name must never reach the cache key", tag)
	}
}

// An unresolvable ref is an error, not an empty set (spec §6).
func TestEvalEndpointUnknownRef(t *testing.T) {
	t.Parallel()
	_, svc := newRealRepo(t)
	ref, err := model.RefEndpoint("no-such-branch")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.EvalEndpoint(context.Background(), ref); err == nil {
		t.Fatal("an unresolvable ref must error")
	}
}

// A change-set enumerates the files it DELETED, but those have no bytes at the
// pair's newer side — Has is what keeps CompareSets from reading a file that is
// not in the b tree (ruling R6).
func TestEvalEndpointPairMarksDeletedMembersAsHavingNoBytes(t *testing.T) {
	t.Parallel()
	dir, svc := newRealRepo(t)
	ctx := context.Background()

	head, ok, err := svc.ResolveRev(ctx, "HEAD")
	if err != nil || !ok {
		t.Fatalf("ResolveRev(HEAD): %v ok=%v", err, ok)
	}
	gittest.Run(t, dir, "rm", "-q", "README.md")
	gittest.Run(t, dir, "commit", "-m", "drop the readme")
	gone, _, err := svc.ResolveRev(ctx, "HEAD")
	if err != nil {
		t.Fatal(err)
	}

	pair, err := model.PairEndpoint(head, gone)
	if err != nil {
		t.Fatal(err)
	}
	fs, err := svc.EvalEndpoint(ctx, pair)
	if err != nil {
		t.Fatalf("EvalEndpoint: %v", err)
	}
	if got := fs.Paths(); len(got) != 1 || got[0] != "README.md" {
		t.Fatalf("Paths() = %v, want [README.md]", got)
	}
	if fs.Has("README.md") {
		t.Fatal("a member the change-set DELETED has no bytes at the pair's newer side")
	}
	if fs.Endpoint().Kind() != model.EndpointCommit || fs.Endpoint().Hash() != gone {
		t.Fatalf("a pair's byte source is its NEW side, got %+v", fs.Endpoint())
	}
}

// A rename is TWO members of the change-set: the new path (with bytes at b)
// and the old one (gone from b, exactly like a deletion). Enumerating only the
// new path made a rename compare as an addition with no matching deletion.
func TestEvalEndpointPairEnumeratesARenamesOldPath(t *testing.T) {
	t.Parallel()
	dir, svc := newRealRepo(t)
	ctx := context.Background()

	head, ok, err := svc.ResolveRev(ctx, "HEAD")
	if err != nil || !ok {
		t.Fatalf("ResolveRev(HEAD): %v ok=%v", err, ok)
	}
	gittest.Run(t, dir, "mv", "README.md", "DOCS.md")
	gittest.Run(t, dir, "commit", "-m", "rename the readme")
	after, _, err := svc.ResolveRev(ctx, "HEAD")
	if err != nil {
		t.Fatal(err)
	}

	pair, err := model.PairEndpoint(head, after)
	if err != nil {
		t.Fatal(err)
	}
	fs, err := svc.EvalEndpoint(ctx, pair)
	if err != nil {
		t.Fatalf("EvalEndpoint: %v", err)
	}
	got := fs.Paths()
	if len(got) != 2 || got[0] != "DOCS.md" || got[1] != "README.md" {
		t.Fatalf("Paths() = %v, want [DOCS.md README.md] — a rename is two members", got)
	}
	if !fs.Has("DOCS.md") {
		t.Error("the rename's NEW path has bytes at the pair's newer side")
	}
	if fs.Has("README.md") {
		t.Error("the rename's OLD path is gone from the pair's newer side")
	}
}

// An abbreviated sha must be normalized: two abbreviations of one commit are
// two CacheTags, which fragments the compare cache and probes one tree twice.
func TestEvalEndpointNormalizesAShortCommit(t *testing.T) {
	t.Parallel()
	_, svc := newRealRepo(t)
	ctx := context.Background()

	full, ok, err := svc.ResolveRev(ctx, "HEAD")
	if err != nil || !ok {
		t.Fatalf("ResolveRev(HEAD): %v ok=%v", err, ok)
	}
	short, err := model.CommitEndpoint(full[:8])
	if err != nil {
		t.Fatal(err)
	}
	fs, err := svc.EvalEndpoint(ctx, short)
	if err != nil {
		t.Fatalf("EvalEndpoint: %v", err)
	}
	if fs.Endpoint().Hash() != full {
		t.Fatalf("Hash() = %q, want the full sha %q", fs.Endpoint().Hash(), full)
	}
}

// The two invariants the type exists for, both of which a refactor to
// `bounded: len(paths) > 0` would silently break.
func TestFileSetInvariants(t *testing.T) {
	t.Parallel()
	ep, err := model.CommitEndpoint("abc1234def5678")
	if err != nil {
		t.Fatal(err)
	}

	// An EMPTY bounded set is legal and stays bounded: a fully merged branch's
	// three-dot pair has merge-base(target, source) == source (ruling R7).
	empty := boundedSetWith(ep, nil, nil)
	if !empty.Bounded() {
		t.Error("an empty change-set is BOUNDED — it enumerates zero paths, it is not a point")
	}
	if p := empty.Paths(); p == nil || len(p) != 0 {
		t.Errorf("a bounded set's Paths() is non-nil and empty, got %v", p)
	}

	// A nil has map means "every member has bytes" — the shelf lane, where
	// every tar member is readable.
	if !empty.Has("anything.txt") {
		t.Error("a nil has map must read as: every member has bytes")
	}

	// Paths() hands out a copy: mutating it must not reach the set.
	set := boundedSetWith(ep, []string{"b.txt", "a.txt"}, nil)
	got := set.Paths()
	got[0] = "MUTATED"
	if again := set.Paths(); again[0] != "a.txt" {
		t.Errorf("Paths() handed out its backing slice: %v", again)
	}

	// An unbounded set enumerates nothing at all.
	if p := unboundedSet(ep).Paths(); p != nil {
		t.Errorf("an unbounded set's Paths() is nil, got %v", p)
	}
}

// endpointHas answers presence for the keys a BOUNDED side supplies, per kind.
// The worktree lane is the one that must not ask git: see the skip-worktree
// test below.
func TestEndpointHasPerKind(t *testing.T) {
	t.Parallel()
	dir, svc := newRealRepo(t)
	ctx := context.Background()

	if err := os.WriteFile(filepath.Join(dir, "kept.txt"), []byte("k\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gittest.Run(t, dir, "add", "kept.txt")
	gittest.Run(t, dir, "commit", "-m", "kept")
	head, ok, err := svc.ResolveRev(ctx, "HEAD")
	if err != nil || !ok {
		t.Fatalf("ResolveRev(HEAD): %v ok=%v", err, ok)
	}
	if err := os.WriteFile(filepath.Join(dir, "fresh.txt"), []byte("f\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	keys := []string{"README.md", "kept.txt", "fresh.txt", "never.txt"}

	commit, err := model.CommitEndpoint(head)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		ep   model.Endpoint
		want map[string]bool
	}{
		// fresh.txt is untracked: in neither the tree nor the index, but it IS
		// on disk.
		{"commit", commit, map[string]bool{"README.md": true, "kept.txt": true, "fresh.txt": false, "never.txt": false}},
		{"index", model.IndexEndpoint(), map[string]bool{"README.md": true, "kept.txt": true, "fresh.txt": false, "never.txt": false}},
		{"worktree", model.WorkTreeEndpoint(), map[string]bool{"README.md": true, "kept.txt": true, "fresh.txt": true, "never.txt": false}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := svc.endpointHas(ctx, tc.ep, keys)
			if err != nil {
				t.Fatalf("endpointHas: %v", err)
			}
			if len(got) != len(keys) {
				t.Fatalf("endpointHas returned %d entries for %d keys: %v", len(got), len(keys), got)
			}
			for k, want := range tc.want {
				if got[k] != want {
					t.Errorf("%s: has[%q] = %v, want %v (full: %v)", tc.name, k, got[k], want, got)
				}
			}
		})
	}
}

// THE reason the worktree lane stats instead of asking git: `ls-files
// --deleted` does not report a SKIP-WORKTREE entry that is gone from disk, so
// a sparse checkout — this repo's primary deployment — would call every
// sparse-excluded path present, which is the exact failure the probe exists to
// prevent.
func TestEndpointHasWorkTreeSeesASkipWorktreeRemoval(t *testing.T) {
	t.Parallel()
	dir, svc := newRealRepo(t)
	ctx := context.Background()

	if err := os.WriteFile(filepath.Join(dir, "sparse.txt"), []byte("s\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gittest.Run(t, dir, "add", "sparse.txt")
	gittest.Run(t, dir, "commit", "-m", "a file that will be sparse-excluded")

	gittest.Run(t, dir, "update-index", "--skip-worktree", "sparse.txt")
	if err := os.Remove(filepath.Join(dir, "sparse.txt")); err != nil {
		t.Fatal(err)
	}

	// Pin the git behaviour this test defends against, so a future git that
	// changes it shows up here rather than as a silent wrong answer.
	deleted, err := svc.LsFiles(ctx, "sparse.txt")
	if err != nil {
		t.Fatal(err)
	}
	if len(deleted) != 1 {
		t.Fatalf("the index still holds a skip-worktree path: ls-files = %v", deleted)
	}

	got, err := svc.endpointHas(ctx, model.WorkTreeEndpoint(), []string{"sparse.txt", "README.md"})
	if err != nil {
		t.Fatalf("endpointHas: %v", err)
	}
	if got["sparse.txt"] {
		t.Error("a skip-worktree file removed from disk is NOT in the working tree — git's --deleted cannot see it, so the probe must stat")
	}
	if !got["README.md"] {
		t.Error("README.md is on disk")
	}
}

// A plain `rm`'d tracked file is the ordinary case of the same rule.
func TestEndpointHasWorkTreeSeesAPlainRemoval(t *testing.T) {
	t.Parallel()
	dir, svc := newRealRepo(t)

	if err := os.Remove(filepath.Join(dir, "README.md")); err != nil {
		t.Fatal(err)
	}
	got, err := svc.endpointHas(context.Background(), model.WorkTreeEndpoint(), []string{"README.md"})
	if err != nil {
		t.Fatalf("endpointHas: %v", err)
	}
	if got["README.md"] {
		t.Error("a file removed from disk is not in the working tree")
	}
}

// Presence is asked only of the side that is UNBOUNDED; a bounded side answers
// from its own Has map, and a ref must have been resolved away long before.
func TestEndpointHasRefusesBoundedAndUnresolvedKinds(t *testing.T) {
	t.Parallel()
	_, svc := newRealRepo(t)
	ctx := context.Background()

	pair, err := model.PairEndpoint("abc1234def5678", "abc9999fff0000")
	if err != nil {
		t.Fatal(err)
	}
	shelf, err := model.ShelfEndpoint("entry-1")
	if err != nil {
		t.Fatal(err)
	}
	ref, err := model.RefEndpoint("main")
	if err != nil {
		t.Fatal(err)
	}
	for _, ep := range []model.Endpoint{pair, shelf, ref} {
		if _, err := svc.endpointHas(ctx, ep, []string{"a.txt"}); err == nil {
			t.Errorf("endpointHas on kind %d must error", ep.Kind())
		}
	}
	// An empty key set is not an error — it is the empty question.
	got, err := svc.endpointHas(ctx, pair, nil)
	if err != nil || len(got) != 0 {
		t.Errorf("endpointHas(_, nil) = %v, %v; want an empty map and no error", got, err)
	}
}

// The batcher is what keeps a large change-set's pathspec under every OS's
// argv cap; one probe per batch, and the union is the answer.
func TestEndpointHasBatchesALargePathSet(t *testing.T) {
	t.Parallel()
	dir, svc := newRealRepo(t)
	ctx := context.Background()

	const n = pathProbeBatch*2 + 7 // spans three batches, last one partial
	keys := make([]string, 0, n)
	for i := range n {
		name := fmt.Sprintf("f%04d.txt", i)
		keys = append(keys, name)
		if i%2 == 0 { // only half exist, so a batch cannot pass by answering "all"
			if err := os.WriteFile(filepath.Join(dir, name), []byte("x\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	gittest.Run(t, dir, "add", "-A")
	gittest.Run(t, dir, "commit", "-m", "half the files")
	head, ok, err := svc.ResolveRev(ctx, "HEAD")
	if err != nil || !ok {
		t.Fatalf("ResolveRev(HEAD): %v ok=%v", err, ok)
	}
	commit, err := model.CommitEndpoint(head)
	if err != nil {
		t.Fatal(err)
	}

	got, err := svc.endpointHas(ctx, commit, keys)
	if err != nil {
		t.Fatalf("endpointHas: %v", err)
	}
	if len(got) != n {
		t.Fatalf("endpointHas returned %d entries, want %d", len(got), n)
	}
	for i, k := range keys {
		if want := i%2 == 0; got[k] != want {
			t.Fatalf("has[%q] = %v, want %v", k, got[k], want)
		}
	}
}

// endpointHas' WORKING-TREE arm must hand back a map the CALLER owns, exactly
// as its commit and index siblings do. WorktreeFilesPresent goes through
// query(), so two callers that coalesce on one flight used to receive the
// leader's map header — the same object, each believing it owned it.
//
// The flight is held open deterministically rather than by sleeping: the test
// takes an exclusive reservation first, so the LEADER parks inside the gate's
// queue (observable through Gate.Queue) while still holding the flight key,
// and the second caller can only be a follower.
func TestEndpointHasWorktreeResultIsNotShared(t *testing.T) {
	t.Parallel()
	dir, svc := newRealRepo(t)
	writeAndCommit(t, dir, "seed", map[string]string{"here.txt": "x\n"})
	ctx := context.Background()
	paths := []string{"here.txt", "gone.txt"}

	gate := svc.gateFor(ctx)
	hold, err := gate.Acquire(ctx, repogate.TreeWrite, "test: hold the gate open")
	if err != nil {
		t.Fatal(err)
	}

	type res struct {
		m   map[string]bool
		err error
	}
	leader, follower := make(chan res, 1), make(chan res, 1)
	go func() {
		m, err := svc.endpointHas(ctx, model.WorkTreeEndpoint(), paths)
		leader <- res{m, err}
	}()

	// Wait for the leader to be PARKED in the gate queue: at that point it is
	// inside flightGroup.Do's fn and the key is held.
	waiters := func() int {
		n := 0
		for _, e := range gate.Queue() {
			if e.Waiting {
				n++
			}
		}
		return n
	}
	deadline := time.Now().Add(10 * time.Second)
	for waiters() == 0 {
		if time.Now().After(deadline) {
			hold.Release()
			t.Fatal("the leader never reached the gate queue")
		}
		time.Sleep(time.Millisecond)
	}

	go func() {
		m, err := svc.endpointHas(ctx, model.WorkTreeEndpoint(), paths)
		follower <- res{m, err}
	}()
	// A follower joins the leader's flight and never queues; a second QUEUED
	// waiter would mean it started its own flight, and the aliasing this test
	// is about could not arise. Settle briefly, then assert we still see one.
	time.Sleep(50 * time.Millisecond)
	if n := waiters(); n != 1 {
		hold.Release()
		t.Skipf("the second caller did not join the leader's flight (%d waiters)", n)
	}

	hold.Release()
	a, b := <-leader, <-follower
	if a.err != nil || b.err != nil {
		t.Fatalf("endpointHas: %v / %v", a.err, b.err)
	}
	// Both callers were told the map is theirs. Prove it of one by writing to
	// it, which is exactly the accident this guards.
	a.m["here.txt"] = !a.m["here.txt"]
	a.m["a-key-the-probe-never-saw"] = true
	if _, leaked := b.m["a-key-the-probe-never-saw"]; leaked {
		t.Fatal("the two coalescing callers share one map: a write by one is visible to the other")
	}
	if !b.m["here.txt"] {
		t.Fatal("the leader's flipped value leaked into the follower's map")
	}
}
