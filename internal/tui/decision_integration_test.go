package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/gigagit/gg/internal/engine"
	"github.com/gigagit/gg/internal/git"
	"github.com/gigagit/gg/internal/gitexec"
	"github.com/gigagit/gg/internal/observ"
)

// Drives a real SmartPull that diverges from origin, so it raises a
// "non-fast-forward" decision; the test answers "rebase" through the modal and
// asserts the op resumes and completes with both remote and local changes.
func TestSmartPullDecisionAnsweredThroughModal(t *testing.T) {
	root := t.TempDir()
	origin := filepath.Join(root, "origin.git")
	seed := filepath.Join(root, "seed")
	clone := filepath.Join(root, "clone")
	gitRun := func(dir string, args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	gitRun(root, "init", "--bare", origin)
	gitRun(root, "clone", origin, seed)
	os.WriteFile(filepath.Join(seed, "f.txt"), []byte("v1\n"), 0o644)
	gitRun(seed, "checkout", "-b", "main")
	gitRun(seed, "add", ".")
	gitRun(seed, "commit", "-m", "v1")
	gitRun(seed, "push", "-u", "origin", "main")
	gitRun(root, "clone", origin, clone)
	gitRun(clone, "checkout", "main")
	// origin advances to v2
	os.WriteFile(filepath.Join(seed, "f.txt"), []byte("v2\n"), 0o644)
	gitRun(seed, "add", ".")
	gitRun(seed, "commit", "-m", "v2")
	gitRun(seed, "push", "origin", "main")
	// clone diverges with its own local commit
	os.WriteFile(filepath.Join(clone, "local.txt"), []byte("l\n"), 0o644)
	gitRun(clone, "add", ".")
	gitRun(clone, "commit", "-m", "local")

	repo := &git.Repo{Runner: gitexec.NewExecRunner("git", clone, observ.NewRing(50))}
	m := New(repo)
	loaded, _ := m.Update(m.loadCmd()())
	m = loaded.(Model)

	m, cmd := m.startOp(engine.SmartPull{Intent: engine.PullAndStay})

	answered := false
	for i := 0; i < 100 && m.running; i++ {
		if m.modal != nil {
			if m.modal.req.ID != "non-fast-forward" {
				t.Fatalf("unexpected decision %q", m.modal.req.ID)
			}
			updated, _ := m.Update(keyMsg("enter")) // selection 0 = "rebase"
			m = updated.(Model)
			answered = true
			continue
		}
		if cmd == nil {
			t.Fatal("no command but op still running")
		}
		msg := cmd()
		updated, next := m.Update(msg)
		m = updated.(Model)
		cmd = next
	}
	if m.running {
		t.Fatal("operation did not finish")
	}
	if !answered {
		t.Fatal("expected a non-fast-forward decision modal")
	}
	// Rebase applied the remote v2 change and preserved the local commit.
	if b, _ := os.ReadFile(filepath.Join(clone, "f.txt")); string(b) != "v2\n" {
		t.Fatalf("f.txt = %q, want v2 (remote change applied via rebase)", string(b))
	}
	if _, err := os.Stat(filepath.Join(clone, "local.txt")); err != nil {
		t.Fatalf("local.txt missing after rebase: %v", err)
	}
}
