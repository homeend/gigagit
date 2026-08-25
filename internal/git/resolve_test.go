package git

import (
	"context"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/gitexec"
)

func TestResolveCommitArgv(t *testing.T) {
	t.Parallel()
	f := gitexec.NewFakeRunner()
	f.SetResponse("git rev-parse verify commit (resolve)", gitexec.Result{Stdout: "abc123def456\n"})
	r := &Repo{Runner: f}

	got, err := r.ResolveCommit(context.Background(), "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if got != "abc123def456" {
		t.Fatalf("ResolveCommit = %q, want %q", got, "abc123def456")
	}
	if argv := strings.Join(f.Calls[0].Argv, " "); argv != "rev-parse --verify HEAD^{commit}" {
		t.Fatalf("argv = %q", argv)
	}
}

func TestResolveCommitRealRepo(t *testing.T) {
	t.Parallel()
	dir, runner := newTestRepo(t)
	r := &Repo{Runner: runner}
	_ = dir

	sha, err := r.ResolveCommit(context.Background(), "HEAD")
	if err != nil {
		t.Fatalf("ResolveCommit: %v", err)
	}
	if len(sha) != 40 {
		t.Fatalf("ResolveCommit(HEAD) = %q, want a 40-char hex SHA", sha)
	}
	for _, c := range sha {
		if !strings.ContainsRune("0123456789abcdef", c) {
			t.Fatalf("ResolveCommit(HEAD) = %q, contains non-hex char %q", sha, c)
		}
	}
}

func TestResolveCommitBogusRefErrors(t *testing.T) {
	t.Parallel()
	_, runner := newTestRepo(t)
	r := &Repo{Runner: runner}

	if _, err := r.ResolveCommit(context.Background(), "does-not-exist"); err == nil {
		t.Fatal("expected an error for a bogus ref, got nil")
	}
}
