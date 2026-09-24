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
	p := m.filesPreview.p
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
	p := m.filesPreview.p
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
	p := m.filesPreview.p
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
	if m.filesPreview.p.lsel.on {
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
	p := m.filesPreview.p
	p.lines = []contentLine{{text: "(loading…)"}}
	p.cur, p.sel = 0, 0
	if _, ok := rowByID(m.contextCopyRows(), "copy-line"); ok {
		t.Fatal("a placeholder line must not offer Copy line")
	}
	m = feedPreview(m, "space")
	if m.filesPreview.p.lsel.on {
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
	if m.filesPreview.p.lsel.on {
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
	p := m.filesPreview.p
	if p.search.cur < 0 {
		t.Fatal("the incremental search must have found the tail")
	}
	hit := p.search.hits[p.search.cur]
	if p.cur != hit.row {
		t.Fatalf("the cursor must land on the hit row: cur = %d, hit = %d", p.cur, hit.row)
	}
	rows := m.filePreviewRowsCap()
	if got := p.searchPos(rows).row; got != hit.row {
		t.Fatalf("searchPos must report the current hit's own row, got %d want %d", got, hit.row)
	}
	// The FALLBACK branch is the one that retires the old behaviour: with no
	// current hit, searchPos must read the CURSOR — as long as the cursor is on
	// screen. Put it and the pager top deliberately out of step.
	savedCur := p.search.cur
	p.search.cur = -1
	p.cur, p.sel = 7, 0
	if got := p.searchPos(rows).row; got != 7 {
		t.Fatalf("with no current hit searchPos must follow the cursor, row = %d want 7 (top line is 0)", got)
	}
	p.search.cur, p.cur = savedCur, hit.row

	// esc cancels the live search and restores the cursor as well as the pager.
	m = feedPreview(m, "esc")
	if m.filesPreview.p.cur != 0 {
		t.Fatalf("esc must restore the cursor to where the search started, cur = %d", m.filesPreview.p.cur)
	}
}

// ↑/↓ scroll the pager WITHOUT moving the cursor, so a search opened after a
// free scroll must start from what is on SCREEN — not from the cursor, which is
// still parked at row 0 far above the window.
func TestPreviewSearchAfterFreeScrollStartsFromTheWindow(t *testing.T) {
	t.Parallel()
	m := previewSearchModel(t) // 60 "line%03d alpha" rows + a "tail beta" row
	const scrolled = 30
	for i := 0; i < scrolled; i++ {
		m = feedPreview(m, "down")
	}
	p := m.filesPreview.p
	if p.sel != scrolled || p.cur != 0 {
		t.Fatalf("↓ must scroll the pager alone: sel = %d (want %d), cur = %d (want 0)", p.sel, scrolled, p.cur)
	}
	m = feedPreview(m, "/", "a", "l", "p", "h", "a")
	if p.search.cur < 0 {
		t.Fatalf("the search must have found a hit")
	}
	if row := p.search.hits[p.search.cur].row; row < scrolled {
		t.Fatalf("the search must start from the visible top, not the off-screen cursor: hit row = %d, want >= %d", row, scrolled)
	}
	// …and the view must not have been yanked back to the top of the file.
	if p.sel < scrolled {
		t.Fatalf("a hit already on screen must not scroll the pager: sel = %d, want >= %d", p.sel, scrolled)
	}
}

// After a COMMITTED search, moving the line cursor re-anchors ]: it steps from
// the cursor, not from the stale current hit (the diff and blame rule).
func TestPreviewStepHitFollowsTheCursor(t *testing.T) {
	t.Parallel()
	m := previewSearchModel(t)
	m = feedPreview(m, "/", "a", "l", "p", "h", "a", "enter")
	p := m.filesPreview.p
	if p.search.hits[p.search.cur].row != 0 {
		t.Fatalf("the committed search must sit on the first hit, row = %d", p.search.hits[p.search.cur].row)
	}
	const walked = 5
	for i := 0; i < walked; i++ {
		m = feedPreview(m, "alt+down")
	}
	if p.cur != walked {
		t.Fatalf("alt+down must walk the cursor, cur = %d want %d", p.cur, walked)
	}
	m = feedPreview(m, "]")
	if row := p.search.hits[p.search.cur].row; row != walked {
		// The old unconditional hit branch stepped from hits[0] and landed on 1.
		t.Fatalf("] must step from the CURSOR's row, landed on %d want %d", row, walked)
	}
}

// A load arrival replaces the lines: the cursor resets and the selection goes.
func TestPreviewLoadArrivalResetsCursorAndSelection(t *testing.T) {
	t.Parallel()
	m := previewTabModel(t)
	m = feedPreview(m, "alt+down", "alt+down", "space")
	u, _ := m.Update(fileContentMsg{tag: m.filesPreview.tag, lines: fileContentLines([]byte("a\nb\n"))})
	m = u.(Model)
	if m.filesPreview.p.cur != 0 || m.filesPreview.p.sel != 0 {
		t.Fatalf("a load must reset the cursor and the top line, cur=%d sel=%d", m.filesPreview.p.cur, m.filesPreview.p.sel)
	}
	if m.filesPreview.p.lsel.on {
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

	// cursor_style off: no band at all, but the stripe still paints. The
	// selection is deliberately left ON (an esc here would clear it and the
	// second assertion would be checking an empty row), so the stripe lands
	// over a BARE base instead of over the band.
	m.diffCursor = "off"
	out = m.renderFilePreview(boxW, boxH)
	bare := sgrBefore(st().selectionStyle(lipgloss.NewStyle()).Render("x"), "x")
	row = previewRowWith(t, out, "line000")
	if got := sgrBefore(row, "line000"); subsetOf(band, got) {
		t.Errorf("with diff_cursor off the preview must paint no band: %q", row)
	}
	if got := sgrBefore(row, "line000"); !subsetOf(bare, got) {
		t.Errorf("with diff_cursor off the stripe must still paint, params %v: %q", got, row)
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
	// Rendered at the fixture's own 100 columns: renderFilePreview truncates
	// the hint to innerW (63 there), so the ORDER is the whole point — the new
	// keys and both exits have to survive the cut on an ordinary terminal.
	boxW, boxH := m.layout().rightW, m.layout().boxH[panelCommits]
	out := m.renderFilePreview(boxW, boxH)
	for _, want := range []string{"[alt+↑↓] line", "[spc] mark", "[/] find", "[esc] close"} {
		if !strings.Contains(out, want) {
			t.Fatalf("the preview hint must still carry %q at 100 columns:\n%s", want, out)
		}
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

// A cursor scrolled OUT of the window re-enters it at the nearest edge on the
// next alt+↑/↓ instead of dragging the viewport back to wherever it was: the
// user scrolled away on purpose. The pager top (sel) must not move on a snap.
func TestPreviewAltDownSnapsToTheTopWhenTheCursorIsAbove(t *testing.T) {
	t.Parallel()
	m := previewTabModel(t)
	p := m.filesPreview.p
	m = feedPreview(m, "down", "down", "down", "down", "down") // cursor 0 is now above the window
	if p.sel != 5 || p.cur != 0 {
		t.Fatalf("setup: sel=%d cur=%d, want 5/0", p.sel, p.cur)
	}
	m = feedPreview(m, "alt+down")
	if p.cur != 5 {
		t.Fatalf("alt+down with the cursor above the window must land on the TOP visible row, cur = %d want 5", p.cur)
	}
	if p.sel != 5 {
		t.Fatalf("a snap must not scroll, sel = %d want 5", p.sel)
	}
	m = feedPreview(m, "alt+up") // above again? no: the cursor is visible now, so it steps and scrolls minimally
	if p.cur != 4 || p.sel != 4 {
		t.Fatalf("a visible cursor steps normally: cur=%d sel=%d, want 4/4", p.cur, p.sel)
	}
}

func TestPreviewAltUpSnapsToTheBottomWhenTheCursorIsBelow(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(prev)

	m := previewTabModel(t)
	p := m.filesPreview.p
	rows := m.filePreviewRowsCap()
	for i := 0; i < rows+5; i++ {
		m = feedPreview(m, "alt+down")
	}
	for i := 0; i < 8; i++ {
		m = feedPreview(m, "up")
	}
	if p.sel != 0 || p.cur != rows+5 {
		t.Fatalf("setup: sel=%d cur=%d, want 0/%d", p.sel, p.cur, rows+5)
	}
	m = feedPreview(m, "alt+up")
	if p.cur != rows-1 {
		t.Fatalf("alt+up with the cursor below the window must land on the BOTTOM visible row, cur = %d want %d", p.cur, rows-1)
	}
	if p.sel != 0 {
		t.Fatalf("a snap must not scroll, sel = %d want 0", p.sel)
	}
	// The band sits on the last body row of the rendered box, which pins that
	// filePreviewRowsCap agrees with renderFilePreview's own row budget.
	boxW, boxH := m.layout().rightW, m.layout().boxH[panelCommits]
	out := m.renderFilePreview(boxW, boxH)
	band := sgrBefore(st().diffCursorRow.Render("x"), "x")
	needle := p.lines[rows-1].text
	row := previewRowWith(t, out, needle)
	if got := sgrBefore(row, needle); !subsetOf(band, got) {
		t.Errorf("the bottom visible row must wear the band, params %v: %q", got, row)
	}
	lines := strings.Split(out, "\n")
	// title, rows..., hint, then the bottom border: the band row is the last body row.
	if want := lines[len(lines)-3]; want != row {
		t.Errorf("the snapped cursor is not on the last body row:\nlast body: %q\ncursor:    %q", want, row)
	}
	m = feedPreview(m, "alt+down") // the cursor is visible: it steps and the pager follows by one
	if p.cur != rows || p.sel != 1 {
		t.Fatalf("a visible cursor at the bottom steps and scrolls by one: cur=%d sel=%d, want %d/1", p.cur, p.sel, rows)
	}
}
