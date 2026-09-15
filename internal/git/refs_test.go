package git

import (
	"context"
	"fmt"
	"testing"
)

func TestRefVerbs(t *testing.T) {
	t.Parallel()
	_, runner := newTestRepo(t)
	r := &Repo{Runner: runner}
	head, err := r.RevParse(context.Background(), "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	ref := "refs/gg/versions/main/1753100000-merge"
	if err := r.UpdateRef(ctx, ref, head); err != nil {
		t.Fatal(err)
	}
	infos, err := r.ForEachRef(ctx, "refs/gg/versions")
	if err != nil || len(infos) != 1 {
		t.Fatalf("ForEachRef = %v, %v; want 1 row", infos, err)
	}
	if infos[0].Ref != ref || infos[0].Hash != head || infos[0].Subject == "" {
		t.Fatalf("row = %+v", infos[0])
	}
	if err := r.DeleteRef(ctx, ref); err != nil {
		t.Fatal(err)
	}
	if infos, _ = r.ForEachRef(ctx, "refs/gg/versions"); len(infos) != 0 {
		t.Fatalf("after delete: %v", infos)
	}
}

// TestVersionRefsUnwrapsGoodSynthetic writes a real synthetic snapshot
// (WriteVersionSnapshot) and asserts VersionRefs unwraps it: Hash is the
// snapshotted tip, not the wrapper commit, and the endpoints round-trip.
func TestVersionRefsUnwrapsGoodSynthetic(t *testing.T) {
	t.Parallel()
	dir, runner := newTestRepo(t)
	r := &Repo{Runner: runner}
	ctx := context.Background()

	tip := revParse(t, dir, "HEAD")
	m := VersionMeta{Op: "rebase", Ours: tip, Other: tip, Base: tip, Source: "feat/x", Target: "main"}
	syn, err := r.WriteVersionSnapshot(ctx, tip, m, 1700000000)
	if err != nil {
		t.Fatalf("WriteVersionSnapshot: %v", err)
	}
	ref := VersionRef("main", "rebase", 1700000000)
	if err := r.UpdateRef(ctx, ref, syn); err != nil {
		t.Fatalf("UpdateRef: %v", err)
	}

	vs, err := r.VersionRefs(ctx, "refs/gg/versions/main")
	if err != nil {
		t.Fatalf("VersionRefs: %v", err)
	}
	if len(vs) != 1 {
		t.Fatalf("got %d rows, want 1: %+v", len(vs), vs)
	}
	v := vs[0]
	if v.Hash != tip {
		t.Errorf("Hash = %s, want the tip %s (synthetic was %s)", v.Hash, tip, syn)
	}
	if v.Ours != tip || v.Base != tip || v.Source != "feat/x" || v.Target != "main" {
		t.Errorf("endpoints = %+v, want the recorded meta", v)
	}
}

// TestVersionRefsWrapperWithBadTrailerStillUnwraps writes a synthetic wrapper
// commit (git commit-tree, first parent = tip) whose Gg-Meta trailer has 4
// fields — a count ParseVersionMeta explicitly rejects (only 1 or 6 fields
// parse) — and asserts VersionRefs still unwraps to the tip: the trailer's
// mere PRESENCE marks this as the synthetic-commit format, so Hash must come
// from the first parent even when the trailer fails to parse. Treating a
// malformed trailer as "no wrapper" (falling back to %(objectname), the
// wrapper's own sha) would point every restore at an empty commit — the exact
// bug this task exists to prevent.
func TestVersionRefsWrapperWithBadTrailerStillUnwraps(t *testing.T) {
	t.Parallel()
	dir, runner := newTestRepo(t)
	r := &Repo{Runner: runner}
	ctx := context.Background()

	tip := revParse(t, dir, "HEAD")
	msg := fmt.Sprintf("gg version snapshot (rebase)\n\n%s: a b c d\n", metaTrailerKey)
	syn := gitOutT(t, dir, "commit-tree", tip+"^{tree}", "-p", tip, "-m", msg)
	if syn == tip {
		t.Fatalf("commit-tree did not produce a new commit")
	}

	ref := VersionRef("main", "rebase", 1700000000)
	if err := r.UpdateRef(ctx, ref, syn); err != nil {
		t.Fatalf("UpdateRef: %v", err)
	}

	vs, err := r.VersionRefs(ctx, "refs/gg/versions/main")
	if err != nil {
		t.Fatalf("VersionRefs: %v", err)
	}
	if len(vs) != 1 {
		t.Fatalf("got %d rows, want 1: %+v", len(vs), vs)
	}
	v := vs[0]
	if v.Hash != tip {
		t.Errorf("Hash = %s, want the tip %s unwrapped from the wrapper %s", v.Hash, tip, syn)
	}
	if v.Ours != "" || v.Other != "" || v.Base != "" || v.Source != "" || v.Target != "" {
		t.Errorf("endpoints = %+v, want all empty (malformed trailer)", v)
	}
}
