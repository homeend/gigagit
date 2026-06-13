package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// contentLine is one display line of a contentPopup. heading lines are section
// headers: the filter never matches them, and they survive filtering only
// while at least one non-heading line beneath them (before the next heading)
// matches.
//
// path/oldPath/status are an optional file payload (the commit files tree):
// the diff loader reads them straight off the selected VISIBLE line —
// payload-on-line is the only shape that survives the /-filter, whose
// visible() returns a reordered subset. Zero-valued for headings and for
// consumers that don't need them (the help window).
type contentLine struct {
	text    string
	heading bool
	path    string // file's (new) path
	oldPath string // set only for renames/copies
	status  string // model.CommitFile.Status letter ("A","M","D","R","C","T")
}

// contentPopup is a generic read-only viewer popup: any list of lines with
// repo-popup-style type-to-filter search and cursor-driven scrolling. The
// help window is its first consumer.
type contentPopup struct {
	title  string
	lines  []contentLine // full, unfiltered content
	query  string        // case-insensitive substring over non-heading lines
	typing bool          // true while /-input mode is capturing keys
	sel    int           // cursor index into the FILTERED view
}

func newContentPopup(title string, lines []contentLine) *contentPopup {
	return &contentPopup{title: title, lines: lines}
}

// visible returns the filtered lines in display order: non-heading lines
// matching query, plus each heading that still has a matching line.
func (p *contentPopup) visible() []contentLine {
	if p.query == "" {
		return p.lines
	}
	q := strings.ToLower(p.query)
	out := make([]contentLine, 0, len(p.lines))
	var pending *contentLine // last heading not yet emitted
	for i := range p.lines {
		l := p.lines[i]
		if l.heading {
			pending = &p.lines[i]
			continue
		}
		if strings.Contains(strings.ToLower(l.text), q) {
			if pending != nil {
				out = append(out, *pending)
				pending = nil
			}
			out = append(out, l)
		}
	}
	return out
}

// move shifts the cursor by delta, clamped to the visible range.
func (p *contentPopup) move(delta int) {
	n := len(p.visible())
	p.sel += delta
	if p.sel > n-1 {
		p.sel = n - 1
	}
	if p.sel < 0 {
		p.sel = 0
	}
}

// contentFastStep is the ctrl+↑/↓ jump (the mouse-wheel tick is the
// configurable wheelStep).
const contentFastStep = 5

// contentPageRows is the popup's visible row capacity: terminal height minus
// chrome (double border 2, vertical padding 2, title 1, blank after title 1,
// hint line 1), floored so tiny terminals still show a few rows.
func (m Model) contentPageRows() int {
	_, h := m.overlayDims()
	n := h - 7
	if n < 3 {
		n = 3
	}
	return n
}

// updateContentPopupKey handles all keys while the viewer is open. It swallows
// everything (no fallthrough to global handlers). Search mirrors the panel
// filter: / starts input mode, enter keeps the query, esc cancels it.
func (m Model) updateContentPopupKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	p := m.contentPopup
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	if p.typing { // /-input mode captures every key
		switch msg.Type {
		case tea.KeyEsc:
			p.typing = false
			p.query = ""
			p.sel = 0
		case tea.KeyEnter:
			p.typing = false // commit: search stays active
		case tea.KeyBackspace, tea.KeyCtrlH:
			if r := []rune(p.query); len(r) > 0 {
				p.query = string(r[:len(r)-1])
			}
			p.sel = 0
		case tea.KeySpace:
			p.query += " "
			p.sel = 0
		case tea.KeyRunes:
			p.query += string(msg.Runes)
			p.sel = 0
		}
		return m, nil
	}
	switch msg.String() {
	case "q": // close the window, not the app (q quits only at top level)
		m.contentPopup = nil
		return m, nil
	case "esc":
		if p.query != "" { // first esc clears the committed search, second closes
			p.query = ""
			p.sel = 0
			return m, nil
		}
		m.contentPopup = nil
		return m, nil
	case "enter":
		m.contentPopup = nil
		return m, nil
	case "/":
		p.typing = true
		p.query = ""
		p.sel = 0
	case "up", "k":
		p.move(-1)
	case "down", "j":
		p.move(1)
	case "ctrl+up":
		p.move(-contentFastStep)
	case "ctrl+down":
		p.move(contentFastStep)
	case "pgup":
		p.move(-m.contentPageRows())
	case "pgdown":
		p.move(m.contentPageRows())
	}
	return m, nil
}

// contentPopupWidth is the viewer's box width: wider than the standard
// 56-column form popup (popupInnerWidth) because it shows a two-column
// reference table — most of the terminal, capped at 100 columns.
func contentPopupWidth(w int) int {
	inner := 100
	if max := w - 8; inner > max {
		inner = max
	}
	if inner < 20 {
		inner = 20
	}
	return inner
}

// renderContentPopup draws the viewer box (composited by render via
// overlayCenter). Headings render bold, the cursor row reversed; the window
// follows the cursor via the same windowRows helper the panels use.
func (m Model) renderContentPopup() string {
	p := m.contentPopup
	w, _ := m.overlayDims()
	inner := contentPopupWidth(w)
	// lipgloss wraps text at Width minus the horizontal padding; truncate to
	// that true text width so a full-width row can never spill onto a wrap line.
	textW := inner - modalStyle.GetHorizontalPadding()

	vis := p.visible()
	rows := make([]string, len(vis))
	for i, l := range vis {
		switch {
		case i == p.sel:
			// Cursor highlight wins over heading style: the cursor must remain
			// visible even when it rests on a heading row.
			rows[i] = selectedRow.Render(truncate("> "+l.text, textW))
		case l.heading:
			rows[i] = titleStyle.Render(truncate(l.text, textW))
		default:
			rows[i] = truncate("  "+l.text, textW)
		}
	}
	capRows := m.contentPageRows()
	win, _, _ := windowRows(rows, capRows, p.sel)

	title := p.title
	if p.typing {
		title += "  /" + p.query + "█"
	} else if p.query != "" {
		title += "  /" + p.query
	}
	var b strings.Builder
	b.WriteString(truncate(title, textW) + "\n\n")
	if len(win) == 0 {
		b.WriteString("  (no match)\n")
	}
	for _, r := range win {
		b.WriteString(r + "\n")
	}
	hint := "[/] search  [q] close"
	if len(vis) > capRows {
		hint = fmt.Sprintf("%d/%d  %s", p.sel+1, len(vis), hint)
	}
	b.WriteString(truncate(hint, textW))
	return modalStyle.Width(inner).Render(b.String()) + "\n"
}
