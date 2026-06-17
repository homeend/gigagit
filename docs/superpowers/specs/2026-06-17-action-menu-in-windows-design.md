# Action menu in every navigable window — Design

**Date:** 2026-06-17
**Status:** Approved (brainstorm)

## Problem

The `.` action menu only opens in the base panel layout. The `KeyMsg` dispatch
in `internal/tui/model.go` is a precedence chain: every keyboard-owning window
(`filesView`, `diffView`, the stash list, and the view-stack surfaces
`historyView`/`blameView`) early-returns to its own key handler **before** the
base switch where `.` opens the menu (`model.go:626`). Those handlers have no
`.` case, so `.` is silently eaten.

Reported symptom: after `l` on the Commits panel (opens the commit **file
tree** = `m.filesView`), `.` stops working; it also never worked inside the
file tree or the other content windows.

## Goal

`.` opens the action menu from any **navigable content window**, showing the
copy actions relevant to the file/commit in view. The window's own keys
(h/b/enter/z/arrows/…) are unchanged.

Windows in scope:
- **File tree** (`m.filesView`, opened by `l` on Commits, or from a stash).
- **Stash list** (`m.stashView` while focused over Commits).
- **Diff view** (`m.diffView`, full-screen side-by-side).
- **File history** (`*historyView`) and **blame** (`*blameView`) — view-stack surfaces.

Explicitly **out of scope** (the menu must NOT open over these): decision
modals, all popups, and transient view-stack editors (the interactive-rebase
editor `*irebaseEditor`, the conflict/hunk picker `*hunkPicker`). The action
menu is itself a popup; it should not stack over another popup/editor.

## Non-goals — deferred to a future stage

A richer set of **text operations** for the file/diff views — copy the selected
line, mark a text range and copy it, copy a diff hunk from the left/right side —
is a natural follow-on but is **a separate future stage**, not this one. This
stage only adds the existing identity-copy actions (file path, file name,
commit id, stash ref) to the windows. Recorded here so it isn't lost.

## Design

The action menu is conceptually a modal-like overlay: when open it owns the
keyboard and draws on top, above every content window (but still below a
decision modal, which never coexists with it). Three coordinated changes:

### 1. Precedence: the menu out-ranks content windows (dispatch + render)

**Dispatch (`model.go`, `case tea.KeyMsg`).** Move the existing
`if m.actionMenu != nil { return m.updateActionMenuKey(msg) }` check from its
current position (after `diffView`/popups) to **immediately after the modal
block**, before the `stackTop()` check. Rationale: today the menu can only open
from the base layout, so no other surface is ever active while it is open —
moving the check up is behavior-neutral for the current cases. After this
change, when the menu is open over a `stackTop` surface or the diff view, its
keys reach `updateActionMenuKey` instead of the underlying surface.

(The `filesView`/`stashView` checks are already *after* the `actionMenu` check,
so those two windows need no dispatch move — only the `.` opt-in below.)

**Render (`view.go`, `View()`).** Add, immediately after the modal overlay
block, a menu overlay that composites over whatever the background would be:

```go
if m.actionMenu != nil {
	return overlayCenter(clipToHeight(m.menuBackground(), h), m.renderActionMenu(), w, h)
}
```

with

```go
// menuBackground is what renders behind the action menu overlay: the topmost
// content window if one is open, else the panel interface.
func (m Model) menuBackground() string {
	if s := m.stackTop(); s != nil {
		return s.render(m)
	}
	if m.diffView != nil {
		return m.renderDiffView()
	}
	return m.renderInterface() // includes filesView
}
```

Remove the now-redundant `actionMenu` branch lower in `View()` and drop
`m.actionMenu == nil` from the "no overlay" guard (the menu is handled at the
top now). The `stackTop`/`diffView` early returns stay for the menu-closed case.

### 2. Each navigable window opts in to `.`

Add `case ".": return m.openActionMenu(), nil` to the key handlers of the
in-scope windows only:

- `updateFilesViewKey` (`files_view.go`)
- `updateStashViewKey` (`stash_view.go`)
- `updateDiffViewKey` (`diff_view.go`)
- `(*historyView).update` (`history_view.go`)
- `(*blameView).update` (`blame_view.go`)

The transient editors (`*irebaseEditor`, `*hunkPicker`) and all popups are
**not** touched, so `.` stays inert there.

### 3. The menu is window-aware (copy rows only, in-view target)

In a content window the menu lists **only** copy actions — never the panel
context bindings. This is a hard early return (not a soft filter): with
`filesView` open, focus is still `panelCommits`, so the `commit-files [l]`
binding is true; if it leaked into the menu as a key-replay row, running it
would replay `l` and close the very window you opened the menu from.

`availableActions` (`action_menu.go`):

```go
func availableActions(m Model) []actionRow {
	if m.inContentWindow() {
		return m.contextCopyRows() // copy rows only; panel bindings don't apply here
	}
	// ... existing: row-scoped then window-scoped context bindings,
	// with contextCopyRows() prepended to the row group ...
}
```

```go
// inContentWindow reports whether a navigable content window owns the keyboard
// (so the . menu should offer only that window's copy actions). Transient
// stack editors (irebase editor, hunk picker) are NOT content windows.
func (m Model) inContentWindow() bool {
	if m.diffView != nil || m.filesView != nil {
		return true
	}
	if m.stashView != nil && m.focus == panelCommits {
		return true
	}
	switch m.stackTop().(type) {
	case *historyView, *blameView:
		return true
	}
	return false
}
```

`contextCopyRows` gains a window-aware prefix (topmost window wins; precedence
mirrors the dispatch/render chain), falling back to the existing panel logic:

```go
func (m Model) contextCopyRows() []actionRow {
	if v := m.diffView; v != nil {
		return m.fileCopyRows(v.title, v.rev) // title = path, rev = commit ("" = working tree)
	}
	switch s := m.stackTop().(type) {
	case *historyView:
		return m.fileCopyRows(s.ctx.path, s.ctx.rev)
	case *blameView:
		return m.fileCopyRows(s.ctx.path, s.ctx.rev)
	}
	if v := m.filesView; v != nil {
		var rows []actionRow
		if m.filesTreeFocused {
			if vis := v.visible(); v.sel >= 0 && v.sel < len(vis) && vis[v.sel].path != "" {
				rows = append(rows, m.fileCopyPathName(vis[v.sel].path)...)
			}
		}
		if m.filesHash != "" { // a commit's files (a stash file tree has no commit id)
			rows = append(rows, m.copyRow("copy-commit-id", "Copy commit id",
				"Copied commit id "+shortHash(m.filesHash), m.filesHash))
		}
		return rows
	}
	if v := m.stashView; v != nil && m.focus == panelCommits {
		if v.sel >= 0 && v.sel < len(v.entries) {
			ref := v.entries[v.sel].Ref
			return []actionRow{m.copyRow("copy-stash-ref", "Copy stash ref",
				"Copied stash ref "+ref, ref)}
		}
		return nil
	}
	// existing panel fallback (unchanged):
	switch {
	case m.focus == panelCommits:
		if bi, ok := m.backingIndex(panelCommits); ok {
			h := m.commits[bi].Hash
			return []actionRow{m.copyRow("copy-commit-id", "Copy commit id",
				"Copied commit id "+shortHash(h), h)}
		}
	case m.isFilesPanel(m.focus):
		if bi, ok := m.backingIndex(m.focus); ok {
			return m.fileCopyPathName(m.status.Files[bi].Path)
		}
	}
	return nil
}

// fileCopyRows: path + name copy rows, plus a commit-id row when rev is a real
// commit (rev == "" means the working tree).
func (m Model) fileCopyRows(filePath, rev string) []actionRow {
	rows := m.fileCopyPathName(filePath)
	if rev != "" {
		rows = append(rows, m.copyRow("copy-commit-id", "Copy commit id",
			"Copied commit id "+shortHash(rev), rev))
	}
	return rows
}

// fileCopyPathName: the path + basename copy rows for a file.
func (m Model) fileCopyPathName(p string) []actionRow {
	return []actionRow{
		m.copyRow("copy-file-path", "Copy file path", "Copied path: "+p, p),
		m.copyRow("copy-file-name", "Copy file name", "Copied file name: "+path.Base(p), path.Base(p)),
	}
}
```

**Refinement vs the approval-gate note:** the stash list now offers **Copy
stash ref** (the selected `stash@{N}`) rather than an empty menu — a trivial,
useful identity copy consistent with the rest. A window with genuinely nothing
copyable still shows the empty "(no match)" body.

### Known minor (accepted)

In the full-screen diff/history/blame views the "Copied …" status line lives in
the panel interface footer, which those surfaces don't render — so the copy
**happens** but its confirmation line may not be visible there. Not reworking
those footers in this stage.

## Testing

`internal/tui` (test files may import `internal/git`/`model`):

- **Opt-in opens the menu:** from `filesView`, `diffView`, `stashView`, and a
  pushed `historyView`/`blameView`, pressing `.` sets `m.actionMenu`.
- **Menu owns keys over a window (dispatch move):** with `m.diffView` set and
  the menu open, `m.Update(esc)` closes the menu and leaves `m.diffView` intact
  (key went to the menu, not the diff view). Same with a pushed `historyView`.
- **`.` is inert over non-content layers:** modal set → `.` doesn't open;
  `repoPopup` set → `.` doesn't open; a pushed `*irebaseEditor` and a pushed
  `*hunkPicker` → `.` doesn't open.
- **Window-aware `contextCopyRows`:** diffView(title,rev) → path/name+commit id;
  filesView tree-focused with a file + `filesHash` → path/name+commit id;
  filesView for a stash (`filesHash==""`) → path/name only; history/blame via
  `ctx`; stashView → `copy-stash-ref`.
- **No `[l]` leak:** with `filesView` open (focus `panelCommits`, `filesHash`
  set), `availableActions` returns only copy ids — no `commit-files` row.
- **Render overlay (view move):** with `m.diffView` set and `m.actionMenu` set,
  `View()` output contains "Actions"; same with a pushed `historyView`.

## File structure

| File | Change |
|---|---|
| `internal/tui/model.go` | Move the `actionMenu` dispatch check above `stackTop`/`diffView`. |
| `internal/tui/view.go` | Hoist the `actionMenu` render overlay above the `stackTop`/`diffView` early returns; add `menuBackground`. |
| `internal/tui/action_menu.go` | `inContentWindow`; window-aware `contextCopyRows`; `fileCopyRows`/`fileCopyPathName` helpers; `availableActions` content-window early return. |
| `internal/tui/files_view.go` | `.` opt-in in `updateFilesViewKey`. |
| `internal/tui/stash_view.go` | `.` opt-in in `updateStashViewKey`. |
| `internal/tui/diff_view.go` | `.` opt-in in `updateDiffViewKey`. |
| `internal/tui/history_view.go` | `.` opt-in in `(*historyView).update`. |
| `internal/tui/blame_view.go` | `.` opt-in in `(*blameView).update`. |
| `internal/tui/*_test.go` | New tests above. |
| `CHANGELOG.md`, `README.md` | `.` works in all navigable windows + stash-ref copy. |

No new package, no dep change, no CLI surface change (no agentskill bump).
`CLAUDE.md` unchanged (no architecture/package-map change).
