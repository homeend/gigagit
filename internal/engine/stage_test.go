package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStageStagesFile(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)
	os.WriteFile(filepath.Join(dir, "new.txt"), []byte("x\n"), 0o644)

	res, err := Stage{Paths: []string{"new.txt"}}.Run(context.Background(), OpDeps{Repo: repo})
	if err != nil {
		t.Fatalf("stage: %v", err)
	}
	if !res.Changed || !strings.Contains(res.Summary, "staged new.txt") {
		t.Fatalf("result = %+v", res)
	}
	// new.txt is now in the index (added).
	if out := gitOut(t, dir, "diff", "--cached", "--name-only"); !strings.Contains(out, "new.txt") {
		t.Fatalf("new.txt not staged; cached names = %q", out)
	}
}

func TestStageUnstagesFile(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)
	os.WriteFile(filepath.Join(dir, "new.txt"), []byte("x\n"), 0o644)
	gitE(t, dir, "add", "new.txt")

	res, err := Stage{Paths: []string{"new.txt"}, Unstage: true}.Run(context.Background(), OpDeps{Repo: repo})
	if err != nil {
		t.Fatalf("unstage: %v", err)
	}
	if !res.Changed || !strings.Contains(res.Summary, "unstaged new.txt") {
		t.Fatalf("result = %+v", res)
	}
	if out := gitOut(t, dir, "diff", "--cached", "--name-only"); strings.Contains(out, "new.txt") {
		t.Fatalf("new.txt still staged; cached names = %q", out)
	}
}

func TestStageAllStagesUntracked(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)
	os.WriteFile(filepath.Join(dir, "new.txt"), []byte("x\n"), 0o644)

	res, err := Stage{All: true}.Run(context.Background(), OpDeps{Repo: repo})
	if err != nil {
		t.Fatalf("stage all: %v", err)
	}
	if !res.Changed || !strings.Contains(res.Summary, "staged all") {
		t.Fatalf("result = %+v", res)
	}
	if out := gitOut(t, dir, "diff", "--cached", "--name-only"); !strings.Contains(out, "new.txt") {
		t.Fatalf("new.txt not staged; cached names = %q", out)
	}
}

func TestStageAllRejectsPaths(t *testing.T) {
	t.Parallel()
	_, repo := newRepo(t)
	if _, err := (Stage{All: true, Paths: []string{"x"}}).Run(context.Background(), OpDeps{Repo: repo}); err == nil {
		t.Fatal("want error for All+Paths")
	}
}

func TestStageAllRejectsUnstage(t *testing.T) {
	t.Parallel()
	_, repo := newRepo(t)
	if _, err := (Stage{All: true, Unstage: true}).Run(context.Background(), OpDeps{Repo: repo}); err == nil {
		t.Fatal("want error for All+Unstage")
	}
}

// writeIgnored plants .gitignore-excluded docs/specs/a.md plus an untracked
// ok.txt next to it.
func writeIgnored(t *testing.T, dir string) {
	t.Helper()
	os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("docs/specs\n"), 0o644)
	os.MkdirAll(filepath.Join(dir, "docs", "specs"), 0o755)
	os.WriteFile(filepath.Join(dir, "docs", "specs", "a.md"), []byte("x\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "ok.txt"), []byte("y\n"), 0o644)
}

func TestStageIgnoredForceAdd(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)
	writeIgnored(t, dir)

	res, err := Stage{Paths: []string{"docs/specs/a.md"}}.Run(context.Background(),
		OpDeps{Repo: repo, Decider: MapDecider{IgnoredPathsDecisionID: "force-add"}})
	if err != nil {
		t.Fatalf("stage: %v", err)
	}
	if !res.Changed || !strings.Contains(res.Summary, "staged docs/specs/a.md") {
		t.Fatalf("result = %+v", res)
	}
	if out := gitOut(t, dir, "diff", "--cached", "--name-only"); !strings.Contains(out, "docs/specs/a.md") {
		t.Fatalf("docs/specs/a.md not staged; cached names = %q", out)
	}
}

// Abort keeps git's refusal as the error; the non-ignored path named in the
// same call is already staged (git stages it before exiting 1).
func TestStageIgnoredAbort(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)
	writeIgnored(t, dir)

	_, err := Stage{Paths: []string{"ok.txt", "docs/specs/a.md"}}.Run(context.Background(),
		OpDeps{Repo: repo, Decider: MapDecider{IgnoredPathsDecisionID: "abort"}})
	if err == nil || !strings.Contains(err.Error(), "ignored by one of your .gitignore files") {
		t.Fatalf("err = %v, want git's ignored-paths refusal", err)
	}
	out := gitOut(t, dir, "diff", "--cached", "--name-only")
	if !strings.Contains(out, "ok.txt") {
		t.Fatalf("ok.txt should be staged despite the refusal; cached names = %q", out)
	}
	if strings.Contains(out, "docs/specs/a.md") {
		t.Fatalf("docs/specs/a.md must not be staged on abort; cached names = %q", out)
	}
}

// With no Decider (the web stage handler, a bare MapDecider) the original
// refusal passes through unchanged.
func TestStageIgnoredNoDeciderKeepsError(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)
	writeIgnored(t, dir)

	_, err := Stage{Paths: []string{"docs/specs/a.md"}}.Run(context.Background(), OpDeps{Repo: repo})
	if err == nil || !strings.Contains(err.Error(), "ignored by one of your .gitignore files") {
		t.Fatalf("err = %v, want git's ignored-paths refusal", err)
	}
}

func TestIgnoredPathsFrom(t *testing.T) {
	t.Parallel()
	err := fmt.Errorf("git add failed (exit 1): The following paths are ignored by one of your .gitignore files:\ndocs/specs\nhint: Use -f if you really want to add them.\nhint: Turn this message off by running\nhint: \"git config advice.addIgnoredFile false\"")
	if got := ignoredPathsFrom(err); len(got) != 1 || got[0] != "docs/specs" {
		t.Fatalf("paths = %v, want [docs/specs]", got)
	}
	if got := ignoredPathsFrom(fmt.Errorf("git add failed (exit 128): index.lock exists")); got != nil {
		t.Fatalf("paths = %v, want nil for an unrelated error", got)
	}
}
