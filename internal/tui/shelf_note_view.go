package tui

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

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
