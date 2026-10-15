# gg notes store format — part 2: reviews, line notes, meta

**Status:** draft for review · **Date:** 2026-10-15 · **Extends:**
`2026-10-15-gg-notes-store-format-design.md` (part 1: the store, the
envelope, overview records). **Audience:** a reader of gg's notes store
that is not gg — the `gg-notes` IDEA plugin — adding the next record
kinds. Part 1's rules (locating the store, the envelope, concurrency,
versions, the markdown subset, the anchor grammar) apply unchanged; this
document adds record kinds and what they mean.

## 1. Scope

New in this part, in the order a reader should take them on:

1. **Review records** (§3): an AI review of a commit, a commit range, a
   branch or a pull request — its summary text, its optional overview, and
   its remarks on the two sides of the reviewed diff.
2. **Line notes and replies** (§4): a note on lines of a file at a commit;
   a reply under any root; thread resolutions.
3. **Live notes** (§5): notes and reviews on a WORKING TREE's uncommitted
   changes, in a per-worktree part file, with the relocation rule gg
   applies when the file has changed since the note was written.
4. **Meta and display** (§6): the author's free key/values on a note or a
   remark and the declaration of how gg shows them.
5. **What is not in the store** (§7): forge (GitHub) comments, shelf notes,
   temporary session notes — so a reader does not look for them.

Still read-only: nothing here authorises a write.

### 1.1 The one predicate table

Every record is classified by `tags`, `address` and `parent_id` alone.
`State` is `model.FileState`: `0` committed, `1` shelf, `2` staged, `3`
unstaged (working file, tracked), `4` untracked.

| kind | `tags` has | `parent_id` | `address` | part file |
|------|-----------|-------------|-----------|-----------|
| overview (part 1) | `overview` | none | `State 0`, `Commit` set, `Path ''` | `commits.toml`; `previews.toml` when `preview` holds `...` |
| review | `review` | none | `State 0`, `Commit` set, `Path ''` | same rule as overview |
| working review | `review` | none | `State 2/3/4`, `Commit ''`, `Path ''`, `Worktree` set | `worktrees/<wkey>.toml` |
| line note | neither | none | `Path` set; `State 0` + `Commit` (committed) or `State 2/3/4` + `Worktree` (live) | `commits.toml` / `worktrees/<wkey>.toml` |
| reply | (any) | set | inherited from its root at creation | the ROOT's part |
| shelf note | — | — | `State 1`, `ShelfID` set | `shelf.toml` — out of scope (§7) |

A record that fits no row is unknown to this version: ignore it.

## 2. Record fields used by this part

Part 1 §4.2 listed the envelope; these are the keys the new kinds use.

| key | type | used by | meaning |
|-----|------|---------|---------|
| `side` | string | notes, replies | `'new'` or `'old'`: which side of a diff `range` indexes. A committed line note is on `'new'` (the file at the commit) unless written from the old side of a diff view |
| `range` | `[a, b]` | notes | 1-based, inclusive lines on `side`; `[0, 0]` on an entry-level record (a review, an overview, a working review) |
| `context_hash` | string | notes | the fingerprint of the anchored lines (§4.3); `''` on entry-level records |
| `scope` | string | reviews | the reviewed hex range: `<a>..<b>` (commits reachable from b, not from a — a branch's `<merge-base>..<tip>`), or `<sha>^..<sha>` (one commit's own change). The review's `address.Commit` is `<b>` |
| `preview` | string | reviews, overviews | the scope's NAME when the review was made in one: a pull request `<base-branch>...refs/gg/pr/<n>`, a merge preview `<target>...<source>` (branch names), a commit pair `<a7>..<b7>` |
| `preview_base` | string | merge-preview / PR reviews, OPTIONAL | the merge base (full sha) gg stamps best-effort on a `...` preview note when it can compute it — informational; the reviewed range is always `scope`. The real PR review on the author's machine has `scope` and no `preview_base` |
| `preview_branch` | string | pair reviews | the branch the pair review was written on; empty for a merge preview's |
| `files` | array of tables | working reviews | `{path, blob?, deleted?}` per file the review read: git's blob id of the working bytes it saw (`deleted = true`: it saw the file absent) — the fingerprint that says whether the review still matches the tree |
| `meta` | array of `{key, value}` | any | the author's free key/values (§6) |
| `display` | table | any | the author's declaration of how meta is shown (§6) |
| `added` | array of tables | any | meta written onto the note later by someone else: `{key, value, by?, at, remark_fp?}` — `remark_fp` set when it targets one remark of a review |
| `link` | string | replies | an address the reply points at: a `gg://` link or a full sha ("here is the fix") |
| `remark`, `remark_fp`, `remark_summary` | string | replies to a review remark | `remark = 'review:<review id>:<n>'` (the n-th remark, 0-based, in document order), `remark_fp` its fingerprint (§3.4), `remark_summary` its summary at reply time |
| `[[resolved]]` (top level) | array of tables | any part | `{root, by?, at, remark_fp?}`: a thread marked resolved — `root` is a stored note id or a remark id `review:<id>:<n>` |

Legacy (format < 3) files: a tag of the form `key: value` (regex
`^([A-Za-z0-9_.-]{1,64}): (.+)$`) is a meta entry, not a tag, and a
`confidence` number (0–1) is meta `confidence` with the word gg maps it to.
gg converts on read; a reader of an unconverted `notes.toml` does the same.

## 3. Review records

### 3.1 What a review is

An AI reviewed a DIFF — the change between two commits (`scope`), or a
pull request's change (`preview` + `preview_base`), or a branch against the
trunk's merge base — and wrote ONE record: `tags = ['review']`,
`address.Commit` = the range's last commit (a branch's tip, a PR's head),
`address.Branch` = the branch name when the review was asked by branch,
`summary` = `Review: <label> (<a7>..<b7>)` (label = the branch name; for
a pull request `PR #<n>`; for a merge preview `<source> → <target>`; else
the commit's short sha + subject. The range in 7-hex, omitted when it
equals the label — e.g. `Review: PR #3703 (2da383c..23d6917)`),
`rationale` = the review document (§3.2). Remarks on lines are INSIDE
the document; there are no per-remark records.

Where it shows in gg: the commit's `✎` marker and its `.` menu, a
`└ Review` row under the branch while its tip is that commit, the PR view
for a PR review, View all notes.

A commit may carry several reviews (different agents, different times);
gg keys them by `id` and lists newest first. Re-running `gg review save`
with `--note-id` rewrites one in place.

### 3.2 The review document

`rationale` is JSON text, stored canonical (two-space indent, keys in this
order). Strictly larger than the overview document — it is the superset
part 1 §5 was cut from:

```json
{
  "version": 1,
  "summary": "<markdown — the review's own text; what a forge gets as the review body>",
  "overview": "<markdown with anchors, optional — the guided walk>",
  "meta": { "verdict": "approve", "confidence": "high" },
  "display": { "<key>": { … } },
  "files": [
    {
      "path": "src/a.go",
      "summary": "<one line about this file, optional>",
      "meta": { … },
      "display": { … },
      "annotations": [
        { "newRange": [12, 14], "summary": "…", "rationale": "…", "meta": { … }, "display": { … } },
        { "oldRange": [40, 40], "summary": "…" }
      ]
    }
  ]
}
```

| field | rule |
|-------|------|
| `version` | `1` |
| `summary` | non-blank markdown (§7 of part 1 applies; links are ordinary — this text has NO anchors, gg renders it as prose) |
| `overview` | optional; when present, non-blank, ≤ 64 KiB, the SAME document kind as an overview (part 1 §6–§7): its anchors are resolved against the reviewed change — a path must be one of the review's files and its lines exist in that file at `address.Commit` (new side); an anchor that is not OK draws as plain text |
| `meta` | optional object of string values (a non-string value is its compact JSON); see §6 |
| `display` | optional declaration; see §6 |
| `files[]` | optional; each `path` non-empty, repo-relative, slash form |
| `files[].summary` | optional one line about the file |
| `annotations[]` | each has EXACTLY ONE of `newRange` / `oldRange` (`[first, last]`, `1 ≤ first ≤ last`) and a non-blank `summary`; `rationale`, `meta`, `display` optional |
| `newRange` | lines of the file on the NEW side: the file at `address.Commit` |
| `oldRange` | lines of the file on the OLD side: the file at the range's base — `<a>` of `scope`, or `preview_base` — i.e. the left side of `git diff <a> <b> -- <path>`. A reader with no diff view lists such a remark under its file with the "old" mark and does not place it in the editor |

Reader rules: the document is `cannot render` (part 1 §8.2) when it is not
JSON or breaks the rules above; unknown keys are ignored. Remarks are
numbered 0, 1, 2… in document order across all files — that number is
what `review:<id>:<n>` (replies, resolutions, `added`) refers to.

### 3.3 Kinds of review, by the record

| kind | how to tell | the diff it reviewed |
|------|-------------|---------------------|
| one commit | `scope = '<sha>^..<sha>'` (a ROOT commit: `scope = '<sha>'` alone — everything it adds) | the commit's own change |
| commit range | `scope = '<a>..<b>'`, `preview` empty | `a..b` |
| branch | `address.Branch` set, `scope = '<merge-base>..<tip>'` | the branch against the trunk's merge base (gg records the base sha in `scope`; when no base was found the scope is the tip's own change) |
| pull request | `preview = '<base>...refs/gg/pr/<n>'`, in `previews.toml` | `scope` = `<merge-base>..<PR head>` at review time (`address.Commit` = that head) |
| merge preview | `preview = '<target>...<source>'` (branch names), in `previews.toml` | `scope` = `<merge-base>..<source tip>` |
| commit pair | `preview = '<a7>..<b7>'`, `preview_branch` set, in `previews.toml` | the pair; shown only on `preview_branch` |
| working changes | §5 | the worktree's uncommitted changes at review time |

### 3.4 Remark identity

Replies, resolutions and added meta name a remark by position
(`review:<id>:<n>`) AND by fingerprint:

```
remark_fp = hex( sha256( path + "\0" + side + "\0" + first + "\0" + last + "\0" + summary )[:8] )
```

with `path` as written in the document (one leading `./` stripped), `side`
= `new`/`old`, `first`/`last` the range in decimal, `summary` as written.
When a review is re-saved and its remarks move, gg re-finds a thread by
`remark_fp`; a reader that cannot match the fingerprint shows the thread
under the review, unplaced, rather than on the wrong remark.

### 3.5 Where replies to a review go

A reply (`parent_id = <review id>`) with `remark` set is a reply to that
remark (a thread under the remark); without `remark` it is a reply to the
review as a whole. Both are stored in the review's part and inherit the
review's `address` (`range = [0, 0]`).

## 4. Line notes and replies (committed)

### 4.1 A line note

`tags` without `review`/`overview` (usually absent), `address.Path` set,
`State = 0`, `Commit` = the commit the file was read at, `side`, `range` =
the lines on that side, `context_hash` = the fingerprint of those lines
(§4.3), `summary` = the note's one-line text, `rationale` = the longer
text (optional), `source` = `'user'` or `'agent'`, `author`.

`side = 'old'` means the note was written on the OLD side of the diff
view of `Commit`: the file at `<Commit>^:<path>` (the first parent's
copy). The lines index that side, not the committed file. A reader
without a diff view places only `new` notes and lists `old` ones.

The two sides, by `State` (what gg's own resolver reads):

| `State` | old side | new side |
|---------|----------|----------|
| 0 committed | `<sha>^:<path>` | `<sha>:<path>` |
| 2 staged | HEAD's copy | the index's copy |
| 3 unstaged | the index's copy | the working file |
| 4 untracked | absent | the working file |

Where it shows in gg: the file's `✎` count in a commit's file list, a box
beside the lines in the diff / file view, View all notes; a `gg://` link
`gg://<repo>/<path>@<sha>:<a>-<b>?note=<id>` opens it.

**Legacy stores — a decision for the user.** On the author's machine
1907 of 1919 repository stores are still un-split `notes.toml` files
(3613 line notes, written before 2026-10-05) that gg converts only when
it next opens that repository. Part 1 §3.3 lets a reader read
`notes.toml` with the same rules (MAY). For "common notes" that MAY
means the plugin shows almost nothing for a repository gg has not
reopened since the split. Ruling (2026-10-15): a reader SHOULD read
`notes.toml` when it is present (gg renames it on conversion, so the two
never coexist for long), applying §2's legacy tag/confidence conversion
on read, and MUST NOT write or rename it.

### 4.2 Replies and threads

A reply has `parent_id` = its root's `id` and, at creation, a COPY of the
root's `address`, `side`, `range` and `context_hash`; gg re-anchors a
reply with its root and never by its own copy. Threads are flat: a reply's
parent is always the root (no reply-to-reply). Order: by `created`.

`[[resolved]]` entries (top level of the part) mark a thread resolved:
`root` = the root's `id` (or a remark id), `by`, `at`. A root that is gone
takes its entries with it. A reader shows a resolved thread collapsed or
marked; it never hides it.

### 4.3 The fingerprint

```
context_hash = hex( sha256( join("\n", [trim(line) for line in lines[first..last]]) ) )
```

— each anchored line with leading and trailing whitespace removed, joined
by `\n` with no trailing newline, SHA-256, full 64-hex. Re-indentation does
not change it; any other edit of those lines does.

On a COMMITTED note the lines never change, so the hash is only a check:
a mismatch means the note was written against different content (a
rewritten commit) — show it as stale (§5.2). The hash earns its keep on
live notes (§5).

## 5. Live notes: a worktree's part

### 5.1 The part file

Notes on uncommitted changes live in `worktrees/<wkey>.toml`, one file per
checkout:

```
wkey = hex( sha256( Clean(<worktree top-level path>) )[:8] )
```

— the same formula as the repository key (part 1 §3.2), over the
worktree's top-level directory as gg had it when it wrote the note:
normally what `git rev-parse --show-toplevel` prints, but gg may use the
path it was opened with (not necessarily symlink-resolved), cleaned with
Go's `filepath.Clean`. **The hashed string is stored verbatim in every
live record's `address.Worktree`**, so a reader does not guess: it lists
`worktrees/*.toml`, reads each, and checks `wkey == sha256(Worktree)[:8]`
and that `Worktree` names the checkout it is showing (compare with the
part-1 identity rule: cleaned, slash form, case-folded on Windows/macOS).
A main checkout and each linked worktree have their own file under the
ONE repository store. A file is removed when its last note goes.

A live record has `address.Worktree` = that top-level path (as hashed),
`Commit = ''`, `State` = `2` (the index's copy), `3` (the working file,
tracked and modified) or `4` (untracked), and for a line note `Path`,
`side`, `range`, `context_hash` as in §4.1. `side = 'old'` on a live note
indexes the copy the diff was drawn against — the §4.1 table: the index's
copy for an unstaged note, HEAD's for a staged one, nothing for an
untracked file.

A working review is the same record shape as §3 with `State = 3`,
`Path = ''`, `Commit = ''`, `scope = ''`, plus `files[]`: the blob id of
each file as it was when reviewed (`git hash-object` of the working bytes;
`deleted = true` when absent). A reader compares those to the current
bytes to say "this review is of the tree as it is" or "the tree has moved
on".

### 5.2 Relocation

A live file changes after the note is written. gg resolves each line note
against the CURRENT text of its side every time it is drawn (never stored):

1. **Entry-level** (`range = [0, 0]`): active where it is.
2. The side's text is absent (file deleted, untracked file gone, index copy
   gone, the commit not in this repository): **orphaned**. gg's FILE views
   drop an orphaned note (nothing to draw it on); the note stays in the
   store and View all notes still lists it. A reader lists it, unplaced.
3. Take `span = last - first + 1` lines at `first`; if the fingerprint of
   those lines equals `context_hash`: **active**, where it was.
4. Else scan OUTWARD from `first` — distance 0, then `first-1` and
   `first+1`, then `first-2` and `first+2`, … — for a window of `span`
   lines whose fingerprint matches (the lower candidate checked before the
   higher at each distance; a window must fit: `1 ≤ start ≤ len - span + 1`).
   First hit: **active**, at that window.
5. No hit: **stale** — drawn at the stored range clamped into the file
   (`first` pulled to `[1, len]`, `last` to `[first, len]`; an empty file
   clamps to `[0, 0]`), marked as not found.

A reply takes its root's status and range. A reader may skip step 4 and
show "stale" in its place; it must not show a stale note as if it were at
its original lines without a mark.

## 6. Meta and display

### 6.1 Meta

`meta` on a record (and `meta` inside a review document at each level) is
the author's free key/values: keys and values are theirs; gg has no
vocabulary and meta never triggers behaviour. A value is one line, ≤ 256
bytes; a key ≤ 64 bytes; ≤ 32 keys; a repeated key keeps the first. In a
document `meta` is a JSON object whose non-string values are stored as
their compact JSON text. On a record it is an array of `{key, value}`
tables (TOML `[[notes.meta]]`; `display` as `[notes.display.<key>]`
sub-tables — gg's encoder also emits the empty parent headers
`[notes.display]` and `[notes.display.<key>.values]`, which any TOML
parser accepts; `added` as `[[notes.added]]`; a working review's `files`
as `[[notes.files]]`; `[[resolved]]` after the last note). No record on
the author's machine carried record-level `meta`/`display` when this was
written, so the part-2 fixture (§9) carries them written through gg's own
encoder — the proof of the spelling.

`added` holds values written later by someone else (the user's mark,
another agent's double-check) with `by` and `at`; an added value for a key
REPLACES the author's for display (`from`/`to`). On a review, `remark_fp`
targets one remark.

The one built-in key is `status` with the values `agreed`, `disagreed`,
`deferred` (what a user sets on an agent's remark); it is shown as a text
badge (green / red / grey) even without a declaration.

### 6.2 Display declaration

`display` says, per meta KEY, how to show it. A reader MAY ignore it and
show meta as `key: value` lines (that is exactly what gg does for an
undeclared key). To match gg:

```json
"display": {
  "severity": {
    "show": ["badge", "count"],
    "label": "Severity",
    "order": 1,
    "style": "filled",
    "as": "level",
    "values": {
      "high":   { "color": "red",    "glyph": "!", "level": "high" },
      "medium": { "color": "orange", "level": "medium" },
      "low":    { "color": "yellow", "level": "low" }
    },
    "other": { "color": "grey" }
  }
}
```

| field | meaning | allowed |
|-------|---------|---------|
| `show` | where the key's value appears | `badge` (a chip on the note), `frame` (colours the note's frame), `count` (counted in the review's totals), `line` (a `label: value` row) |
| `label` | the key's display name | any |
| `order` | sort among badges/lines (ascending, then key name) | integer |
| `style` | badge style | `filled`, `text` |
| `as` | the key's VALUE is itself a semantic | `level`, `audience` |
| `values.<v>` | presentation + semantics of one value | `color` ∈ red, orange, yellow, green, blue, purple, grey; `glyph` = ONE character: printable ASCII (`!`–`~`) or one of `◆ · → » •`; `level` ∈ high, medium, low, info; `audience` ∈ author, reviewer |
| `other` | the same for a value not listed | as above |

Limits: ≤ 8 keys per declaration, ≤ 16 values per key. gg checks only
the declaration's SHAPE when it stores a document (an unknown field is
dropped and named); the vocabularies above are checked when the note is
DISPLAYED — an invalid colour, glyph, place, style, level or audience is
dropped and named as a problem then, the rest stands. So a stored
document may carry an entry a schema rejects; a reader does the same:
ignore the entry, keep the note.

**Inheritance:** in a review document, a declaration at the document level
applies to every file and remark; a file's applies to its remarks; a
remark's to itself — key by key, the NEAREST declaration replaces the
farther one wholesale. A reply inherits its root's.

**Semantics:** `level` and `audience` are what a consumer READS instead of
key names: a note whose effective `level` is `high` is a high-severity
finding whatever the author called the key; `audience: author` marks a
remark meant for the change's author, `reviewer` for the reviewer. A
reader that supports one filter should filter on these, not on keys.

## 7. What is NOT in the store

- **Forge (GitHub) comments and review threads** — gg reads them live
  through `gh` and keeps a per-repository CACHE under
  `<state>/gg/prcache/<key>/` with read-time expiry. The cache is not a
  contract; a reader that wants PR comments runs `gh` itself. Only a user's
  local DRAFT replies to forge threads are in the store: a reply whose
  `parent_id` is `forge:<comment id>` (`address` = the PR head commit +
  the comment's path/side/range). A reader shows such a draft under the
  commit's file, marked as a draft to a forge thread it cannot see.
- **Temporary session notes and overviews** (`gg session note`,
  `gg session overview`) live in a running gg's memory only.
- **Shelf notes** (`State 1`, `shelf.toml`) describe gg's shelf entries
  (non-git saved changes) and mean nothing without the shelf store.
- **Bookmarks, saved comparisons, branch versions** — other stores under
  `<state>/gg/`, out of scope.

## 8. Reader behaviour summary (what the plugin shows)

| kind | list row | opened |
|------|----------|--------|
| review (commit/range/branch) | under its commit: `Review: …`, author, date, the counts of its `count`-declared meta (e.g. 3 high / 2 low) | the `summary` markdown; then its files with each remark (summary, rationale, badges); an `overview` as a part-1 overview; click a new-side remark → the file at `address.Commit` at the lines; an old-side remark → listed, placed only with a diff of `<a>..<b>` |
| PR review | under its PR number (or the commit, marked PR #n) | same; the diff is `scope` (`<merge-base>..<head>`) |
| working review | under "working changes" of that worktree | same; files at the working tree; mark "tree has changed" when a `files[].blob` no longer matches |
| line note | under its commit's file (or the worktree's file) with its thread | the file at the commit (or the live file, relocated per §5.2) at the lines, the note and replies beside; resolved threads collapsed |
| forge draft reply | under the PR head's file, marked draft | text only |

## 9. Conformance material (ships in gg)

Added beside part 1's:

- `review-document.schema.json` (strict, gg's drift guard, validated against
  `notebatch.ReviewDoc.Canonical()`) and `review-document.lenient.schema.json`.
- The fixture store gains: a commit review with new- and old-side remarks,
  meta and a display declaration (`commits.toml`); a PR review with
  `preview_base` (`previews.toml`); a committed line note with a reply and
  a `[[resolved]]` entry (`commits.toml`); a worktree part
  `fixture/worktrees/<wkey>.toml` with a live note, where `<wkey>` is
  documented in the fixture's README together with the top-level path it
  hashes.
- Tests in gg: the fixture read through gg's own store yields exactly the
  listed records per kind; `remark_fp` and `context_hash` test vectors
  (`internal/domain`); the relocation rule's four outcomes pinned by a table
  test the reader can port (inputs: lines, note; outputs: status, range).
- `gg overview store` is unchanged (it already names the directory).

## 10. Open questions

- Whether gg should expose `gg note list --all --json` in a shape that
  mirrors this spec (a second oracle for part 2) — useful, not required.
- Shelf notes stay out until the shelf store has a reader spec of its own.
