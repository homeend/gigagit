package engine

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/gitexec"
	"github.com/homeend/gigagit/internal/observ"
)

func newRepo(t *testing.T) (string, *git.Repo) {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-b", "main")
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("hi\n"), 0o644)
	run("add", ".")
	run("commit", "-m", "initial")
	return dir, &git.Repo{Runner: gitexec.NewExecRunner("git", dir, observ.NewRing(50))}
}

func drain(ch chan Event) []Event {
	var out []Event
	for e := range ch {
		out = append(out, e)
	}
	return out
}

func TestCommitOperationEmitsProgressAndDone(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("changed\n"), 0o644)

	ch := make(chan Event, 16)
	res, err := Commit{Message: "second", All: true}.Run(context.Background(), OpDeps{Repo: repo, Events: ch})
	close(ch)
	if err != nil {
		t.Fatalf("commit op: %v", err)
	}
	if !res.Changed {
		t.Fatalf("result = %+v, want Changed", res)
	}
	events := drain(ch)
	var sawProgress, sawDone bool
	for _, e := range events {
		switch e.(type) {
		case Progress:
			sawProgress = true
		case Done:
			sawDone = true
		}
	}
	if !sawProgress || !sawDone {
		t.Fatalf("events missing progress/done: %#v", events)
	}
}

func TestStashOpByPath(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)
	ctx := context.Background()
	// Add a second tracked file so we can stash one path and keep the other dirty.
	os.WriteFile(filepath.Join(dir, "other.txt"), []byte("base\n"), 0o644)
	if err := repo.StagePaths(ctx, []string{"other.txt"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Commit(ctx, "add other", false, false); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("changed-readme\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "other.txt"), []byte("changed-other\n"), 0o644)

	ch := make(chan Event, 16)
	res, err := Stash{Message: "WIP on main", Paths: []string{"other.txt"}}.Run(ctx, OpDeps{Repo: repo, Events: ch})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("result = %+v, want Changed", res)
	}
	st, _ := repo.Status(ctx)
	dirty := map[string]bool{}
	for _, f := range st.Files {
		dirty[f.Path] = true
	}
	if dirty["other.txt"] {
		t.Error("other.txt should have been stashed")
	}
	if !dirty["README.md"] {
		t.Error("README.md should still be dirty")
	}
}

func TestStashApplyKeepsStash(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)
	ctx := context.Background()
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("changed\n"), 0o644)
	if err := repo.StashPush(ctx, "WIP", nil, false); err != nil {
		t.Fatal(err)
	}
	if _, err := (StashApply{Ref: "stash@{0}"}).Run(ctx, OpDeps{Repo: repo, Events: make(chan Event, 16)}); err != nil {
		t.Fatal(err)
	}
	if st, _ := repo.Status(ctx); st.Counts().Unstaged != 1 {
		t.Fatal("apply should restore the change")
	}
	if list, _ := repo.StashList(ctx); len(list) != 1 {
		t.Fatal("apply should keep the stash")
	}
}

func TestStashPopAppliesAndRemoves(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)
	ctx := context.Background()
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("changed\n"), 0o644)
	if err := repo.StashPush(ctx, "WIP", nil, false); err != nil {
		t.Fatal(err)
	}
	if _, err := (StashPop{Ref: "stash@{0}"}).Run(ctx, OpDeps{Repo: repo, Events: make(chan Event, 16)}); err != nil {
		t.Fatal(err)
	}
	if st, _ := repo.Status(ctx); st.Counts().Unstaged != 1 {
		t.Fatal("pop should restore the change")
	}
	if list, _ := repo.StashList(ctx); len(list) != 0 {
		t.Fatal("pop should remove the stash")
	}
}

func TestStashDropRemoves(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)
	ctx := context.Background()
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("changed\n"), 0o644)
	if err := repo.StashPush(ctx, "WIP", nil, false); err != nil {
		t.Fatal(err)
	}
	if _, err := (StashDrop{Ref: "stash@{0}"}).Run(ctx, OpDeps{Repo: repo, Events: make(chan Event, 16)}); err != nil {
		t.Fatal(err)
	}
	if list, _ := repo.StashList(ctx); len(list) != 0 {
		t.Fatal("drop should remove the stash")
	}
}

func TestCommitAmend(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)
	// newRepo leaves a single "initial" commit. Stage a change and amend it.
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x\n"), 0o644)
	gitE(t, dir, "add", ".")

	res, err := Commit{Message: "reworded", Amend: true}.Run(context.Background(), OpDeps{Repo: repo})
	if err != nil {
		t.Fatalf("amend: %v", err)
	}
	// Summary should now include sha and subject.
	sha := strings.TrimSpace(gitOut(t, dir, "rev-parse", "--short", "HEAD"))
	wantSummary := "amended " + sha + " reworded"
	if res.Summary != wantSummary {
		t.Fatalf("summary = %q, want %q", res.Summary, wantSummary)
	}
	if got := gitOut(t, dir, "log", "-1", "--pretty=%s"); got != "reworded" {
		t.Fatalf("subject = %q, want reworded", got)
	}
	if got := gitOut(t, dir, "rev-list", "--count", "HEAD"); got != "1" {
		t.Fatalf("commit count = %q, want 1 (amend must not add a commit)", got)
	}
}

func TestStashApplyConflictReturnsError(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)
	ctx := context.Background()
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("stashed\n"), 0o644)
	if err := repo.StashPush(ctx, "WIP", nil, false); err != nil {
		t.Fatal(err)
	}
	// Re-dirty the same file so the apply would overwrite local changes.
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("local-change\n"), 0o644)
	_, err := (StashApply{Ref: "stash@{0}"}).Run(ctx, OpDeps{Repo: repo, Events: make(chan Event, 16)})
	if err == nil {
		t.Fatal("applying a stash over a conflicting local change must return an error, not silent success")
	}
}

func TestCommitSummaryIncludesSha(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("changed\n"), 0o644)

	res, err := Commit{Message: "sha in summary", All: true}.Run(context.Background(), OpDeps{Repo: repo})
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	sha := strings.TrimSpace(gitOut(t, dir, "rev-parse", "--short", "HEAD"))
	want := "committed " + sha + " sha in summary"
	if res.Summary != want {
		t.Fatalf("summary = %q, want %q", res.Summary, want)
	}
}
