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

// hunkKeyApplies reports whether H belongs on this view at all: any
// working-tree diff, staged or unstaged. It gates the footer chip — per-file
// refusals are the notice's job, a chip that flickered as the cursor moved
// between files would only be noise.
func (m Model) hunkKeyApplies() bool {
	return m.diffLayer() != nil && (m.diffNav == diffNavStatus || m.diffNav == diffNavStaged)
}

// diffHunkRow is H's . menu row — offered wherever the key applies, so the
// gesture stays discoverable on the single-file line, where the footer has no
// column left for it.
func (m Model) diffHunkRow() (actionRow, bool) {
	if !m.hunkKeyApplies() {
		return actionRow{}, false
	}
	label := i18n.T("Stage hunks of this file…")
	if m.diffNav == diffNavStaged {
		label = i18n.T("Unstage hunks of this file…")
	}
	return actionRow{id: "diff-hunks", key: "H", label: label}, true
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

// hunkReload is a diff the staging round OWES a re-read. A stack reconciles
// itself on every status write (reconcileStatusStack), but a SINGLE-file
// working-tree diff has nothing of the kind — nothing reloads one on a status
// change at all. So the picker's apply parks the file here and the next status
// write consumes it. Parked rather than unconditional on purpose: reloading on
// every status write would throw the reader to the top of the file whenever a
// watch-driven refresh landed.
type hunkReload struct {
	path   string
	staged bool
}

// armHunkReload parks a re-read of the open single-file working-tree diff.
func (m Model) armHunkReload(path string, staged bool) Model {
	if v := m.diffLayer(); v == nil || v.stk != nil {
		return m // a stack re-reads the file in place, keeping the reader's spot
	}
	m.hunkReload = &hunkReload{path: path, staged: staged}
	return m
}

// takeHunkReload consumes a parked reload against the status just written: the
// file is re-read where it is still in its section, and the diff closes when it
// is not (fully staged, or gone) rather than showing a diff that no longer
// exists.
func (m Model) takeHunkReload() (Model, tea.Cmd) {
	r := m.hunkReload
	if r == nil {
		return m, nil
	}
	m.hunkReload = nil
	v := m.diffLayer()
	if v == nil || v.stk != nil {
		return m, nil
	}
	f, ok := m.statusFileOf(r.path)
	if !ok || (r.staged && !inStagedPanel(f)) || (!r.staged && !inFilesPanel(f)) {
		m = m.popLayer()
		m.diffTag = ""
		m.statusMsg = i18n.T("%s has no change left here", r.path)
		return m, nil
	}
	return m, m.loadStatusDiffCmd(f, r.staged)
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
