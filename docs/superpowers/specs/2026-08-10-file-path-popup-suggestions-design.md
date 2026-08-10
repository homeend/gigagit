# File-path popup fuzzy suggestions (palette File history / File blame)

**Date:** 2026-08-10
**Status:** Approved design
**Surface:** `internal/tui/file_path_popup.go` (reached from the command palette,
`ctrl+p` → "File history" / "File blame")

## Problem

The palette's File history / File blame popup takes a raw path and does no
validation: the user must type the exact repo-relative path. A typo, a bare
filename (`main.go`), or a partial path opens the surface anyway, which then
renders "(no history)" or a git error — a dead end that makes the palette
route practically unusable without already knowing the full path.

## Behavior

- Opening the popup also starts the async tracked-file list load (`LsFiles`,
  the same domain call the `F` finder uses). The result is cached in the
  popup for its lifetime.
- **Enter with an exact tracked path** (after `repoRelPath` normalization,
  membership-tested against the `LsFiles` set): opens the history/blame
  surface immediately — today's behavior, now validated. No change in feel.
- **Enter with anything else**: the popup switches into **suggestion mode**.
  The input stays on top; below it appears a ranked list:
  - Row 0 (always present): `open as typed: <input>` — the escape hatch.
    Deleted files have history but are not in `LsFiles`; this row keeps them
    (and any stubborn input) one enter away. A typo never dead-ends.
  - Rows 1..N: `fuzzy.Rank(input, allTracked, limit)` matches, capped like
    the finder (200), rendered window-then-build (~10–16 visible rows,
    maximize-aware).
- **In suggestion mode:**
  - `↑`/`↓`/`pgup`/`pgdn` move the cursor.
  - `enter` opens the selected row on the popup's kind (history or blame) —
    directly, no second action menu; the intent was already expressed in the
    palette.
  - Typing/backspace edits the input and re-ranks the list live (the escape
    row tracks the edited input).
  - `esc` drops back to plain-input mode (list disappears, input preserved);
    `esc` again closes the popup.
- **Loading race:** if `LsFiles` has not returned when a non-exact enter
  lands, the popup enters suggestion mode showing `(loading…)` and resolves
  the list when the result arrives. If `LsFiles` errored, fall through to
  today's behavior: open the surface with the typed path.

## Implementation shape

All contained in `internal/tui/file_path_popup.go` (+ tests):

- `filePathPopup` gains: `all []string`, `set map[string]struct{}` (built once
  when the load lands), `loading bool`, `loadErr error`, `suggesting bool`,
  `matches []fuzzy.Match`, `sel int`.
- `openFilePathPopup` starts the load alongside pushing the layer.
- **Message routing:** the finder's `lsFilesMsg` is currently consumed by the
  `fileFinderPopup` layer. This popup uses a distinct msg type
  (`filePathLsMsg`) delivered by its own load cmd — collision-free with a
  concurrently open finder, no shared-routing logic.
- Reused as-is: `fuzzy.Rank`, `windowBounds`/`renderWindow` (window-then-build
  list), `popupBox`/`popupResolveWidth` layout, `popupMax` ctrl+t maximize.
- Opening a suggestion runs the same code today's enter path runs
  (`newHistoryView`/`newBlameView` + matching load cmd), with the selected
  path; the palette-unwind logic (pop popup, pop palette if beneath) is kept.

## Not in scope

- No change to the `F` finder or its action menu.
- No live-autocomplete while in plain-input mode (search triggers on enter).
- No directory suggestions; the list is tracked files only.
- CLI/MCP/web untouched; no agent-surface change (no using-gg bump).

## i18n

New user-visible strings route through `i18n.T` with literal keys present in
all four bundles (ja/ko/zh/ru), enforced by the AST-gate tests:
- `open as typed: %s`
- suggestion-mode hint line (e.g. `[enter] open  [↑↓] nav  [esc] back`)
- reuse existing `(loading…)` key.

## Testing (TDD)

- Exact tracked path + enter → surface opens directly (both kinds).
- Non-matching input + enter → suggestion mode; escape row first; fuzzy
  matches ranked beneath.
- Typing in suggestion mode re-ranks live; escape row reflects edited input.
- Enter on a suggestion opens the right surface for both history and blame.
- Enter on the escape row opens the surface with the typed path (deleted-file
  case: path absent from LsFiles still opens history).
- Esc unwinds suggestion mode → plain input → closed.
- Enter before LsFiles returns → `(loading…)`, then list appears on delivery.
- LsFiles error + non-exact enter → falls through to open-as-typed.
- Existing `repoRelPath` normalization tests unchanged.

## Docs

- `CHANGELOG.md` entry.
- No README change (palette route already documented at feature level).
