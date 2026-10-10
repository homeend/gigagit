package tui

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/homeend/gigagit/internal/theme"
)

// An unfocused pane draws no cursor row: the box rendered with cursorOff is
// byte-identical to one whose cursor is off-screen, and differs from the
// focused render (which paints the cursor band). Sets the colour profile and
// theme (process-global), so it does NOT call t.Parallel().
func TestPreviewBoxCursorOffHidesTheCursor(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(prev)
	prevTheme := activeTheme()
	defer setTheme(prevTheme)
	setTheme(theme.Dark)
	m := newTestModel(t)
	m.width, m.height = 120, 30
	p := &contentPopup{lines: []contentLine{{text: "one", src: true}, {text: "two", src: true}, {text: "three", src: true}}}
	p.cur = 1
	with := m.renderPreviewBoxOpts(p, "t", 40, 10, previewBoxOpts{focused: true})
	off := m.renderPreviewBoxOpts(p, "t", 40, 10, previewBoxOpts{focused: true, cursorOff: true})
	if with == off {
		t.Fatal("cursorOff must change the render (the cursor band is gone)")
	}
	p.cur = -1
	none := m.renderPreviewBoxOpts(p, "t", 40, 10, previewBoxOpts{focused: true})
	if off != none {
		t.Fatalf("cursorOff must render exactly as a box with no cursor row:\n%s\n---\n%s", off, none)
	}
}
