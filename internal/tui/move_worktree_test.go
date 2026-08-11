package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
)

// linkedWorktreeIndex returns the index of the first worktree whose path is
// not the main worktree's (m.worktrees[0]).
func linkedWorktreeIndex(t *testing.T, m Model) int {
	t.Helper()
	for i, w := range m.worktrees {
		if w.Path != m.worktrees[0].Path {
			return i
		}
	}
	t.Fatalf("expected a linked worktree in %v", m.worktrees)
	return -1
}

// driveOpKeepCmd runs m/cmd to completion like driveOp, but returns the final
// cmd too (driveOp discards it) so a caller can process a post-op chain (a
// reload batch, or — for a current-worktree move — the guardedReRoot batch).
func driveOpKeepCmd(t *testing.T, m Model, cmd tea.Cmd) (Model, tea.Cmd) {
	t.Helper()
	for i := 0; i < 100 && m.running; i++ {
		if cmd == nil {
			t.Fatal("ran out of commands before the operation finished")
		}
		u, next := m.Update(cmd())
		m = u.(Model)
		cmd = next
	}
	if m.running {
		t.Fatal("operation did not finish")
	}
	return m, cmd
}

// applyCmdChain runs cmd (and, if it's a batch, every sub-command) through
// Update once — the post-op reload / reRoot pattern used across the worktree
// tests (see worktree_delete_test.go, reroot_test.go). Reuses batchCmds
// (search_history_test.go) to flatten a possible tea.BatchMsg.
func applyCmdChain(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	for _, sub := range batchCmds(cmd) {
		u, _ := m.Update(sub())
		m = u.(Model)
	}
	return m
}

// TestRenameKeyOpensPopupAndRenames presses e on a linked worktree row: the
// popup opens prefilled with the directory basename; typing a new name and
// pressing enter runs MoveWorktree, and after the op (and its worktrees
// reload) the panel shows the new path, not the old one.
func TestRenameKeyOpensPopupAndRenames(t *testing.T) {
	dir, repo := newRepoDir(t)
	wt := filepath.Join(filepath.Dir(dir), "wt-a")
	runGit(t, dir, "worktree", "add", "-b", "feature/a", wt, "main")

	m := New(domain.New(repo))
	loaded, _ := m.Update(m.loadCmd()())
	m = loaded.(Model)

	m.focus = panelWorktrees
	idx := linkedWorktreeIndex(t, m)
	m.sel[panelWorktrees] = idx
	oldPath := m.worktrees[idx].Path

	updated, _ := m.Update(keyMsg("e"))
	m = updated.(Model)
	p, ok := m.topLayer().(*moveWorktreePopup)
	if !ok {
		t.Fatalf("expected moveWorktreePopup on top; got %T", m.topLayer())
	}
	if !p.rename {
		t.Error("e must open the rename face")
	}
	if got, want := p.field.Value(), filepath.Base(oldPath); got != want {
		t.Errorf("prefill = %q, want basename %q", got, want)
	}

	// Replace the prefilled basename with a new name (equivalent to clearing
	// the field and typing — the field's own edit mechanics are covered by
	// textfield_test.go).
	p.field = newTextField("wt-a-renamed")
	m, cmd := p.update(m, tea.KeyMsg{Type: tea.KeyEnter})
	if _, still := m.topLayer().(*moveWorktreePopup); still {
		t.Fatal("popup must close on enter")
	}
	if !m.running {
		t.Fatal("enter should start the move op")
	}

	m, cmd = driveOpKeepCmd(t, m, cmd)
	m = applyCmdChain(t, m, cmd) // worktrees reload (opAffectedSources)

	wantPath := filepath.Join(filepath.Dir(oldPath), "wt-a-renamed")
	found, stillOld := false, false
	for _, w := range m.worktrees {
		if w.Path == wantPath {
			found = true
		}
		if w.Path == oldPath {
			stillOld = true
		}
	}
	if !found {
		t.Errorf("worktrees after rename = %v, want an entry at %q", m.worktrees, wantPath)
	}
	if stillOld {
		t.Errorf("worktrees after rename still lists old path %q", oldPath)
	}
}

// TestRenameKeyNoOpOnMainWorktree presses e on the main worktree row: since
// canMoveWorktree refuses the main worktree, no popup should open.
func TestRenameKeyNoOpOnMainWorktree(t *testing.T) {
	dir, repo := newRepoDir(t)
	wt := filepath.Join(filepath.Dir(dir), "wt-b")
	runGit(t, dir, "worktree", "add", "-b", "feature/b", wt, "main")

	m := New(domain.New(repo))
	loaded, _ := m.Update(m.loadCmd()())
	m = loaded.(Model)

	m.focus = panelWorktrees
	m.sel[panelWorktrees] = 0 // main worktree is always listed first

	updated, _ := m.Update(keyMsg("e"))
	m = updated.(Model)
	if m.topLayer() != nil {
		t.Fatalf("e on the main worktree must not push a layer; got %T", m.topLayer())
	}
}

// TestDotMenuOffersRenameAndMoveRows checks the . menu on a linked worktree
// row for both the rename row (the auto-generated footer-binding row) and the
// menu-only move row; the move row's popup must prefill the full path.
func TestDotMenuOffersRenameAndMoveRows(t *testing.T) {
	dir, repo := newRepoDir(t)
	wt := filepath.Join(filepath.Dir(dir), "wt-c")
	runGit(t, dir, "worktree", "add", "-b", "feature/c", wt, "main")

	m := New(domain.New(repo))
	loaded, _ := m.Update(m.loadCmd()())
	m = loaded.(Model)

	m.focus = panelWorktrees
	idx := linkedWorktreeIndex(t, m)
	m.sel[panelWorktrees] = idx
	wtPath := m.worktrees[idx].Path

	rows := availableActions(m)
	if _, ok := findRow(rows, "rename-worktree"); !ok {
		t.Errorf("expected a rename-worktree row in %v", rows)
	}
	moveRow, ok := findRow(rows, "move-worktree")
	if !ok {
		t.Fatalf("expected a move-worktree row in %v", rows)
	}
	if moveRow.run == nil {
		t.Fatal("move-worktree row must carry a run handler (menu-only, no key)")
	}
	tm, _ := moveRow.run(m)
	m2 := tm.(Model)
	p, ok := m2.topLayer().(*moveWorktreePopup)
	if !ok {
		t.Fatalf("expected moveWorktreePopup on top; got %T", m2.topLayer())
	}
	if p.rename {
		t.Error("the Move worktree… row must open the move face, not rename")
	}
	if p.field.Value() != wtPath {
		t.Errorf("move popup prefill = %q, want the full path %q", p.field.Value(), wtPath)
	}
}

// TestRenameRejectsPathSeparator: the rename face refuses a name containing a
// path separator — enter keeps the popup open and sets a status message.
func TestRenameRejectsPathSeparator(t *testing.T) {
	dir, repo := newRepoDir(t)
	wt := filepath.Join(filepath.Dir(dir), "wt-d")
	runGit(t, dir, "worktree", "add", "-b", "feature/d", wt, "main")

	m := New(domain.New(repo))
	loaded, _ := m.Update(m.loadCmd()())
	m = loaded.(Model)

	m.focus = panelWorktrees
	m.sel[panelWorktrees] = linkedWorktreeIndex(t, m)

	updated, _ := m.Update(keyMsg("e"))
	m = updated.(Model)
	p, ok := m.topLayer().(*moveWorktreePopup)
	if !ok {
		t.Fatalf("expected moveWorktreePopup on top; got %T", m.topLayer())
	}
	p.field = newTextField("a/b")
	m2, cmd := p.update(m, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("a name with a path separator must not dispatch the op")
	}
	if _, still := m2.topLayer().(*moveWorktreePopup); !still {
		t.Fatal("popup must stay open when the name is rejected")
	}
	if !strings.Contains(m2.statusMsg, "path separator") {
		t.Errorf("statusMsg = %q, want a path-separator refusal", m2.statusMsg)
	}
}

// TestRenameCurrentWorktreeReRoots renames the worktree gg is currently
// rooted in: after the op finishes the model must re-root onto the new path
// (mirrors the reRoot assertion style of reroot_test.go /
// worktree_w_switch_test.go). This exercises the pre-existing generic
// pendingSwitch → opFinishedMsg → guardedReRoot chain (already wired for
// create-and-switch); this task only arms pendingSwitch for a move of the
// current worktree.
func TestRenameCurrentWorktreeReRoots(t *testing.T) {
	dir, repo := newRepoDir(t)
	wt := filepath.Join(filepath.Dir(dir), "wt-e")
	runGit(t, dir, "worktree", "add", "-b", "feature/e", wt, "main")

	m := New(domain.New(repo))
	loaded, _ := m.Update(m.loadCmd()())
	m = loaded.(Model)

	idx := linkedWorktreeIndex(t, m)
	oldPath := m.worktrees[idx].Path
	// Fake "gg is already rooted in the linked worktree" — the op itself runs
	// `git -C <main> worktree move <old> <new>` regardless of gg's own cwd
	// (see engine.MoveWorktree), so this is safe to fake without actually
	// rebinding the model's git.Repo.
	m.currentWorktree = oldPath

	// The popup's update os.Chdir's the real process out of the worktree being
	// renamed (production behavior, needed on Windows) — restore the test
	// binary's cwd afterward so it doesn't leak into later tests.
	origWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	m.focus = panelWorktrees
	m.sel[panelWorktrees] = idx
	if !m.canMoveWorktree() {
		t.Fatal("canMoveWorktree should allow moving the current (non-main) worktree")
	}

	updated, _ := m.Update(keyMsg("e"))
	m = updated.(Model)
	p, ok := m.topLayer().(*moveWorktreePopup)
	if !ok {
		t.Fatalf("expected moveWorktreePopup on top; got %T", m.topLayer())
	}
	p.field = newTextField("wt-e-renamed")
	m, cmd := p.update(m, tea.KeyMsg{Type: tea.KeyEnter})
	if !m.pendingSwitch {
		t.Fatal("renaming the current worktree must arm pendingSwitch")
	}
	if m.pendingWorktreeMoveOld != oldPath {
		t.Errorf("pendingWorktreeMoveOld = %q, want %q", m.pendingWorktreeMoveOld, oldPath)
	}

	m, cmd = driveOpKeepCmd(t, m, cmd) // runs the move; opFinishedMsg chains guardedReRoot
	wantPath := filepath.Join(filepath.Dir(oldPath), "wt-e-renamed")
	if m.switchTarget != wantPath {
		t.Fatalf("switchTarget = %q, want %q (opFinishedMsg should chain the reRoot)", m.switchTarget, wantPath)
	}
	m = applyCmdChain(t, m, cmd) // the reRoot's own load

	resolvedWant, _ := filepath.EvalSymlinks(wantPath)
	resolvedGot, _ := filepath.EvalSymlinks(m.currentWorktree)
	if resolvedGot != resolvedWant {
		t.Fatalf("after rename currentWorktree = %q, want %q", resolvedGot, resolvedWant)
	}
	t.Cleanup(func() {
		if m.watcher != nil {
			_ = m.watcher.Close()
		}
	})
}
