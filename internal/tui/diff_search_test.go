package tui

import (
	"fmt"
	"regexp"
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

// TestDiffSearchSurvivesTheLoadArrival covers the case a search is opened
// while the diff is still loading (pushed with loading: true, no content
// yet): the diff view routes / to diffSearchKey before any loading guard, so
// the query commits with a live search on dv. The old diffMsg handler
// overwrote the whole view (*dv = *msg.view), wiping search and searchOrig
// when the real content landed. The fix saves and restores them across the
// overwrite and re-finds, mirroring blameMsg/fileContentMsg.
func TestDiffSearchSurvivesTheLoadArrival(t *testing.T) {
	t.Parallel()
	m := diffModel()
	m.width, m.height = 100, 20
	m = m.pushLayer(&diffView{loading: true})
	m.diffTag = "status:x"

	// Start a search for a needle that cannot match the (empty) loading view.
	m = typeKeys(m, "/", "n", "e", "e", "d", "l", "e")
	if len(m.diffLayer().search.hits) != 0 {
		t.Fatalf("the empty loading view must not match: hits = %v", m.diffLayer().search.hits)
	}

	rows := []textdiff.Row{
		{Kind: textdiff.Same, Left: "package main", Right: "package main", LeftNo: 1, RightNo: 1},
		{Kind: textdiff.Same, Left: "here is the needle", Right: "here is the needle", LeftNo: 2, RightNo: 2},
	}
	loaded := diffViewWith(rows, nil)
	loaded.relayout(m.width)
	u, _ := m.Update(diffMsg{tag: "status:x", view: loaded})
	m = u.(Model)
	v := m.diffLayer()

	if v.loading {
		t.Fatal("the load must clear loading")
	}
	if v.search.query != "needle" {
		t.Fatalf("the query must survive the load: %+v", v.search)
	}
	if len(v.search.hits) != 1 || v.search.hits[0].row != 1 {
		t.Fatalf("hits = %v, want one hit on row 1", v.search.hits)
	}
	if v.search.cur != 0 {
		t.Fatalf("cur = %d, want 0", v.search.cur)
	}
	if v.curLine != 1 {
		t.Fatalf("curLine = %d, want 1 (the cursor must land on the hit once content arrives)", v.curLine)
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

// TestDiffSearchPaintsTheHit calls diffPaneLines directly (not the full
// renderDiffView, whose header alone would make painted != plain even if the
// six hits arguments in diffPaneLines were all nil) and asserts, per row:
// the TEXT is unchanged (ansi.Strip equal — a hit must never move a
// character), a non-hit row's raw (styled) output is BYTE-IDENTICAL to plain,
// and a hit row carries its search styling — st().diffEmph's colour for an
// ordinary hit, st().currentHitStyle's reverse-video FLIP for the current one
// (assertCurrentHitPaint), and on an UNCHANGED row the mirrored left cell
// too — at EXACTLY the hit's own display-column range — checked with ansi.Cut, which slices a styled string
// by column without disturbing its escape codes — and nowhere earlier on the
// same row. curStart == curEnd (0, 0) removes the cursor marker so it cannot
// confound the diff.
func TestDiffSearchPaintsTheHit(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(prev)

	m := diffModel()
	m.width, m.height = 100, 20
	const w, body = 100, 10

	// The foreground colour every search hit wears, current or not — derived
	// at runtime (never hardcoded) and absent everywhere else in this
	// fixture's plain render (no syntax highlighting, no other styling that
	// sets a foreground colour).
	fgRe := regexp.MustCompile(`38;5;\d+`)
	marker := fgRe.FindString(lipgloss.NewStyle().Inherit(st().diffEmph).Render("x"))
	if marker == "" {
		t.Fatal("could not derive the search-emphasis colour marker from st().diffEmph")
	}

	for _, lm := range []longMode{longScroll, longWrap, longTruncate} {
		v := diffViewWith(searchDiffRows(), []int{2})
		v.long = lm
		v.relayout(w)

		plain := m.diffPaneLines(v, w, body, 0, 0, "row")

		v.search.query = "alpha"
		v.search.hits = findHits(v.searchLines(), "alpha")
		v.search.cur = 0
		if len(v.search.hits) != 2 {
			t.Fatalf("mode %d: hits = %v, want 2", lm, v.search.hits)
		}
		painted := m.diffPaneLines(v, w, body, 0, 0, "row")

		if len(painted) != len(plain) {
			t.Fatalf("mode %d: row count changed, plain %d painted %d", lm, len(plain), len(painted))
		}

		gut := gutterWidth(v.full)
		paneW := (w - 1) / 2
		// hitCol resolves a hit's absolute display-column range in the composed
		// "gutter+left│gutter+right" row string diffPaneLines returns — the
		// same gutter/pane arithmetic diffPaneLines and its cell painters use.
		sideText := func(row textdiff.Row, side int) string {
			if side == 1 {
				return row.Right
			}
			return row.Left
		}
		hitCol := func(row textdiff.Row, h searchHit) (int, int) {
			cs, ce := hitCols(sanitizeLine(sideText(row, h.side)), h)
			off := gut + 1
			if h.side == 1 {
				off += paneW + 1
			}
			return off + cs, off + ce
		}

		hitsByRow := map[int]searchHit{}
		for _, h := range v.search.hits {
			hitsByRow[h.row] = h
		}

		for i := range plain {
			if ansi.Strip(painted[i]) != ansi.Strip(plain[i]) {
				t.Errorf("mode %d row %d: a hit changed the TEXT, not just the styling:\nplain:   %q\npainted: %q",
					lm, i, ansi.Strip(plain[i]), ansi.Strip(painted[i]))
			}
			h, isHit := hitsByRow[i]
			if !isHit {
				if painted[i] != plain[i] {
					t.Errorf("mode %d row %d: a non-hit row's styling changed:\nplain:   %q\npainted: %q", lm, i, plain[i], painted[i])
				}
				continue
			}
			if painted[i] == plain[i] {
				t.Errorf("mode %d row %d: the hit did not change the render", lm, i)
			}
			cs, ce := hitCol(v.lines[i].Row, h)
			wantText := string([]rune(sanitizeLine(sideText(v.lines[i].Row, h.side)))[h.start:h.end])
			hitSlice := ansi.Cut(painted[i], cs, ce)
			// The STRIPPED content of exactly [cs,ce) must be the hit's own
			// text — not "func " (short by the prefix), not "alph" or "lpha"
			// (off by one), not "alpha()" (too wide). A wrong column would
			// show up here as wrong or truncated text, which is what "the
			// styled run starts after exactly the display prefix width" means
			// in practice: cut at the wrong column and you cut mid-word.
			if got := ansi.Strip(hitSlice); got != wantText {
				t.Errorf("mode %d row %d: columns [%d,%d) hold %q, want the hit text %q", lm, i, cs, ce, got, wantText)
			}
			ctx := fmt.Sprintf("mode %d row %d: columns [%d,%d)", lm, i, cs, ce)
			if h == v.search.hits[v.search.cur] {
				// The CURRENT hit (the changed row's left "alpha") flips
				// reverse video against its row — a diff row is never
				// reversed, so reverse goes ON — and wears no foreground
				// marker of its own.
				assertCurrentHitPaint(t, ctx, hitSlice, wantText, marker, false)
			} else if !strings.Contains(hitSlice, marker) {
				t.Errorf("mode %d row %d: no search styling at the hit's own columns [%d,%d):\nslice: %q\nfull:  %q", lm, i, cs, ce, hitSlice, painted[i])
			}
			if got := ansi.Cut(plain[i], cs, ce); strings.Contains(got, marker) {
				t.Errorf("mode %d row %d: fixture is unsound — the PLAIN render already carries the marker at [%d,%d)", lm, i, cs, ce)
			}
			// An UNCHANGED row (identical sides) is searched on the right
			// only — one hit per row for stepping — but it must PAINT on
			// both: the two cells hold the very same text, so the mirrored
			// left cell takes the same display offsets. A CHANGED row must
			// not mirror: its sides differ.
			//
			// The fixture's Same-row hit must stay an ORDINARY hit: the marker
			// below is st().diffEmph's foreground, while the current hit paints
			// through styles.currentHitStyle — if search.cur ever moves onto
			// this row, assert it with assertCurrentHitPaint instead.
			mcs, mce := hitCol(v.lines[i].Row, searchHit{row: h.row, side: 1 - h.side, start: h.start, end: h.end})
			mirror := ansi.Cut(painted[i], mcs, mce)
			if v.lines[i].Row.Kind == textdiff.Same {
				if got := ansi.Strip(mirror); got != wantText {
					t.Errorf("mode %d row %d: mirrored columns [%d,%d) hold %q, want the hit text %q", lm, i, mcs, mce, got, wantText)
				}
				// The sequence that actually paints the mirrored text, not
				// merely one present somewhere in the cut (see sgrSeqBefore).
				if got := sgrSeqBefore(mirror, wantText); !strings.Contains(got, marker) {
					t.Errorf("mode %d row %d: an unchanged row must paint the hit on BOTH sides; columns [%d,%d) are painted by %q:\nslice: %q\nfull:  %q", lm, i, mcs, mce, got, mirror, painted[i])
				}
			} else {
				mt := ansi.Strip(mirror)
				seq := sgrSeqBefore(mirror, mt)
				if strings.Contains(seq, marker) || sgrParams(seq)["7"] {
					t.Errorf("mode %d row %d: a CHANGED row must paint only the side that matched, but the mirrored columns [%d,%d) holding %q are painted by %q", lm, i, mcs, mce, mt, seq)
				}
			}
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

// TestDiffSearchRefindsOnAnyRebuild: a re-find must happen after ANY rebuild
// while a query is active — not only the f key's own path. expandFoldFor
// (note_keys.go) rebuilds straight from partial to full mode (v.partial =
// false; v.rebuild()) without going through updateDiffViewKey's "f" case, so
// the re-find has to live in rebuild() itself. The fold renumbers logical
// lines: a hit index that was valid against partial-mode v.lines names a
// DIFFERENT line once the view expands to full mode, unless the search is
// re-run against the new line stream.
func TestDiffSearchRefindsOnAnyRebuild(t *testing.T) {
	t.Parallel()
	rows := []textdiff.Row{
		{Kind: textdiff.Same, Left: "s0", Right: "s0", LeftNo: 1, RightNo: 1},
		{Kind: textdiff.Same, Left: "s1", Right: "s1", LeftNo: 2, RightNo: 2},
		{Kind: textdiff.Same, Left: "s2", Right: "s2", LeftNo: 3, RightNo: 3},
		{Kind: textdiff.Same, Left: "s3", Right: "s3", LeftNo: 4, RightNo: 4},
		{Kind: textdiff.Same, Left: "s4", Right: "s4", LeftNo: 5, RightNo: 5},
		{Kind: textdiff.Same, Left: "needle line", Right: "needle line", LeftNo: 6, RightNo: 6},
		{Kind: textdiff.Changed, Left: "old", Right: "new", LeftNo: 7, RightNo: 7},
		{Kind: textdiff.Same, Left: "a7", Right: "a7", LeftNo: 8, RightNo: 8},
		{Kind: textdiff.Same, Left: "a8", Right: "a8", LeftNo: 9, RightNo: 9},
		{Kind: textdiff.Same, Left: "a9", Right: "a9", LeftNo: 10, RightNo: 10},
		{Kind: textdiff.Same, Left: "a10", Right: "a10", LeftNo: 11, RightNo: 11},
		{Kind: textdiff.Same, Left: "a11", Right: "a11", LeftNo: 12, RightNo: 12},
		{Kind: textdiff.Same, Left: "a12", Right: "a12", LeftNo: 13, RightNo: 13},
	}
	// diffContext (3) keeps rows 3..9 around the row-6 change; rows 0-2 and
	// 10-12 fold away, so "needle line" (original row 5) sits at a SMALLER
	// index in the partial-mode v.lines than its own row number.
	v := diffViewWith(rows, []int{6})
	v.partial = true
	v.rebuild()

	v.search.query = "needle"
	v.search.hits = findHits(v.searchLines(), v.search.query)
	v.search.cur = 0
	if len(v.search.hits) != 1 {
		t.Fatalf("partial-mode hits = %v, want 1", v.search.hits)
	}
	partialRow := v.search.hits[0].row
	if got := v.lines[partialRow].Row.Left; got != "needle line" {
		t.Fatalf("fixture sanity: partial hit row %d is %q, want the needle line", partialRow, got)
	}
	if partialRow == 5 {
		t.Fatal("fixture sanity: the fold produced no index shift — broaden it")
	}

	// Mirror expandFoldFor: rebuild straight to full mode, NOT through the f
	// key handler (which is covered by TestDiffSearchRefindsAfterAModeToggle).
	v.partial = false
	v.rebuild()

	if len(v.search.hits) != 1 {
		t.Fatalf("hits after a non-f rebuild = %v, want 1", v.search.hits)
	}
	// Find the needle's real row by scanning the REBUILT full-mode v.lines —
	// asserting against a hardcoded index would just re-encode the bug.
	want := -1
	for i, ln := range v.lines {
		if ln.Row.Left == "needle line" {
			want = i
			break
		}
	}
	if want < 0 {
		t.Fatal("fixture sanity: the needle line is missing from the full-mode lines")
	}
	if got := v.search.hits[0].row; got != want {
		t.Fatalf("after a non-f rebuild, hit row = %d, want %d (the needle's real full-mode row)", got, want)
	}
}

func TestDiffHintFitsTheBudget(t *testing.T) {
	t.Parallel()
	for _, lm := range []longMode{longScroll, longWrap, longTruncate} {
		if w := lipgloss.Width(diffHintFor(lm)); w > 140 {
			t.Errorf("mode %d hint is %d columns, the budget is 140: %q", lm, w, diffHintFor(lm))
		}
		hint := diffHintFor(lm)
		find := strings.Index(hint, "[/] find")
		if find < 0 {
			t.Errorf("mode %d hint must advertise the search: %q", lm, hint)
			continue
		}
		// POSITION, not just presence: view.go truncates the footer's TAIL to
		// the terminal width, so whatever sits late is what a narrow terminal
		// loses. [/] find shipped LAST and vanished below 140 columns (the
		// user's "bottom bar ... missing search hints"); it now rides near the
		// front, ahead of the change keys, and must stay there.
		if chg := strings.Index(hint, "[n/p]"); find <= 0 || chg < 0 || find > chg {
			t.Errorf("mode %d: [/] find is at %d, [n/p] at %d — the search hint must come early (after the scroll keys, before the change keys): %q", lm, find, chg, hint)
		}
	}
	// The widest variant is at the cap: any new group must shorten a label
	// first. Update this number and diffHintFor's doc comment together.
	if w := lipgloss.Width(diffHintFor(longScroll)); w != 140 {
		t.Fatalf("the scroll variant measures %d columns; the doc comment says 140", w)
	}
	// The selection variant replaces the whole line in every mode, so it has to
	// fit the same budget — a truncated one would hide [esc] unmark, the way out.
	if w := lipgloss.Width(diffSelectHint()); w > 140 {
		t.Fatalf("the selection hint is %d columns, the budget is 140: %q", w, diffSelectHint())
	}
}
