package tui

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
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
	return m.pushLayer(newContentPopup(i18n.T("Notes on %s", msg.label), shelfNoteLines(msg.notes)))
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
