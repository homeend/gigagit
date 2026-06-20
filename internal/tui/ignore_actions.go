package tui

import (
	"path"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gigagit/gg/internal/engine"
	"github.com/gigagit/gg/internal/model"
)

// untrackedFile resolves the Files-panel selection when it is an untracked
// file — the only case where adding to .gitignore is meaningful (git ignores
// only untracked paths). ok is false otherwise.
func (m Model) untrackedFile() (string, bool) {
	if m.focus != panelFiles || !m.opsIdle() {
		return "", false
	}
	bi, ok := m.backingIndex(panelFiles)
	if !ok {
		return "", false
	}
	f := m.status.Files[bi]
	if f.Kind != model.KindUntracked {
		return "", false
	}
	return f.Path, true
}

// fileIgnoreRow offers "Add to .gitignore" on an untracked Files-panel file:
// ignore that exact file (anchored, glob-escaped).
func (m Model) fileIgnoreRow() (actionRow, bool) {
	p, ok := m.untrackedFile()
	if !ok {
		return actionRow{}, false
	}
	return actionRow{
		id:    "ignore-file",
		label: "Add to .gitignore",
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m.startOp(engine.Ignore{Path: p})
		},
	}, true
}

// fileIgnoreExtRow offers "Add extension to .gitignore" — only when the
// untracked file actually has an extension.
func (m Model) fileIgnoreExtRow() (actionRow, bool) {
	p, ok := m.untrackedFile()
	if !ok || path.Ext(p) == "" {
		return actionRow{}, false
	}
	return actionRow{
		id:    "ignore-ext",
		label: "Add extension to .gitignore (*" + path.Ext(p) + ")",
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m.startOp(engine.Ignore{Path: p, Ext: true})
		},
	}, true
}
