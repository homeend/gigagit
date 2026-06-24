package tui

import (
	"io"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-isatty"

	"github.com/homeend/gigagit/internal/clipboard"
)

// clipboardCopiedMsg reports the outcome of a copy action. ok is the success
// status line; err (when non-nil) becomes a "copy failed: …" status.
type clipboardCopiedMsg struct {
	ok  string
	err error
}

// copyToClipboardCmd writes text to the system clipboard and reports the
// outcome. clipboard.Copy prefers a native OS clipboard command (clip.exe on
// WSL, pbcopy on macOS, wl-copy/xclip/xsel on Linux) and falls back to the
// OSC 52 escape for remote/SSH sessions. The OSC 52 fallback needs a tty, so
// os.Stderr is passed only when it is one — the same tty as the renderer's
// stdout but a separate stream, so the escape never interleaves inside a
// rendered frame; a redirected stderr is omitted instead of dumping escape
// bytes into a file.
func (m Model) copyToClipboardCmd(ok, text string) tea.Cmd {
	return func() tea.Msg {
		var tty io.Writer
		if isatty.IsTerminal(os.Stderr.Fd()) {
			tty = os.Stderr
		}
		if _, err := clipboard.Copy(tty, text); err != nil {
			return clipboardCopiedMsg{err: err}
		}
		return clipboardCopiedMsg{ok: ok}
	}
}
