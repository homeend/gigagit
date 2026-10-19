# ctrl+\ — the Open files tab becomes the "Overviews & notes" tree

**Status:** draft for review · **Date:** 2026-10-19 · **Audience:** the TUI
(plan 1) and gg web (plan 2).

## 1. Why

The `ctrl+\` popup's third tab is called **Open files** and lists every
document of the worktree in one flat, most-recently-shown list. Since
session docs (spec `2026-10-18-gg-session-docs-design.md`) that list is
mostly NOT files the user opened: an agent's overview and every file that
carries one of its notes join it in the background. With one overview and
eleven noted files the overview is one row among twelve, and nothing says
which files belong to it.

The tab becomes a tree whose top level is what an agent (or a review)
PRESENTS — the overviews — with the files each one uses underneath, and is
renamed for what it holds.

## 2. The tree

```
[Overviews & notes 14]
 ▸ ○ Capability scheduling module   overview · 21 anchors · agent · 6m
 ▾ ○ Review of feat/x               review overview · 7 anchors
     ○ src/a.go          3 notes    working tree
     ○ src/b.go                     @ 1a2b3c4
 ▸ Notes                            2 files
 ▸ Opened                           3 files
```

Top level, in this order:

1. **Overviews** — every overview document of the worktree, in the list's
   existing order (most recently shown first): an agent's session overview
   (`srcOverview`), a stored review's overview (`srcReviewOverview`) and a
   permanent overview of a commit or branch (`srcCommitOverview`). The row
   text is today's (`title  overview · N anchors · author · age`).
2. **Notes** — a group row: the noted working-tree files that no listed
   overview names (§3).
3. **Opened** — a group row: every other document — files the user opened
   (`F`, a commit or shelf version), AI results and AI reviews — in today's
   order and with today's row text.

A group with nothing in it is not drawn. Children are indented one step.

## 3. An overview's children

The files an overview USES: one child per distinct path named by its
anchors — a path anchor's path, and a note anchor's note's path (looked up
in the store; a gone note names nothing). In the order the overview first
names them (the order its anchors walk). A path appears under every
overview that names it.

Each child row reads like a file row today: `○ path  :N  K notes  working
tree` (or `@ <sha>` for a stored overview, whose anchors open at its
commit). A child that is an open document shows its cursor line and note
count from that document; one that is not open shows the path and, for a
working-tree file, its note count from the store.

A noted file named by any listed overview is NOT repeated under **Notes**.

## 4. Keys

| key | on an overview | on a child | on a group row | on a Notes/Opened file |
|---|---|---|---|---|
| `enter` | opens the overview (today's `bringToFront`), NOT its files | opens the overview at its first anchor in that file (`openAnchor`: the split, overview beside the file) | expands / collapses | today's `bringToFront` |
| `→` | expands | — | expands | — |
| `←` | collapses | moves to its overview and collapses it | collapses | moves to its group and collapses it |
| `x` | today's close (background) | closes that document if it is open, else nothing | — | today's close |
| `D` | today's delete (asks first) | deletes every note on that file (asks first), as the file row's `D` today | — | today's `D` |

Everything starts collapsed. What is expanded is remembered per overview
(by its document key) and per group while gg runs. `/` filters as today on
the row text of every node: a match on a child shows its overview (expanded
for the filter only) with the matching children; a match on an overview
shows it with all its children.

The hint line: `[enter] open  [→/←] expand/collapse  [x] close  [D] delete
[/] filter  [z] mode  [tab] agents  [ctrl+t] full  [esc] close`.

## 5. The tab

- Label: **Overviews & notes N**, N = the number of documents (overviews +
  their open children + Notes files + Opened files, each counted once).
- The popup still opens on this tab when it is the only one with rows (as
  Open files does today); quit mode still has no such tab.
- The `help.go` row for `ctrl+\` and the README passage describe the tree.
- Every new string goes through `i18n.T` with keys in all four bundles.

## 6. gg web (plan 2)

The web switcher's **Open files** tab gets the same tree, label and keys
(`enter`, `→`/`←`, `x`, `D`, the row menu gains Expand/Collapse), built
from the same rules on the page's list (`ofList` rows already carry the
overview id; the server adds each overview's child paths to its row).

## 7. Not changing

What is in the list, when documents join or leave it, the delete and close
semantics, the session-docs files, `gg session files` (the CLI listing stays
flat) and the `● / ○` marks.

## 8. Testing

- TUI: a model with one session overview naming three files (one through a
  note anchor), two other noted files and one opened commit version →
  the rendered rows and their order, collapsed by default; `→`/`←`/`enter`
  on each node kind; a path named by two overviews under both; a noted file
  named by an overview absent from Notes; filter on a child path; expand
  state kept across a refresh and lost on quit.
- e2e: `tui_session_docs` and `tui_overview_anchor_bands` golden screens
  updated; a new scenario with an overview + its files expanded.
- Web (plan 2): the same over the page's pure tree builder (node test) and
  an HTTP row-shape test.
