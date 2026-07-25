package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/homeend/gigagit/internal/gitexec"
	"github.com/homeend/gigagit/internal/observ"
)

// TestFetchBranches: a clone with a NARROWED refspec + FetchBranches for one
// branch updates exactly that branch's remote-tracking ref.
func TestFetchBranches(t *testing.T) {
	root := t.TempDir()
	run := func(dir string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
			"GIT_CONFIG_GLOBAL="+filepath.Join(root, "gitconfig"),
			"GIT_CONFIG_SYSTEM="+os.DevNull)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return string(out)
	}
	origin := filepath.Join(root, "origin.git")
	run(root, "init", "--bare", "-b", "main", origin)
	seed := filepath.Join(root, "seed")
	run(root, "clone", origin, seed)
	os.WriteFile(filepath.Join(seed, "a.txt"), []byte("1\n"), 0o644)
	run(seed, "add", "-A")
	run(seed, "commit", "-m", "init")
	run(seed, "push", "origin", "main")
	run(seed, "switch", "-c", "feat")
	run(seed, "commit", "--allow-empty", "-m", "feat1")
	run(seed, "push", "origin", "feat")

	local := filepath.Join(root, "local")
	run(root, "clone", "--single-branch", origin, local)
	repo := &Repo{Runner: gitexec.NewExecRunner("git", local, observ.NewRing(50))}
	ctx := context.Background()

	// Map feat, then fetch ONLY feat.
	if err := repo.ConfigAdd(ctx, ConfigLocal, "remote.origin.fetch", "+refs/heads/feat:refs/remotes/origin/feat"); err != nil {
		t.Fatal(err)
	}
	if err := repo.FetchBranches(ctx, "origin", []string{"feat"}); err != nil {
		t.Fatalf("FetchBranches: %v", err)
	}
	refs, err := repo.ForEachRef(ctx, "refs/remotes/origin/feat")
	if err != nil || len(refs) != 1 {
		t.Fatalf("tracking ref after fetch: refs=%v err=%v", refs, err)
	}
}
