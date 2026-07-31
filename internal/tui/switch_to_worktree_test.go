package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// selectBranchRow moves the Branches selection to the row named name.
func selectBranchRow(t *testing.T, m *Model, name string) {
	t.Helper()
	for vi := 0; vi < m.panelLen(panelBranches); vi++ {
		m.sel[panelBranches] = vi
		if bi, ok := m.backingIndex(panelBranches); ok && m.branches[bi].Name == name {
			return
		}
	}
	t.Fatalf("%s not in branches panel: %+v", name, m.branches)
}

func TestSKeyOnOtherWorktreeBranchOpensJumpModal(t *testing.T) {
	dir, repo := newRepoDir(t)
	wt := filepath.Join(filepath.Dir(dir), "wt-feat-e")
	runGit(t, dir, "worktree", "add", "-b", "feature/e", wt, "main")

	m := loadModel(t, repo)
	m.focus = panelBranches
	selectBranchRow(t, &m, "feature/e")

	updated, cmd := m.Update(keyMsg("s"))
	m = updated.(Model)

	if m.running {
		t.Fatal("jumping to a worktree is navigation; no git op should start")
	}
	if cmd != nil {
		t.Fatal("opening the modal should not return a command")
	}
	if m.modal == nil {
		t.Fatal("s on an other-worktree branch should open the jump modal")
	}
	if got := m.modal.req.Options; len(got) != 2 || got[0] != "go to worktree" || got[1] != "cancel" {
		t.Fatalf("modal options = %v", got)
	}
	if !strings.Contains(m.modal.req.Prompt, "feature/e") {
		t.Fatalf("prompt should name the branch: %q", m.modal.req.Prompt)
	}
}

// TestSKeyOnOtherWorktreeBranchOpensJumpModalEvenWhenDirty: checked-out-
// elsewhere precedence wins over the dirty-tree fork — a dirty CURRENT
// worktree selecting a branch checked out elsewhere must still get the
// "switch-to-worktree" jump modal, never "switch-dirty".
func TestSKeyOnOtherWorktreeBranchOpensJumpModalEvenWhenDirty(t *testing.T) {
	dir, repo := newRepoDir(t)
	wt := filepath.Join(filepath.Dir(dir), "wt-feat-e")
	runGit(t, dir, "worktree", "add", "-b", "feature/e", wt, "main")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatalf("dirty README.md: %v", err)
	}

	m := loadModel(t, repo)
	if c := m.status.Counts(); c.Staged+c.Unstaged+c.Conflicted == 0 {
		t.Fatalf("expected a dirty current worktree, got status = %+v", m.status)
	}
	m.focus = panelBranches
	selectBranchRow(t, &m, "feature/e")

	updated, cmd := m.Update(keyMsg("s"))
	m = updated.(Model)

	if m.running {
		t.Fatal("jumping to a worktree is navigation; no git op should start")
	}
	if cmd != nil {
		t.Fatal("opening the modal should not return a command")
	}
	if m.modal == nil || m.modal.req.ID != "switch-to-worktree" {
		t.Fatalf("expected switch-to-worktree modal even on a dirty tree, got %+v", m.modal)
	}
}

func TestJumpModalGoSwitchesToWorktree(t *testing.T) {
	dir, repo := newRepoDir(t)
	wt := filepath.Join(filepath.Dir(dir), "wt-feat-e")
	runGit(t, dir, "worktree", "add", "-b", "feature/e", wt, "main")

	m := loadModel(t, repo)
	m.focus = panelBranches
	selectBranchRow(t, &m, "feature/e")

	u, _ := m.Update(keyMsg("s"))
	m = u.(Model)
	// selection 0 == "go to worktree"
	u, cmd := m.Update(keyMsg("enter"))
	m = u.(Model)

	if m.modal != nil {
		t.Fatal("enter should dismiss the modal")
	}
	want, _ := filepath.EvalSymlinks(wt)
	got, _ := filepath.EvalSymlinks(m.switchTarget)
	if got != want {
		t.Fatalf("switchTarget = %q, want %q", got, want)
	}
	if cmd == nil {
		t.Fatal("jumping to a worktree should return a reload command")
	}
}

func TestJumpModalCancelDoesNothing(t *testing.T) {
	dir, repo := newRepoDir(t)
	wt := filepath.Join(filepath.Dir(dir), "wt-feat-e")
	runGit(t, dir, "worktree", "add", "-b", "feature/e", wt, "main")

	m := loadModel(t, repo)
	m.focus = panelBranches
	selectBranchRow(t, &m, "feature/e")

	u, _ := m.Update(keyMsg("s"))
	m = u.(Model)
	u, cmd := m.Update(keyMsg("esc")) // abortOption -> "cancel"
	m = u.(Model)

	if m.modal != nil {
		t.Fatal("esc should dismiss the modal")
	}
	if m.switchTarget != "" {
		t.Fatalf("esc must not switch: switchTarget = %q", m.switchTarget)
	}
	if cmd != nil {
		t.Fatal("esc should not return a command")
	}
}

func TestSKeyOnLocalBranchStillSmartSwitches(t *testing.T) {
	dir, repo := newRepoDir(t)
	runGit(t, dir, "branch", "feature/local")

	m := loadModel(t, repo)
	m.focus = panelBranches
	selectBranchRow(t, &m, "feature/local")
	m.cfg.UI.DisableSlowOpConfirm = true // test routing (not worktree path), not confirm UX

	u, cmd := m.Update(keyMsg("s"))
	m = u.(Model)

	if m.modal != nil {
		t.Fatal("a branch not checked out elsewhere must not open the jump modal")
	}
	if !m.running || cmd == nil {
		t.Fatal("s on a normal branch should start SmartSwitch")
	}
}
