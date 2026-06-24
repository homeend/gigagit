package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/gitexec"
)

func TestCommitAmendArgv(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git commit", gitexec.Result{})
	r := &Repo{Runner: f}
	if err := r.Commit(context.Background(), "msg", false, true); err != nil {
		t.Fatalf("commit: %v", err)
	}
	want := []string{"commit", "--amend", "-m", "msg"}
	if !reflect.DeepEqual(f.Calls[0].Argv, want) {
		t.Fatalf("argv = %v, want %v", f.Calls[0].Argv, want)
	}
}

func TestCommitPlainArgv(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git commit", gitexec.Result{})
	r := &Repo{Runner: f}
	if err := r.Commit(context.Background(), "msg", false, false); err != nil {
		t.Fatalf("commit: %v", err)
	}
	want := []string{"commit", "-m", "msg"}
	if !reflect.DeepEqual(f.Calls[0].Argv, want) {
		t.Fatalf("argv = %v, want %v", f.Calls[0].Argv, want)
	}
}

func TestLastCommitMessageArgv(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git log -1", gitexec.Result{Stdout: "subject\n\nbody\n"})
	r := &Repo{Runner: f}
	msg, err := r.LastCommitMessage(context.Background())
	if err != nil {
		t.Fatalf("last-commit-message: %v", err)
	}
	want := []string{"log", "-1", "--pretty=%B"}
	if !reflect.DeepEqual(f.Calls[0].Argv, want) {
		t.Fatalf("argv = %v, want %v", f.Calls[0].Argv, want)
	}
	if msg != "subject\n\nbody\n" {
		t.Fatalf("msg = %q", msg)
	}
}

// Real-git: amend rewrites the last commit's message and folds in staged
// changes without adding a new commit.
func TestCommitAmendReal(t *testing.T) {
	dir, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}
	// newTestRepo has one commit ("initial"). Stage a change and amend.
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("v2\n"), 0o644)
	if err := repo.StagePaths(context.Background(), []string{"README.md"}); err != nil {
		t.Fatalf("stage: %v", err)
	}
	if err := repo.Commit(context.Background(), "reworded", false, true); err != nil {
		t.Fatalf("amend: %v", err)
	}
	if msg, _ := repo.LastCommitMessage(context.Background()); strings.TrimSpace(msg) != "reworded" {
		t.Fatalf("LastCommitMessage = %q, want reworded", msg)
	}
	// still a single commit (amend, not a new one)
	if out := gitOutIn(t, dir, "rev-list", "--count", "HEAD"); out != "1" {
		t.Fatalf("commit count = %q, want 1 (amend must not add a commit)", out)
	}
}

// gitOutIn runs git in dir and returns trimmed stdout.
func gitOutIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return strings.TrimSpace(string(out))
}
