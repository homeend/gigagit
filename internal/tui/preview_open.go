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
	moved              string // ref whose tip moved; the "moved" notice, emitted only if the view really re-opens
	gen                int
	eps                domain.PreviewEndpoints
	set                domain.PreviewNoteSet // the note scope; zero when the pair is not ok
	counts             map[string]int        // per-path root-note counts for the file list
	err                error
}

func previewTitle(source, target string) string {
	return i18n.T("Merge preview: %s → %s", source, target)
}

// openPreviewCmd resolves the pair off the UI thread. Always dispatch a
// previewOpenMsg through here: it stamps the current previewGen, and a
// hand-built message would be dropped by the handler's gen check.
func (m Model) openPreviewCmd(id, source, target, keepPath string) tea.Cmd {
	return m.reopenPreviewCmd(id, source, target, keepPath, "")
}

// reopenPreviewCmd is openPreviewCmd carrying the name of the ref whose tip
// moved. The notice rides on the message instead of being written when the
// refresh notices the movement, because a moved tip does not always change
// what the user sees: a target commit off the fork point leaves merge-base and
// source tip — and so the whole target…source diff — untouched. Only the
// re-open path (a changed compare tag) says anything.
func (m Model) reopenPreviewCmd(id, source, target, keepPath, moved string) tea.Cmd {
	svc, gen := m.svc, m.previewGen
	return func() tea.Msg {
		eps, err := svc.PreviewOpen(context.Background(), source, target)
		msg := previewOpenMsg{
			id: id, source: source, target: target,
			keepPath: keepPath, moved: moved, gen: gen, eps: eps, err: err,
		}
		// The note scope rides the SAME resolve, so the file list paints its
		// badges in the first frame rather than after a second round trip.
		if err == nil && eps.Summary.State == domain.PreviewOK {
			if set, serr := svc.PreviewNotes(context.Background(), source, target); serr == nil {
				msg.set = set
				msg.counts, _, _ = svc.PreviewNoteCounts(context.Background(), set)
			}
		}
		return msg
	}
}

// chainPreviewsRead is every chained previews refresh: the branches/remotes
// arrival (a tip that moved changes every saved pair whose source or target it
// is, and may have moved the open preview out from under the compare view), the
// full-snapshot arm, and a store mutation. Previews are never interval-polled,
// so these chains are what refreshes them.
//
// Route ALL of them through here, never a plain reloadSourcesCmd: the read
// inherits the manual flag of a previews read already in flight, and
// superseding a manual read with a silent one would strand srcLoading[previews]
// (the superseded message early-returns on the gen check BEFORE srcLoading is
// cleared), leaving m.loading — and every action guard that reads it, r
// included — stuck for the rest of the session.
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
		if m.pendingPreviewFor(msg.source, msg.target) {
			return m.failPending("the merge preview failed to open: " + msg.err.Error())
		}
		return m, nil
	}
	// Is this message about the preview that is currently open? A resolve for
	// a DIFFERENT pair (a "show once" while a saved preview is open, another
	// row's enter) may report a notice, but must never close someone else's
	// view.
	po := m.previewOpen
	isOpen := po != nil && m.filesView != nil &&
		po.id == msg.id && po.source == msg.source && po.target == msg.target
	if msg.eps.Summary.State != domain.PreviewOK {
		m.statusMsg = previewStateNotice(msg.source, msg.target, msg.eps.Summary.State)
		if isOpen {
			m = m.closePreviewView() // the open pair stopped being previewable
		}
		if m.pendingPreviewFor(msg.source, msg.target) {
			return m.failPending(previewStateReason(msg.source, msg.target, msg.eps.Summary.State))
		}
		return m, nil
	}
	tag := compareTagFor(msg.eps.Left, msg.eps.Right)
	if isOpen && m.compareTag == tag {
		// The diff is unchanged, but a tip may still have moved (target commits
		// off the fork point move neither merge-base nor source). Reconcile the
		// hashes, silently: without this every later previews refresh would see
		// them differ, announce "moved" and spend another resolve, forever.
		po.srcHash, po.tgtHash = msg.eps.Summary.SourceHash, msg.eps.Summary.TargetHash
		// The notes may still have moved even when the diff did not; take the
		// fresh scope without reopening anything.
		if msg.set.OK() {
			set := msg.set
			m.filesPreviewSet, m.filesPreviewCounts = &set, msg.counts
		}
		return m, nil
	}
	// Always defeat openCompareFiles' same-tag guard: the view showing this
	// exact endpoint pair may be a branch compare (comparePair armed, its list
	// origin-filtered), which a preview must never inherit.
	m.compareTag = ""
	var cmd tea.Cmd
	m, cmd = m.openCompareFiles(msg.eps.Left, msg.eps.Right)
	m.filesTitle = previewTitle(msg.source, msg.target)
	m.filesContext = m.filesTitle
	// openCompareFiles ran closeFilesView (which clears previewOpen and bumps
	// previewGen), so the state is armed after it — and the generation is
	// restored to the one this resolve carried, so a second open dispatched in
	// the same window (a quick enter on another row) is still honoured.
	m.previewGen = msg.gen
	// Armed AFTER openCompareFiles: it ran closeFilesView, which clears both.
	if msg.set.OK() {
		set := msg.set
		m.filesPreviewSet = &set
		m.filesPreviewCounts = msg.counts
	}
	m.previewOpen = &previewOpenState{
		id: msg.id, source: msg.source, target: msg.target,
		srcHash: msg.eps.Summary.SourceHash, tgtHash: msg.eps.Summary.TargetHash,
		tag: tag, keepPath: msg.keepPath,
	}
	if msg.moved != "" { // a re-open the user can see: say which tip moved
		m.statusMsg = i18n.T("preview updated: %s moved", msg.moved)
	}
	return m, cmd
}

// previewStateReason is previewStateNotice's ENGLISH twin. A steer reply is
// protocol prose an agent parses and must never carry a translated string.
func previewStateReason(source, target string, st domain.PreviewState) string {
	switch st {
	case domain.PreviewMerged:
		return source + " is already merged into " + target
	case domain.PreviewMissingSource:
		return "missing branch " + source
	case domain.PreviewMissingTarget:
		return "missing branch " + target
	case domain.PreviewNoBase:
		return source + " and " + target + " have no common base"
	}
	return "the pair is not previewable"
}

// pendingPreviewFor reports whether a navigate is parked on THIS pair's open.
func (m Model) pendingPreviewFor(source, target string) bool {
	ps := m.pendingSteer
	return ps != nil && ps.stage == steerStagePreview && ps.source == source && ps.target == target
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
	moved := ""
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
				// The pair did not move, but its NOTES may have (this refresh
				// was chained off srcNotes). Take the fresh counts without
				// re-resolving or re-opening anything — without this the file
				// list badges and the }/{ step go stale in exactly the case
				// the chain exists for. A nil byPath means the counts READ
				// failed (every success path returns a non-nil map), so the
				// badges keep their last known values instead of vanishing on
				// a transient error.
				if r.byPath != nil {
					m.filesPreviewCounts = r.byPath
				}
				return m, nil
			}
			moved = po.source
			if r.sum.SourceHash == po.srcHash {
				moved = po.target
			}
		}
		if !found {
			m = m.closePreviewView()
			m.statusMsg = i18n.T("preview removed")
			return m, nil
		}
	}
	// A transient ("show once", id == "") preview has no row: re-resolve
	// and let handlePreviewOpenMsg's same-tag check decide (unchanged tips
	// build the same tag: it reconciles the hashes and says nothing).
	return m, m.reopenPreviewCmd(po.id, po.source, po.target, m.previewSelectedPath(), moved)
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
