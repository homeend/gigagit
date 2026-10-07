package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/agentdocs"
)

func spanText(lines []contentLine, s anchorSpan) string {
	return string([]rune(lines[s.line].text)[s.from:s.to])
}

// anchorText is an anchor's label as laid out: its spans' text joined by a
// space (a wrap swallowed the space between two rows).
func anchorText(lines []contentLine, a anchor) string {
	var parts []string
	for _, s := range a.spans {
		parts = append(parts, spanText(lines, s))
	}
	return strings.Join(parts, " ")
}

func TestOverviewLinesSpansInAParagraph(t *testing.T) {
	t.Parallel()
	lines, as := overviewLines("See [open files](internal/tui/open_files.go:20) and [x](a.go).", 80, modeScroll)
	if len(as) != 2 {
		t.Fatalf("anchors = %+v, want 2", as)
	}
	if got := anchorText(lines, as[0]); got != "open files" {
		t.Fatalf("anchor 0 text = %q", got)
	}
	if as[0].target != (agentdocs.Anchor{Dest: "internal/tui/open_files.go:20", Path: "internal/tui/open_files.go", Start: 20, End: 20, Label: "open files"}) || as[0].dest != "internal/tui/open_files.go:20" {
		t.Fatalf("anchor 0 = %+v", as[0])
	}
	if got := anchorText(lines, as[1]); got != "x" {
		t.Fatalf("anchor 1 text = %q", got)
	}
	s := as[0].spans[0]
	for k := s.from; k < s.to; k++ {
		if c := lines[s.line].cls[k]; c != mdAnchor {
			t.Fatalf("rune %d class = %d, want mdAnchor", k, c)
		}
	}
	if strings.Contains(lines[0].text, "open_files.go") {
		t.Fatalf("the destination is shown: %q", lines[0].text)
	}
}

func TestOverviewLinesWrappedLabelHasTwoSpans(t *testing.T) {
	t.Parallel()
	lines, as := overviewLines("[alpha beta gamma delta epsilon](a.go)", 20, modeScroll)
	if len(as) != 1 || len(as[0].spans) < 2 {
		t.Fatalf("anchors = %+v, want one anchor over two rows", as)
	}
	if got := anchorText(lines, as[0]); got != "alpha beta gamma delta epsilon" {
		t.Fatalf("anchor text = %q", got)
	}
	for _, l := range lines {
		if len([]rune(l.text)) > 20 {
			t.Fatalf("row wider than the layout width: %q", l.text)
		}
	}
}

func TestOverviewLinesAdjacentAnchorsStayApart(t *testing.T) {
	t.Parallel()
	lines, as := overviewLines("[a](x.go)[b](y.go)", 80, modeScroll)
	if len(as) != 2 || len(as[0].spans) != 1 || len(as[1].spans) != 1 {
		t.Fatalf("anchors = %+v, want two", as)
	}
	if anchorText(lines, as[0]) != "a" || anchorText(lines, as[1]) != "b" {
		t.Fatalf("texts %q %q", anchorText(lines, as[0]), anchorText(lines, as[1]))
	}
}

func TestOverviewLinesAnchorsInListTableQuote(t *testing.T) {
	t.Parallel()
	src := "- see [the list](l.go)\n\n| h |\n|---|\n| [cell](t.go) |\n\n> [quoted](q.go)\n"
	lines, as := overviewLines(src, 80, modeScroll)
	if len(as) != 3 {
		t.Fatalf("anchors = %+v, want 3", as)
	}
	for i, want := range []string{"the list", "cell", "quoted"} {
		if got := anchorText(lines, as[i]); got != want {
			t.Errorf("anchor %d text = %q, want %q", i, got, want)
		}
	}
}

func TestOverviewLinesEmphasisInALabel(t *testing.T) {
	t.Parallel()
	lines, as := overviewLines("[**bold** label](a.go)", 80, modeScroll)
	if len(as) != 1 || anchorText(lines, as[0]) != "bold label" {
		t.Fatalf("anchors = %+v", as)
	}
}

func TestOverviewLinesCapsAnchors(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	for i := 0; i <= agentdocs.MaxAnchors; i++ {
		fmt.Fprintf(&b, "- [label%d](f%d.go)\n", i, i)
	}
	lines, as := overviewLines(b.String(), 80, modeScroll)
	if len(as) != agentdocs.MaxAnchors {
		t.Fatalf("anchors = %d, want %d", len(as), agentdocs.MaxAnchors)
	}
	last := fmt.Sprintf("label%d", agentdocs.MaxAnchors)
	found := false
	for _, l := range lines {
		if strings.Contains(l.text, last) {
			found = true
		}
		for _, c := range l.cls {
			if c >= mdClassEnd {
				t.Fatalf("an anchor id class leaked: %d in %q", c, l.text)
			}
		}
	}
	if !found {
		t.Fatalf("the label past the cap is gone")
	}
}

func TestOverviewPaintMarksSelectedAndMissing(t *testing.T) {
	t.Parallel()
	lines, as := overviewLines("[a](x.go) [b](y.go) [c](z.go)", 80, modeScroll)
	ov := &overview{anchors: as, sel: 1}
	ov.anchors[0].missing = true
	ov.paint(lines)
	cls := func(i int) any { s := ov.anchors[i].spans[0]; return lines[s.line].cls[s.from] }
	if cls(0) != mdAnchorGone || cls(1) != mdAnchorSel || cls(2) != mdAnchor {
		t.Fatalf("classes = %v %v %v", cls(0), cls(1), cls(2))
	}
	ov.sel = 2
	ov.paint(lines)
	if cls(1) != mdAnchor || cls(2) != mdAnchorSel {
		t.Fatalf("after moving the selection: %v %v", cls(1), cls(2))
	}
}

func TestOverviewLinesEmptyText(t *testing.T) {
	t.Parallel()
	lines, as := overviewLines("  \n", 80, modeScroll)
	if len(as) != 0 || len(lines) != 1 || lines[0].src {
		t.Fatalf("lines=%+v anchors=%+v, want one placeholder", lines, as)
	}
}
