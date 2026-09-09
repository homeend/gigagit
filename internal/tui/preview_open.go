package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
)

// previewOpenState records which preview the compare view is showing, so a
// previews refresh can tell whether the tips moved (re-arm) or the pair
// stopped being previewable (close). id is "" for a one-off "show once".
type previewOpenState struct {
	id, source, target string
	srcHash, tgtHash   string
	tag                string // the compare tag opened with
	keepPath           string // path to re-select after a re-arm ("" = top)
}

// previewOpenMsg carries a resolved pair back to the UI thread. gen is the
// files-view generation the resolve was dispatched under (closeFilesView bumps
// it): a result from before the view was closed — esc, a repo switch, another
// surface taking the left column — must be dropped, never re-open the view
// behind the user's back.
type previewOpenMsg struct {
	id, source, target string
	keepPath           string
	gen                int
	eps                domain.PreviewEndpoints
	err                error
}

func previewTitle(source, target string) string {
	return i18n.T("Merge preview: %s → %s", source, target)
}

// openPreviewCmd resolves the pair off the UI thread. Always dispatch a
// previewOpenMsg through here: it stamps the current previewGen, and a
// hand-built message would be dropped by the handler's gen check.
func (m Model) openPreviewCmd(id, source, target, keepPath string) tea.Cmd {
	svc, gen := m.svc, m.previewGen
	return func() tea.Msg {
		eps, err := svc.PreviewOpen(context.Background(), source, target)
		return previewOpenMsg{id: id, source: source, target: target, keepPath: keepPath, gen: gen, eps: eps, err: err}
	}
}

// chainPreviewsRead is the branches/remotes → previews refresh chain: a tip
// that moved changes every saved pair whose source or target it is (and may
// have moved the open preview out from under the compare view), and previews
// are never interval-polled, so the arrival of new tips is what refreshes them.
//
// The read inherits the manual flag of a previews read already in flight:
// superseding a manual read with a silent one would strand srcLoading[previews]
// (the superseded message early-returns on the gen check BEFORE srcLoading is
// cleared), leaving m.loading — and every action guard that reads it — stuck.
func (m Model) chainPreviewsRead() (Model, tea.Cmd) {
	return m.reloadSourcesCmd([]sourceKey{srcPreviews}, reloadOpts{manual: m.srcLoading[srcPreviews]})
}

// previewStateNotice is the status line for a pair that cannot open.
func previewStateNotice(source, target string, st domain.PreviewState) string {
	switch st {
	case domain.PreviewMerged:
		return i18n.T("%s is already merged into %s", source, target)
	case domain.PreviewMissingSource:
		return i18n.T("missing: %s", source)
	case domain.PreviewMissingTarget:
		return i18n.T("missing: %s", target)
	case domain.PreviewNoBase:
		return i18n.T("no common base")
	}
	return ""
}

// handlePreviewOpenMsg opens (or re-opens) the compare view for the pair.
func (m Model) handlePreviewOpenMsg(msg previewOpenMsg) (Model, tea.Cmd) {
	if msg.gen != m.previewGen {
		return m, nil // the view was closed (or replaced) after this resolve started
	}
	if msg.err != nil {
		m.statusMsg = i18n.T("error: %s", msg.err.Error())
		return m, nil
	}
	if msg.eps.Summary.State != domain.PreviewOK {
		m.statusMsg = previewStateNotice(msg.source, msg.target, msg.eps.Summary.State)
		if m.previewOpen != nil && m.filesView != nil {
			m = m.closePreviewView() // was open: the pair stopped being previewable
		}
		return m, nil
	}
	tag := compareTagFor(msg.eps.Left, msg.eps.Right)
	if m.previewOpen != nil && m.filesView != nil && m.compareTag == tag {
		return m, nil // same tips already showing
	}
	if m.previewOpen != nil && m.filesView != nil {
		m.compareTag = "" // defeat the same-tag guard: a re-arm must reload
	}
	var cmd tea.Cmd
	m, cmd = m.openCompareFiles(msg.eps.Left, msg.eps.Right)
	m.filesTitle = previewTitle(msg.source, msg.target)
	m.filesContext = m.filesTitle
	// openCompareFiles ran closeFilesView (which clears previewOpen and bumps
	// previewGen), so the state is armed after it — and the generation is
	// restored to the one this resolve carried, so a second open dispatched in
	// the same window (a quick enter on another row) is still honoured.
	m.previewGen = msg.gen
	m.previewOpen = &previewOpenState{
		id: msg.id, source: msg.source, target: msg.target,
		srcHash: msg.eps.Summary.SourceHash, tgtHash: msg.eps.Summary.TargetHash,
		tag: tag, keepPath: msg.keepPath,
	}
	return m, cmd
}

// previewSelectedPath is the file path under the tree cursor, or "" when the
// cursor is on a heading/placeholder. filesViewSelectedLine is not usable here:
// it refuses compare mode (its consumers are the single-commit preview keys),
// and a preview IS a compare.
func (m Model) previewSelectedPath() string {
	if m.filesView == nil {
		return ""
	}
	vis := m.filesView.visible() // filter-aware: sel indexes the visible rows
	if m.filesView.sel < 0 || m.filesView.sel >= len(vis) {
		return ""
	}
	return vis[m.filesView.sel].path
}

// afterPreviewsRefresh runs after srcPreviews lands: an open preview whose
// tips moved re-opens itself (keeping the selected file when it still
// exists); one whose pair vanished or stopped being ok closes with a notice.
func (m Model) afterPreviewsRefresh() (Model, tea.Cmd) {
	po := m.previewOpen
	if po == nil || m.filesView == nil {
		return m, nil
	}
	if po.id != "" {
		found := false
		for _, r := range m.previews {
			if r.rec.ID != po.id {
				continue
			}
			found = true
			if r.sum.State != domain.PreviewOK {
				m = m.closePreviewView()
				m.statusMsg = previewStateNotice(po.source, po.target, r.sum.State)
				return m, nil
			}
			if r.sum.SourceHash == po.srcHash && r.sum.TargetHash == po.tgtHash {
				return m, nil // unchanged
			}
			moved := po.source
			if r.sum.SourceHash == po.srcHash {
				moved = po.target
			}
			m.statusMsg = i18n.T("preview updated: %s moved", moved)
		}
		if !found {
			m = m.closePreviewView()
			m.statusMsg = i18n.T("preview removed")
			return m, nil
		}
	}
	// A transient ("show once", id == "") preview has no row: re-resolve
	// and let handlePreviewOpenMsg's same-tag check decide (unchanged tips
	// build the same tag and are a no-op).
	return m, m.openPreviewCmd(po.id, po.source, po.target, m.previewSelectedPath())
}

// closePreviewView closes the compare view the way esc does: focus returns
// to the panel that opened it (filesReturnFocus), falling back to the active
// left tab when that panel is not visible (a "show once" opened from the
// Branches tab must not strand focus on a hidden tab).
func (m Model) closePreviewView() Model {
	ret := m.filesReturnFocus
	m = m.closeFilesView()
	if m.layout().boxH[ret] <= 0 {
		ret = m.activeLeftTab
	}
	m.focus = ret
	return m
}
