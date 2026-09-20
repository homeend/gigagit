# Forge PR markdown — plan 2: the TUI

> Executed inline by the session that wrote it (project rule: NEVER subagents),
> task by task, TDD, one commit per task. Steps use `- [ ]` for tracking.

**Goal:** the PR hub popup and the forge review-thread boxes in the PR diff
render pull-request text as markdown, from the parser plan 1 landed.

**Architecture:** one layout function, `mdRows(doc, width) []mdRow`, turns a
`markdown.Doc` into text rows plus a per-rune class mask. The mask is the
EXISTING `[]syntax.Class` channel (`contentLine.cls` → `winRow.cls` →
`styledRuns` → `styles.syntaxStyle`), extended with TUI-local pseudo-classes
for inline styles — so wrapping, horizontal scroll, search emphasis and the
reverse-video rule all keep working with no new mask plumbing.

**Spec:** `docs/superpowers/specs/2026-09-20-forge-prs-markdown-design.md` §7,
amended by this plan (recorded in the spec in Task 4):

- **No new theme roles.** Inline styles are terminal ATTRIBUTES over the
  host's base style (bold, italic, strikethrough, underline, faint); only
  inline code and block code use colour, and they reuse the syntax palette.
  The Terminal theme therefore renders markdown too. (YAGNI: roles can be
  added when someone wants to recolour them.)
- **The hub does not pre-wrap.** The popup is `modeWrap`: `renderWindow`
  wraps each row at render time with its intrinsic hang indent
  (`wrapAlignIndent`), masks included — so ctrl+t maximize needs no re-lay.
  `mdRows(doc, 0)` emits one row per logical line. Note boxes DO pre-wrap
  (`mdRows(doc, innerW)`), as their plain rows always have.

## Global constraints

- Worktree `/mnt/t/others/gigagit/.claude/worktrees/forge-prs-markdown-tui`,
  branch `feat/forge-prs-markdown-tui`; absolute paths for Write/Edit.
- Forge text only: local notes and every other popup render exactly as today.
- Every rune of forge text passes `sanitizeLine` BEFORE its mask is built, so
  `len(cls) == len([]rune(text))` always and no escape reaches the terminal.
- New user-visible strings go through `i18n.T` with keys in all four bundles.
- New tests call `t.Parallel()`; tui tests never use a raw `NewExecRunner`.
- Commits end with the two trailers.

## Tasks

### Task 1: pseudo-classes + `mdRows`

**Files:** create `internal/tui/md_render.go`, `internal/tui/md_render_test.go`;
modify `internal/tui/styles.go` (`syntaxStyle`).

**Produces:**

```go
const ( mdStrong syntax.Class = 100 + iota; mdEm; mdStrongEm; mdDel; mdCode;
        mdLink; mdDim; mdRef; mdHeading; mdQuote )
type mdRow struct { text string; cls []syntax.Class } // len(cls) == runes(text)
func mdRows(doc markdown.Doc, width int) []mdRow      // width <= 0: no wrapping
func mdInlineRows(in []markdown.Inline, width int, lead string) []mdRow
```

`syntaxStyle(base, c)`: `c >= mdStrong` → `mdStyle`: strong bold · em italic ·
strongEm both · del strikethrough · code = the String syntax colour (no colour
in the theme → bold) · link underline · dim faint (a link's ` (url)` tail, the
suggestion caption, table rules) · ref bold · heading bold+underline · quote
faint. Layout rules:

- One blank row between top-level blocks; none inside a tight list.
- `p`: split at `br`; each line word-wrapped to width (hard-split a word wider
  than the width), display-width aware (`lipgloss.Width`, wide glyph = 2).
- `h`: the text, all `mdHeading`.
- `list`: marker `• `, `N. `, `☐ ` / `☑ `; an item's first row carries the
  marker, its other rows a same-width indent (the hang); nested blocks recurse
  at `width - markerW`.
- `quote`: `│ ` (class `mdQuote`) before every inner row, inner text `mdQuote`
  where it has no class of its own.
- `code`: caption row `suggestion` (i18n, `mdDim`) for a suggestion block; each
  line `  ` + text, tabs expanded by `sanitizeLine`, the parser's token runs
  mapped onto the mask (offsets re-based after sanitising — a line whose
  sanitised rune count differs from the source's drops its colours rather
  than mis-colouring); never wrapped, clipped to width when width > 0.
- `table`: columns sized to their widest cell (display width), ` │ ` between,
  header `mdStrong`, a `─┼─` rule under it (`mdDim`), right/center alignment
  honoured; never wrapped, clipped to width when width > 0.
- `hr`: `─` × min(width, 40) (20 when unwrapped), `mdDim`.
- link → its text `mdLink` + ` (url)` `mdDim` (omitted when the text IS the
  url); image → `[image: alt]` `mdLink` + ` (url)`; ref → `mdRef`.

- [ ] Step 1 — failing tests: a `rowsText` helper (rows joined by `\n`) and a
  `clsAt(row, substr)` helper. Cases: emphasis classes land on exactly their
  runes; `a\nb` is two rows; a 30-col wrap of a long paragraph keeps every row
  ≤ 30 wide and loses no word; nested + task + ordered list text layout
  (`• a`, `  • c`, `3. x`, `☑ done`) and the hang indent of a wrapped item;
  quote prefix on every row incl. wrapped ones; go fence → `func` runes carry
  `syntax.Keyword`, the caption row for `suggestion`, a 100-col code line is
  clipped at width 40 not wrapped; table → aligned columns (`Name  │ Count`,
  right-aligned numbers), wide-glyph cell (`日本`) keeps the column straight;
  link/image/ref text; `\x1b[31m` in text never reaches a row; every row of
  every fixture in `internal/markdown/testdata/*.md` satisfies
  `len(cls) == len([]rune(text))` and (width 50) `lipgloss.Width(text) <= 50`.
  `syntaxStyle`: `mdStrong` over a base renders bold, `mdCode` takes the
  String colour, a plain syntax class is unchanged (byte-identical to before).
- [ ] Steps 2–4 — run red, implement, run green.
- [ ] Step 5 — commit `feat(tui): mdRows — a parsed markdown tree as styled rows`.

### Task 2: the PR hub

**Files:** modify `internal/tui/pr_hub.go`; tests in `internal/tui/pr_hub_test.go`
(extend the existing hub tests).

- `prMarkdownLines(body, indent string) []contentLine` replaces `prTextLines`
  for the description, conversation bodies, outdated roots and replies:
  `mdRows(markdown.Parse(body), 0)` → `contentLine{text: indent + row.text,
  cls: pad(indent) + row.cls}`. `prTextLines` stays for the HUNK tail (code,
  not markdown).
- [ ] Failing tests first: a body `# T\n\n- [x] a **b**` yields rows `  T`,
  ``, `  ☑ a b` with `mdHeading` / `mdStrong` on the right runes and
  `len(cls) == runes(text)`; a plain-prose body yields the SAME texts as
  before (compare with `prTextLines`); the hunk tail is untouched; a
  rendered snapshot of the popup at 60 cols shows the wrapped list item
  hanging under its text, and at the maximized width re-wraps (no pre-wrap).
- [ ] Commit `feat(tui): the PR hub renders markdown`.

### Task 3: forge note boxes in the PR diff

**Files:** modify `internal/tui/diff_notes.go` (`noteLine.cls`,
`noteBodyLines`), `internal/tui/diff_render.go` (`noteRowCells`); tests in
`diff_notes_test.go` / the forge-note tests.

- `noteLine` gains `cls []syntax.Class`. For a note whose `Source` is forge:
  summary rows = `mdInlineRows(markdown.ParseInline(r.SummarySrc), w, head)`
  (the `↳ author: ` head is plain; an empty `SummarySrc` — a label summary —
  falls to the plain path); rationale rows = `mdRows(markdown.Parse(
  r.Note.Rationale), w)`. Local notes take today's path, byte-identical.
- `noteRowCells` default case: `nl.cls != nil` → pad text and mask to `inner`
  and paint with `styledRuns(runes, zeroEmph, cls, text)`; a stale box drops
  the mask (grey throughout, as today).
- [ ] Failing tests first: a forge thread with `**bold**` summary + list +
  suggestion rationale lays out rows whose text has no markers, whose masks
  match, all ≤ innerW; the painted row contains a bold SGR for the strong
  run (theme Dark) and the row's visible width is exactly paneW; a LOCAL
  note with the same text keeps its asterisks and has nil masks; the
  collapsed row is unchanged.
- [ ] Commit `feat(tui): forge review threads render markdown in the PR diff`.

### Task 4: web nit, i18n, docs

- Web: a suggestion block that is the FIRST block of a note whose bold summary
  is already the label `suggestion` drops its caption (it said "suggestion"
  twice) — `files.js` passes `{ skipFirstCaption: n.summary === "suggestion" }`
  through `mdHTML(doc, esc, opts)`; node test + the md.mjs S-checks updated.
- i18n: `suggestion`, `[image: %s]`, `[image]` keys in ja/ko/zh/ru.
- Docs: CHANGELOG, README (drop "the TUI still shows PR text unrendered"),
  `docs/web-tui-parity.md`, `docs/CLAUDE-details.md` (a TUI subsection), the
  spec's §7 amendments above, `CLAUDE.md` `markdown` row ("the TUI lays it out
  in rows").
- [ ] Commit `docs: PR text renders as markdown in the TUI`.

### Task 5: verification + gate

- [ ] `./tui-capture.sh` against the fake-gh fixture (`prtui/` recipe in the
  memory file, markdown bodies from `prweb/*.md.json`): the hub popup at 100
  and 60 columns and maximized; the PR diff thread with list + suggestion +
  inline code. Read the snapshots: no markers, no escape garbage, frames
  intact, hang indents right. Reproduce the BEFORE with the installed `gg`.
- [ ] Web: `prweb/runmd.sh` green with the updated S-checks; the caption
  guard removed once → S-check red.
- [ ] `./test.sh race` from a clean tree → stripped verify binary
  (SendUserFile + path) → ASK before merging → after the merge
  `./build.sh install`, `./build.sh web`, remove worktree + branch, memory.
