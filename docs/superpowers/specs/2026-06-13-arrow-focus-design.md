# Left/right arrow window focus — design

Date: 2026-06-13
Status: approved

## What

`←`/`→` switch window focus horizontally, in both layouts:

1. **Normal mode** (three left panels + Commits): `→` from a left panel
   focuses Commits; `←` on Commits returns to the **last-focused left panel**
   (Branches before any was focused). At the edges (`→` on Commits, `←` on a
   left panel) focus stays put.
2. **Files view** (after `l`): `←` focuses the file tree, `→` focuses
   Commits, and `tab`/`shift+tab` toggle between the two. Vertical movement
   (`↑/↓`, `j/k`, `pgup/pgdn`) follows the focused side; **`ctrl+↑/↓` always
   scrolls the tree** regardless of focus.

TUI-only; no engine/CLI/agent-skill changes.

## Approach (decided)

Two small Model fields; `m.focus` semantics unchanged:

- `lastLeftPanel panel` — the left panel `←` returns to. The zero value is
  `panelBranches` (enum value 0), so the default needs no initialization.
  It is recorded whenever focus MOVES OFF a left panel (`→`, `tab`,
  `shift+tab`), so tab-to-Status → tab-to-Commits → `←` lands on Status.
- `filesTreeFocused bool` — which side of the files view owns vertical
  movement. `m.focus` stays `panelCommits` for the whole life of the view
  (tooltip, selection and follow-live machinery keep keying off it).

Rejected: making the tree a `panel` enum member (breaks `panelCount`
cycling, `panelView`, sort/filter plumbing) and putting focus state inside
`contentPopup` (shared with the help window — wrong owner).

## Normal mode

In the normal-key switch in `internal/tui/model.go` (no current bindings for
"left"/"right" — no conflicts):

- `case "right"`: if `m.focus != panelCommits`, record
  `m.lastLeftPanel = m.focus`, then `m.focus = panelCommits`. Else no-op.
- `case "left"`: if `m.focus == panelCommits` AND the layout has a left
  column (`m.width <= 0 || m.width >= 40`), `m.focus = m.lastLeftPanel`.
  Else no-op.
- `tab` / `shift+tab`: before reassigning focus, record
  `m.lastLeftPanel = m.focus` when the OLD focus is a left panel. Cycling
  behavior itself is unchanged.

Hidden-panel caveat: on short terminals the Worktrees panel is not drawn but
`tab` already cycles through it; `←` returning to a hidden `lastLeftPanel`
is the same accepted quirk — no special handling.

Ungated by running/loading (pure focus movement, like `tab` today).

## Files view

State: opening (`l`) leaves `filesTreeFocused = false` (Commits focused,
as today). Both close paths (`esc`, `l`) and the narrow-resize auto-close
reset it to false. `reRoot` clears the view entirely (already).

Key handling in `updateFilesViewKey` (`internal/tui/files_view.go`):

| Key | Tree focused | Commits focused (today's behavior) |
|---|---|---|
| `←` | no-op | focus tree (`filesTreeFocused = true`) |
| `→` | focus Commits | no-op |
| `tab` / `shift+tab` | toggle to Commits | toggle to tree (no longer swallowed) |
| `↑/k` `↓/j` | `p.move(∓1)` on the tree | move the commit selection (follow-live reload) |
| `pgup/pgdn` | `p.move(∓filesPageRows())` | page the commit selection by `pageStep()` via the follow-live path (ONE reload for where it lands) |
| `ctrl+↑/↓` | `p.move(∓1)` | `p.move(∓1)` — always the tree |
| everything else | unchanged and focus-independent: `/` tree search, typing mode, `esc` clear-search-then-close, `l` close, `q`/ctrl+c quit, others swallowed | same |

`moveCommitUnderFilesView` already clamps and dedupes by hash; paging
reuses it with `±m.pageStep()` so a page move fires at most one reload.

## Rendering

Focus must be visible. New helper in `internal/tui/view.go`:

```go
// panelFocused reports whether p should render as the focused panel. While
// the files view's tree side is focused, the Commits panel renders blurred
// even though m.focus still points at it.
func (m Model) panelFocused(p panel) bool {
	return p == m.focus && !(m.filesView != nil && m.filesTreeFocused)
}
```

- `renderPanel` uses `m.panelFocused(p)` for BOTH the border style and the
  `> ` reverse-video row highlight (replacing its two `p == m.focus` checks).
- `renderFilesView` picks `focusedPanel` vs `bluredPanel` from
  `m.filesTreeFocused`. The tree's cursor row stays reverse-video in both
  states (it doubles as the scroll position).
- The truncation tooltip keys off `m.focus` and would describe a blurred
  panel's row while the tree is focused: gate it on `m.panelFocused(m.focus)`.

## Footer & help

- Files-view footer line (`footerLine` override in `internal/tui/footer.go`)
  becomes: `files: [←/→] focus  [↑/↓] move  [ctrl+↑/↓] tree  [/] search  [esc/l] close`.
- No new registry bindings — arrows are navigation like `tab`; the
  registry's `[tab] focus` entry stays as-is.
- Help window (`internal/tui/help.go`): Global section gains
  `r("←/→", "focus the left column / the Commits panel")` next to the tab
  row; the "Commit files view (l)" section is rewritten for the focus model
  (←/→/tab focus row; ↑/↓ move on the focused side; ctrl+↑/↓ always the
  tree; search/close rows unchanged).
- README key table: the `l` row's parenthetical gains the focus keys; a new
  `←`/`→` row next to `tab`.

## Testing

`internal/tui` only, established patterns (`keyMsg` supports "left"/"right"
or extend it if missing; `filesModel`/`openFilesView` helpers):

- Normal mode: `→` from each left panel lands on Commits and `←` returns to
  THAT panel; `←` after tab-to-Status → tab-to-Commits lands on Status;
  `→` on Commits and `←` on a left panel are no-ops; `←` on Commits at
  width 30 is a no-op; up/down still move the selection, not focus.
- Files view: `←` sets `filesTreeFocused`, `→` clears it, `tab` toggles;
  ↑/↓ move the tree cursor when tree-focused and the commit selection
  (with reload cmd) when commits-focused; pgup pages commits via ONE
  follow-live reload when commits-focused and pages the tree when
  tree-focused; ctrl+↓ moves the tree cursor in BOTH focus states; close
  (`esc`/`l`) resets the flag and reopening starts commits-focused; the
  narrow-resize auto-close resets it too.
- Rendering: while tree-focused, the Commits panel has no `> ` reverse
  highlight and the tree box uses the focused border; commits-focused is
  the inverse; tooltip suppressed while tree-focused.

## Not doing (YAGNI)

`←`/`→` movement among the three stacked left panels (they are vertical;
tab covers them); focus-following `/` (it always searches the tree);
mouse click-to-focus; persisting `filesTreeFocused` across view reopens.
