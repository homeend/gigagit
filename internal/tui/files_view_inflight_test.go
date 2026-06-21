package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// While a files-view CommitFiles read is in flight, a further j/k must not issue
// a second read or move the selection (pure-drop); the completion clears the gate.
func TestFilesViewDropsReadWhileInflight(t *testing.T) {
	m := openFilesView(t, filesModel())
	m.filesReadInflight = true
	before := m.sel[panelCommits]

	u, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	mm := u.(Model)
	if cmd != nil {
		t.Fatal("j must not issue a read while one is in flight")
	}
	if mm.sel[panelCommits] != before {
		t.Fatalf("selection moved to %d while a read is in flight (want %d)", mm.sel[panelCommits], before)
	}

	// The read's completion clears the gate so the next keypress advances again.
	u2, _ := mm.Update(commitFilesMsg{hash: mm.filesHash, subject: "x"})
	if u2.(Model).filesReadInflight {
		t.Fatal("commitFilesMsg must clear filesReadInflight")
	}
}

// When no read is in flight, j both moves the selection and issues the reload,
// and that reload marks a read in flight (so the next held j is paced).
func TestFilesViewMoveMarksReadInflight(t *testing.T) {
	m := openFilesView(t, filesModel())
	if m.filesReadInflight {
		t.Fatal("after the open load completes, no read should be in flight")
	}
	u, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	mm := u.(Model)
	if cmd == nil {
		t.Fatal("j on a settled view must issue the follow-live reload")
	}
	if !mm.filesReadInflight {
		t.Fatal("issuing a per-commit reload must mark a read in flight")
	}
}
