# Commit highlight-search (`@`) — Design

**Date:** 2026-06-21
**Status:** Approved (brainstorm)
**Scope:** TUI only (`internal/tui`). No engine / domain / git / CLI / agentskill changes.

## Problem

The Commits panel today has one search, bound to `/`: it **filters** the feed so
only matching commits remain visible. That is the right tool for "show me only
the commits about X", but it loses the surrounding context — you can no longer
see where the matches sit relative to the rest of history, and the commit graph
is suppressed while a filter is active.

We want a second, complementary search that keeps every commit on screen and
instead **highlights** matches in place, so the user can see matches in context
and step between them.

## Behavior

Activated with `@` while the **Commits panel** is focused (parallel to `/`).

- **Entry.** `@` opens a highlight-search input. The panel label shows `@<query>`
  while active (mirroring how `/` shows `/<query>`).
- **Live highlight while typing.** All commits stay visible. A row whose
  searchable text matches the (case-insensitive) query renders normally; every
  non-matching row is **dimmed** (faint/gray). As the query changes, the cursor
  snaps to the nearest match at or after the current cursor position, wrapping to
  the first match from the top if none follow.
- **Commit graph stays visible.** Unlike `/` (which hides the graph because it
  reorders/filters rows), highlight-search never filters or reorders, so the
  commit graph remains drawn in natural feed order.
- **Commit (`enter`).** Leaving the input keeps the highlight active (exactly
  like `/` keeps its filter on `enter`). The query text remains shown in the
  label.
- **Navigation.** While a highlight is active (typing or committed),
  `ctrl+↓` jumps to the next match after the cursor and `ctrl+↑` to the previous
  match before the cursor. Both wrap around the loaded feed. Plain `↑/↓` (and
  `j/k`, `pgup/pgdn`) keep moving row-by-row across the full list.
- **Exit.** `esc` clears the highlight and restores normal styling.
- **Mutual exclusivity with `/`.** Opening `@` cancels an active `/` filter;
  opening `/` clears any active highlight. Only one of the two is ever active.
- **Match field.** The match test reuses the existing `/` haystack for commits:
  commit subject + full hash + ref/branch decorations (`commitHaystackAt`).

### Edge cases

- **No matches.** Nothing is highlighted; every row is dimmed. `ctrl+↑/↓` are
  no-ops (no match to jump to); the cursor does not move.
- **Empty query.** With `@` open but no characters typed yet, nothing is dimmed
  and nothing is treated as a match (identical to how an empty `/` query is
  inert). `esc` from this state simply closes the input.
- **Cursor already on a match.** `ctrl+↓`/`ctrl+↑` move to the *next*/*previous*
  match, not the current row.
- **Feed paging.** Highlighting and navigation operate over the currently loaded
  feed only, exactly as `/` does today. Matches in not-yet-paged history are not
  considered until those commits load.

## Architecture

TUI-only. Three pieces, all on the existing render/keys plumbing.

### 1. View-state (new fields on `Model`)

Kept **separate** from the `/` filter fields (`filterQuery`/`filterTyping`/
`filterPanel`) so that `filterActive`, `displayIndices`, and `commitGraphOn` are
completely untouched and highlight-search never accidentally filters rows or
hides the graph:

- `highlightQuery string` — case-insensitive substring; `""` = inactive.
- `highlightTyping bool` — true while `@`-input is capturing keys.

`highlightActive` is the predicate `highlightQuery != ""` (committed) **or**
`highlightTyping` (mid-entry). Highlight is Commits-panel-scoped; no panel field
is needed because `@` is only offered there.

Mutual exclusivity is enforced at the entry points: the `@` handler clears the
`/` filter fields, and the `/` handler clears the highlight fields.

### 2. Dim/highlight rendering (render-time decorator)

Reuse the per-visible-row decorator path (the same window-then-style mechanism
lane color uses, `commitDecoratorsRange`). For each **visible** row index `i`:

- if `highlightActive` and `commitHaystackAt(i)` does **not** contain the query
  (case-insensitive) → apply a **dim** style to the rendered row;
- otherwise leave the row's normal styling.

This is O(visible rows), never O(feed) — no per-keystroke full-feed scan. The
selected row keeps its selection styling regardless (selection wins over dim).

An empty query (`highlightTyping` with no text) dims nothing.

### 3. `ctrl+↑/↓` match navigation

A helper that, given the current cursor and a direction, scans the loaded feed
from the cursor outward until it finds a row whose haystack matches, wrapping
once around the feed; returns the new index or "no match" (cursor unchanged).
Cost is O(distance to the next match), not O(feed).

`ctrl+↓` = forward, `ctrl+↑` = backward. Wired in two places so it works both
mid-entry and after `enter`:

- inside the highlight-typing key loop, and
- in the main Commits-panel key switch, gated on `highlightActive` (so it does
  not shadow `ctrl+↑/↓` on other surfaces — those live in their own views).

Because the cursor "snap to nearest match" on type uses the same forward scan,
that scan logic is the single source of truth for both behaviors.

### Keys & input capture

`@` entry mirrors `/` entry: it is a new `case` in the Commits-panel key switch
that sets `highlightTyping = true`, clears `highlightQuery`, and clears the `/`
filter fields. The highlight-typing capture loop mirrors the existing
`filterTyping` loop (runes append, backspace, space, `enter` commits, `esc`
clears) with two differences: `ctrl+↑/↓` jump matches, and a query edit snaps the
cursor to the nearest match rather than resetting it to row 0.

## Files

- `internal/tui/model.go` — new `Model` fields; `@` entry case in the Commits
  key switch; highlight-typing capture branch; `ctrl+↑/↓` in the committed-state
  Commits switch; `/` entry clears highlight; `esc`/clear handling.
- `internal/tui/view.go` — dim decorator in `commitDecoratorsRange`; panel label
  shows `@<query>` when highlight active.
- New `internal/tui/commit_highlight.go` (or similar) — the match predicate and
  the forward/backward "nearest/next match" scan helper (the single source of
  truth), kept out of the already-large `model.go`/`view.go`.
- `internal/tui/footer.go` / `internal/tui/help.go` — footer hint + help entries
  for `@` and `ctrl+↑/↓` (next/prev match).
- `CHANGELOG.md` — "Added" entry.
- Tests: `internal/tui/commit_highlight_test.go` (+ render assertions where the
  dim styling is involved).

## Testing

- **Match predicate / scan helper** (pure, table-driven): next/prev with wrap,
  no-match returns unchanged, cursor-on-match skips to the following match,
  empty query = no matches.
- **Entry/exit & mutual exclusivity** (drive `Update`): `@` opens typing and
  clears an active `/` filter; `/` clears an active highlight; `esc` clears
  highlight; `enter` keeps `highlightQuery` and ends typing.
- **Live cursor snap**: typing a query moves the cursor to the nearest match.
- **`ctrl+↑/↓`** move the Commits selection between match rows (committed state),
  with wrap.
- **Render** (real-git `loadedModel`-style, against a feed with known subjects):
  with a highlight active, the assembled `View()` shows matching rows
  un-dimmed and non-matching rows carrying the dim style, and the commit graph
  is still present. Use `termenv.TrueColor` + an end-to-end render assertion
  (per the project's lane-color lesson: lipgloss emits no color in non-TTY
  tests, so only an end-to-end `View()` assertion proves the styling survives).
- **Perf guard**: highlight rendering touches only visible rows — assert the dim
  decorator is computed for the windowed range, not the whole feed (mirrors the
  existing `TestFilteredDisplayIndicesSkipsRowStyling` guard style).

## Out of scope (v1)

- **Match count** ("3/17"). A live count is O(feed) per keystroke, which on the
  large repos this tool targets reintroduces the per-keystroke cost the recent
  render-perf work removed. Omitted; trivially addable later if wanted.
- Highlight-search in any panel other than Commits.
- CLI / scripting surface (this is an interactive navigation aid).
