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
  config key (default 30; `0` keeps forever) with a `settingDoc`; the store
  prunes on load the way versions prune on write. Orphaned notes (anchor
  gone after reconciliation) are dropped in the same pass; `gg note clear`
  remains for explicit cleanup.
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
| 5 | Viewer parity extras | TUI unified toggle, `v`/`y` line selection + copy (folds in the "text operations" backlog item), hunk-header + line-number toggles, tab width, watch-reload of an open diff, `gg pager` / `gg diff --view` / patch-from-stdin viewer, move detection | 0 |

Phases 0–2 are the shortest path to "an agent can leave notes in gg and a
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
