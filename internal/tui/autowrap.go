package tui

import (
	"io"
	"os/exec"

	tea "github.com/charmbracelet/bubbletea"
)

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

// handover suspends the TUI and runs cmd on the real terminal — the one
// path every editor, subshell, external viewer and terminal-mode tool run
// takes (a test forbids a raw tea.ExecProcess in this package). It is
// tea.ExecProcess with one addition: the child gets a terminal that WRAPS.
//
// Why: autowrapOff switches DECAWM off for the whole of p.Run(), and Bubble
// Tea's ReleaseTerminal restores raw mode, the alt screen, mouse and
// bracketed paste but never touches DECAWM — so a child inherits a
// non-wrapping terminal. A shell then clips long lines at the right edge,
// and a full-screen renderer that advances rows by letting the terminal
// wrap (Junie's Compose TUI, 2026-09-18) paints its whole grid onto row 1.
// The wrap-on lands after the alt screen is left and before the child
// starts; wrap-off is written back once the child exits — before Bubble Tea
// re-enters the alt screen — so the wide-glyph clip guard holds for the
// rest of the session.
func handover(cmd *exec.Cmd, fn tea.ExecCallback) tea.Cmd {
	return tea.Exec(&handoverCmd{Cmd: cmd}, fn)
}

// handoverCmd is Bubble Tea's own exec.Cmd adapter (stdio set only when the
// caller left it nil) plus the wrap bracket on the terminal writer.
type handoverCmd struct {
	*exec.Cmd
	term io.Writer // the terminal Bubble Tea handed us, wrap bytes go here
}

func (c *handoverCmd) SetStdin(r io.Reader) {
	if c.Stdin == nil {
		c.Stdin = r
	}
}

func (c *handoverCmd) SetStdout(w io.Writer) {
	c.term = w
	if c.Stdout == nil {
		c.Stdout = w
	}
}

func (c *handoverCmd) SetStderr(w io.Writer) {
	if c.Stderr == nil {
		c.Stderr = w
	}
}

// Run brackets the child with wrap-on / wrap-off. The wrap-off is deferred
// so a child that fails to start, exits non-zero, or panics the callback
// path still leaves the TUI's non-wrapping terminal in place.
func (c *handoverCmd) Run() error {
	if c.term != nil {
		_, _ = io.WriteString(c.term, "\x1b[?7h")
		defer func() { _, _ = io.WriteString(c.term, "\x1b[?7l") }()
	}
	return c.Cmd.Run()
}
