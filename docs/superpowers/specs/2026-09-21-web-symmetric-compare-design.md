# Symmetric view for a link comparison in `gg web` — design

Date: 2026-09-21 · Branch: `feat/web-symmetric-compare` ·
Research: `2026-09-21-web-symmetric-compare-research.md` ·
Mock (approved): `mocks/symmetric-compare-mock.html`

## 1. Goal

A comparison of two change-sets (two previews, a pair vs a preview, two
agents' attempts) reads as what it is: two file lists, row-aligned, with gaps
where a set has no such file, one shared diff between them, and a clickable
arrow that says — and flips — the diff's direction.

## 2. Rulings (defaults taken from the research note §6; say so to change)

1. Identical files ARE shown — dimmed, behind a filter chip; default filter
   is *differences*.
2. "Deleted by this set" is distinguished from "not in this set".
3. The flip swaps the DIFF only; the two columns never move.
4. "A side's row opens that set's own diff" — later, not here.
5. Opt-in by a `⇄ symmetric` chip; the choice is a per-user preference in
   `/api/uistate`; below 1200px the chip is disabled and the view falls back.
6. Web only. TUI, CLI, MCP untouched (the wire addition is additive).

**Which comparisons:** every link comparison (`/api/compare-links`) whose two
sides are BOTH bounded sets — saved or straight from the dialog; it is one
screen. A pair landing (`a`+`b`) is excluded: it is a commit diff with notes,
not two sets. An unbounded side has no member list to align, so the server
simply does not offer the view (no `sym` in the answer → no chip).

## 3. Domain — `internal/domain/symrows.go` (new)

```go
type MemberState string // "absent" | "present" | "deleted"

// SymRow is one aligned row of a bounded × bounded comparison.
type SymRow struct {
	Path        string
	Left, Right MemberState
	Differs     bool // the row is in LinkComparison.Files
}

// SymmetricRows aligns the two sets over the UNION of their members, sorted by
// path. ok is false unless both sets are bounded.
func (c LinkComparison) SymmetricRows() (rows []SymRow, ok bool)
```

- State per side: not in `Paths()` → absent; member and `Has(path)` → present;
  member and `!Has(path)` → deleted (the set deletes the file, ruling R6).
- `Differs` comes from `c.Files` — never re-derived, so the view cannot
  disagree with the listing `CompareSets` produced. A row with
  `Differs == false` is: identical content in both, or no content on either
  side (deleted × deleted, deleted × absent).
- Pure over data the comparison already holds: no git call, no new query.

## 4. Wire — `/api/compare-links`

`files` is unchanged (the live-refresh equality check and every existing
consumer keep working). When `SymmetricRows` is ok the answer gains:

```json
"sym": [{"path":"a.go","left":"present","right":"deleted","differs":true,
         "left_spec":"…","right_spec":"…"}]
```

`left_spec`/`right_spec` follow the existing rule (set only where the member's
bytes live off its side's own endpoint), computed for every row — an identical
row is openable too.

## 5. Web

New module `static/symcompare.js`; `files.js` gets dispatch lines only.

**State** (on `state.compare`, dies with the comparison): `sym` (rows or
null), `symFilter` (`diff|all|one|eq`), `flipped`. Preference:
`uiState.symCompare` (bool, saved through `saveUI`).
`symActive() = compare.sym && uiState.symCompare && innerWidth >= 1200`.

**Layout.** `#panes.detail.sym` is a five-column grid:
`left list | handle-less edge | diff | handle | right list`. The right list is
the existing `#files-pane`; the left is a new `#symleft-pane`. Entering the
symmetric view goes straight to the diff stage with the first visible row
open (there is no commits column to keep: a link comparison has none that
matters). `esc` steps back exactly as today.

**Rows.** While `symActive()`, `state.files` holds the VISIBLE sym rows (so the
cursor, `j/k`, the diff stepper and live refresh keep one index space), each
carrying the `files` fields when it differs. Both lists are painted by
`symcompare.renderSymLists()`, which `renderFiles()` defers to:

| side state | look | click |
|---|---|---|
| present | normal row, `M` when it differs | selects the ROW |
| deleted | struck through, `D` | selects the ROW |
| absent | `· · ·` placeholder, `aria-hidden`, no pointer | nothing |

A centre-facing gutter glyph per row: `≠` differs · `=` same · `◁` only left ·
`▷` only right (kind from the two states + `differs`; never from colour
alone). The selected row highlights on BOTH sides. Rows are 24px on both sides
and the two lists scroll together (one `scroll` listener mirrors `scrollTop`).

**Chips** (in `#compare-bar`, replacing the lone `all (N)` while active):
`differences N · all N · one side only N · identical N`, then
`⇄ symmetric`, then the existing `save comparison…`. Keys `1`–`4` pick a
filter, `s` toggles the view, `x` flips — only while a link comparison with
`sym` is on screen and no input has focus.

**Direction bar** (`#sym-dir`, above the diff, only while active):
`<left desc>  [→]  <right desc>`, the old side tinted red, the new green, with
"old side of the diff" / "new side of the diff" under each. Click or `x`
toggles `flipped` and re-opens the current row.

**Opening a row.** Through the existing `openEntryFileDiff`, with
`left/right` = the row's specs (or the sides' own) — swapped when `flipped`,
labels swapped with them. `status` is derived from the two states in the
arrow's direction (from-absent/deleted → to-present = `A`, the reverse = `D`,
else `M`). A row with no content on either side, and an identical row, paint
a notice in the diff pane instead of asking the server
("identical in both sets" / "neither set has content for this file").
No note context: two sets have no note scope.

**Classic view is untouched** — same list, same letters, no arrow — and is
what a narrow window, an unbounded side or the chip switched off shows.

**Live refresh.** `updateLinkCompareFiles` keeps comparing `files`; when they
differ the new `sym` rides the same answer and replaces `compare.sym`.

## 6. Testing

- Domain (`symrows_test.go`, parallel): a fixture with every combination —
  present×present differing / identical, present×absent, absent×present,
  deleted×present, deleted×absent, deleted×deleted; sorted union; `Differs`
  equals membership in `Files`; an unbounded side → `ok == false`. The
  deleted-state guard is watched failing with `Has` ignored.
- Web Go (`linkcompare_test.go`): `sym` present for two bounded links with the
  states above, absent for a point link and for the `a`+`b` form; `files`
  byte-identical to before.
- Web JS: a playwright probe (scratchpad) against a real `gg web` on a fixture
  repo with two saved previews — asserts VISIBILITY: chip shown; both lists
  have equal row counts; a placeholder is not clickable; clicking the left row
  highlights the right one; `x` swaps the diff header's labels; filter counts;
  at 1000px the chip is disabled and the classic list shows. Run first against
  a build with the feature broken (no `sym` on the wire).
- `linksjs`-style Go test pinning that `static/symcompare.js` is embedded and
  imported (the project's "new file 404" guard).

## 7. Out of scope

Folder grouping, N-way (three agents at once), per-side own diffs, the TUI,
showing the view for pairs/previews, remembering the filter or the flip.
