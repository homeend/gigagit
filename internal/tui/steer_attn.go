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

// resolveAttnKey derives a mark key from a command and normalises a commit
// target onto the FEED's own hash — the string a commit diff's noteAddr
// carries. An agent may name a commit any way git does (a short hash, a ref),
// and two spellings of one commit must not key two different bands. The lookup
// is steerNavigateCommitFile's, and so is the refusal: a commit the feed does
// not hold would key a band nothing could ever match, which is worse than
// saying so. The reason is "" when the key is good.
func (m Model) resolveAttnKey(c steer.Command) (attentionKey, string) {
	k, ok := steerAttnKey(c)
	if !ok {
		return k, "highlight needs a file"
	}
	if k.state != "commit" {
		return k, ""
	}
	if k.commit == "" {
		return k, "target.state \"commit\" needs target.commit"
	}
	row, ok := m.steerCommitRow(k.commit)
	if !ok {
		return k, "commit not loaded in the feed"
	}
	k.commit = row.Hash
	return k, ""
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
	k, why := m.resolveAttnKey(c)
	if why != "" {
		return m, m.answerSteer(c, steerFail(c, why))
	}
	side := c.Side
	if side == "" {
		side = "new"
	}
	if side != "new" && side != "old" {
		return m, m.answerSteer(c, steerFail(c, "unknown side "+strconv.Quote(c.Side)))
	}
	// A preview's old side is the merge base: not addressable, the same refusal
	// the TUI's own `c` gives there. The scope is the TIP, not the open path —
	// every file at that commit shares the one merge base, so a band left on
	// another path would paint the moment the user stepped onto it, and with
	// only the file list open there would be no diff layer to catch it.
	// previewNoteScope reads the view stamp first, then filesPreviewSet.
	// A mark on any OTHER commit (or on the working tree) is the business of
	// the view that will paint it and still lands.
	if side == "old" && k.state == "commit" {
		if set := m.previewNoteScope(); set != nil && k.commit == set.Tip {
			return m, m.answerSteer(c, steerFail(c, "notes in a preview anchor on the new side"))
		}
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
	// A clear drops the resolved key AND the literal one, and never fails on a
	// commit the feed has since dropped: a refusal here would strand marks the
	// agent explicitly asked to take down, and the only way back would be the
	// whole-session clear.
	drop := map[attentionKey]bool{}
	if k, ok := steerAttnKey(c); ok {
		drop[k] = true
	}
	if k, why := m.resolveAttnKey(c); why == "" {
		drop[k] = true
	}
	next := make(map[attentionKey][]steerMark, len(m.attention))
	for key, marks := range m.attention {
		if !drop[key] {
			next[key] = marks
		}
	}
	m.attention = next
	m = m.steerNotice(i18n.T("▸ agent cleared its marks"))
	return m, m.answerSteer(c, steerOK(c, "cleared the marks on "+c.File))
}

// steerReload re-reads the named sources, which re-resolves the open diff's
// notes exactly as the .-menu mutations do. A `status`/`all` reload ALSO drops
// the attention marks: those rebuild the diff geometry the bands are anchored
// against, and a range that drifted under an edit is the agent's to re-post.
// A `notes`-only reload keeps them — every note mutation auto-posts one, so
// wiping there would make the documented highlight-then-note flow erase the
// band it had just painted, with no agent action at all.
func (m Model) steerReload(c steer.Command) (Model, tea.Cmd) {
	names := c.Sources
	if len(names) == 0 {
		names = []string{"notes"}
	}
	var srcs []sourceKey
	all := false
	dropMarks := false
	for _, n := range names {
		switch n {
		case "notes":
			srcs = append(srcs, srcNotes)
		case "status":
			srcs = append(srcs, srcStatus)
			dropMarks = true
		case "all":
			all = true
			dropMarks = true
		default:
			return m, m.answerSteer(c, steerFail(c, "unknown reload source "+strconv.Quote(n)))
		}
	}
	if dropMarks {
		m.attention = map[attentionKey][]steerMark{}
	}
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
