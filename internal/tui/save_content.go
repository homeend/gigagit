package tui

import (
	"os"
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// savedNoteStyle sets the "saved to …" confirmation apart from the window's
// own content: it is a note ABOUT the window, not another row of it. Dark
// green reads as done without competing with the red danger frame the [E]
// viewer can carry. Render it with .Width(n) so the band spans the box rather
// than stopping at the end of the path.
var savedNoteStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color("15")).
	Background(lipgloss.Color("22"))

// Popups show text you often need somewhere else: a notice's `sudo …` fix
// block, git's multi-line stderr in the [E] viewer. Selecting it by hand is
// not an option — a terminal multiplexer selects whole terminal-width LINES,
// so a centred box comes with the panels either side of it, and the box's own
// wrapping breaks a long command across rows.
//
// `s` writes the text to a temp file instead and reports the path. Deliberately
// a file rather than the clipboard: gg's clipboard path can silently no-op
// (a WSL clip.exe that cannot exec, an OSC 52 escape the terminal drops) while
// still reporting success, and the whole reason someone is reading a fix
// instruction out of a popup may be that copying is what broke.

// contentSavedMsg reports the outcome of writing a popup's text to disk.
type contentSavedMsg struct {
	path string
	err  error
}

// savedNotifier is implemented by popups that can display where their text was
// written. The status bar alone is not enough: a tall box covers it, so a user
// still reading the popup sees the confirmation cut off by the box's own
// border — hiding the one thing the save was for. Popups live in the layer
// stack as pointers, so the note survives the Model value copy.
type savedNotifier interface {
	noteSaved(path string)
}

// notifySaved tells the top layer where its text landed, when it can show it.
func (m Model) notifySaved(path string) {
	if n, ok := m.topLayer().(savedNotifier); ok {
		n.noteSaved(path)
	}
}

// saveTextCmd writes lines to a temp file named after slug and reports where
// it landed. The RAW lines are written, never the wrapped/truncated render, so
// a command the box had to split across two rows comes back as one pasteable
// line.
func saveTextCmd(slug string, lines []string) tea.Cmd {
	return func() tea.Msg {
		f, err := os.CreateTemp("", "gg-"+fileSlug(slug)+"-*.txt")
		if err != nil {
			return contentSavedMsg{err: err}
		}
		body := strings.Join(lines, "\n")
		if !strings.HasSuffix(body, "\n") {
			body += "\n"
		}
		if _, err := f.WriteString(body); err != nil {
			f.Close()
			os.Remove(f.Name())
			return contentSavedMsg{err: err}
		}
		if err := f.Close(); err != nil {
			os.Remove(f.Name())
			return contentSavedMsg{err: err}
		}
		return contentSavedMsg{path: f.Name()}
	}
}

// fileSlug reduces a popup title to something safe in a filename. Titles are
// translated, so a non-Latin one can reduce to nothing — hence the fallback.
func fileSlug(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(unicode.ToLower(r))
		case b.Len() > 0 && !strings.HasSuffix(b.String(), "-"):
			b.WriteRune('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "text"
	}
	if len(out) > 40 {
		out = strings.Trim(out[:40], "-")
	}
	return out
}
