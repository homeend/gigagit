package tui

import (
	"testing"

	"github.com/homeend/gigagit/internal/agentdocs"
)

func srcLines(n int) []contentLine {
	out := make([]contentLine, n)
	for i := range out {
		out[i] = contentLine{text: "x", raw: "x", src: true}
	}
	return out
}

func anchorAt(path string, start, end int) agentdocs.Anchor {
	return agentdocs.Anchor{Path: path, Start: start, End: end}
}

// The stream: blank (not on the first file), header, rule, then the
// content or one placeholder; start/hdr/body stamped per file.
func TestFileStreamBuildStampsOffsets(t *testing.T) {
	t.Parallel()
	s := &fileStream{files: []streamFile{
		{path: "a.go", status: "M", notes: 2, lines: srcLines(3), loaded: true},
		{path: "b.go", status: "A"}, // not loaded: (loading…)
		{path: "c.go", status: "D", folded: true, loaded: true, lines: srcLines(5)},
	}}
	got := s.build()
	a, b, c := s.files[0], s.files[1], s.files[2]
	if a.start != 0 || a.hdr != 0 || a.body != 2 {
		t.Fatalf("a = %+v", a)
	}
	if b.start != 5 || b.hdr != 6 || b.body != 8 {
		t.Fatalf("b = %+v (blank 5, header 6, rule 7, placeholder 8)", b)
	}
	if c.start != 9 || c.hdr != 10 || c.body != 12 || len(got) != 13 {
		t.Fatalf("c = %+v len %d", c, len(got))
	}
	if got[a.hdr].text != "━━ a.go  (M)  ◆2" || !got[a.hdr].stackHeader {
		t.Fatalf("header = %q", got[a.hdr].text)
	}
	if got[b.body].text != "  (loading…)" || got[c.body].text != "  (folded)" || !got[a.body].src || got[b.body].src {
		t.Fatalf("placeholders: %q %q", got[b.body].text, got[c.body].text)
	}
}

func TestFileStreamMapsLinesBothWays(t *testing.T) {
	t.Parallel()
	s := &fileStream{files: []streamFile{
		{path: "a.go", lines: srcLines(3), loaded: true},
		{path: "b.go", lines: srcLines(2), loaded: true},
	}}
	s.build()
	for line, want := range map[int]int{0: 0, 2: 0, 4: 0, 5: 1, 6: 1, 8: 1, 9: 1} {
		if got := s.fileAt(line); got != want {
			t.Fatalf("fileAt(%d) = %d, want %d", line, got, want)
		}
	}
	if s.streamLine(1, 2) != 9 || s.streamLine(0, 1) != 2 {
		t.Fatalf("streamLine: %d %d", s.streamLine(1, 2), s.streamLine(0, 1))
	}
	if s.streamLine(1, 9) != 9 { // past the file's end: clamped to its last line
		t.Fatalf("clamp: %d", s.streamLine(1, 9))
	}
	if s.nextHeader(0, 1) != s.files[1].hdr || s.nextHeader(s.files[1].hdr, 1) != -1 || s.nextHeader(s.files[1].hdr, -1) != 0 {
		t.Fatal("nextHeader walks header to header")
	}
	if s.nextHeader(9, -1) != s.files[1].hdr || s.nextHeader(0, -1) != -1 {
		t.Fatal("P inside a file goes to its own header first; on the first header it stops")
	}
	s.files[1].loaded = false
	s.build()
	if s.streamLine(1, 2) != s.files[1].hdr {
		t.Fatal("an unloaded file's line is its header")
	}
}

func TestFileStreamBandsArePerFile(t *testing.T) {
	t.Parallel()
	ov := &openFile{p: &contentPopup{}, ov: &overview{anchors: []anchor{
		{dest: "a.go:2", target: anchorAt("a.go", 2, 0)},
		{dest: "b.go:1-2", target: anchorAt("b.go", 1, 2)},
		{dest: "b.go:9", target: anchorAt("b.go", 9, 0)}, // past b's end: dropped
	}}}
	s := &fileStream{files: []streamFile{
		{path: "a.go", lines: srcLines(3), loaded: true},
		{path: "b.go", lines: srcLines(2), loaded: true},
	}}
	d := &openFile{p: &contentPopup{lines: s.build()}, stream: s, from: ov, anchorCur: "b.go:1-2"}
	bs := d.bands()
	// Stream lines, 1-based: a.go's line 2 is index 3 (header 0, rule 1, line 1 at 2).
	if len(bs) != 2 || bs[0].start != 4 || bs[0].end != 4 || bs[1].start != 9 || bs[1].end != 10 {
		t.Fatalf("bands = %+v", bs)
	}
	if d.curBand(bs) != 1 {
		t.Fatalf("curBand = %d", d.curBand(bs))
	}
	if d.gutterW() == 0 {
		t.Fatal("a stream with bands has the mark gutter")
	}
}
