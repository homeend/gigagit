---
name: adding-tui-windows
description: Use when adding any TUI surface to gigagit — a new panel, popup, overlay, input prompt, or modal — or when changing panel layout, focus cycling, or key routing in internal/tui.
---

# Adding a TUI Window to gigagit

## Overview

Pick the cheapest window type that fits, in this order:

1. **Option-list question → NO new UI.** Emit a `DecisionRequest` from the
   engine op; the existing modal (`m.modal`) renders it for free in every
   frontend. Build nothing.
2. **Free-text / multi-field input → popup** (exemplar:
   `internal/tui/worktree_popup.go`).
3. **Persistent list view → panel** (exemplar: the Worktrees panel).
4. **Transient read-only overlay → tooltip** (exemplar: `internal/tui/tooltip.go`).
5. **Read-only scrollable/searchable text → content popup** (exemplar:
   `internal/tui/content_popup.go`). Don't build a new surface: construct
   `newContentPopup(title, []contentLine{...})` and `pushLayer` it (the help
   window — `model.go`'s `?` handler — and the bookmark/shelf cheat sheets are
   the exemplar consumers). Search (`/`-gated, panel-consistent), scrolling
   (keys + wheel), windowing, and rendering are free; `heading: true` lines
   group sections and are skipped by the filter. New **interactive** surfaces
   must NOT reuse this — build a popup.

## The layer stack — the one thing to understand

Every popup and every full-screen surface (history, blame, the rebase /
conflict / stage editors, the switchers and their child popups) lives on **one
push-ordered stack of `layer`s** (`internal/tui/layer_stack.go`). A `layer` is:

```go
type layer interface {
    update(m Model, msg tea.KeyMsg) (Model, tea.Cmd)
    render(m Model, below string) string
}
```

- **The top layer owns the keyboard.** Dispatch, render, and mouse each route to
  `m.topLayer()` — **one** check, not three per-window edits. Push with
  `m.pushLayer(l)`, close with `m.popLayer()` (reveals the layer beneath, whose
  state was never torn down — that's how esc on a cheat sheet returns to the
  switcher under it), wipe all with `m.clearLayers()`.
- **A popup composites** its centered box over `below`:
  `overlayCenter(clipToHeight(below, h), box, w, h)`.
- **A full-screen surface ignores `below`** (signature `render(m, _ string)`) —
  it owns the screen.
- Reach a live layer by type without a Model field via `layerOf[*xView](m)`
  (used for async results: `historyListMsg`/`blameMsg` find their view even with
  a popup pushed on top).

**The critical invariant:** `Model` is a **value receiver** — Bubble Tea copies
it on every `Update`. Stack mutations survive the copy because `layers
*layerStack` is a **pointer field**; you do NOT add a per-popup Model field.
(State you need *after* an op finishes — switch-after, seq bumps — still goes in
a Model field consumed by `opFinishedMsg`; see `pendingSwitch`/`pendingSeqBump`.)

**Four special single-slot fields stay OFF the stack** — don't add a fifth:
`modal` (decision modal), `proc` (a process that locks the whole interface, e.g.
conflict resolution — see the conflict-process work), `actionMenu` (the `.`
key-replay menu), and `diffView` (the full-screen diff, which is the *base* the
stack composites over — `layerBase()`). Dispatch/render precedence is
`modal → proc → actionMenu → topLayer → diffView → panels`.

## Popup checklist

| # | What |
|---|------|
| 1 | New file `internal/tui/<name>_popup.go`: a state struct that implements the `layer` interface (`update` + `render`). No Model field — the stack holds it. |
| 2 | **Open** with `m = m.pushLayer(&xPopup{…})` from wherever the key is handled. The single `topLayer().update` dispatch routes keys to it automatically — no `Update` edit. The handler **swallows every key** (no fallthrough): `esc` → `m = m.popLayer()`; `ctrl+c` → `tea.Quit`. |
| 3 | `render(m, below string)`: composite the centered box — `overlayCenter(clipToHeight(below, h), box, w, h)`. Style a single-line / fixed input or confirm box with `modalStyle.Width(popupInnerWidth(w))` so long content wraps. (Scrollable lists use `renderWindow` — see below.) |
| 4 | Confirm → `m = m.popLayer()` then `return m.startOp(op)`. A popup that stays visible during its own async op (paste/restore) must guard `update` with `if m.running { return m, nil }` so a keypress can't launch a second op. |
| 5 | If the popup previews template/random/time values, freeze `seed`/`now` at open so recomputes are deterministic (see `tctx()` in `worktree_popup.go`). |
| 6 | Hand off to the full-screen diff? Call `m.openPickerDiff(v, tag, load)` — it `clearLayers()`es (the picker *and* any surface beneath) so the diff owns the screen. |

### List popups (scrollable rows) — use `renderWindow`, not `modalStyle.Width`

A popup that shows a **scrollable list of rows** (repo / bookmark / shelf
switcher, action menu) MUST render through the shared `renderWindow` primitive
so it inherits cutoff/wrap/scroll for free. Exemplar: `internal/tui/repo_popup.go`.

- Give the state struct `mode dispMode` and `hscroll int`.
- Build one `winRow{text, style}` per visible row; **fold any cursor/mark prefix
  into `text`** (the primitive adds none) and set `style = selectedRow` on the
  selected row.
- Cap the visible height and call `renderWindow(rows, winOpts{w: textW, h: h,
  mode: p.mode, anchor: p.sel, hscroll: p.hscroll})`; emit via `popupBox(inner,
  …)`, not `modalStyle.Width(...)`.
- In the key handler, before the navigation switch, handle `z`
  (`p.mode = p.mode.next()`, reset `hscroll`) and `shift+←/→` (pan by
  `m.hscrollStep()`, only in `modeScroll`). Gate these to navigation mode if the
  popup has a `/` filter sub-mode, so `z` stays a query character while typing.
- Add `[z] mode` to the popup's footer hint line.

## Full-screen surface

History, blame, and the rebase/conflict/stage editors are the templates for a
layer that owns the WHOLE screen and every key. Build one as a `layer` and:

1. `render(m, _ string)` — ignore `below`; draw your own header and hint line
   (the registry footer is not rendered). Exemplar: `historyView.render`.
2. Open via `m.pushLayer(&xView{…})`; close (`esc`) via `m = m.popLayer()`,
   which returns to whatever was beneath — leave that state untouched.
3. **Register the type in `isFullScreenLayer`** (`layer_stack.go`). This is what
   lets a popup pushed *above* the surface composite over it as a backdrop; a
   surface you forget here vanishes when any popup opens over it. Keep the
   type-switch in sync — it's the one edit a new surface needs outside its file.
4. Guard narrow terminals (close + `statusMsg`), and tag async loads with an
   identity (commit hash / path) so stale results from fast movement are dropped
   — reach the live view with `layerOf[*xView](m)`, not `topLayer()`, so the
   result still lands when a popup sits on top.

## Panel checklist

| # | Where | What |
|---|-------|------|
| 1 | `model.go` panel enum | Add `panelX` const (iota block, before `panelCount`). Enum order = tab order; inserting mid-block is safe — everything uses the constants. |
| 2 | `model.go` Model | Data field (e.g. `stashes []string`). |
| 3 | `model.go` `panelLen` | Case returning the row count — drives selection clamping and ↑/↓ bounds automatically. |
| 4 | `load.go` | Field on `dataLoadedMsg` + fetch in `loadCmd` (non-fatal if optional) + assignment in the `dataLoadedMsg` case in `model.go`. |
| 5 | `view.go` | A `xRows() []string` builder + a `renderPanel(panelX, "Title", rows, w, h)` call in the layout. Bordered panels need ≥3 rows each; the layout branches on `bodyH` thresholds (see the `bodyH >= 9` three-panel branch) — add a taller breakpoint rather than squeezing. |
| 6 | Keys | tab-cycling, ↑/↓, and the post-load selection clamp are automatic via `panelCount`/`panelLen`. Panel-specific actions: guard with `m.focus == panelX`. Footer hint: add a `footerBinding` to `contextBindings`/`globalBindings` in `footer.go`, gated by a shared predicate from `avail.go` (the same predicate must gate the `Update` arm). New global keys must also get a row in `helpContent()` (`help.go`) — `TestHelpFooterCoverage` fails otherwise. |

## In-panel view (column replacement)

The commit files view (`l`) REPLACES a panel column instead of overlaying — it
is **not** on the layer stack; it's the `m.filesView *contentPopup` slot. Reuse
the `contentPopup` struct (lines, query, typing, sel, `visible()`, `move()`) for
state + filtering, write a `render<X>View(boxW, boxH)` that pads every line to
`boxW-4` and fills to `boxH-2` (exact box size, `bluredPanel` border — focus
stays on the surviving panel), branch in `renderInterface` where the column is
built, and add a routing branch in `Update` BEFORE `filterTyping` that splits
keys between the surviving panel and the view. Tag async per-row loads (commit
hash) for stale-drop. Clear the view in `reRoot`.

## Pair-op popup (two-row operations)

For operations taking TWO rows of one panel: the `m` key marks a row by stable
identity (`panelList.Key`, not index — survives reload/sort/filter; state in
`Model.mark *markState`), a second `m` on another row pushes `pairOpPopup`
listing the panel's registered `pairOp`s (`pairOpsFor(panel)` in `mark.go`).
Labels spell out the argument direction ("Merge A into B"). To give a panel
pair-operations, register entries in `pairOpsFor` — the mark mechanism, popup,
and dispatch are already generic.

## Tests

Helpers: `loadedModel(t)`, `newRepoDir(t)`, `keyMsg("x")`, `runGit`, `driveOp`
(drains an op so goroutines don't leak). Cover:
- open/typing/backspace/esc/enter for popups, plus a **key-swallow test** (press
  a global key like `p` while open → nothing happens) and an **esc-returns test**
  for a popup pushed over another layer (esc → `popLayer` reveals the layer
  beneath);
- panel content in `View()` output, and a **fit test** at small sizes (every line
  ≤ `m.height` lines and `lipgloss.Width(line) ≤ m.width`);
- selection clamp if your change can shrink a list;
- for a full-screen surface, that a popup pushed over it still renders (the
  surface acts as backdrop — see `layer_compositing_test.go`).

## Common mistakes

| Mistake | Fix |
|---------|-----|
| Adding a per-popup `*xPopup` field to Model | Implement `layer` and `pushLayer` it; the stack is the one pointer field. |
| Editing `Update`/`render`/`mouse` to route a new popup | Don't — the single `topLayer()` check already routes it. Just push it. |
| Full-screen surface vanishes when a popup opens over it | Register its type in `isFullScreenLayer` (`layer_stack.go`). |
| Async result dropped when a popup is on top | Use `layerOf[*xView](m)`, not `topLayer()`. |
| Building a popup for a yes/no or pick-one question | Use an engine `DecisionRequest`; the modal is free. |
| Popup keys leaking to global handlers | Swallow everything; test it. |
| New panel squeezed into an existing `bodyH` branch | Add a taller breakpoint; 3 rows minimum per bordered panel. |
| String `truncate` by `len()` | Width must be display-aware (`lipgloss.Width`) — wide runes/ANSI. |
| Forgetting the footer hint | The binding registry in `footer.go` (predicates in `avail.go`). New global keys must also get a row in `helpContent()` (`help.go`) — `TestHelpFooterCoverage` fails otherwise. |
