package tui

import (
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/gigagit/gg/internal/textdiff"
)

var errFake = errors.New("boom")

func itoa(i int) string { return strconv.Itoa(i) }

func renderModelWithDiff(v *diffView) Model {
	m := diffModel()
	m.width = 100
	m.height = 20
	m.diffView = v
	m.diffTag = "status:x"
	return m
}

func TestRenderDiffViewStates(t *testing.T) {
	cases := []struct {
		name string
		v    *diffView
		want string
	}{
		{"loading", &diffView{title: "f.txt", context: "HEAD → working tree", loading: true}, "(loading…)"},
		{"binary", &diffView{title: "f.bin", binary: true}, "(binary file)"},
		{"too large", &diffView{title: "huge", tooLarge: true}, "(file too large)"},
		{"error", &diffView{title: "f", err: errFake}, "error:"},
	}
	for _, c := range cases {
		m := renderModelWithDiff(c.v)
		out := ansi.Strip(m.render())
		if !strings.Contains(out, c.want) {
			t.Fatalf("%s: rendered view missing %q:\n%s", c.name, c.want, out)
		}
	}
}

func TestRenderDiffViewPanes(t *testing.T) {
	res := textdiff.Compare([]byte("same\nold line\n"), []byte("same\nnew line\n"))
	v := &diffView{title: "f.txt", context: "HEAD → working tree", rows: res.Rows, blocks: res.Blocks}
	m := renderModelWithDiff(v)
	out := ansi.Strip(m.render())
	lines := strings.Split(out, "\n")
	for i, l := range lines {
		if lipgloss.Width(l) > m.width {
			t.Fatalf("line %d wider than terminal (%d): %q", i, lipgloss.Width(l), l)
		}
	}
	var found bool
	for _, l := range lines {
		if li := strings.Index(l, "│"); li >= 0 {
			left, right := l[:li], l[li:]
			if strings.Contains(left, "old line") && strings.Contains(right, "new line") {
				found = true
			}
			if strings.Contains(left, "new line") || strings.Contains(right, "old line") {
				t.Fatalf("sides swapped: %q", l)
			}
		}
	}
	if !found {
		t.Fatalf("no pane row with old|new pair:\n%s", out)
	}
	if !strings.Contains(out, "f.txt") || !strings.Contains(out, "HEAD → working tree") {
		t.Fatalf("header incomplete:\n%s", lines[0])
	}
	if !strings.Contains(out, "[esc] close") {
		t.Fatalf("hint line missing:\n%s", out)
	}
}

func TestRenderDiffViewTabsStayInPane(t *testing.T) {
	// Tab-indented content (every Go file): tabs must be expanded, never
	// rendered raw — a raw \t would let the terminal push text through the
	// separator.
	res := textdiff.Compare([]byte("\tindented\n"), []byte("\tindented changed\n"))
	v := &diffView{title: "f.go", rows: res.Rows, blocks: res.Blocks}
	m := renderModelWithDiff(v)
	out := m.render()
	if strings.Contains(out, "\t") {
		t.Fatal("rendered diff contains a raw tab")
	}
}

func TestRenderDiffViewNoContentDifferenceNote(t *testing.T) {
	res := textdiff.Compare([]byte("a\n"), []byte("a\n"))
	v := &diffView{title: "f", context: "@ abc1234", rows: res.Rows, blocks: res.Blocks}
	m := renderModelWithDiff(v)
	out := ansi.Strip(m.render())
	if !strings.Contains(out, "(no content difference)") {
		t.Fatalf("empty-blocks diff must explain itself:\n%s", out)
	}
}

func TestRenderDiffViewTruncatedNote(t *testing.T) {
	v := &diffView{title: "f", truncated: true, rows: []textdiff.Row{{Kind: textdiff.Del, Left: "x", LeftNo: 1}}, blocks: []int{0}}
	m := renderModelWithDiff(v)
	out := ansi.Strip(m.render())
	if !strings.Contains(out, "alignment skipped") {
		t.Fatalf("truncated diff must carry the note:\n%s", out)
	}
}

func TestRenderDiffViewScrollWindow(t *testing.T) {
	rows := make([]textdiff.Row, 100)
	for i := range rows {
		rows[i] = textdiff.Row{Kind: textdiff.Same, Left: "L" + itoa(i), Right: "L" + itoa(i), LeftNo: i + 1, RightNo: i + 1}
	}
	v := &diffView{title: "f", rows: rows, offset: 50}
	m := renderModelWithDiff(v)
	out := ansi.Strip(m.render())
	if !strings.Contains(out, "L50") || strings.Contains(out, "L10 ") {
		t.Fatalf("offset window not applied:\n%s", out)
	}
}
