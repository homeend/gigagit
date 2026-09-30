package tui

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/steer"
)

// The overview steer verbs (gg session overview add|set|list|show|rm). Like
// the note verbs they run past steerRefusal: an add the screen cannot take
// right now lands in the background instead of failing. Reply prose is
// English protocol text.

const (
	overviewMaxBytes       = 64 << 10
	overviewMaxPerWorktree = 20
	overviewMaxTitle       = 200
)

// anchorsCheckedMsg carries the stat of an overview's path anchors, made off
// the UI thread, back to answer the add or set that asked for it.
type anchorsCheckedMsg struct {
	tag     string
	missing map[string]bool // path anchor dest → its file is not there
	cmd     steer.Command
	detail  string
}

func (m Model) steerOverview(c steer.Command) (Model, tea.Cmd) {
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
		if d.ov != nil && d.id() == id {
			return d, ""
		}
	}
	return nil, "no overview " + id
}

// overviewText checks an add's or a set's text and title ("" = fine).
func overviewTextRefusal(c steer.Command, needTitle bool) string {
	switch {
	case strings.TrimSpace(c.Text) == "":
		return "an overview needs text"
	case len(c.Text) > overviewMaxBytes:
		return "the text is over 64 KiB"
	case needTitle && strings.TrimSpace(c.Title) == "":
		return "an overview needs a title"
	case strings.ContainsAny(c.Title, "\r\n") || utf8.RuneCountInString(c.Title) > overviewMaxTitle:
		return "the title must be one line of at most 200 characters"
	}
	return ""
}

func (m Model) overviewCount() int {
	n := 0
	for _, d := range m.openFiles.list(m.currentWorktree) {
		if d.ov != nil {
			n++
		}
	}
	return n
}

func (m Model) steerOverviewAdd(c steer.Command) (Model, tea.Cmd) {
	if c.Worktree != "" && !domain.SameCheckout(c.Worktree, m.snapshotWorktree) {
		return m, m.answerSteer(c, steerFail(c, "gg is showing worktree "+m.snapshotWorktree+", not "+c.Worktree))
	}
	if why := overviewTextRefusal(c, true); why != "" {
		return m, m.answerSteer(c, steerFail(c, why))
	}
	if m.overviewCount() >= overviewMaxPerWorktree {
		return m, m.answerSteer(c, steerFail(c, strconv.Itoa(overviewMaxPerWorktree)+" overviews are open; remove one first"))
	}
	d := newOverviewDoc(strings.TrimSpace(c.Title), c.Text)
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
	if why := overviewTextRefusal(c, false); why != "" {
		return m, m.answerSteer(c, steerFail(c, why))
	}
	ov := d.ov
	old := ""
	if ov.sel >= 0 {
		old = ov.anchors[ov.sel].dest
	}
	ov.text, ov.sel = c.Text, -1
	if t := strings.TrimSpace(c.Title); t != "" {
		d.title = t
	}
	rows, inner := m.viewerGeom()
	d.layOut(rows, m.overviewWidth(inner))
	for i, a := range ov.anchors {
		if old != "" && a.dest == old {
			d.selectAnchor(i, rows)
			break
		}
	}
	return m, m.checkAnchorsCmd(d, c, "set "+d.id())
}

// checkAnchorsCmd stats d's path anchors off the UI thread; the answer to c
// goes out once they are back (anchorsChecked).
func (m Model) checkAnchorsCmd(d *openFile, c steer.Command, detail string) tea.Cmd {
	wt, tag := m.currentWorktree, d.tag
	var paths []string
	for _, a := range d.ov.anchors {
		if a.target.path != "" {
			paths = append(paths, a.dest, a.target.path)
		}
	}
	return func() tea.Msg {
		missing := map[string]bool{}
		for i := 0; i < len(paths); i += 2 {
			st, err := os.Stat(filepath.Join(wt, filepath.FromSlash(paths[i+1])))
			missing[paths[i]] = err != nil || st.IsDir()
		}
		return anchorsCheckedMsg{tag: tag, missing: missing, cmd: c, detail: detail}
	}
}

// anchorsChecked marks the anchors that do not resolve — path anchors from
// the stat, note anchors against the open files now — and answers.
func (m Model) anchorsChecked(msg anchorsCheckedMsg) (Model, tea.Cmd) {
	d := m.openFiles.findTag(msg.tag)
	if d == nil || d.ov == nil {
		return m, m.answerSteer(msg.cmd, steerFail(msg.cmd, "the overview was closed before its anchors were checked"))
	}
	for i := range d.ov.anchors {
		a := &d.ov.anchors[i]
		if a.target.note != "" {
			_, n := m.findFileNote(a.target.note)
			a.missing = n == nil
		} else if gone, ok := msg.missing[a.dest]; ok {
			a.missing = gone
		}
	}
	d.ov.paint(d.p.lines)
	r := steerOK(msg.cmd, msg.detail)
	r.Overviews = []steer.Overview{m.overviewProto(d, false)}
	return m, m.answerSteer(msg.cmd, r)
}

// overviewProto is d in its wire form; text adds the markdown.
func (m Model) overviewProto(d *openFile, text bool) steer.Overview {
	o := steer.Overview{ID: d.id(), Title: d.title, State: "background", Anchors: len(d.ov.anchors)}
	if m.docShown(d) {
		o.State = "shown"
	}
	for _, a := range d.ov.anchors {
		if a.missing {
			o.Unresolved = append(o.Unresolved, a.dest)
		}
	}
	if text {
		o.Text = d.ov.text
	}
	return o
}
