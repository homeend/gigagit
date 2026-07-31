package tui

import (
	"reflect"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/gitexec"
	"github.com/homeend/gigagit/internal/model"
)

// switchDirtyModel mirrors footerModel() (footer_test.go) but feat/x is NOT
// checked out in another worktree, so s on it reaches the dirty-tree fork
// (or, on a clean tree, today's confirm/dispatch path) instead of the
// "switch-to-worktree" jump modal.
func switchDirtyModel() Model {
	return Model{
		svc:       domain.New(&git.Repo{Runner: gitexec.NewFakeRunner()}),
		width:     120,
		height:    40,
		sel:       map[panel]int{panelBranches: 1}, // feat/x selected
		sortModes: map[panel]sortMode{},
		status:    model.WorkingTreeStatus{Branch: "main"},
		branches: []model.Branch{
			{Name: "main", IsHead: true},
			{Name: "feat/x"},
		},
		worktrees: []model.Worktree{
			{Path: "/repo", Branch: "main"},
		},
		currentWorktree: "/repo",
	}
}

// TestSwitchDirtyPromptAppears: dirty tree + s on a switchable branch pushes
// the switch-dirty modal instead of dispatching/confirming.
func TestSwitchDirtyPromptAppears(t *testing.T) {
	m := switchDirtyModel()
	m.status.Files = []model.FileStatus{{Path: "a.txt", Unstaged: 'M'}} // Counts().Unstaged > 0
	mm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	m = mm.(Model)
	if m.modal == nil || m.modal.req.ID != "switch-dirty" {
		t.Fatalf("expected switch-dirty modal, got %+v", m.modal)
	}
	want := []string{"worktree", "carry changes", "cancel"}
	if !reflect.DeepEqual(m.modal.req.Options, want) {
		t.Fatalf("options = %v, want %v", m.modal.req.Options, want)
	}
}

// TestSwitchCleanTreeSkipsDirtyPrompt: a clean tree keeps today's flow (no
// switch-dirty modal; the confirm/dispatch path is reached).
func TestSwitchCleanTreeSkipsDirtyPrompt(t *testing.T) {
	m := switchDirtyModel() // no dirty files
	mm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	m = mm.(Model)
	if m.modal != nil && m.modal.req.ID == "switch-dirty" {
		t.Fatal("clean tree must not get the switch-dirty modal")
	}
}

// TestSwitchDirtyPromptResolutions: "worktree" opens the create-worktree
// popup for the selection; "carry changes" dispatches SmartSwitch.
func TestSwitchDirtyPromptResolutions(t *testing.T) {
	// worktree lane
	m := switchDirtyModel()
	m.status.Files = []model.FileStatus{{Path: "a.txt", Unstaged: 'M'}}
	mm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	m = mm.(Model)
	mm, _ = m.modal.onResolve(m, "worktree")
	m = mm.(Model)
	if _, ok := m.topLayer().(*worktreePopup); !ok {
		t.Fatalf("worktree option must open the worktree popup, top = %T", m.topLayer())
	}

	// carry lane: "carry changes" must launch the op (non-nil cmd), the
	// confirmOp-disabled-path convention (see TestConfirmOpDisabledRunsDirectly).
	m = switchDirtyModel()
	m.status.Files = []model.FileStatus{{Path: "a.txt", Unstaged: 'M'}}
	mm, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	m = mm.(Model)
	_, cmd := m.modal.onResolve(m, "carry changes")
	if cmd == nil {
		t.Fatal("carry changes must launch the SmartSwitch op (non-nil cmd)")
	}
}
