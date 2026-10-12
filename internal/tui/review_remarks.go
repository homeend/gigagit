package tui

import (
	"context"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/agentdocs"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
)

// A review's remarks beside the code (spec 2026-10-10-overview-split-all
// §6): a file a review's overview steered to carries that review's remarks,
// drawn as read-only note boxes, for as long as the overview is its way
// back (d.from) — the rule its anchor bands follow.

// reviewNotesMsg is a fetch of review's remarks on the document tagged tag.
type reviewNotesMsg struct {
	slotStamp
	tag, review string
	notes       []domain.ResolvedNote
	err         error
}

// remarkBoxes are a review's remarks as note boxes: their range, words and
// author; the id keeps the review prefix that makes them read-only.
func remarkBoxes(ns []domain.ResolvedNote) []agentdocs.Note {
	out := make([]agentdocs.Note, 0, len(ns))
	for _, n := range ns {
		out = append(out, agentdocs.Note{ID: n.Note.ID, Path: n.Note.Address.Path, Start: n.Range[0], End: max(n.Range[1], n.Range[0]),
			Summary: n.Note.Summary, Rationale: n.Note.Rationale, Author: n.Note.Author,
			Meta: n.Note.Meta, Display: n.Note.Display, Added: n.Note.Added})
	}
	return out
}

// storedFrom is the stored overview whose remarks d may carry — its way
// back is a review's stored overview or a permanent overview still open:
// its kind and id; (0, "") otherwise.
func (d *openFile) storedFrom() (fileSourceKind, string) {
	if f := d.from; f != nil && f.ov != nil && !f.ov.closed && f.src.kind.storedOverview() {
		return f.src.kind, f.src.rev
	}
	return 0, ""
}

// dropWayBack forgets the overview d was opened from: the same file at the
// same commit opened another way (the Files view preview, a review's file)
// shows none of that overview's remarks or bands (R9). An anchor's open
// sets the way back again after its open (openAnchor).
func (d *openFile) dropWayBack() { d.from, d.anchorCur = nil, "" }

// reviewOfFrom is storedFrom's id: whose remarks d carries; "" for none.
func (d *openFile) reviewOfFrom() string {
	_, id := d.storedFrom()
	return id
}

// shownNotes is every note drawn in d: the agent's (the store's copy), then
// the review's remarks while they are d's, by start line. A stack's are its
// files' notes at their stream lines.
func (d *openFile) shownNotes() []agentdocs.Note {
	if d.stream != nil {
		return d.reviewNotes
	}
	var rn []agentdocs.Note
	if id := d.reviewOfFrom(); id != "" && id == d.reviewNotesOf {
		rn = d.reviewNotes
	}
	if len(rn) == 0 {
		return d.notes
	}
	if len(d.notes) == 0 {
		return rn
	}
	out := append(append([]agentdocs.Note(nil), d.notes...), rn...)
	sort.SliceStable(out, func(a, b int) bool { return out[a].Start < out[b].Start })
	return out
}

// boxNote is note n as its box and full view name it: in a stack, at its
// file's own lines (shownNotes holds stream lines); elsewhere n itself.
func (d *openFile) boxNote(n agentdocs.Note) agentdocs.Note {
	if d.diff != nil { // the diff holds notes at rows: the file's own lines
		if o, ok := d.rowNoteOrigin(n.ID); ok {
			return o
		}
	}
	if d.stream == nil {
		return n
	}
	if fi := d.stream.fileAt(n.Start - 1); fi >= 0 {
		off := d.stream.files[fi].body
		n.Start -= off
		n.End -= off
	}
	return n
}

// readOnlyNote: a review's remark, or any note in a stack.
func (d *openFile) readOnlyNote(n *agentdocs.Note) bool {
	return d.stream != nil || strings.HasPrefix(n.ID, model.ReviewNoteIDPrefix)
}

// readOnlyText is what d or r on a read-only note says.
func (d *openFile) readOnlyText() string {
	if d.stream != nil {
		return i18n.T("▸ notes are read-only in the stack — S opens the file")
	}
	return i18n.T("▸ review notes are read-only")
}

// reviewNotesCmd fetches d's review remarks off the UI thread once its
// lines are in; nil when d carries none to fetch.
func (m Model) reviewNotesCmd(d *openFile) tea.Cmd {
	kind, id := d.storedFrom()
	if id == "" || d.stream != nil || !docLoaded(d) || d.p.img != nil || m.svc == nil {
		return nil
	}
	svc, tag, path, lines, slot := m.svc, d.tag, d.path, rawOf(d.p.lines), m.stamp()
	return func() tea.Msg {
		ns, err := storedFileNotes(svc, kind, id, path, lines)
		return reviewNotesMsg{slotStamp: slot, tag: tag, review: id, notes: ns, err: err}
	}
}

// reviewNotesLanded adopts an answer still meant for its file: one for
// another review (the file was steered elsewhere meanwhile) is dropped.
func (m Model) reviewNotesLanded(msg reviewNotesMsg) Model {
	d := m.openFiles.findTag(msg.tag)
	if d == nil || msg.err != nil || d.reviewOfFrom() != msg.review {
		return m
	}
	d.reviewNotes, d.reviewNotesOf = remarkBoxes(msg.notes), msg.review
	d.syncNoteRows()
	d.p.ensureCursorVisible(m.docRows(d)) // a box above the cursor must not push the steered line out
	return m
}

// storedFileNotes are a stored overview's remarks on path, whose text is
// lines: a permanent overview's, else its review's.
func storedFileNotes(svc *domain.Service, kind fileSourceKind, id, path string, lines []string) ([]domain.ResolvedNote, error) {
	if kind == srcCommitOverview {
		return svc.OverviewFileNotes(context.Background(), id, path, lines)
	}
	return svc.ReviewFileNotes(context.Background(), id, path, lines)
}
