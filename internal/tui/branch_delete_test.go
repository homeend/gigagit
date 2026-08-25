package tui

import (
	"testing"
)

// TestDKeyDeletesBranchThroughConfirmModal presses d on a branch row, answers
// the delete-branch confirm with "delete", and asserts the branch is gone.
func TestDKeyDeletesBranchThroughConfirmModal(t *testing.T) {
	t.Parallel()
	dir, repo := newRepoDir(t)
	runGit(t, dir, "branch", "feat/doomed")

	m := loadModel(t, repo)
	m.focus = panelBranches
	// Select the feat/doomed row (panel order may vary with sort modes).
	found := false
	for vi := 0; vi < m.panelLen(panelBranches); vi++ {
		m.sel[panelBranches] = vi
		if bi, ok := m.backingIndex(panelBranches); ok && m.branches[bi].Name == "feat/doomed" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("feat/doomed not in the panel: %+v", m.branches)
	}

	updated, cmd := m.Update(keyMsg("d"))
	m = updated.(Model)

	answered := false
	for i := 0; i < 100 && m.running; i++ {
		if m.modal != nil {
			if m.modal.req.ID != "delete-branch" {
				t.Fatalf("unexpected decision %q", m.modal.req.ID)
			}
			// selection 0 == "delete"
			u, _ := m.Update(keyMsg("enter"))
			m = u.(Model)
			answered = true
			continue
		}
		if cmd == nil {
			t.Fatal("no command but op still running")
		}
		u, next := m.Update(cmd())
		m = u.(Model)
		cmd = next
	}
	if !answered {
		t.Fatal("expected a delete-branch confirm modal")
	}
	if tuiBranchExists(t, dir, "feat/doomed") {
		t.Fatal("branch still exists after confirmed delete")
	}
}

// TestDKeyOnBranchesEscKeepsBranch answers the confirm with esc (= abort).
func TestDKeyOnBranchesEscKeepsBranch(t *testing.T) {
	t.Parallel()
	dir, repo := newRepoDir(t)
	runGit(t, dir, "branch", "feat/safe")

	m := loadModel(t, repo)
	m.focus = panelBranches
	for vi := 0; vi < m.panelLen(panelBranches); vi++ {
		m.sel[panelBranches] = vi
		if bi, ok := m.backingIndex(panelBranches); ok && m.branches[bi].Name == "feat/safe" {
			break
		}
	}

	updated, cmd := m.Update(keyMsg("d"))
	m = updated.(Model)
	for i := 0; i < 100 && m.running; i++ {
		if m.modal != nil {
			u, _ := m.Update(keyMsg("esc")) // abortOption -> "abort"
			m = u.(Model)
			continue
		}
		if cmd == nil {
			t.Fatal("no command but op still running")
		}
		u, next := m.Update(cmd())
		m = u.(Model)
		cmd = next
	}
	if !tuiBranchExists(t, dir, "feat/safe") {
		t.Fatal("esc on the confirm must keep the branch")
	}
}
