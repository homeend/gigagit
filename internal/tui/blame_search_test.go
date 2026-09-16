package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

// NOTE: TestBlameSearchPaintsTheSelectedRow calls lipgloss.SetColorProfile
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

func TestBlameSearchEscWhileTypingRestoresTheOrigin(t *testing.T) {
	t.Parallel()
	m, b := blameSearchModel()
	b.sel, b.hscroll = 2, 7
	m = typeBlame(m, b, "/", "m", "a", "i", "n")
	_ = typeBlame(m, b, "esc")
	if b.sel != 2 || b.hscroll != 7 {
		t.Fatalf("esc must restore sel/hscroll: %d/%d, want 2/7", b.sel, b.hscroll)
	}
	if b.search.active() {
		t.Fatalf("esc must leave no search: %+v", b.search)
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

func TestBlameSearchPaintsTheSelectedRow(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(prev)

	m, b := blameSearchModel()
	plain := b.render(m, "")
	m = typeBlame(m, b, "/", "m", "a", "i", "n", "enter")
	painted := b.render(m, "")
	if painted == plain {
		t.Fatal("an active search must change the render")
	}
	// The cursor row is reverse video and is exactly where the current hit is:
	// the visible text must be unchanged, only the styling.
	if ansi.Strip(strings.Split(painted, "\n")[1]) != ansi.Strip(strings.Split(plain, "\n")[1]) {
		t.Fatal("the hit changed the visible text of the selected row")
	}
}
