package tui

import (
	"testing"
)

// TestSwitchKeyPopsConfirm: pressing s on a local branch that is NOT checked
// out in any other worktree should open the yes/no confirm modal (not start
// the op immediately).
func TestSwitchKeyPopsConfirm(t *testing.T) {
	dir, repo := newRepoDir(t)
	runGit(t, dir, "branch", "feature/x")

	m := loadModel(t, repo)
	m.focus = panelBranches
	selectBranchRow(t, &m, "feature/x")

	updated, _ := m.Update(keyMsg("s"))
	mm := updated.(Model)

	if mm.modal == nil || !mm.modal.confirm {
		t.Fatal("s on a switchable branch should pop the slow-op confirm modal")
	}
	if mm.modal.req.Options[mm.modal.sel] != "No" {
		t.Fatalf("confirm default must be No, got options=%v sel=%d",
			mm.modal.req.Options, mm.modal.sel)
	}
	if mm.running {
		t.Fatal("op must not start until the user confirms with y")
	}
}
