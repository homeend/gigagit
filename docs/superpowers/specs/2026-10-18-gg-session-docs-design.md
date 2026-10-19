# gg session docs — files an agent writes, every reader shows

**Status:** draft for review · **Date:** 2026-10-18 · **Audience:** gg (the
TUI, gg web, the CLI, the skills) and a reader that is not gg (the
`gg-notes` IDEA plugin). Builds on the live-steering spec
(`2026-10-15-gg-steer-ide-consumer-design.md`: the inbox path, presence,
the `files` / `file_focus` verbs) and reuses the anchor grammar, markdown
subset and meta/display spelling of the notes-store specs
(`2026-10-15-gg-notes-store-format-design.md`, `…-part2-design.md`). It
does NOT change the notes store.

## 1. Purpose

Today an agent's temporary notes and overviews — what it shows beside the
code while it works — exist only in the memory of ONE running gg (the TUI,
or a gg web page, or the plugin's `SessionMemory`), put there by steer
commands (`note_add`, `overview_add`, …) that only a live window can take.
Consequences the user hit: an overview steered into gg is invisible to an
IDEA started a minute later; closing the window loses the tour; an agent
with no window open has to start `gg web` first; two agents cannot write
without going through the same window; and nothing survives a restart.

This spec replaces that memory with a DIRECTORY OF FILES per worktree. An
agent writes a document straight to disk, with no gg process in the write
path and no coordination with any other writer. Every reader for that
worktree — the TUI, a gg web page, the plugin — watches the directory and
shows what is there. A document stays until the user deletes it in a
reader or the agent removes its own file.

Rulings recorded (2026-10-18, from the design conversation):

- **One format for what an agent shows during work.** The in-memory model,
  its overview and note ids (`f<n>` for an OVERVIEW, `t<n>` for a note)
  and the steer verbs `note_*` / `overview_*` are RETIRED. `f<n>` stays
  what it is for an open FILE (`gg session files`, `file_focus`); an
  overview in that list is named by its document id. `gg session note …` and `gg session overview …` become readers
  and writers of the files. The notes store stays what it is: gg's own,
  gg-coordinated format for permanent overviews, reviews and notes on
  commits ("both formats": store for gg-coordinated writes, files for
  agent writes).
- **No lifetime rule.** Nobody sweeps, nobody tracks liveness, nothing is
  marked stale. Closing a reader never removes a document ("if they close
  the client at any time, it would be safer for notes not to disappear").
  Deletion is the user's (a row action in every reader) or the writer's.
  Earlier candidates — owner liveness, last-reader-exit cleanup, a stale
  marker — were considered and dropped as not worth their complexity.
- **Per worktree**, in the worktree's steer inbox, beside the presence
  files that already define "the readers of this worktree".
- **Async writers.** Any number of agents write at once: unique ids minted
  by the writer, temp file + rename, no lock file.
- **Overviews are markdown files, notes are TOML files.** Easy to write
  with a heredoc, readable by the user, one file per document.

## 2. The directory

```
<state>/gg/sessions/<EncodeRepoKey(common dir)>/steer/<EncodeRepoKey(worktree)>/
    tui.json  web.json  ide.json          presence (steer spec §2)
    cmd-*.json  reply-*.json  ide/        steering (steer spec §2)
    docs/                                 ← THIS SPEC
        20261018T143201-9f3a.md           an overview
        20261018T143205-c0d1.toml         a note
        tour-report-gg-12345-7.md         gg's own: an agent's report tour
```

`<state>` and `EncodeRepoKey` are the steer spec's (§2; vectors in
`docs/specs/gg-steer/repokey-vectors.json`). `gg session status` prints
the inbox; `gg session docs path` (new, §6.2) prints the docs directory.

The directory is FLAT: one level, no sub-directories (fsnotify does not
recurse; one watch covers everything). A writer creates it (`0755`, every
parent) when it is missing. Entries that are not a document — a name that
starts with `.`, ends in `.tmp`, has another extension, or is a directory
— are ignored by every reader (a dot name is a writer's own temp file and
is not even reported as a problem). Nothing else is ever put here.

## 3. Documents

### 3.1 Ids and file names

A document's id IS its file name without the extension: `<id>.md` is an
overview, `<id>.toml` is a note. An id matches `^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`
(no dot, so `a.b.md` cannot be read two ways; no path separator). Ids are
case-sensitive in the protocol; writers SHOULD use lower case because some
file systems fold case.

A writer MINTS its ids. The recommended shape — what gg's CLI writes — is
`<UTC stamp>-<4 hex>`: `20261018T143201-9f3a` (`time.Now().UTC().Format("20060102T150405")`
+ `-` + 4 random hex digits). Two writers never collide in practice; a
writer that wants certainty creates the final name with `O_EXCL` instead
of renaming, and picks another suffix on `EEXIST`. gg's own replace-in-place
documents (the agent tours, §6.5) use fixed names: `tour-brief-<proc>-<n>`,
`tour-report-<proc>-<n>` (the full session id `<proc>/<n>` with every
character outside `[A-Za-z0-9_-]` → `-`).

An id named in an anchor (`note:<id>`), on the CLI, in a `file_focus`
command or in a reader's row is this file name.

### 3.2 Overview — `<id>.md`

```markdown
+++
format = 1
title = 'How scroll coalescing works'
author = 'claude'
created = 2026-10-18T14:32:01Z
+++
Scroll events are coalesced per frame so a burst of wheel ticks costs one
layout.

## How it works
1. [The wheel handler](internal/tui/scroll.go:40-58) pushes the delta …
2. [`flush`](internal/tui/scroll.go:102) runs once per tick and …
3. [why one lock](note:20261018T143205-c0d1) …
```

The file is UTF-8. It starts with a front matter block: a line that is
exactly `+++`, TOML, a line that is exactly `+++`. Everything after that
second line is the body.

| key | type | | |
|---|---|---|---|
| `format` | integer | optional, default `1` | this spec is format 1; a reader that meets a larger value still shows title and body (markdown is markdown) and treats keys it does not know as absent |
| `title` | string | REQUIRED | one line, non-blank after trim, ≤ 200 characters (`MaxOverviewTitle`) |
| `author` | string | optional | default `"agent"`; what a row shows as the writer |
| `created` | RFC 3339 date-time | optional | when missing, a reader uses the file's modification time; used for ordering and the row's age |

Unknown keys are ignored. The body is the overview text:

- the markdown subset of the notes-store spec part 1 (what `internal/markdown`
  parses; a reader paints the tree, never raw HTML);
- ≤ 64 KiB (`MaxOverviewBytes`), measured on the body alone; non-blank;
- its links are the ANCHORS, in the grammar gg already parses
  (`agentdocs.ParseAnchorDest`): a destination that is a slash-form path
  relative to the worktree (`internal/x/parse.go`), a path with a line
  (`:40`) or a range (`:40-60`, 1-based, first ≤ last), or a note
  `note:<id>` (§3.1 id). A destination that is none of these — absolute,
  with `..`, with `\`, a URL — is a plain link, not an anchor, and a reader
  renders it as `label (dest)`. The first 100 anchors count
  (`MaxAnchors`); later links are plain text.

An anchor whose file is not in the worktree, or whose note file is not in
the directory, is UNRESOLVED: a reader shows it so (today's "missing"
mark) and re-checks when the worktree or the directory changes. It never
rejects the document for it.

### 3.3 Note — `<id>.toml`

```toml
format = 1
path = 'internal/tui/scroll.go'
start = 102
end = 104
summary = 'One lock around flush: the wheel handler and the tick both reach it.'
rationale = 'Without it a tick between two deltas …'
author = 'claude'
created = 2026-10-18T14:32:05Z
lines = ['func (s *scroller) flush() {', '	s.mu.Lock()', '	defer s.mu.Unlock()']

[[meta]]
key = 'kind'
value = 'design'

[display]
[display.kind]
show = ['badge']

[display.kind.values]
[display.kind.values.design]
color = 'blue'
```

(gg's encoder writes literal single-quoted strings and the empty parent
headers `[display]` / `[display.<key>.values]`; a writer may use double
quotes and skip the empty headers — any TOML parser reads both. A tab
inside a literal string is a raw tab. `docs/specs/gg-session-docs/examples/`
is the proof of the spelling.)

| key | type | | |
|---|---|---|---|
| `format` | integer | optional, default 1 | as for an overview |
| `path` | string | REQUIRED | the file in the WORKING TREE of this worktree: slash form, relative, no leading `/`, no `.` or `..` segment, no `\`, no `:`; a note is always on the working-tree side (as today's temporary notes) |
| `start`, `end` | integer | REQUIRED | 1-based, `1 ≤ start ≤ end`; the lines as the writer saw them |
| `summary` | string | REQUIRED | non-blank after trim, ≤ 500 characters (`MaxNoteSummary`) |
| `rationale` | string | optional | ≤ 4000 characters (`MaxNoteRationale`); multi-line |
| `author` | string | optional | default `"agent"` |
| `created` | RFC 3339 | optional | as for an overview |
| `lines` | array of strings | REQUIRED | the text of lines `start..end` AS THE WRITER SAW THEM, one string per line, no line terminator (`\r\n` and `\n` stripped), exactly `end - start + 1` entries. This is the note's anchor text: it is what lets a reader that never saw the writer's file place the note (§4.3) |
| `[[meta]]` | array of `{key, value}` tables | optional | the author's free key/values; limits and semantics of the notes-store spec part 2 §6.1 (key ≤ 64 bytes, value one line ≤ 256 bytes, ≤ 32 keys, a repeated key keeps the first) |
| `[display]` | table | optional | the author's display declaration, spelled as part 2 §6.2 spells it on a record: `[display.<key>]` sub-tables with their fields, `[display.<key>.values]` for per-value entries; a reader resolves it with the same `notemeta` rules |

Unknown keys are ignored. A note has no `side`: it is on the file as it is
on disk. Notes on diff lines, staged content or a commit stay the notes
store's business (reviews, line notes).

### 3.4 Limits a reader enforces

| what | limit | over it |
|---|---|---|
| a document file | 512 KiB (`steer.MaxCommandBytes`) | ignored unread |
| overview body | 64 KiB | ignored (the CLI refuses to write it) |
| anchors per overview | 100 | later links are plain text |
| title | 200 chars, one line | ignored (CLI refuses) |
| summary / rationale | 500 / 4000 chars | ignored (CLI refuses) |
| notes shown per path | 50 (`MaxNotesPerFile`) | the first 50 by `created`, then id; the rest are not shown |
| overviews shown per worktree | none | a reader lists them all; today's cap of 20 open overviews was a memory limit and goes |

"Ignored" means: not shown, not an error on screen. gg logs the file name
and reason once at debug level (`observ`), so a writer can find out why.
A reader MUST NOT delete, rename or rewrite a file it cannot read.

## 4. Readers

### 4.1 Who reads

Every live side of the worktree: the TUI, the gg web page it hosts or a
standalone `gg web`, and the plugin's project window on this worktree.
Readers are independent; none of them is the master copy. A reader that
starts shows what is in the directory; one that runs sees changes.

### 4.2 Watching

A reader watches the directory (fsnotify: create, write, rename, remove,
chmod — a rename INTO the directory is how every write lands) with a short
debounce (the steer `Watcher` already does this for the inbox; a second
watch on `docs/` is the same code), and falls back to a poll (≤ 2 s) where
watches are not available (the plugin's VFS refresh, a network mount). The
directory may not exist yet when the reader starts: it watches the inbox
for its creation, or polls.

On every wake the reader re-lists the directory and reconciles by id:

- a new id → a new document (an overview joins the list; a note joins its
  file's remarks);
- a known id whose content changed (size or mtime, then bytes; an entry
  whose size and mtime are unchanged is NOT re-read, so a reload costs one
  stat per entry) → the document is REPLACED in place: an open overview keeps its position and
  selection as far as the anchors allow; a note keeps its on-screen state;
- a known id that is gone → the document is dropped: an open overview
  closes (the reader returns the user to where they were, as closing it
  by hand does); a remark disappears from its file.

### 4.3 Placing a note on the file (cold anchoring)

A reader meets a note file without ever having seen the writer's copy of
the file, so it cannot use the old-content/new-content diff that today's
`reanchor` starts from. The FIRST placement of a note against the file
content a reader has is:

1. If `lines` matches the file at `start` exactly (every line equal,
   `start + len(lines) - 1 ≤` file length) → the note is CURRENT at
   `start..end`.
2. Else, if `lines` occurs at exactly ONE place in the file → the note is
   current there (`start'..start'+len(lines)-1`).
3. Else → the note is OUTDATED, kept at `start..end` clamped to the file
   (`end ≤ len`, `start ≤ end`, both ≥ 1).

This is `agentdocs.relocate` applied with `lines` as the anchor — the same
function today's re-anchoring uses for an outdated note. After the first
placement the reader keeps its own copy of the content and re-anchors on
every change exactly as today (`sameLineMap` → `mapRange`; outdated →
`relocate`); `docs/specs/gg-steer/reanchor-cases.json` still holds. The
cold cases are published as `cold-anchor-cases.json` (§9).

A file that has no lines (deleted, empty, binary, too large) gives every
note on it the OUTDATED state at clamped `1..1`; a reader shows them in the
file's list but has no line to mark.

### 4.4 What a reader shows

- **Overviews**: in the list where it shows overviews today, as SESSION
  rows (the plugin's "This session" group; the TUI's open-files window;
  gg web's overview list), ordered newest first by `created`, then id;
  each row carries the title, author, age and anchor count, and whether
  it is open on screen. Opening one is today's overview viewer: anchors
  walked in order, the file follows, a note anchor shows the note's box.
- **Notes**: as the remark rows / boxes on the open file they name, with
  summary, rationale, author, meta badges per the display declaration, and
  the outdated mark; file lists count them as today (`NoteCount`).
- **Close vs delete.** Closing an overview on screen is LOCAL (the window
  shows it no more; the file stays and the row stays in the list). DELETE
  is a separate row action in every reader (the TUI's row menu and a key
  on the open-files window, the web list's row menu, the plugin's tree
  context menu "Delete") that unlinks the file — after the reader's usual
  confirm for a destructive row action. "Delete all notes on this file"
  unlinks every note file with that `path`. A reader deletes only what the
  user asked for, never on its own.
- A reader never writes a document. The one exception is gg's own tours
  (§6.5), which the TUI writes as a WRITER, not as a reader.

## 5. Writers

### 5.1 The write

```
dir := <docs directory>; MkdirAll(dir, 0755)
tmp := dir/.<id>.<random>.tmp        // starts with ".", ends ".tmp": invisible to readers
write the whole file to tmp; fsync optional
rename(tmp, dir/<id>.md | .toml)     // atomic replace on every OS gg runs on
```

A writer never writes a document in place, never holds a lock, never
reads the directory to decide a name (it mints one), never touches a file
it did not write except to remove it when the user or its own skill asks.
Replace = the same write to the same name. Remove = unlink. A writer that
wants to know what is there lists the directory like a reader does.

### 5.2 Who writes

- `gg session note add|rm|clear` and `gg session overview add|set|rm` (§6.2).
- An agent directly, following the skill's recipe (§7): a heredoc and a
  `mv`. The skill carries the two shapes above and the id rule.
- gg's TUI, for the agent tours (§6.5).
- The plugin's own note composer, if it ever writes session notes (not in
  this spec; `NoteComposer` writes the notes store today).

### 5.3 Concurrency

Rename is atomic; a reader sees either the old or the new file. Two writers
never touch the same file (ids are theirs). A reader may observe a
document vanish and come back within one debounce when a writer replaces
it; the reconcile rule (§4.2) handles both orders.

## 6. gg changes

### 6.1 `internal/agentdocs` becomes the directory's reader/writer

The package keeps its responsibilities — the anchor grammar and
`ParseOverview`, note placement (`Align`, `reanchor`, `relocate`), limits,
reply prose, the change signal — and gains the file layer:

- `Attach(root, worktreeDir, docsDir string) error` binds one ROOT (the
  store's existing per-worktree key) to its docs directory: loads it,
  watches it (fsnotify + a 2 s poll fallback), reconciles on every wake and
  signals subscribers exactly as today's mutations did. `Detach(root)`
  stops the watch and drops the root's documents from memory. The one
  `Shared()` store stays (TUI + hosted page), gg web keeps its own; each
  attaches the roots it shows (a view slot, a re-root). Ids are file
  names. A root that is NOT attached keeps today's memory-only behaviour,
  which is what tests and the transition rely on; every frontend attaches
  every root it shows once this ships. Attaching a root drops its memory
  documents (they have no file and could never be deleted afterwards).
- **Ids are unique per worktree, not per store.** Two attached roots may
  hold the same id. Documents are keyed by (root, id); `OverviewIn`,
  `FindNoteIn`, `SetOverviewIn`, `RemoveOverviewIn`, `RemoveNoteIn` take
  the root. The id-only calls (`Overview`, `FindNote`, `SetOverview`,
  `RemoveOverview`, `RemoveNote`) resolve only when exactly one root holds
  the id; an id found in several roots is refused (`overview <id>: the id
  names a document in several worktrees`), so a delete by id alone can
  never reach another worktree's file. Frontends that show several roots
  use the `In` forms.
- One reload per root runs at a time (a per-root mutex across the listing
  and the reconcile), so a poll that listed the directory before a write
  landed cannot undo that write's own reload.
- `Write*`/`Remove*` do §5.1 and return the parsed document; `AddNote` /
  `AddOverview` / `SetOverview` / `RemoveNote` / `RemoveOverview` become
  thin wrappers that validate with today's prose then write.
- The memory is a CACHE of the directory: a document is in the store iff
  its file is in the directory; `Notes`, `Overviews`, `FindNote`,
  `Overview`, `CheckAnchors`, `Align`, `AlignedNotes` keep their shapes.
- Cold anchoring (§4.3) on load; today's re-anchoring after.
- `FileTour(key, …)` keeps its contract (replace in place while open, file
  anew once closed) over fixed file names (§6.5).

What goes: `noteSeq` and the `t<n>` ids, overview ids from `fileSeq`
(`NextFileSeq` stays for open FILES),
`MaxOverviewsPerRoot` as a refusal (listing is unbounded; the open-files
window caps what it SHOWS at once as a UI matter), the steer handlers for
`note_*`/`overview_*` in `internal/tui` and `internal/web`.

### 6.2 CLI

`gg session note` and `gg session overview` keep their verbs and flags but
act on the directory of the worktree they run in (the same inbox
resolution as every `gg session` verb; `--worktree <path>` as today). No
live window is needed for any of them; `no gg session for this worktree`
is no longer an error here.

- `gg session note add <path>:<start>[-<end>] --summary … [--rationale …]
  [--author …] [--meta k=v]… [--display <json>|@file] [--json]`: reads
  `<path>` from the worktree on disk NOW (CRLF-tolerant; a missing or
  binary file is refused: `<path> has no lines to note`), validates with
  today's prose (`a line number is 1-based`, `the range ends before it
  starts`, `line N is past the end of <path> (M lines)`, `a note needs a
  summary`, `the summary is longer than 500 characters`, …), fills `lines`
  from the file, writes `<id>.toml`, prints `noted <path>:<start>-<end> as
  <id>` (`--json`: the note). `--author` default `agent`.
- `gg session note list [--json]`: every note file of the worktree,
  `<id>\t<path>\t<start>-<end>\t<summary>` ordered by path then start.
  `show <id>`: as today, the `lines` from the file. `rm <id>`: unlink,
  `removed <id>`; `no note <id>` otherwise. `clear <path>`: unlink every
  note on `path`, `removed N notes on <path>`.
- `gg session overview add --title … [--file <md>] [--json] [--background]`:
  validates (title, body size, non-blank), writes `<id>.md`, prints the
  id, then one `unresolved: <dest>` line per anchor whose file is not in
  the worktree or whose note is not in the directory. `set <id> [--title
  …] [--file …]`: rewrites the same name (`no overview <id>` if the file
  is not there); an omitted `--title` keeps the stored one. `list`:
  `<id>\t<n> anchors\t<title>` newest first. `show <id>`: front matter
  fields then the body. `rm <id>`: unlink. `export <id> [--out …]`:
  unchanged (the HTML export).
- **Showing it.** Writing a file does not change what any window shows.
  `add` and `set` therefore END by steering, when a window is live, a
  `file_focus` for the new id (routing exactly as any bare verb: TUI or
  web first, the IDE when alone, `--to` to pick), and print its reply line
  (`opened overview <id>` / `queued: …`). `--background` skips the
  steer: the file is written, nothing is focused. With no window live the
  verb prints nothing more and exits 0 — the file waits for the next
  reader.
- `gg session files focus <id>` keeps steering the live window to show a
  document by id; `gg session files` lists what the window shows, document
  ids included.
- New: `gg session docs path [--worktree …]` prints the directory (for an
  agent that writes directly), creating nothing. `gg session docs ls` is
  `note list` + `overview list` in one table (`<kind>\t<id>\t…`).
- `--json` shapes: a note `{id, path, start, end, summary, rationale,
  author, created, lines, meta, display, outdated}`, an overview `{id,
  title, author, created, anchors: n, unresolved: [dest…]}`.

### 6.3 Steer protocol revision

The steer spec is revised (a dated section, not a rewrite): the verbs
`note_add`, `note_list`, `note_show`, `note_rm`, `overview_add`,
`overview_set`, `overview_list`, `overview_show`, `overview_rm` are
RETIRED. A consumer that meets one answers `error: "note_add is retired:
write the file (gg session note add)"` — the same shape as any unknown
verb. `files`, `file_focus` (with a document id), `navigate`, `highlight`
and the rest are unchanged. `command.schema.json`'s `cmd` description and
`steer-cases.json` drop the retired verbs; cases for them become
`retired` cases whose expected reply is the refusal above.

### 6.4 TUI and gg web

- Readers per §4: the open-files window lists session overviews with their
  rows; file views show note boxes and bands as today; the overview viewer
  is unchanged.
- Row actions: close (local) as today; DELETE (unlink, after the usual
  confirm) on an overview row and on a note box; "delete all notes on this
  file" in the file's row menu.
- Hosted web shares the TUI's store; standalone `gg web` opens its own on
  the same directory. A re-root (repo switch) closes the store and opens
  the new worktree's.
- The fast switch (per-worktree view slots) opens one store per slot; a
  worker's console views its worktree's directory, which is where its
  tours live (§6.5).

### 6.5 Agent tours

The TUI writes a spawned worker's brief and every final report as
overview files in the WORKER's worktree directory, names
`tour-brief-<proc>-<n>.md` / `tour-report-<proc>-<n>.md`, front matter
`author = "gg"`. A new report rewrites the report file (replace in
place). A tour the user DELETED comes back only with a NEW report (the TUI
remembers the report seq it wrote, as today); one the user merely closed
stays in the list. `agent_report` / `gg agent report` are unchanged: the
TUI is the writer, composing from `domain.AgentTour` as today. A worker's
IDEA window on that worktree sees the brief and the report too.

## 7. Skills

- **gg-overview v14.** The temporary recipe becomes the SESSION recipe:
  notes and overviews are files in the worktree's docs directory, shown
  by every gg reader of that worktree and kept until deleted. Removed:
  "no window live → start gg web" (write the files; whichever reader
  opens next shows them — say in one line that no window is open and
  where the files are), the `queued:` rule for notes and overviews (a
  file write has no reply; it stays only for `navigate`/`files focus`),
  the overview/note ids `f<n>` / `t<n>` (ids are the file names the CLI
  prints; `f<n>` still names an open file). Added:
  the DIRECT-WRITE recipe for an agent whose gg is old or absent — `gg
  session docs path` (or the path rule), the two file shapes verbatim, the
  id rule, temp + `mv`, and that `lines` must be the file's lines as the
  agent read them, after its last edit. The `--background` note stays
  (now: write without focusing). The reply to the user names the ids.
- **using-gg** (`Version` bump): the "Temporary notes" and "Overviews"
  sections describe the files, the new verbs (`docs path`, `docs ls`), the
  retired steer verbs, and that no window is needed to add.
- **gg-delegate**: a line that the brief and report tours are files in the
  worker's worktree directory (the worker's own IDEA sees them).
- `gg init --update` regenerates the installed copies (dogfood copies in
  `.claude/skills/`).

## 8. Plugin (`gg-notes`)

- A reader per §4 on the project's worktree directory: the "This session"
  group lists the overview files (title, author, age, anchors, on screen);
  note files become remark rows on the editor (the existing `RemarkOverlay`
  path), placed by §4.3 (`Relocator`/`Reanchor` already implement the
  functions) and re-anchored on edits.
- `SessionMemory` and `TempNotes` go; `SteerHandler` keeps `navigate`,
  `highlight`, `files`, `file_focus` (by document id) and answers the
  retired verbs with §6.3's refusal.
- Delete on a session row unlinks the file (the tree's "Delete" item,
  which exists already, gains this meaning on session rows; "Remove from
  gg Store" stays for store rows).
- Migration section for `PLUGIN.md`: what to drop, what to watch, the
  reconcile rule, the cold-anchor cases to run.

## 9. Conformance material

`docs/specs/gg-session-docs/` (copied to `/work/gg-notes/docs/gg-session-docs/`
with the provenance header the other copies carry):

- `README.md` — this spec's pointer and the list below.
- `overview-frontmatter.schema.json`, `note.schema.json` — JSON Schema for
  the PARSED shapes (TOML → JSON mapping: tables are objects, arrays of
  tables are arrays, date-times are RFC 3339 strings), lenient: unknown
  keys allowed; the `required` sets and bounds of §3. gg validates every
  file its CLI writes against them (`TestSessionDocShapesMatchSchemas`).
- `examples/` — a directory written by gg's own CLI through a golden test
  (`-update-fixture`): two overviews (one with a `note:` anchor and one
  unresolved anchor), three notes (one with meta + display, one on CRLF
  content, one outdated against the example tree), plus `tree/` with the
  files they anchor, and an `ids.json` listing which is which. The proof
  of the spelling, as the notes-store fixture is.
- `cold-anchor-cases.json` — §4.3: `{name, file: [lines], note: {start,
  end, lines}, expect: {start, end, outdated}}`; at least: exact at start;
  moved, unique; moved, ambiguous → outdated clamped; file shorter than
  start → clamped; empty file; `lines` of several entries matching
  partially → outdated. gg runs them (`internal/agentdocs`
  `TestColdAnchorCases`); the plugin runs them against its relocator.
- `id-vectors.json` — valid and invalid ids (`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`),
  and file names that readers ignore (`.x.tmp`, `x.txt`, `x.md.tmp`, a
  directory).
- `reanchor-cases.json` and `repokey-vectors.json` stay in `gg-steer/`;
  the README points at them.

## 10. Compatibility and rollout

- A new gg beside an OLD plugin: the plugin still handles `overview_add`,
  but the new CLI never sends it; its "This session" rows stay empty until
  it reads the directory. An old CLI beside a new TUI: `overview_add` is
  refused with §6.3's prose, which names the fix. The skill bump plus
  `gg init --update` is what moves agents.
- Documents written before any reader exists are shown by the first reader
  to start; documents written while a reader is busy are shown on its next
  wake. Nothing depends on order.
- The notes store (`<state>/gg/notes/…`) is not read or written by anything
  in this spec. Promoting a session overview to a permanent one
  (`gg overview save --from <id>`) is a natural follow-up and is NOT in
  scope.
- `agentskill.Version`: main is at 175; the unmerged notes-store spec
  branch takes 176 on merge; this feature takes the next free number at
  merge time. `GGOverviewVersion` 13 → 14; `DelegateVersion` 7 → 8.

## 11. Non-goals

Sweeping or expiring documents; owner or reader liveness; a stale marker;
documents shared across worktrees of one repo; notes on a diff side, a
commit or the index (the notes store's domain); a server or port; any
change to `commits.toml` / `previews.toml` / `worktrees/<key>.toml`.

## 12. Testing

- `internal/agentdocs`: write/read round trip per shape (field by field,
  CRLF, meta/display spelling against the part-2 rules); ignore rules
  (dot files, `.tmp`, foreign extensions, > 512 KiB, bad TOML, missing
  required keys); cold anchoring cases; reconcile (new / changed / gone)
  through a real fsnotify watch on a temp dir with the poll fallback
  forced; replace keeps an open overview's position; schema conformance
  of everything the CLI writes; the golden `examples/` generator.
- `internal/cli`: every verb against a temp worktree + temp state dir
  (`XDG_STATE_HOME`): outputs and exit codes above, `unresolved:` lines,
  `set` on a missing id, `clear`, `docs path`, `docs ls`, `--background`
  vs the `file_focus` steer when a fake presence is live.
- `internal/tui` (headless): a file dropped into the directory appears in
  the open-files window; delete unlinks; a retired steer verb is refused
  with the spec's prose (`TestSteerCases` `retired` cases); tours land in
  the worker's directory. `internal/web`: the same over HTTP.
- e2e scenario: agent writes files by hand (no CLI) → TUI shows them.
- Race gate green before merge.

## Implementation rulings (plan 4, 2026-10-19)

What shipped where the text above says otherwise; the code and its tests
are the truth, this block records the delta.

- §6.2 `note add` answers `noted <path>:<N> as <id>` for a single line
  (`<start>-<end>` only for a range — the existing span prose). It takes a
  PATH only (an open file's `f<n>` is not accepted) and opens nothing; a
  binary file or one over the diff size cap is refused with `<path> has no
  lines to note`. `note clear <path>` with no notes is exit 1 `no notes on
  <path>`. There is no `--worktree` flag (none existed before either).
- §6.2 `--json`: a note keeps the steer `FileNote` shape (`text` for the
  lines, present on `show`; `created` added), an overview the steer
  `Overview` shape plus `author` / `created`.
- §6.2 `overview show` prints `<id>\t<title>\t<author>\t<created>`, a blank
  line, the text. `set` takes `--background` too. The reply the readers give
  to the focus that ends `add`/`set` is their `focused overview <id>
  "<title>"` (not `opened overview <id>`); with no window live nothing is
  printed, exit 0.
- §6.3 the refusal is per kind: `… (gg session overview add)` for an
  `overview_*` verb; the TUI answers it before every other check, gg web
  before its wire check, both as a normal reply. A reader asked to focus a
  document id it has not listed re-reads the directory once first.
- §6.4 the TUI lists a noted working-tree file that is not open in the
  background (as gg web's follow pass does); in the TUI `X` on it closes it
  and the next store signal lists it again, while gg web backgrounds it.
- `agentdocs.Note` carries `Created`; `steer.OpenFile` rows of an overview
  carry `author`, `created`, `anchors` on both readers.
