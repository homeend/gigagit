package tui

import "io"

// autowrapOff runs body with the terminal's automatic right-margin wrap
// (DECAWM, `CSI ? 7 l`) switched off and switches it back on afterwards —
// also when body panics, so a shell never inherits a non-wrapping terminal.
//
// Why: terminals and gg do not always agree on a glyph's width. U+2630 ☰ is
// one column to go-runewidth (so every row that carries it is padded to the
// full pane width) but two columns to tmux and every utf8proc-based
// terminal; that row then overflows by one cell, the terminal wraps it onto
// a fresh line, every row below shifts down, and Bubble Tea's line-diff
// renderer — which believes the screen still holds the frame it wrote —
// repaints only the rows it thinks changed. The screen stays scrambled
// until something forces a full repaint. With wrap off the terminal clips
// the overflowing cell instead, which costs a border glyph on that one row
// and nothing else. Bubble Tea itself never wraps: it moves the cursor and
// ends every line with CR LF.
func autowrapOff(w io.Writer, body func() error) error {
	_, _ = io.WriteString(w, "\x1b[?7l")
	defer func() { _, _ = io.WriteString(w, "\x1b[?7h") }()
	return body()
}
