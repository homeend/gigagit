package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/gitexec"
	"github.com/homeend/gigagit/internal/observ"
)

// gitIn runs a raw git command in dir for test setup.
func gitIn(t *testing.T, dir string, args ...string) {
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

// newClonePair creates a bare remote with one commit on main, then clones it.
func newClonePair(t *testing.T) (string, gitexec.Runner) {
	t.Helper()
	root := t.TempDir()
	origin := filepath.Join(root, "origin.git")
	seed := filepath.Join(root, "seed")
	clone := filepath.Join(root, "clone")

	gitIn(t, root, "init", "--bare", origin)
	gitIn(t, root, "clone", origin, seed)
	if err := os.WriteFile(filepath.Join(seed, "f.txt"), []byte("v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, seed, "checkout", "-b", "main")
	gitIn(t, seed, "add", ".")
	gitIn(t, seed, "commit", "-m", "v1")
	gitIn(t, seed, "push", "-u", "origin", "main")

	gitIn(t, root, "clone", origin, clone)
	gitIn(t, clone, "checkout", "main")
	return clone, gitexec.NewExecRunner("git", clone, observ.NewRing(50))
}

func TestFetchAndPullFFOnly(t *testing.T) {
	t.Parallel()
	clone, runner := newClonePair(t)
	repo := &Repo{Runner: runner}

	if err := repo.Fetch(context.Background(), "origin"); err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if err := repo.PullFFOnly(context.Background(), "origin", "main"); err != nil {
		t.Fatalf("pull ff-only: %v", err)
	}
	_ = clone
}

func TestPushPropagatesCommit(t *testing.T) {
	t.Parallel()
	clone, runner := newClonePair(t)
	repo := &Repo{Runner: runner}

	if err := os.WriteFile(filepath.Join(clone, "f.txt"), []byte("v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := repo.Commit(context.Background(), "v2", true, false); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if err := repo.Push(context.Background(), "origin", "main", false, PushNoForce); err != nil {
		t.Fatalf("push: %v", err)
	}
}

// TestPushForceOverwritesDivergedRemote rewrites local history so it diverges
// from origin: a plain push is rejected, --force-with-lease and --force both win.
func TestPushForceOverwritesDivergedRemote(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		force PushForce
	}{
		{"with-lease", PushForceWithLease},
		{"plain", PushForcePlain},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clone, runner := newClonePair(t)
			repo := &Repo{Runner: runner}

			// Rewrite the tip in place (amend) so it no longer fast-forwards origin.
			if err := os.WriteFile(filepath.Join(clone, "f.txt"), []byte("rewritten\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := repo.Commit(context.Background(), "v1 rewritten", true, true); err != nil {
				t.Fatalf("amend: %v", err)
			}

			if err := repo.Push(context.Background(), "origin", "main", false, PushNoForce); err == nil {
				t.Fatal("plain push of diverged history should be rejected")
			}
			if err := repo.Push(context.Background(), "origin", "main", false, tc.force); err != nil {
				t.Fatalf("force push (%s): %v", tc.name, err)
			}

			origin := filepath.Join(filepath.Dir(clone), "origin.git")
			if got, want := revParse(t, origin, "main"), revParse(t, clone, "HEAD"); got != want {
				t.Fatalf("origin main = %s, want %s (local HEAD)", got, want)
			}
		})
	}
}

// revParse returns the commit hash a ref points to, for assertions.
func revParse(t *testing.T, dir, ref string) string {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", ref)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("rev-parse %s in %s: %v", ref, dir, err)
	}
	return strings.TrimSpace(string(out))
}

func TestFastForwardRefUpdatesNonCheckedOutBranch(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	origin := filepath.Join(root, "origin.git")
	seed := filepath.Join(root, "seed")
	clone := filepath.Join(root, "clone")

	gitIn(t, root, "init", "--bare", origin)
	gitIn(t, root, "clone", origin, seed)
	os.WriteFile(filepath.Join(seed, "f.txt"), []byte("v1\n"), 0o644)
	gitIn(t, seed, "checkout", "-b", "main")
	gitIn(t, seed, "add", ".")
	gitIn(t, seed, "commit", "-m", "v1")
	gitIn(t, seed, "push", "-u", "origin", "main")
	gitIn(t, seed, "checkout", "-b", "dev")
	gitIn(t, seed, "push", "-u", "origin", "dev")

	gitIn(t, root, "clone", origin, clone)
	gitIn(t, clone, "checkout", "main")
	gitIn(t, clone, "branch", "dev", "origin/dev") // local dev at v1, NOT checked out

	// origin advances dev to v2
	gitIn(t, seed, "checkout", "dev")
	os.WriteFile(filepath.Join(seed, "f.txt"), []byte("v2\n"), 0o644)
	gitIn(t, seed, "add", ".")
	gitIn(t, seed, "commit", "-m", "v2")
	gitIn(t, seed, "push", "origin", "dev")

	repo := &Repo{Runner: gitexec.NewExecRunner("git", clone, observ.NewRing(50))}
	if err := repo.FastForwardRef(context.Background(), "origin", "dev"); err != nil {
		t.Fatalf("fast-forward-ref: %v", err)
	}
	if got, want := revParse(t, clone, "dev"), revParse(t, clone, "origin/dev"); got != want {
		t.Fatalf("local dev = %s, want origin/dev %s (ref not fast-forwarded)", got, want)
	}
	cur, _ := repo.CurrentBranch(context.Background())
	if cur != "main" {
		t.Fatalf("current branch = %q, want main (ff-ref must not checkout)", cur)
	}
}

func TestPullFFOnlyRejectsNonFastForward(t *testing.T) {
	t.Parallel()
	clone, runner := newClonePair(t)
	repo := &Repo{Runner: runner}
	root := filepath.Dir(clone)
	origin := filepath.Join(root, "origin.git")

	// A second clone pushes a remote commit to origin/main.
	other := filepath.Join(root, "other")
	gitIn(t, root, "clone", origin, other)
	gitIn(t, other, "checkout", "main")
	os.WriteFile(filepath.Join(other, "f.txt"), []byte("remote-v2\n"), 0o644)
	gitIn(t, other, "add", ".")
	gitIn(t, other, "commit", "-m", "remote-v2")
	gitIn(t, other, "push", "origin", "main")

	// The local clone makes a DIVERGING commit without fetching.
	os.WriteFile(filepath.Join(clone, "f.txt"), []byte("local-v2\n"), 0o644)
	gitIn(t, clone, "add", ".")
	gitIn(t, clone, "commit", "-m", "local-v2")

	if err := repo.PullFFOnly(context.Background(), "origin", "main"); err == nil {
		t.Fatal("expected ff-only pull to fail on diverged history")
	}
}

func TestFastForwardToRef(t *testing.T) {
	t.Parallel()
	dir, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}

	// origin/foo two commits ahead of a local foo at the base.
	gitExec(t, dir, "commit", "--allow-empty", "-m", "c2")
	gitExec(t, dir, "commit", "--allow-empty", "-m", "c3")
	gitExec(t, dir, "update-ref", "refs/remotes/origin/foo", "HEAD")
	gitExec(t, dir, "branch", "foo", "HEAD~2")

	if err := repo.FastForwardToRef(context.Background(), "foo", "refs/remotes/origin/foo"); err != nil {
		t.Fatalf("FastForwardToRef (ff): %v", err)
	}
	if ok, _ := repo.IsAncestor(context.Background(), "refs/remotes/origin/foo", "foo"); !ok {
		t.Fatal("foo was not fast-forwarded to origin/foo")
	}

	// Diverged: give foo its own commit not on origin/foo's line.
	gitExec(t, dir, "checkout", "foo")
	gitExec(t, dir, "commit", "--allow-empty", "-m", "foo-only")
	gitExec(t, dir, "checkout", "main")
	gitExec(t, dir, "update-ref", "refs/remotes/origin/foo", "main") // a different tip than foo
	if err := repo.FastForwardToRef(context.Background(), "foo", "refs/remotes/origin/foo"); err == nil {
		t.Fatal("FastForwardToRef on a diverged branch should error")
	}
}

// gitTry runs a git command in dir and returns its error (non-fatal), for
// presence/absence checks.
func gitTry(dir string, args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	return cmd.Run()
}

func TestRemoteNames(t *testing.T) {
	t.Parallel()
	dir, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}
	gitIn(t, dir, "remote", "add", "origin", "https://example.invalid/x.git")
	gitIn(t, dir, "remote", "add", "upstream", "https://example.invalid/y.git")
	names, err := repo.RemoteNames(context.Background())
	if err != nil {
		t.Fatalf("RemoteNames: %v", err)
	}
	if len(names) != 2 || names[0] != "origin" || names[1] != "upstream" {
		t.Fatalf("names = %v, want [origin upstream]", names)
	}
}

func TestFetchAllUpdatesTrackingRef(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	origin := filepath.Join(root, "origin.git")
	seed := filepath.Join(root, "seed")
	clone := filepath.Join(root, "clone")
	gitIn(t, root, "init", "--bare", origin)
	gitIn(t, root, "clone", origin, seed)
	os.WriteFile(filepath.Join(seed, "f.txt"), []byte("v1\n"), 0o644)
	gitIn(t, seed, "checkout", "-b", "main")
	gitIn(t, seed, "add", ".")
	gitIn(t, seed, "commit", "-m", "c1")
	gitIn(t, seed, "push", "-u", "origin", "main")
	gitIn(t, root, "clone", origin, clone)
	// origin advances main via the seed.
	os.WriteFile(filepath.Join(seed, "f.txt"), []byte("v2\n"), 0o644)
	gitIn(t, seed, "add", ".")
	gitIn(t, seed, "commit", "-m", "c2")
	gitIn(t, seed, "push", "origin", "main")

	repo := &Repo{Runner: gitexec.NewExecRunner("git", clone, observ.NewRing(50))}
	if err := repo.FetchAll(context.Background()); err != nil {
		t.Fatalf("FetchAll: %v", err)
	}
	cmd := exec.Command("git", "log", "-1", "--format=%s", "refs/remotes/origin/main")
	cmd.Dir = clone
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("log: %v\n%s", err, out)
	}
	if strings.TrimSpace(string(out)) != "c2" {
		t.Fatalf("origin/main subject = %q, want c2 (fetch did not update)", strings.TrimSpace(string(out)))
	}
}

func TestPruneRemotes(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	origin := filepath.Join(root, "origin.git")
	seed := filepath.Join(root, "seed")
	clone := filepath.Join(root, "clone")
	gitIn(t, root, "init", "--bare", origin)
	gitIn(t, root, "clone", origin, seed)
	os.WriteFile(filepath.Join(seed, "f.txt"), []byte("v1\n"), 0o644)
	gitIn(t, seed, "checkout", "-b", "main")
	gitIn(t, seed, "add", ".")
	gitIn(t, seed, "commit", "-m", "c1")
	gitIn(t, seed, "push", "-u", "origin", "main")
	gitIn(t, seed, "push", "origin", "main:foo") // create origin/foo
	gitIn(t, root, "clone", origin, clone)       // clone gets refs/remotes/origin/foo
	gitIn(t, seed, "push", "origin", "--delete", "foo")

	repo := &Repo{Runner: gitexec.NewExecRunner("git", clone, observ.NewRing(50))}
	if err := repo.PruneRemotes(context.Background(), "origin"); err != nil {
		t.Fatalf("PruneRemotes: %v", err)
	}
	if gitTry(clone, "rev-parse", "--verify", "refs/remotes/origin/foo") == nil {
		t.Fatal("origin/foo tracking ref should be pruned")
	}
	if gitTry(clone, "rev-parse", "--verify", "refs/remotes/origin/main") != nil {
		t.Fatal("origin/main (live) should survive prune")
	}
	if err := repo.PruneRemotes(context.Background()); err != nil {
		t.Fatalf("PruneRemotes() with no names should be a no-op: %v", err)
	}
}

// gitOut runs git in dir and returns trimmed stdout.
func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return strings.TrimSpace(string(out))
}

func TestPushDeleteArgv(t *testing.T) {
	t.Parallel()
	f := gitexec.NewFakeRunner()
	f.SetResponse("git push delete", gitexec.Result{})
	repo := &Repo{Runner: f}
	if err := repo.PushDelete(context.Background(), "origin", "feat/x"); err != nil {
		t.Fatalf("PushDelete: %v", err)
	}
	var argv []string
	for _, c := range f.Calls {
		if c.Name == "git push delete" {
			argv = c.Argv
		}
	}
	want := []string{"push", "origin", "--delete", "feat/x"}
	if len(argv) != len(want) {
		t.Fatalf("argv = %v, want %v", argv, want)
	}
	for i := range want {
		if argv[i] != want[i] {
			t.Fatalf("argv = %v, want %v", argv, want)
		}
	}
}

func TestPushDeleteRemovesBranchOnRemote(t *testing.T) {
	t.Parallel()
	clone, runner := newClonePair(t)
	repo := &Repo{Runner: runner}
	// Create a branch and push it to origin, then delete it via PushDelete.
	gitIn(t, clone, "branch", "doomed", "main")
	gitIn(t, clone, "push", "origin", "doomed")
	if err := repo.PushDelete(context.Background(), "origin", "doomed"); err != nil {
		t.Fatalf("PushDelete: %v", err)
	}
	// origin must no longer advertise the branch.
	if out := gitOut(t, clone, "ls-remote", "--heads", "origin", "doomed"); strings.TrimSpace(out) != "" {
		t.Fatalf("origin still has doomed branch: %q", out)
	}
}

func TestPushDeleteTagArgv(t *testing.T) {
	t.Parallel()
	f := gitexec.NewFakeRunner()
	f.SetResponse("git push delete tag", gitexec.Result{})
	repo := &Repo{Runner: f}
	if err := repo.PushDeleteTag(context.Background(), "origin", "v1.0.0"); err != nil {
		t.Fatalf("PushDeleteTag: %v", err)
	}
	var argv []string
	for _, c := range f.Calls {
		if c.Name == "git push delete tag" {
			argv = c.Argv
		}
	}
	want := []string{"push", "origin", "--delete", "refs/tags/v1.0.0"}
	if len(argv) != len(want) {
		t.Fatalf("argv = %v, want %v", argv, want)
	}
	for i := range want {
		if argv[i] != want[i] {
			t.Fatalf("argv = %v, want %v", argv, want)
		}
	}
}

func TestPushDeleteTagRemovesTagOnRemote(t *testing.T) {
	t.Parallel()
	clone, runner := newClonePair(t)
	repo := &Repo{Runner: runner}
	gitIn(t, clone, "tag", "v1.0.0")
	gitIn(t, clone, "push", "origin", "v1.0.0")
	if err := repo.PushDeleteTag(context.Background(), "origin", "v1.0.0"); err != nil {
		t.Fatalf("PushDeleteTag: %v", err)
	}
	if out := gitOut(t, clone, "ls-remote", "--tags", "origin", "v1.0.0"); strings.TrimSpace(out) != "" {
		t.Fatalf("origin still has tag v1.0.0: %q", out)
	}
}

func TestPushTagsArgv(t *testing.T) {
	t.Parallel()
	f := gitexec.NewFakeRunner()
	f.SetResponse("git push (tags)", gitexec.Result{})
	repo := &Repo{Runner: f}
	if err := repo.PushTags(context.Background(), "origin", []string{"a", "b"}); err != nil {
		t.Fatalf("PushTags: %v", err)
	}
	var argv []string
	for _, c := range f.Calls {
		if c.Name == "git push (tags)" {
			argv = c.Argv
		}
	}
	want := []string{"push", "origin", "refs/tags/a", "refs/tags/b"}
	if len(argv) != len(want) {
		t.Fatalf("argv = %v, want %v", argv, want)
	}
	for i := range want {
		if argv[i] != want[i] {
			t.Fatalf("argv[%d] = %q, want %q (full: %v)", i, argv[i], want[i], argv)
		}
	}
}

func TestPushTagsEmpty(t *testing.T) {
	t.Parallel()
	f := gitexec.NewFakeRunner()
	repo := &Repo{Runner: f}
	if err := repo.PushTags(context.Background(), "origin", nil); err != nil {
		t.Fatalf("PushTags(empty) should be nil error: %v", err)
	}
	if len(f.Calls) != 0 {
		t.Fatalf("PushTags(empty) must not invoke runner, got %d calls", len(f.Calls))
	}
}

func TestListRemoteHeads(t *testing.T) {
	t.Parallel()
	clone, runner := newClonePair(t)
	repo := &Repo{Runner: runner}
	ctx := context.Background()

	gitIn(t, clone, "branch", "topic/x")
	gitIn(t, clone, "push", "origin", "topic/x")

	heads, err := repo.ListRemoteHeads(ctx, "origin")
	if err != nil {
		t.Fatalf("ListRemoteHeads: %v", err)
	}
	byName := map[string]string{}
	for _, h := range heads {
		byName[h.Name] = h.Hash
	}
	if len(byName) != 2 {
		t.Fatalf("heads = %v, want main + topic/x", heads)
	}
	for _, name := range []string{"main", "topic/x"} {
		if len(byName[name]) != 40 {
			t.Fatalf("head %q: hash %q, want full sha", name, byName[name])
		}
	}
}
