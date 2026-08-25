package domain

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/gitexec"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/observ"
)

// newRealRepo builds a real git repo with one commit (README.md) and returns its
// dir + a Service over an ExecRunner.
func newRealRepo(t *testing.T) (string, *Service) {
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
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "README.md")
	run("commit", "-m", "initial")
	repo := &git.Repo{Runner: gitexec.NewExecRunner("git", dir, observ.NewRing(50))}
	return dir, New(repo)
}

func headHash(t *testing.T, dir string) string {
	t.Helper()
	out, err := func() ([]byte, error) {
		cmd := exec.Command("git", "rev-parse", "HEAD")
		cmd.Dir = dir
		return cmd.Output()
	}()
	if err != nil {
		t.Fatal(err)
	}
	return string(out[:len(out)-1]) // strip newline
}

// CompareFiles against the working tree must include UNTRACKED files (git diff
// omits them); the user's bug was a new untracked file not showing.
func TestCompareFilesIncludesUntracked(t *testing.T) {
	t.Parallel()
	dir, svc := newRealRepo(t)
	ctx := context.Background()
	head := headHash(t, dir)

	// modify a tracked file AND add an untracked one (special-char name).
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	untracked := "timing — kopia.log"
	if err := os.WriteFile(filepath.Join(dir, untracked), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// commit → working tree
	files, err := svc.CompareFiles(ctx,
		model.Endpoint{Kind: model.EndpointCommit, Hash: head},
		model.Endpoint{Kind: model.EndpointWorkTree})
	if err != nil {
		t.Fatal(err)
	}
	byPath := map[string]string{}
	for _, f := range files {
		byPath[f.Path] = f.Status
	}
	if byPath["README.md"] != "M" {
		t.Errorf("README.md status = %q, want M", byPath["README.md"])
	}
	if byPath[untracked] != "A" {
		t.Fatalf("untracked %q missing/wrong (status %q); files=%+v", untracked, byPath[untracked], files)
	}

	// index → working tree (the unstaged diff) must include it too.
	files, err = svc.CompareFiles(ctx,
		model.Endpoint{Kind: model.EndpointIndex},
		model.Endpoint{Kind: model.EndpointWorkTree})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, f := range files {
		if f.Path == untracked && f.Status == "A" {
			found = true
		}
	}
	if !found {
		t.Fatalf("untracked missing from index→worktree: %+v", files)
	}

	// commit → index (staged compare) must NOT include the untracked file.
	files, err = svc.CompareFiles(ctx,
		model.Endpoint{Kind: model.EndpointCommit, Hash: head},
		model.Endpoint{Kind: model.EndpointIndex})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if f.Path == untracked {
			t.Fatalf("untracked must not appear in a staged (commit→index) compare: %+v", files)
		}
	}
}

func TestCompareFilesGatedQuery(t *testing.T) {
	t.Parallel()
	f := gitexec.NewFakeRunner()
	f.SetResponse("git diff (compare files)", gitexec.Result{Stdout: "M\x00README.md\x00A\x00b.txt\x00"})
	f.SetResponse("git ls-files (untracked)", gitexec.Result{Stdout: ""}) // worktree compare also lists untracked
	svc := New(&git.Repo{Runner: f})

	files, err := svc.CompareFiles(context.Background(),
		model.Endpoint{Kind: model.EndpointCommit, Hash: "abc123"},
		model.Endpoint{Kind: model.EndpointWorkTree})
	if err != nil {
		t.Fatalf("CompareFiles err: %v", err)
	}
	if len(files) != 2 || files[0].Path != "README.md" || files[0].Status != "M" || files[1].Path != "b.txt" {
		t.Fatalf("CompareFiles = %+v", files)
	}
}
