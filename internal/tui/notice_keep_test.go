package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// A keep action (the drift notice's copy) leaves the DIALOG open too, not
// just the notice in the list: the user copies a link and keeps reading the
// flagged paths; only Dismiss closes it.
func TestNoticePopupKeepActionLeavesTheDialogOpen(t *testing.T) {
	t.Parallel()
	m := Model{width: 120, height: 40, noticeSessionDismissed: map[string]bool{}} // New() makes the map; a bare Model must too
	ran := false
	m.notices = []notice{{id: "n", title: "t", detail: []string{"  A f3.txt"}, actions: []noticeAction{
		{label: "Copy preview link", keep: true, run: func(m Model) (Model, tea.Cmd) { ran = true; return m, nil }},
		{label: "Dismiss"},
	}}}
	p := &noticePopup{showActions: true}
	m = m.pushLayer(p)
	mm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = mm.(Model)
	if !ran {
		t.Fatal("enter must run the selected action")
	}
	if layerOf[*noticePopup](m) == nil {
		t.Fatal("a keep action must leave the notice dialog open")
	}
	if len(m.notices) != 1 {
		t.Fatalf("notices = %d, want the notice kept", len(m.notices))
	}
	// Dismiss still closes both.
	p.actSel = 1
	mm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = mm.(Model)
	if layerOf[*noticePopup](m) != nil || len(m.notices) != 0 {
		t.Fatalf("Dismiss must close the dialog and drop the notice (layer=%v notices=%d)", layerOf[*noticePopup](m) != nil, len(m.notices))
	}
}
