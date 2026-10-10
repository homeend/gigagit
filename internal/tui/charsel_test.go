package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// rowsOf builds host rows from strings: "~text" is a wrap continuation,
// "!" alone is a dead row.
func rowsOf(ss ...string) []charRow {
	out := make([]charRow, len(ss))
	for i, s := range ss {
		switch {
		case s == "!":
			out[i] = charRow{dead: true}
		case strings.HasPrefix(s, "~"):
			out[i] = charRow{text: []rune(s[1:]), wraps: true}
		default:
			out[i] = charRow{text: []rune(s)}
		}
	}
	return out
}

type fakeHost struct {
	rows []charRow
	page int
}

func (h fakeHost) charRows() []charRow { return h.rows }
func (h fakeHost) charPage() int       { return h.page }

func feedSel(cs *textSel, h charHost, keys ...string) (last charSelResult) {
	for _, k := range keys {
		last = charSelKey(cs, h, keyMsg(k))
	}
	return last
}

// enter lands on the row asked for when it is live, else on the next live
// one; a host with no live row refuses.
func TestCharSelEnterLandsOnALiveRow(t *testing.T) {
	t.Parallel()
	rows := rowsOf("!", "!", "abc", "de")
	var cs textSel
	if !cs.enter(rows, pos{0, 0}) || cs.cur != (pos{2, 0}) || !cs.on || cs.fixed {
		t.Fatalf("enter = %+v", cs)
	}
	var none textSel
	if none.enter(rowsOf("!", "!"), pos{0, 0}) || none.on {
		t.Fatal("a host with no live row must refuse")
	}
	var empty textSel
	if empty.enter(nil, pos{0, 0}) {
		t.Fatal("no rows at all must refuse")
	}
}

// ←/→ move by rune and run across rows; dead rows are stepped over; the
// ends clamp (bump reports a move that went nowhere).
func TestCharSelLeftRightRunAcrossRows(t *testing.T) {
	t.Parallel()
	h := fakeHost{rows: rowsOf("ab", "!", "cd"), page: 10}
	var cs textSel
	cs.enter(h.rows, pos{0, 1})
	if r := feedSel(&cs, h, "right"); cs.cur != (pos{2, 0}) || r.bump {
		t.Fatalf("right past the end of row 0 = %+v (%+v)", cs.cur, r)
	}
	if r := feedSel(&cs, h, "left"); cs.cur != (pos{0, 1}) || r.bump {
		t.Fatalf("left past the start of row 2 = %+v (%+v)", cs.cur, r)
	}
	feedSel(&cs, h, "h")
	if r := feedSel(&cs, h, "h"); cs.cur != (pos{0, 0}) || !r.bump {
		t.Fatalf("left at the very start must bump: %+v (%+v)", cs.cur, r)
	}
	feedSel(&cs, h, "l", "l", "l")
	if r := feedSel(&cs, h, "l"); cs.cur != (pos{2, 1}) || !r.bump {
		t.Fatalf("right at the very end must bump: %+v (%+v)", cs.cur, r)
	}
}

// ↑/↓ keep the wanted column across a shorter row; home/end/0/$ hit the
// row's ends; pgup/pgdn move a page.
func TestCharSelUpDownKeepTheColumn(t *testing.T) {
	t.Parallel()
	h := fakeHost{rows: rowsOf("abcdef", "xy", "123456", "", "qwerty"), page: 2}
	var cs textSel
	cs.enter(h.rows, pos{0, 4})
	feedSel(&cs, h, "down")
	if cs.cur != (pos{1, 1}) {
		t.Fatalf("down onto a short row = %+v, want clamped to its last rune", cs.cur)
	}
	feedSel(&cs, h, "j")
	if cs.cur != (pos{2, 4}) {
		t.Fatalf("down again = %+v, want the wanted column 4 back", cs.cur)
	}
	feedSel(&cs, h, "down")
	if cs.cur != (pos{3, 0}) {
		t.Fatalf("down onto an empty row = %+v, want col 0", cs.cur)
	}
	feedSel(&cs, h, "up", "up", "up")
	if cs.cur != (pos{0, 4}) {
		t.Fatalf("back up = %+v", cs.cur)
	}
	feedSel(&cs, h, "end")
	if cs.cur != (pos{0, 5}) {
		t.Fatalf("end = %+v", cs.cur)
	}
	feedSel(&cs, h, "0")
	if cs.cur != (pos{0, 0}) {
		t.Fatalf("0 = %+v", cs.cur)
	}
	feedSel(&cs, h, "$")
	if cs.cur != (pos{0, 5}) {
		t.Fatalf("$ = %+v", cs.cur)
	}
	feedSel(&cs, h, "home")
	feedSel(&cs, h, "pgdown")
	if cs.cur != (pos{2, 0}) {
		t.Fatalf("pgdn (page 2) = %+v", cs.cur)
	}
	feedSel(&cs, h, "pgup")
	if cs.cur != (pos{0, 0}) {
		t.Fatalf("pgup = %+v", cs.cur)
	}
}

// w/b step by word start, across rows (a row boundary reads as a space).
func TestCharSelWordSteps(t *testing.T) {
	t.Parallel()
	h := fakeHost{rows: rowsOf("ab  cd", "ef"), page: 10}
	var cs textSel
	cs.enter(h.rows, pos{0, 0})
	feedSel(&cs, h, "w")
	if cs.cur != (pos{0, 4}) {
		t.Fatalf("w = %+v, want cd", cs.cur)
	}
	feedSel(&cs, h, "w")
	if cs.cur != (pos{1, 0}) {
		t.Fatalf("w across rows = %+v, want ef", cs.cur)
	}
	if r := feedSel(&cs, h, "w"); cs.cur != (pos{1, 0}) || !r.bump {
		t.Fatalf("w at the last word must bump: %+v", cs.cur)
	}
	feedSel(&cs, h, "b")
	if cs.cur != (pos{0, 4}) {
		t.Fatalf("b = %+v, want cd", cs.cur)
	}
	feedSel(&cs, h, "l", "b")
	if cs.cur != (pos{0, 4}) {
		t.Fatalf("b from inside a word = %+v, want its start", cs.cur)
	}
	feedSel(&cs, h, "b")
	if cs.cur != (pos{0, 0}) {
		t.Fatalf("b = %+v, want ab", cs.cur)
	}
}

// space fixes the start, a second space restarts there; enter copies the
// bounds in reading order; esc drops the start, then leaves.
func TestCharSelSpaceEnterEsc(t *testing.T) {
	t.Parallel()
	h := fakeHost{rows: rowsOf("hello world", "~second row", "third"), page: 10}
	var cs textSel
	cs.enter(h.rows, pos{0, 6})
	if r := feedSel(&cs, h, "enter"); r.copy != "" || r.notice == "" || !cs.on {
		t.Fatalf("enter without a start must say so and stay: %+v", r)
	}
	feedSel(&cs, h, "space")
	if !cs.fixed || cs.anchor != (pos{0, 6}) {
		t.Fatalf("space = %+v", cs)
	}
	feedSel(&cs, h, "down", "end")
	if lo, hi, ok := cs.bounds(); !ok || lo != (pos{0, 6}) || hi != (pos{1, 9}) {
		t.Fatalf("bounds = %+v %+v %v", lo, hi, ok)
	}
	// Backwards: the range reads anchor..cur in reading order either way.
	cs2 := cs
	cs2.cur = pos{0, 2}
	if lo, hi, _ := cs2.bounds(); lo != (pos{0, 2}) || hi != (pos{0, 6}) {
		t.Fatalf("backward bounds = %+v %+v", lo, hi)
	}
	feedSel(&cs, h, "space") // restart at the cursor
	if cs.anchor != (pos{1, 9}) || !cs.fixed {
		t.Fatalf("second space must restart at the cursor: %+v", cs)
	}
	feedSel(&cs, h, "j") // onto "third", column clamped to its last rune
	r := feedSel(&cs, h, "enter")
	if r.copy != "w\nthird" || cs.on {
		t.Fatalf("enter copied %q (on=%v), want the covered text across the row break", r.copy, cs.on)
	}
	cs.enter(h.rows, pos{0, 0})
	feedSel(&cs, h, "space", "l")
	if left := cs.esc(); left || cs.fixed || !cs.on {
		t.Fatalf("first esc drops the start only: left=%v %+v", left, cs)
	}
	if left := cs.esc(); !left || cs.on {
		t.Fatalf("second esc leaves: left=%v %+v", left, cs)
	}
	cs.enter(h.rows, pos{0, 0})
	if r := feedSel(&cs, h, "space", "l", "l", "y"); r.copy != "hel" || cs.on {
		t.Fatalf("y is enter: %q", r.copy)
	}
}

// The copy joins a wrap continuation with one space and any other row with
// a newline, skips dead rows, keeps an empty row as a blank line, and a
// one-rune selection copies that rune.
func TestCharSelTextJoins(t *testing.T) {
	t.Parallel()
	rows := rowsOf("one two", "~three", "!", "", "five")
	if got := charSelText(rows, pos{0, 4}, pos{4, 1}); got != "two three\n\nfi" {
		t.Fatalf("got %q", got)
	}
	if got := charSelText(rows, pos{0, 2}, pos{0, 2}); got != "e" {
		t.Fatalf("one rune = %q", got)
	}
	if got := charSelText(rows, pos{1, 0}, pos{1, 4}); got != "three" {
		t.Fatalf("a continuation alone joins nothing in front: %q", got)
	}
}

// Review Focus 1: runes, never bytes or columns — a wide rune is one step
// and copies whole.
func TestCharSelCopyKeepsWideRunes(t *testing.T) {
	t.Parallel()
	h := fakeHost{rows: rowsOf("a漢字b"), page: 10}
	var cs textSel
	cs.enter(h.rows, pos{0, 0})
	r := feedSel(&cs, h, "l", "space", "l", "enter")
	if r.copy != "漢字" {
		t.Fatalf("copy = %q", r.copy)
	}
	if n := (textSel{on: true, fixed: true, anchor: pos{0, 1}, cur: pos{0, 2}}).count(h.rows); n != 2 {
		t.Fatalf("count = %d", n)
	}
}

// Every key is the mode's while on: an unknown one is handled and inert.
func TestCharSelIsModal(t *testing.T) {
	t.Parallel()
	h := fakeHost{rows: rowsOf("abc"), page: 10}
	var cs textSel
	cs.enter(h.rows, pos{0, 1})
	for _, k := range []string{"n", "/", "v", "ctrl+w", "alt+left", "S", "q"} {
		r := charSelKey(&cs, h, keyMsg(k))
		if !r.handled || r.copy != "" || cs.cur != (pos{0, 1}) || !cs.on {
			t.Fatalf("%s: %+v %+v", k, r, cs)
		}
	}
	if r := charSelKey(&cs, h, tea.KeyMsg{Type: tea.KeyCtrlC}); r.handled {
		t.Fatal("ctrl+c is never the mode's (the host quits)")
	}
}

// The emphasis mask: the covered runes wear emphSel, the cursor emphSelCur,
// an untouched row has no mask; shiftMask moves it behind a lead.
func TestCharSelEmphMask(t *testing.T) {
	t.Parallel()
	cs := textSel{on: true, fixed: true, anchor: pos{0, 1}, cur: pos{1, 1}}
	if m := charSelEmph(cs, 0, 4); !equalEmph(m, []emphLevel{emphNone, emphSel, emphSel, emphSel}) {
		t.Fatalf("row 0 = %v", m)
	}
	if m := charSelEmph(cs, 1, 3); !equalEmph(m, []emphLevel{emphSel, emphSelCur, emphNone}) {
		t.Fatalf("row 1 = %v", m)
	}
	if m := charSelEmph(cs, 2, 3); m != nil {
		t.Fatalf("row 2 = %v, want nil", m)
	}
	loose := textSel{on: true, cur: pos{0, 2}}
	if m := charSelEmph(loose, 0, 3); !equalEmph(m, []emphLevel{emphNone, emphNone, emphSelCur}) {
		t.Fatalf("not fixed = %v, want the cursor only", m)
	}
	if m := shiftMask([]emphLevel{emphSel, emphSelCur}, 2, 5); !equalEmph(m, []emphLevel{emphNone, emphNone, emphSel, emphSelCur, emphNone}) {
		t.Fatalf("shift = %v", m)
	}
	if sp := charSelSpans(cs, 1); len(sp) != 2 || sp[0] != (hitSpan{start: 0, end: 2, sel: true}) || sp[1] != (hitSpan{start: 1, end: 2, sel: true, cur: true}) {
		t.Fatalf("spans = %+v", sp)
	}
}

func equalEmph(a, b []emphLevel) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// The hint names the stage; the copy line counts runes.
func TestCharSelHintAndCopiedText(t *testing.T) {
	t.Parallel()
	rows := rowsOf("abcdef")
	if h := charSelHint(textSel{on: true, cur: pos{0, 0}}, rows); !strings.Contains(h, "[space] start") {
		t.Fatalf("loose hint = %q", h)
	}
	if h := charSelHint(textSel{on: true, fixed: true, anchor: pos{0, 0}, cur: pos{0, 2}}, rows); !strings.Contains(h, "3 chars") {
		t.Fatalf("fixed hint = %q", h)
	}
	if copiedCharsText(1) != "Copied 1 character" || copiedCharsText(3) != "Copied 3 characters" {
		t.Fatalf("%q / %q", copiedCharsText(1), copiedCharsText(3))
	}
}
