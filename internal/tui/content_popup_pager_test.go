package tui

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

var pagerLineRE = regexp.MustCompile(`line (\d\d)`)

// pagerShown renders p and returns the numbered lines it shows, top to bottom.
func pagerShown(t *testing.T, p *contentPopup, m Model) []string {
	t.Helper()
	var out []string
	for _, l := range boxLines(t, p, m) {
		if g := pagerLineRE.FindStringSubmatch(l); g != nil {
			out = append(out, g[1])
		}
	}
	if len(out) == 0 {
		t.Fatal("the popup shows no content line")
	}
	return out
}

func pagerPress(t *testing.T, p *contentPopup, m Model, key tea.KeyType, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		p.update(m, tea.KeyMsg{Type: key})
	}
}

func newPagerPopup(n int) *contentPopup {
	var lines []contentLine
	for i := 1; i <= n; i++ {
		lines = append(lines, contentLine{text: fmt.Sprintf("line %02d", i)})
	}
	p := newContentPopup("pager", lines)
	p.mode = modeWrap
	p.noCursor = true
	return p
}

// A popup without a row cursor is prose to read, so it is a pager: every ↓
// scrolls the text one line. It used to move an invisible cursor instead, and
// the text only started to move once that cursor passed the middle of the
// window — the first presses did nothing on screen.
func TestNoCursorPopupScrollsOnTheFirstPress(t *testing.T) {
	m := sizedModel(t, 80, 30)
	p := newPagerPopup(80)

	before := pagerShown(t, p, m)
	if before[0] != "01" {
		t.Fatalf("opens at line %s, want 01", before[0])
	}
	pagerPress(t, p, m, tea.KeyDown, 1)
	after := pagerShown(t, p, m)
	if after[0] != "02" {
		t.Fatalf("after one ↓ the top line is %s, want 02 (%d lines shown)", after[0], len(after))
	}
}

// At the end of the text ↓ stops scrolling, and the first ↑ scrolls back at
// once — no hidden cursor left to walk back up through the window.
func TestNoCursorPopupUpScrollsAtOnceFromTheEnd(t *testing.T) {
	m := sizedModel(t, 80, 30)
	p := newPagerPopup(80)

	pagerPress(t, p, m, tea.KeyDown, 200)
	end := pagerShown(t, p, m)
	if end[len(end)-1] != "80" {
		t.Fatalf("after ↓ past the end the last line shown is %s, want 80", end[len(end)-1])
	}
	pagerPress(t, p, m, tea.KeyUp, 1)
	up := pagerShown(t, p, m)
	if up[len(up)-1] != "79" || up[0] == end[0] {
		t.Fatalf("one ↑ from the end shows %s..%s, want it to end at 79 (was %s..%s)",
			up[0], up[len(up)-1], end[0], end[len(end)-1])
	}
}

// A wrapped row is several display lines; the pager scrolls by display line,
// so one ↓ reveals the next line of a long paragraph rather than jumping past it.
func TestNoCursorPopupScrollsWrappedRowsByLine(t *testing.T) {
	m := sizedModel(t, 80, 30)
	p := newPagerPopup(80)
	long := strings.TrimSpace(strings.Repeat("word ", 60)) // wraps over several lines
	p.lines = append([]contentLine{{text: long}}, p.lines...)

	first := boxLines(t, p, m)
	pagerPress(t, p, m, tea.KeyDown, 1)
	second := boxLines(t, p, m)
	shift := -1
	for i := range first {
		if i+1 < len(first) && strings.Contains(first[i], "word") && strings.Contains(first[i+1], "word") {
			shift = i
			break
		}
	}
	if shift < 0 {
		t.Fatalf("the long row did not wrap:\n%s", strings.Join(first, "\n"))
	}
	if !strings.Contains(second[shift], "word") {
		t.Fatalf("one ↓ jumped past the wrapped row instead of scrolling one line:\n%s", strings.Join(second, "\n"))
	}
	if pagerShown(t, p, m)[0] != "01" {
		t.Fatal("one ↓ scrolled more than one display line")
	}
}
