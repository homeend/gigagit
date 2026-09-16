# Line Cursor, One-Sided Diff Cursor and Line Selection + Copy Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give the three readers — the diff view, blame and the View-file preview — a tmux-style whole-line selection (`space` marks, `space` again freezes, `enter` copies, `esc` unmarks) with a `Copy line` / `Copy selected lines (N)` pair in each `.` menu, a line cursor in the preview (which had only a pager), and a diff cursor that sits on ONE side so the copy, the review-note anchor and the `gg://` link all take that side's text.

**Architecture:** One pure value type (`internal/tui/lineselect.go`: `lineSel{on, anchor, end, fixed}`) holds the selection for all three hosts; each host embeds it as `lsel`, owns one key hook (`<host>SelectKey`) placed BEFORE its search hook, and one function that builds the copy action rows — so the `enter` key and the `.` menu row are literally the same `actionRow` and a test can read the clipboard payload off `row.copyText` (the `contextLinkRow` pattern). Painting is a base-style swap, not a new mask level: `styles.selectionStyle(base)` follows the search current-hit's two-mode contract (theme role `selection_bg` set → a background patch that clears reverse; unset → a flip of reverse video, which inverts an ordinary row and punches a hole in a reverse-video one). The diff threads it through a new `cellMark.sel` bit and builds TWO marks per row (`mkL`, `mkR`) so the cursor band lands on one cell only; blame needs a body-only style (`winRow.body`, because its gutter is `winRow.prefix` and must stay unpainted); the preview has no prefix at all, so it paints through the existing `winRow.style`.

**Tech Stack:** Go 1.26, Bubble Tea + lipgloss v1.1.0, `internal/theme`, `internal/textdiff`, `internal/syntax`, `internal/i18n`, `internal/tui`.

**Spec:** `docs/superpowers/specs/2026-09-08-hunk-parity-roadmap.md` — §4.7 (lines 957–1149) is binding, including its four user rulings (the range freezes after the second space; a diff selection is locked to its side; a range across a collapsed fold copies only the visible lines; the note default anchor follows the cursor side). §4.3 (in-view search) built the `emphLevel` / `winRow.emph` / `currentHitStyle` machinery this plan borrows from. The rulings that resolve what the spec left open are in **Rulings made while planning** at the end of this file — read them before Task 1.

## Global Constraints

- **Worktree.** All work happens in `/mnt/t/others/gigagit.worktrees/feat-line-select-copy` (branch `feat/line-select-copy`, base main `e3b815eb`). The shell cwd resets to the main checkout between commands: prefix EVERY command with `cd /mnt/t/others/gigagit.worktrees/feat-line-select-copy &&`, use `git -C /mnt/t/others/gigagit.worktrees/feat-line-select-copy` for git, and pass ABSOLUTE paths to Write/Edit. Never touch the main checkout or any other worktree.
- **TDD.** Write the failing test first, run it, watch it fail for the stated reason, then implement. Where a step leaves the package uncompilable between steps the step text says so and names the single run that must go green.
- Tests call `t.Parallel()` and inject state; never `t.Setenv` in `internal/tui` tests.
- **Carve-out to the `t.Parallel()` rule:** any test that calls `lipgloss.SetColorProfile` MUST NOT call `t.Parallel()` — the colour profile is process-global and a parallel sibling's deferred reset lands mid-render and drops the ANSI codes the test asserts on. `internal/tui/window_syntax_test.go` and `internal/tui/search_paint_test.go` carry this note at the top of the file; copy it verbatim into every new test file that sets the profile.
- **Paint tests compare BODY ROWS and assert the runtime-derived SGR at the painted cell.** Use `sgrSeqBefore` / `sgrBefore` / `hasSGR` from `internal/tui/search_paint_test.go` — never `ansi.Cut`, which re-emits surrounding state and drags a later run's SGR into an earlier slice. Never hard-code an escape sequence: derive the expected one by rendering the same style at runtime.
- **Implementers run ONLY the package tests, in the FOREGROUND, ONCE per task:**
  `cd /mnt/t/others/gigagit.worktrees/feat-line-select-copy && go test ./internal/tui/...` (≈ 5.5 min — set the Bash timeout to 600000), plus `go test ./internal/theme/... ./internal/config/...` (fast) where the task touches those packages. Single-test runs (`-run`) during the red/green cycle are fine and encouraged. NEVER run `./test.sh` or `./test.sh race` — the controller runs the race gate.
- Every task ends with `gofmt -l internal` printing NOTHING and `go vet ./internal/tui/... ./internal/theme/...` clean.
- `internal/tui` never imports `internal/git` (enforced by `internal/archtest`). This plan touches `internal/tui`, `internal/theme`, `internal/i18n/lang`, `internal/config` and the docs — nothing else. No web change, no CLI change, no `agentskill` change, no new config key.
- **Every new or changed `i18n.T` literal lands in all four bundles in the SAME commit** (`internal/i18n/lang/{ja,ko,ru,zh}.toml`, `[strings]` table, key = the exact English literal). `TestI18nBundlesComplete` fails BOTH ways: a catalog key with no bundle entry ("missing translation") and a bundle key no longer in the catalog ("orphaned key"). A CHANGED literal therefore means *delete the old key line and add the new one* in each of the four files. **Locate a key to delete by its exact text (`grep -n`), never by the line numbers quoted in the tasks:** those are pre-plan numbers and every earlier task's deletions shift them. The four bundles are line-aligned (1863 lines each today); append new keys at the END of each `[strings]` table — the TOML table is unordered and no test pins the order.
- **Before deleting a key, check it is not still used elsewhere:** `grep -rn '"<the exact English text>"' internal/` over the non-test sources. If another call site still uses it, keep the bundle line.
- **Diff footer ≤ 140 columns in every variant**, `[/] find` second. `TestDiffHintFitsTheBudget` pins the scroll variant at exactly 140; `TestRenderDiffViewPanes` renders at width 140 and fails on any wider line.
- **Commits:** `git add` the named files ONLY (never `git add -A`), message via `git commit -F <file>` (never `-F -`), ending with the two trailers exactly:

```
Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01NDk1DJtgLzZX7hMmxDs9nU
```

- `CHANGELOG.md` contains a literal `<<<<<<<` line further down the file — it is NOT a merge marker. Add the new entry directly under the `## [Unreleased]` heading at `CHANGELOG.md:9`.

## File structure

| File | Responsibility after this plan |
|---|---|
| `internal/tui/lineselect.go` (new) | The whole selection leaf: `lineSel` with `start` / `mark` / `press` / `clear` / `bounds` / `contains`. Pure, no host knowledge. |
| `internal/tui/lineselect_test.go` (new) | Table test for the state machine. |
| `internal/theme/theme.go`, `override.go` | Role `selection_bg` (`Theme.Selection`, `Override.Selection`, `roleFields` row, `roles()` entry, Dark/Light values). |
| `internal/tui/styles.go` | `selectionBg string` + `(*styles).selectionStyle(base)`. |
| `internal/tui/diff_view.go` | `diffView.onOld`, `diffView.lsel`; the `alt+←/→` case; `rebuild`/`ctrl+w` invalidation. |
| `internal/tui/diff_cursor.go` | `sidePresent`, `(*diffView).cursorCell`, `(*diffView).selectedLines`. |
| `internal/tui/diff_select.go` (new) | `diffSelectKey`, `diffCopyLineRows`, `lockedSideNotice`, `diffSelectHint`. |
| `internal/tui/diff_render.go` | Two marks per row in `diffPaneLines`; `cellMark.sel` + `bodyFor`; the side-first header label; the new footer strings. |
| `internal/tui/note_keys.go` | `noteAnchorsAtCursor` lists the cursor side first. |
| `internal/tui/mouse.go` | A left click picks the pane as well as the row. |
| `internal/tui/window.go` | `winRow.body *lipgloss.Style` (body + padding style, prefix keeps `style`); `colouredLine` takes it; the reverse-video class drop widens to cover it. |
| `internal/tui/blame_view.go` | `blameView.lsel`; the stripe on `winRow.body`; the two footer variants. |
| `internal/tui/blame_select.go` (new) | `blameSelectKey`, `blameCopyLineRows`, `(*blameView).selectedLines`. |
| `internal/tui/content_popup.go` | `contentLine.raw` / `.src`; `contentPopup.cur` / `.lsel`. |
| `internal/tui/file_preview.go` | `raw`/`src` filling, the cursor band + stripe in `renderFilePreview`, `ensureCursorVisible`, `searchPos` from the cursor, `snapHit` lands the cursor, the two hint variants. |
| `internal/tui/preview_select.go` (new) | `previewSelectKey`, `previewCopyLineRows`, `(*contentPopup).selectedLines`, `(Model).movePreviewCursor`. |
| `internal/tui/files_view.go` | `alt+↑/↓`; the select hook before the search hook. |
| `internal/tui/action_menu.go` | `rowByID`; the preview branch of `contextCopyRows`; line rows lead the diff/blame/preview copy rows; `onStashList` yields to an open preview. |
| `internal/tui/footer.go`, `help.go` | The preview status-bar variants; help rows for all three hosts. |
| `internal/i18n/lang/{ja,ko,ru,zh}.toml` | Translations, per commit. |
| `README.md`, `CHANGELOG.md`, `docs/CLAUDE-details.md`, `internal/config/template.go`, `internal/config/config.go` | Docs (Task 6). |

---

### Task 1: The selection value type, the theme role and the stripe style

Nothing user-visible ships here: a pure type, one theme role and one style
helper the next four tasks paint through. The role count goes 54 → 55, which
three tests pin.

**Files:**
- Create: `internal/tui/lineselect.go`
- Create: `internal/tui/lineselect_test.go`
- Create: `internal/tui/selection_style_test.go`
- Modify: `internal/theme/theme.go:39-42` (field block), `:70` (`roles()`), `:91` (Dark), `:113` (Light)
- Modify: `internal/theme/override.go:42` (Override field), `:106` (`roleFields` row)
- Modify: `internal/theme/roles_test.go:12` (54 → 55)
- Modify: `internal/tui/theme_editor_popup_test.go:67` (54 → 55)
- Modify: `internal/tui/styles.go:55` (field), `:149` (buildStyles), `:229` (after `currentHitStyle`)

**Interfaces:**
- Consumes: `lipgloss.Style`, `theme.Theme`.
- Produces (later tasks use these exact names):
  - `type lineSel struct { on bool; anchor, end int; fixed bool }`
  - `func (s *lineSel) start(cur int)`, `func (s *lineSel) mark(cur int)`, `func (s *lineSel) press(cur int)`, `func (s *lineSel) clear()`
  - `func (s lineSel) bounds(cur int) (lo, hi int, ok bool)`
  - `func (s lineSel) contains(i, cur int) bool`
  - `theme.Theme.Selection string` (TOML key `selection_bg`), `theme.Override.Selection string`
  - `styles.selectionBg string`
  - `func (s *styles) selectionStyle(base lipgloss.Style) lipgloss.Style`

- [ ] **Step 1: Write the failing `lineSel` table test**

Create `/mnt/t/others/gigagit.worktrees/feat-line-select-copy/internal/tui/lineselect_test.go`:

```go
package tui

import "testing"

// The state machine, in the order the keys fire it: space starts, space again
// freezes, space a third time starts over. bounds() reads the LIVE cursor
// while the range is loose and the frozen end once it is fixed.
func TestLineSelStateMachine(t *testing.T) {
	t.Parallel()
	var s lineSel
	if _, _, ok := s.bounds(5); ok {
		t.Fatal("the zero value must report no selection")
	}
	if s.contains(5, 5) {
		t.Fatal("the zero value contains nothing")
	}

	s.press(10) // first space: anchor at 10, range follows the cursor
	if !s.on || s.fixed || s.anchor != 10 {
		t.Fatalf("after the first press: %+v", s)
	}
	lo, hi, ok := s.bounds(14)
	if !ok || lo != 10 || hi != 14 {
		t.Fatalf("loose bounds at cursor 14 = %d..%d ok=%v, want 10..14", lo, hi, ok)
	}
	// The cursor above the anchor: the range is still anchor..cursor, ordered.
	if lo, hi, _ = s.bounds(3); lo != 3 || hi != 10 {
		t.Fatalf("loose bounds at cursor 3 = %d..%d, want 3..10", lo, hi)
	}

	s.press(14) // second space: the end freezes, the cursor is free
	if !s.fixed || s.end != 14 {
		t.Fatalf("after the second press: %+v", s)
	}
	if lo, hi, _ = s.bounds(99); lo != 10 || hi != 14 {
		t.Fatalf("a frozen range must ignore the cursor: %d..%d, want 10..14", lo, hi)
	}
	if !s.contains(12, 99) || s.contains(15, 99) {
		t.Fatalf("contains is inclusive over the frozen range: %+v", s)
	}

	s.press(20) // third space: a brand-new range at the cursor
	if !s.on || s.fixed || s.anchor != 20 {
		t.Fatalf("after the third press: %+v", s)
	}

	s.clear()
	if s != (lineSel{}) {
		t.Fatalf("clear must restore the zero value, got %+v", s)
	}
}

// A one-space selection copies anchor..cursor, the way tmux does; mark() on a
// cleared selection is a no-op (no host can reach it, but the type must not
// invent an `on` out of a stray call).
func TestLineSelOneSpaceAndInertMark(t *testing.T) {
	t.Parallel()
	var s lineSel
	s.start(7)
	if lo, hi, ok := s.bounds(7); !ok || lo != 7 || hi != 7 {
		t.Fatalf("a one-line selection = %d..%d ok=%v, want 7..7", lo, hi, ok)
	}

	var inert lineSel
	inert.mark(3)
	if inert.on {
		t.Fatalf("mark on a cleared selection must stay off: %+v", inert)
	}
}

// Ordering: the anchor may be BELOW or ABOVE the frozen end, and bounds must
// hand back a low..high pair either way.
func TestLineSelBoundsAreOrdered(t *testing.T) {
	t.Parallel()
	var s lineSel
	s.start(9)
	s.mark(2)
	lo, hi, ok := s.bounds(0)
	if !ok || lo != 2 || hi != 9 {
		t.Fatalf("bounds = %d..%d ok=%v, want 2..9", lo, hi, ok)
	}
	if !s.contains(2, 0) || !s.contains(9, 0) || s.contains(1, 0) || s.contains(10, 0) {
		t.Fatal("contains must be inclusive at both ends and exclusive outside")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-line-select-copy && go test ./internal/tui/ -run TestLineSel -v`
Expected: FAIL — `undefined: lineSel`.

- [ ] **Step 3: Write `lineSel`**

Create `/mnt/t/others/gigagit.worktrees/feat-line-select-copy/internal/tui/lineselect.go`:

```go
package tui

// lineSel is a tmux-style whole-line selection over a host's LOGICAL line
// indexes — the diff's index into v.lines, blame's into b.lines, the preview's
// into p.lines. Never a DISPLAY row: a wrapped line owns several of those, and
// every host already maps one to the other for its cursor.
//
// The zero value is "no selection". The key sequence is space / space / enter:
// the first space starts a range that follows the cursor, the second freezes
// its end (the cursor is then free to move — the user's ruling), a third starts
// a new range. enter copies bounds() and clears; esc clears and does nothing
// else.
type lineSel struct {
	on     bool // a selection exists (anchor is valid)
	anchor int  // the line the first space marked
	end    int  // the line the second space marked; meaningful when fixed
	fixed  bool // the second space landed: the range no longer follows the cursor
}

// start begins a fresh range at cur (the first space, and the third).
func (s *lineSel) start(cur int) {
	s.on, s.anchor, s.end, s.fixed = true, cur, cur, false
}

// mark freezes the range's far end at cur (the second space). Inert with no
// selection on: freezing a range that does not exist would invent one.
func (s *lineSel) mark(cur int) {
	if !s.on {
		return
	}
	s.end, s.fixed = cur, true
}

// press is the space key itself, identical in all three hosts: start, then
// freeze, then start again.
func (s *lineSel) press(cur int) {
	if s.on && !s.fixed {
		s.mark(cur)
		return
	}
	s.start(cur)
}

// clear drops the selection (esc, a copy, a rebuild of the line stream).
func (s *lineSel) clear() { *s = lineSel{} }

// bounds is the inclusive line range the selection covers right now: while the
// range is loose it is anchor..cur in either order, once frozen it is
// anchor..end. ok is false when there is no selection.
func (s lineSel) bounds(cur int) (lo, hi int, ok bool) {
	if !s.on {
		return 0, 0, false
	}
	other := cur
	if s.fixed {
		other = s.end
	}
	lo, hi = s.anchor, other
	if lo > hi {
		lo, hi = hi, lo
	}
	return lo, hi, true
}

// contains reports whether line i is inside the range measured from cursor cur.
func (s lineSel) contains(i, cur int) bool {
	lo, hi, ok := s.bounds(cur)
	return ok && i >= lo && i <= hi
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-line-select-copy && go test ./internal/tui/ -run TestLineSel -v`
Expected: PASS (three tests).

- [ ] **Step 5: Write the failing theme + style tests**

First bump the two role-count pins. In
`/mnt/t/others/gigagit.worktrees/feat-line-select-copy/internal/theme/roles_test.go`, the
block reads:

```go
	if len(rs) != 54 {
		t.Fatalf("Roles() = %d, want 54", len(rs))
	}
```

Replace it with:

```go
	if len(rs) != 55 {
		t.Fatalf("Roles() = %d, want 55", len(rs))
	}
```

In `/mnt/t/others/gigagit.worktrees/feat-line-select-copy/internal/tui/theme_editor_popup_test.go`:

```go
	if len(p.roles) != 54 {
		t.Fatalf("roles = %d, want 54", len(p.roles))
	}
```

becomes:

```go
	if len(p.roles) != 55 {
		t.Fatalf("roles = %d, want 55", len(p.roles))
	}
```

Then create
`/mnt/t/others/gigagit.worktrees/feat-line-select-copy/internal/tui/selection_style_test.go`:

```go
package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/homeend/gigagit/internal/theme"
)

// NOTE: the tests in this file do NOT call t.Parallel() —
// lipgloss.SetColorProfile is process-global, so a parallel sibling's deferred
// reset would land mid-render and drop the ANSI codes they assert on (the same
// rule window_syntax_test.go and search_paint_test.go follow).

// With the role UNSET (the Terminal theme) the stripe flips reverse video
// against the row it lands on: inverted over an ordinary row, a HOLE over a
// reverse-video one (blame's cursor row). This mirrors currentHitStyle's
// contract, minus the bold — a range marks EXTENT, not one hit.
func TestSelectionStyleFlipsReverseWhenTheRoleIsUnset(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(prev)

	s := buildStyles(theme.Terminal)
	plain := s.selectionStyle(lipgloss.NewStyle())
	if !plain.GetReverse() {
		t.Fatal("over an ordinary row the stripe must turn reverse ON")
	}
	if plain.GetBold() {
		t.Fatal("the stripe must not bold a whole range")
	}
	if got := plain.Render("x"); !hasSGR(got, "7") {
		t.Fatalf("no reverse SGR in %q", got)
	}

	hole := s.selectionStyle(s.selectedRow)
	if hole.GetReverse() {
		t.Fatal("over a reverse-video row the stripe must turn reverse OFF (a hole)")
	}
	if got := hole.Render("x"); hasSGR(got, "7") {
		t.Fatalf("the hole must carry no reverse SGR: %q", got)
	}
}

// With the role SET the stripe is an explicit background patch that clears
// reverse, so it reads the same over a plain row, a reversed row and the diff's
// cursor-row band.
func TestSelectionStyleUsesTheThemeBackgroundWhenSet(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(prev)

	s := buildStyles(theme.Theme{Name: "t", Selection: "#264F78"})
	for _, base := range []lipgloss.Style{lipgloss.NewStyle(), s.selectedRow, s.diffCursorRow} {
		sel := s.selectionStyle(base)
		if sel.GetReverse() {
			t.Fatalf("the background patch must clear reverse, base reverse=%v", base.GetReverse())
		}
		if got := sel.GetBackground(); got != lipgloss.Color("#264F78") {
			t.Fatalf("background = %v, want the theme's selection_bg", got)
		}
		got := sel.Render("x")
		if !strings.Contains(got, "48;2;38;79;120") {
			t.Fatalf("no true-colour background escape in %q", got)
		}
		if ansi.Strip(got) != "x" {
			t.Fatalf("the patch changed the text: %q", ansi.Strip(got))
		}
	}
}

// The built-ins carry the editor-selection blues; Terminal leaves the role
// unset so it inherits the terminal's own inversion.
func TestSelectionRoleValues(t *testing.T) {
	if theme.Dark.Selection != "#264F78" {
		t.Fatalf("Dark.Selection = %q, want #264F78", theme.Dark.Selection)
	}
	if theme.Light.Selection != "#ADD6FF" {
		t.Fatalf("Light.Selection = %q, want #ADD6FF", theme.Light.Selection)
	}
	if theme.Terminal.Selection != "" {
		t.Fatalf("Terminal.Selection = %q, want empty", theme.Terminal.Selection)
	}
}
```

- [ ] **Step 6: Run the tests to verify they fail**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-line-select-copy && go test ./internal/tui/ -run 'TestSelectionStyle|TestSelectionRoleValues' -v`
Expected: FAIL to COMPILE — `undefined: theme.Theme.Selection`, `s.selectionStyle undefined`.

Run: `cd /mnt/t/others/gigagit.worktrees/feat-line-select-copy && go test ./internal/theme/ -run TestRolesCoversEveryEditableRole -v`
Expected: FAIL — `Roles() = 54, want 55`.

- [ ] **Step 7: Add the theme role**

In `/mnt/t/others/gigagit.worktrees/feat-line-select-copy/internal/theme/theme.go`, the field
block reads:

```go
	// SearchCurrent is the background the CURRENT in-view search hit is
	// painted with. "" (the Terminal theme) means "no colour of its own":
	// the hit inverts against whatever its row already wears.
	SearchCurrent string
```

Append after it:

```go
	// Selection is the background a line SELECTION's stripe is painted with
	// (spec §4.7). "" (the Terminal theme) means "no colour of its own": the
	// stripe inverts against whatever its row already wears — which punches a
	// hole in blame's reverse-video cursor row, exactly as the search hit does.
	Selection string
```

In `roles()` the list reads:

```go
		t.SearchCurrent,
```

Replace with:

```go
		t.SearchCurrent, t.Selection,
```

In `var Dark`, the line reads:

```go
	MessageBlockBg: "236", SaveBannerFg: "#F2F2F2", SaveBannerBg: "22", SearchCurrent: "#6B5F11",
```

Replace with:

```go
	MessageBlockBg: "236", SaveBannerFg: "#F2F2F2", SaveBannerBg: "22", SearchCurrent: "#6B5F11",
	Selection: "#264F78",
```

In `var Light`, the line reads:

```go
	MessageBlockBg: "#DFDFDA", SaveBannerFg: "#E9E9E5", SaveBannerBg: "#3E8E41", SearchCurrent: "#FFE680",
```

Replace with:

```go
	MessageBlockBg: "#DFDFDA", SaveBannerFg: "#E9E9E5", SaveBannerBg: "#3E8E41", SearchCurrent: "#FFE680",
	Selection: "#ADD6FF",
```

In `/mnt/t/others/gigagit.worktrees/feat-line-select-copy/internal/theme/override.go` the
Override field reads:

```go
	SearchCurrent string `toml:"search_current_bg"`
```

Replace with:

```go
	SearchCurrent string `toml:"search_current_bg"`
	Selection     string `toml:"selection_bg"`
```

And the `roleFields` row:

```go
	{"search_current_bg", "background of the CURRENT in-view search hit (empty = invert the hit against its row)", func(t *Theme) *string { return &t.SearchCurrent }, func(o *Override) *string { return &o.SearchCurrent }},
```

Replace with:

```go
	{"search_current_bg", "background of the CURRENT in-view search hit (empty = invert the hit against its row)", func(t *Theme) *string { return &t.SearchCurrent }, func(o *Override) *string { return &o.SearchCurrent }},
	{"selection_bg", "background of a selected line's text in the diff, blame and the file preview (empty = invert the line against its row)", func(t *Theme) *string { return &t.Selection }, func(o *Override) *string { return &o.Selection }},
```

`roles.go`'s `isBgKey` classifies any key ending in `_bg` as a background, so
`selection_bg` samples as a background swatch in the theme editor with no
further change.

- [ ] **Step 8: Add `selectionStyle`**

In `/mnt/t/others/gigagit.worktrees/feat-line-select-copy/internal/tui/styles.go` the field
reads:

```go
	searchCurBg    string // theme role search_current_bg; "" = flip (see currentHitStyle)
```

Replace with:

```go
	searchCurBg    string // theme role search_current_bg; "" = flip (see currentHitStyle)
	selectionBg    string // theme role selection_bg; "" = flip (see selectionStyle)
```

In `buildStyles` the line reads:

```go
	s.searchCurBg = th.SearchCurrent
```

Replace with:

```go
	s.searchCurBg = th.SearchCurrent
	// Same "kept raw, resolved per row" treatment: the selection stripe is
	// relative to the row it lands on (legacy carries no selection colour, so
	// the Terminal theme flips).
	s.selectionBg = th.Selection
```

Immediately after `currentHitStyle` (which ends with its closing brace at
`styles.go:229`) add:

```go
// selectionStyle paints a line-SELECTION stripe over base — the style the row
// underneath already wears (a diff cell background, the cursor-row band,
// blame's reverse-video cursor row, nothing at all in the preview).
//
// The contract is currentHitStyle's, minus the bold: with the theme role
// selection_bg set the stripe is an explicit background patch that also clears
// reverse, so it overrides the cursor band and reads identically in all three
// readers; with the role unset (the Terminal theme) it FLIPS reverse video —
// inverted over an ordinary row, un-inverted (a hole) over a reversed one.
// No bold: a stripe marks the EXTENT of a range, and bolding every line of it
// is noise, where a single search hit earns the emphasis.
func (s *styles) selectionStyle(base lipgloss.Style) lipgloss.Style {
	if s.selectionBg != "" {
		return base.Reverse(false).Background(lipgloss.Color(s.selectionBg))
	}
	return base.Reverse(!base.GetReverse())
}
```

- [ ] **Step 9: Run the tests to verify they pass**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-line-select-copy && go test ./internal/theme/... && go test ./internal/config/...`
Expected: PASS. (`override_test.go`'s structural-parity check picks the new
field up automatically; `internal/config` has no golden that lists theme roles —
verified: `grep -rn 'selection_bg\|search_current_bg' internal e2e` finds only
the source and one test message.)

Run: `cd /mnt/t/others/gigagit.worktrees/feat-line-select-copy && go test ./internal/tui/ -run 'TestLineSel|TestSelectionStyle|TestSelectionRoleValues|TestThemeEditorListsEveryRole' -v`
Expected: PASS.

- [ ] **Step 10: Run the whole TUI package and the formatters**

Run (FOREGROUND, Bash timeout 600000):
`cd /mnt/t/others/gigagit.worktrees/feat-line-select-copy && go test ./internal/tui/...`
Expected: PASS.

Run: `cd /mnt/t/others/gigagit.worktrees/feat-line-select-copy && gofmt -l internal && go vet ./internal/tui/... ./internal/theme/...`
Expected: no output.

- [ ] **Step 11: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-line-select-copy && cat > /tmp/gg-commit-msg-1.txt <<'EOF'
feat(tui): lineSel, the selection_bg theme role and the stripe style

The pure leaf the three readers' line selection is built on: lineSel
(space starts, space freezes, a third space starts over; bounds follows
the cursor until it is frozen) plus styles.selectionStyle, which paints a
stripe relative to the row it lands on exactly as the search's current hit
does — the new theme role selection_bg as an explicit background patch
(dark #264F78, light #ADD6FF, the editor-selection blues), or a flip of
reverse video when the role is unset, which punches a hole in blame's
reverse-video cursor row. Roles go 54 -> 55.

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01NDk1DJtgLzZX7hMmxDs9nU
EOF
git -C /mnt/t/others/gigagit.worktrees/feat-line-select-copy add \
  internal/tui/lineselect.go internal/tui/lineselect_test.go \
  internal/tui/selection_style_test.go internal/tui/styles.go \
  internal/tui/theme_editor_popup_test.go \
  internal/theme/theme.go internal/theme/override.go internal/theme/roles_test.go
git -C /mnt/t/others/gigagit.worktrees/feat-line-select-copy commit -F /tmp/gg-commit-msg-1.txt
```

---

### Task 2: The diff cursor sits on ONE side

`alt+←`/`alt+→` move the cursor between the two panes of the same aligned row;
the marker paints only that side's cell (gap cells included, in `row` mode);
the header names that side first; the review-note anchor list — and therefore
the note popup's default pick, `noteAnchorAtCursor`, `L` and the `.` menu's
Copy link — puts it first; a left click picks the pane it landed in. The
`lsel` field lands here too, because `alt+←/→` must already refuse to flip
while a selection is live (Task 3 is what sets it from a key).

The footer and help rows for `alt+←/→` land in **Task 3**, together with
`[spc] mark` and the selection variant: the diff footer is ONE `i18n.T`
literal, and changing it twice would mean deleting and re-adding the same
bundle key in two commits.

**Files:**
- Modify: `internal/tui/diff_view.go:36-108` (the struct), `:793-1052` (the key switch), `:154-164` (`rebuild`), `:1021-1035` (`ctrl+w`)
- Modify: `internal/tui/diff_cursor.go` (append `sidePresent`, `cursorCell`)
- Modify: `internal/tui/diff_render.go:261-282` (the header label), `:367-461` (`diffPaneLines`)
- Modify: `internal/tui/note_keys.go:126-146` (`noteAnchorsAtCursor`)
- Modify: `internal/tui/mouse.go:155-162` (the click branch)
- Modify: `internal/tui/model.go:446-475` (`diffMsg` carry-over)
- Create: `internal/tui/diff_side_test.go`
- Modify: `internal/i18n/lang/{ja,ko,ru,zh}.toml` (two new keys)

**Interfaces:**
- Consumes: `lineSel` (Task 1), `textdiff.Row`, `cursorMark`, `noMark`, `m.attnMarkFor`, `m.overlayDims`, `i18n.T`.
- Produces:
  - `diffView.onOld bool` — false (the new/right side) is the default, so every existing constructor and the `diffViewWith` fixture are correct untouched
  - `diffView.lsel lineSel`
  - `func sidePresent(r textdiff.Row, onOld bool) bool`
  - `func (v *diffView) cursorCell() (text string, no int, ok bool)`
  - `func lockedSideNotice(onOld bool) string`

- [ ] **Step 1: Write the failing side/paint tests**

Create `/mnt/t/others/gigagit.worktrees/feat-line-select-copy/internal/tui/diff_side_test.go`:

```go
package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/textdiff"
	"github.com/homeend/gigagit/internal/theme"
)

// NOTE: TestDiffCursorPaintsOneSide calls lipgloss.SetColorProfile
// (process-global) and therefore does NOT call t.Parallel().

// sideRows is a three-row file that exercises every cell shape: an unchanged
// row (both sides present), an Add row (the LEFT cell is absent — a gap) and a
// Del row (the RIGHT cell is absent).
func sideRows() []textdiff.Row {
	return []textdiff.Row{
		{Kind: textdiff.Same, Left: "same-left", Right: "same-left", LeftNo: 1, RightNo: 1},
		{Kind: textdiff.Add, Right: "added-right", RightNo: 2},
		{Kind: textdiff.Del, Left: "gone-left", LeftNo: 2},
	}
}

// sideModel opens a diff over sideRows with the cursor on the first row.
func sideModel(long longMode) Model {
	m := diffModel()
	m.width, m.height = 100, 8 // body = 6, so all three rows are visible
	v := diffViewWith(sideRows(), []int{1})
	v.long = long
	v.relayout(m.width)
	v.curLine = 0
	m = m.pushLayer(v)
	m.diffTag = "status:x"
	return m
}

// The band must land on the CURSOR side's cell only. The other cell renders as
// if this were not the cursor row. Asserted in all three long-line modes,
// because each has its own cell renderer (diffCell / scrollCell / segCell).
func TestDiffCursorPaintsOneSide(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(prev)
	prevTheme := activeTheme()
	defer setTheme(prevTheme)
	setTheme(theme.Dark) // a theme with a real CursorRowBg, so the band has an SGR

	band := sgrParams(st().diffCursorRow.Render("x"))

	for _, lm := range []longMode{longScroll, longWrap, longTruncate} {
		m := sideModel(lm)
		v := m.diffLayer()

		// Default: the cursor is on the NEW (right) side.
		row := diffBodyRow(t, m, 0)
		if got := sgrBefore(row, "same-left"); subsetOf(band, got) {
			t.Errorf("mode %d: the LEFT cell must not wear the cursor band: %q", lm, row)
		}
		if got := sgrBefore(row, "same-left"); got == nil {
			t.Errorf("mode %d: the left cell text is missing from %q", lm, row)
		}
		// The right cell holds the same text, so find the SECOND occurrence.
		right := row[strings.Index(row, "│"):]
		if got := sgrBefore(right, "same-left"); !subsetOf(band, got) {
			t.Errorf("mode %d: the RIGHT cell must wear the cursor band, params %v: %q", lm, got, right)
		}

		// alt+left flips the side: the band moves to the left cell.
		u, _ := m.Update(keyMsg("alt+left"))
		m = u.(Model)
		if !m.diffLayer().onOld {
			t.Fatalf("mode %d: alt+left must put the cursor on the old side", lm)
		}
		row = diffBodyRow(t, m, 0)
		if got := sgrBefore(row, "same-left"); !subsetOf(band, got) {
			t.Errorf("mode %d: after alt+left the LEFT cell must wear the band, params %v: %q", lm, got, row)
		}
		right = row[strings.Index(row, "│"):]
		if got := sgrBefore(right, "same-left"); subsetOf(band, got) {
			t.Errorf("mode %d: after alt+left the RIGHT cell must not wear the band: %q", lm, right)
		}

		// The GAP cell on the cursor side still wears the band: on the Add row
		// (display row 1) the left side has no line, and the user must still be
		// able to see where the cursor is.
		v = m.diffLayer()
		v.curLine = 1 // the Add row; the cursor is still on the old (left) side
		gapRow := diffBodyRow(t, m, 1)
		gapSeq := sgrBefore(gapRow, "·")
		bandBg := st().diffGapCursor.Render("·")
		if !subsetOf(sgrParams(bandBg), gapSeq) {
			t.Errorf("mode %d: the gap cell on the cursor side must wear the cursor tint, params %v: %q", lm, gapSeq, gapRow)
		}

		// alt+right returns to the new side.
		u, _ = m.Update(keyMsg("alt+right"))
		if u.(Model).diffLayer().onOld {
			t.Fatalf("mode %d: alt+right must put the cursor back on the new side", lm)
		}
	}
}

// The header's line number names the CURSOR side: "line N" on the right,
// "old line N" on the left, falling back to the other side's number (with that
// side's label) when the cursor cell is a gap.
func TestDiffHeaderNamesTheCursorSide(t *testing.T) {
	t.Parallel()
	m := sideModel(longScroll)
	head := strings.SplitN(m.renderDiffView(), "\n", 2)[0]
	if !strings.Contains(head, "line 1") || strings.Contains(head, "old line 1") {
		t.Fatalf("on the new side the header must say `line 1`: %q", head)
	}
	u, _ := m.Update(keyMsg("alt+left"))
	m = u.(Model)
	head = strings.SplitN(m.renderDiffView(), "\n", 2)[0]
	if !strings.Contains(head, "old line 1") {
		t.Fatalf("on the old side the header must say `old line 1`: %q", head)
	}
	// Cursor on the Add row (index 1) with the side still old: the left cell is
	// a gap, so the header falls back to the right side's number and label.
	m.diffLayer().curLine = 1
	head = strings.SplitN(m.renderDiffView(), "\n", 2)[0]
	if !strings.Contains(head, "line 2") || strings.Contains(head, "old line 2") {
		t.Fatalf("a gap cell must fall back to the other side's label: %q", head)
	}
}

// The note anchors — hence the popup's default pick, noteAnchorAtCursor, the L
// key and the . menu's Copy link row — follow the cursor side.
func TestNoteAnchorFollowsCursorSide(t *testing.T) {
	t.Parallel()
	m := notedModel(t)
	v := m.diffLayer()
	v.setCursorLine(10, m.diffBodyRows()) // a Same row: both sides exist

	side, line, _, ok := m.noteAnchorAtCursor()
	if !ok || side != model.NoteSideNew || line != v.lines[10].Row.RightNo {
		t.Fatalf("on the new side the default anchor = %v/%d, want new/%d", side, line, v.lines[10].Row.RightNo)
	}

	v.onOld = true
	side, line, _, ok = m.noteAnchorAtCursor()
	if !ok || side != model.NoteSideOld || line != v.lines[10].Row.LeftNo {
		t.Fatalf("on the old side the default anchor = %v/%d, want old/%d", side, line, v.lines[10].Row.LeftNo)
	}
	// Both sides are still OFFERED, just reordered.
	if as := m.noteAnchorsAtCursor(); len(as) != 2 || as[1].side != model.NoteSideNew {
		t.Fatalf("the picker must still list both sides, new second: %+v", as)
	}
	// The gg:// link the L key copies takes the old side too.
	m.linkRepoName = "gigagit"
	m.currentWorktree = "/wt"
	got, ok := m.contextLinkText()
	if !ok || !strings.Contains(got, ":old:") {
		t.Fatalf("contextLinkText on the old side = %q ok=%v, want an :old: link", got, ok)
	}
}

// A live selection LOCKS the side: alt+←/→ leaves onOld alone and posts the
// bottom-left notice instead, so the range can never mix the two sides' text.
func TestDiffAltArrowsLockedWhileSelecting(t *testing.T) {
	t.Parallel()
	m := sideModel(longScroll)
	v := m.diffLayer()
	v.lsel.start(v.curLine)

	u, _ := m.Update(keyMsg("alt+left"))
	m = u.(Model)
	if m.diffLayer().onOld {
		t.Fatal("alt+left must be ignored while a selection is on")
	}
	if !strings.Contains(m.diffNotice, "locked to the new side") {
		t.Fatalf("diffNotice = %q, want the new-side lock notice", m.diffNotice)
	}

	m.diffLayer().onOld = true
	u, _ = m.Update(keyMsg("alt+right"))
	m = u.(Model)
	if !m.diffLayer().onOld {
		t.Fatal("alt+right must be ignored while a selection is on")
	}
	if !strings.Contains(m.diffNotice, "locked to the old side") {
		t.Fatalf("diffNotice = %q, want the old-side lock notice", m.diffNotice)
	}
}

// A left click places the cursor on the row AND on the pane it landed in;
// while a selection is live only the row moves (the side is locked).
//
// The message shape is the package idiom (action_menu_click_test.go:49):
// handleMouse returns immediately unless msg.Action == tea.MouseActionPress,
// and then dispatches on msg.Button — it never reads a Type field.
func TestDiffClickPicksThePane(t *testing.T) {
	t.Parallel()
	m := sideModel(longScroll)
	paneW := (m.width - 1) / 2

	u, _ := m.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 3, Y: 1})
	m = u.(Model)
	if !m.diffLayer().onOld {
		t.Fatal("a click in the left pane must put the cursor on the old side")
	}
	u, _ = m.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: paneW + 5, Y: 1})
	m = u.(Model)
	if m.diffLayer().onOld {
		t.Fatal("a click in the right pane must put the cursor on the new side")
	}

	m.diffLayer().lsel.start(0)
	u, _ = m.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 3, Y: 3})
	m = u.(Model)
	if m.diffLayer().onOld {
		t.Fatal("while a selection is on the click must not flip the side")
	}
	if m.diffLayer().curLine != 2 {
		t.Fatalf("the click must still move the row, curLine = %d want 2", m.diffLayer().curLine)
	}
}

// A reload (diffMsg) must not flip the user's side out from under them: the
// side is carried across `*dv = *msg.view` exactly like the search is.
func TestDiffMsgCarriesTheCursorSide(t *testing.T) {
	t.Parallel()
	m := sideModel(longScroll)
	m.diffLayer().onOld = true
	m.diffLayer().lsel.start(0)

	fresh := diffViewWith(sideRows(), []int{1})
	u, _ := m.Update(diffMsg{tag: m.diffTag, view: fresh})
	m = u.(Model)
	if !m.diffLayer().onOld {
		t.Fatal("a reload must keep the cursor on the side the user chose")
	}
	if m.diffLayer().lsel.on {
		t.Fatal("a reload replaces the line stream: the selection must be gone")
	}
}
```

Two helpers this file needs (put them at the bottom of the same file — they are
new, and no sibling defines them; run `grep -rn 'func diffBodyRow\|func subsetOf' internal/tui` before writing and reuse anything that already exists
rather than redefining it):

```go
// diffBodyRow renders the diff and returns body row i (row 0 of the panes,
// which is screen line 1 — line 0 is the header).
func diffBodyRow(t *testing.T, m Model, i int) string {
	t.Helper()
	lines := strings.Split(m.renderDiffView(), "\n")
	if i+1 >= len(lines) {
		t.Fatalf("body row %d is past the render (%d lines)", i, len(lines))
	}
	return lines[i+1]
}

// subsetOf reports whether every parameter of want is present in got. SGR
// sequences pack several attributes together, so an exact-set comparison would
// break the moment an unrelated attribute joins the run.
func subsetOf(want, got map[string]bool) bool {
	if got == nil || len(want) == 0 {
		return false
	}
	for k := range want {
		if !got[k] {
			return false
		}
	}
	return true
}
```

`activeTheme() theme.Theme` and `setTheme(theme.Theme)` are package-level
helpers in `internal/tui/styles.go:259-262` — verified present, no new helper
needed. The Terminal theme leaves `CursorRowBg` to `legacy`'s fallback, which
still paints, but `theme.Dark` makes the assertion unambiguous.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-line-select-copy && go test ./internal/tui/ -run 'TestDiffCursorPaintsOneSide|TestDiffHeaderNamesTheCursorSide|TestNoteAnchorFollowsCursorSide|TestDiffAltArrowsLockedWhileSelecting|TestDiffClickPicksThePane|TestDiffMsgCarriesTheCursorSide' -v`
Expected: FAIL to COMPILE — `v.onOld undefined`, `v.lsel undefined`.

- [ ] **Step 3: Add the two fields to `diffView`**

In `/mnt/t/others/gigagit.worktrees/feat-line-select-copy/internal/tui/diff_view.go` the
struct reads:

```go
	cur        int         // focused change-block index (the "X" in change X/N); set only by n/p/wrap/mode-toggle
	curLine    int         // cursor: index into lines (never a display row; never a fold) — see diff_cursor.go
	wrapArm    wrapDir     // boundary press primed a wrap-around (see wrapDir); cleared on any other key
```

Replace with:

```go
	cur        int         // focused change-block index (the "X" in change X/N); set only by n/p/wrap/mode-toggle
	curLine    int         // cursor: index into lines (never a display row; never a fold) — see diff_cursor.go
	// onOld is the SIDE the line cursor sits on: false = the new (right) pane,
	// which is where a view opens. alt+←/→ flip it (spec §4.7). It decides
	// which cell wears the marker, which text Copy line / a selection copies,
	// which number the header names, and which review-note anchor comes first.
	// false as the zero value is deliberate: every constructor and the
	// diffViewWith test fixture get the right default with no edit.
	onOld bool
	// lsel is the line selection over v.lines (space/space/enter). Cleared by
	// rebuild(), by ctrl+w's relayout and by a diffMsg reload — anything that
	// changes what a line index MEANS — but never by a resize or a search key.
	lsel       lineSel
	wrapArm    wrapDir     // boundary press primed a wrap-around (see wrapDir); cleared on any other key
```

- [ ] **Step 4: Add the side helpers**

Append to `/mnt/t/others/gigagit.worktrees/feat-line-select-copy/internal/tui/diff_cursor.go`
(after `revealCursorNotes`, at the end of the file):

```go
// sidePresent reports whether the given side of an aligned row actually has a
// line there. An Add row has no old line and a Del row has no new one; those
// cells render as the dotted gap filler, carry no line number, are hidden from
// the copy rows and are skipped by a selection.
func sidePresent(r textdiff.Row, onOld bool) bool {
	if onOld {
		return r.Kind != textdiff.Add
	}
	return r.Kind != textdiff.Del
}

// cursorCell is the SOURCE text under the cursor on the cursor's side, plus
// that side's 1-based line number. ok is false when the view has no cursor row
// (empty stream, or the cursor is on a fold) or when the cursor side is absent
// on that row — the gap-cell case, where there is nothing to copy.
func (v *diffView) cursorCell() (text string, no int, ok bool) {
	r, has := v.cursorRow()
	if !has || !sidePresent(r, v.onOld) {
		return "", 0, false
	}
	if v.onOld {
		return r.Left, r.LeftNo, true
	}
	return r.Right, r.RightNo, true
}
```

- [ ] **Step 5: Bind `alt+←`/`alt+→` and clear the selection on a rebuild**

In `/mnt/t/others/gigagit.worktrees/feat-line-select-copy/internal/tui/diff_view.go` the key
switch has:

```go
	case "up":
		v.scrollBy(-1, body)
	case "down":
		v.scrollBy(1, body)
```

Insert BEFORE `case "up":`:

```go
	case "alt+left", "alt+right":
		// The cursor sits on one pane; these move it across the SAME aligned
		// row. Plain ←/→ keep panning in scroll mode.
		if v.lsel.on {
			// A selection is locked to the side it started on (the user's
			// ruling): a range that mixed the two sides' text would copy
			// nonsense. Say so instead of silently doing nothing.
			m.diffNotice = lockedSideNotice(v.onOld)
			return m, nil
		}
		v.onOld = msg.String() == "alt+left"
```

In the same file, `rebuild()` reads:

```go
func (v *diffView) rebuild() {
	v.sanLeft, v.sanRight = nil, nil // the line stream is about to change
```

Replace those two lines with:

```go
func (v *diffView) rebuild() {
	v.sanLeft, v.sanRight = nil, nil // the line stream is about to change
	v.lsel.clear()                   // …and so do the line indexes it holds
```

`ctrl+w` calls `relayout` directly (it changes the DISPLAY stream, not the
logical one) — but it re-anchors the cursor by source numbers afterwards, so a
frozen range would end up pointing at other lines. The case reads:

```go
	case "ctrl+w":
		ord := v.currentBlockOrdinal()
		cr, hadRow := v.cursorRow()
```

Replace with:

```go
	case "ctrl+w":
		v.lsel.clear() // relayout + reanchor moves what a line index means
		ord := v.currentBlockOrdinal()
		cr, hadRow := v.cursorRow()
```

- [ ] **Step 6: Add the lock notice**

Create `/mnt/t/others/gigagit.worktrees/feat-line-select-copy/internal/tui/diff_select.go` with
just the notice for now (Task 3 grows the file):

```go
package tui

import (
	"github.com/homeend/gigagit/internal/i18n"
)

// lockedSideNotice names the side a live selection is pinned to. alt+←/→ post
// it instead of flipping: the range copies ONE side's text, so the lock is the
// contract, not a limitation — and the notice names the way out (esc).
func lockedSideNotice(onOld bool) string {
	if onOld {
		return i18n.T("▸ selection locked to the old side — esc clears it")
	}
	return i18n.T("▸ selection locked to the new side — esc clears it")
}
```

- [ ] **Step 7: Paint the marker on one side only**

In `/mnt/t/others/gigagit.worktrees/feat-line-select-copy/internal/tui/diff_render.go`,
`diffPaneLines`'s per-row mark block reads:

```go
		mk := noMark()
		r := dr.row
		switch {
		case i >= curStart && i < curEnd:
			// The cursor outranks an attention band: the user must always be
			// able to see where they are. In "number" mode, though, only the
			// GUTTER carries the cursor (mk.row is false), so the row body is
			// free — and the row under the cursor is precisely the one the user
			// is most likely to be reading. Keep the cursor gutter, take the
			// band for the body.
			mk = cursorMark(style)
			if !mk.row {
				if bg, ok := m.attnMarkFor(v, r); ok {
					mk = cellMark{row: true, attn: true, base: bg, gut: mk.gut}
				}
			}
		default:
			if bg, ok := m.attnMarkFor(v, r); ok {
				mk = cellMark{row: true, attn: true, base: bg, gut: s.diffGutter}
			}
		}
```

Replace it with:

```go
		r := dr.row
		// TWO marks, one per pane: the cursor sits on ONE side (v.onOld), and
		// the other cell must render as if this were not the cursor row — its
		// own attention band, if any, wins there. Start both from the band (or
		// nothing), then overwrite the cursor side.
		mkL, mkR := noMark(), noMark()
		if bg, ok := m.attnMarkFor(v, r); ok {
			band := cellMark{row: true, attn: true, base: bg, gut: s.diffGutter}
			mkL, mkR = band, band
		}
		if i >= curStart && i < curEnd {
			// The cursor outranks an attention band: the user must always be
			// able to see where they are. In "number" mode, though, only the
			// GUTTER carries the cursor (cm.row is false), so the row body is
			// free — and the row under the cursor is precisely the one the user
			// is most likely to be reading. Keep the cursor gutter, take the
			// band for the body. "off" lands here too, and keeps the band.
			cm := cursorMark(style)
			if !cm.row {
				if bg, ok := m.attnMarkFor(v, r); ok {
					cm = cellMark{row: true, attn: true, base: bg, gut: cm.gut}
				}
			}
			if v.onOld {
				mkL = cm
			} else {
				mkR = cm
			}
		}
```

Then replace the three per-mode blocks' `mk` arguments with `mkL` for the left
cell and `mkR` for the right. The wrap case reads:

```go
			left := segCell(leftNo, dr.left, gut, paneW, leftGap,
				r.Kind == textdiff.Del || r.Kind == textdiff.Changed, s.diffDelCell, mk, lh)
			right := segCell(rightNo, dr.right, gut, paneW, rightGap,
				r.Kind == textdiff.Add || r.Kind == textdiff.Changed, s.diffAddCell, mk, rh)
```

→

```go
			left := segCell(leftNo, dr.left, gut, paneW, leftGap,
				r.Kind == textdiff.Del || r.Kind == textdiff.Changed, s.diffDelCell, mkL, lh)
			right := segCell(rightNo, dr.right, gut, paneW, rightGap,
				r.Kind == textdiff.Add || r.Kind == textdiff.Changed, s.diffAddCell, mkR, rh)
```

The truncate case reads:

```go
			left := diffCell(r.LeftNo, r.Left, gut, paneW,
				r.Kind == textdiff.Add,
				r.Kind == textdiff.Del || r.Kind == textdiff.Changed, s.diffDelCell, r.LeftSpans, lt, mk, lh)
			right := diffCell(r.RightNo, r.Right, gut, paneW,
				r.Kind == textdiff.Del,
				r.Kind == textdiff.Add || r.Kind == textdiff.Changed, s.diffAddCell, r.RightSpans, rt, mk, rh)
```

→ the same with `mk` → `mkL` on the `left :=` line and `mk` → `mkR` on the
`right :=` line. The scroll case reads:

```go
			left := scrollCell(r.LeftNo, r.Left, r.LeftSpans, lt, v.hOffset, gut, paneW,
				r.Kind == textdiff.Add,
				r.Kind == textdiff.Del || r.Kind == textdiff.Changed, s.diffDelCell, mk, lh)
			right := scrollCell(r.RightNo, r.Right, r.RightSpans, rt, v.hOffset, gut, paneW,
				r.Kind == textdiff.Del,
				r.Kind == textdiff.Add || r.Kind == textdiff.Changed, s.diffAddCell, mk, rh)
```

→ likewise `mkL` / `mkR`.

Finally update the function's doc comment. It reads:

```go
// diffPaneLines renders the visible window of display rows. A note dRow is a
// full-width review-note row; a fold dRow is a full-width separator. Otherwise: wrap off draws the row via diffCell (raw
// text, truncated — byte-identical to before); wrap on draws each side's
// pre-wrapped segment via segCell. Display rows in [curStart, curEnd) carry
// the cursor marker per style ("row" | "number" | "off"); curStart == curEnd
// (the history pane) draws no marker. A fold row is never marked.
```

→

```go
// diffPaneLines renders the visible window of display rows. A note dRow is a
// full-width review-note row; a fold dRow is a full-width separator. Otherwise: wrap off draws the row via diffCell (raw
// text, truncated — byte-identical to before); wrap on draws each side's
// pre-wrapped segment via segCell. Display rows in [curStart, curEnd) carry
// the cursor marker per style ("row" | "number" | "off"); curStart == curEnd
// (the history pane) draws no marker. A fold row is never marked.
//
// The marker lands on ONE cell: the cursor sits on v.onOld's side (spec §4.7),
// so each row builds two marks and the non-cursor cell renders as if this were
// not the cursor row — its attention band, if any, wins there. The history
// pane's embedded diffView has onOld false and an empty cursor range, so it is
// unaffected.
```

- [ ] **Step 8: Name the cursor side in the header**

In the same file, `renderDiffView` reads:

```go
	if r, ok := v.cursorRow(); ok {
		ln := ""
		if r.RightNo > 0 {
			ln = i18n.T("line %d", r.RightNo)
		} else if r.LeftNo > 0 {
			ln = i18n.T("old line %d", r.LeftNo)
		}
```

Replace with:

```go
	if r, ok := v.cursorRow(); ok {
		// The cursor sits on ONE side: name that side first, and fall back to
		// the other side's number WITH ITS OWN LABEL on a gap cell — the same
		// fallback the single-sided version performed, mirrored.
		ln := ""
		if v.onOld {
			if r.LeftNo > 0 {
				ln = i18n.T("old line %d", r.LeftNo)
			} else if r.RightNo > 0 {
				ln = i18n.T("line %d", r.RightNo)
			}
		} else if r.RightNo > 0 {
			ln = i18n.T("line %d", r.RightNo)
		} else if r.LeftNo > 0 {
			ln = i18n.T("old line %d", r.LeftNo)
		}
```

(Both literals already exist in all four bundles — no bundle change here.)

- [ ] **Step 9: Order the note anchors by the cursor side**

In `/mnt/t/others/gigagit.worktrees/feat-line-select-copy/internal/tui/note_keys.go`,
`noteAnchorsAtCursor` reads:

```go
// noteAnchorsAtCursor lists the anchors the cursor row offers: the new side
// first (the default), then the old side — both when the row exists in both
// versions (a Same or Changed row), one on an Add or Del row. The user picks
// between them in the popup; nothing when the view has no cursor row.
func (m Model) noteAnchorsAtCursor() []noteAnchor {
	v := m.diffLayer()
	if v == nil {
		return nil
	}
	r, ok := v.cursorRow()
	if !ok {
		return nil
	}
	var out []noteAnchor
	if r.RightNo > 0 {
		out = append(out, noteAnchor{model.NoteSideNew, r.RightNo, model.NoteContextHash([]string{r.Right})})
	}
	// A preview's old side is the MERGE BASE, which no stored address names,
	// so it offers no anchor at all — the same rule `gg review A..B` follows
	// (reviewImportTarget: ranges anchor to the tip, new side only).
	if r.LeftNo > 0 && v.previewSet == nil {
		out = append(out, noteAnchor{model.NoteSideOld, r.LeftNo, model.NoteContextHash([]string{r.Left})})
	}
	return out
}
```

Replace the whole function with:

```go
// noteAnchorsAtCursor lists the anchors the cursor row offers — both when the
// row exists in both versions (a Same or Changed row), one on an Add or Del
// row. The CURSOR SIDE comes first (spec §4.7), so the popup's default pick,
// noteAnchorAtCursor and everything built on it (L, the . menu's Copy link)
// follow the side the user is reading; the picker still lists both. Nothing
// when the view has no cursor row.
func (m Model) noteAnchorsAtCursor() []noteAnchor {
	v := m.diffLayer()
	if v == nil {
		return nil
	}
	r, ok := v.cursorRow()
	if !ok {
		return nil
	}
	newSide := func() (noteAnchor, bool) {
		if r.RightNo <= 0 {
			return noteAnchor{}, false
		}
		return noteAnchor{model.NoteSideNew, r.RightNo, model.NoteContextHash([]string{r.Right})}, true
	}
	// A preview's old side is the MERGE BASE, which no stored address names,
	// so it offers no anchor at all — the same rule `gg review A..B` follows
	// (reviewImportTarget: ranges anchor to the tip, new side only). That holds
	// whichever side the cursor is on.
	oldSide := func() (noteAnchor, bool) {
		if r.LeftNo <= 0 || v.previewSet != nil {
			return noteAnchor{}, false
		}
		return noteAnchor{model.NoteSideOld, r.LeftNo, model.NoteContextHash([]string{r.Left})}, true
	}
	first, second := newSide, oldSide
	if v.onOld {
		first, second = oldSide, newSide
	}
	var out []noteAnchor
	if a, ok := first(); ok {
		out = append(out, a)
	}
	if a, ok := second(); ok {
		out = append(out, a)
	}
	return out
}
```

- [ ] **Step 10: Pick the pane on a click**

In `/mnt/t/others/gigagit.worktrees/feat-line-select-copy/internal/tui/mouse.go` the diff
click branch reads:

```go
			if msg.Button == tea.MouseButtonLeft && m.actionMenu == nil && msg.Y >= 1 && msg.Y <= m.diffBodyRows() {
				dv.setCursorDisp(dv.offset+msg.Y-1, m.diffBodyRows())
			}
```

Replace with:

```go
			if msg.Button == tea.MouseButtonLeft && m.actionMenu == nil && msg.Y >= 1 && msg.Y <= m.diffBodyRows() {
				dv.setCursorDisp(dv.offset+msg.Y-1, m.diffBodyRows())
				// …and on the PANE the click landed in. renderDiffView pads
				// every line to overlayDims' full width and the layer draws it
				// at 0,0, so msg.X is the pane column directly; paneW mirrors
				// diffPaneLines exactly. A click on the │ separator leaves the
				// side alone, and a live selection is locked to its side, so
				// only the row moves then.
				if !dv.lsel.on {
					w, _ := m.overlayDims()
					paneW := (w - 1) / 2
					if paneW < 4 {
						paneW = 4
					}
					switch {
					case msg.X < paneW:
						dv.onOld = true
					case msg.X > paneW:
						dv.onOld = false
					}
				}
			}
```

- [ ] **Step 11: Carry the side across a reload**

In `/mnt/t/others/gigagit.worktrees/feat-line-select-copy/internal/tui/model.go` the `diffMsg`
case reads:

```go
		search, searchOrig := dv.search, dv.searchOrig
		*dv = *msg.view
		dv.loading = false
		dv.compare = dv.compare || compare
		dv.search, dv.searchOrig = search, searchOrig
```

Replace with:

```go
		search, searchOrig := dv.search, dv.searchOrig
		// The SIDE is the user's choice, not the loader's: a reload of the same
		// file must not throw them back to the right pane. The SELECTION is the
		// opposite — the line stream is brand new, so the indexes it holds mean
		// nothing any more.
		onOld := dv.onOld
		*dv = *msg.view
		dv.loading = false
		dv.compare = dv.compare || compare
		dv.search, dv.searchOrig = search, searchOrig
		dv.onOld = onOld
		dv.lsel.clear()
```

- [ ] **Step 12: Add the two bundle keys**

Append these lines at the END of the `[strings]` table in each bundle
(`internal/i18n/lang/<lang>.toml`):

`ja.toml`:
```toml
"▸ selection locked to the old side — esc clears it" = "▸ 選択は旧側に固定されています — esc で解除"
"▸ selection locked to the new side — esc clears it" = "▸ 選択は新側に固定されています — esc で解除"
```

`ko.toml`:
```toml
"▸ selection locked to the old side — esc clears it" = "▸ 선택이 이전 쪽에 고정됨 — esc 로 해제"
"▸ selection locked to the new side — esc clears it" = "▸ 선택이 새 쪽에 고정됨 — esc 로 해제"
```

`ru.toml`:
```toml
"▸ selection locked to the old side — esc clears it" = "▸ выделение закреплено за старой стороной — esc снимает его"
"▸ selection locked to the new side — esc clears it" = "▸ выделение закреплено за новой стороной — esc снимает его"
```

`zh.toml`:
```toml
"▸ selection locked to the old side — esc clears it" = "▸ 选择已锁定到旧侧 — esc 清除"
"▸ selection locked to the new side — esc clears it" = "▸ 选择已锁定到新侧 — esc 清除"
```

- [ ] **Step 13: Run the new tests, then the whole package**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-line-select-copy && go test ./internal/tui/ -run 'TestDiffCursorPaintsOneSide|TestDiffHeaderNamesTheCursorSide|TestNoteAnchorFollowsCursorSide|TestDiffAltArrowsLockedWhileSelecting|TestDiffClickPicksThePane|TestDiffMsgCarriesTheCursorSide|TestI18n' -v`
Expected: PASS.

Run (FOREGROUND, Bash timeout 600000):
`cd /mnt/t/others/gigagit.worktrees/feat-line-select-copy && go test ./internal/tui/...`
Expected: PASS. Two existing tests deserve a look if they fail:
`TestAddNoteFormOffersTheSideOnTwoSidedRows` (note_keys_test.go) and
`TestContextLinkTextOldSide` (link_test.go) both assume the DEFAULT side, which
is still the new one — they must pass unchanged. If either fails, the default
was flipped by mistake.

Run: `cd /mnt/t/others/gigagit.worktrees/feat-line-select-copy && gofmt -l internal && go vet ./internal/tui/...`
Expected: no output.

- [ ] **Step 14: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-line-select-copy && cat > /tmp/gg-commit-msg-2.txt <<'EOF'
feat(tui): the diff line cursor sits on one side

alt+left / alt+right move the cursor between the two panes of the same
aligned row, and the marker now paints only that side's cell — including a
GAP cell in row mode, so the cursor is visible even where the side has no
line, while the other cell renders as if this were not the cursor row (its
attention band wins there). The header names the cursor side first
("old line N" on the left) and falls back to the other side's number with
its own label on a gap. The review-note anchors are reordered so the
popup's default pick, the L key and the . menu's Copy link follow the
side; the picker still lists both. A left click picks the pane it landed
in. A reload carries the side over; it clears the selection, which a
rebuild and ctrl+w do too. alt+left/right are refused while a line
selection is live, with a bottom-left notice naming the locked side.

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01NDk1DJtgLzZX7hMmxDs9nU
EOF
git -C /mnt/t/others/gigagit.worktrees/feat-line-select-copy add \
  internal/tui/diff_view.go internal/tui/diff_cursor.go internal/tui/diff_select.go \
  internal/tui/diff_render.go internal/tui/note_keys.go internal/tui/mouse.go \
  internal/tui/model.go internal/tui/diff_side_test.go \
  internal/i18n/lang/ja.toml internal/i18n/lang/ko.toml \
  internal/i18n/lang/ru.toml internal/i18n/lang/zh.toml
git -C /mnt/t/others/gigagit.worktrees/feat-line-select-copy commit -F /tmp/gg-commit-msg-2.txt
```

---

### Task 3: The diff view's line selection

`space` marks, `space` again freezes, `enter` copies and clears, `esc` clears.
The `.` menu grows `Copy line` / `Copy line (old side)` and
`Copy selected lines (N)`, and `enter` RUNS the selected-lines row, so a test
reads the exact clipboard payload off `row.copyText`. The selected cells wear
the stripe on the cursor side only. The footer gains `[spc] mark` and
`[alt↔] side` and switches to a selection variant while a range is live.

**Files:**
- Modify: `internal/tui/diff_select.go` (grow the file Task 2 created)
- Modify: `internal/tui/diff_cursor.go` (append `selectedLines`)
- Modify: `internal/tui/diff_view.go:793-815` (the key preamble)
- Modify: `internal/tui/diff_render.go:26-50` (`cellMark`), `:84-114` (`diffHintFor`), `:338` (the hint line), `:367-461` (the stripe), `:467-506` (`segCell`), `:508-583` (`scrollCell`), `:679-721` (`diffCell`)
- Modify: `internal/tui/action_menu.go:524-531` (`rowHasID`, where `rowByID` joins it), `:569-571` (the diff branch of `contextCopyRows`)
- Modify: `internal/tui/help.go:260-288` (the Diff view section)
- Modify: `internal/tui/diff_cursor_test.go` (`TestDiffHintAdvertisesCursorKeys`), `internal/tui/diff_search_test.go` (`TestDiffHintFitsTheBudget`), `internal/tui/action_menu_click_test.go:148`
- Create: `internal/tui/diff_select_test.go`
- Modify: `internal/i18n/lang/{ja,ko,ru,zh}.toml`

**Interfaces:**
- Consumes: `lineSel` (Task 1), `diffView.onOld` / `.lsel`, `sidePresent`, `cursorCell` (Task 2), `m.copyRow`, `st().selectionStyle`.
- Produces:
  - `func rowByID(rows []actionRow, id string) (actionRow, bool)`
  - `func (v *diffView) selectedLines() []string`
  - `func (m Model) diffCopyLineRows() []actionRow` — ids `copy-line`, `copy-selected-lines`
  - `func (m Model) diffSelectKey(v *diffView, msg tea.KeyMsg) (Model, tea.Cmd, bool)`
  - `func diffSelectHint() string`
  - `cellMark.sel bool`, `func (mk cellMark) bodyFor(base lipgloss.Style) lipgloss.Style`

- [ ] **Step 1: Write the failing selection tests**

Create `/mnt/t/others/gigagit.worktrees/feat-line-select-copy/internal/tui/diff_select_test.go`:

```go
package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/homeend/gigagit/internal/textdiff"
)

// NOTE: TestDiffSelectionPaintsOnlyTheCursorSide calls lipgloss.SetColorProfile
// (process-global) and therefore does NOT call t.Parallel().

// selRow finds an action row by id in the . menu's context copy rows.
func selRow(t *testing.T, m Model, id string) actionRow {
	t.Helper()
	r, ok := rowByID(m.contextCopyRows(), id)
	if !ok {
		var ids []string
		for _, x := range m.contextCopyRows() {
			ids = append(ids, x.id)
		}
		t.Fatalf("row %q not offered; rows = %v", id, ids)
	}
	return r
}

// space / move / space / move / enter copies exactly the FROZEN range — the
// second space fixes the end and the cursor is free afterwards. The payload is
// read off the action row the enter key runs, so the test sees the real text.
func TestDiffSelectionCopiesTheFrozenRange(t *testing.T) {
	t.Parallel()
	m := openedDiffModel(12, cursorRows(40), nil) // 40 Same rows: Left "l", Right "r"
	m.diffLayer().setCursorLine(5, m.diffBodyRows())

	m = feedDiff(m, "space", "j", "j", "space", "j", "j") // range 5..7, cursor now 9
	v := m.diffLayer()
	if !v.lsel.on || !v.lsel.fixed {
		t.Fatalf("after two spaces the range must be frozen: %+v", v.lsel)
	}
	if v.curLine != 9 {
		t.Fatalf("the cursor must be free after the freeze, curLine = %d want 9", v.curLine)
	}
	row := selRow(t, m, "copy-selected-lines")
	if row.label != "Copy selected lines (3)" {
		t.Fatalf("label = %q, want `Copy selected lines (3)`", row.label)
	}
	if row.copyText != "r\nr\nr" {
		t.Fatalf("copyText = %q, want the three new-side lines joined with \\n and no trailing newline", row.copyText)
	}

	u, cmd := m.Update(keyMsg("enter"))
	m = u.(Model)
	if cmd == nil {
		t.Fatal("enter with a selection on must issue the clipboard command")
	}
	if m.diffLayer().lsel.on {
		t.Fatal("enter must clear the selection after copying")
	}
}

// A ONE-space selection copies anchor..cursor, the way tmux does.
func TestDiffOneSpaceSelectionFollowsTheCursor(t *testing.T) {
	t.Parallel()
	m := openedDiffModel(12, cursorRows(40), nil)
	m.diffLayer().setCursorLine(2, m.diffBodyRows())
	m = feedDiff(m, "space", "j")
	if got := selRow(t, m, "copy-selected-lines").copyText; got != "r\nr" {
		t.Fatalf("copyText = %q, want two lines (anchor..cursor)", got)
	}
}

// The range takes the CURSOR SIDE's text and skips a cell that side does not
// have: on the new side a Del row contributes nothing, on the old side an Add
// row does not.
func TestDiffSelectionSkipsAbsentCells(t *testing.T) {
	t.Parallel()
	m := sideModel(longScroll) // Same, Add, Del
	v := m.diffLayer()
	v.curLine = 0
	m = feedDiff(m, "space")
	v.curLine = 2 // loose range 0..2

	if got := selRow(t, m, "copy-selected-lines").copyText; got != "same-left\nadded-right" {
		t.Fatalf("new side copyText = %q, want the Same and Add rows only", got)
	}
	if lbl := selRow(t, m, "copy-selected-lines").label; lbl != "Copy selected lines (2)" {
		t.Fatalf("label = %q, want a count of 2 (the Del row has no new cell)", lbl)
	}

	v.onOld = true
	if got := selRow(t, m, "copy-selected-lines").copyText; got != "same-left\ngone-left" {
		t.Fatalf("old side copyText = %q, want the Same and Del rows only", got)
	}
}

// A range that crosses a collapsed FOLD copies only the visible lines (the
// user's ruling): a fold entry is a separator, not a line.
func TestDiffSelectionSkipsFolds(t *testing.T) {
	t.Parallel()
	m := openedDiffModel(12, cursorRows(40, 4, 24), []int{4, 24})
	v := m.diffLayer()
	v.partial = true
	v.rebuild() // partial mode collapses the unchanged runs into fold entries

	folds := 0
	for _, ln := range v.lines {
		if ln.Fold > 0 {
			folds++
		}
	}
	if folds == 0 {
		t.Fatal("the fixture must produce fold entries in partial mode")
	}
	// Line 0 of a partial stream is usually the LEADING fold; snap off it so the
	// fixture does not encode an assumption about the stream's shape.
	v.curLine = v.snapOffFold(0)
	v.lsel.start(0)
	v.lsel.mark(len(v.lines) - 1) // the whole stream

	got := v.selectedLines()
	if len(got) != len(v.lines)-folds {
		t.Fatalf("copied %d lines over a stream of %d with %d folds; folds must be skipped", len(got), len(v.lines), folds)
	}
	for _, s := range got {
		if s != "r" && s != "y" {
			t.Fatalf("an unexpected line %q leaked into the copy — only the new side's text belongs there", s)
		}
	}
}

// A range with NOTHING copyable on this side is a no-op with a notice, not a
// clipboard command carrying an empty string.
func TestDiffSelectionWithNothingOnThisSideNotices(t *testing.T) {
	t.Parallel()
	m := sideModel(longScroll)
	v := m.diffLayer()
	v.onOld = true
	v.curLine = 1 // the Add row: the old side is a gap
	v.lsel.start(1)
	v.lsel.mark(1)

	if _, ok := rowByID(m.contextCopyRows(), "copy-selected-lines"); ok {
		t.Fatal("an empty range must offer no Copy selected lines row")
	}
	u, cmd := m.Update(keyMsg("enter"))
	m = u.(Model)
	if cmd != nil {
		t.Fatal("enter over an empty range must not touch the clipboard")
	}
	if !strings.Contains(m.diffNotice, "nothing to copy") {
		t.Fatalf("diffNotice = %q, want the nothing-to-copy notice", m.diffNotice)
	}
	if m.diffLayer().lsel.on {
		t.Fatal("enter must clear the selection either way")
	}
}

// esc clears the selection and nothing else; the SECOND esc closes the view.
func TestDiffEscClearsSelectionBeforeClosing(t *testing.T) {
	t.Parallel()
	m := openedDiffModel(12, cursorRows(40), nil)
	m = feedDiff(m, "space")
	u, _ := m.Update(keyMsg("esc"))
	m = u.(Model)
	if m.diffLayer() == nil {
		t.Fatal("the first esc must clear the selection, not close the view")
	}
	if m.diffLayer().lsel.on {
		t.Fatal("the first esc must clear the selection")
	}
	u, _ = m.Update(keyMsg("esc"))
	if u.(Model).diffLayer() != nil {
		t.Fatal("the second esc must close the view")
	}
}

// f (the partial toggle) rebuilds the line stream, so the indexes the range
// holds mean nothing any more: the selection goes.
func TestDiffRebuildClearsSelection(t *testing.T) {
	t.Parallel()
	m := openedDiffModel(12, cursorRows(40, 4, 24), []int{4, 24})
	m = feedDiff(m, "space")
	if !m.diffLayer().lsel.on {
		t.Fatal("space must start a selection")
	}
	m = feedDiff(m, "f")
	if m.diffLayer().lsel.on {
		t.Fatal("the partial toggle rebuilds the stream: the selection must be gone")
	}
	m = feedDiff(m, "space")
	m = feedDiff(m, "ctrl+w")
	if m.diffLayer().lsel.on {
		t.Fatal("ctrl+w relayouts and re-anchors the cursor: the selection must be gone")
	}
}

// The side stays locked once a range is live, driven through the real keys.
func TestDiffSelectionLockedToSide(t *testing.T) {
	t.Parallel()
	m := sideModel(longScroll)
	m = feedDiff(m, "space", "alt+left")
	if m.diffLayer().onOld {
		t.Fatal("alt+left must not flip the side while a selection is on")
	}
	if !strings.Contains(m.diffNotice, "locked") {
		t.Fatalf("diffNotice = %q, want the lock notice", m.diffNotice)
	}
	m = feedDiff(m, "esc", "alt+left")
	if !m.diffLayer().onOld {
		t.Fatal("with the selection cleared alt+left must flip the side again")
	}
}

// The stripe paints the CURSOR side's cell only, and never the gutter.
func TestDiffSelectionPaintsOnlyTheCursorSide(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(prev)

	for _, lm := range []longMode{longScroll, longWrap, longTruncate} {
		m := sideModel(lm)
		v := m.diffLayer()
		v.curLine = 0
		v.lsel.start(0)
		v.lsel.mark(0)

		stripe := sgrParams(st().selectionStyle(lipgloss.NewStyle()).Render("x"))
		row := diffBodyRow(t, m, 0)
		if got := sgrBefore(row, "same-left"); subsetOf(stripe, got) {
			t.Errorf("mode %d: the LEFT cell must not be striped while the cursor is on the right: %q", lm, row)
		}
		right := row[strings.Index(row, "│"):]
		if got := sgrBefore(right, "same-left"); !subsetOf(stripe, got) {
			t.Errorf("mode %d: the RIGHT cell must be striped, params %v: %q", lm, got, right)
		}

		// The Del row is NOT in the range and must stay unpainted.
		other := diffBodyRow(t, m, 2)
		if got := sgrBefore(other, "gone-left"); subsetOf(stripe, got) {
			t.Errorf("mode %d: a row outside the range must not be striped: %q", lm, other)
		}
	}
}

// The footer swaps to the selection variant while a range is live, so every key
// that matters is advertised and the user is never trapped.
func TestDiffSelectionFooterVariant(t *testing.T) {
	t.Parallel()
	m := openedDiffModel(12, cursorRows(40), nil)
	// Render WIDE: view.go truncates the hint to the terminal width, and both
	// variants are longer than the 80-column default overlayDims falls back to.
	m.width = 160
	base := lastLine(m.renderDiffView())
	if !strings.Contains(base, "[spc] mark") || !strings.Contains(base, "[alt↔] side") {
		t.Fatalf("the base footer must advertise the new keys: %q", base)
	}
	m = feedDiff(m, "space")
	sel := lastLine(m.renderDiffView())
	for _, want := range []string{"[space] mark end", "[enter] copy", "[esc] unmark", "[alt↔] side (locked)"} {
		if !strings.Contains(sel, want) {
			t.Errorf("the selection footer lacks %q: %q", want, sel)
		}
	}
	if lipgloss.Width(diffSelectHint()) > 140 {
		t.Errorf("the selection hint is %d columns, the budget is 140: %q", lipgloss.Width(diffSelectHint()), diffSelectHint())
	}
}

// A Changed row's HOT background survives under the stripe (the stripe is laid
// OVER whatever the cell already wears, not instead of it) — asserted by the
// text still being painted at all.
func TestDiffSelectionOnAChangedRowStillPaints(t *testing.T) {
	prevP := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(prevP)

	rows := []textdiff.Row{{Kind: textdiff.Changed, Left: "old-text", Right: "new-text", LeftNo: 1, RightNo: 1}}
	m := diffModel()
	m.width, m.height = 100, 6
	v := diffViewWith(rows, []int{0})
	v.relayout(m.width)
	v.lsel.start(0)
	v.lsel.mark(0)
	m = m.pushLayer(v)

	row := diffBodyRow(t, m, 0)
	right := row[strings.Index(row, "│"):]
	if sgrBefore(right, "new-text") == nil {
		t.Fatalf("the changed row's new cell is unpainted: %q", right)
	}
}
```

Two helpers this file needs (append them to the same file; `grep -rn 'func feedDiff\|func lastLine' internal/tui` first and reuse if either already exists):

```go
// feedDiff sends keys through the real Update loop (so the space
// normalization and the layer dispatch are exercised).
func feedDiff(m Model, keys ...string) Model {
	for _, k := range keys {
		u, _ := m.Update(keyMsg(k))
		m = u.(Model)
	}
	return m
}

// lastLine is a render's final line — the hint/footer row.
func lastLine(s string) string {
	lines := strings.Split(s, "\n")
	return lines[len(lines)-1]
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-line-select-copy && go test ./internal/tui/ -run 'TestDiffSelection|TestDiffOneSpace|TestDiffEscClears|TestDiffRebuildClears' -v`
Expected: FAIL to COMPILE — `undefined: rowByID`, `v.selectedLines undefined`, `undefined: diffSelectHint`.

- [ ] **Step 3: Add `rowByID`**

In `/mnt/t/others/gigagit.worktrees/feat-line-select-copy/internal/tui/action_menu.go`, after
`rowHasID` (which reads):

```go
func rowHasID(rows []actionRow, id string) bool {
	for _, row := range rows {
		if row.id == id {
			return true
		}
	}
	return false
}
```

add:

```go
// rowByID returns the row with the given id. It is how a KEY runs the very row
// the . menu offers (the contextLinkRow pattern): the key and the menu can then
// never copy different text, and a test can read the payload off row.copyText.
func rowByID(rows []actionRow, id string) (actionRow, bool) {
	for _, row := range rows {
		if row.id == id {
			return row, true
		}
	}
	return actionRow{}, false
}
```

- [ ] **Step 4: Add `selectedLines` to the diff**

Append to `/mnt/t/others/gigagit.worktrees/feat-line-select-copy/internal/tui/diff_cursor.go`,
after `cursorCell`:

```go
// selectedLines is the cursor side's SOURCE text for every logical line the
// selection covers, in stream order. Two entries are skipped, both by the
// user's ruling: a FOLD (a collapsed run is a separator, not a line — "a range
// across a fold copies only the visible lines") and a row where the cursor side
// is ABSENT (an Add row has no old line, a Del row no new one). The result may
// legitimately be empty; the caller decides what to say about that.
func (v *diffView) selectedLines() []string {
	lo, hi, ok := v.lsel.bounds(v.curLine)
	if !ok {
		return nil
	}
	if lo < 0 {
		lo = 0
	}
	if hi > len(v.lines)-1 {
		hi = len(v.lines) - 1
	}
	var out []string
	for i := lo; i <= hi; i++ {
		ln := v.lines[i]
		if ln.Fold > 0 || !sidePresent(ln.Row, v.onOld) {
			continue
		}
		if v.onOld {
			out = append(out, ln.Row.Left)
		} else {
			out = append(out, ln.Row.Right)
		}
	}
	return out
}
```

- [ ] **Step 5: Grow `diff_select.go` with the rows, the key hook and the hint**

`/mnt/t/others/gigagit.worktrees/feat-line-select-copy/internal/tui/diff_select.go` becomes:

```go
package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/i18n"
)

// lockedSideNotice names the side a live selection is pinned to. alt+←/→ post
// it instead of flipping: the range copies ONE side's text, so the lock is the
// contract, not a limitation — and the notice names the way out (esc).
func lockedSideNotice(onOld bool) string {
	if onOld {
		return i18n.T("▸ selection locked to the old side — esc clears it")
	}
	return i18n.T("▸ selection locked to the new side — esc clears it")
}

// diffCopyLineRows are the . menu's line-copy rows for the diff view on top:
// "Copy line" for the cursor cell (named when the cursor sits on the OLD side,
// where the text is not what the file says today) and "Copy selected lines (N)"
// while a range holds N copyable lines. Both carry their payload as copyText,
// captured at menu-build time, and the enter key runs the second row itself —
// so the key and the menu can never disagree.
//
// Gated exactly like diffEditRow: a loading / failed / binary / too-large view
// has no text to copy. A gap cell offers no "Copy line" at all.
func (m Model) diffCopyLineRows() []actionRow {
	v, ok := m.topLayer().(*diffView)
	if !ok || v.loading || v.err != nil || v.binary || v.tooLarge {
		return nil
	}
	var rows []actionRow
	if text, no, ok := v.cursorCell(); ok {
		label := i18n.T("Copy line")
		if v.onOld {
			label = i18n.T("Copy line (old side)")
		}
		rows = append(rows, m.copyRow("copy-line", label, i18n.T("Copied line %d", no), text))
	}
	if sel := v.selectedLines(); len(sel) > 0 {
		rows = append(rows, m.copyRow("copy-selected-lines",
			i18n.T("Copy selected lines (%d)", len(sel)),
			i18n.T("Copied %d lines", len(sel)),
			strings.Join(sel, "\n")))
	}
	return rows
}

// diffSelectKey gives the line selection its shot at a key, BEFORE the search
// hook. The order matters and is the spec's: a search being TYPED owns every
// key (so space is a space in the query), then the selection, then a committed
// query, then the view's own esc. Since diffSearchKey handles the typing case
// and the committed-query case in one call, the only way to sit between them is
// to run first and decline while typing.
//
// handled == true means the caller must return immediately.
func (m Model) diffSelectKey(v *diffView, msg tea.KeyMsg) (Model, tea.Cmd, bool) {
	if v == nil || v.search.typing {
		return m, nil, false
	}
	switch msg.String() {
	case " ":
		v.lsel.press(v.curLine)
		return m, nil, true
	case "enter":
		// enter has no other meaning in the diff view, so it stays unbound
		// without a selection rather than growing one here.
		if !v.lsel.on {
			return m, nil, false
		}
		// Resolve the row BEFORE clearing: it reads the live range.
		row, ok := rowByID(m.diffCopyLineRows(), "copy-selected-lines")
		v.lsel.clear()
		if !ok {
			m.diffNotice = i18n.T("▸ nothing to copy on this side")
			return m, nil, true
		}
		nm, cmd := row.run(m)
		return nm.(Model), cmd, true
	case "esc":
		if !v.lsel.on {
			return m, nil, false
		}
		v.lsel.clear()
		return m, nil, true
	}
	return m, nil, false
}

// diffSelectHint is the footer while a selection is live: it replaces the whole
// base hint, because every key that matters in that state is here and the user
// must always be able to see the way out (esc). Mode-independent, so one key
// serves the three long-line variants; measured at 86 columns, well inside the
// 140 budget.
func diffSelectHint() string {
	return i18n.T("[space] mark end  [enter] copy  [esc] unmark  [alt+↑↓/jk] extend  [alt↔] side (locked)")
}
```

- [ ] **Step 6: Route the key hook**

In `/mnt/t/others/gigagit.worktrees/feat-line-select-copy/internal/tui/diff_view.go` the
preamble reads:

```go
	body := m.diffBodyRows()
	if nm, cmd, handled := m.diffSearchKey(v, msg, body); handled {
		return nm, cmd
	}
```

Replace with:

```go
	body := m.diffBodyRows()
	// Order (spec §4.7): a search being TYPED owns every key, then the line
	// selection, then a committed query, then the view's own esc. diffSearchKey
	// handles the first and third in one call, so the selection hook runs first
	// and declines while the search is typing.
	if nm, cmd, handled := m.diffSelectKey(v, msg); handled {
		return nm, cmd
	}
	if nm, cmd, handled := m.diffSearchKey(v, msg, body); handled {
		return nm, cmd
	}
```

- [ ] **Step 7: Paint the stripe**

In `/mnt/t/others/gigagit.worktrees/feat-line-select-copy/internal/tui/diff_render.go` the
`cellMark` struct reads:

```go
type cellMark struct {
	row  bool
	attn bool
	base lipgloss.Style
	gut  lipgloss.Style
}
```

Replace with:

```go
type cellMark struct {
	row  bool
	attn bool
	// sel marks a cell inside the line SELECTION: its BODY (never its gutter —
	// the line number keeps its own style, so the eye reads the range against
	// it) is painted through styles.selectionStyle over whatever base the cell
	// would otherwise wear. Only the CURSOR side's mark ever carries it, and
	// never on a gap cell: an absent cell is not selected.
	sel  bool
	base lipgloss.Style
	gut  lipgloss.Style
}
```

After `gapFor` (which ends at `diff_render.go:82`) add:

```go
// bodyFor is the base style a cell's text and trailing padding wear: the style
// the caller already resolved (the cursor band, an attention band, the hot
// add/del shade, or nothing), with the selection stripe laid OVER it when this
// cell is inside the range. In "row" cursor mode the stripe therefore replaces
// the band on the cursor row — the stripe IS the row (spec §4.7).
func (mk cellMark) bodyFor(base lipgloss.Style) lipgloss.Style {
	if !mk.sel {
		return base
	}
	return st().selectionStyle(base)
}
```

In `diffPaneLines`, right after the `mkL, mkR` block Task 2 added (the `if i >= curStart && i < curEnd { … }`), insert:

```go
		// The stripe rides on the CURSOR side only, and only where that side
		// actually has a line — an absent cell is not selected, and the range
		// skips it when copying, so painting it would lie.
		if v.lsel.contains(dr.line, v.curLine) && sidePresent(r, v.onOld) {
			if v.onOld {
				mkL.sel = true
			} else {
				mkR.sel = true
			}
		}
```

In `segCell` the base block reads:

```go
	base := lipgloss.NewStyle()
	if mk.row {
		base = mk.base
	}
	if hot {
		base = mk.hotFor(hotStyle)
	}
	// The hits are offsets in the whole sanitized line; seg.off says where this
```

Replace the three assignments' tail with a `bodyFor` wrap:

```go
	base := lipgloss.NewStyle()
	if mk.row {
		base = mk.base
	}
	if hot {
		base = mk.hotFor(hotStyle)
	}
	base = mk.bodyFor(base)
	// The hits are offsets in the whole sanitized line; seg.off says where this
```

In `scrollCell` the same block reads:

```go
	base := lipgloss.NewStyle()
	if mk.row {
		base = mk.base
	}
	if hot {
		base = mk.hotFor(hotStyle)
	}
	var b strings.Builder
```

Replace with:

```go
	base := lipgloss.NewStyle()
	if mk.row {
		base = mk.base
	}
	if hot {
		base = mk.hotFor(hotStyle)
	}
	base = mk.bodyFor(base)
	var b strings.Builder
```

`diffCell` has TWO paths and both need it. The enriched branch reads:

```go
	if len(spans) > 0 || len(toks) > 0 || len(hits) > 0 {
		base := lipgloss.NewStyle()
		if mk.row {
			base = mk.base
		}
		if hot {
			base = mk.hotFor(hotStyle)
		}
		bodyTxt = hotEmphBody(text, spans, toks, tw, base, hits)
	} else {
		bodyTxt = padRight(truncate(sanitizeLine(text), tw), tw)
		switch {
		case hot:
			bodyTxt = mk.hotFor(hotStyle).Render(bodyTxt)
		case mk.row:
			bodyTxt = mk.base.Render(bodyTxt)
		}
	}
```

Replace the whole if/else with:

```go
	if len(spans) > 0 || len(toks) > 0 || len(hits) > 0 {
		base := lipgloss.NewStyle()
		if mk.row {
			base = mk.base
		}
		if hot {
			base = mk.hotFor(hotStyle)
		}
		bodyTxt = hotEmphBody(text, spans, toks, tw, mk.bodyFor(base), hits)
	} else {
		bodyTxt = padRight(truncate(sanitizeLine(text), tw), tw)
		base := lipgloss.NewStyle()
		painted := false
		switch {
		case hot:
			base, painted = mk.hotFor(hotStyle), true
		case mk.row:
			base, painted = mk.base, true
		}
		// An unmarked, unselected cell is rendered by nobody, so the plain path
		// stays byte-identical to the pre-selection renderer.
		switch {
		case mk.sel:
			bodyTxt = mk.bodyFor(base).Render(bodyTxt)
		case painted:
			bodyTxt = base.Render(bodyTxt)
		}
	}
```

(`scrollCell` delegates to `diffCell` at rest, passing `mk` through, so the
stripe rides along with no extra wiring.)

- [ ] **Step 8: Swap the footer while a selection is live, and rewrite the base hint**

In `renderDiffView` the last content line reads:

```go
	lines = append(lines, truncate(diffHintFor(v.long), w))
	return strings.Join(lines, "\n")
```

Replace with:

```go
	hint := diffHintFor(v.long)
	if v.lsel.on {
		hint = diffSelectHint()
	}
	lines = append(lines, truncate(hint, w))
	return strings.Join(lines, "\n")
```

`diffHintFor`'s body reads:

```go
	return i18n.T("[↑↓/jk] scroll/line  [/] find  [n/p] chg  [c/}{] notes  [z] align  [e] edit  [f] part  [^w] %s", mode) + pan + i18n.T("  [h/b] hist/blame  [esc] close")
```

Replace with:

```go
	return i18n.T("[↑↓/jk] scroll  [/] find  [spc] mark  [alt↔] side  [n/p] chg  [c}{] notes  [e] edit  [f] part  [^w] %s", mode) + pan + i18n.T("  [h/b] hist  [esc] back")
```

and the pan fragment:

```go
	pan := ""
	if long == longScroll {
		pan = i18n.T("  [←→0] pan")
	}
```

→

```go
	pan := ""
	if long == longScroll {
		pan = i18n.T("  [←→] pan")
	}
```

Extend the function's budget comment. It currently ends:

```go
// ORDER IS THE SECOND HALF OF THAT BUDGET: a terminal narrower than 140
// truncates the line's TAIL (view.go cuts it to the width), so whatever sits
// last is what a narrow terminal loses. [/] find rides near the front for
// exactly that reason — it was last when it shipped, and the user reported the
// footer "missing search hints".
```

Append to that comment block, before `func diffHintFor`:

```go
//
// The selection keys (spec §4.7) cost 26 columns — [spc] mark and [alt↔] side
// with their separators — and the line was already AT 140. The shortenings
// available without losing a group ("scroll/line" → "scroll", [c/}{] → [c}{],
// [←→0] → [←→], hist/blame → hist, close → back) come to 14, so one group had
// to go: [z] align, the only one with THREE dedicated . menu rows (Align cursor
// line: top / center / bottom) plus a help row, so nothing becomes
// undiscoverable. [alt↔] rather than [alt←→] pays the last column; ↔ is already
// gg's own glyph for "both directions" (a compare title reads "a ↔ b"). While a
// selection is live the whole line is replaced by diffSelectHint.
```

- [ ] **Step 9: Lead the diff's copy rows with the line rows**

In `/mnt/t/others/gigagit.worktrees/feat-line-select-copy/internal/tui/action_menu.go`,
`contextCopyRows` reads:

```go
	if v := m.diffLayer(); v != nil {
		return m.fileCopyRows(v.title, v.rev) // title = path; rev = commit ("" = working tree)
	}
```

Replace with:

```go
	if v := m.diffLayer(); v != nil {
		// The LINE rows lead (spec §4.7): what the user is looking at is a
		// line, and the file's path/name/commit rows stay right behind them —
		// which also keeps copy-file-path as insertCopyLinkRow's anchor.
		return append(m.diffCopyLineRows(), m.fileCopyRows(v.title, v.rev)...) // title = path; rev = commit ("" = working tree)
	}
```

- [ ] **Step 10: Help rows**

In `/mnt/t/others/gigagit.worktrees/feat-line-select-copy/internal/tui/help.go` the Diff view
section contains:

```go
		r("j/k", i18n.T("move the line cursor (the view scrolls only as needed); pgup/pgdn, home/end and n/p move it too; a click places it")),
```

Insert immediately AFTER it:

```go
		r("alt+←/→", i18n.T("move the cursor to the other side of the same row (the copy, the note anchor and the gg:// link follow it); ignored while a line selection is on")),
		r("space", i18n.T("start a line selection at the cursor; press again to freeze its end (the cursor is then free), a third time to start a new one")),
		r("enter", i18n.T("with a line selection on: copy the selected lines (the cursor side's text, absent cells and folded lines skipped) and clear the selection")),
```

In the same section the `.` row reads:

```go
		r(".", i18n.T("export this file's diff as patch; also Cursor marker, Open in editor at line and the three Align cursor line rows")),
```

Replace with:

```go
		r(".", i18n.T("export this file's diff as patch; also Copy line / Copy selected lines, Cursor marker, Open in editor at line and the three Align cursor line rows")),
```

and the Diff view section's `esc` row reads:

```go
		r("esc", i18n.T("clear the search, then close")),
		r("ctrl+c", i18n.T("quit")),
		h(i18n.T("History view (h)")),
```

Replace the `esc` line (ONLY the one immediately above the `History view (h)`
heading — the same literal is used by the files-view section, which keeps it)
with:

```go
		r("esc", i18n.T("clear the search, then the line selection, then close")),
```

- [ ] **Step 11: Update the three tests that pin the old footer**

In `/mnt/t/others/gigagit.worktrees/feat-line-select-copy/internal/tui/diff_cursor_test.go`:

```go
	for _, k := range []string{"[↑↓/jk]", "[z]", "[e]"} {
```

→

```go
	for _, k := range []string{"[↑↓/jk]", "[spc]", "[alt↔]", "[e]"} {
```

In `/mnt/t/others/gigagit.worktrees/feat-line-select-copy/internal/tui/diff_search_test.go`,
`TestDiffHintFitsTheBudget` ends:

```go
	// The widest variant is at the cap: any new group must shorten a label
	// first. Update this number and diffHintFor's doc comment together.
	if w := lipgloss.Width(diffHintFor(longScroll)); w != 140 {
		t.Fatalf("the scroll variant measures %d columns; the doc comment says 140", w)
	}
}
```

Replace that tail with:

```go
	// The widest variant is at the cap: any new group must shorten a label
	// first. Update this number and diffHintFor's doc comment together.
	if w := lipgloss.Width(diffHintFor(longScroll)); w != 140 {
		t.Fatalf("the scroll variant measures %d columns; the doc comment says 140", w)
	}
	// The selection variant replaces the whole line in every mode, so it has to
	// fit the same budget — a truncated one would hide [esc] unmark, the way out.
	if w := lipgloss.Width(diffSelectHint()); w > 140 {
		t.Fatalf("the selection hint is %d columns, the budget is 140: %q", w, diffSelectHint())
	}
}
```

In `/mnt/t/others/gigagit.worktrees/feat-line-select-copy/internal/tui/action_menu_click_test.go`:

```go
	for _, key := range []string{"[esc]", "[c/}{]", "[^w]", "[h/b]"} {
```

→

```go
	for _, key := range []string{"[esc]", "[c}{]", "[^w]", "[h/b]"} {
```

- [ ] **Step 12: i18n — three changed keys and seven new ones**

DELETE these three keys from each of `internal/i18n/lang/{ja,ko,ru,zh}.toml`
(locate each by its exact English text with `grep -n`):

```
"[↑↓/jk] scroll/line  [/] find  [n/p] chg  [c/}{] notes  [z] align  [e] edit  [f] part  [^w] %s"
"  [←→0] pan"
"  [h/b] hist/blame  [esc] close"
```

Then append the replacements and the new keys at the END of each `[strings]`
table.

`ja.toml`:
```toml
"[↑↓/jk] scroll  [/] find  [spc] mark  [alt↔] side  [n/p] chg  [c}{] notes  [e] edit  [f] part  [^w] %s" = "[↑↓/jk] スクロール  [/] 検索  [spc] 選択  [alt↔] 側  [n/p] 変更  [c}{] ノート  [e] 編集  [f] 部分  [^w] %s"
"  [←→] pan" = "  [←→] パン"
"  [h/b] hist  [esc] back" = "  [h/b] 履歴  [esc] 戻る"
"[space] mark end  [enter] copy  [esc] unmark  [alt+↑↓/jk] extend  [alt↔] side (locked)" = "[space] 終端を決定  [enter] コピー  [esc] 選択解除  [alt+↑↓/jk] 伸縮  [alt↔] 側 (固定)"
"▸ nothing to copy on this side" = "▸ この側にコピーできる行がありません"
"Copy line" = "行をコピー"
"Copy line (old side)" = "行をコピー (旧側)"
"Copied line %d" = "%d 行目をコピーしました"
"Copy selected lines (%d)" = "選択した行をコピー (%d)"
"Copied %d lines" = "%d 行をコピーしました"
"move the cursor to the other side of the same row (the copy, the note anchor and the gg:// link follow it); ignored while a line selection is on" = "同じ行の反対側へカーソルを移す (コピー・ノートの位置・gg:// リンクが従う); 行選択中は無効"
"start a line selection at the cursor; press again to freeze its end (the cursor is then free), a third time to start a new one" = "カーソル行から行選択を開始; もう一度押すと終端を確定 (以降カーソルは自由)、三度目で新しい選択を開始"
"with a line selection on: copy the selected lines (the cursor side's text, absent cells and folded lines skipped) and clear the selection" = "行選択中: 選択行をコピー (カーソル側のテキスト。欠落セルと折りたたみ行は除く) して選択を解除"
"export this file's diff as patch; also Copy line / Copy selected lines, Cursor marker, Open in editor at line and the three Align cursor line rows" = "このファイルの差分をパッチとして書き出し; ほかに 行をコピー / 選択した行をコピー、カーソル表示、指定行をエディタで開く、カーソル行の位置 3 行"
"clear the search, then the line selection, then close" = "検索、次に行選択を解除してから閉じる"
```

`ko.toml`:
```toml
"[↑↓/jk] scroll  [/] find  [spc] mark  [alt↔] side  [n/p] chg  [c}{] notes  [e] edit  [f] part  [^w] %s" = "[↑↓/jk] 스크롤  [/] 찾기  [spc] 선택  [alt↔] 쪽  [n/p] 변경  [c}{] 노트  [e] 편집  [f] 부분  [^w] %s"
"  [←→] pan" = "  [←→] 이동"
"  [h/b] hist  [esc] back" = "  [h/b] 히스토리  [esc] 뒤로"
"[space] mark end  [enter] copy  [esc] unmark  [alt+↑↓/jk] extend  [alt↔] side (locked)" = "[space] 끝 지정  [enter] 복사  [esc] 선택 해제  [alt+↑↓/jk] 확장  [alt↔] 쪽 (고정)"
"▸ nothing to copy on this side" = "▸ 이 쪽에는 복사할 것이 없습니다"
"Copy line" = "줄 복사"
"Copy line (old side)" = "줄 복사 (이전 쪽)"
"Copied line %d" = "%d 번째 줄을 복사했습니다"
"Copy selected lines (%d)" = "선택한 줄 복사 (%d)"
"Copied %d lines" = "%d 줄을 복사했습니다"
"move the cursor to the other side of the same row (the copy, the note anchor and the gg:// link follow it); ignored while a line selection is on" = "같은 행의 반대쪽으로 커서를 옮긴다 (복사·노트 기준·gg:// 링크가 따라간다); 줄 선택 중에는 무시"
"start a line selection at the cursor; press again to freeze its end (the cursor is then free), a third time to start a new one" = "커서 줄에서 줄 선택을 시작; 다시 누르면 끝을 고정 (이후 커서는 자유), 세 번째는 새 선택을 시작"
"with a line selection on: copy the selected lines (the cursor side's text, absent cells and folded lines skipped) and clear the selection" = "줄 선택 중: 선택한 줄을 복사 (커서 쪽 텍스트, 없는 셀과 접힌 줄은 제외) 하고 선택을 해제"
"export this file's diff as patch; also Copy line / Copy selected lines, Cursor marker, Open in editor at line and the three Align cursor line rows" = "이 파일의 diff 를 패치로 내보내기; 그 밖에 줄 복사 / 선택한 줄 복사, 커서 표시, 해당 줄을 편집기로 열기, 커서 줄 위치 3 개"
"clear the search, then the line selection, then close" = "검색, 그다음 줄 선택을 해제한 뒤 닫는다"
```

`ru.toml`:
```toml
"[↑↓/jk] scroll  [/] find  [spc] mark  [alt↔] side  [n/p] chg  [c}{] notes  [e] edit  [f] part  [^w] %s" = "[↑↓/jk] прокрутка  [/] поиск  [spc] выделить  [alt↔] сторона  [n/p] изменение  [c}{] заметки  [e] правка  [f] часть  [^w] %s"
"  [←→] pan" = "  [←→] панорама"
"  [h/b] hist  [esc] back" = "  [h/b] история  [esc] назад"
"[space] mark end  [enter] copy  [esc] unmark  [alt+↑↓/jk] extend  [alt↔] side (locked)" = "[space] отметить конец  [enter] копировать  [esc] снять  [alt+↑↓/jk] расширить  [alt↔] сторона (закреплена)"
"▸ nothing to copy on this side" = "▸ на этой стороне нечего копировать"
"Copy line" = "Копировать строку"
"Copy line (old side)" = "Копировать строку (старая сторона)"
"Copied line %d" = "Скопирована строка %d"
"Copy selected lines (%d)" = "Копировать выделенные строки (%d)"
"Copied %d lines" = "Скопировано строк: %d"
"move the cursor to the other side of the same row (the copy, the note anchor and the gg:// link follow it); ignored while a line selection is on" = "перенести курсор на другую сторону той же строки (копирование, якорь заметки и gg://-ссылка следуют за ним); игнорируется при активном выделении строк"
"start a line selection at the cursor; press again to freeze its end (the cursor is then free), a third time to start a new one" = "начать выделение строк от курсора; второе нажатие фиксирует конец (курсор дальше свободен), третье начинает новое"
"with a line selection on: copy the selected lines (the cursor side's text, absent cells and folded lines skipped) and clear the selection" = "при активном выделении: скопировать выделенные строки (текст стороны курсора, отсутствующие ячейки и свёрнутые строки пропускаются) и снять выделение"
"export this file's diff as patch; also Copy line / Copy selected lines, Cursor marker, Open in editor at line and the three Align cursor line rows" = "выгрузить diff этого файла как патч; а также Копировать строку / Копировать выделенные строки, Маркер курсора, Открыть в редакторе на строке и три строки выравнивания курсора"
"clear the search, then the line selection, then close" = "сначала снять поиск, затем выделение строк, затем закрыть"
```

`zh.toml`:
```toml
"[↑↓/jk] scroll  [/] find  [spc] mark  [alt↔] side  [n/p] chg  [c}{] notes  [e] edit  [f] part  [^w] %s" = "[↑↓/jk] 滚动  [/] 查找  [spc] 选择  [alt↔] 侧  [n/p] 变更  [c}{] 笔记  [e] 编辑  [f] 部分  [^w] %s"
"  [←→] pan" = "  [←→] 平移"
"  [h/b] hist  [esc] back" = "  [h/b] 历史  [esc] 返回"
"[space] mark end  [enter] copy  [esc] unmark  [alt+↑↓/jk] extend  [alt↔] side (locked)" = "[space] 标记结束  [enter] 复制  [esc] 取消选择  [alt+↑↓/jk] 扩展  [alt↔] 侧 (已锁定)"
"▸ nothing to copy on this side" = "▸ 这一侧没有可复制的内容"
"Copy line" = "复制行"
"Copy line (old side)" = "复制行 (旧侧)"
"Copied line %d" = "已复制第 %d 行"
"Copy selected lines (%d)" = "复制选中的行 (%d)"
"Copied %d lines" = "已复制 %d 行"
"move the cursor to the other side of the same row (the copy, the note anchor and the gg:// link follow it); ignored while a line selection is on" = "把光标移到同一行的另一侧 (复制、笔记锚点和 gg:// 链接随之改变); 行选择期间无效"
"start a line selection at the cursor; press again to freeze its end (the cursor is then free), a third time to start a new one" = "从光标行开始行选择; 再按一次固定末端 (此后光标自由), 第三次开始新的选择"
"with a line selection on: copy the selected lines (the cursor side's text, absent cells and folded lines skipped) and clear the selection" = "行选择中: 复制选中的行 (光标一侧的文本, 跳过缺失单元格和折叠行) 并清除选择"
"export this file's diff as patch; also Copy line / Copy selected lines, Cursor marker, Open in editor at line and the three Align cursor line rows" = "把该文件的 diff 导出为补丁; 另有 复制行 / 复制选中的行、光标标记、在编辑器中打开该行, 以及三个光标行对齐项"
"clear the search, then the line selection, then close" = "先清除搜索, 再清除行选择, 然后关闭"
```

Two orphan checks before deleting:
- `"clear the search, then close"` — DO NOT delete it. It is still used by the
  files-view section's `esc` row, so it must stay in all four bundles. Verify
  with `grep -n 'clear the search, then close' internal/tui/help.go` (one hit
  left after the Diff-view row changes).
- `"  [←→0] pan"` has exactly ONE call site (`diff_render.go:111`) — verified
  with `grep -rn '←→0' internal/tui/*.go`; help.go's pan row is a different
  literal (`r("← → 0", "scroll mode: pan left / right / reset")`) and keeps the
  `0` key advertised, which is why dropping it from the footer costs nothing.
  So this one IS deleted.

- [ ] **Step 13: Run the tests, then the whole package**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-line-select-copy && go test ./internal/tui/ -run 'TestDiffSelection|TestDiffOneSpace|TestDiffEscClears|TestDiffRebuildClears|TestDiffHint|TestQuestionMarkInDiffView|TestI18n' -v`
Expected: PASS.

Run (FOREGROUND, Bash timeout 600000):
`cd /mnt/t/others/gigagit.worktrees/feat-line-select-copy && go test ./internal/tui/...`
Expected: PASS. If `TestRenderDiffViewPanes` fails on a width, the footer went
over 140 — re-measure with `lipgloss.Width(diffHintFor(longScroll))`.

Run: `cd /mnt/t/others/gigagit.worktrees/feat-line-select-copy && gofmt -l internal && go vet ./internal/tui/...`
Expected: no output.

- [ ] **Step 14: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-line-select-copy && cat > /tmp/gg-commit-msg-3.txt <<'EOF'
feat(tui): line selection and copy in the diff view

space starts a range at the cursor, space again freezes its end (the
cursor is then free), a third space starts over; enter copies and clears,
esc clears and does nothing else. The copy takes the CURSOR SIDE's source
text, skipping cells that side does not have and collapsed folds — so a
range across a fold copies only what is visible — joined with \n and no
trailing newline. The . menu carries the same rows (Copy line / Copy line
(old side) / Copy selected lines (N)) and enter runs the second of them,
so the key and the menu can never copy different text. Selected cells wear
a stripe on the cursor side only, painted over whatever the cell already
wears; the line-number gutter keeps its own style. The footer gains
[spc] mark and [alt<->] side (paid for by shortenings and by dropping
[z] align, which has three . menu rows of its own) and switches to a
selection variant while a range is live.

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01NDk1DJtgLzZX7hMmxDs9nU
EOF
git -C /mnt/t/others/gigagit.worktrees/feat-line-select-copy add \
  internal/tui/diff_select.go internal/tui/diff_cursor.go internal/tui/diff_view.go \
  internal/tui/diff_render.go internal/tui/action_menu.go internal/tui/help.go \
  internal/tui/diff_select_test.go internal/tui/diff_cursor_test.go \
  internal/tui/diff_search_test.go internal/tui/action_menu_click_test.go \
  internal/i18n/lang/ja.toml internal/i18n/lang/ko.toml \
  internal/i18n/lang/ru.toml internal/i18n/lang/zh.toml
git -C /mnt/t/others/gigagit.worktrees/feat-line-select-copy commit -F /tmp/gg-commit-msg-3.txt
```

---

### Task 4: Blame — the line selection, Copy line, and a body-only row style

Blame's rows carry a frozen commit gutter as `winRow.prefix`, and the stripe
must paint the CODE only. `winRow.style` covers the prefix too, so the window
primitive grows one optional field — `body *lipgloss.Style` — that styles the
text and the trailing padding while the prefix keeps `style`. A pointer, not a
value: `lipgloss.Style` holds `TerminalColor` interface fields, so `== zero` is
not a safe "is it set" test.

**Files:**
- Modify: `internal/tui/window.go:33-63` (`winRow`), `:150-283` (`renderWindow`), `:285-307` (`colouredLine`)
- Modify: `internal/tui/blame_view.go:21-38` (the struct), `:273-355` (`render`), `:374-453` (`update`)
- Create: `internal/tui/blame_select.go`
- Modify: `internal/tui/action_menu.go` (the `*blameView` branch of `contextCopyRows`)
- Modify: `internal/tui/model.go:920-940` (`blameMsg`)
- Modify: `internal/tui/help.go:296-304` (the Blame view section)
- Create: `internal/tui/blame_select_test.go`
- Modify: `internal/i18n/lang/{ja,ko,ru,zh}.toml`

**Interfaces:**
- Consumes: `lineSel`, `st().selectionStyle`, `rowByID`, `m.copyRow`, `sliceMask`, `styledRuns`.
- Produces:
  - `winRow.body *lipgloss.Style` (nil = today's path)
  - `func colouredLine(pre, text string, cls []syntax.Class, emph []emphLevel, style lipgloss.Style, bodySt *lipgloss.Style, w int) string`
  - `blameView.lsel lineSel`
  - `func (b *blameView) selectedLines() []string`
  - `func (m Model) blameCopyLineRows(b *blameView) []actionRow` — ids `copy-line`, `copy-selected-lines`
  - `func (m Model) blameSelectKey(b *blameView, msg tea.KeyMsg) (Model, tea.Cmd, bool)`

- [ ] **Step 1: Write the failing blame tests**

Create `/mnt/t/others/gigagit.worktrees/feat-line-select-copy/internal/tui/blame_select_test.go`:

```go
package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/syntax"
)

// NOTE: TestBlameSelectionPaintsTheCodeNotTheGutter and
// TestBlameSelectionUnderReverseDropsSyntaxColours call
// lipgloss.SetColorProfile (process-global) and therefore do NOT call
// t.Parallel().

// space / move / space / enter copies exactly the frozen range, read off the
// action row the key runs.
func TestBlameSelectionCopies(t *testing.T) {
	t.Parallel()
	m, b := blameSearchModel()
	m = typeBlame(m, b, "space", "down", "space")
	if !b.lsel.on || !b.lsel.fixed {
		t.Fatalf("two spaces must freeze the range: %+v", b.lsel)
	}
	row, ok := rowByID(m.contextCopyRows(), "copy-selected-lines")
	if !ok {
		t.Fatal("no Copy selected lines row while a blame selection is on")
	}
	if row.label != "Copy selected lines (2)" {
		t.Fatalf("label = %q, want `Copy selected lines (2)`", row.label)
	}
	if row.copyText != "package main\nfunc main() {}" {
		t.Fatalf("copyText = %q, want the two blame lines joined with \\n", row.copyText)
	}

	m, cmd := b.update(m, keyMsg("enter"))
	if cmd == nil {
		t.Fatal("enter with a selection on must issue the clipboard command")
	}
	if b.lsel.on {
		t.Fatal("enter must clear the selection after copying")
	}
	if _, ok := m.topLayer().(*historyView); ok {
		t.Fatal("enter with a selection on must NOT open the history view")
	}
}

// Copy line is always offered — every blame row IS a line — and names the
// file's own line number.
func TestBlameCopyLineRow(t *testing.T) {
	t.Parallel()
	m, b := blameSearchModel()
	b.sel = 2 // the uncommitted "dirty" line, LineNo 3
	row, ok := rowByID(m.contextCopyRows(), "copy-line")
	if !ok {
		t.Fatal("blame must always offer Copy line")
	}
	if row.copyText != "dirty" {
		t.Fatalf("copyText = %q, want the raw line content", row.copyText)
	}
	if row.label != "Copy line" {
		t.Fatalf("label = %q, want `Copy line`", row.label)
	}
	// The path/name/commit rows the blame surface already offered are still there.
	if _, ok := rowByID(m.contextCopyRows(), "copy-file-path"); !ok {
		t.Fatal("the file copy rows must survive under the line rows")
	}
}

// enter WITHOUT a selection keeps its old meaning: the history of the commit
// under the cursor.
func TestBlameEnterWithoutSelectionOpensHistory(t *testing.T) {
	t.Parallel()
	m, b := blameSearchModel()
	b.sel = 0 // a committed line
	m, _ = b.update(m, keyMsg("enter"))
	if _, ok := m.topLayer().(*historyView); !ok {
		t.Fatalf("enter with no selection must open the history view, top = %T", m.topLayer())
	}
}

// esc clears the selection first, then goes back. b always goes back.
func TestBlameEscClearsSelectionBeforeClosing(t *testing.T) {
	t.Parallel()
	m, b := blameSearchModel()
	m = typeBlame(m, b, "space")
	m, _ = b.update(m, keyMsg("esc"))
	if b.lsel.on {
		t.Fatal("the first esc must clear the selection")
	}
	if m.topLayer() != b {
		t.Fatal("the first esc must not close the blame view")
	}
	m, _ = b.update(m, keyMsg("esc"))
	if m.topLayer() == b {
		t.Fatal("the second esc must close the blame view")
	}

	// b never waits: it closes even with a selection on.
	m2, b2 := blameSearchModel()
	m2 = typeBlame(m2, b2, "space")
	m2, _ = b2.update(m2, keyMsg("b"))
	if m2.topLayer() == b2 {
		t.Fatal("b must always go back, selection or not")
	}
}

// A reload replaces the lines: the selection goes with them.
func TestBlameMsgClearsSelection(t *testing.T) {
	t.Parallel()
	m, b := blameSearchModel()
	m = typeBlame(m, b, "space")
	u, _ := m.Update(blameMsg{tag: b.tag, lines: []model.BlameLine{{Hash: "z", LineNo: 1, Content: "x"}}})
	_ = u
	if b.lsel.on {
		t.Fatal("blameMsg must clear the selection")
	}
}

// The stripe paints the CODE and leaves the commit gutter alone.
func TestBlameSelectionPaintsTheCodeNotTheGutter(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(prev)

	m := Model{width: 100, height: 30}
	b := blameFixture()
	b.sel = 1
	b.lsel.start(1)
	b.lsel.mark(1)

	out := b.render(m, "")
	line := ""
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "func main()") {
			line = l
			break
		}
	}
	if line == "" {
		t.Fatalf("the selected line is missing from the render:\n%s", out)
	}
	stripe := sgrParams(st().selectionStyle(st().selectedRow).Render("x"))
	if got := sgrBefore(line, "func main()"); !subsetOf(stripe, got) {
		t.Errorf("the code must wear the stripe, params %v: %q", got, line)
	}
	// The gutter's hash is rendered under the row style, never the stripe's
	// background — assert the two SGR sequences differ.
	if sgrSeqBefore(line, "aaaaaaa") == sgrSeqBefore(line, "func main()") {
		t.Errorf("the gutter and the code must not share one style: %q", line)
	}
}

// A selected NON-cursor row under an UNSET selection_bg reverses the body. A
// reversed body would turn per-token syntax FOREGROUNDS into per-token
// BACKGROUNDS — the very hazard winRow.cls documents — so the class mask must
// drop for a reversed body exactly as it does for a reversed style.
func TestBlameSelectionUnderReverseDropsSyntaxColours(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(prev)
	prevTheme := activeTheme()
	defer setTheme(prevTheme)
	setTheme(theme.Terminal) // selection_bg unset => the stripe flips reverse

	m := Model{width: 100, height: 30}
	b := blameFixture()
	b.sel = 0                       // the CURSOR is on line 0…
	b.tok = [][]syntax.Tok{          // …and line 1 carries syntax runs
		nil,
		{{Start: 0, End: 4, Class: syntax.Keyword}},
		nil,
	}
	b.lsel.start(1)
	b.lsel.mark(1)

	out := b.render(m, "")
	line := ""
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "func main()") {
			line = l
			break
		}
	}
	if line == "" {
		t.Fatalf("the selected line is missing:\n%s", out)
	}
	if strings.Contains(line, "38;2;") || strings.Contains(line, "38;5;") {
		t.Errorf("a reversed body must drop the class mask; found a foreground escape: %q", line)
	}
}

// A row with a nil body renders BYTE-IDENTICALLY to the pre-body renderer.
// The oracle is a GOLDEN captured from the current worktree before Step 3
// touches window.go — see the note under this test for how to fill it in.
func TestRenderWindowNilBodyIsUnchanged(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(prev)

	rows := []winRow{
		{prefix: "abc│", text: "hello world", style: st().selectedRow},
		{prefix: "   │", text: "second line"},
	}
	// Filled in from the PRE-CHANGE renderer (see the note below): one entry
	// per dispMode, each the two rendered rows.
	want := map[dispMode][]string{
		modeCutoff: {"", ""},
		modeWrap:   {"", ""},
		modeScroll: {"", ""},
	}
	for _, mode := range []dispMode{modeCutoff, modeWrap, modeScroll} {
		got := renderWindow(rows, winOpts{w: 40, h: 2, mode: mode, prefixW: 4})
		for i := range got {
			if got[i] != want[mode][i] {
				t.Fatalf("mode %d row %d moved:\n got %q\nwant %q", mode, i, got[i], want[mode][i])
			}
		}
	}
}
```

The `theme` import is needed by `TestBlameSelectionUnderReverseDropsSyntaxColours`;
add `"github.com/homeend/gigagit/internal/theme"` to the import block. This file
also calls `lipgloss.SetColorProfile`, so it carries the no-`t.Parallel()` note
at the top and the two paint tests plus `TestRenderWindowNilBodyIsUnchanged`
omit `t.Parallel()`.

**Filling in `TestRenderWindowNilBodyIsUnchanged`'s golden — do this BEFORE
Step 3 edits `window.go`.** Temporarily replace the comparison loop with a
printer and run it against the UNCHANGED renderer:

```go
		for i := range got {
			t.Logf("mode %d row %d = %q", mode, i, got[i])
		}
```

Run `cd /mnt/t/others/gigagit.worktrees/feat-line-select-copy && go test ./internal/tui/ -run TestRenderWindowNilBodyIsUnchanged -v`, paste the six
quoted strings into the `want` map exactly as logged (they contain escape
sequences — copy the `%q` form and keep it a Go string literal), then restore
the comparison loop. The test must now PASS against the unchanged renderer;
that is what makes it a real byte-identity oracle once Step 3 lands.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-line-select-copy && go test ./internal/tui/ -run 'TestBlame(Selection|CopyLine|Enter|Esc|Msg)|TestRenderWindowNilBody' -v`
Expected: FAIL to COMPILE — `b.lsel undefined`, `winRow.body undefined`.

Then do the golden-filling detour described above (it needs the package to
compile, so comment out the `body:` field references in the blame tests, or
simply run only `-run TestRenderWindowNilBodyIsUnchanged` after Step 3's
`winRow` field exists but before the `renderWindow`/`colouredLine` edits — the
field alone changes no output). Whichever order, the golden must be captured
from a renderer that has NOT yet learned about `body`.

- [ ] **Step 3: Give `winRow` a body style**

In `/mnt/t/others/gigagit.worktrees/feat-line-select-copy/internal/tui/window.go` the struct
ends:

```go
	// emph is an optional emphasis level per DISPLAY RUNE of text, filled by
	// the in-view search with its hits (spec §4.3); nil = no emphasis, the path
	// every non-searching caller takes. It is sliced with the text exactly like
	// cls, and — unlike cls — it SURVIVES a reverse-video style: bold still
	// reads after the swap, and the current hit — precisely the row the cursor
	// sits on — paints relative to it (styles.currentHitStyle).
	emph []emphLevel
}
```

Replace with:

```go
	// emph is an optional emphasis level per DISPLAY RUNE of text, filled by
	// the in-view search with its hits (spec §4.3); nil = no emphasis, the path
	// every non-searching caller takes. It is sliced with the text exactly like
	// cls, and — unlike cls — it SURVIVES a reverse-video style: bold still
	// reads after the swap, and the current hit — precisely the row the cursor
	// sits on — paints relative to it (styles.currentHitStyle).
	emph []emphLevel
	// body, when non-nil, styles the row's TEXT and its trailing padding while
	// the PREFIX keeps style. It exists for the line selection's stripe, which
	// paints the code and never blame's commit gutter (spec §4.7). nil = the
	// whole row wears style — the path every other caller takes, and the one
	// that renders byte-identically to before this field existed.
	//
	// A POINTER, not a value: lipgloss.Style holds TerminalColor interface
	// fields, so there is no safe "is it the zero value" comparison.
	body *lipgloss.Style
}
```

In `renderWindow`, the `dline` type reads:

```go
		// emph rides alongside cls; either one being non-nil takes the painted
		// path (a blame row with syntax off carries emphasis and no classes).
		emph []emphLevel
		pre  string
	}
```

Replace with:

```go
		// emph rides alongside cls; either one being non-nil takes the painted
		// path (a blame row with syntax off carries emphasis and no classes).
		emph []emphLevel
		pre  string
		// body is winRow.body carried through: non-nil also forces the painted
		// path, because splitting prefix from text is the whole point of it.
		body *lipgloss.Style
	}
```

The reverse-video class drop reads:

```go
		rcls := r.cls
		if r.style.GetReverse() {
			rcls = nil
		}
		remph := r.emph
```

Replace with:

```go
		rcls := r.cls
		// The BODY's style decides this as much as the row's: a stripe that
		// flips reverse video (the unset selection_bg case) would turn per-token
		// foregrounds into per-token backgrounds just the same.
		if r.style.GetReverse() || (r.body != nil && r.body.GetReverse()) {
			rcls = nil
		}
		remph := r.emph
```

The segment loop reads:

```go
			if segCls == nil && segEmph == nil {
				dl = append(dl, dline{text: pre + s, style: r.style, deco: r.decorate, hs: hs, si: si, row: ri})
				continue
			}
			dl = append(dl, dline{text: s, pre: pre, cls: maskAt(segCls, si), emph: maskAt(segEmph, si), style: r.style, hs: hs, si: si, row: ri})
```

Replace with:

```go
			if segCls == nil && segEmph == nil && r.body == nil {
				dl = append(dl, dline{text: pre + s, style: r.style, deco: r.decorate, hs: hs, si: si, row: ri})
				continue
			}
			dl = append(dl, dline{text: s, pre: pre, cls: maskAt(segCls, si), emph: maskAt(segEmph, si), style: r.style, body: r.body, hs: hs, si: si, row: ri})
```

The output loop reads:

```go
		if dl[idx].cls != nil || dl[idx].emph != nil {
			out = append(out, colouredLine(dl[idx].pre, dl[idx].text, dl[idx].cls, dl[idx].emph, dl[idx].style, w))
			continue
		}
```

Replace with:

```go
		if dl[idx].cls != nil || dl[idx].emph != nil || dl[idx].body != nil {
			out = append(out, colouredLine(dl[idx].pre, dl[idx].text, dl[idx].cls, dl[idx].emph, dl[idx].style, dl[idx].body, w))
			continue
		}
```

And `colouredLine` itself:

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
	if pad := w - lipgloss.Width(pre) - lipgloss.Width(body); pad > 0 {
		b.WriteString(style.Render(strings.Repeat(" ", pad)))
	}
	return b.String()
}
```

Replace with:

```go
func colouredLine(pre, text string, cls []syntax.Class, emph []emphLevel, style lipgloss.Style, bodySt *lipgloss.Style, w int) string {
	disp := []rune(text)
	// Defensive: exactly one entry per rune on both masks (either may be nil).
	cls = sliceMask(cls, 0, len(disp))
	emph = sliceMask(emph, 0, len(disp))
	// The frozen prefix always wears the ROW's style; the body and the trailing
	// padding wear bodySt when the caller set one (the line-selection stripe,
	// which must not reach blame's commit gutter). bodySt nil = base == style,
	// byte-identical to before the field existed.
	base := style
	if bodySt != nil {
		base = *bodySt
	}
	var b strings.Builder
	if pre != "" {
		b.WriteString(style.Render(pre))
	}
	b.WriteString(styledRuns(disp, emph, cls, base))
	if pad := w - lipgloss.Width(pre) - lipgloss.Width(text); pad > 0 {
		b.WriteString(base.Render(strings.Repeat(" ", pad)))
	}
	return b.String()
}
```

Also update `colouredLine`'s doc comment, which reads:

```go
// colouredLine renders one display line of a class-masked row: the frozen
// prefix and the trailing padding under style, the body painted run by run
```

→

```go
// colouredLine renders one display line of a class-masked row: the frozen
// prefix under style, the body and the trailing padding under bodySt (or under
// style when bodySt is nil), the body painted run by run
```

- [ ] **Step 4: Add the field, the stripe and the footer variants to blame**

In `/mnt/t/others/gigagit.worktrees/feat-line-select-copy/internal/tui/blame_view.go` the
struct reads:

```go
	sel     int            // line cursor (index into lines)
	mode    dispMode       // text display mode; z cycles (applies to the whole gutter│code row)
```

Replace with:

```go
	sel     int            // line cursor (index into lines)
	// lsel is the line selection over lines (space/space/enter, spec §4.7).
	// Cleared by blameMsg: a reload replaces the very lines it indexes.
	lsel    lineSel
	mode    dispMode       // text display mode; z cycles (applies to the whole gutter│code row)
```

In `render`, the hint line reads:

```go
	hint := truncate(i18n.T("[↑↓] line  [pgup/pgdn] page  [/] find  [enter] history  [e] editor  [esc/b] back"), w)
```

Replace with:

```go
	hint := truncate(i18n.T("[↑↓] line  [pgup/pgdn] page  [spc] mark  [/] find  [enter] history  [e] editor  [esc/b] back"), w)
	if b.lsel.on {
		// The selection variant replaces the lot, so the way out (esc) is
		// always on screen.
		hint = truncate(i18n.T("[space] mark end  [enter] copy  [esc] unmark  [↑↓] extend"), w)
	}
```

In the row loop, this block:

```go
		var st lipgloss.Style
		if i == b.sel {
			st = s.selectedRow
		}
```

Replace with:

```go
		var st lipgloss.Style
		if i == b.sel {
			st = s.selectedRow
		}
		// The stripe paints the CODE only: the commit gutter is winRow.prefix
		// and keeps the row style, so the eye reads the range's extent against
		// an unchanged gutter. On the cursor row the stripe sits over the
		// reverse-video base, which with selection_bg unset un-reverses it — the
		// "hole" the theme contract describes.
		var bodySt *lipgloss.Style
		if b.lsel.contains(i, b.sel) {
			sel := s.selectionStyle(st)
			bodySt = &sel
		}
```

and the two `winRow` constructions:

```go
		if b.tok == nil { // no lexer / colouring off: the plain (pre-syntax) path
			wr[i-lo] = winRow{prefix: gutter + "│", text: sanitizeLine(ln.Content), emph: emph, style: st}
			continue
		}
		...
		wr[i-lo] = winRow{prefix: gutter + "│", text: string(disp), cls: cls, emph: emph, style: st}
```

→

```go
		if b.tok == nil { // no lexer / colouring off: the plain (pre-syntax) path
			wr[i-lo] = winRow{prefix: gutter + "│", text: sanitizeLine(ln.Content), emph: emph, style: st, body: bodySt}
			continue
		}
		...
		wr[i-lo] = winRow{prefix: gutter + "│", text: string(disp), cls: cls, emph: emph, style: st, body: bodySt}
```

- [ ] **Step 5: Add `blame_select.go`**

Create `/mnt/t/others/gigagit.worktrees/feat-line-select-copy/internal/tui/blame_select.go`:

```go
package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/i18n"
)

// selectedLines is the SOURCE content of every blame line the selection covers.
// Blame has no absent cells and no folds — every row is a line — so the only
// filtering is the clamp to the slice.
func (b *blameView) selectedLines() []string {
	lo, hi, ok := b.lsel.bounds(b.sel)
	if !ok {
		return nil
	}
	if lo < 0 {
		lo = 0
	}
	if hi > len(b.lines)-1 {
		hi = len(b.lines) - 1
	}
	var out []string
	for i := lo; i <= hi; i++ {
		out = append(out, b.lines[i].Content)
	}
	return out
}

// blameCopyLineRows are the . menu's line-copy rows for the blame view: Copy
// line is always offered (every blame row IS a line of the file), and Copy
// selected lines (N) joins it while a range is live. Both carry their payload
// as copyText, and the enter key runs the second row itself.
func (m Model) blameCopyLineRows(b *blameView) []actionRow {
	if b == nil || b.sel < 0 || b.sel >= len(b.lines) {
		return nil
	}
	ln := b.lines[b.sel]
	rows := []actionRow{
		m.copyRow("copy-line", i18n.T("Copy line"), i18n.T("Copied line %d", ln.LineNo), ln.Content),
	}
	if sel := b.selectedLines(); len(sel) > 0 {
		rows = append(rows, m.copyRow("copy-selected-lines",
			i18n.T("Copy selected lines (%d)", len(sel)),
			i18n.T("Copied %d lines", len(sel)),
			strings.Join(sel, "\n")))
	}
	return rows
}

// blameSelectKey gives the line selection its shot at a key, BEFORE the search
// hook — see diffSelectKey for why that order is the one the spec asks for.
// enter WITHOUT a selection keeps its own meaning (the history of the commit
// under the cursor), so the hook declines it; b is never two-stage and never
// reaches here.
func (m Model) blameSelectKey(b *blameView, msg tea.KeyMsg) (Model, tea.Cmd, bool) {
	if b == nil || b.search.typing {
		return m, nil, false
	}
	switch msg.String() {
	case " ":
		if len(b.lines) == 0 {
			return m, nil, true
		}
		b.lsel.press(b.sel)
		return m, nil, true
	case "enter":
		if !b.lsel.on {
			return m, nil, false
		}
		row, ok := rowByID(m.blameCopyLineRows(b), "copy-selected-lines")
		b.lsel.clear()
		if !ok {
			return m, nil, true
		}
		nm, cmd := row.run(m)
		return nm.(Model), cmd, true
	case "esc":
		if !b.lsel.on {
			return m, nil, false
		}
		b.lsel.clear()
		return m, nil, true
	}
	return m, nil, false
}
```

- [ ] **Step 6: Route the key hook and the copy rows**

In `/mnt/t/others/gigagit.worktrees/feat-line-select-copy/internal/tui/blame_view.go`,
`update` reads:

```go
	// The search owns / @ ] [ and — only while a query is live — esc. b is NOT
	// two-stage: it always leaves (never trap the user behind a search).
	if nm, cmd, handled := m.blameSearchKey(b, msg); handled {
		return nm, cmd
	}
```

Replace with:

```go
	// Order (spec §4.7): a search being TYPED owns every key, then the line
	// selection, then a committed query, then the view's own esc. blameSearchKey
	// covers the first and third in one call, so the selection hook runs first
	// and declines while the search is typing. b is NOT two-stage at all: it
	// always leaves (never trap the user behind a search or a selection).
	if nm, cmd, handled := m.blameSelectKey(b, msg); handled {
		return nm, cmd
	}
	if nm, cmd, handled := m.blameSearchKey(b, msg); handled {
		return nm, cmd
	}
```

In `/mnt/t/others/gigagit.worktrees/feat-line-select-copy/internal/tui/action_menu.go`,
`contextCopyRows` reads:

```go
	case *blameView:
		return m.fileCopyRows(s.ctx.path, s.ctx.rev)
	}
```

Replace with:

```go
	case *blameView:
		// The LINE rows lead; the file's path/name/commit rows stay behind them
		// (and keep copy-file-path as insertCopyLinkRow's anchor).
		return append(m.blameCopyLineRows(s), m.fileCopyRows(s.ctx.path, s.ctx.rev)...)
	}
```

In `/mnt/t/others/gigagit.worktrees/feat-line-select-copy/internal/tui/model.go` the
`blameMsg` case reads:

```go
			b.san = nil // the search's display-text cache belongs to the old lines
			b.sel = 0
```

Replace with:

```go
			b.san = nil // the search's display-text cache belongs to the old lines
			b.sel = 0
			b.lsel.clear() // …and so does everything the selection's indexes meant
```

- [ ] **Step 7: Help rows**

In `/mnt/t/others/gigagit.worktrees/feat-line-select-copy/internal/tui/help.go` the Blame view
section reads:

```go
		r("pgup/pgdn", i18n.T("move the cursor one screen")),
		r("enter", i18n.T("file history at the commit under the cursor")),
		r("e", i18n.T("open the blamed file (at this revision) in your external editor (read-only)")),
		r("/ @", i18n.T("find text forward / backward (enter keeps it, esc cancels)")),
		r("] [", i18n.T("next / previous hit (wraps around)")),
		r("esc/b", i18n.T("esc clears the search first, then goes back; b always goes back")),
```

Replace with:

```go
		r("pgup/pgdn", i18n.T("move the cursor one screen")),
		r("space", i18n.T("start a line selection at the cursor; press again to freeze its end (the cursor is then free), a third time to start a new one")),
		r("enter", i18n.T("file history at the commit under the cursor; while a line selection is on, copy the selected lines instead")),
		r("e", i18n.T("open the blamed file (at this revision) in your external editor (read-only)")),
		r("/ @", i18n.T("find text forward / backward (enter keeps it, esc cancels)")),
		r("] [", i18n.T("next / previous hit (wraps around)")),
		r(".", i18n.T("Copy line / Copy selected lines, plus the file's path, name and commit id")),
		r("esc/b", i18n.T("esc backs out one thing at a time — a search you are typing, then a line selection, then a kept query — and only then goes back; b always goes back")),
```

The `space` literal is the SAME string the diff's help row uses (Task 3), so it
is already in the bundles — do not add it twice; `TestI18nBundlesComplete` would
not mind, but a duplicate TOML key is a parse error.

- [ ] **Step 8: i18n — two changed keys, three new**

DELETE from all four bundles:

```
"[↑↓] line  [pgup/pgdn] page  [/] find  [enter] history  [e] editor  [esc/b] back"
"file history at the commit under the cursor"
"esc clears the search first, then goes back; b always goes back"
```

(Confirm with `grep -rn` over `internal/` that none of the three has another
call site before deleting.)

Append to `ja.toml`:
```toml
"[↑↓] line  [pgup/pgdn] page  [spc] mark  [/] find  [enter] history  [e] editor  [esc/b] back" = "[↑↓] 行  [pgup/pgdn] ページ  [spc] 選択  [/] 検索  [enter] 履歴  [e] エディタ  [esc/b] 戻る"
"[space] mark end  [enter] copy  [esc] unmark  [↑↓] extend" = "[space] 終端を決定  [enter] コピー  [esc] 選択解除  [↑↓] 伸縮"
"file history at the commit under the cursor; while a line selection is on, copy the selected lines instead" = "カーソル行のコミットのファイル履歴; 行選択中は代わりに選択行をコピー"
"Copy line / Copy selected lines, plus the file's path, name and commit id" = "行をコピー / 選択した行をコピー、ほかにファイルのパス・名前・コミット ID"
"esc backs out one thing at a time — a search you are typing, then a line selection, then a kept query — and only then goes back; b always goes back" = "esc は一度に一つずつ解除する — 入力中の検索、次に行選択、次に確定した検索語 — その後に戻る; b は常に戻る"
```

Append to `ko.toml`:
```toml
"[↑↓] line  [pgup/pgdn] page  [spc] mark  [/] find  [enter] history  [e] editor  [esc/b] back" = "[↑↓] 줄  [pgup/pgdn] 페이지  [spc] 선택  [/] 찾기  [enter] 히스토리  [e] 편집기  [esc/b] 뒤로"
"[space] mark end  [enter] copy  [esc] unmark  [↑↓] extend" = "[space] 끝 지정  [enter] 복사  [esc] 선택 해제  [↑↓] 확장"
"file history at the commit under the cursor; while a line selection is on, copy the selected lines instead" = "커서 줄 커밋의 파일 히스토리; 줄 선택 중에는 대신 선택한 줄을 복사"
"Copy line / Copy selected lines, plus the file's path, name and commit id" = "줄 복사 / 선택한 줄 복사, 그리고 파일의 경로·이름·커밋 ID"
"esc backs out one thing at a time — a search you are typing, then a line selection, then a kept query — and only then goes back; b always goes back" = "esc 는 한 번에 하나씩 해제한다 — 입력 중인 검색, 그다음 줄 선택, 그다음 유지된 검색어 — 그 후에 뒤로; b 는 항상 뒤로"
```

Append to `ru.toml`:
```toml
"[↑↓] line  [pgup/pgdn] page  [spc] mark  [/] find  [enter] history  [e] editor  [esc/b] back" = "[↑↓] строка  [pgup/pgdn] страница  [spc] выделить  [/] поиск  [enter] история  [e] редактор  [esc/b] назад"
"[space] mark end  [enter] copy  [esc] unmark  [↑↓] extend" = "[space] отметить конец  [enter] копировать  [esc] снять  [↑↓] расширить"
"file history at the commit under the cursor; while a line selection is on, copy the selected lines instead" = "история файла на коммите под курсором; при активном выделении строк — скопировать выделенные строки"
"Copy line / Copy selected lines, plus the file's path, name and commit id" = "Копировать строку / Копировать выделенные строки, а также путь, имя файла и id коммита"
"esc backs out one thing at a time — a search you are typing, then a line selection, then a kept query — and only then goes back; b always goes back" = "esc отменяет по одному — набираемый поиск, затем выделение строк, затем сохранённый запрос — и только потом возвращает; b возвращает всегда"
```

Append to `zh.toml`:
```toml
"[↑↓] line  [pgup/pgdn] page  [spc] mark  [/] find  [enter] history  [e] editor  [esc/b] back" = "[↑↓] 行  [pgup/pgdn] 翻页  [spc] 选择  [/] 查找  [enter] 历史  [e] 编辑器  [esc/b] 返回"
"[space] mark end  [enter] copy  [esc] unmark  [↑↓] extend" = "[space] 标记结束  [enter] 复制  [esc] 取消选择  [↑↓] 扩展"
"file history at the commit under the cursor; while a line selection is on, copy the selected lines instead" = "光标所在提交的文件历史; 行选择中则改为复制选中的行"
"Copy line / Copy selected lines, plus the file's path, name and commit id" = "复制行 / 复制选中的行, 以及文件路径、名称和提交 ID"
"esc backs out one thing at a time — a search you are typing, then a line selection, then a kept query — and only then goes back; b always goes back" = "esc 一次撤销一层 — 正在输入的搜索, 然后行选择, 然后保留的查询 — 之后才返回; b 始终返回"
```

- [ ] **Step 9: Run the tests, then the whole package**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-line-select-copy && go test ./internal/tui/ -run 'TestBlame|TestRenderWindow|TestI18n' -v`
Expected: PASS. (`TestRenderWindow*` is the existing window suite plus the new
nil-body test; every one of them exercises the nil path, which must not have
moved a byte.)

Run (FOREGROUND, Bash timeout 600000):
`cd /mnt/t/others/gigagit.worktrees/feat-line-select-copy && go test ./internal/tui/...`
Expected: PASS.

Run: `cd /mnt/t/others/gigagit.worktrees/feat-line-select-copy && gofmt -l internal && go vet ./internal/tui/...`
Expected: no output.

- [ ] **Step 10: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-line-select-copy && cat > /tmp/gg-commit-msg-4.txt <<'EOF'
feat(tui): line selection and Copy line in the blame view

The same space / space / enter / esc sequence as the diff, over blame's
line cursor, plus a Copy line row that is always offered (every blame row
IS a line). enter without a selection keeps its old meaning — the history
of the commit under the cursor — and b still leaves unconditionally.

The stripe paints the CODE and not the commit gutter, which needed a new
optional winRow.body: the row's prefix keeps its own style while the text
and the padding take the body's. A reversed body drops the syntax class
mask for the same reason a reversed row does — reverse turns per-token
foregrounds into per-token backgrounds. Rows with no body render
byte-identically to before.

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01NDk1DJtgLzZX7hMmxDs9nU
EOF
git -C /mnt/t/others/gigagit.worktrees/feat-line-select-copy add \
  internal/tui/window.go internal/tui/blame_view.go internal/tui/blame_select.go \
  internal/tui/action_menu.go internal/tui/model.go internal/tui/help.go \
  internal/tui/blame_select_test.go \
  internal/i18n/lang/ja.toml internal/i18n/lang/ko.toml \
  internal/i18n/lang/ru.toml internal/i18n/lang/zh.toml
git -C /mnt/t/others/gigagit.worktrees/feat-line-select-copy commit -F /tmp/gg-commit-msg-4.txt
```

---

### Task 5: The View-file preview — a line cursor, raw text, the selection and Copy line

The preview is a pure pager today: `contentPopup.sel` is the TOP line, there is
no cursor, and `contentLine` keeps display-sanitized text only. This task gives
it a cursor (`cur`), keeps the SOURCE text alongside the display text, paints
the cursor band and the stripe, and wires the same space/space/enter/esc
selection plus the `.` menu rows.

**Files:**
- Modify: `internal/tui/content_popup.go:24-35` (`contentLine`), `:40-70` (`contentPopup`), `:70-73` (`previewOrigin`)
- Modify: `internal/tui/file_preview.go:202-234` (`fileContentLinesTok`), `:318-346` (`searchPos`, `snapHit`), `:351-414` (`renderFilePreview`)
- Create: `internal/tui/preview_select.go`
- Modify: `internal/tui/files_view.go:453-458` (the hook order), `:652-665` (the arrow cases), `:688-730` (`previewSearchKey` origin)
- Modify: `internal/tui/action_menu.go:36-50` (`onStashList`), `:571-585` (`contextCopyRows`'s files-view branch)
- Modify: `internal/tui/model.go:703-724` (`fileContentMsg`)
- Modify: `internal/tui/footer.go:198-200` (the preview status bar)
- Modify: `internal/tui/help.go:242` (the files-view `.` row)
- Modify: `internal/tui/file_preview_syntax_test.go:19-31` (the legacy oracle)
- Create: `internal/tui/preview_select_test.go`
- Modify: `internal/i18n/lang/{ja,ko,ru,zh}.toml`

**Interfaces:**
- Consumes: `lineSel`, `st().selectionStyle`, `st().diffCursorRow`, `m.cursorStyle()`, `previewClamp`, `m.filePreviewRowsCap`, `rowByID`, `m.copyRow`.
- Produces:
  - `contentLine.raw string`, `contentLine.src bool`
  - `contentPopup.cur int`, `contentPopup.lsel lineSel`
  - `previewOrigin{sel, cur, hscroll int}`
  - `func (p *contentPopup) ensureCursorVisible(rowsCap int)`
  - `func (m Model) movePreviewCursor(delta int)`
  - `func (p *contentPopup) selectedLines() []string`
  - `func (m Model) previewCopyLineRows() []actionRow` — ids `copy-line`, `copy-selected-lines`
  - `func (m Model) previewSelectKey(msg tea.KeyMsg) (Model, tea.Cmd, bool)`

- [ ] **Step 1: Write the failing preview tests**

Create `/mnt/t/others/gigagit.worktrees/feat-line-select-copy/internal/tui/preview_select_test.go`:

```go
package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// NOTE: TestPreviewCursorPaint calls lipgloss.SetColorProfile (process-global)
// and therefore does NOT call t.Parallel().

// previewTabModel opens a preview over a file whose first line carries a real
// TAB, so the raw-vs-display distinction is testable.
func previewTabModel(t *testing.T) Model {
	t.Helper()
	var b strings.Builder
	b.WriteString("\tindented\n")
	b.WriteString("\n") // a genuinely EMPTY source line: copyable
	for i := 0; i < 60; i++ {
		fmt.Fprintf(&b, "line%03d\n", i)
	}
	return openPreview(t, fullTreeTreeSideOf(t, previewModelN(b.String())))
}

// alt+↓ moves the CURSOR and scrolls only when the cursor would leave the
// window; plain ↓ scrolls the viewport and leaves the cursor alone.
func TestPreviewAltDownMovesCursorNotViewport(t *testing.T) {
	t.Parallel()
	m := previewTabModel(t)
	p := m.filesPreview
	if p.cur != 0 || p.sel != 0 {
		t.Fatalf("a fresh preview starts at cursor 0 / top 0, got cur=%d sel=%d", p.cur, p.sel)
	}
	m = feedPreview(m, "alt+down")
	if p.cur != 1 {
		t.Fatalf("alt+down must move the cursor, cur = %d want 1", p.cur)
	}
	if p.sel != 0 {
		t.Fatalf("a cursor still inside the window must not scroll, sel = %d want 0", p.sel)
	}
	// Walk the cursor past the bottom edge: now the pager must follow, minimally.
	rows := m.filePreviewRowsCap()
	for i := 1; i < rows+2; i++ {
		m = feedPreview(m, "alt+down")
	}
	if p.cur != rows+1 {
		t.Fatalf("cursor = %d, want %d", p.cur, rows+1)
	}
	if p.sel != p.cur-rows+1 {
		t.Fatalf("the pager must follow minimally: sel = %d, want %d", p.sel, p.cur-rows+1)
	}
	m = feedPreview(m, "alt+up")
	if p.cur != rows {
		t.Fatalf("alt+up must step the cursor back, cur = %d", p.cur)
	}
}

func TestPreviewDownScrollsNotCursor(t *testing.T) {
	t.Parallel()
	m := previewTabModel(t)
	p := m.filesPreview
	m = feedPreview(m, "down", "down")
	if p.sel != 2 {
		t.Fatalf("↓ must scroll the pager, sel = %d want 2", p.sel)
	}
	if p.cur != 0 {
		t.Fatalf("↓ must not move the cursor, cur = %d want 0", p.cur)
	}
}

// Copy takes the SOURCE line: the tab survives, the display expansion does not.
func TestPreviewSelectionCopiesRawText(t *testing.T) {
	t.Parallel()
	m := previewTabModel(t)
	p := m.filesPreview
	if p.lines[0].raw != "\tindented" {
		t.Fatalf("raw = %q, want the source line with its tab", p.lines[0].raw)
	}
	if !strings.Contains(p.lines[0].text, "    ") {
		t.Fatalf("the DISPLAY text should have the tab expanded: %q", p.lines[0].text)
	}
	if !p.lines[0].src || !p.lines[1].src || p.lines[1].raw != "" {
		t.Fatalf("an empty SOURCE line must be src with an empty raw: %#v", p.lines[1])
	}

	m = feedPreview(m, "space", "alt+down") // loose range 0..1
	row, ok := rowByID(m.contextCopyRows(), "copy-selected-lines")
	if !ok {
		t.Fatal("no Copy selected lines row while a preview selection is on")
	}
	if row.copyText != "\tindented\n" {
		t.Fatalf("copyText = %q, want the tab line and the empty line", row.copyText)
	}
	if row.label != "Copy selected lines (2)" {
		t.Fatalf("label = %q", row.label)
	}

	u, cmd := m.Update(keyMsg("enter"))
	m = u.(Model)
	if cmd == nil {
		t.Fatal("enter with a selection on must issue the clipboard command")
	}
	if m.filesPreview.lsel.on {
		t.Fatal("enter must clear the selection")
	}
	if m.filesTreeFocused {
		t.Fatal("copying must not move focus to the tree")
	}
}

// Copy line on the cursor line, with the preview's own 1-based number.
func TestPreviewCopyLineRow(t *testing.T) {
	t.Parallel()
	m := previewTabModel(t)
	m = feedPreview(m, "alt+down", "alt+down") // cursor on line index 2
	row, ok := rowByID(m.contextCopyRows(), "copy-line")
	if !ok {
		t.Fatal("the focused preview must offer Copy line")
	}
	if row.copyText != "line000" {
		t.Fatalf("copyText = %q, want the third source line", row.copyText)
	}
	// The tree's own path/name/commit rows stay reachable underneath.
	if _, ok := rowByID(m.contextCopyRows(), "copy-file-path"); !ok {
		t.Fatal("the file copy rows must stay reachable while the preview is focused")
	}
}

// A placeholder line is not a line of the file: no Copy line, and space is inert.
func TestPreviewCopyLineAbsentOnPlaceholder(t *testing.T) {
	t.Parallel()
	m := previewTabModel(t)
	p := m.filesPreview
	p.lines = []contentLine{{text: "(loading…)"}}
	p.cur, p.sel = 0, 0
	if _, ok := rowByID(m.contextCopyRows(), "copy-line"); ok {
		t.Fatal("a placeholder line must not offer Copy line")
	}
	m = feedPreview(m, "space")
	if m.filesPreview.lsel.on {
		t.Fatal("space on a placeholder must not start a selection")
	}
}

// enter with NO selection keeps its old meaning: focus moves to the tree.
func TestPreviewEnterWithoutSelectionFocusesTree(t *testing.T) {
	t.Parallel()
	m := previewTabModel(t)
	m = feedPreview(m, "enter")
	if !m.filesTreeFocused {
		t.Fatal("enter with no selection must focus the tree, as it did before")
	}
}

// esc clears the selection first, then closes the preview.
func TestPreviewEscClearsSelectionBeforeClosing(t *testing.T) {
	t.Parallel()
	m := previewTabModel(t)
	m = feedPreview(m, "space", "esc")
	if m.filesPreview == nil {
		t.Fatal("the first esc must clear the selection, not close the preview")
	}
	if m.filesPreview.lsel.on {
		t.Fatal("the first esc must clear the selection")
	}
	m = feedPreview(m, "esc")
	if m.filesPreview != nil {
		t.Fatal("the second esc must close the preview")
	}
}

// A search hit LANDS the cursor, and ] / [ then measure from it.
func TestPreviewSearchHitLandsCursor(t *testing.T) {
	t.Parallel()
	m := previewSearchModel(t) // 60 "line%03d alpha" rows + a "tail beta" row
	m = feedPreview(m, "/", "b", "e", "t", "a")
	p := m.filesPreview
	if p.search.cur < 0 {
		t.Fatal("the incremental search must have found the tail")
	}
	hit := p.search.hits[p.search.cur]
	if p.cur != hit.row {
		t.Fatalf("the cursor must land on the hit row: cur = %d, hit = %d", p.cur, hit.row)
	}
	if got := p.searchPos().row; got != hit.row {
		t.Fatalf("searchPos must report the current hit's own row, got %d want %d", got, hit.row)
	}
	// The FALLBACK branch is the one that retires the old behaviour: with no
	// current hit, searchPos must read the CURSOR, not the pager's top line.
	// Put the two deliberately out of step and check which one it follows.
	savedCur := p.search.cur
	p.search.cur = -1
	p.cur, p.sel = 7, 0
	if got := p.searchPos().row; got != 7 {
		t.Fatalf("with no current hit searchPos must follow the cursor, row = %d want 7 (top line is 0)", got)
	}
	p.search.cur, p.cur = savedCur, hit.row

	// esc cancels the live search and restores the cursor as well as the pager.
	m = feedPreview(m, "esc")
	if m.filesPreview.cur != 0 {
		t.Fatalf("esc must restore the cursor to where the search started, cur = %d", m.filesPreview.cur)
	}
}

// A load arrival replaces the lines: the cursor resets and the selection goes.
func TestPreviewLoadArrivalResetsCursorAndSelection(t *testing.T) {
	t.Parallel()
	m := previewTabModel(t)
	m = feedPreview(m, "alt+down", "alt+down", "space")
	u, _ := m.Update(fileContentMsg{tag: m.filesPreviewTag, lines: fileContentLines([]byte("a\nb\n"))})
	m = u.(Model)
	if m.filesPreview.cur != 0 || m.filesPreview.sel != 0 {
		t.Fatalf("a load must reset the cursor and the top line, cur=%d sel=%d", m.filesPreview.cur, m.filesPreview.sel)
	}
	if m.filesPreview.lsel.on {
		t.Fatal("a load must clear the selection")
	}
}

// The cursor row wears the diff's cursor band; a selected row wears the stripe,
// and the stripe wins on the cursor row itself (the stripe IS the row).
func TestPreviewCursorPaint(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(prev)

	m := previewTabModel(t)
	m = feedPreview(m, "alt+down", "alt+down") // cursor on "line000"
	boxW, boxH := m.layout().rightW, m.layout().boxH[panelCommits]
	out := m.renderFilePreview(boxW, boxH)

	band := sgrParams(st().diffCursorRow.Render("x"))
	row := previewRowWith(t, out, "line000")
	if got := sgrBefore(row, "line000"); !subsetOf(band, got) {
		t.Errorf("the cursor row must wear the band, params %v: %q", got, row)
	}
	other := previewRowWith(t, out, "line001")
	if got := sgrBefore(other, "line001"); subsetOf(band, got) {
		t.Errorf("a non-cursor row must not wear the band: %q", other)
	}

	// Now select the cursor row: the stripe replaces the band.
	m = feedPreview(m, "space")
	out = m.renderFilePreview(boxW, boxH)
	stripe := sgrParams(st().selectionStyle(st().diffCursorRow).Render("x"))
	row = previewRowWith(t, out, "line000")
	if got := sgrBefore(row, "line000"); !subsetOf(stripe, got) {
		t.Errorf("a selected cursor row must wear the stripe, params %v: %q", got, row)
	}

	// cursor_style off: no band at all, but the stripe still paints.
	m.diffCursor = "off"
	m = feedPreview(m, "esc")
	out = m.renderFilePreview(boxW, boxH)
	row = previewRowWith(t, out, "line000")
	if got := sgrBefore(row, "line000"); subsetOf(band, got) {
		t.Errorf("with diff_cursor off the preview must paint no band: %q", row)
	}
}

// previewRowWith returns the rendered preview line containing needle.
func previewRowWith(t *testing.T, out, needle string) string {
	t.Helper()
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, needle) {
			return l
		}
	}
	t.Fatalf("no rendered row contains %q:\n%s", needle, out)
	return ""
}

// The hint line advertises the new keys and swaps to the selection variant.
func TestPreviewHintVariants(t *testing.T) {
	t.Parallel()
	m := previewTabModel(t)
	// Render WIDE: renderFilePreview truncates the hint to innerW, and both
	// variants are longer than the right column at the fixture's 100 columns.
	m.width = 220
	boxW, boxH := m.layout().rightW, m.layout().boxH[panelCommits]
	out := m.renderFilePreview(boxW, boxH)
	if !strings.Contains(out, "[alt+↑↓] line") || !strings.Contains(out, "[spc] mark") {
		t.Fatalf("the preview hint must advertise the cursor and mark keys:\n%s", out)
	}
	if f, _ := m.footerOverride(); !strings.Contains(f, "[spc] mark") {
		t.Fatalf("the status bar must advertise the mark key: %q", f)
	}

	m = feedPreview(m, "space")
	out = m.renderFilePreview(boxW, boxH)
	for _, want := range []string{"[space] mark end", "[enter] copy", "[esc] unmark"} {
		if !strings.Contains(out, want) {
			t.Errorf("the selection hint lacks %q:\n%s", want, out)
		}
	}
	if f, _ := m.footerOverride(); !strings.Contains(f, "[enter] copy") {
		t.Fatalf("the status bar must switch to the selection variant: %q", f)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-line-select-copy && go test ./internal/tui/ -run TestPreview -v`
Expected: FAIL to COMPILE — `p.cur undefined`, `p.lines[0].raw undefined`,
`m.movePreviewCursor undefined`.

- [ ] **Step 3: Keep the source text on every preview line**

In `/mnt/t/others/gigagit.worktrees/feat-line-select-copy/internal/tui/content_popup.go`,
`contentLine` reads:

```go
type contentLine struct {
	text string
	// cls is an optional syntax class per DISPLAY RUNE of text (nil = plain),
	// handed to winRow.cls by the renderers. Only the file preview fills it in;
	// heading lines never carry one.
	cls     []syntax.Class
	heading bool
```

Replace with:

```go
type contentLine struct {
	text string
	// raw is the line's SOURCE text — line breaks normalized, but NOT display-
	// sanitized: tabs are still tabs, and there is no gutter or line number.
	// It is what Copy line and a line selection put on the clipboard (spec
	// §4.7). src says the line came from the file at all: a PLACEHOLDER
	// ("(loading…)", "(empty file)", "(file too large to preview)",
	// "(load failed: …)") has src false, while a genuinely EMPTY source line
	// has src true and raw "" — an empty line IS a line, and is copyable.
	// Only the file preview fills these in; every other consumer leaves them
	// zero, which reads as "nothing to copy here".
	raw string
	src bool
	// cls is an optional syntax class per DISPLAY RUNE of text (nil = plain),
	// handed to winRow.cls by the renderers. Only the file preview fills it in;
	// heading lines never carry one.
	cls     []syntax.Class
	heading bool
```

In `/mnt/t/others/gigagit.worktrees/feat-line-select-copy/internal/tui/file_preview.go`,
`fileContentLinesTok`'s body reads:

```go
	s = normalizeLineBreaks(s)
	parts := strings.Split(sanitizeForDisplay(s), "\n")
	out := make([]contentLine, len(parts))
	for i, ln := range parts {
		out[i] = contentLine{text: ln}
	}
	if tok == nil {
		return out
	}
	// The mask is derived from the RAW line (pre-sanitize): token offsets are
	// rune indices into it, and only the raw line knows which runes the sweep
	// expanded (a tab) or dropped (a control).
	raw := strings.Split(s, "\n")
	for i := range out {
		if i >= len(raw) {
			break
		}
		out[i].cls = displayCls(raw[i], tokAt(tok, i+1))
	}
	return out
```

Replace with:

```go
	s = normalizeLineBreaks(s)
	// The RAW line (post-normalize, PRE-sanitize) is what Copy puts on the
	// clipboard and what the class mask is derived from — token offsets are
	// rune indices into it, and only it knows which runes the sweep expanded
	// (a tab) or dropped (a control). Split it once and zip.
	raw := strings.Split(s, "\n")
	parts := strings.Split(sanitizeForDisplay(s), "\n")
	out := make([]contentLine, len(parts))
	for i, ln := range parts {
		out[i] = contentLine{text: ln, src: true}
		if i < len(raw) {
			out[i].raw = raw[i]
		}
	}
	if tok == nil {
		return out
	}
	for i := range out {
		if i >= len(raw) {
			break
		}
		out[i].cls = displayCls(raw[i], tokAt(tok, i+1))
	}
	return out
```

(The `(empty file)` early return above is untouched: a placeholder, `src` false.)

- [ ] **Step 4: Teach the plain-path oracle about the new fields**

`TestFileContentLinesPlainPathUnchanged` compares against a hand-copied
pre-mask implementation, so it must learn the two fields or it will fail on
every input. In
`/mnt/t/others/gigagit.worktrees/feat-line-select-copy/internal/tui/file_preview_syntax_test.go`
the oracle reads:

```go
	s = normalizeLineBreaks(s)
	parts := strings.Split(sanitizeForDisplay(s), "\n")
	out := make([]contentLine, len(parts))
	for i, ln := range parts {
		out[i] = contentLine{text: ln}
	}
	return out
}
```

Replace with:

```go
	s = normalizeLineBreaks(s)
	raw := strings.Split(s, "\n")
	parts := strings.Split(sanitizeForDisplay(s), "\n")
	out := make([]contentLine, len(parts))
	for i, ln := range parts {
		out[i] = contentLine{text: ln, src: true}
		if i < len(raw) {
			out[i].raw = raw[i]
		}
	}
	return out
}
```

That keeps the oracle honest about `text` (its whole point) and makes the two
new fields tautological here — which is why
`TestPreviewSelectionCopiesRawText` asserts them against literal expectations
instead.

- [ ] **Step 5: Add the cursor and the selection to `contentPopup`**

In `content_popup.go` the struct reads:

```go
	sel     int           // cursor index into the FILTERED view
	mode    dispMode      // text display mode; z cycles
```

Replace with:

```go
	sel     int           // cursor index into the FILTERED view — and, in the FILE PREVIEW, the TOP visible line (it is a pager)
	// cur is the FILE PREVIEW's line cursor: an index into lines (spec §4.7).
	// It is read ONLY by renderFilePreview and the preview key paths — the help
	// window, the files tree and the error popup share this struct and never
	// look at it, so the zero value is inert for them. ↑/↓ and the wheel still
	// scroll sel alone; alt+↑/↓ move cur and scroll minimally.
	cur     int
	// lsel is the preview's line selection over lines (space/space/enter).
	lsel    lineSel
	mode    dispMode      // text display mode; z cycles
```

`previewOrigin` reads:

```go
// previewOrigin is the pager state a live preview search restores on esc.
type previewOrigin struct{ sel, hscroll int }
```

Replace with:

```go
// previewOrigin is the view state a live preview search restores on esc: the
// pager top, the line cursor (a snapped hit moves it, so esc must put it back)
// and the horizontal pan.
type previewOrigin struct{ sel, cur, hscroll int }
```

- [ ] **Step 6: Move the cursor, and derive the search from it**

Create `/mnt/t/others/gigagit.worktrees/feat-line-select-copy/internal/tui/preview_select.go`:

```go
package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/i18n"
)

// ensureCursorVisible scrolls the pager's top line (sel) the minimum needed for
// cur to sit inside a rowsCap-row window — the diff's j/k rule. In wrap mode a
// row can occupy several display lines, so rowsCap is an upper bound there and
// the cursor may end up one row short of the bottom; that is the same
// approximation snapHit already makes, and previewClamp keeps the top legal.
func (p *contentPopup) ensureCursorVisible(rowsCap int) {
	if rowsCap < 1 {
		rowsCap = 1
	}
	if p.cur < p.sel {
		p.sel = p.cur
	}
	if p.cur >= p.sel+rowsCap {
		p.sel = p.cur - rowsCap + 1
	}
	p.sel = previewClamp(p.sel, len(p.lines), rowsCap, p.mode)
}

// movePreviewCursor steps the focused preview's line cursor by delta, clamps it
// to the file and scrolls minimally to keep it on screen. The Model is a value
// but filesPreview is a pointer, so the mutation is visible to the caller.
func (m Model) movePreviewCursor(delta int) {
	p := m.filesPreview
	if p == nil || len(p.lines) == 0 {
		return
	}
	p.cur += delta
	if p.cur < 0 {
		p.cur = 0
	}
	if p.cur > len(p.lines)-1 {
		p.cur = len(p.lines) - 1
	}
	p.ensureCursorVisible(m.filePreviewRowsCap())
}

// selectedLines is the SOURCE text of every preview line the selection covers.
// Placeholder lines carry src false and are skipped — they are not lines of the
// file. An EMPTY source line is kept: it is a line.
func (p *contentPopup) selectedLines() []string {
	lo, hi, ok := p.lsel.bounds(p.cur)
	if !ok {
		return nil
	}
	if lo < 0 {
		lo = 0
	}
	if hi > len(p.lines)-1 {
		hi = len(p.lines) - 1
	}
	var out []string
	for i := lo; i <= hi; i++ {
		if !p.lines[i].src {
			continue
		}
		out = append(out, p.lines[i].raw)
	}
	return out
}

// previewCopyLineRows are the . menu's line-copy rows for a FOCUSED View-file
// preview. Copy line is offered only on a real source line; a placeholder
// ("(loading…)") is not a line of the file and has nothing to copy.
func (m Model) previewCopyLineRows() []actionRow {
	p := m.filesPreview
	if p == nil || m.filesTreeFocused {
		return nil
	}
	var rows []actionRow
	if p.cur >= 0 && p.cur < len(p.lines) && p.lines[p.cur].src {
		rows = append(rows, m.copyRow("copy-line", i18n.T("Copy line"),
			i18n.T("Copied line %d", p.cur+1), p.lines[p.cur].raw))
	}
	if sel := p.selectedLines(); len(sel) > 0 {
		rows = append(rows, m.copyRow("copy-selected-lines",
			i18n.T("Copy selected lines (%d)", len(sel)),
			i18n.T("Copied %d lines", len(sel)),
			strings.Join(sel, "\n")))
	}
	return rows
}

// previewSelectKey gives the preview's line selection its shot at a key, BEFORE
// previewSearchKey — see diffSelectKey for why that order is the spec's. enter
// WITHOUT a selection keeps its old meaning (focus moves to the tree), so the
// hook declines it.
func (m Model) previewSelectKey(msg tea.KeyMsg) (Model, tea.Cmd, bool) {
	p := m.filesPreview
	if p == nil || m.filesTreeFocused || p.search.typing {
		return m, nil, false
	}
	switch msg.String() {
	case " ":
		// The preview owns the keyboard, so the key is consumed either way —
		// but a placeholder line cannot start a range.
		if p.cur < 0 || p.cur >= len(p.lines) || !p.lines[p.cur].src {
			return m, nil, true
		}
		p.lsel.press(p.cur)
		return m, nil, true
	case "enter":
		if !p.lsel.on {
			return m, nil, false
		}
		row, ok := rowByID(m.previewCopyLineRows(), "copy-selected-lines")
		p.lsel.clear()
		if !ok {
			return m, nil, true
		}
		nm, cmd := row.run(m)
		return nm.(Model), cmd, true
	case "esc":
		if !p.lsel.on {
			return m, nil, false
		}
		p.lsel.clear()
		return m, nil, true
	}
	return m, nil, false
}
```

In `file_preview.go`, `searchPos` reads:

```go
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
```

Replace with:

```go
// searchPos is where ] and [ measure from: the current hit's exact column when
// there is one, otherwise the head of the LINE CURSOR's row. (It used to be the
// top visible line, because the preview had no cursor — the deferred "searchPos
// ignores p.sel after a free scroll" item; a cursor retires it.)
func (p *contentPopup) searchPos() searchPos {
	if p.search.cur >= 0 && p.search.cur < len(p.search.hits) {
		h := p.search.hits[p.search.cur]
		return searchPos{row: h.row, side: h.side, col: h.start}
	}
	return searchPos{row: p.cur, side: 0, col: -1}
}
```

`snapHit` reads:

```go
	h := p.search.hits[p.search.cur]
	switch {
	case h.row < p.sel:
		p.sel = h.row
	case h.row >= p.sel+rowsCap:
		p.sel = h.row - rowsCap + 1
	}
```

Replace with:

```go
	h := p.search.hits[p.search.cur]
	// The hit LANDS the cursor (spec §4.7), so ]/[ and the next search step
	// measure from where the user is actually looking.
	p.cur = h.row
	switch {
	case h.row < p.sel:
		p.sel = h.row
	case h.row >= p.sel+rowsCap:
		p.sel = h.row - rowsCap + 1
	}
```

and its doc comment's sentence `There is no cursor to place — the current hit's
own colouring is the marker.` becomes `The cursor lands on the hit row too, so
]/[ step from there.`

- [ ] **Step 7: Paint the band and the stripe, and swap the hint**

In `renderFilePreview` the row loop reads:

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
```

Replace with:

```go
	wr := make([]winRow, len(window))
	cursorOff := m.cursorStyle() == "off"
	for i, l := range window {
		wr[i] = winRow{text: l.text, cls: l.cls}
		// The preview rows carry no prefix, so winRow.style IS the body style —
		// no winRow.body needed here, and reverse video correctly drops the
		// class mask on a stripe that inverts (per-token foregrounds would
		// become per-token backgrounds).
		//
		// [ui] diff_cursor governs the preview cursor too; "number" falls back
		// to the band, because there is no gutter to carry a number.
		row := start + i
		var rowStyle lipgloss.Style
		marked := false
		if row == p.cur && !cursorOff {
			rowStyle, marked = st().diffCursorRow, true
		}
		if p.lsel.contains(row, p.cur) {
			// The stripe REPLACES the band on the cursor row — the stripe is
			// the row (spec §4.7).
			rowStyle, marked = st().selectionStyle(rowStyle), true
		}
		if marked {
			wr[i].style = rowStyle
		}
		if p.search.active() {
			if hs := p.search.hitsOn(row, 0); len(hs) > 0 {
				wr[i].emph = overlayHits(nil, 0, len([]rune(l.text)), hs)
			}
		}
	}
```

(The `start+i` that fed `hitsOn` is now the named `row`; behaviour unchanged.
An unmarked row still leaves `style` at its zero value, so its render is
byte-identical to before.)

The hint line reads:

```go
	hint := i18n.T("%d/%d  [↑/↓] scroll  [ctrl+w] view  [/] find  [esc] close", start+1, len(vis))
```

Replace with:

```go
	hint := i18n.T("%d/%d  [↑/↓] scroll  [alt+↑↓] line  [spc] mark  [ctrl+w] view  [/] find  [esc] close", start+1, len(vis))
	if p.lsel.on {
		hint = i18n.T("%d/%d  [space] mark end  [enter] copy  [esc] unmark  [alt+↑↓] extend", start+1, len(vis))
	}
```

- [ ] **Step 8: Route the keys**

In `/mnt/t/others/gigagit.worktrees/feat-line-select-copy/internal/tui/files_view.go`,
`updateFilesViewKey` starts:

```go
	// The focused preview's in-view search comes first: it owns / @ ] [ and,
	// while a query is live, esc — otherwise esc still closes the preview below.
	if nm, cmd, handled := m.previewSearchKey(msg); handled {
		return nm, cmd
	}
```

Replace with:

```go
	// Order (spec §4.7): a search being TYPED owns every key, then the line
	// selection, then a committed query, then the preview's own esc.
	// previewSearchKey covers the first and third in one call, so the selection
	// hook runs first and declines while the search is typing.
	if nm, cmd, handled := m.previewSelectKey(msg); handled {
		return nm, cmd
	}
	// The focused preview's in-view search comes next: it owns / @ ] [ and,
	// while a query is live, esc — otherwise esc still closes the preview below.
	if nm, cmd, handled := m.previewSearchKey(msg); handled {
		return nm, cmd
	}
```

In the main switch, this pair exists:

```go
	case "up", "k":
		if m.filesTreeFocused {
			p.move(-1)
			return m, nil
		}
		return m.moveListUnderFilesView(-1)
```

Insert immediately BEFORE `case "up", "k":`:

```go
	case "alt+up", "alt+down":
		// The LINE CURSOR of a focused preview; ↑/↓ keep scrolling the viewport
		// only. Inert on the tree side and with no preview open, where alt+↑/↓
		// belong to the search-recall dropdown (handled above while typing).
		if m.filesPreview != nil && !m.filesTreeFocused {
			delta := 1
			if msg.String() == "alt+up" {
				delta = -1
			}
			m.movePreviewCursor(delta)
		}
		return m, nil
```

In `previewSearchKey` the origin capture reads:

```go
	case searchOpenFwd, searchOpenBack:
		p.searchOrig = previewOrigin{sel: p.sel, hscroll: p.hscroll}
```

→

```go
	case searchOpenFwd, searchOpenBack:
		p.searchOrig = previewOrigin{sel: p.sel, cur: p.cur, hscroll: p.hscroll}
```

and both restore sites read:

```go
			} else {
				p.sel, p.hscroll = p.searchOrig.sel, p.searchOrig.hscroll
			}
		case searchCancelled:
			p.sel, p.hscroll = p.searchOrig.sel, p.searchOrig.hscroll
		}
```

→

```go
			} else {
				p.sel, p.cur, p.hscroll = p.searchOrig.sel, p.searchOrig.cur, p.searchOrig.hscroll
			}
		case searchCancelled:
			p.sel, p.cur, p.hscroll = p.searchOrig.sel, p.searchOrig.cur, p.searchOrig.hscroll
		}
```

- [ ] **Step 9: Reset on a load arrival**

In `/mnt/t/others/gigagit.worktrees/feat-line-select-copy/internal/tui/model.go` the
`fileContentMsg` case reads:

```go
		if msg.err != nil {
			m.filesPreview.lines = []contentLine{{text: i18n.T("(load failed: %s)", msg.err.Error())}}
			return m, nil
		}
		p := m.filesPreview
		p.lines = msg.lines
```

Replace with:

```go
		if msg.err != nil {
			m.filesPreview.lines = []contentLine{{text: i18n.T("(load failed: %s)", msg.err.Error())}}
			m.filesPreview.cur, m.filesPreview.sel = 0, 0
			m.filesPreview.lsel.clear()
			return m, nil
		}
		p := m.filesPreview
		p.lines = msg.lines
		// The lines the cursor and the selection indexed are gone.
		p.cur = 0
		p.lsel.clear()
```

(The existing `p.sel = 0` in the no-search branch stays; the search branch
re-snaps both through `snapHit`.)

- [ ] **Step 10: The `.` menu case and the stash short-circuit**

In `/mnt/t/others/gigagit.worktrees/feat-line-select-copy/internal/tui/action_menu.go`,
`contextCopyRows` reads:

```go
	if v := m.filesView; v != nil {
		var rows []actionRow
```

Insert BEFORE it:

```go
	if p := m.filesPreview; p != nil && !m.filesTreeFocused {
		// A FOCUSED preview owns the right column, so the . menu is about the
		// line under its cursor. The tree's own path/name/commit rows stay
		// reachable underneath (and keep copy-file-path as the anchor
		// insertCopyLinkRow looks for).
		return append(m.previewCopyLineRows(), m.fileCopyRows(p.title, m.filesHash)...)
	}
	if v := m.filesView; v != nil {
		var rows []actionRow
```

In `availableActions` the stash short-circuit reads:

```go
		onStashList := m.stashView != nil && m.focus == panelCommits &&
			!m.filesTreeFocused && m.diffLayer() == nil
```

Replace with:

```go
		// …and not while a PREVIEW owns the right column: a preview opened from
		// a stash file tree leaves the tree unfocused, which would otherwise
		// route the menu to Apply/Pop/Drop and never reach the preview's own
		// line rows.
		onStashList := m.stashView != nil && m.focus == panelCommits &&
			!m.filesTreeFocused && m.diffLayer() == nil && m.filesPreview == nil
```

- [ ] **Step 11: The status-bar hint**

In `/mnt/t/others/gigagit.worktrees/feat-line-select-copy/internal/tui/footer.go`:

```go
		if m.filesPreview != nil && !m.filesTreeFocused {
			return i18n.T("file: [↑/↓] scroll  [ctrl+w] view  [←/tab] back to tree  [esc] close preview"), true
		}
```

Replace with:

```go
		if m.filesPreview != nil && !m.filesTreeFocused {
			if m.filesPreview.lsel.on {
				return i18n.T("file: [space] mark end  [enter] copy  [esc] unmark  [alt+↑↓] extend"), true
			}
			return i18n.T("file: [↑/↓] scroll  [alt+↑↓] line  [spc] mark  [ctrl+w] view  [←/tab] back to tree  [esc] close preview"), true
		}
```

- [ ] **Step 12: The files-view help row**

In `/mnt/t/others/gigagit.worktrees/feat-line-select-copy/internal/tui/help.go` the files-view
`.` row reads:

```go
		r(".", i18n.T("tree side: View file — show the file's content at this commit (no diff) in the right pane ([↑/↓] scroll, [ctrl+w] view mode, / or @ find text forward / backward and ] [ step the hits, [esc] clears the search then closes, [←] back to the tree); Open in external editor — open that content in $VISUAL/$EDITOR, read-only")),
```

Replace with:

```go
		r(".", i18n.T("tree side: View file — show the file's content at this commit (no diff) in the right pane ([↑/↓] scroll, [alt+↑/↓] move the line cursor, [space] starts a line selection and [enter] copies it, [ctrl+w] view mode, / or @ find text forward / backward and ] [ step the hits, [esc] clears the search, then the selection, then closes, [←] back to the tree; the preview's own . menu carries Copy line and Copy selected lines); Open in external editor — open that content in $VISUAL/$EDITOR, read-only")),
```

- [ ] **Step 13: i18n — three changed keys, two new**

DELETE from all four bundles (locate by exact text):

```
"%d/%d  [↑/↓] scroll  [ctrl+w] view  [/] find  [esc] close"
"file: [↑/↓] scroll  [ctrl+w] view  [←/tab] back to tree  [esc] close preview"
"tree side: View file — show the file's content at this commit (no diff) in the right pane ([↑/↓] scroll, [ctrl+w] view mode, / or @ find text forward / backward and ] [ step the hits, [esc] clears the search then closes, [←] back to the tree); Open in external editor — open that content in $VISUAL/$EDITOR, read-only"
```

Append to `ja.toml`:
```toml
"%d/%d  [↑/↓] scroll  [alt+↑↓] line  [spc] mark  [ctrl+w] view  [/] find  [esc] close" = "%d/%d  [↑/↓] スクロール  [alt+↑↓] 行  [spc] 選択  [ctrl+w] 表示  [/] 検索  [esc] 閉じる"
"%d/%d  [space] mark end  [enter] copy  [esc] unmark  [alt+↑↓] extend" = "%d/%d  [space] 終端を決定  [enter] コピー  [esc] 選択解除  [alt+↑↓] 伸縮"
"file: [↑/↓] scroll  [alt+↑↓] line  [spc] mark  [ctrl+w] view  [←/tab] back to tree  [esc] close preview" = "ファイル: [↑/↓] スクロール  [alt+↑↓] 行  [spc] 選択  [ctrl+w] 表示  [←/tab] ツリーへ戻る  [esc] プレビューを閉じる"
"file: [space] mark end  [enter] copy  [esc] unmark  [alt+↑↓] extend" = "ファイル: [space] 終端を決定  [enter] コピー  [esc] 選択解除  [alt+↑↓] 伸縮"
"tree side: View file — show the file's content at this commit (no diff) in the right pane ([↑/↓] scroll, [alt+↑/↓] move the line cursor, [space] starts a line selection and [enter] copies it, [ctrl+w] view mode, / or @ find text forward / backward and ] [ step the hits, [esc] clears the search, then the selection, then closes, [←] back to the tree; the preview's own . menu carries Copy line and Copy selected lines); Open in external editor — open that content in $VISUAL/$EDITOR, read-only" = "ツリー側: ファイルを表示 — このコミット時点の内容を右ペインに表示 (差分ではない)。[↑/↓] スクロール、[alt+↑/↓] 行カーソル移動、[space] で行選択を開始し [enter] でコピー、[ctrl+w] 表示モード、/ または @ で前方/後方検索・] [ でヒット移動、[esc] は検索、次に選択を解除してから閉じる、[←] ツリーへ戻る。プレビューの . メニューには 行をコピー と 選択した行をコピー がある。外部エディタで開く — その内容を $VISUAL/$EDITOR で読み取り専用で開く"
```

Append to `ko.toml`:
```toml
"%d/%d  [↑/↓] scroll  [alt+↑↓] line  [spc] mark  [ctrl+w] view  [/] find  [esc] close" = "%d/%d  [↑/↓] 스크롤  [alt+↑↓] 줄  [spc] 선택  [ctrl+w] 보기  [/] 찾기  [esc] 닫기"
"%d/%d  [space] mark end  [enter] copy  [esc] unmark  [alt+↑↓] extend" = "%d/%d  [space] 끝 지정  [enter] 복사  [esc] 선택 해제  [alt+↑↓] 확장"
"file: [↑/↓] scroll  [alt+↑↓] line  [spc] mark  [ctrl+w] view  [←/tab] back to tree  [esc] close preview" = "파일: [↑/↓] 스크롤  [alt+↑↓] 줄  [spc] 선택  [ctrl+w] 보기  [←/tab] 트리로 복귀  [esc] 미리보기 닫기"
"file: [space] mark end  [enter] copy  [esc] unmark  [alt+↑↓] extend" = "파일: [space] 끝 지정  [enter] 복사  [esc] 선택 해제  [alt+↑↓] 확장"
"tree side: View file — show the file's content at this commit (no diff) in the right pane ([↑/↓] scroll, [alt+↑/↓] move the line cursor, [space] starts a line selection and [enter] copies it, [ctrl+w] view mode, / or @ find text forward / backward and ] [ step the hits, [esc] clears the search, then the selection, then closes, [←] back to the tree; the preview's own . menu carries Copy line and Copy selected lines); Open in external editor — open that content in $VISUAL/$EDITOR, read-only" = "트리 쪽: 파일 보기 — 이 커밋의 파일 내용을 오른쪽 창에 표시 (diff 아님). [↑/↓] 스크롤, [alt+↑/↓] 줄 커서 이동, [space] 로 줄 선택 시작하고 [enter] 로 복사, [ctrl+w] 보기 모드, / 또는 @ 로 앞/뒤 검색하고 ] [ 로 히트 이동, [esc] 는 검색, 그다음 선택을 해제한 뒤 닫기, [←] 트리로 복귀. 미리보기의 . 메뉴에는 줄 복사 와 선택한 줄 복사 가 있다. 외부 편집기에서 열기 — 그 내용을 $VISUAL/$EDITOR 에서 읽기 전용으로 연다"
```

Append to `ru.toml`:
```toml
"%d/%d  [↑/↓] scroll  [alt+↑↓] line  [spc] mark  [ctrl+w] view  [/] find  [esc] close" = "%d/%d  [↑/↓] прокрутка  [alt+↑↓] строка  [spc] выделить  [ctrl+w] вид  [/] поиск  [esc] закрыть"
"%d/%d  [space] mark end  [enter] copy  [esc] unmark  [alt+↑↓] extend" = "%d/%d  [space] отметить конец  [enter] копировать  [esc] снять  [alt+↑↓] расширить"
"file: [↑/↓] scroll  [alt+↑↓] line  [spc] mark  [ctrl+w] view  [←/tab] back to tree  [esc] close preview" = "файл: [↑/↓] прокрутка  [alt+↑↓] строка  [spc] выделить  [ctrl+w] вид  [←/tab] назад к дереву  [esc] закрыть предпросмотр"
"file: [space] mark end  [enter] copy  [esc] unmark  [alt+↑↓] extend" = "файл: [space] отметить конец  [enter] копировать  [esc] снять  [alt+↑↓] расширить"
"tree side: View file — show the file's content at this commit (no diff) in the right pane ([↑/↓] scroll, [alt+↑/↓] move the line cursor, [space] starts a line selection and [enter] copies it, [ctrl+w] view mode, / or @ find text forward / backward and ] [ step the hits, [esc] clears the search, then the selection, then closes, [←] back to the tree; the preview's own . menu carries Copy line and Copy selected lines); Open in external editor — open that content in $VISUAL/$EDITOR, read-only" = "сторона дерева: Показать файл — содержимое файла на этом коммите в правой панели (не diff). [↑/↓] прокрутка, [alt+↑/↓] переместить курсор строки, [space] начинает выделение строк, [enter] копирует его, [ctrl+w] режим показа, / или @ ищут вперёд/назад, ] [ шагают по совпадениям, [esc] снимает поиск, затем выделение, затем закрывает, [←] назад к дереву; в меню . самого предпросмотра есть Копировать строку и Копировать выделенные строки. Открыть во внешнем редакторе — открыть это содержимое в $VISUAL/$EDITOR только для чтения"
```

Append to `zh.toml`:
```toml
"%d/%d  [↑/↓] scroll  [alt+↑↓] line  [spc] mark  [ctrl+w] view  [/] find  [esc] close" = "%d/%d  [↑/↓] 滚动  [alt+↑↓] 行  [spc] 选择  [ctrl+w] 视图  [/] 查找  [esc] 关闭"
"%d/%d  [space] mark end  [enter] copy  [esc] unmark  [alt+↑↓] extend" = "%d/%d  [space] 标记结束  [enter] 复制  [esc] 取消选择  [alt+↑↓] 扩展"
"file: [↑/↓] scroll  [alt+↑↓] line  [spc] mark  [ctrl+w] view  [←/tab] back to tree  [esc] close preview" = "文件: [↑/↓] 滚动  [alt+↑↓] 行  [spc] 选择  [ctrl+w] 视图  [←/tab] 回到树  [esc] 关闭预览"
"file: [space] mark end  [enter] copy  [esc] unmark  [alt+↑↓] extend" = "文件: [space] 标记结束  [enter] 复制  [esc] 取消选择  [alt+↑↓] 扩展"
"tree side: View file — show the file's content at this commit (no diff) in the right pane ([↑/↓] scroll, [alt+↑/↓] move the line cursor, [space] starts a line selection and [enter] copies it, [ctrl+w] view mode, / or @ find text forward / backward and ] [ step the hits, [esc] clears the search, then the selection, then closes, [←] back to the tree; the preview's own . menu carries Copy line and Copy selected lines); Open in external editor — open that content in $VISUAL/$EDITOR, read-only" = "树一侧: 查看文件 — 在右侧面板显示该提交处的文件内容 (不是 diff)。[↑/↓] 滚动, [alt+↑/↓] 移动行光标, [space] 开始行选择, [enter] 复制, [ctrl+w] 显示模式, / 或 @ 向前/向后查找, ] [ 在命中间移动, [esc] 先清除搜索, 再清除选择, 然后关闭, [←] 回到树; 预览自身的 . 菜单包含 复制行 与 复制选中的行。在外部编辑器中打开 — 以只读方式在 $VISUAL/$EDITOR 中打开该内容"
```

- [ ] **Step 14: Run the tests, then the whole package**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-line-select-copy && go test ./internal/tui/ -run 'TestPreview|TestFilePreview|TestFileContentLines|TestI18n' -v`
Expected: PASS.

Run (FOREGROUND, Bash timeout 600000):
`cd /mnt/t/others/gigagit.worktrees/feat-line-select-copy && go test ./internal/tui/...`
Expected: PASS.

Run: `cd /mnt/t/others/gigagit.worktrees/feat-line-select-copy && gofmt -l internal && go vet ./internal/tui/...`
Expected: no output.

- [ ] **Step 15: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-line-select-copy && cat > /tmp/gg-commit-msg-5.txt <<'EOF'
feat(tui): a line cursor, line selection and Copy line in the View-file preview

The preview was a pure pager. It now carries a line cursor: alt+up/down
move it and scroll the minimum needed to keep it visible, while up/down,
the wheel and the page keys still scroll the viewport alone; the cursor
row wears the diff's cursor band under [ui] diff_cursor (number falls back
to the band — there is no gutter to number). A search hit lands the cursor
and ]/[ step from it, which also retires the old "searchPos ignores sel
after a free scroll" behaviour; esc restores the cursor with the pager.

Copy ships the SOURCE line: contentLine keeps the normalized-but-
unsanitized text, so a tab is copied as a tab, and an empty source line is
copyable while a placeholder ("(loading…)") is not. space/space/enter/esc
select and copy exactly as in the diff and blame, and the focused
preview's . menu now leads with Copy line / Copy selected lines with the
tree's path, name and commit-id rows behind them.

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01NDk1DJtgLzZX7hMmxDs9nU
EOF
git -C /mnt/t/others/gigagit.worktrees/feat-line-select-copy add \
  internal/tui/content_popup.go internal/tui/file_preview.go \
  internal/tui/preview_select.go internal/tui/files_view.go \
  internal/tui/action_menu.go internal/tui/model.go internal/tui/footer.go \
  internal/tui/help.go internal/tui/preview_select_test.go \
  internal/tui/file_preview_syntax_test.go \
  internal/i18n/lang/ja.toml internal/i18n/lang/ko.toml \
  internal/i18n/lang/ru.toml internal/i18n/lang/zh.toml
git -C /mnt/t/others/gigagit.worktrees/feat-line-select-copy commit -F /tmp/gg-commit-msg-5.txt
```

---

### Task 6: Docs and the final sweep

No code behaviour changes here. README gets the three reader paragraphs and
the new theme role, CHANGELOG an `## [Unreleased]` entry, `docs/CLAUDE-details.md`
a "Line selection + copy" section plus a correction to the phase-0 cursor
paragraph (the marker is one-sided now), and the two stale
"on both panes" descriptions of `[ui] diff_cursor` are fixed.

**Files:**
- Modify: `README.md:56` (the diff row), `:68` (the `l` row's preview clause), `:70` (the blame row), `:523-526` (`[ui] diff_cursor`)
- Modify: `CHANGELOG.md:9` (under `## [Unreleased]`)
- Modify: `docs/CLAUDE-details.md:136-157` (the phase-0 cursor paragraph), and a new section right after the in-view search one's last paragraph (`:987`)
- Modify: `internal/config/template.go:49` (the `diff_cursor` settingDoc)
- Modify: `internal/config/config.go:68-73` (the `DiffCursor` doc comment)

**Interfaces:** none — documentation only.

- [ ] **Step 1: README — the diff row**

In `/mnt/t/others/gigagit.worktrees/feat-line-select-copy/README.md`, line 56 contains this
fragment (find it with `grep -n 'move the \*\*line cursor\*\*' README.md`):

```
`j`/`k` (or `alt+↑/↓`) move the **line cursor** (`↑`/`↓` scroll without moving it; `z` cycles its position center/top/bottom; a click places it), `e` opens the file in your editor at that line,
```

Replace it with:

```
`j`/`k` (or `alt+↑/↓`) move the **line cursor** (`↑`/`↓` scroll without moving it; `z` cycles its position center/top/bottom; a click places it), `alt+←`/`alt+→` move it to the **other pane of the same row** — the cursor sits on ONE side, only that side's cell is marked (a `·` gap cell included, so you can see where you are even where the side has no line), the header names it (`old line N` on the left), and the copy, the review-note anchor and the `gg://` link all follow it — `space` starts a **line selection** at the cursor and a second `space` freezes its end (the cursor is then free; a third starts a new range), `enter` copies the selected lines — the cursor side's source text, skipping cells that side hasn't got and collapsed folds — and `esc` unmarks (the side is locked while a selection is live), `e` opens the file in your editor at that line,
```

And, in the same row, the clause that names the search role:

```
hits are highlighted like word differences and the current one is painted with the `search_current_bg` theme role (a background patch; the `terminal` theme leaves it unset and inverts the hit against its row instead)
```

Replace with:

```
hits are highlighted like word differences and the current one is painted with the `search_current_bg` theme role (a background patch; the `terminal` theme leaves it unset and inverts the hit against its row instead), while a line selection paints its lines with the `selection_bg` role the same two ways
```

- [ ] **Step 2: README — the `l` row's preview clause and the blame row**

Line 68 contains (find it with `grep -n 'with the \*\*View file\*\* preview focused' README.md`):

```
`/` searches paths — with the **View file** preview focused it searches the previewed content instead (`@` backwards, `]`/`[` step the hits, `esc` clears the search then closes the preview);
```

Replace with:

```
`/` searches paths — with the **View file** preview focused it searches the previewed content instead (`@` backwards, `]`/`[` step the hits and land the preview's **line cursor** on them, `esc` clears the search, then a line selection, then closes the preview); in that preview `alt+↑`/`alt+↓` move the line cursor (plain `↑`/`↓` keep scrolling the viewport), `space`/`space` select a range of lines and `enter` copies them as the file's own text (tabs intact), and the `.` menu carries **Copy line** and **Copy selected lines**;
```

Line 70 (the blame row) contains:

```
`enter` opens that commit's history, `/` (or `@`) finds text in the file with `]`/`[` stepping the hits, `esc`/`b` go back (a live search takes the first `esc`; `b` always goes back)
```

Replace with:

```
`enter` opens that commit's history, `space`/`space` select a range of lines and `enter` then copies them instead (the `.` menu offers **Copy line** and **Copy selected lines** too), `/` (or `@`) finds text in the file with `]`/`[` stepping the hits, `esc`/`b` go back (a live search takes the first `esc`, a line selection the next; `b` always goes back)
```

- [ ] **Step 3: README — `[ui] diff_cursor`**

Lines 523–526 read:

```
`[ui] diff_cursor` (default `"row"`) picks the diff view's current-line
marker: `"row"` paints a background under the cursor line, `"number"`
highlights only its gutter numbers, `"off"` hides it (the cursor still
drives `e`). The `.` menu's **Cursor marker** row switches it for the session.
```

Replace with:

```
`[ui] diff_cursor` (default `"row"`) picks the current-line marker: `"row"`
paints a background under the cursor line, `"number"` highlights only its
gutter numbers, `"off"` hides it (the cursor still drives `e`, the review-note
anchor and the line selection). In the diff the marker lands on the **cursor
side's cell only** — `alt+←`/`alt+→` move it across. It governs the **View
file** preview's line cursor as well, where `"number"` falls back to the band
(the preview has no gutter to number). The `.` menu's **Cursor marker** row
switches it for the session.
```

- [ ] **Step 4: CHANGELOG**

In `/mnt/t/others/gigagit.worktrees/feat-line-select-copy/CHANGELOG.md`, immediately after
the `## [Unreleased]` heading on line 9 (the file contains a literal `<<<<<<<`
much further down — it is prose, not a merge marker; anchor on the heading),
insert a blank line and:

```markdown
- **Select lines and copy them, in all three readers.** The diff view, blame
  and the **View file** preview take tmux's copy-mode keys: `space` starts a
  line selection at the cursor, a second `space` freezes its end (the cursor
  is free afterwards; a third starts a new range), `enter` copies the range
  and clears it, `esc` just clears it. What lands on the clipboard is the
  file's own text — tabs intact, no gutter, no line numbers — joined with
  newlines and with no trailing one. The `.` menu of each reader carries the
  same actions (**Copy line** and **Copy selected lines (N)**), and `enter`
  runs that very row, so the key and the menu can never copy different things.
  Selected lines are painted with the new `selection_bg` theme role (dark
  `#264F78`, light `#ADD6FF`; `terminal` leaves it unset and inverts the line
  against its row instead) — on the text only, so the blame gutter and the
  diff's line numbers keep marking the extent.

- **The diff's line cursor sits on ONE side.** `alt+←`/`alt+→` move it between
  the two panes of the same row. Only that side's cell wears the marker —
  including a `·` gap cell, so the cursor is visible even where the side has
  no line — while the other cell renders as it would without a cursor. The
  header names the side (`old line N` on the left), a left click picks the
  pane it landed in, and the copy, the review note `c` adds, and the `gg://`
  link `L` copies all follow the side. A selection is locked to the side it
  started on: `alt+←/→` are refused with a bottom-left notice while one is
  live.

- **The View file preview has a line cursor.** It was a pager: `alt+↑`/`alt+↓`
  now move a cursor and scroll only as much as they must, while `↑`/`↓`, the
  wheel and the page keys keep scrolling the viewport alone. A search hit
  lands the cursor, so `]`/`[` step from where you are looking. The marker
  follows `[ui] diff_cursor`, which now governs the preview too (`number`
  falls back to the band — the preview has no gutter).

- The diff footer makes room for `[spc] mark` and `[alt↔] side` by shortening
  several labels and dropping `[z] align`, which has three `.` menu rows
  (**Align cursor line: top / center / bottom**) and a help row of its own.
  While a selection is live the whole footer switches to the selection keys in
  each of the three readers, so the way out is always on screen.
```

- [ ] **Step 5: `docs/CLAUDE-details.md` — correct the phase-0 paragraph**

In `/mnt/t/others/gigagit.worktrees/feat-line-select-copy/docs/CLAUDE-details.md` the "Diff
view line cursor" paragraph contains:

```
`alignCursor(mode cursorAlign, body int)` places the cursor
line's first display row at the top/centre/bottom (`alignTop`/`alignCenter`/
`alignBottom`, the `z`-key cycle order); `diffPaneLines(v, w, body, curStart,
curEnd, style)` paints the marked range — the history pane calls it with
`0, 0, "off"` so its rows are never marked.
```

Replace with:

```
`alignCursor(mode cursorAlign, body int)` places the cursor
line's first display row at the top/centre/bottom (`alignTop`/`alignCenter`/
`alignBottom`, the `z`-key cycle order); `diffPaneLines(v, w, body, curStart,
curEnd, style)` paints the marked range — the history pane calls it with
`0, 0, "off"` so its rows are never marked. Since §4.7 the cursor also has a
SIDE (`diffView.onOld`, false = the new/right pane): `diffPaneLines` builds two
`cellMark`s per row and gives the cursor mark to that side's cell only, so the
other cell renders as if this were not the cursor row (its attention band wins
there). `cursorRow()` still hands back the whole aligned row; `cursorCell()` is
the side-aware reader — the source text plus that side's line number, refusing
a cell the side has not got (`sidePresent`). The marker DOES paint a gap cell
on the cursor side in `row` mode, which is the one relaxation of the phase-0
"a gap filler is never marked" rule; attention bands still skip them.
```

- [ ] **Step 6: `docs/CLAUDE-details.md` — a new section**

Immediately AFTER the in-view search section's last paragraph (the one that
begins "**Both cells of an unchanged row paint.**" — find it with
`grep -n 'Both cells of an unchanged row paint' docs/CLAUDE-details.md` and
insert after that paragraph ends), add:

```markdown
**Line selection + copy (phase 5, spec §4.7).** One pure type in
`internal/tui/lineselect.go` — `lineSel{on, anchor, end, fixed}` with
`start`/`mark`/`press`/`clear`/`bounds`/`contains` — serves the diff view,
blame and the View-file preview. `press` is the whole space key: start a range
at the cursor, freeze its end, start a new one. `bounds(cur)` follows the LIVE
cursor while the range is loose and the frozen `end` once it is fixed, which is
why moving the cursor extends a one-space selection and leaves a two-space one
alone — no host does any bookkeeping for that.

Each host embeds it as `lsel` over its own LOGICAL line indexes (`v.lines`,
`b.lines`, `p.lines` — never a display row) and owns one key hook,
`<host>SelectKey`, placed BEFORE its search hook and declining while
`search.typing`. That placement is the only way to get the spec's esc order —
search typing → selection → committed query → close — because each host's
search hook handles the typing case and the committed-query case in the same
call.

Copy goes through ACTION ROWS, never a bare clipboard call: each host has one
`…CopyLineRows` function returning `Copy line` (`copy-line`) and
`Copy selected lines (N)` (`copy-selected-lines`) built with `m.copyRow`, the
`.` menu splices them ahead of the file path/name/commit rows, and the `enter`
key looks the second row up with `rowByID` and runs it. So the key and the menu
can never copy different text, and a test reads the exact payload off
`row.copyText` — the same seam `contextLinkRow` established for `L`.

What is copied is SOURCE text: `textdiff.Row.Left/Right` in the diff,
`model.BlameLine.Content` in blame, and — new — `contentLine.raw` in the
preview, which `fileContentLinesTok` fills from the normalized-but-unsanitized
line so a tab stays a tab. `contentLine.src` distinguishes a genuinely empty
source line (`src` true, `raw` "" — copyable, it is a line) from a placeholder
such as `(loading…)` (`src` false — not a line of the file, no Copy line row,
and `space` is inert on it). A diff range skips a fold entry and a cell the
cursor side has not got, which is the user's "a range across a collapsed fold
copies only the visible lines" ruling.

Painting is a base-style SWAP, not a new emphasis level: `styles.selectionStyle`
follows `currentHitStyle`'s two-mode contract (the `selection_bg` role as a
background patch that clears reverse, or a flip of reverse video when unset)
minus the bold — a stripe marks extent, not one hit. The three hosts reach it
differently, because of what their rows are made of: the diff carries a new
`cellMark.sel` bit whose `bodyFor` wraps each cell's resolved base (only the
cursor side's mark ever sets it, and never on a gap); blame needs a BODY-ONLY
style, because its commit gutter is `winRow.prefix` and `winRow.style` covers
the prefix too, so `winRow.body *lipgloss.Style` styles the text and the padding
while the prefix keeps `style` — a pointer, since `lipgloss.Style` holds
interface fields and has no safe zero comparison, and a reversed body drops the
class mask exactly as a reversed style does; the preview's rows carry no prefix
at all, so its `winRow.style` IS the body style and nothing new was needed.

The preview also gained the cursor it never had: `contentPopup.cur` (read only
by `renderFilePreview` and the preview key paths — the same struct backs the
help window and the files tree, which never look at it) moved by `alt+↑/↓`
through `movePreviewCursor` + `ensureCursorVisible`, painted with
`st().diffCursorRow` under `[ui] diff_cursor` (`number` falls back to the band:
there is no gutter to number). `searchPos()` now derives from `cur` and
`snapHit` lands it, which retires the deferred "preview `searchPos` ignores
`p.sel` after a free scroll" item; `previewOrigin` carries `cur` so esc restores
it with the pager.

Invalidation: the diff clears on `rebuild()` (the `f` toggle), on `ctrl+w`
(which relayouts and re-anchors rather than rebuilding) and on a `diffMsg`
arrival — but NOT on a resize, which changes no line index; blame on `blameMsg`;
the preview on `fileContentMsg` and with the struct itself on close. Search keys
never clear it. The diff's `onOld`, by contrast, is CARRIED across `diffMsg`
(it is the user's choice, not the loader's) and is not inherited by a new file.
```

- [ ] **Step 7: The two stale `diff_cursor` descriptions**

In `/mnt/t/others/gigagit.worktrees/feat-line-select-copy/internal/config/template.go`:

```go
	{"ui", "diff_cursor", "row", "diff-view current-line marker: row (background), number (gutter only) or off; the . menu's Cursor marker row switches it for the session"},
```

Replace with:

```go
	{"ui", "diff_cursor", "row", "current-line marker in the diff view (on the cursor's side only) and the View file preview: row (background), number (gutter only; the preview has no gutter, so it falls back to the background) or off; the . menu's Cursor marker row switches it for the session"},
```

In `/mnt/t/others/gigagit.worktrees/feat-line-select-copy/internal/config/config.go`:

```go
	// DiffCursor selects how the diff view marks its current line:
	//   "row"    — a background under the cursor row on both panes. THE DEFAULT.
	//   "number" — only the gutter line numbers are highlighted.
	//   "off"    — no marker (the cursor still drives e / notes).
```

Replace with:

```go
	// DiffCursor selects how the diff view — and the View file preview —
	// mark their current line:
	//   "row"    — a background under the cursor row, on the CURSOR'S SIDE
	//              only in the diff (alt+←/→ move it). THE DEFAULT.
	//   "number" — only that side's gutter line number is highlighted; the
	//              preview has no gutter, so it falls back to the background.
	//   "off"    — no marker (the cursor still drives e / notes / selection).
```

- [ ] **Step 8: Verify the docs mention nothing that does not exist**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-line-select-copy && grep -rn 'selection_bg' README.md docs/CLAUDE-details.md CHANGELOG.md internal/theme/override.go`
Expected: the role appears in the README diff row, the CHANGELOG entry, the
CLAUDE-details section and `roleFields`.

Run: `cd /mnt/t/others/gigagit.worktrees/feat-line-select-copy && grep -rn 'on both panes' internal/ README.md`
Expected: no output (the stale phrase is gone).

- [ ] **Step 9: Final sweep**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-line-select-copy && gofmt -l internal`
Expected: no output.

Run: `cd /mnt/t/others/gigagit.worktrees/feat-line-select-copy && go vet ./internal/tui/... ./internal/theme/... ./internal/config/...`
Expected: no output.

Run (FOREGROUND, Bash timeout 600000):
`cd /mnt/t/others/gigagit.worktrees/feat-line-select-copy && go test ./internal/tui/... ./internal/theme/... ./internal/config/... ./internal/i18n/...`
Expected: PASS.

Do NOT run `./test.sh` or `./test.sh race` — the controller runs the race gate
on the finished branch.

- [ ] **Step 10: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-line-select-copy && cat > /tmp/gg-commit-msg-6.txt <<'EOF'
docs: line selection + copy, and the one-sided diff cursor

README (the diff, View file and blame rows, the selection_bg role and a
diff_cursor paragraph that now covers the preview and the one-sided
marker), CHANGELOG, and a CLAUDE-details section describing the shared
lineSel leaf, the action-row copy seam, the three painting routes and the
invalidation rules — plus a correction to the phase-0 cursor paragraph,
which still said the marker covered both panes. The [ui] diff_cursor
settingDoc and its config doc comment lose the same stale claim.

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01NDk1DJtgLzZX7hMmxDs9nU
EOF
git -C /mnt/t/others/gigagit.worktrees/feat-line-select-copy add \
  README.md CHANGELOG.md docs/CLAUDE-details.md \
  internal/config/template.go internal/config/config.go
git -C /mnt/t/others/gigagit.worktrees/feat-line-select-copy commit -F /tmp/gg-commit-msg-6.txt
```

---

## Rulings made while planning

These resolve what the spec and the brief left open, or where a naive reading
contradicts the codebase. They are binding; the "why" matters as much as the
ruling.

**R1. The selection hook runs BEFORE the search hook, with a `search.typing`
guard.** The brief asked for it after; the spec's esc order (search typing →
selection → committed query → close) makes that impossible, because each host's
search hook — `diffSearchKey`, `blameSearchKey`, `previewSearchKey` — handles
the typing case AND the committed-query case in ONE call. Running the selection
hook first and having it decline while `search.typing` puts it exactly between
the two. The resulting per-host order, verified against each host's existing
routing:
- **Diff** (`updateDiffViewKey`): the preamble (wrap/file arms, `diffNotice`,
  `zCycle`) → `diffSelectKey` → `diffSearchKey` → the switch (`esc` → `popLayer`).
- **Blame** (`blameView.update`): ctrl+c → `blameSelectKey` → `blameSearchKey` →
  the switch (`esc`, `b` → `popLayer`). `b` is never two-stage and never reaches
  the hook's `esc` arm, so it still leaves with a selection on — never trap the
  user.
- **Preview** (`updateFilesViewKey`): `previewSelectKey` → `previewSearchKey` →
  the tree's `/`-filter typing branch → the switch (`esc` → `closePreview`). The
  tree filter can only be typing when the tree is focused (or on a stash list,
  where `previewSearchKey` has already taken `/`), and both hooks decline when
  `m.filesTreeFocused`, so a space can never be stolen from the filter.

**R2. `winRow.body` is a POINTER, not a value.** `lipgloss.Style` holds
`TerminalColor` INTERFACE fields, so `st == lipgloss.Style{}` compiles but is
not a safe "is it set" test (it can panic on an uncomparable dynamic type, and
a zero renderer pointer makes it unreliable anyway). `body *lipgloss.Style` with
nil = unset is unambiguous, is one field rather than two, and keeps every
existing `winRow` literal correct untouched.

**R3. The preview paints through `winRow.style`, not `winRow.body`.** The brief
asked for `body`; the preview's rows carry NO prefix (`renderFilePreview` passes
no `prefixW` and sets no `winRow.prefix`), so `style` already means "the body".
It is also the CORRECT choice, not merely the smaller one: `renderWindow` drops
the syntax class mask when the ROW style reverses, and with `selection_bg` unset
the stripe is exactly a reverse flip — under `body` the classes would survive
and paint per-token BACKGROUNDS, the hazard `winRow.cls`'s own doc comment
warns about. Blame is the only host that needs `body`, because its commit
gutter is `winRow.prefix` and must stay unpainted.

**R4. A reversed BODY drops the class mask too.** `renderWindow`'s guard widens
from `r.style.GetReverse()` to `r.style.GetReverse() || (r.body != nil &&
r.body.GetReverse())`. Without it, a selected non-cursor blame row under the
Terminal theme (style zero, body `Reverse(true)`) keeps its syntax classes and
turns per-token foregrounds into per-token backgrounds — the exact bug the
field's doc warns about. `TestBlameSelectionUnderReverseDropsSyntaxColours`
pins it.

**R5. The diff side field is `onOld bool`, not `curSide int`.** `false` = the
new/right pane = the default, so every constructor (`openStatusDiff`,
`loadStatusDiffCmd`, `loadCommitDiffCmd`, `loadCompareDiffCmd`) and the
`diffViewWith` test fixture are correct with no edit. It is CARRIED across
`diffMsg`'s `*dv = *msg.view` exactly like `search`/`searchOrig` — a reload of
the same file must not throw the user back to the right pane — and is NOT
carried by `inheritIdentity`, so a new file opens on the right.

**R6. The selection stripe is `cellMark.sel` + `bodyFor`, not a new parameter
chain.** Three cell renderers (`segCell`, `scrollCell`, `diffCell`) already take
`mk cellMark`; a bit on it reaches all of them with no signature churn, and
`bodyFor(base)` is one place where "the stripe is laid OVER whatever the cell
already wears" lives. `diffCell` has TWO paths — the enriched one that builds a
`base` and the plain one that renders under `mk.hotFor` / `mk.base` — and BOTH
are wired, or a plain unchanged row inside a range would not stripe. Gap cells
return early through `gapFor()` and are never striped: an absent cell is not
selected, and the range skips it when copying.

**R7. Only the cursor side's mark carries `sel`, and only on a present cell.**
`diffPaneLines` sets it from `v.lsel.contains(dr.line, v.curLine) &&
sidePresent(r, v.onOld)`. A fold row and a note row `continue` before the mark
is built, so neither can ever be striped.

**R8. `contentLine` gains TWO fields, `raw` and `src`, not one.** The brief
asked for `raw` alone with "placeholder ⇒ raw == ''", but an EMPTY SOURCE LINE
is also `raw == ""` and the spec says it is copyable ("an empty source line IS
copyable (it is a line)"). `src` — true for every line that came out of the file
— separates the two without a second string. `fileContentLinesTok` sets
`src: true` for every split line and leaves the four placeholder constructions
(`(empty file)`, `(loading…)`, `(file too large to preview)`,
`(load failed: …)`) at the zero value. `TestFileContentLinesPlainPathUnchanged`'s
hand-copied oracle learns both fields so it keeps testing `text`, and
`TestPreviewSelectionCopiesRawText` asserts them against literals instead.

**R9. Blame and the preview need no "nothing to copy" notice.** The brief asked
for one in each host; it is unreachable in two of them. Every blame row is a
line, so a blame range always has content. In the preview, `space` is INERT on a
line with `src` false, so a range can never consist entirely of uncopyable
lines. The notice exists only in the diff, where a range can legitimately hold
nothing on the cursor side (an all-`Add` range read from the left), and it rides
on `m.diffNotice` — the diff's existing bottom-left transient, cleared by the
next key. Neither blame nor the preview has such a surface, which is the second
reason not to invent one.

**R10. The footer arithmetic, and why `[z] align` goes.** The scroll variant was
at EXACTLY 140, the cap. `[spc] mark` and `[alt←→] side` cost 26 columns with
their separators. The shortenings available without losing a group come to 14
(`scroll/line`→`scroll` −5, `[c/}{]`→`[c}{]` −1, `[←→0]`→`[←→]` −1,
`hist/blame`→`hist` −6, `close`→`back` −1), so one group HAD to go. `[z] align`
is the one with three dedicated `.` menu rows (**Align cursor line: top /
center / bottom**) plus a help row, so nothing becomes undiscoverable; `[e]
edit` and `[f] part` stay. That lands at 141 — one over — and `[alt↔]` rather
than `[alt←→]` pays the last column. `↔` is already gg's own glyph for "both
directions" (a two-sided compare's title reads `a ↔ b`), it is East-Asian-
ambiguous and therefore one column wide exactly like the `↑↓←→` the line already
counts as one, and the help row spells the binding out as `alt+←/→`. Final
measurements: scroll **140**, wrap 128, trunc 129; the selection variant 86.
`TestDiffHintFitsTheBudget` pins the 140 and now also caps the selection
variant.

**R11. The selection footer is a SEPARATE function, not a parameter.**
`diffSelectHint()` sits beside `diffHintFor(long)`, and `renderDiffView` picks
between them. That leaves `diffHintFor`'s signature — and the `?` key's call
site, which opens the help with the footer's keys listed first — untouched, and
it is honest: the selection variant is mode-independent, so threading `long`
through it would be a lie. Blame and the preview do the same inline, each being
one `if`.

**R12. `[ui] diff_cursor` governs the preview, and `number` falls back to the
band there.** No new config key (the spec forbids one). The preview has no
gutter to carry a number-only marker, so anything other than `"off"` paints the
band — `renderFilePreview` tests `m.cursorStyle() == "off"` and nothing finer.

**R13. `enter` is bound only where it had no meaning, or only under a
selection.** The diff has no `enter` case at all today, so `diffSelectKey`
declines it without a selection and it stays unbound. Blame's `enter` opens the
history of the commit under the cursor and the preview's moves focus to the
tree; both hooks decline when `lsel.on` is false, so both keep their meaning
exactly. `TestBlameEnterWithoutSelectionOpensHistory` and
`TestPreviewEnterWithoutSelectionFocusesTree` guard the two.

**R14. The mouse CAN pick the pane.** `renderDiffView` pads every line to
`overlayDims()`' full width and the layer renders it at 0,0, so `msg.X` is the
pane column directly — no layout refactor, and the spec's convenience ships.
`paneW` mirrors `diffPaneLines` (`(w-1)/2`, floored at 4). `msg.X == paneW` is
the `│` separator and leaves the side alone; a live selection keeps the side
locked, so a click then moves only the row.

**R15. Row ids are shared across the three hosts on purpose.** All three use
`copy-line` and `copy-selected-lines`. Only one reader is ever the context at a
time (`contextCopyRows` dispatches on the front surface), so they can never
collide, and one id means one `rowByID` lookup in all three key hooks. The
labels differ where the side matters (`Copy line (old side)`).

**R16. The line rows LEAD each host's copy rows**, before the file path/name/
commit rows — the spec's placement, and it also keeps `copy-file-path` present
as the anchor `insertCopyLinkRow` looks for, so the `.` menu's **Copy link** row
does not move.

**R17. The stash short-circuit must yield to an open preview.**
`availableActions` computes `onStashList` from `stashView != nil && focus ==
panelCommits && !filesTreeFocused && diffLayer() == nil` and returns the
Apply/Pop/Drop menu before `contextCopyRows` is ever called. A preview opened
from a stash file tree leaves the tree UNFOCUSED, so it matched — and the
preview's own line rows were unreachable. `&& m.filesPreview == nil` fixes it:
a preview owns the right column, and the stash list is not the selection then.

**R18. The paint tests assert a SUBSET of the SGR parameters.** `subsetOf(want,
got)` over `sgrBefore`'s parameter set, never an exact match: lipgloss packs
several attributes into one `ESC[…m`, so an exact-set comparison breaks the
moment an unrelated attribute joins the run. The expected parameters are always
DERIVED at runtime (`sgrParams(st().diffCursorRow.Render("x"))`), never
hard-coded — the phase-6 lesson — and the sequence is located with
`sgrSeqBefore`, never by cutting the row with `ansi.Cut`, which drags a later
run's state into an earlier slice.

**R19. Invalidation sites, exactly.** Diff: `rebuild()` (the `f` toggle calls
it), the `ctrl+w` case explicitly (it calls `relayout` + `reanchorAfterRebuild`,
not `rebuild`), and `diffMsg` after the side carry-over. NOT `relayout()`
itself — a terminal resize goes through it and must not throw a selection away.
Blame: `blameMsg`. Preview: `fileContentMsg` (both the success and the error
branch) and, implicitly, `closePreview`/`openPreviewSrc`, which replace the
whole struct. Search keys never clear anything.

**R20. Scope.** No web change, no CLI change, no `agentskill` change, no new
config key, no e2e scenario (the feature is keyboard-and-paint, which the e2e
harness — real repo, CLI runs, semantic state assertions — cannot observe). The
hunk picker is explicitly out of scope per the spec, and its `space` (pick a
region) is untouched.

## Self-review

**Spec coverage (§4.7, lines 957–1149), paragraph by paragraph.**
- *Hosts and their cursors* — the preview's `cur`, its band under
  `[ui] diff_cursor` with `number` falling back (R12), `↑/↓`/wheel/page keys
  still scrolling only, `alt+↑/↓` moving one line and scrolling minimally,
  `j`/`k` left unbound (they are the tree's), search `]`/`[`/`enter` landing the
  cursor and `searchPos` deriving from it, cursor 0 on open, `fileContentMsg`
  resetting it, blame unchanged: **Task 5**.
- *Raw text* — `contentLine.raw` from the normalized-but-unsanitized line, an
  empty source line copyable, a placeholder not (R8): **Task 5**.
- *The diff cursor sits on one side* — `onOld`, `alt+←/→`, the click, the
  one-sided marker including the gap cell in `row` mode, the other cell's
  attention band winning, the header's side-first label, `e` unchanged
  (`editLine` reads `RightNo` and is not touched): **Task 2**.
- *Absent cell* — `sidePresent`, copy rows hidden, ranges skipping: **Tasks 2–3**.
- *Notes and links follow the side* — `noteAnchorsAtCursor` reordered, both
  sides still listed, the preview-diff old-side rule preserved, the gap-cell
  fallback: **Task 2**.
- *Selection (all three hosts)* — the shared `lineSel` with the exact field and
  method names the spec declares: **Task 1**; the keys, the esc order and the
  `alt+←/→` lock notice: **Tasks 2–5**.
- *What copy produces* — `\n`-joined, no trailing newline, absent cells and
  folds skipped, `Copied line N` / `Copied N lines`, an empty range a no-op with
  the notice, `Copy line` copying one line with no newline: **Tasks 3–5**.
- *Painting* — `selectionStyle`'s two modes, Dark/Light values, Terminal unset,
  roles 54 → 55 with all three pins, TEXT only (not the blame gutter, not the
  diff gutter), cursor-side present cells only in the diff, fold and note rows
  never painted, the stripe replacing the band on the cursor row in `row` mode:
  **Tasks 1, 3, 4, 5**.
- *Invalidation* — R19: **Tasks 2, 3, 4, 5**.
- *`.` menu rows* — the diff gating, blame's always-on `Copy line`, the new
  preview case that also returns `fileCopyRows(p.title, m.filesHash)`, the
  key-and-row shared path: **Tasks 3, 4, 5**.
- *Footer and help* — the diff's three mode variants inside 140 with `[/] find`
  second and the selection variant, blame's two, the preview's two (in-box and
  status bar), the files-view tree footer untouched, help rows for all three
  hosts, every changed literal deleted and re-added in four bundles:
  **Tasks 3, 4, 5**.
- *Config and docs* — no new key, the `diff_cursor` settingDoc naming the
  preview, README, CHANGELOG, `docs/CLAUDE-details.md`: **Task 6**. The spec
  also asks for the §6 phase-5 row to replace `v`/`y` with space/enter and point
  here; §4.7 IS that pointer and the row's text is the spec's own, so Task 6
  leaves the spec file alone — the plan does not edit its own spec.
- *Tests (the review gate)* — every named test is in R11's list below.
**No gaps.**

**Test list against the brief's R11.** `lineselect_test.go` table (Task 1) ✓;
diff: `TestDiffCursorPaintsOneSide` (three modes, band present on the cursor
cell and absent on the other, `alt+left` flips it, the gap cell on an Add row
still wears it) ✓, `TestDiffHeaderNamesTheCursorSide` ✓,
`TestDiffSelectionCopiesTheFrozenRange` ✓, `TestDiffSelectionSkipsAbsentCells`
+ `TestDiffSelectionSkipsFolds` (the brief's single
`TestDiffSelectionSkipsAbsentCellsAndFolds`, split because the two need
different fixtures — a three-row side file and a 40-row partial-mode file) ✓,
`TestDiffSelectionLockedToSide` ✓, `TestDiffSelectionPaintsOnlyTheCursorSide`
✓, `TestDiffRebuildClearsSelection` ✓, `TestNoteAnchorFollowsCursorSide` ✓;
blame: `TestBlameSelectionCopies` ✓, `TestBlameEnterWithoutSelectionOpensHistory`
✓, `TestBlameEscClearsSelectionBeforeClosing` ✓,
`TestBlameSelectionPaintsTheCodeNotTheGutter` (the brief's
`TestBlameSelectionPaint`) ✓; preview: `TestPreviewAltDownMovesCursorNotViewport`
✓, `TestPreviewDownScrollsNotCursor` ✓, `TestPreviewCursorPaint` ✓,
`TestPreviewSelectionCopiesRawText` (with a tab in the source) ✓,
`TestPreviewSearchHitLandsCursor` ✓, `TestPreviewCopyLineAbsentOnPlaceholder`
✓; theme: the two role-count pins plus
`TestSelectionStyleFlipsReverseWhenTheRoleIsUnset` /
`TestSelectionStyleUsesTheThemeBackgroundWhenSet`, mirroring the
`currentHitStyle` pair ✓. Extra beyond R11, each earning its place:
`TestDiffAltArrowsLockedWhileSelecting` (Task 2, the lock before the keys that
set it exist), `TestDiffOneSpaceSelectionFollowsTheCursor` and
`TestDiffSelectionWithNothingOnThisSideNotices` (the spec's tmux rule and the
empty-range notice), `TestDiffEscClearsSelectionBeforeClosing`,
`TestDiffClickPicksThePane`, `TestDiffMsgCarriesTheCursorSide`,
`TestDiffSelectionFooterVariant`, `TestDiffSelectionOnAChangedRowStillPaints`,
`TestBlameCopyLineRow`, `TestBlameMsgClearsSelection`,
`TestBlameSelectionUnderReverseDropsSyntaxColours` (R4),
`TestRenderWindowNilBodyIsUnchanged`, `TestPreviewCopyLineRow`,
`TestPreviewEnterWithoutSelectionFocusesTree`,
`TestPreviewEscClearsSelectionBeforeClosing`,
`TestPreviewLoadArrivalResetsCursorAndSelection`, `TestPreviewHintVariants`.
`t.Parallel()` is on every one except those calling `lipgloss.SetColorProfile`,
which carry the file-top note; no test calls `t.Setenv`.

**Placeholder scan.** No "TBD", no "similar to Task N", no "add error handling",
no "write tests for the above". Every code step carries the code; every edit to
existing code quotes the surrounding lines it anchors on; every i18n step
carries all four translations; every new function has its doc comment in the
step.

**Type and name consistency.** `lineSel` with `start`/`mark`/`press`/`clear`/
`bounds`/`contains` (Task 1) is used with those names in Tasks 2–5. The host
field is `lsel` everywhere (`diffView.lsel`, `blameView.lsel`,
`contentPopup.lsel`). `onOld bool` (never `curSide`) in Tasks 2, 3 and 6.
`sidePresent(r, onOld)` and `cursorCell()` (Task 2) are used by `selectedLines`
and `diffCopyLineRows` (Task 3) and by the paint step. `selectionStyle` /
`selectionBg` / `Theme.Selection` / `Override.Selection` / `selection_bg`
(Task 1) are used with those exact spellings in Tasks 3–6. `rowByID` (Task 3) is
used by Tasks 4 and 5. `cellMark.sel` + `bodyFor` (Task 3) are diff-only.
`winRow.body *lipgloss.Style` and the six-plus-one-parameter `colouredLine`
(Task 4) have exactly two call sites, both edited in that task. `contentLine.raw`
/ `.src`, `contentPopup.cur`, `previewOrigin{sel, cur, hscroll}` (Task 5) are
used only within Task 5 and Task 6's prose. Row ids `copy-line` /
`copy-selected-lines` and ok messages `Copied line %d` / `Copied %d lines` are
identical in all three hosts (R15). Notice strings are the three from R3 of the
brief, unchanged. Footer strings are the measured ones from R10, and the same
literals appear in the code step, the i18n step and the test that pins them.

**i18n completeness.** Task 2 adds 2 keys; Task 3 deletes 3 and adds 15; Task 4
deletes 3 and adds 5 (and REUSES the `space` help literal Task 3 added — a
duplicate TOML key would be a parse error, so the step says not to add it
twice); Task 5 deletes 3 and adds 5. Every add and delete is spelled out for all
four bundles in the same step, and therefore the same commit, so
`TestI18nBundlesComplete` passes at every commit boundary. `"clear the search,
then close"` is explicitly NOT deleted — the files-view help row still uses it.





