package tui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/homeend/gigagit/internal/hunkpick"
)

// NOTE: TestPickerSearchPaintsTheHit calls lipgloss.SetColorProfile
// (process-global) and therefore does NOT call t.Parallel().

// pickerDoc (conflict_picker_test.go) is: literal "top"; block 0 current
// ["foo"] / incoming ["bar"]; literal "mid"; block 1 current ["A","B"] /
// incoming ["C"]. The flat search rows are therefore
// 0:foo 1:bar 2:A 3:B 4:C — the literals are NOT searchable.
func pickerSearchModel() (Model, *hunkPicker) {
	e := newConflictPicker("f.txt", pickerDoc())
	m := Model{layers: &layerStack{entries: []layer{e}}, width: 80, height: 24}
	return m, e
}

func typePicker(m Model, e *hunkPicker, keys ...string) Model {
	for _, k := range keys {
		m, _ = e.update(m, keyMsg(k))
	}
	return m
}

func TestPickerSearchJumpsTheTwoDimensionalCursor(t *testing.T) {
	t.Parallel()
	m, e := pickerSearchModel()
	m = typePicker(m, e, "/", "b")
	if len(e.search.hits) != 2 {
		t.Fatalf("hits = %v, want 2 (bar, B)", e.search.hits)
	}
	if e.bi != 0 || e.side != hunkpick.Incoming || e.line != 0 {
		t.Fatalf("cursor = %d/%v/%d, want block 0 incoming line 0 (\"bar\")", e.bi, e.side, e.line)
	}
	m = typePicker(m, e, "enter", "]")
	if e.bi != 1 || e.side != hunkpick.Current || e.line != 1 {
		t.Fatalf("] = %d/%v/%d, want block 1 current line 1 (\"B\")", e.bi, e.side, e.line)
	}
	_ = typePicker(m, e, "]") // wrap
	if e.bi != 0 || e.side != hunkpick.Incoming {
		t.Fatalf("] must wrap to the first hit, got %d/%v", e.bi, e.side)
	}
}

// emptyCurrentSideDoc has two blocks: block 0's Current side is EMPTY (a
// pure-add hunk, as the stage/unstage pickers can show) with Incoming
// ["needle"]; block 1 has Current ["x"] and Incoming ["needle"] too, so a
// search for "needle" finds two hits and a bug that ignores side on the
// stepping-position check can walk from the first straight past it to the
// second.
func emptyCurrentSideDoc() *hunkpick.Doc {
	d, _ := hunkpick.ParseConflict([]byte(
		"top\n<<<<<<< HEAD\n=======\nneedle\n>>>>>>> x\nmid\n<<<<<<< HEAD\nx\n=======\nneedle\n>>>>>>> x\n"))
	return d
}

// emptyIncomingSideDoc is the symmetric fixture: block 0's INCOMING side is
// empty (Current ["x"], Incoming []), immediately followed by block 1, whose
// Current side ["needle"] carries a hit, and block 2, whose Incoming side
// ["needle"] carries a second, farther hit — needed so a wrong skip lands on
// a DIFFERENT hit instead of coincidentally wrapping back to the only one.
func emptyIncomingSideDoc() *hunkpick.Doc {
	d, _ := hunkpick.ParseConflict([]byte(
		"top\n<<<<<<< HEAD\nx\n=======\n>>>>>>> x\nmid\n<<<<<<< HEAD\nneedle\n=======\ny\n>>>>>>> x\n" +
			"end\n<<<<<<< HEAD\nz\n=======\nneedle\n>>>>>>> x\n"))
	return d
}

// TestPickerSearchStepHonorsSideOnEmptySide guards searchPos: a hit's row can
// coincide with the flat row of the CURSOR's own (empty) side on the SAME
// block when that side contributes zero lines to the flat row space (an empty
// Current side starts at the very row its Incoming side's first line takes).
// Without also comparing side, searchPos would report the cursor as already
// sitting ON that Incoming hit while it is actually on the empty Current
// side, and "]" (strictly after the position) would skip over it.
func TestPickerSearchStepHonorsSideOnEmptySide(t *testing.T) {
	t.Parallel()
	e := newConflictPicker("f.txt", emptyCurrentSideDoc())
	m := Model{layers: &layerStack{entries: []layer{e}}, width: 80, height: 24}
	m = typePicker(m, e, "/", "needle", "enter")
	if len(e.search.hits) != 2 {
		t.Fatalf("hits = %v, want 2", e.search.hits)
	}
	if e.bi != 0 || e.side != hunkpick.Incoming || e.line != 0 {
		t.Fatalf("cursor = %d/%v/%d, want block 0 incoming line 0", e.bi, e.side, e.line)
	}
	m = typePicker(m, e, "left") // block 0's Current side is empty
	if e.side != hunkpick.Current {
		t.Fatalf("← should focus the (empty) current side, got %v", e.side)
	}
	_ = typePicker(m, e, "]")
	if e.bi != 0 || e.side != hunkpick.Incoming || e.line != 0 {
		t.Fatalf("] must land back on block 0's incoming hit, got %d/%v/%d", e.bi, e.side, e.line)
	}

	// Symmetric case: block 0's INCOMING side is empty. searchRow(0,
	// Incoming, 0) then aliases onto block 1's first CURRENT row (the next
	// block's first row in flat search-row space), while the position still
	// carries the cursor's own side (Incoming = 1). A hit on that aliased row
	// carries side 0, so it sorts BEFORE the buggy position and "]" (which
	// wants the first hit STRICTLY after) skips straight past it to the next
	// hit two blocks away.
	e2 := newConflictPicker("f.txt", emptyIncomingSideDoc())
	m2 := Model{layers: &layerStack{entries: []layer{e2}}, width: 80, height: 24}
	m2 = typePicker(m2, e2, "/", "needle", "enter")
	if len(e2.search.hits) != 2 {
		t.Fatalf("hits = %v, want 2", e2.search.hits)
	}
	if e2.bi != 1 || e2.side != hunkpick.Current || e2.line != 0 {
		t.Fatalf("cursor = %d/%v/%d, want block 1 current line 0 (\"needle\")", e2.bi, e2.side, e2.line)
	}
	e2.bi, e2.side, e2.line = 0, hunkpick.Incoming, 0 // block 0's Incoming side is empty
	_ = typePicker(m2, e2, "]")
	if e2.bi != 1 || e2.side != hunkpick.Current || e2.line != 0 {
		t.Fatalf("] must land on block 1's current hit (the aliased row), got %d/%v/%d", e2.bi, e2.side, e2.line)
	}
}

func TestPickerSearchSkipsLiterals(t *testing.T) {
	t.Parallel()
	m, e := pickerSearchModel()
	_ = typePicker(m, e, "/", "t", "o", "p")
	if len(e.search.hits) != 0 {
		t.Fatalf("literal context must not be searched, got %v", e.search.hits)
	}
}

func TestPickerSearchFromTheOutputPaneReturnsToTheGrid(t *testing.T) {
	t.Parallel()
	m, e := pickerSearchModel()
	m = typePicker(m, e, "tab") // focus the output pane
	if !e.outFocused {
		t.Fatal("tab should focus the output pane")
	}
	_ = typePicker(m, e, "/", "b")
	if e.outFocused {
		t.Fatal("/ must move the search back into the grid (the output pane is never searched)")
	}
	if !e.search.typing {
		t.Fatal("/ pressed on the output pane must still open the search")
	}
}

func TestPickerSearchEscIsTwoStage(t *testing.T) {
	t.Parallel()
	m, e := pickerSearchModel()
	m = typePicker(m, e, "/", "b", "enter")
	m, _ = e.update(m, keyMsg("esc")) // clears the query
	if layerOf[*hunkPicker](m) == nil {
		t.Fatal("the first esc must not close the picker")
	}
	if e.search.active() {
		t.Fatalf("the first esc must clear the query: %+v", e.search)
	}
	m, _ = e.update(m, keyMsg("esc"))
	if layerOf[*hunkPicker](m) != nil {
		t.Fatal("the second esc must close the picker")
	}
}

func TestPickerSearchEscWhileTypingRestoresTheCursor(t *testing.T) {
	t.Parallel()
	m, e := pickerSearchModel()
	e.bi, e.side, e.line = 1, hunkpick.Incoming, 0
	m = typePicker(m, e, "/", "b")
	_ = typePicker(m, e, "esc")
	if e.bi != 1 || e.side != hunkpick.Incoming || e.line != 0 {
		t.Fatalf("esc must restore the cursor: %d/%v/%d", e.bi, e.side, e.line)
	}
	if e.search.active() {
		t.Fatalf("esc must leave no search: %+v", e.search)
	}
}

func TestPickerSearchBadgeAndHint(t *testing.T) {
	t.Parallel()
	m, e := pickerSearchModel()
	m = typePicker(m, e, "/", "b", "enter")
	out := ansi.Strip(e.render(m, ""))
	if !strings.Contains(strings.Split(out, "\n")[0], "/b  1/2") {
		t.Fatalf("the header must carry the badge:\n%s", strings.Split(out, "\n")[0])
	}
	if !strings.Contains(out, "[/] find") {
		t.Fatalf("the hint must advertise the search:\n%s", out)
	}
}

// The process-owned picker never sees esc: conflictProcess.update eats it. A
// live search must get it first, or the query can only be dismissed by leaving
// the editor.
func TestProcessPickerEscClearsTheSearchFirst(t *testing.T) {
	t.Parallel()
	e := newProcessConflictPicker("f.txt", pickerDoc())
	p := &conflictProcess{st: confPicking, picker: e, pickPath: "f.txt"}
	m := Model{width: 80, height: 24, proc: p}
	m, _ = p.update(m, keyMsg("/"))
	m, _ = p.update(m, keyMsg("b"))
	m, _ = p.update(m, keyMsg("enter"))
	if !e.search.active() {
		t.Fatalf("the process must route the search keys to the picker: %+v", e.search)
	}
	m, _ = p.update(m, keyMsg("esc"))
	if p.picker == nil || p.st != confPicking {
		t.Fatal("the first esc must stay in the editor and clear the search")
	}
	if e.search.active() {
		t.Fatalf("the first esc must clear the query: %+v", e.search)
	}
	m, _ = p.update(m, keyMsg("esc"))
	if p.picker != nil || p.st != confListing {
		t.Fatal("the second esc must leave the editor")
	}
	_ = m
}

// TestProcessPickerEscZoomedTypingSearchWinsFirst guards the esc order in the
// process-owned picker when BOTH a zoom and a live search are in play: the
// search must win first in both hosts (controller ruling) — esc while typing
// cancels the search and leaves the zoom untouched; only once no search is
// left does esc fall through to un-zoom, and only then to leaving the editor.
func TestProcessPickerEscZoomedTypingSearchWinsFirst(t *testing.T) {
	t.Parallel()
	e := newProcessConflictPicker("f.txt", pickerDoc())
	e.zoomed = true
	p := &conflictProcess{st: confPicking, picker: e, pickPath: "f.txt"}
	m := Model{width: 80, height: 24, proc: p}
	m, _ = p.update(m, keyMsg("/"))
	m, _ = p.update(m, keyMsg("b")) // still typing — not committed
	if !e.search.typing {
		t.Fatalf("expected the search to still be typing: %+v", e.search)
	}

	m, _ = p.update(m, keyMsg("esc"))
	if p.picker == nil || p.st != confPicking {
		t.Fatal("the first esc must stay in the editor")
	}
	if e.search.active() {
		t.Fatalf("the first esc must cancel the typing search: %+v", e.search)
	}
	if !e.zoomed {
		t.Fatal("the first esc must leave the zoom untouched")
	}

	m, _ = p.update(m, keyMsg("esc"))
	if p.picker == nil || p.st != confPicking {
		t.Fatal("the second esc must still stay in the editor")
	}
	if e.zoomed {
		t.Fatal("the second esc must un-zoom, now that no search is live")
	}

	m, _ = p.update(m, keyMsg("esc"))
	if p.picker != nil || p.st != confListing {
		t.Fatal("the third esc must leave the editor")
	}
	_ = m
}

// TestPickerSearchPaintsTheHit is a strengthened version of the brief's paint
// coverage (reviews of Tasks 3 and 4 rejected a paint test the header badge
// alone could satisfy): it renders the GRID with and without a live search,
// asserts every row's TEXT is unchanged, every row with no hit at all is
// byte-identical, and each hit row carries the search-emphasis colour (shared
// by st().diffEmph/st().searchCur) at EXACTLY its own hit's column range
// while its sibling column reports no hits at all (checked at the hitsOn data
// level, not by slicing composed ANSI bytes — see the inline note below) —
// once with the hit ("bar", right column) sitting under the reversed cursor
// cell, and once with the cursor elsewhere so both hits ("bar" and "B", left
// column) take the ordinary non-reversed painted path.
//
// Row layout is fixed by pickerDoc's fixture (see the comment above): with
// the output pane collapsed the grid's colRow list is
//
//	0 "  top" (literal)             4 block 1 header
//	1 block 0 header                5 "A" / "C"
//	2 "foo" / "bar"                 6 "B" / (blank)
//	3 "  mid" (literal)
//
// so with a query of "b" the hits land on row 2's RIGHT column ("bar") and
// row 6's LEFT column ("B").
func TestPickerSearchPaintsTheHit(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(prev)

	fgRe := regexp.MustCompile(`38;5;\d+`)
	marker := fgRe.FindString(lipgloss.NewStyle().Inherit(st().diffEmph).Render("x"))
	if marker == "" {
		t.Fatal("could not derive the search-emphasis colour marker from st().diffEmph")
	}

	m := Model{width: 80, height: 30}
	sepW := lipgloss.Width(pickerColSep)
	colW := (80 - sepW) / 2
	const gutterW = 6 // "  [ ] " / "> [ ] "
	leftPrefix := gutterW
	rightPrefix := colW + sepW + gutterW

	type cursorCase struct {
		name string
		bi   int
		side hunkpick.Side
		line int
	}
	cases := []cursorCase{
		{name: "cursor on the bar hit (reversed cell)", bi: 0, side: hunkpick.Incoming, line: 0},
		{name: "cursor elsewhere (ordinary painted path for both hits)", bi: 1, side: hunkpick.Current, line: 0},
	}

	for _, cc := range cases {
		t.Run(cc.name, func(t *testing.T) {
			e := newConflictPicker("f.txt", pickerDoc())
			e.outCollapsed = true
			e.bi, e.side, e.line = cc.bi, cc.side, cc.line

			plain := e.render(m, "")
			if e.lastGridH < 7 {
				t.Fatalf("grid too short to show every candidate row: %d, want >= 7", e.lastGridH)
			}

			e.search.query = "b"
			e.search.hits = findHits(e.searchLines(), "b")
			if len(e.search.hits) != 2 {
				t.Fatalf("hits = %v, want 2 (bar, B)", e.search.hits)
			}
			e.search.cur = 0 // the "bar" hit

			painted := e.render(m, "")

			plainLines := strings.Split(plain, "\n")
			paintedLines := strings.Split(painted, "\n")
			if len(plainLines) != len(paintedLines) {
				t.Fatalf("row count changed: plain %d painted %d", len(plainLines), len(paintedLines))
			}

			// grid row index -> (prefix column, hit text, the OTHER side's flat
			// search row/side) for the two known hits. The other side's cell
			// must carry no hits at all — checked at the hitsOn data level
			// (never by slicing the composed row's ANSI bytes at an internal
			// column boundary: x/ansi's Cut was found, empirically, to drag
			// trailing housekeeping escape sequences belonging to a LATER,
			// unrelated styled run into an earlier narrow slice — see the RED
			// run note in the task report — so a byte-range comparison at a
			// non-terminal cut point is not trustworthy here).
			type hitAt struct {
				col                    int
				text                   string
				otherRow, otherSideNum int
			}
			hitRows := map[int]hitAt{
				// "bar" (Incoming) is the hit; Current ("foo") is the other side.
				2: {col: rightPrefix, text: "b", otherRow: e.searchRow(0, hunkpick.Current, 0), otherSideNum: 0},
				// "B" (Current) is the hit; Incoming (out of range, blank) is the other side.
				6: {col: leftPrefix, text: "b", otherRow: e.searchRow(1, hunkpick.Incoming, 1), otherSideNum: 1},
			}

			for i := 0; i < e.lastGridH; i++ {
				idx := 2 + i // header, colLabels, then the grid body
				p, w := plainLines[idx], paintedLines[idx]
				if ansi.Strip(w) != ansi.Strip(p) {
					t.Errorf("row %d: a hit changed the TEXT, not just the styling:\nplain:   %q\npainted: %q", i, ansi.Strip(p), ansi.Strip(w))
				}
				ha, isHit := hitRows[i]
				if !isHit {
					if w != p {
						t.Errorf("row %d: a non-hit row's styling changed:\nplain:   %q\npainted: %q", i, p, w)
					}
					continue
				}
				if w == p {
					t.Errorf("row %d: the hit did not change the render", i)
				}
				hitSlice := ansi.Cut(w, ha.col, ha.col+1)
				if got := ansi.Strip(hitSlice); !strings.EqualFold(got, ha.text) {
					t.Errorf("row %d: columns [%d,%d) hold %q, want the hit's own text %q", i, ha.col, ha.col+1, got, ha.text)
				}
				if !strings.Contains(hitSlice, marker) {
					t.Errorf("row %d: no search styling at the hit's own columns [%d,%d):\nslice: %q\nfull:  %q", i, ha.col, ha.col+1, hitSlice, w)
				}
				if got := ansi.Cut(p, ha.col, ha.col+1); strings.Contains(got, marker) {
					t.Errorf("row %d: fixture is unsound — the PLAIN render already carries the marker at [%d,%d)", i, ha.col, ha.col+1)
				}
				// The OTHER cell on the same row must carry no hits — no
				// bleed-over from this row's overlay onto its sibling column.
				if oh := e.search.hitsOn(ha.otherRow, ha.otherSideNum); len(oh) != 0 {
					t.Errorf("row %d: the non-hit cell unexpectedly has hits: %v", i, oh)
				}
			}
		})
	}
}
