package tui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/homeend/gigagit/internal/theme"
)

// NOTE: the paint tests here set the process-global colour profile — not
// parallel (see diff_select_test.go).

// terminalPaint switches to the terminal theme under a 256-colour profile
// (the cursor is a reverse cell there) and returns the undo.
func terminalPaint() func() {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	prevTheme := activeTheme()
	setTheme(theme.Terminal)
	return func() {
		setTheme(prevTheme)
		lipgloss.SetColorProfile(prev)
	}
}

// reverseCell matches an SGR sequence that turns reverse video on (the 7
// parameter, alone or among others — never the 7 of a 237 colour).
var reverseCell = regexp.MustCompile(`\x1b\[(?:[0-9;]*;)?7(?:;[0-9;]*)?m `)

// The cursor shows on an EMPTY row of the diff: a reversed cell at column 0,
// where the row has no rune to reverse.
func TestDiffCharSelCursorShowsOnAnEmptyRow(t *testing.T) {
	defer terminalPaint()()
	m := openedDiffModel(12, textRows("abc", "", "def"), nil)
	v := m.diffLayer()
	v.setCursorLine(1, m.diffBodyRows())
	off := strings.Split(m.renderDiffView(), "\n")[2]
	m = feedDiff(m, "v")
	on := strings.Split(m.renderDiffView(), "\n")[2]
	right := on[strings.Index(on, "│"):]
	if on == off || !reverseCell.MatchString(right) {
		t.Fatalf("the empty row must show the cursor on its side:\n on  %q\n off %q", on, off)
	}
}

// …in the Summary popup: the blank line between two paragraphs.
func TestSummaryCharSelCursorShowsOnAnEmptyRow(t *testing.T) {
	defer terminalPaint()()
	m, p := openedSummaryPopup(t)
	m = feedKeys(m, "v")
	for len(p.csRows[p.cs.cur.row].text) > 0 {
		before := p.cs.cur.row
		m = feedKeys(m, "j")
		if p.cs.cur.row == before {
			t.Fatalf("no empty row among %q", charRowTexts(p.csRows))
		}
	}
	if !reverseCell.MatchString(m.View()) {
		t.Fatal("no reversed cell on the empty row")
	}
}

// …and in the stack's summary element.
func TestStackCharSelCursorShowsOnAnEmptyRow(t *testing.T) {
	defer terminalPaint()()
	m, v, first := openedReviewStack(t)
	v.setCursorLine(first, m.diffBodyRows())
	m = feedDiff(m, "v")
	rows := v.charRows()
	for len(rows[v.cs.cur.row].text) > 0 {
		before := v.cs.cur.row
		m = feedDiff(m, "j")
		if v.cs.cur.row == before {
			t.Fatalf("no empty prose row among %q", charRowTexts(rows))
		}
	}
	line := strings.Split(m.renderDiffView(), "\n")[v.curLine-v.offset+1]
	if !reverseCell.MatchString(line) {
		t.Fatalf("no reversed cell on the empty prose row: %q", line)
	}
}

// v on a row the mode skips (a stack header) lands the character cursor on
// the first live row — and the line cursor goes there AT ONCE, not on the
// next key.
func TestStackCharSelFromAHeaderMovesTheLineCursorAtOnce(t *testing.T) {
	t.Parallel()
	m, v, first := openedReviewStack(t)
	v.setCursorLine(v.stk.files[0].hdr, m.diffBodyRows())
	m = feedDiff(m, "v")
	if !v.cs.on || v.curLine != first {
		t.Fatalf("v on the header: curLine %d, want the first prose line %d", v.curLine, first)
	}
}
