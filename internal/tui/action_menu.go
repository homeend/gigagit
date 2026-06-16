package tui

import (
	tea "github.com/charmbracelet/bubbletea"
)

// actionRow is one runnable action in the . menu: its stable id, the key that
// runs it, and the footer-style label.
type actionRow struct {
	id    string
	key   string
	label string
}

// availableActions returns the currently-available actions (context then
// global, registry order) as menu rows: every binding whose predicate is true,
// excluding pure navigation (id == "") and the menu's own entry (actions).
func availableActions(m Model) []actionRow {
	var out []actionRow
	add := func(bs []footerBinding) {
		for _, b := range bs {
			if b.id == "" || b.id == "actions" {
				continue
			}
			if b.when(m) {
				out = append(out, actionRow{id: b.id, key: b.key, label: b.label})
			}
		}
	}
	add(contextBindings)
	add(globalBindings)
	return out
}

// synthKey reproduces the keypress that runs an action's key, for replay
// through Update. enter/space are the only non-rune keys any action id carries;
// everything else (single runes incl. / , ? .) is a KeyRunes.
func synthKey(name string) tea.KeyMsg {
	switch name {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "space":
		return tea.KeyMsg{Type: tea.KeySpace}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(name)}
	}
}
