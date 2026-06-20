# "Edit in editor" Files-panel action — Design

**Date:** 2026-06-21
**Status:** Approved, ready for planning
**Scope:** TUI-only (Files panel, menu-only)

## Goal

A `.`-action-menu entry on the selected **Files-panel** file that suspends the
TUI, opens the file in the user's editor, and refreshes the working-tree status
when the editor exits.

## Decisions (from brainstorming)

- **Files panel only** (not Staged, not the commit files-view).
- **Menu-only** — no new keybind.
- **Editor fallback**: `$VISUAL` → `$EDITOR` → `vi` (unix) / `notepad`
  (Windows), git-style.

## Why pure TUI

Launching an editor is a terminal-control concern (suspend the alt-screen, run a
foreground process, restore). It is not a git operation, so there is **no engine
op, no git verb, no domain method, no CLI, no e2e, and no agentskill bump**.
`internal/tui` may import `os` / `os/exec` (the archtest only forbids
`internal/git`). The post-edit refresh reuses the existing status-read path.

## Components

### Editor resolution — `resolveEditor() string`

```
TrimSpace(VISUAL) ?: TrimSpace(EDITOR) ?: defaultEditor()
```

The env values are **trimmed** before the empty check, so a whitespace-only
`$EDITOR`/`$VISUAL` (e.g. `"   "`) is treated as unset and falls through —
otherwise `strings.Fields` would yield an empty slice and `editorCommand` would
panic (`fields[0]`/`fields[1:]` out of range), a hard crash from an env var.

`defaultEditor()` returns `"notepad"` when `runtime.GOOS == "windows"`, else
`"vi"`. Always returns a non-empty string. `editorCommand` additionally guards
the empty-slice case (`len(fields) == 0 → defaultEditor()`) as belt-and-braces.

### Command building — `editorCommand(editor, absPath string) *exec.Cmd`

`strings.Fields(editor)` splits the editor string: the first field is the
binary, the remaining fields are leading arguments, and `absPath` is appended as
the final argument. `cmd.Dir` is set to the worktree root by the caller (via
`editFileCmd`). This handles common forms: `vim`, `nano`, `code -w`,
`emacs -nw`, `subl -w`.

**v1 limitation (documented):** no shell-quote parsing. An editor command whose
*binary path* contains spaces, or that relies on shell quoting, is not
supported. `strings.Fields` is sufficient for the overwhelmingly common
`binary [flags]` forms.

### Trigger — `fileEditRow() (actionRow, bool)`

Gated `m.focus == panelFiles && m.opsIdle()` with a live selection resolved via
`m.backingIndex(panelFiles)` → `m.status.Files[bi].Path`. No file-kind
restriction — every Files-panel row is a real file on disk (modified, untracked,
or conflicted; editing a conflicted file to resolve markers by hand is a valid
use). Wired into `availableActions` (action_menu.go). The menu closes before the
`run` handler fires (existing dispatch), so `ExecProcess` runs with the menu
gone.

```go
actionRow{
    id:    "edit-file",
    label: "Edit in editor",
    run:   func(m Model) (tea.Model, tea.Cmd) { return m, m.editFileCmd(p) },
}
```

### Suspend + run — `editFileCmd(rel string) tea.Cmd`

```go
abs := filepath.Join(m.currentWorktree, rel)
cmd := editorCommand(resolveEditor(), abs)
return tea.ExecProcess(cmd, func(err error) tea.Msg {
    return editorFinishedMsg{path: rel, err: err}
})
```

`tea.ExecProcess` releases the terminal, runs the editor in the foreground, and
restores the TUI on exit.

### Return — `editorFinishedMsg{path string; err error}` + status refresh

Handled in `model.go` Update:

- On `err != nil`: `m.statusMsg = "edit: " + err.Error()`, then dispatch
  `m.reloadStatusCmd("")` (empty summary won't overwrite the error message).
- On success: dispatch `m.reloadStatusCmd("edited " + path.Base(msg.path))`.

`reloadStatusCmd(summary string)` reads only the working-tree status off the UI
thread and returns a `statusRefreshedMsg{summary, status, err}` — the existing
handler (model.go:917) updates `m.status`, sets the summary, and clamps the
Files/Staged selections. Editing a file changes only working-tree status, never
commits, so a status-only refresh is correct and snappy on large repos.

## Testing

Pure / unit-testable parts:

- `resolveEditor()`: `t.Setenv` VISUAL+EDITOR → VISUAL wins; only EDITOR → EDITOR;
  neither → `defaultEditor()`. (Unset both with `t.Setenv(x, "")`.)
- `editorCommand()`: `"code -w"` → argv `[code -w <abs>]`; `"vim"` →
  `[vim <abs>]`; the final arg is always `absPath`.
- `defaultEditor()`: non-empty; `vi` on the linux test host.
- `fileEditRow()`: present on Files panel with a selection; absent on Staged and
  Commits panels and while `m.running`; appears in `availableActions(m)` for a
  Files selection.
- `editorFinishedMsg` handler: error sets the `edit:` status message and returns
  a non-nil cmd; success returns a non-nil cmd. (`reloadStatusCmd` is exercised
  here; the actual `ExecProcess` suspend is not unit-tested — the argv/resolution
  helpers carry the logic.)

## Files

- **Create:** `internal/tui/edit_actions.go`, `internal/tui/edit_actions_test.go`
- **Modify:** `internal/tui/action_menu.go` (wire `fileEditRow`)
- **Modify:** `internal/tui/op.go` (`editorFinishedMsg`, `editFileCmd`,
  `reloadStatusCmd`) — or place the cmd helpers in `edit_actions.go`; the msg
  type lives next to the other op messages.
- **Modify:** `internal/tui/model.go` (`editorFinishedMsg` case in Update)
- **Modify:** `internal/tui/help.go`, `CHANGELOG.md`

## Out of scope (v1)

- Staged panel and commit files-view editing.
- A dedicated keybind (menu-only).
- Shell-quote parsing of the editor command (whitespace-split only).
- **Non-blocking GUI editors:** an editor that returns immediately without a
  wait flag (e.g. `EDITOR=code` instead of `code -w`) fires the refresh before
  the user saves. This matches git's behavior and is the user's own config —
  configure the editor to block (`code -w`, `subl -w`, `idea --wait`).
- Blocking input "while editing" — `ExecProcess` suspends the whole program, so
  there is no concurrent UI to guard.
