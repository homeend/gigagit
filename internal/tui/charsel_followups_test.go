package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/homeend/gigagit/internal/textdiff"
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

const charSelListDoc = `{"version":1,"summary":"Items:\n\n10. The tenth item is long enough to wrap inside the reading column more than once, so that its hang indent shows on the continuation rows below it.\n11. short","files":[{"path":"a.go","annotations":[{"newRange":[1,1],"summary":"adds A"}]}]}`

// A numbered item's continuation rows hang behind a lead wider than two
// runes ("10. "): the copy never carries the hang indent, and the stripe
// lands on the very runes it copies — in the popup and in the stack.
func TestCharSelNumberedListLead(t *testing.T) {
	t.Parallel()
	m, id := reviewViewModel(t, charSelListDoc)
	m, cmd := m.openReview(id, "Review")
	m = drainCmds(t, m, cmd)
	pm, _ := m.openReviewSummary()
	p := pm.topLayer().(*reviewSummaryPopup)
	var got string
	pm = captureClip(pm, &got)
	pm = feedKeys(pm, "v")
	cont := -1
	for i, r := range p.csRows {
		if r.wraps && strings.HasPrefix(string(p.csRows[i-1].text), "10.") {
			cont = i
			break
		}
	}
	if cont < 0 {
		t.Fatalf("no continuation of the numbered item: %q", charRowTexts(p.csRows))
	}
	for p.cs.cur.row < cont {
		pm = feedKeys(pm, "j")
	}
	pm = feedKeys(pm, "home", "space", "l")
	seg := p.csSeg[cont]
	if seg.off <= 2 {
		t.Fatalf("the continuation sits at %d, want behind the 2-rune lead AND the hang indent", seg.off)
	}
	wr := []rune(p.csRows[cont].text)
	n := seg.off + len(wr)
	mask := p.charEmphFor(seg.line, n+8)
	if mask == nil || mask[seg.off] != emphSel || mask[seg.off+1] != emphSelCur {
		t.Fatalf("popup mask at off %d = %v", seg.off, mask)
	}
	u, cmd := pm.Update(keyMsg("enter"))
	drainCmds(t, u.(Model), cmd)
	if want := string(wr[:2]); got != want || strings.HasPrefix(got, " ") {
		t.Fatalf("popup copied %q, want %q", got, want)
	}

	// The stack's summary element.
	m = m.setStackedPref(true)
	l, _ := filesLine(t, m, "a.go")
	su, cmd := m.openDiffForFileLine(l)
	sm := drainCmds(t, su.(Model), cmd)
	v := sm.diffLayer()
	sm = captureClip(sm, &got)
	contLine := -1
	for i, ln := range v.lines {
		if ln.kind != lineProse {
			continue
		}
		f, _ := v.stackFileAt(i)
		if r := f.prose[ln.prose]; r.cont && r.lead > 2 {
			contLine = i
			break
		}
	}
	if contLine < 0 {
		t.Fatal("no prose continuation with a lead wider than two runes")
	}
	v.setCursorLine(contLine, sm.diffBodyRows())
	sm = feedDiff(sm, "v", "space", "l")
	f, _ := v.stackFileAt(contLine)
	row := f.prose[v.lines[contLine].prose]
	pmask := v.charEmphProse(contLine, len([]rune(row.text)))
	if pmask == nil || pmask[row.lead] != emphSel || pmask[row.lead+1] != emphSelCur {
		t.Fatalf("stack mask (lead %d) = %v", row.lead, pmask)
	}
	u, cmd = sm.Update(keyMsg("enter"))
	drainCmds(t, u.(Model), cmd)
	if want := string([]rune(row.text)[row.lead : row.lead+2]); got != want {
		t.Fatalf("stack copied %q, want %q", got, want)
	}
}

// pgdown / pgup step a real page: the body's row count, the line cursor and
// the view following.
func TestDiffCharSelPagesByTheBody(t *testing.T) {
	t.Parallel()
	m := openedDiffModel(12, sameRowsTUI(60, 10, 50), nil)
	v := m.diffLayer()
	v.setCursorLine(0, m.diffBodyRows())
	page := m.diffBodyRows()
	if page < 2 {
		t.Fatalf("body %d rows: the page must be more than one", page)
	}
	m = feedDiff(m, "v", "pgdown")
	if v.cs.cur.row != page || v.curLine != page {
		t.Fatalf("pgdown: row %d curLine %d, want %d", v.cs.cur.row, v.curLine, page)
	}
	if v.curLine < v.offset || v.curLine >= v.offset+page {
		t.Fatalf("the cursor line %d left the view [%d, %d)", v.curLine, v.offset, v.offset+page)
	}
	m = feedDiff(m, "pgup")
	if v.cs.cur.row != 0 || v.offset != 0 {
		t.Fatalf("pgup: row %d offset %d, want 0 0", v.cs.cur.row, v.offset)
	}
}

// Review Focus 1 inside a host: wide runes are one step each and copy whole.
func TestDiffCharSelWideRunes(t *testing.T) {
	t.Parallel()
	m := openedDiffModel(12, textRows("日本語 ok"), nil)
	v := m.diffLayer()
	v.setCursorLine(0, m.diffBodyRows())
	var got string
	m = captureClip(m, &got)
	m = feedDiff(m, "v", "l", "space", "l")
	if sp := v.charSpansOn(0); len(sp) != 2 || sp[0].start != 1 || sp[0].end != 3 {
		t.Fatalf("spans %+v, want the stripe over runes 1..3 (本語)", sp)
	}
	u, cmd := m.Update(keyMsg("enter"))
	drainCmds(t, u.(Model), cmd)
	if got != "本語" {
		t.Fatalf("copied %q, want 本語", got)
	}
}

// v on a folded file: its element has no live row — nothing to select, and
// the mode stays off.
func TestStackCharSelOnACollapsedFile(t *testing.T) {
	t.Parallel()
	m, v, _ := openedReviewStack(t)
	v.setCursorLine(v.stk.files[1].hdr, m.diffBodyRows())
	m = feedDiff(m, "-")
	if !v.stk.files[1].collapsed {
		t.Fatal("- must fold a.go")
	}
	m = feedDiff(m, "v")
	if v.cs.on || m.diffNotice != "▸ nothing to select" {
		t.Fatalf("v on a folded file: on=%v notice %q", v.cs.on, m.diffNotice)
	}
}

// A reload (a diffMsg for the open view) arriving mid-mode leaves it: the
// line indexes now name other lines.
func TestDiffCharSelLeavesOnAReload(t *testing.T) {
	t.Parallel()
	m := openedDiffModel(12, textRows("one", "two", "three"), nil)
	m = feedDiff(m, "v", "space", "l")
	loaded := diffViewWith([]textdiff.Row{{Kind: textdiff.Same, Left: "x", Right: "x", LeftNo: 1, RightNo: 1}}, nil)
	loaded.relayout(m.width)
	u, _ := m.Update(diffMsg{tag: m.diffTag, view: loaded})
	m = u.(Model)
	if v := m.diffLayer(); v.cs.on {
		t.Fatalf("a reload must leave the mode: %+v", v.cs)
	}
	if !strings.Contains(m.renderDiffView(), "x") {
		t.Fatal("the reloaded text must show")
	}
}

// The wheel scrolls the Summary popup like ↑/↓ — and, in the copy mode,
// moves the pager only (the preview's rule): the cursor stays where it is,
// and the next key brings the view back to it.
func TestSummaryPopupWheelScrolls(t *testing.T) {
	t.Parallel()
	m, p := openedSummaryPopup(t)
	wheel := func(m Model, b tea.MouseButton) Model {
		u, _ := m.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: b, X: 50, Y: 10})
		return u.(Model)
	}
	m = wheel(m, tea.MouseButtonWheelDown)
	if p.sel == 0 {
		t.Fatal("the wheel must scroll the Summary popup")
	}
	m = wheel(m, tea.MouseButtonWheelUp)
	if p.sel != 0 {
		t.Fatalf("wheel up: top %d, want 0", p.sel)
	}
	m = feedKeys(m, "v")
	cur := p.cs.cur
	m = wheel(m, tea.MouseButtonWheelDown)
	if p.sel == 0 || p.cs.cur != cur || !p.cs.on {
		t.Fatalf("in the mode the wheel moves the pager only: top %d cursor %+v (was %+v)", p.sel, p.cs.cur, cur)
	}
	m = feedKeys(m, "l")
	if p.cs.cur.row < p.sel {
		t.Fatalf("the next key must bring the cursor row %d back into view (top %d)", p.cs.cur.row, p.sel)
	}
	_ = m
}
