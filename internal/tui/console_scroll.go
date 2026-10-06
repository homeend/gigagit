package tui

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
)

// consoleScroll is scroll mode: a frozen History the user pages through
// while the program runs on underneath (spec §2).
type consoleScroll struct {
	hist   domain.SessionHistory
	top    int     // the first history row shown
	cursor int     // the cursor row (a history index)
	sel    lineSel // space/space/enter whole-line selection
	drag   charSel // a mouse stream selection
}

// charSel is a mouse stream selection over history cells; active while the
// button is held.
type charSel struct {
	on, active     bool
	r0, c0, r1, c1 int
}

// consoleRows is the content rows of the console's box.
func (m Model) consoleRows() int {
	w, h := m.consoleBox()
	_, rows := consoleInner(w, h)
	return rows
}

// consoleOwnsKeys: the console, not a panel, gets gg's keys — focused, or
// unfocused with its column focused (and no files tree holding them), or
// full-screen (updateConsoleKey's rule).
func (m Model) consoleOwnsKeys() bool {
	return m.console.focused || m.consoleFull() ||
		(m.focus == panelCommits && !(m.filesView != nil && m.filesTreeFocused))
}

// enterConsoleScroll freezes the console's history with the view at the
// bottom (exactly the live screen) and the cursor on the last row.
func (m Model) enterConsoleScroll() Model {
	s, ok := m.consoleSession()
	if !ok || m.console.scroll != nil {
		return m
	}
	h := s.History()
	rows := m.consoleRows()
	m.console.scroll = &consoleScroll{hist: h, top: max(h.Len()-rows, 0), cursor: max(h.Len()-1, 0)}
	return m
}

func (m Model) leaveConsoleScroll() Model {
	m.console.scroll = nil
	return m
}

// maxTop is the last view position: the bottom page.
func (sc *consoleScroll) maxTop(rows int) int { return max(sc.hist.Len()-rows, 0) }

// follow keeps the cursor inside the view after the view moved.
func (sc *consoleScroll) follow(rows int) {
	sc.cursor = max(min(sc.cursor, sc.top+rows-1, sc.hist.Len()-1), sc.top, 0)
}

// reveal moves the view so the cursor shows.
func (sc *consoleScroll) reveal(rows int) {
	if sc.cursor < sc.top {
		sc.top = sc.cursor
	}
	if sc.cursor >= sc.top+rows {
		sc.top = sc.cursor - rows + 1
	}
	sc.top = max(min(sc.top, sc.maxTop(rows)), 0)
}

// scrollConsoleBy moves the view d rows (the wheel); moving down from the
// bottom page leaves scroll mode — unless a drag is held, which stays.
func (m Model) scrollConsoleBy(d int) Model {
	sc := m.console.scroll
	if sc == nil {
		return m
	}
	rows := m.consoleRows()
	if d > 0 && sc.top >= sc.maxTop(rows) && !sc.drag.active {
		return m.leaveConsoleScroll()
	}
	sc.top = max(min(sc.top+d, sc.maxTop(rows)), 0)
	sc.follow(rows)
	return m
}

// consoleScrollKey is scroll mode's keyboard (spec §2). true = consumed;
// false = scroll mode has been left and the key goes on as usual (typing
// reaches the agent, alt+a cycles, the step-out key steps out).
func (m Model) consoleScrollKey(msg tea.KeyMsg) (Model, tea.Cmd, bool) {
	sc := m.console.scroll
	rows := m.consoleRows()
	last := sc.hist.Len() - 1
	switch msg.String() {
	case "up", "k":
		sc.cursor = max(sc.cursor-1, 0)
	case "down", "j":
		sc.cursor = min(sc.cursor+1, last)
	case "pgup", "alt+pgup":
		sc.cursor = max(sc.cursor-rows, 0)
		sc.top = max(sc.top-rows, 0)
	case "pgdown", "alt+pgdown":
		if sc.top >= sc.maxTop(rows) {
			return m.leaveConsoleScroll(), nil, true
		}
		sc.cursor = min(sc.cursor+rows, last)
		sc.top = min(sc.top+rows, sc.maxTop(rows))
	case "home", "g":
		sc.cursor, sc.top = 0, 0
	case "end", "G":
		sc.cursor, sc.top = last, sc.maxTop(rows)
	case " ":
		sc.sel.press(sc.cursor)
		sc.drag = charSel{}
		return m, nil, true
	case "enter":
		lo, hi, ok := sc.sel.bounds(sc.cursor)
		if !ok {
			lo, hi = sc.cursor, sc.cursor // enter with no selection copies the cursor row
		}
		text := sc.hist.Text(lo, 0, hi, sc.hist.Width()-1)
		sc.sel.clear()
		return m, m.copyToClipboardCmd(copiedLines(hi-lo+1), text), true
	case "esc":
		if sc.sel.on || sc.drag.on {
			sc.sel.clear()
			sc.drag = charSel{}
			return m, nil, true
		}
		return m.leaveConsoleScroll(), nil, true
	case "q":
		return m.leaveConsoleScroll(), nil, true
	default:
		return m.leaveConsoleScroll(), nil, false
	}
	sc.drag = charSel{}
	sc.reveal(rows)
	return m, nil, true
}

// consoleScrollTitle is the title row while scrolling: the position, a
// flag once the program has printed since the snapshot, and the keys.
func (m Model) consoleScrollTitle(s *domain.AgentSession) string {
	sc := m.console.scroll
	t := i18n.T("scroll ↑ %s / %s", fmt.Sprint(sc.top+1), fmt.Sprint(sc.hist.Len()))
	if s.LastOutput().After(sc.hist.Taken()) {
		t += " · " + i18n.T("new output")
	}
	return t + "  " + i18n.T("[esc] leave  [spc] select  [enter] copy")
}

// consoleRowMarks is history row i's highlight: the cursor, the line
// selection (whole row) or the stream selection's span on that row.
func (m Model) consoleRowMarks(i int) domain.SessionRowMarks {
	sc := m.console.scroll
	mk := domain.SessionRowMarks{Cursor: i == sc.cursor}
	if lo, hi, ok := sc.sel.bounds(sc.cursor); ok && i >= lo && i <= hi {
		mk.SelFrom, mk.SelTo = 0, sc.hist.Width()
	}
	if d := sc.drag; d.on {
		r0, c0, r1, c1 := d.r0, d.c0, d.r1, d.c1
		if r1 < r0 || (r1 == r0 && c1 < c0) {
			r0, c0, r1, c1 = r1, c1, r0, c0
		}
		if i >= r0 && i <= r1 {
			mk.SelFrom, mk.SelTo = 0, sc.hist.Width()
			if i == r0 {
				mk.SelFrom = c0
			}
			if i == r1 {
				mk.SelTo = c1 + 1
			}
		}
	}
	return mk
}

// consoleClicks counts left presses on one cell within clickWindow.
type consoleClicks struct {
	x, y, n int
	at      time.Time
}

const clickWindow = 400 * time.Millisecond

// press records a press and returns its count: 1, 2 (double), 3 (triple),
// then 1 again.
func (c *consoleClicks) press(x, y int, now time.Time) int {
	if c.n > 0 && x == c.x && y == c.y && now.Sub(c.at) <= clickWindow {
		c.n = c.n%3 + 1
	} else {
		c.n = 1
	}
	c.x, c.y, c.at = x, y, now
	return c.n
}

// historyAt maps a content cell to a history row/column in the frozen view,
// clamped to the history.
func (sc *consoleScroll) historyAt(cx, cy, rows int) (r, c int) {
	r = max(min(sc.top+max(min(cy, rows-1), 0), sc.hist.Len()-1), 0)
	return r, max(min(cx, sc.hist.Width()-1), 0)
}

// consoleScrollMouse is the mouse on a normal-screen console (spec §2): the
// wheel scrolls (entering scroll mode on the way up; while a drag is held it
// scrolls and extends it); a press starts a stream selection (entering
// scroll mode frozen at the live view), a second / third press on the same
// cell selects the word / the row; motion extends, past the top or bottom
// edge it scrolls a row; release copies a non-empty selection.
func (m Model) consoleScrollMouse(msg tea.MouseMsg, cx, cy int, inContent bool) (Model, tea.Cmd, bool) {
	rows := m.consoleRows()
	switch {
	case msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonWheelUp:
		m = m.enterConsoleScroll()
		m = m.scrollConsoleBy(-m.wheelStep())
		return m.extendDrag(cx, cy, rows), nil, true
	case msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonWheelDown:
		m = m.scrollConsoleBy(m.wheelStep())
		return m.extendDrag(cx, cy, rows), nil, true
	case msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft && inContent:
		m = m.enterConsoleScroll()
		sc := m.console.scroll
		if sc == nil {
			return m, nil, true
		}
		r, c := sc.historyAt(cx, cy, rows)
		sc.sel.clear()
		sc.cursor = r
		switch m.console.clicks.press(cx, cy, time.Now()) {
		case 2:
			c0, c1 := sc.hist.WordAt(r, c)
			if c0 > c1 {
				sc.drag = charSel{}
				return m, nil, true
			}
			sc.drag = charSel{on: true, r0: r, c0: c0, r1: r, c1: c1}
			return m, m.copyDrag(), true
		case 3:
			sc.drag = charSel{on: true, r0: r, c0: 0, r1: r, c1: max(sc.hist.RowWidth(r)-1, 0)}
			return m, m.copyDrag(), true
		}
		sc.drag = charSel{on: true, active: true, r0: r, c0: c, r1: r, c1: c}
		return m, nil, true
	case msg.Action == tea.MouseActionMotion:
		return m.extendDrag(cx, cy, rows), nil, true
	case msg.Action == tea.MouseActionRelease:
		sc := m.console.scroll
		if sc == nil || !sc.drag.active {
			return m, nil, true
		}
		m = m.extendDrag(cx, cy, rows)
		sc.drag.active = false
		if sc.drag.r0 == sc.drag.r1 && sc.drag.c0 == sc.drag.c1 {
			sc.drag = charSel{} // a plain click: nothing selected, nothing copied
			return m, nil, true
		}
		return m, m.copyDrag(), true
	}
	return m, nil, true
}

// extendDrag moves a held selection's far end to the content cell (cx, cy);
// a cell above / below the content scrolls one row first.
func (m Model) extendDrag(cx, cy, rows int) Model {
	sc := m.console.scroll
	if sc == nil || !sc.drag.active {
		return m
	}
	switch {
	case cy < 0:
		sc.top = max(sc.top-1, 0)
	case cy >= rows:
		sc.top = min(sc.top+1, sc.maxTop(rows))
	}
	sc.drag.r1, sc.drag.c1 = sc.historyAt(cx, cy, rows)
	sc.cursor = sc.drag.r1
	return m
}

// copyDrag copies the stream selection.
func (m Model) copyDrag() tea.Cmd {
	d := m.console.scroll.drag
	text := m.console.scroll.hist.Text(d.r0, d.c0, d.r1, d.c1)
	return m.copyToClipboardCmd(copiedLines(max(d.r0, d.r1)-min(d.r0, d.r1)+1), text)
}
