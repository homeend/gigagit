# Unstage picker — design

**Date:** 2026-08-14
**Status:** approved (user confirmed trigger, new-file refusal, and the
"none" → "empty" suffix wording)

## Problem

`H` on the Files panel opens the region/line **staging** picker (index ↔
working tree → `StageHunks`). There is no region/line lane in the other
direction: removing part of a staged change means unstaging the whole file
(`space` on the Staged panel) and re-staging. This is sub-project 2 of the
GitKraken-inspired picker plan: a `git reset -p` analog on the same surface.

## Behavior

**Trigger:** `H` on the **Staged** panel — the mirror of `H` on Files.

- Gate (`canUnstageHunks`): focus on the Staged panel, ops idle, backing row
  present, row is tracked and non-conflicted.
- A file **not in HEAD** (newly added, `KindAdded`) is refused with a status
  message: `new file — space unstages it whole` (user decision). Rationale:
  `StageHunks` sets index *content* and cannot remove an entry, so "take all
  HEAD" would leave an empty file staged — dishonest semantics. Detection is
  by the row's kind before loading, with a load-time fallback (HEAD read
  fails → same message) in case kind classification misses.
- Binary content (either side) refuses like the stage picker:
  `unstage hunks: binary file`.
- No differing regions → `unstage hunks: nothing to unstage`.

**Load:** `loadUnstageHunksCmd(path)` reads the index blob
(`svc.ShowFile(ctx, "", path)`) and the HEAD blob
(`svc.ShowFile(ctx, "HEAD", path)`) off the UI thread — both queries already
exist (the staged diff and staged-editor lanes use them).

**Doc & picker:** `hunkpick.FromDiff(index, head)` +
`doc.SetAll(hunkpick.TakeCurrent)`:

- Left column **staged** (Current) — the current index state, all kept by
  default, so an immediate enter changes nothing. Same "left = what you
  have, default-kept" convention as the stage picker.
- Right column **HEAD** (Incoming) — taking it for a region reverts that
  region of the index to HEAD. Same muscle memory as staging (toggle right
  on / left off; any mix works — both-selectable ordered picks apply).
- `newUnstagePicker`: title `Unstage hunks: %s`, labels staged/HEAD,
  `requireAll=false` (apply freely, like staging).
- Apply → the existing `engine.StageHunks{Path, Content}` — the index entry
  becomes the assembled content, the working tree is untouched. That is
  `git reset -p`. **Zero engine/domain changes**; `StageHunks` is already in
  `opAffectedSources`.

Everything the picker surface already has comes along: side toggles +
checkbox hierarchy, output pane + tab focus + oshift, free view-scroll
(vshift), ctrl+t zoom, z display modes, CRLF display sanitization.

## Carry-list items (folded in from sub-project 1's final review)

1. **`LinePicked` zero-line-side guard** — parity with `SideState`
   (currently unreachable via pickerCell's early return; defensive).
2. **Touched-empty suffix gets its own key**: English changes
   `" — none"` → `" — empty"` (user-approved wording change; more accurate
   for "region decided to resolve to nothing"). New literal key `"empty"` in
   all four bundles with precise translations; the shared `"none"` key stays
   for its other users (settings popup).
3. **Render test for the `%s first` suffix** branch (both sides on, order
   shown) — was untested.
4. **Interleaved multi-line `ToggleSide` clear-order test**: space-pick
   incoming 1, current 1, incoming 2, then `ToggleSide(Current)` clears only
   the current picks and preserves incoming order (i1, i2).
5. **Tiny-overlay output pane**: when the split cannot give the pane its
   spec'd 3-line minimum, hide it entirely (`outH = 0`) instead of degrading
   to 1–2 rows. Tab on a hidden-because-tiny pane keeps current behavior
   (the parked sub-10-row edge remains parked).

## Docs

- Footer: `[H] hunks` entry gated on `canUnstageHunks` (Staged panel).
- Help: Staged-panel section gains the `H` row; the "Hunk picker" section
  header already covers the shared keys (update its parenthetical to name
  all three entry points: x conflict resolve / H stage / H unstage).
- README: Staged-panel documentation (the `space` unstage row's
  neighborhood) gains the `H` row description.
- CHANGELOG bullet. All new user-visible strings: literal i18n keys ×4
  bundles.

## Out of scope

- CLI hunk-level unstage (whole-file `gg unstage` exists; nothing requested).
- Web parity (sub-project 3, on `web-dev`).
- Removing a newly-added file from the index via the picker (refused; whole
  entry unstage is `space`).

## Testing

TDD:

- `internal/hunkpick`: `LinePicked` zero-line guard test; interleaved
  ToggleSide clear-order test.
- `internal/tui`:
  - gate: `canUnstageHunks` true only on the Staged panel for tracked
    non-conflicted rows; `KindAdded` row → refusal message, no layer pushed.
  - load handler: binary refusal; empty-diff refusal; success pushes a
    picker titled `Unstage hunks:` with staged/HEAD column labels and
    everything default-kept (enter immediately dispatches `StageHunks` with
    content == index bytes).
  - apply: toggling a region to HEAD assembles head lines for that region
    (assert the `StageHunks` op's Content).
  - `%s first` suffix render test; `" — empty"` suffix render test.
  - tiny-overlay: pane hidden (no rule line) when the minimum can't be met.
  - i18n gates stay green (new keys ×4).
