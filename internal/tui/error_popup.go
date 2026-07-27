package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/i18n"
)

// The status bar is one line, so a long failure — a git push against an
// unreachable host writes a paragraph of stderr — is cut off at the terminal
// edge. [E] reopens that message here in full: wrapped, scrollable, and
// framed in red so it reads as the failure it is. The text shown is the same
// string the status bar led with, kept untruncated in Model.lastError by the
// Update wrapper.
//
// This is a contentPopup, not a new surface: search, scrolling, the z display
// modes and ctrl+t maximize all come for free, and the viewer is read-only.
// The only additions are the wrap-by-default mode (a status message is prose,
// not a row list, so cutoff mode would hide the tail all over again) and the
// danger frame.

// openErrorPopup shows the last failure, wrapped. It is inert when nothing has
// failed yet — the [E] footer hint is gated on the same condition, so the key
// only advertises itself once there is something to show.
func (m Model) openErrorPopup() (tea.Model, tea.Cmd) {
	if m.lastError == "" {
		return m, nil
	}
	return m.pushLayer(newErrorPopup(m.lastError)), nil
}

// newErrorPopup builds the viewer over a failure message. git writes multi-line
// stderr, so the message is split on newlines into one contentLine each and
// left to wrap — collapsing it to a single line the way the status bar does
// would throw away the structure git provides.
func newErrorPopup(msg string) *contentPopup {
	var lines []contentLine
	for _, l := range strings.Split(strings.TrimRight(sanitizeForDisplay(msg), "\n"), "\n") {
		lines = append(lines, contentLine{text: strings.TrimRight(l, " \t")})
	}
	// The viewer also serves messages that are merely too long for the bar, so
	// the red frame and the title follow what the message actually is — framing
	// a successful summary as an error would misreport it.
	isErr := statusIsError(msg)
	title := i18n.T("Full message")
	if isErr {
		title = i18n.T("Last error")
	}
	p := newContentPopup(title, lines)
	p.mode = modeWrap
	p.danger = isErr
	p.noCursor = true
	return p
}

// sanitizeForDisplay makes arbitrary git/ssh stderr safe to draw inside a box.
// The text is not ours: ssh ends its lines with CRLF, and a surviving \r tells
// the terminal to jump back to column 0 mid-line — the rest of the row then
// overwrites the box's own left border and the text that was there. Width math
// cannot see it either, since \r measures as zero columns, so the corruption
// only shows on a real terminal. Tabs are the same class of problem from the
// other side: they measure as one column but a terminal expands them to the
// next tab stop, shifting everything right of them off the box. The one-line
// status bar never hit any of this because oneLine collapses every whitespace
// run, including \r and \t, before it renders.
//
// Newlines survive — the caller splits on them to keep git's line structure.
func sanitizeForDisplay(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\n':
			b.WriteRune(r)
		case r == '\t':
			b.WriteString("    ") // width-exact: measured after substitution
		case r < 0x20 || r == 0x7f:
			// Every other C0 control (\r, ESC, backspace, …) can move the cursor
			// or start an escape sequence; drop it rather than let stderr draw.
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
