package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
)

// The . menu on a file panel offers "Stage all files" whenever anything is
// unstaged; running it stages every working-tree change — untracked files
// included — in one op.
func TestStageAllRowStagesEverything(t *testing.T) {
	t.Parallel()
	m, dir := multiStageModel(t) // a,b,c all unstaged
	os.WriteFile(filepath.Join(dir, "new.txt"), []byte("untracked\n"), 0o644)
	loaded, _ := m.Update(m.loadCmd()())
	m = loaded.(Model)
	m.focus = panelFiles

	r, ok := m.stageAllRow()
	if !ok {
		t.Fatal("stageAllRow must be offered when unstaged files exist")
	}
	if r.run == nil {
		t.Fatal("stageAllRow must carry a direct run handler")
	}
	updated, cmd := r.run(m)
	m = driveStage(t, updated.(Model), cmd)

	for _, p := range []string{"a.txt", "b.txt", "c.txt", "new.txt"} {
		if !isStaged(stagedByte(m, p)) {
			t.Errorf("%s should be staged; staged byte = %q", p, stagedByte(m, p))
		}
	}
	if m.running {
		t.Fatal("running must be cleared after the status refresh")
	}
}

// The remaining working-tree changes are Files-panel members even when the
// Staged panel is the focus: Stage all files must be offered there too.
func TestStageAllRowFromStagedPanel(t *testing.T) {
	t.Parallel()
	m, dir := multiStageModel(t)
	gitInDir(t, dir, "add", "a.txt") // only a.txt staged; b,c stay unstaged
	loaded, _ := m.Update(m.loadCmd()())
	m = loaded.(Model)
	m.focus = panelStaged

	r, ok := m.stageAllRow()
	if !ok {
		t.Fatal("stageAllRow must be offered from the Staged panel too")
	}
	updated, cmd := r.run(m)
	m = driveStage(t, updated.(Model), cmd)

	for _, p := range []string{"b.txt", "c.txt"} {
		if !isStaged(stagedByte(m, p)) {
			t.Errorf("%s should be staged; staged byte = %q", p, stagedByte(m, p))
		}
	}
}

func TestStageAllRowGating(t *testing.T) {
	t.Parallel()
	m, dir := multiStageModel(t)
	gitInDir(t, dir, "add", ".") // everything staged, nothing left to add
	loaded, _ := m.Update(m.loadCmd()())
	m = loaded.(Model)
	m.focus = panelFiles
	if _, ok := m.stageAllRow(); ok {
		t.Error("no unstaged files → no Stage all files row")
	}
	gitInDir(t, dir, "restore", "--staged", ".")
	loaded, _ = m.Update(m.loadCmd()())
	m = loaded.(Model)
	m.focus = panelBranches
	if _, ok := m.stageAllRow(); ok {
		t.Error("non-file panel focus → no Stage all files row")
	}
}

// A paused merge with every conflict resolved and staged (MERGE_HEAD present,
// commit pending) must NOT offer the row even when unstaged changes exist:
// mass-staging mid-merge would silently fold unrelated edits into the merge
// commit. Same gate as unstageAllRow / canEnterConflict.
func TestStageAllRowHiddenWhilePausedOp(t *testing.T) {
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
	// An unrelated unstaged change: without the paused-op gate the row would show.
	os.WriteFile(filepath.Join(dir, "d.txt"), []byte("dirty\n"), 0o644)

	m := New(domain.New(repo))
	loaded, _ := m.Update(m.loadCmd()())
	m = loaded.(Model)
	m.focus = panelFiles
	if _, ok := m.stageAllRow(); ok {
		t.Fatal("Stage all files must be hidden while a merge sits paused")
	}
}

// The row must surface in the . menu immediately after the "Stage file" row on
// the Files panel.
func TestStageAllRowInActionMenuAfterStageFile(t *testing.T) {
	t.Parallel()
	m, _ := multiStageModel(t)
	m.focus = panelFiles
	m.sel[panelFiles] = 0

	stageIdx, stageAllIdx := -1, -1
	for i, r := range availableActions(m) {
		switch r.id {
		case "stage":
			stageIdx = i
		case "stage-all":
			stageAllIdx = i
		}
	}
	if stageIdx < 0 || stageAllIdx < 0 {
		t.Fatalf("stage row %d / stage-all row %d missing from the . menu", stageIdx, stageAllIdx)
	}
	if stageAllIdx != stageIdx+1 {
		t.Fatalf("stage-all must sit right after stage: stage=%d stage-all=%d", stageIdx, stageAllIdx)
	}
}

// On the Staged panel there is no "Stage file" row; the fallback placement
// still surfaces the row in the . menu.
func TestStageAllRowInActionMenuOnStagedPanel(t *testing.T) {
	t.Parallel()
	m, dir := multiStageModel(t)
	gitInDir(t, dir, "add", "a.txt")
	loaded, _ := m.Update(m.loadCmd()())
	m = loaded.(Model)
	m.focus = panelStaged
	m.sel[panelStaged] = 0

	found := false
	for _, r := range availableActions(m) {
		if r.id == "stage-all" {
			found = true
		}
	}
	if !found {
		t.Fatal("stage-all row missing from the . menu on the Staged panel")
	}
}
