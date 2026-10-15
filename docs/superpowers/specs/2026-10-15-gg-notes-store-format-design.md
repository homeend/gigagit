# gg notes store format — overview records

**Status:** draft for review · **Date:** 2026-10-15 · **Audience:** anyone
writing a READER of gg's notes store that is not gg — the first one is an
IntelliJ IDEA plugin (working name `gg-notes`) built by another agent from
this document alone. It must not need gg's source.

## 1. Purpose and scope

gg lets an agent present a result as an **overview**: a short markdown
document whose links are **anchors** into the code (a file, a line, a
range), plus **remarks** on exact lines. gg stores a permanent overview on
the commit it explains, in a machine-local store. This document specifies
that stored form so another tool can show the same overview the user sees
in gg: list the overviews of a repository, render the document, open a
file at the anchored line, show the remarks beside the lines.

In scope (v1 of this spec):

- locating the store of a repository (§3);
- the file envelope and the record of ONE kind: a commit-level overview
  (§4);
- the overview document (§5), its anchor grammar (§6) and markdown
  subset (§7);
- reader rules: concurrency, errors, versions (§8);
- conformance material gg ships so it cannot drift from this text (§9);
- the handoff to the plugin author (§10).

Out of scope, and deliberately additive later (same files, same envelope,
new record kinds): line notes and review remarks (`tags = ['review']`,
notes with a `Path`), replies (`parent_id`), notes on previews / shelf
entries / working trees (`previews.toml`, `shelf.toml`, `worktrees/`),
and any WRITE. A reader of this spec never writes the store.

### 1.1 Ruling recorded

gg is licensed PolyForm Noncommercial and its source may close. This
document is a deliberately OPEN interface: what it describes may be
implemented by anyone. It is kept in this repository; nothing is published
online as part of this work.

### 1.2 Portability line

What travels between machines and tools: the commit sha, repo-relative
slash paths, 1-based inclusive line numbers, the title, the document.

What is machine-local and MUST NOT be used as identity by another tool:
the note `id` (random per store), the store directory key (§3.2), and the
`gg://…?overview=<id>` link hint. Two overviews are the same overview when
they have the same commit (or PR scope) and the same title — that is gg's
own replace rule (§4.4).

## 2. Terms

- **store** — the per-repository directory gg keeps its notes in.
- **part** — one TOML file of the store; overviews live in `commits.toml`.
- **record** — one `[[notes]]` table of a part.
- **overview record** — a record that holds an overview (§4.3).
- **document** — the JSON text inside an overview record's `rationale`.
- **anchor** — a markdown link in the document whose destination names a
  file, a line or a range (§6). Every other link is text or an http(s)
  link.
- **remark** — an entry of the document's `files[].annotations`: a note on
  exact lines of one file at the commit.

## 3. Locating the store

### 3.1 Base directory

The first rule that applies wins, on every platform:

1. `$XDG_STATE_HOME` is set and non-empty → `$XDG_STATE_HOME/gg/notes/`
2. Windows and `%LocalAppData%` is set → `%LocalAppData%\gg\notes\`
3. otherwise → `<home>/.local/state/gg/notes/` (`<home>` = the user's
   home directory as the OS reports it — on macOS this is
   `~/.local/state/gg/notes/`, not `~/Library`).

### 3.2 Repository key

Each repository has one directory under the base: `<base>/<key>/`.

```
key = lowercase hex of the first 8 bytes of
      SHA-256( clean( git rev-parse --path-format=absolute --git-common-dir ) )
```

- The git output is taken with surrounding whitespace trimmed.
- `clean` is Go's `filepath.Clean`: it removes `.` segments, resolves `..`
  lexically, collapses repeated separators, drops a trailing separator and
  — on Windows only — turns every `/` into `\`. It does NOT resolve
  symlinks, case or 8.3 names, and gg applies NO case-folding here (its
  checkout-path identity rule is not used for this key). On Linux and
  macOS a git path is already clean, so the hashed string is exactly what
  git printed. (gg's chain, verified: `git rev-parse` → trim → Clean →
  Clean again, idempotent → SHA-256 → first 8 bytes.)
- The common dir of a linked worktree is the MAIN checkout's git directory,
  so every worktree of a repository shares one store.

Test vector (Linux, confirmed on disk 2026-10-15):

```
git prints:  /work/gigagit/.git
hashed:      "/work/gigagit/.git"            (18 bytes, no newline)
key:         d10200a3a219735f
store:       ~/.local/state/gg/notes/d10200a3a219735f/
```

Windows: git prints `T:/others/gigagit/.git`; the hashed string is
`T:\others\gigagit\.git` (drive letter as git printed it). This spelling
follows from the rule above but has not been confirmed against an on-disk
store (no Windows repository on the author's machine has a note yet). The
plugin author MUST confirm it with the oracle in §9.3 before relying on it.

### 3.3 Files in the store

| name | meaning for a reader |
|------|----------------------|
| `commits.toml` | commit-level notes — ALL overview records are here |
| `previews.toml`, `shelf.toml`, `worktrees/<key>.toml` | other note kinds; out of scope, ignore |
| `*.lock` | gg's cross-process write lock; never read, never create, never delete |
| `.notes-*.tmp` | a write in progress; ignore |
| `*.corrupt-<unix>`, `notes.toml.migrated-<unix>` | quarantined / converted old files; ignore |
| `notes.toml` | a pre-split store an old gg wrote; gg converts it on its next run. A reader MAY read it with the same envelope rules (format < 3, §8.3) but MUST NOT convert it |

A missing store directory or a missing `commits.toml` means "no
overviews", not an error.

## 4. The envelope: `commits.toml`

### 4.1 File

A TOML 1.0 document. Use a real TOML parser — gg writes strings with the
quote style its encoder picks (today single-quoted literal strings where
possible, e.g. `tags = ['overview']`, and double-quoted basic strings with
escapes when the value needs them, e.g. the `rationale` of an overview).
A text match on one spelling misses the other.

Top level:

```toml
format = 3

[[notes]]
…

[[resolved]]   # thread resolutions; not present in every file; ignore
…
```

- `format` (integer): the store format this file was written in. See §8.3.
  Absent means 1.
- `notes` (array of tables): the records, in no guaranteed order.
- Any other top-level key or any unknown key inside a record MUST be
  tolerated and ignored.

### 4.2 Record fields

Every record has these keys; a reader of overviews needs the starred ones.
Types are TOML types.

| key | type | meaning |
|-----|------|---------|
| `id` ★ | string | 8 lowercase hex chars, unique within this store only |
| `parent_id` | string | present on a REPLY; an overview record never has one |
| `source` | string | `'user'`, `'agent'` |
| `author` ★ | string | the writer's name (for an overview: the agent's name) |
| `side` | string | `'new'` or `'old'`; an overview record has `'new'` |
| `range` | array of 2 integers | `[0, 0]` on a commit-level record |
| `context_hash` | string | `''` on a commit-level record |
| `summary` ★ | string | for an overview: its TITLE (≤ 200 bytes) |
| `rationale` ★ | string | for an overview: the DOCUMENT, JSON text (§5) |
| `tags` ★ | array of strings | `'overview'` marks an overview record |
| `preview` | string | set only on a PR overview (§4.3) |
| `created` ★, `updated` ★ | offset date-time | RFC 3339, UTC, nanoseconds |
| `meta`, `display`, `added`, `scope`, `preview_branch`, `preview_base`, `files`, `link`, `remark`, `remark_fp`, `remark_summary` | — | other note kinds; absent or ignorable on an overview record |
| `address` ★ | table | see below |

`[notes.address]` — written with these exact key spellings (capitalised,
they are the field names of gg's Go type):

| key | type | on an overview record |
|-----|------|-----------------------|
| `Commit` ★ | string | the full 40-hex sha the overview explains |
| `Branch` | string | the branch name recorded at save time, or `''` — informational only (the "└ Overview" row under a branch while its tip is that commit) |
| `Path` | string | `''` (a commit-level note has no file) |
| `State` | integer | `0` (= committed) |
| `Worktree`, `ShelfID` | string | `''` |

### 4.3 What is an overview record

A record is an overview record when ALL hold:

1. `tags` contains the string `overview`;
2. `address.State == 0`, `address.Commit` is a non-empty sha and
   `address.Path == ''` (commit-level);
3. `parent_id` is absent or empty.

Anything else in `commits.toml` is another note kind and is ignored by a
reader of this version of the spec.

Two overviews of the same commit:

- a **commit overview** has `preview` absent/empty;
- a **PR overview** (an overview of a pull request stored on its tip) has
  `preview = '<base>...refs/gg/pr/<n>'` — git's three-dot spelling with
  the base BRANCH NAME on the left and gg's PR ref on the right, `<n>` the
  PR number. Two PR scopes are the same when their `<n>` is the same (the
  base half may move); v1 readers list it under the commit like any other
  overview and MAY show the PR number parsed from the right half.

### 4.4 Uniqueness and replacement

gg's writer rewrites in place an existing overview with the same commit
(or, for a PR overview, the same PR scope) and the same `summary` (title,
compared trimmed), keeping its `id` and `created` and bumping `updated`.
So within one store a (commit, title) pair — or (PR scope, title) — names at
most one overview. A reader MUST key its UI on that pair, never on `id`.

### 4.5 Example — a real record

Written by gg on 2026-10-13 (document shortened):

```toml
format = 3

[[notes]]
id = '7cf0812f'
source = 'agent'
author = 'Claude Opus 5.5'
side = 'new'
range = [0, 0]
context_hash = ''
summary = 'GitFocus tab: branch-first rows'
rationale = "{\n  \"version\": 1,\n  \"title\": \"GitFocus tab: branch-first rows\",\n  \"overview\": \"This merge changes each worktree row …\\n\\n## 1. Row text\\n\\n- [`rowText`](src/main/kotlin/dev/gitfocus/worktrees/WorktreesList.kt:20-28) makes the text of a row. …\",\n  \"files\": [ … ]\n}"
tags = ['overview']
created = 2026-10-13T23:53:56.742993129Z
updated = 2026-10-13T23:53:56.742993129Z

[notes.address]
Worktree = ''
Branch = ''
Commit = 'd5efa035df1f3c4c949040bebb924a35d0ea61a0'
ShelfID = ''
Path = ''
State = 0
```

## 5. The overview document

`rationale` holds UTF-8 JSON text — one JSON object, indented by two
spaces as gg's encoder writes it. Decode it with a JSON parser after the
TOML string has been decoded (two decodes: TOML string → JSON text →
object).

gg stores the document in its **canonical** shape — strictly smaller than
what an agent may hand `gg overview save` (which also accepts an envelope
`{"result": "<document text>"}`, a fenced block, `oldRange`, per-file
`summary`, `meta`, `display`: all refused or dropped before storage). A
reader implements only the stored shape:

```json
{
  "version": 1,
  "title": "GitFocus tab: branch-first rows",
  "overview": "<markdown, see §6–§7>",
  "files": [
    {
      "path": "src/main/kotlin/dev/gitfocus/worktrees/WorktreesList.kt",
      "annotations": [
        { "newRange": [20, 28], "summary": "one sentence", "rationale": "the reasoning (optional)" }
      ]
    }
  ]
}
```

Field rules (these ARE what gg enforces on save; a reader may trust them
but must survive their violation, §8.2):

| field | rule |
|-------|------|
| `version` | the integer `1` |
| `title` | non-empty after trim, ≤ 200 bytes; equals the record's `summary` |
| `overview` | non-empty, ≤ 65 536 bytes (64 KiB), markdown |
| `files` | optional array; absent when the agent made no remark |
| `files[].path` | non-empty, repo-relative, slash form, as it is AT the commit |
| `files[].annotations` | optional array (omitted when empty) |
| `annotations[].newRange` | `[first, last]`, integers, `1 ≤ first ≤ last`, 1-based, INCLUSIVE, lines of the file at `address.Commit` |
| `annotations[].summary` | non-empty one-liner |
| `annotations[].rationale` | optional, may be absent or empty |

Order is meaningful: `files` and `annotations` are in the author's order
(gg draws remarks in that order; it does not sort).

A JSON Schema of this shape ships with gg (§9.1).

## 6. Anchors

Inside `overview`, a markdown link `[label](dest)` is an **anchor** when
`dest` parses by this grammar; otherwise it is an ordinary link (http/https)
or plain text (anything else, §7).

```
anchor   = [ "./" ] path [ ":" line [ "-" end ] ]
path     = one or more segments joined by "/" ; see rejections
line     = digits, value ≥ 1
end      = digits, value ≥ line
```

Parsing, in order:

1. Trim surrounding whitespace. Strip ONE leading `./`.
2. Reject (not an anchor) when: empty; starts with `/`; contains `\`;
   contains `://`; looks like a Windows drive (`X:` or `X:/…`, X a
   letter); contains any byte ≤ 0x20 (space, tab, newline…) or 0x7F; any
   `/`-separated segment equals `..`; or the destination starts with
   `note:` (a temporary-note anchor — never valid in a stored overview;
   gg refuses to save one).
3. Split at the LAST `:`. If the tail is `digits` or `digits-digits`, it is
   the line / range and the head is the path; a tail that is not digits
   leaves the whole string as the path (a path may contain `:` only in
   this position — e.g. `a:b` is the path `a:b`; `a:b:3` is path `a:b`,
   line 3).
4. `line < 1` or `end < line` → not an anchor. `end` absent → `end = line`.
5. An empty path after step 3 → not an anchor.

Semantics:

- `path` names a file at `address.Commit` — resolve with
  `git show <sha>:<path>` (or the VCS layer's equivalent). A reader MAY
  offer the working-tree file instead when its content equals the
  committed one, or when the user prefers "live", but the committed
  content is the reference for line numbers.
- no line → the file (open at top); `line` → that line; `line-end` → the
  inclusive range, selected/highlighted.
- Anchors are numbered in document order (1-based) — gg's "next/previous
  anchor" walks that order. **Only the first 100 anchors count**; a
  qualifying link after the 100th is rendered as plain text, exactly like a
  non-anchor destination.
- An anchor whose path does not exist at the commit, or whose lines exceed
  the file, is still drawn as an anchor but marked MISSING (gg greys it
  and refuses to open it); it is never dropped. gg refuses to SAVE such a
  document (and one whose anchor names a directory), so a stored overview
  was whole at save time; a later history rewrite can still orphan it.

Examples:

| dest | result |
|------|--------|
| `internal/x/parse.go` | file |
| `internal/x/parse.go:40` | line 40 |
| `./internal/x/parse.go:40-60` | lines 40–60 |
| `config.toml:0` | not an anchor (line < 1) → text |
| `a.go:9-3` | not an anchor (end < line) → text |
| `../other/a.go:3` | not an anchor (`..`) → text |
| `C:/src/a.go:3`, `/abs/a.go`, `dir\a.go` | not anchors → text |
| `note:t12` | not an anchor → text |
| `https://example.com/x:3` | an http(s) LINK (not an anchor) |

The link's label is the anchor's text; markup inside it (`` `code` ``,
`**strong**`) is kept by the renderer, flattened for lists of anchors.

## 7. Markdown subset

gg parses `overview` with its own bounded, HTML-free parser. A reader may
use any CommonMark/GFM renderer that (a) lets it intercept link
destinations to apply §6 and (b) never interprets raw HTML. What gg
understands, and so what a document may contain:

Blocks: paragraph; ATX heading (`#`…`######`); bullet and ordered list
(nested), task items (`- [ ]`, `- [x]`); block quote; fenced code (info
string → language); GFM table; thematic break. Setext headings and
indented code are NOT recognised (they read as paragraphs).

Inlines: text; `**strong**`; `*em*`; `~~del~~`; `` `code` ``; link
`[t](dest)`; image `![alt](dest)`; hard break — EVERY newline inside a
paragraph is a hard break, as in a GitHub comment (no two-space rule);
and the anchor (§6).

Rules that differ from a stock renderer:

- Raw HTML is ordinary TEXT, never markup.
- A link or image destination survives only when it is a clean `http://`
  or `https://` URL (scheme sniffing ignores embedded whitespace/control
  bytes, as browsers do). Any other destination that is not an anchor
  renders as the link's TEXT with no link — `[x](javascript:…)`,
  `[x](mailto:…)`, `[x](file:…)` all become `x`.
- Oversized input, very deep nesting and very wide tables degrade to plain
  paragraphs rather than failing. A reader need not replicate the limits;
  a document that gg accepted is ≤ 64 KiB.

## 8. Reader rules

### 8.1 Concurrency

gg writes a part by writing a temp file in the same directory and renaming
it over `commits.toml`, under `commits.toml.lock` (an O_EXCL lock file with
a stale-lock breaker). A reader therefore:

- never takes or waits for the lock — reads are always safe;
- re-reads when the file's mtime or size changes (poll or a file watcher on
  the store DIRECTORY — the file is replaced, not modified, so an inode
  watch dies on the first write);
- on a parse failure retries once after a short delay before reporting (a
  torn read across the rename is momentary; a persistent failure is a
  real corrupt file, which gg will quarantine on its next run).

### 8.2 Errors

- A record that is not an overview record (§4.3) is silently skipped.
- An overview record whose `rationale` is not JSON, or does not match §5,
  is shown as a ROW (title, author, date) marked "cannot render" — never
  dropped silently, never a crash.
- A missing anchored file or out-of-range line: the anchor is drawn and
  marked missing (§6).
- An overview whose commit is not in the repository (not fetched, rewritten,
  gone): list it, mark the commit unknown, do not resolve anchors.

### 8.3 Versions

- `format` (file): this spec describes 3. A file with a GREATER `format`
  was written by a newer gg: a reader MAY still read it, tolerating unknown
  keys, but MUST tell the user it may be incomplete. `format` < 3 (or
  absent): read with the SAME rules — gg's 2→3 conversion only rewrites
  `key: value` tags and the old `confidence` number into `meta`, which
  never touches an overview record's `tags = ['overview']`, `summary`,
  `rationale` or `address`. (Overviews and format 3 landed the same day,
  2026-10-12; a format-2 file holding an overview is possible, not likely.)
- `version` (document): this spec describes 1. A document with another
  version is "cannot render" (§8.2).
- Unknown keys anywhere — in the file, a record, or the document — are
  ignored, never fatal. The strict schema of §9.1 is gg's own test, not a
  reader's gate.

## 9. Conformance material (ships in gg)

So gg cannot drift from this document without a test failing:

### 9.1 JSON Schema

`docs/specs/gg-notes-store/overview-document.schema.json` — JSON Schema
2020-12 for §5, the stored canonical shape, with
`additionalProperties: false` on every object. It is gg's OWN drift guard:
a Go test in `internal/notebatch` validates the `Canonical()` output of
every overview-document test fixture against it, so a new stored field
fails gg's build until this document and the schema say so. A READER MUST
NOT validate documents strictly with it at runtime (§8.3 — unknown keys
are ignored): it may use the schema's types and required fields, or the
lenient twin `overview-document.lenient.schema.json` (same shape,
`additionalProperties: true`) that ships beside it.

### 9.2 Fixture store

`docs/specs/gg-notes-store/fixture/commits.toml` — a store with: one
commit overview with two anchored files and remarks; one PR overview
(`preview` set); one non-overview record (`tags = ['review']`, a `Path`)
that a reader must skip. The same Go test reads it through gg's own store
code and asserts the two overviews are the ones found, so the fixture is
proven readable by gg, not just hand-written.

### 9.3 Oracle for the key rule

`gg overview list --json` gains two fields: `"store"`, the absolute path
of the `commits.toml` gg read, and `"key"`, the directory key. Running it
in the repository the plugin targets proves the plugin's §3.2 derivation
on any platform in one command. (~20 lines; the one spec rule that could
not be confirmed from disk on Windows.)

## 10. Handoff — the `gg-notes` IDEA plugin prototype

Inputs the plugin author needs from the user: the plugin's name and package;
the GitFocus repository (its Gradle/IntelliJ Platform setup is the
template); the IDE version to target.

v1 (read-only), the acceptance scenario included:

1. **Store resolution.** From the project's git root(s): run the
   `rev-parse` of §3.2 (or ask the IDE's git integration for the common
   dir and apply `clean`), derive the key, find `commits.toml`. Confirm
   with `gg overview list --json` (§9.3) once on each platform.
2. **Tool window "gg Overviews".** Lists overview records of the open
   repository grouped by commit (HEAD's first, then by `updated`
   descending), each row: title, author, date, short sha, PR number when a
   PR overview. Refreshes on file change (§8.1).
3. **Overview view.** Renders the document (§7) with anchors as links
   (§6) and a "next/previous anchor" action over the first 100 anchors.
   Clicking an anchor opens the file AT THE COMMIT (IDE's VCS file content
   for a revision) — or the working file when identical — scrolled to the
   line, the range selected; Back returns to the overview.
4. **Remarks.** For each `files[].annotations` entry, an inlay/gutter hint
   on `newRange` in the opened file showing `summary` (and `rationale` on
   hover), only while that overview is open.
5. **Acceptance.** Opened on the GitFocus checkout, the tool window shows
   "GitFocus tab: branch-first rows" on commit `d5efa035…` (author
   "Claude Opus 5.5", 2026-10-13); its anchors open
   `src/main/kotlin/dev/gitfocus/worktrees/WorktreesList.kt:20-28` etc.;
   the fixture of §9.2, copied into a throwaway store (`XDG_STATE_HOME`
   pointed at a temp dir), shows exactly two overviews.

Out of scope for the prototype: writing anything; line notes and reviews;
PR comments; anything requiring gg to run. A later spec adds the next
record kinds under the same envelope (§1).

## 11. Open questions

None blocking. Deferred, for the next record kinds: how a reader shows the
`display` declaration of meta-bearing notes; whether gg should also mirror
overviews into a git notes ref so they travel with the repository (ruled
out of v1 by the user: "use the same files").
