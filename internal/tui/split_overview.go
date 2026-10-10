package tui

import (
	tea "github.com/charmbracelet/bubbletea"

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
	}
	return p.docPane.update(m, msg)
}

// anchorSource is where an overview's anchors open: the reviewed tip, or
// the working tree for a working review.
func anchorSource(ov *openFile) fileSource {
	if ov.ov.tip == "" {
		return fileSource{kind: srcWorktree}
	}
	return fileSource{kind: srcCommit, rev: ov.ov.tip}
}

// steerSplit shows the overview's selected anchor on the right pane. A
// plain anchor is never selected, so there is always a target.
func (m Model) steerSplit(sv *splitView) (Model, tea.Cmd) {
	ov := sv.left.doc()
	if ov == nil || ov.ov == nil || ov.ov.sel < 0 || ov.ov.sel >= len(ov.ov.anchors) {
		return m, nil
	}
	a := ov.ov.anchors[ov.ov.sel]
	if a.plain || a.target.Note != "" || a.target.Path == "" {
		return m, nil
	}
	switch r := sv.right.(type) {
	case *docPane:
		return m.docSteer(sv, r, ov, a)
	}
	return m, nil
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
	d := m.reviewOverviewDoc(st, rows, inner)
	m = m.detachDoc(d) // a viewer still showing it (opened narrow before) gives it up
	sv := &splitView{onLeft: true}
	sv.left = &overviewPane{docPane: docPane{d: d}, sv: sv}
	sv.right = &docPane{}
	if m.reviewStacked {
		sv.right = m.newStackPane(sv, st)
	}
	sv.onClose = func(m Model) Model {
		d.backgrounded = true
		if rd := sv.right.doc(); rd != nil {
			rd.backgrounded = true
		}
		return m.landOnOverviewRow()
	}
	m = m.pushLayer(sv)
	m = m.registerDoc(d)
	return m.steerSplit(sv)
}

// newStackPane is Task 5's stacked file pane; single mode until it exists.
func (m Model) newStackPane(sv *splitView, st *reviewViewState) pane { return &docPane{} }
