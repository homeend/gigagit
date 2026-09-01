package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
)

// The . menu on a file panel offers "Unstage all" whenever anything is staged;
// running it pulls every staged file back out of the index in one op.
func TestUnstageAllRowUnstagesEverything(t *testing.T) {
	t.Parallel()
	m, dir := multiStageModel(t)
	gitInDir(t, dir, "add", ".") // a,b,c all staged
	loaded, _ := m.Update(m.loadCmd()())
	m = loaded.(Model)
	m.focus = panelStaged

	r, ok := m.unstageAllRow()
	if !ok {
		t.Fatal("unstageAllRow must be offered when staged files exist")
	}
	if r.run == nil {
		t.Fatal("unstageAllRow must carry a direct run handler")
	}
	updated, cmd := r.run(m)
	m = driveStage(t, updated.(Model), cmd)

	for _, p := range []string{"a.txt", "b.txt", "c.txt"} {
		if isStaged(stagedByte(m, p)) {
			t.Errorf("%s should be unstaged; staged byte = %q", p, stagedByte(m, p))
		}
	}
	if m.running {
		t.Fatal("running must be cleared after the status refresh")
	}
}

// A partially-staged file (staged + unstaged halves) is a Staged-panel member
// too: Unstage all must include it even when the Files panel is the focus.
func TestUnstageAllRowFromFilesPanel(t *testing.T) {
	t.Parallel()
	m, dir := multiStageModel(t)
	gitInDir(t, dir, "add", "a.txt") // only a.txt staged; b,c stay unstaged
	loaded, _ := m.Update(m.loadCmd()())
	m = loaded.(Model)
	m.focus = panelFiles

	r, ok := m.unstageAllRow()
	if !ok {
		t.Fatal("unstageAllRow must be offered from the Files panel too")
	}
	updated, cmd := r.run(m)
	m = driveStage(t, updated.(Model), cmd)

	if isStaged(stagedByte(m, "a.txt")) {
		t.Errorf("a.txt should be unstaged; staged byte = %q", stagedByte(m, "a.txt"))
	}
}

func TestUnstageAllRowGating(t *testing.T) {
	t.Parallel()
	m, _ := multiStageModel(t) // nothing staged
	m.focus = panelStaged
	if _, ok := m.unstageAllRow(); ok {
		t.Error("no staged files → no Unstage all row")
	}
	m.focus = panelBranches
	if _, ok := m.unstageAllRow(); ok {
		t.Error("non-file panel focus → no Unstage all row")
	}
}

// A paused merge with every conflict resolved and staged (MERGE_HEAD present,
// commit pending) must NOT offer the row: unstaging the auto-merged results
// would hollow out the merge commit. Same gate as canEnterConflict.
func TestUnstageAllRowHiddenWhilePausedOp(t *testing.T) {
	t.Parallel()
	dir, repo := newRepoDir(t)
	gitInDir(t, dir, "checkout", "-b", "feat")
	os.WriteFile(filepath.Join(dir, "c.txt"), []byte("feat\n"), 0o644)
	gitInDir(t, dir, "add", ".")
	gitInDir(t, dir, "commit", "-m", "feat")
	gitInDir(t, dir, "checkout", "main")
	os.WriteFile(filepath.Join(dir, "c.txt"), []byte("main\n"), 0o644)
	gitInDir(t, dir, "add", ".")
	gitInDir(t, dir, "commit", "-m", "main")
	merge := exec.Command("git", "merge", "feat")
	merge.Dir = dir
	merge.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	_ = merge.Run() // expected to conflict — that's the point
	// Resolve and stage the conflict WITHOUT committing: the merge stays paused.
	os.WriteFile(filepath.Join(dir, "c.txt"), []byte("resolved\n"), 0o644)
	gitInDir(t, dir, "add", "c.txt")

	m := New(domain.New(repo))
	loaded, _ := m.Update(m.loadCmd()())
	m = loaded.(Model)
	m.focus = panelStaged
	if _, ok := m.unstageAllRow(); ok {
		t.Fatal("Unstage all must be hidden while a merge sits paused")
	}
}

// The row must actually surface in the . menu rows for the Staged panel.
func TestUnstageAllRowInActionMenu(t *testing.T) {
	t.Parallel()
	m, dir := multiStageModel(t)
	gitInDir(t, dir, "add", ".")
	loaded, _ := m.Update(m.loadCmd()())
	m = loaded.(Model)
	m.focus = panelStaged
	m.sel[panelStaged] = 0

	found := false
	for _, r := range availableActions(m) {
		if r.id == "unstage-all" {
			found = true
		}
	}
	if !found {
		t.Fatal("unstage-all row missing from the . menu on the Staged panel")
	}
}
