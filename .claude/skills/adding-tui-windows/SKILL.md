---
name: adding-tui-windows
description: Use when adding any TUI surface to gigagit — a new panel, popup, overlay, input prompt, or modal — or when changing panel layout, focus cycling, or key routing in internal/tui.
---

# Adding a TUI Window to gigagit

## Overview

Three window types — pick the cheapest that fits, in this order:

1. **Option-list question → NO new UI.** Emit a `DecisionRequest` from the
   engine op; the existing modal (`m.modal`) renders it for free in every
   frontend. Build nothing.
2. **Free-text / multi-field input → popup overlay** (exemplar:
   `internal/tui/worktree_popup.go`).
3. **Persistent list view → panel** (exemplar: the Worktrees panel).
4. **Transient read-only overlay → tooltip** (exemplar: `internal/tui/tooltip.go`).
5. **Read-only scrollable/searchable text → content popup** (exemplar:
   `internal/tui/content_popup.go`). Don't build a new surface: construct
   `newContentPopup(title, []contentLine{...})` and assign it to
   `m.contentPopup`. Filtering, scrolling (keys + wheel), windowing, and
   rendering are free. `heading: true` lines group sections and are skipped
   by the filter. The help window (`help.go`) is the exemplar consumer.
   Positioned via `layoutGeom.pos` + `overlayAt`; receives no key events, owns
   no state, and is auto-shown from `render()` only when the plain (no modal,
   no popup) state is active. New **interactive** surfaces must NOT use this
   kind — use a popup instead.

**The critical invariant:** `Model` is a **value receiver** — Bubble Tea copies
it on every `Update`. Window state that must survive the copy (popup contents,
modal selection) MUST live behind a **pointer field** (`popup *worktreePopup`,
`modal *decisionState`). A value field's mutations silently vanish.

## Panel checklist

| # | Where | What |
|---|-------|------|
| 1 | `model.go` panel enum | Add `panelX` const (iota block, before `panelCount`). Enum order = tab order; inserting mid-block is safe — everything uses the constants. |
| 2 | `model.go` Model | Data field (e.g. `stashes []string`). |
| 3 | `model.go` `panelLen` | Case returning the row count — drives selection clamping and ↑/↓ bounds automatically. |
| 4 | `load.go` | Field on `dataLoadedMsg` + fetch in `loadCmd` (non-fatal if optional) + assignment in the `dataLoadedMsg` case in `model.go`. |
| 5 | `view.go` | A `xRows() []string` builder + a `renderPanel(panelX, "Title", rows, w, h)` call in the layout. Bordered panels need ≥3 rows each; the layout branches on `bodyH` thresholds (see the `bodyH >= 9` three-panel branch) — add a taller breakpoint rather than squeezing. |
| 6 | Keys | tab-cycling, ↑/↓, and the post-load selection clamp are automatic via `panelCount`/`panelLen`. Panel-specific actions: guard with `m.focus == panelX`. Footer hint: the `footerText` const in `view.go`. New global keys must also get a row in `helpContent()` (`help.go`) — `TestHelpFooterCoverage` fails otherwise. |

## Popup checklist

| # | What |
|---|------|
| 1 | New file `internal/tui/<name>_popup.go`: a state struct + a **pointer field on Model** (see invariant above). |
| 2 | Key routing precedence in `Update`'s `tea.KeyMsg` case: `modal` first, then popups, then normal keys. Add `if m.xPopup != nil { return m.updateXPopupKey(msg) }` before the normal-key switch. The handler **swallows every key** (no fallthrough): `esc` always cancels (`m.xPopup = nil`), `ctrl+c` always `tea.Quit`. |
| 3 | Render as a centered overlay in `view.go`'s `render()`: `overlayCenter(bg, m.renderXPopup(), w, h)` — ANSI-aware compositing, don't hand-roll. Style with `modalStyle.Width(inner)` so long content wraps instead of overflowing. |
| 4 | Confirm → `m.xPopup = nil` then `return m.startOp(op)`. State needed after the op finishes (e.g. switch-after, seq bumps) goes in Model fields consumed by `opFinishedMsg` (see `pendingSwitch`/`pendingSeqBump`). |
| 5 | If the popup shows previews of template/random/time values: freeze `seed`/`now` at open so recomputes are deterministic (see `tctx()` in `worktree_popup.go`). |

## Tests

Helpers: `loadedModel(t)`, `newRepoDir(t)`, `keyMsg("x")`, `runGit`, `driveOp`
(drains an op so goroutines don't leak). Cover:
- open/typing/backspace/esc/enter for popups, plus a **key-swallow test**
  (press a global key like `p` while open → nothing happens);
- panel content in `View()` output, and a **fit test** at small sizes
  (every line ≤ `m.height` lines and `lipgloss.Width(line) ≤ m.width`);
- selection clamp if your change can shrink a list.

### Pair-op popup (two-row operations)

For operations taking TWO rows of one panel: the `m` key marks a row by
stable identity (`panelList.Key`, not index — survives reload/sort/filter;
state in `Model.mark *markState`), a second `m` on another row opens
`pairOpPopup` listing the panel's registered `pairOp`s (`pairOpsFor(panel)`
in `mark.go`). Labels spell out the argument direction ("Merge A into B").
To give a panel pair-operations, register entries in `pairOpsFor` — the
mark mechanism, popup, and dispatch are already generic.

## Common mistakes

| Mistake | Fix |
|---------|-----|
| Popup state as a value field on Model | Pointer field — value mutations vanish across the Update copy. |
| Building a popup for a yes/no or pick-one question | Use an engine `DecisionRequest`; the modal is free. |
| Popup keys leaking to global handlers | Swallow everything; test it. |
| New panel squeezed into an existing `bodyH` branch | Add a taller breakpoint; 3 rows minimum per bordered panel. |
| String `truncate` by `len()` | Width must be display-aware (`lipgloss.Width`) — wide runes/ANSI. |
| Forgetting the footer hint | The `footerText` const in `view.go`. New global keys must also get a row in `helpContent()` (`help.go`) — `TestHelpFooterCoverage` fails otherwise. |
