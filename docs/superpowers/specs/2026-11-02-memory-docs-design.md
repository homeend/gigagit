# Memory documents — notes and overviews with no file

Date: 2026-11-02 · Branch: `feat/memory-docs`

## Why

An agent wants to leave a short explanation for other agents running in the
same gg process: a few lines of text, with a `gg://` link it can hand over, and
with a way for the user to copy the whole text to the clipboard. Today the
closest thing is a session overview, which writes a file into the worktree's
session-docs directory, stays until someone deletes it, and renders as markdown
with an "it has no anchor" warning.

The user's model, which this spec follows: **a note or an overview is backed by
a file or by nothing (memory). Only two things differ: the backing and, when
asked for, the rendering (plain text).** Everything else (the ctrl+\ window, the
link, copy, delete) is the same as for a file-backed document. The user can also
create one in the ctrl+\ window.

## Goals

1. An agent creates a memory document from the command line, with the text on
   the command line, and gets its link back.
2. Another agent reads it through that link, also from another worktree.
3. It appears in ctrl+\ **Overviews & notes** of the TUI and the gg web page.
   The user opens it, copies the whole text, copies the link and deletes it, with
   the same keys as a file-backed document.
4. The user creates one in the ctrl+\ window (TUI and web).
5. Backing is decided per document, for overviews and for line notes.
6. Plain-text rendering is a separate choice, for either backing.

## Non-goals

- The JetBrains plugin. It reads the directory, so it does not see memory
  documents.
- Surviving gg's exit. Memory means memory.
- Sharing between gg processes: a standalone `gg web` and a TUI each hold their
  own.
- Converting a memory document to a file or back.
- HTML export of a memory or a plain document: no Export row, and the CLI answers
  `overview <id> has no export: read it with gg overview show`.

## Anti-goals (a failure even if it works)

- Asked for memory, gg silently writes a file. With no live gg, adding fails.
- A memory document is written to disk beyond the moment it crosses the steer
  inbox (the command and reply files are removed once consumed, as today). It
  never enters the session-docs directory, session snapshots or state dumps. The
  link history records the link, as it does for every link, never the text.
- Adding or reading one moves anything on the user's screen, or raises the
  "switch worktrees?" notice. The exception is `add`/`set` without `--background`,
  which shows the document as they do today.
- A file-backed document changes behaviour: its file, its link and its listing
  stay exactly as they are (a new line of output is additive, see below).

## What the agent does

```bash
gg session overview add --title "Why the lock moved" --memory --plain --background \
  --text "The gate now takes RefWrite first, because …"
```

prints

```
20261102T031500-a1b2
link: gg:///work/repo/.claude/worktrees/x?overview=20261102T031500-a1b2
```

Another agent runs `gg overview show <link>`, as for any session overview.

### Flags (`gg session overview add|set`)

- `--memory` (new, `add` only): no file. `set` follows what the id already is.
- `--plain` (new): the text is plain, for either backing. `set` keeps the current
  value when omitted.
- `--text "…"` (new): the text on the command line. Exactly one of `--text`,
  `--file` and stdin, for either backing; a second one is exit 2.
- `--background` keeps its meaning. The skill tells agents to pass it for a text
  meant for other agents.

`gg session note add --memory` (new) puts a line note in memory the same way. The
CLI reads the file's lines, as it does now, and sends them with the command.

### The answers

- `add` prints a new `link: <link>` line after the id, for both backings; `--json`
  gains `link`. This also closes the existing gap where an agent has to write a
  session overview's link by hand.
- No live gg: `no gg session for this worktree: a memory document needs one`,
  exit 1. Nothing is written. Today's "the file waits for the next reader" does
  not apply.
- `--plain` suppresses the no-anchor warning; a plain document has no anchors.
- The limits stay the same: 64 KiB of text, a 200-character title, 20 memory
  overviews per worktree, 50 notes per file.

### Reading and listing

`gg session overview list|show|set|rm`, `gg session note list|show|rm` and `gg
session docs ls` read the directory as now and also ask the live gg, when there
is one, for its memory documents. A listing with no live gg lists the files and
says nothing more.

Text listings append `\tmemory` to a memory row only, so a file row's output is
byte-identical. `--json` gains `"backing": "file" | "memory"` and `"plain"`.

`gg overview show <link>`: a link whose file is not there asks the live gg of that
worktree before it answers `overview <id> no longer exists`. A link to a memory
document after gg quit gets that same answer. The same fallback applies wherever
a session overview link is resolved or described and the file is missing
(`domain/linkresolve.go`, `domain/linkdesc.go`, `gg open`, `gg session navigate`).

## What the user sees

**ctrl+\ Overviews & notes (TUI and web).** Memory documents are listed with the
other documents of the worktree on screen, with a dim `memory` tag on the row
(translated). The keys are unchanged: `enter` opens, `y` copies the link, `x`
closes it in this reader (the document stays, as for a file), `D` deletes it after
asking.

**New: `n` creates one.** It opens a window with a title field and a multi-line
text field, reusing the text-template editor's text area (`text_templates.go`) in
the TUI. Saving creates a memory, plain document in the worktree on screen and
opens it. `esc` leaves without creating anything, asking first if text was typed.
There is no backing choice in this window. The web page has the same key and a
matching dialog, and goes through the same store. `n` goes in the `?` help and the
footer in both.

**A plain document, open.** Its text is shown as written: no markdown, no links,
no anchors, wrapped to the window. The whole-text copy is there as for any
overview (TUI `.` menu **Copy overview**, web `y`), as are the link copy (TUI `.`
menu **Copy gg link**, web `L`), `/` search and `v` selection. Tab-from-anchor keys
have nothing to step through.

**A memory markdown document, open.** Identical to a file-backed overview, without
the Export row.

## How it works

### Backing per document (`internal/agentdocs`)

Today the backing is decided per root: an attached root writes files, an unattached
root keeps documents in memory, and `Attach` drops the root's memory documents
(`attach.go`, review finding D: they could never be deleted).

- `Overview` and `Note` gain `Memory bool`; `Overview` gains `Plain bool`.
- New `AddMemoryOverview`, `SetOverview`-through and `AddMemoryNote` always take the
  memory path, attached root or not. A memory document's id comes from `MintID`, so
  `ValidID` accepts it and the existing link grammar carries it, unlike the legacy
  `f<n>`. Adding refuses an id that already exists in the root.
- `removeOverview`, `SetOverview` and note removal branch on the document, not the
  root. A memory document never touches `r.dir`.
- `Reload` reconciles against `r.seen`, which holds only files, so it already leaves
  memory documents alone. A test pins this.
- `dropRootLocked` keeps documents with `Memory` set, so `Attach` and `Detach` (a
  pruned slot, a repo switch) do not lose them: they live until deleted or until gg
  exits, and show again when that worktree is on screen. Legacy unattached documents,
  which do not have the flag, keep today's behaviour.
- `Overview` keeps the worktree it was created for (`ovEntry.dir`), so a link can be
  built on a root that is not attached. `overview_link.go` and
  `web/session_overview.go` use it for a memory document.
- A plain overview is not passed through `ParseOverview` and has no anchors.
- File-backed `--plain` is one more front-matter field, `plain = true`, omitted when
  false. Format stays as it is if old readers ignore unknown keys; the builder checks
  this, and bumps the format only if they do not.

### Carrying the text to gg (`internal/steer`)

The CLI is another process, and the file was the carrier. Without one, the text goes
through the steer inbox, which already reaches gg from any agent whose worktree gg
holds an inbox for: the worktree on screen, and a console child's `GG_INBOX`
(`steer_kept.go`). `MaxCommandBytes` is 512 KiB, enough for a 64 KiB text.

New verbs, one per operation, in `steer.go`: `memdoc_add`, `memdoc_set`, `memdoc_list`,
`memdoc_show`, `memdoc_rm`, and `memnote_add`. They use the existing `Title`, `Text`,
`FileID`, `Author` and `Worktree` command fields and answer in `Reply.Overviews` /
`Reply.Notes`. `Command` gains `Plain bool`. The retired `overview_*`/`note_*` verbs
stay retired and are not revived, so an old CLI still gets its refusal.

The root is `CheckoutKey(c.Worktree)`, the sender's worktree. The consumers answer
these verbs:

- **Without touching the screen:** before `steerShownMismatch`, the switch ask, and
  any in-flight lane (TUI `steer.go`; web `handleSteer`, beside `files`, before
  `toSteerWire`).
- **On either side of one active interface:** a TUI whose hosted page is active still
  answers, and so does the page. They share `agentdocs.Shared()`. The builder checks
  how `sessionDir.target()` routes a verb when one side is blocked (`tuiBlocked`,
  `webBlocked`) and makes these verbs reach whichever side is live.

**Keeping a link reachable.** A gg that moved to another worktree would stop holding
that worktree's inbox, and `gg overview show <link>` would wrongly say the document
is gone. So `keptInboxes` also includes the inbox of every worktree with a memory
document, and `tendKeptInboxes` keeps its presence as it does for a running child. It
releases the inbox when the last such document is deleted. Another live gg on that
inbox still wins, as today; that gg has no such document and the reader gets "no longer
exists". A standalone `gg web` has one worktree and no such case.

### Rendering

- TUI: `openFile` with `ov` set draws its rows from a new plain-rows path when `Plain`,
  bypassing `tui/md_render.go`. `overviewRows` drops the anchor rows and, for memory or
  plain, the Export row.
- Web: `viewer.js` paints plain text (`textContent`, never HTML) and hides the
  export button for memory and plain. `/api/open-files` gains `memory` and `plain` on
  a row; `openfiles.js` draws the tag and handles `n`.
- `gg web` creates through a new POST route under the existing Host/Origin guards,
  with the store's limits as the only size check.

## Docs and tests

- `CHANGELOG.md`, `README.md`; `internal/agentskill/using-gg.md` (the flags, the
  answers, "for a short text for other agents, use `--memory --plain --background`")
  with the `agentskill.Version` bump and the regenerated dogfood copy; the CLAUDE.md
  rows for `agentdocs`, `steer` and `cli`; `docs/CLAUDE-details.md`; the web parity doc.
- Translations in all four bundles for every new TUI string.
- Tests:
  - `agentdocs`: a memory document survives `Reload`, `Attach` and `Detach`; its removal
    touches no file; legacy unattached documents still drop.
  - `cli`: every answer above, including no live gg (exit 1 and no file in the directory),
    `--text`/`--file` exclusion, file rows byte-identical.
  - `tui`: `n` end to end, tag, plain rows, copy rows, `D`; no screen change and no
    switch notice on a verb from another worktree.
  - `web`: the steer verbs, the create route, no HTML interpretation of plain text.
  - e2e: one scenario, agent adds and another worktree reads through the link, then a
    TUI golden of the tagged row.
  - A browser probe of `n`, the tag, copy text and copy link, in hosted and standalone.

## Left to the builder

The verb and helper names above, the exact window layout of `n`, where the fallback
lookup lives between `domain` and `cli`, and the tag's styling.
