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
	lines []uv.Line
	width int
	taken time.Time
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
	return History{lines: lines, width: w, taken: time.Now()}
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
			row[c].Style.Attrs ^= uv.AttrReverse
		}
		if mk.Cursor {
			row[c].Style.Underline = uv.UnderlineSingle
		}
	}
	return row.Render()
}

// RowWidth is the columns up to row r's last non-blank cell.
func (h History) RowWidth(r int) int {
	for c := h.width - 1; c >= 0; c-- {
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
		from, to := 0, h.width-1
		if r == r0 {
			from = c0
		}
		if r == r1 {
			to = min(c1, h.width-1)
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
	for c1 < h.width-1 && !h.wordBreak(r, c1+1) {
		c1++
	}
	return c0, c1
}
