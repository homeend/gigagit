package git

import (
	"context"
	"os/exec"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/gitexec"
)

func TestMergeBaseArgv(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git merge-base", gitexec.Result{Stdout: "abc123\n"})
	r := &Repo{Runner: f}

	got, err := r.MergeBase(context.Background(), "main", "feature")
	if err != nil {
		t.Fatal(err)
	}
	if got != "abc123" {
		t.Fatalf("MergeBase = %q, want %q", got, "abc123")
	}
	if argv := strings.Join(f.Calls[0].Argv, " "); argv != "merge-base main feature" {
		t.Fatalf("argv = %q", argv)
	}
}

func TestMergeBaseRealRepo(t *testing.T) {
	dir, runner := newTestRepo(t)
	r := &Repo{Runner: runner}

	gitIn(t, dir, "checkout", "-b", "feature")
	gitIn(t, dir, "commit", "--allow-empty", "-m", "feature commit")

	base, err := r.MergeBase(context.Background(), "main", "feature")
	if err != nil {
		t.Fatalf("MergeBase: %v", err)
	}

	// The merge-base of main and feature (branched off main with one extra
	// commit) is main's tip.
	cmd := exec.Command("git", "rev-parse", "main")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	wantBase := strings.TrimSpace(string(out))
	if base != wantBase {
		t.Fatalf("MergeBase = %q, want %q", base, wantBase)
	}
}

func TestMergeBaseNoCommonAncestorErrors(t *testing.T) {
	dir, runner := newTestRepo(t)
	r := &Repo{Runner: runner}

	if _, err := r.MergeBase(context.Background(), "main", "does-not-exist"); err == nil {
		t.Fatal("expected an error for a bad ref, got nil")
	}
	_ = dir
}

func TestUpstreamRefArgv(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git rev-parse (upstream)", gitexec.Result{Stdout: "origin/feature\n"})
	r := &Repo{Runner: f}

	got, err := r.UpstreamRef(context.Background(), "feature")
	if err != nil {
		t.Fatal(err)
	}
	if got != "origin/feature" {
		t.Fatalf("UpstreamRef = %q, want %q", got, "origin/feature")
	}
	if argv := strings.Join(f.Calls[0].Argv, " "); argv != "rev-parse --abbrev-ref feature@{upstream}" {
		t.Fatalf("argv = %q", argv)
	}
}

func TestUpstreamRefRealRepoNoUpstreamErrors(t *testing.T) {
	_, runner := newTestRepo(t)
	r := &Repo{Runner: runner}

	if _, err := r.UpstreamRef(context.Background(), "main"); err == nil {
		t.Fatal("expected an error: main has no upstream configured")
	}
}
