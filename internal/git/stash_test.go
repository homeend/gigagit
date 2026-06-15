package git

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/gigagit/gg/internal/gitexec"
)

func TestStashPushArgv(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git stash push", gitexec.Result{})
	r := &Repo{Runner: f}
	if err := r.StashPush(context.Background(), "WIP on main", []string{"a.go", "b.go"}, true); err != nil {
		t.Fatal(err)
	}
	want := []string{"stash", "push", "-m", "WIP on main", "-u", "--", "a.go", "b.go"}
	if !reflect.DeepEqual(f.Calls[0].Argv, want) {
		t.Fatalf("argv = %v, want %v", f.Calls[0].Argv, want)
	}
}

func TestStashPushNoPathsNoUntracked(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git stash push", gitexec.Result{})
	r := &Repo{Runner: f}
	_ = r.StashPush(context.Background(), "msg", nil, false)
	want := []string{"stash", "push", "-m", "msg"}
	if !reflect.DeepEqual(f.Calls[0].Argv, want) {
		t.Fatalf("argv = %v, want %v", f.Calls[0].Argv, want)
	}
}

func TestStashPopRefArgv(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git stash pop", gitexec.Result{})
	r := &Repo{Runner: f}
	_ = r.StashPop(context.Background(), "stash@{2}")
	if want := []string{"stash", "pop", "stash@{2}"}; !reflect.DeepEqual(f.Calls[0].Argv, want) {
		t.Fatalf("argv = %v, want %v", f.Calls[0].Argv, want)
	}
	f2 := gitexec.NewFakeRunner()
	f2.SetResponse("git stash pop", gitexec.Result{})
	r2 := &Repo{Runner: f2}
	_ = r2.StashPop(context.Background(), "")
	if want := []string{"stash", "pop"}; !reflect.DeepEqual(f2.Calls[0].Argv, want) {
		t.Fatalf("empty-ref argv = %v, want %v", f2.Calls[0].Argv, want)
	}
}

func TestStashApplyDropArgv(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git stash apply", gitexec.Result{})
	f.SetResponse("git stash drop", gitexec.Result{})
	r := &Repo{Runner: f}
	_ = r.StashApply(context.Background(), "stash@{1}")
	_ = r.StashDrop(context.Background(), "stash@{1}")
	if want := []string{"stash", "apply", "stash@{1}"}; !reflect.DeepEqual(f.Calls[0].Argv, want) {
		t.Fatalf("apply argv = %v, want %v", f.Calls[0].Argv, want)
	}
	if want := []string{"stash", "drop", "stash@{1}"}; !reflect.DeepEqual(f.Calls[1].Argv, want) {
		t.Fatalf("drop argv = %v, want %v", f.Calls[1].Argv, want)
	}
}

func TestStashCommitArgv(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git rev-parse (stash)", gitexec.Result{Stdout: "deadbeef\n"})
	r := &Repo{Runner: f}
	sha, err := r.StashCommit(context.Background(), "stash@{0}")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"rev-parse", "stash@{0}"}; !reflect.DeepEqual(f.Calls[0].Argv, want) {
		t.Fatalf("argv = %v, want %v", f.Calls[0].Argv, want)
	}
	if sha != "deadbeef" {
		t.Fatalf("sha = %q, want deadbeef", sha)
	}
}

// Real-git: stash one tracked modification by path; the other stays dirty.
func TestStashPushByPathRoundTrip(t *testing.T) {
	dir, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}
	ctx := context.Background()
	// Both files must be TRACKED before a path-scoped stash without -u: git
	// stash push -- <untracked> errors. Commit a baseline, then stash a
	// modification of one tracked file.
	os.WriteFile(filepath.Join(dir, "keep.txt"), []byte("base\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "stashme.txt"), []byte("base\n"), 0o644)
	if err := repo.StagePaths(ctx, []string{"keep.txt", "stashme.txt"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Commit(ctx, "baseline", false); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "keep.txt"), []byte("changed-keep\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "stashme.txt"), []byte("changed-stash\n"), 0o644)

	if err := repo.StashPush(ctx, "WIP on main", []string{"stashme.txt"}, false); err != nil {
		t.Fatal(err)
	}
	st, _ := repo.Status(ctx)
	dirty := map[string]bool{}
	for _, fl := range st.Files {
		dirty[fl.Path] = true
	}
	if dirty["stashme.txt"] {
		t.Error("stashme.txt should have been stashed (reverted)")
	}
	if !dirty["keep.txt"] {
		t.Error("keep.txt should still be dirty")
	}
	list, _ := repo.StashList(ctx)
	if len(list) != 1 {
		t.Fatalf("want 1 stash, got %d: %v", len(list), list)
	}
	if err := repo.StashPop(ctx, ""); err != nil {
		t.Fatal(err)
	}
}

// Real-git smoke for whole-tree push/list/pop (updated from the original
// single-arg signatures).
func TestStashPushListPop(t *testing.T) {
	dir, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}

	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := repo.StashPush(context.Background(), "gg-test", nil, false); err != nil {
		t.Fatalf("stash push: %v", err)
	}
	st, _ := repo.Status(context.Background())
	if c := st.Counts(); c.Unstaged != 0 {
		t.Fatalf("expected clean tree after stash, got %+v", c)
	}
	list, err := repo.StashList(context.Background())
	if err != nil {
		t.Fatalf("stash list: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("stash list = %v, want 1 entry", list)
	}
	if err := repo.StashPop(context.Background(), ""); err != nil {
		t.Fatalf("stash pop: %v", err)
	}
	st, _ = repo.Status(context.Background())
	if c := st.Counts(); c.Unstaged != 1 {
		t.Fatalf("expected change restored after pop, got %+v", c)
	}
}
