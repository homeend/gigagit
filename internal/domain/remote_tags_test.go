package domain

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/gitexec"
	"github.com/homeend/gigagit/internal/observ"
)

// gitInDir runs a raw git command in dir for test setup.
func gitInDir(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// TestRemoteTagsNoRemote: no remote configured → empty map, nil error.
func TestRemoteTagsNoRemote(t *testing.T) {
	_, svc := newRealRepo(t)
	got, err := svc.RemoteTags(context.Background())
	if err != nil {
		t.Fatalf("no remote: want nil err, got %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("no remote: want empty map, got %v", got)
	}
}

// TestRemoteTagsOriginPushedVsLocalOnly: origin has v1 pushed; v2 is local-only.
func TestRemoteTagsOriginPushedVsLocalOnly(t *testing.T) {
	root := t.TempDir()
	bare := filepath.Join(root, "bare.git")
	workdir := filepath.Join(root, "work")

	// Create a bare remote.
	gitInDir(t, root, "init", "--bare", bare)
	// Clone it to get a working repo with origin configured.
	gitInDir(t, root, "clone", bare, workdir)

	// Set up user identity inside the clone.
	gitInDir(t, workdir, "config", "user.email", "t@t")
	gitInDir(t, workdir, "config", "user.name", "t")

	// Make an initial commit so the branch exists.
	if err := os.WriteFile(filepath.Join(workdir, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitInDir(t, workdir, "checkout", "-b", "main")
	gitInDir(t, workdir, "add", "README.md")
	gitInDir(t, workdir, "commit", "-m", "initial")
	gitInDir(t, workdir, "push", "-u", "origin", "main")

	// Create v1 (lightweight) and push it to origin.
	gitInDir(t, workdir, "tag", "v1")
	gitInDir(t, workdir, "push", "origin", "v1")

	// Create v2 locally only — do NOT push.
	gitInDir(t, workdir, "tag", "v2")

	svc := New(&git.Repo{Runner: gitexec.NewExecRunner("git", workdir, observ.NewRing(50))})
	got, err := svc.RemoteTags(context.Background())
	if err != nil {
		t.Fatalf("RemoteTags: %v", err)
	}
	if !got["v1"] {
		t.Errorf("v1 was pushed to origin: want got[\"v1\"]=true, got %v", got)
	}
	if got["v2"] {
		t.Errorf("v2 was not pushed: want got[\"v2\"]=false, got %v", got)
	}
}

// TestRemoteTagsQueryQuietDoesNotRecordFailure: queryQuiet must not call
// observ.NoteFailure. Point the service at a bogus remote so ls-remote errors;
// assert the error is returned (not swallowed) but SessionFailures is unchanged.
func TestRemoteTagsQueryQuietDoesNotRecordFailure(t *testing.T) {
	dir, svc := newRealRepo(t)

	// Add a remote that points at a nonexistent local path so ls-remote fails fast.
	gitInDir(t, dir, "remote", "add", "origin", filepath.Join(dir, "nonexistent.git"))

	observ.ResetFailures()
	beforeCount := len(observ.SessionFailures())

	_, err := svc.RemoteTags(context.Background())
	if err == nil {
		t.Fatal("bogus remote: want non-nil error, got nil")
	}

	afterCount := len(observ.SessionFailures())
	if afterCount > beforeCount {
		t.Fatalf("queryQuiet must not record to the failure seam: before=%d after=%d failures=%+v",
			beforeCount, afterCount, observ.SessionFailures())
	}
}

// TestRemoteTagsFreshNoRemote: no remote configured → empty map, nil error.
func TestRemoteTagsFreshNoRemote(t *testing.T) {
	_, svc := newRealRepo(t)
	got, err := svc.RemoteTagsFresh(context.Background())
	if err != nil {
		t.Fatalf("no remote: want nil err, got %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("no remote: want empty map, got %v", got)
	}
}

// TestRemoteTagsFreshOriginPushedVsLocalOnly: origin has v1 pushed; v2 is local-only.
func TestRemoteTagsFreshOriginPushedVsLocalOnly(t *testing.T) {
	root := t.TempDir()
	bare := filepath.Join(root, "bare.git")
	workdir := filepath.Join(root, "work")

	gitInDir(t, root, "init", "--bare", bare)
	gitInDir(t, root, "clone", bare, workdir)
	gitInDir(t, workdir, "config", "user.email", "t@t")
	gitInDir(t, workdir, "config", "user.name", "t")

	if err := os.WriteFile(filepath.Join(workdir, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitInDir(t, workdir, "checkout", "-b", "main")
	gitInDir(t, workdir, "add", "README.md")
	gitInDir(t, workdir, "commit", "-m", "initial")
	gitInDir(t, workdir, "push", "-u", "origin", "main")

	// Create v1 (lightweight) and push it to origin.
	gitInDir(t, workdir, "tag", "v1")
	gitInDir(t, workdir, "push", "origin", "v1")

	// Create v2 locally only — do NOT push.
	gitInDir(t, workdir, "tag", "v2")

	svc := New(&git.Repo{Runner: gitexec.NewExecRunner("git", workdir, observ.NewRing(50))})
	got, err := svc.RemoteTagsFresh(context.Background())
	if err != nil {
		t.Fatalf("RemoteTagsFresh: %v", err)
	}
	if !got["v1"] {
		t.Errorf("v1 was pushed to origin: want got[\"v1\"]=true, got %v", got)
	}
	if got["v2"] {
		t.Errorf("v2 was not pushed: want got[\"v2\"]=false, got %v", got)
	}
}

// TestRemoteTagsFreshDoesNotRecordFailure: RemoteTagsFresh must not call
// observ.NoteFailure even when the git call fails. Use a bogus remote so
// ls-remote errors with a real (non-context) git error, then assert
// SessionFailures count is unchanged — the discriminating test that would fail
// if the method were incorrectly wired through query() instead.
func TestRemoteTagsFreshDoesNotRecordFailure(t *testing.T) {
	dir, svc := newRealRepo(t)

	// Add a remote that points at a nonexistent local path so ls-remote fails fast.
	gitInDir(t, dir, "remote", "add", "origin", filepath.Join(dir, "nonexistent.git"))

	observ.ResetFailures()
	beforeCount := len(observ.SessionFailures())

	_, err := svc.RemoteTagsFresh(context.Background())
	if err == nil {
		t.Fatal("bogus remote: want non-nil error, got nil")
	}

	afterCount := len(observ.SessionFailures())
	if afterCount > beforeCount {
		t.Fatalf("RemoteTagsFresh must not record to the failure seam: before=%d after=%d failures=%+v",
			beforeCount, afterCount, observ.SessionFailures())
	}
}
