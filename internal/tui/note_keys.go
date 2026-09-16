package tui

import (
	"context"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/textdiff"
)

// notesLoadedMsg carries the resolved notes for one open diff. tag gates a
// stale result exactly like diffMsg (the diff may have been stepped away).
type notesLoadedMsg struct {
	tag   string
	notes []domain.ResolvedNote
	err   error
}

// noteMutatedMsg reports the outcome of an add/edit/reply/remove.
type noteMutatedMsg struct{ err error }

// diffNoteAddress is the address notes hang off for the open diff. It is the
// field the LOADER stamped (diffView.noteAddr), never something derived from
// Model focus at key time: the focus cannot tell a staged diff from an unstaged
// one, and the stored State is the pair of texts the sweep re-reads — getting
// it wrong resolves the note against the wrong side and deletes it. A view no
// loader stamped (every two-sided compare, and the shelf/bookmark-vs-working
// diffs, whose old side is not any address's old side) has no address, so notes
// are inert there.
func (m Model) diffNoteAddress() (model.FileAddress, bool) {
	v := m.diffLayer()
	if v == nil || v.noteAddr.Path == "" {
		return model.FileAddress{}, false
	}
	return v.noteAddr, true
}

// previewNoteSet is the merge-preview scope of the diff on top, or nil. Like
// diffNoteAddress it reads the field the LOADER stamped, never Model state at
// key time.
func (m Model) previewNoteSet() *domain.PreviewNoteSet {
	v := m.diffLayer()
	if v == nil {
		return nil
	}
	return v.previewSet
}

// previewNoteScope is the preview a file-list gesture acts in: the OPEN diff's
// own stamp first (the rule everywhere else — the view's field, not Model state
// at key time), falling back to the files view's set when no diff is open.
// The top view's stamp wins even when it is NIL, on purpose: a commit diff
// pushed over a preview's file list is an ordinary two-sided diff with a
// perfectly addressable old side, so falling back to filesPreviewSet there
// would refuse a valid `c` — or a valid steer — on the view actually on screen.
func (m Model) previewNoteScope() *domain.PreviewNoteSet {
	if v := m.diffLayer(); v != nil {
		return v.previewSet
	}
	return m.filesPreviewSet
}

// previewPathGoneAtTip reports whether the compare file list marks path as
// deleted — in a preview that means the source tip no longer has the file, so
// nothing can anchor on its new side.
func (m Model) previewPathGoneAtTip(path string) bool {
	if m.filesView == nil {
		return false
	}
	for _, l := range m.filesView.visible() {
		if l.path == path {
			return l.status == "D"
		}
	}
	return false
}

// loadNotesCmd resolves this diff's notes off the UI thread. The rows are the
// SHARED cached rows: they are read, wrapped in a domain.Diff value and never
// mutated.
func (m Model) loadNotesCmd() tea.Cmd {
	v := m.diffLayer()
	if v == nil || m.svc == nil {
		return nil
	}
	// The address comes off the SAME view whose rows are resolved — a blame or
	// history layer pushed over the diff cannot redirect the read any more.
	addr := v.noteAddr
	if addr.Path == "" {
		return nil
	}
	svc, tag, rows := m.svc, m.diffTag, v.full
	// A preview gathers its notes along the branch and resolves them against
	// the tip's content; every other view reads the address's own notes.
	set := v.previewSet
	return func() tea.Msg {
		d := domain.Diff{Result: textdiff.Result{Rows: rows}}
		if set != nil {
			ns, err := svc.PreviewNotesFor(context.Background(), *set, addr.Path, d)
			return notesLoadedMsg{tag: tag, notes: ns, err: err}
		}
		ns, err := svc.NotesFor(context.Background(), addr, d)
		return notesLoadedMsg{tag: tag, notes: ns, err: err}
	}
}

// noteAnchorAtCursor is where `c` puts a new note: the cursor side's anchor
// (new side by default), or the other side's line when the cursor side is a
// gap (§4.1's cursorRow contract, sided by §4.7). The noteAnchor is one side a
// new note can hang off: the side, its line number and the fingerprint of that
// line's text.
type noteAnchor struct {
	side model.NoteSide
	line int
	hash string
}

// noteAnchorsAtCursor lists the anchors the cursor row offers — both when the
// row exists in both versions (a Same or Changed row), one on an Add or Del
// row. The CURSOR SIDE comes first (spec §4.7), so the popup's default pick,
// noteAnchorAtCursor and everything built on it (L, the . menu's Copy link)
// follow the side the user is reading; the picker still lists both. Nothing
// when the view has no cursor row.
func (m Model) noteAnchorsAtCursor() []noteAnchor {
	v := m.diffLayer()
	if v == nil {
		return nil
	}
	r, ok := v.cursorRow()
	if !ok {
		return nil
	}
	newSide := func() (noteAnchor, bool) {
		if r.RightNo <= 0 {
			return noteAnchor{}, false
		}
		return noteAnchor{model.NoteSideNew, r.RightNo, model.NoteContextHash([]string{r.Right})}, true
	}
	// A preview's old side is the MERGE BASE, which no stored address names,
	// so it offers no anchor at all — the same rule `gg review A..B` follows
	// (reviewImportTarget: ranges anchor to the tip, new side only). That holds
	// whichever side the cursor is on.
	oldSide := func() (noteAnchor, bool) {
		if r.LeftNo <= 0 || v.previewSet != nil {
			return noteAnchor{}, false
		}
		return noteAnchor{model.NoteSideOld, r.LeftNo, model.NoteContextHash([]string{r.Left})}, true
	}
	first, second := newSide, oldSide
	if v.onOld {
		first, second = oldSide, newSide
	}
	var out []noteAnchor
	if a, ok := first(); ok {
		out = append(out, a)
	}
	if a, ok := second(); ok {
		out = append(out, a)
	}
	return out
}

// noteAnchorAtCursor is the default anchor — the cursor side's anchor (new
// side by default) when the row has one — for callers that do not offer a
// choice.
func (m Model) noteAnchorAtCursor() (model.NoteSide, int, string, bool) {
	as := m.noteAnchorsAtCursor()
	if len(as) == 0 {
		return "", 0, "", false
	}
	return as[0].side, as[0].line, as[0].hash, true
}

// noteTarget is the ONE note E / R / Delete note act on. It names a single
// note (a root, or a reply when the root is hidden) together with its thread's
// root and anchor, so E edits exactly the row the user can see while R still
// replies into that row's thread.
type noteTarget struct {
	note    model.Note     // the targeted note itself (root or reply)
	rootID  string         // the thread root's id (== note.ID on a root)
	line    int            // the thread's resolved anchor line (popup heading)
	side    model.NoteSide // the thread's side
	hash    string         // the thread's context fingerprint
	replies int            // replies a delete would take along (0 unless note is the root)
}

// noteTargetIn picks the targetable note inside one thread: the first note
// whose OWN row survives the agent filter — the root, else the first visible
// reply. This is exactly dropAgentRows' per-ROW rule, so every row the user
// can see is targetable and no hidden row ever is. ok=false when the whole
// thread is filtered away (it then draws no rows either).
func (v *diffView) noteTargetIn(r domain.ResolvedNote) (noteTarget, bool) {
	shown := func(n model.Note) bool {
		return !v.hideAgent || n.Source != model.NoteSourceAgent
	}
	t := noteTarget{rootID: r.Note.ID, line: r.Range[1], side: r.Note.Side, hash: r.Note.ContextHash}
	if shown(r.Note) {
		// Deleting a root takes its replies, hidden ones included — the count
		// the confirmation quotes is the STORED thread, not the shown rows.
		t.note, t.replies = r.Note, len(r.Replies)
		return t, true
	}
	for _, rep := range r.Replies {
		if shown(rep.Note) {
			t.note = rep.Note
			return t, true
		}
	}
	return noteTarget{}, false
}

// notesAtCursor is the set of threads E/R/Delete may act on: the ones whose
// note rows sit NEXT to the cursor row. A thread's rows render under its
// anchor line, so that is every thread anchored on the cursor line itself
// (rows directly below the cursor), or — when the cursor line carries none —
// every thread anchored on the nearest real line above it (rows directly
// above the cursor). Both sides adjacent means the cursor line's own threads
// win. Anything further away is out of reach: the keys are inert and the .
// menu offers no note rows. Order follows v.notes (resolution order), so a
// chooser lists several threads on one line stably.
func (m Model) notesAtCursor() []noteTarget {
	v := m.diffLayer()
	if v == nil || len(v.notes) == 0 || v.curLine < 0 || v.curLine >= len(v.lines) {
		return nil
	}
	prev := -1
	for li := v.curLine - 1; li >= 0; li-- {
		if v.lines[li].Fold == 0 {
			prev = li
			break
		}
	}
	var at, above []noteTarget
	for _, r := range v.notes {
		t, ok := v.noteTargetIn(r)
		if !ok {
			continue
		}
		li, _ := v.noteAnchorLine(r)
		switch {
		case li < 0:
			// the anchor is not in this view at all
		case li == v.curLine:
			at = append(at, t)
		case li == prev:
			above = append(above, t)
		}
	}
	if len(at) > 0 {
		return at
	}
	return above
}

// noteNearCursor is the single note in reach (the first of notesAtCursor),
// for callers that only need to know whether the note rows are offered.
func (m Model) noteNearCursor() (noteTarget, bool) {
	ts := m.notesAtCursor()
	if len(ts) == 0 {
		return noteTarget{}, false
	}
	return ts[0], true
}

// noteQuote is the one-line "◆ author: summary" a confirmation shows for a
// note, control characters flattened and the text cut to a modal's width.
func noteQuote(n model.Note) string {
	who := n.Author
	if who == "" {
		who = string(n.Source)
	}
	return truncate(sanitizeLine("◆ "+who+": "+n.Summary), 72)
}

// noteChoiceOptions labels the threads in reach for the chooser modal —
// "1: summary" … plus the trailing Cancel that esc maps to. The labels are the
// notes' own text, so they render as-is (untranslated by design).
func noteChoiceOptions(ts []noteTarget) []string {
	opts := make([]string, 0, len(ts)+1)
	for i, t := range ts {
		opts = append(opts, fmt.Sprintf("%d: %s", i+1, truncate(sanitizeLine(t.note.Summary), 48)))
	}
	return append(opts, "Cancel")
}

// withNoteTarget runs act on the note in reach. One thread: straight away.
// Several threads on the same line (two notes on one line is common once an
// agent has been through): a chooser modal lists them by summary and act runs
// on the pick; esc / Cancel does nothing.
func (m Model) withNoteTarget(act func(Model, noteTarget) (tea.Model, tea.Cmd)) (tea.Model, tea.Cmd) {
	ts := m.notesAtCursor()
	switch len(ts) {
	case 0:
		return m, nil
	case 1:
		return act(m, ts[0])
	}
	opts := noteChoiceOptions(ts)
	m.modal = &decisionState{
		req: engine.DecisionRequest{
			ID:      "note-choose",
			Prompt:  i18n.T("Which note?"),
			Options: noteChoiceOptions(ts), // dynamic by nature: the notes' own summaries
		},
		onResolve: func(m Model, opt string) (tea.Model, tea.Cmd) {
			for i := range ts {
				if opt == opts[i] {
					return act(m, ts[i])
				}
			}
			return m, nil
		},
	}
	return m, nil
}

// nextNoteLine is the next (dir>0) / previous (dir<0) logical line that carries
// a note — including a FOLD entry that hides one, which the caller expands.
func (v *diffView) nextNoteLine(dir int) (int, bool) {
	byLine, foldMark := v.noteRowIndex()
	best, found := -1, false
	consider := func(li int) {
		if dir > 0 && li <= v.curLine {
			return
		}
		if dir < 0 && li >= v.curLine {
			return
		}
		if !found || (dir > 0 && li < best) || (dir < 0 && li > best) {
			best, found = li, true
		}
	}
	for li := range byLine {
		consider(li)
	}
	for li := range foldMark {
		consider(li)
	}
	return best, found
}

// jumpNote moves the cursor to the next/previous annotated line, expanding the
// view when the target hides under a fold (what f does), and reports whether
// it moved.
func (m Model) jumpNote(dir int) (Model, bool) {
	v := m.diffLayer()
	if v == nil {
		return m, false
	}
	body := m.diffBodyRows()
	find := func() (int, bool) { return v.nextNoteLine(dir) }
	li, ok := find()
	if !ok {
		return m, false
	}
	m, li, ok = m.expandFoldFor(v, li, find)
	if !ok {
		return m, false
	}
	v.setCursorLine(li, body)
	return m, true
}

// expandFoldFor reveals a note hidden under the fold at logical line li:
// expand to the full file (what the f toggle does), then re-find the anchor
// with find in the REBUILT stream. curLine is a partial-mode index and would be
// stale after the rebuild — always smaller than the same row's full-mode index
// — so a search against it would reject the very note we expanded for.
// Re-anchor first. A li that is not a fold is returned unchanged, unexpanded.
func (m Model) expandFoldFor(v *diffView, li int, find func() (int, bool)) (Model, int, bool) {
	if li >= len(v.lines) || v.lines[li].Fold == 0 {
		return m, li, true
	}
	cr, hadRow := v.cursorRow()
	v.partial = false
	v.rebuild()
	m.diffPartial = false
	if hadRow {
		v.reanchorCursor(cr.LeftNo, cr.RightNo)
	}
	li, ok := find()
	return m, li, ok
}

// peekNotedFile / stepNotedFile are the }/{ file step: the next file in the
// open diff's source list that CARRIES notes (NoteCounts, no store read).
// They mirror stepDiffFile's structure and reuse its per-nav steppers.
func (m Model) peekNotedFile(dir int) bool {
	_, ok := m.nextNotedFile(dir)
	return ok
}

func (m Model) stepNotedFile(dir int) (tea.Model, tea.Cmd) {
	steps, ok := m.nextNotedFile(dir)
	if !ok {
		return m, nil
	}
	nm, cmd := m, tea.Cmd(nil)
	for i := 0; i < steps; i++ {
		var tm tea.Model
		tm, cmd = nm.stepDiffFile(dir)
		nm = tm.(Model)
	}
	return nm, cmd
}

// nextNotedFile counts how many plain file steps in direction dir land on a
// file that carries notes (0, false = none). Stepping N times reuses the
// existing per-nav steppers unchanged, so tree/status/staged all work.
func (m Model) nextNotedFile(dir int) (int, bool) {
	paths := m.diffFileSequence(dir) // paths after the current one, in step order
	for i, p := range paths {
		if m.notedFilePath(p) {
			return i + 1, true
		}
	}
	return 0, false
}

// notedFilePath reports whether a path carries notes at the open diff's
// provenance: by path for a working-tree diff, by "<sha>:<path>" for a commit.
func (m Model) notedFilePath(path string) bool {
	if m.previewNoteScope() != nil {
		// A preview gathers notes from every commit on the branch, so the
		// tip-keyed ByCommitPath map would miss most of them.
		if m.filesPreviewCounts[path] == 0 {
			return false
		}
		// A file DELETED at the tip still counts for the panel badge (spec
		// §1.2, the "retired file" rule), but its new-side notes can never
		// render — the file has no new side. Stepping onto it would land the
		// user on a diff that shows no note at all, so `}`/`{` pass it by.
		return !m.previewPathGoneAtTip(path)
	}
	if v := m.diffLayer(); v != nil && v.rev != "" {
		return m.noteCounts.ByCommitPath[v.rev+":"+path] > 0
	}
	return m.noteCounts.ByPath[path] > 0
}

// noteMenuRows are the . menu's note rows — Edit / Reply / Delete — offered
// while a diff is on top and a note sits in reach. E and R also have keys; the
// rows make them discoverable (the diff footer has no room left for them), and
// Delete note has no key at all.
func (m Model) noteMenuRows() []actionRow {
	if _, ok := m.topLayer().(*diffView); !ok {
		return nil
	}
	if _, ok := m.noteNearCursor(); !ok {
		return nil
	}
	open := func(id, label string, mode noteFormMode) actionRow {
		return actionRow{id: id, label: label, run: func(m Model) (tea.Model, tea.Cmd) {
			return m.openNotePopup(mode)
		}}
	}
	rows := []actionRow{
		open("note-edit", i18n.T("Edit note"), noteEdit),
		open("note-reply", i18n.T("Reply to note"), noteReply),
	}
	if r, ok := m.noteDeleteRow(); ok {
		rows = append(rows, r)
	}
	return rows
}

// noteDeleteRow is the . menu's "Delete note" (there is no key for it). The
// write is destructive and unrecoverable — a root takes its replies with it —
// so it goes through the same yes/no modal the bookmark/shelf removals use;
// esc answers Cancel.
func (m Model) noteDeleteRow() (actionRow, bool) {
	if _, ok := m.topLayer().(*diffView); !ok {
		return actionRow{}, false
	}
	if _, ok := m.noteNearCursor(); !ok {
		return actionRow{}, false
	}
	return actionRow{
		id:    "note-delete",
		label: i18n.T("Delete note"),
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m.withNoteTarget(func(m Model, t noteTarget) (tea.Model, tea.Cmd) {
				return m.confirmNoteDelete(t)
			})
		},
	}, true
}

// confirmNoteDelete raises the yes/no modal for one targeted note.
func (m Model) confirmNoteDelete(t noteTarget) (tea.Model, tea.Cmd) {
	id, replies := t.note.ID, t.replies
	prompt := i18n.T("Delete this note?")
	if replies > 0 {
		prompt = i18n.T("Delete this note and its %d replies?", replies)
	}
	// Quote the note itself under the question — "this note" alone says
	// nothing about which one is about to go.
	prompt += "\n" + noteQuote(t.note)
	m.modal = &decisionState{
		req: engine.DecisionRequest{
			ID:      "note-remove",
			Prompt:  prompt,
			Options: []string{"Delete", "Cancel"},
		},
		sel: 1, // default highlight = Cancel
		onResolve: func(m Model, opt string) (tea.Model, tea.Cmd) {
			if opt == "Delete" {
				return m, m.noteRemoveCmd(id)
			}
			return m, nil
		},
	}
	return m, nil
}

// noteRemoveCmd deletes a note (a root takes its replies) off the UI thread.
func (m Model) noteRemoveCmd(id string) tea.Cmd {
	svc := m.svc
	if svc == nil {
		return nil
	}
	return func() tea.Msg {
		return noteMutatedMsg{err: svc.NoteRemove(context.Background(), id)}
	}
}
