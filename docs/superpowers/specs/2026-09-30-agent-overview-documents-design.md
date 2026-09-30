# Overview documents: an agent's guided tour through files

Status: agreed in brainstorming 2026-09-30. TUI only; `gg web` follows once
the TUI feature is settled.

## Goal

The user asks an agent about something ("how does the open-files list
work?"). The agent answers with an **overview**: a short markdown document
that lives only in the running gg, and whose links point at real files, at
lines in them, and at the temporary notes the agent put on them. The user
reads the overview, moves from anchor to anchor, opens one — the real file
comes to the front at the right place — looks around, presses backspace and
is back in the overview on the anchor they left, then opens the next one.

It is an interactive presentation of what the agent has to say, built on the
open-files list and the open-file notes.

## What already exists (not rebuilt here)

- Open files (`openFile`, `openFilesReg`, cap 20 per worktree), the
  full-screen `fileViewer`, the ctrl+\\ switcher, `gg session files` and
  `gg session files focus <id>`, esc-backgrounds / X-closes for a
  backgrounded document.
- Open-file notes (`2026-09-30-open-file-notes-design.md`): `t<n>` notes on
  open working-tree files, memory only, never evicted.
- AI-result documents (`srcExternal`, `srcNote`) rendered as markdown in the
  centred reading column (`prose`, `readingColumn`).
- `internal/markdown` (the one parser) and `tui/md_render.go` (rows + a
  per-rune class mask).
- The preview's line selection (`lineSel`) and the landing of a link's line
  (`pendingLine`).

## Rulings (user, 2026-09-30 — do not re-ask)

1. An anchor points at a **file**, a **line or range** in a file, or one of
   the agent's **notes**. Overviews do not link to other overviews.
2. A file opened from an anchor **stays open in the background** after back.
3. Back is **step by step** (see "Back"; with ruling 1 the chain is one step
   deep: file → the overview it was opened from).
4. **Several overviews** may exist; the agent can **replace** one's text by
   id.
5. Assumptions the user accepted: markdown; memory only (gone on X or quit);
   an overview is an open file (listed, switchable, esc backgrounds, X
   closes, never evicted by the cap); TUI only first; keys tab / shift+tab /
   enter / click / backspace; `r` copies a reference; `y` copies the text.

## Model

An overview is an `openFile` with a new source kind `srcOverview`. Its
`path` is a display name (`overview-<n>.md`), its `title` the agent's title,
and it carries an `*overview`:

```
overview {
    text    string        // the agent's markdown, as sent
    anchors []anchor      // in document order
    sel     int           // the selected anchor (-1 = none)
    w       int           // the width the rows were laid out at (0 = never)
}
anchor {
    target  anchorTarget  // what it opens
    label   string        // its text as shown
    spans   []anchorSpan  // where it sits: line index + rune range (a wrapped
                          // label has one span per row)
    missing bool          // its file was not found when last checked
}
anchorTarget { path string; start, end int; note string }  // note = "t<n>"
```

- **Identity.** An overview is named by its open-file id (`f<n>`); no second
  id space. `gg session files focus f<n>` brings it to the front like any
  open file.
- **Lifetime.** Memory only. It is created `backgrounded` (esc backgrounds,
  only X closes) and the 20-file cap never evicts it (the eviction rule
  already skips documents with notes; it also skips overviews).
- **No disk.** `onDisk()` is false: never watched, never polled, never
  re-read. Every load builds its rows from `text` (see Layout).
- **Limits.** Text ≤ 64 KiB; at most 200 anchors (links past the 200th are
  plain text); at most 20 overviews per worktree (a 21st `add` is refused).
  The title is one line, ≤ 200 runes.

## Anchor grammar

An anchor is a markdown link `[label](dest)` whose destination is one of:

| dest | opens |
|------|-------|
| `path` | the working-tree file |
| `path:N` | the file, cursor on line N |
| `path:N-M` | the file, cursor on N, lines N–M selected |
| `note:t<n>` | the file the note is on, cursor on the note's first line |

`path` is repo-relative in slash form; a leading `./` is dropped. An
absolute path, a `..` segment, a scheme (`x://`), whitespace or a control
character makes the link **not an anchor**. http(s) links keep today's
meaning (a link, not an anchor). Anything else keeps today's rendering
(label plus the destination as dim text).

**Parser change.** `internal/markdown` gains `ParseWith(src, Options)`;
`Options.Anchor func(dest string) bool` is asked about every link destination
that is not http(s). A destination it accepts becomes a new inline kind
`InAnchor` (`URL` = the destination, `In` = the label). `Parse(src)` is
`ParseWith(src, Options{})`: no caller other than the overview passes an
`Anchor` func, so the web and every forge rendering are unchanged and never
see `InAnchor`.

## Layout

The rows are laid out by gg (not wrapped by the window) at the frame's
reading-column width, so **one display row is one line** and a click maps
straight to a line and a column:

- `overviewLines(text, width) (lines []contentLine, anchors []anchor)` —
  `markdown.ParseWith` → `mdRows(doc, width)` with anchor runs carried
  through wrapping, then the anchor spans read off the rows. Anchor text
  wears a new class `mdAnchor` (underlined); the selected anchor wears
  `mdAnchorSel` (reverse video + bold); a missing one `mdAnchorGone`
  (faint + strikethrough).
- The document is re-laid out when the frame's width changes (checked when
  the viewer draws, like the note boxes' `noteW`); the selected anchor stays
  selected and on screen.
- Rows are `noWrap` (code and tables are clipped exactly as today); the
  viewer shows the overview in `modeScroll`, and ctrl+w is inert for it.

## Keys (the full-screen viewer showing an overview)

| key | does |
|-----|------|
| `tab` / `shift+tab` | select the next / previous anchor (wraps at the ends), move the line cursor to it and scroll it into view; the first tab selects the first anchor below the top of the window |
| `enter` | open the selected anchor (a live line selection keeps its meaning: enter copies it) |
| left click | on an anchor: select it; a second click on the same anchor within the double-click window opens it |
| `r` | copy a reference to the selected anchor (see below) |
| `y` | copy the overview's markdown |
| esc / X / ctrl+] / `.` / `/` / ↑↓ / pgup/pgdn | as in every open file |

The hint line of an overview leads with `[tab] next  [enter] open  [esc]
background  [X] close`.

**Opening an anchor.**

- A path anchor opens the working-tree file in the full-screen viewer on top
  of the overview (`openFileViewerEv`), landing its line; a range also
  selects its lines (a fixed `lineSel`, set when the load lands — a new
  `pendingEnd` beside `pendingLine`).
- A note anchor brings the note's file to the front with the cursor on the
  note's first line.
- The opened file is marked `backgrounded` (esc keeps it open) and remembers
  where it came from (`openFile.from`, see Back).
- An anchor whose target cannot be opened — its file missing, its note gone
  (dismissed, or its file closed) — does not open: the status line says why
  (`no file <path>`, `note t7 is gone`), and it is drawn as missing.

**Reference (`r`).** `gg overview f12 "<title>" → <dest>` goes to the
clipboard, so the user can paste it to the agent ("explain this step").

## Back

A file opened from an anchor carries `from *openFile` (the overview). In the
viewer showing that file, **backspace**:

1. sends the file to the background (it stays open — ruling 2), and
2. brings the overview to the front at its place, with the anchor that was
   opened still selected.

The file keeps `from` until another anchor opens it (the latest jump wins).
Backspace in a file with no `from` does nothing. If the overview was closed
in the meantime, backspace says `the overview was closed` and leaves the
file on screen. The hint of a file with a `from` leads with `[bksp] back`.

esc on such a file (backgrounded) also reveals the overview beneath it when
the file was opened over it; backspace is the key that finds the overview
wherever it is (the user may have switched files in between).

## Agent surface

### Steer protocol (`internal/steer`)

New commands, answered by the TUI before `steerRefusal` (they never fail
because the user is busy), web-refused like the note verbs:

| cmd | fields | answer |
|-----|--------|--------|
| `overview_add` | `Title`, `Text`, `Background` | `Reply.Overviews[0]` (id, title, anchors, unresolved) |
| `overview_set` | `FileID`, `Text`, optional `Title` | same |
| `overview_list` | — | every overview of the current worktree |
| `overview_show` | `FileID` | one, with `Text` |
| `overview_rm` | `FileID` | closes it |

`steer.Overview{ID, Title, State, Anchors int, Unresolved []string, Text}`.
`OpenFile` gains `Title` (set for overviews and AI results) and the source
`"overview"`.

- `overview_add` shows the overview in the full-screen viewer unless
  `Background` is set, or unless the screen cannot move right now
  (`steerRefusal` would refuse a navigate) — then it is added in the
  background and the reply's `Detail` says so.
- `overview_set` keeps the reader's place: the selected anchor stays
  selected when an anchor with the same destination exists in the new text,
  else the selection is cleared; the scroll position is kept (clamped).
- **Unresolved anchors.** On add and set, each path anchor's file is
  stat'ed (off the UI thread) and each note anchor looked up. The answer
  lists the destinations that do not resolve, and those anchors are drawn
  missing. An anchor is re-checked when it is opened.
- `MaxCommandBytes` rises from 16 KiB to 512 KiB (a 64 KiB text, JSON
  escaped, stays well under it).

### CLI (`internal/cli/session_overview.go`)

```
gg session overview add --title "…" [--file F] [--background] [--json]   (text from F, else stdin)
gg session overview set <id> [--title "…"] [--file F] [--json]
gg session overview list [--json]
gg session overview show <id> [--json]
gg session overview rm <id>
```

`add` prints the id and one `unresolved: <dest>` line per unresolved anchor.
The CLI checks the text size and the title before posting (exit 2 on
misuse). Like the note verbs it needs a TUI: a web-only worktree gets
`overviews need a gg TUI` (exit 1).

### Skill

`using-gg` gains an "Overviews" section: when to write one (the user asks to
be shown or walked through something spanning several files), the anchor
grammar, combining with notes (`note add` first, then `[…](note:t7)`), and
the verbs. `agentskill.Version` bumps.

## Switcher and lists

- The ctrl+\\ switcher row of an overview: `<title>  overview · <n> anchors`.
- `gg session files` shows it with source `overview` and its title.

## Errors and edge cases

- Empty text → refused (`an overview needs text`). Text over 64 KiB or a
  21st overview → refused with the limit named.
- An overview with no anchors is allowed (a plain note to the user); tab
  does nothing.
- A worktree switch: overviews stay in their worktree's list, like every
  open file; anchors always resolve against the worktree the overview
  belongs to (the current one while it is shown).
- `overview_set`/`show`/`rm` with an id that is not an overview → `no
  overview f<n>`.
- The file an anchor opens is closed later: nothing to repair (the next open
  re-opens it). The overview it came from is closed: see Back.

## Testing

- `internal/markdown`: `ParseWith` accepts/refuses destinations per the
  func, never emits `InAnchor` from `Parse`, http(s) unchanged, labels with
  emphasis, links in tables and lists.
- `overviewLines`: anchor spans correct through wrapping, wide runes, tables
  and lists; the anchor grammar table (accepted and refused destinations).
- Keys: tab order and wrap, enter on each target kind, range selection,
  missing anchors, r/y, click select + open.
- Back: returns to the overview with the anchor selected; the file stays
  open; closed overview; latest jump wins.
- Steer + CLI: add/set/list/show/rm, background add, refusal fallback,
  unresolved list, limits, web-only exit 1.
- Registry: never evicted by the cap; switcher row; `gg session files` row.
- One headless TUI run (`tui-capture.sh`) of a five-anchor overview:
  tab, enter, backspace, next anchor.

## Docs

CHANGELOG (entry on top), README (the overview verbs), CLAUDE-details
(an "Overview documents" subsection under open files), the skill.
