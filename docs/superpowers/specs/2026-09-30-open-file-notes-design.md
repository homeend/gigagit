# Open-file notes: an agent's temporary remarks on an open file

Status: agreed in brainstorming 2026-09-30. TUI only; `gg web` follows once
the TUI feature is settled.

## Goal

The user asks an agent a question in an agent console ("what is wrong with
X?") and then says "show me on the files". The agent opens the files that
matter in the background, puts short remarks on the lines it is talking
about, and brings the files to the front one by one. The user reads the
remark next to the code, can drop a remark they are done with, and can copy a
reference to one so they can ask the agent a follow-up about exactly that
remark.

The remarks are **temporary**: they belong to the open file, not to the
repository. They are gone when the file is closed.

## What already exists (not rebuilt here)

From the open-files feature (`2026-09-24-open-files-design.md`, all merged):

- `gg open <content-link> --background` / `gg session navigate <link>
  --background` — load an on-disk file into the open-files list without
  touching the screen.
- `gg session files [--json]` — list the open files (`f<n>` ids).
- `gg session files focus <id|path>[:<line>]` — bring one to the front.
- The ctrl+\\ switcher lists the open files; `x` there closes one.
- A backgrounded file (`openFile.backgrounded`) goes back to the background
  on esc; only `X` (viewer) / `x` (switcher) closes it.
- Working-tree files are watched and reloaded, keeping the cursor line.
- Inside a console gg started, `$GG_INBOX` routes every `gg session` verb to
  that gg.

The only new thing is the note layer.

## Rulings (user, 2026-09-30 — do not re-ask)

1. Notes are **temporary**: they live until the file is closed with X (or
   dismissed, or removed by the agent). They are kept in the TUI's memory
   only — never in the notes store, never in View all notes; quitting gg
   drops them.
2. **esc backgrounds** a file that carries notes; only X / the switcher's x
   closes it and drops its notes. No confirmation prompt.
3. The user can **read** a note, **dismiss** one note, and **copy a
   reference** to one. No reply, no editing, no "keep as a permanent note".
4. The reference goes to the **clipboard** and carries the **note id +
   path:lines** (not the note text).
5. A note **follows its text** when the file changes on disk; when its lines
   are gone it stays at its old line number, marked outdated.
6. **TUI only** for now.
7. A file carrying notes is **never pushed out** by the 20-file cap.

## Scope

- Working-tree documents only (`srcWorktree` — the file as it is on disk).
  Commit and shelf versions, AI-task results (`srcExternal`) and images take
  no notes; the verbs refuse them.
- Notes show wherever that document is drawn: the full-screen viewer and the
  files view's right-column preview of the same registered document (both
  draw through `renderPreviewBox`). The F window's unregistered live preview
  shows none.

Non-goals: persistence, replies, collapsing note boxes, the web page, a
permanent-note conversion, user-authored temporary notes.

## Model

A new file `internal/tui/open_file_notes.go`.

```go
// fileNote is one temporary remark an agent left on an open file.
type fileNote struct {
	id        string   // "t<seq>", unique in the process across all files
	start,end int      // 1-based line range, as it sits in the lines shown now
	anchor    []string // the text of those lines when the note was placed/last re-anchored
	summary   string
	rationale string
	author    string   // --author, else "agent"
	outdated  bool     // its lines are gone from the file
}
```

`openFile` gains `notes []*fileNote` (ordered by `start`, then id). The id
counter is a process-global `atomic.Int64` like `openFileSeq`, so an id names
one note without naming its file.

Limits, enforced when a note is added (refused with a clear error, never
truncated silently): summary 500 runes, rationale 4000 runes, 50 notes per
file.

### Lifetime

- Adding the first note sets `d.backgrounded = true`, so `escDoc` already
  sends the file to the background instead of closing it (ruling 2).
- `closeDoc` drops the document and with it the notes. Nothing else to clean.
- `openFilesReg.touch` skips a document with notes when it looks for one to
  evict (ruling 7). When every candidate carries notes or is on screen,
  nothing is evicted and the list grows past 20.
- Dismissing or removing the last note leaves the file open and
  `backgrounded` as it is.

### Re-anchoring on reload

In `openFile.fill`, when a reload lands on a document that has notes and
both the old and the new content are real file lines (`docLoaded`):

1. Line-align the old lines against the new ones with `internal/textdiff`.
2. For each note, map `start` and `end` through the alignment. When every
   line of the range maps to a line of the new file and the mapped lines are
   contiguous, the note moves to the mapped range, `anchor` is refreshed and
   `outdated` is cleared.
3. Otherwise the note keeps its old numbers, clamped to the new line count,
   and `outdated` is set. An outdated note is re-tried on every later reload
   against its kept `anchor` text (a revert brings it back).

The alignment runs once per reload of an annotated file, never for a file
without notes. A placeholder fill (load failed, deleted on disk, too large)
leaves the notes untouched and draws none; they come back with the next
real fill.

## Rendering

The preview is a pager: `p.sel` is the top visible LINE, `p.cur` the cursor
LINE, both indexes into `p.lines`. Note rows are **virtual**: they are never
put into `p.lines`, so every line index (cursor, selection, search hits,
`pendingLine`, the keep-place) stays a file line number.

`renderPreviewBox` builds its window by walking file lines from `p.sel`:
after the line a note ENDS on, it emits the note's box rows, until the row
cap is filled. The box is the diff view's note box (the frame and body
layout of `diff_notes.go`, factored so both call one builder), so a note
looks the same in both surfaces:

```
        for _, d := range m.openFiles {
            if d.tag == tag {
    ┌ agent · t7 · lines 119-120 ──────────────┐
    │ The tag is compared before the load      │
    │ lands, so a reused file matches twice.   │
    └──────────────────────────────────────────┘
                return d, rows, innerW, true
```

- The title reads `<author> · <id> · line N` / `lines N-M`, plus ` · outdated`
  for an outdated note. Summary then rationale, laid out by the diff box's
  own body builder. Text wraps to the box; it is never cut.
- While a file has notes its lines give up a 2-column gutter on the left;
  the lines a note covers carry `│ ` there, so the range is visible without
  reading the title. A file without notes has no gutter.
- Note rows take no cursor, no selection stripe and no search hit.
- `previewClamp`, `ensureCursorVisible` and the page step count display rows
  through one helper (`previewRowsBetween(p, from, to)`) that adds the note
  rows of the lines in between; without notes it is the plain difference, so
  a file without notes behaves exactly as today.
- The switcher's open-file rows show the count in plain text after the
  path (`3 notes`) — no glyph, so the row width stays exact.

## Keys (viewer and the focused right-column preview)

| key | action |
|-----|--------|
| `}` / `{` | next / previous note in this file; past the last (first) one, the next (previous) open file that has notes comes to the front on its first (last) note — the diff view's `}`/`{` rule |
| `d` | dismiss the note the cursor line carries (the first one when several) |
| `r` | copy the reference of that note to the clipboard |

`d` and `r` do nothing on a line without a note. The three are also rows of
the `.` action menu ("Next note", "Dismiss note", "Copy note reference"), and
are advertised in the viewer's hint line, the bottom bar and the help window
only while the shown file has notes. The exact letters are re-checked against
the right-column preview's bindings at plan time; a clash moves the letter,
not the feature.

The copied reference is one line:

```
gg note t7 internal/tui/open_file.go:119-120
```

It is text for the agent, not a command: the skill teaches that `gg note
t<n> <path>:<lines>` in a user's message means "run `gg session note show
t<n>`". The path and lines keep it meaningful after the note is gone.

All new strings go through `i18n.T` with keys in all four bundles.

## Agent surface

### Steer protocol (`internal/steer`)

Four new `Command.Cmd` values, answered by the TUI itself like `files`:
`note_add`, `note_list`, `note_show`, `note_rm`.

- New `Command` fields: `NoteID string`, `Summary string`, `Rationale
  string`, `Author string`. `File`/`FileID` name the file, `Start`/`End` the
  range (both already exist).
- New `Reply.Notes []FileNote`:

```go
type FileNote struct {
	ID        string `json:"id"`                 // "t<n>"
	FileID    string `json:"file_id"`            // "f<n>"
	Path      string `json:"path"`
	Start     int    `json:"start"`
	End       int    `json:"end"`
	Summary   string `json:"summary"`
	Rationale string `json:"rationale,omitempty"`
	Author    string `json:"author,omitempty"`
	Outdated  bool   `json:"outdated,omitempty"`
	Text      []string `json:"text,omitempty"`   // note_show only: the lines it sits on now
}
```

- `steer.OpenFile` gains `Notes int` (the count), so `gg session files` and
  the snapshot's `open_files` show which files are annotated.

TUI handlers live in a new `internal/tui/steer_file_notes.go`:

- `note_add`: the worktree check of `steerNavigateBackground` (another
  worktree is refused, never asked about). The file is found in the open
  list, or opened in the background first — then the note is added when the
  load lands (it rides `contentLandedMsg`, as the background open's reply
  does), because a range can only be checked against loaded lines. Refused:
  not a working-tree file, a placeholder document, `start < 1`, `end <
  start`, `end` past the last line, a limit exceeded. The reply's `Detail`
  is `noted <path>:<start>[-<end>] as t<n>` and `Notes` holds the new note.
  Never moves the screen (bypasses `steerRefusal`, like `files`).
- `note_list`: all notes of the current worktree's open files, or of one
  file (`File`/`FileID`). Read-only.
- `note_show`: one note by id, with `Text`. `no note t<n>` when it is gone.
- `note_rm`: by `NoteID` one note; by `File`/`FileID` with no `NoteID`
  every note of that file. Replies with the count removed.

### CLI (`internal/cli/session_note.go`)

```
gg session note add <path>:<start>[-<end>] --summary "…" [--rationale "…"] [--author <name>] [--json]
gg session note list [<path>|<file-id>] [--json]
gg session note show <note-id> [--json]
gg session note rm <note-id>
gg session note clear <path>|<file-id>
```

- Plain output: `add` prints the reply's detail; `list` prints one line per
  note `<id>\t<path>\t<start>-<end>\t<summary>` (` (outdated)` appended);
  `show` prints the header line, the summary/rationale, then the lines.
- Exit 0 on success; 1 when no gg TUI is live for the worktree, when only
  `gg web` is live (`temporary notes need a gg TUI`), or when the TUI
  refuses; 2 on a usage error.
- `<path>` is repo-relative in git slash form, passed as-is (as
  `gg session files focus` takes it); an open file's id (`f3:10-12`) works
  too.
- `gg note` is unchanged and keeps refusing content links: stored notes and
  temporary notes are different things with different verbs.

The CLI never posts a note command to a `gg web` page (it exits 1 there),
so the web needs no change.

### Skill

`internal/agentskill/using-gg.md` gains the verbs and the walkthrough
("open A, B, C with `--background`; `gg session note add` on each; `gg
session files focus <id>:<line>` one at a time"), the meaning of a pasted
`gg note t<n> …` reference, and the fact that these notes vanish when the
user closes the file. `agentskill.Version` is bumped.

## Errors and edge cases

- The file shrinks under a note: outdated, clamped to the last line.
- The file is deleted on disk: the placeholder shows, notes are kept and
  re-anchored when it comes back.
- A repo/worktree switch: the list is per worktree (existing rule); notes
  stay with their documents and are there again on return.
- Two notes ending on one line: both boxes, in id order.
- A note added to a file the user is reading: the box appears in place; the
  top line and cursor line do not move.
- `gg session note add` from a worktree gg is not showing: refused with the
  existing `gg is showing worktree <a>, not <b>` text.

## Testing

- `open_file_notes_test.go`: add/limits/ordering; re-anchor (insert above,
  delete above, edit inside → outdated, revert → restored, shrink → clamped);
  eviction skips a noted file; esc backgrounds a noted file; X drops notes.
- Render tests over `renderPreviewBox`: box under the end line, wrap, the
  outdated title, a file without notes renders byte-identical to today,
  cursor stays visible with a tall box between top and cursor, wrap mode.
- Key tests: `}`/`{` within a file and across files, `d`, `r` (clipboard
  text), the `.` menu rows, hints only when notes exist.
- Steer tests for the four commands, including add-opens-in-background and
  every refusal.
- CLI tests with the `FakeRunner`/steer fixtures for argv parsing, output
  and exit codes; an e2e scenario driving `gg session note` against a live
  TUI if the harness allows it, else a headless `tui-capture.sh` check.
- i18n gate tests pass with the new keys in ja/ko/zh/ru.

## Docs

`CHANGELOG.md`, `README.md` (agent verbs + the viewer keys),
`internal/agentskill/using-gg.md` (+ version bump, `gg init --update`),
`docs/CLAUDE-details.md` (the note layer, the virtual-row rule). `CLAUDE.md`
needs no change: no new package, no new convention.
