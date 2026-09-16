package tui

import (
	"fmt"
	"sort"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// emphLevel is one display rune's emphasis in a paint mask. It replaces the
// single bool the word-diff used to carry, because the in-view search needs two
// more states than "emphasized": an ordinary hit and THE current one (spec
// §4.3). Precedence is the enum order — the current hit outranks a hit, a hit
// outranks word emphasis, and any of them outranks the syntax colour, which is
// what keeps a search hit legible on a changed line.
type emphLevel uint8

const (
	emphNone emphLevel = iota // plain: the syntax class (if any) paints
	emphWord                  // inside a word-diff span (what sanitizeCell marks)
	emphHit                   // inside a search hit
	emphCur                   // inside the CURRENT search hit
)

// hitSpan is a search hit reduced to what a painter needs: a half-open range of
// DISPLAY runes in the line the painter is about to draw, and whether it is the
// current hit. Painters never see searchHit — hitsOn converts.
type hitSpan struct {
	start, end int
	cur        bool
}

// overlayHits paints hit spans onto a display-rune emphasis mask. emph is the
// mask of the n runes starting at display-rune offset off (nil when the caller
// has no mask yet — an unlexed blame line); the spans are offsets in the SAME
// index space as off, i.e. the whole sanitized line.
//
// When nothing intersects the window the input is returned untouched, so a view
// with no search allocates nothing and renders byte-identically. When something
// does, a COPY is painted: every mask a caller hands in may be a shared cache
// value (the picker gives the same sanLine to the grid and to the output pane,
// and textdiff rows are shared across views), so writing through is a bug.
func overlayHits(emph []emphLevel, off, n int, hits []hitSpan) []emphLevel {
	if n <= 0 || len(hits) == 0 {
		return emph
	}
	var out []emphLevel
	for _, h := range hits {
		lo, hi := h.start-off, h.end-off
		if lo < 0 {
			lo = 0
		}
		if hi > n {
			hi = n
		}
		if lo >= hi {
			continue
		}
		if out == nil {
			out = make([]emphLevel, n)
			copy(out, emph)
		}
		lvl := emphHit
		if h.cur {
			lvl = emphCur
		}
		for i := lo; i < hi; i++ {
			out[i] = lvl
		}
	}
	if out == nil {
		return emph
	}
	return out
}

// searchLine is one searchable line handed to findHits: the host's own row id,
// which column it belongs to (0 = left / current / the only column, 1 = right /
// incoming) and the DISPLAY text — already sanitized, because every offset this
// file produces is a display-rune offset the painters index masks with.
//
// NOTE: (*contentPopup).searchLine is an unrelated method that renders the
// popup's own /-input line; a method name and a package type may coincide.
type searchLine struct {
	row  int
	side int
	text string
}

// searchHit is one match: [start, end) display-rune offsets into the line
// (row, side). Hits are kept in document order — row, then side, then start.
type searchHit struct {
	row, side  int
	start, end int
}

// searchPos is a position in the same coordinates, used to decide which hit is
// "next". col may be -1, meaning "before anything on this row" — which is what
// a host whose cursor is a whole row reports when the cursor is not sitting on
// a hit, so ] finds the first hit on the line the user just walked to.
type searchPos struct {
	row, side, col int
}

// textSearch is one host's in-view search state (spec §4.3). cur is an index
// into hits, -1 when there is none. origin is where the incremental search
// measures from — the cursor when / or @ opened it; the host keeps its own view
// state (scroll offsets, its 2D cursor) to restore when esc cancels.
type textSearch struct {
	query    string
	typing   bool
	backward bool
	hits     []searchHit
	cur      int
	origin   searchPos
}

// active reports whether a search is live: typing, or a committed query.
func (s textSearch) active() bool { return s.typing || s.query != "" }

// open starts a search from pos. The host records its own view origin first.
func (s *textSearch) open(backward bool, pos searchPos) {
	s.query = ""
	s.typing = true
	s.backward = backward
	s.hits = nil
	s.cur = -1
	s.origin = pos
}

// clear drops the query and every hit (esc on a committed search).
func (s *textSearch) clear() {
	s.query = ""
	s.typing = false
	s.hits = nil
	s.cur = -1
}

// refindFrom re-runs the search over lines and re-snaps cur to the hit nearest
// pos. Hosts call it after every keystroke (from the origin) and after any
// rebuild that can move rows (from the cursor).
func (s *textSearch) refindFrom(lines []searchLine, pos searchPos) {
	s.hits = findHits(lines, s.query)
	s.cur = nearestHit(s.hits, pos, s.backward)
}

// badge is the header status: "/foo  3/12", "@foo" for a backward search, a
// block cursor while typing, "0/0" when nothing matched, "" when no search is
// active. Deliberately built WITHOUT i18n.T — it is punctuation, the user's own
// query and two numbers, so there is nothing to translate and no bundle key to
// keep in sync.
func (s textSearch) badge() string {
	if !s.active() {
		return ""
	}
	lead := "/"
	if s.backward {
		lead = "@"
	}
	b := lead + s.query
	if s.typing {
		b += "█"
	}
	if s.query == "" {
		return b
	}
	n, i := len(s.hits), 0
	if n > 0 && s.cur >= 0 && s.cur < n {
		i = s.cur + 1
	}
	return b + "  " + fmt.Sprintf("%d/%d", i, n)
}

// hitsOn returns the spans a painter must paint on one line, the current hit
// flagged. Binary search: this runs once per visible row per frame, and a big
// file can hold thousands of hits.
func (s textSearch) hitsOn(row, side int) []hitSpan {
	if len(s.hits) == 0 {
		return nil
	}
	i := sort.Search(len(s.hits), func(i int) bool {
		h := s.hits[i]
		return h.row > row || (h.row == row && h.side >= side)
	})
	var out []hitSpan
	for ; i < len(s.hits) && s.hits[i].row == row && s.hits[i].side == side; i++ {
		out = append(out, hitSpan{start: s.hits[i].start, end: s.hits[i].end, cur: i == s.cur})
	}
	return out
}

// findHits returns every case-insensitive match of query in lines, in the order
// the lines are given (so the caller controls document order) and left to right
// within a line. Overlapping matches are not reported — the walk advances past
// each hit.
//
// Matching walks RUNES: strings.ToLower + strings.Index would return BYTE
// offsets, and folding can change a string's rune count, either of which puts
// the painted span on the wrong columns.
func findHits(lines []searchLine, query string) []searchHit {
	q := foldRunes(query)
	if len(q) == 0 {
		return nil
	}
	var out []searchHit
	for _, ln := range lines {
		t := foldRunes(ln.text)
		for i := 0; i+len(q) <= len(t); {
			if runesMatchAt(t, q, i) {
				out = append(out, searchHit{row: ln.row, side: ln.side, start: i, end: i + len(q)})
				i += len(q)
				continue
			}
			i++
		}
	}
	return out
}

// foldRunes lowercases s rune by rune — 1:1, so an index into the result is an
// index into s's runes and therefore into the display mask.
func foldRunes(s string) []rune {
	r := []rune(s)
	for i, c := range r {
		r[i] = unicode.ToLower(c)
	}
	return r
}

func runesMatchAt(t, q []rune, i int) bool {
	for k := range q {
		if t[i+k] != q[k] {
			return false
		}
	}
	return true
}

// before reports whether a comes strictly before b in document order.
func (a searchPos) before(b searchPos) bool {
	if a.row != b.row {
		return a.row < b.row
	}
	if a.side != b.side {
		return a.side < b.side
	}
	return a.col < b.col
}

func hitPos(h searchHit) searchPos {
	return searchPos{row: h.row, side: h.side, col: h.start}
}

// nearestHit is the index of the first hit at or after pos (forward) or the
// last at or before it (backward), wrapping around; -1 when there are none.
// This is what the incremental search snaps to after every keystroke, so a hit
// the cursor already sits on stays selected instead of jumping away.
func nearestHit(hits []searchHit, pos searchPos, backward bool) int {
	if len(hits) == 0 {
		return -1
	}
	if backward {
		for i := len(hits) - 1; i >= 0; i-- {
			if !pos.before(hitPos(hits[i])) {
				return i
			}
		}
		return len(hits) - 1
	}
	for i, h := range hits {
		if !hitPos(h).before(pos) {
			return i
		}
	}
	return 0
}

// stepHit is ] and [: the first hit STRICTLY after pos (delta >= 0) or the last
// strictly before it (delta < 0), wrapping around; -1 when there are no hits.
// Strictness is what makes a second ] move off the hit the cursor landed on,
// and it means the stepping keys always walk document order regardless of
// whether the search was opened with / or @.
func stepHit(hits []searchHit, pos searchPos, delta int) int {
	if len(hits) == 0 {
		return -1
	}
	if delta >= 0 {
		for i, h := range hits {
			if pos.before(hitPos(h)) {
				return i
			}
		}
		return 0
	}
	for i := len(hits) - 1; i >= 0; i-- {
		if hitPos(hits[i]).before(pos) {
			return i
		}
	}
	return len(hits) - 1
}

// panFor returns the horizontal offset that brings display columns
// [colStart, colEnd) inside the window [hOffset, hOffset+tw), moving as little
// as possible — a hit already on screen never pans. A hit wider than the window
// shows its head. The columns are DISPLAY columns (lipgloss.Width of the text
// before the hit), never rune indexes: a wide glyph occupies two.
func panFor(hOffset, tw, colStart, colEnd int) int {
	if tw < 1 {
		tw = 1
	}
	if colEnd > colStart+tw {
		colEnd = colStart + tw
	}
	switch {
	case colStart < hOffset:
		hOffset = colStart
	case colEnd > hOffset+tw:
		hOffset = colEnd - tw
	}
	if hOffset < 0 {
		hOffset = 0
	}
	return hOffset
}

// hitCols maps a hit's rune offsets into display columns of its (sanitized)
// line — what panFor wants.
func hitCols(text string, h searchHit) (int, int) {
	r := []rune(text)
	start, end := h.start, h.end
	if start > len(r) {
		start = len(r)
	}
	if end > len(r) {
		end = len(r)
	}
	return lipgloss.Width(string(r[:start])), lipgloss.Width(string(r[:end]))
}

// searchEvent is what a host must do after one of the shared key handlers has
// looked at a key.
type searchEvent int

const (
	searchNotOurs   searchEvent = iota // not a search key: run the host's own handling
	searchIgnored                      // swallowed; the host does nothing
	searchChanged                      // the query changed: re-find and re-snap from the origin
	searchCommitted                    // enter: the query stays, typing stops (re-find: recall may have replaced it)
	searchCancelled                    // esc while typing: restore the view origin, no query left
	searchCleared                      // esc on a committed query: drop query + hits, stay in the view
	searchOpenFwd                      // /
	searchOpenBack                     // @
	searchNext                         // ]
	searchPrev                         // [
)

// searchTypingKey handles one key while s is capturing text. History recall
// runs FIRST (alt+↑/↓ means "scroll the view" in two of the hosts, so it may
// only mean "recall" inside the typing branch), then the small fixed vocabulary
// of an inline gg search field: esc, enter, backspace, space, runes. Every
// other key is swallowed — while typing, j is query text, not motion.
func (m Model) searchTypingKey(s *textSearch, msg tea.KeyMsg) (Model, tea.Cmd, searchEvent) {
	if !s.typing {
		return m, nil, searchNotOurs
	}
	if nm, nq, handled, commit := m.recallUpdate(scopeInView, msg, s.query); handled {
		m = nm
		s.query = nq
		if commit { // enter accepted a ring entry
			s.typing = false
			rm, cmd := m.recordSearch(scopeInView, s.query)
			return rm, cmd, searchCommitted
		}
		// A previewed ring entry (or esc closing the dropdown and restoring the
		// draft) changed the displayed query: re-find, do not cancel.
		return m, nil, searchChanged
	} else {
		m = nm
	}
	switch msg.Type {
	case tea.KeyEsc:
		s.clear()
		return m, nil, searchCancelled
	case tea.KeyEnter:
		s.typing = false
		if s.query == "" { // nothing typed: record nothing, leave no query active
			s.clear()
			return m, nil, searchCancelled
		}
		rm, cmd := m.recordSearch(scopeInView, s.query)
		return rm, cmd, searchCommitted
	case tea.KeyBackspace, tea.KeyCtrlH, tea.KeyDelete:
		if r := []rune(s.query); len(r) > 0 {
			s.query = string(r[:len(r)-1])
		}
		return m, nil, searchChanged
	case tea.KeySpace:
		s.query += " "
		return m, nil, searchChanged
	case tea.KeyRunes:
		s.query += string(msg.Runes)
		return m, nil, searchChanged
	}
	return m, nil, searchIgnored
}

// searchCommandKey handles the search keys a host sees while NOT typing. esc is
// claimed only when a query is live, so a host's own esc (close the view) keeps
// working; ] and [ are inert without one.
func searchCommandKey(s *textSearch, msg tea.KeyMsg) searchEvent {
	switch msg.String() {
	case "/":
		return searchOpenFwd
	case "@":
		return searchOpenBack
	case "]":
		if s.query == "" {
			return searchIgnored
		}
		return searchNext
	case "[":
		if s.query == "" {
			return searchIgnored
		}
		return searchPrev
	case "esc":
		if s.query != "" {
			return searchCleared
		}
	}
	return searchNotOurs
}
