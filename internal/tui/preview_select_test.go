package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// NOTE: TestPreviewCursorPaint calls lipgloss.SetColorProfile (process-global)
// and therefore does NOT call t.Parallel().

// previewTabModel opens a preview over a file whose first line carries a real
// TAB, so the raw-vs-display distinction is testable.
func previewTabModel(t *testing.T) Model {
	t.Helper()
	var b strings.Builder
	b.WriteString("\tindented\n")
	b.WriteString("\n") // a genuinely EMPTY source line: copyable
	for i := 0; i < 60; i++ {
		fmt.Fprintf(&b, "line%03d\n", i)
	}
	return openPreview(t, fullTreeTreeSideOf(t, previewModelN(b.String())))
}

// alt+↓ moves the CURSOR and scrolls only when the cursor would leave the
// window; plain ↓ scrolls the viewport and leaves the cursor alone.
func TestPreviewAltDownMovesCursorNotViewport(t *testing.T) {
	t.Parallel()
	m := previewTabModel(t)
	p := m.filesPreview
	if p.cur != 0 || p.sel != 0 {
		t.Fatalf("a fresh preview starts at cursor 0 / top 0, got cur=%d sel=%d", p.cur, p.sel)
	}
	m = feedPreview(m, "alt+down")
	if p.cur != 1 {
		t.Fatalf("alt+down must move the cursor, cur = %d want 1", p.cur)
	}
	if p.sel != 0 {
		t.Fatalf("a cursor still inside the window must not scroll, sel = %d want 0", p.sel)
	}
	// Walk the cursor past the bottom edge: now the pager must follow, minimally.
	rows := m.filePreviewRowsCap()
	for i := 1; i < rows+1; i++ {
		m = feedPreview(m, "alt+down")
	}
	if p.cur != rows+1 {
		t.Fatalf("cursor = %d, want %d", p.cur, rows+1)
	}
	if p.sel != p.cur-rows+1 {
		t.Fatalf("the pager must follow minimally: sel = %d, want %d", p.sel, p.cur-rows+1)
	}
	m = feedPreview(m, "alt+up")
	if p.cur != rows {
		t.Fatalf("alt+up must step the cursor back, cur = %d", p.cur)
	}
}

func TestPreviewDownScrollsNotCursor(t *testing.T) {
	t.Parallel()
	m := previewTabModel(t)
	p := m.filesPreview
	m = feedPreview(m, "down", "down")
	if p.sel != 2 {
		t.Fatalf("↓ must scroll the pager, sel = %d want 2", p.sel)
	}
	if p.cur != 0 {
		t.Fatalf("↓ must not move the cursor, cur = %d want 0", p.cur)
	}
}

// Copy takes the SOURCE line: the tab survives, the display expansion does not.
func TestPreviewSelectionCopiesRawText(t *testing.T) {
	t.Parallel()
	m := previewTabModel(t)
	p := m.filesPreview
	if p.lines[0].raw != "\tindented" {
		t.Fatalf("raw = %q, want the source line with its tab", p.lines[0].raw)
	}
	if !strings.Contains(p.lines[0].text, "    ") {
		t.Fatalf("the DISPLAY text should have the tab expanded: %q", p.lines[0].text)
	}
	if !p.lines[0].src || !p.lines[1].src || p.lines[1].raw != "" {
		t.Fatalf("an empty SOURCE line must be src with an empty raw: %#v", p.lines[1])
	}

	m = feedPreview(m, "space", "alt+down") // loose range 0..1
	row, ok := rowByID(m.contextCopyRows(), "copy-selected-lines")
	if !ok {
		t.Fatal("no Copy selected lines row while a preview selection is on")
	}
	if row.copyText != "\tindented\n" {
		t.Fatalf("copyText = %q, want the tab line and the empty line", row.copyText)
	}
	if row.label != "Copy selected lines (2)" {
		t.Fatalf("label = %q", row.label)
	}

	u, cmd := m.Update(keyMsg("enter"))
	m = u.(Model)
	if cmd == nil {
		t.Fatal("enter with a selection on must issue the clipboard command")
	}
	if m.filesPreview.lsel.on {
		t.Fatal("enter must clear the selection")
	}
	if m.filesTreeFocused {
		t.Fatal("copying must not move focus to the tree")
	}
}

// Copy line on the cursor line, with the preview's own 1-based number.
func TestPreviewCopyLineRow(t *testing.T) {
	t.Parallel()
	m := previewTabModel(t)
	m = feedPreview(m, "alt+down", "alt+down") // cursor on line index 2
	row, ok := rowByID(m.contextCopyRows(), "copy-line")
	if !ok {
		t.Fatal("the focused preview must offer Copy line")
	}
	if row.copyText != "line000" {
		t.Fatalf("copyText = %q, want the third source line", row.copyText)
	}
	// The tree's own path/name/commit rows stay reachable underneath.
	if _, ok := rowByID(m.contextCopyRows(), "copy-file-path"); !ok {
		t.Fatal("the file copy rows must stay reachable while the preview is focused")
	}
}

// A placeholder line is not a line of the file: no Copy line, and space is inert.
func TestPreviewCopyLineAbsentOnPlaceholder(t *testing.T) {
	t.Parallel()
	m := previewTabModel(t)
	p := m.filesPreview
	p.lines = []contentLine{{text: "(loading…)"}}
	p.cur, p.sel = 0, 0
	if _, ok := rowByID(m.contextCopyRows(), "copy-line"); ok {
		t.Fatal("a placeholder line must not offer Copy line")
	}
	m = feedPreview(m, "space")
	if m.filesPreview.lsel.on {
		t.Fatal("space on a placeholder must not start a selection")
	}
}

// enter with NO selection keeps its old meaning: focus moves to the tree.
func TestPreviewEnterWithoutSelectionFocusesTree(t *testing.T) {
	t.Parallel()
	m := previewTabModel(t)
	m = feedPreview(m, "enter")
	if !m.filesTreeFocused {
		t.Fatal("enter with no selection must focus the tree, as it did before")
	}
}

// esc clears the selection first, then closes the preview.
func TestPreviewEscClearsSelectionBeforeClosing(t *testing.T) {
	t.Parallel()
	m := previewTabModel(t)
	m = feedPreview(m, "space", "esc")
	if m.filesPreview == nil {
		t.Fatal("the first esc must clear the selection, not close the preview")
	}
	if m.filesPreview.lsel.on {
		t.Fatal("the first esc must clear the selection")
	}
	m = feedPreview(m, "esc")
	if m.filesPreview != nil {
		t.Fatal("the second esc must close the preview")
	}
}

// A search hit LANDS the cursor, and ] / [ then measure from it.
func TestPreviewSearchHitLandsCursor(t *testing.T) {
	t.Parallel()
	m := previewSearchModel(t) // 60 "line%03d alpha" rows + a "tail beta" row
	m = feedPreview(m, "/", "b", "e", "t", "a")
	p := m.filesPreview
	if p.search.cur < 0 {
		t.Fatal("the incremental search must have found the tail")
	}
	hit := p.search.hits[p.search.cur]
	if p.cur != hit.row {
		t.Fatalf("the cursor must land on the hit row: cur = %d, hit = %d", p.cur, hit.row)
	}
	if got := p.searchPos().row; got != hit.row {
		t.Fatalf("searchPos must report the current hit's own row, got %d want %d", got, hit.row)
	}
	// The FALLBACK branch is the one that retires the old behaviour: with no
	// current hit, searchPos must read the CURSOR, not the pager's top line.
	// Put the two deliberately out of step and check which one it follows.
	savedCur := p.search.cur
	p.search.cur = -1
	p.cur, p.sel = 7, 0
	if got := p.searchPos().row; got != 7 {
		t.Fatalf("with no current hit searchPos must follow the cursor, row = %d want 7 (top line is 0)", got)
	}
	p.search.cur, p.cur = savedCur, hit.row

	// esc cancels the live search and restores the cursor as well as the pager.
	m = feedPreview(m, "esc")
	if m.filesPreview.cur != 0 {
		t.Fatalf("esc must restore the cursor to where the search started, cur = %d", m.filesPreview.cur)
	}
}

// A load arrival replaces the lines: the cursor resets and the selection goes.
func TestPreviewLoadArrivalResetsCursorAndSelection(t *testing.T) {
	t.Parallel()
	m := previewTabModel(t)
	m = feedPreview(m, "alt+down", "alt+down", "space")
	u, _ := m.Update(fileContentMsg{tag: m.filesPreviewTag, lines: fileContentLines([]byte("a\nb\n"))})
	m = u.(Model)
	if m.filesPreview.cur != 0 || m.filesPreview.sel != 0 {
		t.Fatalf("a load must reset the cursor and the top line, cur=%d sel=%d", m.filesPreview.cur, m.filesPreview.sel)
	}
	if m.filesPreview.lsel.on {
		t.Fatal("a load must clear the selection")
	}
}

// The cursor row wears the diff's cursor band; a selected row wears the stripe,
// and the stripe wins on the cursor row itself (the stripe IS the row).
func TestPreviewCursorPaint(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(prev)

	m := previewTabModel(t)
	m = feedPreview(m, "alt+down", "alt+down") // cursor on "line000"
	boxW, boxH := m.layout().rightW, m.layout().boxH[panelCommits]
	out := m.renderFilePreview(boxW, boxH)

	band := sgrBefore(st().diffCursorRow.Render("x"), "x")
	row := previewRowWith(t, out, "line000")
	if got := sgrBefore(row, "line000"); !subsetOf(band, got) {
		t.Errorf("the cursor row must wear the band, params %v: %q", got, row)
	}
	other := previewRowWith(t, out, "line001")
	if got := sgrBefore(other, "line001"); subsetOf(band, got) {
		t.Errorf("a non-cursor row must not wear the band: %q", other)
	}

	// Now select the cursor row: the stripe replaces the band.
	m = feedPreview(m, "space")
	out = m.renderFilePreview(boxW, boxH)
	stripe := sgrBefore(st().selectionStyle(st().diffCursorRow).Render("x"), "x")
	row = previewRowWith(t, out, "line000")
	if got := sgrBefore(row, "line000"); !subsetOf(stripe, got) {
		t.Errorf("a selected cursor row must wear the stripe, params %v: %q", got, row)
	}

	// cursor_style off: no band at all, but the stripe still paints.
	m.diffCursor = "off"
	m = feedPreview(m, "esc")
	out = m.renderFilePreview(boxW, boxH)
	row = previewRowWith(t, out, "line000")
	if got := sgrBefore(row, "line000"); subsetOf(band, got) {
		t.Errorf("with diff_cursor off the preview must paint no band: %q", row)
	}
}

// previewRowWith returns the rendered preview line containing needle.
func previewRowWith(t *testing.T, out, needle string) string {
	t.Helper()
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, needle) {
			return l
		}
	}
	t.Fatalf("no rendered row contains %q:\n%s", needle, out)
	return ""
}

// The hint line advertises the new keys and swaps to the selection variant.
func TestPreviewHintVariants(t *testing.T) {
	t.Parallel()
	m := previewTabModel(t)
	// Render WIDE: renderFilePreview truncates the hint to innerW, and both
	// variants are longer than the right column at the fixture's 100 columns.
	m.width = 220
	boxW, boxH := m.layout().rightW, m.layout().boxH[panelCommits]
	out := m.renderFilePreview(boxW, boxH)
	if !strings.Contains(out, "[alt+↑↓] line") || !strings.Contains(out, "[spc] mark") {
		t.Fatalf("the preview hint must advertise the cursor and mark keys:\n%s", out)
	}
	if f, _ := m.footerOverride(); !strings.Contains(f, "[spc] mark") {
		t.Fatalf("the status bar must advertise the mark key: %q", f)
	}

	m = feedPreview(m, "space")
	out = m.renderFilePreview(boxW, boxH)
	for _, want := range []string{"[space] mark end", "[enter] copy", "[esc] unmark"} {
		if !strings.Contains(out, want) {
			t.Errorf("the selection hint lacks %q:\n%s", want, out)
		}
	}
	if f, _ := m.footerOverride(); !strings.Contains(f, "[enter] copy") {
		t.Fatalf("the status bar must switch to the selection variant: %q", f)
	}
}
