# Copy absolute file path — design

## Summary

Add a **"Copy absolute file path"** clipboard action alongside every existing
"Copy file path" surface in the TUI. It is additive: the existing "Copy file
path" (which copies the **repo-relative** path) is unchanged; the new action
copies the **absolute** filesystem path.

Absolute path = `filepath.Join(<worktree-root>, <repo-relative-path>)`, anchored
to the worktree the file belongs to. No symlink resolution (a literal join, so
it works even for a commit-only file not present on disk).

This is a **pure TUI clipboard action** — like the current copy-path, it goes
through `clipboard.Copy` (native OS command / OSC 52 fallback). There is **no**
engine operation, no CLI surface, and no `domain` query.

## Motivation

Users viewing a file in gg often want its full on-disk path to paste into a
terminal, editor, or another tool. Today only the repo-relative path and the
basename are offered. The absolute path is the one form gg does not provide.

## The three surfaces

"Copy file path" is offered in exactly three places. Each gains an absolute
sibling.

### 1. The `.` action menu — `action_menu.go` → `fileCopyPathName(p)`

This central helper backs every `.`-menu file surface: the Files panel, the
Staged panel, the files-view tree, and the history / blame / diff views (via
`fileCopyRows`). One new row is inserted **between** the path and name rows:

| id                 | label                     | text                                   | status                        |
|--------------------|---------------------------|----------------------------------------|-------------------------------|
| `copy-file-abspath`| `Copy absolute file path` | `filepath.Join(m.currentWorktree, p)`  | `Copied absolute path: <abs>` |

Base is always `m.currentWorktree` here — every file reachable from these views
lives in the currently-open worktree's tree.

### 2. The `y` copy chooser — `clipboard_cmd.go` → `copyFilePrompt` / `copyFileChoice`

Used by the `g` (bookmark) and `G` (shelf) quick-switchers. The chooser is
threaded with a **base worktree**:

- `copyFilePrompt(base, p string)` — opens the modal; an empty `base` falls back
  to `m.currentWorktree`.
- `copyFileChoice(option, base, p string)` — gains a `"Copy absolute file path"`
  case returning `filepath.Join(baseOr(base, m.currentWorktree), p)`.

Option order (Cancel stays last so `esc` maps to Cancel):

```
Copy file path · Copy absolute file path · Copy file name · Cancel
```

Call sites pass the entry's **own origin worktree** as the base:

- bookmark: `m.copyFilePrompt(b.Worktree, b.Path)`
- shelf:    `m.copyFilePrompt(e.Origin.Worktree, e.Origin.Path)`

`model.Bookmark.Worktree` / `model.FileAddress.Worktree` are populated for file
entries (staged/unstaged/untracked) and empty for committed-only entries; an
empty base falls back to the current worktree, so the copy is still sensible.
This is the behavior chosen during brainstorming: a bookmark pointing into a
sibling worktree resolves against **that** worktree's root, not the open one.

### 3. The fuzzy file finder — `file_finder.go` → `ff-copy-path`

The finder's per-file menu offers `"Copy path"`. It gains a sibling
`ff-copy-abspath` labelled **`"Copy absolute path"`** (matching the finder's
shorter wording), text `filepath.Join(m.currentWorktree, path)`.

## Shared helper

A small method centralizes the join + empty-base fallback so all three surfaces
agree byte-for-byte:

```go
// absFilePath joins a repo-relative path onto base, defaulting to the current
// worktree when base is empty.
func (m Model) absFilePath(base, rel string) string {
    if base == "" {
        base = m.currentWorktree
    }
    return filepath.Join(base, rel)
}
```

## Labels & ordering (summary)

- `.` menu:  `Copy file path` · **`Copy absolute file path`** · `Copy file name`
- finder:    `Copy path` · **`Copy absolute path`**
- `y` chooser: `Copy file path` · **`Copy absolute file path`** · `Copy file name` · `Cancel`

Status line for the new action mirrors the existing one: `Copied absolute path: <abs>`.

## Testing (TDD)

Existing tests cover the copy rows / chooser; extend them:

- `action_menu_copyrows_test.go` — `fileCopyPathName` now yields the
  `copy-file-abspath` row carrying `filepath.Join(worktree, p)`.
- `clipboard_cmd_test.go` — `copyFileChoice` returns the absolute form for
  `"Copy absolute file path"`; empty-`base` fallback to current worktree; the
  prompt's option list includes it in the right order.
- `file_finder_actions_test.go` — the finder menu includes `ff-copy-abspath`
  with the joined text.
- `bookmark_popup_test.go` / `shelf_popup_test.go` — the `y` chooser lists the
  absolute option and anchors it on the entry's origin worktree.

## Docs

- `internal/tui/help.go` — the copy-summary line (255) and the descriptive `y` /
  finder help lines.
- `internal/tui/popup_help.go` — the `y` cheat rows for the switchers.
- `CHANGELOG.md`.

No README or CLAUDE.md change: no architecture, package-map, or CLI-surface
change.

## Out of scope (YAGNI)

- Canonical / symlink-resolved paths (`filepath.EvalSymlinks`).
- A CLI `gg` verb for copying a path.
- Any new generalized "copy anything" framework — three call sites, one concept,
  extended in place.
