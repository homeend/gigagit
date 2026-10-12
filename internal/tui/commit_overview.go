package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
)

// Permanent overviews in the TUI (spec 2026-10-12-permanent-overviews §9):
// an overview stored on a commit opens as the overview split — the document
// on the left, the file AT ITS COMMIT on the right with the overview's
// remarks — through the same door every overview uses (showOverview).

// commitOverviewMsg is openCommitOverview's read.
type commitOverviewMsg struct {
	slotStamp
	id  string
	ov  domain.Overview
	doc domain.OverviewDoc
	err error
}

// commitOverviewKey is permanent overview id's open-file identity.
func commitOverviewKey(id string) (fileSource, string) {
	return fileSource{kind: srcCommitOverview, rev: id}, "overview-" + id + ".md"
}

// openCommitOverview reads overview id off the UI thread and shows it. An
// overview already open comes to the front instead.
func (m Model) openCommitOverview(id string) (Model, tea.Cmd) {
	if d := m.openFiles.find(m.currentWorktree, docKey(commitOverviewKey(id))); d != nil {
		return m.showOverview(d)
	}
	if m.svc == nil {
		return m, nil
	}
	svc, slot := m.svc, m.stamp()
	return m, func() tea.Msg {
		ctx := context.Background()
		msg := commitOverviewMsg{slotStamp: slot, id: id}
		if msg.ov, msg.err = svc.Overview(ctx, id); msg.err != nil {
			return msg
		}
		msg.doc, msg.err = svc.OverviewDocAt(ctx, id)
		return msg
	}
}

// onCommitOverview builds the open document and shows it.
func (m Model) onCommitOverview(msg commitOverviewMsg) (Model, tea.Cmd) {
	if msg.err != nil {
		m.statusMsg = i18n.T("overview: %s", msg.err.Error())
		return m, nil
	}
	src, path := commitOverviewKey(msg.id)
	if d := m.openFiles.find(m.currentWorktree, docKey(src, path)); d != nil {
		return m.showOverview(d) // opened twice before the first read landed
	}
	d := m.newOpenFile(src, path)
	d.title = i18n.T("Overview: %s", msg.ov.Title)
	d.p.prose, d.p.mode = true, modeScroll
	d.ov = &overview{text: msg.doc.Text, sel: -1, tip: msg.ov.Commit}
	lw, _, bh := m.splitGeom()
	rows, inner := paneGeom(lw, bh)
	if w, _ := m.overlayDims(); w < splitMinWidth {
		rows, inner = m.viewerGeom()
		inner = m.overviewWidth(inner)
	}
	d.layOut(rows, inner)
	if len(d.ov.anchors) == len(msg.doc.Anchors) { // one parser, one order
		for i := range d.ov.anchors {
			d.ov.anchors[i].plain = !msg.doc.Anchors[i].OK
		}
		d.ov.paint(d.p.lines)
	}
	d.stepAnchor(1, rows) // the first resolved anchor is current on open
	return m.showOverview(d)
}
