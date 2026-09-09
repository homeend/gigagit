package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
)

// hasActionRow reports whether the . menu currently offers the row with id.
func hasActionRow(m Model, id string) bool {
	_, ok := findRow(availableActions(m), id)
	return ok
}

// runActionRow runs the . menu row with id, failing when it is not offered.
func runActionRow(t *testing.T, m Model, id string) Model {
	t.Helper()
	r, ok := findRow(availableActions(m), id)
	if !ok {
		t.Fatalf("the . menu offers no %q row", id)
	}
	if r.run == nil {
		t.Fatalf("the %s row must carry a direct run handler", id)
	}
	tm, _ := r.run(m)
	return tm.(Model)
}

func TestNoteListRowIsOfferedAwayFromTheCursorAndOnlyWithNotes(t *testing.T) {
	t.Parallel()
	// A diff with notes, cursor parked far from any of them: unlike Edit /
	// Reply / Delete (which need a note next to the cursor), List notes is
	// offered on the whole diff.
	m := notedModel(t)
	m.diffLayer().setCursorLine(15, m.diffBodyRows())
	if _, ok := m.noteNearCursor(); ok {
		t.Fatal("fixture broken: line 15 must not sit next to a note")
	}
	if hasActionRow(m, "note-edit") {
		t.Fatal("Edit note must stay cursor-scoped")
	}
	if !hasActionRow(m, "note-list") {
		t.Fatal("List notes must be offered anywhere in a diff that carries notes")
	}
	// The same diff with its notes gone offers nothing.
	m.diffLayer().notes = nil
	if hasActionRow(m, "note-list") {
		t.Fatal("List notes must not be offered on a diff without notes")
	}
}

func TestNotesListPopupListsEveryRootInAnchorOrder(t *testing.T) {
	t.Parallel()
	m := notedModel(t)
	v := m.diffLayer()
	v.notes[0].Replies = []domain.ResolvedNote{{
		Note:   model.Note{ID: "r1", ParentID: "n1", Author: "bot", Summary: "agreed"},
		Status: model.NoteActive, Range: [2]int{5, 5},
	}, {
		Note:   model.Note{ID: "r2", ParentID: "n1", Author: "bot", Summary: "same"},
		Status: model.NoteActive, Range: [2]int{5, 5},
	}}
	m = runActionRow(t, m, "note-list")
	p := layerOf[*notesListPopup](m)
	if p == nil {
		t.Fatal("List notes must push a notesListPopup")
	}
	if len(p.entries) != 2 {
		t.Fatalf("popup lists %d entries, want the two roots", len(p.entries))
	}
	first := p.entries[0].line(80)
	if !strings.HasPrefix(first, "◆ new:5  ada  first") {
		t.Fatalf("first row = %q, want the anchor, author and summary", first)
	}
	if !strings.HasSuffix(first, "  +2") {
		t.Fatalf("first row = %q, want the reply count appended", first)
	}
	if second := p.entries[1].line(80); !strings.HasPrefix(second, "◆ new:25  ada  second") {
		t.Fatalf("second row = %q, want the line-25 note", second)
	} else if strings.Contains(second, "+") {
		t.Fatalf("a thread with no replies must carry no count: %q", second)
	}
	if !p.entries[1].agent {
		t.Fatal("the agent-written note must be flagged so its ◆ is painted")
	}
}

func TestNotesListPopupTrimsALongSummary(t *testing.T) {
	t.Parallel()
	m := notedModel(t)
	// Double-width glyphs: a row that counted runes instead of display columns
	// would overflow its box here (a CJK summary is exactly the case the note
	// boxes already sanitize for).
	m.diffLayer().notes[0].Note.Summary = strings.Repeat("実装が壊れている ", 10)
	m = runActionRow(t, m, "note-list")
	p := layerOf[*notesListPopup](m)
	got := p.entries[0].line(40)
	if w := lipgloss.Width(got); w > 40 {
		t.Fatalf("row %q is %d columns wide, want at most 40", got, w)
	}
	if !strings.Contains(got, "…") {
		t.Fatalf("a trimmed summary must end in an ellipsis: %q", got)
	}
}

func TestNotesListPopupTypeToFilter(t *testing.T) {
	t.Parallel()
	m := notedModel(t)
	m = runActionRow(t, m, "note-list")
	p := layerOf[*notesListPopup](m)
	m, _ = p.update(m, keyMsg("s"))
	m, _ = p.update(m, keyMsg("e"))
	if vis := p.visible(); len(vis) != 1 || vis[0].rootID != "n2" {
		t.Fatalf("filter %q left %d rows, want only the \"second\" note", p.query, len(vis))
	}
	m, _ = p.update(m, tea.KeyMsg{Type: tea.KeyBackspace})
	if len(p.visible()) != 2 {
		t.Fatalf("backspace must widen the filter again, got %d rows", len(p.visible()))
	}
	// The author is part of the haystack too.
	p.setQuery("ada")
	if len(p.visible()) != 2 {
		t.Fatalf("filtering by author = %d rows, want both", len(p.visible()))
	}
	p.setQuery("zzz")
	if len(p.visible()) != 0 {
		t.Fatal("an unmatched filter must leave no rows")
	}
	if box := p.box(m); !strings.Contains(box, i18n.T("(no matching notes)")) {
		t.Fatalf("an empty result must say so:\n%s", box)
	}
}

func TestNotesListPopupEnterGoesToTheNoteAndEscDoesNot(t *testing.T) {
	t.Parallel()
	m := notedModel(t)
	m.diffLayer().setCursorLine(0, m.diffBodyRows())
	m = runActionRow(t, m, "note-list")
	p := layerOf[*notesListPopup](m)
	p.move(1) // the line-25 note
	m, _ = p.update(m, tea.KeyMsg{Type: tea.KeyEnter})
	if layerOf[*notesListPopup](m) != nil {
		t.Fatal("enter must close the popup")
	}
	if got := m.diffLayer().curLine; got != 24 {
		t.Fatalf("enter landed the cursor on line index %d, want 24 (the note on line 25)", got)
	}

	m = runActionRow(t, m, "note-list")
	p = layerOf[*notesListPopup](m)
	p.move(-1) // back to the line-5 note
	m, _ = p.update(m, tea.KeyMsg{Type: tea.KeyEsc})
	if layerOf[*notesListPopup](m) != nil {
		t.Fatal("esc must close the popup")
	}
	if got := m.diffLayer().curLine; got != 24 {
		t.Fatalf("esc must not move the cursor, curLine = %d", got)
	}
}

func TestNotesListPopupEnterExpandsAFoldedNote(t *testing.T) {
	t.Parallel()
	m := openedDiffModel(12, cursorRows(40, 4, 34), []int{4, 34})
	v := m.diffLayer()
	v.noteAddr = model.FileAddress{State: model.StateUnstaged, Worktree: "/wt", Path: "a/b.go"}
	v.notes = []domain.ResolvedNote{rootNote("n1", 21, "buried", "", model.NoteSourceUser, model.NoteActive)}
	v.partial = true
	v.rebuild()
	v.setCursorLine(0, m.diffBodyRows())
	m = runActionRow(t, m, "note-list")
	p := layerOf[*notesListPopup](m)
	m, _ = p.update(m, tea.KeyMsg{Type: tea.KeyEnter})
	v = m.diffLayer()
	if v.partial {
		t.Fatal("enter onto a folded note must expand the view (like })")
	}
	if v.lines[v.curLine].Row.RightNo != 21 {
		t.Fatalf("cursor landed on RightNo %d, want 21", v.lines[v.curLine].Row.RightNo)
	}
}

func TestNotesListPopupBoxIsNoTallerThanItsContent(t *testing.T) {
	t.Parallel()
	m := notedModel(t)
	m.width, m.height = 100, 40
	m = runActionRow(t, m, "note-list")
	p := layerOf[*notesListPopup](m)
	// Two notes: header + blank + 2 rows + blank + one hint line, inside the
	// double border and its vertical padding. A body padded to the row budget
	// would add ten blank rows.
	if got := len(strings.Split(strings.TrimRight(p.box(m), "\n"), "\n")); got > 11 {
		t.Fatalf("the box is %d lines tall for two notes:\n%s", got, p.box(m))
	}
}

func TestNotesListPopupEnterOnAnEmptyFilterKeepsThePopup(t *testing.T) {
	t.Parallel()
	m := notedModel(t)
	m = runActionRow(t, m, "note-list")
	p := layerOf[*notesListPopup](m)
	p.setQuery("zzz")
	before := m.diffLayer().curLine
	m, cmd := p.update(m, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("enter with nothing selected must do nothing")
	}
	if layerOf[*notesListPopup](m) == nil {
		t.Fatal("enter on an empty filter result must keep the popup open")
	}
	if got := m.diffLayer().curLine; got != before {
		t.Fatalf("the cursor moved to %d, want it left at %d", got, before)
	}
}

func TestNotesListPopupEnterOnAHiddenAgentNoteLiftsTheLayer(t *testing.T) {
	t.Parallel()
	m := notedModel(t) // n2 (line 25) is agent-written
	m.notesAgentOff = true
	v := m.diffLayer()
	v.hideAgent = true
	v.relayout(v.width)
	v.setCursorLine(0, m.diffBodyRows())
	m = runActionRow(t, m, "note-list")
	p := layerOf[*notesListPopup](m)
	if len(p.entries) != 2 {
		t.Fatalf("the list is an inventory: it must offer the hidden agent thread too, got %d", len(p.entries))
	}
	p.move(1) // the agent note
	m, _ = p.update(m, tea.KeyMsg{Type: tea.KeyEnter})
	v = m.diffLayer()
	if v.hideAgent || m.notesAgentOff {
		t.Fatal("jumping to a hidden agent thread must lift the agent layer, session flag included")
	}
	if v.curLine != 24 {
		t.Fatalf("cursor landed on line index %d, want 24", v.curLine)
	}
	// The box the jump was for is actually in the display stream now.
	var shown bool
	for _, dr := range v.disp {
		if dr.note != nil && strings.Contains(dr.note.text, "second") {
			shown = true
		}
	}
	if !shown {
		t.Fatal("the agent note must be visible after the jump — landing on a line with no box looks like a dead key")
	}
	// A USER thread must not disturb the layer.
	m2 := notedModel(t)
	m2.notesAgentOff = true
	m2.diffLayer().hideAgent = true
	m2.diffLayer().relayout(m2.diffLayer().width)
	m2 = runActionRow(t, m2, "note-list")
	p2 := layerOf[*notesListPopup](m2)
	m2, _ = p2.update(m2, tea.KeyMsg{Type: tea.KeyEnter}) // entry 0 = the user note
	if !m2.diffLayer().hideAgent || !m2.notesAgentOff {
		t.Fatal("jumping to a visible user thread must leave the agent layer alone")
	}
}

func TestNotesListPopupEnterOnAVanishedNoteSaysSo(t *testing.T) {
	t.Parallel()
	m := notedModel(t)
	m = runActionRow(t, m, "note-list")
	p := layerOf[*notesListPopup](m)
	// Another gg (or a `gg note` run) removed the thread under the open list.
	m.diffLayer().notes = m.diffLayer().notes[:1]
	p.move(1)
	m, _ = p.update(m, tea.KeyMsg{Type: tea.KeyEnter})
	if want := i18n.T("Note is no longer in this diff"); !strings.Contains(m.diffNotice, want) {
		t.Fatalf("diffNotice = %q, want it to carry %q instead of a silent no-op", m.diffNotice, want)
	}
}
