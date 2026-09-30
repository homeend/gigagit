# Agent notes and overviews in gg web

Status: agreed in brainstorming 2026-10-01. Two plans: plan 1 = the shared
store + notes on the web, plan 2 = overviews on the web.

## Goal

An agent tells the user things with two TUI-only features today:

- **open-file notes** (`gg session note add|list|show|rm|clear`, spec
  `2026-09-30-open-file-notes-design.md`): temporary remarks under lines of
  open working-tree files;
- **overview documents** (`gg session overview add|set|list|show|rm`, spec
  `2026-09-30-agent-overview-documents-design.md`): an in-memory markdown
  tour whose links are anchors to files, lines, ranges and notes.

Both were ruled "TUI only now, web later". This feature brings them to the
gg web page, so what the agent shows in the terminal also works in the
browser — and, when the TUI hosts the page, is the **same** data in both.

## Rulings (user, 2026-10-01 — do not re-ask)

1. **One shared copy.** When the terminal and the browser are both open on a
   worktree (the TUI hosting the page), the browser shows the SAME notes and
   overviews: same ids; dismissing or closing in one removes it from the
   other. A standalone `gg web` with no TUI keeps its own copy.
2. **Single click** opens an anchor in the browser (browser convention). The
   keyboard stays as in the TUI: tab / shift+tab / enter / backspace.
3. **One spec, two plans**: notes first (overviews' note anchors need them),
   each merged separately, the user asked before each merge.
4. Approach: **one process-global store** (the `domain.Sessions()` pattern),
   not a TUI-owned snapshot, not a merged open-files list.
5. Every ruling of the two TUI specs still holds: memory only, notes on
   working-tree files only, a noted file / an overview is never evicted by
   the 20-file cap, esc backgrounds it and only X closes, anchors are files /
   lines / ranges / notes (never other overviews), a visited file stays open
   after back, back is step by step, overviews are replaceable by id.

## What already exists (not rebuilt here)

- TUI: `openFile` + `openFilesReg`, notes on `openFile.notes`
  (`open_file_notes.go`, `open_file_note_keys.go`, `steer_file_notes.go`),
  overviews (`overview.go`, `overview_keys.go`, `steer_overview.go`).
- Web: the server's own open-files registry (`internal/web/openfiles.go`,
  one list per worktree shared by every tab, ids `f<n>` from its own
  counter), the viewer overlay (`static/viewer.js`), the ctrl+\\ switcher
  (`static/openfiles.js`), `/api/file-content`, the file-stamp poll, the
  steer answers for `files` / background open / `file_focus`
  (`steer_files.go`).
- `internal/markdown`: `ParseWith(src, Options{Anchor})` yields the inline
  kind `anchor`; `static/markdown.js` paints the block tree.
- `web.Host`: the TUI serves the page in-process (`tui/webhost.go`), so the
  page and the terminal share one process — the reason one store can serve
  both.

## Architecture

```
  TUI Model ──reads copies / writes──┐        ┌── web Server (hosted or standalone)
                                      ▼        ▼
                     internal/agentdocs  (Shared(): one per process)
                     notes per (root, path) · aligned content per path
                     overviews per root · f<n>/t<n> counters · Broadcaster
```

The store is a DAG leaf: stdlib + `internal/markdown` + `internal/steer`
(wire types only). The TUI and the web
import it directly (as they import `steer`); it holds no git state, so it is
not reached through `domain`. It lives outside every `domain.Service`, so it
survives the TUI's re-root. Nothing is passed through the `WebHost` seam: a
hosted page and its TUI find the same `agentdocs.Shared()`.

### Routing (unchanged protocol)

`gg session note …` and `gg session overview …` keep using
`steerLive(both=false)`: the TUI answers whenever one is live (and writes
the store, where a hosted page sees it); with no TUI, a live `gg web` answers
from its own store. The two refusals "temporary notes need a gg TUI" /
"overviews need a gg TUI" go away. A hosted page therefore never receives
these verbs — no double add.

**Known limit (written down, accepted with ruling 1):** a TUI plus a
*separate-process* `gg web` on the same worktree — the verbs go to the TUI,
and that page shows nothing of them.

## The store — `internal/agentdocs`

### Roots

Everything is filed under a worktree root. The store normalises a root with
the rule `domain.SamePath` uses (`filepath.Clean`, slash form, case-folded
where the filesystem is case-insensitive) — copied, because a leaf cannot
import `domain`; a test in `internal/tui` asserts the two agree on a table of
inputs. Paths are repo-relative, slash form.

### Ids

- `NextFileSeq() int64` — ONE process counter for open-file ids. The TUI's
  `openFileSeq` and the web registry's `r.seq` are deleted; both lists draw
  from it. An overview's `f<n>` is allocated once by the store and used by
  both lists, so it can never collide with a plain file's id in either.
  Plain files still get a different `f<n>` in each list (true today; the
  agent sees the TUI's answer).
- Note ids `t<n>` come from the store's counter (the TUI's `fileNoteSeq` is
  deleted).

### Notes

```go
type Note struct {
    ID        string   // "t<n>"
    Seq       int64
    Root      string   // normalised
    Path      string
    Start     int      // 1-based, as the lines sit NOW
    End       int
    Anchor    []string // the lines' text when placed / last re-anchored
    Summary   string
    Rationale string
    Author    string
    Outdated  bool
}

func (s *Store) AddNote(root, path string, lines []string, start, end int,
    summary, rationale, author string) (Note, error)
func (s *Store) Align(root, path string, lines []string) (changed bool)
func (s *Store) Notes(root, path string) []Note   // copies, reading order
func (s *Store) NotedPaths(root string) []string
func (s *Store) FindNote(id string) (Note, bool)
func (s *Store) RemoveNote(id string) bool
func (s *Store) ClearPath(root, path string) int
func NoteReference(n Note) string                 // "gg note t7 a/b.go:12-14"
```

- `AddNote` first aligns to `lines` (so a note is always placed against the
  content the caller just read), then applies today's checks and limits and
  returns today's English errors (`maxFileNoteSummary` 500,
  `maxFileNoteRationale` 4000, the range checks).
- **Align is idempotent.** The store keeps, per (root, path) that has notes,
  the content it last aligned against. `Align` with identical content is a
  no-op; different content runs today's `reanchorNotes` rule (follow the
  line alignment; outdated when lines are gone; come back only at the old
  place or a single exact match) with the kept content as `old`. When the
  last note of a path goes, its kept content is dropped. The TUI and the web
  both call `Align` after every read of a noted working-tree file; whichever
  comes second with the same bytes changes nothing.
- Moved out of the TUI with their tests: `addNote` checks, `reanchorNotes`,
  `sameLineMap`, `mapRange`, `relocate`, `sortNotes`, `reference`.

### Overviews

```go
type Anchor struct {
    Dest    string // as written
    Path    string // "" for a note anchor
    Start   int    // 0 = the whole file
    End     int
    Note    string // "t<n>" for a note anchor
    Missing bool   // file or note not found when last checked
}

type Overview struct {
    ID      string // "f<n>"
    Seq     int64
    Root    string
    Title   string
    Text    string
    Anchors []Anchor // document order, at most 100
}

func ParseAnchorDest(dest string) (Anchor, bool)
func ParseOverview(text string) []Anchor          // via markdown.ParseWith
func (s *Store) AddOverview(root, title, text string) (Overview, error)
func (s *Store) SetOverview(id, title, text string) (Overview, error)
func (s *Store) RemoveOverview(id string) bool
func (s *Store) Overview(id string) (Overview, bool)
func (s *Store) Overviews(root string) []Overview
func (s *Store) CheckAnchors(id string) (changed bool) // stats; call off the UI thread
func AnchorReference(o Overview, a Anchor) string    // `gg overview f7 "<title>" → <dest>`
```

- Limits and errors as today: text ≤ 64 KiB, ≤ 20 overviews per root, title
  ≤ 200 runes and one line, anchors past 100 stay plain text.
- `CheckAnchors`: a file anchor is missing when `root/path` is not a regular
  file; a note anchor when `FindNote` fails. It signals only when a
  `Missing` flag changed.
- Moved out of the TUI: `parseAnchorDest`, the anchor extraction half of
  `overviewLines` (the layout and span recovery stay in the TUI).

### Change signal

`Subscribe() (<-chan struct{}, func())` / internal `signal()` — a copy of
`agentsession.Broadcaster` (per-subscriber buffered-1 channel, never
blocks, never a shared channel). Every mutation that changes state signals
once. Subscribers re-read what they show.

### Reply prose

Every steer reply that describes a note or an overview is built from store
data by helpers in the store, so a TUI and a standalone `gg web` answer word
for word alike: `NoteWire(n Note, fileID string, text []string)
steer.FileNote`, `OverviewWire(o Overview, state string, withText bool)
steer.Overview`, and the fixed reply sentences (errors, "removed t7", …).
`agentdocs` may therefore import `steer` (itself a leaf: stdlib +
fsnotify). Only the parts about each side's own open-files list differ:
"opened … in the background", "; closed <path> (20 files open)".

## TUI changes (plan 1 for notes, plan 2 for overviews)

- **Copies for drawing.** An `openFile` keeps `notes []agentdocs.Note` (a
  copy) and an overview keeps its `agentdocs.Overview` copy plus its own
  `sel`, `w`, spans. View never reads the store — a browser change cannot
  shift the rows mid-frame.
- **Subscription.** On start the TUI subscribes; `waitAgentDocsCmd` turns a
  wake into `agentDocsChangedMsg`, re-armed on every delivery (the console /
  `domain.Sessions()` shape). The handler re-reads the copies of the
  current worktree's open files, recounts note rows (`syncNoteRows`),
  re-lays out an overview whose text changed, and applies removals (below).
- **Writes.** Its own actions write the store and refresh the copy in the
  same Update (no wait for the signal): `d` → `RemoveNote`; X on a noted file
  → `ClearPath`; X on an overview → `RemoveOverview`; the `note_*` /
  `overview_*` steer verbs as today.
- **Align.** The `fileContentMsg` fill for a working-tree document with notes
  calls `Align(root, path, lines)` instead of `reanchorNotes(old, cur)`.
- **Removed from the browser:**
  - a note dismissed there leaves on the next change message; the cursor
    stays;
  - a noted file closed there (x in the switcher) loses its notes; the file
    stays open in the terminal;
  - an overview closed there: a background one leaves the list; the one on
    screen closes as X would, and the title line says
    `overview f7 was closed in the browser`; a later backspace from an anchor
    says `the overview was closed` (today's text).
- **Ids.** `openFileSeq` and `fileNoteSeq` are deleted (see Ids).
- New strings go through `i18n.T` in all four bundles.

## Web — notes (plan 1)

### Server

- **Store follow.** `Host.Start` / `Serve` subscribes to the store. On each
  signal, for the served root: every path in `NotedPaths` without an entry
  in the page's list is opened there in the background (the TUI's
  note_add rule); then the tabs get an SSE `liveMsg{Reason: "agentdocs"}`.
- **`/api/file-content`** for a worktree source: after the read, `Align`,
  and the response gains `notes: [{id, start, end, summary, rationale,
  author, outdated, ref}]` (`ref` = `NoteReference`). Commit and shelf
  versions never carry notes; the F finder's preview ignores the field.
- **`GET /api/file-notes?path=`** — the current notes of a working-tree
  path (after a dismiss in the terminal the page re-fetches notes without
  re-reading the file).
- **`POST /api/file-notes {op:"dismiss", id}`** (write guard) —
  `RemoveNote`; 404-style refusal text `no note <id>` when gone.
- **Closing.** esc on a file with notes BACKGROUNDS it (today web esc closes
  unless another tab shows it) — TUI parity. The switcher's x (close
  everywhere) calls `ClearPath`. A plain close by esc of an un-noted file
  is unchanged.
- **Eviction.** `frontLocked` never evicts an entry with notes.
- **`gg session files`** from the web fills `Notes` (the count).
- **Steer verbs with no TUI.** `POST /api/session/steer` answers
  `note_add|note_list|note_show|note_rm` itself (200 + `steer.Reply`, like
  `files`): `note_add` refuses another worktree (today's text), opens the
  file in the background when needed (`WorktreeFilesPresent` check, as
  `steerBackground`), reads it, `AddNote`; the reply text is the TUI's.
  These verbs skip the op-in-flight 409 (they never move the screen).

### Page

- **Box.** Under the last line of a note's range, a box: title
  `note t7 · <author> · lines 12–14` (+ ` · outdated`, and the box faint),
  then the summary and the rationale as plain pre-wrapped text. No height
  cap (the page scrolls). Buttons `dismiss` and `copy reference`.
- **Gutter.** Lines covered by a note get an accent bar left of the line
  number.
- **Keys** (viewer, cursor on a noted line): `d` dismiss, `r` copy the
  reference; `}` / `{` next / previous note, then on to the next noted open
  file in the TUI's order (notes by start line; files by note seq).
- **Switcher.** A noted file's row shows `· N notes`.
- **Help / footer.** The `?` help lists `d r } {`; the viewer's footer chips
  show them while the file has notes.
- **Refresh.** On an `agentdocs` live message the page re-fetches the shown
  file's notes and the open-files list.

## Web — overviews (plan 2)

### Server

- **Store follow.** On each signal: an overview of the served root not in
  the page's list joins it in the background under the store's `f<n>` (key
  `{Src:"overview", Path:"overview-<n>.md"}`); an entry whose overview left
  the store is removed, and tabs showing it get a live message that closes
  their viewer with `overview f7 was closed`.
- **List rules.** Overview entries are never evicted, skipped by the stat
  poll and the file-stamp check, and listed by `gg session files` as
  `source: overview` with `title`.
- **`GET /api/overview?id=`** → `{id, title, text, blocks, anchors}`:
  `blocks` = `markdown.ParseWith(text, Options{Anchor: accept})` with the
  store's rule (so anchors arrive as kind `anchor`, in document order);
  `anchors[k]` = `{dest, path, start, end, note, missing, ref}`. The
  request runs `CheckAnchors`; a change signals and the page repaints.
- **Steer verbs with no TUI.** `overview_add|set|list|show|rm` answered by
  the server with the TUI's reply text. `add` shows it in every open tab (a
  steer emit, as `file_focus`) → `showing f7`, `; no gg web tab is open to
  show it` when none; `add --background` only lists it; `set` replaces
  title/text (tabs showing it repaint); `rm` removes it everywhere. An add
  that pushes a plain file out of the list appends today's `; closed <path>
  (20 files open)`.
- **Hosted page.** An overview added through the TUI appears on the
  terminal's screen and joins the browser's list in the background; it does
  not jump onto the browser's screen.

### Page

- **Document mode.** An overview opens in the viewer overlay; the body is
  `mdHTML(blocks, {anchors: true})`; the title is the overview's title.
- **Anchors.** `markdown.js` gains an `anchors` option: kind `anchor` →
  `<a class="md-anchor" data-a="k" href="#">` numbered in document order
  (the server's order). Without the option an anchor paints as plain text,
  so PR / review rendering is unchanged.
- **Styles.** Selected: reverse + bold. Missing: faint + line-through.
- **Keys.** tab / shift+tab select (wrapping), enter or a single click opens,
  `r` copies the selected anchor's reference, `y` copies the overview's
  text, esc backgrounds; x in the switcher closes everywhere.
- **Opening** (a file stays in the list afterwards, ruling 5):
  file → the viewer at its top; line → at that line; range → at its first
  line with lines start..end highlighted (new viewer state `view.range`,
  cleared when the viewer shows another file); note → the note's file at the
  note's first line with its box in view.
- **Missing.** enter / click on a missing anchor → status `no file <path>`
  or `note <id> is gone`; nothing opens.
- **Back.** backspace in a file opened from an anchor returns to the
  overview with that anchor selected. One step deep, per tab (page state,
  not server state). When the overview was closed meanwhile: status
  `the overview was closed` and the way back is dropped.
- **Switcher.** Overview rows show the title and `overview · N anchors`.
- **Help / footer** cover the keys.

## Errors and edge cases

| Case | Behaviour |
|------|-----------|
| note_add on a file not in the working tree (web) | `<path> is not in the working tree` |
| note_add, another worktree (web) | `gg web is showing worktree <a>, not <b>` |
| dismiss of a note already gone (page) | refusal `no note t7`; the page re-fetches |
| file deleted on disk with notes | as TUI: placeholder, notes kept, outdated on return if moved |
| two tabs, one dismisses | both repaint on the live message |
| overview closed in the terminal while a tab shows it | that tab's viewer closes with `overview f7 was closed` |
| TUI + separate `gg web` process | verbs go to the TUI; the page shows none (known limit) |
| re-root (TUI switches repo) | the store is keyed by root; the hosted page follows the new root and shows that root's notes/overviews |

## Testing

- `internal/agentdocs`: the moved TUI tests (alignment, relocation, limits,
  anchor grammar) + new ones: `Align` idempotence, `ClearPath`, removal
  signals, `NextFileSeq` shared, `CheckAnchors` changes, root
  normalisation agreeing with `domain.SamePath` (the table test lives in
  `internal/tui`, which may import both).
- TUI: existing notes / overview tests stay green; new tests drive the store
  from outside (dismiss, clear path, remove overview in the background and
  on screen) and assert the repaint.
- Web Go tests: the endpoints, the steer answers, the store-follow (background
  open, overview add/remove), eviction exemption, esc-backgrounds a noted
  file.
- Node tests: note box title / order, anchor painting with and without the
  option, anchor stepping, the back stack.
- e2e: a note and an overview added with only a web page live
  (`[input] web = true`).
- Browser check (visibility asserted, run against the unfixed build first):
  a note added through the TUI shows in the hosted page; a dismiss in either
  removes it from both; overview tab / open / backspace / closed.
- Race gate `./test.sh race` → "all green" before each merge.

## Docs (each plan)

CHANGELOG, README, `docs/CLAUDE-details.md` (the store, routing, the known
limit), `internal/agentskill/using-gg.md` (bump past v107: the web answers
the note / overview verbs too), CLAUDE.md package-map row for `agentdocs`,
the archtest leaf rule for `agentdocs` (stdlib + `markdown` + `steer`
only).

## Plans

1. **Plan 1 — store + notes on the web.** `agentdocs` (ids, notes, signal),
   TUI onto it for notes and ids, CLI refusal removed for notes, web server +
   page for notes, docs.
2. **Plan 2 — overviews on the web.** Overviews into `agentdocs`, TUI
   overview onto it, CLI refusal removed for overviews, web server + page
   for overviews, docs.

## Out of scope

- Mirroring the two open-files lists (plain files keep separate ids per
  list).
- A separate-process `gg web` seeing a TUI's notes.
- Persisting notes or overviews.
