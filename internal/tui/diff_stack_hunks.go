package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
)

// Hunk staging from inside the diff view (design §11 item 3).
//
// The TUI has no inline picking lane to make per-file: hunk staging is a
// full-screen hunkPicker LAYER, opened from the Files / Staged panel's H. So a
// stack's H opens that same picker over the stack — pushLayer returns to the
// layer beneath when it closes, which is how the reader keeps their place
// (design §9's "the picker cannot live inside a stack" read the other way
// round: it lives OVER one). The web half picks inline, per slot; that
// asymmetry is deliberate.

// hunkFileHere is the file H acts on: the cursor's file in a stack, the open
// file in a single working-tree diff. staged says which lane it is (the Staged
// section unstages), and why is a refusal ("" = go ahead) worded for the status
// bar. The rules are the panel's own (canStageHunks / canUnstageHunks), judged
// on this file rather than on whatever the panel behind the view has selected.
func (m Model) hunkFileHere() (f model.FileStatus, staged bool, why string) {
	v := m.diffLayer()
	if v == nil {
		return f, false, i18n.T("hunks: no file here")
	}
	switch m.diffNav {
	case diffNavStatus:
	case diffNavStaged:
		staged = true
	default:
		return f, false, i18n.T("hunks: only a working-tree file can be staged")
	}
	if v.stk != nil {
		sf := v.stk.files[v.curFile()]
		if sf.conflict {
			return sf.fs, staged, i18n.T("conflicted — [enter] opens the resolver")
		}
		f = sf.fs
	} else {
		fs, ok := m.statusFileOf(v.title)
		if !ok {
			return f, staged, i18n.T("hunks: this file is no longer in the working tree")
		}
		f = fs
	}
	if !m.opsIdle() {
		return f, staged, i18n.T("hunks: an operation is running")
	}
	switch {
	case f.Kind == model.KindUntracked:
		return f, staged, i18n.T("hunks: an untracked file is staged whole")
	case f.Kind == model.KindUnmerged:
		return f, staged, i18n.T("conflicted — [enter] opens the resolver")
	case staged && f.Staged == 'A':
		// StageHunks can only set index content, never remove the entry: a file
		// not yet in HEAD is unstaged whole (the Staged panel's own rule).
		return f, staged, i18n.T("hunks: a newly added file is unstaged whole")
	}
	return f, staged, ""
}

// statusFileOf finds a path in the working-tree status.
func (m Model) statusFileOf(path string) (model.FileStatus, bool) {
	for _, f := range m.status.Files {
		if f.Path == path {
			return f, true
		}
	}
	return model.FileStatus{}, false
}

// diffHunkKey is H in the diff view: open the hunk picker over this file.
func (m Model) diffHunkKey() (tea.Model, tea.Cmd) {
	f, staged, why := m.hunkFileHere()
	if why != "" {
		m.statusMsg = why
		return m, nil
	}
	if staged {
		return m, m.loadUnstageHunksCmd(f.Path)
	}
	return m, m.loadStageHunksCmd(f.Path)
}
