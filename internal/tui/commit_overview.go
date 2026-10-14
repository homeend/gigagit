package tui

import (
	"context"
	"errors"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/steer"
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
	// steer is the ?overview= navigate this read answers (nil = none): the
	// read, not the lookup before it, says whether the overview opened.
	steer *steer.Command
}

// commitOverviewKey is permanent overview id's open-file identity.
func commitOverviewKey(id string) (fileSource, string) {
	return fileSource{kind: srcCommitOverview, rev: id}, "overview-" + id + ".md"
}

// openCommitOverview reads overview id off the UI thread and shows it —
// always read: an agent may have re-saved it (same commit + title replaces)
// since it was opened; an open copy of the same save comes to the front.
func (m Model) openCommitOverview(id string) (Model, tea.Cmd) {
	return m.openCommitOverviewFor(id, nil)
}

// openCommitOverviewFor is openCommitOverview answering steer c (nil =
// none) once the read lands.
func (m Model) openCommitOverviewFor(id string, c *steer.Command) (Model, tea.Cmd) {
	if m.svc == nil {
		return m, nil
	}
	svc, slot := m.svc, m.stamp()
	return m, func() tea.Msg {
		ctx := context.Background()
		msg := commitOverviewMsg{slotStamp: slot, id: id, steer: c}
		if msg.ov, msg.err = svc.Overview(ctx, id); msg.err != nil {
			return msg
		}
		msg.doc, msg.err = svc.OverviewDocAt(ctx, id)
		return msg
	}
}

// onCommitOverview builds the open document and shows it, answering the
// navigate that asked for it.
func (m Model) onCommitOverview(msg commitOverviewMsg) (Model, tea.Cmd) {
	if msg.err != nil {
		m.statusMsg = i18n.T("overview: %s", msg.err.Error())
		if c := msg.steer; c != nil {
			return m, m.answerSteer(*c, steerFail(*c, "overview "+msg.id+": "+msg.err.Error()))
		}
		return m, nil
	}
	nm, cmd := m.showCommitOverview(msg)
	if c := msg.steer; c != nil {
		cmd = tea.Batch(cmd, nm.answerSteer(*c, steerOK(*c, "opened overview "+msg.id)))
	}
	return nm, cmd
}

func (m Model) showCommitOverview(msg commitOverviewMsg) (Model, tea.Cmd) {
	src, path := commitOverviewKey(msg.id)
	saved := msg.ov.Updated
	if saved.IsZero() {
		saved = msg.ov.Created
	}
	if d := m.openFiles.find(m.currentWorktree, docKey(src, path)); d != nil {
		if d.ov != nil && d.ov.saved.Equal(saved) {
			return m.showOverview(d) // the same save: the open copy comes to the front
		}
		m = m.closeDoc(d) // re-saved since it was read: the stale copy goes
		// Its files keep the remarks fetched for the old save under the same
		// id: drop them, so the next steer fetches the new ones.
		for _, f := range m.openFiles.list(m.currentWorktree) {
			if f.reviewNotesOf == msg.id {
				f.reviewNotes, f.reviewNotesOf = nil, ""
			}
		}
	}
	d := m.newOpenFile(src, path)
	d.title = i18n.T("Overview: %s", msg.ov.Title)
	d.p.prose, d.p.mode = true, modeScroll
	d.ov = &overview{text: msg.doc.Text, sel: -1, tip: msg.ov.Commit, saved: saved}
	if msg.ov.Doc != nil {
		d.ov.remarks = remarkLines(msg.ov.Doc.Files)
	}
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

// overviewHintMsg is steerNavigateOverview's lookup.
type overviewHintMsg struct {
	// svc is the repository the lookup ran against: a repo switch since
	// (reRoot replaces m.svc) makes the answer stale.
	svc   *domain.Service
	cmd   steer.Command
	found bool
	err   error
}

// steerNavigateOverview opens the permanent overview a ?overview= link
// names (gg session navigate, gg open, the # prompt). The overview is
// looked up in the store, not the loaded counts: an agent that just saved
// it links it at once. A gone overview opens its commit instead and says so.
func (m Model) steerNavigateOverview(c steer.Command) (Model, tea.Cmd) {
	svc := m.svc
	if svc == nil {
		return m, m.answerSteer(c, steerFail(c, "no repository is open"))
	}
	return m, func() tea.Msg {
		_, err := svc.Overview(context.Background(), c.HintID)
		if errors.Is(err, domain.ErrOverviewNotFound) {
			return overviewHintMsg{svc: svc, cmd: c}
		}
		return overviewHintMsg{svc: svc, cmd: c, found: err == nil, err: err}
	}
}

func (m Model) onOverviewHint(msg overviewHintMsg) (Model, tea.Cmd) {
	c := msg.cmd
	if msg.svc != m.svc {
		return m, m.answerSteer(c, steerFail(c, "the repository changed before the overview opened"))
	}
	if !msg.found {
		plain := c
		plain.HintKind, plain.HintID = "", ""
		nm, cmd := m.steerNavigate(plain)
		if msg.err == nil {
			nm.statusMsg = i18n.T("That overview no longer exists; opened its commit")
		} else {
			nm.statusMsg = i18n.T("overview: %s", msg.err.Error())
		}
		return nm, cmd
	}
	nm := m.steerToPanels()
	nm, open := nm.openCommitOverviewFor(c.HintID, &c)
	// Just saved behind this process: re-read the counts so its ≡ and rows
	// show — and so the open overview is in them before a reload could
	// judge it gone (dropGoneOverviews).
	nm.svc.InvalidateNoteCounts()
	var counts tea.Cmd
	nm, counts = nm.reloadSourcesCmd([]sourceKey{srcNotes}, reloadOpts{})
	return nm, tea.Batch(open, counts)
}
