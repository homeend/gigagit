# Character selection (`v` copy mode) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task (native execution by the session that wrote it; no implementer subagents). Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A modal, vim-visual-like copy mode in the TUI — `v` enters, `space` fixes the start, `enter`/`y` copies the covered **rendered** text, `esc` backs out — in the review Summary popup, the stacked review view (its summary element and its files) and the single diff view.

**Architecture:** One pure model (`charsel.go`: `charSel` over `charRow`s a `charHost` supplies, with the shared key handler `charSelKey`) and thin adapters per host. Painting rides the existing per-rune emphasis mask (`emphLevel`): two new levels, `emphSel` and `emphSelCur`, painted by `styledRuns` with the theme's `selection_bg` role (`styles.selectionStyle`) and the current-hit style for the cursor cell, so no renderer is added — the popup and the stack's prose rows get a `winRow.emph` / `colouredLine` mask, the diff's cells get selection spans through the `hitSpan` channel the search already uses.

**Tech Stack:** Go 1.26, Bubble Tea / lipgloss (`internal/tui`), `internal/i18n` bundles, the TUI's test helpers (`openedReviewView`, `openedDiffModel`, `feedDiff`, `captureClip`, `drainCmds`).

**Spec:** `docs/superpowers/specs/2026-10-10-char-select-design.md` (rulings C1–C8).

## Global Constraints

- The copy is the **rendered text**, never the markdown source (C1); `y` outside the mode keeps copying the summary's markdown.
- The mode is **modal** (C5): while on, only its keys act; every other key is handled and inert.
- Keys: `v` enters; `←→↑↓`/`hjkl`, `home end`/`0 $`, `w b`, `pgup pgdn` move; `space` fixes the start (again = restart); `enter`/`y` copies and leaves; `esc` drops the fixed start, then leaves (C4, C6).
- Every user-visible string goes through `i18n.T` with a literal key present in **all four** bundles `internal/i18n/lang/{ja,ko,zh,ru}.toml` (the AST gates `i18n_scan_test.go`, `options_vocab_test.go` fail otherwise); bundles are not key-sorted — append new keys at the end of `[strings]`.
- Clipboard writes go through `Model.copyToClipboardCmd`; tests read the copy back with `captureClip(m, &got)` and never touch the real clipboard.
- `internal/tui` never imports `internal/git` (archtest); no new `.` menu rows (keys only), so `diff_menu_parity_test.go` is untouched.
- Tests call `t.Parallel()` unless they set the process-global colour profile (`lipgloss.SetColorProfile`), as `diff_select_test.go` notes.
- Commits: one per task, `gg add <files>` then `git commit -F <msgfile>` with trailers `Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>` and `Claude-Session: https://claude.ai/code/session_01SxvmdF5zQp68vkozNLGDiW`; never `git add -A`, never push, never publish. Worktree `/work/gigagit/.claude/worktrees/char-select` (branch `feat/char-select`); absolute paths everywhere.

## Review Focus

1. **A wide rune (CJK, an emoji) inside a selected prose row**: the stripe and the cursor must land on the right cells and the copy must hold the whole rune. Pinned in Task 1 (`TestCharSelCopyKeepsWideRunes`): the model works in runes, never bytes or columns.
2. **A tab inside a selected diff line**: the stripe must cover the tab's expanded cells and the copy must keep the tab. Pinned in Task 4 (`TestDiffCharSelTabsPaintExpandedAndCopyRaw`): `dispCol` mirrors `sanitizeCell`'s expansion.
3. **The summary popup resized (or `ctrl+t`) while the mode is on**: the rows re-wrap, so the positions mean other text; the mode must leave rather than paint the wrong runes. Pinned in Task 3 (`TestSummaryCharSelLeavesOnRelayout`).
4. **`v` on a stack's header, gap or rule row, or on a folded file**: the element has rows but the cursor line is dead; the cursor must land on the element's first live row (or say nothing to select), never on a dead one. Pinned in Task 5 (`TestStackCharSelFromAHeaderLandsOnTheFirstRow`).
5. **A diff reload while the mode is on** (a `diffMsg`, `f`, `ctrl+w`): the line indexes mean other lines; the mode must leave with the line selection. Pinned in Task 4 (`TestDiffCharSelLeavesOnRebuild`).

---

## File structure

| File | Responsibility |
|---|---|
| `internal/tui/charsel.go` (new) | the pure model: `pos`, `charRow`, `charHost`, `charSel`, movement, `charSelText`, `charSelKey`, `charSelHint`, `charSelEmph`, `charSelSpans`, `shiftMask` |
| `internal/tui/charsel_test.go` (new) | the model's tests |
| `internal/tui/textsearch.go` | `emphSel`, `emphSelCur`; `hitSpan.sel`; `overlayHits` maps them |
| `internal/tui/diff_render.go` | `styledRuns` paints the two levels; `renderDiffView` feeds selection spans and the mode's hint; `diffHintFor` untouched |
| `internal/tui/content_popup.go` | `contentPopup.cs` + its layout (`charLayout`, `charRows`, `charPage`, `charEmphFor`, `charFollow`); `layoutWidths`; `pageH`; the keys/hint swap while on |
| `internal/tui/window.go` | `wrapSegOffsets` (the inverse of `wrapSegMask`'s mapping) |
| `internal/tui/review_view.go` | `reviewSummaryPopup.update` routes `v` and the mode; the keys line gains `[v] select` |
| `internal/tui/diff_view.go` | `diffView.cs`, `csBase`, `csFile`, `csPage`; `dispCol`; `charRows`/`charPage`/`charSpansOn`/`charEmphProse`; the `v` hook; the mode leaves on rebuild/relayout |
| `internal/tui/diff_charsel.go` (new) | `m.diffCharKey` (the hook), `m.diffCharEnter`, the stack's element bounds and notices |
| `internal/tui/diff_stack_render.go` | prose rows take the selection mask |
| `internal/tui/md_render.go` | `mdRow.cont` (a wrap continuation), set by `mdWrap`, carried by `mdPrefix` |
| `internal/tui/help.go` | the `v` rows |
| `internal/i18n/lang/{ja,ko,zh,ru}.toml` | the strings |
| `README.md`, `CHANGELOG.md`, `docs/CLAUDE-details.md` | docs |

---

### Task 1: The pure model (`charsel.go`)

**Files:**
- Create: `internal/tui/charsel.go`
- Test: `internal/tui/charsel_test.go`

**Interfaces:**
- Produces (all in package `tui`):
  - `type pos struct{ row, col int }`
  - `type charRow struct{ text []rune; wraps, dead bool }`
  - `type charHost interface{ charRows() []charRow; charPage() int }`
  - `type charSel struct{ on, fixed bool; anchor, cur pos; want int }` with `enter(rows []charRow, at pos) bool`, `leave()`, `press()`, `esc() (left bool)`, `bounds() (lo, hi pos, ok bool)`, `covers(row, col int) bool`, `count(rows []charRow) int`, `left/right/up/down/home/end/word(rows …)`.
  - `charSelText(rows []charRow, lo, hi pos) string`
  - `type charSelResult struct{ handled bool; copy, notice string; bump bool }`
  - `charSelKey(cs *charSel, host charHost, msg tea.KeyMsg) charSelResult`
  - `charSelHint(cs charSel, rows []charRow) string`
  - `copiedCharsText(n int) string`
  - `charSelEmph(cs charSel, row, n int) []emphLevel` and `shiftMask(mask []emphLevel, lead, n int) []emphLevel` (Task 2 adds the two levels they use; Task 1 declares them in `textsearch.go` so this task compiles).
  - `charSelSpans(cs charSel, row int) []hitSpan` (source-rune spans, `sel: true`, the cursor's `cur: true`).

- [ ] **Step 1: Write the failing tests**

Create `internal/tui/charsel_test.go`:

```go
package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// rowsOf builds host rows from strings: "~text" is a wrap continuation,
// "!" alone is a dead row.
func rowsOf(ss ...string) []charRow {
	out := make([]charRow, len(ss))
	for i, s := range ss {
		switch {
		case s == "!":
			out[i] = charRow{dead: true}
		case strings.HasPrefix(s, "~"):
			out[i] = charRow{text: []rune(s[1:]), wraps: true}
		default:
			out[i] = charRow{text: []rune(s)}
		}
	}
	return out
}

type fakeHost struct {
	rows []charRow
	page int
}

func (h fakeHost) charRows() []charRow { return h.rows }
func (h fakeHost) charPage() int       { return h.page }

func feedSel(cs *charSel, h charHost, keys ...string) (last charSelResult) {
	for _, k := range keys {
		last = charSelKey(cs, h, keyMsg(k))
	}
	return last
}

// enter lands on the row asked for when it is live, else on the next live
// one; a host with no live row refuses.
func TestCharSelEnterLandsOnALiveRow(t *testing.T) {
	t.Parallel()
	rows := rowsOf("!", "!", "abc", "de")
	var cs charSel
	if !cs.enter(rows, pos{0, 0}) || cs.cur != (pos{2, 0}) || !cs.on || cs.fixed {
		t.Fatalf("enter = %+v", cs)
	}
	var none charSel
	if none.enter(rowsOf("!", "!"), pos{0, 0}) || none.on {
		t.Fatal("a host with no live row must refuse")
	}
	var empty charSel
	if empty.enter(nil, pos{0, 0}) {
		t.Fatal("no rows at all must refuse")
	}
}

// ←/→ move by rune and run across rows; dead rows are stepped over; the
// ends clamp (bump reports a move that went nowhere).
func TestCharSelLeftRightRunAcrossRows(t *testing.T) {
	t.Parallel()
	h := fakeHost{rows: rowsOf("ab", "!", "cd"), page: 10}
	var cs charSel
	cs.enter(h.rows, pos{0, 1})
	if r := feedSel(&cs, h, "right"); cs.cur != (pos{2, 0}) || r.bump {
		t.Fatalf("right past the end of row 0 = %+v (%+v)", cs.cur, r)
	}
	if r := feedSel(&cs, h, "left"); cs.cur != (pos{0, 1}) || r.bump {
		t.Fatalf("left past the start of row 2 = %+v (%+v)", cs.cur, r)
	}
	feedSel(&cs, h, "h")
	if r := feedSel(&cs, h, "h"); cs.cur != (pos{0, 0}) || !r.bump {
		t.Fatalf("left at the very start must bump: %+v (%+v)", cs.cur, r)
	}
	feedSel(&cs, h, "l", "l", "l")
	if r := feedSel(&cs, h, "l"); cs.cur != (pos{2, 1}) || !r.bump {
		t.Fatalf("right at the very end must bump: %+v (%+v)", cs.cur, r)
	}
}

// ↑/↓ keep the wanted column across a shorter row; home/end/0/$ hit the
// row's ends; pgup/pgdn move a page.
func TestCharSelUpDownKeepTheColumn(t *testing.T) {
	t.Parallel()
	h := fakeHost{rows: rowsOf("abcdef", "xy", "123456", "", "qwerty"), page: 2}
	var cs charSel
	cs.enter(h.rows, pos{0, 4})
	feedSel(&cs, h, "down")
	if cs.cur != (pos{1, 1}) {
		t.Fatalf("down onto a short row = %+v, want clamped to its last rune", cs.cur)
	}
	feedSel(&cs, h, "j")
	if cs.cur != (pos{2, 4}) {
		t.Fatalf("down again = %+v, want the wanted column 4 back", cs.cur)
	}
	feedSel(&cs, h, "down")
	if cs.cur != (pos{3, 0}) {
		t.Fatalf("down onto an empty row = %+v, want col 0", cs.cur)
	}
	feedSel(&cs, h, "up", "up", "up")
	if cs.cur != (pos{0, 4}) {
		t.Fatalf("back up = %+v", cs.cur)
	}
	feedSel(&cs, h, "end")
	if cs.cur != (pos{0, 5}) {
		t.Fatalf("end = %+v", cs.cur)
	}
	feedSel(&cs, h, "0")
	if cs.cur != (pos{0, 0}) {
		t.Fatalf("0 = %+v", cs.cur)
	}
	feedSel(&cs, h, "$")
	if cs.cur != (pos{0, 5}) {
		t.Fatalf("$ = %+v", cs.cur)
	}
	feedSel(&cs, h, "home")
	feedSel(&cs, h, "pgdown")
	if cs.cur != (pos{2, 0}) {
		t.Fatalf("pgdn (page 2) = %+v", cs.cur)
	}
	feedSel(&cs, h, "pgup")
	if cs.cur != (pos{0, 0}) {
		t.Fatalf("pgup = %+v", cs.cur)
	}
}

// w/b step by word start, across rows (a row boundary reads as a space).
func TestCharSelWordSteps(t *testing.T) {
	t.Parallel()
	h := fakeHost{rows: rowsOf("ab  cd", "ef"), page: 10}
	var cs charSel
	cs.enter(h.rows, pos{0, 0})
	feedSel(&cs, h, "w")
	if cs.cur != (pos{0, 4}) {
		t.Fatalf("w = %+v, want cd", cs.cur)
	}
	feedSel(&cs, h, "w")
	if cs.cur != (pos{1, 0}) {
		t.Fatalf("w across rows = %+v, want ef", cs.cur)
	}
	if r := feedSel(&cs, h, "w"); cs.cur != (pos{1, 0}) || !r.bump {
		t.Fatalf("w at the last word must bump: %+v", cs.cur)
	}
	feedSel(&cs, h, "b")
	if cs.cur != (pos{0, 4}) {
		t.Fatalf("b = %+v, want cd", cs.cur)
	}
	feedSel(&cs, h, "l", "b")
	if cs.cur != (pos{0, 4}) {
		t.Fatalf("b from inside a word = %+v, want its start", cs.cur)
	}
	feedSel(&cs, h, "b")
	if cs.cur != (pos{0, 0}) {
		t.Fatalf("b = %+v, want ab", cs.cur)
	}
}

// space fixes the start, a second space restarts there; enter copies the
// bounds in reading order; esc drops the start, then leaves.
func TestCharSelSpaceEnterEsc(t *testing.T) {
	t.Parallel()
	h := fakeHost{rows: rowsOf("hello world", "~second row", "third"), page: 10}
	var cs charSel
	cs.enter(h.rows, pos{0, 6})
	if r := feedSel(&cs, h, "enter"); r.copy != "" || r.notice == "" || !cs.on {
		t.Fatalf("enter without a start must say so and stay: %+v", r)
	}
	feedSel(&cs, h, "space")
	if !cs.fixed || cs.anchor != (pos{0, 6}) {
		t.Fatalf("space = %+v", cs)
	}
	feedSel(&cs, h, "down", "end")
	if lo, hi, ok := cs.bounds(); !ok || lo != (pos{0, 6}) || hi != (pos{1, 9}) {
		t.Fatalf("bounds = %+v %+v %v", lo, hi, ok)
	}
	// Backwards: the range reads anchor..cur in reading order either way.
	cs2 := cs
	cs2.cur = pos{0, 2}
	if lo, hi, _ := cs2.bounds(); lo != (pos{0, 2}) || hi != (pos{0, 6}) {
		t.Fatalf("backward bounds = %+v %+v", lo, hi)
	}
	feedSel(&cs, h, "space") // restart at the cursor
	if cs.anchor != (pos{1, 9}) || !cs.fixed {
		t.Fatalf("second space must restart at the cursor: %+v", cs)
	}
	feedSel(&cs, h, "j") // onto "third", column clamped to its last rune
	r := feedSel(&cs, h, "enter")
	if r.copy != "w\nthird" || cs.on {
		t.Fatalf("enter copied %q (on=%v), want the covered text across the row break", r.copy, cs.on)
	}
	cs.enter(h.rows, pos{0, 0})
	feedSel(&cs, h, "space", "l")
	if left := cs.esc(); left || cs.fixed || !cs.on {
		t.Fatalf("first esc drops the start only: left=%v %+v", left, cs)
	}
	if left := cs.esc(); !left || cs.on {
		t.Fatalf("second esc leaves: left=%v %+v", left, cs)
	}
	cs.enter(h.rows, pos{0, 0})
	if r := feedSel(&cs, h, "space", "l", "l", "y"); r.copy != "hel" || cs.on {
		t.Fatalf("y is enter: %q", r.copy)
	}
}

// The copy joins a wrap continuation with one space and any other row with
// a newline, skips dead rows, keeps an empty row as a blank line, and a
// one-rune selection copies that rune.
func TestCharSelTextJoins(t *testing.T) {
	t.Parallel()
	rows := rowsOf("one two", "~three", "!", "", "five")
	if got := charSelText(rows, pos{0, 4}, pos{4, 1}); got != "two three\n\nfi" {
		t.Fatalf("got %q", got)
	}
	if got := charSelText(rows, pos{0, 2}, pos{0, 2}); got != "e" {
		t.Fatalf("one rune = %q", got)
	}
	if got := charSelText(rows, pos{1, 0}, pos{1, 4}); got != "three" {
		t.Fatalf("a continuation alone joins nothing in front: %q", got)
	}
}

// Review Focus 1: runes, never bytes or columns — a wide rune is one step
// and copies whole.
func TestCharSelCopyKeepsWideRunes(t *testing.T) {
	t.Parallel()
	h := fakeHost{rows: rowsOf("a漢字b"), page: 10}
	var cs charSel
	cs.enter(h.rows, pos{0, 0})
	r := feedSel(&cs, h, "l", "space", "l", "enter")
	if r.copy != "漢字" {
		t.Fatalf("copy = %q", r.copy)
	}
	if n := (charSel{on: true, fixed: true, anchor: pos{0, 1}, cur: pos{0, 2}}).count(h.rows); n != 2 {
		t.Fatalf("count = %d", n)
	}
}

// Every key is the mode's while on: an unknown one is handled and inert.
func TestCharSelIsModal(t *testing.T) {
	t.Parallel()
	h := fakeHost{rows: rowsOf("abc"), page: 10}
	var cs charSel
	cs.enter(h.rows, pos{0, 1})
	for _, k := range []string{"n", "/", "v", "ctrl+w", "alt+left", "S", "q"} {
		r := charSelKey(&cs, h, keyMsg(k))
		if !r.handled || r.copy != "" || cs.cur != (pos{0, 1}) || !cs.on {
			t.Fatalf("%s: %+v %+v", k, r, cs)
		}
	}
	if r := charSelKey(&cs, h, tea.KeyMsg{Type: tea.KeyCtrlC}); r.handled {
		t.Fatal("ctrl+c is never the mode's (the host quits)")
	}
}

// The emphasis mask: the covered runes wear emphSel, the cursor emphSelCur,
// an untouched row has no mask; shiftMask moves it behind a lead.
func TestCharSelEmphMask(t *testing.T) {
	t.Parallel()
	cs := charSel{on: true, fixed: true, anchor: pos{0, 1}, cur: pos{1, 1}}
	if m := charSelEmph(cs, 0, 4); !equalEmph(m, []emphLevel{emphNone, emphSel, emphSel, emphSel}) {
		t.Fatalf("row 0 = %v", m)
	}
	if m := charSelEmph(cs, 1, 3); !equalEmph(m, []emphLevel{emphSel, emphSelCur, emphNone}) {
		t.Fatalf("row 1 = %v", m)
	}
	if m := charSelEmph(cs, 2, 3); m != nil {
		t.Fatalf("row 2 = %v, want nil", m)
	}
	loose := charSel{on: true, cur: pos{0, 2}}
	if m := charSelEmph(loose, 0, 3); !equalEmph(m, []emphLevel{emphNone, emphNone, emphSelCur}) {
		t.Fatalf("not fixed = %v, want the cursor only", m)
	}
	if m := shiftMask([]emphLevel{emphSel, emphSelCur}, 2, 5); !equalEmph(m, []emphLevel{emphNone, emphNone, emphSel, emphSelCur, emphNone}) {
		t.Fatalf("shift = %v", m)
	}
	if sp := charSelSpans(cs, 1); len(sp) != 2 || sp[0] != (hitSpan{start: 0, end: 2, sel: true}) || sp[1] != (hitSpan{start: 1, end: 2, sel: true, cur: true}) {
		t.Fatalf("spans = %+v", sp)
	}
}

func equalEmph(a, b []emphLevel) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// The hint names the stage; the copy line counts runes.
func TestCharSelHintAndCopiedText(t *testing.T) {
	t.Parallel()
	rows := rowsOf("abcdef")
	if h := charSelHint(charSel{on: true, cur: pos{0, 0}}, rows); !strings.Contains(h, "[space] start") {
		t.Fatalf("loose hint = %q", h)
	}
	if h := charSelHint(charSel{on: true, fixed: true, anchor: pos{0, 0}, cur: pos{0, 2}}, rows); !strings.Contains(h, "3 chars") {
		t.Fatalf("fixed hint = %q", h)
	}
	if copiedCharsText(1) != "Copied 1 character" || copiedCharsText(3) != "Copied 3 characters" {
		t.Fatalf("%q / %q", copiedCharsText(1), copiedCharsText(3))
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `cd /work/gigagit/.claude/worktrees/char-select && go test ./internal/tui/ -run 'TestCharSel' 2>&1 | tail -5`
Expected: build failure — `undefined: charRow`, `charSel`, …

- [ ] **Step 3: Declare the two emphasis levels and the `sel` flag**

In `internal/tui/textsearch.go`, the `emphLevel` constants become:

```go
const (
	emphNone emphLevel = iota // plain: the syntax class (if any) paints
	emphWord                  // inside a word-diff span (what sanitizeCell marks)
	emphHit                   // inside a search hit
	emphCur                   // inside the CURRENT search hit
	emphSel                   // inside a CHARACTER selection (charsel.go): the selection stripe
	emphSelCur                // the character selection's CURSOR cell
)
```

and `hitSpan` gains a field (Task 2 teaches `overlayHits` and `styledRuns` about it):

```go
type hitSpan struct {
	start, end int
	cur        bool
	// sel marks a CHARACTER-selection span (charsel.go) rather than a search
	// hit: it paints emphSel, and with cur set emphSelCur (the cursor cell).
	sel bool
}
```

- [ ] **Step 4: Write the model**

Create `internal/tui/charsel.go`:

```go
package tui

import (
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/i18n"
)

// charsel.go — the character selection (spec 2026-10-10-char-select): a
// MODAL copy mode in the spirit of vim's visual mode and tmux's copy mode,
// over a host's rows of text. v enters it (the host's key), the cursor moves
// by rune / row / word / page, space fixes the start, enter or y copies the
// covered text and leaves, esc drops the start, then leaves. While it is on
// EVERY key is the mode's: the hosts route each key through charSelKey and
// never see one themselves (C5).
//
// The model is pure: it knows rows of runes (charRow) a host hands it, never
// display cells, gutters or line numbers. A wrapped paragraph is several
// rows, as on screen (the host says which rows continue the previous one,
// so a copy joins them with a space); a row the host marks dead (a diff
// side's absent cell, a folded line, a header) is stepped over and never
// copied.

// pos is a position in a host's rows: a row index and a rune index into it.
type pos struct{ row, col int }

// before reports whether p reads before q (row first, then column).
func (p pos) before(q pos) bool { return p.row < q.row || (p.row == q.row && p.col < q.col) }

// charRow is one row of a host's text.
type charRow struct {
	text  []rune
	wraps bool // continues the previous row: a copy joins them with one space
	dead  bool // not selectable: an absent diff cell, a folded line, a placeholder
}

// charHost is what a surface hands the mode: its rows, in display order,
// and the visible height (pgup/pgdn).
type charHost interface {
	charRows() []charRow
	charPage() int
}

// charSel is the mode's state. The zero value is "off".
type charSel struct {
	on     bool // the mode is active: a cursor shows, the keys are modal
	fixed  bool // space fixed the start: anchor..cur is the live range
	anchor pos
	cur    pos
	want   int // the column ↑/↓ try to keep (clamped on a shorter row)
}

// liveRow is the nearest live row from `from` stepping dir (±1), `from`
// itself included; -1 when there is none that way.
func liveRow(rows []charRow, from, dir int) int {
	for i := from; i >= 0 && i < len(rows); i += dir {
		if !rows[i].dead {
			return i
		}
	}
	return -1
}

// lastCol is the last rune index of a row (0 on an empty row: the cursor
// sits on the empty row itself).
func lastCol(rows []charRow, row int) int {
	if n := len(rows[row].text); n > 0 {
		return n - 1
	}
	return 0
}

// enter turns the mode on with the cursor at `at`, or on the first live row
// below it (then above it) when `at`'s row is dead; false — and off — when
// the host has no live row at all.
func (s *charSel) enter(rows []charRow, at pos) bool {
	*s = charSel{}
	if len(rows) == 0 {
		return false
	}
	row := at.row
	if row < 0 {
		row = 0
	}
	if row > len(rows)-1 {
		row = len(rows) - 1
	}
	r := liveRow(rows, row, 1)
	if r < 0 {
		r = liveRow(rows, row, -1)
	}
	if r < 0 {
		return false
	}
	col := at.col
	if r != at.row {
		col = 0
	}
	if col < 0 {
		col = 0
	}
	if col > lastCol(rows, r) {
		col = lastCol(rows, r)
	}
	s.on, s.cur, s.want = true, pos{r, col}, col
	return true
}

// leave turns the mode off.
func (s *charSel) leave() { *s = charSel{} }

// press is space: fix the start at the cursor — again, start over there.
func (s *charSel) press() {
	if !s.on {
		return
	}
	s.fixed, s.anchor = true, s.cur
}

// esc drops the fixed start, or — with none — leaves the mode. left says
// the mode is now off.
func (s *charSel) esc() (left bool) {
	if s.fixed {
		s.fixed = false
		return false
	}
	s.leave()
	return true
}

// bounds is the inclusive range anchor..cur in reading order; ok is false
// without a fixed start.
func (s charSel) bounds() (lo, hi pos, ok bool) {
	if !s.on || !s.fixed {
		return pos{}, pos{}, false
	}
	lo, hi = s.anchor, s.cur
	if hi.before(lo) {
		lo, hi = hi, lo
	}
	return lo, hi, true
}

// covers reports whether (row, col) is inside the fixed range.
func (s charSel) covers(row, col int) bool {
	lo, hi, ok := s.bounds()
	if !ok {
		return false
	}
	p := pos{row, col}
	return !p.before(lo) && !hi.before(p)
}

// count is how many runes the fixed range covers (dead rows skipped).
func (s charSel) count(rows []charRow) int {
	lo, hi, ok := s.bounds()
	if !ok {
		return 0
	}
	n := 0
	for i := lo.row; i <= hi.row && i < len(rows); i++ {
		if rows[i].dead {
			continue
		}
		a, b := 0, len(rows[i].text)
		if i == lo.row {
			a = lo.col
		}
		if i == hi.row {
			b = min(hi.col+1, len(rows[i].text))
		}
		if b > a {
			n += b - a
		}
	}
	return n
}

// --- movement: every move clamps to the rows and steps over dead ones; it
// reports whether the cursor went anywhere (false = a bump at an end) ----

func (s *charSel) left(rows []charRow) bool {
	if s.cur.col > 0 {
		s.cur.col--
		s.want = s.cur.col
		return true
	}
	if r := liveRow(rows, s.cur.row-1, -1); r >= 0 {
		s.cur = pos{r, lastCol(rows, r)}
		s.want = s.cur.col
		return true
	}
	return false
}

func (s *charSel) right(rows []charRow) bool {
	if s.cur.col < lastCol(rows, s.cur.row) {
		s.cur.col++
		s.want = s.cur.col
		return true
	}
	if r := liveRow(rows, s.cur.row+1, 1); r >= 0 {
		s.cur = pos{r, 0}
		s.want = 0
		return true
	}
	return false
}

// vertical moves n live rows in dir (±1), keeping the wanted column.
func (s *charSel) vertical(rows []charRow, dir, n int) bool {
	moved := false
	for k := 0; k < n; k++ {
		r := liveRow(rows, s.cur.row+dir, dir)
		if r < 0 {
			break
		}
		s.cur.row = r
		moved = true
	}
	if moved {
		s.cur.col = min(s.want, lastCol(rows, s.cur.row))
	}
	return moved
}

func (s *charSel) home(rows []charRow) bool {
	moved := s.cur.col != 0
	s.cur.col, s.want = 0, 0
	return moved
}

func (s *charSel) end(rows []charRow) bool {
	c := lastCol(rows, s.cur.row)
	moved := s.cur.col != c
	s.cur.col, s.want = c, c
	return moved
}

// wordStarts lists every word start (a non-space rune after a space, a
// row's first non-space rune) in reading order.
func wordStarts(rows []charRow) []pos {
	var out []pos
	for i, r := range rows {
		if r.dead {
			continue
		}
		inWord := false
		for c, ch := range r.text {
			if unicode.IsSpace(ch) {
				inWord = false
				continue
			}
			if !inWord {
				out = append(out, pos{i, c})
				inWord = true
			}
		}
	}
	return out
}

// word is w (dir 1): the next word start after the cursor; b (dir -1): the
// previous one before it.
func (s *charSel) word(rows []charRow, dir int) bool {
	starts := wordStarts(rows)
	if dir > 0 {
		for _, p := range starts {
			if s.cur.before(p) {
				s.cur, s.want = p, p.col
				return true
			}
		}
		return false
	}
	for i := len(starts) - 1; i >= 0; i-- {
		if starts[i].before(s.cur) {
			s.cur, s.want = starts[i], starts[i].col
			return true
		}
	}
	return false
}

// charSelText is the covered text: on the first row from lo.col, on the
// last to hi.col inclusive, whole rows between; dead rows skipped; a wrap
// continuation joins the row before it with one space, any other row with a
// newline; no trailing newline.
func charSelText(rows []charRow, lo, hi pos) string {
	var b strings.Builder
	first := true
	for i := lo.row; i <= hi.row && i < len(rows); i++ {
		r := rows[i]
		if r.dead {
			continue
		}
		a, e := 0, len(r.text)
		if i == lo.row {
			a = min(lo.col, len(r.text))
		}
		if i == hi.row {
			e = min(hi.col+1, len(r.text))
		}
		if !first {
			if r.wraps {
				b.WriteByte(' ')
			} else {
				b.WriteByte('\n')
			}
		}
		first = false
		if e > a {
			b.WriteString(string(r.text[a:e]))
		}
	}
	return b.String()
}

// charSelResult is what a key did: handled (every key while the mode is on,
// except ctrl+c), the copied text (enter / y with a fixed start), a notice
// for the host's status line, and bump — a move that hit an end (a stacked
// host says which element bounds it).
type charSelResult struct {
	handled bool
	copy    string
	notice  string
	bump    bool
}

// charSelKey applies one key to cs over host. The hosts call it for every
// key while cs.on and act on the result; they never interpret a key
// themselves in the mode (C5).
func charSelKey(cs *charSel, host charHost, msg tea.KeyMsg) charSelResult {
	if !cs.on || msg.Type == tea.KeyCtrlC {
		return charSelResult{}
	}
	rows := host.charRows()
	res := charSelResult{handled: true}
	moved := true
	switch msg.String() {
	case "left", "h":
		moved = cs.left(rows)
	case "right", "l":
		moved = cs.right(rows)
	case "up", "k":
		moved = cs.vertical(rows, -1, 1)
	case "down", "j":
		moved = cs.vertical(rows, 1, 1)
	case "pgup":
		moved = cs.vertical(rows, -1, max(host.charPage(), 1))
	case "pgdown":
		moved = cs.vertical(rows, 1, max(host.charPage(), 1))
	case "home", "0":
		cs.home(rows)
	case "end", "$":
		cs.end(rows)
	case "w":
		moved = cs.word(rows, 1)
	case "b":
		moved = cs.word(rows, -1)
	case " ":
		cs.press()
	case "enter", "y":
		lo, hi, ok := cs.bounds()
		if !ok {
			res.notice = i18n.T("▸ space fixes the start first")
			return res
		}
		res.copy = charSelText(rows, lo, hi)
		cs.leave()
	case "esc":
		cs.esc()
	}
	res.bump = !moved
	return res
}

// charSelHint is the footer while the mode is on: the keys of its stage.
func charSelHint(cs charSel, rows []charRow) string {
	if cs.fixed {
		return i18n.T("[←→↑↓ hjkl] move  [space] restart  [enter y] copy %d chars  [esc] drop", cs.count(rows))
	}
	return i18n.T("[←→↑↓ hjkl] move  [w b] word  [space] start  [enter] copy  [esc] leave")
}

// copiedCharsText is the status line after a copy.
func copiedCharsText(n int) string {
	if n == 1 {
		return i18n.T("Copied 1 character")
	}
	return i18n.T("Copied %d characters", n)
}

// charSelEmph is the emphasis mask of row `row` (n runes): emphSel on the
// covered runes, emphSelCur on the cursor; nil when the row is untouched.
func charSelEmph(cs charSel, row, n int) []emphLevel {
	if !cs.on {
		return nil
	}
	var out []emphLevel
	set := func(i int, l emphLevel) {
		if i < 0 || i >= n {
			return
		}
		if out == nil {
			out = make([]emphLevel, n)
		}
		out[i] = l
	}
	if lo, hi, ok := cs.bounds(); ok && lo.row <= row && row <= hi.row {
		a, e := 0, n-1
		if row == lo.row {
			a = lo.col
		}
		if row == hi.row {
			e = hi.col
		}
		for i := a; i <= e; i++ {
			set(i, emphSel)
		}
	}
	if cs.cur.row == row {
		set(cs.cur.col, emphSelCur)
	}
	return out
}

// shiftMask places mask behind `lead` runes in a row of n: what a host whose
// display row starts with layout (a "  " lead, a wrap indent) needs.
func shiftMask(mask []emphLevel, lead, n int) []emphLevel {
	if mask == nil {
		return nil
	}
	out := make([]emphLevel, n)
	for i, l := range mask {
		if j := i + lead; j >= 0 && j < n {
			out[j] = l
		}
	}
	return out
}

// charSelSpans is the selection on row `row` as spans in the row's own rune
// space (sel: true): the covered range, then the cursor cell (cur: true) —
// in that order, so the cursor paints over the stripe. nil when untouched.
func charSelSpans(cs charSel, row int) []hitSpan {
	if !cs.on {
		return nil
	}
	var out []hitSpan
	if lo, hi, ok := cs.bounds(); ok && lo.row <= row && row <= hi.row {
		a, e := 0, 1<<30
		if row == lo.row {
			a = lo.col
		}
		if row == hi.row {
			e = hi.col + 1
		}
		out = append(out, hitSpan{start: a, end: e, sel: true})
	}
	if cs.cur.row == row {
		out = append(out, hitSpan{start: cs.cur.col, end: cs.cur.col + 1, sel: true, cur: true})
	}
	return out
}
```

The test expects `charSelSpans` row 1's first span to be `{0, 2}`: with `hi.col = 1`, `e = 2` — but the "whole rows between" case uses `1<<30`; the painters clamp (`overlayHits` clamps `hi` to `n`). Keep the test's row 1 case as written: it is the LAST row, so `e = hi.col+1 = 2`.

- [ ] **Step 5: Add the strings to the four bundles**

Append under `[strings]` at the end of each file:

`internal/i18n/lang/ja.toml`:
```toml
"▸ space fixes the start first" = "▸ まず space で始点を固定します"
"[←→↑↓ hjkl] move  [space] restart  [enter y] copy %d chars  [esc] drop" = "[←→↑↓ hjkl] 移動  [space] やり直し  [enter y] %d 文字をコピー  [esc] 解除"
"[←→↑↓ hjkl] move  [w b] word  [space] start  [enter] copy  [esc] leave" = "[←→↑↓ hjkl] 移動  [w b] 単語  [space] 始点  [enter] コピー  [esc] 終了"
"Copied 1 character" = "1 文字をコピーしました"
"Copied %d characters" = "%d 文字をコピーしました"
```
`ko.toml`:
```toml
"▸ space fixes the start first" = "▸ 먼저 space로 시작점을 고정하세요"
"[←→↑↓ hjkl] move  [space] restart  [enter y] copy %d chars  [esc] drop" = "[←→↑↓ hjkl] 이동  [space] 다시 시작  [enter y] %d자 복사  [esc] 해제"
"[←→↑↓ hjkl] move  [w b] word  [space] start  [enter] copy  [esc] leave" = "[←→↑↓ hjkl] 이동  [w b] 단어  [space] 시작  [enter] 복사  [esc] 나가기"
"Copied 1 character" = "1자를 복사했습니다"
"Copied %d characters" = "%d자를 복사했습니다"
```
`zh.toml`:
```toml
"▸ space fixes the start first" = "▸ 先按 space 固定起点"
"[←→↑↓ hjkl] move  [space] restart  [enter y] copy %d chars  [esc] drop" = "[←→↑↓ hjkl] 移动  [space] 重新开始  [enter y] 复制 %d 个字符  [esc] 取消"
"[←→↑↓ hjkl] move  [w b] word  [space] start  [enter] copy  [esc] leave" = "[←→↑↓ hjkl] 移动  [w b] 单词  [space] 起点  [enter] 复制  [esc] 退出"
"Copied 1 character" = "已复制 1 个字符"
"Copied %d characters" = "已复制 %d 个字符"
```
`ru.toml`:
```toml
"▸ space fixes the start first" = "▸ сначала space фиксирует начало"
"[←→↑↓ hjkl] move  [space] restart  [enter y] copy %d chars  [esc] drop" = "[←→↑↓ hjkl] переход  [space] заново  [enter y] копировать %d симв.  [esc] снять"
"[←→↑↓ hjkl] move  [w b] word  [space] start  [enter] copy  [esc] leave" = "[←→↑↓ hjkl] переход  [w b] слово  [space] начало  [enter] копировать  [esc] выйти"
"Copied 1 character" = "Скопирован 1 символ"
"Copied %d characters" = "Скопировано %d символов"
```

- [ ] **Step 6: Run the tests**

Run: `cd /work/gigagit/.claude/worktrees/char-select && gofmt -l internal/tui/ && go vet ./internal/tui/ && go test ./internal/tui/ -run 'TestCharSel|TestI18n|TestOptionsVocab' 2>&1 | tail -5`
Expected: PASS (the i18n gates see every new key in all four bundles).

- [ ] **Step 7: Commit**

```bash
cd /work/gigagit/.claude/worktrees/char-select && gg add internal/tui/charsel.go internal/tui/charsel_test.go internal/tui/textsearch.go internal/i18n/lang/ja.toml internal/i18n/lang/ko.toml internal/i18n/lang/zh.toml internal/i18n/lang/ru.toml && git commit -F <msgfile>
```
Message: `tui(charsel): the character-selection model — rows, cursor moves, space/enter/esc, the copy join, the emphasis mask`.

---

### Task 2: Painting the two emphasis levels

**Files:**
- Modify: `internal/tui/textsearch.go` (`overlayHits`)
- Modify: `internal/tui/diff_render.go` (`styledRuns`)
- Test: `internal/tui/charsel_paint_test.go` (new)

**Interfaces:**
- Consumes: `emphSel`, `emphSelCur`, `hitSpan.sel` (Task 1); `styles.selectionStyle`, `styles.currentHitStyle`.
- Produces: `styledRuns` paints `emphSel` with `st().selectionStyle(base)` and `emphSelCur` with `st().currentHitStyle(st().selectionStyle(base))`; `overlayHits` maps a `sel` span to those levels.

- [ ] **Step 1: Write the failing test**

Create `internal/tui/charsel_paint_test.go`:

```go
package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/homeend/gigagit/internal/theme"
)

// NOTE: sets the process-global colour profile — not parallel (see
// diff_select_test.go).

// emphSel paints the selection stripe, emphSelCur the cursor cell over it;
// overlayHits maps a sel span to them (the cursor span wins).
func TestCharSelLevelsPaintStripeAndCursor(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(prev)
	prevTheme := activeTheme()
	defer setTheme(prevTheme)
	setTheme(theme.Dark) // selection_bg set: the stripe is a background patch
	s := st()
	base := lipgloss.NewStyle()
	got := styledRuns([]rune("abc"), []emphLevel{emphNone, emphSel, emphSelCur}, nil, base)
	wantB := s.selectionStyle(base).Render("b")
	wantC := s.currentHitStyle(s.selectionStyle(base)).Render("c")
	if !strings.Contains(got, wantB) || !strings.Contains(got, wantC) || strings.HasPrefix(got, "\x1b") {
		t.Fatalf("got %q\nwant a plain a, then %q, then %q", got, wantB, wantC)
	}
	emph := overlayHits(nil, 0, 4, []hitSpan{{start: 1, end: 4, sel: true}, {start: 2, end: 3, sel: true, cur: true}})
	if !equalEmph(emph, []emphLevel{emphNone, emphSel, emphSelCur, emphSel}) {
		t.Fatalf("overlay = %v", emph)
	}
	if emph := overlayHits(nil, 0, 2, []hitSpan{{start: 0, end: 2, cur: true}}); !equalEmph(emph, []emphLevel{emphCur, emphCur}) {
		t.Fatalf("a search hit still maps to emphCur: %v", emph)
	}
}
```

(`activeTheme()` / `setTheme(theme.Terminal)` are what `TestDiffSelectionUnderReverseDropsSyntaxColours` uses; `theme.Dark` and `theme.Terminal` are package vars.)

- [ ] **Step 2: Run it to verify it fails**

Run: `cd /work/gigagit/.claude/worktrees/char-select && go test ./internal/tui/ -run TestCharSelLevelsPaintStripeAndCursor 2>&1 | tail -4`
Expected: FAIL — `b` and `c` render plain (the levels fall into `styledRuns`'s default arm) and `overlay` shows `emphHit`/`emphCur`.

- [ ] **Step 3: Implement**

`internal/tui/textsearch.go`, in `overlayHits`, replace

```go
		lvl := emphHit
		if h.cur {
			lvl = emphCur
		}
```

with

```go
		lvl := emphHit
		switch {
		case h.sel && h.cur:
			lvl = emphSelCur
		case h.sel:
			lvl = emphSel
		case h.cur:
			lvl = emphCur
		}
```

`internal/tui/diff_render.go`, in `styledRuns`'s `switch emph[i]`, add before `case emphCur:`:

```go
		case emphSelCur: // the character selection's cursor: visible inside the stripe
			b.WriteString(s.currentHitStyle(s.selectionStyle(base)).Render(seg))
		case emphSel: // the character selection's stripe (styles.selectionStyle)
			b.WriteString(s.selectionStyle(base).Render(seg))
```

and extend the function's doc comment with one sentence: "A character selection (charsel.go) rides the same mask: emphSel wears the selection stripe, emphSelCur the current-hit style over it, so the cursor stays visible inside the range."

- [ ] **Step 4: Run the test and the renderer's tests**

Run: `cd /work/gigagit/.claude/worktrees/char-select && go test ./internal/tui/ -run 'TestCharSel|TestDiffSelection|TestStyledRuns|TestRenderWindow' 2>&1 | tail -3`
Expected: PASS.

- [ ] **Step 5: Commit**

Message: `tui(charsel): emphSel / emphSelCur paint the stripe and the cursor through styledRuns`.

---

### Task 3: The Summary popup

**Files:**
- Modify: `internal/tui/window.go` (`wrapSegOffsets`)
- Modify: `internal/tui/content_popup.go` (fields, `layoutWidths`, `charLayout`, `charRows`, `charPage`, `charEmphFor`, `charFollow`, the keys/hint swap, `pageH`)
- Modify: `internal/tui/review_view.go` (`reviewSummaryPopup.update`, the keys line)
- Test: `internal/tui/review_charsel_test.go` (new), `internal/tui/window_test.go` (`wrapSegOffsets`)

**Interfaces:**
- Consumes: Task 1's model and `charSelEmph`/`shiftMask`; Task 2's painting.
- Produces: `contentPopup` implements `charHost`; `func (p *contentPopup) charEnter(m Model, at int) bool`; `func (p *contentPopup) charKey(m Model, msg tea.KeyMsg) (Model, tea.Cmd, bool)`; `wrapSegOffsets(text string, segs []string, indent int) (offs, pads []int)`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/tui/window_test.go` (create the file if absent, package `tui`):

```go
// wrapSegOffsets is the inverse of wrapSegMask's mapping: where each display
// segment's TEXT starts in the logical row, and how many leading layout
// spaces it carries.
func TestWrapSegOffsets(t *testing.T) {
	t.Parallel()
	text := "- one two three"
	segs, indent := wrapRow(text, 9, winOpts{}, 0)
	if len(segs) < 2 {
		t.Fatalf("segs = %q, want a wrap", segs)
	}
	offs, pads := wrapSegOffsets(text, segs, indent)
	if offs[0] != 0 || pads[0] != 0 {
		t.Fatalf("first = %d/%d", offs[0], pads[0])
	}
	// The second segment's text starts where the first's ended (the break's
	// space dropped) and sits behind the hang indent.
	first := len([]rune(segs[0]))
	if offs[1] != first+1 || pads[1] != indent {
		t.Fatalf("second = off %d pad %d, want off %d pad %d (segs %q)", offs[1], pads[1], first+1, indent, segs)
	}
}
```

Create `internal/tui/review_charsel_test.go`:

```go
package tui

import (
	"strings"
	"testing"
)

const charSelReviewDoc = `{"version":1,"summary":"## Overview\nLooks fine, see A.\n\n` +
	`This paragraph is long enough to wrap inside the popup's reading column several times over, ` +
	`because the character selection must be tested across a wrapped row and not only on short ones.",` +
	`"meta":{"verdict":"approve"},"files":[{"path":"a.go","annotations":[{"newRange":[1,1],"summary":"adds A"}]}]}`

// openedSummaryPopup is the review view with its Summary popup on top.
func openedSummaryPopup(t *testing.T) (Model, *reviewSummaryPopup) {
	t.Helper()
	m, id := reviewViewModel(t, charSelReviewDoc)
	m, cmd := m.openReview(id, "Review")
	m = drainCmds(t, m, cmd)
	m, _ = m.openReviewSummary()
	p, ok := m.topLayer().(*reviewSummaryPopup)
	if !ok {
		t.Fatalf("top layer %T, want the summary popup", m.topLayer())
	}
	return m, p
}

func feedKeys(m Model, keys ...string) Model {
	for _, k := range keys {
		u, _ := m.Update(keyMsg(k))
		m = u.(Model)
	}
	return m
}

// v / l l l / space / … / enter copies the rendered text, not the markdown:
// the heading row reads "Overview", never "## Overview".
func TestSummaryCharSelCopiesRenderedText(t *testing.T) {
	t.Parallel()
	m, p := openedSummaryPopup(t)
	var got string
	m = captureClip(m, &got)
	m = feedKeys(m, "v")
	if !p.cs.on || p.cs.cur != (pos{0, 0}) {
		t.Fatalf("v: %+v", p.cs)
	}
	m = feedKeys(m, "space", "l", "l", "l")
	u, cmd := m.Update(keyMsg("enter"))
	m = drainCmds(t, u.(Model), cmd)
	if got != "Over" || p.cs.on {
		t.Fatalf("copied %q (on=%v), want Over", got, p.cs.on)
	}
	if !strings.Contains(m.statusMsg, "Copied 4 characters") {
		t.Fatalf("status = %q", m.statusMsg)
	}
}

// A selection across a wrapped row copies the paragraph joined with one
// space; the popup's own keys (y, o, /) are inert while the mode is on.
func TestSummaryCharSelJoinsAWrappedRowAndIsModal(t *testing.T) {
	t.Parallel()
	m, p := openedSummaryPopup(t)
	var got string
	m = captureClip(m, &got)
	m = feedKeys(m, "v")
	rows := p.charRows()
	wrapAt := -1
	for i, r := range rows {
		if r.wraps {
			wrapAt = i
			break
		}
	}
	if wrapAt < 1 {
		t.Fatalf("no wrapped row among %d rows", len(rows))
	}
	for p.cs.cur.row < wrapAt-1 {
		m = feedKeys(m, "j")
	}
	m = feedKeys(m, "home", "space", "j", "l", "l")
	before := got
	m = feedKeys(m, "y") // y in the mode COPIES the selection (not the markdown)
	if got == before || strings.Contains(got, "##") {
		t.Fatalf("y copied %q", got)
	}
	want := string(rows[wrapAt-1].text) + " " + string(rows[wrapAt].text[:3])
	if got != want {
		t.Fatalf("copied %q\nwant   %q", got, want)
	}
	m = feedKeys(m, "v", "o", "/")
	if !p.cs.on || p.typing || m.topLayer() != p {
		t.Fatalf("o and / must be inert in the mode: on=%v typing=%v top=%T", p.cs.on, p.typing, m.topLayer())
	}
}

// esc drops the start, then leaves; the popup stays open.
func TestSummaryCharSelEscStages(t *testing.T) {
	t.Parallel()
	m, p := openedSummaryPopup(t)
	m = feedKeys(m, "v", "space", "l", "esc")
	if !p.cs.on || p.cs.fixed {
		t.Fatalf("first esc: %+v", p.cs)
	}
	m = feedKeys(m, "esc")
	if p.cs.on || m.topLayer() != p {
		t.Fatalf("second esc leaves the mode, not the popup: on=%v top=%T", p.cs.on, m.topLayer())
	}
}

// Review Focus 3: a relayout (ctrl+t) while the mode is on leaves it.
func TestSummaryCharSelLeavesOnRelayout(t *testing.T) {
	t.Parallel()
	m, p := openedSummaryPopup(t)
	m = feedKeys(m, "v", "space", "l")
	p.maximized = !p.maximized // what ctrl+t does to the box width
	_ = m.View()
	if p.cs.on {
		t.Fatal("a width change must leave the mode (the rows re-wrapped)")
	}
}

// The painted popup shows the stripe on the covered runes: the rendered
// view carries the selection style's escape before "ver" and the hint names
// the stage.
func TestSummaryCharSelPaintsAndHints(t *testing.T) {
	t.Parallel()
	m, p := openedSummaryPopup(t)
	m = feedKeys(m, "v")
	view := m.View()
	if !strings.Contains(view, "[space] start") {
		t.Fatalf("hint missing:\n%s", view)
	}
	m = feedKeys(m, "space", "l", "l")
	if !strings.Contains(m.View(), "[enter y] copy 3 chars") {
		t.Fatalf("fixed hint missing:\n%s", m.View())
	}
	if mask := p.charEmphFor(0, len([]rune("  Overview"))); mask == nil || mask[2] != emphSel || mask[4] != emphSelCur {
		t.Fatalf("row 0 mask = %v", mask)
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `cd /work/gigagit/.claude/worktrees/char-select && go test ./internal/tui/ -run 'TestWrapSegOffsets|TestSummaryCharSel' 2>&1 | tail -5`
Expected: build failure — `undefined: wrapSegOffsets`, `p.cs`, `p.charRows`.

- [ ] **Step 3: `wrapSegOffsets` (window.go, after `wrapSegMask`)**

```go
// wrapSegOffsets is wrapSegMask's mapping the other way round: for each
// display segment of a wrapped row, where its TEXT starts in the logical
// row's runes (the break's dropped spaces skipped) and how many leading
// layout spaces it carries (the hang indent on a continuation). The same
// try-indent-then-zero rule as wrapSegMask, so the two never disagree; a
// layout it cannot explain (never, by construction) answers consecutive
// offsets with no pads.
func wrapSegOffsets(text string, segs []string, indent int) (offs, pads []int) {
	try := func(pad int) ([]int, []int) {
		offs := make([]int, len(segs))
		pads := make([]int, len(segs))
		rest := []rune(text)
		off := 0
		for i, s := range segs {
			r := []rune(s)
			p := 0
			if i > 0 {
				p = pad
			}
			if len(r) < p {
				return nil, nil
			}
			for _, c := range r[:p] {
				if c != ' ' {
					return nil, nil
				}
			}
			body := r[p:]
			// The break dropped the spaces it fell on: skip them in the source.
			for off < len(rest) && rest[off] == ' ' && (len(body) == 0 || body[0] != ' ') {
				off++
			}
			if off+len(body) > len(rest) || string(rest[off:off+len(body)]) != string(body) {
				return nil, nil
			}
			offs[i], pads[i] = off, p
			off += len(body)
		}
		return offs, pads
	}
	if indent > 0 {
		if o, p := try(indent); o != nil {
			return o, p
		}
	}
	if o, p := try(0); o != nil {
		return o, p
	}
	offs = make([]int, len(segs))
	pads = make([]int, len(segs))
	off := 0
	for i, s := range segs {
		offs[i] = off
		off += len([]rune(s))
	}
	return offs, pads
}
```

- [ ] **Step 4: The popup adapter (content_popup.go)**

Fields, after `lsel lineSel`:

```go
	// cs is the character selection (charsel.go, spec 2026-10-10): a modal
	// copy mode over the DISPLAY rows the box draws. csRows / csSeg are its
	// layout, rebuilt on v for the box's current width (csW) and dropped —
	// with the mode — when the width changes (the rows re-wrap, so a
	// position would name other runes). pageH is the pager height the last
	// render used (pgup/pgdn, keeping the cursor on screen).
	cs     charSel
	csRows []charRow
	csSeg  []charSeg
	csW    int
	pageH  int
```

and the segment record (same file, above the struct):

```go
// charSeg maps one of the character selection's display rows back to the
// box's logical line: the line's index in visible(), the rune offset of the
// row's text in that line's winRow text, and the layout runes (lead, hang
// indent) before it.
type charSeg struct{ line, off, pad int }
```

Extract the width math from `box()` into:

```go
// layoutWidths is the box's width arithmetic — the inner box, the text
// width, the prose margin, block mode's gutter and the body width rows are
// laid out at — shared by the render and the character selection's layout.
func (p *contentPopup) layoutWidths(m Model) (inner, textW, margin, gutter, bodyW int) {
	w, _ := m.overlayDims()
	inner = popupResolveWidth(w, p.maximized, contentPopupWidth(w))
	if p.fitContent {
		inner = popupFitWidth(w, p.maximized, contentPopupWidth(w), p.widestLine())
	}
	textW = inner - st().modalStyle.GetHorizontalPadding()
	if p.prose {
		textW, margin = readingColumn(textW, m.readingWidth())
	}
	if p.block {
		gutter = messageBlockGutter
	}
	bodyW = textW - 2*gutter
	return inner, textW, margin, gutter, bodyW
}
```

and in `box()` replace the lines from `w, _ := m.overlayDims()` through `bodyW := textW - 2*gutter` with:

```go
	inner, textW, margin, gutter, bodyW := p.layoutWidths(m)
	s := st()
	pad := strings.Repeat(" ", gutter)
```

(`w` is no longer needed there.) Then the adapter:

```go
// charLead is the layout runes a non-block row's text sits behind ("  ", or
// "> " on the cursor row): the selection never covers them.
func (p *contentPopup) charLead() int {
	if p.block {
		return 0
	}
	return 2
}

// charLayout rebuilds the character selection's rows for the box's current
// width: in wrap mode one row per display segment (wrapRow, the layout
// renderWindow uses), with the lead and the hang indent stripped and marked
// as the segment's pad; in the other modes one row per line.
func (p *contentPopup) charLayout(m Model) {
	_, _, _, _, bodyW := p.layoutWidths(m)
	p.csW = bodyW
	p.csRows, p.csSeg = nil, nil
	lead := p.charLead()
	o := winOpts{w: bodyW, mode: p.mode, charWrap: p.charWrap}
	for i, l := range p.visible() {
		text := strings.Repeat(" ", lead) + l.text
		if p.mode != modeWrap || l.noWrap {
			r := []rune(text)
			p.csRows = append(p.csRows, charRow{text: r[min(lead, len(r)):]})
			p.csSeg = append(p.csSeg, charSeg{line: i, off: lead, pad: lead})
			continue
		}
		segs, indent := wrapRow(text, bodyW, o, 0)
		offs, pads := wrapSegOffsets(text, segs, indent)
		for si, s := range segs {
			r := []rune(s)
			pad := pads[si]
			if si == 0 {
				pad = min(lead, len(r))
			}
			p.csRows = append(p.csRows, charRow{text: r[pad:], wraps: si > 0})
			p.csSeg = append(p.csSeg, charSeg{line: i, off: offs[si] + (pad - pads[si]), pad: pad})
		}
	}
}

func (p *contentPopup) charRows() []charRow { return p.csRows }
func (p *contentPopup) charPage() int       { return max(p.pageH, 1) }

// charEnter turns the mode on at display row `at` (the pager's top line);
// false when there is nothing to select.
func (p *contentPopup) charEnter(m Model, at int) bool {
	p.charLayout(m)
	return p.cs.enter(p.csRows, pos{row: at})
}

// charFollow keeps the cursor's row on screen: the pager's top (sel) moves
// the least it must.
func (p *contentPopup) charFollow() {
	h := p.charPage()
	if p.cs.cur.row < p.sel {
		p.sel = p.cs.cur.row
	}
	if p.cs.cur.row >= p.sel+h {
		p.sel = p.cs.cur.row - h + 1
	}
}

// charEmphFor is line i's emphasis mask (n = its winRow text's runes): the
// selection of every display row that belongs to it, placed behind each
// row's pad. nil when the mode is off or the line is untouched.
func (p *contentPopup) charEmphFor(i, n int) []emphLevel {
	if !p.cs.on {
		return nil
	}
	var out []emphLevel
	for k, seg := range p.csSeg {
		if seg.line != i {
			continue
		}
		m := charSelEmph(p.cs, k, len(p.csRows[k].text))
		if m == nil {
			continue
		}
		if out == nil {
			out = make([]emphLevel, n)
		}
		for j, l := range m {
			if at := seg.off + j; at >= 0 && at < n && l != emphNone {
				out[at] = l
			}
		}
	}
	return out
}

// charKey routes a key to the mode while it is on; handled says the host
// must return. The copy goes to the clipboard with its count on the status
// line; a notice goes to the status line too.
func (p *contentPopup) charKey(m Model, msg tea.KeyMsg) (Model, tea.Cmd, bool) {
	if !p.cs.on {
		return m, nil, false
	}
	res := charSelKey(&p.cs, p, msg)
	if !res.handled {
		return m, nil, false
	}
	p.charFollow()
	if res.notice != "" {
		m.statusMsg = res.notice
	}
	if res.copy != "" {
		return m, m.copyToClipboardCmd(copiedCharsText(len([]rune(res.copy))), res.copy), true
	}
	return m, nil, true
}
```

In `box()`, after the `for i, l := range vis { wr[i].noWrap = … }` loop, paint and guard the width:

```go
	if p.cs.on && p.csW != bodyW {
		p.cs.leave() // the rows re-wrapped (a resize, ctrl+t): positions mean other runes
	}
	if p.cs.on {
		for i := range wr {
			wr[i].emph = p.charEmphFor(i, len([]rune(wr[i].text)))
		}
	}
```

Store the pager height: after `win, total = p.pagerWindow(wr, o, capRows)` add `p.pageH = len(win)`; after `win = renderWindow(wr, o)` add `p.pageH = o.h`. The keys line and the hint while on — replace

```go
	if p.keys != "" {
		if !p.block && p.saved == "" {
			b.WriteString("\n")
		}
		b.WriteString(pad + truncate(p.keys, textW-gutter) + "\n")
	}
```

with

```go
	keys := p.keys
	if p.cs.on {
		keys = charSelHint(p.cs, p.csRows)
	}
	if keys != "" {
		if !p.block && p.saved == "" {
			b.WriteString("\n")
		}
		b.WriteString(pad + truncate(keys, textW-gutter) + "\n")
	}
```

and the hint line: `hint := i18n.T("[/] search  [ctrl+w] mode  [s] save  [ctrl+t] full  [q] close")` becomes

```go
	hint := i18n.T("[/] search  [ctrl+w] mode  [s] save  [ctrl+t] full  [q] close")
	if p.cs.on {
		hint = i18n.T("[esc] leave the selection")
	}
```

(`hintGap()` and `extra` already count the keys line when `p.keys != ""`; the summary popup always has keys, so the budget is unchanged.)

- [ ] **Step 5: Route the keys in `reviewSummaryPopup.update` (review_view.go)**

```go
func (p *reviewSummaryPopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if nm, cmd, handled := p.charKey(m, msg); handled { // the mode owns every key (C5)
		return nm, cmd
	}
	if !p.typing { // while the / filter is capturing, every key is query text
		switch msg.String() {
		case "v":
			if !p.charEnter(m, p.sel) {
				m.statusMsg = i18n.T("▸ nothing to select")
			}
			return m, nil
		case "y":
			return m, m.copyToClipboardCmd(i18n.T("copied the review summary"), p.st.review.Doc.Summary)
		…
```

(the rest unchanged). The keys line in `openReviewSummary`: `cp.keys = i18n.T("[y] copy  [v] select")`.

Bundles (append):

ja: `"[y] copy  [v] select" = "[y] コピー  [v] 選択"`, `"▸ nothing to select" = "▸ 選択できるものがありません"`, `"[esc] leave the selection" = "[esc] 選択を終了"`
ko: `"[y] copy  [v] select" = "[y] 복사  [v] 선택"`, `"▸ nothing to select" = "▸ 선택할 것이 없습니다"`, `"[esc] leave the selection" = "[esc] 선택 나가기"`
zh: `"[y] copy  [v] select" = "[y] 复制  [v] 选择"`, `"▸ nothing to select" = "▸ 没有可选择的内容"`, `"[esc] leave the selection" = "[esc] 退出选择"`
ru: `"[y] copy  [v] select" = "[y] копировать  [v] выделить"`, `"▸ nothing to select" = "▸ нечего выделять"`, `"[esc] leave the selection" = "[esc] выйти из выделения"`

- [ ] **Step 6: Run the tests**

Run: `cd /work/gigagit/.claude/worktrees/char-select && gofmt -l internal/tui/ && go test ./internal/tui/ -run 'TestWrapSegOffsets|TestSummaryCharSel|TestReview|TestContentPopup|TestI18n' 2>&1 | tail -4`
Expected: PASS. If `TestSummaryCharSelJoinsAWrappedRow` finds no wrapped row, the popup's reading column is wider than the paragraph: lengthen `charSelReviewDoc`'s paragraph (never shrink the column).

- [ ] **Step 7: Commit**

Message: `tui(review): v selects characters of the Summary popup — the wrapped rows as on screen, the stripe and cursor, the keys line`.

---

### Task 4: The single diff view

**Files:**
- Create: `internal/tui/diff_charsel.go`
- Modify: `internal/tui/diff_view.go` (fields; the hook's place in `updateDiffViewKey`; the mode leaves where `lsel.clear()` runs)
- Modify: `internal/tui/diff_render.go` (`renderDiffView`: the spans per row, the hint)
- Test: `internal/tui/diff_charsel_test.go` (new)

**Interfaces:**
- Consumes: Task 1's model, `charSelSpans`; Task 2's painting; `v.setCursorLine(li, body)`, `m.diffBodyRows()`, `sidePresent`, `diffLine.isBody`, `v.fileLineRange(i)`, `v.lines[li].file`, `v.stk.files[i]`.
- Produces: `diffView.cs charSel`, `csBase, csFile, csPage int`; `func (v *diffView) charRows() []charRow`, `charPage() int`, `charSpansOn(li int) []hitSpan`, `charEmphProse(li, n int) []emphLevel`; `func (m Model) diffCharKey(v *diffView, msg tea.KeyMsg) (Model, tea.Cmd, bool)`; `func dispCol(text string, col int) int`. Task 5 uses `csBase`/`csFile` for the stack's element.

- [ ] **Step 1: Write the failing tests**

Create `internal/tui/diff_charsel_test.go`:

```go
package tui

import (
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/textdiff"
)

// textRows is n Same rows whose both sides read the given lines (cycled).
func textRows(lines ...string) []textdiff.Row {
	out := make([]textdiff.Row, len(lines))
	for i, l := range lines {
		out[i] = textdiff.Row{Kind: textdiff.Same, Left: l, Right: l, LeftNo: i + 1, RightNo: i + 1}
	}
	return out
}

// v / l l / space / j / end / enter copies the cursor side's text from the
// start column to the end column across the lines, joined with newlines.
func TestDiffCharSelCopiesAcrossLines(t *testing.T) {
	t.Parallel()
	m := openedDiffModel(12, textRows("hello world", "second line", "third"), nil)
	v := m.diffLayer()
	v.setCursorLine(0, m.diffBodyRows())
	var got string
	m = captureClip(m, &got)
	m = feedDiff(m, "v")
	if !v.cs.on || v.cs.cur != (pos{0, 0}) {
		t.Fatalf("v: %+v", v.cs)
	}
	m = feedDiff(m, "l", "l", "space", "j", "end")
	if v.curLine != 1 {
		t.Fatalf("the line cursor must follow the character cursor's row: curLine = %d", v.curLine)
	}
	u, cmd := m.Update(keyMsg("enter"))
	m = drainCmds(t, u.(Model), cmd)
	if got != "llo world\nsecond line" || v.cs.on {
		t.Fatalf("copied %q (on=%v)", got, v.cs.on)
	}
}

// Review Focus 2: a tab paints its expanded cells and copies as a tab.
func TestDiffCharSelTabsPaintExpandedAndCopyRaw(t *testing.T) {
	t.Parallel()
	m := openedDiffModel(12, textRows("a\tb", "x"), nil)
	v := m.diffLayer()
	v.setCursorLine(0, m.diffBodyRows())
	var got string
	m = captureClip(m, &got)
	m = feedDiff(m, "v", "space", "l", "l") // a, the tab, b
	sp := v.charSpansOn(0)
	if len(sp) != 2 || sp[0].start != 0 || sp[0].end != 5 || sp[1].start != 4 || sp[1].end != 5 {
		t.Fatalf("spans = %+v, want the stripe over a + 3 tab cells + b (0..5) and the cursor on b (4..5)", sp)
	}
	u, cmd := m.Update(keyMsg("enter"))
	drainCmds(t, u.(Model), cmd)
	if got != "a\tb" {
		t.Fatalf("copied %q, want the raw tab", got)
	}
	if dispCol("\tx", 1) != 4 || dispCol("ab", 2) != 2 || dispCol("a\tb", 2) != 4 {
		t.Fatalf("dispCol: %d %d %d", dispCol("\tx", 1), dispCol("ab", 2), dispCol("a\tb", 2))
	}
}

// The mode is modal in the diff: alt+←/→ (the side), n/p, / and S are
// inert; v clears a live line selection (one selection kind at a time).
func TestDiffCharSelIsModalAndClearsTheLineSelection(t *testing.T) {
	t.Parallel()
	m := openedDiffModel(12, textRows("one", "two", "three"), nil)
	v := m.diffLayer()
	v.setCursorLine(0, m.diffBodyRows())
	m = feedDiff(m, "space") // a line selection
	if !v.lsel.on {
		t.Fatal("space must start a line selection")
	}
	m = feedDiff(m, "v")
	if v.lsel.on || !v.cs.on {
		t.Fatalf("v must clear the line selection and enter the mode: lsel=%+v cs=%+v", v.lsel, v.cs)
	}
	m = feedDiff(m, "alt+left", "n", "/", "S")
	if v.onOld || !v.cs.on || v.search.typing || v.stk != nil {
		t.Fatalf("keys leaked through the mode: onOld=%v on=%v typing=%v stacked=%v", v.onOld, v.cs.on, v.search.typing, v.stk != nil)
	}
	if hint := strings.Join(strings.Fields(m.View()), " "); !strings.Contains(hint, "[space] start") {
		t.Fatalf("the footer must show the mode's keys:\n%s", m.View())
	}
}

// The cursor side's absent cell is dead: on an Add row the old side has no
// text, so a selection on the old side steps over it and copies nothing of it.
func TestDiffCharSelSkipsAbsentCellsOnTheCursorSide(t *testing.T) {
	t.Parallel()
	rows := []textdiff.Row{
		{Kind: textdiff.Same, Left: "keep", Right: "keep", LeftNo: 1, RightNo: 1},
		{Kind: textdiff.Add, Right: "added", RightNo: 2},
		{Kind: textdiff.Same, Left: "tail", Right: "tail", LeftNo: 2, RightNo: 3},
	}
	m := openedDiffModel(12, rows, nil)
	v := m.diffLayer()
	v.setCursorLine(0, m.diffBodyRows())
	m = feedDiff(m, "alt+left") // the old side
	var got string
	m = captureClip(m, &got)
	m = feedDiff(m, "v", "space", "j", "end")
	if v.cs.cur.row != 2 {
		t.Fatalf("j must step over the absent old cell: row %d", v.cs.cur.row)
	}
	u, cmd := m.Update(keyMsg("enter"))
	drainCmds(t, u.(Model), cmd)
	if got != "keep\ntail" {
		t.Fatalf("copied %q", got)
	}
}

// Review Focus 5: a rebuild (f, the partial toggle) leaves the mode.
func TestDiffCharSelLeavesOnRebuild(t *testing.T) {
	t.Parallel()
	m := openedDiffModel(12, textRows("one", "two", "three"), nil)
	v := m.diffLayer()
	m = feedDiff(m, "v", "space", "l")
	v.rebuild()
	if v.cs.on {
		t.Fatal("a rebuild must leave the mode")
	}
}

// Nothing to select: a loading view consumes v and says so.
func TestDiffCharSelOnALoadingViewSaysSo(t *testing.T) {
	t.Parallel()
	m := openedDiffModel(12, nil, nil)
	v := m.diffLayer()
	v.loading = true
	m = feedDiff(m, "v")
	if v.cs.on || m.diffNotice == "" {
		t.Fatalf("on=%v notice=%q", v.cs.on, m.diffNotice)
	}
}
```

(`func (v *diffView) rebuild()` exists at `diff_view.go:226`.)

- [ ] **Step 2: Run them to verify they fail**

Run: `cd /work/gigagit/.claude/worktrees/char-select && go test ./internal/tui/ -run 'TestDiffCharSel' 2>&1 | tail -5`
Expected: build failure — `v.cs undefined`.

- [ ] **Step 3: Fields (diff_view.go), after `selKeptAt lineSel`**

```go
	// cs is the character selection (charsel.go, spec 2026-10-10) over the
	// CURSOR SIDE's lines; stacked, over ONE element's lines (csFile, its
	// first line csBase — the model's row 0). csPage is the body height the
	// last key saw (pgup/pgdn). Left with the line selection wherever the
	// line indexes change meaning (rebuild, relayout, a reload).
	cs     charSel
	csBase int
	csFile int
	csPage int
```

Wherever `diff_view.go` runs `v.lsel.clear()` (the two in `rebuild`, the ones at ~1336 and ~1352), add `v.cs.leave()` on the next line. In `diff_stack.go`'s re-splice (`v.lsel.clear()` at ~487, before `v.spliceStack()`), add `v.cs.leave()` too (a re-splice moves the element's lines; the user presses `v` again).

- [ ] **Step 4: The adapter and the hook (`diff_charsel.go`, new)**

```go
package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/i18n"
)

// The diff view as a character-selection host (charsel.go): the rows are
// the cursor side's lines — stacked, one element's — a row dead where the
// side has no cell or the line is a fold, a header, a gap. Column math is
// in SOURCE runes (a tab is one rune, copied as a tab); the painter maps a
// column to the sanitized cell through dispCol.

// charRange is the lines the mode works in: the whole stream for a single
// file, the element (csFile) for a stack.
func (v *diffView) charRange() (lo, hi int) {
	if v.stk == nil {
		return 0, len(v.lines) - 1
	}
	return v.fileLineRange(v.csFile)
}

func (v *diffView) charRows() []charRow {
	lo, hi := v.charRange()
	if lo < 0 || hi < lo || hi >= len(v.lines) {
		return nil
	}
	out := make([]charRow, 0, hi-lo+1)
	for li := lo; li <= hi; li++ {
		ln := v.lines[li]
		switch {
		case ln.kind == lineProse: // the stack's summary element (Task 5)
			if f, ok := v.stackFileAt(li); ok && ln.prose >= 0 && ln.prose < len(f.prose) {
				out = append(out, charRow{text: []rune(f.prose[ln.prose].text), wraps: false}) // Task 5: mdRow.cont
				continue
			}
			out = append(out, charRow{dead: true})
		case ln.isBody() && sidePresent(ln.Row, v.onOld):
			if v.onOld {
				out = append(out, charRow{text: []rune(ln.Row.Left)})
			} else {
				out = append(out, charRow{text: []rune(ln.Row.Right)})
			}
		default:
			out = append(out, charRow{dead: true})
		}
	}
	return out
}

func (v *diffView) charPage() int { return max(v.csPage, 1) }

// dispCol is the sanitized-cell rune index of source column col of text:
// the same expansion sanitizeCell applies (a tab to the next 4-column stop,
// a control rune to one cell, every other rune to one).
func dispCol(text string, col int) int {
	idx, c := 0, 0
	for i, r := range []rune(text) {
		if i >= col {
			break
		}
		switch {
		case r == '\t':
			n := 4 - c%4
			idx += n
			c += n
		default:
			idx++
			c++
		}
	}
	return idx
}

// charSpansOn is the selection on logical line li as spans in the sanitized
// cell's rune space (what the cell painters overlay); nil when the mode is
// off or the line is outside its element.
func (v *diffView) charSpansOn(li int) []hitSpan {
	if !v.cs.on {
		return nil
	}
	lo, hi := v.charRange()
	if li < lo || li > hi {
		return nil
	}
	ln := v.lines[li]
	if !ln.isBody() || !sidePresent(ln.Row, v.onOld) {
		return nil
	}
	text := ln.Row.Right
	if v.onOld {
		text = ln.Row.Left
	}
	spans := charSelSpans(v.cs, li-lo)
	for i := range spans {
		spans[i].start = dispCol(text, spans[i].start)
		if spans[i].end < 1<<29 {
			spans[i].end = dispCol(text, spans[i].end)
		}
	}
	return spans
}

// charEmphProse is the mask of a stack's prose row (Task 5 paints it).
func (v *diffView) charEmphProse(li, n int) []emphLevel {
	if !v.cs.on {
		return nil
	}
	lo, hi := v.charRange()
	if li < lo || li > hi {
		return nil
	}
	return charSelEmph(v.cs, li-lo, n)
}

// diffCharEnter is v: the mode on at the cursor line, column 0 — stacked,
// within the cursor's element. false with nothing to select.
func (m Model) diffCharEnter(v *diffView) bool {
	v.lsel.clear() // one selection kind at a time
	v.csBase, v.csFile = 0, 0
	if v.stk != nil {
		v.csFile = v.lines[v.curLine].file
		v.csBase, _ = v.fileLineRange(v.csFile)
	}
	v.csPage = m.diffBodyRows()
	return v.cs.enter(v.charRows(), pos{row: v.curLine - v.csBase})
}

// diffCharKey gives the character selection every key while it is on, and
// v when it is off. It runs BEFORE the line selection and the search hooks
// (updateDiffViewKey): the mode is modal, nothing else may see a key.
// handled == true means the caller must return immediately.
func (m Model) diffCharKey(v *diffView, msg tea.KeyMsg) (Model, tea.Cmd, bool) {
	if v == nil {
		return m, nil, false
	}
	if !v.cs.on {
		if msg.String() != "v" || v.search.typing {
			return m, nil, false
		}
		if v.loading || v.err != nil || v.binary || v.tooLarge || len(v.lines) == 0 {
			m.diffNotice = i18n.T("▸ nothing to select")
			return m, nil, true
		}
		if !m.diffCharEnter(v) {
			m.diffNotice = i18n.T("▸ nothing to select")
		}
		return m, nil, true
	}
	v.csPage = m.diffBodyRows()
	res := charSelKey(&v.cs, v, msg)
	if !res.handled {
		return m, nil, false
	}
	if v.cs.on {
		v.setCursorLine(v.csBase+v.cs.cur.row, m.diffBodyRows()) // the view scrolls with the cursor
	}
	if res.notice != "" {
		m.diffNotice = res.notice
	}
	if res.bump && v.stk != nil {
		m.diffNotice = m.stackCharBoundNotice(v)
	}
	if res.copy != "" {
		return m, m.copyToClipboardCmd(copiedCharsText(len([]rune(res.copy))), res.copy), true
	}
	return m, nil, true
}

// stackCharBoundNotice says what bounds the selection in a stack: the
// element (the summary, a shelved set's note) or the file.
func (m Model) stackCharBoundNotice(v *diffView) string {
	if v.stk != nil && v.csFile >= 0 && v.csFile < len(v.stk.files) {
		if f := v.stk.files[v.csFile]; f.summary || f.label != "" {
			return i18n.T("▸ the selection stays in this element")
		}
	}
	return i18n.T("▸ the selection stays in this file")
}
```

`mdRow.cont` does not exist until Task 5, hence the `wraps: false` with its comment; Task 5 replaces it. The two stack notices `stackCharBoundNotice` uses need their bundle entries now (append):

ja: `"▸ the selection stays in this element" = "▸ 選択はこの要素内にとどまります"`, `"▸ the selection stays in this file" = "▸ 選択はこのファイル内にとどまります"`
ko: `"▸ the selection stays in this element" = "▸ 선택은 이 요소 안에 머뭅니다"`, `"▸ the selection stays in this file" = "▸ 선택은 이 파일 안에 머뭅니다"`
zh: `"▸ the selection stays in this element" = "▸ 选择范围限于此元素"`, `"▸ the selection stays in this file" = "▸ 选择范围限于此文件"`
ru: `"▸ the selection stays in this element" = "▸ выделение остаётся в этом элементе"`, `"▸ the selection stays in this file" = "▸ выделение остаётся в этом файле"`

- [ ] **Step 5: Wire the hook and the painting**

`diff_view.go` `updateDiffViewKey`, before the `diffSelectKey` call (after `m.diffNotice = ""` and the arms are captured — the hook sets its own notice):

```go
	// The character selection (charsel.go) is MODAL: while it is on, every
	// key is its own, before the line selection and the search hooks.
	if nm, cmd, handled := m.diffCharKey(v, msg); handled {
		return nm, cmd
	}
```

`diff_render.go` `renderDiffView`, in the row loop right after the `if v.search.active() { … }` block that fills `lh, rh`:

```go
		// The character selection's stripe and cursor ride the hit channel
		// (sel spans), on the cursor side only.
		if sp := v.charSpansOn(dr.line); sp != nil {
			if v.onOld {
				lh = append(lh, sp...)
			} else {
				rh = append(rh, sp...)
			}
		}
```

and the hint, after `if v.lsel.on { hint = diffSelectHint() }`:

```go
	if v.cs.on {
		hint = charSelHint(v.cs, v.charRows())
	}
```

Check `charSpansOn`'s `end` clamp: `overlayHits` clamps `hi` to `n`, so the `1<<30` "whole row" span is safe; `dispCol` is only applied to a real end.

- [ ] **Step 6: Run the tests**

Run: `cd /work/gigagit/.claude/worktrees/char-select && gofmt -l internal/tui/ && go vet ./internal/tui/ && go test ./internal/tui/ -run 'TestDiffCharSel|TestDiffSelection|TestDiffView|TestI18n' 2>&1 | tail -4`
Expected: PASS.

- [ ] **Step 7: Commit**

Also add `internal/i18n/lang/*.toml` to the commit. Message: `tui(diff): v selects characters on the cursor side — spans through the hit channel, tabs expanded on screen and raw in the copy, the mode leaves on a rebuild`.

---

### Task 5: The stacked review view

**Files:**
- Modify: `internal/tui/md_render.go` (`mdRow.cont`, set in `mdWrap`, carried by `mdPrefix`)
- Modify: `internal/tui/diff_charsel.go` (the prose arm's `wraps`)
- Modify: `internal/tui/diff_stack_render.go` (the prose row's mask)
- Test: `internal/tui/stack_charsel_test.go` (new), `internal/tui/md_render_test.go` (`cont`; create it if absent, with `markdown` imported)

**Interfaces:**
- Consumes: Task 4's `charRows`/`charEmphProse`/`diffCharKey`, `stackCharBoundNotice`; `stackProseLead`, `m.stackProseWidth()`, `colouredLine`.
- Produces: `mdRow.cont bool`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/tui/md_render_test.go`:

```go
// A wrapped paragraph's continuation rows say so (cont), and a prefix
// (a quote bar) keeps the flag.
func TestMdWrapMarksContinuations(t *testing.T) {
	t.Parallel()
	rows := mdRows(markdown.Parse("one two three four"), 9)
	if len(rows) < 2 || rows[0].cont || !rows[1].cont {
		t.Fatalf("rows = %+v", rows)
	}
	q := mdRows(markdown.Parse("> one two three four"), 11)
	if len(q) < 2 || q[0].cont || !q[1].cont {
		t.Fatalf("quote rows = %+v", q)
	}
}
```

Create `internal/tui/stack_charsel_test.go`:

```go
package tui

import (
	"strings"
	"testing"
)

// openedReviewStack is the review view's stacked diff: the summary element
// first, then a.go; the model, the view and the first prose line's index.
func openedReviewStack(t *testing.T) (Model, *diffView, int) {
	t.Helper()
	m, id := reviewViewModel(t, charSelReviewDoc)
	m, cmd := m.openReview(id, "Review")
	m = drainCmds(t, m, cmd)
	m = m.setStackedPref(true)
	l, _ := filesLine(t, m, "a.go")
	u, cmd := m.openDiffForFileLine(l)
	m = drainCmds(t, u.(Model), cmd)
	v := m.diffLayer()
	if v == nil || v.stk == nil || !v.stk.files[0].summary {
		t.Fatalf("stack %+v, want the summary first", v)
	}
	for i, ln := range v.lines {
		if ln.kind == lineProse {
			return m, v, i
		}
	}
	t.Fatal("no prose line")
	return m, nil, -1
}

// v on a prose row selects within the summary element: the copy is the
// rendered row text (no "##"), and the element bounds the cursor.
func TestStackCharSelInTheSummaryElement(t *testing.T) {
	t.Parallel()
	m, v, first := openedReviewStack(t)
	v.setCursorLine(first, m.diffBodyRows())
	var got string
	m = captureClip(m, &got)
	m = feedDiff(m, "v")
	if !v.cs.on || v.csFile != 0 || v.cs.cur != (pos{first - v.csBase, 0}) {
		t.Fatalf("v: %+v base %d file %d", v.cs, v.csBase, v.csFile)
	}
	m = feedDiff(m, "k") // above the first prose row: the header, the rule — dead; the element's top
	if m.diffNotice != "▸ the selection stays in this element" {
		t.Fatalf("notice = %q", m.diffNotice)
	}
	m = feedDiff(m, "space", "end")
	u, cmd := m.Update(keyMsg("enter"))
	drainCmds(t, u.(Model), cmd)
	if got != "Overview" {
		t.Fatalf("copied %q, want the rendered heading", got)
	}
}

// A wrapped paragraph in the stack's summary copies joined with a space.
func TestStackCharSelJoinsWrappedProse(t *testing.T) {
	t.Parallel()
	m, v, first := openedReviewStack(t)
	v.setCursorLine(first, m.diffBodyRows())
	var got string
	m = captureClip(m, &got)
	m = feedDiff(m, "v")
	rows := v.charRows()
	wrapAt := -1
	for i, r := range rows {
		if r.wraps {
			wrapAt = i
			break
		}
	}
	if wrapAt < 1 {
		t.Fatalf("no wrapped prose row among %d (widen charSelReviewDoc's paragraph)", len(rows))
	}
	for v.cs.cur.row < wrapAt-1 {
		m = feedDiff(m, "j")
	}
	m = feedDiff(m, "home", "space", "j", "l", "l")
	u, cmd := m.Update(keyMsg("enter"))
	drainCmds(t, u.(Model), cmd)
	if want := string(rows[wrapAt-1].text) + " " + string(rows[wrapAt].text[:3]); got != want {
		t.Fatalf("copied %q\nwant   %q", got, want)
	}
}

// v on a file's row selects within that file on the cursor side; the file
// bounds the cursor (the notice names the file).
func TestStackCharSelInAFile(t *testing.T) {
	t.Parallel()
	m, v, _ := openedReviewStack(t)
	body := -1
	for i, ln := range v.lines {
		if ln.file == 1 && ln.isBody() && sidePresent(ln.Row, false) {
			body = i
			break
		}
	}
	if body < 0 {
		t.Fatal("a.go has no body line")
	}
	v.setCursorLine(body, m.diffBodyRows())
	var got string
	m = captureClip(m, &got)
	m = feedDiff(m, "v", "space", "l")
	if v.csFile != 1 {
		t.Fatalf("element = file %d, want a.go (1)", v.csFile)
	}
	m = feedDiff(m, "pgup") // the file's first live row at most; never into the summary
	if v.csFile != 1 || v.cs.cur.row < 0 || m.diffNotice != "▸ the selection stays in this file" {
		t.Fatalf("pgup left the file: file %d row %d notice %q", v.csFile, v.cs.cur.row, m.diffNotice)
	}
	m = feedDiff(m, "end") // the start is still column 0: the whole line
	u, cmd := m.Update(keyMsg("enter"))
	drainCmds(t, u.(Model), cmd)
	if want := v.lines[body].Row.Right; got != want {
		t.Fatalf("copied %q, want %q", got, want)
	}
}

// Review Focus 4: v on the element's header lands on its first live row.
func TestStackCharSelFromAHeaderLandsOnTheFirstRow(t *testing.T) {
	t.Parallel()
	m, v, first := openedReviewStack(t)
	v.setCursorLine(v.stk.files[0].hdr, m.diffBodyRows())
	m = feedDiff(m, "v")
	if !v.cs.on || v.cs.cur.row != first-v.csBase {
		t.Fatalf("v on the header: %+v, want row %d", v.cs, first-v.csBase)
	}
}

// The stripe reaches the prose row's painter.
func TestStackCharSelPaintsProseRows(t *testing.T) {
	t.Parallel()
	m, v, first := openedReviewStack(t)
	v.setCursorLine(first, m.diffBodyRows())
	m = feedDiff(m, "v", "space", "l", "l")
	if mask := v.charEmphProse(first, 8); mask == nil || mask[0] != emphSel || mask[2] != emphSelCur {
		t.Fatalf("mask = %v", mask)
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `cd /work/gigagit/.claude/worktrees/char-select && go test ./internal/tui/ -run 'TestMdWrapMarksContinuations|TestStackCharSel' 2>&1 | tail -6`
Expected: build failure on `rows[1].cont`; the stack tests fail on the notice / the join (`wraps` is false in Task 4's arm).

- [ ] **Step 3: `mdRow.cont`**

`md_render.go`, the struct:

```go
type mdRow struct {
	text string
	cls  []syntax.Class
	pre  bool
	// cont marks a wrap CONTINUATION of the row before it (mdWrap broke a
	// paragraph at width): a copy that spans the break joins the two with
	// one space (charsel.go), never a newline.
	cont bool
}
```

In `mdWrap`, after the loop builds `out`, before `return out`:

```go
	for i := 1; i < len(out); i++ {
		out[i].cont = true
	}
```

(read `mdWrap`'s tail to place it after every `emit` — the function ends with a final `emit` then `return out`). In `mdPrefix`: `rows[i] = mdRow{text: …, cls: …, pre: rows[i].pre, cont: rows[i].cont}`.

- [ ] **Step 4: The prose rows' `wraps` and painting**

`diff_charsel.go`, the `lineProse` arm: `wraps: f.prose[ln.prose].cont` (drop the Task 4 comment).

`diff_stack_render.go`, the `lineProse` case: replace `colouredLine(stackProseLead(w, m.stackProseWidth()), row.text, row.cls, nil, style, nil, w)` with

```go
			return colouredLine(stackProseLead(w, m.stackProseWidth()), row.text, row.cls, v.charEmphProse(dr.line, len([]rune(row.text))), style, nil, w)
```

- [ ] **Step 5: Run the tests**

Run: `cd /work/gigagit/.claude/worktrees/char-select && gofmt -l internal/tui/ && go test ./internal/tui/ -run 'TestMd|TestStackCharSel|TestReviewStack|TestDiffCharSel|TestI18n' 2>&1 | tail -4`
Expected: PASS (the stack notices' bundle entries landed in Task 4).

- [ ] **Step 6: Commit**

Message: `tui(stack): v selects characters inside one element of a stack — the summary's wrapped prose joins with a space, a file on its side, bounded by the element`.

---

### Task 6: Help, docs

**Files:**
- Modify: `internal/tui/help.go` (the review view's and the diff's `v` rows)
- Modify: `README.md`, `CHANGELOG.md`, `docs/CLAUDE-details.md`
- Modify: the four bundles (the help strings)
- Test: `internal/tui/help_test.go` (pin the rows exist)

- [ ] **Step 1: Write the failing test**

Append to `internal/tui/help_test.go`:

```go
// The v copy mode is documented where its hosts are: the review view and
// the diff.
func TestHelpListsTheCharacterSelection(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	for _, l := range helpContent() {
		b.WriteString(l.text + "\n")
	}
	text := b.String()
	for _, want := range []string{"select characters of the summary", "select characters to copy, on the cursor's side"} {
		if !strings.Contains(text, want) {
			t.Fatalf("help lacks %q", want)
		}
	}
}
```

(`helpContent()` is what `TestHelpDocumentsTags` walks.)

- [ ] **Step 2: Run it to verify it fails** — `go test ./internal/tui/ -run TestHelpListsTheCharacterSelection` → FAIL (`help lacks …`).

- [ ] **Step 3: The help rows**

In `help.go`, after the review view's `r("n / p", …)` row (the one starting "an AI review opens as the review view"), add:

```go
		r("v", i18n.T("select characters of the summary to copy — in the Summary popup and the stacked view's summary: move the cursor (←→↑↓ hjkl, w b by word, home end, pgup pgdn), space fixes the start, move to the end, enter (or y) copies the text as it reads on screen (a wrapped row joins with a space) and leaves, esc backs out; the mode is modal: every other key waits")),
```

After the diff's `r("enter", i18n.T("with a line selection on: copy the selected lines …"))` row, add:

```go
		r("v", i18n.T("select characters to copy, on the cursor's side: move (←→↑↓ hjkl, w b by word, home end, pgup pgdn), space fixes the start, enter (or y) copies the covered text — absent cells and folded lines skipped, tabs kept — and leaves, esc backs out; the mode is modal (alt+←/→, n/p, / wait); stacked, the selection stays inside the file or the summary it started in")),
```

Bundles (append; keep every `(`, `/` and key name as in the key):

ja: `"select characters of the summary to copy — …" = "サマリーの文字を選択してコピー — Summary ポップアップとスタック表示のサマリーで: カーソルを移動 (←→↑↓ hjkl、w b は単語単位、home end、pgup pgdn)、space で始点を固定、終点へ移動、enter (または y) で画面に表示されているとおりのテキストをコピーして終了 (折り返し行はスペースで結合)、esc で戻る; このモードはモーダルで、他のキーは待機します"` and `"select characters to copy, on the cursor's side: …" = "カーソル側の文字を選択してコピー: 移動 (←→↑↓ hjkl、w b は単語単位、home end、pgup pgdn)、space で始点を固定、enter (または y) で範囲のテキストをコピーして終了 (存在しないセルと折りたたまれた行は飛ばし、タブはそのまま)、esc で戻る; このモードはモーダル (alt+←/→、n/p、/ は待機); スタック表示では選択は開始したファイルまたはサマリーの中にとどまります"`
ko: `"… summary …" = "요약의 문자를 선택해 복사 — Summary 팝업과 스택 보기의 요약에서: 커서 이동 (←→↑↓ hjkl, w b 단어 단위, home end, pgup pgdn), space로 시작점 고정, 끝으로 이동, enter (또는 y)로 화면에 보이는 그대로 텍스트를 복사하고 나가기 (줄바꿈된 행은 공백으로 연결), esc로 되돌리기; 이 모드는 모달이라 다른 키는 기다립니다"` and `"… cursor's side …" = "커서 쪽의 문자를 선택해 복사: 이동 (←→↑↓ hjkl, w b 단어 단위, home end, pgup pgdn), space로 시작점 고정, enter (또는 y)로 선택한 텍스트를 복사하고 나가기 (없는 셀과 접힌 줄은 건너뛰고 탭은 유지), esc로 되돌리기; 이 모드는 모달 (alt+←/→, n/p, /는 기다림); 스택에서는 선택이 시작한 파일 또는 요약 안에 머뭅니다"`
zh: `"… summary …" = "选择摘要中的字符并复制 — 在 Summary 弹窗和堆叠视图的摘要中: 移动光标 (←→↑↓ hjkl, w b 按单词, home end, pgup pgdn), space 固定起点, 移到终点, enter (或 y) 按屏幕显示复制文本并退出 (换行的行以空格连接), esc 返回; 此模式为模态, 其他键等待"` and `"… cursor's side …" = "选择光标一侧的字符并复制: 移动 (←→↑↓ hjkl, w b 按单词, home end, pgup pgdn), space 固定起点, enter (或 y) 复制所选文本并退出 (跳过缺失单元格和折叠行, 保留制表符), esc 返回; 此模式为模态 (alt+←/→, n/p, / 等待); 堆叠视图中选择范围限于起始的文件或摘要"`
ru: `"… summary …" = "выделить символы сводки для копирования — в окне Summary и в сводке стопки: переместите курсор (←→↑↓ hjkl, w b по словам, home end, pgup pgdn), space фиксирует начало, переместитесь к концу, enter (или y) копирует текст как на экране (перенесённая строка соединяется пробелом) и выходит, esc отменяет; режим модальный: остальные клавиши ждут"` and `"… cursor's side …" = "выделить символы для копирования на стороне курсора: перемещение (←→↑↓ hjkl, w b по словам, home end, pgup pgdn), space фиксирует начало, enter (или y) копирует выделенный текст (пропуская отсутствующие ячейки и свёрнутые строки, табуляции сохраняются) и выходит, esc отменяет; режим модальный (alt+←/→, n/p, / ждут); в стопке выделение остаётся в файле или сводке, где началось"`

(Write the full English key verbatim in each bundle, not the `…` shorthand.)

- [ ] **Step 4: README** — in the review view passage (the paragraph starting "Opening such a review shows the **review view**", ~line 1455) after "`o` in the summary lists those other notes to open one." add: "`v` in the summary starts a **character selection** (a modal copy mode, as vim's visual mode): move the cursor (`←→↑↓`, `hjkl`, `w`/`b` by word, `home`/`end`), `space` fixes the start, move to the end, `enter` (or `y`) copies the text as it reads on screen — a wrapped row joins with a space — and leaves, `esc` backs out; the stacked view's summary has it too." In the diff's selection passage (grep `space` / `mark end` in README's diff section, the paragraph describing space / space / enter) add: "`v` selects by **character** instead, on the cursor's side: the same modal mode (`space` start, `enter`/`y` copy, `esc` leave), absent cells and folded lines skipped, tabs kept; stacked, it stays inside the file or the summary it started in."

- [ ] **Step 5: CHANGELOG** — a new top section:

```markdown
## Character selection: the v copy mode (TUI)

A modal copy mode in the spirit of vim's visual mode and tmux's copy mode,
spec `docs/superpowers/specs/2026-10-10-char-select-design.md`: `v` enters
it and shows a character cursor, `←→↑↓` / `hjkl` / `w b` / `home end` /
`pgup pgdn` move, `space` fixes the start (again: restart), `enter` or `y`
copies the covered text and leaves, `esc` drops the start, then leaves.
While it is on every other key waits.

- **Where.** The review's Summary popup, the stacked review view (its
  summary element, or one file on the cursor's side — the element bounds
  the selection) and the single diff view (the cursor's side; `v` clears a
  live line selection, one kind at a time). Blame and the View-file preview
  follow later through the same model.
- **What is copied.** The rendered text, never the markdown: a wrapped
  prose row joins with one space, any other row with a newline; a diff
  copies the side's source lines (tabs kept), absent cells and folded
  lines skipped. The status line counts the characters.
- **How it is drawn.** The covered runes wear the `selection_bg` stripe,
  the cursor cell the current-hit style over it; the footer shows the
  mode's keys. One pure model (`charsel.go`) with thin host adapters; the
  painting rides the per-rune emphasis mask (`emphSel`, `emphSelCur`).
- The mode leaves when its rows change meaning: a popup resize or `ctrl+t`,
  a diff reload, a fold, `ctrl+w`.
```

- [ ] **Step 6: CLAUDE-details** — a block before "### PR review, plan 3 — the web":

```markdown
### Character selection — the v copy mode (2026-10-10, spec `docs/superpowers/specs/2026-10-10-char-select-design.md`)

- **Model** (`charsel.go`, pure): `pos{row, col}` over a host's `charRow{text []rune, wraps, dead}`; `charSel{on, fixed, anchor, cur, want}` with `enter/leave/press/esc/bounds/count` and the moves (`left/right` run across rows, `vertical` keeps `want`, `word` walks `wordStarts`); `charSelText` joins (`wraps` = one space, else `\n`, dead rows skipped); `charSelKey(cs, host, msg) charSelResult{handled, copy, notice, bump}` — every key while on except ctrl+c; `charSelHint`, `copiedCharsText`; `charSelEmph(cs, row, n)` / `shiftMask` for mask painters, `charSelSpans(cs, row)` (`hitSpan{sel: true}`, the cursor `cur`) for the diff's cell painters.
- **Painting**: `emphSel` / `emphSelCur` (`textsearch.go`); `overlayHits` maps a `sel` span; `styledRuns` paints them with `selectionStyle(base)` and `currentHitStyle(selectionStyle(base))`.
- **Summary popup** (`contentPopup`): `cs`, `csRows`/`csSeg` (`charSeg{line, off, pad}`) laid out by `charLayout` from `layoutWidths` + `wrapRow` + `wrapSegOffsets` (the inverse of `wrapSegMask`); rows are DISPLAY rows (the pager's unit), the "  " lead and the hang indent are pads; `charEmphFor(line, n)` fills `winRow.emph`; `charFollow` scrolls the pager; `box()` leaves the mode when `csW != bodyW` (a resize re-wraps); the keys line and the hint swap while on. `reviewSummaryPopup.update` routes `charKey` first, then `v`.
- **Diff** (`diff_charsel.go`): `diffView.cs/csBase/csFile/csPage`; `charRows` over `charRange()` (the stream, or a stack element's `fileLineRange(csFile)`), dead where `!isBody || !sidePresent`, prose rows from `f.prose` with `wraps = mdRow.cont`; `charSpansOn(li)` converts columns with `dispCol` (sanitizeCell's tab expansion) and `renderDiffView` appends them to the cursor side's `lh`/`rh`; `charEmphProse` for `stackRow`'s prose case; `diffCharKey` runs before `diffSelectKey` (modal), `diffCharEnter` clears `lsel`, the line cursor follows (`setCursorLine`), a bump in a stack says `stackCharBoundNotice`. The mode leaves wherever `lsel.clear()` runs (rebuild, relayout, the stack re-splice).
- `mdRow.cont` marks a wrap continuation (`mdWrap`, carried by `mdPrefix`).
```

- [ ] **Step 7: Run the package and commit**

Run: `cd /work/gigagit/.claude/worktrees/char-select && go test ./internal/tui/ 2>&1 | tail -3` → `ok`.
Message: `docs(tui): the v copy mode — help rows, README, CHANGELOG, CLAUDE-details`.

---

## After the last task

1. `./test.sh race` in the worktree, output to a log file, sandbox disabled, in the background; green only on the literal `all green` and zero `FAIL` / `DATA RACE` lines.
2. A read-only reviewer subagent (Opus) over the whole branch with this plan, the spec and the Review Focus list; fix Critical/Important test-first in ONE pass; minors to the handover.
3. A manual check in the real TUI (`./tui-capture.sh` or a terminal): open a review, `≡ Summary`, `v`, walk, `space`, `enter`; the stacked view on the summary and on a file; the single diff with a tab-indented file. Describe what you SAW in the handover.
4. Hand over for the user's "merge": `git merge-base --is-ancestor main feat/char-select && gg merge -F <msgfile> --into main feat/char-select` from `/work/gigagit`, `test -z "$(git status --short)"`, `./build.sh install`, remove the worktree and branch, update memory (`char-select-feature.md`).
