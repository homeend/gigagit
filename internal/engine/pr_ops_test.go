package engine

import (
	"context"
	"os/exec"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/repogate"
)

func prGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestFetchPRHeadFetchesThenNoOps(t *testing.T) {
	t.Parallel()
	upDir, _ := newRepo(t)
	prGit(t, upDir, "commit", "--allow-empty", "-m", "pr head")
	head := prGit(t, upDir, "rev-parse", "HEAD")
	prGit(t, upDir, "update-ref", "refs/pull/7/head", head)
	dir, repo := newRepo(t)
	op := FetchPRHead{Remote: upDir, Refspec: "refs/pull/7/head", Number: 7, HeadSHA: head}
	if op.LockMode() != repogate.RefWrite {
		t.Fatal("LockMode != RefWrite")
	}
	res, err := op.Run(context.Background(), OpDeps{Repo: repo})
	if err != nil || !res.Changed {
		t.Fatalf("first run = %+v, %v", res, err)
	}
	if got := prGit(t, dir, "rev-parse", git.PRRef(7)); got != head {
		t.Fatalf("ref = %s", got)
	}
	// Second run: the ref already IS HeadSHA. Point Remote at nothing — a
	// fetch attempt would fail, so success proves no git fetch ran.
	op.Remote = "/nonexistent/remote"
	res, err = op.Run(context.Background(), OpDeps{Repo: repo})
	if err != nil || res.Changed {
		t.Fatalf("second run = %+v, %v (want no-op)", res, err)
	}
	// And the guard really is HeadSHA: a different sha must attempt the fetch.
	op.HeadSHA = strings.Repeat("0", 40)
	if _, err = op.Run(context.Background(), OpDeps{Repo: repo}); err == nil {
		t.Fatal("stale HeadSHA + dead remote = nil error; the no-op path is too eager")
	}
}

func TestFetchPRHeadValidates(t *testing.T) {
	t.Parallel()
	_, repo := newRepo(t)
	for _, op := range []FetchPRHead{{Number: 0, Remote: "o", Refspec: "x"}, {Number: 1, Refspec: "x"}, {Number: 1, Remote: "o"}} {
		if _, err := op.Run(context.Background(), OpDeps{Repo: repo}); err == nil {
			t.Errorf("%+v: err = nil", op)
		}
	}
}

func TestForgetPRDeletesOnlyItsRef(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)
	head := prGit(t, dir, "rev-parse", "HEAD")
	prGit(t, dir, "update-ref", git.PRRef(7), head)
	prGit(t, dir, "update-ref", git.PRRef(8), head)
	res, err := ForgetPR{Number: 7}.Run(context.Background(), OpDeps{Repo: repo})
	if err != nil || !res.Changed {
		t.Fatalf("run = %+v, %v", res, err)
	}
	if out, err := exec.Command("git", "-C", dir, "rev-parse", "--verify", "-q", git.PRRef(7)).Output(); err == nil {
		t.Fatalf("ref survived: %s", out)
	}
	if got := prGit(t, dir, "rev-parse", git.PRRef(8)); got != head {
		t.Fatal("a neighbouring PR ref was touched")
	}
	// Forgetting a PR with no ref is a clean no-op, not an error.
	res, err = ForgetPR{Number: 7}.Run(context.Background(), OpDeps{Repo: repo})
	if err != nil || res.Changed {
		t.Fatalf("second run = %+v, %v", res, err)
	}
}
