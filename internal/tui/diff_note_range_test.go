package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

// A note about several lines draws a bar beside every one of them, on its own
// side only, and the note's box still sits under the last.
func TestRangedNoteMarksItsLines(t *testing.T) {
	t.Parallel()
	n := rootNote("n1", 5, "these three", "", model.NoteSourceUser, model.NoteActive)
	n.Range, n.Note.Range = [2]int{3, 5}, [2]int{3, 5}
	v := notedView([]domain.ResolvedNote{n})
	m := diffModel()
	plainView := notedView(nil)
	got := m.diffPaneLines(v, 80, 12, 0, 0, "off")
	plain := m.diffPaneLines(plainView, 80, 12, 0, 0, "off")

	barred := map[int]bool{}
	for i, line := range got {
		if i >= len(v.disp) || v.disp[i].note != nil {
			continue
		}
		left, right, _ := strings.Cut(ansiStrip(line), "│")
		if strings.Contains(left, noteBarGlyph) {
			t.Errorf("row %d: the OLD pane carries a bar for a new-side note: %q", i, left)
		}
		if strings.Contains(right, noteBarGlyph) {
			no := v.disp[i].row.RightNo
			barred[no] = true
			// The bar sits in the separator column: the number stays whole.
			if want := fmt.Sprintf("%d%s", no, noteBarGlyph); !strings.Contains(right, want) || strings.Contains(right, "…") {
				t.Errorf("new line %d: gutter %q, want the number then the bar", no, right[:min(len(right), 12)])
			}
		}
	}
	for no := 1; no <= 7; no++ {
		if want := no >= 3 && no <= 5; barred[no] != want {
			t.Errorf("new line %d: bar = %v, want %v", no, barred[no], want)
		}
	}
	for i := range plain {
		if i < 4 && lipgloss.Width(got[i]) != lipgloss.Width(plain[i]) {
			t.Errorf("row %d: width %d with the bar, %d without", i, lipgloss.Width(got[i]), lipgloss.Width(plain[i]))
		}
	}

	// A single-line note marks nothing.
	one := notedView([]domain.ResolvedNote{rootNote("n2", 4, "one line", "", model.NoteSourceUser, model.NoteActive)})
	for _, line := range m.diffPaneLines(one, 80, 12, 0, 0, "off") {
		if strings.Contains(line, noteBarGlyph) {
			t.Fatalf("a one-line note drew a bar: %q", ansiStrip(line))
		}
	}

	// A hidden agent layer hides its ranges.
	n.Note.Source = model.NoteSourceAgent
	av := notedView([]domain.ResolvedNote{n})
	av.hideAgent = true
	if len(av.noteSpans()) != 0 {
		t.Error("hidden agent notes must not mark their lines")
	}
}

// A note whose anchor line is not in the view draws no box, so no bar either.
func TestRangedNoteWithoutABoxDrawsNoBar(t *testing.T) {
	t.Parallel()
	n := rootNote("n1", 5, "gone", "", model.NoteSourceUser, model.NoteStale)
	n.Range, n.Note.Range = [2]int{3, 900}, [2]int{3, 900} // ends on a line this file does not have
	v := notedView([]domain.ResolvedNote{n})
	if byLine, _ := v.noteRowIndex(); len(byLine) != 0 {
		t.Fatalf("fixture: the note still has a box (%d lines)", len(byLine))
	}
	if spans := v.noteSpans(); len(spans) != 0 {
		t.Errorf("spans = %+v, want none for a note with no box", spans)
	}
	for _, line := range diffModel().diffPaneLines(v, 80, 12, 0, 0, "off") {
		if strings.Contains(line, noteBarGlyph) {
			t.Fatalf("a bar with no box: %q", ansiStrip(line))
		}
	}
}
