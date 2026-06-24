# Command palette (`ctrl+p`) + Show-commit-by-SHA (`#`) — Design

## Goal

Let the user open **any** commit's files in the files-view by typing its SHA,
and introduce a small **generic command palette** as the home for such global
actions. For now the palette holds a single command, "Show commit", which is
also reachable directly via `#`.

## User decisions

- **Palette key = `ctrl+p`.** `ctrl+shift+p` is indistinguishable from `ctrl+p`
  in most terminals (Bubble Tea sees plain `ctrl+p`), so binding the literal
  `ctrl+shift+p` would silently never fire. `ctrl+p` is reliable everywhere.
- **SHA validation = validate then inline-error.** On enter, resolve the typed
  text via `git rev-parse`. If it doesn't resolve, keep the popup open and show
  an inline error; only open the files-view on a valid commit.

## Surfaces (both are layer-stack popups)

### A — Show commit by SHA (`#`)

New file `internal/tui/goto_commit_popup.go`.

```go
type gotoCommitPopup struct {
    input     textfield // the SHA / ref to resolve (no spaces)
    err       string    // inline error from the last failed resolve; "" = none
    resolving bool       // a resolve cmd is in flight
}
```

- `#` (base key switch, global, `opsIdle`) pushes an empty `gotoCommitPopup`.
- **enter** → if input is empty, no-op. Else set `resolving`, clear `err`, and
  fire `resolveCommitCmd(input)` (popup stays open). **esc** → `popLayer`.
  Typing edits the field; **space is dropped** (a commit-ish has no spaces).
- `resolveCommitCmd(rev)` calls `svc.RevParse(rev)` → `gotoCommitResolvedMsg{rev, hash, err}`.
- `model.go` handles `gotoCommitResolvedMsg` with a **tag-gate**: act only when
  the top layer is the `*gotoCommitPopup` *and* its current input still equals
  `rev` (so a stale resolve from a since-edited field can't land):
  - err → `p.err = "no such commit: <rev>"`, `p.resolving = false`, stay open.
  - ok → `popLayer`, then the canonical by-hash open used by `openReflogFiles`:
    `openChangedFiles(model.Commit{Hash: hash})`, then `m.focus = panelCommits`
    and `focusTree()` (the commit may not be in the feed, so open on the tree).
- render: title `Show commit`, the SHA field, an inline error line when set,
  hint `[enter] show  [esc] cancel`.

### B — Generic command palette (`ctrl+p`)

New file `internal/tui/command_palette.go`.

```go
type paletteCommand struct {
    label   string
    keyHint string
    run     func(Model) (Model, tea.Cmd)
}
type commandPalette struct {
    cmds []paletteCommand
    sel  int
}
```

- `ctrl+p` (base key switch, global, `opsIdle`) pushes a `commandPalette` whose
  only command is `{"Show commit", "#", openGotoCommitPopup}`.
- **up/down/j/k** move `sel`; **enter** runs the selected command's `run`
  (which pops the palette and pushes the goto popup); **esc** → `popLayer`.
- render: title `Commands`, rows `label … keyHint` (selected row reversed via
  `selectedRow`), hint `[enter] run  [esc] close`. Styled with `popupBox` like
  the action menu. No `/` filter yet (trivial with one row; easy to add later).

`openGotoCommitPopup(m) (Model, tea.Cmd)` is the shared seam, called by both the
`#` key and the palette command, so the two paths never diverge.

## Wiring

- `model.go` base key switch: `case "#"` → `openGotoCommitPopup`; `case "ctrl+p"`
  → push `commandPalette`. Both gated on `opsIdle` (mirrors `R`/`,`).
- `model.go` msg switch: `case gotoCommitResolvedMsg` (tag-gated, as above).
- `footer.go` global tail: one binding advertising `[ctrl+p] commands`.
- `help.go`: `#` and `ctrl+p` lines.
- `CHANGELOG.md`: Added entry.

## Tests

- `goto_commit_popup_test.go`: `#` opens the popup from a non-Commits panel;
  enter routes through `Update` → resolve cmd → **bad SHA** sets inline error &
  keeps the popup open; **good SHA** opens the files-view by hash on the tree;
  **stale-resolve** (input edited after firing) is rejected by the tag-gate.
- `command_palette_test.go`: `ctrl+p` opens the palette; enter on "Show commit"
  closes the palette and opens the goto popup; esc closes the palette.

## Out of scope

- Triggering `#` from *inside* an open files-view (it is a base-panel key).
- Palette `/` filter, multiple commands, fuzzy match — deferred until a 2nd
  command exists.
