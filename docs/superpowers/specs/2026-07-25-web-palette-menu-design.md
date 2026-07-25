# Web command palette + global ☰ menu — design

Date: 2026-07-25 · Branch: `feat/web-palette` (off `web-dev`) · Wave 3, track 1

## Goal

Give the web UI a keyboard-first **command palette** (Ctrl+K / Ctrl+P) and a
mouse-first **global ☰ menu** in the top bar, both built on the wave-2 layer
stack. The palette includes the **MRU repo picker** — the first client
consumer of `GET /api/repos`, feeding the existing `doReroot` POST+reload
pattern.

No server changes. This track is client-only (`app.js`, `index.html`,
`style.css`).

## Background (wave-2 foundations this builds on)

- Layer stack (`app.js:49-73`): `pushLayer(id, el, {onKey})` /
  `closeLayer(id)` / `topLayer()`. `pushLayer` dedups by id and DISCARDS a
  re-passed `onKey` — every close path must fully `closeLayer` so reopening
  re-registers the handler. `onKey` returning true = consumed (handler calls
  `preventDefault` itself); a non-empty stack always short-circuits global
  keys; bare Escape closes a layer with no `onKey`.
- Keydown router (`app.js:1198+`): layer routing → form-field guard →
  global key chain. Taken global keys: j/k/enter/esc/g/b/p/P/s/u/r/?.
- Overlay CSS templates: `#modal` (z-index 10) and `#help` (z-index 11)
  centered-backdrop pattern (backdrop click closes, box `stopPropagation`);
  `#ctx-menu` (z-index 20) floating pattern with generic
  `showCtxMenu(items, x, y)` where items = `[{label, act, danger?}]` and
  outside-click dismiss is already wired.
- `GET /api/repos` → `{repos: [{path, name}]}` (MRU registry; allowlist
  source for re-root). `doReroot(path)` (`app.js:285`) posts
  `/api/reroot` then `location.reload()`; errors land on the status strip.
- localStorage convention `gg.<feature>.<subkey>` via `lsGet`/`lsSet`.

## Design

### Global ☰ menu

- New `<button id="menu-btn" title="menu">☰</button>` as the FIRST child of
  `#top` (before `#repo-name`).
- Clicking it calls the existing `showCtxMenu(items, x, y)` anchored at the
  button's bounding rect (left edge, below). **No new overlay machinery** —
  the ctx layer already handles esc + outside-click dismiss and dedups by
  id (clicking ☰ twice: the document-level outside-click handler closes it
  first since the button is outside `#ctx-menu`; re-click reopens).
- Items (top to bottom):
  1. `pull` → `doPull()`
  2. `push` → `doPush()`
  3. `refresh` → `if (!state.op) refreshAfterOp()`
  4. `switch repo…` → `openPalette("repo")`
  5. `command palette…` → `openPalette("cmd")`
  6. `toggle sidebar` → `toggleSidebar()`
  7. `toggle graph` → `toggleGraphMode()`
  8. `help` → `openHelp()`
- No dedicated keyboard key for the menu (the palette covers keyboard users).

### Command palette

Markup (in `index.html`, after `#modal`, before `#help`):

```html
<div id="palette" class="hidden">
  <div id="palette-box">
    <input id="palette-input" type="text" autocomplete="off" spellcheck="false">
    <ul id="palette-list"></ul>
  </div>
</div>
```

CSS: the `#help` centered-backdrop template, but top-aligned (box at ~18vh
from the top, the familiar palette position), `z-index: 21` (above modal,
help, ctx-menu), box `max-width: 560px; width: 90vw`, list rows with
`.sel` highlight using `var(--sel)`, dim right-aligned detail span
(`var(--dim)`), and `#palette.hidden { display: none; }`.

**Modes.** `openPalette(mode)` with `mode ∈ {"cmd", "repo"}`; module state
`let pal = null;` holding `{mode, items, filtered, sel, fromCmd}`.

- `"cmd"` mode lists the command registry (below), filtered by
  case-insensitive substring on label.
- `"repo"` mode fetches `GET /api/repos` on entry (loading row while
  in-flight; fetch error → close + `opLine(error, true)`), lists
  `name` with the dim `path` as detail, filtered on both name and path.
  Best-effort: a row whose path equals the currently served worktree
  (`state.repo && state.repo.worktree`, from the boot `/api/repo` payload —
  if app.js does not currently retain it, retain it in `loadRepo`) is
  filtered out; if retention is awkward the row may stay (re-rooting to
  self is harmless).
- Enter on a repo row → `closePalette()` then `doReroot(path)`.
- `"repo"` entered from cmd mode (`fromCmd: true`): Escape goes BACK to cmd
  mode; entered directly (☰ "switch repo…"): Escape closes.

**Command registry** (`paletteCommands()`, a function so it can consult live
state; each `{label, hint?, run}`):

| label | hint | run |
|---|---|---|
| `pull` | `p` | `doPull()` |
| `push` | `P` | `doPush()` |
| `refresh` | `r` | `if (!state.op) refreshAfterOp()` |
| `switch repo…` | | switch to repo mode in place (`fromCmd: true`) |
| `open working tree` | | `openWorkingTree(0)` (the WT row index — a bare call corrupts state.cursor) |
| `toggle sidebar` | `b` | `toggleSidebar()` |
| `toggle graph` | `g` | `toggleGraphMode()` |
| `help` | `?` | `openHelp()` |

Every `run` except "switch repo…" first `closePalette()`.

**Opening.** `openPalette(mode)`:
1. build `pal`, render list (sel = 0), clear `#palette-input`,
2. `pushLayer("palette", $("palette"), { onKey: paletteKey })`,
3. `$("palette-input").focus()`.

**Keyboard** (`paletteKey(e)`):
- `ArrowDown`/`ArrowUp` → move `sel` (clamped), re-render, `preventDefault`,
  return true.
- `Enter` → run the selected row (no-op when the filtered list is empty),
  `preventDefault`, return true.
- `Escape` → repo-mode-with-`fromCmd` → back to cmd mode; else
  `closePalette()`; `preventDefault`, return true.
- `Tab` → `preventDefault`, return true (swallowed; focus stays in the input).
- Anything else → return false. The event then dead-ends in the router (a
  non-empty stack short-circuits globals) and the browser delivers the
  keystroke to the focused input natively; the input's `input` event
  re-filters (sel reset to 0).

**Closing.** ALL close paths route through
`closePalette() { closeLayer("palette"); $("palette-input").blur(); pal = null; }`.
The `blur()` is load-bearing: without it the input keeps focus after the
layer closes and the form-field guard swallows every global key afterwards.
Backdrop click closes (help pattern: `#palette` click → `closePalette()`,
`#palette-box` click → `stopPropagation`). Row click runs that row.

**Global shortcut.** In the keydown router, AFTER the layer-routing block and
BEFORE the form-field guard, insert:

```js
if ((e.ctrlKey || e.metaKey) && (e.key === "k" || e.key === "p")) {
  e.preventDefault(); // Ctrl+P would open the browser print dialog
  openPalette("cmd");
  return;
}
```

Placement rationale: after layer routing so an open modal/help/palette keeps
the keyboard (no palette over a parked decision modal); before the form
guard so Ctrl+K works while the commit box is focused (the input blurs when
the palette's own input takes focus).

**Footer.** Add `<button data-act="palette">ctrl+k palette</button>` after the
help chip, and a `case "palette": openPalette("cmd"); break;` arm in the
footer dispatch switch.

**Help overlay.** Add two Keys rows: `ctrl+k / ctrl+p` → "command palette",
`☰` under Mouse → "global menu (top-left)".

## Declared edit regions (for cross-track merge safety)

This track edits ONLY:
- `app.js`: (a) one NEW contiguous region (all palette/menu functions)
  appended at the END of the file, after the existing boot block; (b) ONE
  inserted ctrl-combo block in the keydown router (between layer routing and
  form guard); (c) ONE new `case "palette"` arm in the footer switch;
  (d) `loadRepo` retains the `/api/repo` payload on `state.repo` if not
  already retained.
- `index.html`: `#menu-btn` in `#top`; `#palette` markup; one footer button;
  two help rows.
- `style.css`: appended `#palette*` rules only.

It does NOT touch: `renderDiff`/`openFile`/`openStatusDiff`/`updateDiffNav`/
`drillOut`, the `#files-list` contextmenu handler, `#diff-nav` markup, or any
Go file. (Those belong to the wave-3 hunks-UI and discard tracks.)

## Error handling

- `/api/repos` fetch failure → `closePalette()` + `opLine("error: …", true)`.
- `doReroot` failures already land on the status strip; palette is closed
  before the call.
- Ops started from the palette/menu go through the existing `startOp`
  locking (`state.op` guard) — a second op attempt is a silent no-op exactly
  as the footer chips behave today.

## Testing

- `node --check internal/web/static/app.js` gate.
- Playwright scenario (scratch repo under /tmp): open palette via Ctrl+K,
  filter, run "toggle sidebar" (assert sidebar hidden); reopen, "switch
  repo…" → assert repo rows listed from a seeded MRU (serve repo A after
  touching repo B), Enter re-roots + reloads into B (assert
  `#repo-worktree`); esc-in-repo-mode returns to cmd mode; ☰ click shows the
  menu via `#ctx-menu`; palette closes on backdrop click; after close,
  `j`/`k` still navigate (the blur regression); Ctrl+K while commit box
  focused opens the palette.
- Layer regressions: help still opens over nothing, modal still wins keys
  (no new code path touches them — spot-check esc chains in the sweep).

## Out of scope

- Branch switching / arbitrary command args in the palette (registry is
  static v1; extending it is one array entry).
- Palette over a parked decision modal (deliberately unreachable, same as
  help-over-modal reachability in wave 2).
- Recent-command ordering / `gg.palette.*` persistence (add later if wanted).
