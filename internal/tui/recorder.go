package tui

import (
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// recorder appends a TUI session's keystrokes to a file in the tui-capture.sh
// keyscript format (one token per line), so the session can be replayed
// headlessly. It lags by one key so the terminating quit (q / ctrl+c) is
// never written. Best-effort: a write error disables it rather than
// disturbing the live session. A nil *recorder is a no-op.
type recorder struct {
	f       *os.File
	pending string // buffered token (lag-by-one)
	has     bool   // whether pending holds a real token
	broken  bool   // a write failed; stop recording
}

// newRecorder creates/truncates path and writes a self-documenting comment
// header (which repo, and when) so a scenario file records its own context.
func newRecorder(path, repo string) (*recorder, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	r := &recorder{f: f}
	r.writeLine("# gg keystroke recording")
	r.writeLine("# repo: " + repo)
	r.writeLine("# recorded: " + time.Now().UTC().Format(time.RFC3339))
	return r, nil
}

func (r *recorder) writeLine(s string) {
	if r.broken {
		return
	}
	if _, err := fmt.Fprintln(r.f, s); err != nil {
		r.broken = true
	}
}

// note records one keypress. A supported key is buffered (lag-by-one) so the
// final quit can be dropped at close. An unsupported key flushes any buffered
// token, then writes a replay-skipped `#` comment so the scenario stays honest.
func (r *recorder) note(msg tea.KeyMsg) {
	if r == nil || r.broken {
		return
	}
	tok, ok := keyToken(msg)
	if !ok {
		if r.has {
			r.writeLine(r.pending)
			r.has = false
		}
		r.writeLine("# unrecorded key: " + msg.String())
		return
	}
	if r.has {
		r.writeLine(r.pending)
	}
	r.pending, r.has = tok, true
}

// close closes the file WITHOUT flushing the buffered token — that token is
// the session-terminating quit, which must not appear in a replayable script.
func (r *recorder) close() {
	if r == nil {
		return
	}
	_ = r.f.Close()
}

// keyToken maps a bubbletea key to a tui-capture token. ok is false for a key
// outside send_tokens' vocabulary (page keys, home/end, function keys, …); the
// caller records those as comments. Every ok==true token is one send_tokens
// accepts: a named key, a C-/M- chord, or a literal rune.
func keyToken(msg tea.KeyMsg) (string, bool) {
	// Alt-modified keys are not in send_tokens' vocabulary (meta+arrow/rune
	// does not round-trip reliably through tmux), and the type switch below
	// would otherwise silently collapse alt+down to "down", alt+a to "a", etc.
	// Mark them unsupported so the recorder emits an honest
	// "# unrecorded key: alt+…" comment instead of a wrong token.
	if msg.Alt {
		return "", false
	}
	switch msg.Type {
	case tea.KeyRunes:
		return string(msg.Runes), true
	case tea.KeyEnter:
		return "enter", true
	case tea.KeyEsc:
		return "esc", true
	case tea.KeySpace:
		return "space", true
	case tea.KeyTab:
		return "tab", true
	case tea.KeyUp:
		return "up", true
	case tea.KeyDown:
		return "down", true
	case tea.KeyLeft:
		return "left", true
	case tea.KeyRight:
		return "right", true
	case tea.KeyBackspace:
		return "bspace", true
	}
	s := msg.String()
	if strings.HasPrefix(s, "ctrl+") {
		return "C-" + strings.TrimPrefix(s, "ctrl+"), true
	}
	return "", false
}
