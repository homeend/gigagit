package git

import (
	"context"
	"strings"
	"testing"
)

func TestFormatParseVersionMetaRoundTrip(t *testing.T) {
	t.Parallel()
	m := VersionMeta{
		Op: "merge", Ours: "aaa", Other: "bbb", Base: "ccc",
		Source: "feat/x", Target: "main",
	}
	got, ok := ParseVersionMeta(FormatVersionMeta(m))
	if !ok {
		t.Fatalf("ParseVersionMeta(%q) not ok", FormatVersionMeta(m))
	}
	if got != m {
		t.Errorf("round trip = %+v, want %+v", got, m)
	}
}

func TestFormatParseVersionMetaRoundTripOneBranchOp(t *testing.T) {
	t.Parallel()
	// One-branch ops (amend, reset, undo-commit, delete-branch, restore) leave
	// every field but Op empty; Format collapses that to just the op word.
	m := VersionMeta{Op: "amend"}
	s := FormatVersionMeta(m)
	if s != "amend" {
		t.Fatalf("FormatVersionMeta(%+v) = %q, want %q", m, s, "amend")
	}
	got, ok := ParseVersionMeta(s)
	if !ok {
		t.Fatalf("ParseVersionMeta(%q) not ok", s)
	}
	if got != m {
		t.Errorf("round trip = %+v, want %+v", got, m)
	}
}

func TestParseVersionMetaRejectsJunk(t *testing.T) {
	t.Parallel()
	for _, s := range []string{"", "merge aaa", "merge aaa bbb ccc", "merge aaa bbb ccc feat/x"} {
		if _, ok := ParseVersionMeta(s); ok {
			t.Errorf("ParseVersionMeta(%q) = ok, want not ok", s)
		}
	}
}

func TestWriteVersionSnapshotShapeAndTrailer(t *testing.T) {
	t.Parallel()
	dir, runner := newTestRepo(t)
	r := &Repo{Runner: runner}
	ctx := context.Background()

	tip := revParse(t, dir, "HEAD")
	m := VersionMeta{Op: "rebase", Ours: tip, Other: tip, Base: tip, Source: "feat/x", Target: "main"}

	sha, err := r.WriteVersionSnapshot(ctx, tip, m, 1700000000)
	if err != nil {
		t.Fatalf("WriteVersionSnapshot: %v", err)
	}
	if sha == "" || sha == tip {
		t.Fatalf("got %q, want a new synthetic commit distinct from the tip", sha)
	}

	// p1 is the snapshotted tip: every reader unwraps it.
	if p1 := gitOut(t, dir, "rev-parse", sha+"^1"); p1 != tip {
		t.Errorf("first parent = %s, want the tip %s", p1, tip)
	}
	// The tree is the tip's own, so `git log --all -p` shows no diff for it.
	if a, b := gitOut(t, dir, "rev-parse", sha+"^{tree}"), gitOut(t, dir, "rev-parse", tip+"^{tree}"); a != b {
		t.Errorf("tree = %s, want the tip's tree %s", a, b)
	}
	if diff := gitOut(t, dir, "show", "--format=", "--name-only", sha); strings.TrimSpace(diff) != "" {
		t.Errorf("synthetic commit shows a diff: %q", diff)
	}
	// The metadata must come back through ONE for-each-ref trailer atom.
	gitRun(t, dir, "update-ref", "refs/gg/versions/main/1700000000-rebase", sha)
	out := gitOut(t, dir, "for-each-ref",
		"--format=%(trailers:key=Gg-Meta,valueonly,separator=%x20)", "refs/gg/versions/")
	got, ok := ParseVersionMeta(strings.TrimSpace(out))
	if !ok || got != m {
		t.Errorf("trailer round trip = %+v (ok=%v), want %+v", got, ok, m)
	}
}

func TestWriteVersionSnapshotIsDeterministic(t *testing.T) {
	t.Parallel()
	dir, runner := newTestRepo(t)
	r := &Repo{Runner: runner}
	ctx := context.Background()
	tip := revParse(t, dir, "HEAD")
	m := VersionMeta{Op: "rebase", Ours: tip, Other: tip, Base: tip, Source: "a", Target: "b"}

	// Fixed identity + fixed dates: the same inputs must yield the same object,
	// and it must not depend on the user's git config.
	a, err := r.WriteVersionSnapshot(ctx, tip, m, 1700000000)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	b, err := r.WriteVersionSnapshot(ctx, tip, m, 1700000000)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if a != b {
		t.Errorf("not deterministic: %s then %s", a, b)
	}
}
