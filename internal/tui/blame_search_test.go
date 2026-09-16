package tui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/homeend/gigagit/internal/model"
)

// NOTE: TestBlameSearchPaintsTheHit calls lipgloss.SetColorProfile
// (process-global) and therefore does NOT call t.Parallel().

func blameSearchModel() (Model, *blameView) {
	m := Model{width: 100, height: 30}
	b := blameFixture()
	m = m.pushLayer(b)
	return m, b
}

func typeBlame(m Model, b *blameView, keys ...string) Model {
	for _, k := range keys {
		m, _ = b.update(m, keyMsg(k))
	}
	return m
}

func TestBlameSearchMovesTheLineCursor(t *testing.T) {
	t.Parallel()
	m, b := blameSearchModel()
	b.sel = 2
	m = typeBlame(m, b, "/", "m", "a", "i", "n")
	if len(b.search.hits) != 2 {
		t.Fatalf("hits = %v, want 2", b.search.hits)
	}
	// From line 2 the forward search wraps to the first hit.
	if b.sel != 0 {
		t.Fatalf("sel = %d, want 0 (wrapped)", b.sel)
	}
	m = typeBlame(m, b, "enter", "]")
	if b.sel != 1 || b.search.cur != 1 {
		t.Fatalf("] = sel %d cur %d, want 1/1", b.sel, b.search.cur)
	}
}

func TestBlameSearchBackwardOpensWithAt(t *testing.T) {
	t.Parallel()
	m, b := blameSearchModel()
	b.sel = 2
	_ = typeBlame(m, b, "@", "m", "a", "i", "n")
	if !b.search.backward {
		t.Fatal("@ must open a backward search")
	}
	if b.sel != 1 {
		t.Fatalf("backward from line 2 must land on the last hit at/before it: sel = %d, want 1", b.sel)
	}
}

func TestBlameSearchEscIsTwoStageButBIsNot(t *testing.T) {
	t.Parallel()
	m, b := blameSearchModel()
	b.sel = 0
	m = typeBlame(m, b, "/", "m", "a", "i", "n", "enter")
	m, _ = b.update(m, keyMsg("esc"))
	if layerOf[*blameView](m) == nil {
		t.Fatal("the first esc must only clear the search")
	}
	if b.search.active() {
		t.Fatalf("the first esc must clear the query: %+v", b.search)
	}
	m, _ = b.update(m, keyMsg("esc"))
	if layerOf[*blameView](m) != nil {
		t.Fatal("the second esc must close the view")
	}

	// b closes even with a live query — only esc is two-stage.
	m, b = blameSearchModel()
	m = typeBlame(m, b, "/", "m", "enter")
	m, _ = b.update(m, keyMsg("b"))
	if layerOf[*blameView](m) != nil {
		t.Fatal("b must close the view regardless of the search")
	}
}

// TestBlameSearchEscWhileTypingRestoresTheOrigin uses a fixture whose only
// match sits far past a narrow pane's right edge, in modeScroll: this forces
// goToHit to actually pan b.hscroll away from its origin while typing, so the
// esc-restores-the-origin assertion below is not vacuously true (a hit that
// never needed a pan would leave b.hscroll unchanged whether or not the
// restore code ran at all).
func TestBlameSearchEscWhileTypingRestoresTheOrigin(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("x", 100) + "main" + strings.Repeat("y", 20)
	b := &blameView{
		ctx: navContext{path: "a.go", rev: ""},
		lines: []model.BlameLine{
			{Hash: "aaaaaaa", Author: "Ada", Time: 1, LineNo: 1, Content: "package foo"},
			{Hash: "aaaaaaa", Author: "Ada", Time: 1, LineNo: 2, Content: long},
			{Hash: "", Author: "Not Committed Yet", LineNo: 3, Content: "dirty"},
		},
		mode: modeScroll,
	}
	b.blocks = groupBlame(b.lines)
	m := Model{width: 60, height: 30}
	m = m.pushLayer(b)

	b.sel, b.hscroll = 2, 7
	m = typeBlame(m, b, "/", "m", "a", "i", "n")
	if b.hscroll == 7 {
		t.Fatal("fixture is unsound — the incremental search never panned, so the hscroll assertion below is vacuous")
	}
	_ = typeBlame(m, b, "esc")
	if b.sel != 2 || b.hscroll != 7 {
		t.Fatalf("esc must restore sel/hscroll: %d/%d, want 2/7", b.sel, b.hscroll)
	}
	if b.search.active() {
		t.Fatalf("esc must leave no search: %+v", b.search)
	}
}

// TestBlameSearchPansTheHitIntoView covers goToHit's modeScroll branch: a hit
// past the pane width must pull b.hscroll to reveal it. The expectation is
// computed from panFor/hitCols themselves (the same arithmetic goToHit uses),
// never a hardcoded column number, so the test tracks the production formula
// instead of freezing one of its outputs.
func TestBlameSearchPansTheHitIntoView(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("x", 100) + "needle" + strings.Repeat("y", 20)
	b := &blameView{
		ctx:   navContext{path: "a.go", rev: ""},
		lines: []model.BlameLine{{Hash: "aaaaaaa", Author: "Ada", Time: 1, LineNo: 1, Content: long}},
		mode:  modeScroll,
	}
	b.blocks = groupBlame(b.lines)
	m := Model{width: 60, height: 30}
	m = m.pushLayer(b)

	b.search.query = "needle"
	b.search.hits = findHits(b.searchLines(), "needle")
	if len(b.search.hits) != 1 {
		t.Fatalf("hits = %v, want 1", b.search.hits)
	}

	w, _ := m.overlayDims()
	gw := blameGutterW
	if gw > w-10 {
		gw = w - 10
	}
	tw := w - (gw + 1)
	cs, ce := hitCols(b.searchText(0), b.search.hits[0])
	if ce <= tw {
		t.Fatalf("fixture is unsound — the hit at columns [%d,%d) already fits inside [0,%d)", cs, ce, tw)
	}

	b.goToHit(m, 0)

	want := panFor(0, tw, cs, ce)
	if b.hscroll != want {
		t.Fatalf("hscroll = %d, want %d (panFor(tw=%d, cs=%d, ce=%d))", b.hscroll, want, tw, cs, ce)
	}
}

func TestBlameSearchBadgeAndHint(t *testing.T) {
	t.Parallel()
	m, b := blameSearchModel()
	m = typeBlame(m, b, "/", "m", "a", "i", "n", "enter")
	out := ansi.Strip(b.render(m, ""))
	head := strings.Split(out, "\n")[0]
	if !strings.Contains(head, "/main  1/2") {
		t.Fatalf("header must carry the badge: %q", head)
	}
	if lipgloss.Width(head) > 100 {
		t.Fatalf("header is %d columns wide", lipgloss.Width(head))
	}
	if !strings.Contains(out, "[/] find") {
		t.Fatalf("the hint must advertise the search:\n%s", out)
	}
}

// TestBlameSearchPaintsTheHit asserts, per body row (skipping the header,
// which alone could make painted != plain even with emph nil throughout):
// every row's TEXT is unchanged (ansi.Strip equal — a hit must never move a
// character), a non-hit row's raw (styled) output is BYTE-IDENTICAL to
// plain, and a hit row carries the search-emphasis colour (shared by
// st().diffEmph and st().searchCur, which only adds underline) at EXACTLY
// the hit's own display-column range — checked with ansi.Cut, which slices a
// styled string by column without disturbing its escape codes — and nowhere
// else on the same row. Two selRow cases run: 0 puts the cursor ON hit row 0,
// proving emphasis survives the reversed selectedRow style, and 2 (the
// "dirty" line, no hit) leaves both hits on non-selected rows, proving the
// ordinary non-reversed painted path.
func TestBlameSearchPaintsTheHit(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(prev)

	// The foreground colour every search hit wears, current or not — derived
	// at runtime (never hardcoded) and absent everywhere else in this
	// fixture's plain render (no syntax highlighting, no other styling that
	// sets a foreground colour).
	fgRe := regexp.MustCompile(`38;5;\d+`)
	marker := fgRe.FindString(lipgloss.NewStyle().Inherit(st().diffEmph).Render("x"))
	if marker == "" {
		t.Fatal("could not derive the search-emphasis colour marker from st().diffEmph")
	}

	m := Model{width: 100, height: 30}
	body := m.blameBodyRows()
	gw := blameGutterW
	prefixW := gw + 1

	for _, selRow := range []int{0, 2} {
		b := blameFixture()
		b.sel = selRow // fixed before "plain" too: search must be the only delta
		plain := b.render(m, "")

		b.search.query = "main"
		b.search.hits = findHits(b.searchLines(), "main")
		if len(b.search.hits) != 2 {
			t.Fatalf("selRow %d: hits = %v, want 2", selRow, b.search.hits)
		}
		b.search.cur = 0

		painted := b.render(m, "")

		plainLines := strings.Split(plain, "\n")
		paintedLines := strings.Split(painted, "\n")
		if len(plainLines) != len(paintedLines) {
			t.Fatalf("selRow %d: row count changed: plain %d painted %d", selRow, len(plainLines), len(paintedLines))
		}

		hitsByRow := map[int]searchHit{}
		for _, h := range b.search.hits {
			hitsByRow[h.row] = h
		}

		// Body rows occupy lines [1, 1+body) of the joined render: line 0 is
		// the header (which carries the badge and must be skipped here), and
		// line 1+body is the hint.
		for i := 0; i < body; i++ {
			idx := i + 1
			p, w := plainLines[idx], paintedLines[idx]
			if ansi.Strip(w) != ansi.Strip(p) {
				t.Errorf("selRow %d row %d: a hit changed the TEXT, not just the styling:\nplain:   %q\npainted: %q", selRow, i, ansi.Strip(p), ansi.Strip(w))
			}
			h, isHit := hitsByRow[i]
			if !isHit {
				if w != p {
					t.Errorf("selRow %d row %d: a non-hit row's styling changed:\nplain:   %q\npainted: %q", selRow, i, p, w)
				}
				continue
			}
			if w == p {
				t.Errorf("selRow %d row %d: the hit did not change the render", selRow, i)
			}
			cs, ce := hitCols(b.searchText(i), h)
			cs, ce = prefixW+cs, prefixW+ce
			wantText := string([]rune(b.searchText(i))[h.start:h.end])
			hitSlice := ansi.Cut(w, cs, ce)
			if got := ansi.Strip(hitSlice); got != wantText {
				t.Errorf("selRow %d row %d: columns [%d,%d) hold %q, want the hit text %q", selRow, i, cs, ce, got, wantText)
			}
			if !strings.Contains(hitSlice, marker) {
				t.Errorf("selRow %d row %d: no search styling at the hit's own columns [%d,%d):\nslice: %q\nfull:  %q", selRow, i, cs, ce, hitSlice, w)
			}
			if got := ansi.Cut(p, cs, ce); strings.Contains(got, marker) {
				t.Errorf("selRow %d row %d: fixture is unsound — the PLAIN render already carries the marker at [%d,%d)", selRow, i, cs, ce)
			}
		}
	}
}
