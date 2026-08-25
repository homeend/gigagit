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

// narrowClone builds origin(bare, main pushed) + a --single-branch clone on a
// new local branch "feat" with one commit — the fetch refspec maps only main.
func narrowClone(t *testing.T) *git.Repo {
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
	run(root, "clone", "--single-branch", origin, local)
	run(local, "switch", "-c", "feat")
	run(local, "commit", "--allow-empty", "-m", "feat1")
	return &git.Repo{Runner: gitexec.NewExecRunner("git", local, observ.NewRing(50))}
}

func TestPushUnmappedBranchAddMapsAndFetches(t *testing.T) {
	t.Parallel()
	repo := narrowClone(t)
	ctx := context.Background()
	ch := make(chan Event, 64)
	res, err := Push{Remote: "origin", Branch: "feat", SetUpstream: true}.Run(ctx,
		OpDeps{Repo: repo, Events: ch, Decider: MapDecider{FetchMappingDecisionID: "add"}})
	if err != nil {
		t.Fatalf("push: %v", err)
	}
	if !res.Changed {
		t.Fatal("push should report Changed")
	}
	if !strings.Contains(res.Summary, "mapped origin/feat for tracking") {
		t.Fatalf("summary = %q", res.Summary)
	}
	specs, _ := repo.ConfigGetAll(ctx, "remote.origin.fetch")
	want := "+refs/heads/feat:refs/remotes/origin/feat"
	found := false
	for _, s := range specs {
		if s == want {
			found = true
		}
	}
	if !found {
		t.Fatalf("refspec %q not in %v", want, specs)
	}
	refs, err := repo.ForEachRef(ctx, "refs/remotes/origin/feat")
	if err != nil || len(refs) != 1 {
		t.Fatalf("tracking ref: %v err=%v", refs, err)
	}
}

func TestPushUnmappedBranchSkipLeavesConfigAlone(t *testing.T) {
	t.Parallel()
	repo := narrowClone(t)
	ctx := context.Background()
	res, err := Push{Remote: "origin", Branch: "feat", SetUpstream: true}.Run(ctx,
		OpDeps{Repo: repo, Events: make(chan Event, 64), Decider: MapDecider{FetchMappingDecisionID: "skip"}})
	if err != nil {
		t.Fatalf("push: %v", err)
	}
	if res.Summary != "pushed" {
		t.Fatalf("summary = %q, want plain %q", res.Summary, "pushed")
	}
	if refs, _ := repo.ForEachRef(ctx, "refs/remotes/origin/feat"); len(refs) != 0 {
		t.Fatalf("tracking ref should not exist: %v", refs)
	}
}

func TestPushUnmappedBranchDeciderErrorSkips(t *testing.T) {
	t.Parallel()
	repo := narrowClone(t)
	// MapDecider with no entry errors (ErrDecisionRequired) → must skip, not fail.
	res, err := Push{Remote: "origin", Branch: "feat", SetUpstream: true}.Run(context.Background(),
		OpDeps{Repo: repo, Events: make(chan Event, 64), Decider: MapDecider{}})
	if err != nil {
		t.Fatalf("push must not fail on a decider error after success: %v", err)
	}
	if res.Summary != "pushed" {
		t.Fatalf("summary = %q", res.Summary)
	}
}

func TestPushMappedBranchNeverAsks(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)
	ctx := context.Background()
	root := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	origin := filepath.Join(root, "origin.git")
	run("init", "--bare", "-b", "main", origin)
	run("remote", "add", "origin", origin) // default wildcard refspec
	failing := DeciderFunc(func(context.Context, DecisionRequest) (DecisionResponse, error) {
		t.Fatal("decision must not fire for a mapped branch")
		return DecisionResponse{}, nil
	})
	res, err := Push{Remote: "origin", Branch: "main", SetUpstream: true}.Run(ctx,
		OpDeps{Repo: repo, Events: make(chan Event, 64), Decider: failing})
	if err != nil || res.Summary != "pushed" {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}
