# Open files — stage 5: the web

Status: agreed in brainstorming 2026-09-26. Stage 5 of
`2026-09-24-open-files-design.md` (the TUI stages 1–4 are merged, last
`f7538a76`). Four plans, each merged on its own: 5a → 5b → 5c → 5d.

## Goal

`gg web` gets what the TUI has: a file viewer (working tree, commit, shelf),
a shared open-files list that follows the disk, the agent verbs
(`--background`, `gg session files`, `files focus`), and the F working-tree
finder. Today the page copies content links but cannot open them.

## Rulings (agreed)

1. **All three pieces plus F**, viewer first (5a), then the list (5b), the
   agent verbs (5c), the F finder (5d).
2. **The viewer is an overlay layer** like blame (`pushLayer`) — the web's
   full-screen viewer. The diff pane stays diffs only (F's preview is the one
   exception, 5d).
3. **Entry points:** *view file* in the file-row menus; a content link pasted
   into `#`; the steered navigate and `gg open --web`; the F finder.
4. **One shared list, owned by the gg web server** — per worktree, every tab
   sees it, an agent's `gg session files` answers from it. Cursor and scroll
   are per tab; the server keeps the LAST cursor line a tab reported.
5. The F finder **takes the files pane**, with a live preview in the diff pane.

## Architecture

### Server (`internal/web`, domain reads only)

- **Registry** (`openfiles.go`, pure core, unit-tested): per worktree, cap 20,
  most recently shown first; `touch` evicts the least recently shown entry no
  tab shows. Entry: id `f<n>` (process counter), source (`worktree` |
  `commit` + sha | `shelf` + id), path, line (last reported, 1-based, 0 =
  none), shown (by any tab). Lives as long as the gg web process. Guarded by
  a mutex (handlers run concurrently).
- **Shown tracking:** a tab reports which file it shows (open/close/background
  of its overlay) with its tab id (the SSE client id); an entry is shown while
  any live tab shows it. A tab whose SSE stream closes stops showing
  everything.
- **Content:** `GET /api/file-content?src=worktree|commit|shelf&rev=&path=` →
  `{lines, tok, disk:{size,mtime,missing}}`. Reads through the domain calls the
  TUI uses (`WorktreeFile`, `ShowFile`, `ResolveBytes`); tokens via the syntax
  lexer as `/api/blame` does (`SyntaxHighlighting()`, `MaxSyntaxBytes`). A
  working-tree file is stat'ed BEFORE the read (the TUI's baseline rule); a
  missing one answers `missing:true`, not an error. Every value through
  `isGitArgSafe` → 400.
- **List:** `GET /api/open-files` (the current worktree's list, wire form =
  `steer.OpenFile`); `POST /api/open-files` with `{op: open|background|focus|
  close|cursor|shown, id|src+rev+path, line, tab}`. Mutations go through the
  write guard. Each change is broadcast over the existing SSE stream as
  `open_files` (the whole list); an eviction's name rides the event and the
  POST answer.
- **Watching:** the server stat-polls its open working-tree entries — shown
  ones every 1 s, background ones every 5 s — plain `os.Stat`, never git;
  `internal/filewatch` wakes the poll early where supported (the TUI's rule:
  events only wake, the stat decides). A change → SSE `file_changed {id}`;
  tabs showing it re-fetch and keep their place (cursor line + scroll top at
  fill time; a link's pending line wins).

### Page (`static/`)

- **Viewer overlay** (`viewer.js`): title `View <path> (working tree | @ sha7 |
  shelf)` (path cut in the middle), line numbers, syntax colour, the shared
  `w` long-line mode. Line cursor: `↑↓ j k`, pgup/pgdn, home/end, click.
  In-view search `/`, `] [` via `inviewsearch.js`. `.` / right-click menu:
  **Copy file link** (at the cursor line), **Copy line**, **Diff** (a
  working-tree file: HEAD ↔ working tree; a commit version: that commit's
  change; none for a shelf version), **History**, **Blame** (at the version's
  commit; working tree for a working-tree file). `ctrl+]` background (5b), `esc`
  close. Keys are advertised in the footer chips, never inside the box.
- **Switcher (5b):** `ctrl+\` popup over the server list — `●` shown in this
  tab, `○` otherwise, `:line`, the version; `enter` brings it back where this
  tab left it (else at the server's line), `x` closes it.
- **F finder (5d):** `F` turns the files pane into a fuzzy list of every
  working-tree file (tracked minus status `D`, plus untracked marked
  `(untracked)` — the TUI's `worktreeFileList` rule, served by a new
  `GET /api/worktree-files`); `/` filters (fuzzy rank, cap 200); the diff pane
  previews the file under the cursor after a 150 ms settle; `enter` / `.` open
  the actions (view file, diff, history, blame, copy name / path / link);
  `ctrl+]` on a row opens it in the background; `esc` restores the pane.

### Agents (5c)

- `toSteerWire` accepts `Background`, `files`, `file_focus` (the stage-4
  refusals go). `files` is answered SYNCHRONOUSLY in the steer POST's
  response body (the server owns the list); `--background` loads into the
  list; `file_focus` is broadcast to the tabs (every tab brings it up) and
  answered `focused <path> [at line N]` from the server's own lines.
- CLI routing: `steerTUI` becomes "the live session": TUI when live (its
  reply decides), else the web page's server. A background open and a focus go
  to BOTH when both are live (as `navigate` does); `files` answers from the TUI
  when live, else from the web. `gg web does not keep open files yet` goes.
- using-gg: the verbs work with `gg web` too; bump `agentskill.Version`.

## Plans

| Plan | Ships |
|------|-------|
| 5a viewer | `/api/file-content`, the overlay (cursor, search, `.` menu, Copy link at line), *view file* in the working-tree / commit / shelf menus, content links land on the web (`#` paste, steered navigate, `gg open --web` — both refusals lifted). No list: esc closes. |
| 5b open files | server registry + endpoints + SSE events, `ctrl+]`, the `ctrl+\` switcher, stat poll + filewatch wake, deleted placeholder, reuse on reopen |
| 5c agents | steer verbs on the web, CLI routing, skill |
| 5d F finder | `/api/worktree-files`, files-pane mode, fuzzy, diff-pane preview, actions |

## Error handling

| Case | Result |
|------|--------|
| file not in the working tree | navigate fails `<path> is not in the working tree`; menus never offer it |
| load fails | `(load failed: …)` in the viewer; a steered navigate fails |
| 21st file | least recently shown, not shown by any tab, is closed; status + agent reply name it |
| deleted on disk | kept, `(file deleted on disk)`, reloads when the file returns |
| `files focus` unknown | exit 1 `no open file <x>` |
| unsafe path/rev | 400 |
| line past the end | lands on the last line, `(line N is past the end, M lines)` |

## Testing

Go unit tests for the registry (order, cap, eviction, reuse, shown-by-tab,
tab drop) and every handler; page logic in the `*js_test.go` style; a
browser check per plan that ASSERTS VISIBILITY, run against the unfixed
build first; e2e rows for a web-only `gg session files`; `./test.sh` +
`./test.sh race` per plan; `./build.sh web` after each merge.

## Docs

Per plan: CHANGELOG, README (the web section + *Open files*),
`docs/CLAUDE-details.md`, memory; 5c: `using-gg.md` + version.
