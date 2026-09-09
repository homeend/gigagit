package tui

import (
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
)

// notesListRows is the visible-row budget of the unmaximized list popup; ctrl+t
// (popupMax) trades it for the terminal-derived cap.
const notesListRows = 12

// noteListDot is the column the ◆ sits in: past the two-column cursor marker
// every popup row carries ("> " / "  ").
const noteListDot = 2

// noteListEntry is one THREAD in the list: its anchor, author and summary, plus
// the reply count the row appends. The parts are kept apart from the rendered
// line so the summary — the only elastic part — is the one that gets trimmed
// when the popup is narrow, and so the filter matches the note's own text
// rather than whatever the layout happened to produce.
type noteListEntry struct {
	rootID  string
	anchor  string // "new:15" / "old:15-23"
	author  string
	summary string
	replies int
	stale   bool // the anchor text is gone: the ◆ goes dim
	agent   bool // agent-written thread: the ◆ takes the agent frame colour
	filter  string
}

// line renders the entry to w columns: "◆ new:15  ada  summary  +2". The fixed
// parts are laid out first and the summary takes whatever is left, so the reply
// count survives a narrow popup and only the free text is cut.
func (e noteListEntry) line(w int) string {
	head := "◆ " + e.anchor + "  "
	if e.author != "" {
		head += e.author + "  "
	}
	tail := ""
	if e.replies > 0 {
		tail = "  +" + strconv.Itoa(e.replies)
	}
	budget := w - lipgloss.Width(head) - lipgloss.Width(tail)
	if budget < 1 {
		// Nothing left for free text: keep the identifying parts and let the
		// box's own clamp cut them.
		return truncate(head+tail, w)
	}
	return head + truncate(sanitizeLine(e.summary), budget) + tail
}

// noteListEntries projects the open diff's threads into list entries, in
// v.notes order — which is resolution order: new side first, then by line, so
// the list already reads in anchor order. Every ROOT is listed, including the
// agent-written ones the `a` layer may currently be hiding in the diff body:
// the list is the inventory of what is stored on this file.
func noteListEntries(ns []domain.ResolvedNote) []noteListEntry {
	out := make([]noteListEntry, 0, len(ns))
	for _, r := range ns {
		anchor := string(r.Note.Side) + ":" + strconv.Itoa(r.Range[0])
		if r.Range[1] != r.Range[0] {
			anchor += "-" + strconv.Itoa(r.Range[1])
		}
		author := sanitizeLine(r.Note.Author)
		summary := sanitizeLine(r.Note.Summary)
		out = append(out, noteListEntry{
			rootID:  r.Note.ID,
			anchor:  anchor,
			author:  author,
			summary: summary,
			replies: len(r.Replies),
			stale:   r.Status == model.NoteStale,
			agent:   r.Note.Source == model.NoteSourceAgent,
			filter:  strings.ToLower(summary + "\x00" + author),
		})
	}
	return out
}

// notesListPopup is the "List notes" overlay: every thread on the open diff,
// one row each, with type-to-filter and enter to land the diff cursor on a
// thread's anchor. It is a plain inventory — no mutation — so it never needs
// the note target machinery E/R/Delete go through.
type notesListPopup struct {
	popupMax
	entries []noteListEntry
	query   string
	sel     int
}

// noteListMenuRow is the . menu's "List notes…". Unlike the Edit/Reply/Delete
// rows it is NOT cursor-scoped: it is offered wherever the cursor sits, as long
// as the open diff carries at least one thread.
func (m Model) noteListMenuRow() (actionRow, bool) {
	if !m.diffHasNotes() {
		return actionRow{}, false
	}
	return actionRow{
		id:    "note-list",
		label: i18n.T("List notes…"),
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m.openNotesList()
		},
	}, true
}

// diffHasNotes reports whether a diff is on top and carries at least one root
// note — the gate both whole-diff note rows (List / Remove all) share.
func (m Model) diffHasNotes() bool {
	if _, ok := m.topLayer().(*diffView); !ok {
		return false
	}
	v := m.diffLayer()
	return v != nil && len(v.notes) > 0
}

// openNotesList pushes the list over the diff.
func (m Model) openNotesList() (tea.Model, tea.Cmd) {
	v := m.diffLayer()
	if v == nil || len(v.notes) == 0 {
		return m, nil
	}
	return m.pushLayer(&notesListPopup{entries: noteListEntries(v.notes)}), nil
}

// visible is the entries the current filter keeps: a case-insensitive substring
// of the summary or the author.
func (p *notesListPopup) visible() []noteListEntry {
	if p.query == "" {
		return p.entries
	}
	q := strings.ToLower(p.query)
	var out []noteListEntry
	for _, e := range p.entries {
		if strings.Contains(e.filter, q) {
			out = append(out, e)
		}
	}
	return out
}

// setQuery replaces the filter and re-homes the cursor, the single chokepoint
// every query edit goes through.
func (p *notesListPopup) setQuery(q string) {
	p.query = q
	p.sel = 0
}

// move steps the selection by d, clamped to the filtered list.
func (p *notesListPopup) move(d int) {
	n := len(p.visible())
	if n == 0 {
		p.sel = 0
		return
	}
	p.sel = min(max(p.sel+d, 0), n-1)
}

// update handles one key. The popup is type-to-filter like the . menu, so
// letters are never bindings: only the arrows/pages/home/end move, enter goes
// to a note and esc closes.
func (p *notesListPopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	if filterMotion(msg, p.move, popupFilterPage) {
		return m, nil
	}
	switch msg.Type {
	case tea.KeyHome:
		p.sel = 0
		return m, nil
	case tea.KeyEnd:
		p.move(len(p.visible()))
		return m, nil
	case tea.KeyEsc:
		return m.popLayer(), nil
	case tea.KeyEnter:
		vis := p.visible()
		if p.sel < 0 || p.sel >= len(vis) {
			return m, nil
		}
		id := vis[p.sel].rootID
		m = m.popLayer()
		m, _ = m.gotoNote(id)
		return m, nil
	case tea.KeyBackspace, tea.KeyCtrlH:
		if r := []rune(p.query); len(r) > 0 {
			p.setQuery(string(r[:len(r)-1]))
		}
		return m, nil
	case tea.KeySpace:
		// Summaries are prose: a space extends the filter like any rune.
		p.setQuery(p.query + " ")
		return m, nil
	case tea.KeyRunes:
		p.setQuery(p.query + string(msg.Runes))
		return m, nil
	}
	return m, nil
}

func (p *notesListPopup) render(m Model, below string) string {
	w, h := m.overlayDims()
	return overlayCenter(clipToHeight(below, h), p.box(m), w, h)
}

func (p *notesListPopup) box(m Model) string {
	w, h := m.overlayDims()
	inner := popupResolveWidth(w, p.maximized, popupWideInnerWidth(w))
	textW := popupTextWidth(inner)
	vis := p.visible()

	var bodyLines []string
	if len(vis) == 0 {
		bodyLines = []string{padRight("  "+i18n.T("(no matching notes)"), textW)}
	} else {
		rows := make([]winRow, len(vis))
		for i, e := range vis {
			prefix := "  "
			var st lipgloss.Style
			if i == p.sel {
				prefix, st = "> ", selectedRow
			}
			r := winRow{text: prefix + e.line(textW-len(prefix)), style: st}
			// The selected row is reverse-video: a foreground on the ◆ would
			// paint a per-glyph BACKGROUND there, so it stays plain.
			if i != p.sel {
				r.decorate = noteListDotDecorator(e)
			}
			rows[i] = r
		}
		bodyLines = renderWindow(rows, winOpts{
			w: textW, anchor: p.sel,
			h: popupResolveRowCap(p.maximized, h, notesListRows),
		})
	}

	header := i18n.T("Notes on %s", m.diffLayer().title)
	if p.query != "" {
		header += "  " + p.query + "█"
	}
	parts := []string{header, ""}
	parts = append(parts, bodyLines...)
	parts = append(parts, "")
	parts = append(parts, wrapParts([]string{
		i18n.T("[↑/↓] move"),
		i18n.T("[enter] go to note"),
		i18n.T("type to filter"),
		i18n.T("[ctrl+t] fullscreen"),
		i18n.T("[esc] close"),
	}, textW, "  ")...)
	return popupBox(inner, strings.Join(parts, "\n"))
}

// noteListDotDecorator paints just the ◆ in its thread's colour — agent violet,
// user blue, dim when the anchor text is gone — leaving the row's width and the
// rest of its text untouched.
func noteListDotDecorator(e noteListEntry) rowDecorator {
	st := noteFrameUser
	switch {
	case e.stale:
		st = noteFrameStale
	case e.agent:
		st = noteFrameAgent
	}
	return func(visible string, hscroll, visualLine int) string {
		r := []rune(visible)
		if len(r) <= noteListDot || r[noteListDot] != '◆' {
			return visible
		}
		return string(r[:noteListDot]) + st.Render("◆") + string(r[noteListDot+1:])
	}
}

// gotoNote lands the diff cursor on one thread's anchor line, expanding the
// view when the anchor hides under a fold (exactly what } does). It reports
// whether it moved, and reveals the thread's own rows so the note the user
// picked is on screen, not just its line.
func (m Model) gotoNote(rootID string) (Model, bool) {
	v := m.diffLayer()
	if v == nil {
		return m, false
	}
	find := func() (int, bool) {
		for _, r := range v.notes {
			if r.Note.ID != rootID {
				continue
			}
			li, _ := v.noteAnchorLine(r)
			return li, li >= 0
		}
		return 0, false
	}
	li, ok := find()
	if !ok {
		return m, false
	}
	if li < len(v.lines) && v.lines[li].Fold > 0 {
		// The note hides under this fold: expand to the full file (the f
		// toggle), then re-find the anchor in the REBUILT stream — the
		// pre-rebuild index would be stale (see jumpNote).
		cr, hadRow := v.cursorRow()
		v.partial = false
		v.rebuild()
		m.diffPartial = false
		if hadRow {
			v.reanchorCursor(cr.LeftNo, cr.RightNo)
		}
		if li, ok = find(); !ok {
			return m, false
		}
	}
	body := m.diffBodyRows()
	v.setCursorLine(li, body)
	v.revealCursorNotes(body)
	return m, true
}
