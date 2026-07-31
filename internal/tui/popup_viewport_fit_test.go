package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/exttool"
)

// longCommitBody builds a description far taller than any terminal: 60 short
// logical lines (each fits contentWidth unwrapped, so display lines == logical
// lines) ending in a unique marker at the cursor position.
func longCommitBody() string {
	var b strings.Builder
	for i := 0; i < 60; i++ {
		fmt.Fprintf(&b, "line %02d filler text\n", i)
	}
	b.WriteString("ENDMARK")
	return b.String()
}

// TestCommitPopupLongDescriptionFitsTerminal pins the viewport-fit invariant
// for the commit popup: a long (AI-generated / squashed) description must
// never grow the box past the terminal height — overlayCenter silently drops
// rows outside termH, which cut off the fields and the footer hints with no
// way to see them. The description gets an internal scrolling window instead;
// with the cursor at the end of the buffer, the window must show the end.
func TestCommitPopupLongDescriptionFitsTerminal(t *testing.T) {
	m := New(nil) // zero width/height → overlayDims falls back to 80×24
	p := &commitPopup{field: 1}
	p.title = newTextField("subject")
	p.desc = newTextField(longCommitBody()) // cursor at end

	out := p.box(m)
	_, termH := m.overlayDims()
	if lines := strings.Split(out, "\n"); len(lines) > termH {
		t.Fatalf("box rendered %d lines, want <= terminal height %d (overlayCenter would clip the footer):\n%s", len(lines), termH, out)
	}
	if !strings.Contains(out, "[esc] cancel") {
		t.Fatalf("footer hints must stay visible under a long description:\n%s", out)
	}
	if !strings.Contains(out, "ENDMARK") {
		t.Fatalf("description window must follow the cursor (at buffer end):\n%s", out)
	}
}

// TestCommitPopupDescScrollFollowsCursor moves the cursor back to the start of
// the same over-long description: the internal window must scroll up so the
// cursor's line is visible again (and the box still fits the terminal).
func TestCommitPopupDescScrollFollowsCursor(t *testing.T) {
	m := New(nil)
	p := &commitPopup{field: 1}
	p.title = newTextField("subject")
	p.desc = newTextField(longCommitBody())

	_ = p.box(m) // first render: window at the end (cursor there)
	p.desc.cursor = 0
	out := p.box(m)

	if !strings.Contains(out, "line 00") {
		t.Fatalf("description window must scroll back up to the cursor at line 0:\n%s", out)
	}
	if strings.Contains(out, "ENDMARK") {
		t.Fatalf("a 60-line body cannot show both ends at once on a 24-row terminal — the window did not move:\n%s", out)
	}
	_, termH := m.overlayDims()
	if lines := strings.Split(out, "\n"); len(lines) > termH {
		t.Fatalf("box rendered %d lines, want <= %d:\n%s", len(lines), termH, out)
	}
}

// TestToolsWizardHeightStableAcrossSelection pins the External-tools wizard's
// fixed-height contract on a big terminal: the command preview area is sized
// once from the TALLEST command across all rows, so moving the selection
// through tools whose commands wrap to different line counts must not change
// the box height (the live report: the popup visibly grows/shrinks per row
// while scrolling the list).
func TestToolsWizardHeightStableAcrossSelection(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/xdg")

	rows := wizardRows(nil)
	if len(rows) < 2 {
		t.Fatal("catalog too small to exercise selection changes")
	}
	m := toolCfg()
	m.width, m.height = 200, 60
	p := &settingsPopup{toolsView: true, toolRows: rows, toolChecked: defaultToolChecked(rows), sel: 0}

	base := len(strings.Split(p.box(m), "\n"))
	for sel := 1; sel < len(rows); sel++ {
		p.sel = sel
		if h := len(strings.Split(p.box(m), "\n")); h != base {
			t.Fatalf("box height changed with selection: row 0 → %d lines, row %d (%s: %s) → %d lines",
				base, sel, rows[sel].tmpl.Category, rows[sel].tmpl.Name, h)
		}
	}
}

// TestToolsWizardSmallTerminalShowsPreviewAndFooter pins the small-screen
// budget order: with more catalog rows than the terminal can list, the
// command preview and the footer must still be INSIDE the box's termH budget
// (rows past termH are silently dropped by overlayCenter) — the LIST is what
// shrinks and scrolls, never the preview.
func TestToolsWizardSmallTerminalShowsPreviewAndFooter(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/xdg")

	rows := wizardRows(nil)
	m := toolCfg() // 80×24 default
	p := &settingsPopup{toolsView: true, toolRows: rows, toolChecked: defaultToolChecked(rows), sel: 0}

	out := p.box(m)
	_, termH := m.overlayDims()
	if lines := strings.Split(out, "\n"); len(lines) > termH {
		t.Fatalf("box rendered %d lines, want <= terminal height %d — everything past that is invisible:\n%s", len(lines), termH, out)
	}
	if !strings.Contains(out, "[esc] back") {
		t.Fatalf("footer hint must stay visible with a long tool list:\n%s", out)
	}
	if !strings.Contains(out, "writes to:") {
		t.Fatalf("destination line must stay visible with a long tool list:\n%s", out)
	}
	cmd := exttool.GenerateCommand(rows[0].tmpl, rows[0].det.Bin)
	firstToken := strings.Fields(cmd)[0]
	if !strings.Contains(out, firstToken) {
		t.Fatalf("the selected row's command preview (starting %q) must stay visible with a long tool list:\n%s", firstToken, out)
	}
}
