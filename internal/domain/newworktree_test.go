package domain

import (
	"context"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// PreviewNewWorktree is the TUI worktree popup's recompute: on a branch
// entrance only the path template applies and the branch is the selection.
func TestPreviewNewWorktreeExisting(t *testing.T) {
	t.Parallel()
	dir, svc := newRealRepo(t)
	ctx := context.Background()
	p := svc.PreviewNewWorktree(ctx, NewWorktreeSpec{
		Start: "b", Existing: true,
		BranchTemplate: "<parent-branch>-<user:never>",
		PathTemplate:   "../<repo>.worktrees/<branch>-<user:ticket>",
		Inputs:         map[string]string{"ticket": "T1"},
		Now:            time.Now(),
	})
	if p.Err != nil {
		t.Fatalf("err: %v", p.Err)
	}
	if !reflect.DeepEqual(p.Labels, []string{"ticket"}) {
		t.Errorf("labels = %v, want only the path template's", p.Labels)
	}
	if p.Branch != "b" {
		t.Errorf("branch = %q, want the selection", p.Branch)
	}
	if want := "../" + filepath.Base(dir) + ".worktrees/b-T1"; p.Path != want {
		t.Errorf("path = %q, want %q", p.Path, want)
	}
}

// A non-branch entrance takes both templates' labels; an empty name previews
// the branch template; a fixed path wins over the path template.
func TestPreviewNewWorktreeFromRef(t *testing.T) {
	t.Parallel()
	_, svc := newRealRepo(t)
	ctx := context.Background()
	spec := NewWorktreeSpec{
		Start:          "v1",
		BranchTemplate: "<parent-branch>-<user:a>",
		PathTemplate:   "../wt/<branch>-<user:b>",
		Inputs:         map[string]string{"a": "x", "b": "y"},
		Now:            time.Now(),
	}
	p := svc.PreviewNewWorktree(ctx, spec)
	if !reflect.DeepEqual(p.Labels, []string{"a", "b"}) {
		t.Errorf("labels = %v", p.Labels)
	}
	if p.Branch != "v1-x" || p.Path != "../wt/v1-x-y" {
		t.Errorf("preview = %q %q", p.Branch, p.Path)
	}
	spec.FixedBranch = "mine"
	spec.FixedPath = "/tmp/elsewhere"
	p = svc.PreviewNewWorktree(ctx, spec)
	if p.Branch != "mine" || p.Path != "/tmp/elsewhere" {
		t.Errorf("fixed = %q %q", p.Branch, p.Path)
	}
}

// The seed and the clock are fixed by the caller, so the same spec previews
// the same names; counters are peeked, never bumped.
func TestPreviewNewWorktreeStable(t *testing.T) {
	t.Parallel()
	_, svc := newRealRepo(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 24, 9, 5, 0, 0, time.UTC)
	spec := NewWorktreeSpec{Start: "b", Existing: true, PathTemplate: "../w/<branch>-<random-alpha:6>-<date>-<seq:n:2>", Seed: 42, Now: now}
	a := svc.PreviewNewWorktree(ctx, spec)
	b := svc.PreviewNewWorktree(ctx, spec)
	if a.Err != nil || a.Path != b.Path {
		t.Fatalf("not stable: %q vs %q (%v)", a.Path, b.Path, a.Err)
	}
	if !strings.HasSuffix(a.Path, "-2026-10-24-01") {
		t.Errorf("path = %q, want the fixed date and the peeked counter", a.Path)
	}
	spec.Seed = 43
	if c := svc.PreviewNewWorktree(ctx, spec); c.Path == a.Path {
		t.Errorf("another seed drew the same random: %q", c.Path)
	}
	if !reflect.DeepEqual(WorktreeSeqNames("../w/<seq:n:2>-<seq:m>"), []string{"n", "m"}) {
		t.Errorf("seq names = %v", WorktreeSeqNames("../w/<seq:n:2>-<seq:m>"))
	}
}

// A template that cannot resolve reports, the labels still come back.
func TestPreviewNewWorktreeError(t *testing.T) {
	t.Parallel()
	_, svc := newRealRepo(t)
	p := svc.PreviewNewWorktree(context.Background(), NewWorktreeSpec{Start: "b", Existing: true, PathTemplate: "../<nope:x>", Now: time.Now()})
	if p.Err == nil {
		t.Error("want an error for an unknown token")
	}
}
