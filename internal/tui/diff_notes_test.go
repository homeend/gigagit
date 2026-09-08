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
		Note:   model.Note{ID: "r1", ParentID: "n1", Author: "bot", Summary: "agreed", Source: model.NoteSourceUser},
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

// reply hangs a reply off a resolved root note.
func reply(root domain.ResolvedNote, id, author, summary string, src model.NoteSource) domain.ResolvedNote {
	root.Replies = append(root.Replies, domain.ResolvedNote{
		Note: model.Note{ID: id, ParentID: root.Note.ID, Author: author,
			Summary: summary, Source: src},
		Status: model.NoteActive, Range: root.Range,
	})
	return root
}

// noteRowsAt collects the note rows relayout appended under logical line li.
func noteRowsAt(v *diffView, li int) []noteLine {
	var rows []noteLine
	for i := v.lineStart[li] + 1; i < len(v.disp) && v.disp[i].note != nil; i++ {
		rows = append(rows, *v.disp[i].note)
	}
	return rows
}

func TestAgentLayerFiltersEachRowBySource(t *testing.T) {
	t.Parallel()
	// A user root with an agent reply, and an agent root with a user reply.
	userRoot := reply(rootNote("u", 5, "mine", "", model.NoteSourceUser, model.NoteActive),
		"ur", "bot", "bot's reply", model.NoteSourceAgent)
	agentRoot := reply(rootNote("a", 6, "bot's root", "", model.NoteSourceAgent, model.NoteActive),
		"ar", "ada", "my reply", model.NoteSourceUser)
	v := notedView([]domain.ResolvedNote{userRoot, agentRoot})
	v.hideAgent = true
	v.relayout(0)

	rows5 := noteRowsAt(v, 4) // the user root's line
	if len(rows5) != 1 || !strings.Contains(rows5[0].text, "mine") {
		t.Fatalf("line 5 rows = %+v, want the user root only", rows5)
	}
	rows6 := noteRowsAt(v, 5) // the agent root's line
	if len(rows6) != 1 || !strings.Contains(rows6[0].text, "my reply") {
		t.Fatalf("line 6 rows = %+v, want the user reply only", rows6)
	}
	if rows6[0].depth != 1 {
		t.Fatalf("a surviving reply keeps its indentation, depth = %d", rows6[0].depth)
	}
	// With the layer back on, every row returns.
	v.hideAgent = false
	v.relayout(0)
	if got := len(noteRowsAt(v, 4)) + len(noteRowsAt(v, 5)); got != 4 {
		t.Fatalf("agent layer on: %d rows, want all 4", got)
	}
}

func TestNoteRowsSanitizeControlCharactersAndSplitRationale(t *testing.T) {
	t.Parallel()
	n := rootNote("n1", 5, "sum\nmary", "one\ttab\nsecond\rline", model.NoteSourceUser, model.NoteActive)
	v := notedView([]domain.ResolvedNote{n})
	rows := noteRowsAt(v, 4)
	if len(rows) != 3 { // summary + one row per rationale line
		t.Fatalf("want 3 rows (summary + 2 rationale lines), got %d: %+v", len(rows), rows)
	}
	if !strings.Contains(rows[1].text, "one") || !strings.Contains(rows[2].text, "second") {
		t.Fatalf("the rationale must split on newlines, got %+v", rows[1:])
	}
	// What relayout counted must be what the renderer draws: one physical row
	// each, with no raw control character surviving into the frame.
	physical := 0
	for _, nl := range rows {
		got := noteRowText(nl, 80)
		physical += lipgloss.Height(got)
		if strings.ContainsAny(got, "\n\t\r") {
			t.Fatalf("raw control character survived into %q", got)
		}
	}
	if physical != len(rows) {
		t.Fatalf("%d note display rows rendered as %d physical rows", len(rows), physical)
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
