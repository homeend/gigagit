package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestSynthKey(t *testing.T) {
	if got := synthKey("enter"); got.Type != tea.KeyEnter {
		t.Errorf("enter -> %v, want KeyEnter", got.Type)
	}
	if got := synthKey("space"); got.Type != tea.KeySpace {
		t.Errorf("space -> %v, want KeySpace", got.Type)
	}
	for _, k := range []string{"p", "P", "/", ",", "?", "."} {
		if got := synthKey(k); got.String() != k {
			t.Errorf("synthKey(%q).String() = %q", k, got.String())
		}
	}
}

func TestAvailableActionsExcludesNavAndSelf(t *testing.T) {
	m := footerModel()
	m.loading = false
	ids := map[string]bool{}
	for _, r := range availableActions(m) {
		ids[r.id] = true
	}
	if !ids["pull"] || !ids["repo"] {
		t.Errorf("expected global actions present, got %v", ids)
	}
	if ids["actions"] {
		t.Error("the menu must not list itself (actions)")
	}
	for _, nav := range []string{"tab", "ctrl+←/→"} {
		if ids[nav] {
			t.Errorf("navigation key %q must not appear as an action", nav)
		}
	}
}
