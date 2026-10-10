package tui

import (
	"os/exec"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/model"
)

// driveToPostOp runs an op to completion and returns the model plus the command
// opFinishedMsg returned (the post-op refresh) — which driveOp discards.
func driveToPostOp(t *testing.T, m Model, cmd tea.Cmd) (Model, tea.Cmd) {
	t.Helper()
	for i := 0; i < 50 && m.running; i++ {
		if cmd == nil {
			t.Fatal("ran out of commands before the operation finished")
		}
		updated, next := m.Update(cmd())
		m = updated.(Model)
		cmd = next
	}
	if m.running {
		t.Fatal("operation did not finish")
	}
	return m, cmd
}

// worktreeCreatePopup builds a worktreePopup with a resolved preview, as if the
// user had filled in the create-worktree dialog (branch feature-x at main).
func worktreeCreatePopup(branch, path string) *worktreePopup {
	return &worktreePopup{
		startPoint:     "main",
		fromCommit:     true,
		branchOverride: branch,
		previewBranch:  branch,
		previewPath:    path,
	}
}

// TestCreateWorktreeRefreshesRefsOnly drives the real popup confirm path
// (startCreateFromPopup) and proves: the non-switch create sets pendingSources
// to {srcBranches, srcWorktrees} (the production wiring), the post-op reload is
// the targeted per-source refresh (dataAvailableMsg) rather than a full Snapshot
// (dataLoadedMsg), and applying it surfaces the new branch and worktree.
func TestCreateWorktreeRefreshesRefsOnly(t *testing.T) {
	t.Parallel()
	_, repo := newRepoDir(t)
	m := New(domain.New(repo))
	loaded, _ := m.Update(m.loadCmd()())
	m = loaded.(Model)
	if len(m.worktrees) != 1 {
		t.Fatalf("setup: want 1 worktree, got %+v", m.worktrees)
	}

	p := worktreeCreatePopup("feature-x", filepath.Join(t.TempDir(), "wt"))
	m = m.pushLayer(p)
	m, cmd := m.startCreateFromPopup(p, false) // w / enter: create without switching
	if len(m.pendingSources) != 2 ||
		m.pendingSources[0] != srcBranches ||
		m.pendingSources[1] != srcWorktrees {
		t.Fatalf("startCreateFromPopup(_, false) must set pendingSources={srcBranches,srcWorktrees}, got %v", m.pendingSources)
	}

	m, post := driveToPostOp(t, m, cmd)
	if post == nil {
		t.Fatal("no post-op refresh command")
	}
	// post is a tea.Batch of per-source reads; drive each through Update.
	msg := post()
	batchMsgs, ok := msg.(tea.BatchMsg)
	if !ok {
		t.Fatalf("post-op refresh = %T, want tea.BatchMsg (targeted source batch)", msg)
	}
	for _, subCmd := range batchMsgs {
		updated, _ := m.Update(subCmd())
		m = updated.(Model)
	}
	if !hasBranch(m.branches, "feature-x") {
		t.Fatalf("new branch feature-x not in refreshed branches: %+v", m.branches)
	}
	if len(m.worktrees) != 2 {
		t.Fatalf("new worktree not in refreshed worktrees: %+v", m.worktrees)
	}
}

// TestCreateAndSwitchReRootsOnlyForAnUnlistedWorktree pins the switch path
// (W): the create-and-switch confirm arms pendingSwitch so the op switches
// into the new worktree. A path the Worktrees list does not know yet (the
// op just made it) takes the full reRoot; a listed one takes the fast slot
// swap and never blanks the screen.
func TestCreateAndSwitchReRootsOnlyForAnUnlistedWorktree(t *testing.T) {
	t.Parallel()
	dir, repo := newRepoDir(t)
	m := New(domain.New(repo))
	loaded, _ := m.Update(m.loadCmd()())
	m = loaded.(Model)

	p := worktreeCreatePopup("feature-y", filepath.Join(t.TempDir(), "wt2"))
	m = m.pushLayer(p)
	m, _ = m.startCreateFromPopup(p, true) // W: create and switch
	if !m.pendingSwitch {
		t.Fatal("create-and-switch must arm pendingSwitch for the switch")
	}
	// A listed path (the current worktree itself): the fast path, no reload.
	updated, _ := m.Update(opFinishedMsg{res: engine.Result{Path: dir}})
	if mm := updated.(Model); mm.loading || !mm.ready {
		t.Fatalf("a listed worktree must not reload: loading=%v ready=%v", mm.loading, mm.ready)
	}
	// An unlisted path (what the create op really hands back — the list has
	// not been re-read yet): reRoot.
	wt3 := filepath.Join(t.TempDir(), "wt3")
	if out, err := exec.Command("git", "-C", dir, "worktree", "add", "-b", "feature-z", wt3).CombinedOutput(); err != nil {
		t.Fatalf("worktree add: %v\n%s", err, out)
	}
	m.pendingSwitch = true
	updated, _ = m.Update(opFinishedMsg{res: engine.Result{Path: wt3}})
	if mm := updated.(Model); !mm.loading || mm.ready {
		t.Fatalf("an unlisted worktree must reRoot: loading=%v ready=%v", mm.loading, mm.ready)
	}
}

func hasBranch(bs []model.Branch, name string) bool {
	for _, b := range bs {
		if b.Name == name {
			return true
		}
	}
	return false
}
