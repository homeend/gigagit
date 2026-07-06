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
	popupMax
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
	box := modalStyle.Width(popupResolveWidth(w, p.maximized, popupInnerWidth(w))).Render(b.String()) + "\n"
	return overlayCenter(clipToHeight(below, h), box, w, h)
}

// startExportFilePatch resolves a single file's patch within sha off-thread
// (ExportDefaultDir + FilePatch), then delivers patchResolvedMsg (reused).
func (m Model) startExportFilePatch(sha, path string) (Model, tea.Cmd) {
	svc := m.svc
	return m, func() tea.Msg {
		ctx := context.Background()
		data, name, err := svc.FilePatch(ctx, sha, path)
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

// exportFilePatchRow offers "Export this file's diff as patch" inside the diff
// view, but ONLY when the diff view is the FRONT surface showing a
// commit-vs-parent file diff: dv.rev is the commit, and NOT compare mode
// (compare-mode diffs also set dv.rev, but the patch would be commit-vs-parent,
// not the compared endpoints). A merge dv.rev is caught by the domain guard
// (surfaced as a status message).
//
// Also excluded in full-tree mode (m.inFullTree(), the "a" toggle): there the
// on-screen diff is commit-vs-WORKING-TREE even though dv.rev is still set to
// the commit, so a commit-vs-parent patch would silently diverge from what's
// displayed — violating the "exported patch == displayed diff" invariant.
// Also excluded for stash diffs (m.stashView != nil): dv.rev there isn't a
// plain commit-vs-parent diff either, and offering the row just leads to a
// confusing "merge commit" refusal from the domain guard.
//
// Deliberately uses m.topLayer() (the literal top of the stack), NOT
// m.diffLayer() (layerOf[*diffView], which scans top-down and returns a diff
// buried under a later push). Pressing h/b on a diff view pushes a
// *historyView/*blameView ON TOP of it without popping it (diff_view.go's "h"/
// "b" cases), so diffLayer() still finds the buried diff while a history/blame
// surface is genuinely front. Using diffLayer() here would leak this row onto
// that surface's . menu, acting on a diff the user can no longer see — the
// exact leak availableActions's onStackFile check (action_menu.go) guards
// against for the neighboring files-view rows.
func (m Model) exportFilePatchRow() (actionRow, bool) {
	if !m.opsIdle() {
		return actionRow{}, false
	}
	dv, ok := m.topLayer().(*diffView)
	if !ok || dv.rev == "" || m.inCompareMode() || m.inFullTree() || m.stashView != nil {
		return actionRow{}, false
	}
	sha, path := dv.rev, dv.title
	return actionRow{
		id:    "file-export-patch",
		label: "Export this file's diff as patch",
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m.startExportFilePatch(sha, path)
		},
	}, true
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
