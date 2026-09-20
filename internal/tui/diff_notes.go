package tui

import (
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/markdown"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/syntax"
	"github.com/homeend/gigagit/internal/textdiff"
)

// Review-note display rows. A resolved note becomes synthetic display rows
// appended AFTER its anchored line's content rows: the summary (◆ author:
// summary), one row per line of the optional rationale, and the same again per
// reply, indented. They live only in v.disp — v.lines, the change blocks and
// the shared textdiff rows are untouched, so the cursor, n/p, wrap and the
// fold machinery keep working on the same logical stream as before.
//
// Summaries and rationales are free text an agent may have written, so the row
// count relayout computes must not depend on what is inside them: a rationale
// is SPLIT on newlines here (one row each, so it reads naturally), and every
// row is sanitizeLine'd at render time — a stray \n, \t or bare \r can then
// never draw more physical rows than relayout accounted for.

// noteLine is ONE display row of a note (not one note): the pointer on dRow
// names the row, while id/rootID name the note it came from so E/R/Delete can
// find it again.
type noteLine struct {
	id     string         // the note (root or reply) this row shows
	rootID string         // the thread's root id (== id on a root row)
	depth  int            // 0 = root, 1 = reply (indent = 2*depth)
	kind   noteRowKind    // which part of the box this row is
	side   model.NoteSide // the pane the box sits in (old = left, new = right)
	text   string         // this row's text (already wrapped to the box; frame rows: the title)
	stale  bool           // the anchor text is gone: render dim
	agent  bool           // this ROW's own note is agent-written (the `a` layer filter)
	// cls is the row's per-rune class mask — set only on a FORGE note's rows,
	// whose text is rendered markdown (md_render.go); nil everywhere else.
	cls []syntax.Class
}

// noteRowKind is one row's role inside a note box, hunk-style: a rounded
// frame carrying the title in its top rule, a blank line, the summary in
// bold, the rationale, then a blank line and the bottom rule.
type noteRowKind int

const (
	noteRowTop       noteRowKind = iota // ╭─ title ───╮
	noteRowBlank                        // │           │
	noteRowSummary                      // │ Summary   │  (bold; wraps, never cut)
	noteRowText                         // │ rationale │  (wraps)
	noteRowBottom                       // ╰───────────╯
	noteRowCollapsed                    // ▸ author: summary… (N replies) — the whole thread on one row
)

// noteBoxFrame is the fixed width a box row spends on its frame and inner
// padding: "│ " on the left and " │" on the right.
const noteBoxFrame = 4

// noteInnerWidth is the text width inside a note box for the current layout:
// one pane less the frame. 0 (no layout yet, or a degenerate pane) means
// "do not wrap" — one row per source line, truncated when painted.
func (v *diffView) noteInnerWidth() int {
	if v.width <= 0 {
		return 0
	}
	paneW := (v.width - 1) / 2
	if paneW-noteBoxFrame < 8 {
		return 0
	}
	return paneW - noteBoxFrame
}

// noteRowIndex maps logical line index → its note rows, and fold line index →
// "a note hides under this fold". While the agent layer is off, every ROW is
// filtered by its own note's source: an agent reply under a user root goes,
// a user reply under an agent root stays (keeping its indentation). User notes
// always render (hunk's policy). A thread with nothing left to show marks no
// fold either — a marker for something you cannot reveal is a lie.
func (v *diffView) noteRowIndex() (map[int][]noteLine, map[int]bool) {
	if len(v.notes) == 0 {
		return nil, nil
	}
	byLine := map[int][]noteLine{}
	foldMark := map[int]bool{}
	innerW := v.noteInnerWidth()
	for _, r := range v.notes {
		rows := v.noteBoxLines(r, innerW)
		if v.hideAgent {
			rows = dropAgentRows(rows)
		}
		if !hasNoteContent(rows) {
			continue
		}
		li, visible := v.noteAnchorLine(r)
		if li < 0 {
			continue // the anchor is not in this view at all
		}
		if !visible {
			foldMark[li] = true // folded away: mark the fold rule instead
			continue
		}
		byLine[li] = append(byLine[li], rows...)
	}
	return byLine, foldMark
}

// dropAgentRows keeps the rows the hidden agent layer still shows. It filters
// per ROW, not per thread, so the two mixed-authorship cases both behave.
func dropAgentRows(rows []noteLine) []noteLine {
	out := rows[:0:0]
	for _, nl := range rows {
		if !nl.agent {
			out = append(out, nl)
		}
	}
	return out
}

// noteAnchorLine finds the logical line a note hangs off: the line carrying
// the END of its range on its side (§4.4 phase-2 hunks anchor at the range
// end). See lineAnchor for the return contract.
func (v *diffView) noteAnchorLine(r domain.ResolvedNote) (int, bool) {
	if isFileLevelNote(r) {
		// A forge comment on the whole file hangs off the view's first real
		// line — the top of the file. (The line may be a fold: then it is the
		// fold that gets the note mark, like any other hidden anchor.)
		if len(v.lines) == 0 {
			return -1, false
		}
		return 0, v.lines[0].Fold == 0
	}
	return v.lineAnchor(r.Range[1], r.Note.Side == model.NoteSideOld)
}

// isFileLevelNote reports a forge comment anchored to the file, not a line:
// the zero range. Only a forge note may be one — a STORED note with a zero
// range is malformed and stays unanchored.
func isFileLevelNote(r domain.ResolvedNote) bool {
	return r.Note.Source == model.NoteSourceForge && r.Range == [2]int{}
}

// lineAnchor finds the logical line carrying number no on the old (old=true) or
// new side. visible=false means the number exists in the file but is folded
// away, and the returned index is the FOLD entry that hides it. (-1, false)
// means the number is not in this view at all. Shared by the note anchors and
// by live steering's landing, which knows only a side and a number.
func (v *diffView) lineAnchor(no int, old bool) (int, bool) {
	if no <= 0 {
		return -1, false
	}
	numOf := func(ln textdiff.Line) int {
		if old {
			return ln.Row.LeftNo
		}
		return ln.Row.RightNo
	}
	prev := 0
	for i, ln := range v.lines {
		if ln.Fold > 0 {
			// The hidden run spans (prev, next): find the next real number.
			next := 0
			for j := i + 1; j < len(v.lines); j++ {
				if v.lines[j].Fold == 0 && numOf(v.lines[j]) > 0 {
					next = numOf(v.lines[j])
					break
				}
			}
			if no > prev && (next == 0 || no < next) {
				return i, false
			}
			continue
		}
		if numOf(ln) == no {
			return i, true
		}
		if n := numOf(ln); n > 0 {
			prev = n
		}
	}
	return -1, false
}

// lastLineNo is the highest line number present on one side, or 0 when the side
// has none (a pure addition has no old side). Live steering clamps a landing to
// it rather than refusing an agent whose line number drifted past the end.
func (v *diffView) lastLineNo(old bool) int {
	last := 0
	for _, ln := range v.lines {
		if ln.Fold > 0 {
			continue
		}
		n := ln.Row.RightNo
		if old {
			n = ln.Row.LeftNo
		}
		if n > last {
			last = n
		}
	}
	return last
}

// hasNoteContent reports whether any summary/text row survived filtering —
// a frame with nothing inside is not a note to show.
func hasNoteContent(rows []noteLine) bool {
	for _, nl := range rows {
		if nl.kind == noteRowSummary || nl.kind == noteRowText || nl.kind == noteRowCollapsed {
			return true
		}
	}
	return false
}

// noteBoxLines lays one thread out as a box: the title row, a blank, the
// root's summary (bold, wrapped) and rationale (wrapped), each reply as a
// "↳ author: summary" + rationale block, a blank and the bottom rule. innerW
// is the wrap width (0 = no wrapping). Frame rows are agent-tagged only when
// the whole thread is agent-written, so a user reply under an agent root
// keeps its frame while the `a` layer is off.
func (v *diffView) noteBoxLines(r domain.ResolvedNote, innerW int) []noteLine {
	if v.collapsed[r.Note.ID] {
		return []noteLine{v.collapsedNoteLine(r)}
	}
	stale := r.Status == model.NoteStale
	allAgent := r.Note.Source == model.NoteSourceAgent
	for _, rep := range r.Replies {
		if rep.Note.Source != model.NoteSourceAgent {
			allAgent = false
		}
	}
	frame := func(kind noteRowKind, text string) noteLine {
		return noteLine{id: r.Note.ID, rootID: r.Note.ID, kind: kind, side: r.Note.Side, text: text, stale: stale, agent: allAgent}
	}
	rows := []noteLine{frame(noteRowTop, v.noteBoxTitle(r)), frame(noteRowBlank, "")}
	rows = append(rows, noteBodyLines(r, r.Note.ID, 0, innerW, stale)...)
	for _, rep := range r.Replies {
		rows = append(rows, noteBodyLines(rep, r.Note.ID, 1, innerW, stale)...)
	}
	return append(rows, frame(noteRowBlank, ""), frame(noteRowBottom, ""))
}

// collapsedNoteLine is a whole thread on one row: "author: summary (N
// replies)". The painter prefixes ▸ and cuts it to the pane. It is agent-tagged
// like a frame row — only when the whole thread is — so the `a` layer hides it
// exactly when it would hide the open box.
func (v *diffView) collapsedNoteLine(r domain.ResolvedNote) noteLine {
	allAgent := r.Note.Source == model.NoteSourceAgent
	for _, rep := range r.Replies {
		if rep.Note.Source != model.NoteSourceAgent {
			allAgent = false
		}
	}
	text := r.Note.Summary
	if r.Note.Source == model.NoteSourceForge && r.SummarySrc == "" {
		text = forgeLabelText(text) // a label summary reads in the UI's language
	}
	if r.Note.Author != "" {
		text = r.Note.Author + ": " + text
	}
	switch n := len(r.Replies); {
	case n == 1:
		text += " " + i18n.T("(1 reply)")
	case n > 1:
		text += " " + i18n.T("(%d replies)", n)
	}
	side := r.Note.Side
	if side == "" {
		side = model.NoteSideNew
	}
	return noteLine{id: r.Note.ID, rootID: r.Note.ID, kind: noteRowCollapsed, side: side,
		text: sanitizeLine(text), stale: r.Status == model.NoteStale, agent: allAgent}
}

// setNotes replaces the view's threads and seeds the collapse state of the
// ones seen for the first time: a forge thread marked resolved starts folded.
func (v *diffView) setNotes(ns []domain.ResolvedNote) {
	v.notes = ns
	if v.collapsed == nil {
		v.collapsed = map[string]bool{}
	}
	if v.collapseSeeded == nil {
		v.collapseSeeded = map[string]bool{}
	}
	for _, r := range ns {
		if v.collapseSeeded[r.Note.ID] {
			continue
		}
		v.collapseSeeded[r.Note.ID] = true
		if r.Note.Source == model.NoteSourceForge && model.NoteHasTag(r.Note, model.NoteTagResolved) {
			v.collapsed[r.Note.ID] = true
		}
	}
}

// relayoutKeepingCursor re-lays the view after its note rows changed height,
// the way a notes arrival does: the cursor stays on its logical line and a
// free-scrolled view keeps its place.
func (m Model) relayoutKeepingCursor(v *diffView) {
	body := m.diffBodyRows()
	cr, hadRow := v.cursorRow()
	wasVisible := v.cursorVisible(body)
	v.relayout(v.width)
	v.reanchorAfterRebuild(cr, hadRow, wasVisible, body)
}

// toggleNoteCollapse folds or unfolds every thread in reach of the cursor
// (the ones E/R/Delete would offer — forge threads included: folding changes
// nothing but the view).
func (m Model) toggleNoteCollapse() Model {
	v := m.diffLayer()
	ts := m.notesAtCursor()
	if v == nil || len(ts) == 0 {
		return m
	}
	if v.collapsed == nil {
		v.collapsed = map[string]bool{}
	}
	want := false // any open thread in reach → fold them all; else unfold
	for _, t := range ts {
		if !v.collapsed[t.rootID] {
			want = true
		}
	}
	for _, t := range ts {
		v.collapsed[t.rootID] = want
	}
	m.relayoutKeepingCursor(v)
	return m
}

// toggleAllNotesCollapse folds every thread when any is open, else unfolds all.
func (m Model) toggleAllNotesCollapse() Model {
	v := m.diffLayer()
	if v == nil || len(v.notes) == 0 {
		return m
	}
	if v.collapsed == nil {
		v.collapsed = map[string]bool{}
	}
	want := false
	for _, r := range v.notes {
		if !v.collapsed[r.Note.ID] {
			want = true
		}
	}
	for _, r := range v.notes {
		v.collapsed[r.Note.ID] = want
	}
	m.relayoutKeepingCursor(v)
	return m
}

// allNotesCollapsed reports whether O would expand (every thread is folded).
func (v *diffView) allNotesCollapsed() bool {
	for _, r := range v.notes {
		if !v.collapsed[r.Note.ID] {
			return false
		}
	}
	return len(v.notes) > 0
}

// noteBoxTitle is the text in a box's top rule: "agent note" or "note", the
// author, the file and the anchored line (R for the new side, L for the old),
// plus "(stale)" when the anchor text is gone — hunk's
// "agent note - updates/garmin.go R204".
func (v *diffView) noteBoxTitle(r domain.ResolvedNote) string {
	if r.Note.Source == model.NoteSourceForge {
		return v.forgeNoteTitle(r)
	}
	kind := i18n.T("note")
	if r.Note.Source == model.NoteSourceAgent {
		kind = i18n.T("agent note")
	}
	t := kind
	if r.Note.Author != "" {
		t += " · " + r.Note.Author
	}
	sideMark := "R"
	if r.Note.Side == model.NoteSideOld {
		sideMark = "L"
	}
	t += " · " + v.noteAddr.Path + " " + sideMark + strconv.Itoa(r.Range[1])
	if r.Status == model.NoteStale {
		if v.previewSet != nil {
			// In a preview a note whose lines a later commit changed is the
			// EXPECTED case, so it is named plainly and marked in the gutter
			// rather than whispered as an edge condition.
			return "⊘ " + t + " " + i18n.T("(outdated)")
		}
		t += " " + i18n.T("(stale)")
	}
	return t
}

// forgeNoteTitle is a forge review thread's top rule: "review · author · 2d ago
// · path R12", "(file)" in place of the line for a whole-file comment, and
// "· resolved" when its reviewers closed the thread. The login, the path and
// the age stamp are data; only the words are translated.
func (v *diffView) forgeNoteTitle(r domain.ResolvedNote) string {
	parts := []string{i18n.T("review")}
	if r.Note.Author != "" {
		parts = append(parts, r.Note.Author)
	}
	if !r.Note.Created.IsZero() {
		parts = append(parts, ageString(time.Now(), r.Note.Created))
	}
	where := v.noteAddr.Path
	switch {
	case isFileLevelNote(r):
		where += " " + i18n.T("(file)")
	case r.Note.Side == model.NoteSideOld:
		where += " L" + strconv.Itoa(r.Range[1])
	default:
		where += " R" + strconv.Itoa(r.Range[1])
	}
	parts = append(parts, where)
	if model.NoteHasTag(r.Note, model.NoteTagResolved) {
		parts = append(parts, i18n.T("resolved"))
	}
	return strings.Join(parts, " · ")
}

// noteBodyLines is one note's own rows inside a box: the summary (bold on a
// root; "↳ author: summary" on a reply), wrapped to the box and never cut —
// a pasted paragraph in the title just takes more rows — then the rationale,
// one row per source line, each wrapped. Control characters are flattened so
// a row is always exactly one physical line.
func noteBodyLines(r domain.ResolvedNote, rootID string, depth, innerW int, stale bool) []noteLine {
	agent := r.Note.Source == model.NoteSourceAgent
	mk := func(kind noteRowKind, text string) noteLine {
		return noteLine{id: r.Note.ID, rootID: rootID, depth: depth, kind: kind, side: r.Note.Side, text: text, stale: stale, agent: agent}
	}
	indent := strings.Repeat("  ", depth)
	head := ""
	if depth > 0 {
		head = "↳ "
		if r.Note.Author != "" {
			head += r.Note.Author + ": "
		}
	}
	var rows []noteLine
	w := innerW - len([]rune(indent))
	if r.Note.Source == model.NoteSourceForge {
		return forgeNoteBodyLines(r, mk, indent, head, w)
	}
	for _, ln := range noteWrap(sanitizeLine(head+r.Note.Summary), innerW-len([]rune(indent))) {
		rows = append(rows, mk(noteRowSummary, indent+ln))
	}
	if r.Note.Rationale != "" {
		for _, src := range strings.Split(r.Note.Rationale, "\n") {
			for _, ln := range noteWrap(sanitizeLine(src), innerW-len([]rune(indent))) {
				rows = append(rows, mk(noteRowText, indent+ln))
			}
		}
	}
	return rows
}

// noteWrap word-wraps one sanitized line to w columns (w <= 0: no wrap). An
// empty line stays one empty row so a blank line in a rationale survives.
func noteWrap(line string, w int) []string {
	if w <= 0 || lipgloss.Width(line) <= w {
		return []string{line}
	}
	out := wrapWords(line, w)
	if len(out) == 0 {
		return []string{""}
	}
	return out
}

// noteBadge is the trailing "◆N" a Files/Commits row carries when its target
// has notes. Display only — never part of a filter haystack. n <= 0 (a missing
// map entry, or a NoteCounts that failed to load) yields "", so rows in a repo
// without notes stay byte-identical.
func noteBadge(n int) string {
	if n <= 0 {
		return ""
	}
	return "  ◆" + strconv.Itoa(n)
}

// noteCollapseRows are the . menu's collapse rows (keys o / O; the diff footer
// has no room left for them, so the menu and ? are where they are advertised).
func (m Model) noteCollapseRows() []actionRow {
	v, ok := m.topLayer().(*diffView)
	if !ok || len(v.notes) == 0 {
		return nil
	}
	var rows []actionRow
	if ts := m.notesAtCursor(); len(ts) > 0 {
		label := i18n.T("Collapse note")
		if v.collapsed[ts[0].rootID] {
			label = i18n.T("Expand note")
		}
		rows = append(rows, actionRow{id: "note-collapse", key: "o", label: label, run: func(m Model) (tea.Model, tea.Cmd) {
			return m.toggleNoteCollapse(), nil
		}})
	}
	label := i18n.T("Collapse all notes")
	if v.allNotesCollapsed() {
		label = i18n.T("Expand all notes")
	}
	return append(rows, actionRow{id: "note-collapse-all", key: "O", label: label, run: func(m Model) (tea.Model, tea.Cmd) {
		return m.toggleAllNotesCollapse(), nil
	}})
}

// forgeNoteBodyLines is noteBodyLines for a forge comment, whose text is
// markdown: the summary from its source line's inline tree (a label summary —
// "suggestion" — has none and stays plain), the rationale from its parsed
// blocks. Both are wrapped here, to the box, masks and all.
func forgeNoteBodyLines(r domain.ResolvedNote, mk func(noteRowKind, string) noteLine, indent, head string, w int) []noteLine {
	pad := make([]syntax.Class, len([]rune(indent)))
	var rows []noteLine
	add := func(kind noteRowKind, mr []mdRow) {
		for _, row := range mr {
			nl := mk(kind, indent+row.text)
			nl.cls = append(append([]syntax.Class{}, pad...), row.cls...)
			rows = append(rows, nl)
		}
	}
	if r.SummarySrc != "" {
		add(noteRowSummary, mdInlineRows(markdown.ParseInline(r.SummarySrc), w, head))
	} else {
		add(noteRowSummary, mdInlineRows([]markdown.Inline{{Kind: markdown.InText, Text: forgeLabelText(r.Note.Summary)}}, w, head))
	}
	if r.Note.Rationale != "" {
		body := mdRows(markdown.Parse(r.Note.Rationale), w)
		// A thread summarised by the label "suggestion" opens with that very
		// block: its caption would only say the summary again.
		if r.SummarySrc == "" && r.Note.Summary == markdown.LabelSuggestion && len(body) > 0 && body[0].text == i18n.T("suggestion") {
			body = body[1:]
		}
		add(noteRowText, body)
	}
	return rows
}

// forgeLabelText translates a LABEL summary — what domain calls a review
// comment that opens with a block instead of a line of prose (English
// protocol text, markdown.Label*). Anything else is returned as it is.
func forgeLabelText(summary string) string {
	switch summary {
	case markdown.LabelSuggestion:
		return i18n.T("suggestion")
	case markdown.LabelCode:
		return i18n.T("code")
	case markdown.LabelQuote:
		return i18n.T("quote")
	case markdown.LabelTable:
		return i18n.T("table")
	}
	return summary
}
