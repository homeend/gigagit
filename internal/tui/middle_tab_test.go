package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func ctrlRight(t *testing.T, m Model) Model {
	t.Helper()
	u, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlRight})
	return u.(Model)
}

// ctrl+→ while the top slot owns focus still cycles the top slot.
func TestCtrlCycleTopSlotWhenTopFocused(t *testing.T) {
	t.Parallel()
	m := footerModel()
	m.focus = panelBranches
	m.activeLeftTab = panelBranches
	m = ctrlRight(t, m)
	if m.activeLeftTab == panelBranches {
		t.Fatalf("top slot did not cycle: still %v", m.activeLeftTab)
	}
	if m.activeFilesTab == panelTags {
		t.Fatalf("middle slot must not change when top is focused")
	}
}

// ctrl+→ while the middle box owns focus cycles Files⇄Tags.
func TestCtrlCycleMiddleSlotWhenFilesFocused(t *testing.T) {
	t.Parallel()
	m := footerModel()
	m.focus = panelFiles
	m = ctrlRight(t, m)
	if m.activeFilesTab != panelTags {
		t.Fatalf("middle slot did not switch to Tags: %v", m.activeFilesTab)
	}
	if m.focus != panelTags {
		t.Fatalf("focus must follow the now-active middle tab: %v", m.focus)
	}
}
