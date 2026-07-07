package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// reviewView is the full-screen, read-only report viewer: displays a captured
// review report (the review-tool capture lane's Result) as plain scrollable
// text. One responsibility: display a report. No markdown rendering, no
// per-line cursor — just a titled scrollable box over `lines` with a `scroll`
// offset naming the first visible line.
type reviewView struct {
	path   string // report file path; reopened by 'e'
	title  string
	lines  []string
	scroll int

	mode    dispMode // long-line layout: cutoff / wrap / scroll (z cycles)
	hscroll int      // modeScroll horizontal offset (columns); meaningful only in modeScroll

	typing bool   // true while a '/' search query is being typed
	query  string // last committed search substring (case-insensitive)
}

// newReviewView splits content into display lines. A single trailing "\n" (the
// common case for a generated report) contributes no extra blank line.
func newReviewView(title, path, content string) *reviewView {
	lines := strings.Split(content, "\n")
	if n := len(lines); n > 0 && lines[n-1] == "" {
		lines = lines[:n-1]
	}
	return &reviewView{title: title, path: path, lines: lines}
}

// bodyRows is the scrollable area height: full height minus header + hint.
func (r *reviewView) bodyRows(m Model) int {
	_, h := m.overlayDims()
	n := h - 2
	if n < 1 {
		n = 1
	}
	return n
}

// displayLines projects the logical lines onto the mode's display layout at
// width w. Every logical line contributes at least one display line, and
// scroll/search/maxScroll all operate over THIS slice (so a scroll offset is a
// display-line index, not a logical-line index — they coincide only in the
// single-line modes). sanitizeLine is applied here, once, so callers pad the
// result straight to width.
func (r *reviewView) displayLines(w int) []string {
	if w < 1 {
		w = 1
	}
	out := make([]string, 0, len(r.lines))
	for _, ln := range r.lines {
		s := sanitizeLine(ln)
		switch r.mode {
		case modeWrap:
			segs := wrapWidth(s, w, 1<<20) // huge cap => clean full wrap, no ellipsis
			if len(segs) == 0 {
				segs = []string{""} // an empty logical line is still one display line
			}
			out = append(out, segs...)
		case modeScroll:
			out = append(out, hslice(s, r.hscroll, w))
		default: // modeCutoff
			out = append(out, truncate(s, w))
		}
	}
	return out
}

// maxScroll is the highest scroll offset that still fills the body with content.
// n is the display-line count (len(displayLines)).
func (r *reviewView) maxScroll(body, n int) int {
	max := n - body
	if max < 0 {
		max = 0
	}
	return max
}

func (r *reviewView) clampScroll(body, n int) {
	if max := r.maxScroll(body, n); r.scroll > max {
		r.scroll = max
	}
	if r.scroll < 0 {
		r.scroll = 0
	}
}

// jumpToNextMatch scrolls to the next display line (after the current scroll,
// wrapping) containing the committed query, case-insensitive. It scans the
// mode's display lines at width w — a match split across a wrap boundary is an
// accepted rare miss. A no-op when the query is empty or matches nothing.
func (r *reviewView) jumpToNextMatch(w int) {
	q := strings.ToLower(r.query)
	dl := r.displayLines(w)
	n := len(dl)
	if q == "" || n == 0 {
		return
	}
	for i := 1; i <= n; i++ {
		idx := (r.scroll + i) % n
		if strings.Contains(strings.ToLower(dl[idx]), q) {
			r.scroll = idx
			return
		}
	}
}

func (r *reviewView) render(m Model, _ string) string {
	w, scrH := m.overlayDims()
	body := r.bodyRows(m)
	dl := r.displayLines(w)
	r.clampScroll(body, len(dl))

	header := truncate(r.title, w)
	hint := truncate("[↑↓] scroll  [pgup/pgdn] page  [z] wrap  [e] edit  [/] search  [esc] close", w)
	if r.typing {
		hint = truncate("/"+r.query+"█", w)
	} else if r.query != "" {
		hint = truncate("/"+r.query+"  [n] next  "+hint, w)
	}

	end := r.scroll + body
	if end > len(dl) {
		end = len(dl)
	}
	var visible []string
	if r.scroll < len(dl) {
		visible = dl[r.scroll:end]
	}

	rows := make([]string, body)
	for i := 0; i < body; i++ {
		if i < len(visible) {
			rows[i] = padRight(visible[i], w) // displayLines already sanitized + width-fit
		} else {
			rows[i] = padRight("", w)
		}
	}
	if len(dl) == 0 {
		rows[0] = padRight(truncate("(empty report)", w), w)
	}

	out := header + "\n" + strings.Join(rows, "\n") + "\n" + hint
	return clipToHeight(out, scrH)
}

func (r *reviewView) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	w, _ := m.overlayDims()
	body := r.bodyRows(m)

	if r.typing {
		switch msg.Type {
		case tea.KeyEsc:
			r.typing = false
			r.query = ""
		case tea.KeyEnter:
			r.typing = false
			r.jumpToNextMatch(w)
		case tea.KeyBackspace, tea.KeyCtrlH:
			if rs := []rune(r.query); len(rs) > 0 {
				r.query = string(rs[:len(rs)-1])
			}
		case tea.KeySpace:
			r.query += " "
		case tea.KeyRunes:
			r.query += string(msg.Runes)
		}
		return m, nil
	}

	switch msg.String() {
	case "esc":
		return m.popLayer(), nil
	case "e": // open the report file in $EDITOR (read-only view, like history/blame)
		return m, m.openInEditorCmd(filepath.Base(r.path), func(ctx context.Context) ([]byte, error) {
			return os.ReadFile(r.path)
		})
	case "/":
		r.typing = true
		r.query = ""
	case "n": // jump to the next match of the committed search
		r.jumpToNextMatch(w)
	case "z": // cycle the long-line layout (cutoff / wrap / scroll)
		r.mode = r.mode.next()
		r.hscroll = 0
	case "shift+left":
		if r.mode == modeScroll && r.hscroll > 0 {
			if r.hscroll -= m.hscrollStep(); r.hscroll < 0 {
				r.hscroll = 0
			}
		}
	case "shift+right":
		if r.mode == modeScroll {
			r.hscroll += m.hscrollStep()
		}
	case "down", "j":
		r.scroll++
	case "up", "k":
		r.scroll--
	case "pgdown":
		r.scroll += body
	case "pgup":
		r.scroll -= body
	case "home":
		r.scroll = 0
	case "end":
		r.scroll = 1 << 30 // clampScroll below pins it to maxScroll for the current mode
	}
	r.clampScroll(body, len(r.displayLines(w)))
	return m, nil
}
