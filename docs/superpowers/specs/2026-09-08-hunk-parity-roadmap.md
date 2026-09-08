# Hunk parity roadmap: review notes, agent skill, syntax highlighting

Date: 2026-09-08. Status: research + proposal, awaiting approval. Nothing
below is implemented.

Source studied: `modem-dev/hunk` at its `main` tip on 2026-09-08 (v0.21.x,
TypeScript on Bun, OpenTUI + React, `@pierre/diffs` for diffing and
highlighting). Cloned read-only into the session scratchpad; nothing from it
is vendored.

## 1. What hunk is, in one paragraph

Hunk is a review-first terminal diff viewer, not a git client. It opens one
changeset (`hunk diff`, `hunk show <rev>`, `hunk patch -`, two raw files) as a
multi-file scrolling stream with a files sidebar, auto split/unified layout,
syntax highlighting, word-level intraline emphasis, collapsible unchanged
gaps, mouse support, remappable keybindings and Shiki themes. Its two
distinctive features are **inline review notes** (human notes with threaded
replies, plus agent annotations loaded from a JSON sidecar or pushed live)
and a **loopback session daemon** that lets a coding agent inspect, steer and
annotate the user's live window through `hunk session …` commands, taught by
a bundled `SKILL.md`. It also ships a TypeScript extension host, `hunk log`
history browser, `hunk pager` (git core.pager), and jj/sapling adapters.

## 2. Feature inventory: hunk vs gg

Legend: **have** = gg already does it, **partial** = exists in one frontend or
in a weaker form, **missing** = not in gg.

| Hunk capability | gg status | Notes |
|---|---|---|
| Side-by-side diff with intraline word emphasis | have | `textdiff` Enhanced spans; TUI + web |
| Unified layout | partial | web switches below ~950px; TUI is split-only |
| Auto split/unified by width | partial | web only |
| Collapse/expand unchanged gaps (`z`) | have | `f` toggles partial/full for the whole file, not per gap |
| Syntax highlighting | missing | see §5 |
| Files sidebar + multi-file review stream | partial | gg is one file at a time; `N`/`P` step files |
| Next/prev hunk (`]`/`[`) | have | `n`/`p` over change blocks, wrap-around |
| Next/prev **annotated** hunk (`}`/`{`) | missing | needs notes |
| Line numbers / wrap / hunk-header toggles | partial | wrap via `ctrl+w`; no line-number or hunk-header toggle |
| Current-line cursor + align top/center/bottom | missing | gg diff view is pure scroll, no cursor row |
| Visual line selection + copy (`v`, `y`) | missing | already on the backlog as "text operations" |
| Open file in editor at the current line (`e`) | partial | external editor exists for messages; not from a diff line |
| Move detection (moved-line kind) | missing | |
| Tab width setting | missing | |
| Watch mode (auto-reload open diff) | partial | file-watch refreshes panels; nothing in the refresh path touches an open diff layer (`diffLayer()` has no refresh caller), so an open diff stays as loaded |
| `hunk pager` as git core.pager / `hunk patch -` from stdin / two raw files | missing | `gg apply` imports patches; nothing shows a patch as a viewer |
| `hunk log` history browser | have | Commits panel is richer |
| Keybinding remap via config | missing | |
| Theme selector (Shiki themes) | missing | |
| Human review notes (`c` add, `E` edit, `R` reply, threads) | missing | |
| Agent annotations: JSON sidecar + live push, `a` toggle | missing | |
| Attention marks (character-range highlights with tones) | missing | |
| Agent session CLI (`session list/get/context/review/navigate/reload/comment/highlight`) | partial | MCP has read tools + `gg_ui_state` snapshot; no inbound control |
| Bundled agent skill (`hunk skill path`) | have | `gg init` installs `using-gg`; no review-specific skill |
| MCP server | have | `gg mcp` |
| Extension host (TS plugins) | missing | out of scope, §7 |
| jj / sapling | missing | out of scope, §7 |

What gg has that hunk does not, so this is parity on a subset, not a
rewrite: the full client (SmartPull/Switch/Merge/Rebase, push, stash,
worktrees, tags, remotes, reflog), conflict and staging hunk pickers, AI
commit-message / review / conflict-complete lanes, bookmarks and shelves,
branch versions, repo switcher, four frontends, i18n, and the big-repo
machinery (repogate, cached differ, tags fingerprint cache).

## 3. Review notes and the agent skill (the priority)

### 3.1 How hunk does it

- **Note record** (`ReviewNoteV1`): `id`, optional `parentId` (threads),
  `source` (user / agent / ai), `fileKey`, `anchor` (a range on the old or
  new side plus the owning hunk index), `summary`, optional `rationale`,
  `markup` (experimental STML), `title`, `author`, `tags`, `confidence`,
  `editable`, timestamps.
- **Reconciliation**: on reload each note is re-anchored and marked
  `active`, `stale` (context moved, still shown at last anchor) or
  `orphaned` (hidden).
- **Sidecar input** (`--agent-context notes.json`, schema version 1):

  ```json
  {"version":1,"summary":"…","files":[{"path":"src/x.ts","summary":"…",
    "annotations":[{"newRange":[15,35],"summary":"…","rationale":"…","author":"sonnet"}]}]}
  ```

- **Live lane**: the TUI registers with an authenticated loopback daemon;
  `hunk session comment add|apply --stdin|list|rm|clear`, `navigate
  --file --hunk|--new-line|--old-line`, `--next-comment`, `reload -- diff …`,
  `highlight add --start --end --tone`. The skill tells the agent: never run
  the TUI yourself, inspect `session review --json` first, navigate before
  commenting, batch with `comment apply`, do not comment on every hunk.
- **Persistence**: none found. Notes live in the in-memory review store
  (`core/review/store.ts`); a grep over `core/review`, `session/agent`,
  `session/broker` and `app` finds no note write path, only the broker's
  lock and metadata files. Hunk's own extension-API audit lists "no
  host-managed persistence" as a gap, which corroborates the grep.

### 3.2 Proposal for gg

**Persist notes per repo, with an expiry.** This is the first
differentiator: a note survives quitting the TUI, and a CLI or MCP call can
add one while no TUI is running. Decided 2026-09-08: notes are working
material that should be discarded at some point, like branch versions, not
kept like bookmarks.

- **Store**: shelf-style, `notes.toml` beside `shelf.toml` in the per-repo
  XDG state dir, owned by `domain`, frontends never touch it directly. The
  branch-versions ref namespace (`refs/gg/versions/…`) was considered and
  rejected: a ref must point at a git object, and most notes anchor to
  uncommitted working-tree lines that have none.
- **Expiry**: the branch-versions rule, copied. A `notes.max_age_days`
  config key (default 30; `0` or less keeps forever) with a `settingDoc`.
  Pruning is NOT done on load: the "Growth" rule below (decided later the
  same day) moves it to a startup goroutine, and reads never rewrite.
  Orphaned notes (anchor gone after reconciliation) are dropped in the same
  sweep; `gg note clear` remains for explicit cleanup.
- Machine-private by design; notes do not travel with the branch.
- **Growth** (decided 2026-09-08): one `notes.toml` per repo; records are
  short text (~300 bytes). Housekeeping runs as a **background goroutine on
  every gg start** (TUI, `gg web`, and every `gg note …` CLI invocation): it
  loads the file, drops notes past `notes.max_age_days` (default 30, `0` =
  never) and orphaned notes (target file/commit gone, or the anchored lines'
  fingerprint no longer found), and rewrites the file once, off the UI
  thread. The entry cap (`notes.max_entries`, default 2000, oldest dropped
  first) is enforced on every write, since a write is the only moment the
  file can grow. Reads never rewrite. Writers use the shelf's atomic-rewrite
  pattern under a short file lock and re-read before writing, so the
  startup sweep and an agent's `gg note add` cannot clobber each other.
  Sharding per anchor target stays the escape hatch if a single file ever
  becomes a problem; the model does not change.

**Note model** (`model.Note`, engine-free, mapped from hunk's record):

- `ID`, `ParentID` (thread), `Source` (`user` | `agent`), `Author`
- `Address model.FileAddress` (worktree state or commit + path; reuses the
  bookmark/shelf provenance type)
- `Side` (`old` | `new`), `Range [2]int` (1-based lines), `ContextHash`
  (hash of the anchored lines for stale/orphaned reconciliation)
- `Summary`, `Rationale`, `Tags`, `Confidence`, `Created`, `Updated`
- Resolution (`active` / `stale` / `orphaned`) is computed at load time,
  never stored.

**Import/batch format**: adopt hunk's `agent-context.json` v1 unchanged as
`gg note apply --stdin`'s input. Agents already trained on hunk's skill emit
it, and `examples/3-agent-review-demo/agent-context.json` is a free test
fixture. gg's own richer fields ride on top as optional keys.

**TUI surface** (diff view + Files panel):

- Notes render as inline rows under their anchored line, prefixed by
  author/source; `a` toggles the agent layer (user notes always stay
  visible, hunk's policy), `c` adds at the cursor line, `E` edits, `R`
  replies, `}`/`{` jump to the next/previous annotated block, and a `◆`
  gutter marker shows a note on a collapsed row.
- Files panel rows show a note count; the Commits panel shows one on a
  commit whose files carry notes.
- Notes on the working tree re-anchor by `ContextHash` after each refresh.

**Web surface**: same rows in `diffHTML`, same keys, a note count in the
files list. Web already has the SSE hub, so live agent notes appear without
polling.

**CLI lane** (`internal/cli`, terse agent verbs):

```
gg diff --hunks [<rev>|<A..B>] [-- <paths>...] [--json]   # numbered hunks per file with ranges
gg note add    --file <path> (--hunk N | --new-line N | --old-line N) [--rev <commit>] --summary "…" [--rationale "…"] [--author x]
gg note reply  <note-id> --summary "…"
gg note apply  --stdin            # hunk agent-context v1 JSON
gg note list   [--file <path>] [--type user|agent|all] [--json]
gg note rm     <note-id>
gg note clear  [--file <path>] [--all] --yes
```

**MCP tools**: `gg_notes_list`, `gg_note_add`, `gg_notes_apply` (batch),
`gg_note_rm`; read tools stay `readOnly`, mutations gated like
`gg_write_to_worktree`.

**`gg review` integration**: add `--notes`, which asks the configured review
tool for sidecar JSON (the context doc gains a short "emit agent-context v1"
instruction) and imports it, so an AI review lands as anchored notes rather
than a text report. The freeform report stays the default.

**Skill**: ship a second embedded skill, `reviewing-with-gg`, next to
`using-gg` rather than bloating it. `gg skill path [review]` prints it (hunk's
verb). Content mirrors hunk's: never launch the TUI yourself; inspect with
`gg diff --stat` / `gg diff -- <file>` / `gg note list --json`; navigate
before commenting once live steering exists; batch with `gg note apply`;
comment on what the user would not spot. `gg init --update` installs both;
`agentskill.Version` bumps.

## 4. Can gg's model carry this? Adjustments needed

**As a standalone viewer**: technically already. The diff view is a
self-contained layer (`diffView` + `domain.Differ`) that the Files, Commits,
history, blame, compare and bookmark surfaces all open the same way, so it
does not depend on the panels being visible. What is missing is an entry
point that opens it *without* the panels the way `hunk diff`, `hunk show`,
`hunk pager` and `hunk patch -` do: `gg diff --view [rev] [-- path]`,
`gg pager` for git's `core.pager`, and a stdin patch loader (a patch has no
full sides, so it needs the fragment path from §5). That sits in phase 5
today and can be pulled forward if "run this as a diff viewer" is the goal.

**As a notes/review host**: mostly yes. Four concrete gaps, in dependency
order:

1. **No cursor row in the diff view.** `diffView` is pure scroll
   (`offset`, `cur` change block). Notes, `c` at a line, `e` open-at-line,
   `--new-line N` navigation and hunk's align-top/center/bottom all need a
   line cursor. Add `curLine` to `diffView` with `j`/`k` (arrows keep
   scrolling), a current-line marker style, and a `line ↔ display row`
   mapping that already exists as `lineStart`. This is phase 0.

2. **One file at a time vs hunk's stream.** "Next annotated hunk" must cross
   files. Recommend cross-file hopping first: a repo-wide note index in
   domain, and `}` at the last annotated block of a file steps to the next
   file with notes, reusing the `fileArm` prev/next-file mechanism. A
   stitched multi-file stream view is a separate, larger feature and not
   needed for notes.

3. **What is "hunk N"?** Agents read `gg diff` output, which prints git's
   `@@` hunks. The TUI navigates `textdiff` change blocks, which are finer
   (one per contiguous change, no context merging). Decided 2026-09-08 (user):
   **`--hunk N` is implemented in phase 2**, numbered by git's `@@` hunk
   order within the file, 1-based. Domain resolves hunk N to its old/new
   line range; the TUI scrolls to the first change block inside that range;
   a note anchored by hunk covers the hunk's whole new-side range (old side
   for a pure deletion). A `gg diff --hunks [<rev>] [-- <paths>]` listing
   (`path  n  old-start,len  new-start,len  header`, `--json` too) lets an
   agent pick a number without parsing the patch. Line anchors remain the
   precise form and the skill's default. The conflict and staging pickers
   are untouched: they work on conflict-marker regions and textdiff blocks,
   never on this numbering.

4. **Cached diff rows are shared and read-only** (`differ.go`: a cache hit
   aliases rows into the view). Note markers, resolution verdicts and
   highlight tokens must live in sidecar slices indexed by row, never as
   fields on `textdiff.Row`. `textdiff` stays pure; `domain.Diff` grows
   optional parallel slices.

**Live steering** (hunk's `session navigate/reload/highlight`). gg's TUI has
only an outbound channel: `session_snapshot.go` writes a JSON file that
`gg_ui_state` reads. Two options:

- **(a) Inbox file** beside the snapshot: `gg session navigate …` appends a
  JSON line; the TUI watches it (fsnotify via `gitwatch`'s wrapper, plus a
  1s poll fallback because fsnotify is unreliable on 9p/drvfs mounts) and
  applies commands on the Bubble Tea loop. No new network surface, no
  credentials, works when several TUIs are open (one inbox per snapshot).
  **Decided 2026-09-08: this is the channel.**
- **(b) Loopback control server**: reuse `gg web`'s guarded loopback server
  as the control plane. Steers the web page for free, but the TUI would need
  to run a listener too, and the Host/Origin/credential story hunk built
  (owner-private token, signed responses) would have to be repeated.

Attention marks (`highlight add --start --end --tone`) are a cheap add once
the inbox exists: a transient per-row span list rendered like emphasis
spans, cleared on reload of that file.

### 4.1 Phase 0 design: the diff-view line cursor (approved 2026-09-08)

**Cursor model.** `diffView` gains `curLine int`, an index into the logical
line stream `v.lines` — never a display row. Wrap continuation rows belong
to the same line; `lineStart[curLine]` is its first display row; resize
re-anchors for free. Fold entries (`Line.Fold > 0`) are skipped: the cursor
only ever rests on a real row. After `f`/`ctrl+w` rebuild `v.lines`, the
cursor re-anchors by its row's `(LeftNo, RightNo)` (first row whose numbers
match, else clamped), independently of the focused change block `cur`; the
view scrolls to the re-anchored cursor ONLY if the cursor was visible before
the toggle (a free-scrolled view keeps its place, as arrows already allow).
On open the cursor sits on the first row of the focused change, else line 0.

**Phase 1 contract.** `func (v *diffView) cursorRow() (textdiff.Row, bool)`
returns the row under the cursor (Kind, LeftNo, RightNo). Notes anchor on
`RightNo` (new side), or `LeftNo` on a `Del` row.

**Keys — one rule.** Arrows and the wheel move only the viewport; the cursor
may scroll off-screen and the header keeps naming its line. `j`/`k` move the
cursor one line and scroll minimally so it stays inside `[offset,
offset+body)`. `pgup`/`pgdn`, `home`/`end` and `n`/`p` (and their wraps,
`N`/`P` file steps) move the cursor as well: `focusBlock` sets it to the
block's first row, page keys move it by one body of DISPLAY rows
(`lineStart[curLine] ± body`, then the owning line — logical lines would
overshoot in wrap mode), home/end put it on the
first/last row. `scrollBy`'s `deriveOrdinal` resync stays as is (cursor and
`cur` are independent). `z` cycles the cursor line's viewport position:
center → top → bottom (Emacs recenter order; `z` is unbound inside the diff
layer, and hunk ships align with no default key). A left click on a body row
places the cursor on that row's line (wheel unchanged). The three alignments
are also `.`-menu rows.

**Marker.** `[ui] diff_cursor = "row" | "number" | "off"`, default `"row"`
(zero/empty = unset in the overlay; a `settingDoc` + layers test like
`diff_syntax`). `row` paints a background under both panes of the cursor's
display rows — syntax colours survive because `syntaxStyle(base, c)`
inherits the base; gap filler and fold separators are never marked. `number`
highlights only the gutter numbers. `off` draws nothing; the cursor still
works for `e` and notes. A `.`-menu row cycles the style for the session
(`m.diffCursor`, the `m.diffPartial`/`m.diffLong` pattern). The header gains
`line N` beside `change X/N`: the new-side number, or the old-side number on
a `Del` row. `diffPaneLines` takes the cursor display-row range as a
parameter; the history view's embedded pane passes none and gets no marker.

**`e` opens the editor at the cursor line.** One line-aware argv builder,
`editorCommandAt(editor, absPath, line)`, shared by both editor paths (the
live `editFileCmd` and the read-only `openInEditorCmd`/`viewExternalCmd`,
whose `editorViewMsg` carries the line). Program name = basename of the
first field, `.exe`/`.cmd` stripped, lower-cased: `vim`/`nvim`/`vi`/`nano`/
`emacs`/`micro`/`kak` → `+N path`; `code`/`code-insiders`/`codium`/`cursor`
→ `--goto path:N`; `hx`/`subl`/`zed` → `path:N`; anything else → `path`
(no line). `line <= 0` = plain path. Which file: `rev != ""` → read-only temp
of `rev:path` at the line; `rev == ""` → live edit of the working-tree file.
The staged diff (HEAD → index) also has `rev == ""`; its new side is the
index, so the line is an approximation of the working file — stated in help.
Line rule: the cursor row's `RightNo`; on a `Del` row the next row with
`RightNo > 0`, else the last such row. `e` is absent on `compare` views (no
single file). `.`-menu row "Open in editor at line N".

**Not in this phase.** No web cursor (notes add one with note rows). No
per-gap fold toggle. No line selection/copy (phase 5).

**Convention tax.** Every new string in all four bundles; `diffHintFor`
grows `[j/k] line  [z] align  [e] edit`; help.go Diff-view rows; new footer
bindings get `actionMenuLabel` cases; `config` settingDoc + `TestUIDiffCursorLayers`;
CHANGELOG + README (diff view paragraph + `## Configuration`).

### 4.2 Conflict-resolver syntax colouring (phase 5 item; approved 2026-09-08)

The hunk picker keeps its own per-side sanitized caches (`sanLit`/`sanCur`/
`sanInc`) and renders through the two-column cell primitive (`winCell`), so
phase 4 never reached it. **Lexing:** when the picker opens, build two
full-file texts — the current-side file (literal context + every block's
`Current` lines) and the incoming-side file (context + `Incoming`) — and lex
each once with `syntax.Detect(path)`/`syntax.Lex`, under the same
`[ui] diff_syntax` switch and `domain.MaxSyntaxBytes` cap as the diff; in the
loader's goroutine where one exists, else synchronously (milliseconds for
ordinary files). **Rendering:** the caches gain a per-line class mask
produced by `sanitizeCell` (so tabs stay aligned); `winCell` gains `cls`
and the two-column renderer paints runs after slicing with the same
post-slice painter the window primitive uses (`styledRuns`, base = the
cell's style). The output pane reuses the masks of the lines it was
assembled from (no re-lex per pick). The cursor row stays plain
reverse-video (the reverse-video ruling from the blame work).

### 4.3 In-view text search (phase 6; approved 2026-09-08)

**One helper, four hosts.** `internal/tui/textsearch.go`: a search state
(query, typing, direction, matches `[]match{row, side, start, end}`, cur)
plus pure functions — find matches over a host's lines (case-insensitive
substring, like every other gg search), next/prev with wrap-around, a
header badge (`/foo  3/12`, `@foo` for backward). Hosts: diff view, blame,
View file preview, conflict resolver.

**Keys (decided by the user):** `/` opens a forward search, `@` a backward
search; both search incrementally from the cursor as you type; `enter`
keeps the query, `esc` cancels. With a query active, `]`/`[` step to the
next/previous hit (wrap-around); `n`/`p` keep their change/region meaning;
`esc` clears the query first, then closes the view on the next press.
History: one shared ring scope for the four readers, recalled with
`alt+↑/↓` like every other search field.

**What a hit does per host.** Diff view: both sides are searched; the line
cursor moves to the hit, the view scrolls to it and, in scroll mode, pans
to the hit's column. Blame and preview: code lines only (gutter excluded).
Conflict resolver: current and incoming candidate lines in document order;
the 2D cursor (block, side, line) jumps to the hit; the output pane is not
searched.

**Hit colouring.** Hits paint like word emphasis, the current hit brighter
(distinct style). The diff pane takes them as extra spans; the window
primitive (`winRow`) and the picker cells (`winCell`) get a small
post-slice span painter — the same mechanism as the class mask, so the
offsets are already known per mode.

**Not included.** Regex, whole-word, the web UI (the browser has find).

### 4.4 Phase 1 design: notes core (approved 2026-09-08)

The persisted note record, its store, the domain queries that resolve notes
against an open diff, and the TUI/web rows. No CLI, MCP, `--hunk N`,
`gg review --notes` or skill: those are phase 2 and consume this phase's
domain surface unchanged.

**Record** (`model.Note`, `internal/model`, engine-free):

| Field | Type | Meaning |
|---|---|---|
| `ID` | string | random 8-hex id, unique per repo store |
| `ParentID` | string | thread parent (a reply); empty for a root note |
| `Source` | `NoteSource` (`user` / `agent`) | who authored it; `a` toggles the agent layer |
| `Author` | string | free label (git user.name for the TUI, the tool/model name for agents) |
| `Address` | `model.FileAddress` | worktree state or commit + path; `Path` in git slash form |
| `Side` | `NoteSide` (`old` / `new`) | which side of the diff the range indexes |
| `Range` | `[2]int` | 1-based inclusive line range on that side |
| `ContextHash` | string | fingerprint of the anchored lines (below) |
| `Summary`, `Rationale` | string | the note; rationale optional |
| `Tags` | []string | optional |
| `Confidence` | float64 | 0 = unset; agents may fill it |
| `Created`, `Updated` | time.Time | UTC |

A reply inherits its parent's `Address`, `Side`, `Range` and `ContextHash`
at creation time and is re-anchored with the parent. Resolution (below) is
computed, never stored.

**Old-side base.** `Address.State` names the pair of texts the note was
made against, matching the diff view that created it: `StateUnstaged` =
index → working file (the Files panel diffs index→worktree), `StateStaged`
= HEAD → index, `StateUntracked` = nothing → working file, `StateCommitted`
= first parent → commit, `StateShelf` = nothing → shelf blob. Within the
working tree a note is shown for a path regardless of the stored state
(fingerprint re-anchoring absorbs the line shift); the state matters to the
sweep, which reads exactly that pair. A note on a Del row (no new-side line)
anchors on the old side; everything else anchors on the new side, per
`diffView.cursorRow()`'s contract (§4.1).

**Fingerprint.** `ContextHash` = hex sha256 of the anchored lines, each
trimmed of leading/trailing whitespace, joined with `"\n"`, no trailing
newline. Fixed now so phase-2 hunk anchors (a range of several lines) reuse
it unchanged.

**Resolution** (`NoteStatus`: `active` / `stale` / `orphaned`), computed
when a diff is opened or refreshed, against that diff's side texts:

1. hash matches at the stored `Range` → `active`;
2. the same trimmed line sequence is found elsewhere on that side (first
   occurrence scanning from the stored start outward) → `active` at the
   moved range; the move lives in the resolved view only, the store is
   untouched (reads never rewrite);
3. not found, but the file/commit still exists → `stale`: drawn greyed at
   the stored range clamped into the side's line count;
4. the target is gone (path absent on that side, commit unreachable) →
   `orphaned`: hidden.

The startup sweep drops expired, orphaned and stale notes (the decided
rule). Consequence: editing the annotated line itself makes the note stale;
it stays visible for the rest of the session and is dropped on the next gg
start.

**Store** (`internal/notes`, owned by `domain`, frontends never import it):

- `Store` interface: `Load() ([]model.Note, error)`,
  `Put(model.Note) error` (add or replace by ID), `Remove(id) error`,
  `Sweep(keep func(model.Note) bool) (dropped int, err error)`; all
  mutations take a `Policy{MaxEntries int}` set on the store.
- `FileStore(root)`: one `notes.toml` under
  `<state>/gg/notes/<repoKey>/` (`domain.notesBaseDir()` mirrors
  `bookmarkBaseDir()`; `repoKey` reused). Temp-file + rename like the
  bookmark store, PLUS — new to this codebase — a cross-process lock:
  `notes.toml.lock` created O_EXCL, retried for up to ~2 s, a lock older
  than 30 s is treated as stale and replaced; and a process-local mutex so
  the sweep goroutine and a `c` keypress in the same process serialise.
  Every write re-reads the file under the lock, applies the mutation, then
  enforces `MaxEntries` oldest-`Created`-first (a dropped root takes its
  replies with it), then rewrites. `Load` never writes.
- Clock seam: `notes.Now` package var (like `snapshotNow`) so expiry tests
  are deterministic.
- Config: `[notes] max_age_days = 30` (`<= 0` keeps forever — the code
  treats `0` and `-1` alike; the spec's `0` wording stands) and
  `[notes] max_entries = 2000` (`<= 0` = uncapped); `NotesConfig` struct,
  overlay, two `settingDoc` rows, `"notes"` added to the template section
  loop, `TestNotesLayers`.

**Sweep.** `domain.Service` starts `sweepNotes` in a goroutine when a
frontend constructs the service (TUI, web; phase 2 adds the CLI verbs). It
loads the store, resolves every note (file/commit existence + fingerprint
search over the side text read via the git verbs), and calls `Sweep` with
`keep = !expired && status == active`. It runs at most once per process and
never blocks a read; failures are logged to the session error ring, not
surfaced.

**Domain surface** (`internal/domain/notes.go`):

```go
type ResolvedNote struct { Note model.Note; Status model.NoteStatus; Range [2]int; Replies []ResolvedNote }
func (s *Service) NoteAdd(ctx, n model.Note) (model.Note, error)      // fills ID/Created/Updated/ContextHash from the diff
func (s *Service) NoteEdit(ctx, id, summary, rationale string) error
func (s *Service) NoteReply(ctx, parentID string, n model.Note) (model.Note, error)
func (s *Service) NoteRemove(ctx, id string) error                     // a root takes its replies
func (s *Service) NotesFor(ctx, addr model.FileAddress, d Diff) ([]ResolvedNote, error) // roots with threaded replies, sorted by side/line
func (s *Service) NoteCounts(ctx) (NoteCounts, error)                  // {ByPath map[string]int (worktree notes), ByCommit map[string]int}
```

`NoteCounts` is cached and invalidated by any note mutation; the Files and
Commits row painters read it and never touch the store. Note mutations are
domain methods like bookmarks, not engine ops: the TUI registers a
`srcNotes` refresh source (invalidating the open diff view, Files and
Commits) and fires it after each mutation; `opAffectedSources` is not
involved.

**TUI** (diff view):

- Note rows are synthetic full-width display rows appended after their
  line in `relayout` (`dRow.note *ResolvedNote`, like the fold separator),
  rendered `◆ <author>: <summary>` with the rationale on a second row when
  present, replies indented two cells, stale rows greyed, agent-sourced
  rows hidden while the agent layer is off (`a`; user notes always show).
- The cursor never rests on a note row: `cursorDispRange` ends at the first
  note row of the line, a click on a note row places the cursor on the
  owning line, `pageCursor`/`cursorVisible` count display rows as before.
- Under a fold in partial mode the `◆` marker goes on the fold separator;
  `}`/`{` onto a folded note expands the view (as `f` does) and lands.
- Keys: `c` add at cursor (popup: summary + rationale textfields, reusing
  the commit popup's title/description pair; empty summary = cancel), `E`
  edit the root note nearest above the cursor, `R` reply to it, `a` toggle
  the agent layer (session-scoped), `}`/`{` next/previous annotated line
  with the existing two-press `fileArm` step to the next/previous file
  that carries notes. "Delete note" is a `.` action-menu row (no key).
  Footer, help, four i18n bundles, `actionMenuLabel` cases, and
  `advertise-features-in-help-and-footer`.
- Files-panel rows and Commits-panel rows get a trailing `◆N` badge from
  `NoteCounts` (worktree notes by path; commit notes by sha).

**Web:** `GET /api/notes?path=&rev=` (resolved, threaded), `POST
/api/notes/{add,edit,reply,remove}` with allowlisted wire values, a
`notes` SSE event after each mutation, `<tr class="note">` rows in
`diffHTML` (keys `c`/`E`/`R`/`a`/`}`/`{` mirrored), a `◆N` count in
`renderFiles`. Per-id hidden rule (`#id.hidden`) respected. Heads-up for
phase 2: the existing `/api/diff` `hunks` meta numbers textdiff blocks for
the web hunk picker, not git `@@` hunks — `--hunk N` must not reuse it.

**Testing:** store round-trip, cap and lock tests (`t.TempDir`, injected
clock); resolution table tests (active / moved / stale / orphaned); domain
tests on a real repo (`newTestRepo`); TUI tests for relayout note rows,
cursor skipping, `}`/`{` with fold expansion and file stepping, the popup
flow; web handler tests; an e2e scenario is deferred to phase 2 when the
CLI exists.

## 5. How hunk highlights syntax, and what gg should do

Hunk does not own a highlighter. `@pierre/diffs` wraps **Shiki** (TextMate
grammars; hunk asks for the `shiki-wasm` preferred highlighter) with themes
from `@shikijs/themes`;
`syntax_scopes` in a custom theme are raw TextMate scope selectors. Language
comes from `getFiletypeFromFileName` plus a small reserved override table
(`.mts`, `.cts` → typescript) and extension-registered matchers. Two paths:

- **Full-source highlighting** when both sides can be fetched: lex each whole
  side once so multi-line lexer state (block comments, template strings)
  survives, then remap tokens onto diff lines. Patch-fragment lexing is the
  fallback, with identical context lines aliased between sides.
- **Caching and offload**: results cached by (language, theme, appearance,
  file metadata); `--fast` moves lexing to a Bun worker (disabled in
  compiled Windows builds); tokenizing stops at 1000 chars per line and word
  diff at 10k. Word-level emphasis is Pierre's `word-alt` line-diff, composed
  on top of the syntax colors.

**Plan for gg**: one engine in `domain`, two renderers.

- New pure package `internal/syntax` (DAG leaf): `Lex(lang, []byte) [][]Tok`
  returning per-line token runs `{Start, End (rune offsets), Class}`, plus
  `Detect(path, head []byte) lang`. Candidate library: `alecthomas/chroma`
  v2 (pure Go, ~250 lexers, `lexers.Match`/`Analyse`, built-in styles). It
  is **not** in go.mod today, so the first task is a spike: lexer coverage
  for the user's repos (Go, TS, C/C++, Python, YAML…), full-file lex
  throughput on a 20k-line file, and whether stateful lexing across lines
  holds.

  **Spike result (2026-09-08, chroma v2.27.0, throwaway program in the
  session scratchpad): PASS.**

  | Check | Result |
  |---|---|
  | Lexers / styles | 297 / 74; every one of 41 probed filenames matched, incl. `Dockerfile`, `CMakeLists.txt`, `go.mod`, `.mts`, `.cts`, `.tf` |
  | Throughput | ~1 MB/s for Go/TS/JS: 20k-line Go file (600 KB) in 646 ms; a 3.5k-line Go file in 123 ms; a 2.6k-line TSX file in 56 ms; markdown is slowest at 0.5 MB/s |
  | Tokens cross lines | yes (block comments, raw strings arrive as one token), so per-row slicing is required |
  | Line-by-line lexing | loses state: the middle line of a block comment lexes as `NameOther`; whole-file lexing is mandatory, as planned |
  | Binary cost | a spike binary with all lexers is 8.3 MB; gg is 16.5 MB today, so expect roughly +7 MB unless lexers are trimmed to a curated set |
  | Dependencies | pure Go (`chroma/v2` + `dlclark/regexp2`), no cgo |

  Consequences for the design: lex each side once off-thread, cache with
  the diff, and slice tokens per row by rune offset. A 20k-line file costs
  ~0.6 s per side, so the "render plain, restyle when tokens arrive" rule
  matters. Decide at implementation time whether to embed every lexer or a
  curated subset (chroma lexers are registered individually, so trimming is
  a build-list choice, not a fork).
- `domain.Diff` gains optional `LeftTok, RightTok [][]syntax.Tok` sidecars
  (per row, computed from the full sides the differ already read). Cached
  with the diff under the same byte budget; skipped above the existing
  `TooLarge` guard and for `Binary`.
- TUI: `styledRuns` becomes a two-mask compositor: syntax class → foreground
  from a small style table, emphasis span → background, sanitize/wrap
  unchanged. Off-thread via the existing diff `tea.Cmd`; the view renders
  plain first and restyles when tokens arrive, so a slow lexer never blocks
  opening a diff.
- Web: `/api/diff` rows carry the token runs; `diffHTML` wraps them in
  `<span class="tok-<class>">`, colours from CSS variables, so the theme
  follows the page's light/dark mode. No external CDN.
- Config: `diff.syntax = auto|off` and `diff.syntax_theme`, each with a
  `settingDoc`; TUI Settings popup rows follow.

## 6. Phased plan

Each phase is one feature branch off `main`, worktree, TDD, e2e scenario
where the CLI changes. Convention tax per phase is listed so estimates are
honest: every TUI string needs a key in all four i18n bundles, new option
values need an `i18n_display.go` case, new footer bindings need an
`actionMenuLabel` case, new ops go in `opAffectedSources`, each config key
needs a `settingDoc`, CLI changes update `using-gg.md` + `agentskill.Version`
+ CHANGELOG/README.

| # | Phase | Scope | Depends on |
|---|---|---|---|
| 0 | Diff line cursor | `curLine`, `j`/`k`, marker style, align top/center/bottom, `e` open editor at line | – |
| 1 | Notes core | `model.Note`, `internal/notes` store, domain queries + reconciliation, TUI rows + `c`/`E`/`R`/`a`/`}`/`{`, web rows, files/commits counts | 0 |
| 2 | Agent lane | `gg note …` verbs (hunk v1 JSON), MCP note tools, `gg review --notes`, `reviewing-with-gg` skill + `gg skill path`, `gg init` installs it | 1 |
| 3 | Live steering | session inbox + `gg session navigate/reload/focus`, TUI watcher with poll fallback, web POST endpoint, attention marks | 1 (2 for the skill text) |
| 4 | Syntax highlighting | chroma spike → `internal/syntax`, domain sidecars, TUI compositor, web classes, config keys | – (parallel with 1–3) |
| 5 | Viewer parity extras | **conflict-resolver syntax colouring** (the hunk picker renders its own cells; give it the same `syntax` runs + `styledRuns` as the diff pane), TUI unified toggle, `v`/`y` line selection + copy (folds in the "text operations" backlog item), hunk-header + line-number toggles, tab width, watch-reload of an open diff, `gg pager` / `gg diff --view` / patch-from-stdin viewer, move detection | 0 |
| 6 | In-view text search | `/` incremental search inside the full-screen readers: diff view (both sides, jumps the line cursor), blame, the View file preview, and the conflict resolver; shared search-state helper (query, match list, next/prev with wrap, match count in the header, matches emphasised like word spans); keys DECIDED 2026-09-08 (user): `/` opens a forward search, `@` a backward search (both incremental from the cursor; `enter` keeps the query, `esc` cancels), `]`/`[` step to the next/previous hit with wrap-around — `n`/`p` keep their change/region meaning; `esc` with an active query clears it first; the conflict resolver's output pane is not searched; reuses the search-history dropdown (`alt+↑/↓`) | 0 (cursor) |

Phase 6 (in-view search) and the conflict-resolver colouring in phase 5 were added on the user's request on 2026-09-08 after phase 0 shipped. Phases 0–2 are the shortest path to "an agent can leave notes in gg and a
human reads them in the TUI". Phase 4 is independent and can run in
parallel. Phase 3 is where hunk's daemon complexity lives; the inbox design
keeps it small. Phase 5 is a grab bag to schedule item by item.

## 7. Out of scope, deliberately

- **TypeScript extension host**: different runtime, huge surface, and gg's
  extension story is external tools + MCP.
- **STML rich note markup**: experimental in hunk; plain summary + rationale
  is the contract agents use.
- **jj / sapling adapters**: gg shells out to git only.
- **Multi-file stream view**: cross-file hopping covers the review use case.

## 8. Open questions

Decided 2026-09-08:

1. ~~Hunk addressing~~ — `--hunk N` IS implemented, git `@@` numbering
   (§4.3), plus `gg diff --hunks`.
2. ~~Steering channel~~ — inbox file watched by the TUI.
3. ~~Notes location~~ — shelf-style XDG store with the branch-versions
   expiry rule (§3.2).
4. ~~Skill~~ — separate `reviewing-with-gg` file.
5. ~~Highlighting spike~~ — passed; result recorded in §5.

6. ~~`notes.toml` growth~~ — one file; startup-goroutine sweep (expiry +
   orphans) + cap on write (§3.2 "Growth").
