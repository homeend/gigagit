package tui

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/gitexec"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/observ"
)

// narrowRefspecRepo mirrors internal/engine's narrowClone (unexported there,
// so re-created here): a bare origin (main pushed) + a --single-branch local
// clone, with a second local branch "feat" pushed to origin too — origin has
// the branch, but the clone's fetch refspec still only maps main, so "feat"
// stays unmapped/untracked exactly like the real narrowed-refspec bug.
func narrowRefspecRepo(t *testing.T) *git.Repo {
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
	run(local, "push", "origin", "feat") // exists on origin, but narrow refspec still won't see it
	return &git.Repo{Runner: gitexec.NewExecRunner("git", local, observ.NewRing(50))}
}

func TestNarrowRefspecNoticeNilWhenNoUnmapped(t *testing.T) {
	if n := narrowRefspecNotice(model.RepoHealth{GitCommonDir: "/k"}); n != nil {
		t.Fatalf("want nil when UnmappedBranches is empty, got %+v", n)
	}
}

func TestNarrowRefspecNoticeBuilder(t *testing.T) {
	h := model.RepoHealth{GitCommonDir: "/k", UnmappedBranches: []string{"feat"}}
	n := narrowRefspecNotice(h)
	if n == nil {
		t.Fatal("want the narrow-refspec notice, got nil")
	}
	if n.id != noticeNarrowRefspec {
		t.Fatalf("id = %q, want %q", n.id, noticeNarrowRefspec)
	}
	if n.repoKey != "/k" {
		t.Fatalf("repoKey = %q, want %q", n.repoKey, "/k")
	}
}

// TestNarrowRefspecNoticeFixActionMapsAndFetches drives the fix action's
// dispatched op to real completion against a narrow-clone repo and checks
// the git-visible side effects: this is the only way to pin down that
// AddFetchMappings actually ran with Remote:"origin" and exactly
// h.UnmappedBranches (["feat"], not "main", not the wildcard, not some
// stale/empty set) — a bogus remote or branch list here would either fail
// the fetch outright or, worse, silently write the wrong mapping.
func TestNarrowRefspecNoticeFixActionMapsAndFetches(t *testing.T) {
	repo := narrowRefspecRepo(t)
	m := New(domain.New(repo))
	u, _ := m.Update(m.loadCmd()())
	m = u.(Model)
	m.repoHealth = model.RepoHealth{GitCommonDir: "/k", UnmappedBranches: []string{"feat"}}

	n := narrowRefspecNotice(m.repoHealth)
	if n == nil || len(n.actions) == 0 || n.actions[0].run == nil {
		t.Fatal("expected a populated notice with a runnable fix action")
	}
	if want := "Add mappings + fetch these branches"; n.actions[0].label != want {
		t.Fatalf("action label = %q, want %q", n.actions[0].label, want)
	}

	m, cmd := n.actions[0].run(m)
	if cmd == nil {
		t.Fatal("fix action must start an op (non-nil cmd)")
	}
	if !m.running {
		t.Fatal("fix action must mark the model running")
	}
	if !m.refreshHealthAfterOp {
		t.Fatal("fix action must request a health re-read once the op finishes")
	}
	m = driveOp(t, m, cmd)
	if m.statusMsg == "" {
		t.Fatal("expected a status message after the operation")
	}

	// Behavioral proof the op ran with Remote:"origin" and Branches:["feat"]
	// only: origin's fetch refspec now maps "feat", and the tracking ref for
	// it exists — nothing else was mapped or fetched.
	specs, err := repo.ConfigGetAll(context.Background(), "remote.origin.fetch")
	if err != nil {
		t.Fatalf("ConfigGetAll: %v", err)
	}
	found := false
	for _, s := range specs {
		if s == "+refs/heads/feat:refs/remotes/origin/feat" {
			found = true
		}
	}
	if !found {
		t.Fatalf("remote.origin.fetch = %v, want a mapping for feat", specs)
	}
	if refs, err := repo.ForEachRef(context.Background(), "refs/remotes/origin/feat"); err != nil || len(refs) != 1 {
		t.Fatalf("refs/remotes/origin/feat missing after fix action: refs=%v err=%v", refs, err)
	}
}
