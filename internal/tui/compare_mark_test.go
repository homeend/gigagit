package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/gigagit/gg/internal/domain"
	"github.com/gigagit/gg/internal/git"
	"github.com/gigagit/gg/internal/gitexec"
	"github.com/gigagit/gg/internal/observ"
)

// loadedModelTwoCommits builds a real 2-commit repo and a loaded model, so the
// mark/compare tests exercise actual commit hashes (newRepo/loadedModel make
// only one commit, which would make these tests vacuously skip).
func loadedModelTwoCommits(t *testing.T) Model {
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
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("1\n"), 0o644)
	run("add", ".")
	run("commit", "-m", "first")
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("2\n"), 0o644)
	run("add", ".")
	run("commit", "-m", "second")

	repo := &git.Repo{Runner: gitexec.NewExecRunner("git", dir, observ.NewRing(50))}
	m := New(domain.New(repo))
	loaded, _ := m.Update(m.loadCmd()())
	mm := loaded.(Model)
	if len(mm.commits) < 2 {
		t.Fatalf("expected ≥2 commits loaded, got %d", len(mm.commits))
	}
	return mm
}

func TestCommitCompareMarkedRow(t *testing.T) {
	m := loadedModelTwoCommits(t)
	m.focus = panelCommits

	// No mark yet → no row.
	if _, ok := m.commitCompareMarkedRow(); ok {
		t.Fatal("row must be absent with no mark")
	}

	// Mark commit[1] (older), select commit[0] (newer).
	m.mark = &markState{panel: panelCommits, key: m.commits[1].Hash, display: m.commits[1].Hash}
	m.sel[panelCommits] = 0
	r, ok := m.commitCompareMarkedRow()
	if !ok {
		t.Fatal("row must be present with a mark on another commit")
	}
	u, cmd := r.run(m)
	mm := u.(Model)
	if !mm.filesCompare {
		t.Fatal("running the row must open compare mode")
	}
	// older→newer: left = commit[1] (older), right = commit[0] (newer)
	if mm.filesLeft.Hash != m.commits[1].Hash || mm.filesRight.Hash != m.commits[0].Hash {
		t.Fatalf("endpoints = %s ↔ %s, want %s ↔ %s",
			mm.filesLeft.Hash, mm.filesRight.Hash, m.commits[1].Hash, m.commits[0].Hash)
	}
	if cmd == nil {
		t.Fatal("expected a load command")
	}
}

func TestCommitCompareMarkedRowOrdersByFeed(t *testing.T) {
	m := loadedModelTwoCommits(t)
	m.focus = panelCommits
	// Mark the NEWER commit[0], select the OLDER commit[1]: still older→newer.
	m.mark = &markState{panel: panelCommits, key: m.commits[0].Hash, display: m.commits[0].Hash}
	m.sel[panelCommits] = 1
	r, ok := m.commitCompareMarkedRow()
	if !ok {
		t.Fatal("row must be present")
	}
	u, _ := r.run(m)
	mm := u.(Model)
	if mm.filesLeft.Hash != m.commits[1].Hash || mm.filesRight.Hash != m.commits[0].Hash {
		t.Fatalf("endpoints = %s ↔ %s, want older→newer %s ↔ %s",
			mm.filesLeft.Hash, mm.filesRight.Hash, m.commits[1].Hash, m.commits[0].Hash)
	}
}

func TestCommitCompareMarkedRowSameCommitAbsent(t *testing.T) {
	m := loadedModelTwoCommits(t)
	m.focus = panelCommits
	m.mark = &markState{panel: panelCommits, key: m.commits[0].Hash, display: m.commits[0].Hash}
	m.sel[panelCommits] = 0
	if _, ok := m.commitCompareMarkedRow(); ok {
		t.Fatal("row must be absent when the mark equals the selection")
	}
}
