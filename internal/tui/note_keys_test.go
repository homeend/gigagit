package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

// notedModel opens a diff over 40 rows with notes on lines 5 and 25. The title
// and tag matter: diffNoteAddress() goes through focusedBookmark(), which
// refuses a titleless view, and loadNotesCmd tags its result with m.diffTag.
func notedModel(t *testing.T) Model {
	t.Helper()
	m := openedDiffModel(12, cursorRows(40, 4, 24), []int{4, 24})
	v := m.diffLayer()
	v.title = "a/b.go"
	m.diffTag = statusDiffTag("a/b.go", false)
	v.notes = []domain.ResolvedNote{
		rootNote("n1", 5, "first", "", model.NoteSourceUser, model.NoteActive),
		rootNote("n2", 25, "second", "", model.NoteSourceAgent, model.NoteActive),
	}
	v.relayout(0)
	return m
}

func TestBraceJumpsBetweenAnnotatedLines(t *testing.T) {
	t.Parallel()
	m := notedModel(t)
	v := m.diffLayer()
	v.setCursorLine(0, m.diffBodyRows())
	m, _ = v.update(m, synthKey("}"))
	if m.diffLayer().curLine != 4 {
		t.Fatalf("} from the top = line %d, want 4 (the note on line 5)", m.diffLayer().curLine)
	}
	m, _ = m.diffLayer().update(m, synthKey("}"))
	if m.diffLayer().curLine != 24 {
		t.Fatalf("second } = line %d, want 24", m.diffLayer().curLine)
	}
	m, _ = m.diffLayer().update(m, synthKey("{"))
	if m.diffLayer().curLine != 4 {
		t.Fatalf("{ = line %d, want 4", m.diffLayer().curLine)
	}
}

func TestBraceExpandsAFoldedNote(t *testing.T) {
	t.Parallel()
	m := openedDiffModel(12, cursorRows(40, 4, 34), []int{4, 34})
	v := m.diffLayer()
	v.notes = []domain.ResolvedNote{rootNote("n1", 21, "buried", "", model.NoteSourceUser, model.NoteActive)}
	v.partial = true
	v.rebuild()
	v.setCursorLine(0, m.diffBodyRows())
	m, _ = v.update(m, synthKey("}"))
	v = m.diffLayer()
	if v.partial {
		t.Fatal("} onto a folded note must expand the view (like f)")
	}
	if v.lines[v.curLine].Row.RightNo != 21 {
		t.Fatalf("cursor landed on RightNo %d, want 21", v.lines[v.curLine].Row.RightNo)
	}
}

func TestAgentLayerToggleHidesAgentNotes(t *testing.T) {
	t.Parallel()
	m := notedModel(t)
	m, _ = m.diffLayer().update(m, synthKey("a"))
	if !m.notesAgentOff || !m.diffLayer().hideAgent {
		t.Fatal("a must flip BOTH the session flag and the view's mirror")
	}
	for _, dr := range m.diffLayer().disp {
		if dr.note != nil && strings.Contains(dr.note.text, "second") {
			t.Fatal("the agent note must be gone from the display stream")
		}
	}
}

func TestCOpensTheNotePopupAnchoredAtTheCursor(t *testing.T) {
	t.Parallel()
	m := notedModel(t)
	m.diffLayer().setCursorLine(9, m.diffBodyRows())
	m, _ = m.diffLayer().update(m, synthKey("c"))
	p, ok := m.topLayer().(*notePopup)
	if !ok {
		t.Fatalf("c must push a notePopup, top is %T", m.topLayer())
	}
	if p.mode != noteAdd || p.line != 10 || p.side != model.NoteSideNew {
		t.Fatalf("popup anchor = mode %v line %d side %q", p.mode, p.line, p.side)
	}
	// An empty summary cancels: esc-equivalent, nothing submitted.
	// synthKey maps only enter/esc/space to a Type — ctrl+s must be built by hand.
	m, cmd := p.update(m, tea.KeyMsg{Type: tea.KeyCtrlS})
	if _, still := m.topLayer().(*notePopup); still {
		t.Fatal("ctrl+s with an empty summary must close the popup")
	}
	if cmd != nil {
		t.Fatal("ctrl+s with an empty summary must not dispatch a write")
	}
}

func TestERTargetTheNoteNearestAboveTheCursor(t *testing.T) {
	t.Parallel()
	m := notedModel(t)
	m.diffLayer().setCursorLine(30, m.diffBodyRows()) // below both notes
	m, _ = m.diffLayer().update(m, synthKey("E"))
	p, ok := m.topLayer().(*notePopup)
	if !ok || p.mode != noteEdit || p.targetID != "n2" {
		t.Fatalf("E must edit the nearest note above the cursor, got %#v (ok %v)", p, ok)
	}
	m = m.popLayer()
	m, _ = m.diffLayer().update(m, synthKey("R"))
	p, _ = m.topLayer().(*notePopup)
	if p == nil || p.mode != noteReply || p.targetID != "n2" {
		t.Fatalf("R must reply to the same note, got %#v", p)
	}
}

func TestNoteKeysInertWithoutNotes(t *testing.T) {
	t.Parallel()
	m := openedDiffModel(12, cursorRows(40), nil)
	for _, k := range []string{"E", "R", "}", "{"} {
		mm, _ := m.diffLayer().update(m, synthKey(k))
		if _, isPopup := mm.topLayer().(*notePopup); isPopup {
			t.Fatalf("%s must be inert with no notes", k)
		}
	}
}

func TestSrcNotesRegistered(t *testing.T) {
	t.Parallel()
	if sourceNames[srcNotes] != "notes" || sourceDisplayName(srcNotes) != "notes" {
		t.Fatal("srcNotes needs a name and a display name")
	}
	if len(srcConsumers[srcNotes]) == 0 {
		t.Fatal("srcNotes must list its consumer panels")
	}
	for _, it := range scheduledItems {
		if !it.isFetch && !it.isRemoteTags && it.source == srcNotes {
			t.Fatal("srcNotes must never be polled by the background scheduler")
		}
	}
}

func TestNoteDeleteRowOnlyWithANoteInReach(t *testing.T) {
	t.Parallel()
	m := notedModel(t)
	m.diffLayer().setCursorLine(30, m.diffBodyRows())
	if _, ok := m.noteDeleteRow(); !ok {
		t.Fatal("the . menu must offer Delete note when a note sits above the cursor")
	}
	m2 := openedDiffModel(12, cursorRows(40), nil)
	if _, ok := m2.noteDeleteRow(); ok {
		t.Fatal("no notes ⇒ no Delete note row")
	}
}

var _ = tea.KeyMsg{}
