package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// paintFrame lays bg/fg under every cell of frame, a w×h screen: each line is
// prefixed with the colour SGR, the SGR is re-asserted after every reset
// lipgloss emits (\x1b[0m), the line is padded with spaces to w display cells
// (ANSI-aware, wide glyphs count 2), and the frame is padded to h lines. Lines
// already ≥ w are never truncated. Empty bg and fg → frame unchanged, so the
// `terminal` theme is byte-identical to the pre-theme renderer.
//
// The SGR comes from a lipgloss style so the colour-profile downgrade
// (truecolor → 256 → 16) applies exactly as it does to every other style.
func paintFrame(frame string, w, h int, bg, fg lipgloss.Color) string {
	if bg == "" && fg == "" {
		return frame
	}
	style := lipgloss.NewStyle()
	if bg != "" {
		style = style.Background(bg)
	}
	if fg != "" {
		style = style.Foreground(fg)
	}
	// Render a single space to harvest the SGR prefix lipgloss emits for
	// this bg/fg; everything up to the space is the escape we re-assert.
	probe := style.Render(" ")
	sgr := probe[:strings.IndexByte(probe, ' ')]
	const reset = "\x1b[0m"

	lines := strings.Split(frame, "\n")
	var b strings.Builder
	b.Grow(len(frame) + (len(lines)+h)*(len(sgr)+len(reset)+8))
	for i, line := range lines {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(sgr)
		b.WriteString(strings.ReplaceAll(line, reset, reset+sgr))
		if pad := w - ansi.StringWidth(line); pad > 0 {
			b.WriteString(strings.Repeat(" ", pad))
		}
		b.WriteString(reset)
	}
	blank := sgr + strings.Repeat(" ", max(w, 0)) + reset
	for i := len(lines); i < h; i++ {
		b.WriteByte('\n')
		b.WriteString(blank)
	}
	return b.String()
}
