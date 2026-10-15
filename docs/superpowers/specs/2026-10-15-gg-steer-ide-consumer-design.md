# gg live steering — the IDE as a third consumer

**Status:** draft for review · **Date:** 2026-10-15 · **Audience:** gg (one
small change) and a reader that is not gg — the `gg-notes` IDEA plugin —
which becomes a target of `gg session …`. Builds on the notes-store specs
(`2026-10-15-gg-notes-store-format-design.md` for the anchor grammar and
the markdown subset) but is independent of them: nothing here touches the
store.

## 1. Purpose

An agent presents a result by STEERING what the user looks at: "open
`parse.go` at 40–60", "mark these lines", "here is an overview — walk it".
gg's channel for that is the live-steering inbox: `gg session <verb>` posts
a small JSON command file; the running TUI (or the `gg web` page, over
HTTP) applies it and replies. The agent's skills (`using-gg`,
`gg-overview`) already say when to do this.

This spec makes the IDE a third consumer of the SAME commands, so an agent
in a terminal next to IDEA — or started from it — can say
`gg session navigate gg://…:40-60 --to ide` and the editor jumps there,
`gg session overview add --to ide` and the plugin's tool window shows the
overview, with no new protocol and no new skill vocabulary beyond
`--to ide`.

Rulings recorded (2026-10-15):

- **The IDE is reached only explicitly** (`--to ide`, or the new
  `GG_SESSION_TO=ide` default, §4). Without either, routing is exactly what
  it is today (TUI / web); an `ide.json` presence never changes it.
- The channel stays a directory of files; no port, no server in the IDE.

## 2. The inbox (what exists; the IDE's addition in bold)

One inbox per WORKTREE:

```
<state>/gg/sessions/<EncodeRepoKey(common dir)>/steer/<EncodeRepoKey(worktree)>/
    tui.json  web.json  ide.json      presence; liveness = a FRESH MTIME
    cmd-<id>.json                     a command for the TUI, temp+rename, unlinked by the TUI
    reply-<id>.json                   the TUI's answer, unlinked by the CLI that read it
    ide/                              ← NEW: the IDE's own command/reply files
        cmd-<id>.json  reply-<id>.json
```

- `<state>`: `$XDG_STATE_HOME` → Windows `%LocalAppData%` → `~/.local/state`
  (part 1 §3.1's rule).
- `EncodeRepoKey(p)` = `filepath.Clean(p)` with every `/`, `\` and `:`
  replaced by `-`. Common dir = `git rev-parse --path-format=absolute
  --git-common-dir`; worktree = `git rev-parse --show-toplevel`. Real
  example: common dir `/home/homeend/.claude/jobs/2620a6cb/tmp/cmprepo/.git`
  → `sessions/-home-homeend-.claude-jobs-2620a6cb-tmp-cmprepo-.git/steer/-home-homeend-.claude-jobs-2620a6cb-tmp-cmprepo/`.
  Windows (derived, unconfirmed on disk): `C:\src\x\.git` → `C--src-x-.git`.
- Every worktree of a repository has its own inbox (unlike the notes
  store): a command about worktree A reaches the session showing A.
- `[ui] agent_steering = "off"` in gg's config turns gg's side off (no
  presence, no posting). The IDE cannot read that; it publishes regardless
  and simply receives nothing.

### 2.1 Why a sub-directory

The TUI treats the inbox as ITS queue: it `Discard`s every `cmd-*.json` /
`reply-*.json` at startup and, on every watcher wake, `Drain`s — reads and
UNLINKS — every `cmd-*.json` it finds. A command meant for the IDE in the
same directory would be eaten by a live TUI before the plugin saw it. The
web page avoids this only because the CLI reaches it over HTTP. The IDE's
files therefore live in `<inbox>/ide/`: gg's listing skips directories and
its `Discard` touches top-level files only, so every gg shipped to date
leaves the sub-inbox alone. The presence file stays at the top so
`gg session status` and the routing see it beside the others.

### 2.2 Presence (`ide.json`)

```json
{"pid": 12345, "worktree": "/home/dev/src/gate", "started": "2026-10-15T12:00:00Z", "ide": "IntelliJ IDEA 2026.2", "plugin": "0.2.0"}
```

`pid`, `worktree`, `started` as the TUI writes them (`worktree` = the
top-level path the inbox was derived from, exactly as hashed); `ide` and
`plugin` are the IDE's additions (display only; gg ignores unknown keys).
The plugin **touches the file every 1 s** (set its mtime; rewrite it when
that fails), and gg counts it live while the mtime is **under 5 s old**.
Remove it on project close; a crash leaves a file that goes stale by itself.
One presence per inbox: a second IDE window on the same worktree takes the
file over (last toucher wins) — acceptable for v1.

### 2.3 Command and reply files

`ide/cmd-<id>.json`, written by `gg session` temp+rename; `id` =
`<unix-nanoseconds>-<pid>` (sorts by post time). The plugin:

1. watches `<inbox>/ide/` (a directory watcher; poll every 500 ms as a
   fallback — the CLI's 25 ms reply poll tolerates either);
2. on wake, lists `cmd-*.json` in name order, and for each: deletes a file
   over 512 KiB unread; reads, unlinks, parses; drops a command whose `id`
   is empty or not a single path component (its reply could escape the
   inbox);
3. applies it off the EDT, then — only when `wait` is true — writes
   `ide/reply-<id>.json` temp+rename. The CLI waits **2 s**, then prints
   "queued" and exits 0: a late command MUST still be applied, its reply
   is simply never read (the CLI deletes replies it reads; the plugin
   sweeps `ide/` at project open the way gg's `Discard` does).

## 3. The wire (what exists, unchanged)

### 3.1 Command

```json
{"id":"…","cmd":"navigate","file":"src/a.go","target":{"state":"commit","commit":"<40-hex>"},
 "line":{"side":"new","no":40,"end":60},"worktree":"/home/dev/src/gate","wait":true}
```

| field | meaning |
|-------|---------|
| `cmd` | the verb (§3.3) |
| `file` | repo-relative, git slash form |
| `target` | where the file lives: `state` ∈ `unstaged` \| `staged` \| `untracked` \| `commit` (+ `commit` 40-hex) \| `preview` (+ `source`, `target` branch names) \| `ref` (+ `ref` name) \| `pair` (+ `a`, `b`); absent = the working tree |
| `commit` | navigate: reveal this commit, no file |
| `line` | `{side: "new"\|"old", no, end?, asked?, anchor?, matches?}`; absent = open the file, leave the caret alone; `end > no` = select the range; `asked`/`anchor`/`matches` describe a fingerprinted link whose text moved (`anchor` ∈ `same`\|`moved`\|`changed`) |
| `step` | navigate: `next_note` \| `prev_note` |
| `background` | navigate: open without showing |
| `file_id` | an open file's id `f<n>` (file_focus); `file` names one by path instead |
| `side`, `start`, `end`, `tone` | highlight: `side` ∈ `new`\|`old`, 1-based inclusive lines, `tone` ∈ `info`\|`warn`\|`error` |
| `note_id`, `summary`, `rationale`, `author`, `meta`, `display` | the note verbs (§5) |
| `title`, `text` | the overview verbs (§5) |
| `hint_kind`, `hint_id` | the UI surface a link was copied from (`bookmark`, `shelf`, `stash`, `preview`, `version`, `view`); never changes WHERE a navigate lands |
| `worktree` | the checkout the sender runs in; a consumer showing another checkout refuses (§3.4) |
| `wait` | true = the sender awaits a reply |
| `sources`, `dir`, `panel` | reload / focus — TUI only (§3.3) |

### 3.2 Reply

```json
{"id":"…","ok":true,"detail":"opened src/a.go:40-60 at line 40"}
{"id":"…","ok":false,"error":"no open file f9"}
```

`detail` / `error` are English protocol prose an agent reads; `files`,
`notes`, `overviews` carry the list verbs' answers (§5 shapes). The TUI's
exact prose per verb is in §3.3 and pinned by the replay corpus (§7); the
IDE uses the SAME words where the situation is the same, and its own only
for refusals the TUI never gives.

### 3.3 Verbs and what the IDE does

| verb | IDE v1 | reply |
|------|--------|-------|
| `navigate` with `file` | open the file at the target (`unstaged`/`untracked`: the working file; `staged`: the index's copy, read-only; `commit`: the file at that commit, read-only, as the IDE's VCS layer shows a revision), caret at `line.no`, `no..end` selected; `background`: open the editor tab without activating it; the file is added to the open-files list (§5) | `opened <file>:<no>` / `opened <file>:<no>-<end>` / `opened <file>` (no line); a line past the end lands on the last line: `opened <file>:<asked>; clamped to line <last>` |
| `navigate` with `commit` only | reveal the commit in the IDE's git log (or show its details) | `revealed commit <sha7>` |
| `navigate` with `step` | move to the next/previous temporary note of the file in front (§5) | `opened <file>:<n>` / error `no next note in this diff` · `no previous note in this diff` |
| `navigate` with `line.side == "old"` | refused unless a diff view of that file is open: `error: the IDE shows no old side` | — |
| `highlight` | a persistent line marker (gutter + background in the `tone` colour) on `start..end` of `file` on `side` (`new`; an empty `side` is `new`); cleared by `highlight_clear` or when the file closes | `marked <file> new:<a>-<b> as <tone>` |
| `highlight_clear` | remove every highlight; with `file`, only that file's | `cleared every mark` / `cleared the marks on <file>` |
| `files` | the open-files list (§5) | `files: [{id, path, source, title?, rev?, line, state, notes}]`, most recently shown first; `detail: "no open files"` when empty |
| `file_focus` | bring `file_id`/`file` to the front; with `line`, caret there | `focused <path>` — `focused <path> at line <n>` when the command named a line (an overview: `focused <title>`) / `no open file <name>` |
| `note_add/list/show/rm` | §5 | §5 |
| `overview_add/set/list/show/rm` | §5 | §5 |
| `focus`, `reload`, `serve` | refused: `error: <verb> is a gg TUI command` | — |
| `target.state` ∈ `preview`, `ref`, `pair` | refused in v1: `error: the IDE cannot show a <state> target` | — |
| unknown `cmd` | refused: `error: unknown command "<cmd>"` (quoted, as the TUI says it) | — |

### 3.4 Worktree guard

A command carries `worktree` (the sender's checkout, or a link's). The IDE
applies it only when that path is one of the open project's git roots
(compare with the part-1 identity rule: cleaned, slash form, case-folded on
Windows/macOS); otherwise `error: the IDE shows <root>, not <worktree>` —
the TUI's refusal in the same shape. Empty `worktree` = apply.

## 4. Routing in gg (the change)

- `steer.IDEPresence = "ide.json"`, `steer.IDEInbox = "ide"`.
- `gg session <verb> --to ide`: resolve the inbox as today (`$GG_INBOX`
  first, else this worktree's / the link's checkout's), require a live
  `ide.json` there (`no IDE for this worktree` otherwise, exit 1), post to
  `<inbox>/ide/`, await the reply there with the verb's usual wait, print
  the reply the usual way.
- **Never implicit.** With no `--to`, `routeFor` ignores `ide.json`; `--to
  tui|web` are unchanged.
- **`GG_SESSION_TO`** (new): when set to `tui`, `web` or `ide` and the
  command has no `--to`, it is the `--to`. A terminal or agent started from
  the IDE exports `GG_SESSION_TO=ide` — and ONLY that: `GG_INBOX` means "an
  agent inside gg" to the rest of gg (`gg pr send` refuses, mutating verbs
  post reload notices) and must not be set by an IDE. The cwd's inbox is
  enough. An explicit `--to` always wins; `status` ignores the variable. gg
  strips `GG_SESSION_TO` from the environment of every console it starts,
  so a TUI launched from an IDE terminal keeps its agents on itself.
- `gg session status`: a third line `ide: pid <pid>, <ide> <plugin> (since
  <started>)` when live (JSON: `"ide": {"pid", "started", "ide", "plugin"}`
  or `null`); exit 0 when ANY of the three is live.
- Skills: `using-gg` gains `--to tui|web|ide` in every `gg session` usage
  line and one paragraph: "the IDE is a target only when asked: pass `--to
  ide` when the user works in the IDE (or `GG_SESSION_TO=ide` is set);
  `gg session status` lists it"; `gg-overview` says the same where it
  chooses between TUI and web. `Version` bump, dogfood copy, CHANGELOG.

## 5. The in-memory model the IDE keeps

A copy of gg's session memory (`internal/agentdocs`), per worktree, gone
when the project closes. Ids are the plugin's own: open files `f<n>`, notes
`t<n>`, counters never reused in a session.

**Open files** (`files`, `file_focus`): every file a navigate opened or a
note was added to, newest-shown first; `source` ∈ `worktree` \| `commit` \|
`overview` (`shelf`/`result`/`review` are TUI kinds); `state` `shown` when
its editor tab is active, else `background`; `notes` = its note count. gg's
TUI caps the list and closes the oldest background file past the cap
(`detail` adds `; closed <path> (…)`); the IDE has its own tabs and keeps
no cap — the list is whatever is open.

**Temporary notes** (`note_add <file>:<start>[-<end>] --summary … [--rationale …] [--author …] [--meta k=v] [--display json]`):
- placed on the file's lines **as shown now** (the editor's current text —
  the plugin reads the document, not the disk); gg's exact refusals, which
  the IDE repeats: `<path> has no lines to note`, `a line number is 1-based`,
  `the range ends before it starts`, `line <n> is past the end of <path>
  (<len> lines)`, `a note needs a summary`, `the summary is longer than 500
  characters`, `the rationale is longer than 4000 characters`, `<path>
  already carries 50 notes`; meta/display as in the store spec part 2 §6,
  enforced the same way;
- the note keeps the TEXT of its lines; as the user edits, it follows an
  exact-line alignment (a Myers line diff of the text it was aligned to
  against the current text: the note moves when every one of its lines
  survives unchanged and still sits together); otherwise it becomes
  `outdated`, keeps its numbers clamped into the file, and comes back only
  when its text is again at its old place or at exactly ONE place in the
  file (a lone `}` must never adopt another brace). This is gg's
  `agentdocs.reanchor`, NOT the store's hash relocation; the replay corpus
  has cases;
- drawn as read-only boxes beside the lines (the same overlay the overview
  remarks use); `note_list` → `notes: [{id, file_id, path, start, end,
  summary, rationale?, author?, outdated?, meta?, display?}]`; `note_show`
  adds `text` (the lines it sits on now); `note_rm` removes one; `error: no
  note t<n>` / `no open file <name>` when absent.

**Overviews** (`overview_add --title … [--file md] [--background]`,
`overview_set <id> …`, `overview_list`, `overview_show <id>`,
`overview_rm <id>`): an overview is an open file of source `overview`
(id `f<n>`), title ≤ 200, text ≤ 64 KiB, ≤ 20 per worktree; its anchors are
part 1 §6's grammar plus `note:t<n>` (an anchor to a temporary note → that
note's file at its lines), the first 100 count; `unresolved` lists
destinations whose file or note was not found. Refusals, gg's strings:
`an overview needs text`, `the text is over 64 KiB`, `an overview needs a
title`, `the title must be one line of at most 200 characters`, `20
overviews are open; remove one first`, `no overview <id>`. Refusals: `an overview needs
text`, `the text is over 64 KiB`, `an overview needs a title`, `the title
must be one line of at most 200 characters`, `no overview <id>`. `overview_add` shows it in
the plugin's overview tool window (the same renderer as the stored
overviews) unless `background`; `overview_set` replaces text/title in place
(the walk stays on the same anchor); `overview_show` returns `text`;
`overview_rm` closes it. Replies: `showing f<n>` / `added f<n> in the
background` / `no overview <id>`.

## 6. What this does NOT cover

- Writing to the notes store, `gg note`/`gg review`/`gg overview save` —
  those are CLI verbs that write files; the IDE only re-reads the store
  (notes-store specs).
- The TUI-only verbs (`focus`, `reload`, `serve`) and TUI-only sources
  (`shelf`, `result`, `review`).
- The web page's HTTP delivery; the IDE never serves.
- Multiple IDE windows on one worktree; agent consoles inside the IDE.

## 7. Conformance material (ships in gg)

Under `docs/specs/gg-steer/`:

- `command.schema.json`, `reply.schema.json`, `presence.schema.json`
  (lenient: unknown keys tolerated) — pinned against gg's `steer` types by a
  test that round-trips every example through them.
- `steer-cases.json` — a replay corpus: a worktree fixture (a handful of
  files with known content) + `[{command, reply}]` pairs including every
  refusal above. gg's TUI handler is tested against it headlessly; the
  plugin runs its handler against the same file.
- `reanchor-cases.json` — `{old, cur, note{start,end}, expect{start,end,outdated}}`
  cases run against `agentdocs.reanchor` and portable to the plugin.
- `repokey-vectors.json` — `EncodeRepoKey` inputs/outputs (the real path
  above; a Windows one derived).

## 8. Acceptance (the plugin)

With the plugin's project open on `~/git-focus` and no gg TUI running:
`gg session status` lists `ide:`; `gg session navigate
gg://git-focus/src/main/kotlin/dev/gitfocus/worktrees/WorktreesList.kt@<sha>:20-28 --to ide`
opens the file at that commit with lines 20–28 selected and replies
`opened … at line 20`; `gg session highlight add …:21 --to ide` marks line
21; `gg session note add WorktreesList.kt:21 --summary "…" --to ide` draws
the box and `gg session note list --to ide` lists it; `gg session overview
add --title "Tour" --file tour.md --to ide` shows the overview in the tool
window with its anchors walking the editor; with a gg TUI ALSO live on the
worktree, the same commands without `--to` go to the TUI and the IDE stays
untouched.
