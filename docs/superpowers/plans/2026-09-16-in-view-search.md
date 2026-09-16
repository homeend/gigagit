# In-View Text Search Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give the four reading surfaces — the diff view, the blame view, the View-file preview and the hunk picker — one shared in-view text search: `/` searches forward, `@` backward, both incrementally from the cursor; `enter` keeps the query, `esc` cancels; `]`/`[` step between hits with wrap-around; hits paint like word emphasis and the current hit brighter.

**Architecture:** One pure helper file (`internal/tui/textsearch.go`) owns the state (`textSearch`), the matcher (`findHits` over display strings, rune-by-rune case folding), the stepping (`nearestHit`/`stepHit`), the header badge, the pan helper and the two shared key handlers, so the four hosts contain only "build my lines, call the helper, act on the event". Painting is an **overlay onto the existing per-display-rune emphasis mask**: the `[]bool` mask every painter already carries widens to `[]emphLevel` (`none | word | hit | current`), and each host drops its hit spans onto a fresh copy of that mask after sanitization (diff cells) or after slicing (`winRow`, `winCell`) — the same post-slice mechanism the syntax class mask uses, so the per-mode offsets are already known. No new theme role: a hit wears `st().diffEmph`, the current hit `st().searchCur` (= `diffEmph` plus an underline, which survives both the Terminal theme and the reverse-video cursor row the current hit lands on).

**Tech Stack:** Go 1.26, Bubble Tea + lipgloss, `internal/syntax` (chroma classes), `internal/textdiff`, `internal/hunkpick`, `internal/i18n`, `internal/tui`.

**Spec:** `docs/superpowers/specs/2026-09-08-hunk-parity-roadmap.md` — §4.3 (lines 346–377) is binding; §4.2 (lines 328–344) built the `runMask`/`winCell` post-slice painter this plan fills in; §6's phase-6 row is the roadmap entry. The rulings that resolve everything the spec left open are in **Rulings made while planning** at the end of this file — read them before Task 1.

## Global Constraints

- **Worktree.** All work happens in `/mnt/t/others/gigagit.worktrees/feat-in-view-search` (branch `feat/in-view-search`, base main `ab91757a`). The shell cwd resets to the main checkout between commands: prefix EVERY command with `cd /mnt/t/others/gigagit.worktrees/feat-in-view-search &&`, use `git -C /mnt/t/others/gigagit.worktrees/feat-in-view-search` for git, and pass absolute paths to Write/Edit.
- **TDD.** Write the failing test first, run it, watch it fail for the stated reason, then implement. Some steps of Task 1 leave the package uncompilable between steps — the step text says so and names the single run that must go green.
- Tests call `t.Parallel()` and inject state; never `t.Setenv` in `internal/tui` tests.
- **Carve-out to the `t.Parallel()` rule:** any test that calls `lipgloss.SetColorProfile` MUST NOT call `t.Parallel()` — the colour profile is process-global and a parallel sibling's deferred reset lands mid-render and drops the ANSI codes the test asserts on. `internal/tui/window_syntax_test.go` carries this note verbatim at the top of the file; copy it into every new test file that sets the profile.
- **Implementers run ONLY `cd /mnt/t/others/gigagit.worktrees/feat-in-view-search && go test ./internal/tui/...` in the FOREGROUND.** Never `./test.sh race` — the controller runs the race gate.
- `internal/tui` never imports `internal/git` (enforced by `internal/archtest`). This plan adds no package outside `internal/tui` and touches no package outside `internal/tui` + `internal/i18n/lang`.
- **Every new or changed `i18n.T` literal lands in all four bundles in the SAME commit** (`internal/i18n/lang/{ja,ko,ru,zh}.toml`, `[strings]` table, key = the exact English literal). `TestI18nBundlesComplete` runs over the whole package and fails both ways: a catalog key with no bundle entry ("missing translation") and a bundle key no longer in the catalog ("orphaned key"). So a CHANGED literal means *delete the old key line and add the new one* in each of the four files. The four bundles are line-aligned (1859 lines each today); append new keys at the END of each `[strings]` table — the TOML table is unordered and no test pins the order. **Locate a key to delete by its exact text, never by the line numbers quoted in the tasks:** those are pre-plan numbers, and every earlier task's deletions shift them (Task 3 alone removes three lines from each bundle).
- **Diff footer ≤ 140 columns in every variant.** `TestRenderDiffViewPanes` renders at width 140 and fails on any line wider than that.
- No web change, no config key, no CLI change, no `agentskill` change (R20).
- **Commits:** `git add` the named files ONLY (never `git add -A`), message via `git commit -F <file>`, ending with the two trailers exactly:

```
Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01NDk1DJtgLzZX7hMmxDs9nU
```

- `CHANGELOG.md` contains a literal `<<<<<<<` line further down the file — it is NOT a merge marker. Add the new entry directly under the `## [Unreleased]` heading at `CHANGELOG.md:9`.

## File structure

| File | Responsibility after this plan |
|---|---|
| `internal/tui/textsearch.go` (new) | The whole search leaf: `emphLevel`, `hitSpan`, `overlayHits` (Task 1); `searchLine`/`searchHit`/`searchPos`/`textSearch`, `findHits`, `nearestHit`, `stepHit`, `badge`, `panFor`, `hitCols`, `hitsOn`, `searchTypingKey`, `searchCommandKey` (Task 2). Pure except the two key helpers, which need `Model` for history recall. |
| `internal/tui/window.go` | `winRow.emph`; masks sliced by one generic `sliceMask`/`wrapSegMask`; `colouredLine` paints emphasis; reverse-video drops classes but keeps emphasis. |
| `internal/tui/twocol.go` | `runMask.emph` widened; `renderPiece` keeps emphasis under reverse video. |
| `internal/tui/diff_render.go` | `cellSeg.off`; `segCell`/`scrollCell`/`diffCell`/`hotEmphBody` take hit spans; `styledRuns` paints three emphasis levels; new footer hint. |
| `internal/tui/diff_syntax.go` | `sanitizeCell` returns `[]emphLevel`. |
| `internal/tui/styles.go` | `searchCur` style. |
| `internal/tui/diff_view.go`, `diff_cursor.go` | Diff host: state, keys, lines, jump, pan, origin restore. |
| `internal/tui/blame_view.go` | Blame host. |
| `internal/tui/file_preview.go`, `files_view.go`, `content_popup.go` | Preview host. |
| `internal/tui/conflict_picker.go`, `conflict_process.go` | Picker host. |
| `internal/tui/help.go` | Help rows for all four hosts + the Global recall row. |
| `internal/i18n/lang/{ja,ko,ru,zh}.toml` | Translations, per commit. |

---

### Task 1: The third emphasis level, and the hit overlay

Today every painter carries a per-display-rune `emph []bool` — one bit, "this rune is inside a word-diff span". The search needs three states (word emphasis, a hit, THE current hit), so the bit widens to a small enum and one pure function paints hit spans onto it. Nothing in this task changes what the user sees: with no search active, every render is byte-identical, and the tests pin that.

Also in this task: the current hit lands on the cursor row, whose style is reverse video, and both painters throw the whole paint mask away on a reversed style. Reverse video swaps foreground and background, so per-token *colours* must indeed go — but bold and underline survive a swap, so **emphasis stays and only the class mask drops**.

**Files:**
- Create: `internal/tui/textsearch.go` (only `emphLevel`, `hitSpan`, `overlayHits` in this task; Task 2 grows the same file)
- Create: `internal/tui/search_paint_test.go`
- Modify: `internal/tui/diff_syntax.go:31-61` (`sanitizeCell`)
- Modify: `internal/tui/diff_render.go:108-171` (`cellSeg`, `wrapCells`), `:429-465` (`segCell`), `:467-538` (`scrollCell`), `:636-706` (`diffCell`, `hotEmphBody`), `:708-731` (`styledRuns`)
- Modify: `internal/tui/window.go:43-59` (`winRow`), `:150-250` (`renderWindow`), `:252-269` (`colouredLine`), `:271-283` (`sliceCls` → `sliceMask`), `:296-348` (`wrapSegCls` → `wrapSegMask`)
- Modify: `internal/tui/twocol.go:11-62` (`runMask`), `:117-163` (`cellPieces`), `:224-239` (`renderPiece`)
- Modify: `internal/tui/styles.go:54` and `:144`
- Modify (test migration, Step 8): `internal/tui/diff_render_test.go`, `internal/tui/diff_syntax_test.go:17`, `internal/tui/twocol_syntax_test.go:23,37`

**Interfaces:**
- Consumes: `syntax.Class`, `syntax.Plain`, `st()`, `styleCell`, `truncate`, `padRight`, `hslice`, `hscrollRuneOff`, `wrapHang`, `wrapWidth`, `lipgloss.Style.GetReverse`.
- Produces (every later task depends on these exact names):
  - `type emphLevel uint8` with `emphNone`, `emphWord`, `emphHit`, `emphCur`
  - `type hitSpan struct { start, end int; cur bool }` — half-open DISPLAY-rune range
  - `func overlayHits(emph []emphLevel, off, n int, hits []hitSpan) []emphLevel`
  - `func sliceMask[T any](m []T, off, n int) []T`
  - `func wrapSegMask[T any](text string, m []T, segs []string, indent, bodyW int) [][]T`
  - `func sanitizeCell(s string, spans []textdiff.Span, toks []syntax.Tok) (disp []rune, emph []emphLevel, cls []syntax.Class)`
  - `type cellSeg struct { disp []rune; emph []emphLevel; cls []syntax.Class; off int }`
  - `func wrapCells(disp []rune, emph []emphLevel, cls []syntax.Class, tw int) []cellSeg`
  - `func styledRuns(disp []rune, emph []emphLevel, cls []syntax.Class, base lipgloss.Style) string`
  - `func colouredLine(pre, body string, cls []syntax.Class, emph []emphLevel, style lipgloss.Style, w int) string`
  - `winRow.emph []emphLevel` (new field; nil = today's path)
  - `runMask.emph []emphLevel`; `func (m runMask) hasEmph() bool`
  - `func segCell(no int, seg cellSeg, gut, width int, gap, hot bool, hotStyle lipgloss.Style, mk cellMark, hits []hitSpan) string`
  - `func scrollCell(no int, text string, spans []textdiff.Span, toks []syntax.Tok, hOffset, gut, width int, gap, hot bool, hotStyle lipgloss.Style, mk cellMark, hits []hitSpan) string`
  - `func diffCell(no int, text string, gut, width int, gap, hot bool, hotStyle lipgloss.Style, spans []textdiff.Span, toks []syntax.Tok, mk cellMark, hits []hitSpan) string`
  - `func hotEmphBody(text string, spans []textdiff.Span, toks []syntax.Tok, tw int, base lipgloss.Style, hits []hitSpan) string`
  - `st().searchCur` — the current-hit style

- [ ] **Step 1: Write the failing tests**

Create `/mnt/t/others/gigagit.worktrees/feat-in-view-search/internal/tui/search_paint_test.go`:

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

// NOTE: the render tests in this file do NOT call t.Parallel() —
// lipgloss.SetColorProfile is process-global, so a parallel sibling's deferred
// reset would land mid-render and drop the ANSI codes they assert on (the same
// rule window_syntax_test.go follows). The pure overlayHits tests do.

func TestOverlayHitsReturnsInputWhenNothingIntersects(t *testing.T) {
	t.Parallel()
	base := []emphLevel{emphWord, emphNone, emphNone}
	got := overlayHits(base, 0, 3, nil)
	if &got[0] != &base[0] {
		t.Fatal("no hits must return the very same slice (the no-search path allocates nothing)")
	}
	// A hit entirely outside the window is not a reason to allocate either.
	got = overlayHits(base, 0, 3, []hitSpan{{start: 10, end: 12}})
	if &got[0] != &base[0] {
		t.Fatal("a hit outside [off, off+n) must return the input untouched")
	}
	if overlayHits(nil, 0, 3, nil) != nil {
		t.Fatal("a nil mask with no hits must stay nil")
	}
}

func TestOverlayHitsPaintsOnACopy(t *testing.T) {
	t.Parallel()
	base := []emphLevel{emphWord, emphWord, emphNone, emphNone}
	got := overlayHits(base, 0, 4, []hitSpan{{start: 1, end: 3, cur: true}})
	want := []emphLevel{emphWord, emphCur, emphCur, emphNone}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("overlay = %v, want %v", got, want)
		}
	}
	if base[1] != emphWord {
		t.Fatal("the input mask must not be written through (it is a shared cache value)")
	}
}

// A window that starts mid-line: the hit's offsets are in the FULL line's index
// space and must be shifted and clipped by off/n.
func TestOverlayHitsShiftsAndClipsToTheWindow(t *testing.T) {
	t.Parallel()
	got := overlayHits(nil, 4, 3, []hitSpan{{start: 2, end: 6}})
	want := []emphLevel{emphHit, emphHit, emphNone}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("overlay = %v, want %v", got, want)
		}
	}
}

func TestStyledRunsPaintsThreeLevels(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(prev)

	disp := []rune("abcd")
	cls := make([]syntax.Class, 4)
	plain := styledRuns(disp, make([]emphLevel, 4), cls, lipgloss.NewStyle())
	word := styledRuns(disp, []emphLevel{emphWord, emphWord, emphNone, emphNone}, cls, lipgloss.NewStyle())
	hit := styledRuns(disp, []emphLevel{emphHit, emphHit, emphNone, emphNone}, cls, lipgloss.NewStyle())
	cur := styledRuns(disp, []emphLevel{emphCur, emphCur, emphNone, emphNone}, cls, lipgloss.NewStyle())
	if word == plain {
		t.Fatal("word emphasis must change the output")
	}
	if hit != word {
		t.Fatalf("a hit paints exactly like word emphasis (spec §4.3): %q vs %q", hit, word)
	}
	if cur == hit {
		t.Fatal("the current hit must differ from an ordinary hit")
	}
	for _, s := range []string{word, hit, cur} {
		if ansi.Strip(s) != "abcd" || lipgloss.Width(s) != 4 {
			t.Fatalf("emphasis must not change the visible text or width: %q", s)
		}
	}
}

// The cursor row is reverse video, and that is exactly where the current hit
// lands: per-token COLOURS must drop (reverse would turn them into per-token
// backgrounds) but bold/underline emphasis must survive.
func TestRenderPieceReverseKeepsEmphasisDropsClasses(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(prev)

	rev := st().selectedRow
	body := "func main() {"
	n := len([]rune(body))
	cls := make([]syntax.Class, n)
	cls[0], cls[1], cls[2], cls[3] = syntax.Keyword, syntax.Keyword, syntax.Keyword, syntax.Keyword

	plainPiece := cellPiece{pre: "> ", body: body, mask: runMask{cls: cls, emph: make([]emphLevel, n)}}
	got := renderPiece(rev, plainPiece, 30)
	want := styleCell(rev, "> "+body, 30)
	if got != want {
		t.Fatalf("a reversed cell with no hit must be byte-identical to the plain path:\n got %q\nwant %q", got, want)
	}

	emph := make([]emphLevel, n)
	emph[5], emph[6], emph[7], emph[8] = emphCur, emphCur, emphCur, emphCur
	hitPiece := cellPiece{pre: "> ", body: body, mask: runMask{cls: cls, emph: emph}}
	hitOut := renderPiece(rev, hitPiece, 30)
	if hitOut == want {
		t.Fatal("a hit on the reversed cursor row must still paint")
	}
	if lipgloss.Width(hitOut) != lipgloss.Width(want) {
		t.Fatalf("the hit must not change the cell width: %d vs %d", lipgloss.Width(hitOut), lipgloss.Width(want))
	}
}

// The same ruling for the window primitive: a reversed winRow keeps emph and
// drops cls.
func TestRenderWindowReversedRowKeepsEmphasis(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(prev)

	text := "func main() {"
	n := len([]rune(text))
	cls := make([]syntax.Class, n)
	cls[0], cls[1], cls[2], cls[3] = syntax.Keyword, syntax.Keyword, syntax.Keyword, syntax.Keyword
	o := winOpts{w: 30, h: 1, mode: modeCutoff}

	withCls := renderWindow([]winRow{{text: text, cls: cls, style: st().selectedRow}}, o)
	noMask := renderWindow([]winRow{{text: text, style: st().selectedRow}}, o)
	if withCls[0] != noMask[0] {
		t.Fatalf("a reversed row must still drop its class mask:\n got %q\nwant %q", withCls[0], noMask[0])
	}

	emph := make([]emphLevel, n)
	emph[5], emph[6] = emphCur, emphCur
	withHit := renderWindow([]winRow{{text: text, cls: cls, emph: emph, style: st().selectedRow}}, o)
	if withHit[0] == noMask[0] {
		t.Fatal("a hit on the reversed selected row must paint")
	}
	if lipgloss.Width(withHit[0]) != lipgloss.Width(noMask[0]) {
		t.Fatalf("the hit must not change the row width: %d vs %d", lipgloss.Width(withHit[0]), lipgloss.Width(noMask[0]))
	}
}

// An unreversed row paints emphasis through colouredLine even with no class
// mask at all (blame with syntax off is exactly that row).
func TestRenderWindowEmphWithoutClasses(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(prev)

	text := "alpha beta"
	emph := make([]emphLevel, len([]rune(text)))
	emph[6], emph[7], emph[8], emph[9] = emphHit, emphHit, emphHit, emphHit
	o := winOpts{w: 20, h: 1, mode: modeCutoff}
	got := renderWindow([]winRow{{text: text, emph: emph}}, o)
	plain := renderWindow([]winRow{{text: text}}, o)
	if got[0] == plain[0] {
		t.Fatal("an emph-only row must paint (cls stays nil for an unlexed blame)")
	}
	if ansi.Strip(got[0]) != ansi.Strip(plain[0]) {
		t.Fatalf("emphasis changed the visible text: %q vs %q", ansi.Strip(got[0]), ansi.Strip(plain[0]))
	}
}

// emph, like cls, WINS over decorate — a row that sets both takes the painted
// path and decorate is never called (no caller combines them; this is the
// sibling of TestRenderWindowClsWinsOverDecorate).
func TestRenderWindowEmphWinsOverDecorate(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(prev)

	text := "alpha beta"
	emph := make([]emphLevel, len([]rune(text)))
	emph[0] = emphHit
	deco := func(visible string, hscroll, visualLine int) string { return "DECORATED" }
	out := renderWindow([]winRow{{text: text, emph: emph, decorate: deco}}, winOpts{w: 20, h: 1, mode: modeCutoff})
	if strings.Contains(ansi.Strip(out[0]), "DECORATED") {
		t.Fatalf("an emphasized row must take the painted path, not decorate: %q", out[0])
	}
}

// wrapCells must record where each segment starts, so the renderer can overlay
// whole-line hit offsets onto a wrapped segment without re-laying-out.
func TestWrapCellsRecordsSegmentOffsets(t *testing.T) {
	t.Parallel()
	disp := []rune("aaaa bbbb cccc")
	segs := wrapCells(disp, make([]emphLevel, len(disp)), make([]syntax.Class, len(disp)), 5)
	if len(segs) < 2 {
		t.Fatalf("want several segments, got %d", len(segs))
	}
	off := 0
	for i, s := range segs {
		if s.off != off {
			t.Fatalf("segment %d: off = %d, want %d", i, s.off, off)
		}
		off += len(s.disp)
	}
	if off != len(disp) {
		t.Fatalf("segments cover %d runes, want %d", off, len(disp))
	}
}

// A hit paints in every long-line mode of the diff pane.
func TestDiffCellsPaintHits(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(prev)

	hits := []hitSpan{{start: 0, end: 3, cur: true}}
	plain := diffCell(1, "foobar", 3, 20, false, false, st().diffDelCell, nil, nil, noMark(), nil)
	hit := diffCell(1, "foobar", 3, 20, false, false, st().diffDelCell, nil, nil, noMark(), hits)
	if hit == plain {
		t.Fatal("diffCell must paint a hit even with no spans and no syntax runs")
	}
	if lipgloss.Width(hit) != lipgloss.Width(plain) {
		t.Fatalf("width changed: %d vs %d", lipgloss.Width(hit), lipgloss.Width(plain))
	}

	sPlain := scrollCell(1, strings.Repeat("x", 40)+"foobar", nil, nil, 30, 3, 20, false, false, st().diffDelCell, noMark(), nil)
	sHit := scrollCell(1, strings.Repeat("x", 40)+"foobar", nil, nil, 30, 3, 20, false, false, st().diffDelCell, noMark(), []hitSpan{{start: 40, end: 46}})
	if sHit == sPlain {
		t.Fatal("scrollCell must paint a hit inside the scrolled window")
	}

	disp, emph, cls := sanitizeCell("aaaa bbbb", nil, nil)
	segs := wrapCells(disp, emph, cls, 5)
	segPlain := segCell(1, segs[1], 3, 20, false, false, st().diffAddCell, noMark(), nil)
	segHit := segCell(1, segs[1], 3, 20, false, false, st().diffAddCell, noMark(), []hitSpan{{start: 5, end: 9, cur: true}})
	if segHit == segPlain {
		t.Fatal("segCell must paint a hit that lands on a wrap continuation")
	}
}
```

- [ ] **Step 2: Run the tests to watch them fail**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-in-view-search && go test ./internal/tui/ -run 'TestOverlayHits|TestStyledRunsPaints|TestRenderPieceReverse|TestRenderWindowReversed|TestRenderWindowEmph|TestWrapCellsRecords|TestDiffCellsPaintHits' 2>&1 | head -20`

Expected: FAIL to build — `undefined: emphLevel`, `undefined: overlayHits`, `undefined: hitSpan`, plus "too many arguments" on `diffCell`/`scrollCell`/`segCell`. The package stays uncompilable until Step 9; that is expected.

- [ ] **Step 3: Create the emphasis level and the overlay**

Create `/mnt/t/others/gigagit.worktrees/feat-in-view-search/internal/tui/textsearch.go`:

```go
package tui

// emphLevel is one display rune's emphasis in a paint mask. It replaces the
// single bool the word-diff used to carry, because the in-view search needs two
// more states than "emphasized": an ordinary hit and THE current one (spec
// §4.3). Precedence is the enum order — the current hit outranks a hit, a hit
// outranks word emphasis, and any of them outranks the syntax colour, which is
// what keeps a search hit legible on a changed line.
type emphLevel uint8

const (
	emphNone emphLevel = iota // plain: the syntax class (if any) paints
	emphWord                  // inside a word-diff span (what sanitizeCell marks)
	emphHit                   // inside a search hit
	emphCur                   // inside the CURRENT search hit
)

// hitSpan is a search hit reduced to what a painter needs: a half-open range of
// DISPLAY runes in the line the painter is about to draw, and whether it is the
// current hit. Painters never see searchHit — hitsOn converts.
type hitSpan struct {
	start, end int
	cur        bool
}

// overlayHits paints hit spans onto a display-rune emphasis mask. emph is the
// mask of the n runes starting at display-rune offset off (nil when the caller
// has no mask yet — an unlexed blame line); the spans are offsets in the SAME
// index space as off, i.e. the whole sanitized line.
//
// When nothing intersects the window the input is returned untouched, so a view
// with no search allocates nothing and renders byte-identically. When something
// does, a COPY is painted: every mask a caller hands in may be a shared cache
// value (the picker gives the same sanLine to the grid and to the output pane,
// and textdiff rows are shared across views), so writing through is a bug.
func overlayHits(emph []emphLevel, off, n int, hits []hitSpan) []emphLevel {
	if n <= 0 || len(hits) == 0 {
		return emph
	}
	var out []emphLevel
	for _, h := range hits {
		lo, hi := h.start-off, h.end-off
		if lo < 0 {
			lo = 0
		}
		if hi > n {
			hi = n
		}
		if lo >= hi {
			continue
		}
		if out == nil {
			out = make([]emphLevel, n)
			copy(out, emph)
		}
		lvl := emphHit
		if h.cur {
			lvl = emphCur
		}
		for i := lo; i < hi; i++ {
			out[i] = lvl
		}
	}
	if out == nil {
		return emph
	}
	return out
}
```

- [ ] **Step 4: Widen the diff painters**

In `/mnt/t/others/gigagit.worktrees/feat-in-view-search/internal/tui/diff_syntax.go`, change `sanitizeCell`'s signature and the three `emph = append(...)` sites (the body is otherwise unchanged). Replace:

```go
func sanitizeCell(s string, spans []textdiff.Span, toks []syntax.Tok) (disp []rune, emph []bool, cls []syntax.Class) {
	s = strings.TrimSuffix(s, "\r")
	runes := []rune(s)
	cover := coverMask(len(runes), spans)
	classes := classMask(len(runes), toks)
	col := 0
	for raw, r := range runes {
		on, c := cover[raw], classes[raw]
```

with:

```go
func sanitizeCell(s string, spans []textdiff.Span, toks []syntax.Tok) (disp []rune, emph []emphLevel, cls []syntax.Class) {
	s = strings.TrimSuffix(s, "\r")
	runes := []rune(s)
	cover := coverMask(len(runes), spans)
	classes := classMask(len(runes), toks)
	col := 0
	for raw, r := range runes {
		// sanitizeCell only ever marks emphWord: search hits are overlaid on
		// top of its mask afterwards (overlayHits), never mixed in here.
		on, c := emphNone, classes[raw]
		if cover[raw] {
			on = emphWord
		}
```

and update the doc comment two lines above the signature, replacing `emph marks runes whose source raw rune is covered by a word-diff span` with `emph marks runes whose source raw rune is covered by a word-diff span (emphWord; search hits are overlaid later)`.

In `/mnt/t/others/gigagit.worktrees/feat-in-view-search/internal/tui/diff_render.go`:

1. `cellSeg` (at `:108-115`) — replace

```go
type cellSeg struct {
	disp []rune
	emph []bool
	cls  []syntax.Class
}
```

with

```go
type cellSeg struct {
	disp []rune
	emph []emphLevel
	cls  []syntax.Class
	// off is this segment's display-rune offset within the sanitized line it
	// was wrapped from. Search hits are found on the whole line, so the
	// renderer needs it to place them on a continuation without re-wrapping —
	// wrap mode must not re-lay-out on every keystroke (spec §4.3).
	off int
}
```

2. `wrapCells` (at `:124`) — change the signature to `func wrapCells(disp []rune, emph []emphLevel, cls []syntax.Class, tw int) []cellSeg` and the append at `:160` to carry the offset:

```go
		segs = append(segs, cellSeg{disp: disp[start:brk], emph: emph[start:brk], cls: cls[start:brk], off: start})
```

3. `styledRuns` (at `:713`) — replace the whole function body's signature and switch:

```go
func styledRuns(disp []rune, emph []emphLevel, cls []syntax.Class, base lipgloss.Style) string {
	s := st()
	var b strings.Builder
	for i := 0; i < len(disp); {
		j := i + 1
		for j < len(disp) && emph[j] == emph[i] && cls[j] == cls[i] {
			j++
		}
		seg := string(disp[i:j])
		switch emph[i] {
		case emphCur:
			b.WriteString(base.Inherit(s.searchCur).Render(seg))
		case emphHit, emphWord:
			b.WriteString(base.Inherit(s.diffEmph).Render(seg))
		default:
			b.WriteString(s.syntaxStyle(base, cls[i]).Render(seg))
		}
		i = j
	}
	return b.String()
}
```

and extend its doc comment's first sentence to `styledRuns renders disp grouping consecutive runes by (emph, cls): the current search hit wears st().searchCur, an ordinary hit and a word-diff span st().diffEmph (both inherited over base, so the cell background shows through), the rest wear base plus their syntax class's foreground.`

4. `segCell` (at `:429`) — add the parameter and overlay before painting. Replace

```go
func segCell(no int, seg cellSeg, gut, width int, gap, hot bool, hotStyle lipgloss.Style, mk cellMark) string {
```

with

```go
func segCell(no int, seg cellSeg, gut, width int, gap, hot bool, hotStyle lipgloss.Style, mk cellMark, hits []hitSpan) string {
```

and replace the body line at `:454`

```go
	body := styledRuns(seg.disp, seg.emph, seg.cls, base)
```

with

```go
	// The hits are offsets in the whole sanitized line; seg.off says where this
	// segment starts in it, so they land on the right continuation with no
	// relayout.
	body := styledRuns(seg.disp, overlayHits(seg.emph, seg.off, len(seg.disp), hits), seg.cls, base)
```

5. `scrollCell` (at `:467`) — add the parameter, overlay onto the FULL mask before the column window loop slices it, and forward the hits to the `diffCell` fast path. Replace

```go
func scrollCell(no int, text string, spans []textdiff.Span, toks []syntax.Tok, hOffset, gut, width int, gap, hot bool, hotStyle lipgloss.Style, mk cellMark) string {
```

with

```go
func scrollCell(no int, text string, spans []textdiff.Span, toks []syntax.Tok, hOffset, gut, width int, gap, hot bool, hotStyle lipgloss.Style, mk cellMark, hits []hitSpan) string {
```

and replace

```go
	disp, emph, cls := sanitizeCell(text, spans, toks)
	full := lipgloss.Width(string(disp))
	if hOffset <= 0 && full <= tw {
		return diffCell(no, text, gut, width, false, hot, hotStyle, spans, toks, mk)
	}
```

with

```go
	disp, emph, cls := sanitizeCell(text, spans, toks)
	// Overlay on the whole line BEFORE the window loop: it slices emph for free.
	emph = overlayHits(emph, 0, len(disp), hits)
	full := lipgloss.Width(string(disp))
	if hOffset <= 0 && full <= tw {
		return diffCell(no, text, gut, width, false, hot, hotStyle, spans, toks, mk, hits)
	}
```

and the window loop's mask declaration at `:497`

```go
	var wemph []bool
```

with

```go
	var wemph []emphLevel
```

6. `diffCell` (at `:636`) — add the parameter and widen the enriched-path gate: a hit on a plain context row must paint, and today that row takes the `padRight(truncate(sanitizeLine(...)))` fast path. Replace

```go
func diffCell(no int, text string, gut, width int, gap, hot bool, hotStyle lipgloss.Style, spans []textdiff.Span, toks []syntax.Tok, mk cellMark) string {
```

with

```go
func diffCell(no int, text string, gut, width int, gap, hot bool, hotStyle lipgloss.Style, spans []textdiff.Span, toks []syntax.Tok, mk cellMark, hits []hitSpan) string {
```

and

```go
	var bodyTxt string
	if len(spans) > 0 || len(toks) > 0 {
```

with

```go
	var bodyTxt string
	// A search hit is enough on its own: without it a hit on an unchanged,
	// unlexed row would take the plain path and never paint.
	if len(spans) > 0 || len(toks) > 0 || len(hits) > 0 {
```

and the call inside it

```go
		bodyTxt = hotEmphBody(text, spans, toks, tw, base)
```

with

```go
		bodyTxt = hotEmphBody(text, spans, toks, tw, base, hits)
```

7. `hotEmphBody` (at `:678`) — add the parameter and overlay. Replace

```go
func hotEmphBody(text string, spans []textdiff.Span, toks []syntax.Tok, tw int, base lipgloss.Style) string {
	disp, emph, cls := sanitizeCell(text, spans, toks)
```

with

```go
func hotEmphBody(text string, spans []textdiff.Span, toks []syntax.Tok, tw int, base lipgloss.Style, hits []hitSpan) string {
	disp, emph, cls := sanitizeCell(text, spans, toks)
	emph = overlayHits(emph, 0, len(disp), hits)
```

- [ ] **Step 5: Widen the window primitive**

In `/mnt/t/others/gigagit.worktrees/feat-in-view-search/internal/tui/window.go`:

1. `winRow` — add the field after `cls` (keep the existing `cls` doc block intact) and extend the reverse-video sentence. The struct becomes:

```go
	cls []syntax.Class
	// emph is an optional emphasis level per DISPLAY RUNE of text, filled by
	// the in-view search with its hits (spec §4.3); nil = no emphasis, the path
	// every non-searching caller takes. It is sliced with the text exactly like
	// cls, and — unlike cls — it SURVIVES a reverse-video style: bold and
	// underline still read after the swap, and the current hit is precisely the
	// row the cursor sits on.
	emph []emphLevel
}
```

2. `renderWindow`'s `dline` (at `:150-160`) — add the field next to `cls`:

```go
		cls []syntax.Class
		// emph rides alongside cls; either one being non-nil takes the painted
		// path (a blame row with syntax off carries emphasis and no classes).
		emph []emphLevel
		pre  string
```

3. The per-row mask build (at `:162-202`) — replace

```go
		var segs []string
		var segCls [][]syntax.Class // nil unless the row carries a class mask
		hs := 0
		// A row whose style reverses video would turn per-token foregrounds into
		// per-token backgrounds, so it takes the plain path (see winRow.cls).
		rcls := r.cls
		if r.style.GetReverse() {
			rcls = nil
		}
		switch o.mode {
		case modeWrap:
			indent := wrapAlignIndent(r.text, bodyW)
			segs = wrapHang(r.text, bodyW, indent, 1<<20) // huge cap => clean full wrap, no ellipsis
			if rcls != nil {
				segCls = wrapSegCls(r.text, rcls, segs, indent, bodyW)
			}
		case modeScroll:
			segs = []string{hslice(r.text, o.hscroll, bodyW)}
			hs = o.hscroll
			if rcls != nil {
				segCls = [][]syntax.Class{sliceCls(rcls, hscrollRuneOff(r.text, o.hscroll), len([]rune(segs[0])))}
			}
		default:
			segs = []string{truncate(r.text, bodyW)}
			if rcls != nil {
				mask := sliceCls(rcls, 0, len([]rune(segs[0])))
```

with

```go
		var segs []string
		var segCls [][]syntax.Class  // nil unless the row carries a class mask
		var segEmph [][]emphLevel    // nil unless the row carries emphasis
		hs := 0
		// A row whose style reverses video would turn per-token foregrounds into
		// per-token backgrounds, so the CLASS mask drops (see winRow.cls). The
		// emphasis mask does not: bold/underline survive the swap, which is how
		// a search hit stays visible on the selected row (winRow.emph).
		rcls := r.cls
		if r.style.GetReverse() {
			rcls = nil
		}
		remph := r.emph
		switch o.mode {
		case modeWrap:
			indent := wrapAlignIndent(r.text, bodyW)
			segs = wrapHang(r.text, bodyW, indent, 1<<20) // huge cap => clean full wrap, no ellipsis
			if rcls != nil {
				segCls = wrapSegMask(r.text, rcls, segs, indent, bodyW)
			}
			if remph != nil {
				segEmph = wrapSegMask(r.text, remph, segs, indent, bodyW)
			}
		case modeScroll:
			segs = []string{hslice(r.text, o.hscroll, bodyW)}
			hs = o.hscroll
			if rcls != nil {
				segCls = [][]syntax.Class{sliceMask(rcls, hscrollRuneOff(r.text, o.hscroll), len([]rune(segs[0])))}
			}
			if remph != nil {
				segEmph = [][]emphLevel{sliceMask(remph, hscrollRuneOff(r.text, o.hscroll), len([]rune(segs[0])))}
			}
		default:
			segs = []string{truncate(r.text, bodyW)}
			if remph != nil {
				em := sliceMask(remph, 0, len([]rune(segs[0])))
				// Same ellipsis fix as the class mask below: the last slot
				// lands on the first DROPPED rune, so a hit that starts right
				// past the cut would emphasize the synthetic "…".
				if lipgloss.Width(r.text) > bodyW && len(em) > 0 {
					em[len(em)-1] = emphNone
				}
				segEmph = [][]emphLevel{em}
			}
			if rcls != nil {
				mask := sliceMask(rcls, 0, len([]rune(segs[0])))
```

(the ellipsis fix-up inside that `if rcls != nil` block, and its comment, stay exactly as they are — only `sliceCls` became `sliceMask`).

4. The segment loop (at `:203-222`) — replace

```go
		if len(segs) == 0 {
			segs = []string{""}
			segCls = nil
		}
```

with

```go
		if len(segs) == 0 {
			segs = []string{""}
			segCls, segEmph = nil, nil
		}
```

and

```go
			if segCls == nil {
				dl = append(dl, dline{text: pre + s, style: r.style, deco: r.decorate, hs: hs, si: si, row: ri})
				continue
			}
			dl = append(dl, dline{text: s, pre: pre, cls: segCls[si], style: r.style, hs: hs, si: si, row: ri})
```

with

```go
			if segCls == nil && segEmph == nil {
				dl = append(dl, dline{text: pre + s, style: r.style, deco: r.decorate, hs: hs, si: si, row: ri})
				continue
			}
			dl = append(dl, dline{text: s, pre: pre, cls: maskAt(segCls, si), emph: maskAt(segEmph, si), style: r.style, hs: hs, si: si, row: ri})
```

5. The emit loop (at `:232-249`) — replace

```go
		if dl[idx].cls != nil {
			out = append(out, colouredLine(dl[idx].pre, dl[idx].text, dl[idx].cls, dl[idx].style, w))
			continue
		}
```

with

```go
		if dl[idx].cls != nil || dl[idx].emph != nil {
			out = append(out, colouredLine(dl[idx].pre, dl[idx].text, dl[idx].cls, dl[idx].emph, dl[idx].style, w))
			continue
		}
```

6. `colouredLine` (at `:257`) — replace

```go
func colouredLine(pre, body string, cls []syntax.Class, style lipgloss.Style, w int) string {
	disp := []rune(body)
	cls = sliceCls(cls, 0, len(disp)) // defensive: exactly one class per rune
	var b strings.Builder
	if pre != "" {
		b.WriteString(style.Render(pre))
	}
	b.WriteString(styledRuns(disp, make([]bool, len(disp)), cls, style))
```

with

```go
func colouredLine(pre, body string, cls []syntax.Class, emph []emphLevel, style lipgloss.Style, w int) string {
	disp := []rune(body)
	// Defensive: exactly one entry per rune on both masks (either may be nil).
	cls = sliceMask(cls, 0, len(disp))
	emph = sliceMask(emph, 0, len(disp))
	var b strings.Builder
	if pre != "" {
		b.WriteString(style.Render(pre))
	}
	b.WriteString(styledRuns(disp, emph, cls, style))
```

and change its doc's parenthetical `(styledRuns, no word-diff emphasis)` to `(styledRuns, with the search's emphasis mask when it carries one)`.

7. `sliceCls` (at `:271-283`) — replace the function with the generic one plus the index helper:

```go
// sliceMask returns n entries of a per-display-rune mask starting at off,
// padding with the zero value when the mask runs out (a cutoff ellipsis, a
// clamped token end) and reading an out-of-range window as all-zero. One helper
// for both masks the window carries: syntax classes (zero = syntax.Plain) and
// emphasis levels (zero = emphNone). A nil mask yields n zero entries, which is
// exactly "plain".
func sliceMask[T any](m []T, off, n int) []T {
	out := make([]T, n)
	if off < 0 {
		off = 0
	}
	for i := 0; i < n && off+i < len(m); i++ {
		out[i] = m[off+i]
	}
	return out
}

// maskAt returns the ith per-segment mask, or nil when the row carries none.
func maskAt[T any](m [][]T, i int) []T {
	if i < len(m) {
		return m[i]
	}
	return nil
}
```

8. `wrapSegCls` (at `:296-348`) — rename to the generic `wrapSegMask` and swap `syntax.Class` for `T`. Replace the signature and the two typed lines:

```go
func wrapSegCls(text string, cls []syntax.Class, segs []string, indent, bodyW int) [][]syntax.Class {
	if indent > bodyW-1 {
		indent = bodyW - 1 // wrapHang's own clamp
	}
	try := func(pad int) [][]syntax.Class {
		out := make([][]syntax.Class, len(segs))
```

with

```go
func wrapSegMask[T any](text string, m []T, segs []string, indent, bodyW int) [][]T {
	if indent > bodyW-1 {
		indent = bodyW - 1 // wrapHang's own clamp
	}
	try := func(pad int) [][]T {
		out := make([][]T, len(segs))
```

then inside `try`:

```go
			m := make([]syntax.Class, p)
			out[i] = append(m, sliceCls(cls, off, len(r)-p)...)
```

becomes (note the local rename — `m` is now the mask parameter):

```go
			pre := make([]T, p)
			out[i] = append(pre, sliceMask(m, off, len(r)-p)...)
```

and the all-plain fallback at the end:

```go
	plain := make([][]syntax.Class, len(segs))
	for i, s := range segs {
		plain[i] = make([]syntax.Class, len([]rune(s)))
	}
	return plain
```

becomes

```go
	plain := make([][]T, len(segs))
	for i, s := range segs {
		plain[i] = make([]T, len([]rune(s)))
	}
	return plain
```

Finally update the first line of its doc comment to `wrapSegMask maps a per-display-rune mask (syntax classes or emphasis levels) onto the segments wrapHang produced for text.` and, in the `modeCutoff` comment block inside `renderWindow`, change the two mentions of `sliceCls` to `sliceMask`.

- [ ] **Step 6: Widen the two-column cell mask**

In `/mnt/t/others/gigagit.worktrees/feat-in-view-search/internal/tui/twocol.go`:

1. `runMask` — replace

```go
type runMask struct {
	cls  []syntax.Class
	emph []bool
}
```

with

```go
type runMask struct {
	cls  []syntax.Class
	emph []emphLevel
}

// hasEmph reports whether any rune actually carries emphasis. A mask of all
// emphNone is indistinguishable from none at all, and the reverse-video path
// uses that to keep an un-searched cursor row byte-identical to the plain one.
func (m runMask) hasEmph() bool {
	for _, e := range m.emph {
		if e != emphNone {
			return true
		}
	}
	return false
}
```

2. `slice` — the allocation line becomes `out := runMask{cls: make([]syntax.Class, n), emph: make([]emphLevel, n)}`, and its doc's `padding with Plain/false` becomes `padding with Plain/emphNone`.

3. `pad` — replace `emph: append(make([]bool, n), m.emph...)` with `emph: append(make([]emphLevel, n), m.emph...)`.

4. `cellPieces`' cutoff ellipsis fix — replace `m.emph[len(m.emph)-1] = false` with `m.emph[len(m.emph)-1] = emphNone`.

5. `renderPiece` — replace

```go
func renderPiece(style lipgloss.Style, p cellPiece, w int) string {
	if p.mask.empty() || style.GetReverse() {
		return styleCell(style, p.pre+p.body, w)
	}
```

with

```go
func renderPiece(style lipgloss.Style, p cellPiece, w int) string {
	if style.GetReverse() {
		// Reverse swaps foreground and background, so per-token colours would
		// paint per-token BACKGROUNDS: the class mask drops. Emphasis is bold /
		// underline, which reads either way — and the search's current hit
		// lands on exactly this cell, the cursor row (spec §4.3). A mask with
		// nothing emphasized keeps the byte-identical plain path.
		if !p.mask.hasEmph() {
			return styleCell(style, p.pre+p.body, w)
		}
		p.mask = runMask{emph: p.mask.emph}
	}
	if p.mask.empty() {
		return styleCell(style, p.pre+p.body, w)
	}
```

and extend the `winCell.mask` doc block at `:68-74`, replacing the sentence `and a cell whose style REVERSES video drops the mask — reverse swaps foreground and background, so per-token colours would paint per-token backgrounds (the same ruling winRow.cls follows).` with `and a cell whose style REVERSES video drops the CLASS half of the mask — reverse swaps foreground and background, so per-token colours would paint per-token backgrounds (the same ruling winRow.cls follows) — while the emphasis half survives, because bold/underline read either way and the in-view search's current hit sits on the cursor cell.`

- [ ] **Step 7: Add the current-hit style**

In `/mnt/t/others/gigagit.worktrees/feat-in-view-search/internal/tui/styles.go`, add the field right after `diffEmph` (`:54`):

```go
	diffEmph       lipgloss.Style
	searchCur      lipgloss.Style
```

and build it right after `s.diffEmph` (`:144`):

```go
	s.diffEmph = ns().Bold(true).Foreground(bright)
	// The in-view search's CURRENT hit: word emphasis plus an underline. No new
	// theme role (spec §4.3 asks for "word emphasis, the current hit
	// brighter"): underline is the one attribute that reads under the Terminal
	// theme, where bright is empty and diffEmph is bold-only, AND on the
	// reverse-video cursor row the current hit always lands on.
	s.searchCur = s.diffEmph.Underline(true)
```

- [ ] **Step 8: Migrate the existing tests that build or read an emph mask**

Nine edits, all mechanical. In
`/mnt/t/others/gigagit.worktrees/feat-in-view-search/internal/tui/diff_render_test.go`:

1. The two `styledRuns` calls — replace

```go
	out := styledRuns(disp, []bool{false, false, false, false}, cls, lipgloss.NewStyle())
```

with

```go
	out := styledRuns(disp, []emphLevel{emphNone, emphNone, emphNone, emphNone}, cls, lipgloss.NewStyle())
```

and

```go
	out = styledRuns(disp, []bool{true, true, false, false}, cls, lipgloss.NewStyle())
```

with

```go
	out = styledRuns(disp, []emphLevel{emphWord, emphWord, emphNone, emphNone}, cls, lipgloss.NewStyle())
```

2. The `runesEmph` fixture — replace

```go
func runesEmph(s string, on bool) (disp []rune, emph []bool) {
	disp = []rune(s)
	emph = make([]bool, len(disp))
	for i := range emph {
		emph[i] = on
	}
	return disp, emph
}
```

with

```go
func runesEmph(s string, on bool) (disp []rune, emph []emphLevel) {
	disp = []rune(s)
	emph = make([]emphLevel, len(disp))
	for i := range emph {
		if on {
			emph[i] = emphWord
		}
	}
	return disp, emph
}
```

3. `TestWrapCellsCarriesEmphMask`'s inner check — replace

```go
		for _, on := range s.emph {
			if !on {
```

with

```go
		for _, on := range s.emph {
			if on == emphNone {
```

4. `TestWrapCellsSlicesClassMaskAlongside` — replace

```go
	emph := make([]bool, len(disp))
	cls := make([]syntax.Class, len(disp))
```

with

```go
	emph := make([]emphLevel, len(disp))
	cls := make([]syntax.Class, len(disp))
```

5. Every `diffCell(`, `scrollCell(` and `segCell(` call gains a final `nil`
argument (the hit spans) — 23 calls, at lines 178–179, 317–318, 327, 336, 347,
354, 500 and 598–611. E.g.

```go
	emph := diffCell(1, "foobar", 3, 20, false, true, st().diffDelCell, []textdiff.Span{{Start: 0, End: 3}}, nil, noMark())
```

becomes

```go
	emph := diffCell(1, "foobar", 3, 20, false, true, st().diffDelCell, []textdiff.Span{{Start: 0, End: 3}}, nil, noMark(), nil)
```

6. The two `cellSeg` literals in that same table (lines 604–605) — replace both
occurrences of

```go
cellSeg{disp: []rune("abc"), emph: make([]bool, 3), cls: make([]syntax.Class, 3)}
```

with

```go
cellSeg{disp: []rune("abc"), emph: make([]emphLevel, 3), cls: make([]syntax.Class, 3)}
```

In `/mnt/t/others/gigagit.worktrees/feat-in-view-search/internal/tui/diff_syntax_test.go`,
`TestSanitizeSpansMapsThroughTabExpansion` — replace

```go
	want := []bool{false, false, false, false, true}
```

with

```go
	want := []emphLevel{emphNone, emphNone, emphNone, emphNone, emphWord}
```

(the `coverMask` test just below keeps `[]bool` — `coverMask` itself is
unchanged, it is `sanitizeCell` that maps its bools to levels.)

In `/mnt/t/others/gigagit.worktrees/feat-in-view-search/internal/tui/twocol_syntax_test.go`,
replace both mask constructions

```go
	return runMask{cls: cls, emph: make([]bool, len(cls))}
```

with

```go
	return runMask{cls: cls, emph: make([]emphLevel, len(cls))}
```

and

```go
	pm := runMask{cls: make([]syntax.Class, n), emph: make([]bool, n)}
```

with

```go
	pm := runMask{cls: make([]syntax.Class, n), emph: make([]emphLevel, n)}
```

- [ ] **Step 9: Build and run the whole package**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-in-view-search && go build ./cmd/gg && go test ./internal/tui/...`

Expected: PASS, including the pre-existing byte-identity pins `TestFileContentLinesPlainPathUnchanged`, `TestTwoColPlainMaskIsByteIdenticalToNoMask`, `TestRenderWindowClsWinsOverDecorate`, `TestEmphasisActuallyChangesOutput` and `TestRenderDiffViewPanes`. If `TestTwoColPlainMaskIsByteIdenticalToNoMask` fails, `renderPiece`'s reverse branch is testing `empty()` before `hasEmph()` — the order in Step 6 matters.

Any remaining compile error will be a `diffCell`/`scrollCell`/`segCell` call site that still passes the old argument count; the only non-test ones are inside `diffPaneLines` (`internal/tui/diff_render.go:399-419`) — pass `nil` for `hits` there for now; Task 3 wires the real spans.

- [ ] **Step 10: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-in-view-search && \
cat > /tmp/gg-msg-t1.txt <<'EOF'
feat(tui): widen the paint mask to an emphasis LEVEL and overlay hit spans

The in-view search (spec §4.3) needs two emphasis states the word-diff bool
cannot express: a hit and THE current hit. emph []bool becomes
[]emphLevel across every painter, overlayHits paints display-rune hit spans
onto a copy of a mask (never through a shared cache value), cellSeg records
each wrapped segment's offset so wrap mode needs no relayout per keystroke,
and the class/emphasis slicing collapses into one generic sliceMask /
wrapSegMask pair.

Reverse video now drops only the class mask: bold and underline read after
the swap, and the current hit lands on exactly the reversed cursor row.
st().searchCur is diffEmph plus an underline — no new theme role.

No user-visible change: with no search active every render is
byte-identical, pinned by the reverse-video and plain-mask tests.

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01NDk1DJtgLzZX7hMmxDs9nU
EOF
git -C /mnt/t/others/gigagit.worktrees/feat-in-view-search add \
  internal/tui/textsearch.go internal/tui/search_paint_test.go \
  internal/tui/diff_syntax.go internal/tui/diff_render.go internal/tui/window.go \
  internal/tui/twocol.go internal/tui/styles.go \
  internal/tui/diff_render_test.go internal/tui/diff_syntax_test.go internal/tui/twocol_syntax_test.go && \
git -C /mnt/t/others/gigagit.worktrees/feat-in-view-search commit -F /tmp/gg-msg-t1.txt
```

---

### Task 2: The search helper — state, matcher, stepping, badge, keys

One leaf every host calls. It holds no host state: a host builds `[]searchLine`
(its rows as DISPLAY strings), hands them to `findHits`, and acts on the
`searchEvent` the two key helpers return. Everything here is pure except
`searchTypingKey`, which needs `Model` for the history-recall dropdown.

**Files:**
- Modify: `internal/tui/textsearch.go` (grow the file Task 1 created)
- Modify: `internal/tui/search_history.go:17-25` (the scope consts)
- Create: `internal/tui/textsearch_test.go`

**Interfaces:**
- Consumes: `m.recallUpdate(scope string, msg tea.KeyMsg, curQuery string) (Model, string, bool, bool)`, `m.recordSearch(scope, phrase string) (Model, tea.Cmd)`, `m.recallReset() Model` (`internal/tui/search_history.go`); `overlayHits`, `hitSpan`, `emphLevel` (Task 1).
- Produces:
  - `const scopeInView = "inview"`
  - `type searchLine struct { row, side int; text string }`
  - `type searchHit struct { row, side, start, end int }`
  - `type searchPos struct { row, side, col int }`
  - `type textSearch struct { query string; typing, backward bool; hits []searchHit; cur int; origin searchPos }`
  - `func (s textSearch) active() bool`, `func (s textSearch) badge() string`, `func (s textSearch) hitsOn(row, side int) []hitSpan`
  - `func (s *textSearch) open(backward bool, pos searchPos)`, `func (s *textSearch) clear()`, `func (s *textSearch) refindFrom(lines []searchLine, pos searchPos)`
  - `func findHits(lines []searchLine, query string) []searchHit`
  - `func nearestHit(hits []searchHit, pos searchPos, backward bool) int`
  - `func stepHit(hits []searchHit, pos searchPos, delta int) int`
  - `func panFor(hOffset, tw, colStart, colEnd int) int`
  - `func hitCols(text string, h searchHit) (int, int)`
  - `type searchEvent int` with `searchNotOurs`, `searchIgnored`, `searchChanged`, `searchCommitted`, `searchCancelled`, `searchCleared`, `searchOpenFwd`, `searchOpenBack`, `searchNext`, `searchPrev`
  - `func (m Model) searchTypingKey(s *textSearch, msg tea.KeyMsg) (Model, tea.Cmd, searchEvent)`
  - `func searchCommandKey(s *textSearch, msg tea.KeyMsg) searchEvent`

- [ ] **Step 1: Write the failing tests**

Create `/mnt/t/others/gigagit.worktrees/feat-in-view-search/internal/tui/textsearch_test.go`:

```go
package tui

import (
	"reflect"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// srchLines is one searchable line per text, numbered in order. (Named for the
// search, not "lines", so it cannot shadow a local `lines` in a sibling test.)
func srchLines(texts ...string) []searchLine {
	out := make([]searchLine, len(texts))
	for i, t := range texts {
		out[i] = searchLine{row: i, side: 1, text: t}
	}
	return out
}

func TestFindHitsCaseInsensitiveAndOrdered(t *testing.T) {
	t.Parallel()
	got := findHits(srchLines("Alpha beta", "no match", "alphaALPHA"), "alpha")
	want := []searchHit{
		{row: 0, side: 1, start: 0, end: 5},
		{row: 2, side: 1, start: 0, end: 5},
		{row: 2, side: 1, start: 5, end: 10},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("findHits = %v, want %v", got, want)
	}
	if findHits(srchLines("anything"), "") != nil {
		t.Fatal("an empty query matches nothing")
	}
}

// Offsets must be RUNE offsets into the display string: a multi-byte rune
// before the hit (byte offsets would drift) and a wide glyph inside it.
func TestFindHitsCountsRunesNotBytes(t *testing.T) {
	t.Parallel()
	got := findHits(srchLines("日本語 alpha"), "alpha")
	if len(got) != 1 || got[0].start != 4 || got[0].end != 9 {
		t.Fatalf("hit = %v, want one hit at runes [4,9)", got)
	}
	got = findHits(srchLines("x漢字y"), "漢字")
	if len(got) != 1 || got[0].start != 1 || got[0].end != 3 {
		t.Fatalf("wide-glyph hit = %v, want [1,3)", got)
	}
}

// Overlapping matches are not reported: the walk advances past each hit.
func TestFindHitsDoesNotOverlap(t *testing.T) {
	t.Parallel()
	got := findHits(srchLines("aaaa"), "aa")
	if len(got) != 2 || got[0].start != 0 || got[1].start != 2 {
		t.Fatalf("hits = %v, want [0,2) and [2,4)", got)
	}
}

func TestNearestHitSnapsForwardAndBackwardWithWrap(t *testing.T) {
	t.Parallel()
	hits := []searchHit{
		{row: 1, side: 0, start: 2, end: 5},
		{row: 4, side: 1, start: 0, end: 3},
	}
	if got := nearestHit(hits, searchPos{row: 2}, false); got != 1 {
		t.Fatalf("forward from row 2 = %d, want 1", got)
	}
	if got := nearestHit(hits, searchPos{row: 9}, false); got != 0 {
		t.Fatalf("forward past the end must wrap to 0, got %d", got)
	}
	if got := nearestHit(hits, searchPos{row: 2}, true); got != 0 {
		t.Fatalf("backward from row 2 = %d, want 0", got)
	}
	if got := nearestHit(hits, searchPos{row: 0}, true); got != 1 {
		t.Fatalf("backward before the start must wrap to the last, got %d", got)
	}
	if got := nearestHit(nil, searchPos{}, false); got != -1 {
		t.Fatalf("no hits = %d, want -1", got)
	}
}

// ] is the first hit STRICTLY after the position, [ the last strictly before —
// so sitting exactly on a hit steps off it. Both wrap.
func TestStepHitIsStrictAndWraps(t *testing.T) {
	t.Parallel()
	hits := []searchHit{
		{row: 1, side: 0, start: 2, end: 5},
		{row: 1, side: 1, start: 0, end: 3},
		{row: 4, side: 1, start: 7, end: 10},
	}
	on := searchPos{row: 1, side: 0, col: 2}
	if got := stepHit(hits, on, 1); got != 1 {
		t.Fatalf("next from hit 0 = %d, want 1 (left side steps to the right side)", got)
	}
	if got := stepHit(hits, on, -1); got != 2 {
		t.Fatalf("prev from hit 0 must wrap to the last, got %d", got)
	}
	last := searchPos{row: 4, side: 1, col: 7}
	if got := stepHit(hits, last, 1); got != 0 {
		t.Fatalf("next from the last must wrap to 0, got %d", got)
	}
	if got := stepHit(nil, on, 1); got != -1 {
		t.Fatalf("no hits = %d, want -1", got)
	}
}

func TestHitsOnReturnsSpansAndMarksCurrent(t *testing.T) {
	t.Parallel()
	s := textSearch{
		query: "a",
		hits: []searchHit{
			{row: 1, side: 0, start: 0, end: 1},
			{row: 1, side: 1, start: 3, end: 4},
			{row: 2, side: 1, start: 5, end: 6},
		},
		cur: 1,
	}
	got := s.hitsOn(1, 1)
	want := []hitSpan{{start: 3, end: 4, cur: true}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("hitsOn(1,1) = %v, want %v", got, want)
	}
	if got := s.hitsOn(1, 0); len(got) != 1 || got[0].cur {
		t.Fatalf("hitsOn(1,0) = %v, want one non-current span", got)
	}
	if got := s.hitsOn(3, 1); got != nil {
		t.Fatalf("a row with no hits must return nil, got %v", got)
	}
}

func TestBadge(t *testing.T) {
	t.Parallel()
	var s textSearch
	if s.badge() != "" {
		t.Fatalf("an inactive search has no badge, got %q", s.badge())
	}
	s = textSearch{query: "foo", typing: true, cur: -1}
	if got := s.badge(); got != "/foo█  0/0" {
		t.Fatalf("typing badge = %q", got)
	}
	s = textSearch{query: "foo", backward: true, hits: make([]searchHit, 12), cur: 2}
	if got := s.badge(); got != "@foo  3/12" {
		t.Fatalf("backward badge = %q, want %q", got, "@foo  3/12")
	}
	s = textSearch{typing: true, cur: -1}
	if got := s.badge(); got != "/█" {
		t.Fatalf("empty typing badge = %q", got)
	}
}

func TestPanForMovesMinimally(t *testing.T) {
	t.Parallel()
	// Already inside the window: no pan.
	if got := panFor(10, 20, 12, 16); got != 10 {
		t.Fatalf("visible hit panned to %d, want 10", got)
	}
	// Left of the window: the hit's start becomes the left edge.
	if got := panFor(10, 20, 4, 8); got != 4 {
		t.Fatalf("pan left = %d, want 4", got)
	}
	// Right of the window: the hit's end becomes the right edge.
	if got := panFor(10, 20, 34, 38); got != 18 {
		t.Fatalf("pan right = %d, want 18", got)
	}
	// A hit wider than the window shows its head.
	if got := panFor(0, 10, 40, 80); got != 40 {
		t.Fatalf("oversized hit = %d, want 40", got)
	}
	if got := panFor(5, 20, 0, 2); got != 0 {
		t.Fatalf("pan to the left edge = %d, want 0", got)
	}
}

// Display COLUMNS, not rune indexes: a wide glyph before the hit occupies two.
func TestHitColsUsesDisplayColumns(t *testing.T) {
	t.Parallel()
	start, end := hitCols("漢字abc", searchHit{start: 2, end: 5})
	if start != 4 || end != 7 {
		t.Fatalf("hitCols = (%d,%d), want (4,7)", start, end)
	}
}

func TestSearchCommandKey(t *testing.T) {
	t.Parallel()
	var s textSearch
	if got := searchCommandKey(&s, keyMsg("/")); got != searchOpenFwd {
		t.Fatalf("/ = %v", got)
	}
	if got := searchCommandKey(&s, keyMsg("@")); got != searchOpenBack {
		t.Fatalf("@ = %v", got)
	}
	// No query: the stepping keys are inert and esc is not ours.
	if got := searchCommandKey(&s, keyMsg("]")); got != searchIgnored {
		t.Fatalf("] with no query = %v, want searchIgnored", got)
	}
	if got := searchCommandKey(&s, keyMsg("esc")); got != searchNotOurs {
		t.Fatalf("esc with no query = %v, want searchNotOurs", got)
	}
	s.query = "x"
	if got := searchCommandKey(&s, keyMsg("]")); got != searchNext {
		t.Fatalf("] = %v", got)
	}
	if got := searchCommandKey(&s, keyMsg("[")); got != searchPrev {
		t.Fatalf("[ = %v", got)
	}
	if got := searchCommandKey(&s, keyMsg("esc")); got != searchCleared {
		t.Fatalf("esc with a query = %v, want searchCleared", got)
	}
	if got := searchCommandKey(&s, keyMsg("j")); got != searchNotOurs {
		t.Fatalf("an unrelated key = %v, want searchNotOurs", got)
	}
}

func TestSearchTypingKeyBuildsAndCommits(t *testing.T) {
	t.Parallel()
	m := Model{}
	s := &textSearch{typing: true, cur: -1}

	m, _, ev := m.searchTypingKey(s, keyMsg("a"))
	if ev != searchChanged || s.query != "a" {
		t.Fatalf("rune: ev=%v query=%q", ev, s.query)
	}
	m, _, ev = m.searchTypingKey(s, keyType(tea.KeySpace))
	if ev != searchChanged || s.query != "a " {
		t.Fatalf("space: ev=%v query=%q", ev, s.query)
	}
	m, _, ev = m.searchTypingKey(s, keyType(tea.KeyBackspace))
	if ev != searchChanged || s.query != "a" {
		t.Fatalf("backspace: ev=%v query=%q", ev, s.query)
	}
	// Every other key is swallowed while typing: j is query text, not motion.
	_, _, ev = m.searchTypingKey(s, keyType(tea.KeyTab))
	if ev != searchIgnored {
		t.Fatalf("tab while typing = %v, want searchIgnored", ev)
	}
	_, _, ev = m.searchTypingKey(s, keyType(tea.KeyEnter))
	if ev != searchCommitted || s.typing {
		t.Fatalf("enter: ev=%v typing=%v", ev, s.typing)
	}
}

func TestSearchTypingKeyEscCancelsAndEmptyEnterCancels(t *testing.T) {
	t.Parallel()
	m := Model{}
	s := &textSearch{typing: true, query: "ab", hits: make([]searchHit, 2), cur: 1}
	_, _, ev := m.searchTypingKey(s, keyType(tea.KeyEsc))
	if ev != searchCancelled || s.query != "" || s.typing || s.hits != nil || s.cur != -1 {
		t.Fatalf("esc: ev=%v state=%+v", ev, *s)
	}

	s = &textSearch{typing: true, cur: -1}
	_, _, ev = m.searchTypingKey(s, keyType(tea.KeyEnter))
	if ev != searchCancelled || s.typing {
		t.Fatalf("enter on an empty query = %v (typing=%v), want searchCancelled", ev, s.typing)
	}
}

// alt+↓ opens the shared in-view ring; the previewed phrase becomes the query
// and the host must re-find (searchChanged).
func TestSearchTypingKeyRecallPreviewsTheRing(t *testing.T) {
	t.Parallel()
	m := Model{searchHist: map[string][]string{scopeInView: {"older"}}}
	s := &textSearch{typing: true, query: "ol", cur: -1}
	m, _, ev := m.searchTypingKey(s, keyMsg("alt+down"))
	if ev != searchChanged || s.query != "older" {
		t.Fatalf("recall: ev=%v query=%q", ev, s.query)
	}
	if !m.recallOpen {
		t.Fatal("the dropdown must be open")
	}
	// esc with the dropdown OPEN closes it and restores the draft — it must NOT
	// cancel the search.
	m, _, ev = m.searchTypingKey(s, keyType(tea.KeyEsc))
	if ev != searchChanged || s.query != "ol" || !s.typing {
		t.Fatalf("esc under recall: ev=%v query=%q typing=%v", ev, s.query, s.typing)
	}
	if m.recallOpen {
		t.Fatal("the dropdown must have closed")
	}
}

func TestRefindFromSnapsToTheNearestHit(t *testing.T) {
	t.Parallel()
	s := &textSearch{query: "a"}
	s.refindFrom(srchLines("xa", "xx", "aa"), searchPos{row: 1})
	if len(s.hits) != 3 {
		t.Fatalf("hits = %v, want 3", s.hits)
	}
	if s.cur != 1 {
		t.Fatalf("cur = %d, want 1 (the first hit at or after row 1)", s.cur)
	}
	s.query = "zzz"
	s.refindFrom(srchLines("xa"), searchPos{})
	if len(s.hits) != 0 || s.cur != -1 {
		t.Fatalf("a query with no match must leave no hits and cur=-1: %v %d", s.hits, s.cur)
	}
}
```

Add the two imports the test needs at the top: `tea "github.com/charmbracelet/bubbletea"` (for `tea.KeySpace` etc.) alongside `reflect` and `testing`. `keyMsg` is `internal/tui/model_test.go:11`; `keyType` is `internal/tui/irebase_view_test.go:136`.

- [ ] **Step 2: Run the tests to watch them fail**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-in-view-search && go test ./internal/tui/ -run 'TestFindHits|TestNearestHit|TestStepHit|TestHitsOn|TestBadge|TestPanFor|TestHitCols|TestSearchCommandKey|TestSearchTypingKey|TestRefindFrom' 2>&1 | head -20`

Expected: FAIL to build — `undefined: searchLine`, `undefined: findHits`, `undefined: scopeInView`, …

- [ ] **Step 3: Add the shared history scope**

In `/mnt/t/others/gigagit.worktrees/feat-in-view-search/internal/tui/search_history.go`, replace

```go
// Search-history ring scopes. The panel filter and @ highlight share scopePanel.
const (
	scopePanel    = "panel"
	scopeFiletree = "filetree"
	scopeBookmark = "bookmark"
	scopeShelf    = "shelf"
	scopeShellCmd = "shellcmd"
)
```

with

```go
// Search-history ring scopes. The panel filter and @ highlight share scopePanel;
// the four in-view readers (diff, blame, file preview, hunk picker) share
// scopeInView, so a phrase typed in one is recallable in the next (spec §4.3).
const (
	scopePanel    = "panel"
	scopeFiletree = "filetree"
	scopeBookmark = "bookmark"
	scopeShelf    = "shelf"
	scopeShellCmd = "shellcmd"
	scopeInView   = "inview"
)
```

- [ ] **Step 4: Write the search state, matcher and stepping**

Append to `/mnt/t/others/gigagit.worktrees/feat-in-view-search/internal/tui/textsearch.go` (the file Task 1 created), and add the imports `"fmt"`, `"sort"`, `"unicode"`, `"github.com/charmbracelet/lipgloss"`, `tea "github.com/charmbracelet/bubbletea"`:

```go
// searchLine is one searchable line handed to findHits: the host's own row id,
// which column it belongs to (0 = left / current / the only column, 1 = right /
// incoming) and the DISPLAY text — already sanitized, because every offset this
// file produces is a display-rune offset the painters index masks with.
//
// NOTE: (*contentPopup).searchLine is an unrelated method that renders the
// popup's own /-input line; a method name and a package type may coincide.
type searchLine struct {
	row  int
	side int
	text string
}

// searchHit is one match: [start, end) display-rune offsets into the line
// (row, side). Hits are kept in document order — row, then side, then start.
type searchHit struct {
	row, side  int
	start, end int
}

// searchPos is a position in the same coordinates, used to decide which hit is
// "next". col may be -1, meaning "before anything on this row" — which is what
// a host whose cursor is a whole row reports when the cursor is not sitting on
// a hit, so ] finds the first hit on the line the user just walked to.
type searchPos struct {
	row, side, col int
}

// textSearch is one host's in-view search state (spec §4.3). cur is an index
// into hits, -1 when there is none. origin is where the incremental search
// measures from — the cursor when / or @ opened it; the host keeps its own view
// state (scroll offsets, its 2D cursor) to restore when esc cancels.
type textSearch struct {
	query    string
	typing   bool
	backward bool
	hits     []searchHit
	cur      int
	origin   searchPos
}

// active reports whether a search is live: typing, or a committed query.
func (s textSearch) active() bool { return s.typing || s.query != "" }

// open starts a search from pos. The host records its own view origin first.
func (s *textSearch) open(backward bool, pos searchPos) {
	s.query = ""
	s.typing = true
	s.backward = backward
	s.hits = nil
	s.cur = -1
	s.origin = pos
}

// clear drops the query and every hit (esc on a committed search).
func (s *textSearch) clear() {
	s.query = ""
	s.typing = false
	s.hits = nil
	s.cur = -1
}

// refindFrom re-runs the search over lines and re-snaps cur to the hit nearest
// pos. Hosts call it after every keystroke (from the origin) and after any
// rebuild that can move rows (from the cursor).
func (s *textSearch) refindFrom(lines []searchLine, pos searchPos) {
	s.hits = findHits(lines, s.query)
	s.cur = nearestHit(s.hits, pos, s.backward)
}

// badge is the header status: "/foo  3/12", "@foo" for a backward search, a
// block cursor while typing, "0/0" when nothing matched, "" when no search is
// active. Deliberately built WITHOUT i18n.T — it is punctuation, the user's own
// query and two numbers, so there is nothing to translate and no bundle key to
// keep in sync.
func (s textSearch) badge() string {
	if !s.active() {
		return ""
	}
	lead := "/"
	if s.backward {
		lead = "@"
	}
	b := lead + s.query
	if s.typing {
		b += "█"
	}
	if s.query == "" {
		return b
	}
	n, i := len(s.hits), 0
	if n > 0 && s.cur >= 0 && s.cur < n {
		i = s.cur + 1
	}
	return b + "  " + fmt.Sprintf("%d/%d", i, n)
}

// hitsOn returns the spans a painter must paint on one line, the current hit
// flagged. Binary search: this runs once per visible row per frame, and a big
// file can hold thousands of hits.
func (s textSearch) hitsOn(row, side int) []hitSpan {
	if len(s.hits) == 0 {
		return nil
	}
	i := sort.Search(len(s.hits), func(i int) bool {
		h := s.hits[i]
		return h.row > row || (h.row == row && h.side >= side)
	})
	var out []hitSpan
	for ; i < len(s.hits) && s.hits[i].row == row && s.hits[i].side == side; i++ {
		out = append(out, hitSpan{start: s.hits[i].start, end: s.hits[i].end, cur: i == s.cur})
	}
	return out
}

// findHits returns every case-insensitive match of query in lines, in the order
// the lines are given (so the caller controls document order) and left to right
// within a line. Overlapping matches are not reported — the walk advances past
// each hit.
//
// Matching walks RUNES: strings.ToLower + strings.Index would return BYTE
// offsets, and folding can change a string's rune count, either of which puts
// the painted span on the wrong columns.
func findHits(lines []searchLine, query string) []searchHit {
	q := foldRunes(query)
	if len(q) == 0 {
		return nil
	}
	var out []searchHit
	for _, ln := range lines {
		t := foldRunes(ln.text)
		for i := 0; i+len(q) <= len(t); {
			if runesMatchAt(t, q, i) {
				out = append(out, searchHit{row: ln.row, side: ln.side, start: i, end: i + len(q)})
				i += len(q)
				continue
			}
			i++
		}
	}
	return out
}

// foldRunes lowercases s rune by rune — 1:1, so an index into the result is an
// index into s's runes and therefore into the display mask.
func foldRunes(s string) []rune {
	r := []rune(s)
	for i, c := range r {
		r[i] = unicode.ToLower(c)
	}
	return r
}

func runesMatchAt(t, q []rune, i int) bool {
	for k := range q {
		if t[i+k] != q[k] {
			return false
		}
	}
	return true
}

// before reports whether a comes strictly before b in document order.
func (a searchPos) before(b searchPos) bool {
	if a.row != b.row {
		return a.row < b.row
	}
	if a.side != b.side {
		return a.side < b.side
	}
	return a.col < b.col
}

func hitPos(h searchHit) searchPos {
	return searchPos{row: h.row, side: h.side, col: h.start}
}

// nearestHit is the index of the first hit at or after pos (forward) or the
// last at or before it (backward), wrapping around; -1 when there are none.
// This is what the incremental search snaps to after every keystroke, so a hit
// the cursor already sits on stays selected instead of jumping away.
func nearestHit(hits []searchHit, pos searchPos, backward bool) int {
	if len(hits) == 0 {
		return -1
	}
	if backward {
		for i := len(hits) - 1; i >= 0; i-- {
			if !pos.before(hitPos(hits[i])) {
				return i
			}
		}
		return len(hits) - 1
	}
	for i, h := range hits {
		if !hitPos(h).before(pos) {
			return i
		}
	}
	return 0
}

// stepHit is ] and [: the first hit STRICTLY after pos (delta >= 0) or the last
// strictly before it (delta < 0), wrapping around; -1 when there are no hits.
// Strictness is what makes a second ] move off the hit the cursor landed on,
// and it means the stepping keys always walk document order regardless of
// whether the search was opened with / or @.
func stepHit(hits []searchHit, pos searchPos, delta int) int {
	if len(hits) == 0 {
		return -1
	}
	if delta >= 0 {
		for i, h := range hits {
			if pos.before(hitPos(h)) {
				return i
			}
		}
		return 0
	}
	for i := len(hits) - 1; i >= 0; i-- {
		if hitPos(hits[i]).before(pos) {
			return i
		}
	}
	return len(hits) - 1
}

// panFor returns the horizontal offset that brings display columns
// [colStart, colEnd) inside the window [hOffset, hOffset+tw), moving as little
// as possible — a hit already on screen never pans. A hit wider than the window
// shows its head. The columns are DISPLAY columns (lipgloss.Width of the text
// before the hit), never rune indexes: a wide glyph occupies two.
func panFor(hOffset, tw, colStart, colEnd int) int {
	if tw < 1 {
		tw = 1
	}
	if colEnd > colStart+tw {
		colEnd = colStart + tw
	}
	switch {
	case colStart < hOffset:
		hOffset = colStart
	case colEnd > hOffset+tw:
		hOffset = colEnd - tw
	}
	if hOffset < 0 {
		hOffset = 0
	}
	return hOffset
}

// hitCols maps a hit's rune offsets into display columns of its (sanitized)
// line — what panFor wants.
func hitCols(text string, h searchHit) (int, int) {
	r := []rune(text)
	start, end := h.start, h.end
	if start > len(r) {
		start = len(r)
	}
	if end > len(r) {
		end = len(r)
	}
	return lipgloss.Width(string(r[:start])), lipgloss.Width(string(r[:end]))
}
```

- [ ] **Step 5: Write the two shared key handlers**

Append to the same file:

```go
// searchEvent is what a host must do after one of the shared key handlers has
// looked at a key.
type searchEvent int

const (
	searchNotOurs   searchEvent = iota // not a search key: run the host's own handling
	searchIgnored                      // swallowed; the host does nothing
	searchChanged                      // the query changed: re-find and re-snap from the origin
	searchCommitted                    // enter: the query stays, typing stops (re-find: recall may have replaced it)
	searchCancelled                    // esc while typing: restore the view origin, no query left
	searchCleared                      // esc on a committed query: drop query + hits, stay in the view
	searchOpenFwd                      // /
	searchOpenBack                     // @
	searchNext                         // ]
	searchPrev                         // [
)

// searchTypingKey handles one key while s is capturing text. History recall
// runs FIRST (alt+↑/↓ means "scroll the view" in two of the hosts, so it may
// only mean "recall" inside the typing branch), then the small fixed vocabulary
// of an inline gg search field: esc, enter, backspace, space, runes. Every
// other key is swallowed — while typing, j is query text, not motion.
func (m Model) searchTypingKey(s *textSearch, msg tea.KeyMsg) (Model, tea.Cmd, searchEvent) {
	if !s.typing {
		return m, nil, searchNotOurs
	}
	if nm, nq, handled, commit := m.recallUpdate(scopeInView, msg, s.query); handled {
		m = nm
		s.query = nq
		if commit { // enter accepted a ring entry
			s.typing = false
			rm, cmd := m.recordSearch(scopeInView, s.query)
			return rm, cmd, searchCommitted
		}
		// A previewed ring entry (or esc closing the dropdown and restoring the
		// draft) changed the displayed query: re-find, do not cancel.
		return m, nil, searchChanged
	} else {
		m = nm
	}
	switch msg.Type {
	case tea.KeyEsc:
		s.clear()
		return m, nil, searchCancelled
	case tea.KeyEnter:
		s.typing = false
		if s.query == "" { // nothing typed: record nothing, leave no query active
			s.clear()
			return m, nil, searchCancelled
		}
		rm, cmd := m.recordSearch(scopeInView, s.query)
		return rm, cmd, searchCommitted
	case tea.KeyBackspace, tea.KeyCtrlH, tea.KeyDelete:
		if r := []rune(s.query); len(r) > 0 {
			s.query = string(r[:len(r)-1])
		}
		return m, nil, searchChanged
	case tea.KeySpace:
		s.query += " "
		return m, nil, searchChanged
	case tea.KeyRunes:
		s.query += string(msg.Runes)
		return m, nil, searchChanged
	}
	return m, nil, searchIgnored
}

// searchCommandKey handles the search keys a host sees while NOT typing. esc is
// claimed only when a query is live, so a host's own esc (close the view) keeps
// working; ] and [ are inert without one.
func searchCommandKey(s *textSearch, msg tea.KeyMsg) searchEvent {
	switch msg.String() {
	case "/":
		return searchOpenFwd
	case "@":
		return searchOpenBack
	case "]":
		if s.query == "" {
			return searchIgnored
		}
		return searchNext
	case "[":
		if s.query == "" {
			return searchIgnored
		}
		return searchPrev
	case "esc":
		if s.query != "" {
			return searchCleared
		}
	}
	return searchNotOurs
}
```

- [ ] **Step 6: Run the tests**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-in-view-search && go test ./internal/tui/...`

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-in-view-search && \
cat > /tmp/gg-msg-t2.txt <<'EOF'
feat(tui): the in-view search helper — state, matcher, stepping, keys

textsearch.go grows the leaf the four readers will share (spec §4.3): the
search state, a rune-walking case-insensitive matcher over DISPLAY strings
(so a hit's offsets are already the painter's offsets), at/after and
strictly-after stepping with wrap-around, the header badge, the minimal
horizontal pan, and the two key handlers — a typing branch with history
recall on the new shared "inview" ring, and a command branch for / @ ] [ and
the two-stage esc.

No host uses it yet.

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01NDk1DJtgLzZX7hMmxDs9nU
EOF
git -C /mnt/t/others/gigagit.worktrees/feat-in-view-search add \
  internal/tui/textsearch.go internal/tui/textsearch_test.go internal/tui/search_history.go && \
git -C /mnt/t/others/gigagit.worktrees/feat-in-view-search commit -F /tmp/gg-msg-t2.txt
```

---

### Task 3: The diff view host

The first and richest host: both sides are searched, the line cursor moves to
the hit, the view scrolls to it and — in scroll mode — pans to its column.

**Files:**
- Modify: `internal/tui/diff_view.go:40-96` (struct), `:138-146` (`rebuild`), `:222-241` (`clampHOffset`), `:688-707` (key preamble)
- Modify: `internal/tui/diff_render.go:93-106` (`diffHintFor`), `:283-291` (header), `:345-423` (`diffPaneLines`)
- Modify: `internal/tui/help.go:50` (Global recall row), `:278-283` (Diff view rows)
- Modify: `internal/i18n/lang/{ja,ko,ru,zh}.toml`
- Create: `internal/tui/diff_search_test.go`

**Interfaces:**
- Consumes: everything Task 2 produced; `v.setCursorLine(li, body int)` (`diff_cursor.go:90`), `v.scroll(delta, body int)` (`diff_view.go:263`), `v.clampHOffset()`, `gutterWidth(v.full)`, `sanitizeLine`, `m.diffBodyRows()`, `m.recallReset()`.
- Produces:
  - `type diffOrigin struct { curLine, offset, hOffset int }`
  - `diffView.search textSearch`, `diffView.searchOrig diffOrigin`, `diffView.sanLeft/sanRight []string`
  - `func (v *diffView) searchLines() []searchLine`
  - `func (v *diffView) searchPos() searchPos`
  - `func (v *diffView) goToHit(i, body int)`
  - `func (v *diffView) textWidth() int`
  - `func (v *diffView) restoreSearchOrigin(body int)`
  - `func (m Model) diffSearchKey(v *diffView, msg tea.KeyMsg, body int) (Model, tea.Cmd, bool)` — `true` = the key was the search's, the caller returns immediately

- [ ] **Step 1: Write the failing tests**

Create `/mnt/t/others/gigagit.worktrees/feat-in-view-search/internal/tui/diff_search_test.go`:

```go
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
```

- [ ] **Step 2: Run the tests to watch them fail**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-in-view-search && go test ./internal/tui/ -run TestDiffSearch 2>&1 | head -20`

Expected: FAIL — `v.search undefined (type *diffView has no field or method search)`.

- [ ] **Step 3: Add the search state to the view**

In `/mnt/t/others/gigagit.worktrees/feat-in-view-search/internal/tui/diff_view.go`, add to `diffView` right before the closing brace (after the `previewSet *domain.PreviewNoteSet` field):

```go
	previewSet *domain.PreviewNoteSet
	// search is the in-view text search (spec §4.3). It is per VIEW, not per
	// file: stepping to another file (N/P, home/end) replaces the whole
	// diffView, so the query does not follow — the new file is a new search.
	search textSearch
	// searchOrig is the view state a live search will restore if esc cancels it.
	searchOrig diffOrigin
	// sanLeft/sanRight are the per-logical-line DISPLAY strings the search
	// matches against (sanitizeLine of the raw side; identical to what the
	// panes paint). Built lazily on the first search and dropped by rebuild,
	// which is the only thing that changes v.lines.
	sanLeft  []string
	sanRight []string
}

// diffOrigin is where a live search started: the cursor line, the vertical
// scroll and the horizontal pan, restored verbatim when esc cancels.
type diffOrigin struct{ curLine, offset, hOffset int }
```

In `rebuild` (`:138`), drop the cache — add the two lines at the top of the function:

```go
func (v *diffView) rebuild() {
	v.sanLeft, v.sanRight = nil, nil // the line stream is about to change
	if v.partial {
```

- [ ] **Step 4: Add the view's search helpers**

Append to `/mnt/t/others/gigagit.worktrees/feat-in-view-search/internal/tui/diff_view.go` (after `segAt`, keeping the file's layout-helper grouping):

```go
// textWidth is one pane's text column count — the pane minus the gutter and
// its separating space. relayout and clampHOffset do this same arithmetic;
// this is the shared copy the search's pan uses.
func (v *diffView) textWidth() int {
	paneW := (v.width - 1) / 2
	if paneW < 4 {
		paneW = 4
	}
	tw := paneW - gutterWidth(v.full) - 1
	if tw < 1 {
		tw = 1
	}
	return tw
}

// searchLines is what the in-view search matches: the VISIBLE logical lines as
// the display strings the panes paint, so a hit's offsets are already the
// painter's offsets. Partial mode's folded rows are not in v.lines and are
// therefore not searched — this is an in-view search (spec §4.3).
//
// A row whose two sides carry the same text (Same) is searched on the RIGHT
// only: searching both would make ] stop twice on one piece of text.
func (v *diffView) searchLines() []searchLine {
	if v.sanLeft == nil {
		v.sanLeft = make([]string, len(v.lines))
		v.sanRight = make([]string, len(v.lines))
		for i, ln := range v.lines {
			if ln.Fold > 0 {
				continue
			}
			v.sanLeft[i] = sanitizeLine(ln.Row.Left)
			v.sanRight[i] = sanitizeLine(ln.Row.Right)
		}
	}
	out := make([]searchLine, 0, len(v.lines))
	for i, ln := range v.lines {
		if ln.Fold > 0 {
			continue
		}
		switch ln.Row.Kind {
		case textdiff.Del:
			out = append(out, searchLine{row: i, side: 0, text: v.sanLeft[i]})
		case textdiff.Add:
			out = append(out, searchLine{row: i, side: 1, text: v.sanRight[i]})
		case textdiff.Changed:
			out = append(out, searchLine{row: i, side: 0, text: v.sanLeft[i]})
			out = append(out, searchLine{row: i, side: 1, text: v.sanRight[i]})
		default: // Same
			out = append(out, searchLine{row: i, side: 1, text: v.sanRight[i]})
		}
	}
	return out
}

// searchPos is where ] and [ measure from: the current hit while the cursor
// still sits on its row (so ] steps OFF it), else the head of the cursor line
// (so ] finds the first hit on the line the user just walked to).
func (v *diffView) searchPos() searchPos {
	if v.search.cur >= 0 && v.search.cur < len(v.search.hits) {
		if h := v.search.hits[v.search.cur]; h.row == v.curLine {
			return searchPos{row: h.row, side: h.side, col: h.start}
		}
	}
	return searchPos{row: v.curLine, side: 0, col: -1}
}

// goToHit selects hits[i]: the cursor moves to its line, the view scrolls
// minimally to show it, and in scroll mode the pane pans so the hit's columns
// are on screen (a hit hidden past the right edge is no hit at all).
func (v *diffView) goToHit(i, body int) {
	if i < 0 || i >= len(v.search.hits) {
		return
	}
	h := v.search.hits[i]
	v.search.cur = i
	v.setCursorLine(h.row, body)
	if v.long != longScroll || h.row >= len(v.sanRight) {
		return
	}
	text := v.sanRight[h.row]
	if h.side == 0 {
		text = v.sanLeft[h.row]
	}
	cs, ce := hitCols(text, h)
	v.hOffset = panFor(v.hOffset, v.textWidth(), cs, ce)
	v.clampHOffset()
}

// restoreSearchOrigin puts the view back exactly where / or @ found it.
func (v *diffView) restoreSearchOrigin(body int) {
	v.curLine, v.offset, v.hOffset = v.searchOrig.curLine, v.searchOrig.offset, v.searchOrig.hOffset
	v.clampHOffset()
	v.scroll(0, body)
}
```

While here, make `clampHOffset` (`:222`) use the shared arithmetic — replace its first eight lines

```go
func (v *diffView) clampHOffset() {
	paneW := (v.width - 1) / 2
	if paneW < 4 {
		paneW = 4
	}
	tw := paneW - gutterWidth(v.full) - 1
	if tw < 1 {
		tw = 1
	}
	max := v.maxCell - tw
```

with

```go
func (v *diffView) clampHOffset() {
	max := v.maxCell - v.textWidth()
```

- [ ] **Step 5: Route the keys**

Append to `/mnt/t/others/gigagit.worktrees/feat-in-view-search/internal/tui/diff_view.go`:

```go
// diffSearchKey gives the in-view search first refusal on a key. It reports
// handled == true when the key was the search's, in which case the caller must
// return immediately: while typing, every key belongs to the query.
func (m Model) diffSearchKey(v *diffView, msg tea.KeyMsg, body int) (Model, tea.Cmd, bool) {
	if v.search.typing {
		nm, cmd, ev := m.searchTypingKey(&v.search, msg)
		m = nm
		switch ev {
		case searchChanged, searchCommitted:
			// searchCommitted re-finds too: recall's enter can commit a phrase
			// the user never typed.
			v.search.refindFrom(v.searchLines(), v.search.origin)
			if v.search.cur >= 0 {
				v.goToHit(v.search.cur, body)
			} else {
				v.restoreSearchOrigin(body)
			}
		case searchCancelled:
			v.restoreSearchOrigin(body)
		}
		return m, cmd, true
	}
	switch searchCommandKey(&v.search, msg) {
	case searchOpenFwd, searchOpenBack:
		v.searchOrig = diffOrigin{curLine: v.curLine, offset: v.offset, hOffset: v.hOffset}
		v.search.open(msg.String() == "@", v.searchPos())
		return m.recallReset(), nil, true
	case searchNext:
		v.goToHit(stepHit(v.search.hits, v.searchPos(), 1), body)
		return m, nil, true
	case searchPrev:
		v.goToHit(stepHit(v.search.hits, v.searchPos(), -1), body)
		return m, nil, true
	case searchCleared:
		v.search.clear()
		return m, nil, true
	case searchIgnored:
		return m, nil, true
	}
	return m, nil, false
}
```

and call it in `updateDiffViewKey`, right after the body line — replace

```go
	zc := v.zCycle
	v.zCycle = alignCenter
	body := m.diffBodyRows()
	switch msg.String() {
```

with

```go
	zc := v.zCycle
	v.zCycle = alignCenter
	body := m.diffBodyRows()
	if nm, cmd, handled := m.diffSearchKey(v, msg, body); handled {
		return nm, cmd
	}
	switch msg.String() {
```

A rebuild can move rows under a committed query, so re-find after the two mode
toggles. In the `case "f":` arm, replace

```go
		v.partial = !v.partial
		v.rebuild()
		m.diffPartial = v.partial
```

with

```go
		v.partial = !v.partial
		v.rebuild()
		v.refindAfterRebuild()
		m.diffPartial = v.partial
```

and in the `case "ctrl+w":` arm, replace

```go
		v.hOffset = 0
		v.relayout(v.width)
		m.diffLong = v.long
```

with

```go
		v.hOffset = 0
		v.relayout(v.width)
		v.refindAfterRebuild()
		m.diffLong = v.long
```

and append the helper next to the others:

```go
// refindAfterRebuild re-runs a committed search over the new line stream and
// re-snaps to the hit nearest the cursor. Partial mode hides rows, so the hits
// (and their row indices) genuinely change.
func (v *diffView) refindAfterRebuild() {
	if v.search.query == "" {
		return
	}
	v.search.refindFrom(v.searchLines(), v.searchPos())
}
```

- [ ] **Step 6: Paint the hits and show the badge**

In `/mnt/t/others/gigagit.worktrees/feat-in-view-search/internal/tui/diff_render.go`, inside `diffPaneLines`, add the per-row lookup right after the syntax-run lookup — replace

```go
		lt, rt := tokAt(v.oldTok, r.LeftNo), tokAt(v.newTok, r.RightNo)
		switch v.long {
```

with

```go
		lt, rt := tokAt(v.oldTok, r.LeftNo), tokAt(v.newTok, r.RightNo)
		// In-view search hits for this logical line, per side. Nil for every
		// row when no search is active (and for the history pane, whose
		// diffView never routes search keys), so the no-search render is
		// untouched.
		var lh, rh []hitSpan
		if v.search.active() {
			lh, rh = v.search.hitsOn(dr.line, 0), v.search.hitsOn(dr.line, 1)
		}
		switch v.long {
```

then pass them to the six cell calls — `segCell(leftNo, dr.left, …, mk)` → `segCell(leftNo, dr.left, …, mk, lh)`, `segCell(rightNo, dr.right, …, mk)` → `…, mk, rh)`, both `diffCell(r.LeftNo, …, lt, mk)` → `…, lt, mk, lh)` and `diffCell(r.RightNo, …, rt, mk)` → `…, rt, mk, rh)`, and both `scrollCell(r.LeftNo, …, mk)` → `…, mk, lh)` / `scrollCell(r.RightNo, …, mk)` → `…, mk, rh)` (these replace the `nil` placeholders Task 1 left).

In `renderDiffView`, add the badge immediately BEFORE the wrap cue block (so a primed wrap still leads the status line) — replace

```go
	// Primed wrap-around cue: only when armed, so the unarmed header stays
	// byte-identical. Leads the status so a narrow terminal keeps the prompt.
	if cue := wrapCue(v.wrapArm); cue != "" {
```

with

```go
	// In-view search badge: "/foo  3/12" (spec §4.3), right-aligned status like
	// everything else here, so the avail math absorbs it.
	if bd := v.search.badge(); bd != "" {
		if right != "" {
			right = bd + "  " + right
		} else {
			right = bd
		}
	}
	// Primed wrap-around cue: only when armed, so the unarmed header stays
	// byte-identical. Leads the status so a narrow terminal keeps the prompt.
	if cue := wrapCue(v.wrapArm); cue != "" {
```

- [ ] **Step 7: Rebuild the footer inside the 140-column budget**

The hint is at 139 of its 140 columns and `[/] find` costs 10, so three labels
shrink to pay for it: `[↑↓] scroll  [j/k] line` → `[↑↓/jk] scroll/line` (−4),
`[ctrl+w]` → `[^w]` (−4) and `[←→/0] pan` → `[←→0] pan` (−1). The scroll variant
lands at exactly 140.

In `/mnt/t/others/gigagit.worktrees/feat-in-view-search/internal/tui/diff_render.go`, replace the doc comment and body of `diffHintFor`:

```go
// diffHintFor builds the diff-view hint for the current long-line mode. Every
// diff-view binding that is not help-only appears here, so the line is packed:
// the widest (scroll) English variant measures 140 display columns and MUST
// stay at or under 140, the width TestRenderDiffViewPanes renders at and the
// narrowest common wide terminal — past that the truncation eats [esc] close
// first, hiding the way out. There is NO headroom left: that budget is why the
// labels are terse (scroll/line on one key group, chg, ^w for ctrl+w,
// hist/blame) and why the three note keys share one [c/}{] notes group
// (E/R/a are help-and-menu-only). Shortening a label is the way to add a
// group; growing the line is not. TestDiffHintFitsTheBudget pins the number.
// The scroll variant appends the pan keys.
func diffHintFor(long longMode) string {
	mode := i18n.T("scroll")
	switch long {
	case longWrap:
		mode = i18n.T("wrap")
	case longTruncate:
		mode = i18n.T("trunc")
	}
	pan := ""
	if long == longScroll {
		pan = i18n.T("  [←→0] pan")
	}
	return i18n.T("[↑↓/jk] scroll/line  [c/}{] notes  [z] align  [e] edit  [n/p] chg  [f] part  [/] find  [^w] %s", mode) + pan + i18n.T("  [h/b] hist/blame  [esc] close")
}
```

- [ ] **Step 8: Update the four bundles for the two changed footer keys**

In EACH of `internal/i18n/lang/{ja,ko,ru,zh}.toml`: delete the two existing key
lines (line 300 and line 301 in every bundle — they are line-aligned) and append
the replacements at the end of the `[strings]` table. Deleting is mandatory:
`TestI18nBundlesComplete` reports a key no longer in the catalog as "orphaned".

Delete from all four:

```toml
"[↑↓] scroll  [j/k] line  [c/}{] notes  [z] align  [e] edit  [n/p] chg  [f] part  [ctrl+w] %s" = …
"  [←→/0] pan" = …
```

Append to `ja.toml`:

```toml
"[↑↓/jk] scroll/line  [c/}{] notes  [z] align  [e] edit  [n/p] chg  [f] part  [/] find  [^w] %s" = "[↑↓/jk] スクロール/行  [c/}{] ノート  [z] 位置  [e] 編集  [n/p] 変更  [f] 部分  [/] 検索  [^w] %s"
"  [←→0] pan" = "  [←→0] パン"
```

Append to `ko.toml`:

```toml
"[↑↓/jk] scroll/line  [c/}{] notes  [z] align  [e] edit  [n/p] chg  [f] part  [/] find  [^w] %s" = "[↑↓/jk] 스크롤/줄  [c/}{] 노트  [z] 정렬  [e] 편집  [n/p] 변경  [f] 부분  [/] 찾기  [^w] %s"
"  [←→0] pan" = "  [←→0] 이동"
```

Append to `zh.toml`:

```toml
"[↑↓/jk] scroll/line  [c/}{] notes  [z] align  [e] edit  [n/p] chg  [f] part  [/] find  [^w] %s" = "[↑↓/jk] 滚动/行  [c/}{] 笔记  [z] 对齐  [e] 编辑  [n/p] 变更  [f] 部分  [/] 查找  [^w] %s"
"  [←→0] pan" = "  [←→0] 平移"
```

Append to `ru.toml`:

```toml
"[↑↓/jk] scroll/line  [c/}{] notes  [z] align  [e] edit  [n/p] chg  [f] part  [/] find  [^w] %s" = "[↑↓/jk] прокрутка/строка  [c/}{] заметки  [z] выровнять  [e] правка  [n/p] изменение  [f] часть  [/] поиск  [^w] %s"
"  [←→0] pan" = "  [←→0] панорама"
```

- [ ] **Step 9: Help rows**

In `/mnt/t/others/gigagit.worktrees/feat-in-view-search/internal/tui/help.go`, in the "Diff view (enter)" section, replace

```go
		r("f", i18n.T("toggle full file ↔ changed lines only")),
```

with

```go
		r("f", i18n.T("toggle full file ↔ changed lines only")),
		r("/ @", i18n.T("find text forward / backward (enter keeps it, esc cancels)")),
		r("] [", i18n.T("next / previous hit (wraps around)")),
```

and replace the section's

```go
		r("esc", i18n.T("close")),
```

with

```go
		r("esc", i18n.T("clear the search, then close")),
```

(that literal already exists in all four bundles — the help window's own esc row
uses it — so it needs no translation work, and `"close"` stays in use on two
other rows, so nothing is orphaned).

Then the Global section's recall row — replace

```go
		r("alt+↑/↓", i18n.T("recall previous searches (history dropdown) while typing in the / filter, @ highlight, or the bookmark/shelf/files-tree search")),
```

with

```go
		r("alt+↑/↓", i18n.T("recall previous searches (history dropdown) while typing in the / filter, @ highlight, the bookmark/shelf/files-tree search, or the in-view search (/ @) of the diff, blame, file preview and hunk picker")),
```

- [ ] **Step 10: Bundle the three help literals**

In each of the four bundles, delete the old recall key line (line 593, line-aligned) and append its replacement plus the two new rows:

`ja.toml`:

```toml
"recall previous searches (history dropdown) while typing in the / filter, @ highlight, the bookmark/shelf/files-tree search, or the in-view search (/ @) of the diff, blame, file preview and hunk picker" = "/ フィルタ、@ ハイライト、ブックマーク/シェルフ/ファイルツリー検索、または diff・blame・ファイルプレビュー・ハンクピッカーのビュー内検索 (/ @) の入力中に過去の検索を呼び出す (履歴ドロップダウン)"
"find text forward / backward (enter keeps it, esc cancels)" = "テキストを前方 / 後方へ検索 (enter で確定、esc でキャンセル)"
"next / previous hit (wraps around)" = "次 / 前のヒットへ (循環)"
```

`ko.toml`:

```toml
"recall previous searches (history dropdown) while typing in the / filter, @ highlight, the bookmark/shelf/files-tree search, or the in-view search (/ @) of the diff, blame, file preview and hunk picker" = "/ 필터, @ 강조, 북마크/보관함/파일 트리 검색, 또는 diff·blame·파일 미리보기·헝크 피커의 뷰 내 검색 (/ @) 입력 중 이전 검색 불러오기 (히스토리 드롭다운)"
"find text forward / backward (enter keeps it, esc cancels)" = "텍스트를 앞으로 / 뒤로 검색 (enter 유지, esc 취소)"
"next / previous hit (wraps around)" = "다음 / 이전 일치로 (순환)"
```

`zh.toml`:

```toml
"recall previous searches (history dropdown) while typing in the / filter, @ highlight, the bookmark/shelf/files-tree search, or the in-view search (/ @) of the diff, blame, file preview and hunk picker" = "在 / 筛选、@ 高亮、书签/搁置区/文件树搜索，或 diff、blame、文件预览、块选择器的视图内搜索 (/ @) 输入时调出之前的搜索 (历史下拉)"
"find text forward / backward (enter keeps it, esc cancels)" = "向前 / 向后查找文本 (enter 保留、esc 取消)"
"next / previous hit (wraps around)" = "下一个 / 上一个匹配 (循环)"
```

`ru.toml`:

```toml
"recall previous searches (history dropdown) while typing in the / filter, @ highlight, the bookmark/shelf/files-tree search, or the in-view search (/ @) of the diff, blame, file preview and hunk picker" = "вызвать прежние запросы (выпадающая история) при вводе в / фильтре, @ подсветке, поиске закладок/полки/дерева файлов или во внутреннем поиске (/ @) диффа, blame, предпросмотра файла и сборщика ханков"
"find text forward / backward (enter keeps it, esc cancels)" = "искать текст вперёд / назад (enter оставляет, esc отменяет)"
"next / previous hit (wraps around)" = "к следующему / предыдущему совпадению (по кругу)"
```

- [ ] **Step 11: Run the tests**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-in-view-search && go build ./cmd/gg && go test ./internal/tui/...`

Expected: PASS, including `TestI18nBundlesComplete` (all four bundles), `TestHelpFooterCoverage` and `TestRenderDiffViewPanes`. If `TestDiffHintFitsTheBudget` reports anything other than 140 for the scroll variant, a label was mistyped — compare against the literal in Step 7 character by character.

- [ ] **Step 12: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-in-view-search && \
cat > /tmp/gg-msg-t3.txt <<'EOF'
feat(tui): in-view text search in the diff view

/ searches forward, @ backward, both incrementally from the line cursor;
enter keeps the query, esc restores where the search started, and a second
esc closes the view. ] and [ step between hits with wrap-around. Both sides
are searched — a row whose sides are identical on the right only, so ] never
stops twice on one piece of text — and folded rows are not searched: this is
an in-view search. A hit moves the cursor, scrolls the view minimally and,
in scroll mode, pans to the hit's columns. The header carries /foo 3/12.

The footer pays for [/] find by shortening three labels (scroll/line share a
group, ^w, [←→0] pan); the scroll variant is now exactly at its 140-column
budget, pinned by a test.

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01NDk1DJtgLzZX7hMmxDs9nU
EOF
git -C /mnt/t/others/gigagit.worktrees/feat-in-view-search add \
  internal/tui/diff_view.go internal/tui/diff_render.go internal/tui/help.go \
  internal/tui/diff_search_test.go \
  internal/i18n/lang/ja.toml internal/i18n/lang/ko.toml internal/i18n/lang/zh.toml internal/i18n/lang/ru.toml && \
git -C /mnt/t/others/gigagit.worktrees/feat-in-view-search commit -F /tmp/gg-msg-t3.txt
```

---

### Task 4: The blame view host

Blame's window is built row by row into `winRow`s, whose `text` is the code
alone (the commit gutter rides in `prefix`), so "code lines only" is free. The
hit mask goes on `winRow.emph`, which Task 1 taught `renderWindow` to slice and
`colouredLine` to paint — including on the reverse-video selected row, which is
where the current hit always lands.

**Files:**
- Modify: `internal/tui/blame_view.go:21-32` (struct), `:158-223` (render), `:242-316` (keys)
- Modify: `internal/tui/model.go:897-906` (`case blameMsg`)
- Modify: `internal/tui/help.go:292-298` (Blame view rows)
- Modify: `internal/i18n/lang/{ja,ko,ru,zh}.toml`
- Create: `internal/tui/blame_search_test.go`

**Interfaces:**
- Consumes: Task 1 + Task 2; `windowRowBounds`, `renderWindow`, `winOpts`, `blameGutterW`, `sanitizeLine`, `sanitizeCell`, `m.overlayDims()`, `m.popLayer()`.
- Produces:
  - `type blameOrigin struct { sel, hscroll int }`
  - `blameView.search textSearch`, `blameView.searchOrig blameOrigin`, `blameView.san []string`
  - `func (b *blameView) ensureSan()`, `func (b *blameView) searchText(i int) string`, `func (b *blameView) searchLines() []searchLine`, `func (b *blameView) searchPos() searchPos`, `func (b *blameView) goToHit(m Model, i int)`
  - `func (m Model) blameSearchKey(b *blameView, msg tea.KeyMsg) (Model, tea.Cmd, bool)`

- [ ] **Step 1: Write the failing tests**

Create `/mnt/t/others/gigagit.worktrees/feat-in-view-search/internal/tui/blame_search_test.go`:

```go
package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

// NOTE: TestBlameSearchPaintsTheSelectedRow calls lipgloss.SetColorProfile
// (process-global) and therefore does NOT call t.Parallel().

func blameSearchModel() (Model, *blameView) {
	m := Model{width: 100, height: 30}
	b := blameFixture()
	m = m.pushLayer(b)
	return m, b
}

func typeBlame(m Model, b *blameView, keys ...string) Model {
	for _, k := range keys {
		m, _ = b.update(m, keyMsg(k))
	}
	return m
}

func TestBlameSearchMovesTheLineCursor(t *testing.T) {
	t.Parallel()
	m, b := blameSearchModel()
	b.sel = 2
	m = typeBlame(m, b, "/", "m", "a", "i", "n")
	if len(b.search.hits) != 2 {
		t.Fatalf("hits = %v, want 2", b.search.hits)
	}
	// From line 2 the forward search wraps to the first hit.
	if b.sel != 0 {
		t.Fatalf("sel = %d, want 0 (wrapped)", b.sel)
	}
	m = typeBlame(m, b, "enter", "]")
	if b.sel != 1 || b.search.cur != 1 {
		t.Fatalf("] = sel %d cur %d, want 1/1", b.sel, b.search.cur)
	}
}

func TestBlameSearchBackwardOpensWithAt(t *testing.T) {
	t.Parallel()
	m, b := blameSearchModel()
	b.sel = 2
	_ = typeBlame(m, b, "@", "m", "a", "i", "n")
	if !b.search.backward {
		t.Fatal("@ must open a backward search")
	}
	if b.sel != 1 {
		t.Fatalf("backward from line 2 must land on the last hit at/before it: sel = %d, want 1", b.sel)
	}
}

func TestBlameSearchEscIsTwoStageButBIsNot(t *testing.T) {
	t.Parallel()
	m, b := blameSearchModel()
	b.sel = 0
	m = typeBlame(m, b, "/", "m", "a", "i", "n", "enter")
	m, _ = b.update(m, keyMsg("esc"))
	if layerOf[*blameView](m) == nil {
		t.Fatal("the first esc must only clear the search")
	}
	if b.search.active() {
		t.Fatalf("the first esc must clear the query: %+v", b.search)
	}
	m, _ = b.update(m, keyMsg("esc"))
	if layerOf[*blameView](m) != nil {
		t.Fatal("the second esc must close the view")
	}

	// b closes even with a live query — only esc is two-stage.
	m, b = blameSearchModel()
	m = typeBlame(m, b, "/", "m", "enter")
	m, _ = b.update(m, keyMsg("b"))
	if layerOf[*blameView](m) != nil {
		t.Fatal("b must close the view regardless of the search")
	}
}

func TestBlameSearchEscWhileTypingRestoresTheOrigin(t *testing.T) {
	t.Parallel()
	m, b := blameSearchModel()
	b.sel, b.hscroll = 2, 7
	m = typeBlame(m, b, "/", "m", "a", "i", "n")
	_ = typeBlame(m, b, "esc")
	if b.sel != 2 || b.hscroll != 7 {
		t.Fatalf("esc must restore sel/hscroll: %d/%d, want 2/7", b.sel, b.hscroll)
	}
	if b.search.active() {
		t.Fatalf("esc must leave no search: %+v", b.search)
	}
}

func TestBlameSearchBadgeAndHint(t *testing.T) {
	t.Parallel()
	m, b := blameSearchModel()
	m = typeBlame(m, b, "/", "m", "a", "i", "n", "enter")
	out := ansi.Strip(b.render(m, ""))
	head := strings.Split(out, "\n")[0]
	if !strings.Contains(head, "/main  1/2") {
		t.Fatalf("header must carry the badge: %q", head)
	}
	if lipgloss.Width(head) > 100 {
		t.Fatalf("header is %d columns wide", lipgloss.Width(head))
	}
	if !strings.Contains(out, "[/] find") {
		t.Fatalf("the hint must advertise the search:\n%s", out)
	}
}

func TestBlameSearchPaintsTheSelectedRow(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(prev)

	m, b := blameSearchModel()
	plain := b.render(m, "")
	m = typeBlame(m, b, "/", "m", "a", "i", "n", "enter")
	painted := b.render(m, "")
	if painted == plain {
		t.Fatal("an active search must change the render")
	}
	// The cursor row is reverse video and is exactly where the current hit is:
	// the visible text must be unchanged, only the styling.
	if ansi.Strip(strings.Split(painted, "\n")[1]) != ansi.Strip(strings.Split(plain, "\n")[1]) {
		t.Fatal("the hit changed the visible text of the selected row")
	}
}
```

- [ ] **Step 2: Run the tests to watch them fail**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-in-view-search && go test ./internal/tui/ -run TestBlameSearch 2>&1 | head -20`

Expected: FAIL — `b.search undefined (type *blameView has no field or method search)`.

- [ ] **Step 3: Add the state and the helpers**

In `/mnt/t/others/gigagit.worktrees/feat-in-view-search/internal/tui/blame_view.go`, extend the struct:

```go
	err     error
	tag     string // gates stale loads
	// search is the in-view text search (spec §4.3); san caches every line's
	// display text (what the window paints) so a keystroke does not
	// re-sanitize a 40k-line file, and searchOrig is what esc restores.
	search     textSearch
	searchOrig blameOrigin
	san        []string
}

// blameOrigin is the view state a live search restores when esc cancels it.
type blameOrigin struct{ sel, hscroll int }
```

and append the helpers after `blockAt`:

```go
// ensureSan builds the per-line display strings the search matches. They are
// exactly what the window shows: sanitizeCell's disp for a lexed file is the
// same expansion sanitizeLine performs, so one cache serves both paths.
func (b *blameView) ensureSan() {
	if len(b.san) == len(b.lines) {
		return
	}
	b.san = make([]string, len(b.lines))
	for i, ln := range b.lines {
		b.san[i] = sanitizeLine(ln.Content)
	}
}

func (b *blameView) searchText(i int) string {
	b.ensureSan()
	if i < 0 || i >= len(b.san) {
		return ""
	}
	return b.san[i]
}

// searchLines is the code, and only the code: the commit gutter lives in
// winRow.prefix, never in text, so "code lines only" (spec §4.3) is automatic.
func (b *blameView) searchLines() []searchLine {
	b.ensureSan()
	out := make([]searchLine, len(b.san))
	for i, t := range b.san {
		out[i] = searchLine{row: i, side: 0, text: t}
	}
	return out
}

// searchPos is where ] and [ measure from — the current hit while the cursor is
// still on its line, else the head of the cursor line.
func (b *blameView) searchPos() searchPos {
	if b.search.cur >= 0 && b.search.cur < len(b.search.hits) {
		if h := b.search.hits[b.search.cur]; h.row == b.sel {
			return searchPos{row: h.row, side: h.side, col: h.start}
		}
	}
	return searchPos{row: b.sel, side: 0, col: -1}
}

// goToHit selects hits[i]: the line cursor moves onto it (the render anchors
// its window on sel, so the scroll follows) and scroll mode pans to its column.
func (b *blameView) goToHit(m Model, i int) {
	if i < 0 || i >= len(b.search.hits) {
		return
	}
	h := b.search.hits[i]
	b.search.cur = i
	b.sel = h.row
	if b.mode != modeScroll {
		return
	}
	w, _ := m.overlayDims()
	gw := blameGutterW
	if gw > w-10 {
		gw = w - 10
	}
	if gw < 0 {
		gw = 0
	}
	cs, ce := hitCols(b.searchText(h.row), h)
	b.hscroll = panFor(b.hscroll, w-(gw+1), cs, ce) // prefixW = gw+1
}

// blameSearchKey gives the in-view search first refusal on a key (see
// diffSearchKey; the shape is deliberately identical across the four hosts).
func (m Model) blameSearchKey(b *blameView, msg tea.KeyMsg) (Model, tea.Cmd, bool) {
	if b.search.typing {
		nm, cmd, ev := m.searchTypingKey(&b.search, msg)
		m = nm
		switch ev {
		case searchChanged, searchCommitted:
			b.search.refindFrom(b.searchLines(), b.search.origin)
			if b.search.cur >= 0 {
				b.goToHit(m, b.search.cur)
			} else {
				b.sel, b.hscroll = b.searchOrig.sel, b.searchOrig.hscroll
			}
		case searchCancelled:
			b.sel, b.hscroll = b.searchOrig.sel, b.searchOrig.hscroll
		}
		return m, cmd, true
	}
	switch searchCommandKey(&b.search, msg) {
	case searchOpenFwd, searchOpenBack:
		b.searchOrig = blameOrigin{sel: b.sel, hscroll: b.hscroll}
		b.search.open(msg.String() == "@", b.searchPos())
		return m.recallReset(), nil, true
	case searchNext:
		b.goToHit(m, stepHit(b.search.hits, b.searchPos(), 1))
		return m, nil, true
	case searchPrev:
		b.goToHit(m, stepHit(b.search.hits, b.searchPos(), -1))
		return m, nil, true
	case searchCleared:
		b.search.clear()
		return m, nil, true
	case searchIgnored:
		return m, nil, true
	}
	return m, nil, false
}
```

- [ ] **Step 4: Route the keys**

In `update` (`:242`), insert the hook right after the ctrl+c guard — replace

```go
func (b *blameView) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	switch msg.String() {
```

with

```go
func (b *blameView) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	// The search owns / @ ] [ and — only while a query is live — esc. b is NOT
	// two-stage: it always leaves (never trap the user behind a search).
	if nm, cmd, handled := m.blameSearchKey(b, msg); handled {
		return nm, cmd
	}
	switch msg.String() {
```

- [ ] **Step 5: Paint the hits, badge the header, extend the hint**

In `render` (`:158`), replace the header/hint pair

```go
	header := truncate(i18n.T("blame: %s", b.ctx.path+revSuffix(b.ctx.rev)), w)
	hint := truncate(i18n.T("[↑↓] line  [pgup/pgdn] page  [enter] history  [e] editor  [esc/b] back"), w)
```

with

```go
	title := i18n.T("blame: %s", b.ctx.path+revSuffix(b.ctx.rev))
	header := truncate(title, w)
	if bd := b.search.badge(); bd != "" { // right-aligned, like the diff's
		avail := w - lipgloss.Width(bd) - 2
		if avail < 1 {
			avail = 1
		}
		header = truncate(padRight(truncate(title, avail), avail)+"  "+bd, w)
	}
	hint := truncate(i18n.T("[↑↓] line  [pgup/pgdn] page  [/] find  [enter] history  [e] editor  [esc/b] back"), w)
```

and inside the row loop replace

```go
		if b.tok == nil { // no lexer / colouring off: the plain (pre-syntax) path
			wr[i-lo] = winRow{prefix: gutter + "│", text: sanitizeLine(ln.Content), style: st}
			continue
		}
```

with

```go
		// Search hits ride on winRow.emph — a separate mask from cls, so an
		// unlexed file still paints them, and they survive the selected row's
		// reverse video (which is exactly where the current hit lands).
		var emph []emphLevel
		if b.search.active() {
			if hs := b.search.hitsOn(i, 0); len(hs) > 0 {
				emph = overlayHits(nil, 0, len([]rune(b.searchText(i))), hs)
			}
		}
		if b.tok == nil { // no lexer / colouring off: the plain (pre-syntax) path
			wr[i-lo] = winRow{prefix: gutter + "│", text: sanitizeLine(ln.Content), emph: emph, style: st}
			continue
		}
```

and the lexed branch's row build

```go
		wr[i-lo] = winRow{prefix: gutter + "│", text: string(disp), cls: cls, style: st}
```

with

```go
		wr[i-lo] = winRow{prefix: gutter + "│", text: string(disp), cls: cls, emph: emph, style: st}
```

- [ ] **Step 6: Drop the cache when a blame result lands**

In `/mnt/t/others/gigagit.worktrees/feat-in-view-search/internal/tui/model.go`, in `case blameMsg:`, replace

```go
			b.blocks = groupBlame(msg.lines)
			b.sel = 0
```

with

```go
			b.blocks = groupBlame(msg.lines)
			b.san = nil // the search's display-text cache belongs to the old lines
			b.sel = 0
```

- [ ] **Step 7: Help rows**

In `/mnt/t/others/gigagit.worktrees/feat-in-view-search/internal/tui/help.go`, replace the Blame view block

```go
		r("e", i18n.T("open the blamed file (at this revision) in your external editor (read-only)")),
		r("esc/b", i18n.T("back")),
```

with

```go
		r("e", i18n.T("open the blamed file (at this revision) in your external editor (read-only)")),
		r("/ @", i18n.T("find text forward / backward (enter keeps it, esc cancels)")),
		r("] [", i18n.T("next / previous hit (wraps around)")),
		r("esc/b", i18n.T("esc clears the search first, then goes back; b always goes back")),
```

(the `/ @` and `] [` literals were bundled in Task 3; `"back"` stays in use on the History view's `esc/h` row, so nothing is orphaned.)

- [ ] **Step 8: Bundle the two changed/new blame literals**

In each of the four bundles delete the old hint key (line 1377, line-aligned) and append the replacement plus the new esc row:

`ja.toml`:

```toml
"[↑↓] line  [pgup/pgdn] page  [/] find  [enter] history  [e] editor  [esc/b] back" = "[↑↓] 行  [pgup/pgdn] ページ  [/] 検索  [enter] 履歴  [e] エディタ  [esc/b] 戻る"
"esc clears the search first, then goes back; b always goes back" = "esc はまず検索を解除し、次に戻る。b は常に戻る"
```

`ko.toml`:

```toml
"[↑↓] line  [pgup/pgdn] page  [/] find  [enter] history  [e] editor  [esc/b] back" = "[↑↓] 줄  [pgup/pgdn] 페이지  [/] 찾기  [enter] 히스토리  [e] 편집기  [esc/b] 뒤로"
"esc clears the search first, then goes back; b always goes back" = "esc는 검색을 먼저 지우고 그다음 뒤로, b는 항상 뒤로"
```

`zh.toml`:

```toml
"[↑↓] line  [pgup/pgdn] page  [/] find  [enter] history  [e] editor  [esc/b] back" = "[↑↓] 行  [pgup/pgdn] 翻页  [/] 查找  [enter] 历史  [e] 编辑器  [esc/b] 返回"
"esc clears the search first, then goes back; b always goes back" = "esc 先清除搜索，再返回；b 始终返回"
```

`ru.toml`:

```toml
"[↑↓] line  [pgup/pgdn] page  [/] find  [enter] history  [e] editor  [esc/b] back" = "[↑↓] строка  [pgup/pgdn] страница  [/] поиск  [enter] история  [e] редактор  [esc/b] назад"
"esc clears the search first, then goes back; b always goes back" = "esc сначала сбрасывает поиск, затем возвращает; b возвращает всегда"
```

- [ ] **Step 9: Run the tests**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-in-view-search && go build ./cmd/gg && go test ./internal/tui/...`

Expected: PASS, including the existing `TestBlameRenderGutterFirstLineOnly` and `TestBlameDownMovesCursorClamped`.

- [ ] **Step 10: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-in-view-search && \
cat > /tmp/gg-msg-t4.txt <<'EOF'
feat(tui): in-view text search in the blame view

Same keys as the diff (/ @ ] [, alt+↑/↓ recall on the shared ring): the line
cursor moves onto the hit, scroll mode pans to its column, and the header
carries /foo 3/12. Only the code is searched — the commit gutter is the
window's frozen prefix, never part of the row text.

esc is two-stage (clear the search, then leave); b always leaves. The hit
mask rides on winRow.emph, so it paints on an unlexed file and survives the
selected row's reverse video, which is exactly where the current hit sits.

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01NDk1DJtgLzZX7hMmxDs9nU
EOF
git -C /mnt/t/others/gigagit.worktrees/feat-in-view-search add \
  internal/tui/blame_view.go internal/tui/model.go internal/tui/help.go \
  internal/tui/blame_search_test.go \
  internal/i18n/lang/ja.toml internal/i18n/lang/ko.toml internal/i18n/lang/zh.toml internal/i18n/lang/ru.toml && \
git -C /mnt/t/others/gigagit.worktrees/feat-in-view-search commit -F /tmp/gg-msg-t4.txt
```

---

### Task 5: The View-file preview host

The preview is a `*contentPopup` hanging off the Model, not a layer, so its keys
arrive through `updateFilesViewKey` — which already has a typing branch for the
TREE's `/` filter. The preview's own search branch must therefore run FIRST, and
the explicit "`/` does nothing while the preview is open" guard goes away.

The preview has no cursor (`sel` is the top visible line), so the current hit's
own colouring IS the marker: the scroll is minimal — a hit above the window
becomes the top line, one below it becomes the last.

**Files:**
- Modify: `internal/tui/content_popup.go:40-65` (struct)
- Modify: `internal/tui/file_preview.go:265-271` (rowsCap neighbourhood), `:296-346` (render)
- Modify: `internal/tui/files_view.go:448-478` (key preamble), `:573-576` (the `/` guard)
- Modify: `internal/tui/help.go:240` (the files-view `.` row)
- Modify: `internal/i18n/lang/{ja,ko,ru,zh}.toml`
- Create: `internal/tui/file_preview_search_test.go`

**Interfaces:**
- Consumes: Task 1 + Task 2; `previewClamp(top, n, rowsCap int, mode dispMode) int`, `m.filePreviewRowsCap()`, `m.layout().rightW`, `m.closePreview()`.
- Produces:
  - `type previewOrigin struct { sel, hscroll int }`
  - `contentPopup.search textSearch`, `contentPopup.searchOrig previewOrigin`
  - `func previewSearchLines(p *contentPopup) []searchLine`
  - `func (p *contentPopup) searchPos() searchPos`
  - `func (p *contentPopup) snapHit(rowsCap, innerW int)`
  - `func (m Model) filePreviewInnerW() int`
  - `func (m Model) previewSearchKey(msg tea.KeyMsg) (Model, tea.Cmd, bool)`

- [ ] **Step 1: Write the failing tests**

Create `/mnt/t/others/gigagit.worktrees/feat-in-view-search/internal/tui/file_preview_search_test.go`:

```go
package tui

import (
	"fmt"
	"strings"
	"testing"
)

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
```

- [ ] **Step 2: Run the tests to watch them fail**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-in-view-search && go test ./internal/tui/ -run TestPreviewSearch 2>&1 | head -20`

Expected: FAIL — `p.search undefined (type *contentPopup has no field or method search)`.

- [ ] **Step 3: Add the state**

In `/mnt/t/others/gigagit.worktrees/feat-in-view-search/internal/tui/content_popup.go`, add to `contentPopup` right before the closing brace (after `saved string`):

```go
	saved string
	// search is the FILE PREVIEW's in-view text search (spec §4.3). Only the
	// right-column preview uses it — the help window and the files tree filter
	// with query/typing above; the zero value is inert everywhere else.
	search     textSearch
	searchOrig previewOrigin
}

// previewOrigin is the pager state a live preview search restores on esc.
type previewOrigin struct{ sel, hscroll int }
```

- [ ] **Step 4: Add the preview's search helpers**

Append to `/mnt/t/others/gigagit.worktrees/feat-in-view-search/internal/tui/file_preview.go`:

```go
// filePreviewInnerW mirrors renderFilePreview's innerW (border 2 + horizontal
// padding 2), so the search's horizontal pan agrees with what is rendered.
func (m Model) filePreviewInnerW() int {
	n := m.layout().rightW - 4
	if n < 1 {
		n = 1
	}
	return n
}

// previewSearchLines is every preview line's display text. contentLine.text is
// already sanitized (fileContentLines drops control runes and expands tabs), so
// a hit's rune offsets index the class mask and the window slices directly.
func previewSearchLines(p *contentPopup) []searchLine {
	out := make([]searchLine, len(p.lines))
	for i, l := range p.lines {
		out[i] = searchLine{row: i, side: 0, text: l.text}
	}
	return out
}

// searchPos is where ] and [ measure from. The preview has no cursor, so before
// there is a current hit it is the top visible line — the first ] then finds the
// first hit on screen rather than jumping back to the top of the file.
func (p *contentPopup) searchPos() searchPos {
	if p.search.cur >= 0 && p.search.cur < len(p.search.hits) {
		h := p.search.hits[p.search.cur]
		return searchPos{row: h.row, side: h.side, col: h.start}
	}
	return searchPos{row: p.sel, side: 0, col: -1}
}

// snapHit scrolls the pager so the current hit is visible, moving as little as
// possible: a hit above the window becomes the top line, one below it becomes
// the last. There is no cursor to place — the current hit's own colouring is
// the marker. In scroll mode the columns are panned to as well.
func (p *contentPopup) snapHit(rowsCap, innerW int) {
	if p.search.cur < 0 || p.search.cur >= len(p.search.hits) {
		return
	}
	h := p.search.hits[p.search.cur]
	switch {
	case h.row < p.sel:
		p.sel = h.row
	case h.row >= p.sel+rowsCap:
		p.sel = h.row - rowsCap + 1
	}
	p.sel = previewClamp(p.sel, len(p.lines), rowsCap, p.mode)
	if p.mode == modeScroll && h.row < len(p.lines) {
		cs, ce := hitCols(p.lines[h.row].text, h)
		p.hscroll = panFor(p.hscroll, innerW, cs, ce)
	}
}
```

- [ ] **Step 5: Route the keys**

Append to `/mnt/t/others/gigagit.worktrees/feat-in-view-search/internal/tui/files_view.go`:

```go
// previewSearchKey gives the file preview's in-view search first refusal on a
// key. It runs BEFORE the tree's own /-filter typing branch, so a query typed
// into the preview can never land in the tree's filter, and only while the
// preview owns the right column (the tree side keeps its own / entirely).
func (m Model) previewSearchKey(msg tea.KeyMsg) (Model, tea.Cmd, bool) {
	p := m.filesPreview
	if p == nil || m.filesTreeFocused {
		return m, nil, false
	}
	rows, inner := m.filePreviewRowsCap(), m.filePreviewInnerW()
	if p.search.typing {
		nm, cmd, ev := m.searchTypingKey(&p.search, msg)
		m = nm
		switch ev {
		case searchChanged, searchCommitted:
			p.search.refindFrom(previewSearchLines(p), p.search.origin)
			if p.search.cur >= 0 {
				p.snapHit(rows, inner)
			} else {
				p.sel, p.hscroll = p.searchOrig.sel, p.searchOrig.hscroll
			}
		case searchCancelled:
			p.sel, p.hscroll = p.searchOrig.sel, p.searchOrig.hscroll
		}
		return m, cmd, true
	}
	switch searchCommandKey(&p.search, msg) {
	case searchOpenFwd, searchOpenBack:
		p.searchOrig = previewOrigin{sel: p.sel, hscroll: p.hscroll}
		p.search.open(msg.String() == "@", p.searchPos())
		return m.recallReset(), nil, true
	case searchNext:
		p.search.cur = stepHit(p.search.hits, p.searchPos(), 1)
		p.snapHit(rows, inner)
		return m, nil, true
	case searchPrev:
		p.search.cur = stepHit(p.search.hits, p.searchPos(), -1)
		p.snapHit(rows, inner)
		return m, nil, true
	case searchCleared:
		p.search.clear()
		return m, nil, true
	case searchIgnored:
		return m, nil, true
	}
	return m, nil, false
}
```

and hook it in at the top of `updateFilesViewKey` — replace

```go
	p := m.filesView
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	if p.typing { // /-input mode captures every key (same as the help window)
```

with

```go
	p := m.filesView
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	// The focused preview's in-view search comes first: it owns / @ ] [ and,
	// while a query is live, esc — otherwise esc still closes the preview below.
	if nm, cmd, handled := m.previewSearchKey(msg); handled {
		return nm, cmd
	}
	if p.typing { // /-input mode captures every key (same as the help window)
```

Then delete the preview guard in the `case "/":` arm — replace

```go
	case "/":
		if m.filesPreview != nil { // no commit filter while the preview owns the right column
			return m, nil
		}
		// Focus decides the search target. The commit-list side routes to the
```

with

```go
	case "/":
		// A FOCUSED preview already took / above (its in-view text search); the
		// tree side keeps its own filter even while a preview is open.
		// Focus decides the search target. The commit-list side routes to the
```

- [ ] **Step 6: Badge the title line, extend the hint, and paint**

In `renderFilePreview`, replace

```go
	wr := make([]winRow, len(window))
	for i, l := range window {
		wr[i] = winRow{text: l.text, cls: l.cls}
	}

	lines := make([]string, 0, contentH)
	lines = append(lines, padRight(truncate(i18n.T("View %s", p.title), innerW), innerW))
```

with

```go
	wr := make([]winRow, len(window))
	for i, l := range window {
		wr[i] = winRow{text: l.text, cls: l.cls}
		if p.search.active() {
			if hs := p.search.hitsOn(start+i, 0); len(hs) > 0 {
				wr[i].emph = overlayHits(nil, 0, len([]rune(l.text)), hs)
			}
		}
	}

	title := i18n.T("View %s", p.title)
	if bd := p.search.badge(); bd != "" { // right-aligned on the title line
		avail := innerW - lipgloss.Width(bd) - 2
		if avail < 1 {
			avail = 1
		}
		title = padRight(truncate(title, avail), avail) + "  " + bd
	}
	lines := make([]string, 0, contentH)
	lines = append(lines, padRight(truncate(title, innerW), innerW))
```

and the hint line

```go
	hint := i18n.T("%d/%d  [↑/↓] scroll  [ctrl+w] view  [esc] close", start+1, len(vis))
```

with

```go
	hint := i18n.T("%d/%d  [↑/↓] scroll  [ctrl+w] view  [/] find  [esc] close", start+1, len(vis))
```

`file_preview.go` does NOT import lipgloss today — add `"github.com/charmbracelet/lipgloss"` to its import block (below `tea "github.com/charmbracelet/bubbletea"`, in the same group).

- [ ] **Step 7: Help row**

In `/mnt/t/others/gigagit.worktrees/feat-in-view-search/internal/tui/help.go`, replace the files-view `.` row

```go
		r(".", i18n.T("tree side: View file — show the file's content at this commit (no diff) in the right pane ([↑/↓] scroll, [ctrl+w] view mode, [esc] close, [←] back to the tree); Open in external editor — open that content in $VISUAL/$EDITOR, read-only")),
```

with (one line, copied verbatim — the bundles in Step 8 key off this exact text):

```go
		r(".", i18n.T("tree side: View file — show the file's content at this commit (no diff) in the right pane ([↑/↓] scroll, [ctrl+w] view mode, / or @ find text forward / backward and ] [ step the hits, [esc] clears the search then closes, [←] back to the tree); Open in external editor — open that content in $VISUAL/$EDITOR, read-only")),
```

- [ ] **Step 8: Bundle the two changed preview literals**

In each of the four bundles delete the old preview hint key (`"%d/%d  [↑/↓] scroll  [ctrl+w] view  [esc] close"`, line 303, line-aligned) and the old files-view `.` row key (search for `tree side: View file`), then append:

`ja.toml`:

```toml
"%d/%d  [↑/↓] scroll  [ctrl+w] view  [/] find  [esc] close" = "%d/%d  [↑/↓] スクロール  [ctrl+w] 表示  [/] 検索  [esc] 閉じる"
"tree side: View file — show the file's content at this commit (no diff) in the right pane ([↑/↓] scroll, [ctrl+w] view mode, / or @ find text forward / backward and ] [ step the hits, [esc] clears the search then closes, [←] back to the tree); Open in external editor — open that content in $VISUAL/$EDITOR, read-only" = "ツリー側: View file — このコミット時点のファイル内容を右ペインに表示 (差分なし) ([↑/↓] スクロール、[ctrl+w] 表示モード、/ または @ でテキストを前方 / 後方へ検索し ] [ でヒットを移動、[esc] は検索を解除してから閉じる、[←] ツリーへ戻る); Open in external editor — その内容を $VISUAL/$EDITOR で読み取り専用で開く"
```

`ko.toml`:

```toml
"%d/%d  [↑/↓] scroll  [ctrl+w] view  [/] find  [esc] close" = "%d/%d  [↑/↓] 스크롤  [ctrl+w] 보기  [/] 찾기  [esc] 닫기"
"tree side: View file — show the file's content at this commit (no diff) in the right pane ([↑/↓] scroll, [ctrl+w] view mode, / or @ find text forward / backward and ] [ step the hits, [esc] clears the search then closes, [←] back to the tree); Open in external editor — open that content in $VISUAL/$EDITOR, read-only" = "트리 쪽: View file — 이 커밋 시점의 파일 내용을 오른쪽 창에 표시 (차이 없음) ([↑/↓] 스크롤, [ctrl+w] 보기 모드, / 또는 @로 텍스트를 앞뒤로 검색하고 ] [로 일치 이동, [esc]는 검색을 지운 뒤 닫기, [←] 트리로 돌아가기); Open in external editor — 그 내용을 $VISUAL/$EDITOR에서 읽기 전용으로 열기"
```

`zh.toml`:

```toml
"%d/%d  [↑/↓] scroll  [ctrl+w] view  [/] find  [esc] close" = "%d/%d  [↑/↓] 滚动  [ctrl+w] 视图  [/] 查找  [esc] 关闭"
"tree side: View file — show the file's content at this commit (no diff) in the right pane ([↑/↓] scroll, [ctrl+w] view mode, / or @ find text forward / backward and ] [ step the hits, [esc] clears the search then closes, [←] back to the tree); Open in external editor — open that content in $VISUAL/$EDITOR, read-only" = "树侧：View file — 在右栏显示该提交时的文件内容 (不是差异) ([↑/↓] 滚动、[ctrl+w] 视图模式、/ 或 @ 向前 / 向后查找文本并用 ] [ 跳转匹配、[esc] 先清除搜索再关闭、[←] 返回树); Open in external editor — 以只读方式在 $VISUAL/$EDITOR 中打开该内容"
```

`ru.toml`:

```toml
"%d/%d  [↑/↓] scroll  [ctrl+w] view  [/] find  [esc] close" = "%d/%d  [↑/↓] прокрутка  [ctrl+w] вид  [/] поиск  [esc] закрыть"
"tree side: View file — show the file's content at this commit (no diff) in the right pane ([↑/↓] scroll, [ctrl+w] view mode, / or @ find text forward / backward and ] [ step the hits, [esc] clears the search then closes, [←] back to the tree); Open in external editor — open that content in $VISUAL/$EDITOR, read-only" = "сторона дерева: View file — показать содержимое файла на этом коммите (без диффа) в правой панели ([↑/↓] прокрутка, [ctrl+w] режим показа, / или @ ищут текст вперёд / назад, ] [ переходят по совпадениям, [esc] сначала сбрасывает поиск, затем закрывает, [←] назад к дереву); Open in external editor — открыть это содержимое в $VISUAL/$EDITOR только для чтения"
```

- [ ] **Step 9: Run the tests**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-in-view-search && go build ./cmd/gg && go test ./internal/tui/...`

Expected: PASS, including the existing preview tests (`TestFilePreviewScrollsViewportImmediately`, `TestFilePreviewScrollAndClose`, `TestFilePreviewRendersInRightColumn`) and `TestI18nBundlesComplete`. `i18n.CheckVerbs` compares format verbs, so the `%d/%d` in the new hint must survive in all four translations.

- [ ] **Step 10: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-in-view-search && \
cat > /tmp/gg-msg-t5.txt <<'EOF'
feat(tui): in-view text search in the View-file preview

/ and @ now search the previewed file instead of doing nothing: the pager
scrolls minimally so the current hit is visible (the preview has no cursor —
the hit's own colouring is the marker), scroll mode pans to its column, ] and
[ step with wrap-around, and the title line carries /foo 3/12.

The preview's branch runs before the tree's own /-filter branch, so a query
typed into the preview can never land in the tree's filter; with the TREE
focused / still opens the tree filter, which the old blanket guard used to
swallow.

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01NDk1DJtgLzZX7hMmxDs9nU
EOF
git -C /mnt/t/others/gigagit.worktrees/feat-in-view-search add \
  internal/tui/content_popup.go internal/tui/file_preview.go internal/tui/files_view.go \
  internal/tui/help.go internal/tui/file_preview_search_test.go \
  internal/i18n/lang/ja.toml internal/i18n/lang/ko.toml internal/i18n/lang/zh.toml internal/i18n/lang/ru.toml && \
git -C /mnt/t/others/gigagit.worktrees/feat-in-view-search commit -F /tmp/gg-msg-t5.txt
```

---

### Task 6: The hunk picker host

The picker's cursor is 2D-plus — block, side, line — so its hits are addressed
by a FLAT candidate row: for each block in order, every `Current` line then
every `Incoming` line. Literal context rows and the output pane are never
searched (spec §4.3). All four picker kinds (conflict resolve, the
process-owned conflict resolve, stage, unstage) share `hunkPicker`, so they all
get the search from this one change.

**Files:**
- Modify: `internal/tui/conflict_picker.go:26-74` (struct), `:285-324` (`ensureSan` neighbourhood), `:369-450` (keys), `:560-583` (`pickerCell`), `:585-708` (render)
- Modify: `internal/tui/conflict_process.go:88-103` (the esc intercept)
- Modify: `internal/tui/help.go:148-164` (Hunk picker rows)
- Modify: `internal/i18n/lang/{ja,ko,ru,zh}.toml`
- Create: `internal/tui/conflict_picker_search_test.go`

**Interfaces:**
- Consumes: Task 1 + Task 2; `e.ensureSan()`, `e.sanCur/sanInc [][]sanLine`, `e.clampLine()`, `hunkpick.Current`/`hunkpick.Incoming`, `pickerColSep`, `m.overlayDims()`.
- Produces:
  - `type pickerAddr struct { bi int; side hunkpick.Side; line int }`
  - `type pickerOrigin struct { bi int; side hunkpick.Side; line, vshift, hscroll int }`
  - `hunkPicker.search textSearch`, `.searchOrig pickerOrigin`, `.searchRows []pickerAddr`, `.searchBase [][2]int`
  - `func (e *hunkPicker) ensureSearchRows()`, `func (e *hunkPicker) searchLines() []searchLine`, `func (e *hunkPicker) searchRow(bi int, side hunkpick.Side, line int) int`, `func (e *hunkPicker) searchPos() searchPos`, `func (e *hunkPicker) goToHit(m Model, i int)`, `func (e *hunkPicker) restoreSearchOrigin()`, `func (e *hunkPicker) textWidth(w int) int`
  - `func pickerCell(blk *hunkpick.Block, san []sanLine, side hunkpick.Side, r int, cursor bool, hits []hitSpan) *winCell` (one new parameter)

- [ ] **Step 1: Write the failing tests**

Create `/mnt/t/others/gigagit.worktrees/feat-in-view-search/internal/tui/conflict_picker_search_test.go`:

```go
package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/homeend/gigagit/internal/hunkpick"
)

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

func TestPickerSearchSkipsLiteralsAndTheOutputPane(t *testing.T) {
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
```

- [ ] **Step 2: Run the tests to watch them fail**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-in-view-search && go test ./internal/tui/ -run 'TestPickerSearch|TestProcessPickerEsc' 2>&1 | head -20`

Expected: FAIL — `e.search undefined (type *hunkPicker has no field or method search)`.

- [ ] **Step 3: Add the state**

In `/mnt/t/others/gigagit.worktrees/feat-in-view-search/internal/tui/conflict_picker.go`, extend the struct (after the two `lastGridH`/`lastOutH` fields):

```go
	lastGridH int // grid height at the last render — the pgup/pgdn page size
	lastOutH  int // output-pane height at the last render — its page size

	// search is the in-view text search (spec §4.3). Its rows are the FLAT
	// candidate list — per block, every Current line then every Incoming line —
	// so one hit names one (block, side, line) cursor position; searchRows maps
	// a row back, searchBase maps a cursor forward. Literal context rows and
	// the output pane are not in it and are therefore never searched.
	search     textSearch
	searchOrig pickerOrigin
	searchRows []pickerAddr
	searchBase [][2]int // per block: the flat row each side's lines start at
}

// pickerAddr is where a flat search row lives in the 2D cursor.
type pickerAddr struct {
	bi   int
	side hunkpick.Side
	line int
}

// pickerOrigin is the cursor and viewport a live search restores on esc.
type pickerOrigin struct {
	bi      int
	side    hunkpick.Side
	line    int
	vshift  int
	hscroll int
}
```

- [ ] **Step 4: Add the picker's search helpers**

Append to `/mnt/t/others/gigagit.worktrees/feat-in-view-search/internal/tui/conflict_picker.go` (after `ensureOutput`, with the other cache builders):

```go
// ensureSearchRows flattens the candidate lines into the search's row space:
// for each block in order, every Current line, then every Incoming line. The
// shape depends only on the document, which is immutable while the picker is
// open, so it is built once — the async lexer rebuilds the masks but never the
// line texts, so hits stay valid across it.
func (e *hunkPicker) ensureSearchRows() {
	e.ensureSan()
	if e.searchBase != nil {
		return
	}
	e.searchBase = make([][2]int, len(e.blocks))
	e.searchRows = e.searchRows[:0]
	for bi := range e.blocks {
		e.searchBase[bi][0] = len(e.searchRows)
		for r := range e.sanCur[bi] {
			e.searchRows = append(e.searchRows, pickerAddr{bi: bi, side: hunkpick.Current, line: r})
		}
		e.searchBase[bi][1] = len(e.searchRows)
		for r := range e.sanInc[bi] {
			e.searchRows = append(e.searchRows, pickerAddr{bi: bi, side: hunkpick.Incoming, line: r})
		}
	}
}

// searchLines is the searchable text: every candidate line as the display
// string the grid paints.
func (e *hunkPicker) searchLines() []searchLine {
	e.ensureSearchRows()
	out := make([]searchLine, len(e.searchRows))
	for i, a := range e.searchRows {
		san, side := e.sanCur[a.bi], 0
		if a.side == hunkpick.Incoming {
			san, side = e.sanInc[a.bi], 1
		}
		out[i] = searchLine{row: i, side: side, text: san[a.line].text}
	}
	return out
}

// searchRow is the flat search row of a 2D cursor position, or -1 when there
// is none (no blocks).
func (e *hunkPicker) searchRow(bi int, side hunkpick.Side, line int) int {
	e.ensureSearchRows()
	if bi < 0 || bi >= len(e.searchBase) {
		return -1
	}
	k := 0
	if side == hunkpick.Incoming {
		k = 1
	}
	return e.searchBase[bi][k] + line
}

// searchPos is where ] and [ measure from — the current hit while the cursor is
// still on it, else the head of the cursor's own candidate line.
func (e *hunkPicker) searchPos() searchPos {
	row := e.searchRow(e.bi, e.side, e.line)
	side := 0
	if e.side == hunkpick.Incoming {
		side = 1
	}
	if e.search.cur >= 0 && e.search.cur < len(e.search.hits) {
		if h := e.search.hits[e.search.cur]; h.row == row {
			return searchPos{row: h.row, side: h.side, col: h.start}
		}
	}
	return searchPos{row: row, side: side, col: -1}
}

// textWidth is one candidate column's text width: the two-column split minus
// the fixed "  [ ] " gutter (cursor marker + checkbox).
func (e *hunkPicker) textWidth(w int) int {
	colW := (w - lipgloss.Width(pickerColSep)) / 2
	tw := colW - 6
	if tw < 1 {
		tw = 1
	}
	return tw
}

// goToHit selects hits[i]: the 2D cursor jumps to it (the render anchors its
// window on the cursor, so the scroll follows), the free view-scroll is
// released, focus returns to the grid, and scroll mode pans to its column.
func (e *hunkPicker) goToHit(m Model, i int) {
	if i < 0 || i >= len(e.search.hits) {
		return
	}
	h := e.search.hits[i]
	if h.row < 0 || h.row >= len(e.searchRows) {
		return
	}
	a := e.searchRows[h.row]
	e.search.cur = i
	e.bi, e.side, e.line = a.bi, a.side, a.line
	e.vshift = 0
	e.outFocused = false
	if e.mode != modeScroll {
		return
	}
	san := e.sanCur[a.bi]
	if a.side == hunkpick.Incoming {
		san = e.sanInc[a.bi]
	}
	w, _ := m.overlayDims()
	cs, ce := hitCols(san[a.line].text, h)
	e.hscroll = panFor(e.hscroll, e.textWidth(w), cs, ce)
}

// restoreSearchOrigin puts the cursor and viewport back where / or @ found them.
func (e *hunkPicker) restoreSearchOrigin() {
	o := e.searchOrig
	e.bi, e.side, e.line, e.vshift, e.hscroll = o.bi, o.side, o.line, o.vshift, o.hscroll
	e.clampLine()
}
```

- [ ] **Step 5: Route the keys**

In `update` (`:369`), insert the typing branch right after the ctrl+c guard — replace

```go
func (e *hunkPicker) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	if msg.String() == "tab" {
```

with

```go
func (e *hunkPicker) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	// While the search is capturing text it owns EVERY key: tab, ctrl+t and the
	// picks all wait. History recall runs inside searchTypingKey, which is why
	// alt+↑/↓ can still mean "scroll the view" below.
	if e.search.typing {
		nm, cmd, ev := m.searchTypingKey(&e.search, msg)
		m = nm
		switch ev {
		case searchChanged, searchCommitted:
			e.search.refindFrom(e.searchLines(), e.search.origin)
			if e.search.cur >= 0 {
				e.goToHit(m, e.search.cur)
			} else {
				e.restoreSearchOrigin()
			}
		case searchCancelled:
			e.restoreSearchOrigin()
		}
		return m, cmd
	}
	if msg.String() == "tab" {
```

Add the four keys to the output-pane pass-through allowlist — replace

```go
		case "esc", "enter", "ctrl+s", "ctrl+w", "shift+left", "shift+right", "alt+up", "alt+down":
```

with

```go
		// / and @ are allowed through and move focus back to the grid: the
		// output pane is never searched, but a / pressed here must not look dead.
		case "esc", "enter", "ctrl+s", "ctrl+w", "shift+left", "shift+right", "alt+up", "alt+down", "/", "@", "[", "]":
```

and insert the command switch right before the main switch — replace

```go
	b := e.cur()
	switch msg.String() {
	case "esc":
		return m.popLayer(), nil
```

with

```go
	switch searchCommandKey(&e.search, msg) {
	case searchOpenFwd, searchOpenBack:
		e.outFocused = false // the search always lives in the grid
		e.searchOrig = pickerOrigin{bi: e.bi, side: e.side, line: e.line, vshift: e.vshift, hscroll: e.hscroll}
		e.search.open(msg.String() == "@", e.searchPos())
		return m.recallReset(), nil
	case searchNext:
		e.goToHit(m, stepHit(e.search.hits, e.searchPos(), 1))
		return m, nil
	case searchPrev:
		e.goToHit(m, stepHit(e.search.hits, e.searchPos(), -1))
		return m, nil
	case searchCleared:
		e.search.clear()
		return m, nil
	case searchIgnored:
		return m, nil
	}

	b := e.cur()
	switch msg.String() {
	case "esc":
		return m.popLayer(), nil
```

- [ ] **Step 6: Let the process-owned picker see esc**

In `/mnt/t/others/gigagit.worktrees/feat-in-view-search/internal/tui/conflict_process.go`, replace

```go
		if msg.String() == "esc" { // leave the editor without applying → back to the list
			if p.picker.zoomed { // esc restores the zoomed split before it can leave
				p.picker.zoomed = false
				return m, nil
			}
			p.picker = nil
			p.st = confListing
			return m, nil
		}
```

with

```go
		if msg.String() == "esc" { // leave the editor without applying → back to the list
			if p.picker.zoomed { // esc restores the zoomed split before it can leave
				p.picker.zoomed = false
				return m, nil
			}
			// A live in-view search owns esc first (cancel while typing, clear a
			// committed query) and stays in the editor — otherwise the query
			// could only be dismissed by leaving.
			if p.picker.search.typing || p.picker.search.query != "" {
				return p.picker.update(m, msg)
			}
			p.picker = nil
			p.st = confListing
			return m, nil
		}
```

- [ ] **Step 7: Paint the hits, badge the header, extend the hint**

In `pickerCell` (`:566`), replace the signature and the cell build:

```go
// pickerCell builds the winCell for one candidate line; r past the side's line
// count yields a blank cell (the gap when sides differ in length). cursor adds
// the "> " marker so the gutter width is constant (focused or not) and puts the
// cell under selectedRow — reverse video, which renderPiece takes as the signal
// to drop the CLASS half of the paint mask; search hits survive it, because the
// current hit is always on the cursor row. hits are the in-view search's spans
// for this line, already display-rune offsets into san[r].text.
func pickerCell(blk *hunkpick.Block, san []sanLine, side hunkpick.Side, r int, cursor bool, hits []hitSpan) *winCell {
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
	if len(hits) > 0 {
		// NEVER write into the cached mask: ensureOutput hands the very same
		// sanLine to the output pane. overlayHits paints a copy.
		c.mask = runMask{
			cls:  san[r].mask.cls,
			emph: overlayHits(san[r].mask.emph, 0, len([]rune(san[r].text)), hits),
		}
	}
	if cursor {
		c.style = st().selectedRow
	}
	return c
}
```

In `render`, badge the header — replace

```go
	header := e.title + "    " + i18n.T("%d hunks", len(e.blocks))
	if e.requireAll {
		header = e.title + "    " + i18n.T("%d regions · %d left", len(e.blocks), e.doc.Pending())
	}
```

with

```go
	header := e.title + "    " + i18n.T("%d hunks", len(e.blocks))
	if e.requireAll {
		header = e.title + "    " + i18n.T("%d regions · %d left", len(e.blocks), e.doc.Pending())
	}
	if bd := e.search.badge(); bd != "" { // the in-view search, after the counts
		header += "    " + bd
	}
```

extend the grid hint — replace

```go
		i18n.T("[←/→] side"), i18n.T("[shift+←/→] scroll"), i18n.T("[ctrl+w] mode"), i18n.T("[↑/↓] line"), i18n.T("[pgup/pgdn] page"), i18n.T("[alt+↑/↓] view"), i18n.T("[space] pick"),
```

with

```go
		i18n.T("[←/→] side"), i18n.T("[shift+←/→] scroll"), i18n.T("[ctrl+w] mode"), i18n.T("[↑/↓] line"), i18n.T("[/] find"), i18n.T("[pgup/pgdn] page"), i18n.T("[alt+↑/↓] view"), i18n.T("[space] pick"),
```

and feed the hits to the two cells — replace

```go
			rows = append(rows, colRow{
				left:  pickerCell(blk, e.sanCur[blockNo], hunkpick.Current, r, lCur),
				right: pickerCell(blk, e.sanInc[blockNo], hunkpick.Incoming, r, rCur),
			})
```

with

```go
			var lh, rh []hitSpan
			if e.search.active() {
				lh = e.search.hitsOn(e.searchRow(blockNo, hunkpick.Current, r), 0)
				rh = e.search.hitsOn(e.searchRow(blockNo, hunkpick.Incoming, r), 1)
			}
			rows = append(rows, colRow{
				left:  pickerCell(blk, e.sanCur[blockNo], hunkpick.Current, r, lCur, lh),
				right: pickerCell(blk, e.sanInc[blockNo], hunkpick.Incoming, r, rCur, rh),
			})
```

- [ ] **Step 8: Help rows**

In `/mnt/t/others/gigagit.worktrees/feat-in-view-search/internal/tui/help.go`, in the Hunk picker section replace

```go
		r("n / p", i18n.T("jump to the next / previous region (wraps around)")),
```

with

```go
		r("n / p", i18n.T("jump to the next / previous region (wraps around)")),
		r("/ @", i18n.T("find text forward / backward (enter keeps it, esc cancels)")),
		r("] [", i18n.T("next / previous hit (wraps around)")),
```

and

```go
		r("esc", i18n.T("cancel (close without resolving)")),
```

with

```go
		r("esc", i18n.T("clear the search, then cancel (close without resolving)")),
```

- [ ] **Step 9: Bundle the two picker literals**

In each of the four bundles delete `"cancel (close without resolving)"` (line 695, line-aligned) and append:

`ja.toml`:

```toml
"[/] find" = "[/] 検索"
"clear the search, then cancel (close without resolving)" = "検索を解除し、次にキャンセル (解決せずに閉じる)"
```

`ko.toml`:

```toml
"[/] find" = "[/] 찾기"
"clear the search, then cancel (close without resolving)" = "검색을 지우고, 그다음 취소 (해결하지 않고 닫기)"
```

`zh.toml`:

```toml
"[/] find" = "[/] 查找"
"clear the search, then cancel (close without resolving)" = "先清除搜索，再取消 (不解决直接关闭)"
```

`ru.toml`:

```toml
"[/] find" = "[/] поиск"
"clear the search, then cancel (close without resolving)" = "сбросить поиск, затем отмена (закрыть без разрешения)"
```

- [ ] **Step 10: Run the tests**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-in-view-search && go build ./cmd/gg && go test ./internal/tui/...`

Expected: PASS, including the existing picker tests (`TestConflictPickerTakeSides`, `TestHunkPickerRenderFitsHeight`, `picker_keys_test.go`, `picker_syntax_test.go`) and the conflict-process tests.

- [ ] **Step 11: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-in-view-search && \
cat > /tmp/gg-msg-t6.txt <<'EOF'
feat(tui): in-view text search in the hunk picker

The fourth and last reader (spec §4.3). Candidate lines are flattened into
one search row space — per region, every current line then every incoming
line — so a hit names a (block, side, line) cursor position the 2D cursor
jumps to; literal context rows and the output pane are never searched. /
pressed while the output pane is focused returns focus to the grid instead of
looking dead, the header carries /foo 3/12, and hits paint on a copy of the
cached line mask, which the output pane shares.

esc is two-stage here too — and the process-owned picker, whose esc the
conflict process used to eat, now gets it while a search is live.

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01NDk1DJtgLzZX7hMmxDs9nU
EOF
git -C /mnt/t/others/gigagit.worktrees/feat-in-view-search add \
  internal/tui/conflict_picker.go internal/tui/conflict_process.go internal/tui/help.go \
  internal/tui/conflict_picker_search_test.go \
  internal/i18n/lang/ja.toml internal/i18n/lang/ko.toml internal/i18n/lang/zh.toml internal/i18n/lang/ru.toml && \
git -C /mnt/t/others/gigagit.worktrees/feat-in-view-search commit -F /tmp/gg-msg-t6.txt
```

---

### Task 7: Docs and the headless smoke

**Files:**
- Modify: `README.md` (four table rows — diff, files-view `l`, blame, hunk picker)
- Modify: `CHANGELOG.md:9` (under `## [Unreleased]`)
- Modify: `docs/CLAUDE-details.md` (append one section)
- No code changes.

- [ ] **Step 1: README — the diff row**

In `/mnt/t/others/gigagit.worktrees/feat-in-view-search/README.md`, replace (it
occurs exactly once)

```
`esc` closes. Changed lines highlight the exact words that differ
```

with

```
`/` searches the text in view (`@` searches backwards) — type to search incrementally from the cursor line, `enter` keeps the query, `esc` cancels it, `]`/`[` step to the next/previous hit (wrapping), `alt+↑`/`alt+↓` recall previous searches; hits are highlighted like word differences, the current one underlined, `esc` closes. Changed lines highlight the exact words that differ
```

- [ ] **Step 2: README — the files-view (`l`) row, which documents the preview**

Replace (occurs once)

```
always scroll the tree; `/` searches paths; `esc`/`l` close)
```

with

```
always scroll the tree; `/` searches paths — with the **View file** preview focused it searches the previewed content instead (`@` backwards, `]`/`[` step the hits, `esc` clears the search then closes the preview); `esc`/`l` close)
```

- [ ] **Step 3: README — the blame row**

Replace (occurs once)

```
`enter` opens that commit's history, `esc`/`b` go back
```

with

```
`enter` opens that commit's history, `/` (or `@`) finds text in the file with `]`/`[` stepping the hits, `esc`/`b` go back (a live search takes the first `esc`; `b` always goes back)
```

- [ ] **Step 4: README — the hunk-picker row**

Replace (occurs once)

```
`ctrl+s` applies once every region is resolved, `esc` cancels;
```

with

```
`ctrl+s` applies once every region is resolved, `/` (or `@`) finds text across the candidate lines — the 2D cursor jumps to the hit, `]`/`[` step (the literal context and the output pane are not searched) —, `esc` cancels (clearing a live search first);
```

- [ ] **Step 5: CHANGELOG**

Insert directly under the `## [Unreleased]` heading at `CHANGELOG.md:9` (the
file contains a literal `<<<<<<<` line much further down — that is NOT a merge
marker, do not go looking for it):

```markdown
- **In-view text search in the four readers.** The diff view, blame, the
  View-file preview and the hunk picker share one search: `/` searches
  forward, `@` backward, both incrementally from the cursor as you type;
  `enter` keeps the query, `esc` cancels it (and a second `esc` closes the
  view); `]`/`[` step to the next/previous hit, wrapping around; `n`/`p` keep
  their change/region meaning. Hits paint like word differences with the
  current one underlined, the header shows `/foo  3/12`, the cursor moves onto
  the hit and scroll mode pans to its column. One shared history ring for all
  four, recalled with `alt+↑`/`alt+↓` like every other gg search field.
  Case-insensitive substring only — no regex, no whole-word; the browser UI
  keeps its own find.
```

- [ ] **Step 6: docs/CLAUDE-details.md**

Append at the end of the file:

```markdown
### In-view text search (phase 6, spec §4.3)

`internal/tui/textsearch.go` is the whole leaf: `textSearch` (query, typing,
direction, `hits`, `cur`, `origin`), a rune-walking case-insensitive matcher,
at/after (`nearestHit`) and strictly-after (`stepHit`) stepping with wrap, the
`badge()`, the minimal `panFor`, and the two key handlers every host routes
through — `searchTypingKey` (history recall FIRST, then esc/enter/backspace/
space/runes; everything else swallowed) and `searchCommandKey` (`/ @ ] [`, and
`esc` only while a query is live).

**One index space: display runes.** Every host searches the DISPLAY string it
paints (`sanitizeLine`/`sanitizeCell`'s disp, `contentLine.text`,
`sanLine.text`), so a hit's `[start, end)` are already the painter's offsets —
no raw→display mapping, and `hitCols` converts to display COLUMNS (wide glyphs)
only for the pan.

**Painting is an overlay, never a mutation.** The per-display-rune `emph` mask
widened from `[]bool` to `[]emphLevel` (`none | word | hit | current`);
`overlayHits(emph, off, n, hits)` returns its input untouched when nothing
intersects (so the no-search render allocates nothing and is byte-identical)
and otherwise paints a COPY — `textdiff.Row` spans and the picker's `sanLine`
masks are shared cache values that the output pane and other views read.
`cellSeg.off` records where a wrapped segment starts, so wrap mode overlays at
render instead of re-laying-out on every keystroke.

**Reverse video keeps emphasis.** `window.go` and `twocol.go` drop only the
CLASS mask on a reversed row/cell (reverse would turn per-token foregrounds
into per-token backgrounds); bold and underline read either way, and the
current hit is by definition on the cursor row. `st().searchCur` is
`diffEmph.Underline(true)` — no new theme role, and underline survives the
Terminal theme, where `bright` is empty and `diffEmph` is bold-only.

**Stepping is relative to the cursor, not to `cur`.** `]` is the first hit
strictly after the cursor position, `[` the last strictly before; both wrap.
Direction (`/` vs `@`) only picks which hit the incremental search snaps to and
which glyph the badge leads with. Each host's `searchPos()` returns the current
hit while the cursor still sits on it and the head of the cursor line
(`col: -1`) otherwise, which is what makes a `j`-then-`]` sequence find the hit
on the line the user just walked to.

**Per-host addressing.** Diff: `row` = index into `v.lines` (folded rows are
not searched — it is an IN-VIEW search), `side` 0/1, and a row whose sides are
identical is searched on the right only so `]` never stops twice on one piece
of text. Blame and preview: one column, `side` 0, the gutter is the window's
frozen prefix and so is never searchable. Picker: a FLAT candidate row (per
block, every `Current` line then every `Incoming` line) plus `searchBase` to map
a 2D cursor forward and `searchRows` to map a hit back; literals and the output
pane are outside that space. Search state is per VIEW: stepping to another file
replaces the `diffView`, so the query does not follow.
```

- [ ] **Step 7: Build a fixture repo and run the headless smoke**

The recorder cannot emit alt keys and `tui-capture.sh` has no meta-arrow token,
so the recall path is covered by the Go tests only; `/ @ ] [ esc enter` and the
query runes are all literal tokens.

**`enter` after the query is mandatory:** while the search is typing, `]` is
just another rune and would be appended to the query.

```bash
rm -rf /tmp/ggsearch && mkdir -p /tmp/ggsearch && cd /tmp/ggsearch && \
git init -q && \
printf 'package main\n\nfunc main() {\n\tprintln("alpha")\n}\n\nfunc helper() {\n\tprintln("alpha beta")\n}\n' > main.go && \
git add main.go && \
git -c user.name=t -c user.email=t@t commit -qm "add main.go" && \
cd /mnt/t/others/gigagit.worktrees/feat-in-view-search && \
go build -o /tmp/ggsearch-gg ./cmd/gg && \
./tui-capture.sh --repo /tmp/ggsearch --gg /tmp/ggsearch-gg --out /tmp/ggsearch-snaps \
  "commits: right ; files: l ; tree: enter ; diff: enter ; find: / alpha ; keep: enter ; step: ] ; clear: esc ; close: esc"
```

`tui-capture.sh` writes `snap-<NN>-<label>.txt`, starting at `snap-00-init.txt`
and numbering the steps from 01, so the expected screens are:

| file | screen |
|---|---|
| `snap-00-init.txt` | the panels, focus in the left column |
| `snap-01-commits.txt` | focus on the Commits panel |
| `snap-02-files.txt` | the commit's files view |
| `snap-03-tree.txt` | focus on the file tree |
| `snap-04-diff.txt` | the full-screen diff of `main.go` |
| `snap-05-find.txt` | typing: badge `/alpha█  1/2` |
| `snap-06-keep.txt` | committed: badge `/alpha  1/2` |
| `snap-07-step.txt` | `]`: badge `/alpha  2/2` |
| `snap-08-clear.txt` | `esc`: no badge |
| `snap-09-close.txt` | the files view again |

- [ ] **Step 8: Assert the smoke**

```bash
grep -c '/alpha█  1/2' /tmp/ggsearch-snaps/snap-05-find.txt
grep -c '/alpha  1/2'  /tmp/ggsearch-snaps/snap-06-keep.txt
grep -c '/alpha  2/2'  /tmp/ggsearch-snaps/snap-07-step.txt
grep -c 'println("alpha")' /tmp/ggsearch-snaps/snap-06-keep.txt
grep -c '\[/\] find' /tmp/ggsearch-snaps/snap-06-keep.txt
grep -c '/alpha' /tmp/ggsearch-snaps/snap-08-clear.txt   # expect 0
```

Expected: `1` from each of the first five, `0` from the last (grep exits 1 on
no match — that is the pass for the `clear` line). If `snap-04-diff.txt` is not
the diff of `main.go`, the fixture repo was not built by the command above —
rebuild it and re-run.

- [ ] **Step 9: Full local gate**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-in-view-search && go build ./cmd/gg && ./test.sh unit`

Expected: PASS. Do NOT run `./test.sh race` — the controller runs the race gate.

- [ ] **Step 10: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-in-view-search && \
cat > /tmp/gg-msg-t7.txt <<'EOF'
docs: in-view text search — README, CHANGELOG, CLAUDE-details

The four reader rows gain the keys, the Unreleased section gains the feature
bullet, and CLAUDE-details records the mechanism: one display-rune index
space, an overlay that never writes through a shared mask, reverse video
keeping emphasis while dropping classes, cursor-relative stepping, and the
per-host row addressing (including the picker's flat candidate space).

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01NDk1DJtgLzZX7hMmxDs9nU
EOF
git -C /mnt/t/others/gigagit.worktrees/feat-in-view-search add \
  README.md CHANGELOG.md docs/CLAUDE-details.md && \
git -C /mnt/t/others/gigagit.worktrees/feat-in-view-search commit -F /tmp/gg-msg-t7.txt
```

---

## Rulings made while planning

These resolve what the spec left open, or where a naive reading of it
contradicts the codebase. They are binding; the "why" matters as much as the
ruling.

**R1. Hit offsets are DISPLAY-rune offsets in every host.** The searched text is
the display string: the diff uses `sanitizeLine(raw)` per side (lazily cached
per logical line, dropped by `rebuild`), blame `sanitizeLine(Content)` (equal to
`sanitizeCell`'s `disp`), the preview `contentLine.text`, the picker
`sanLine.text`. Painting overlays those offsets onto the display-rune emphasis
mask: post-sanitize for the diff cells, post-slice for the window and two-column
primitives (whose slicers already carry the per-mode offsets). Never append to
`Row.LeftSpans/RightSpans` (a shared `domain.Diff`) and never write into the
picker's cached `sanLine.mask` — always overlay onto a fresh slice; the
no-query path allocates nothing. *Why:* three of the four hosts already hold
display strings, and one index space means one overlay function. This deviates
from the spec's "the diff pane takes them as extra spans": the spans handed to
the cell painters are display-indexed hit spans applied after sanitize, not raw
`textdiff.Span`s.

**R2. The third emphasis state is a widened mask, not a parallel one.**
`emph []bool` → `[]emphLevel` (`emphNone/emphWord/emphHit/emphCur`) across every
signature that carries it, in one mechanical task. `styledRuns` still groups by
value, so precedence is current > hit > word > syntax for free. The no-query
render is pinned byte-identical by self-oracles (a reversed cell with no
emphasis equals `styleCell`; a reversed row with classes equals the class-less
row), not by captured goldens.

**R3. No new theme role.** A hit is `st().diffEmph` (word emphasis, exactly as
the spec says); the current hit is `st().searchCur` = `diffEmph.Underline(true)`,
built in `buildStyles`. *Why:* underline survives the Terminal theme (where
`bright` is empty and word emphasis is bold-only) and does not use reverse
video, which would cancel out on the reversed cursor row the current hit lands
on. Nothing in `internal/theme` changes, so the role-count tests (`53`) stand.
*Visual note for the reviewer:* under a reversed base, `Inherit(diffEmph)` keeps
`Foreground(bright)`, which reverse renders as a bright BACKGROUND patch on the
cursor row. That is legible and the width is unchanged; it is expected in the
smoke, not a defect.

**R4. Emphasis survives reverse video; only `cls` is dropped.** `window.go`
(`rcls = nil` under `GetReverse()`) and `twocol.go` (`renderPiece`) now keep the
emphasis half, and `colouredLine`'s "is there a mask" gate widens to "classes or
emphasis" (blame with syntax off has nil `cls`). The two-column path only keeps
it when some rune is actually emphasized (`runMask.hasEmph`), which is what
preserves byte-identity on an un-searched cursor row.

**R5. Wrap mode must not relayout per keystroke.** `cellSeg` gains `off` (the
segment's display-rune offset in its line), set by `wrapCells`; hits overlay at
render in all three diff modes (cutoff/scroll already slice by the window).
`winRow.emph` is sliced exactly like `cls` in all three window modes, and the
picker's `winCell` gets its hits on a per-render COPY of the cached mask.

**R6. A row whose two sides are identical is searched on the RIGHT only.**
Changed rows are searched left then right; Del left; Add right. Fold rows and
synthetic note rows are never searched. Hits are ordered (row, side, start).
*Why:* searching both sides of an unchanged row would make `]` visit the same
text twice.

**R7. Stepping is relative to the cursor.** `]` is the first hit strictly after
the cursor position, `[` the last strictly before, both wrapping; sitting
exactly on `hits[cur]` therefore yields `cur±1`. `]`/`[` always walk document
order regardless of `/` vs `@` — direction only picks the first hit from the
origin while typing and the badge glyph. `n`/`p` keep their change/region
meaning untouched, and `]`/`[` with no query are a no-op. The cursor position a
host reports when it is NOT sitting on a hit is the HEAD of its row
(`col: -1`), so `[` from such a row skips that row's own hits and lands on the
previous row's — the same column-0 semantics `]` uses in the other direction.
That is deliberate (one rule, both directions); it is not a bug report.

**R8. Origin and incremental search.** `/` or `@` records an origin (the cursor
position, plus the host's own scroll/pan). After every keystroke the host
re-finds and snaps to the first hit at/after (forward) or at/before (backward)
the origin, wrapping; the cursor moves there and the view scrolls/pans. `esc`
while typing restores the origin exactly and clears the query; `enter` keeps
both query and cursor; zero hits leaves the cursor at the origin with a `0/0`
badge; an empty query on `enter` records nothing and leaves no query active.
While typing, only esc / enter / backspace (+ctrl+h/delete) / space / runes and
the `alt+↑/↓` recall are handled — every other key is ignored, so `j` is text.

**R9. Panning is one shared helper.** `panFor(hOffset, tw, colStart, colEnd)`
moves minimally so `[colStart, colEnd)` is inside the window; `colStart` is a
display COLUMN (`lipgloss.Width` of the sanitized prefix), never a rune index.
The spec names only the diff, but a hit hidden past the right edge is a visible
defect in any of them, so blame, the preview and the picker pan in `modeScroll`
too. Vertically: the diff uses `setCursorLine` (minimal scroll); blame sets
`b.sel` (the render anchors on it); the preview has NO cursor — the current
hit's own colouring is the marker, so it scrolls minimally (above the window →
top line, below → last line); the picker sets `bi/side/line` and releases
`vshift` so the render re-anchors on the cursor.

**R10. Esc routing.** Diff / blame / picker: typing → cancel; a committed query
→ clear it and stay; otherwise the existing close. Blame's `b` closes
unconditionally — only `esc` is two-stage (never trap the user behind a search).
The preview replaces the old blanket "`/` does nothing while a preview is open"
guard, and its search branch runs BEFORE the tree's own typing branch; its `esc`
falls through to `closePreview` when no query is live. The picker's zoom still
takes `esc` first (`ctrl+t` state is the innermost), then the search, then the
close; the process-owned picker's `esc`, which `conflictProcess.update`
intercepts, is delegated to the picker whenever it is typing or holds a query.
`/` `@` `[` `]` join the output-pane pass-through allowlist, and `/`/`@` set
`outFocused = false` — the search always lives in the grid.

**R11. The badge rides on each host's header line**, never on new chrome: the
preview's `filePreviewRowsCap` mirrors the render math and a new line would need
a lockstep edit, and the diff's footer has no room. Diff: prepended to the
right-aligned status like `wrapCue`. Blame and preview: `padRight(truncate(title,
w-badge-2)) + "  " + badge`. Picker: appended after the region counts. The badge
is built WITHOUT `i18n.T` — `"/"`/`"@"` + the user's query + `"█"` while typing +
`"  N/M"` — so it adds no bundle key.

**R12. The diff footer stays inside 140 columns.** `[/] find` (10 columns) is
paid for by `[↑↓] scroll  [j/k] line` → `[↑↓/jk] scroll/line` (−4), `[ctrl+w]` →
`[^w]` (−4) and `[←→/0] pan` → `[←→0] pan` (−1). Measured: the scroll variant is
139 today and **exactly 140** after, wrap 127, trunc 128. *Deviation from the
brief, which expected two shortenings to suffice:* they come to 141, so the pan
label pays the last column, and `TestDiffHintFitsTheBudget` pins the 140 so the
next person shortens a label instead of overflowing. Blame, preview and picker
hints simply gain `[/] find` (the picker's wraps, never truncates).

**R13. i18n per commit.** Every commit that adds or changes an `i18n.T` literal
adds all four translations in that same commit, and DELETES the key it replaced
(`TestI18nBundlesComplete` fails on orphans as well as on gaps). The plan writes
out every translation line. Two rows reuse literals that already exist in the
bundles (`"clear the search, then close"` from the help window), and `"close"`
and `"back"` stay in use elsewhere, so nothing is orphaned by those.

**R14. Case folding walks runes.** `findHits` lowercases rune by rune (1:1) and
compares rune slices, so offsets stay aligned with the display mask;
`strings.ToLower` + `strings.Index` would give byte offsets and can change the
rune count. Overlapping matches are not reported (the walk advances past each
hit). Tested with a multi-byte rune before a hit and a wide glyph inside one.

**R15. Partial mode searches `v.lines` only** — folded rows are not searched,
because this is an *in-view* search — and the diff re-finds after any rebuild
(`f`, `ctrl+w`) while a query is active, re-snapping `cur` to the hit nearest
the cursor.

**R16. History.** `scopeInView = "inview"` is one shared ring for the four
readers, recalled via `m.recallUpdate(scopeInView, …)` at the TOP of the typing
branch (so `alt+↑/↓` keeps its own meaning outside it), recorded on `enter` with
a non-empty query, and `recallReset()` on open. *Verified, no change needed:*
`withRecall` is applied to the layer frame (`view.go:299`) and to the base frame
(`view.go:307`), so the dropdown draws over the diff, blame and picker layers
and over the preview's panel layout alike.

**R17. One helper file**, `internal/tui/textsearch.go`, holds the types, the
pure functions and the two shared key helpers, so no host copies a typing
branch. Three clarifications to the brief's sketch:

- `textSearch.origin` is a `searchPos` (the document position the incremental
  snap measures from). The view state each host restores on `esc` lives in the
  HOST struct (`diffOrigin`, `blameOrigin`, `previewOrigin`, `pickerOrigin`) —
  which is what the ruling itself requires, since those fields differ per host.
- `stepHit(hits, pos, delta)` takes no `cur`: strict before/after comparison
  against the cursor position already yields `cur±1` when the cursor sits on
  `hits[cur]`, so the extra parameter would be dead.
- The picker addresses hits by a FLAT candidate row plus side, because its
  cursor is three-valued (`bi`, `side`, `line`); `searchBase`/`searchRows` map
  between the two. Every other host's `row` is a line index.

  (Note: the package already has a `(*contentPopup).searchLine()` method; a
  method name and a package-level type may coincide in Go, and the preview's
  helpers are free functions, so there is no shadowing.)

**R18. Picker candidates** are, per block in order, every `Current` line (side
0) then every `Incoming` line (side 1). Literal context lines and the output
pane are never searched. A hit sets `e.bi`, `e.side`, `e.line`.

**R19. Help and footer are part of "done" for every host** (project memory:
advertise features in help AND footer). Each host section gains `r("/ @", …)`
and `r("] [", …)` rows, its `esc` row's wording changes to say the search is
cleared first, and the Global `alt+↑/↓` row grows the four new readers.

**R20. Scope.** No web change, no config key, no CLI or `agentskill` change.
README gains one clause per host row, CHANGELOG one `## [Unreleased]` bullet,
`docs/CLAUDE-details.md` one section, and the final task runs a headless
`tui-capture.sh` smoke against a purpose-built fixture repo.

**Further deviations from the brief, with reasons:**

- **`diffPaneLines` grows no parameter.** The brief expected a search argument
  plus a "no search" value at `history_view.go:267`; but `diffPaneLines` already
  receives the `*diffView`, which owns the search, and the history pane renders
  its own `diffView` that never routes search keys — so its search is the zero
  value and the call site needs no change at all. Fewer moving parts, same
  behaviour.
- **`sliceCls`/`wrapSegCls` become generic** (`sliceMask`/`wrapSegMask`) instead
  of growing emphasis-specific twins. Both masks pad with their zero value, so
  one implementation serves both and the two can never drift.

## Self-review

**Spec coverage (§4.3, lines 346–377).** One helper + four hosts: Task 2 builds
the helper, Tasks 3–6 the hosts. The state fields the spec names (query, typing,
direction, matches `{row, side, start, end}`, cur) are `textSearch` verbatim;
the pure functions (find, next/prev with wrap, badge `/foo 3/12` and `@foo`) are
`findHits`, `stepHit`/`nearestHit`, `badge`. Keys: `/` `@` incremental from the
cursor (R8), `enter` keeps, `esc` cancels, `]`/`[` wrap (R7), `n`/`p` untouched
(verified: no host's `n`/`p` arm is modified), two-stage `esc` (R10), one shared
history ring with `alt+↑/↓` (R16). Per-host behaviour: the diff searches both
sides, moves the cursor, scrolls and pans (Task 3); blame and the preview search
code lines only — the blame gutter is `winRow.prefix` and the preview rows carry
no prefix at all (Tasks 4–5); the picker searches current+incoming candidates in
document order, jumps the 2D cursor and leaves the output pane alone (Task 6).
Hit colouring: word emphasis for a hit, a distinct brighter style for the
current one, through `winRow`'s and `winCell`'s post-slice painters — the same
mechanism as the class mask (Task 1, R1/R3/R4). Excluded, and excluded here: no
regex, no whole-word, no web change (R20). **No gaps.**

**Placeholder scan.** No "TBD", no "similar to Task N", no "add error handling";
every code step carries the code, every i18n step carries the four translations,
every edit quotes the surrounding lines it anchors on. The one hedge that was in
an early draft (two alternative help wordings) was removed in favour of the
single literal the bundles translate.

**Type consistency.** `emphLevel`, `hitSpan`, `overlayHits`, `sliceMask`,
`wrapSegMask`, `maskAt` (Task 1) are used with those exact names in Tasks 3–6.
`searchLine`/`searchHit`/`searchPos`/`textSearch`/`searchEvent` and the ten
event constants (Task 2) are used unchanged by all four hosts. `hitCols` and
`panFor` take and return the same types everywhere. The four hosts' key hooks
share one shape — `(Model, tea.Cmd, bool)` for the diff/blame/preview, and the
picker inlines the same two blocks because its `update` already returns
`(Model, tea.Cmd)` from several early branches. `pickerCell`'s new parameter is
added at every call site (both, in one render loop). `sanitizeCell`'s new return
type reaches five callers: `wrapSide`, `scrollCell`, `hotEmphBody`,
`sanPickLine` and blame's render — the first four are edited in Task 1
(`sanPickLine` assigns into `runMask`, which widened in the same task), blame's
discards it (`disp, _, cls`) and needs no edit.

