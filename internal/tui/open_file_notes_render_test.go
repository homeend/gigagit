package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// notedViewer is a 40-line file ("line 1"…) open in the full-screen viewer.
func notedViewer(t *testing.T) (Model, *openFile) {
	t.Helper()
	m := loadedNavModel(t)
	d := newOpenFile(fileSource{kind: srcWorktree}, "a.txt")
	d.fill(fileContentMsg{lines: docLines(40)}, 10, 80)
	m.openFiles.touch(m.currentWorktree, d, m.docShown)
	return m.pushLayer(&fileViewer{d}), d
}

func boxText(m Model, d *openFile, w, h int) []string {
	return strings.Split(ansi.Strip(m.renderPreviewBox(d.p, "a.txt", w, h, true, true)), "\n")
}

func indexOf(lines []string, sub string) int {
	for i, l := range lines {
		if strings.Contains(l, sub) {
			return i
		}
	}
	return -1
}

func TestFileWithoutNotesRendersExactlyAsBefore(t *testing.T) {
	t.Parallel()
	m, d := notedViewer(t)
	d.backgrounded = true // a note makes the file stay open on esc, and that outlives the note
	before := m.renderPreviewBox(d.p, "a.txt", 80, 20, true, true)
	n, _ := d.addNote(2, 2, "s", "", "")
	d.removeNote(n.id)
	d.syncNoteRows()
	if after := m.renderPreviewBox(d.p, "a.txt", 80, 20, true, true); after != before {
		t.Fatal("a file whose notes are gone renders differently from one that never had any")
	}
}

func TestFileNoteBoxSitsUnderItsLastLine(t *testing.T) {
	t.Parallel()
	m, d := notedViewer(t)
	if _, err := d.addNote(2, 3, "the summary", "why it matters", ""); err != nil {
		t.Fatal(err)
	}
	out := boxText(m, d, 80, 24)
	l3, top, sum, why, l4 := indexOf(out, "line 3"), indexOf(out, "lines 2-3"), indexOf(out, "the summary"), indexOf(out, "why it matters"), indexOf(out, "line 4")
	if !(l3 >= 0 && l3 < top && top < sum && sum < why && why < l4) {
		t.Fatalf("order line3=%d title=%d summary=%d rationale=%d line4=%d\n%s", l3, top, sum, why, l4, strings.Join(out, "\n"))
	}
	if !strings.Contains(out[top], "agent") || !strings.Contains(out[top], "╭") {
		t.Errorf("title row = %q, want the framed `agent · t<n> · lines 2-3`", out[top])
	}
	for _, ln := range []int{indexOf(out, "line 2"), l3} {
		if !strings.Contains(out[ln], "│ line") {
			t.Errorf("covered row %q lacks the range mark", out[ln])
		}
	}
	if strings.Contains(out[l4], "│ line") {
		t.Errorf("row %q is outside the range but wears the mark", out[l4])
	}
}

func TestOutdatedFileNoteSaysSoInItsTitle(t *testing.T) {
	t.Parallel()
	m, d := notedViewer(t)
	n, _ := d.addNote(2, 2, "s", "", "")
	n.outdated = true
	out := boxText(m, d, 80, 24)
	if i := indexOf(out, "outdated"); i < 0 || !strings.Contains(out[i], "line 2") {
		t.Fatalf("no `… line 2 · outdated` title:\n%s", strings.Join(out, "\n"))
	}
}

// Review Focus 1.
func TestFileNoteBoxOnTheLastLineIsReachable(t *testing.T) {
	t.Parallel()
	m, d := notedViewer(t)
	if _, err := d.addNote(40, 40, "at the very end", "", ""); err != nil {
		t.Fatal(err)
	}
	boxText(m, d, 80, 20)                      // lays the box out (noteW)
	d.p.sel = d.p.clampTop(len(d.p.lines), 16) // what `end` does in an 80x20 viewer box (16 rows)
	out := boxText(m, d, 80, 20)
	if indexOf(out, "line 40") < 0 || indexOf(out, "at the very end") < 0 || indexOf(out, "╰") < 0 {
		t.Fatalf("scrolled to the end, the last note's box is not fully shown:\n%s", strings.Join(out, "\n"))
	}
}

func TestCursorStaysVisibleBelowATallNote(t *testing.T) {
	t.Parallel()
	m, d := notedViewer(t)
	if _, err := d.addNote(2, 2, "s", strings.Repeat("a rationale line\n", 12), ""); err != nil {
		t.Fatal(err)
	}
	m.renderPreviewBox(d.p, "a.txt", 80, 20, true, true) // lays the box out (noteW)
	rowsCap := 20 - 2 - 2
	d.p.sel, d.p.cur = 0, 0
	for i := 0; i < 6; i++ {
		d.p.cur++
		d.p.ensureCursorVisible(rowsCap)
	}
	out := boxText(m, d, 80, 20)
	if indexOf(out, "line 7") < 0 {
		t.Fatalf("the cursor line (7) left the window:\n%s", strings.Join(out, "\n"))
	}
}

// Review Focus 2.
func TestFileNoteBoxRowsAreExactlyTheBoxWidth(t *testing.T) {
	t.Parallel()
	m, d := notedViewer(t)
	hostile := "tab\there \x1b[31mred\x1b[0m\rcr " + strings.Repeat("W", 300) + " 日本語の長い説明文"
	if _, err := d.addNote(1, 1, hostile, "one\ntwo\n\nfour "+hostile, ""); err != nil {
		t.Fatal(err)
	}
	raw := strings.Split(m.renderPreviewBox(d.p, "a.txt", 60, 40, true, true), "\n")
	for i, l := range raw {
		if w := lipgloss.Width(l); w != 60 {
			t.Fatalf("row %d is %d columns wide, want 60: %q", i, w, ansi.Strip(l))
		}
	}
	if len(raw) != 40 {
		t.Fatalf("the box is %d rows tall, want 40", len(raw))
	}
}

// Review Focus 3.
func TestFileNoteBoxSurvivesANarrowBox(t *testing.T) {
	t.Parallel()
	m, d := notedViewer(t)
	if _, err := d.addNote(1, 2, "summary text", "rationale text", ""); err != nil {
		t.Fatal(err)
	}
	for _, w := range []int{1, 4, 6, 9, 12} {
		_ = m.renderPreviewBox(d.p, "a.txt", w, 10, true, true) // must not panic
	}
}
