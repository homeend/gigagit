package tui

import (
	"context"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/engine"
)

// patchResolvedMsg carries a resolved patch (bytes + default destination path)
// back to the UI thread so the editable-destination popup can open prefilled.
// err is set when generating the patch failed (including ErrMergeCommitPatch).
type patchResolvedMsg struct {
	data        []byte
	defaultPath string
	err         error
}

// startExportCommitPatch resolves the whole-commit patch + default path
// off-thread (ExportDefaultDir + CommitPatch), then delivers patchResolvedMsg.
func (m Model) startExportCommitPatch(sha string) (Model, tea.Cmd) {
	svc := m.svc
	return m, func() tea.Msg {
		ctx := context.Background()
		data, name, err := svc.CommitPatch(ctx, sha)
		if err != nil {
			return patchResolvedMsg{err: err}
		}
		dir, err := svc.ExportDefaultDir(ctx)
		if err != nil {
			return patchResolvedMsg{err: err}
		}
		return patchResolvedMsg{data: data, defaultPath: filepath.Join(dir, name)}
	}
}

// exportPatchPopup is the editable-destination confirmation shown after a patch
// has been generated. dest is prefilled with <defaultDir>/<name>; enter runs
// engine.ExportFile with the (possibly edited) full path. Mirrors tempExportPopup.
type exportPatchPopup struct {
	dest textfield
	data []byte
}

func (p *exportPatchPopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	switch msg.Type {
	case tea.KeyEsc:
		m = m.popLayer()
	case tea.KeyEnter:
		path := strings.TrimSpace(p.dest.Value())
		if path == "" || len(p.data) == 0 {
			return m, nil
		}
		data := p.data
		m = m.popLayer()
		return m.startOp(engine.ExportFile{Path: path, Data: data})
	default:
		p.dest.HandleEditKey(msg)
	}
	return m, nil
}

func (p *exportPatchPopup) render(m Model, below string) string {
	w, h := m.overlayDims()
	var b strings.Builder
	b.WriteString("Export as patch\n\n")
	b.WriteString(viewField("path: ", p.dest, true, popupContentWidth(w)) + "\n\n")
	b.WriteString("[type] path  [enter] write  [esc] cancel")
	box := modalStyle.Width(popupInnerWidth(w)).Render(b.String()) + "\n"
	return overlayCenter(clipToHeight(below, h), box, w, h)
}

// commitExportPatchRow offers "Export commit as patch" on the Commits panel (and
// the commit-list side of a files view). Pre-hidden for merge commits: their
// patch would be wrong (domain refuses them, but hiding avoids a dead row).
func (m Model) commitExportPatchRow() (actionRow, bool) {
	if m.focus != panelCommits || !m.opsIdle() {
		return actionRow{}, false
	}
	bi, ok := m.backingIndex(panelCommits)
	if !ok {
		return actionRow{}, false
	}
	c := m.commits[bi]
	if len(c.Parents) > 1 {
		return actionRow{}, false // merge: format-patch would emit the wrong commit
	}
	sha := c.Hash
	return actionRow{
		id:    "commit-export-patch",
		label: "Export commit as patch",
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m.startExportCommitPatch(sha)
		},
	}, true
}
