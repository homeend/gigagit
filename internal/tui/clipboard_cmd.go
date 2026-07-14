package tui

import (
	"io"
	"os"
	"path"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-isatty"

	"github.com/homeend/gigagit/internal/clipboard"
	"github.com/homeend/gigagit/internal/engine"
)

// absFilePath joins a repo-relative path onto base, defaulting to the current
// worktree when base is empty. It is the single source of truth for the
// "Copy absolute file path" actions so every surface agrees byte-for-byte.
func (m Model) absFilePath(base, rel string) string {
	if base == "" {
		base = m.currentWorktree
	}
	return filepath.Join(base, rel)
}

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

// copyFileChoice maps a copy-chooser option to its status line and clipboard
// text. ok is false for Cancel or an unknown option. The strings match the
// Files-panel copy rows (fileCopyPathName) so both surfaces speak alike.
func copyFileChoice(option, p string) (okMsg, text string, ok bool) {
	switch option {
	case "Copy file path":
		return "Copied path: " + p, p, true
	case "Copy file name":
		return "Copied file name: " + path.Base(p), path.Base(p), true
	}
	return "", "", false
}

// copyFilePrompt opens the path/name copy chooser for a repo-relative file
// path. The modal renders above the calling popup (which stays on the layer
// stack); Cancel — kept last so esc maps to it — reveals it unchanged.
func (m Model) copyFilePrompt(p string) (Model, tea.Cmd) {
	m.modal = &decisionState{
		req: engine.DecisionRequest{
			ID:      "copy-file",
			Prompt:  "Copy — " + p,
			Options: []string{"Copy file path", "Copy file name", "Cancel"},
		},
		onResolve: func(m Model, opt string) (tea.Model, tea.Cmd) {
			if okMsg, text, ok := copyFileChoice(opt, p); ok {
				return m, m.copyToClipboardCmd(okMsg, text)
			}
			return m, nil
		},
	}
	return m, nil
}
