package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/syntax"
	"github.com/homeend/gigagit/internal/theme"
)

// NOTE: TestBlameSelectionPaintsTheCodeNotTheGutter,
// TestBlameSelectionUnderReverseDropsSyntaxColours and
// TestRenderWindowNilBodyIsUnchanged call lipgloss.SetColorProfile
// (process-global) and therefore do NOT call t.Parallel().

// space / move / space / enter copies exactly the frozen range, read off the
// action row the key runs.
func TestBlameSelectionCopies(t *testing.T) {
	t.Parallel()
	m, b := blameSearchModel()
	m = typeBlame(m, b, "space", "down", "space")
	if !b.lsel.on || !b.lsel.fixed {
		t.Fatalf("two spaces must freeze the range: %+v", b.lsel)
	}
	row, ok := rowByID(m.contextCopyRows(), "copy-selected-lines")
	if !ok {
		t.Fatal("no Copy selected lines row while a blame selection is on")
	}
	if row.label != "Copy selected lines (2)" {
		t.Fatalf("label = %q, want `Copy selected lines (2)`", row.label)
	}
	if row.copyText != "package main\nfunc main() {}" {
		t.Fatalf("copyText = %q, want the two blame lines joined with \\n", row.copyText)
	}

	m, cmd := b.update(m, keyMsg("enter"))
	if cmd == nil {
		t.Fatal("enter with a selection on must issue the clipboard command")
	}
	if b.lsel.on {
		t.Fatal("enter must clear the selection after copying")
	}
	if _, ok := m.topLayer().(*historyView); ok {
		t.Fatal("enter with a selection on must NOT open the history view")
	}
}

// Copy line is always offered — every blame row IS a line — and names the
// file's own line number.
func TestBlameCopyLineRow(t *testing.T) {
	t.Parallel()
	m, b := blameSearchModel()
	b.sel = 2 // the uncommitted "dirty" line, LineNo 3
	row, ok := rowByID(m.contextCopyRows(), "copy-line")
	if !ok {
		t.Fatal("blame must always offer Copy line")
	}
	if row.copyText != "dirty" {
		t.Fatalf("copyText = %q, want the raw line content", row.copyText)
	}
	if row.label != "Copy line" {
		t.Fatalf("label = %q, want `Copy line`", row.label)
	}
	// The path/name/commit rows the blame surface already offered are still there.
	if _, ok := rowByID(m.contextCopyRows(), "copy-file-path"); !ok {
		t.Fatal("the file copy rows must survive under the line rows")
	}
}

// enter WITHOUT a selection keeps its old meaning: the history of the commit
// under the cursor.
func TestBlameEnterWithoutSelectionOpensHistory(t *testing.T) {
	t.Parallel()
	m, b := blameSearchModel()
	b.sel = 0 // a committed line
	m, _ = b.update(m, keyMsg("enter"))
	if _, ok := m.topLayer().(*historyView); !ok {
		t.Fatalf("enter with no selection must open the history view, top = %T", m.topLayer())
	}
}

// esc clears the selection first, then goes back. b always goes back.
func TestBlameEscClearsSelectionBeforeClosing(t *testing.T) {
	t.Parallel()
	m, b := blameSearchModel()
	m = typeBlame(m, b, "space")
	m, _ = b.update(m, keyMsg("esc"))
	if b.lsel.on {
		t.Fatal("the first esc must clear the selection")
	}
	if m.topLayer() != b {
		t.Fatal("the first esc must not close the blame view")
	}
	m, _ = b.update(m, keyMsg("esc"))
	if m.topLayer() == b {
		t.Fatal("the second esc must close the blame view")
	}

	// b never waits: it closes even with a selection on.
	m2, b2 := blameSearchModel()
	m2 = typeBlame(m2, b2, "space")
	m2, _ = b2.update(m2, keyMsg("b"))
	if m2.topLayer() == b2 {
		t.Fatal("b must always go back, selection or not")
	}
}

// A reload replaces the lines: the selection goes with them.
func TestBlameMsgClearsSelection(t *testing.T) {
	t.Parallel()
	m, b := blameSearchModel()
	m = typeBlame(m, b, "space")
	u, _ := m.Update(blameMsg{tag: b.tag, lines: []model.BlameLine{{Hash: "z", LineNo: 1, Content: "x"}}})
	_ = u
	if b.lsel.on {
		t.Fatal("blameMsg must clear the selection")
	}
}

// The stripe paints the CODE and leaves the commit gutter alone.
//
// Line 0 is the one the assertion can see both halves of: it OPENS its commit
// block, so its gutter carries the hash (line 1 shares it and renders a blank
// gutter). The Dark theme is pinned because it SETS selection_bg — with the
// Terminal theme the stripe over the reverse-video cursor row is a hole
// (Reverse(false)), which renders no SGR at all and would make both assertions
// vacuously true.
func TestBlameSelectionPaintsTheCodeNotTheGutter(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(prev)
	prevTheme := activeTheme()
	defer setTheme(prevTheme)
	setTheme(theme.Dark)

	m := Model{width: 100, height: 30}
	b := blameFixture()
	b.sel = 0
	b.lsel.start(0)
	b.lsel.mark(0)

	out := b.render(m, "")
	line := ""
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "package main") {
			line = l
			break
		}
	}
	if line == "" {
		t.Fatalf("the selected line is missing from the render:\n%s", out)
	}
	stripe := sgrBefore(st().selectionStyle(st().selectedRow).Render("x"), "x")
	if got := sgrBefore(line, "package main"); !subsetOf(stripe, got) {
		t.Errorf("the code must wear the stripe, params %v: %q", got, line)
	}
	// The gutter's hash is rendered under the row style, never the stripe's
	// background — assert the two SGR sequences differ.
	if sgrSeqBefore(line, "aaaaaaa") == sgrSeqBefore(line, "package main") {
		t.Errorf("the gutter and the code must not share one style: %q", line)
	}
}

// A selected NON-cursor row under an UNSET selection_bg reverses the body. A
// reversed body would turn per-token syntax FOREGROUNDS into per-token
// BACKGROUNDS — the very hazard winRow.cls documents — so the class mask must
// drop for a reversed body exactly as it does for a reversed style.
func TestBlameSelectionUnderReverseDropsSyntaxColours(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(prev)
	prevTheme := activeTheme()
	defer setTheme(prevTheme)
	setTheme(theme.Terminal) // selection_bg unset => the stripe flips reverse

	m := Model{width: 100, height: 30}
	b := blameFixture()
	b.sel = 0               // the CURSOR is on line 0…
	b.tok = [][]syntax.Tok{ // …and line 1 carries syntax runs
		nil,
		{{Start: 0, End: 4, Class: syntax.Keyword}},
		nil,
	}
	b.lsel.start(1)
	b.lsel.mark(1)

	out := b.render(m, "")
	line := ""
	// The needle sits PAST the token run on purpose: while the mask is still
	// applied the row reads "…[38;5;141mfunc[0m main() {}", so anchoring on
	// "func main()" would miss the row entirely and report it as absent instead
	// of reporting the escape this test is about.
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "main() {}") {
			line = l
			break
		}
	}
	if line == "" {
		t.Fatalf("the selected line is missing:\n%s", out)
	}
	if strings.Contains(line, "38;2;") || strings.Contains(line, "38;5;") {
		t.Errorf("a reversed body must drop the class mask; found a foreground escape: %q", line)
	}
}

// A row with a nil body renders BYTE-IDENTICALLY to the pre-body renderer.
// The oracle is a GOLDEN captured from the current worktree before Step 3
// touches window.go.
func TestRenderWindowNilBodyIsUnchanged(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(prev)

	rows := []winRow{
		{prefix: "abc│", text: "hello world", style: st().selectedRow},
		{prefix: "   │", text: "second line"},
	}
	// Captured from the PRE-CHANGE renderer (before winRow.body existed): one
	// entry per dispMode, each the two rendered rows. The row that carries a
	// style is wrapped in ONE escape pair around prefix+text+padding — exactly
	// the shape the nil-body path must keep.
	want := map[dispMode][]string{
		modeCutoff: {
			"\x1b[7mabc│hello world                         \x1b[0m",
			"   │second line                         ",
		},
		modeWrap: {
			"\x1b[7mabc│hello world                         \x1b[0m",
			"   │second line                         ",
		},
		modeScroll: {
			"\x1b[7mabc│hello world                         \x1b[0m",
			"   │second line                         ",
		},
	}
	for _, mode := range []dispMode{modeCutoff, modeWrap, modeScroll} {
		got := renderWindow(rows, winOpts{w: 40, h: 2, mode: mode, prefixW: 4})
		for i := range got {
			if got[i] != want[mode][i] {
				t.Fatalf("mode %d row %d moved:\n got %q\nwant %q", mode, i, got[i], want[mode][i])
			}
		}
	}
}
