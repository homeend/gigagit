package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"testing"

	"github.com/homeend/gigagit/internal/model"
)

// m on panels with no mark consumer (Tags, Reflog, Remotes, Worktrees) must be
// inert: nothing ever pairs with or acts on a mark there (pair operations exist
// only on Branches), so setting one would be a dead ◆ with no purpose.
func TestMarkInertOnNonPairPanels(t *testing.T) {
	t.Parallel()
	base := Model{
		width: 100, height: 30,
		sel:       map[panel]int{},
		sortModes: map[panel]sortMode{},
		tags:      []model.Tag{{Name: "v1.0.0", Target: "aaaaaaa"}},
		reflog: []model.ReflogEntry{
			{Selector: "HEAD@{0}", Hash: "aaaaaaaa", ShortHash: "aaaaaaa", Subject: "commit: x"},
		},
		remoteBranches: []model.RemoteBranch{
			{Name: "origin/main", Remote: "origin", Branch: "main", Hash: "aaaaaaa"},
		},
		worktrees: []model.Worktree{{Path: "/tmp/wt", Branch: "main", Head: "aaaaaaa"}},
	}
	for _, tc := range []struct {
		name string
		p    panel
	}{
		{"tags", panelTags},
		{"reflog", panelReflog},
		{"remotes", panelRemotes},
		{"worktrees", panelWorktrees},
	} {
		m := base
		m.focus = tc.p
		m = pressRune(t, m, "m")
		if m.mark != nil {
			t.Errorf("%s: m must be inert, got mark %+v", tc.name, m.mark)
		}
	}
}

// esc on a file panel drops ALL m-marked files at once (mirroring the Commits
// esc behavior on the ◉ compare set).
func TestEscClearsFileMarks(t *testing.T) {
	t.Parallel()
	m := statusModel()
	m.sel[panelFiles] = 0
	m = pressRune(t, m, "m")
	m.sel[panelFiles] = 1
	m = pressRune(t, m, "m")
	if len(m.fileMarks) != 2 {
		t.Fatalf("setup: want 2 file marks, got %v", m.fileMarks)
	}
	m = pressType(t, m, tea.KeyEscape)
	if len(m.fileMarks) != 0 {
		t.Fatalf("esc must clear all file marks, got %v", m.fileMarks)
	}
}

// esc clears file marks from the Staged panel too — both file panels share the
// mark set, so either one can drop it.
func TestEscClearsFileMarksFromStaged(t *testing.T) {
	t.Parallel()
	m := statusModel()
	m.sel[panelFiles] = 0
	m = pressRune(t, m, "m")
	m.focus = panelStaged
	m = pressType(t, m, tea.KeyEscape)
	if len(m.fileMarks) != 0 {
		t.Fatalf("esc on Staged must clear the file marks, got %v", m.fileMarks)
	}
}

// esc on a non-file panel leaves the file marks alone — the focused panel's
// own selection state peels first, matching the Commits-set behavior.
func TestEscElsewhereKeepsFileMarks(t *testing.T) {
	t.Parallel()
	m := statusModel()
	m.sel[panelFiles] = 0
	m = pressRune(t, m, "m")
	m.branches = []model.Branch{{Name: "main", IsHead: true}}
	m.focus = panelBranches
	m = pressType(t, m, tea.KeyEscape)
	if !m.fileMarks["a.go"] {
		t.Fatalf("esc on Branches must not clear file marks, got %v", m.fileMarks)
	}
}
