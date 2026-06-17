package tui

import (
	"errors"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-isatty"

	"github.com/gigagit/gg/internal/clipboard"
)

// clipboardCopiedMsg reports the outcome of a copy action. ok is the success
// status line; err (when non-nil) becomes a "copy failed: …" status.
type clipboardCopiedMsg struct {
	ok  string
	err error
}

var errNoTTY = errors.New("clipboard unavailable (no terminal)")

// copyToClipboardCmd writes text to the clipboard via OSC 52 and reports the
// outcome. It emits to os.Stderr — the same tty as the renderer's stdout but a
// separate stream, so the screen-neutral OSC 52 escape never interleaves inside
// a rendered frame. The isatty guard makes a redirected stderr a no-op instead
// of dumping escape bytes into a file.
func (m Model) copyToClipboardCmd(ok, text string) tea.Cmd {
	return func() tea.Msg {
		if !isatty.IsTerminal(os.Stderr.Fd()) {
			return clipboardCopiedMsg{err: errNoTTY}
		}
		if err := clipboard.Copy(os.Stderr, text); err != nil {
			return clipboardCopiedMsg{err: err}
		}
		return clipboardCopiedMsg{ok: ok}
	}
}
