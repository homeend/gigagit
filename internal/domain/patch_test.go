package domain

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommitPatchAndFilePatch(t *testing.T) {
	repoDir, svc := newRealRepo(t)
	ctx := context.Background()
	gitRun(t, repoDir, "commit", "--allow-empty", "-m", "base")
	writeCommit(t, repoDir, "foo.go", "a\nb\nc\n", "add foo")
	// add a second file in the SAME commit so file-scoping is observable
	if err := os.WriteFile(filepath.Join(repoDir, "bar.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repoDir, "add", "bar.txt")
	gitRun(t, repoDir, "commit", "-m", "add bar")
	sha := headHash(t, repoDir)

	patch, name, err := svc.CommitPatch(ctx, sha)
	if err != nil {
		t.Fatalf("CommitPatch: %v", err)
	}
	if !strings.HasPrefix(string(patch), "From ") {
		t.Fatalf("not a mailbox patch: %q", string(patch)[:20])
	}
	if name != shortSHA(sha)+".patch" {
		t.Fatalf("commit defaultName = %q", name)
	}

	fpatch, fname, err := svc.FilePatch(ctx, sha, "bar.txt")
	if err != nil {
		t.Fatalf("FilePatch: %v", err)
	}
	if !strings.Contains(string(fpatch), "bar.txt") || strings.Contains(string(fpatch), "foo.go") {
		t.Fatalf("file patch should be scoped to bar.txt:\n%s", fpatch)
	}
	if fname != shortSHA(sha)+"-bar.txt.patch" {
		t.Fatalf("file defaultName = %q", fname)
	}
}

func TestCommitPatchRefusesMerge(t *testing.T) {
	repoDir, svc := newRealRepo(t)
	ctx := context.Background()
	writeCommit(t, repoDir, "a.txt", "1\n", "base")
	gitRun(t, repoDir, "checkout", "-b", "topic")
	writeCommit(t, repoDir, "a.txt", "2\n", "topic change")
	gitRun(t, repoDir, "checkout", "-")
	writeCommit(t, repoDir, "b.txt", "3\n", "main change")
	gitRun(t, repoDir, "merge", "--no-ff", "topic", "-m", "merge topic")
	mergeSHA := headHash(t, repoDir)

	if _, _, err := svc.CommitPatch(ctx, mergeSHA); !errors.Is(err, ErrMergeCommitPatch) {
		t.Fatalf("CommitPatch(merge) err = %v, want ErrMergeCommitPatch", err)
	}
	if _, _, err := svc.FilePatch(ctx, mergeSHA, "a.txt"); !errors.Is(err, ErrMergeCommitPatch) {
		t.Fatalf("FilePatch(merge) err = %v, want ErrMergeCommitPatch", err)
	}
}

func TestCommitPatchAmRoundTrip(t *testing.T) {
	repoDir, svc := newRealRepo(t)
	ctx := context.Background()
	writeCommit(t, repoDir, "foo.go", "a\nb\nc\n", "base")
	writeCommit(t, repoDir, "foo.go", "a\nB\nc\n", "change foo")
	sha := headHash(t, repoDir)

	patch, _, err := svc.FilePatch(ctx, sha, "foo.go")
	if err != nil {
		t.Fatal(err)
	}
	// Apply onto a fresh repo seeded with the parent content.
	dst := t.TempDir()
	gitRun(t, dst, "init")
	gitRun(t, dst, "config", "user.email", "t@t")
	gitRun(t, dst, "config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(dst, "foo.go"), []byte("a\nb\nc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dst, "add", "foo.go")
	gitRun(t, dst, "commit", "-m", "seed base")

	patchFile := filepath.Join(t.TempDir(), "p.patch")
	if err := os.WriteFile(patchFile, patch, 0o644); err != nil {
		t.Fatal(err)
	}
	am := exec.Command("git", "am", patchFile)
	am.Dir = dst
	am.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := am.CombinedOutput(); err != nil {
		t.Fatalf("git am failed: %v\n%s", err, out)
	}
	got, _ := os.ReadFile(filepath.Join(dst, "foo.go"))
	if string(got) != "a\nB\nc\n" {
		t.Fatalf("after am foo.go = %q, want a\\nB\\nc\\n", got)
	}
}

func TestExportDefaultDirIsParentOfRepo(t *testing.T) {
	repoDir, svc := newRealRepo(t)
	dir, err := svc.ExportDefaultDir(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Dir(filepath.Clean(repoDir)); dir != want {
		t.Fatalf("ExportDefaultDir = %q, want %q (parent of repo)", dir, want)
	}
}
