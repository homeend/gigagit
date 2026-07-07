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

// maxScroll is the highest scroll offset that still fills the body with content.
func (r *reviewView) maxScroll(body int) int {
	max := len(r.lines) - body
	if max < 0 {
		max = 0
	}
	return max
}

func (r *reviewView) clampScroll(body int) {
	if max := r.maxScroll(body); r.scroll > max {
		r.scroll = max
	}
	if r.scroll < 0 {
		r.scroll = 0
	}
}

// jumpToNextMatch scrolls to the next line (after the current scroll,
// wrapping) containing the committed query, case-insensitive. A no-op when
// the query is empty or matches nothing.
func (r *reviewView) jumpToNextMatch() {
	q := strings.ToLower(r.query)
	n := len(r.lines)
	if q == "" || n == 0 {
		return
	}
	for i := 1; i <= n; i++ {
		idx := (r.scroll + i) % n
		if strings.Contains(strings.ToLower(r.lines[idx]), q) {
			r.scroll = idx
			return
		}
	}
}

func (r *reviewView) render(m Model, _ string) string {
	w, scrH := m.overlayDims()
	body := r.bodyRows(m)
	r.clampScroll(body)

	header := truncate(r.title, w)
	hint := truncate("[↑↓] scroll  [pgup/pgdn] page  [e] edit  [/] search  [esc] close", w)
	if r.typing {
		hint = truncate("/"+r.query+"█", w)
	} else if r.query != "" {
		hint = truncate("/"+r.query+"  [n] next  "+hint, w)
	}

	end := r.scroll + body
	if end > len(r.lines) {
		end = len(r.lines)
	}
	visible := r.lines[r.scroll:end]

	rows := make([]string, body)
	for i := 0; i < body; i++ {
		if i < len(visible) {
			rows[i] = padRight(truncate(sanitizeLine(visible[i]), w), w)
		} else {
			rows[i] = padRight("", w)
		}
	}
	if len(r.lines) == 0 {
		rows[0] = padRight(truncate("(empty report)", w), w)
	}

	out := header + "\n" + strings.Join(rows, "\n") + "\n" + hint
	return clipToHeight(out, scrH)
}

func (r *reviewView) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	body := r.bodyRows(m)

	if r.typing {
		switch msg.Type {
		case tea.KeyEsc:
			r.typing = false
			r.query = ""
		case tea.KeyEnter:
			r.typing = false
			r.jumpToNextMatch()
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
		r.jumpToNextMatch()
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
		r.scroll = r.maxScroll(body)
	}
	r.clampScroll(body)
	return m, nil
}
