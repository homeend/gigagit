package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/gigagit/gg/internal/domain"
	"github.com/gigagit/gg/internal/git"
	"github.com/gigagit/gg/internal/gitexec"
	"github.com/gigagit/gg/internal/model"
	"github.com/gigagit/gg/internal/observ"
)

func newRepo(t *testing.T) *git.Repo {
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
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("hi\n"), 0o644)
	run("add", ".")
	run("commit", "-m", "initial")
	return &git.Repo{Runner: gitexec.NewExecRunner("git", dir, observ.NewRing(50))}
}

func TestLoadCmdReturnsPopulatedData(t *testing.T) {
	repo := newRepo(t)
	m := New(domain.New(repo))
	msg := m.loadCmd()() // run the command synchronously
	loaded, ok := msg.(dataLoadedMsg)
	if !ok {
		t.Fatalf("expected dataLoadedMsg, got %T", msg)
	}
	if loaded.err != nil {
		t.Fatalf("load error: %v", loaded.err)
	}
	if loaded.status.Branch != "main" {
		t.Fatalf("branch = %q, want main", loaded.status.Branch)
	}
	if len(loaded.branches) != 1 {
		t.Fatalf("branches = %d, want 1", len(loaded.branches))
	}
	if len(loaded.commits) != 1 {
		t.Fatalf("commits = %d, want 1", len(loaded.commits))
	}
}

func TestUpdateAppliesLoadedData(t *testing.T) {
	repo := newRepo(t)
	m := New(domain.New(repo))
	msg := m.loadCmd()()
	updated, _ := m.Update(msg)
	mm := updated.(Model)
	if mm.status.Branch != "main" || len(mm.branches) != 1 || len(mm.commits) != 1 {
		t.Fatalf("model not populated: %+v", mm)
	}
	if mm.loading {
		t.Fatal("loading should be false after data applied")
	}
}

func TestLoadIncludesWorktrees(t *testing.T) {
	m := loadedModel(t)
	if len(m.worktrees) < 1 {
		t.Fatalf("expected at least the main worktree, got %d", len(m.worktrees))
	}
	if m.currentWorktree == "" {
		t.Error("expected currentWorktree to be set")
	}
}

func TestLoadIncludesConfigAndCommonDir(t *testing.T) {
	m := loadedModel(t)
	if m.cfg.Worktree.DefaultBranchTemplate == "" {
		t.Error("expected a default branch template from config defaults")
	}
	if m.cfg.Worktree.PathTemplate == "" {
		t.Error("expected a default path template from config defaults")
	}
	if m.gitCommonDir == "" {
		t.Error("expected gitCommonDir to be set")
	}
}

// TestStaleSnapshotDropped: a dataLoadedMsg from an older generation is
// ignored, so a superseded in-flight load cannot paint over a newer one.
func TestStaleSnapshotDropped(t *testing.T) {
	m := New(domain.New(&git.Repo{Runner: gitexec.NewFakeRunner()}))
	m.loadGen = 5

	stale, _ := m.Update(dataLoadedMsg{gen: 4, branches: []model.Branch{{Name: "x"}}})
	if mm := stale.(Model); len(mm.branches) != 0 {
		t.Fatal("stale-generation snapshot was applied")
	}

	fresh, _ := m.Update(dataLoadedMsg{gen: 5, branches: []model.Branch{{Name: "y"}}})
	if mm := fresh.(Model); len(mm.branches) != 1 || mm.branches[0].Name != "y" {
		t.Fatal("current-generation snapshot was not applied")
	}
}
