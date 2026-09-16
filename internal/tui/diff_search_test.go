package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/homeend/gigagit/internal/textdiff"
)

// NOTE: TestDiffSearchPaintsTheHit calls lipgloss.SetColorProfile, which is
// process-global, so it does NOT call t.Parallel() (see window_syntax_test.go).

// searchDiffRows: a tiny file whose text is worth searching. "alpha" occurs on
// the LEFT of the changed row (line index 2) and on both sides of the unchanged
// row (line index 3), which is searched on the right side only.
func searchDiffRows() []textdiff.Row {
	return []textdiff.Row{
		{Kind: textdiff.Same, Left: "package main", Right: "package main", LeftNo: 1, RightNo: 1},
		{Kind: textdiff.Same, Left: "", Right: "", LeftNo: 2, RightNo: 2},
		{Kind: textdiff.Changed, Left: "func alpha() {", Right: "func beta() {", LeftNo: 3, RightNo: 3},
		{Kind: textdiff.Same, Left: "\tprintln(\"alpha\")", Right: "\tprintln(\"alpha\")", LeftNo: 4, RightNo: 4},
		{Kind: textdiff.Same, Left: "}", Right: "}", LeftNo: 5, RightNo: 5},
	}
}

func searchDiffModel() Model {
	m := diffModel()
	m.width, m.height = 100, 20
	v := diffViewWith(searchDiffRows(), []int{2})
	v.relayout(m.width)
	m = m.pushLayer(v)
	m.diffTag = "status:x"
	return m
}

// typeKeys feeds one key per token ("/" then each rune of a query).
func typeKeys(m Model, keys ...string) Model {
	for _, k := range keys {
		u, _ := m.Update(keyMsg(k))
		m = u.(Model)
	}
	return m
}

func TestDiffSearchIsIncrementalFromTheCursor(t *testing.T) {
	t.Parallel()
	m := searchDiffModel()
	m = typeKeys(m, "/", "a", "l", "p", "h", "a")
	v := m.diffLayer()
	if !v.search.typing || v.search.query != "alpha" {
		t.Fatalf("typing state = %+v", v.search)
	}
	if len(v.search.hits) != 2 {
		t.Fatalf("hits = %v, want 2 (left of the changed row, right of the same row)", v.search.hits)
	}
	if v.search.hits[0].row != 2 || v.search.hits[0].side != 0 {
		t.Fatalf("first hit = %+v, want row 2 side 0", v.search.hits[0])
	}
	if v.search.hits[1].row != 3 || v.search.hits[1].side != 1 {
		t.Fatalf("second hit = %+v, want row 3 side 1 (a Same row is searched on the right only)", v.search.hits[1])
	}
	if v.curLine != 2 {
		t.Fatalf("the cursor must follow the incremental search: curLine = %d, want 2", v.curLine)
	}
	// enter keeps the query and stops capturing keys.
	u, _ := m.Update(keyMsg("enter"))
	v = u.(Model).diffLayer()
	if v.search.typing || v.search.query != "alpha" {
		t.Fatalf("after enter: %+v", v.search)
	}
}

func TestDiffSearchEscWhileTypingRestoresTheOrigin(t *testing.T) {
	t.Parallel()
	m := searchDiffModel()
	v := m.diffLayer()
	v.curLine = 4 // below both hits: the forward search wraps up to the first
	m = typeKeys(m, "/", "a", "l", "p", "h", "a")
	if v.curLine != 2 {
		t.Fatalf("the incremental search must have moved the cursor, curLine = %d", v.curLine)
	}
	u, _ := m.Update(keyMsg("esc"))
	v = u.(Model).diffLayer()
	// (offset is not asserted: this fixture is 5 rows in an 18-row body, so
	// scroll() legitimately clamps every offset to 0.)
	if v.curLine != 4 {
		t.Fatalf("esc must restore the origin: curLine = %d, want 4", v.curLine)
	}
	if v.search.active() || v.search.hits != nil {
		t.Fatalf("esc must leave no search: %+v", v.search)
	}
	if u.(Model).diffLayer() == nil {
		t.Fatal("esc while typing must not close the view")
	}
}

func TestDiffSearchEscIsTwoStage(t *testing.T) {
	t.Parallel()
	m := searchDiffModel()
	m = typeKeys(m, "/", "a", "l", "p", "h", "a", "enter")
	u, _ := m.Update(keyMsg("esc")) // first esc clears the query
	mm := u.(Model)
	if mm.diffLayer() == nil {
		t.Fatal("the first esc must not close the view")
	}
	if mm.diffLayer().search.active() {
		t.Fatalf("the first esc must clear the query: %+v", mm.diffLayer().search)
	}
	u, _ = mm.Update(keyMsg("esc")) // second esc closes
	if u.(Model).diffLayer() != nil {
		t.Fatal("the second esc must close the view")
	}
}

func TestDiffSearchStepsAndWraps(t *testing.T) {
	t.Parallel()
	m := searchDiffModel()
	m = typeKeys(m, "/", "a", "l", "p", "h", "a", "enter")
	u, _ := m.Update(keyMsg("]"))
	v := u.(Model).diffLayer()
	if v.search.cur != 1 || v.curLine != 3 {
		t.Fatalf("] = hit %d at line %d, want 1 at 3", v.search.cur, v.curLine)
	}
	u, _ = u.(Model).Update(keyMsg("]")) // past the end: wrap
	v = u.(Model).diffLayer()
	if v.search.cur != 0 || v.curLine != 2 {
		t.Fatalf("] must wrap to hit 0 at line 2, got %d at %d", v.search.cur, v.curLine)
	}
	u, _ = u.(Model).Update(keyMsg("[")) // back off the start: wrap the other way
	v = u.(Model).diffLayer()
	if v.search.cur != 1 {
		t.Fatalf("[ must wrap to the last hit, got %d", v.search.cur)
	}
}

func TestDiffSearchBracketsAreInertWithoutAQuery(t *testing.T) {
	t.Parallel()
	m := searchDiffModel()
	before := m.diffLayer().curLine
	u, _ := m.Update(keyMsg("]"))
	if got := u.(Model).diffLayer().curLine; got != before {
		t.Fatalf("] with no query moved the cursor to %d", got)
	}
}

func TestDiffSearchBadgeIsOnTheHeader(t *testing.T) {
	t.Parallel()
	m := searchDiffModel()
	m = typeKeys(m, "/", "a", "l", "p", "h", "a", "enter")
	head := strings.Split(ansi.Strip(m.renderDiffView()), "\n")[0]
	if !strings.Contains(head, "/alpha  1/2") {
		t.Fatalf("header must carry the badge: %q", head)
	}
	if lipgloss.Width(head) > m.width {
		t.Fatalf("header is %d columns, terminal is %d", lipgloss.Width(head), m.width)
	}
}

func TestDiffSearchPaintsTheHit(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(prev)

	plain := searchDiffModel().renderDiffView()
	m := typeKeys(searchDiffModel(), "/", "a", "l", "p", "h", "a", "enter")
	painted := m.renderDiffView()
	if painted == plain {
		t.Fatal("an active search must change the rendered body")
	}
	if ansi.Strip(strings.Split(painted, "\n")[3]) == "" {
		t.Fatal("the body must still render")
	}
	// Every mode paints; wrap must not need a relayout to do it.
	for _, lm := range []longMode{longWrap, longTruncate, longScroll} {
		mm := searchDiffModel()
		mm.diffLayer().long = lm
		mm.diffLayer().relayout(mm.width)
		before := mm.renderDiffView()
		mm = typeKeys(mm, "/", "a", "l", "p", "h", "a", "enter")
		if mm.renderDiffView() == before {
			t.Errorf("mode %d did not paint the hit", lm)
		}
	}
}

func TestDiffSearchRefindsAfterAModeToggle(t *testing.T) {
	t.Parallel()
	m := searchDiffModel()
	m = typeKeys(m, "/", "a", "l", "p", "h", "a", "enter")
	u, _ := m.Update(keyMsg("f")) // toggle partial: v.lines is rebuilt
	v := u.(Model).diffLayer()
	if v.search.query != "alpha" {
		t.Fatalf("the query must survive a rebuild: %+v", v.search)
	}
	for _, h := range v.search.hits {
		if h.row >= len(v.lines) {
			t.Fatalf("hit row %d is outside the rebuilt %d lines", h.row, len(v.lines))
		}
	}
}

func TestDiffHintFitsTheBudget(t *testing.T) {
	t.Parallel()
	for _, lm := range []longMode{longScroll, longWrap, longTruncate} {
		if w := lipgloss.Width(diffHintFor(lm)); w > 140 {
			t.Errorf("mode %d hint is %d columns, the budget is 140: %q", lm, w, diffHintFor(lm))
		}
		if !strings.Contains(diffHintFor(lm), "[/] find") {
			t.Errorf("mode %d hint must advertise the search: %q", lm, diffHintFor(lm))
		}
	}
	// The widest variant is at the cap: any new group must shorten a label
	// first. Update this number and diffHintFor's doc comment together.
	if w := lipgloss.Width(diffHintFor(longScroll)); w != 140 {
		t.Fatalf("the scroll variant measures %d columns; the doc comment says 140", w)
	}
}
