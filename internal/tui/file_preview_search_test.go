package tui

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/homeend/gigagit/internal/i18n"
)

// NOTE: TestFilePreviewSearchPaintsTheHit calls lipgloss.SetColorProfile
// (process-global) and therefore does NOT call t.Parallel().

// previewSearchModel opens a preview over a file whose text is worth searching.
func previewSearchModel(t *testing.T) Model {
	t.Helper()
	var b strings.Builder
	for i := 0; i < 60; i++ {
		fmt.Fprintf(&b, "line%03d alpha\n", i)
	}
	b.WriteString("tail beta\n")
	return openPreview(t, fullTreeTreeSideOf(t, previewModelN(b.String())))
}

func feedPreview(m Model, keys ...string) Model {
	for _, k := range keys {
		u, _ := m.Update(keyMsg(k))
		m = u.(Model)
	}
	return m
}

func TestPreviewSearchScrollsToTheHit(t *testing.T) {
	t.Parallel()
	m := previewSearchModel(t)
	if m.filesTreeFocused {
		t.Fatal("the preview must own the right column")
	}
	m = feedPreview(m, "/", "b", "e", "t", "a")
	p := m.filesPreview
	if p.search.query != "beta" {
		t.Fatalf("query = %q", p.search.query)
	}
	if len(p.search.hits) != 1 || p.search.hits[0].row != 60 {
		t.Fatalf("hits = %v, want one on line 60", p.search.hits)
	}
	rows := m.filePreviewRowsCap()
	if p.sel != 60-rows+1 {
		t.Fatalf("a hit below the window must become the LAST visible line: sel = %d, want %d", p.sel, 60-rows+1)
	}
	out := m.renderFilePreview(m.layout().rightW, m.layout().boxH[panelCommits])
	if !strings.Contains(out, "tail beta") {
		t.Fatalf("the hit line must be on screen:\n%s", out)
	}
}

func TestPreviewSearchTreeFilterIsNotTouched(t *testing.T) {
	t.Parallel()
	m := previewSearchModel(t)
	m = feedPreview(m, "/", "b")
	if m.filesView.typing {
		t.Fatal("the tree's own /-filter must not capture the preview's search")
	}
	if m.filesView.query != "" {
		t.Fatalf("the tree query changed: %q", m.filesView.query)
	}
}

func TestPreviewSearchEscIsTwoStageThenCloses(t *testing.T) {
	t.Parallel()
	m := previewSearchModel(t)
	m = feedPreview(m, "/", "a", "l", "p", "h", "a", "enter")
	m = feedPreview(m, "esc")
	if m.filesPreview == nil {
		t.Fatal("the first esc must only clear the search")
	}
	if m.filesPreview.search.active() {
		t.Fatalf("the first esc must clear the query: %+v", m.filesPreview.search)
	}
	m = feedPreview(m, "esc")
	if m.filesPreview != nil {
		t.Fatal("the second esc must close the preview")
	}
}

func TestPreviewSearchEscWhileTypingRestoresTheOrigin(t *testing.T) {
	t.Parallel()
	m := previewSearchModel(t)
	m.filesPreview.sel = 12
	m = feedPreview(m, "/", "t", "a", "i", "l")
	if m.filesPreview.sel == 12 {
		t.Fatal("the incremental search must have scrolled")
	}
	m = feedPreview(m, "esc")
	if m.filesPreview == nil {
		t.Fatal("esc while typing must not close the preview")
	}
	if m.filesPreview.sel != 12 {
		t.Fatalf("esc must restore the scroll: sel = %d, want 12", m.filesPreview.sel)
	}
}

func TestPreviewSearchStepsAndBadges(t *testing.T) {
	t.Parallel()
	m := previewSearchModel(t)
	m = feedPreview(m, "/", "a", "l", "p", "h", "a", "enter")
	p := m.filesPreview
	if len(p.search.hits) != 60 {
		t.Fatalf("hits = %d, want 60", len(p.search.hits))
	}
	first := p.search.cur
	m = feedPreview(m, "]")
	if m.filesPreview.search.cur != first+1 {
		t.Fatalf("] = %d, want %d", m.filesPreview.search.cur, first+1)
	}
	out := m.renderFilePreview(m.layout().rightW, m.layout().boxH[panelCommits])
	if !strings.Contains(out, "/alpha  2/60") {
		t.Fatalf("the title line must carry the badge:\n%s", strings.Split(out, "\n")[1])
	}
	if !strings.Contains(out, "[/] find") {
		t.Fatalf("the hint must advertise the search:\n%s", out)
	}
}

// TestPreviewSearchStartedDuringLoadRefindsOnContentArrival covers the case a
// search is opened while the preview still shows the "(loading…)"
// placeholder: its hits (and cur) were computed against that single line, so
// when the real content lands in fileContentMsg they are stale until the
// fix re-runs refindFrom/snapHit in that handler. Row/sel are asserted via
// the same previewClamp/snapHit arithmetic the production code uses, not a
// magic number, so the test tracks the real geometry regardless of terminal
// size.
func TestPreviewSearchStartedDuringLoadRefindsOnContentArrival(t *testing.T) {
	t.Parallel()
	m := fullTreeTreeSideOf(t, previewModelN("ignored\n"))
	for _, r := range availableActions(m) {
		if r.id == "view-file" {
			updated, _ := r.run(m) // open the preview but do NOT deliver the load cmd yet
			m = updated.(Model)
		}
	}
	if m.filesPreview == nil || m.filesPreview.lines[0].text != i18n.T("(loading…)") {
		t.Fatalf("expected the preview to still show the placeholder, got %+v", m.filesPreview)
	}
	tag := m.filesPreviewTag
	rows := m.filePreviewRowsCap()

	// Start a search for a needle that is not in the placeholder text.
	m = feedPreview(m, "/", "n", "e", "e", "d", "l", "e")
	if len(m.filesPreview.search.hits) != 0 {
		t.Fatalf("the placeholder must not match: hits = %v", m.filesPreview.search.hits)
	}

	// The needle lands well past the bottom of the window (rows+10), so a
	// stale sel=0 would leave it off-screen — proving snapHit actually ran.
	var lines []contentLine
	for i := 0; i < rows+10; i++ {
		lines = append(lines, contentLine{text: fmt.Sprintf("line%03d", i)})
	}
	needleRow := len(lines)
	lines = append(lines, contentLine{text: "here is the needle"})
	u, _ := m.Update(fileContentMsg{tag: tag, lines: lines})
	m = u.(Model)

	p := m.filesPreview
	if len(p.search.hits) != 1 || p.search.hits[0].row != needleRow {
		t.Fatalf("hits = %v, want one hit on row %d", p.search.hits, needleRow)
	}
	if p.search.cur != 0 {
		t.Fatalf("cur = %d, want 0", p.search.cur)
	}
	want := previewClamp(needleRow-rows+1, len(lines), rows, p.mode)
	if p.sel != want {
		t.Fatalf("sel = %d, want %d (the hit row must scroll into view once the content lands)", p.sel, want)
	}
}

// TestFilePreviewSearchPaintsTheHit strengthens the brief's paint check
// (which only asserted the header badge, and which the reviews of Tasks 3
// and 4 both rejected for that reason) to prove the search actually colours
// the hit row: per BODY row (skipping the title, which carries the badge,
// and the hint), a non-hit row's raw render is byte-identical to plain and
// the hit row carries the search-emphasis colour at EXACTLY the hit's own
// display columns — nowhere else on the row. Same idiom as
// TestBlameSearchPaintsTheHit.
func TestFilePreviewSearchPaintsTheHit(t *testing.T) {
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
	p := &contentPopup{title: "f.go", lines: []contentLine{
		{text: "alpha one"},
		{text: "plain line"},
		{text: "alpha two"},
	}}
	m.filesPreview = p
	boxW, boxH := m.layout().rightW, m.layout().boxH[panelCommits]

	plain := m.renderFilePreview(boxW, boxH)

	p.search.query = "alpha"
	p.search.hits = findHits(previewSearchLines(p), "alpha")
	if len(p.search.hits) != 2 {
		t.Fatalf("hits = %v, want 2", p.search.hits)
	}
	p.search.cur = 0

	painted := m.renderFilePreview(boxW, boxH)

	plainLines := strings.Split(plain, "\n")
	paintedLines := strings.Split(painted, "\n")
	if len(plainLines) != len(paintedLines) {
		t.Fatalf("row count changed: plain %d painted %d", len(plainLines), len(paintedLines))
	}

	hitsByRow := map[int]searchHit{}
	for _, h := range p.search.hits {
		hitsByRow[h.row] = h
	}

	// Body rows start at output line 2 (line 0 is the box's top border, line
	// 1 is the title): the box is a bordered/padded panel (border char + 1
	// padding space), which offsets every content column by prefixW.
	const prefixW = 2
	for i, l := range p.lines {
		idx := i + 2
		pl, wl := plainLines[idx], paintedLines[idx]
		if ansi.Strip(wl) != ansi.Strip(pl) {
			t.Errorf("row %d: a hit changed the TEXT, not just the styling:\nplain:   %q\npainted: %q", i, ansi.Strip(pl), ansi.Strip(wl))
		}
		h, isHit := hitsByRow[i]
		if !isHit {
			if wl != pl {
				t.Errorf("row %d: a non-hit row's styling changed:\nplain:   %q\npainted: %q", i, pl, wl)
			}
			continue
		}
		if wl == pl {
			t.Errorf("row %d: the hit did not change the render", i)
		}
		cs, ce := hitCols(l.text, h)
		cs, ce = prefixW+cs, prefixW+ce
		wantText := string([]rune(l.text)[h.start:h.end])
		hitSlice := ansi.Cut(wl, cs, ce)
		if got := ansi.Strip(hitSlice); got != wantText {
			t.Errorf("row %d: columns [%d,%d) hold %q, want the hit text %q", i, cs, ce, got, wantText)
		}
		if !strings.Contains(hitSlice, marker) {
			t.Errorf("row %d: no search styling at the hit's own columns [%d,%d):\nslice: %q\nfull:  %q", i, cs, ce, hitSlice, wl)
		}
		if got := ansi.Cut(pl, cs, ce); strings.Contains(got, marker) {
			t.Errorf("row %d: fixture is unsound — the PLAIN render already carries the marker at [%d,%d)", i, cs, ce)
		}
	}
}
