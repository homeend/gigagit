package domain

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/homeend/gigagit/internal/gittest"
	"github.com/homeend/gigagit/internal/model"
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

// endpointPaths's working-tree lane is tracked ∪ untracked − deleted. The
// subtraction is the whole point: `git ls-files` lists the INDEX, so a tracked
// file the user removed from disk still appears there.
func TestEndpointPathsWorkTreeSubtractsDeleted(t *testing.T) {
	t.Parallel()
	dir, svc := newRealRepo(t)
	ctx := context.Background()

	for _, name := range []string{"kept.txt", "dropped.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gittest.Run(t, dir, "add", "kept.txt", "dropped.txt")
	gittest.Run(t, dir, "commit", "-m", "two tracked files")
	if err := os.WriteFile(filepath.Join(dir, "fresh.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// os.Remove, NOT `git rm`: the file must stay in the index and vanish from
	// disk, which is exactly what `ls-files --deleted` reports.
	if err := os.Remove(filepath.Join(dir, "dropped.txt")); err != nil {
		t.Fatal(err)
	}

	set, err := svc.endpointPaths(ctx, model.WorkTreeEndpoint())
	if err != nil {
		t.Fatalf("endpointPaths(worktree): %v", err)
	}
	for _, want := range []string{"README.md", "kept.txt", "fresh.txt"} {
		if !set[want] {
			t.Errorf("working tree must contain %q, got %v", want, set)
		}
	}
	if set["dropped.txt"] {
		t.Errorf("a file removed from disk is not in the working tree, got %v", set)
	}

	// The index still holds it — that is the difference the two lanes encode.
	idx, err := svc.endpointPaths(ctx, model.IndexEndpoint())
	if err != nil {
		t.Fatalf("endpointPaths(index): %v", err)
	}
	if !idx["dropped.txt"] {
		t.Errorf("the index still holds a file only removed from disk, got %v", idx)
	}
	if idx["fresh.txt"] {
		t.Errorf("an untracked file is not in the index, got %v", idx)
	}
}

// A bounded endpoint has no member set to probe: asking for one is a caller bug.
func TestEndpointPathsRefusesABoundedEndpoint(t *testing.T) {
	t.Parallel()
	_, svc := newRealRepo(t)
	pair, err := model.PairEndpoint("abc1234def5678", "abc9999fff0000")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.endpointPaths(context.Background(), pair); err == nil {
		t.Fatal("endpointPaths on a bounded endpoint must error")
	}
}
