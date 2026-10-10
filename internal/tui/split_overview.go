package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/agentdocs"
	"github.com/homeend/gigagit/internal/i18n"
)

// The overview pane (spec 2026-10-10-review-overview-split §3): the review's
// stored overview on the left of a split. Up/down step anchor to anchor and
// STEER the right pane to the anchor's target; enter steers and moves the
// focus right. Page keys and home/end scroll the text and leave the
// selection alone.

// overviewPane is a docPane over an overview document plus the split it
// steers.
type overviewPane struct {
	docPane
	sv *splitView
}

func (p *overviewPane) hint(Model) string {
	return i18n.T("[↑↓] anchor  [enter] open  [/] find  [y] copy  [.] menu")
}

func (p *overviewPane) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	d := p.d
	if d == nil || d.ov == nil {
		return p.docPane.update(m, msg)
	}
	// A line selection and a search come first, as in the viewer: enter on
	// a marked range copies it, a typed search takes every key.
	if nm, cmd, ok := m.previewSelectKey(msg); ok {
		return nm, cmd
	}
	if nm, cmd, ok := m.previewSearchKey(msg); ok {
		return nm, cmd
	}
	_, rows, _, _ := m.activePreview()
	switch msg.String() {
	case "down", "j":
		d.stepAnchor(1, rows)
		return m.steerSplit(p.sv)
	case "up", "k":
		d.stepAnchor(-1, rows)
		return m.steerSplit(p.sv)
	case "enter":
		nm, cmd := m.steerSplit(p.sv)
		p.sv.onLeft = false
		return nm, cmd
	case "y":
		return m, m.copyToClipboardCmd(i18n.T("copied"), d.ov.text)
	case "r":
		return m, nil // a stored overview has no store reference (A9)
	case "X", "ctrl+]":
		// The overview is what the split steers by: it leaves the screen the
		// way esc takes it (kept open in the background, only the switcher's x
		// closes it), never leaving a split with nothing to steer.
		return m.closeSplit(p.sv), nil
	}
	return p.docPane.update(m, msg)
}

// stepOverviewAnchor steps the overview's anchor selection by dir and
// steers the right pane to it, leaving the focus where it is.
func (m Model) stepOverviewAnchor(sv *splitView, dir int) (Model, tea.Cmd) {
	ov := sv.left.doc()
	if ov == nil || ov.ov == nil {
		return m, nil
	}
	rows, _ := sv.geomOf(m, sv.left)
	ov.stepAnchor(dir, rows)
	return m.steerSplit(sv)
}

// anchorSource is where an overview's anchors open: the reviewed tip, or
// the working tree for a working review.
func anchorSource(ov *openFile) fileSource {
	if ov.ov.tip == "" {
		return fileSource{kind: srcWorktree}
	}
	return fileSource{kind: srcCommit, rev: ov.ov.tip}
}

// steerSplit shows the overview's selected anchor on the right pane: a
// file anchor's place, or a note anchor's note (anchorPlace).
func (m Model) steerSplit(sv *splitView) (Model, tea.Cmd) {
	ov := sv.left.doc()
	if ov == nil || ov.ov == nil || ov.ov.sel < 0 || ov.ov.sel >= len(ov.ov.anchors) {
		return m, nil
	}
	a := ov.ov.anchors[ov.ov.sel]
	path, line, dest, ok := m.anchorPlace(ov, ov.ov.sel)
	if !ok {
		if a.target.Note != "" && ov.src.kind == srcOverview {
			m.statusMsg = i18n.T("note %s is gone", a.target.Note)
		}
		return m, nil
	}
	var cmd tea.Cmd
	switch r := sv.right.(type) {
	case *docPane:
		m, cmd = m.docSteer(sv, r, ov, anchor{dest: dest, target: anchorTarget(path, line)})
	case *stackPane:
		m, cmd = m.stackSteer(sv, r, ov.ov.sel)
	}
	k, n := 0, 0 // the anchor's place among those a steer can reach
	for i, x := range ov.ov.anchors {
		if !steerable(ov, x) {
			continue
		}
		if n++; i == ov.ov.sel {
			k = n
		}
	}
	label := a.target.Label
	if label == "" {
		label = a.dest
	}
	if m.statusMsg == "" || !strings.HasPrefix(m.statusMsg, i18n.T("no file %s", a.target.Path)) {
		m.statusMsg = i18n.T("anchor %d/%d · %s", k, n, label)
	}
	return m, cmd
}

// steerable reports whether a steer can reach anchor a of overview ov: a
// file anchor, or a note anchor of an agent's overview (a stored overview's
// note anchors are plain).
func steerable(ov *openFile, a anchor) bool {
	return !a.plain && (a.target.Path != "" || a.target.Note != "" && ov.src.kind == srcOverview)
}

// anchorPlace is where overview anchor i opens in a split: a file anchor's
// path and line with its band (dest), or a note anchor's file and first line
// with no band (dest "" — a note is no band, as openAnchor has it). ok false:
// a plain anchor, a stored overview's note, or a note that is gone — marked
// missing.
func (m Model) anchorPlace(ov *openFile, i int) (path string, line int, dest string, ok bool) {
	a := &ov.ov.anchors[i]
	if !steerable(ov, *a) {
		return "", 0, "", false
	}
	if id := a.target.Note; id != "" {
		d, n := m.findFileNote(id)
		if n == nil {
			a.missing = true
			if m.docs != nil {
				m.docs.SetAnchorMissing(ov.id(), i, true)
			}
			ov.ov.paint(ov.p.lines)
			return "", 0, "", false
		}
		return d.path, n.Start, "", true
	}
	return a.target.Path, a.target.Start, a.dest, true
}

// docSteer is the single-mode steer: the anchor's file at the tip in the
// right pane, the cursor on its line, the anchor's band current. The same
// file lands without a reload; another file replaces the pane's document,
// which stays open in the background. A file never opened is read off the
// UI thread; liveDoc finds the pane when the lines arrive.
func (m Model) docSteer(sv *splitView, r *docPane, ov *openFile, a anchor) (Model, tea.Cmd) {
	src, path := anchorSource(ov), a.target.Path
	rows, _ := sv.geomOf(m, r)
	land := func(d *openFile) {
		d.from, d.anchorCur = ov, a.dest
		d.pendingLine, d.pendingEnd = a.target.Start, 0 // a band, never a selection
	}
	if d := r.d; d != nil && d.key() == docKey(src, path) {
		land(d)
		if docLoaded(d) {
			d.landPendingLine(rows)
			return m, nil
		}
		return m, m.loadDoc(d)
	}
	d := m.openFiles.find(m.currentWorktree, docKey(src, path))
	if d == nil {
		d = m.newOpenFile(src, path)
	} else {
		m = m.detachDoc(d) // out of any other frame (never this split: the key differs)
	}
	if old := r.d; old != nil {
		old.backgrounded = true
	}
	d.backgrounded = true // esc on the split steps aside; only X closes
	land(d)
	r.d = d
	m = m.registerDoc(d)
	if docLoaded(d) && !d.onDisk() {
		d.landPendingLine(rows)
		return m, nil
	}
	d.keepPlace()
	return m, m.loadDoc(d)
}

// landOnOverviewRow puts the review tree's cursor on its ≡ Overview row —
// where the split was opened from.
func (m Model) landOnOverviewRow() Model {
	if m.filesView == nil {
		return m
	}
	for i, l := range m.filesView.visible() {
		if l.overviewDoc {
			m.filesView.sel = i
			break
		}
	}
	return m
}

// openReviewOverviewSplit is enter on the review tree's ≡ Overview row: the
// overview on the left, its selected anchor's file on the right (stacked
// when the user chose so), the focus on the overview. Too narrow a terminal
// opens the full-screen overview viewer instead.
func (m Model) openReviewOverviewSplit() (Model, tea.Cmd) {
	st := m.filesReview
	if st == nil || st.overview == nil {
		return m, nil
	}
	if w, _ := m.overlayDims(); w < splitMinWidth {
		return m.openReviewOverviewDoc()
	}
	lw, _, bh := m.splitGeom()
	rows, inner := paneGeom(lw, bh)
	return m.openOverviewSplit(m.reviewOverviewDoc(st, rows, inner))
}

// reviewOf is the open review view whose stored overview ov is, or nil.
func (m Model) reviewOf(ov *openFile) *reviewViewState {
	if st := m.filesReview; st != nil && ov != nil && ov.src.kind == srcReviewOverview && ov.src.rev == st.id {
		return st
	}
	return nil
}

// showOverview is the door an overview comes to the front through (spec
// 2026-10-10-overview-split-all §3): the split on a terminal 60 columns or
// wider, the full-screen viewer below.
func (m Model) showOverview(d *openFile) (Model, tea.Cmd) {
	if w, _ := m.overlayDims(); w >= splitMinWidth {
		return m.openOverviewSplit(d)
	}
	m = m.detachDoc(d)
	m = m.pushLayer(&fileViewer{d})
	return m.registerDoc(d), nil
}

// openOverviewSplit puts overview ov on the left of a split, its selected
// anchor's place on the right (stacked when the user chose so), the focus
// on the overview. A split already showing ov is raised instead.
func (m Model) openOverviewSplit(ov *openFile) (Model, tea.Cmd) {
	if sv := m.splitShowing(ov); sv != nil && sv.left.doc() == ov {
		m = m.removeLayer(sv)
		sv.onLeft = true
		return m.pushLayer(sv), nil
	}
	lw, _, bh := m.splitGeom()
	rows, inner := paneGeom(lw, bh)
	m = m.detachDoc(ov) // a viewer still showing it gives it up
	if ov.ov.w != inner || ov.ov.mode != ov.p.mode {
		ov.layOut(rows, inner)
	}
	sv := &splitView{onLeft: true}
	sv.left = &overviewPane{docPane: docPane{d: ov, stackable: true}, sv: sv}
	sv.right = &docPane{stackable: true}
	if m.reviewStacked {
		sv.right = m.newStackPane(sv)
	}
	sv.onClose = func(m Model) Model {
		ov.backgrounded = true
		if rd := sv.right.doc(); rd != nil {
			rd.backgrounded = true
		}
		if m.reviewOf(ov) != nil {
			return m.landOnOverviewRow()
		}
		return m
	}
	m = m.pushLayer(sv)
	m = m.registerDoc(ov)
	if ov.ov.sel < 0 {
		ov.stepAnchor(1, rows)
	}
	return m.steerSplit(sv)
}

// anchorTarget is a place in a file as an anchor target: path at line
// (1-based; 0 = the top).
func anchorTarget(path string, line int) agentdocs.Anchor {
	return agentdocs.Anchor{Path: path, Start: line}
}
