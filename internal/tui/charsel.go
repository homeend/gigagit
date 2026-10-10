package tui

import (
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/i18n"
)

// charsel.go — the character selection (spec 2026-10-10-char-select): a
// MODAL copy mode in the spirit of vim's visual mode and tmux's copy mode,
// over a host's rows of text. v enters it (the host's key), the cursor moves
// by rune / row / word / page, space fixes the start, enter or y copies the
// covered text and leaves, esc drops the start, then leaves. While it is on
// EVERY key is the mode's: the hosts route each key through charSelKey and
// never see one themselves (C5).
//
// The model is pure: it knows rows of runes (charRow) a host hands it, never
// display cells, gutters or line numbers. A wrapped paragraph is several
// rows, as on screen (the host says which rows continue the previous one,
// so a copy joins them with a space); a row the host marks dead (a diff
// side's absent cell, a folded line, a header) is stepped over and never
// copied.

// pos is a position in a host's rows: a row index and a rune index into it.
type pos struct{ row, col int }

// before reports whether p reads before q (row first, then column).
func (p pos) before(q pos) bool { return p.row < q.row || (p.row == q.row && p.col < q.col) }

// charRow is one row of a host's text.
type charRow struct {
	text  []rune
	wraps bool // continues the previous row: a copy joins them with one space
	dead  bool // not selectable: an absent diff cell, a folded line, a placeholder
}

// charHost is what a surface hands the mode: its rows, in display order,
// and the visible height (pgup/pgdn).
type charHost interface {
	charRows() []charRow
	charPage() int
}

// textSel is the mode's state. The zero value is "off".
type textSel struct {
	on     bool // the mode is active: a cursor shows, the keys are modal
	fixed  bool // space fixed the start: anchor..cur is the live range
	anchor pos
	cur    pos
	want   int // the column ↑/↓ try to keep (clamped on a shorter row)
}

// liveRow is the nearest live row from `from` stepping dir (±1), `from`
// itself included; -1 when there is none that way.
func liveRow(rows []charRow, from, dir int) int {
	for i := from; i >= 0 && i < len(rows); i += dir {
		if !rows[i].dead {
			return i
		}
	}
	return -1
}

// lastCol is the last rune index of a row (0 on an empty row: the cursor
// sits on the empty row itself).
func lastCol(rows []charRow, row int) int {
	if n := len(rows[row].text); n > 0 {
		return n - 1
	}
	return 0
}

// enter turns the mode on with the cursor at `at`, or on the first live row
// below it (then above it) when `at`'s row is dead; false — and off — when
// the host has no live row at all.
func (s *textSel) enter(rows []charRow, at pos) bool {
	*s = textSel{}
	if len(rows) == 0 {
		return false
	}
	row := at.row
	if row < 0 {
		row = 0
	}
	if row > len(rows)-1 {
		row = len(rows) - 1
	}
	r := liveRow(rows, row, 1)
	if r < 0 {
		r = liveRow(rows, row, -1)
	}
	if r < 0 {
		return false
	}
	col := at.col
	if r != at.row {
		col = 0
	}
	if col < 0 {
		col = 0
	}
	if col > lastCol(rows, r) {
		col = lastCol(rows, r)
	}
	s.on, s.cur, s.want = true, pos{r, col}, col
	return true
}

// leave turns the mode off.
func (s *textSel) leave() { *s = textSel{} }

// press is space: fix the start at the cursor — again, start over there.
func (s *textSel) press() {
	if !s.on {
		return
	}
	s.fixed, s.anchor = true, s.cur
}

// esc drops the fixed start, or — with none — leaves the mode. left says
// the mode is now off.
func (s *textSel) esc() (left bool) {
	if s.fixed {
		s.fixed = false
		return false
	}
	s.leave()
	return true
}

// bounds is the inclusive range anchor..cur in reading order; ok is false
// without a fixed start.
func (s textSel) bounds() (lo, hi pos, ok bool) {
	if !s.on || !s.fixed {
		return pos{}, pos{}, false
	}
	lo, hi = s.anchor, s.cur
	if hi.before(lo) {
		lo, hi = hi, lo
	}
	return lo, hi, true
}

// covers reports whether (row, col) is inside the fixed range.
func (s textSel) covers(row, col int) bool {
	lo, hi, ok := s.bounds()
	if !ok {
		return false
	}
	p := pos{row, col}
	return !p.before(lo) && !hi.before(p)
}

// count is how many runes the fixed range covers (dead rows skipped).
func (s textSel) count(rows []charRow) int {
	lo, hi, ok := s.bounds()
	if !ok {
		return 0
	}
	n := 0
	for i := lo.row; i <= hi.row && i < len(rows); i++ {
		if rows[i].dead {
			continue
		}
		a, b := 0, len(rows[i].text)
		if i == lo.row {
			a = lo.col
		}
		if i == hi.row {
			b = min(hi.col+1, len(rows[i].text))
		}
		if b > a {
			n += b - a
		}
	}
	return n
}

// --- movement: every move clamps to the rows and steps over dead ones; it
// reports whether the cursor went anywhere (false = a bump at an end) ----

func (s *textSel) left(rows []charRow) bool {
	if s.cur.col > 0 {
		s.cur.col--
		s.want = s.cur.col
		return true
	}
	if r := liveRow(rows, s.cur.row-1, -1); r >= 0 {
		s.cur = pos{r, lastCol(rows, r)}
		s.want = s.cur.col
		return true
	}
	return false
}

func (s *textSel) right(rows []charRow) bool {
	if s.cur.col < lastCol(rows, s.cur.row) {
		s.cur.col++
		s.want = s.cur.col
		return true
	}
	if r := liveRow(rows, s.cur.row+1, 1); r >= 0 {
		s.cur = pos{r, 0}
		s.want = 0
		return true
	}
	return false
}

// vertical moves n live rows in dir (±1), keeping the wanted column.
func (s *textSel) vertical(rows []charRow, dir, n int) bool {
	moved := false
	for k := 0; k < n; k++ {
		r := liveRow(rows, s.cur.row+dir, dir)
		if r < 0 {
			break
		}
		s.cur.row = r
		moved = true
	}
	if moved {
		s.cur.col = min(s.want, lastCol(rows, s.cur.row))
	}
	return moved
}

func (s *textSel) home(rows []charRow) bool {
	moved := s.cur.col != 0
	s.cur.col, s.want = 0, 0
	return moved
}

func (s *textSel) end(rows []charRow) bool {
	c := lastCol(rows, s.cur.row)
	moved := s.cur.col != c
	s.cur.col, s.want = c, c
	return moved
}

// wordStarts lists every word start (a non-space rune after a space, a
// row's first non-space rune) in reading order.
func wordStarts(rows []charRow) []pos {
	var out []pos
	for i, r := range rows {
		if r.dead {
			continue
		}
		inWord := false
		for c, ch := range r.text {
			if unicode.IsSpace(ch) {
				inWord = false
				continue
			}
			if !inWord {
				out = append(out, pos{i, c})
				inWord = true
			}
		}
	}
	return out
}

// word is w (dir 1): the next word start after the cursor; b (dir -1): the
// previous one before it.
func (s *textSel) word(rows []charRow, dir int) bool {
	starts := wordStarts(rows)
	if dir > 0 {
		for _, p := range starts {
			if s.cur.before(p) {
				s.cur, s.want = p, p.col
				return true
			}
		}
		return false
	}
	for i := len(starts) - 1; i >= 0; i-- {
		if starts[i].before(s.cur) {
			s.cur, s.want = starts[i], starts[i].col
			return true
		}
	}
	return false
}

// charSelText is the covered text: on the first row from lo.col, on the
// last to hi.col inclusive, whole rows between; dead rows skipped; a wrap
// continuation joins the row before it with one space, any other row with a
// newline; no trailing newline.
func charSelText(rows []charRow, lo, hi pos) string {
	var b strings.Builder
	first := true
	for i := lo.row; i <= hi.row && i < len(rows); i++ {
		r := rows[i]
		if r.dead {
			continue
		}
		a, e := 0, len(r.text)
		if i == lo.row {
			a = min(lo.col, len(r.text))
		}
		if i == hi.row {
			e = min(hi.col+1, len(r.text))
		}
		if !first {
			if r.wraps {
				b.WriteByte(' ')
			} else {
				b.WriteByte('\n')
			}
		}
		first = false
		if e > a {
			b.WriteString(string(r.text[a:e]))
		}
	}
	return b.String()
}

// charSelResult is what a key did: handled (every key while the mode is on,
// except ctrl+c), the copied text (enter / y with a fixed start), a notice
// for the host's status line, and bump — a move that hit an end (a stacked
// host says which element bounds it).
type charSelResult struct {
	handled bool
	copy    string
	count   int // the runes the copy COVERS (what the hint counted; the joins add more)
	notice  string
	bump    bool
}

// charSelKey applies one key to cs over host. The hosts call it for every
// key while cs.on and act on the result; they never interpret a key
// themselves in the mode (C5).
func charSelKey(cs *textSel, host charHost, msg tea.KeyMsg) charSelResult {
	if !cs.on || msg.Type == tea.KeyCtrlC {
		return charSelResult{}
	}
	rows := host.charRows()
	res := charSelResult{handled: true}
	moved := true
	switch msg.String() {
	case "left", "h":
		moved = cs.left(rows)
	case "right", "l":
		moved = cs.right(rows)
	case "up", "k":
		moved = cs.vertical(rows, -1, 1)
	case "down", "j":
		moved = cs.vertical(rows, 1, 1)
	case "pgup":
		moved = cs.vertical(rows, -1, max(host.charPage(), 1))
	case "pgdown":
		moved = cs.vertical(rows, 1, max(host.charPage(), 1))
	case "home", "0":
		cs.home(rows)
	case "end", "$":
		cs.end(rows)
	case "w":
		moved = cs.word(rows, 1)
	case "b":
		moved = cs.word(rows, -1)
	case " ":
		cs.press()
	case "enter", "y":
		lo, hi, ok := cs.bounds()
		if !ok {
			res.notice = i18n.T("▸ space fixes the start first")
			return res
		}
		res.copy = charSelText(rows, lo, hi)
		res.count = cs.count(rows)
		cs.leave()
	case "esc":
		cs.esc()
	}
	res.bump = !moved
	return res
}

// charSelHint is the footer while the mode is on: the keys of its stage.
func charSelHint(cs textSel, rows []charRow) string {
	if cs.fixed {
		return i18n.T("[←→↑↓ hjkl] move  [space] restart  [enter y] copy %d chars  [esc] drop", cs.count(rows))
	}
	return i18n.T("[←→↑↓ hjkl] move  [w b] word  [space] start  [enter] copy  [esc] leave")
}

// copiedCharsText is the status line after a copy.
func copiedCharsText(n int) string {
	if n == 1 {
		return i18n.T("Copied 1 character")
	}
	return i18n.T("Copied %d characters", n)
}

// charSelEmph is the emphasis mask of row `row` (n runes): emphSel on the
// covered runes, emphSelCur on the cursor; nil when the row is untouched.
func charSelEmph(cs textSel, row, n int) []emphLevel {
	if !cs.on {
		return nil
	}
	var out []emphLevel
	set := func(i int, l emphLevel) {
		if i < 0 || i >= n {
			return
		}
		if out == nil {
			out = make([]emphLevel, n)
		}
		out[i] = l
	}
	if lo, hi, ok := cs.bounds(); ok && lo.row <= row && row <= hi.row {
		a, e := 0, n-1
		if row == lo.row {
			a = lo.col
		}
		if row == hi.row {
			e = hi.col
		}
		for i := a; i <= e; i++ {
			set(i, emphSel)
		}
	}
	if cs.cur.row == row {
		set(cs.cur.col, emphSelCur)
	}
	return out
}

// shiftMask places mask behind `lead` runes in a row of n: what a host whose
// display row starts with layout (a "  " lead, a wrap indent) needs.
func shiftMask(mask []emphLevel, lead, n int) []emphLevel {
	if mask == nil {
		return nil
	}
	out := make([]emphLevel, n)
	for i, l := range mask {
		if j := i + lead; j >= 0 && j < n {
			out[j] = l
		}
	}
	return out
}

// charSelSpans is the selection on row `row` as spans in the row's own rune
// space (sel: true): the covered range, then the cursor cell (cur: true) —
// in that order, so the cursor paints over the stripe. nil when untouched.
func charSelSpans(cs textSel, row int) []hitSpan {
	if !cs.on {
		return nil
	}
	var out []hitSpan
	if lo, hi, ok := cs.bounds(); ok && lo.row <= row && row <= hi.row {
		a, e := 0, 1<<30
		if row == lo.row {
			a = lo.col
		}
		if row == hi.row {
			e = hi.col + 1
		}
		out = append(out, hitSpan{start: a, end: e, sel: true})
	}
	if cs.cur.row == row {
		out = append(out, hitSpan{start: cs.cur.col, end: cs.cur.col + 1, sel: true, cur: true})
	}
	return out
}
