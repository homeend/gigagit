# gg web F4 — small rows

Six rows of `docs/web-tui-parity.md`. The TUI is the spec; the web sidebar
has no keyboard cursor, so where a TUI key lives on a sidebar list a ruling
names the stand-in.

## 1. Stash window `/` filter (TUI `stash_view.go`)

TUI: `/` starts a fresh filter, a case-insensitive substring of
`<ref>  <subject>`; typing narrows live, enter keeps it, esc drops it,
`ctrl+r` clears a kept one.

Port: the Stashes header gets a `/` button opening an inline filter input
over the list (same match rule, pure `stashVisible(entries, q)`); typing
narrows live; enter keeps the filter (the input stays, the list keeps its
rows); esc drops it; a kept filter shows `× clear` (the TUI's `ctrl+r`).
**Ruling:** a header button stands in for the `/` key (no sidebar focus).

## 2. Find current (`f`) on Branches and Worktrees (TUI `find_current.go`)

TUI: the cursor jumps to the checked-out branch / the shown worktree;
`no current branch: HEAD is detached`, `current worktree unknown`, or
`<name> is hidden by the / filter`.

Port: a `⌖` button in the Branches and Worktrees headers (unfolds the
section, scrolls to the row and flashes it; the TUI's words on the op line
when it cannot, the web's branch filter standing for `/`), and palette rows
`find current branch` / `find current worktree`. **Ruling:** a header button
and the palette stand in for `f`.

## 3. Settings filter (TUI `settings_popup.go`)

TUI: type to filter the Settings menu (case-insensitive substring of a
row's title); first esc clears the filter, the next closes; `(no match)`.

Port: the web Settings is one page of rows under headings, so the filter
input at the top keeps a row whose label or note matches, or whose heading
matches (pure `settingsRowMatch(q, heading, text)`); a heading with no
visible row hides; `(no match)` when nothing is left. Typing a printable key
while no field has focus goes into the filter; first esc clears it, the next
closes (the TUI's).

## 4. Token cheat sheets (TUI `popup_help.go`, `ctrl+d`)

TUI: `ctrl+d` in the add-prefix form opens `Prefix tokens & date formats`;
in the text-template add/edit form `Text template tokens & date formats`
(adds `<branch>`, `<parent-branch>` = the same as `<branch>`, any other
`<…>` stays). Live examples from now. `alt+x` opens text templates, also
from over an unfocused console.

Port: pure `tokensHelp(now, text)` returns the TUI's lines verbatim; a
`ctrl+d` key and a `formats` button in both forms open it in a content
overlay (esc returns to the form). `alt+x` opens text templates from
anywhere a layer does not own the keys.

## 5. "List notes…" in a diff (TUI `notes_list_popup.go`)

TUI: the `.` menu row `List notes…` (whenever the open diff has a thread):
`Notes on <path>`, one row per thread `◆ [<path> ]<anchor>  [review  ]
[<author>  ]<summary>[  +N][  resolved]` (anchor `new:15` / `old:15-23`,
`file` for a file-level forge note), type to filter (summary, author,
path), `(no matching notes)`, enter lands on the thread, `Note is no longer
in this diff` otherwise.

Port: the diff's right-click menu (and a ◆ row's) offers `List notes…`
when the open diff (or any loaded file of a stack) has a thread; an overlay
with the TUI's header, rows, filter and hint; enter calls `landNote(id)`.
Pure `noteListEntries(notes, path)`, `noteListLine(e)`.
**Ruling (stack):** a stack lists the threads of the files it has loaded;
an unread file's threads join once it is read.

## 6. ◇ Staged feed row (TUI `wip_rows.go`)

TUI: a ◇ Staged row below ◇ Working tree while anything is staged;
markable; its compare is HEAD ↔ index; marks pair in the order working
tree, staged, commits.

Port: a `◇ Staged (N)` row under the web's Working tree row while
`counts.staged > 0`; click opens `compare-sides commit:HEAD ↔ staged`;
ctrl+click / right-click mark it (`@staged`); two marks among working tree,
staged and one commit compare in that order. **Ruling:** the web's Working
tree row keeps opening the status view (staged and unstaged) — a web
design difference already listed.
