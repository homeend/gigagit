package tui

import (
	"strconv"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/agentdocs"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/steer"
)

// The overview steer verbs (gg session overview add|set|list|show|rm). Like
// the note verbs they run past steerRefusal: an add the screen cannot take
// right now lands in the background instead of failing. Reply prose is
// English protocol text.

// anchorsCheckedMsg says the store checked an overview's anchors (off the UI
// thread), so the add or set that asked can be answered.
type anchorsCheckedMsg struct {
	tag    string
	cmd    steer.Command
	detail string
}

func (m Model) steerOverview(c steer.Command) (Model, tea.Cmd) {
	switch c.Cmd {
	case "overview_add", "overview_set", "overview_show", "overview_rm":
		// An overview id is this worktree's: an agent in another one names
		// some other overview.
		if c.Worktree != "" && !domain.SameCheckout(c.Worktree, m.snapshotWorktree) {
			return m, m.answerSteer(c, steerFail(c, "gg is showing worktree "+m.snapshotWorktree+", not "+c.Worktree))
		}
	}
	switch c.Cmd {
	case "overview_add":
		return m.steerOverviewAdd(c)
	case "overview_set":
		return m.steerOverviewSet(c)
	case "overview_list":
		r := steerOK(c, "")
		for _, d := range m.openFiles.list(m.currentWorktree) {
			if d.ov != nil {
				r.Overviews = append(r.Overviews, m.overviewProto(d, false))
			}
		}
		r.Detail = strconv.Itoa(len(r.Overviews)) + " overviews"
		return m, m.answerSteer(c, r)
	case "overview_show":
		d, why := m.findOverview(c.FileID)
		if d == nil {
			return m, m.answerSteer(c, steerFail(c, why))
		}
		r := steerOK(c, d.id())
		r.Overviews = []steer.Overview{m.overviewProto(d, true)}
		return m, m.answerSteer(c, r)
	case "overview_rm":
		d, why := m.findOverview(c.FileID)
		if d == nil {
			return m, m.answerSteer(c, steerFail(c, why))
		}
		m = m.closeDoc(d)
		return m, m.answerSteer(c, steerOK(c, "closed "+d.id()))
	}
	return m, m.answerSteer(c, steerFail(c, "unknown command "+strconv.Quote(c.Cmd)))
}

// findOverview is the current worktree's overview with id, or nil and why.
func (m Model) findOverview(id string) (*openFile, string) {
	for _, d := range m.openFiles.list(m.currentWorktree) {
		if d.ov != nil && d.src.kind == srcOverview && d.id() == id {
			return d, ""
		}
	}
	return nil, "no overview " + id
}

func (m Model) steerOverviewAdd(c steer.Command) (Model, tea.Cmd) {
	o, err := m.docs.AddOverview(domain.CheckoutKey(m.currentWorktree), m.currentWorktree, c.Title, c.Text)
	if err != nil {
		return m, m.answerSteer(c, steerFail(c, err.Error()))
	}
	d := newOverviewDocFrom(o)
	rows, inner := m.viewerGeom()
	d.layOut(rows, m.overviewWidth(inner))
	busy := m.steerRefusal()
	var detail string
	switch {
	case c.Background:
		detail = "added " + d.id() + " in the background"
	case busy != "":
		detail = "added " + d.id() + " in the background (" + busy + ")"
	default:
		m = m.pushLayer(&fileViewer{d})
		detail = "showing " + d.id()
	}
	status := m.statusMsg
	m, ev := m.registerDocEv(d)
	m.statusMsg = status // an agent's overview never takes over the status line
	if p := evictedPath(ev); p != "" {
		detail += "; closed " + p + " (" + strconv.Itoa(maxOpenFiles) + " files open)"
	}
	return m, m.checkAnchorsCmd(d, c, detail)
}

func (m Model) steerOverviewSet(c steer.Command) (Model, tea.Cmd) {
	d, why := m.findOverview(c.FileID)
	if d == nil {
		return m, m.answerSteer(c, steerFail(c, why))
	}
	o, err := m.docs.SetOverview(d.id(), c.Title, c.Text)
	if err != nil {
		return m, m.answerSteer(c, steerFail(c, err.Error()))
	}
	rows, inner := m.viewerGeom()
	d.adoptOverview(o, rows, m.overviewWidth(inner))
	return m, m.checkAnchorsCmd(d, c, "set "+d.id())
}

// checkAnchorsCmd has the store check d's anchors off the UI thread; the
// answer to c goes out once that is done (anchorsChecked).
func (m Model) checkAnchorsCmd(d *openFile, c steer.Command, detail string) tea.Cmd {
	docs, id, tag := m.docs, d.id(), d.tag
	return func() tea.Msg {
		docs.CheckAnchors(id)
		return anchorsCheckedMsg{tag: tag, cmd: c, detail: detail}
	}
}

// anchorsChecked paints what the store's check found and answers.
func (m Model) anchorsChecked(msg anchorsCheckedMsg) (Model, tea.Cmd) {
	d := m.openFiles.findTag(msg.tag)
	if d == nil || d.ov == nil {
		return m, m.answerSteer(msg.cmd, steerFail(msg.cmd, "the overview was closed before its anchors were checked"))
	}
	if o, ok := m.docs.Overview(d.id()); ok {
		rows, inner := m.viewerGeom()
		d.adoptOverview(o, rows, m.overviewWidth(inner))
	}
	r := steerOK(msg.cmd, msg.detail)
	r.Overviews = []steer.Overview{m.overviewProto(d, false)}
	return m, m.answerSteer(msg.cmd, r)
}

// overviewProto is d in its wire form; text adds the markdown.
func (m Model) overviewProto(d *openFile, text bool) steer.Overview {
	state := "background"
	if m.docShown(d) {
		state = "shown"
	}
	return agentdocs.OverviewWire(d.overviewCopy(), state, text)
}
