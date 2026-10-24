# gg web — bookmark and shelf rows as the TUI switchers

User report: the web's bookmark and shelf right-click menus read like a
file's menu (file history, three copy rows) rather than the TUI's bookmark
(`g`) and shelf (`G`) switchers. Asked for: a kind letter before every
entry, and entry paths elided like file paths (the last segment kept).

## Rows (TUI `bookmark_popup.go` / `shelf_popup.go` rows)

- A kind letter leads each row: **F** a file, **C** a commit, **S** a shelved
  set of files.
- The text is the TUI's display: `<container> / <commit-or-state> / <path>`,
  then ` — <label>` (a commit bookmark's subject; any shelf label).
- A row with a path is cut with `elidePath` (the file rows' rule: whole
  segments from the middle, the file name kept) to the list's columns; the
  full text is the tooltip. A commit row keeps its end cut.
- Rows re-elide when the sidebar is resized (the Branches / Worktrees rule).

## Menus (the switchers' keys, in their order and words)

| TUI key | file bookmark | commit bookmark | shelf file | shelf commit / set |
|---|---|---|---|---|
| enter | diff against the working-tree file | compare against the selected commit | diff against the working-tree file | browse its files |
| p | paste to a path… | — | restore to a path… (prefilled) | restore a file… |
| t | copy to a new dir under `<repo>.tmp`… | same | same | same |
| a | — | cherry-pick onto the current branch | — | cherry-pick (commit only) |
| y | copy path… (path / absolute path / file name) | copy commit id | copy path… | — |
| L, m, c | the existing link and compare rows | | | |
| n | — | — | view note | view note |
| x | remove | remove | remove | remove |

Then web-only rows below a separator: file history (a file entry), show
commit / show the original commit, view file. A left click runs the menu's
first row (the TUI's enter).

- **Ruling (`e`):** the external editor waits for the editor-launch stage;
  the row is not offered.
- **Ruling (y):** one `copy path…` row opens the three choices as a second
  menu at the same place (the TUI's y asks which).
- Words the TUI prints: `no commit selected to compare against`, `select a
  different commit to compare against`.
