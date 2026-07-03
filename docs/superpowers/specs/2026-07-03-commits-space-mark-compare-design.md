# Commits panel: space-mark & auto-compare

**Date:** 2026-07-03
**Status:** approved

## Problem

Comparing two commits today takes four steps: `m` on the first commit, move,
`m` on the second, then `.` → "Compare the 2 selected commits". The menu labels
("Add to compare selection" / "Remove from compare selection" / "Clear compare
selection") also don't read as *mark/unmark*, so the affordance is easy to
miss. There is no single-gesture path from "I'm looking at two commits" to
"show me the diff between them".

## Decision summary

- **Space shares the existing ◉ `commitCompareSet`** — one marking concept.
  Space is a fast-path gesture over the same set that `m`, shift+↑/↓, the
  `.`-menu rows, squash, and drop already use. No second selection concept.
- **Space caps at two marks.** With ≥2 valid marks, space on an unmarked row
  refuses with a status hint. It never grows a 2+ set (a 3+ set built with
  `m`/shift+↑↓ for range-compare/squash/drop keeps working; space just won't
  add to it).
- **The second space-mark always opens the compare immediately.**
- **Re-opening the same pair is a no-op** when the files view already shows
  that comparison (no visible refresh).

## 1. The space gesture

Space in the Commits panel currently routes to `handleStageKey()`, whose
`canStage()` gate requires a files panel — so space is a free key there. The
space dispatch in `model.go` branches: `m.focus == panelCommits` → new
`handleCommitSpaceKey()`; all other panels keep the staging behavior.

`handleCommitSpaceKey` keys off `selectedKey(panelCommits)` — the same stable
row key `handleMarkKey` uses — so ◇ Working-tree / ◇ Staged WIP pseudo-rows
participate exactly as they do with `m`. State machine over
`m.commitCompareSet`, counting only `validCompareKeys()` (the set is
deliberately stale-tolerant; raw `len` would let an off-feed ghost mark eat
one of the two slots):

| Cursor row | Valid marks | Action |
|---|---|---|
| marked | any | unmark it (space is always a toggle on a marked row) |
| unmarked | 0 | mark it |
| unmarked | 1 | mark it **and open the compare** |
| unmarked | ≥2 | refuse; status: `2 commits already marked — space a marked one to unmark, or . → Unmark all` |

The compare uses `compareSelectionEndpoints()` — the same older→newer
endpoint resolution as the `.`-menu row, including its commit↔working-tree /
commit↔staged support. Marks **persist** after the view opens (matching the
`.`-menu compare); esc returns to the Commits panel with both ◉ still set.

Like `handleMarkKey`, the toggle itself is not gated on `opsIdle()`; the
compare open is a read-only files-view load and needs no gate either.

Windows note: `KeyRunes{' '}` is already normalized to `KeySpace` at the top
of the key handler, so the new handler keys off `tea.KeySpace` only.

## 2. Same-pair no-refresh guard

Implemented **inside `openCompareFiles`**, so every entry point (space,
`.`-menu, bookmark/shelf compares) gets it for free:

- If the files view is already open in compare mode
  (`m.filesMode == filesModeCompare`) and the new pair's tag
  (`"cmp:" + left.CacheTag() + ":" + right.CacheTag()`) equals the current
  `m.compareTag`, return without rebuilding the view or re-running the load.
  Endpoints are canonically ordered older→newer by feed rank before the tag
  is built, so the same pair always produces the same tag regardless of
  marking order.
- A **failed** compare load (`compareFilesMsg.err != nil`) clears
  `m.compareTag`, so retrying the same pair after an error re-opens instead
  of being swallowed by the guard. An in-flight load (tag set, result
  pending) is skipped like a finished one — the pending message will land.

## 3. Context-menu rows (`commit_scope.go`)

Relabeling + visibility on the two existing rows; no new machinery:

| State | Row shown |
|---|---|
| cursor commit **marked** | **"Unmark commit"** (today's "Remove from compare selection") |
| **≥2 marks** in the set | **"Unmark all commits (N)"** (today's "Clear compare selection"; N = valid count) |
| exactly **1 mark, cursor elsewhere** | **"Unmark the marked commit"** (preserves today's ability to clear a single off-cursor or stale mark, which would otherwise be menu-unreachable) |
| cursor commit unmarked | "Add to compare selection" stays, label gains the space hint (e.g. "Add to compare selection (space)") |

"Unmark all commits (N)" gates on raw set size ≥ 2 (like today's clear row)
but displays the valid count, mirroring how the compare row counts.

## 4. Advertising & docs

- `help.go` Commits section: a `space` row describing mark / unmark /
  auto-compare-on-second and the 2-mark cap.
- Commits panel footer: advertise the space binding (per the
  help-AND-footer convention).
- CHANGELOG entry; README if the Commits-panel key table is listed there.

## Testing

Unit tests on `Model` (existing commit_scope/mark test patterns):

1. Space marks an unmarked commit (set gains the key).
2. Space on a marked commit unmarks it.
3. Second space-mark opens the compare (filesView non-nil, mode compare,
   correct older→newer endpoints) and both marks persist.
4. Space on a third unmarked commit with 2 marked: set unchanged, status
   message set.
5. A stale mark (key not in feed) does not consume a slot: with 1 valid +
   1 stale mark, space on an unmarked commit still marks-and-compares.
6. `openCompareFiles` with the tag already showing: same model state, no
   new load command (no visible refresh).
7. Failed compare load clears the tag; the same pair re-opens afterward.
8. Menu rows: "Unmark commit" only when cursor marked; "Unmark all commits
   (N)" only at ≥2; "Unmark the marked commit" only at exactly 1 with cursor
   elsewhere.
9. WIP row: space toggles the sentinel key like `m` does; commit + WIP pair
   compares.

## Out of scope

- No change to `m`, shift+↑/↓, squash, drop, or the 3+ range-compare
  semantics.
- No new marker glyph — space marks render as the existing ◉.
- No CLI surface (TUI-only gesture).
