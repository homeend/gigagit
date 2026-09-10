# Conflict-Resolver Syntax Colouring Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give the hunk picker (conflict resolver + hunk staging/unstaging) the same per-token syntax colouring the diff pane, blame view and file preview already have.

**Architecture:** The picker renders through the two-column cell primitive (`winCell`/`renderTwoCol`), which phase 4's colouring never reached. We (a) give `winCell` a generic post-slice paint mask (`runMask{cls, emph}`) that `renderTwoCol` slices alongside the body in all three long-line modes and paints with the existing `styledRuns`, exactly as `renderWindow` does for `winRow.cls`; (b) when the picker opens, assemble the two full-file texts its document describes — literal context + every block's `Current` (the current side), the same context + every block's `Incoming` (the incoming side) — lex each once with `syntax.Detect`/`syntax.Lex` under the `[ui] diff_syntax` switch and the `domain.MaxSyntaxBytes` cap, and index the runs so every document line maps to its own side's full-file line number; (c) build the per-line masks once in the existing `ensureSan` cache and let the output pane reuse the very same `sanLine` values (no re-sanitize, no re-lex per pick).

**Tech Stack:** Go 1.26, Bubble Tea + lipgloss, `internal/syntax` (chroma), `internal/hunkpick`, `internal/tui`.

**Spec:** `docs/superpowers/specs/2026-09-08-hunk-parity-roadmap.md` — §4.2 (lines 328–344) is binding; §4.3 (lines 346–376) describes the phase-6 search painter that must reuse this same post-slice mechanism; §6's phase-5 row (~line 1036) is the roadmap entry.

## Global Constraints

- **Worktree.** All work happens in `/mnt/t/others/gigagit.worktrees/feat-conflict-syntax` (branch `feat/conflict-syntax`, base main `eb759989`). The shell cwd resets to the main checkout between commands: prefix EVERY command with `cd /mnt/t/others/gigagit.worktrees/feat-conflict-syntax &&`, use `git -C <abs>` for git, and pass absolute paths to Write/Edit.
- **TDD.** Write the failing test first, run it, watch it fail for the stated reason, then implement.
- Tests use a real `git` in a `t.TempDir()` (see `newRepo`/`newTestRepo` helpers) or pure fixtures; TUI tests call `t.Parallel()` and inject state (never `t.Setenv`).
- **Carve-out to the `t.Parallel()` rule:** any test that calls `lipgloss.SetColorProfile` MUST NOT call `t.Parallel()` — the colour profile is process-global and a parallel sibling's deferred reset lands mid-render and drops the ANSI codes the test asserts on. `internal/tui/window_syntax_test.go` carries this note verbatim at the top of the file; copy it into every new test file that sets the profile.
- `internal/tui` never imports `internal/git` (enforced by `internal/archtest`); `internal/syntax` and `internal/hunkpick` are DAG leaves and must stay so (`hunkpick` imports only stdlib + `internal/textdiff`).
- No new config keys. No new user-visible TUI strings — if one is unavoidable it needs a literal `i18n.T` key present in all four bundles (ja/ko/zh/ru); this plan adds none.
- **Every commit:** `cd` into the worktree, `git add` the named files ONLY (never `git add -A`), and end the message with the two trailers:

```
Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01NDk1DJtgLzZX7hMmxDs9nU
```

- `CHANGELOG.md` contains a literal `<<<<<<<` line further down the file — it is NOT a merge marker. Add the new entry by line number, right under the `## [Unreleased]` heading at `CHANGELOG.md:9`.
- Build/test: `go build ./cmd/gg`, `./test.sh unit`, `./test.sh race`. Run `./test.sh race` before declaring done — do not wait for other sessions.

---

### Task 1: A generic post-slice paint mask on the two-column cell

The picker's cells are laid out by `cellSegs` and painted by `styleCell`, neither of which knows about syntax runs. Give `winCell` a mask that survives the cutoff/scroll/wrap slicing, and paint it with the same `styledRuns` painter `renderWindow` uses. The mask carries BOTH a class array and an emphasis array so phase 6's in-view search painter (spec §4.3) can reuse this entry point unchanged.

**Files:**
- Modify: `internal/tui/twocol.go:9-17` (`winCell`), `:39-72` (`cellSegs`), `:87-202` (`renderTwoCol`)
- Test: `internal/tui/twocol_syntax_test.go` (create)

**Interfaces:**
- Consumes: `styledRuns(disp []rune, emph []bool, cls []syntax.Class, base lipgloss.Style) string` (`internal/tui/diff_render.go:713`); `hslice`, `truncate`, `wrapWidth`, `padRight`, `styleCell`, `hscrollRuneOff` (`internal/tui/window.go:281`); `syntax.Class`.
- Produces:
  - `type runMask struct { cls []syntax.Class; emph []bool }`
  - `func (m runMask) empty() bool`
  - `func (m runMask) slice(off, n int) runMask`
  - `func (m runMask) pad(n int) runMask`
  - `winCell.mask runMask` (new field; zero value = today's plain path)
  - `type cellPiece struct { pre, body string; mask runMask }`
  - `func cellPieces(c *winCell, width int, mode dispMode, hscroll int) []cellPiece`
  - `func pieceOrBlank(ps []cellPiece, k int) cellPiece`
  - `func renderPiece(style lipgloss.Style, p cellPiece, w int) string`
  - `func wrapSegMasks(body string, m runMask, segs []string) []runMask`
  - `cellSegs` and `segOrBlank` keep their exact current signatures and behaviour (`internal/tui/twocol_window_test.go:45-46` calls `segOrBlank`).

- [ ] **Step 1: Write the failing tests**

Create `/mnt/t/others/gigagit.worktrees/feat-conflict-syntax/internal/tui/twocol_syntax_test.go`:

```go
package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/homeend/gigagit/internal/syntax"
)

// NOTE: none of the tests in this file call t.Parallel() —
// lipgloss.SetColorProfile is process-global, so a parallel sibling's deferred
// reset would land mid-render and drop the ANSI codes these assert on (the
// same rule window_syntax_test.go follows).

// goFuncMask is goFuncCls (window_syntax_test.go) as a cell paint mask: the
// class run for "func main() {" with no emphasis.
func goFuncMask() runMask {
	cls := goFuncCls()
	return runMask{cls: cls, emph: make([]bool, len(cls))}
}

// An all-Plain mask must render byte-identically to no mask at all. (A file
// with no lexer takes the empty-mask path instead, so it never reaches here —
// this pins the case where the lexer ran but a given line carries no runs and
// something upstream still hands down a full Plain array.)
func TestTwoColPlainMaskIsByteIdenticalToNoMask(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(prev)

	const text = "    return foo(bar) and a good deal more text besides"
	n := len([]rune(text))
	pm := runMask{cls: make([]syntax.Class, n), emph: make([]bool, n)}
	for _, mode := range []dispMode{modeCutoff, modeWrap, modeScroll} {
		o := twoColOpts{w: 40, h: 4, sep: " ║ ", mode: mode, hscroll: 3}
		want, _ := renderTwoCol([]colRow{{
			left:  &winCell{gutter: "[ ] ", body: text},
			right: &winCell{gutter: "[ ] ", body: "z"},
		}}, o)
		got, _ := renderTwoCol([]colRow{{
			left:  &winCell{gutter: "[ ] ", body: text, mask: pm},
			right: &winCell{gutter: "[ ] ", body: "z"},
		}}, o)
		if strings.Join(got, "\n") != strings.Join(want, "\n") {
			t.Errorf("mode %d: a plain mask changed the render\n got %q\nwant %q", mode, got, want)
		}
	}
}

func TestTwoColMaskColoursCutoff(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(prev)

	out, _ := renderTwoCol([]colRow{{
		left:  &winCell{gutter: "[ ] ", body: "func main() {", mask: goFuncMask()},
		right: &winCell{},
	}}, twoColOpts{w: 43, h: 1, sep: " ║ ", mode: modeCutoff})
	// colW = (43-3)/2 = 20; gutter 4 → bodyW 16, so the body fits whole.
	if got := ansi.Strip(out[0]); !strings.HasPrefix(got, "[ ] func main() {") {
		t.Fatalf("visible text changed: %q", got)
	}
	if !strings.Contains(out[0], kwSeq()+"mfunc") {
		t.Errorf("`func` should wear the keyword colour: %q", out[0])
	}
	if !strings.Contains(out[0], fnSeq()+"mmain") {
		t.Errorf("`main` should wear the func colour: %q", out[0])
	}
	if strings.Contains(out[0], kwSeq()+"m[") || strings.Contains(out[0], fnSeq()+"m[") {
		t.Errorf("the gutter must never be painted: %q", out[0])
	}
}

func TestTwoColMaskColoursScrolledSegment(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(prev)

	out, _ := renderTwoCol([]colRow{{
		left:  &winCell{gutter: "", body: "func main() {", mask: goFuncMask()},
		right: &winCell{},
	}}, twoColOpts{w: 43, h: 1, sep: " ║ ", mode: modeScroll, hscroll: 5})
	if !strings.Contains(out[0], fnSeq()+"mmain") {
		t.Errorf("`main` should stay coloured after the scroll: %q", out[0])
	}
	if strings.Contains(out[0], kwSeq()) {
		t.Errorf("the scrolled-off keyword must not colour anything: %q", out[0])
	}
}

// A run that lands on a wrap continuation is coloured there.
func TestTwoColMaskColoursWrapContinuation(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(prev)

	// colW = (25-3)/2 = 11; gutter 4 → bodyW 7, so wrapWidth splits
	// "func main() {" into "func ma" + "in() {" — main's Func run straddles.
	out, _ := renderTwoCol([]colRow{{
		left:  &winCell{gutter: "[ ] ", body: "func main() {", mask: goFuncMask()},
		right: &winCell{},
	}}, twoColOpts{w: 25, h: 2, sep: " ║ ", mode: modeWrap})
	if len(out) != 2 {
		t.Fatalf("want 2 lines, got %d", len(out))
	}
	if !strings.Contains(out[0], kwSeq()+"mfunc") {
		t.Errorf("first line should colour `func`: %q", out[0])
	}
	if !strings.Contains(out[1], fnSeq()+"min") {
		t.Errorf("continuation should colour main's tail `in`: %q", out[1])
	}
}

// truncate keeps a prefix and APPENDS "…", so an unfixed mask lands the
// ellipsis on the class of the first DROPPED rune.
func TestTwoColMaskEllipsisIsPlain(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(prev)

	// colW = (27-3)/2 = 12; gutter 4 → bodyW 8: keeps "func ma" + "…" and the
	// first dropped rune ('i') sits inside main's Func run.
	out, _ := renderTwoCol([]colRow{{
		left:  &winCell{gutter: "[ ] ", body: "func main() {", mask: goFuncMask()},
		right: &winCell{},
	}}, twoColOpts{w: 27, h: 1, sep: " ║ ", mode: modeCutoff})
	if got := ansi.Strip(out[0]); !strings.HasPrefix(got, "[ ] func ma…") {
		t.Fatalf("cutoff = %q, want prefix %q", got, "[ ] func ma…")
	}
	if strings.Contains(out[0], fnSeq()+"mma…") {
		t.Errorf("the ellipsis must not fuse into main's colour run: %q", out[0])
	}
}

// A reverse-video cell style (the picker's cursor row) drops the mask —
// reverse swaps fg/bg, so per-token foregrounds would paint per-token
// BACKGROUNDS. This is what keeps the cursor row plain.
func TestTwoColMaskDroppedUnderReverseStyle(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(prev)

	rev := lipgloss.NewStyle().Reverse(true)
	o := twoColOpts{w: 43, h: 1, sep: " ║ ", mode: modeCutoff}
	got, _ := renderTwoCol([]colRow{{
		left:  &winCell{gutter: "[ ] ", body: "func main() {", mask: goFuncMask(), style: rev},
		right: &winCell{},
	}}, o)
	want, _ := renderTwoCol([]colRow{{
		left:  &winCell{gutter: "[ ] ", body: "func main() {", style: rev},
		right: &winCell{},
	}}, o)
	if got[0] != want[0] {
		t.Errorf("a reverse-video cell must render exactly as the unmasked one\n got %q\nwant %q", got[0], want[0])
	}
	if strings.Contains(got[0], kwSeq()) {
		t.Errorf("no syntax colour may appear on a reverse-video cell: %q", got[0])
	}
}

// cellSegs keeps its exact contract: pre+body concatenated, one entry per
// display segment (twocol_window_test.go depends on it).
func TestCellSegsStillConcatenatesPieces(t *testing.T) {
	c := &winCell{gutter: "[x] ", body: "aaa bbb ccc", mask: runMask{}}
	segs := cellSegs(c, 10, modeWrap, 0)
	ps := cellPieces(c, 10, modeWrap, 0)
	if len(segs) != len(ps) {
		t.Fatalf("cellSegs %d segments, cellPieces %d", len(segs), len(ps))
	}
	for i := range segs {
		if segs[i] != ps[i].pre+ps[i].body {
			t.Errorf("seg %d = %q, want %q", i, segs[i], ps[i].pre+ps[i].body)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-conflict-syntax && go test ./internal/tui -run 'TwoColMask|TwoColPlainMask|CellSegsStill' -v`
Expected: FAIL to compile — `unknown field mask in struct literal of type winCell`, `undefined: runMask`, `undefined: cellPieces`.

- [ ] **Step 3: Implement the mask, the pieces and the painter**

In `/mnt/t/others/gigagit.worktrees/feat-conflict-syntax/internal/tui/twocol.go`, add `"github.com/homeend/gigagit/internal/syntax"` to the import block, then replace the `winCell` declaration (lines 9-17) with:

```go
// runMask is a per-display-rune paint mask for a winCell body: a syntax class
// per rune plus an emphasis flag per rune. The zero value — both nil — is the
// plain path every uncoloured cell takes. It is deliberately generic rather
// than syntax-specific: phase 6's in-view search paints its hits through the
// same entry point by filling emph (spec §4.3).
type runMask struct {
	cls  []syntax.Class
	emph []bool
}

// empty reports the plain path (no runs to paint).
func (m runMask) empty() bool { return len(m.cls) == 0 && len(m.emph) == 0 }

// slice returns n runes of the mask starting at off, padding with Plain/false
// where a side runs out (a cutoff ellipsis, a clamped token end) and reading
// an out-of-range window as fully plain. A masked cell always yields n entries
// on BOTH sides so styledRuns can index either without a bounds check.
//
// It always ALLOCATES: a cell's mask is a shared cache value (the picker hands
// the same sanLine to the grid and to the output pane), so the layout must
// never write through to it — cellPieces' ellipsis fix relies on that.
func (m runMask) slice(off, n int) runMask {
	if m.empty() || n <= 0 {
		return runMask{}
	}
	if off < 0 {
		off = 0
	}
	out := runMask{cls: make([]syntax.Class, n), emph: make([]bool, n)}
	for i := 0; i < n; i++ {
		j := off + i
		if j < len(m.cls) {
			out.cls[i] = m.cls[j]
		}
		if j < len(m.emph) {
			out.emph[i] = m.emph[j]
		}
	}
	return out
}

// pad returns the mask with n leading plain runes — for a body that carries a
// fixed indent the lexer's runs do not cover (the picker's literal rows).
func (m runMask) pad(n int) runMask {
	if m.empty() || n <= 0 {
		return m
	}
	return runMask{
		cls:  append(make([]syntax.Class, n), m.cls...),
		emph: append(make([]bool, n), m.emph...),
	}
}

// winCell is one cell of a two-column window: a fixed gutter (shown verbatim,
// never transformed — the cursor marker + checkbox live here) and a body the
// display mode transforms. style is applied to the whole padded cell after
// slicing, so width math stays ANSI-safe. The zero value is a blank cell.
//
// mask is an optional paint mask over the body's DISPLAY runes (so
// len(mask.cls) == len([]rune(body))). The layout slices it alongside the body
// in all three modes, so a coloured run lands on the right columns after a
// cutoff, a horizontal scroll or a wrap. An empty mask is the byte-identical
// plain path, and a cell whose style REVERSES video drops the mask — reverse
// swaps foreground and background, so per-token colours would paint per-token
// backgrounds (the same ruling winRow.cls follows).
type winCell struct {
	gutter string
	body   string
	style  lipgloss.Style
	mask   runMask
}
```

Then replace `cellSegs` (lines 39-72) with the piece-based layout plus the thin wrapper:

```go
// cellPiece is one laid-out display segment of a cell: the frozen gutter (or,
// on a wrap continuation, its blank indent), the body slice itself, and that
// slice's paint mask. Keeping the prefix out of the body is what lets the
// masked path paint the body run by run while the gutter and the trailing
// padding stay under the cell's style.
type cellPiece struct {
	pre  string
	body string
	mask runMask
}

// cellPieces lays a cell's body out at width under mode and returns one piece
// per display segment. It is cellSegs split in two; cellSegs is now a wrapper
// over it, so the two can never disagree about the layout.
func cellPieces(c *winCell, width int, mode dispMode, hscroll int) []cellPiece {
	if c == nil {
		return []cellPiece{{}}
	}
	gw := lipgloss.Width(c.gutter)
	bodyW := width - gw
	if bodyW < 1 {
		bodyW = 1
	}
	switch mode {
	case modeWrap:
		ws := wrapWidth(c.body, bodyW, 1<<20)
		if len(ws) == 0 {
			return []cellPiece{{pre: c.gutter}}
		}
		masks := wrapSegMasks(c.body, c.mask, ws)
		indent := strings.Repeat(" ", gw)
		out := make([]cellPiece, len(ws))
		for i, s := range ws {
			pre := indent
			if i == 0 {
				pre = c.gutter
			}
			out[i] = cellPiece{pre: pre, body: s, mask: masks[i]}
		}
		return out
	case modeScroll:
		body := hslice(c.body, hscroll, bodyW)
		return []cellPiece{{
			pre:  c.gutter,
			body: body,
			mask: c.mask.slice(hscrollRuneOff(c.body, hscroll), len([]rune(body))),
		}}
	default: // modeCutoff
		body := truncate(c.body, bodyW)
		m := c.mask.slice(0, len([]rune(body)))
		// truncate keeps a prefix and APPENDS "…" (it does not replace a kept
		// rune), so the mask's last slot lands on the class of the first
		// DROPPED rune. Force it plain so the ellipsis never wears a colour it
		// did not earn — window.go does exactly this for winRow.cls.
		if !m.empty() && lipgloss.Width(c.body) > bodyW {
			m.cls[len(m.cls)-1] = syntax.Plain
			m.emph[len(m.emph)-1] = false
		}
		return []cellPiece{{pre: c.gutter, body: body, mask: m}}
	}
}

// wrapSegMasks maps a cell's paint mask onto the segments cellPieces produced
// in wrap mode. wrapWidth (at the huge line cap cellPieces passes) slices runes
// verbatim and never rewrites them, so each segment's mask is the slice of the
// cell's mask at the running rune offset. The layout is VERIFIED against the
// body before it is trusted: if the segments do not reconstruct it, every
// segment is reported empty and the cell renders plain rather than mis-coloured.
func wrapSegMasks(body string, m runMask, segs []string) []runMask {
	out := make([]runMask, len(segs))
	if m.empty() {
		return out
	}
	var joined strings.Builder
	off := 0
	for i, s := range segs {
		n := len([]rune(s))
		out[i] = m.slice(off, n)
		joined.WriteString(s)
		off += n
	}
	if joined.String() != body {
		return make([]runMask, len(segs))
	}
	return out
}

// cellSegs lays a cell's body out at width under mode, returning the raw
// (unstyled, unpadded) display segments with the gutter on the first segment
// and a blank indent of the gutter's width on wrap continuations.
func cellSegs(c *winCell, width int, mode dispMode, hscroll int) []string {
	ps := cellPieces(c, width, mode, hscroll)
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = p.pre + p.body
	}
	return out
}

// pieceOrBlank returns the kth piece or a blank one when the cell ran out (the
// blank-pad that keeps a wrapped pair registered).
func pieceOrBlank(ps []cellPiece, k int) cellPiece {
	if k < len(ps) {
		return ps[k]
	}
	return cellPiece{}
}

// renderPiece renders one laid-out segment padded to w: pre+body under style —
// byte-identical to styleCell(style, pre+body, w) — or, for a masked piece,
// the body painted run by run (styledRuns) with the prefix and the trailing
// padding under style. Mirrors renderWindow's colouredLine.
func renderPiece(style lipgloss.Style, p cellPiece, w int) string {
	if p.mask.empty() || style.GetReverse() {
		return styleCell(style, p.pre+p.body, w)
	}
	disp := []rune(p.body)
	m := p.mask.slice(0, len(disp)) // defensive: exactly one entry per rune
	var b strings.Builder
	if p.pre != "" {
		b.WriteString(style.Render(p.pre))
	}
	b.WriteString(styledRuns(disp, m.emph, m.cls, style))
	if pad := w - lipgloss.Width(p.pre) - lipgloss.Width(p.body); pad > 0 {
		b.WriteString(style.Render(strings.Repeat(" ", pad)))
	}
	return b.String()
}
```

Leave `segOrBlank` (lines 74-81) exactly as it is — `internal/tui/twocol_window_test.go:45-46` still calls it.

Now switch `renderTwoCol` onto the pieces. In the non-wrap fast path replace lines 131-137 with:

```go
			r := rows[idx]
			if r.full != nil {
				out = append(out, renderPiece(r.full.style, cellPieces(r.full, w, o.mode, o.hscroll)[0], w))
				continue
			}
			left := renderPiece(cellStyle(r.left), cellPieces(r.left, colW, o.mode, o.hscroll)[0], colW)
			right := renderPiece(cellStyle(r.right), cellPieces(r.right, colW, o.mode, o.hscroll)[0], colW)
			out = append(out, left+o.sep+right)
```

and in the wrap pass replace lines 147-165 with:

```go
	for ri, r := range rows {
		if r.full != nil {
			for _, p := range cellPieces(r.full, w, o.mode, o.hscroll) {
				dl = append(dl, dline{text: renderPiece(r.full.style, p, w), row: ri})
			}
			continue
		}
		ls := cellPieces(r.left, colW, o.mode, o.hscroll)
		rs := cellPieces(r.right, colW, o.mode, o.hscroll)
		n := len(ls)
		if len(rs) > n {
			n = len(rs)
		}
		for k := 0; k < n; k++ {
			left := renderPiece(cellStyle(r.left), pieceOrBlank(ls, k), colW)
			right := renderPiece(cellStyle(r.right), pieceOrBlank(rs, k), colW)
			dl = append(dl, dline{text: left + o.sep + right, row: ri})
		}
	}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-conflict-syntax && go test ./internal/tui -run 'TwoCol|CellSegs' -v`
Expected: PASS, including the pre-existing `TestTwoColCutoffTruncates`, `TestTwoColScrollReveals`, `TestTwoColWrapAlignsPairsAndGutterOnlyFirst` and everything in `twocol_window_test.go`.

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-conflict-syntax && git add internal/tui/twocol.go internal/tui/twocol_syntax_test.go && git commit -m "feat(tui): winCell carries a post-slice paint mask

Give the two-column cell primitive a runMask (syntax classes + an
emphasis flag per display rune) that the layout slices alongside the
body in cutoff/scroll/wrap and styledRuns paints after the slice — the
same mechanism renderWindow uses for winRow.cls. An empty mask is the
byte-identical plain path and a reverse-video cell drops the mask.

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01NDk1DJtgLzZX7hMmxDs9nU"
```

---

### Task 2: `Block.ResolvedPicks` — provenance for the output pane

`Block.ResolvedLines()` returns plain strings, so the picker's output pane currently re-sanitizes every assembled line and would have no way to reuse a mask. Add the same expansion with provenance so the pane can look each line up in the per-side sanitized cache instead.

**Files:**
- Modify: `internal/hunkpick/hunkpick.go:181-186` (right after `ResolvedLines`)
- Test: `internal/hunkpick/resolvedpicks_test.go` (create)

**Interfaces:**
- Consumes: `Block.Mode`, `Block.Picks`, `Block.Current`, `Block.Incoming`, `fullPicks(s Side, n int) []Pick` (`internal/hunkpick/hunkpick.go:115`), `(b *Block) lines(s Side) []string` (`:43`).
- Produces: `func (b *Block) ResolvedPicks() ([]Pick, bool)` — the (side, line) of every line the block contributes, in output order; `ok == false` while `Undecided`; a `Skipped()` block returns `(nil, true)`. Its `[]Pick` expansion is line-for-line identical to `ResolvedLines()`.

- [ ] **Step 1: Write the failing test**

Create `/mnt/t/others/gigagit.worktrees/feat-conflict-syntax/internal/hunkpick/resolvedpicks_test.go`:

```go
package hunkpick

import "testing"

// ResolvedPicks must expand to exactly the lines ResolvedLines produces, but
// tagged with the side and line index each came from.
func TestResolvedPicksMatchesResolvedLines(t *testing.T) {
	t.Parallel()
	mk := func() *Block {
		return &Block{Current: []string{"c0", "c1", "c2"}, Incoming: []string{"i0", "i1"}}
	}
	cases := []struct {
		name  string
		setup func(*Block)
	}{
		{"take current", func(b *Block) { b.Mode = TakeCurrent }},
		{"take incoming", func(b *Block) { b.Mode = TakeIncoming }},
		{"line by line", func(b *Block) {
			b.Mode = LineByLine
			b.Picks = []Pick{{Side: Incoming, Line: 1}, {Side: Current, Line: 0}}
		}},
		{"skipped", func(b *Block) { b.Skip() }},
	}
	for _, tc := range cases {
		b := mk()
		tc.setup(b)
		want, wantOK := b.ResolvedLines()
		ps, ok := b.ResolvedPicks()
		if ok != wantOK {
			t.Fatalf("%s: ok = %v, want %v", tc.name, ok, wantOK)
		}
		if len(ps) != len(want) {
			t.Fatalf("%s: %d picks, want %d lines", tc.name, len(ps), len(want))
		}
		for i, p := range ps {
			var got string
			if p.Side == Current {
				got = b.Current[p.Line]
			} else {
				got = b.Incoming[p.Line]
			}
			if got != want[i] {
				t.Errorf("%s: pick %d resolves to %q, want %q", tc.name, i, got, want[i])
			}
		}
	}
}

func TestResolvedPicksUndecidedIsNotOK(t *testing.T) {
	t.Parallel()
	b := &Block{Current: []string{"c0"}, Incoming: []string{"i0"}}
	if ps, ok := b.ResolvedPicks(); ok || ps != nil {
		t.Fatalf("undecided block = (%v, %v), want (nil, false)", ps, ok)
	}
}

// An out-of-range pick (a stale index) is dropped, exactly as resolved() drops it.
func TestResolvedPicksDropsOutOfRangeLines(t *testing.T) {
	t.Parallel()
	b := &Block{Current: []string{"c0"}, Incoming: nil, Mode: LineByLine,
		Picks: []Pick{{Side: Current, Line: 5}, {Side: Current, Line: 0}}}
	ps, ok := b.ResolvedPicks()
	if !ok || len(ps) != 1 || ps[0].Line != 0 {
		t.Fatalf("picks = %v ok=%v, want the single in-range pick", ps, ok)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-conflict-syntax && go test ./internal/hunkpick -run ResolvedPicks -v`
Expected: FAIL to compile — `b.ResolvedPicks undefined (type *Block has no field or method ResolvedPicks)`.

- [ ] **Step 3: Implement**

In `/mnt/t/others/gigagit.worktrees/feat-conflict-syntax/internal/hunkpick/hunkpick.go`, insert directly after `ResolvedLines` (which ends at line 186):

```go
// ResolvedPicks is ResolvedLines with provenance: the (side, line) of every
// line the block contributes, in the same order, with the legacy whole-side
// modes reading as that side's full picks and out-of-range picks dropped
// exactly as resolved() drops them. ok=false while Undecided; a skipped block
// resolves to no picks at all. Callers that already hold a per-side view of
// the lines (the TUI picker's sanitized display cache) use it to reuse each
// line's prepared form instead of re-deriving it from the assembled strings.
func (b *Block) ResolvedPicks() ([]Pick, bool) {
	switch b.Mode {
	case TakeCurrent:
		return fullPicks(Current, len(b.Current)), true
	case TakeIncoming:
		return fullPicks(Incoming, len(b.Incoming)), true
	case LineByLine:
		var out []Pick
		for _, p := range b.Picks {
			if ls := b.lines(p.Side); p.Line >= 0 && p.Line < len(ls) {
				out = append(out, p)
			}
		}
		return out, true
	default:
		return nil, false
	}
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-conflict-syntax && go test ./internal/hunkpick -v`
Expected: PASS (all existing hunkpick tests too).

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-conflict-syntax && git add internal/hunkpick/hunkpick.go internal/hunkpick/resolvedpicks_test.go && git commit -m "feat(hunkpick): ResolvedPicks — ResolvedLines with provenance

Same expansion, but each contributed line is tagged with the side and
line index it came from, so a caller holding a per-side prepared view of
the lines can reuse it instead of re-deriving from the strings.

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01NDk1DJtgLzZX7hMmxDs9nU"
```

---

### Task 3: Lex the picker's two sides and index the runs

Assemble the two full-file texts the document describes and lex each once. The index mapping is the delicate part: literal context lines are shared by both sides, but each block contributes a DIFFERENT number of lines to each side, so the two full-file line cursors advance independently.

**Files:**
- Modify: `internal/tui/conflict_picker.go:25-64` (struct), `:76-137` (the four constructors)
- Create: `internal/tui/picker_syntax.go`
- Test: `internal/tui/picker_syntax_test.go` (create)

**Interfaces:**
- Consumes: `syntax.Detect(path string) string`, `syntax.Lex(lang string, src []byte) [][]syntax.Tok`, `syntax.Tok`; `domain.MaxSyntaxBytes` (`internal/domain/differ.go:18`); `hasBareCR(data []byte) bool` (`internal/tui/diff_syntax.go:68`); `hunkpick.Doc.Items`, `hunkpick.Item.Literal`, `hunkpick.Item.Block`.
- Produces:
  - `hunkPicker.path string` — set by every constructor.
  - `hunkPicker.curTok, hunkPicker.incTok [][]syntax.Tok` — runs per full-file line of each assembled side (index = line number − 1, the `tokAt` numbering).
  - `func lexPickerDoc(path string, doc *hunkpick.Doc, on bool) (cur, inc [][]syntax.Tok)`
  - `func (e *hunkPicker) withSyntax(on bool) *hunkPicker` — chainable; `on == false` is a no-op that leaves the plain path.

- [ ] **Step 1: Write the failing tests**

Create `/mnt/t/others/gigagit.worktrees/feat-conflict-syntax/internal/tui/picker_syntax_test.go`:

```go
package tui

import (
	"testing"

	"github.com/homeend/gigagit/internal/hunkpick"
	"github.com/homeend/gigagit/internal/syntax"
)

// pickerSyntaxDoc is a two-block Go document whose sides differ in LENGTH, so
// a mapping that advanced one full-file line cursor for both sides would land
// the second block's incoming line on the wrong line number.
//
//	current side (6 lines)      incoming side (4 lines)
//	0 package main              0 package main
//	1 var a int                 1 var x int
//	2 var b int                 2 const K = 1
//	3 var c int                 3 // tail
//	4 const K = 1
//	5 var d int
func pickerSyntaxDoc() *hunkpick.Doc {
	return &hunkpick.Doc{FinalNewline: true, Items: []hunkpick.Item{
		{Literal: []string{"package main"}},
		{Block: &hunkpick.Block{
			Current:  []string{"var a int", "var b int", "var c int"},
			Incoming: []string{"var x int"},
		}},
		{Literal: []string{"const K = 1"}},
		{Block: &hunkpick.Block{
			Current:  []string{"var d int"},
			Incoming: []string{"// tail"},
		}},
	}}
}

func TestPickerLexesBothSidesWithIndependentLineNumbers(t *testing.T) {
	t.Parallel()
	e := newConflictPicker("f.go", pickerSyntaxDoc()).withSyntax(true)
	if len(e.curTok) != 6 {
		t.Fatalf("current side lexed %d lines, want 6", len(e.curTok))
	}
	if len(e.incTok) != 4 {
		t.Fatalf("incoming side lexed %d lines, want 4", len(e.incTok))
	}
	// Block 2's incoming line is incoming line 4 (1-based) — a comment. Had the
	// mapping used the CURRENT side's cursor it would read line 6, past the end.
	if toks := tokAt(e.incTok, 4); len(toks) == 0 || toks[0].Class != syntax.Comment {
		t.Errorf("incoming line 4 (`// tail`) runs = %v, want a leading Comment", toks)
	}
	// Block 2's current line is current line 6 — `var` is a keyword.
	if toks := tokAt(e.curTok, 6); len(toks) == 0 || toks[0].Class != syntax.Keyword {
		t.Errorf("current line 6 (`var d int`) runs = %v, want a leading Keyword", toks)
	}
}

func TestPickerSyntaxOffLeavesNoRuns(t *testing.T) {
	t.Parallel()
	e := newConflictPicker("f.go", pickerSyntaxDoc()).withSyntax(false)
	if e.curTok != nil || e.incTok != nil {
		t.Fatalf("syntax off must leave both sides unlexed: cur=%d inc=%d", len(e.curTok), len(e.incTok))
	}
}

func TestPickerUnknownLanguageLeavesNoRuns(t *testing.T) {
	t.Parallel()
	e := newConflictPicker("f.unknownext", pickerSyntaxDoc()).withSyntax(true)
	if e.curTok != nil || e.incTok != nil {
		t.Fatalf("a path with no lexer must leave both sides unlexed: cur=%d inc=%d", len(e.curTok), len(e.incTok))
	}
}

// A side holding a BARE \r is refused, exactly as lexBlame/lexPreview refuse one.
func TestPickerBareCRSideIsNotLexed(t *testing.T) {
	t.Parallel()
	doc := &hunkpick.Doc{Items: []hunkpick.Item{
		{Literal: []string{"package main"}},
		{Block: &hunkpick.Block{Current: []string{"var a\rint"}, Incoming: []string{"var b int"}}},
	}}
	e := newConflictPicker("f.go", doc).withSyntax(true)
	if e.curTok != nil {
		t.Errorf("a bare-\\r current side must not be lexed: %d lines", len(e.curTok))
	}
	if e.incTok == nil {
		t.Errorf("the clean incoming side should still be lexed")
	}
}

// Every constructor records its path, so withSyntax needs no second argument.
func TestPickerConstructorsRecordPath(t *testing.T) {
	t.Parallel()
	d := pickerSyntaxDoc()
	for name, e := range map[string]*hunkPicker{
		"conflict": newConflictPicker("a.go", d),
		"process":  newProcessConflictPicker("b.go", d),
		"stage":    newStagePicker("c.go", d),
		"unstage":  newUnstagePicker("d.go", d),
	} {
		if e.path == "" {
			t.Errorf("%s picker did not record its path", name)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-conflict-syntax && go test ./internal/tui -run 'PickerLexes|PickerSyntaxOff|PickerUnknownLanguage|PickerBareCR|PickerConstructorsRecordPath' -v`
Expected: FAIL to compile — `e.withSyntax undefined`, `e.curTok undefined`, `e.path undefined`.

- [ ] **Step 3: Implement the lexer and the fields**

Create `/mnt/t/others/gigagit.worktrees/feat-conflict-syntax/internal/tui/picker_syntax.go`:

```go
// Syntax colouring of the hunk picker: assembling and lexing the two full-file
// texts its document describes, so every candidate line, context line and
// output line can be painted from its own side's runs (spec §4.2).

package tui

import (
	"strings"
	"sync"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/hunkpick"
	"github.com/homeend/gigagit/internal/syntax"
)

// lexPickerDoc assembles the two full-file texts doc describes — the literal
// context plus every block's Current lines (the current side), the same context
// plus every block's Incoming lines (the incoming side) — and lexes each once
// with path's lexer. The two sides are lexed CONCURRENTLY: each can cost ~1 s
// of chroma at domain.MaxSyntaxBytes and they are independent, so the worst
// case is one side's time rather than their sum.
//
// A side comes back nil (the plain path) when the switch is off, the path has
// no lexer, the side is past domain.MaxSyntaxBytes, or the side holds a BARE \r.
// The picker splits its lines on \n exactly as syntax.Lex numbers them, so a
// bare \r causes it no desync — but the two other self-lexing viewers
// (lexBlame, lexPreview) refuse such content and this one refuses it alike
// rather than growing a third rule for pathological input.
func lexPickerDoc(path string, doc *hunkpick.Doc, on bool) (cur, inc [][]syntax.Tok) {
	if !on || doc == nil {
		return nil, nil
	}
	lang := syntax.Detect(path)
	if lang == "" {
		return nil, nil
	}
	var curL, incL []string
	for _, it := range doc.Items {
		if it.Block == nil {
			curL = append(curL, it.Literal...)
			incL = append(incL, it.Literal...)
			continue
		}
		curL = append(curL, it.Block.Current...)
		incL = append(incL, it.Block.Incoming...)
	}
	// Trailing "\n": the doc's lines carry no terminator, and syntax.Lex adds
	// no line for a trailing newline, so each side still yields exactly one
	// token slice per line. A CRLF document keeps its \r at each line's end;
	// sanitizeCell trims that last rune and classMask clamps token ends to the
	// trimmed length, so the display masks stay aligned.
	lex := func(lines []string) [][]syntax.Tok {
		if len(lines) == 0 {
			return nil
		}
		src := strings.Join(lines, "\n") + "\n"
		if len(src) > domain.MaxSyntaxBytes || hasBareCR([]byte(src)) {
			return nil
		}
		return syntax.Lex(lang, []byte(src))
	}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); cur = lex(curL) }()
	go func() { defer wg.Done(); inc = lex(incL) }()
	wg.Wait()
	return cur, inc
}

// withSyntax lexes the picker's two sides and stores the runs, so every line
// it renders can be painted from the mask its own side's lexer produced. It is
// chained at the four open sites, where the [ui] diff_syntax switch is known;
// on=false is a documented no-op leaving the byte-identical plain path (every
// test constructor and any future caller takes it).
//
// The lex is SYNCHRONOUS: the loaders that fetch a picker's bytes off the UI
// thread hand back raw sides (or, for a conflict, marker text), and the
// hunkpick.Doc that determines the two full-file texts is only assembled once
// the message reaches Update — no loader goroutine holds it. Spec §4.2 rules
// this acceptable ("else synchronously"); domain.MaxSyntaxBytes bounds it.
//
// MUST be called before the first render: ensureSan builds the display masks
// once, lazily, from these runs.
func (e *hunkPicker) withSyntax(on bool) *hunkPicker {
	if !on {
		return e
	}
	e.curTok, e.incTok = lexPickerDoc(e.path, e.doc, true)
	return e
}
```

In `/mnt/t/others/gigagit.worktrees/feat-conflict-syntax/internal/tui/conflict_picker.go`, add the fields to `hunkPicker` — after the `apply` field (line 30), insert:

```go
	// path is the repo-relative file the document came from; it selects the
	// syntax lexer (withSyntax) and nothing else.
	path string
	// curTok/incTok hold the syntax runs of the two assembled full-file sides,
	// indexed by line number − 1 (the tokAt numbering). nil = render plain.
	// The literal context lines are shared by both sides and take the CURRENT
	// side's runs; each block's lines take their own side's.
	curTok, incTok [][]syntax.Tok
```

and add `"github.com/homeend/gigagit/internal/syntax"` to the file's import block.

Set `path` in each constructor's struct literal: in `newConflictPicker` (line 78), `newStagePicker` (line 109) and `newUnstagePicker` (line 126), add `path: path,` to the literal (e.g. change `doc: doc, blocks: doc.Blocks(), side: hunkpick.Current, mode: modeScroll,` to `path: path, doc: doc, blocks: doc.Blocks(), side: hunkpick.Current, mode: modeScroll,`). `newProcessConflictPicker` delegates to `newConflictPicker`, so it inherits it.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-conflict-syntax && go test ./internal/tui -run 'PickerLexes|PickerSyntaxOff|PickerUnknownLanguage|PickerBareCR|PickerConstructorsRecordPath' -v`
Expected: PASS. If `TestPickerLexesBothSidesWithIndependentLineNumbers` fails on the class of `// tail`, print `tokAt(e.incTok, 4)` to confirm chroma's Go lexer emits `Comment` for it before changing anything else.

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-conflict-syntax && git add internal/tui/conflict_picker.go internal/tui/picker_syntax.go internal/tui/picker_syntax_test.go && git commit -m "feat(tui): lex the hunk picker's two sides

Assemble the current-side and incoming-side full-file texts the picker's
document describes (shared literal context, each block's own lines) and
lex each once, concurrently, under the [ui] diff_syntax switch and the
MaxSyntaxBytes cap. The two sides carry INDEPENDENT line numbering
because a block contributes a different number of lines to each.

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01NDk1DJtgLzZX7hMmxDs9nU"
```

---

### Task 4: Paint the grid — masks in the sanitized cache

Turn the picker's `[][]string` sanitized caches into `[][]sanLine` (text + mask), building each mask with `sanitizeCell` so tabs stay aligned, and attach it to every cell.

**Files:**
- Modify: `internal/tui/conflict_picker.go:47-60` (cache fields), `:272-303` (`ensureSan`), `:527-547` (`pickerCell`), `:611-653` (`render`'s row build)
- Modify: `internal/tui/picker_syntax.go` (add `sanLine` + `sanPickLine`)
- Test: `internal/tui/picker_syntax_test.go` (extend)

**Interfaces:**
- Consumes: `runMask`, `winCell.mask`, `runMask.pad` (Task 1); `hunkPicker.curTok`/`incTok`/`path`/`withSyntax` (Task 3); `sanitizeCell(s string, spans []textdiff.Span, toks []syntax.Tok) (disp []rune, emph []bool, cls []syntax.Class)` (`internal/tui/diff_syntax.go:31`); `sanitizeLine(s string) string` (`internal/tui/diff_render.go:173`); `tokAt(side [][]syntax.Tok, no int) []syntax.Tok` (`internal/tui/diff_syntax.go:19`); `st().selectedRow` (reverse video, `internal/tui/styles.go:125`).
- Produces:
  - `type sanLine struct { text string; mask runMask }`
  - `func sanPickLine(l string, toks []syntax.Tok) sanLine`
  - `hunkPicker.sanLit, sanCur, sanInc` change type from `[][]string` to `[][]sanLine`
  - `func pickerCell(blk *hunkpick.Block, san []sanLine, side hunkpick.Side, r int, cursor bool) *winCell` (parameter type change)

- [ ] **Step 1: Write the failing tests**

Append to `/mnt/t/others/gigagit.worktrees/feat-conflict-syntax/internal/tui/picker_syntax_test.go` (and add `"strings"`, `"github.com/charmbracelet/lipgloss"`, `"github.com/charmbracelet/x/ansi"`, `"github.com/muesli/termenv"` to its imports):

```go
// pickerLineWith returns the first line of a rendered picker whose visible
// text contains needle, or "" when there is none.
func pickerLineWith(render, needle string) string {
	for _, l := range strings.Split(render, "\n") {
		if strings.Contains(ansi.Strip(l), needle) {
			return l
		}
	}
	return ""
}

func TestEnsureSanBuildsMasksFromTheRightSide(t *testing.T) {
	t.Parallel()
	e := newConflictPicker("f.go", pickerSyntaxDoc()).withSyntax(true)
	e.ensureSan()

	// Literal context takes the CURRENT side's runs: `const` is a keyword.
	lit := e.sanLit[2][0]
	if lit.text != "const K = 1" {
		t.Fatalf("literal text = %q", lit.text)
	}
	if lit.mask.empty() || lit.mask.cls[0] != syntax.Keyword {
		t.Errorf("literal `const` should be a keyword: %v", lit.mask.cls)
	}
	// Block 2's incoming line is a comment; block 2's current line is a keyword.
	if got := e.sanInc[1][0]; got.text != "// tail" || got.mask.empty() || got.mask.cls[0] != syntax.Comment {
		t.Errorf("sanInc[1][0] = %q cls=%v, want `// tail` starting Comment", got.text, got.mask.cls)
	}
	if got := e.sanCur[1][0]; got.text != "var d int" || got.mask.empty() || got.mask.cls[0] != syntax.Keyword {
		t.Errorf("sanCur[1][0] = %q cls=%v, want `var d int` starting Keyword", got.text, got.mask.cls)
	}
	// Every mask is exactly one entry per DISPLAY rune.
	if n := len([]rune(lit.text)); len(lit.mask.cls) != n || len(lit.mask.emph) != n {
		t.Errorf("mask must parallel the display runes: cls=%d emph=%d runes=%d", len(lit.mask.cls), len(lit.mask.emph), n)
	}
}

// A tab expands to the 4-column stop and the mask follows it.
func TestSanPickLineCarriesClassesThroughTabs(t *testing.T) {
	t.Parallel()
	got := sanPickLine("\tvar x", []syntax.Tok{{Start: 1, End: 4, Class: syntax.Keyword}})
	if got.text != "    var x" {
		t.Fatalf("text = %q, want %q", got.text, "    var x")
	}
	want := []syntax.Class{0, 0, 0, 0, syntax.Keyword, syntax.Keyword, syntax.Keyword, 0, 0}
	for i := range want {
		if got.mask.cls[i] != want[i] {
			t.Fatalf("cls = %v, want %v", got.mask.cls, want)
		}
	}
}

func TestSanPickLineWithoutRunsIsPlain(t *testing.T) {
	t.Parallel()
	got := sanPickLine("\tvar x", nil)
	if got.text != "    var x" {
		t.Fatalf("text = %q", got.text)
	}
	if !got.mask.empty() {
		t.Errorf("no runs must leave an empty mask: %v", got.mask)
	}
}

func TestPickerGridColoursCodeAndKeepsCursorPlain(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(prev)

	e := newConflictPicker("f.go", pickerSyntaxDoc()).withSyntax(true)
	m := Model{layers: &layerStack{entries: []layer{e}}, width: 100, height: 30}
	out := e.render(m, "")

	kw := "38;5;" + st().syntaxColor(syntax.Keyword)
	// The cursor starts on block 0 / current / line 0 — "var a int". Only the
	// CURSOR CELL is plain; the incoming cell sharing that row is still
	// coloured, so scope the assertion to the left half. renderTwoCol appends
	// pickerColSep raw between the two cells, so splitting on it is safe.
	cur := pickerLineWith(out, "> [ ] var a int")
	if cur == "" {
		t.Fatalf("cursor row not found:\n%s", ansi.Strip(out))
	}
	halves := strings.SplitN(cur, pickerColSep, 2)
	if len(halves) != 2 {
		t.Fatalf("cursor row has no column separator: %q", cur)
	}
	if strings.Contains(halves[0], kw) {
		t.Errorf("the cursor cell must stay plain reverse-video, no class runs: %q", halves[0])
	}
	if !strings.Contains(halves[1], kw+"mvar") {
		t.Errorf("the non-cursor cell on the SAME row should still be coloured: %q", halves[1])
	}
	// A non-cursor candidate line IS coloured.
	code := pickerLineWith(out, "[ ] var b int")
	if code == "" {
		t.Fatalf("candidate row `var b int` not found:\n%s", ansi.Strip(out))
	}
	if !strings.Contains(code, kw+"mvar") {
		t.Errorf("`var` should wear the keyword colour: %q", code)
	}
	// The literal context row is coloured too.
	lit := pickerLineWith(out, "const K = 1")
	if lit == "" || !strings.Contains(lit, kw+"mconst") {
		t.Errorf("literal context `const` should be coloured: %q", lit)
	}
}

// Without withSyntax the render is byte-identical to today's.
func TestPickerRenderUnwiredIsPlain(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(prev)

	mk := func(on bool) string {
		e := newConflictPicker("f.go", pickerSyntaxDoc()).withSyntax(on)
		m := Model{layers: &layerStack{entries: []layer{e}}, width: 100, height: 30}
		return e.render(m, "")
	}
	off, on := mk(false), mk(true)
	if ansi.Strip(off) != ansi.Strip(on) {
		t.Errorf("colouring must not change the visible text:\n off %q\n on %q", ansi.Strip(off), ansi.Strip(on))
	}
	if strings.Contains(off, "38;5;"+st().syntaxColor(syntax.Keyword)) {
		t.Errorf("an unwired picker must render no class runs: %q", off)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-conflict-syntax && go test ./internal/tui -run 'EnsureSanBuilds|SanPickLine|PickerGridColours|PickerRenderUnwired' -v`
Expected: FAIL to compile — `undefined: sanPickLine`, and `e.sanLit[2][0].text undefined (type string has no field or method text)`.

- [ ] **Step 3: Implement**

Append to `/mnt/t/others/gigagit.worktrees/feat-conflict-syntax/internal/tui/picker_syntax.go`:

```go
// sanLine is one sanitized display line of the picker's document plus its
// paint mask. The mask is empty when the picker was opened without syntax runs
// (or the line's own runs are empty), which is the byte-identical plain path.
type sanLine struct {
	text string
	mask runMask
}

// sanPickLine sanitizes one document line for display and builds its paint
// mask from that line's syntax runs. sanitizeCell does the mapping so a tab's
// 4-column expansion carries the classes with it; with no runs the line takes
// sanitizeLine and an empty mask.
func sanPickLine(l string, toks []syntax.Tok) sanLine {
	if len(toks) == 0 {
		return sanLine{text: sanitizeLine(l)}
	}
	disp, emph, cls := sanitizeCell(l, nil, toks)
	return sanLine{text: string(disp), mask: runMask{cls: cls, emph: emph}}
}
```

In `/mnt/t/others/gigagit.worktrees/feat-conflict-syntax/internal/tui/conflict_picker.go`, change the three cache field declarations (lines 53-55) to:

```go
	sanLit   [][]sanLine // per doc.Items index: sanitized literal lines (nil for blocks)
	sanCur   [][]sanLine // per block index: sanitized current-side lines
	sanInc   [][]sanLine // per block index: sanitized incoming-side lines
```

Replace `ensureSan` (lines 272-303) with:

```go
// ensureSan builds the once-per-picker sanitized copies of the doc's lines and
// their paint masks (display only — resolution reads the doc's raw lines and
// keeps CRLF). The two assembled sides are numbered INDEPENDENTLY: they share
// the literal context, but each block contributes a different number of lines
// to each side, so curNo and incNo advance separately. Literal context takes
// the CURRENT side's runs — it is byte-identical on both sides.
func (e *hunkPicker) ensureSan() {
	if e.sanBuilt {
		return
	}
	e.sanBuilt = true
	e.sanLit = make([][]sanLine, len(e.doc.Items))
	e.sanCur = make([][]sanLine, len(e.blocks))
	e.sanInc = make([][]sanLine, len(e.blocks))
	bi, curNo, incNo := 0, 0, 0
	for i, it := range e.doc.Items {
		if it.Block == nil {
			ls := make([]sanLine, len(it.Literal))
			for k, l := range it.Literal {
				ls[k] = sanPickLine(l, tokAt(e.curTok, curNo+k+1))
			}
			e.sanLit[i] = ls
			curNo += len(it.Literal)
			incNo += len(it.Literal)
			continue
		}
		cur := make([]sanLine, len(it.Block.Current))
		for k, l := range it.Block.Current {
			cur[k] = sanPickLine(l, tokAt(e.curTok, curNo+k+1))
		}
		inc := make([]sanLine, len(it.Block.Incoming))
		for k, l := range it.Block.Incoming {
			inc[k] = sanPickLine(l, tokAt(e.incTok, incNo+k+1))
		}
		e.sanCur[bi], e.sanInc[bi] = cur, inc
		curNo += len(it.Block.Current)
		incNo += len(it.Block.Incoming)
		bi++
	}
}
```

Replace `pickerCell` (lines 527-547) with:

```go
// pickerCell builds the winCell for one candidate line; r past the side's line
// count yields a blank cell (the gap when sides differ in length). cursor adds
// the "> " marker so the gutter width is constant (focused or not) and puts the
// cell under selectedRow — reverse video, which renderPiece takes as the signal
// to drop the paint mask, so the cursor row stays plain.
func pickerCell(blk *hunkpick.Block, san []sanLine, side hunkpick.Side, r int, cursor bool) *winCell {
	if r >= len(san) {
		return &winCell{}
	}
	cur := "  "
	if cursor {
		cur = "> "
	}
	tick := "[ ] "
	if blk.LinePicked(side, r) {
		tick = "[x] "
	}
	c := &winCell{gutter: cur + tick, body: san[r].text, mask: san[r].mask}
	if cursor {
		c.style = st().selectedRow
	}
	return c
}
```

In `render`, replace the literal-row loop (lines 618-620) with:

```go
			for _, l := range e.sanLit[ii] {
				// The two-space indent is part of the body (it scrolls with
				// the text), so the mask gains two leading plain runes.
				rows = append(rows, colRow{full: &winCell{body: "  " + l.text, style: dim, mask: l.mask.pad(2)}})
			}
```

Finally, the output cache must follow the type change or the package will not build (`ensureOutput` does `append(e.outLines, e.sanLit[i]...)`). Make the MINIMAL adjustment now — Task 5 replaces it with the mask-reusing assembly:

- field (line 59): `outLines []string` becomes `outLines []sanLine`
- in `ensureOutput`, `e.outLines = append(e.outLines, sanitizeLine(l))` becomes `e.outLines = append(e.outLines, sanLine{text: sanitizeLine(l)})`, and `e.outLines = append(e.outLines, i18n.T("‹region %d undecided›", bi+1))` becomes `e.outLines = append(e.outLines, sanLine{text: i18n.T("‹region %d undecided›", bi+1)})`
- `outputLines()` (line 695) returns `([]sanLine, int)`
- in `renderOutput`'s non-wrap branch, `l := src[idx]` becomes `l := src[idx].text`
- in `renderOutput`'s wrap branch, `ws := wrapWidth(l, w, 1<<20)` becomes `ws := wrapWidth(l.text, w, 1<<20)`

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-conflict-syntax && go build ./cmd/gg && go test ./internal/tui -run 'EnsureSanBuilds|SanPickLine|PickerGrid|PickerRenderUnwired|HunkPicker|ConflictPicker' -v`
Expected: build clean, all PASS.

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-conflict-syntax && git add internal/tui/conflict_picker.go internal/tui/picker_syntax.go internal/tui/picker_syntax_test.go && git commit -m "feat(tui): paint the hunk picker's grid with syntax runs

The sanitized caches become sanLine (text + paint mask), built once by
ensureSan from each side's own full-file runs via sanitizeCell so tabs
stay aligned. Candidate cells and literal context rows carry the mask;
the cursor row's reverse-video style drops it and stays plain.

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01NDk1DJtgLzZX7hMmxDs9nU"
```

---

### Task 5: The output pane reuses the grid's masks

The live result pane assembles the picked lines. It must reuse the very `sanLine` values the grid already holds — no re-sanitize, no re-lex per pick — and paint them through the same post-slice primitive.

**Files:**
- Modify: `internal/tui/conflict_picker.go:305-333` (`ensureOutput`), `:704-793` (`renderOutput`)
- Test: `internal/tui/picker_syntax_test.go` (extend)

**Interfaces:**
- Consumes: `sanLine`, `runMask` (Tasks 1, 4); `cellPieces`, `renderPiece`, `winCell.mask` (Task 1); `hunkpick.Block.ResolvedPicks() ([]Pick, bool)` (Task 2); `hunkPicker.outLines []sanLine` and `outputLines() ([]sanLine, int)` (already retyped by Task 4); `windowStart`, `padRight`.
- Produces: no new symbols — `ensureOutput` now assembles from provenance and `renderOutput` paints through the cell primitive.

- [ ] **Step 1: Write the failing tests**

Append to `/mnt/t/others/gigagit.worktrees/feat-conflict-syntax/internal/tui/picker_syntax_test.go` (add `"github.com/homeend/gigagit/internal/hunkpick"` if not already imported):

```go
// The output pane must reuse the grid's prepared lines, not rebuild them:
// pointer identity of the mask backing array proves no re-sanitize/re-lex.
func TestOutputPaneReusesTheGridsSanLines(t *testing.T) {
	t.Parallel()
	e := newConflictPicker("f.go", pickerSyntaxDoc()).withSyntax(true)
	e.doc.SetAll(hunkpick.TakeCurrent)
	e.ensureOutput()

	// outLines[0] is the literal "package main"; [1..3] are block 0's current
	// lines; [4] the second literal; [5] block 1's current line.
	if len(e.outLines) != 6 {
		t.Fatalf("outLines = %d entries, want 6: %v", len(e.outLines), e.outLines)
	}
	if e.outLines[1].text != "var a int" {
		t.Fatalf("outLines[1] = %q, want %q", e.outLines[1].text, "var a int")
	}
	if e.outLines[1].mask.empty() || e.sanCur[0][0].mask.empty() {
		t.Fatal("both the grid line and the output line must carry a mask")
	}
	if &e.outLines[1].mask.cls[0] != &e.sanCur[0][0].mask.cls[0] {
		t.Error("the output pane must reuse the grid's mask, not rebuild one")
	}
	if &e.outLines[0].mask.cls[0] != &e.sanLit[0][0].mask.cls[0] {
		t.Error("literal output lines must reuse the grid's literal mask")
	}
}

// A re-pick rebuilds the assembly from the same cached lines (masks survive).
func TestOutputPaneKeepsMasksAcrossPicks(t *testing.T) {
	t.Parallel()
	e := newConflictPicker("f.go", pickerSyntaxDoc()).withSyntax(true)
	e.doc.SetAll(hunkpick.TakeCurrent)
	e.ensureOutput()
	e.blocks[0].ToggleSide(hunkpick.Incoming)
	e.pickRev++
	e.ensureOutput()
	for i, l := range e.outLines {
		if l.text != "" && l.mask.empty() {
			t.Fatalf("outLines[%d] (%q) lost its mask after a re-pick", i, l.text)
		}
	}
}

// An undecided region's placeholder carries no mask and is never painted.
func TestOutputPanePlaceholderIsPlain(t *testing.T) {
	t.Parallel()
	e := newConflictPicker("f.go", pickerSyntaxDoc()).withSyntax(true)
	e.ensureOutput()
	if !e.outLines[1].mask.empty() {
		t.Errorf("the undecided placeholder must carry no mask: %v", e.outLines[1])
	}
}

func TestRenderOutputColoursTheAssembledLines(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(prev)

	kw := "38;5;" + st().syntaxColor(syntax.Keyword)
	for _, mode := range []dispMode{modeScroll, modeCutoff, modeWrap} {
		e := newConflictPicker("f.go", pickerSyntaxDoc()).withSyntax(true)
		e.mode = mode
		e.doc.SetAll(hunkpick.TakeCurrent)
		lines := e.renderOutput(60, 8)
		joined := strings.Join(lines, "\n")
		if !strings.Contains(joined, kw+"mvar") {
			t.Errorf("mode %d: the output pane should colour `var`:\n%q", mode, joined)
		}
		if !strings.Contains(ansi.Strip(joined), "var a int") {
			t.Errorf("mode %d: visible text lost:\n%q", mode, ansi.Strip(joined))
		}
		for _, l := range lines {
			if w := ansi.StringWidth(l); w != 60 {
				t.Fatalf("mode %d: line width = %d, want 60: %q", mode, w, l)
			}
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-conflict-syntax && go test ./internal/tui -run 'OutputPane|RenderOutputColours' -v`
Expected: the package compiles (Task 4's stopgap keeps `outLines []sanLine`) and three tests FAIL: `TestOutputPaneReusesTheGridsSanLines` with "both the grid line and the output line must carry a mask" (the stopgap re-runs `sanitizeLine`, producing an empty mask), `TestOutputPaneKeepsMasksAcrossPicks` with "lost its mask after a re-pick", and `TestRenderOutputColoursTheAssembledLines` with "the output pane should colour `var`". `TestOutputPanePlaceholderIsPlain` already passes — it pins behaviour this task must not regress.

- [ ] **Step 3: Implement**

In `/mnt/t/others/gigagit.worktrees/feat-conflict-syntax/internal/tui/conflict_picker.go`, update the `outLines` field comment (line 59) to `outLines []sanLine // assembled output, reusing the grid's sanitized lines` — the type is already `[]sanLine` from Task 4.

Replace `ensureOutput` (lines 305-333, as Task 4 left it) with:

```go
// ensureOutput (re)assembles the output lines and each block's start offset —
// only when the picks changed since the last build. Every line is the very
// sanLine the grid already holds (looked up through the block's ResolvedPicks
// provenance), so the pane never re-sanitizes and never re-lexes a pick.
func (e *hunkPicker) ensureOutput() {
	if e.outBuilt && e.outRev == e.pickRev {
		return
	}
	e.ensureSan()
	e.outBuilt, e.outRev = true, e.pickRev
	e.outLines = e.outLines[:0]
	if e.outStart == nil {
		e.outStart = make([]int, len(e.blocks))
	}
	bi := 0
	for i, it := range e.doc.Items {
		if it.Block == nil {
			e.outLines = append(e.outLines, e.sanLit[i]...)
			continue
		}
		e.outStart[bi] = len(e.outLines)
		if ps, ok := it.Block.ResolvedPicks(); ok {
			for _, p := range ps {
				side := e.sanCur[bi]
				if p.Side == hunkpick.Incoming {
					side = e.sanInc[bi]
				}
				if p.Line >= 0 && p.Line < len(side) {
					e.outLines = append(e.outLines, side[p.Line])
				}
			}
		} else {
			e.outLines = append(e.outLines, sanLine{text: i18n.T("‹region %d undecided›", bi+1)})
		}
		bi++
	}
}
```

`outputLines`' signature already returns `([]sanLine, int)` from Task 4; its body is unchanged.

In `renderOutput`, replace the two line-transform sites Task 4 left as stopgaps. Replace the non-wrap transform (Task 4 left it as `l := src[idx].text` followed by the `hslice`/`truncate`/`padRight` block) with:

```go
			p := cellPieces(&winCell{body: src[idx].text, mask: src[idx].mask}, w, e.mode, e.hscroll)[0]
			out = append(out, renderPiece(lipgloss.Style{}, p, w))
```

and replace the wrap layout (the `var dl []string` loop and the final emit loop) with a piece-based one:

```go
	var dl []cellPiece
	anchor := 0
	for i, l := range src {
		if i == srcAnchor {
			anchor = len(dl)
		}
		dl = append(dl, cellPieces(&winCell{body: l.text, mask: l.mask}, w, modeWrap, e.hscroll)...)
	}
```

```go
	out := make([]string, 0, h)
	for i := 0; i < h; i++ {
		if idx := start + i; idx < len(dl) {
			out = append(out, renderPiece(lipgloss.Style{}, dl[idx], w))
		} else {
			out = append(out, padRight("", w))
		}
	}
	return out
```

Note the wrap branch no longer needs the `if len(ws) == 0 { ws = []string{""} }` guard — `cellPieces` already returns a single blank piece for an empty body. Update the function's doc comment's last sentence to read: "The lines arrive pre-sanitized (with their paint masks) from the output cache; outside wrap mode the window is computed first and only the h visible lines are laid out and painted."

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-conflict-syntax && go test ./internal/tui/... ./internal/hunkpick/... && cd /mnt/t/others/gigagit.worktrees/feat-conflict-syntax && go build ./cmd/gg`
Expected: PASS, build clean.

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-conflict-syntax && git add internal/tui/conflict_picker.go internal/tui/picker_syntax_test.go && git commit -m "feat(tui): the picker's output pane reuses the grid's painted lines

ensureOutput now assembles from Block.ResolvedPicks provenance, taking
the very sanLine the grid holds for each picked line — no re-sanitize,
no re-lex per pick — and renderOutput lays each one out through
cellPieces/renderPiece so the runs survive the hscroll and the wrap.

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01NDk1DJtgLzZX7hMmxDs9nU"
```

---

### Task 6: Wire the four open sites, smoke-capture, and document

Nothing is coloured yet in a running `gg`: `withSyntax` is never called. Wire all four picker kinds (each has a real repo-relative path, so none is skipped), verify headlessly, and update the docs.

**Files:**
- Modify: `internal/tui/model.go:3073` (process conflict), `:3078` (conflict), `:3096` (stage), `:3114` (unstage)
- Modify: `CHANGELOG.md:9` (under `## [Unreleased]`)
- Modify: `docs/CLAUDE-details.md` (after the `winRow.cls` paragraph that ends "...which they turn into a line break and `syntax.Lex` does not.", ~line 109)
- Modify: `README.md:440` (the `[ui] diff_syntax` paragraph)
- Test: `internal/tui/picker_syntax_test.go` (extend)

**Interfaces:**
- Consumes: `(e *hunkPicker) withSyntax(on bool) *hunkPicker` (Task 3); `m.cfg.UI.SyntaxOn() bool` (`internal/config/config.go:122`).
- Produces: nothing new — this is the wiring + documentation task.

- [ ] **Step 1: Write the failing tests**

Append to `/mnt/t/others/gigagit.worktrees/feat-conflict-syntax/internal/tui/picker_syntax_test.go`:

```go
// goConflict is a conflicted Go file: one region, `var` on both sides.
const goConflict = "package main\n<<<<<<< HEAD\nvar a int\n=======\nvar b int\n>>>>>>> x\n"

// Opening a picker through the real message path must lex it — all four kinds.
func TestConflictLoaderWiresSyntax(t *testing.T) {
	t.Parallel()
	m := Model{width: 80, height: 24}
	m.cfg.UI.DiffSyntax = "auto"
	u, _ := m.Update(conflictFileLoadedMsg{path: "f.go", content: []byte(goConflict)})
	e, ok := u.(Model).topLayer().(*hunkPicker)
	if !ok {
		t.Fatalf("conflict load should push the hunk picker, got %T", u.(Model).topLayer())
	}
	if e.curTok == nil || e.incTok == nil {
		t.Errorf("the conflict picker was opened unlexed: cur=%v inc=%v", e.curTok, e.incTok)
	}
}

func TestProcessConflictLoaderWiresSyntax(t *testing.T) {
	t.Parallel()
	m := conflictModel()
	m, _ = startConflictProcess(m)
	m.proc.(*conflictProcess).st = confWorking
	m.cfg.UI.DiffSyntax = "auto"
	u, _ := m.Update(conflictFileLoadedMsg{path: "uu.go", content: []byte(goConflict)})
	cp := u.(Model).proc.(*conflictProcess)
	if cp.picker == nil {
		t.Fatalf("a loaded conflict file must show the process picker, got st=%d", cp.st)
	}
	if cp.picker.curTok == nil || cp.picker.incTok == nil {
		t.Errorf("the process picker was opened unlexed: cur=%v inc=%v", cp.picker.curTok, cp.picker.incTok)
	}
}

func TestStageLoaderWiresSyntax(t *testing.T) {
	t.Parallel()
	m := Model{width: 80, height: 24}
	m.cfg.UI.DiffSyntax = "auto"
	u, _ := m.Update(stageHunksLoadedMsg{path: "f.go",
		index: []byte("package main\nvar a int\n"), work: []byte("package main\nvar b int\n")})
	e, ok := u.(Model).topLayer().(*hunkPicker)
	if !ok {
		t.Fatalf("stage load should push the hunk picker, got %T", u.(Model).topLayer())
	}
	if e.curTok == nil || e.incTok == nil {
		t.Errorf("the stage picker was opened unlexed: cur=%v inc=%v", e.curTok, e.incTok)
	}
}

func TestUnstageLoaderWiresSyntax(t *testing.T) {
	t.Parallel()
	m := Model{width: 80, height: 24}
	m.cfg.UI.DiffSyntax = "auto"
	u, _ := m.Update(unstageHunksLoadedMsg{path: "f.go",
		index: []byte("package main\nvar a int\n"), head: []byte("package main\nvar b int\n")})
	e, ok := u.(Model).topLayer().(*hunkPicker)
	if !ok {
		t.Fatalf("unstage load should push the hunk picker, got %T", u.(Model).topLayer())
	}
	if e.curTok == nil || e.incTok == nil {
		t.Errorf("the unstage picker was opened unlexed: cur=%v inc=%v", e.curTok, e.incTok)
	}
}

// diff_syntax = "off" must leave every picker on the plain path.
func TestLoaderHonoursSyntaxOff(t *testing.T) {
	t.Parallel()
	m := Model{width: 80, height: 24}
	m.cfg.UI.DiffSyntax = "off"
	u, _ := m.Update(conflictFileLoadedMsg{path: "f.go", content: []byte(goConflict)})
	e := u.(Model).topLayer().(*hunkPicker)
	if e.curTok != nil || e.incTok != nil {
		t.Errorf("diff_syntax=off must leave the picker unlexed: cur=%d inc=%d", len(e.curTok), len(e.incTok))
	}
}
```

The idioms above are the package's own: `Model{width: 80, height: 24}` + `m.topLayer()` is what `conflict_picker_test.go:145-160` uses, and `conflictModel()` + `startConflictProcess(m)` + `m.proc.(*conflictProcess)` is what `conflict_process_test.go:231-240` uses. `conflictModel` and `startConflictProcess` need no `*testing.T`. Note the zero `Model`'s `cfg.UI.DiffSyntax` is `""`, which `SyntaxOn()` already reads as ON — the tests set it explicitly so the intent is on the page.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-conflict-syntax && go test ./internal/tui -run 'LoaderWiresSyntax|LoaderHonoursSyntaxOff|ProcessConflictLoaderWiresSyntax' -v`
Expected: FAIL — "the conflict picker was opened unlexed: cur=[] inc=[]" (the constructors run, but nothing calls `withSyntax`).

- [ ] **Step 3: Wire the four open sites**

In `/mnt/t/others/gigagit.worktrees/feat-conflict-syntax/internal/tui/model.go`:

line 3073 — `cp.picker = newProcessConflictPicker(msg.path, doc)` becomes

```go
			cp.picker = newProcessConflictPicker(msg.path, doc).withSyntax(m.cfg.UI.SyntaxOn())
```

line 3078 — `m = m.pushLayer(newConflictPicker(msg.path, doc))` becomes

```go
		m = m.pushLayer(newConflictPicker(msg.path, doc).withSyntax(m.cfg.UI.SyntaxOn()))
```

line 3096 — `m = m.pushLayer(newStagePicker(msg.path, doc))` becomes

```go
		m = m.pushLayer(newStagePicker(msg.path, doc).withSyntax(m.cfg.UI.SyntaxOn()))
```

line 3114 — `m = m.pushLayer(newUnstagePicker(msg.path, doc))` becomes

```go
		m = m.pushLayer(newUnstagePicker(msg.path, doc).withSyntax(m.cfg.UI.SyntaxOn()))
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-conflict-syntax && go test ./internal/tui -run 'LoaderWiresSyntax|LoaderHonoursSyntaxOff|ProcessConflictLoaderWiresSyntax' -v`
Expected: PASS.

- [ ] **Step 5: Headless smoke check on a real conflicted Go file**

The unit tests are the gate; this proves the escapes reach a real terminal. First confirm the keyscript reaches the picker with the plain harness, then re-drive it with escapes preserved (`tui-capture.sh` captures without `-e`, so it cannot show colour):

```bash
cd /mnt/t/others/gigagit.worktrees/feat-conflict-syntax && go build -o /tmp/gg-picker ./cmd/gg
R=$(mktemp -d)/conf && mkdir -p "$R"
git -C "$R" init -q -b main
printf 'package main\n\nfunc main() {\n\tprintln("base")\n}\n' > "$R/main.go"
git -C "$R" add main.go
git -C "$R" -c user.email=t@t -c user.name=t commit -qm base
git -C "$R" checkout -qb feat
printf 'package main\n\nfunc main() {\n\tprintln("theirs")\n}\n' > "$R/main.go"
git -C "$R" -c user.email=t@t -c user.name=t commit -qam theirs
git -C "$R" checkout -q main
printf 'package main\n\nfunc main() {\n\tprintln("ours")\n}\n' > "$R/main.go"
git -C "$R" -c user.email=t@t -c user.name=t commit -qam ours
git -C "$R" merge feat || true   # leaves main.go conflicted
echo "repo: $R"
cd /mnt/t/others/gigagit.worktrees/feat-conflict-syntax && ./tui-capture.sh --repo "$R" --gg /tmp/gg-picker --out /tmp/gg-snaps "files: tab ; picker: enter"
```

Read `/tmp/gg-snaps/snap-*.txt`. The last snapshot must show the picker header (`Resolve conflicts: main.go`) and the two-column grid with `println("ours")` / `println("theirs")`, gutters aligned and no visible garbage. If `tab` did not land on the Files panel (focus starts on Branches, and the left column's tab order can differ), read `snap-01-files.txt` and adjust the keyscript before continuing.

Then re-drive with escapes preserved and assert the colour:

```bash
tmux new-session -d -s ggsyn -x 140 -y 40 "cd $R && /tmp/gg-picker"
sleep 4
tmux send-keys -t ggsyn Tab;   sleep 2
tmux send-keys -t ggsyn Enter; sleep 3
tmux capture-pane -e -p -t ggsyn > /tmp/picker-esc.txt
tmux kill-session -t ggsyn
# NOTE: match 38;2; too — a terminal advertising RGB makes lipgloss emit
# truecolor sequences, and a 38;5;-only grep would report zero on a good build.
# 1. code cells wear a foreground colour
grep -cE $'\x1b\\[38;[25];' /tmp/picker-esc.txt
# 2. a candidate code line is coloured
grep -E $'\x1b\\[38;[25];' /tmp/picker-esc.txt | grep -c 'println'
# 3. the cursor CELL is reverse video and carries NO class run
grep '> \[' /tmp/picker-esc.txt | cat -v | head -3
```

Expected: (1) > 0, (2) ≥ 1, (3) the cursor row shows `^[[7m` (reverse) and the text LEFT of the `║` separator contains no `38;2;`/`38;5;` sequence — the incoming cell right of it may well be coloured, which is correct. Clean up: `rm -rf "$R" /tmp/gg-picker /tmp/gg-snaps /tmp/picker-esc.txt`.

- [ ] **Step 6: Update the docs**

`CHANGELOG.md` — insert immediately after line 9 (`## [Unreleased]`), as the first bullet:

```markdown
- **Conflict resolver and hunk staging are syntax-coloured.** The region/line
  picker (`x` → enter, `enter` on a conflicted Files row, and `H` on a Files or
  Staged row for hunk staging / unstaging) now colours code by file type, the same
  lexers and `[ui] diff_syntax` switch as the diff views. When a picker opens
  it assembles the two versions of the file its regions describe — the shared
  context plus each region's current-side lines, and the same context plus its
  incoming-side lines — and lexes each once, so both columns are coloured by
  their own side's grammar and the live output pane reuses the same runs. The
  cursor row stays plain reverse-video, and a file with no known lexer, a side
  past 1 MB, or `[ui] diff_syntax = "off"` renders exactly as before.
```

`docs/CLAUDE-details.md` — insert a new paragraph directly after the `winRow.cls` paragraph (the one ending "...which they turn into a line break and `syntax.Lex` does not."):

```markdown
The **hunk picker** (conflict resolver + hunk staging/unstaging) reaches the
same colouring through **`winCell.mask`** — a `runMask{cls, emph}` per display
rune that `cellPieces` slices alongside the body in all three modes and
`renderPiece` paints with `styledRuns` (empty mask = the byte-identical plain
path; a reverse-video cell style drops it, which is what keeps the cursor row
plain). `emph` is carried but unused today: phase 6's in-view search paints its
hits through it. `withSyntax` (called at the four open sites in `model.go`)
runs `lexPickerDoc`, which assembles the current-side and incoming-side
full-file texts the `hunkpick.Doc` describes — shared literal context plus each
block's own lines — and lexes them CONCURRENTLY under `domain.MaxSyntaxBytes`;
synchronously, because no loader goroutine holds the Doc (the loaders hand back
raw sides or marker text and Update parses them). The two sides are numbered
INDEPENDENTLY, since a block contributes a different number of lines to each;
`ensureSan` walks both cursors at once and stores `sanLine{text, mask}` per
line, literal context taking the current side's runs. The output pane assembles
from `Block.ResolvedPicks` provenance so it reuses those very `sanLine` values
— no re-sanitize and no re-lex per pick. A side holding a bare `\r` is refused,
as in `lexBlame`/`lexPreview`.
```

`README.md:440` — extend the `[ui] diff_syntax` paragraph's last sentence. Change

```
known lexer, or a side larger than 1 MB, renders plain. Set `"off"` to
disable everywhere (TUI and `gg web`).
```

to

```
known lexer, or a side larger than 1 MB, renders plain. The same switch and
lexers colour the **hunk picker** — the conflict resolver's two candidate
columns and its live output pane, and the hunk staging/unstaging pickers —
where each column is lexed with its own side's version of the file; the
cursor row there stays plain. Set `"off"` to disable everywhere (TUI and
`gg web`).
```

The CLI surface did not change, so `internal/agentskill/using-gg.md` and `agentskill.Version` stay untouched. `CLAUDE.md` stays untouched: no package responsibility or convention changed.

- [ ] **Step 7: Run the full gates**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-conflict-syntax && ./test.sh unit
cd /mnt/t/others/gigagit.worktrees/feat-conflict-syntax && ./test.sh race
```

Expected: both green (vet + gofmt + unit, then the same with `-race`). Start the race run immediately — it does not need a quiet machine.

- [ ] **Step 8: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-conflict-syntax && git add internal/tui/model.go internal/tui/picker_syntax_test.go CHANGELOG.md docs/CLAUDE-details.md README.md && git commit -m "feat(tui): wire syntax colouring into all four picker open sites

All four kinds (conflict, process-conflict, stage, unstage) get the same
runs — each has a real repo-relative path — gated by [ui] diff_syntax.
CHANGELOG, CLAUDE-details and the README diff_syntax paragraph updated.

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01NDk1DJtgLzZX7hMmxDs9nU"
```

---

## Rulings made while planning (spec §4.2 was silent)

1. **The lex is synchronous.** §4.2 says "in the loader's goroutine where one exists, else synchronously". No loader goroutine holds a `hunkpick.Doc`: `loadConflictFileCmd` returns marker text and `loadStageHunksCmd`/`loadUnstageHunksCmd` return two raw sides; the `Doc` is only assembled in `Update` (`model.go:3068/3090/3108`). So the "else" branch applies. The two sides are lexed concurrently and each is capped at `domain.MaxSyntaxBytes`.
2. **Literal context takes the CURRENT side's runs.** The lines are byte-identical on both sides; picking one keeps a single mask per literal line and one code path.
3. **A side holding a bare `\r` is not lexed.** The picker splits on `\n` exactly as `syntax.Lex` numbers lines, so it has no desync of its own — but `lexBlame` and `lexPreview` both refuse such content, and matching them beats a third rule for pathological input.
4. **All four picker kinds get colour.** Each carries a real repo-relative path (`msg.path`), so none is skipped.
5. **Literal context rows are coloured, losing their `dim` foreground on coloured tokens** — the diff pane treats its context rows the same way, and §4.2 assembles context into the lexed text precisely so it is coloured.
6. **`emph` is carried but always false this phase.** It exists so phase 6's search painter (§4.3) reuses `winCell.mask` unchanged rather than growing a second post-slice mechanism.
