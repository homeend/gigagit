# gg web — open in external editor (parity stage J1)

Parity row: docs/web-tui-parity.md "Open in external editor" (ABSENT). Stage J
is split: J1 = the editor (this), J2 = the external-tools wizard and the
repo settings location.

## TUI behaviour (source + captures, fx25 with a fake `$EDITOR`)

The TUI hands its terminal to `$VISUAL`, else `$EDITOR`, else `vi` (`notepad`
on Windows), through `handover()`. A line goes to the editor as `+N` for vi,
vim, nvim, nano, emacs, micro and kak, as `--goto path:N` for code, codium and
cursor, as `path:N` for hx, subl and zed, and is dropped for any other
program. Two kinds of open:

- **Live edit** of the working-tree file (`editFileAtCmd`, cwd = the
  worktree). When the editor exits the status reloads and the status line
  says `edited <base>`; on an error, `edit: <err>` and still a reload.
- **Read-only view**: the bytes go to a temp file `gg-<rand>-<base>` (0400,
  the extension kept for highlighting), the editor opens it, and the temp is
  removed on exit. Status `viewed <base>`, or `view: <err>`. A file over
  `domain.MaxDiffBytes`: `file too large to open (N bytes)`.

| Surface | TUI | Kind | Status line (captured) |
|---|---|---|---|
| Files panel `.` → **Edit in editor**; F finder → Edit in editor | menu | live | `edited f.txt`, the Files list reloads, cursor kept |
| Staged panel `.` → **Open staged version in external editor** (not a staged deletion) | menu | view of the index blob | `viewed s.txt` |
| Commits **`I`** (and the commit menu's "open message in editor") | key | view of the full message, temp named `COMMIT_EDITMSG` | `viewed COMMIT_EDITMSG` |
| Diff **`e`** / `.` → **Open in editor at line N** (not on a two-sided compare, a binary, a too-large diff, or while an op runs) | key + menu | working-tree diff (staged too): live at N; commit diff: view of rev:path at N | the diff stays on screen |
| File history **`e`** | key | view of the selected commit's version | `viewed <base>` |
| Blame **`e`** | key | view at the blamed rev (working-tree blame: the on-disk file, as a copy) | `viewed <base>` |
| Bookmark switcher **`e`** (file bookmarks) / shelf switcher **`e`** (file entries) | key | view of the stored copy | `viewed <base>` |
| A commit's files view `.` → **Open in external editor** (tree side, not a deleted row; a shelved commit's member in shelf mode) | menu | view of the file at the commit | `viewed <base>` |

Side effects on screen: only the status line, and for a live edit the
status reload. The cursor, the focused panel and any layer under the
editor stay as they were; the editor owns the whole screen while it runs.

## Port

The browser has no terminal to hand over, and `$EDITOR` is usually a
terminal program, so the server cannot spawn it detached. **Ruling:** the
editor runs in a PTY session that the page shows as a console (the same
console the agents and terminals use). That console is the terminal the TUI
hands over.

### Domain

- `domain.ResolveEditor(getenv) string` and `domain.EditorArgv(editor,
  path string, line int) []string`, moved out of `internal/tui` with the
  same rules. The TUI's `resolveEditor` / `editorCommandAt` become thin
  wrappers, so its tests keep pinning the behaviour.
- `domain.WriteViewCopy(name, data) (string, error)` and
  `RemoveViewCopy(path)`: the TUI's read-only temp helpers, moved likewise.
- `(*Service).WorktreeAbsPath(ctx, rel) (string, error)`: the working-tree
  path of rel, refused when it escapes the tree (git's `worktreePath`).
- `(*Service).StartEditor(ctx, argv, label, worktreeDir, cwd, cols, rows,
  done func()) (*AgentSession, error)`: a Terminal session (label `Editor:
  <base>`). A goroutine waits on `Done()`, then runs `done` (removing the
  temp file of a view) and removes the session from the list. It is
  started directly through domain in hosted mode too: `Sessions()` is
  process-global and an editor needs no steer inbox or agent channel.

### Server: `POST /api/editor-open` (writeGuard)

Body: `{source, path, rev, id, line, cols, rows}`. `source` is one of:
- `worktree`: a live edit of path;
- `staged`: the index blob;
- `commit`: rev:path;
- `message`: rev's message;
- `bookmark` / `shelf`: by id;
- `worktree-copy`: a working-tree blame's on-disk file, as a copy.

The editor command comes from the SERVER's environment, never from the
request. `rev` and `id` go through the existing resolvers (an unknown one
→ 404), and path through `WorktreeAbsPath` / the git readers. Over
`MaxDiffBytes` → 413 with the TUI's words. The answer is `{session, name,
live}`. Read errors give 4xx/5xx with the git error, and the page shows
`edit: …` / `view: …`.

### Page (editor.js)

`openInEditor(req, name, live)` posts, opens the console bound (focused),
and remembers `{id → name, live}`. When that session leaves the list (the
sessions event), the op line says `edited <base>` (and the status reloads)
or `viewed <base>`. The console's own "session gone" close brings the page
back where it was (leaveConsole).

Surfaces (the TUI's key where the web has it free; a menu row where the TUI
has a menu row):
- the working-tree file list's menu: **edit in editor** (also the F
  finder's file menu, if it has one);
- a staged file's menu: **open staged version in external editor** (not a
  staged deletion);
- commits: **`I`** and the commit menu row **open message in editor**;
- diff: **`e`** and the diff menu row **open in editor at line N**. The
  line is the clicked row's new-side number (`tr.cur`), else the first
  visible row's. Not offered on a compare (`ctx.compare`), the all-files
  tree diff, or a binary or too-large diff. `rev === ""` → live at N
  (staged included, approximate as in the TUI); else a view of rev:path at
  N;
- file history: **`e`**, the selected commit's version;
- blame: **`e`**;
- bookmark and shelf entry menus: **open in external editor** (file
  entries only);
- a commit's file list menu: **open in external editor** (not a deleted
  row). The TUI's file viewer has no editor key, and the web's gets none.

Every new key goes in help and its surface's foot chips.

### Design differences (parity doc)

- The editor runs in a page console, not over the whole screen. The page
  stays usable beside it, and the session is listed (and walked by alt+t)
  while it runs; it leaves the list when the editor exits.
- A GUI editor that returns at once opens and closes the console right
  away (the TUI blinks the same way).
- The editor is the server's `$VISUAL` / `$EDITOR`, which is the machine
  gg runs on.

## Out of scope

Choosing the editor from the page; editing a commit message through the
editor (reword stays the page's own form).
