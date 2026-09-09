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
	s = strings.ReplaceAll(s, "\n", " ")
	if _, err := fmt.Fprintln(r.f, s); err != nil {
		r.broken = true
	}
}

// note records one keypress. A supported key is buffered (lag-by-one) so the
// final quit can be dropped at close. A KeyRunes carrying several runes (fast
// typing coalesced into one message, or a paste) is expanded into one
// single-rune token per rune, so replay types them verbatim and a coalesced
// run like "up" can never be mis-sent as the Up key. keyToken's ok==false is
// now Alt-modified keys only (see its doc); one of those flushes any
// buffered token, then writes a replay-skipped `#` comment instead — every
// other key, including one outside the named vocabulary, still lands a real
// line (see keyToken's "<...>" fallback) so a recording never silently drops
// a keystroke.
func (r *recorder) note(msg tea.KeyMsg) {
	if r == nil || r.broken {
		return
	}
	if msg.Type == tea.KeyRunes && len(msg.Runes) > 1 {
		for _, ru := range msg.Runes {
			r.push(string(ru))
		}
		return
	}
	tok, ok := keyToken(msg)
	if !ok {
		r.flush()
		r.writeLine("# unrecorded key: " + msg.String())
		return
	}
	r.push(tok)
}

// push buffers a real token, flushing the previously buffered one (lag-by-one).
func (r *recorder) push(tok string) {
	if r.has {
		r.writeLine(r.pending)
	}
	r.pending, r.has = tok, true
}

// flush writes any buffered token now (used before a comment, to keep order).
func (r *recorder) flush() {
	if r.has {
		r.writeLine(r.pending)
		r.has = false
	}
}

// close closes the file WITHOUT flushing the buffered token — that token is
// the session-terminating quit, which must not appear in a replayable script.
func (r *recorder) close() {
	if r == nil {
		return
	}
	_ = r.f.Close()
}

// keyToken maps a bubbletea key to a tui-capture token. ok is false only for
// an Alt-modified key (meta+arrow/rune does not round-trip reliably through
// tmux); the caller records those as comments. Every other key lands a real
// token: the named send_tokens vocabulary, a C-/M- chord, a literal rune, or
// — for a key type outside all of those (function keys, and anything this
// vocabulary has not grown a name for yet) — a bracketed "<...>" fallback so
// a recording never silently drops a keystroke. That fallback is diagnostic
// only: tui-capture.sh's send_tokens recognizes it by shape and skips it
// rather than mis-sending it as literal text (see its own comment).
func keyToken(msg tea.KeyMsg) (string, bool) {
	// Alt-modified keys are not in send_tokens' vocabulary, and the type
	// switch below would otherwise silently collapse alt+down to "down",
	// alt+a to "a", etc. Mark them unsupported so the recorder emits an
	// honest "# unrecorded key: alt+…" comment instead of a wrong token.
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
	case tea.KeyDelete:
		return "delete", true
	case tea.KeyHome:
		return "home", true
	case tea.KeyEnd:
		return "end", true
	case tea.KeyPgUp:
		return "pgup", true
	case tea.KeyPgDown:
		return "pgdown", true
	}
	s := msg.String()
	if strings.HasPrefix(s, "ctrl+") {
		return "C-" + strings.TrimPrefix(s, "ctrl+"), true
	}
	return "<" + s + ">", true
}
