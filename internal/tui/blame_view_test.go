package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/syntax"
)

func TestGroupBlameCollapsesRuns(t *testing.T) {
	t.Parallel()
	lines := []model.BlameLine{
		{Hash: "aaa"}, {Hash: "aaa"}, {Hash: "bbb"}, {Hash: "aaa"},
	}
	blocks := groupBlame(lines)
	if len(blocks) != 3 {
		t.Fatalf("want 3 blocks (aaa,aaa | bbb | aaa), got %d: %+v", len(blocks), blocks)
	}
	if blocks[0].start != 0 || blocks[0].end != 1 || blocks[0].hash != "aaa" {
		t.Errorf("block 0 wrong: %+v", blocks[0])
	}
	if blocks[1].start != 2 || blocks[1].end != 2 || blocks[1].hash != "bbb" {
		t.Errorf("block 1 wrong: %+v", blocks[1])
	}
	if blocks[2].start != 3 || blocks[2].end != 3 {
		t.Errorf("block 2 wrong: %+v", blocks[2])
	}
}

func TestGroupBlameEdges(t *testing.T) {
	t.Parallel()
	if got := groupBlame(nil); len(got) != 0 {
		t.Errorf("empty input → no blocks, got %+v", got)
	}
	all := groupBlame([]model.BlameLine{{Hash: "x"}, {Hash: "x"}, {Hash: "x"}})
	if len(all) != 1 || all[0].start != 0 || all[0].end != 2 {
		t.Errorf("all-same → one block, got %+v", all)
	}
}

func TestBlockAt(t *testing.T) {
	t.Parallel()
	blocks := []blameBlock{{start: 0, end: 1, hash: "aaa"}, {start: 2, end: 2, hash: "bbb"}}
	if b, ok := blockAt(blocks, 1); !ok || b.hash != "aaa" {
		t.Errorf("line 1 should be in block aaa, got %+v ok=%v", b, ok)
	}
	if b, ok := blockAt(blocks, 2); !ok || b.hash != "bbb" {
		t.Errorf("line 2 should be in block bbb, got %+v ok=%v", b, ok)
	}
	if _, ok := blockAt(blocks, 9); ok {
		t.Error("out-of-range line should not match a block")
	}
}

func TestBlameAge(t *testing.T) {
	t.Parallel()
	now := time.Unix(1_000_000_000, 0)
	cases := []struct {
		ago  time.Duration
		want string
	}{
		{30 * time.Second, "now"},
		{5 * time.Minute, "5m"},
		{3 * time.Hour, "3h"},
		{2 * 24 * time.Hour, "2d"},
		{90 * 24 * time.Hour, "3mo"},
		{800 * 24 * time.Hour, "2y"},
	}
	for _, c := range cases {
		if got := blameAge(now, now.Add(-c.ago)); got != c.want {
			t.Errorf("blameAge(-%s) = %q, want %q", c.ago, got, c.want)
		}
	}
}

func blameFixture() *blameView {
	b := &blameView{
		ctx: navContext{path: "a.go", rev: ""},
		lines: []model.BlameLine{
			{Hash: "aaaaaaa", Author: "Ada", Time: 1, LineNo: 1, Content: "package main"},
			{Hash: "aaaaaaa", Author: "Ada", Time: 1, LineNo: 2, Content: "func main() {}"},
			{Hash: "", Author: "Not Committed Yet", LineNo: 3, Content: "dirty"},
		},
	}
	b.blocks = groupBlame(b.lines)
	return b
}

func TestBlameRenderGutterFirstLineOnly(t *testing.T) {
	t.Parallel()
	m := Model{width: 100, height: 30}
	b := blameFixture()
	out := b.render(m, "")
	if !contains(out, "package main") || !contains(out, "func main()") {
		t.Errorf("blame render missing source lines:\n%s", out)
	}
	if !contains(out, "a.go") {
		t.Errorf("blame header missing path:\n%s", out)
	}
	if !contains(out, "Ada") {
		t.Errorf("gutter missing author on the block's first line:\n%s", out)
	}
	if !contains(out, "uncommitted") {
		t.Errorf("uncommitted block should show an (uncommitted) gutter:\n%s", out)
	}
}

func TestBlameDownMovesCursorClamped(t *testing.T) {
	t.Parallel()
	m := Model{width: 100, height: 30}
	b := blameFixture()
	m = m.pushLayer(b)
	for i := 0; i < 5; i++ {
		m, _ = b.update(m, keyMsg("j"))
	}
	if b.sel != len(b.lines)-1 {
		t.Fatalf("j should clamp at the last line %d, got %d", len(b.lines)-1, b.sel)
	}
}

func TestBlameEnterOnCommitPushesHistory(t *testing.T) {
	t.Parallel()
	m := Model{width: 100, height: 30}
	b := blameFixture()
	m = m.pushLayer(b)
	b.sel = 0 // a committed block (aaaaaaa)
	m, cmd := b.update(m, keyMsg("enter"))
	h, ok := m.topLayer().(*historyView)
	if !ok {
		t.Fatal("enter on a committed block should push a historyView")
	}
	if h.ctx.path != "a.go" || h.ctx.rev != "aaaaaaa" {
		t.Errorf("history navContext wrong: %+v", h.ctx)
	}
	if cmd == nil {
		t.Error("pushing history should fire its list-load cmd")
	}
}

func TestBlameEnterOnUncommittedIsNoop(t *testing.T) {
	t.Parallel()
	m := Model{width: 100, height: 30}
	b := blameFixture()
	m = m.pushLayer(b)
	b.sel = 2 // the uncommitted block
	m, _ = b.update(m, keyMsg("enter"))
	if _, ok := m.topLayer().(*blameView); !ok {
		t.Fatal("enter on an uncommitted block should be a no-op (stay on blame)")
	}
}

func TestBlameEscAndBPop(t *testing.T) {
	t.Parallel()
	for _, key := range []string{"esc", "b"} {
		m := Model{width: 100, height: 30}
		b := blameFixture()
		m = m.pushLayer(b)
		m, _ = b.update(m, keyMsg(key))
		if m.topLayer() != nil {
			t.Fatalf("%q should pop the blame surface", key)
		}
	}
}

// q no longer quits from the blame view — only the base layout quits on q.
func TestBlameQInert(t *testing.T) {
	t.Parallel()
	m := Model{width: 100, height: 30}
	b := blameFixture()
	m = m.pushLayer(b)
	m, cmd := b.update(m, keyMsg("q"))
	if cmd != nil {
		t.Fatal("q must not quit from the blame view (inert)")
	}
	if m.topLayer() == nil {
		t.Fatal("q must leave the blame surface on the stack")
	}
}

func TestStatusBOpensBlame(t *testing.T) {
	t.Parallel()
	m := Model{width: 100, height: 30, focus: panelFiles, sel: map[panel]int{}}
	m.status = model.WorkingTreeStatus{Files: []model.FileStatus{{Path: "a.go", Unstaged: 'M'}}}
	mm, _ := m.Update(keyMsg("b"))
	got := mm.(Model)
	bv, ok := got.topLayer().(*blameView)
	if !ok {
		t.Fatal("b on a Status file should push a blameView")
	}
	if bv.ctx.path != "a.go" || bv.ctx.rev != "" {
		t.Errorf("wrong navContext: %+v", bv.ctx)
	}
}

func TestFilesViewBOpensBlame(t *testing.T) {
	t.Parallel()
	m := Model{width: 100, height: 30}
	m.filesView = &contentPopup{lines: []contentLine{{text: "a.go", path: "a.go"}}}
	m.filesTreeFocused = true
	m.filesHash = "abc123"
	mm, _ := m.updateFilesViewKey(keyMsg("b"))
	bv, ok := mm.(Model).topLayer().(*blameView)
	if !ok {
		t.Fatal("b on a files-view row should push a blameView")
	}
	if bv.ctx.path != "a.go" || bv.ctx.rev != "abc123" {
		t.Errorf("wrong navContext: %+v", bv.ctx)
	}
}

func TestDiffViewBOpensBlame(t *testing.T) {
	t.Parallel()
	m := Model{width: 100, height: 30}
	m = m.pushLayer(&diffView{title: "a.go", rev: "abc123"})
	mm, _ := m.updateDiffViewKey(keyMsg("b"))
	bv, ok := mm.(Model).topLayer().(*blameView)
	if !ok {
		t.Fatal("b in the diff view should push a blameView")
	}
	if bv.ctx.path != "a.go" || bv.ctx.rev != "abc123" {
		t.Errorf("wrong navContext: %+v", bv.ctx)
	}
}

func TestHistoryBOpensBlameAtSelected(t *testing.T) {
	t.Parallel()
	m := Model{width: 100, height: 30}
	h := &historyView{
		ctx: navContext{path: "a.go", rev: ""},
		commits: []model.FileCommit{
			{Commit: model.Commit{Hash: "aaaaaaa", Subject: "edit"}, Status: "M", Path: "a.go"},
			{Commit: model.Commit{Hash: "bbbbbbb", Subject: "add"}, Status: "A", Path: "a.go"},
		},
		sel: 1,
	}
	m = m.pushLayer(h)
	m, cmd := h.update(m, keyMsg("b"))
	bv, ok := m.topLayer().(*blameView)
	if !ok {
		t.Fatal("b in history should push a blameView")
	}
	if bv.ctx.path != "a.go" || bv.ctx.rev != "bbbbbbb" {
		t.Errorf("blame should target the selected commit, got %+v", bv.ctx)
	}
	if cmd == nil {
		t.Error("pushing blame should fire its load cmd")
	}
}

func TestBlamePageDownPageUpClamped(t *testing.T) {
	t.Parallel()
	m := Model{width: 100, height: 12} // small body so a page is a few rows
	lines := make([]model.BlameLine, 40)
	for i := range lines {
		lines[i] = model.BlameLine{Hash: "aaaaaaa", Author: "Ada", LineNo: i + 1, Content: "x"}
	}
	b := &blameView{ctx: navContext{path: "a.go"}, lines: lines, blocks: groupBlame(lines)}
	m = m.pushLayer(b)
	page := m.blameBodyRows()

	m, _ = b.update(m, keyMsg("pgdown"))
	if b.sel != page {
		t.Fatalf("pgdown should move one page (%d), got %d", page, b.sel)
	}
	// Page down past the end clamps to the last line.
	for i := 0; i < 100; i++ {
		m, _ = b.update(m, keyMsg("pgdown"))
	}
	if b.sel != len(lines)-1 {
		t.Fatalf("pgdown should clamp at last line %d, got %d", len(lines)-1, b.sel)
	}
	// Page up past the top clamps to 0.
	for i := 0; i < 100; i++ {
		m, _ = b.update(m, keyMsg("pgup"))
	}
	if b.sel != 0 {
		t.Fatalf("pgup should clamp at line 0, got %d", b.sel)
	}
}

func TestBlameRenderRowsFullWidthAndSanitized(t *testing.T) {
	t.Parallel()
	m := Model{width: 80, height: 12}
	lines := []model.BlameLine{
		{Hash: "aaaaaaa", Author: "Ada", LineNo: 1, Content: "\tindented with a tab"},
		{Hash: "aaaaaaa", Author: "Ada", LineNo: 2, Content: "short"},
	}
	b := &blameView{ctx: navContext{path: "a.go"}, lines: lines, blocks: groupBlame(lines)}
	out := b.render(m, "")
	if strings.Contains(out, "\t") {
		t.Error("blame content must be tab-sanitized; a raw tab leaked into the output")
	}
	for _, ln := range strings.Split(out, "\n") {
		if strings.Contains(ln, "│") && lipgloss.Width(ln) != 80 {
			t.Errorf("content row not full-width (%d): %q", lipgloss.Width(ln), ln)
		}
	}
}

func TestBlameViewWrapMode(t *testing.T) {
	t.Parallel()
	b := &blameView{
		ctx:   navContext{path: "x"},
		lines: []model.BlameLine{{Hash: "abcdef0", Author: "a", Content: strings.Repeat("y", 200)}},
		mode:  modeWrap,
	}
	b.blocks = groupBlame(b.lines)
	m := Model{width: 80, height: 24}
	out := b.render(m, "")
	if strings.Count(out, "y") < 100 {
		t.Errorf("blame wrap mode did not expand the long code line:\n%s", out)
	}
}

// In wrap mode the gutter (hash/author/age) is a frozen left column: it shows on
// the block's first line only, and wrapped continuation lines stay clear of it —
// the long content must not bleed across the author column.
func TestBlameWrapFreezesGutter(t *testing.T) {
	t.Parallel()
	b := &blameView{
		ctx:   navContext{path: "x"},
		lines: []model.BlameLine{{Hash: "abcdef0", Author: "Zoe", Time: 1, LineNo: 1, Content: strings.Repeat("y", 200)}},
		mode:  modeWrap,
	}
	b.blocks = groupBlame(b.lines)
	m := Model{width: 80, height: 24}
	out := b.render(m, "")
	if strings.Count(out, "y") < 100 {
		t.Fatalf("wrap did not expand the long line:\n%s", out)
	}
	// The author rides only the first display line of the block.
	if n := strings.Count(out, "Zoe"); n != 1 {
		t.Errorf("author must appear exactly once (frozen gutter), got %d:\n%s", n, out)
	}
	// A wrapped continuation line carries content but no gutter, so its gutter
	// columns must be blank — content may not start at column 0.
	for _, ln := range strings.Split(out, "\n") {
		if strings.Count(ln, "y") > 5 && !strings.Contains(ln, "abcdef0") {
			if !strings.HasPrefix(ln, strings.Repeat(" ", 25)) {
				t.Errorf("wrapped continuation bled into the gutter column: %q", ln)
			}
		}
	}
}

// In scroll mode the gutter stays put while only the content pans; panning right
// must not slide the author column off the left edge.
func TestBlameScrollFreezesGutter(t *testing.T) {
	t.Parallel()
	b := &blameView{
		ctx:   navContext{path: "x"},
		lines: []model.BlameLine{{Hash: "abcdef0", Author: "Zoe", Time: 1, LineNo: 1, Content: strings.Repeat("y", 200)}},
		mode:  modeScroll,
	}
	b.blocks = groupBlame(b.lines)
	m := Model{width: 80, height: 24}
	b.hscroll = 40 // pan the content well to the right
	out := b.render(m, "")
	if !strings.Contains(out, "Zoe") {
		t.Errorf("scroll mode must keep the gutter author fixed while panning content:\n%s", out)
	}
}

// blameHugeFixture builds a 5000-line blame view: hash blocks of varying
// length (so the gutter's "first line of a block" text appears on scattered
// rows, not just row 0), lines of varying length (short and >100 cols, so
// wrap/scroll modes actually do work), and syntax tokens on every other line
// (so the coloured winRow path is exercised alongside the plain one). All
// Time fields are 0 (age reads as a huge, stable "Ny" no matter how many
// microseconds separate two independent time.Now() calls in this test).
func blameHugeFixture() *blameView {
	const n = 5000
	lines := make([]model.BlameLine, n)
	tok := make([][]syntax.Tok, n)
	hash := ""
	for i := 0; i < n; i++ {
		if i%7 == 0 { // new block roughly every 7 lines, varying since 7∤most windows
			hash = fmt.Sprintf("%07x", i)
		}
		content := fmt.Sprintf("line %d short", i)
		if i%5 == 0 {
			content = fmt.Sprintf("line %d %s", i, strings.Repeat("x", 150)) // forces wrap/scroll to act
		}
		lines[i] = model.BlameLine{Hash: hash, Author: "Ada", Time: 0, LineNo: i + 1, Content: content}
		if i%2 == 0 {
			tok[i] = []syntax.Tok{{Start: 0, End: 4, Class: syntax.Keyword}}
		}
	}
	b := &blameView{ctx: navContext{path: "a.go"}, lines: lines, tok: tok}
	b.blocks = groupBlame(lines)
	return b
}

// blameRowsFull rebuilds a winRow for EVERY line the pre-optimisation code
// would have (finding 2's "old way"): no windowRowBounds pre-slice, full
// b.lines range, full-index gutter lookback. Mirrors render's per-row loop
// exactly, parameterised on gw/now so the reference build uses the same
// values render's own call would.
func blameRowsFull(b *blameView, gw int, now time.Time) []winRow {
	wr := make([]winRow, len(b.lines))
	s := st()
	for i, ln := range b.lines {
		gutter := padRight("", gw)
		if i == 0 || b.lines[i-1].Hash != ln.Hash {
			gutter = padRight(truncate(blameGutterText(ln, now), gw), gw)
		}
		var st lipgloss.Style
		if i == b.sel {
			st = s.selectedRow
		}
		if b.tok == nil {
			wr[i] = winRow{prefix: gutter + "│", text: sanitizeLine(ln.Content), style: st}
			continue
		}
		disp, _, cls := sanitizeCell(ln.Content, nil, tokAt(b.tok, i+1))
		wr[i] = winRow{prefix: gutter + "│", text: string(disp), cls: cls, style: st}
	}
	return wr
}

// blameBodyLines extracts the body (window) lines from a full b.render(m,"")
// output: everything between the one-line header and the one-line hint.
func blameBodyLines(t *testing.T, out string, body int) []string {
	t.Helper()
	all := strings.Split(out, "\n")
	if len(all) != body+2 {
		t.Fatalf("render produced %d lines, want header+%d body+hint=%d:\n%q", len(all), body, body+2, out)
	}
	return all[1 : 1+body]
}

// TestBlameRenderMatchesFullBuildRegardlessOfWindow is finding 2's pin: on a
// 5000-line blame, render's optimised per-visible-row build must be
// byte-identical to building a winRow for every line (the old way) and
// letting renderWindow do all the windowing itself — across cutoff, wrap,
// and scroll, with the cursor at the start, middle, and end of the file.
func TestBlameRenderMatchesFullBuildRegardlessOfWindow(t *testing.T) {
	t.Parallel()
	b := blameHugeFixture()
	m := Model{width: 100, height: 30}
	w, _ := m.overlayDims()
	body := m.blameBodyRows()
	gw := blameGutterW
	if gw > w-10 {
		gw = w - 10
	}

	n := len(b.lines)
	sels := []int{0, n / 2, n - 1}
	modes := []dispMode{modeCutoff, modeWrap, modeScroll}
	for _, mode := range modes {
		for _, sel := range sels {
			t.Run(fmt.Sprintf("mode=%d/sel=%d", mode, sel), func(t *testing.T) {
				b.mode = mode
				b.sel = sel
				b.hscroll = 0
				if mode == modeScroll {
					b.hscroll = 12
				}

				now := time.Now()
				got := blameBodyLines(t, b.render(m, ""), body)
				full := blameRowsFull(b, gw, now)
				want := renderWindow(full, winOpts{w: w, h: body, mode: mode, anchor: sel, hscroll: b.hscroll, prefixW: gw + 1})

				if len(got) != len(want) {
					t.Fatalf("body line count %d != reference %d", len(got), len(want))
				}
				for i := range want {
					if got[i] != want[i] {
						t.Errorf("line %d differs:\n got  %q\n want %q", i, got[i], want[i])
					}
				}
			})
		}
	}
}

// A file with no HEAD version has nothing to blame: an untracked file makes
// git blame fail outright ("no such path in HEAD"), and a staged-new file
// would blame every line as "Not Committed Yet". Both Files-panel rows render
// the A attribute, so b is inert on them (and h likewise has no history).
func TestStatusBIgnoresFilesNotInHEAD(t *testing.T) {
	t.Parallel()
	for name, f := range map[string]model.FileStatus{
		"untracked":  {Path: "new.txt", Kind: model.KindUntracked},
		"staged-new": {Path: "new.txt", Staged: 'A', Unstaged: '.'},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			m := Model{width: 100, height: 30, focus: panelFiles, sel: map[panel]int{}}
			m.status = model.WorkingTreeStatus{Files: []model.FileStatus{f}}
			if m.canBlameFile() {
				t.Error("canBlameFile should be false for a file not in HEAD")
			}
			mm, _ := m.Update(keyMsg("b"))
			if _, ok := mm.(Model).topLayer().(*blameView); ok {
				t.Fatal("b on a file not in HEAD must not push a blameView")
			}
		})
	}
	// The gate does not lose the tracked case.
	m := Model{width: 100, height: 30, focus: panelFiles, sel: map[panel]int{}}
	m.status = model.WorkingTreeStatus{Files: []model.FileStatus{{Path: "a.go", Unstaged: 'M'}}}
	if !m.canBlameFile() {
		t.Error("canBlameFile should be true for a tracked modified file")
	}
}
