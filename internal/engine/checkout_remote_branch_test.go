package engine

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/gitexec"
	"github.com/homeend/gigagit/internal/observ"
)

// narrowCloneWithUnseen builds a single-branch clone of a remote that also
// holds a "topic" branch the clone's fetch refspec does not cover — the
// browse-remote-branches scenario.
func narrowCloneWithUnseen(t *testing.T) *git.Repo {
	t.Helper()
	return cloneWithUnseen(t, true)
}

// cloneWithUnseen: narrow = a --single-branch clone made after "topic" was
// pushed; !narrow = an ordinary (wildcard-refspec) clone made BEFORE "topic"
// was pushed — topic exists on the remote but has not been fetched yet.
func cloneWithUnseen(t *testing.T, narrow bool) *git.Repo {
	t.Helper()
	root := t.TempDir()
	run := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
			"GIT_CONFIG_GLOBAL="+filepath.Join(root, "gitconfig"),
			"GIT_CONFIG_SYSTEM="+os.DevNull)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	origin := filepath.Join(root, "origin.git")
	run(root, "init", "--bare", "-b", "main", origin)
	seed := filepath.Join(root, "seed")
	run(root, "clone", origin, seed)
	os.WriteFile(filepath.Join(seed, "a.txt"), []byte("1\n"), 0o644)
	run(seed, "add", "-A")
	run(seed, "commit", "-m", "init")
	run(seed, "push", "origin", "main")
	local := filepath.Join(root, "local")
	if !narrow {
		run(root, "clone", origin, local)
	}
	run(seed, "switch", "-c", "topic")
	run(seed, "commit", "--allow-empty", "-m", "topic1")
	run(seed, "push", "origin", "topic")
	if narrow {
		run(root, "clone", "--single-branch", origin, local)
	}
	return &git.Repo{Runner: gitexec.NewExecRunner("git", local, observ.NewRing(50))}
}

func TestCheckoutRemoteBranchRequiresFields(t *testing.T) {
	t.Parallel()
	if _, err := (CheckoutRemoteBranch{Remote: "origin"}).Run(context.Background(), OpDeps{}); err == nil {
		t.Fatal("missing Branch must fail")
	}
	if _, err := (CheckoutRemoteBranch{Branch: "x"}).Run(context.Background(), OpDeps{}); err == nil {
		t.Fatal("missing Remote must fail")
	}
}

func TestCheckoutRemoteBranchStay(t *testing.T) {
	t.Parallel()
	repo := narrowCloneWithUnseen(t)
	ctx := context.Background()
	res, err := CheckoutRemoteBranch{Remote: "origin", Branch: "topic"}.Run(ctx,
		OpDeps{Repo: repo, Events: make(chan Event, 64)})
	if err != nil {
		t.Fatalf("op: %v", err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
	if cur, _ := repo.CurrentBranch(ctx); cur != "main" {
		t.Fatalf("current branch = %q, want main (stay)", cur)
	}
	if ok, _ := repo.LocalBranchExists(ctx, "topic"); !ok {
		t.Fatal("local branch topic missing")
	}
	if refs, _ := repo.ForEachRef(ctx, "refs/remotes/origin/topic"); len(refs) != 1 {
		t.Fatalf("tracking ref missing: %v", refs)
	}
	specs, _ := repo.ConfigGetAll(ctx, "remote.origin.fetch")
	n := 0
	for _, s := range specs {
		if s == fetchSpec("origin", "topic") {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("fetch mapping not added exactly once: %v", specs)
	}
}

func TestCheckoutRemoteBranchSwitch(t *testing.T) {
	t.Parallel()
	repo := narrowCloneWithUnseen(t)
	ctx := context.Background()
	res, err := CheckoutRemoteBranch{Remote: "origin", Branch: "topic", Intent: CheckoutSwitch}.Run(ctx,
		OpDeps{Repo: repo, Events: make(chan Event, 64)})
	if err != nil {
		t.Fatalf("op: %v", err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
	if cur, _ := repo.CurrentBranch(ctx); cur != "topic" {
		t.Fatalf("current branch = %q, want topic", cur)
	}
}

func TestCheckoutRemoteBranchRerunIsIdempotentOnMapping(t *testing.T) {
	t.Parallel()
	repo := narrowCloneWithUnseen(t)
	ctx := context.Background()
	if _, err := (CheckoutRemoteBranch{Remote: "origin", Branch: "topic"}).Run(ctx,
		OpDeps{Repo: repo, Events: make(chan Event, 64)}); err != nil {
		t.Fatalf("first run: %v", err)
	}
	// A second run must not duplicate the config line; SmartCheckout then
	// refuses or fast-forwards per its own rules (here: ff no-op succeeds).
	if _, err := (CheckoutRemoteBranch{Remote: "origin", Branch: "topic"}).Run(ctx,
		OpDeps{Repo: repo, Events: make(chan Event, 64)}); err != nil {
		t.Fatalf("second run: %v", err)
	}
	specs, _ := repo.ConfigGetAll(ctx, "remote.origin.fetch")
	n := 0
	for _, s := range specs {
		if s == fetchSpec("origin", "topic") {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("mapping duplicated: %v", specs)
	}
}

// On an ordinary clone the wildcard refspec already maps every branch: a
// not-yet-fetched branch is fetched and checked out WITHOUT a per-branch
// line (one would outlive the branch upstream and break a later plain
// `git fetch` once it is deleted there).
func TestCheckoutRemoteBranchWildcardAddsNoMapping(t *testing.T) {
	t.Parallel()
	repo := cloneWithUnseen(t, false)
	ctx := context.Background()
	before, _ := repo.ConfigGetAll(ctx, "remote.origin.fetch")
	if _, err := (CheckoutRemoteBranch{Remote: "origin", Branch: "topic", Intent: CheckoutSwitch}).Run(ctx,
		OpDeps{Repo: repo, Events: make(chan Event, 64)}); err != nil {
		t.Fatalf("op: %v", err)
	}
	if cur, _ := repo.CurrentBranch(ctx); cur != "topic" {
		t.Fatalf("current branch = %q, want topic", cur)
	}
	if refs, _ := repo.ForEachRef(ctx, "refs/remotes/origin/topic"); len(refs) != 1 {
		t.Fatalf("tracking ref missing: %v", refs)
	}
	if after, _ := repo.ConfigGetAll(ctx, "remote.origin.fetch"); !slices.Equal(before, after) {
		t.Fatalf("fetch specs changed: %v → %v", before, after)
	}
}
