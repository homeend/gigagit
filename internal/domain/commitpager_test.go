package domain

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"

	"github.com/gigagit/gg/internal/git"
	"github.com/gigagit/gg/internal/gitexec"
	"github.com/gigagit/gg/internal/observ"
)

// realFeedRepo builds a Service over a real 2-commit repo.
func realFeedRepo(t *testing.T) *Service {
	t.Helper()
	dir := t.TempDir()
	run := func(a ...string) {
		c := exec.Command("git", a...)
		c.Dir = dir
		c.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", a, err, out)
		}
	}
	run("init", "-b", "main")
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("1\n"), 0o644)
	run("add", ".")
	run("commit", "-m", "c1")
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("2\n"), 0o644)
	run("add", ".")
	run("commit", "-m", "c2")
	return New(&git.Repo{Runner: gitexec.NewExecRunner("git", dir, observ.NewRing(50))})
}

// TestFeedLoadsViaPager: the feed still returns the repo's commits after the
// refactor (behavior-identical load through the pager).
func TestFeedLoadsViaPager(t *testing.T) {
	feed := realFeedRepo(t).CommitFeed()
	st, err := feed.LoadInitial(context.Background())
	if err != nil {
		t.Fatalf("LoadInitial: %v", err)
	}
	if len(st.Commits) < 2 {
		t.Fatalf("loaded %d commits, want ≥2", len(st.Commits))
	}
}

// TestFeedStillUsesDateOrder: the feed's page fetch still passes --date-order
// (the default dateOrderPager), proving no behavior change.
func TestFeedStillUsesDateOrder(t *testing.T) {
	f := gitexec.NewFakeRunner()
	var argv []string
	f.SetHandler("git log", func(ctx context.Context, a []string) (gitexec.Result, error) {
		argv = a
		return gitexec.Result{Stdout: ""}, nil
	})
	feed := New(&git.Repo{Runner: f}).CommitFeed()
	feed.LoadInitial(context.Background())
	if !slices.Contains(argv, "--date-order") {
		t.Errorf("feed git log argv missing --date-order: %v", argv)
	}
}

func TestDateOrderPagerUsesDateOrder(t *testing.T) {
	f := gitexec.NewFakeRunner()
	var argv []string
	f.SetHandler("git log", func(ctx context.Context, a []string) (gitexec.Result, error) {
		argv = a
		return gitexec.Result{Stdout: ""}, nil
	})
	svc := New(&git.Repo{Runner: f})
	p := dateOrderPager{svc: svc}

	if p.Name() != "date-order" {
		t.Errorf("Name() = %q, want date-order", p.Name())
	}
	if _, err := p.Page(context.Background(), 10, 0, 1, LogScope{}); err != nil {
		t.Fatalf("Page: %v", err)
	}
	if !slices.Contains(argv, "--date-order") {
		t.Errorf("git log argv missing --date-order: %v", argv)
	}
}
