package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

// notedView builds a 40-row view carrying the given resolved notes.
func notedView(ns []domain.ResolvedNote, changed ...int) *diffView {
	v := diffViewWith(cursorRows(40, changed...), changed)
	v.notes = ns
	v.relayout(0)
	return v
}

func rootNote(id string, line int, summary, rationale string, src model.NoteSource, status model.NoteStatus) domain.ResolvedNote {
	return domain.ResolvedNote{
		Note: model.Note{ID: id, Source: src, Author: "ada", Summary: summary,
			Rationale: rationale, Side: model.NoteSideNew, Range: [2]int{line, line}},
		Status: status, Range: [2]int{line, line},
	}
}

func TestRelayoutAppendsNoteRowsUnderTheirLine(t *testing.T) {
	t.Parallel()
	n := rootNote("n1", 5, "off by one", "the loop runs one short", model.NoteSourceUser, model.NoteActive)
	n.Replies = []domain.ResolvedNote{{
		Note:   model.Note{ID: "r1", ParentID: "n1", Author: "bot", Summary: "agreed", Source: model.NoteSourceAgent},
		Status: model.NoteActive, Range: [2]int{5, 5},
	}}
	v := notedView([]domain.ResolvedNote{n})
	// Line 5 is RightNo 5 => logical line index 4.
	start := v.lineStart[4]
	if v.disp[start].note != nil {
		t.Fatal("the content row must come first")
	}
	got := []string{}
	for i := start + 1; i < len(v.disp) && v.disp[i].note != nil; i++ {
		got = append(got, v.disp[i].note.text)
	}
	if len(got) != 3 {
		t.Fatalf("want summary + rationale + reply rows, got %q", got)
	}
	if !strings.Contains(got[0], "ada") || !strings.Contains(got[0], "off by one") || !strings.HasPrefix(got[0], "◆") {
		t.Fatalf("summary row = %q", got[0])
	}
	if !strings.Contains(got[1], "the loop runs one short") {
		t.Fatalf("rationale row = %q", got[1])
	}
	if !strings.Contains(got[2], "agreed") || v.disp[start+3].note.depth != 1 {
		t.Fatalf("reply row = %q depth %d", got[2], v.disp[start+3].note.depth)
	}
	// The NEXT logical line must start after the note rows.
	if v.lineStart[5] != start+4 {
		t.Fatalf("lineStart[5] = %d, want %d (content + 3 note rows)", v.lineStart[5], start+4)
	}
}

func TestNoteRowsHiddenWhenAgentLayerOff(t *testing.T) {
	t.Parallel()
	user := rootNote("u", 5, "mine", "", model.NoteSourceUser, model.NoteActive)
	agent := rootNote("a", 6, "bot's", "", model.NoteSourceAgent, model.NoteActive)
	v := notedView([]domain.ResolvedNote{user, agent})
	v.hideAgent = true
	v.relayout(0)
	for _, dr := range v.disp {
		if dr.note != nil && strings.Contains(dr.note.text, "bot's") {
			t.Fatal("agent notes must be hidden while the agent layer is off")
		}
	}
	found := false
	for _, dr := range v.disp {
		if dr.note != nil && strings.Contains(dr.note.text, "mine") {
			found = true
		}
	}
	if !found {
		t.Fatal("user notes must stay visible with the agent layer off")
	}
}

func TestCursorRangeStopsBeforeNoteRows(t *testing.T) {
	t.Parallel()
	v := notedView([]domain.ResolvedNote{rootNote("n1", 5, "s", "", model.NoteSourceUser, model.NoteActive)})
	v.setCursorLine(4, 10)
	s, e := v.cursorDispRange()
	if e != s+1 {
		t.Fatalf("cursor range = [%d,%d), want the content row only", s, e)
	}
	// A click on the note row still lands on the OWNING line.
	v.setCursorDisp(s+1, 10)
	if v.curLine != 4 {
		t.Fatalf("clicking a note row put the cursor on line %d, want 4", v.curLine)
	}
}

func TestFoldSeparatorMarkedWhenItHidesANote(t *testing.T) {
	t.Parallel()
	// Changes at 10 and 30 => partial mode folds the rest; line 20 is hidden.
	v := diffViewWith(cursorRows(40, 10, 30), []int{10, 30})
	v.notes = []domain.ResolvedNote{rootNote("n1", 21, "hidden note", "", model.NoteSourceUser, model.NoteActive)}
	v.partial = true
	v.rebuild()
	marked := 0
	for _, dr := range v.disp {
		if dr.fold > 0 && dr.noteMark {
			marked++
		}
		if dr.note != nil {
			t.Fatal("a note whose line is folded away must not get its own row")
		}
	}
	if marked != 1 {
		t.Fatalf("exactly one fold must carry the ◆ marker, got %d", marked)
	}
}

// NOTE: serial (no t.Parallel) — it renders styled text, and the default test
// colour profile is Ascii, under which every style renders as a no-op and the
// stale row would be byte-identical to the active one. lipgloss.SetColorProfile
// is process-global (the house pattern of TestEmphasisActuallyChangesOutput).
func TestNoteRowRendersStaleDimmedAndFitsWidth(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(prev)

	stale := noteLine{id: "n", text: "◆ ada: gone", stale: true}
	long := noteLine{id: "n", text: "◆ ada: " + strings.Repeat("x", 200)}
	if got := noteRowText(long, 40); lipgloss.Width(got) > 40 {
		t.Fatalf("a note row must be truncated to the width, got %d cols", lipgloss.Width(got))
	}
	if noteRowText(stale, 40) == noteRowText(noteLine{id: "n", text: "◆ ada: gone"}, 40) {
		t.Fatal("a stale note must render differently from an active one")
	}
}

func TestRenderWithoutNotesIsUnchanged(t *testing.T) {
	t.Parallel()
	m := openedDiffModel(12, sameRowsTUI(60, 10, 50), []int{10, 50})
	m.width = 140
	before := m.View()
	v := m.diffLayer()
	v.notes = nil
	v.relayout(v.width)
	if got := m.View(); got != before {
		t.Fatal("a view with no notes must render byte-identically")
	}
}
