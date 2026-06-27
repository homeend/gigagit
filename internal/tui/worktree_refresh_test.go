package tui

import (
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

// TestCreateAndSwitchUsesFullReRoot pins the switch path (W): the create-and-
// switch confirm arms pendingSwitch so the op reRoots into the new worktree
// (full load) instead of the partial refresh. pendingSources is now set by
// startOp via opAffectedSources — it is captured by the opFinishedMsg handler
// but discarded when reRoot fires (pendingSwitch path wins over the per-source
// registry), so the switch still results in a full load.
func TestCreateAndSwitchUsesFullReRoot(t *testing.T) {
	dir, repo := newRepoDir(t)
	m := New(domain.New(repo))
	loaded, _ := m.Update(m.loadCmd()())
	m = loaded.(Model)

	p := worktreeCreatePopup("feature-y", filepath.Join(t.TempDir(), "wt2"))
	m = m.pushLayer(p)
	m, _ = m.startCreateFromPopup(p, true) // W: create and switch
	if !m.pendingSwitch {
		t.Fatal("create-and-switch must arm pendingSwitch for the reRoot")
	}
	// Prove the outcome: when the op finishes with a path, reRoot fires (full reload).
	// reRoot sets loading=true, ready=false — not a targeted per-source refresh.
	updated, _ := m.Update(opFinishedMsg{res: engine.Result{Path: dir}})
	mm := updated.(Model)
	if !mm.loading || mm.ready {
		t.Fatalf("reRoot path: opFinishedMsg with Path must set loading=true ready=false, got loading=%v ready=%v", mm.loading, mm.ready)
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
