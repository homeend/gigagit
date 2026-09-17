# Blame: highlight lines changed within a time span — design

Date: 2026-09-17 · Branch: `feat/blame-recent-highlight` · Status: approved

## Goal

In the blame view (TUI and web), let the user press a key, type a time span
("7d", "1d 3h 5m", "36h", "90m"), and have every line whose commit is younger
than that span stand out. A second key turns the highlight off.

## Scope

Blame only. The Commits feed keeps its `@` highlight; branches keep their
age-filter slots. Nothing else changes.

## Keys

| Key | TUI blame | Web blame overlay |
|-----|-----------|-------------------|
| `d` | open the span dialog (also while on, to change the span) | same |
| `D` | turn the highlight off, no dialog | same |

`h` is deliberately avoided: it means "history" in the diff view. `d`/`D` are
free in blame today (`blameSelectKey`, `blameSearchKey`, the external-editor
row and the view's own switch bind neither).

## Dialog

One text field. Prefilled with the last span used (initially `7d`). Enter
parses and applies (highlight ON, span stored); Esc cancels and leaves the
current state untouched. A span that does not parse, or sums to zero, keeps
the dialog open and shows an inline error under the field (the go-to-commit
popup's shape). The web uses the shared `openPrompt` box with the same title
and error behaviour (the prompt re-opens prefilled with the bad text and an
error line).

Title: "Highlight lines changed within the last…"; placeholder/hint:
"e.g. 7d, 1d 3h 5m, 36h, 90m".

## Span grammar (`internal/timespan`, DAG leaf)

```
span  := token (ws* token)*
token := digits unit?
unit  := 'w' | 'd' | 'h' | 'm'
```

- `w` weeks (7d), `d` days (24h), `h` hours, `m` MINUTES — not months. The
  branch-filter age grammar (`branchfilter.ParseAge`, d|w|m|y) is a different
  vocabulary and stays as is; the two never share a field.
- Tokens sum; order does not matter; whitespace between tokens is optional.
- A bare number (no unit) means days, so the old "number of days" habit still
  works.
- Leading/trailing whitespace is trimmed. Case-insensitive units.
- Errors: empty input, an unknown unit, a token without digits, a zero total.

API:

```go
func Parse(s string) (time.Duration, error)
func Format(d time.Duration) string   // "1d3h5m": largest units first, zero parts dropped, "0m" for zero
```

`Format(Parse(x))` is the canonical form shown in badges. The web has a JS
port of both (`parseSpan`/`formatSpan` in filehist.js) pinned to the Go
table by a parity test in `internal/web` (same pattern as the commit-date
layout port).

## Recency rule

A line is recent when `now − authorTime ≤ span`, or the line is uncommitted
(hash `""`, the newest change of all). `now` is wall clock at render time —
NOT the blamed revision's date — so blaming an old commit may highlight
nothing. Documented, not a bug.

Pure helper: `blameRecent(line, now, span) bool` in the TUI; the web computes
the same from the row's `time` field.

## State

TUI: `Model.blameRecent struct{ on bool; span time.Duration; last string }`
(value on the Model, survives closing and reopening blame for the session).
Web: `state.blameRecent = { on, span, last }`. Not persisted across runs
(not in `/api/uistate`, not in prompts.toml). Ruling: session-scoped; revisit
only if asked.

## Painting

### TUI

Recent rows get a background tint over the WHOLE row (gutter and code) via a
new theme role `blame_recent_bg` (`Theme.BlameRecentBg`; Dark `#1F3A26`,
Light `#DFF5E3`). Stacking, most specific wins:

1. cursor row: `st().selectedRow` reverse video, untouched by the tint;
2. selection stripe on the code half (`selectionStyle`), untouched;
3. otherwise recent → the tint as the row style; syntax `cls` foregrounds
   paint on top (a background-only style keeps token colours);
4. otherwise the plain row.

The Terminal theme leaves every role empty; an empty `blame_recent_bg` means
BOLD for the recent row instead of a tint (bold survives syntax colours and
reads in any terminal). Adding the role touches `theme.go` (struct,
`roles()`, Dark, Light), `override.go` (`Override` field + `roleFields`
row + doc) and `styles.go` (`blameRecentStyle(base)`).

### Web

`openFileBlame` stamps each `.bline` with `data-t="<unix>"` (and
`data-u="1"` for uncommitted). `applyBlameRecent()` walks the rows and toggles
`.brecent` from `state.blameRecent` — no refetch. CSS:
`#blame-body .bline.brecent { background: var(--recent-bg) }` with a
variable defined next to the other blame colours. Applied at open and after
every `d`/`D`.

## Advertising

- TUI footer hint gains `[d] recent`; when on, a right-aligned badge
  `≤<span>` (e.g. `≤1d3h5m`) sits in the header like the search badge
  (search badge wins the slot when both are present: `≤7d · 3/12`).
- Web `#blame-hint` gains `d recent lines · D off`; the title shows
  ` · ≤7d` when on.
- Every new TUI string is a literal `i18n.T` key present in all four bundles
  (the AST gates enforce it).

## Tests

- `internal/timespan`: Parse table (each unit, bare number, mixed order,
  no-space, whitespace, case, empty, unknown unit, zero, junk), Format table,
  round-trip.
- TUI: `blameRecent` predicate (exact boundary inclusive, uncommitted,
  Time 0 is old); key tests `d` opens popup, enter with a good span sets
  state and pops, enter with junk keeps the popup with an error, esc leaves
  state, `D` clears; paint test on a 3-line blame (one recent) asserting the
  tinted row's SGR background on both gutter and code, the cursor row still
  reverse, and bold under the Terminal theme; footer/badge strings.
- Theme: existing role-count gates pick up the new role.
- Web: JS source-pin test for the wiring (`data-t`, `.brecent`, `d`/`D` in
  the blame layer's `onKey`); Go↔JS parity test for parse/format.

## Files

`internal/timespan/{timespan.go,timespan_test.go}` (new),
`internal/tui/blame_recent.go` + `blame_recent_popup.go` + tests,
`internal/tui/blame_view.go`, `internal/tui/styles.go`,
`internal/theme/{theme.go,override.go}`, `internal/i18n/*.toml` (four),
`internal/web/static/{filehist.js,style.css,index.html}`,
`internal/web/blamerecentjs_test.go`, `CHANGELOG.md`, `docs/CLAUDE-details.md`.
