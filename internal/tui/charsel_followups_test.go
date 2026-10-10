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

// A fixed range that covers no rune (an empty row alone) copies nothing: the
// mode says so and stays, never leaving in silence.
func TestDiffCharSelEmptySelectionSaysSoAndStays(t *testing.T) {
	t.Parallel()
	m := openedDiffModel(12, textRows("abc", "", "def"), nil)
	v := m.diffLayer()
	v.setCursorLine(1, m.diffBodyRows())
	got := "untouched"
	m = captureClip(m, &got)
	m = feedDiff(m, "v", "space")
	u, cmd := m.Update(keyMsg("enter"))
	m = drainCmds(t, u.(Model), cmd)
	if got != "untouched" || !v.cs.on || m.diffNotice != "▸ the selection is empty — nothing to copy" {
		t.Fatalf("clip %q on=%v notice %q", got, v.cs.on, m.diffNotice)
	}
}

// A CRLF file's lines end in \r: the diff never shows it, so neither
// selection may copy it, and the character cursor never sits on it.
func TestDiffSelectionsDropTheCarriageReturn(t *testing.T) {
	t.Parallel()
	m := openedDiffModel(12, textRows("ab\r", "cd\r"), nil)
	v := m.diffLayer()
	v.setCursorLine(0, m.diffBodyRows())
	var got string
	m = captureClip(m, &got)
	m = feedDiff(m, "v", "end")
	if v.cs.cur.col != 1 {
		t.Fatalf("end lands on col %d, want 1 (b, not the \\r)", v.cs.cur.col)
	}
	m = feedDiff(m, "home", "space", "j", "end")
	u, cmd := m.Update(keyMsg("enter"))
	m = drainCmds(t, u.(Model), cmd)
	if got != "ab\ncd" {
		t.Fatalf("character selection copied %q", got)
	}
	v.setCursorLine(0, m.diffBodyRows())
	m = feedDiff(m, "space", "j")
	u, cmd = m.Update(keyMsg("enter"))
	drainCmds(t, u.(Model), cmd)
	if got != "ab\ncd" {
		t.Fatalf("line selection copied %q", got)
	}
}

// v in the popup's cutoff or scroll mode lays the text out wrapped while the
// mode is on — a cut row would let the cursor walk past what the box shows —
// and leaving puts the reader's mode back.
func TestSummaryCharSelWrapsWhileOn(t *testing.T) {
	t.Parallel()
	for _, presses := range []int{1, 2} {
		m, p := openedSummaryPopup(t)
		for k := 0; k < presses; k++ {
			m = feedKeys(m, "ctrl+w")
		}
		mode := p.mode
		if mode == modeWrap {
			t.Fatalf("%d ctrl+w presses left the popup wrapped", presses)
		}
		m = feedKeys(m, "down", "down", "down") // the pager top on line 3, the long paragraph
		m = feedKeys(m, "v")
		if !p.cs.on || p.mode != modeWrap {
			t.Fatalf("v in mode %v: on=%v mode %v, want wrapped", mode, p.cs.on, p.mode)
		}
		if line := p.csSeg[p.cs.cur.row].line; line != 3 || p.csSeg[p.sel].line != 3 {
			t.Fatalf("v: the cursor on line %d, the pager top on line %d — want both on line 3", line, p.csSeg[p.sel].line)
		}
		if long := len(p.csRows); long < 5 {
			t.Fatalf("the long paragraph must wrap into several rows, got %d rows", long)
		}
		m = feedKeys(m, "esc")
		if p.cs.on || p.mode != mode || p.sel != 3 {
			t.Fatalf("after esc: on=%v mode %v top %d, want %v back on line 3", p.cs.on, p.mode, p.sel, mode)
		}
		_ = m
	}
}
