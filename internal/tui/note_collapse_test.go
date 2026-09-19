package tui

import (
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

func noteRowsOf(v *diffView, rootID string) []noteLine {
	var out []noteLine
	for _, d := range v.disp {
		if d.note != nil && d.note.rootID == rootID {
			out = append(out, *d.note)
		}
	}
	return out
}

func TestCollapsedNoteIsOneRow(t *testing.T) {
	t.Parallel()
	m := notedModel(t)
	v := m.diffLayer()
	r := rootNote("n1", 5, "first line of the summary", "a long rationale\nover two lines", model.NoteSourceUser, model.NoteActive)
	r.Replies = []domain.ResolvedNote{rootNote("r1", 5, "reply", "", model.NoteSourceUser, model.NoteActive), rootNote("r2", 5, "reply 2", "", model.NoteSourceUser, model.NoteActive)}
	v.notes = []domain.ResolvedNote{r}
	v.relayout(0)
	open := len(noteRowsOf(v, "n1"))
	if open < 6 {
		t.Fatalf("an open box has its frame, summary, rationale and replies; got %d rows", open)
	}
	before := len(v.disp)
	v.collapsed = map[string]bool{"n1": true}
	v.relayout(0)
	rows := noteRowsOf(v, "n1")
	if len(rows) != 1 || rows[0].kind != noteRowCollapsed {
		t.Fatalf("collapsed rows = %+v", rows)
	}
	if got := rows[0].text; got != "ada: first line of the summary (2 replies)" {
		t.Fatalf("collapsed text = %q", got)
	}
	if len(v.disp) != before-open+1 {
		t.Fatalf("display rows = %d, want %d", len(v.disp), before-open+1)
	}
	cell := noteRowCells(rows[0], 60)
	if !strings.Contains(cell, "▸ ada: first line") {
		t.Fatalf("painted row = %q", cell)
	}
}

func TestOTogglesTheNoteAtTheCursor(t *testing.T) {
	t.Parallel()
	m := forgeNotedModel(t)
	v := m.diffLayer()
	v.notes = []domain.ResolvedNote{forgeRoot("C1", 5, "rename this", false)}
	v.relayout(0)
	v.setCursorLine(4, m.diffBodyRows())
	m, _ = v.update(m, synthKey("o"))
	v = m.diffLayer()
	if !v.collapsed["forge:C1"] || len(noteRowsOf(v, "forge:C1")) != 1 {
		t.Fatalf("o must collapse the (read-only) thread at the cursor: %v", v.collapsed)
	}
	if v.curLine != 4 {
		t.Fatalf("the cursor moved to %d", v.curLine)
	}
	m, _ = v.update(m, synthKey("o"))
	if v = m.diffLayer(); v.collapsed["forge:C1"] {
		t.Fatal("o again expands it")
	}
	// Away from any note, o does nothing.
	v.setCursorLine(15, m.diffBodyRows())
	m, _ = v.update(m, synthKey("o"))
	if len(m.diffLayer().collapsed) != 0 && m.diffLayer().collapsed["forge:C1"] {
		t.Fatal("o with no note in reach changed something")
	}
}

func TestShiftOCollapsesAndExpandsAll(t *testing.T) {
	t.Parallel()
	m := notedModel(t) // notes n1 (line 5) and n2 (line 25)
	v := m.diffLayer()
	m, _ = v.update(m, synthKey("O"))
	v = m.diffLayer()
	if !v.collapsed["n1"] || !v.collapsed["n2"] {
		t.Fatalf("O with anything open collapses all: %v", v.collapsed)
	}
	m, _ = v.update(m, synthKey("O"))
	if v = m.diffLayer(); v.collapsed["n1"] || v.collapsed["n2"] {
		t.Fatalf("O with everything collapsed expands all: %v", v.collapsed)
	}
}

func TestResolvedForgeThreadsStartCollapsedOnce(t *testing.T) {
	t.Parallel()
	m := notedModel(t)
	v := m.diffLayer()
	notes := []domain.ResolvedNote{forgeRoot("C1", 5, "open thread", false), forgeRoot("C2", 25, "settled", true)}
	v.setNotes(notes)
	if v.collapsed["forge:C1"] || !v.collapsed["forge:C2"] {
		t.Fatalf("only the resolved thread starts collapsed: %v", v.collapsed)
	}
	// The user expands it; a reload (a comment re-poll) must not fold it again.
	v.collapsed["forge:C2"] = false
	v.setNotes(notes)
	if v.collapsed["forge:C2"] {
		t.Fatal("a reload re-collapsed a thread the user had opened")
	}
}

func TestBraceStillLandsOnACollapsedNote(t *testing.T) {
	t.Parallel()
	m := notedModel(t)
	v := m.diffLayer()
	v.collapsed = map[string]bool{"n1": true, "n2": true}
	v.relayout(0)
	v.setCursorLine(0, m.diffBodyRows())
	m, _ = v.update(m, synthKey("}"))
	if m.diffLayer().curLine != 4 {
		t.Fatalf("} = line %d, want 4", m.diffLayer().curLine)
	}
}
