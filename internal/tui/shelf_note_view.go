package tui

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
)

// Notes on a whole shelf entry — gg writes them when a set cannot carry what
// it annotates (a recycled worktree's deletions and renames). The switcher
// marks such rows ◆N and `n` reads them; there is no way to write one here.

// shelfNotesMsg carries one entry's notes, loaded off the UI thread.
type shelfNotesMsg struct {
	id, label string
	notes     []domain.ResolvedNote
	err       error
}

// shelfNoteCount is how many notes hang off entry id itself.
func (m Model) shelfNoteCount(id string) int { return m.noteCounts.ByShelf[id] }

// shelfNotesCmd loads the selected entry's notes; nil when it has none.
func (m Model) shelfNotesCmd(p *shelfPopup) tea.Cmd {
	e, ok := p.selected()
	if !ok || m.shelfNoteCount(e.ID) == 0 || m.svc == nil {
		return nil
	}
	svc, label := m.svc, shelfEntryDisplay(e)
	return func() tea.Msg {
		ns, err := svc.ShelfNotes(context.Background(), e.ID)
		return shelfNotesMsg{id: e.ID, label: label, notes: ns, err: err}
	}
}

// openShelfNotes pushes the read-only viewer over the switcher (esc returns
// to it — the layer-stack contract).
func (m Model) openShelfNotes(msg shelfNotesMsg) Model {
	if msg.err != nil {
		m.statusMsg = i18n.T("shelf: %s", msg.err.Error())
		return m
	}
	if len(msg.notes) == 0 {
		m.statusMsg = i18n.T("This shelf entry has no notes")
		return m
	}
	cp := newContentPopup(i18n.T("Notes on %s", msg.label), shelfNoteLines(msg.notes))
	cp.fitContent = true
	cp.prose = true
	return m.pushLayer(cp)
}

// shelfNoteLines renders each note: its summary as a heading, "author · date",
// then its text verbatim (the text is a list — never reflowed).
func shelfNoteLines(ns []domain.ResolvedNote) []contentLine {
	var out []contentLine
	for i, r := range ns {
		if i > 0 {
			out = append(out, contentLine{})
		}
		n := r.Note
		out = append(out, contentLine{text: n.Summary, heading: true, elide: true})
		meta := n.Created.Local().Format("2006-01-02 15:04")
		if n.Author != "" {
			meta = n.Author + " · " + meta
		}
		out = append(out, contentLine{text: meta, dim: true})
		for _, l := range strings.Split(strings.TrimRight(n.Rationale, "\n"), "\n") {
			if l != "" {
				out = append(out, contentLine{text: l, noWrap: true, elide: true})
			}
		}
	}
	return out
}

// loadShelfMemberDiffCmd diffs one member of a shelved set (old) against the
// working file (new). A member the working tree does not have is an ABSENT new
// side — the diff shows it removed — never an "open …: no such file" error. An
// empty member has nothing to show, so the view says what it is instead: for
// the recycle's placeholder, where to read what it stands for.
func (m Model) loadShelfMemberDiffCmd(id, path, subtitle, tag string) tea.Cmd {
	svc := m.svc
	differ := m.diffDiffer()
	body := m.diffBodyRows()
	v := &diffView{title: path, context: subtitle, compare: true, partial: m.diffPartial, long: m.diffLong}
	v.width, _ = m.overlayDims()
	return func() tea.Msg {
		ctx := context.Background()
		old, err := svc.ResolveBytes(ctx, model.FileRef{Source: model.SourceShelf, Locator: id, Path: path})
		if err != nil {
			v.err = err
			return diffMsg{tag: tag, view: v}
		}
		oldSrc := func(context.Context) ([]byte, error) { return old, nil }
		newSrc := domain.ByteSource(func(ctx context.Context) ([]byte, error) {
			return svc.ResolveBytes(ctx, model.FileRef{Source: model.SourceUnstaged, Path: path})
		})
		if present, perr := svc.WorktreeFilesPresent(ctx, []string{path}); perr == nil && !present[path] {
			newSrc = nil
		}
		if len(old) == 0 {
			v.notice = i18n.T("(empty file)")
			if path == domain.RecyclePlaceholder {
				v.notice = i18n.T("(an empty placeholder: every change in the recycled worktree was a deletion. Press n on this entry in the shelf to read which files were deleted.)")
			}
		}
		out, err := differ.Diff(ctx, domain.Request{Key: "", Path: path, Old: oldSrc, New: newSrc})
		if err != nil {
			v.err = err
			return diffMsg{tag: tag, view: v}
		}
		applyDiff(v, out, body)
		return diffMsg{tag: tag, view: v}
	}
}

// shelfNoteStackFile is note id of the open shelved set as a stack element: a
// prose block labelled by the note's summary, holding "author · date" and the
// note's text. The text is a list of paths, laid out once at the stack's width
// with each over-long line cut in its MIDDLE (the file name survives).
func (m Model) shelfNoteStackFile(id string) (stackFile, bool) {
	for _, r := range m.filesShelfNotes {
		if r.Note.ID != id {
			continue
		}
		n := r.Note
		w, _ := m.overlayDims()
		w -= 4
		meta := n.Created.Local().Format("2006-01-02 15:04")
		if n.Author != "" {
			meta = n.Author + " · " + meta
		}
		prose := []mdRow{{text: meta, cls: nil}, {}}
		for _, l := range strings.Split(strings.TrimRight(n.Rationale, "\n"), "\n") {
			prose = append(prose, mdRow{text: elidePath(sanitizeLine(l), w), pre: true})
		}
		return stackFile{overview: true, load: stackLoaded, label: sanitizeLine(n.Summary), prose: prose}, true
	}
	return stackFile{}, false
}

// elideNoteSummary fits a note's summary into n columns. A recycle's summary
// is "Recycled from <dir> (<branch>)", and a branch holds slashes: a trailing
// "(…)" group stays whole, so do the words before the path while there is
// room, and only the path between them loses its middle.
// Anything else is an ordinary path cut (elidePath).
func elideNoteSummary(s string, n int) string {
	if lipgloss.Width(s) <= n {
		return s
	}
	if i := strings.LastIndex(s, " ("); i > 0 && strings.HasSuffix(s, ")") {
		group := s[i:]
		if room := n - lipgloss.Width(group); room >= 2 {
			body := s[:i]
			// The words before the path ("Recycled from ") stay whole
			// while the path still gets a few columns of its own.
			if j := strings.IndexAny(body, `/\`); j > 0 {
				if left := room - lipgloss.Width(body[:j]); left >= 8 {
					return body[:j] + elidePath(body[j:], left) + group
				}
			}
			return elidePath(body, room) + group
		}
		// Not even the group fits: the worktree's name says more than a
		// sliver of the branch would ("…/x)").
		return elidePath(s[:i], n)
	}
	return elidePath(s, n)
}
