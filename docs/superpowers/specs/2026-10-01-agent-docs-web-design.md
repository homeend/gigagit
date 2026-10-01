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
                     internal/agentdocs  (*Store; Shared() at composition)
                     notes per (root, path) · aligned content per path
                     overviews per root · f<n>/t<n> counters · signal
```

The store is a DAG leaf: stdlib + `internal/steer` (wire types only) +
`internal/textdiff` (the alignment engine) + `internal/markdown` (plan 2)
and their closures (`markdown` pulls `syntax`/chroma). The
TUI and the web import it directly (as they import `steer`); it holds no git
state, so it is not reached through `domain`. It lives outside every
`domain.Service`, so it survives the TUI's re-root.

**Injected, not global.** The store is a VALUE the frontends hold: a
`docs *agentdocs.Store` field on `web.Server` and on `tui.Model` (a pointer
field, so it survives the value copy). `agentdocs.Shared()` — the one store
per process — is read only at the composition points: the TUI's
`NewModel`/`New` path, `web.Serve`, and `web.NewHost` (which the TUI's
`NewWebHost` calls — so a hosted page and its TUI hold the same `Shared()`
without any change to the `WebHost` seam). Tests build `agentdocs.New()`
per test: ids stay deterministic (`f1`, `t1`) under `t.Parallel()`, and the
20-overview cap is never shared between tests. A test that wants the hosted
case hands the same `New()` store to a Model and a Server.

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

Everything is filed under a worktree root. The store does NOT normalise: the
callers pass `domain.CheckoutKey(root)` — a new export of the key
`domain.SameCheckout` already compares (`linkPathKey(filepath.Clean(p))`,
`internal/domain/linkresolve.go`), so there is one rule and no copy. Paths
are repo-relative, slash form.

### Ids

- `(*Store).NextFileSeq() int64` — the store's ONE counter for open-file
  ids. The TUI's `openFileSeq` (`open_file.go`) and the web registry's
  `r.seq` (`openfiles.go`) are deleted; both lists draw from their store.
  With the shared store an overview's `f<n>` is allocated once and used by
  both lists, so it can never collide with a plain file's id in either.
  Plain files still get a different `f<n>` in each list (true today; the
  agent sees the TUI's answer). The TUI's `newOpenFile` takes the seq from
  the store; a new `newOverviewDocWith(o agentdocs.Overview)` builds the
  overview document with the store's seq.
- Note ids `t<n>` come from the store's own note counter (the TUI's
  `fileNoteSeq` is deleted).

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

type Fingerprint [32]byte                         // sha256 of the lines joined by "\n"
func Print(lines []string) Fingerprint

func (s *Store) AddNote(root, path string, lines []string, start, end int,
    summary, rationale, author string) (Note, error)
func (s *Store) Align(root, path string, lines []string) (changed bool)
func (s *Store) Notes(root, path string) ([]Note, Fingerprint) // copies, reading order
func (s *Store) NotedPaths(root string) []string
func (s *Store) FindNote(id string) (Note, bool)
func (s *Store) RemoveNote(id string) bool
func (s *Store) ClearPath(root, path string) int
func NoteReference(n Note) string                 // "gg note t7 a/b.go:12-14"
```

- `AddNote` first aligns to `lines` (so a note is always placed against the
  content the caller just read), then applies today's checks and limits and
  returns today's English errors (`maxFileNotes` 50 per file,
  `maxFileNoteSummary` 500, `maxFileNoteRationale` 4000, the range checks).
  The source check (`temporary notes go on working-tree files only`) stays
  with the callers — the store never sees a source.
- **Align is idempotent.** The store keeps, per (root, path) that has notes,
  the content it last aligned against and its fingerprint. `Align` with the
  same fingerprint is a no-op; different content runs today's
  `reanchorNotes` rule (follow the line alignment; outdated when lines are
  gone; come back only at the old place or a single exact match) with the
  kept content as `old`. When the last note of a path goes, its kept content
  is dropped. `Align` is called ONLY right after a fresh disk read of a
  noted working-tree file (TUI fill, web `/api/file-content`, the web
  note_add), never with a copy a side merely has on screen.
- **A side adopts notes only for the content it shows.** `Notes` returns the
  fingerprint the notes are aligned to. A side whose loaded lines have a
  different fingerprint keeps its previous copy and reloads the file (TUI:
  the doc re-read path `docCurrent` already uses; web: the page re-fetches
  `/api/file-content`), whose read then calls `Align`. Two reads racing an
  edit can align out of order (old after new); the side that then sees a
  mismatch re-reads and the store converges on the newest content.
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
  ≤ 200 runes and one line (`steer_overview.go` constants), anchors past 100
  stay plain text (`overviewMaxAnchors`, today in `md_render.go`). All move
  into the store.
- `CheckAnchors`: a file anchor is missing when `root/path` is not a regular
  file; a note anchor when `FindNote` fails OR the note belongs to another
  root. It signals only when a `Missing` flag changed.
- Moved out of the TUI: `parseAnchorDest`, the anchor extraction half of
  `overviewLines` (the layout and span recovery stay in the TUI).

### Change signal

`Subscribe() (<-chan struct{}, func())` / internal `signal()` — a copy of
`agentsession.Broadcaster` (per-subscriber buffered-1 channel, never
blocks, never a shared channel; ~40 lines, copied rather than shared:
`agentsession` is a PTY package frontends may not import). Every mutation
that changes state signals once, after releasing the store's lock. The
channel coalesces, so a subscriber re-reads EVERYTHING it shows, never "the
one change".

**Concurrency.** The store guards all state with one mutex and hands out
copies only (slices copied, never aliased). The TUI touches it from Update
(and from the off-thread `CheckAnchors` cmd); the web from HTTP handlers and
the follow goroutine. Nothing calls back into a frontend while holding the
lock.

### Reply prose

Every steer reply that describes a note or an overview is built from store
data by helpers in the store, so a TUI and a standalone `gg web` answer word
for word alike: `NoteWire(n Note, fileID string, text []string)
steer.FileNote`, `OverviewWire(o Overview, state string, withText bool)
steer.Overview`, and the fixed reply sentences — every one the TUI sends
today, including: `removed t7`, `removed N notes from <path>`, `no open
file <name>`, `<path> was closed before the note landed`, `closed f7`, `set
f7`, `showing f7`, `added f7 in the background (<why>)`, `the overview was
closed before its anchors were checked`, and the limit errors. The plan
lists them from `steer_file_notes.go` / `steer_overview.go` and moves each
into a store helper with a test. `agentdocs` may therefore import `steer`
(itself a leaf: stdlib + fsnotify; `TestSteerIsAStdlibLeaf` is untouched).
Only the parts about each side's own open-files list differ: "opened … in
the background", "; closed <path> (20 files open)", and the other-worktree
refusal, which keeps each side's existing words (`gg is showing worktree …`
in the TUI, `gg web is showing worktree …` on the web, as `steer_files.go`
already says).

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
  When a change message brings notes aligned to a different fingerprint
  than the doc's loaded lines, the TUI keeps its copy and re-reads the doc
  (see "A side adopts notes only for the content it shows").
- **Per-worktree.** The TUI's open-files list is already per worktree
  (`openFilesReg.byWT`); the change handler only refreshes the current
  worktree's documents, and a re-root refreshes the new worktree's from the
  store.
- **TUI-only state stays in the TUI**: an overview's `sel`, `w`, spans; an
  anchor-opened file's `from` (the way back) and `pendingEnd` (the range
  selection). None of it goes to the store or the browser.
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

- **Store follow.** A server-level goroutine, started in `Host.Start` (which
  `Serve` uses) and ended by `Close` through the host's context, subscribes
  to the store and runs one **follow pass** at start, on every signal, and
  from `adoptService` (`reroot.go`, shared by the hosted `Reroot` and the
  standalone re-root) right after the new service is adopted. A pass reads
  the served root live (`CheckoutKey(s.service().Root())`) and, for that
  root: opens every path in `NotedPaths` that has no entry in the page's
  list in the background (the TUI's note_add rule) and marks noted entries
  `pinned`, clearing `pinned` on entries whose notes are gone (plan 2 adds
  the overview half). Then it tells the tabs with a live message
  `Reason: "agentdocs"` sent through the hub's `fanOut`, never `emit`
  (`emit` is dropped while an op is in flight, `live.go`).
- **`/api/file-content`** for a worktree source: after the read, `Align`,
  and the response gains `notes: [{id, start, end, summary, rationale,
  author, outdated, ref}]` (`ref` = `NoteReference`). Commit and shelf
  versions never carry notes; the F finder's preview ignores the field.
- **No separate notes GET.** On an `agentdocs` live message the page
  re-reads `/api/file-content` (which aligns and returns lines + notes in
  one answer), so notes are never paired with lines from another read.
- **`POST /api/file-notes {op:"dismiss", id}`** (write guard) —
  `RemoveNote`; 404-style refusal text `no note <id>` when gone.
- **Closing — decided by the server.** The page's esc and backdrop click
  send `op:"close"` (`viewer.js`); the page may not know a note arrived
  after it loaded. So the `close` op in `openfiles_http.go` decides: a
  non-`everywhere` close of a `pinned` entry (notes, plan 2: an overview)
  BACKGROUNDS it instead — TUI parity (esc backgrounds, only X closes); an
  `everywhere` close (the switcher's x) of a noted entry calls `ClearPath`
  (plan 2: `RemoveOverview`) and removes it. An un-pinned close is
  unchanged.
- **Eviction.** `ofEntry` gains `pinned bool`, kept by the follow pass and
  the note handlers; `frontLocked` never evicts a pinned entry. When nothing
  is evictable the list grows past 20 (the notes spec's rule).
- **`gg session files`** from the web fills `Notes` (the count).
- **Steer verbs with no TUI.** `POST /api/session/steer` answers
  `note_add|note_list|note_show|note_rm` itself (200 + `steer.Reply`, like
  `files`). In `handleSteer` they are dispatched on `Cmd` BEFORE
  `toSteerWire` (whose `default` answers 400 `unknown command`) and BEFORE
  the `c.Background` branch — the existing `files` branch is the model.
  - `note_add` refuses another worktree (`gg web is showing worktree …`),
    resolves `FileID` through `ofs.resolve` or the path, refuses a path not
    in the working tree (`WorktreeFilesPresent`, as `steerBackground`),
    opens it in the background when needed, reads it, `Align`, `AddNote`.
  - `note_list` / `note_show` / `note_rm` with an id as the TUI.
  - **`note clear`** (`note_rm` carrying `File` or `FileID`, CLI
    `session_note.go`) → `ClearPath`; replies `removed N notes from <path>`
    / `no open file <name>` as the TUI.
  - Every reply text is the TUI's (store helpers), except the
    other-worktree refusal (above).
  - These verbs skip the op-in-flight 409 (they never move the screen).

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

- **Store follow** (the plan-1 pass gains this half): an overview of the
  served root not in the page's list joins it in the background, `pinned`,
  under the store's `f<n>` (key `{Src:"overview", Path:"overview-<n>.md"}`,
  the TUI's display name); an entry whose overview left the store is
  removed, and tabs showing it get a live message that closes their viewer
  with `overview f7 was closed`.
- **List rules.** Overview entries are pinned (never evicted; esc
  backgrounds; x closes everywhere → `RemoveOverview`), skipped by the stat
  poll and the file-stamp check, and listed by `gg session files` as
  `source: overview` with `title`.
- **`GET /api/overview?id=`** → `{id, title, text, blocks, anchors}`:
  `blocks` = `markdown.ParseWith(text, Options{Anchor: accept})` with the
  store's rule (so anchors arrive as kind `anchor`, in document order);
  `anchors[k]` = `{dest, path, start, end, note, missing, ref}`. The
  request runs `CheckAnchors`; a change signals and the page repaints.
- **Steer verbs with no TUI.** `overview_add|set|list|show|rm` answered by
  the server with the TUI's reply text, dispatched on `Cmd` before
  `toSteerWire` and the `c.Background` branch (CLI `session_overview.go`
  sets `Background` on `add --background`; the generic branch would take it
  for a file open). `add` shows it in every open tab (a steer emit, as
  `file_focus`) → `showing f7`, `; no gg web tab is open to show it` when
  none; `add --background` only lists it; `set` replaces title/text (tabs
  showing it repaint); `rm` removes it everywhere. An add that pushes a plain
  file out of the list appends today's `; closed <path> (20 files open)`.
- **The op-in-flight 409.** `overview_add` without `--background` moves the
  screen, so it sits behind the 409 like `file_focus`; an add the gate
  refuses falls back to the background with the TUI's `added f7 in the
  background (<why>)`. `--background`, `set`, `list`, `show`, `rm` skip the
  gate.
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
| re-root (TUI switches repo) | the store is keyed by root; `adoptService` runs a follow pass, so the hosted page shows the new root's notes/overviews; the old root's stay in the store (the TUI's list for that worktree still holds them) |
| esc on a file that got a note after the page loaded it | the server's close op sees `pinned` and backgrounds it |
| a note added after the page read the content | the follow pass's live message makes the page re-fetch notes; their fingerprint differs from the page's content only if the file changed, and then the page re-fetches the content |
| more than 20 pinned entries | nothing is evictable; the list grows past 20 |

## Testing

- Every test builds its own `agentdocs.New()`; no test touches `Shared()`
  except one asserting the composition points use it.
- `internal/agentdocs`: the moved TUI tests (alignment, relocation, limits,
  anchor grammar) + new ones: `Align` idempotence and fingerprints,
  out-of-order aligns converging, `ClearPath`, removal signals, one
  `NextFileSeq` across two consumers, `CheckAnchors` changes (incl. a note
  of another root), the reply-sentence helpers, `-race` with concurrent
  writers and readers.
- `domain.CheckoutKey`: agrees with `SameCheckout` on a table.
- TUI: existing notes / overview tests stay green (ids still `f1`/`t1` with a
  fresh store); new tests drive the store from outside (dismiss, clear path,
  remove overview in the background and on screen, a fingerprint mismatch
  triggering a re-read) and assert the repaint.
- Web Go tests: existing open-files tests keep their literal `f1` ids (fresh
  store per server); the endpoints; the steer answers incl. `note clear`,
  dispatch before `toSteerWire` / the background branch, the 409 rules; the
  follow pass at start, on signal and on `adoptService`; `pinned` eviction
  exemption; the close op backgrounding a pinned entry and `everywhere`
  clearing it; one store shared by a Model and a Server (the hosted case).
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
the archtest leaf rule for `agentdocs` (gigagit imports limited to
`markdown`, `steer` and their closures — `syntax`; no `domain`, `git`,
frontends).

## Plans

1. **Plan 1 — store + notes on the web.** `agentdocs` (ids, notes, signal,
   reply helpers), `domain.CheckoutKey`, store injection at the composition
   points, TUI onto it for notes and ids, CLI refusal removed for notes, web
   follow pass + `pinned` + close op + endpoints + steer verbs, the page,
   docs.
2. **Plan 2 — overviews on the web.** Overviews into `agentdocs`, TUI
   overview onto it, CLI refusal removed for overviews, web server + page
   for overviews, docs.

## Out of scope

- Mirroring the two open-files lists (plain files keep separate ids per
  list).
- A separate-process `gg web` seeing a TUI's notes.
- Persisting notes or overviews.
