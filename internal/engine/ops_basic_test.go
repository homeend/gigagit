package engine

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/gigagit/gg/internal/git"
	"github.com/gigagit/gg/internal/gitexec"
	"github.com/gigagit/gg/internal/observ"
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
	dir, repo := newRepo(t)
	ctx := context.Background()
	// Add a second tracked file so we can stash one path and keep the other dirty.
	os.WriteFile(filepath.Join(dir, "other.txt"), []byte("base\n"), 0o644)
	if err := repo.StagePaths(ctx, []string{"other.txt"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Commit(ctx, "add other", false); err != nil {
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
