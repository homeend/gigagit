# Open files: background file viewers + a switcher

Status: agreed in brainstorming 2026-09-24. TUI first; the web follows once
the whole TUI feature is complete.

## Goal

A file shown to the user — by the user or by an agent — can be sent to the
background instead of closed, and brought back later exactly as it was
(cursor line, scroll, selection, search). Up to 20 files stay open per
worktree. A switcher lists them. An agent can open a file in the foreground
or the background, list the open files and bring one to the front, so it can
walk the user through "this is in file A line 40, that is in file B line 12".

## Rulings (user, 2026-09-24 — do not re-ask)

1. Files only — no diffs in the list (a later stage if ever).
2. **esc closes** a file (it leaves the list); **ctrl+]** sends it to the
   background (it stays in the list).
3. The switcher is the existing **ctrl+\\** popup, grown a second group:
   agent sessions first, then open files.
4. Cap **20** open files per worktree; opening a 21st **drops the oldest**
   (least recently shown) with a notice.
5. Opening a file that is already open **reuses** its entry (brings it to the
   front, moves the cursor to the new line when one is given).
6. Working-tree files are **watched** and **reloaded** when the disk changes,
   keeping the cursor line.
7. The list is **per worktree** and lives as long as the TUI process.
8. Every entry point takes part: `gg open`, `gg session navigate`, the
   palette's "Open gg:// link…" (`#` prompt), and the tree's context-menu
   **View file** (the right-column preview).
9. A file backgrounded from the right-column preview comes back in the
   **full-screen viewer** (option a): the switcher works from anywhere, and the
   files view it came from may be closed by then. The right-column preview
   itself stays as it is.

## Architecture

### Stage 1 — refactor: one open-file document (no behaviour change)

Today a viewed file's state lives twice: the files view's right-column
preview (`m.filesPreview` + `m.filesPreviewTag`) and the full-screen
`fileViewer` (its own popup + tag + `pendingLine`), and `fileContentMsg` has
two near-identical fill arms. A list of open files would be a third copy.

- **`openFile`** (new, `internal/tui/open_file.go`): the document —
  `src fileSource` (working tree | commit `<sha>` | shelf `<id>`), `path`,
  `p *contentPopup` (lines, cursor, selection, search, mode — the viewing
  state), `tag` (unique per document, gates stale loads), `pendingLine`,
  `load func(ctx) ([]byte, error)`, and ONE `fill(m, fileContentMsg)` that
  both arms call today.
- The right-column preview and `fileViewer` each hold an `*openFile`; they are
  two FRAMES over one document type. `activePreview()` keeps answering with
  the frame's `doc.p`, so every preview key keeps working unchanged.
- `fileContentMsg` routes by **tag** to its document, not to "the topmost
  `fileViewer` on the stack" — with several open files (and background
  reloads) the topmost viewer is not the one a result belongs to.
- `fileViewer` becomes source-aware: title `View <path> (working tree)` /
  `View <path> @ <sha7>` / `View <path> (shelf)`. Only working-tree documents
  are watched or produce content links' lines as-is (the preview's Copy file
  link keeps its disk-match rule).
- `fileSource` + `path` is the **reuse key** (ruling 5).

Tests: the existing preview / viewer / file-link / steer-content suites stay
green unchanged; one new test proves a stale tag and a second open document
do not cross-fill.

### Stage 2 — the open-files list (TUI)

- **Registry** `openFiles` (a pointer field on Model, keyed by worktree path —
  the list you see is the current worktree's; `reRoot` to another worktree
  shows that worktree's list). Entries are `*openFile` in most-recently-shown
  order. Survives repo/worktree switches for the life of the process.
- **Open** (every entry point): look up the reuse key → reuse or create;
  push its `fileViewer` frame (or fill the right-column preview for View
  file); move it to the front of the recency order. A 21st entry evicts the
  least recently shown **background** entry — status
  `closed <path> (20 files open)`; the agent reply names it too.
- **ctrl+]** in `fileViewer`: remove the frame from the stack, keep the
  document → the screen returns to whatever was beneath. In the right-column
  preview: close the preview pane (the tree takes the column back), keep the
  document in the list. Status: `<path> is in the background — ctrl+\ lists
  open files`.
- **esc**: as today (closes), and the document leaves the list.
- **Switcher** (ctrl+\\): a second group `Open files` under the agent
  sessions, rows `<path>  :<line>  <source>` with `●` on the one on screen.
  `enter` brings it to the front (a `fileViewer` frame over whatever is open);
  `x` closes it; `/` filters both groups. The group is shown even with no
  agent sessions (the popup opens whenever either group has rows).
- Footer/help: `[ctrl+]] background` in the viewer's hint line; the global
  `[ctrl+\] agents` footer entry becomes `[ctrl+\] agents & files` when files
  are open; help rows updated (`TestHelpFooterCoverage`).
- The `.` menu of a viewer also offers **Send to background** (discoverability
  for mouse users).

### Stage 3 — watching working-tree files

- A small watcher over the open **working-tree** documents of the current
  worktree: fsnotify on each file's directory **plus a polling fallback**
  (stat size + mtime: the file on screen every ~1 s, background ones every
  ~5 s). Polling is required: the user's repos live on `/mnt` (WSL drvfs/9p),
  where inotify misses edits made from Windows, and `gitwatch` already turns
  itself off there.
- On a change the document reloads off-thread; the fill keeps the cursor on
  the same line number (clamped), the scroll top, and a live search (re-run).
  A selection is dropped (its lines may have moved) with no notice.
- A file deleted on disk: the entry stays, shows `(file deleted on disk)`,
  and reloads if the file comes back.
- Commit and shelf documents are immutable: never watched.

### Stage 4 — agent verbs

- `gg open <content-link> --background` / `gg session navigate … --background`:
  load the file into the list without touching the screen; reply
  `opened <path> in the background [at line N]`. Refused (exit 2) for a link
  that is not a content link.
- `gg session files [--json]`: the current worktree's open files — id, path,
  source, line (cursor, 1-based), `shown` / `background`. Also published in the
  session snapshot (`open_files`) for MCP's `gg_ui_state`.
- `gg session files focus <id|path>[:<line>]`: bring an open file to the
  front, optionally at a line; exit 1 when no such open file.
- A foreground `gg open` of an open file IS a focus (reuse rule) — the agent
  needs `files focus` only for commit/shelf versions the user opened, which
  have no content link.
- Steer protocol (additive): `Command.Background bool`; new commands
  `files` (reply carries the list) and `file_focus` (`ID`/`File` + `Line`).
  `gg web` refuses the new commands until the web stage.
- using-gg skill: the flow "open A, B, C in the background, then focus each
  while explaining" + the verbs.

### Stage 5 — web (later, after the TUI is complete)

## Error handling

| Case | Result |
|------|--------|
| 21st file | oldest background entry dropped, status + reply name it |
| `--background` on a non-content link | exit 2 |
| `files focus` unknown id/path | exit 1 `no open file <x>` |
| watched file deleted | entry kept, `(file deleted on disk)`, reload on return |
| load fails | as today: `(load failed: …)`; a navigate fails |

## Testing

Per stage, TDD: registry order/cap/eviction/reuse (pure), ctrl+] / esc / the
switcher's enter/x, per-worktree lists across `reRoot`, watcher polling with
an injected clock + a real temp file, steer `files`/`file_focus`/
`Background`, CLI verbs + exit codes, an e2e scenario for `gg session files`
without a live TUI (clear error), the i18n gates, `TestHelpFooterCoverage`.

## Docs

CHANGELOG, README, `docs/CLAUDE-details.md` (the document/frame split, tag
routing, the registry), `using-gg.md` + version, memory.
