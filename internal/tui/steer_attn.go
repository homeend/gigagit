package tui

import (
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/steer"
	"github.com/homeend/gigagit/internal/textdiff"
)

// attentionKey addresses one file's marks the way a note address does: the
// path plus the target it lives in, so the same path in the index, the working
// tree and a commit keep separate bands.
type attentionKey struct{ path, state, commit string }

// steerMark is one band: an inclusive 1-based range on one side, in one tone.
type steerMark struct {
	side       string
	start, end int
	tone       string
}

// fileStateProto maps a file state onto the protocol string a steering command
// carries. It is the inverse of the web's noteState allowlist.
func fileStateProto(s model.FileState) string {
	switch s {
	case model.StateStaged:
		return "staged"
	case model.StateUntracked:
		return "untracked"
	case model.StateCommitted:
		return "commit"
	default:
		return "unstaged"
	}
}

// attnKeyFor derives a mark key from an open diff's note address. A view with
// no address (a compare, a shelf file) can carry no marks — it has no stable
// identity to key them by.
func attnKeyFor(a model.FileAddress) (attentionKey, bool) {
	if a.Path == "" {
		return attentionKey{}, false
	}
	return attentionKey{path: a.Path, state: fileStateProto(a.State), commit: a.Commit}, true
}

// steerAttnKey derives a mark key from a command's file + target.
func steerAttnKey(c steer.Command) (attentionKey, bool) {
	if c.File == "" {
		return attentionKey{}, false
	}
	k := attentionKey{path: c.File, state: "unstaged"}
	if c.Target != nil {
		if c.Target.State != "" {
			k.state = c.Target.State
		}
		k.commit = c.Target.Commit
	}
	return k, true
}

// attnMarkFor reports the band style row r wears in view v, if any. The first
// matching mark wins: two overlapping bands from one agent are its own business,
// and blending them would produce a colour it never asked for.
func (m Model) attnMarkFor(v *diffView, r textdiff.Row) (lipgloss.Style, bool) {
	if len(m.attention) == 0 || v == nil {
		return lipgloss.Style{}, false
	}
	k, ok := attnKeyFor(v.noteAddr)
	if !ok {
		return lipgloss.Style{}, false
	}
	for _, mk := range m.attention[k] {
		no := r.RightNo
		if mk.side == "old" {
			no = r.LeftNo
		}
		if no == 0 || no < mk.start || no > mk.end {
			continue
		}
		if s, ok := attnStyle(mk.tone); ok {
			return s, true
		}
	}
	return lipgloss.Style{}, false
}

// steerHighlight records one band. Every field is validated here — the values
// come from an agent, and an unrecognized tone must paint nothing rather than
// default to a colour nobody asked for.
func (m Model) steerHighlight(c steer.Command) (Model, tea.Cmd) {
	k, ok := steerAttnKey(c)
	if !ok {
		return m, m.answerSteer(c, steerFail(c, "highlight needs a file"))
	}
	side := c.Side
	if side == "" {
		side = "new"
	}
	if side != "new" && side != "old" {
		return m, m.answerSteer(c, steerFail(c, "unknown side "+strconv.Quote(c.Side)))
	}
	if _, ok := attnStyle(c.Tone); !ok {
		return m, m.answerSteer(c, steerFail(c, "unknown tone "+strconv.Quote(c.Tone)))
	}
	if c.Start < 1 {
		return m, m.answerSteer(c, steerFail(c, "start must be a 1-based line number"))
	}
	end := c.End
	if end == 0 {
		end = c.Start
	}
	if end < c.Start {
		return m, m.answerSteer(c, steerFail(c, "end is before start"))
	}
	if m.attention == nil {
		m.attention = map[attentionKey][]steerMark{}
	}
	m.attention[k] = append(m.attention[k], steerMark{side: side, start: c.Start, end: end, tone: c.Tone})
	m = m.steerNotice(i18n.T("▸ agent marked lines in %s", c.File))
	return m, m.answerSteer(c, steerOK(c, "marked "+c.File+" "+side+":"+strconv.Itoa(c.Start)+"-"+strconv.Itoa(end)+" as "+c.Tone))
}

// steerHighlightClear drops one file's marks, or every mark when the command
// names no file.
func (m Model) steerHighlightClear(c steer.Command) (Model, tea.Cmd) {
	if c.File == "" {
		m.attention = map[attentionKey][]steerMark{}
		m = m.steerNotice(i18n.T("▸ agent cleared its marks"))
		return m, m.answerSteer(c, steerOK(c, "cleared every mark"))
	}
	k, _ := steerAttnKey(c)
	next := make(map[attentionKey][]steerMark, len(m.attention))
	for key, marks := range m.attention {
		if key != k {
			next[key] = marks
		}
	}
	m.attention = next
	m = m.steerNotice(i18n.T("▸ agent cleared its marks"))
	return m, m.answerSteer(c, steerOK(c, "cleared the marks on "+c.File))
}

// steerReload re-reads the named sources, which re-resolves the open diff's
// notes exactly as the .-menu mutations do. An explicit reload also drops the
// attention marks: the agent is saying "look again", and a band whose range
// drifted under an edit is its own to re-post.
func (m Model) steerReload(c steer.Command) (Model, tea.Cmd) {
	names := c.Sources
	if len(names) == 0 {
		names = []string{"notes"}
	}
	var srcs []sourceKey
	all := false
	for _, n := range names {
		switch n {
		case "notes":
			srcs = append(srcs, srcNotes)
		case "status":
			srcs = append(srcs, srcStatus)
		case "all":
			all = true
		default:
			return m, m.answerSteer(c, steerFail(c, "unknown reload source "+strconv.Quote(n)))
		}
	}
	m.attention = map[attentionKey][]steerMark{}
	var cmd tea.Cmd
	if all {
		m, cmd = m.reloadAllCmd(reloadOpts{manual: true, hardFeed: true})
	} else {
		m, cmd = m.reloadSourcesCmd(srcs, reloadOpts{})
	}
	m = m.steerNotice(i18n.T("▸ agent asked for a reload"))
	return m, tea.Batch(cmd, m.answerSteer(c, steerOK(c, "reloaded "+strings.Join(names, ", "))))
}

// steerNotice posts a transient "an agent did this" line — on the diff view's
// notice line when one is open, otherwise on the status bar. Both clear on the
// next key, like every other notice: the user should see WHY the window moved,
// and then never think about it again.
func (m Model) steerNotice(s string) Model {
	if m.diffLayer() != nil {
		m.diffNotice = s
		return m
	}
	m.statusMsg = s
	return m
}
