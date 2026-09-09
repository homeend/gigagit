package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
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
	all := noteRowsAt(v, 4)
	if all[0].kind != noteRowTop || !strings.Contains(all[0].text, "ada") || all[len(all)-1].kind != noteRowBottom {
		t.Fatalf("the box must open with a titled top rule and close with the bottom rule, got %+v", all)
	}
	content := noteContentAt(v, 4)
	got := []string{}
	for _, nl := range content {
		got = append(got, nl.text)
	}
	if len(got) != 3 {
		t.Fatalf("want summary + rationale + reply rows, got %q", got)
	}
	if content[0].kind != noteRowSummary || got[0] != "off by one" {
		t.Fatalf("summary row = %q (kind %d)", got[0], content[0].kind)
	}
	if !strings.Contains(got[1], "the loop runs one short") {
		t.Fatalf("rationale row = %q", got[1])
	}
	if !strings.Contains(got[2], "↳ bot: agreed") || content[2].depth != 1 {
		t.Fatalf("reply row = %q depth %d", got[2], content[2].depth)
	}
	// The NEXT logical line must start after the note rows.
	if want := start + 1 + len(all); v.lineStart[5] != want {
		t.Fatalf("lineStart[5] = %d, want %d (content + the %d box rows)", v.lineStart[5], want, len(all))
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

// noteContentAt is noteRowsAt without the box frame (top/blank/bottom rows):
// the summary and rationale rows a reader actually reads.
func noteContentAt(v *diffView, li int) []noteLine {
	var out []noteLine
	for _, nl := range noteRowsAt(v, li) {
		if nl.kind == noteRowSummary || nl.kind == noteRowText {
			out = append(out, nl)
		}
	}
	return out
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

	rows5 := noteContentAt(v, 4) // the user root's line
	if len(rows5) != 1 || !strings.Contains(rows5[0].text, "mine") {
		t.Fatalf("line 5 rows = %+v, want the user root only", rows5)
	}
	rows6 := noteContentAt(v, 5) // the agent root's line
	if len(rows6) != 1 || !strings.Contains(rows6[0].text, "my reply") {
		t.Fatalf("line 6 rows = %+v, want the user reply only", rows6)
	}
	if rows6[0].depth != 1 {
		t.Fatalf("a surviving reply keeps its indentation, depth = %d", rows6[0].depth)
	}
	// The mixed thread keeps its frame (its title still says whose note it is).
	if frame := noteRowsAt(v, 5); len(frame) == 0 || frame[0].kind != noteRowTop {
		t.Fatalf("a thread with a visible user reply must keep its box, got %+v", frame)
	}
	// With the layer back on, every row returns.
	v.hideAgent = false
	v.relayout(0)
	if got := len(noteContentAt(v, 4)) + len(noteContentAt(v, 5)); got != 4 {
		t.Fatalf("agent layer on: %d content rows, want all 4", got)
	}
}

func TestNoteRowsSanitizeControlCharactersAndSplitRationale(t *testing.T) {
	t.Parallel()
	n := rootNote("n1", 5, "sum\nmary", "one\ttab\nsecond\rline", model.NoteSourceUser, model.NoteActive)
	v := notedView([]domain.ResolvedNote{n})
	rows := noteRowsAt(v, 4)
	// A box: top rule, blank, summary, one row per rationale line, blank, bottom.
	if len(rows) != 7 {
		t.Fatalf("want 7 rows (frame + summary + 2 rationale lines), got %d: %+v", len(rows), rows)
	}
	if rows[2].kind != noteRowSummary || rows[3].kind != noteRowText || rows[4].kind != noteRowText {
		t.Fatalf("row kinds = %+v", rows)
	}
	if !strings.Contains(rows[3].text, "one") || !strings.Contains(rows[4].text, "second") {
		t.Fatalf("the rationale must split on newlines, got %+v", rows[3:5])
	}
	// What relayout counted must be what the renderer draws: one physical row
	// each, with no raw control character surviving into the frame.
	physical := 0
	for _, nl := range rows {
		got := noteRowCells(nl, 40)
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

	stale := noteLine{id: "n", kind: noteRowSummary, text: "gone", stale: true}
	long := noteLine{id: "n", kind: noteRowText, text: strings.Repeat("x", 200)}
	if got := noteRowCells(long, 40); lipgloss.Width(got) != 81 {
		t.Fatalf("a box row must span both panes (40 + │ + 40), got %d cols", lipgloss.Width(got))
	}
	if noteRowCells(stale, 40) == noteRowCells(noteLine{id: "n", kind: noteRowSummary, text: "gone"}, 40) {
		t.Fatal("a stale note must render differently from an active one")
	}
	// The box sits in the pane of its side: an old-side row starts with the
	// frame, a new-side row starts with the blank left pane.
	oldRow := noteRowCells(noteLine{id: "n", kind: noteRowSummary, side: model.NoteSideOld, text: "x"}, 20)
	newRow := noteRowCells(noteLine{id: "n", kind: noteRowSummary, side: model.NoteSideNew, text: "x"}, 20)
	if !strings.HasPrefix(ansi.Strip(oldRow), "│ x") || !strings.HasPrefix(ansi.Strip(newRow), strings.Repeat(" ", 20)+"│") {
		t.Fatalf("box placement: old=%q new=%q", ansi.Strip(oldRow), ansi.Strip(newRow))
	}
	top := ansi.Strip(noteRowCells(noteLine{id: "n", kind: noteRowTop, text: "note · ada · a.go R5"}, 30))
	if !strings.Contains(top, "│╭─ note · ada · a.go R5 ─") || !strings.HasSuffix(top, "─╮") || lipgloss.Width(top) != 61 {
		t.Fatalf("top rule = %q (%d cols)", top, lipgloss.Width(top))
	}
}

// A long pasted summary wraps to the box and is never cut; every wrapped row
// is one physical line of the pane width.
func TestNoteSummaryWrapsInsteadOfTruncating(t *testing.T) {
	t.Parallel()
	words := strings.Repeat("word ", 60)
	n := rootNote("n1", 5, strings.TrimSpace(words), "", model.NoteSourceUser, model.NoteActive)
	v := notedView([]domain.ResolvedNote{n})
	v.relayout(100) // pane 49, inner 45
	rows := noteRowsAt(v, 4)
	sum := 0
	joined := ""
	for _, nl := range rows {
		if nl.kind == noteRowSummary {
			sum++
			joined += nl.text + " "
			if lipgloss.Width(nl.text) > 45 {
				t.Fatalf("summary row wider than the box: %q", nl.text)
			}
		}
	}
	if sum < 6 || strings.Count(joined, "word") != 60 {
		t.Fatalf("summary wrapped into %d rows carrying %d words, want every word kept", sum, strings.Count(joined, "word"))
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
