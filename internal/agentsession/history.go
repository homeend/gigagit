package agentsession

import (
	"slices"
	"strings"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
)

// History is a frozen copy of what the session printed: its scrollback (the
// main screen's; none in the alt screen) followed by the visible screen.
// Scrollback lines are cloned by the emulator when pushed and never written
// again, so the snapshot shares them and copies only the slice of headers;
// screen rows are cloned. Rows render lazily (Row), a page at a time.
type History struct {
	lines  []uv.Line
	width  int
	taken  time.Time
	extent Extent
}

// Extent is where the program's output stands: the scrollback length, the
// screen rows up to the last non-blank one, which screen it is on, and
// whether the scrollback is at its cap. A redraw in place (a spinner) leaves
// it as it was; a new line, a clear or a switch of screen does not.
type Extent struct {
	Scrollback, Screen int
	Alt, Full          bool
}

// Extent measures the output now.
func (s *Session) Extent() Extent {
	s.ioMu.Lock()
	defer s.ioMu.Unlock()
	return s.extent()
}

// extent is Extent under ioMu.
func (s *Session) extent() Extent {
	e := Extent{Alt: s.emu.IsAltScreen()}
	if !e.Alt {
		e.Scrollback = s.emu.ScrollbackLen()
		e.Full = e.Scrollback >= ScrollbackLines
	}
	w := s.emu.Width()
	for y := s.emu.Height() - 1; y >= 0; y-- {
		for x := range w {
			if c := s.emu.CellAt(x, y); c != nil && c.Content != "" && c.Content != " " {
				e.Screen = y + 1
				return e
			}
		}
	}
	return e
}

// History snapshots the scrollback and the screen.
func (s *Session) History() History {
	s.ioMu.Lock() // no write or resize in between: CellAt hands out live cells
	defer s.ioMu.Unlock()
	w, h := s.emu.Width(), s.emu.Height()
	var lines []uv.Line
	if !s.emu.IsAltScreen() {
		if sb := s.emu.Scrollback(); sb != nil {
			lines = slices.Clone(sb.Lines())
		}
	}
	for y := range h {
		row := make(uv.Line, w)
		for x := range w {
			if c := s.emu.CellAt(x, y); c != nil {
				row[x] = *c
			} else {
				row[x] = uv.EmptyCell
			}
		}
		lines = append(lines, row)
	}
	return History{lines: lines, width: w, taken: time.Now(), extent: s.extent()}
}

// Outgrown reports whether the output now (now = Session.Extent) has rows
// this snapshot lacks: the scrollback changed (grew, or a clear wiped it),
// the screen's output reaches further, or it is on the other screen. With
// the scrollback full a new line pushes one out and no length moves, so
// there it is always true: the caller's LastOutput check decides alone.
func (h History) Outgrown(now Extent) bool {
	return now.Full || now.Alt != h.extent.Alt || now.Scrollback != h.extent.Scrollback || now.Screen > h.extent.Screen
}

// Len is the number of rows: scrollback then screen.
func (h History) Len() int { return len(h.lines) }

// Width is the emulator width when the snapshot was taken.
func (h History) Width() int { return h.width }

// Taken is when the snapshot was taken (compare with LastOutput).
func (h History) Taken() time.Time { return h.taken }

// cell is row r's cell at column c; a blank past a trimmed scrollback line.
func (h History) cell(r, c int) uv.Cell {
	if r < 0 || r >= len(h.lines) || c < 0 || c >= len(h.lines[r]) {
		return uv.EmptyCell
	}
	return h.lines[r][c]
}

// wideHalf: (r, c) is the right half of the wide glyph before it.
func (h History) wideHalf(r, c int) bool {
	if c <= 0 {
		return false
	}
	x, prev := h.cell(r, c), h.cell(r, c-1)
	return x.IsZero() && prev.Width > 1
}

// wordBreak: a space, or an empty cell that is not a wide glyph's right half.
func (h History) wordBreak(r, c int) bool {
	if h.wideHalf(r, c) {
		return false
	}
	x := h.cell(r, c)
	return x.Content == "" || x.Content == " "
}

// RowMarks highlights one rendered row: cells SelFrom..SelTo-1 reversed
// (a selection; none when SelFrom >= SelTo), Cursor underlines the whole
// row (scroll mode's cursor).
type RowMarks struct {
	SelFrom, SelTo int
	Cursor         bool
}

// Row renders row i, exactly Width cells, with its marks.
func (h History) Row(i int, mk RowMarks) string {
	row := make(uv.Line, h.width)
	for c := range h.width {
		row[c] = h.cell(i, c)
		if (&row[c]).IsZero() {
			if h.wideHalf(i, c) {
				continue // the glyph to its left draws both columns
			}
			row[c] = uv.EmptyCell // a stray zero cell
		}
		if c >= mk.SelFrom && c < mk.SelTo {
			row[c].Style.Attrs |= uv.AttrReverse // set, not flip: a cell the program drew reversed stays marked
		}
		if mk.Cursor {
			row[c].Style.Underline = uv.UnderlineSingle
		}
	}
	return row.Render()
}

// rowCells is row r's length in cells: the snapshot width, or more for a
// scrollback line printed while the emulator was wider (x/vt keeps old
// lines as they were, no reflow).
func (h History) rowCells(r int) int {
	if r >= 0 && r < len(h.lines) {
		return max(h.width, len(h.lines[r]))
	}
	return h.width
}

// RowWidth is the columns up to row r's last non-blank cell.
func (h History) RowWidth(r int) int {
	for c := h.rowCells(r) - 1; c >= 0; c-- {
		if !h.wordBreak(r, c) {
			return c + 1
		}
	}
	return 0
}

// Text is the stream selection from (r0, c0) to (r1, c1), both ends
// included, in either order: the first row from c0, whole rows between,
// the last row to c1. Cell text only; a wide glyph's right half is skipped
// and a selection that starts on one takes the glyph; trailing blanks are
// trimmed per row; rows join with \n (the emulator keeps no soft-wrap flag).
func (h History) Text(r0, c0, r1, c1 int) string {
	if r1 < r0 || (r1 == r0 && c1 < c0) {
		r0, c0, r1, c1 = r1, c1, r0, c0
	}
	rows := make([]string, 0, r1-r0+1)
	for r := r0; r <= r1; r++ {
		from, to := 0, h.rowCells(r)-1
		if r == r0 {
			from = c0
		}
		if r == r1 {
			to = min(c1, to)
		}
		if h.wideHalf(r, from) {
			from-- // started on a right half: take the glyph
		}
		var b strings.Builder
		for c := max(from, 0); c <= to; c++ {
			x := h.cell(r, c)
			switch {
			case h.wideHalf(r, c):
			case x.Content == "":
				b.WriteByte(' ')
			default:
				b.WriteString(x.Content)
			}
		}
		rows = append(rows, strings.TrimRight(b.String(), " "))
	}
	return strings.Join(rows, "\n")
}

// WordAt is the inclusive column run of non-blank cells around (r, c);
// c0 > c1 when (r, c) is blank.
func (h History) WordAt(r, c int) (c0, c1 int) {
	if h.wideHalf(r, c) {
		c-- // a wide glyph's right half belongs to the glyph
	}
	if h.wordBreak(r, c) {
		return 1, 0
	}
	c0, c1 = c, c
	for c0 > 0 && !h.wordBreak(r, c0-1) {
		c0--
	}
	for c1 < h.rowCells(r)-1 && !h.wordBreak(r, c1+1) {
		c1++
	}
	return c0, c1
}
