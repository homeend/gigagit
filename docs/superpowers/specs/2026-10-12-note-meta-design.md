# Note meta: free key/values, an agent-declared display — design

Date: 2026-10-12. Status: design approved in brainstorm (rulings R1–R16,
§13); this document is the spec for the user's review. Four plans follow
(§14).

## 0. Vocabulary

- **Meta** — a note's free key/value pairs (`kind: issue`,
  `severity: high`, `intent: checked the lock path`). The keys and values
  are the author's (an agent's) choice; gg owns no vocabulary. The skills
  only *suggest* one (§9).
- **Display declaration** (`display`) — what the author says about how its
  meta keys are shown: per key, the **places** it appears in (§3.2) and,
  per value, its **presentation** (colour, glyph, style) and its
  **semantics** (§3.3). The places and the options are gg's fixed design;
  which keys use them is the author's choice.
- **Semantics** — gg's own small vocabulary that a value (or a whole key)
  can be mapped onto: `level` and `audience` (§3.3). It is the ONLY part of
  meta gg itself understands; frontends and forge adapters read semantics,
  never key names. The set grows by adding a semantic and teaching each
  consumer, never by reading agents' keys.
- **Added meta** — meta written onto a note after its author wrote it: by
  the person reading it (the Mark use case, §5.4) or by another agent (a
  double-check marking a remark `status: disagreed`). Stored beside the
  author's meta, never over it (§2.4).
- **Resolved display** — the result of evaluating a note's meta, its
  declaration (with inheritance) and the added meta: a list of badges, a
  frame colour, line rows, count keys, semantics and the problems found.
  One pure package computes it (§4); every consumer renders it.

Meta is a **glance aid**: it tells the reader what a note is about without
reading it. It never triggers anything — nothing is sent, ticked, filtered,
blocked or resolved because of a meta value (R12).

## 1. What is built

1. **Meta + display on every note** (§2): standalone notes, replies, review
   remarks, and the agent's temporary notes on open files. Review documents
   declare their display once for all their remarks (R2).
2. **A fixed display mechanism** (§3): five places (`badge`, `frame`,
   `count`, `line`, stored-only), a fixed colour set, an optional glyph, a
   badge style, and two semantics (`level`, `audience`).
3. **`notemeta`**, a pure leaf package (§4): types, validation, limits,
   inheritance, resolution, counts.
4. **Generic operations in domain** (§5) and every write surface: the three
   agent JSON shapes, the CLI (`--meta`, `--display`, `gg note meta`), MCP
   tools, `gg session note`.
5. **One viewer use case** (§5.4): mark a note agreed / disagreed /
   deferred — a plain tag with no consequence.
6. **Rendering in the TUI and the web** (§6, §7): note box title badges,
   frame colour, line rows, review sidebar counts, the send panel's
   expanded row.
7. **Forge adapters** (§8): domain hands the provider a neutral comment;
   the GitHub renderer turns semantics + shown badges into text. Free meta
   is never sent.
8. **Skills** (§9): suggested vocabulary with meanings, the `intent` and
   `audience` guidance, a default display block, worked examples.
9. **Notes format 3** (§2.5): a format bump with a lossless conversion of
   today's `key: value` tags.

## 2. Data model and storage

### 2.1 The note record

`model.Note` gains three fields (TOML names in brackets):

- `Meta []notemeta.KV` (`[[notes.meta]]` → `key`, `value`) — ordered, keys
  unique.
- `Display notemeta.Display` (`[notes.display.<key>]`) — the note's own
  declaration; empty = none.
- `Added []notemeta.Added` (`[[notes.added]]`) — meta written onto the note
  after its author wrote it (§2.4).

The types live in `notemeta` (§4), a stdlib-only leaf, so `notebatch`
(stdlib-only) and `agentdocs` (a leaf) can use them; `model` imports it.
`notebatch.MetaKV` becomes an alias of `notemeta.KV`.

### 2.2 Review documents

The stored review document (JSON kept in the review note's `rationale`)
already carries `meta` at the document, file and annotation level. It gains
`display` at the same three levels:

```json
{
  "version": 1,
  "summary": "…",
  "meta": {"verdict": "comment"},
  "display": { … applies to every remark … },
  "files": [{
    "path": "packages/babel-core/src/config/caching.ts",
    "display": { … overrides for this file's remarks … },
    "annotations": [{
      "newRange": [440, 441],
      "summary": "The no-op catch only marks the promise as handled; waiters still reject",
      "rationale": "…",
      "meta": {"kind": "good", "intent": "verified the rejection path"},
      "display": { … overrides for this remark … }
    }]
  }]
}
```

The review document's version stays 1: `display` is an optional field, and
an older gg keeps the whole JSON verbatim (it lives in a string).

### 2.3 Inheritance

A note's effective declaration is built key by key, nearest wins, and a
nearer entry for a key **replaces** the farther one wholesale (no deep
merge — what you read in one place is what applies):

- review remark: remark → file → document;
- reply: its own → its root's effective declaration;
- standalone note, temporary note: its own only.

### 2.4 Added meta

Added meta is stored ON a stored note, in its `Added` list:

```toml
[[notes.added]]
remark_fp = '…'            # set on a review note's entry for one remark
key = 'status'
value = 'agreed'
by = 'MR'                  # the writer: the user's name or the agent's label
at = 2026-10-12T10:41:00Z
```

- A review remark's entries live on its review note, keyed by the remark's
  fingerprint (the `RemarkSends` precedent), so they follow the remark
  when the review is re-saved and go when the review goes. A remark's own
  meta stays in its review's JSON, never rewritten.
- A standalone note's meta written by its own author (`by` = `Author`) is
  rewritten in place in `Meta`; a different writer's goes to `Added`.
- One entry per (remark_fp, key); setting it again replaces it, an empty
  value removes it.
- In the resolved display an added value for a key replaces the author's
  value for that key; the author's original stays in the note's details
  row (`severity: high (Claude Code) → low (MR)`).

### 2.5 Format 3 and the conversion (R10)

The notes store is machine-local, so its format lives in its own files —
never in a git-ref marker (which `domain/preflight.go` reserves for data
shared through `.git`):

- Every part file gets a top-level `format = 3`, written on every save.
- A part whose `format` is above 3 still READS, but every write to it
  fails with `notes.ErrNewerFormat` ("this notes file was written by a
  newer gg; upgrade gg to change it"), so a later build's fields are never
  dropped by this one.
- **Known limit, accepted:** builds already installed (including an old
  `gg mcp` an agent runs) have no such check. They read format 3 fine but
  drop `Meta`, `Display` and `Added` on their next save of that part. The
  protection holds only from this build onward.
- **Conversion (lossless, no prompt, at read time):** whenever a part is
  read, every tag of the form `key: value` (key = letters, digits, `_`,
  `-`, `.`, at most 64 bytes) moves into `Meta` in tag order; `review`,
  `resolved` and other plain tags stay tags. The old `confidence` number
  moves into `Meta` as `confidence = low|medium|high` (0.3/0.6/0.9; any
  other number as its decimal text). The Go field becomes the decode-only
  `LegacyConfidence` and is never written again. A part is rewritten in
  the new shape on its next real change; until then every reader sees the
  converted notes.
- After conversion a note with meta and no declaration shows its meta in
  the `line` place (§3.2) — what tags look like today (R13).

### 2.6 Limits

Per note: at most 32 meta keys; a key at most 64 bytes, a value at most
256 bytes, both single-line. Per declaration: at most 8 keys, at most 16
values per key. Over-limit entries are dropped at write time with a
problem recorded (§3.5); the write itself succeeds.

## 3. The display declaration

### 3.1 Shape

```json
"display": {
  "<meta key>": {
    "show": ["badge", "count"],
    "label": "kind",
    "order": 1,
    "style": "filled",
    "as": "audience",
    "values": {
      "<value>": {
        "color": "red",
        "glyph": "!",
        "level": "high",
        "audience": "author"
      }
    },
    "other": { "color": "grey" }
  }
}
```

Every field is optional. `values` keys match meta values exactly
(case-sensitive). `other` applies to any value not listed in `values`.
Keys are ordered by `order` (ascending, missing = 0), then by key name.

### 3.2 Places (`show`)

| Place | TUI | Web |
|---|---|---|
| `badge` | a chip in the note box title, before today's parts | a chip in the note header |
| `frame` | the note box border colour (the first key in order that shows `frame` owns it) | the box border colour |
| `count` | per-value counts in the review sidebar header and on its file rows | the same |
| `line` | a dim `label: value` row under the box title | the same |
| *(absent / `[]`)* | stored only: listed in the note's details row and `gg note show` | the same |

A key may show in several places. A key whose meta value is missing on a
note shows nothing on that note.

### 3.3 Presentation and semantics per value

Presentation:
- `color` — one of `red`, `orange`, `yellow`, `green`, `blue`, `purple`,
  `grey`. Unset = the place's default (badge: grey; frame: today's
  user/agent frame colour).
- `glyph` — optional, ONE character from the safe set: printable ASCII
  (not space) or one of `◆ · → » •`. Shown before the badge's value. Any
  other glyph is dropped (R14).
- `style` (on the key) — `filled` (coloured background, contrasting text)
  or `text` (coloured text). Default `filled`.
- `label` (on the key) — the word shown in a `line` row and in counts'
  hover/help; default = the key.

Semantics (gg's vocabulary, R7):
- `level` — `high` | `medium` | `low` | `info`. How much attention the note
  asks for. Used by forge renderers (§8) and nowhere else in v1.
- `audience` — `author` | `reviewer`. Who the note is for: the PR author
  (act or answer), or the reviewer only (verification, explanation,
  context). Shown as a marker (§6.1) and in counts; it decides nothing.

`as` (on the key) maps the key's VALUE straight onto a semantic: a key
declared `"as": "audience"` whose value is `author` gives the note
`audience: author`. This is how one note overrides a value-derived
semantic: a direct `as` key wins over semantics set through `values`. Two
keys giving the same semantic: the first in order wins.

### 3.4 Built-in declarations

gg ships exactly one: for the key `status` (§5.4), used when
the note's effective declaration has no `status` entry:

```json
"status": {"show": ["badge"], "order": 100, "style": "text",
  "values": {"agreed": {"color": "green"},
             "disagreed": {"color": "red"},
             "deferred": {"color": "grey"}}}
```

and one fallback: a note with meta and no declaration at all shows every
key in `line` (§2.5).

### 3.5 Validation

Nothing in a declaration is ever an error: an unknown place, colour,
semantic value, glyph, field or an over-limit entry is dropped and named
in the note's **problems** (`display.kind.values.issue.color: "crimson" is
not a colour`). Problems show in the note's details row and in
`gg note show`, so an agent can see why something did not display. The
JSON itself must parse — a malformed `display` is a write error of the
command that carried it (§10). An unknown FIELD is named when the
declaration is written (the CLI / MCP / batch reply); everything else is
re-checked on every read, so it shows in the details row as long as it
stands.

## 4. `notemeta` (new pure leaf package)

Imports: stdlib only (a DAG leaf). `model`, `notebatch`, `agentdocs`,
`domain` and the frontends import it.

- Types `KV`, `Added`, `Display` / `Key` / `Value`, `Problem`.
- `ParseDisplay(raw []byte) (Display, []Problem, error)` — malformed JSON is
  an error, an unknown field a problem; `CheckMeta([]KV) ([]KV, []Problem)`
  — the limits.
- `Effective(levels ...Display) Display` — inheritance (§2.3), farthest
  first.
- `Resolve(meta []KV, d Display, added []Added) Resolved` (re-validates the
  declaration on every call):

```go
type Resolved struct {
	Badges   []Badge  // ordered: Key, Value, Color, Glyph, Style
	Frame    string   // colour name, "" = default
	Lines    []Line   // Label, Value
	Counted  []Counted // Key, Label, Value, Color this note contributes to counts
	Level    string   // "" when unset
	Audience string   // "" when unset
	Stored   []KV     // meta shown nowhere (details row only)
	Added    []Change // Key, author's value, added value, by
	Problems []Problem
}
```

- `Count(rs []Resolved) Counts` — per counted key, per value, in
  first-seen order; plus the audience totals (`author` / `reviewer`).
- `GlyphOK(string) bool` — the safe set (§3.3).
- Constants for places, colours, semantics; the built-in `status`
  declaration.

## 5. Operations and write surfaces

### 5.1 Domain (the only API)

On the notes store's existing `Edit` path, under the part lock:

- `SetNoteMeta(ctx, ref NoteRef, by string, kv []notemeta.KV)` /
  `UnsetNoteMeta(ctx, ref, by string, keys []string)` — `by` is the writer
  (the user's name, or the agent's label from `--author`).
- `SetNoteDisplay(ctx, ref, d notemeta.Display)` (empty clears)

`NoteRef` = a note id, or a review note id + remark fingerprint (the form
`[[resolved]]` uses; `review:<id>:<n>` and `review:latest:<n>` resolve to
it as they do for replies). Where the meta goes (§2.4): on a remark,
always an added-meta record; on a standalone note, in place when `by` is
the note's author, otherwise an added-meta record. `SetNoteDisplay` on a
remark is refused (`a remark's display is its review's`): a declaration
belongs to whoever wrote the meta it describes, and an added `status`
shows through the built-in declaration (§3.4).

Reads: every note-returning query (`ResolvedNote`, the wire notes, review
show, send candidates, agentdocs notes) carries its `notemeta.Resolved`.
Review queries carry the review's `[]CountGroup` and each file row's.

### 5.2 Agent JSON input

All three shapes in `notebatch` accept `meta` (object of string/number/bool
→ text, as review meta does today) and `display`:

- review document: §2.2;
- agent-context v1: `meta` + `display` per annotation, `display` at top
  level (applies to the batch's notes like a review document's);
- comment batch: `meta` + `display` per item.

Legacy `tags` / `confidence` keep folding into meta (`foldMeta`).

### 5.3 CLI, MCP, temporary notes

- `gg note add` / `gg note reply`: `--meta k=v` (repeatable),
  `--display <json|@file>`.
- New `gg note meta <id|link> set k=v… | unset k… | display <json|@file>`
  (`--author <label>` names the writer, as `gg note add` does).
- `gg note show` / `list`: meta, resolved places and problems (text and
  `--json`).
- MCP: `gg_note_add`, `gg_note_reply`, `gg_notes_apply` accept `meta` and
  `display`; new `gg_note_meta` (the same three actions).
- `gg session note` (agentdocs temporary notes): `--meta`, `--display`;
  `agentdocs.Note` gains `Meta` and `Display`, memory only, rendered the
  same way.

### 5.4 The one viewer use case: mark (R6, R12)

A note's menu (TUI note-box `.` menu and the review view's remark rows;
web note-box menu) gets **Mark ▸ agreed / disagreed / deferred / clear**.
It calls `SetNoteMeta(ref, <user>, status=…)` (clear = `UnsetNoteMeta`)
and shows through the built-in
`status` declaration. It is a tag and nothing else: it does not resolve,
send, reply, tick, filter or hide anything. An agent double-checking a
review writes the same key as meta (`status: disagreed`) — same display,
same (absent) consequences.

No frontend gets a generic meta editor (R5).

## 6. Rendering: TUI

### 6.1 Note box

- **Title** (`noteBoxTitle`, `forgeNoteTitle`): badges first, then the
  audience marker (`→ author` / `for you`), then today's parts:
  `issue  high  → author · review · Claude Code · 14h · …/configuration.ts R12-14`.
  A badge = glyph (if any) + value, in its colour and style. When the
  title is too wide, the path is elided first (in the middle, `elidePath`),
  then today's parts, badges last.
- **Frame**: `Resolved.Frame` replaces the user/agent frame colour; a stale
  note stays dim (dim wins).
- **Line rows**: one dim `label: value` row per `Lines` entry, under the
  title, inside the box, before the summary.
- **Details row** (where tags and `(stale)` live today): stored meta, the
  added-meta changes, the problems.

### 6.2 Review view and sidebar

- Sidebar header: the counts in plain words —
  `3 issue · 2 suggestion · 8 good · 5 for author · 9 for you`.
- File rows: compact counts after today's `◆2`, the value's colour on its
  number (`issue 1 good 1`), cut before the path is.
- Remark rows: their badges before the summary.

### 6.3 Send panel (R9)

Unchanged: nothing ticked on open, no filtering, no dimming, no new key,
collapsed rows as today (their severity word now read from meta). The
**expanded row** (full title and description) gains one meta row: badges,
audience marker, then each `line` entry.

### 6.4 Colours and words

- Seven new theme roles `meta_red` … `meta_grey` in `theme.roleFields`,
  defined for Dark, Light and Terminal (Terminal = basic ANSI), overridable
  under `[themes.<name>]`; `styles.go` builds filled and text styles.
- gg's own words (`→ author`, `for you`, `agreed`, `disagreed`,
  `deferred`, `Mark`, `clear`, count phrasing) go through `i18n.T` in all
  four bundles. Agent keys and values are data: shown verbatim, never
  translated.

## 7. Rendering: web

The same resolved display over the wire (`WireNote.Display`), the same
places: chips in `notebox.js` `noteTitle` and `viewer.js` `noteBoxTitle`,
the border colour by class, line rows, counts in the review pane header and
file rows, the expanded send-panel row (`prsendpanel.js`), the Mark menu.
Colours are CSS tokens `--meta-red` … `--meta-grey` with light and dark
values on `:root`. The `.psev` chip takes the severity colour from the
resolved badges.

## 8. Forge adapters (R7, R11)

### 8.1 The neutral comment

Domain stops building posted text. It hands the provider:

```go
type Comment struct {
	Quote     []string // file-level stand-in's lines, as today
	QuoteLine int
	Summary   string
	Rationale string
	Badges    []notemeta.Badge
	Lines     []notemeta.Line
	Level     string
	Audience  string
	Author    string // agent signature; "" for the user's own
	MarkerKey string
}
```

The forge package gets a renderer per provider —
`RenderComment(Comment) string`, `RenderReview(ReviewBody) string` — and a
capability list. Today's `sendBody` / `reviewSendBody` / `quoteLines`
become the GitHub renderer. **Stored-only meta, added meta and the
declaration never leave gg.**

### 8.2 GitHub

Body, in order:
1. the quote (as today);
2. the badge line `**issue · high**` — wrapped in an alert when `Level` is
   set: high `> [!CAUTION]`, medium `> [!WARNING]`, low `> [!NOTE]`;
   `info` or unset gives the plain bold line; no badges, no line. Alerts
   inside review comments are undocumented: plan 4 tests on a sandbox PR
   first, and if they do not render the plain bold line is used always;
3. the summary;
4. `<details>`: the rationale, then the `line` entries (`intent: …`) —
   today's raw tag list and `confidence:` line go;
5. the signature and the hidden marker, as today.

`Audience` is not used by GitHub. Suggestion fences pass through. The
verdict stays the user's pick in the confirm.

### 8.3 Reading back

gg's own comments match their local note by the marker; the local note
keeps its meta. Other people's comments get no meta in v1 (no inference
from `Nit:` prefixes or bot badges).

### 8.4 Other providers

Not built. Appendix A is the mapping reference a future adapter starts
from, including where each would hide the marker.

## 9. Skills (R8)

Changed: `reviewing-with-gg` and the engine's review prompt
(`engine/review_changes.go`), `gg-review`, `gg-cross-review`,
`gg-overview`, `using-gg` (flags, `gg note meta`). `agentskill.Version`
bumps.

Each note-writing skill gets a **Meta and display** section:

1. **The fixed part, stated strictly**: the places, colours, the glyph
   safe set, style, `as`, the two semantics and what they mean, the
   inheritance order, the limits, "a key with no declaration is stored and
   listed, not shown", "meta never triggers anything — it is for the
   reader's glance".
2. **Suggested vocabulary — suggested, not required**:
   - `kind` — `issue` (wrong now), `suggestion` (fine now, could be
     better), `nitpick` (style or taste, trivial), `question` (you need
     the author's answer), `note` (context or impact, nothing to fix),
     `good` (you checked it and it holds). Tie-breaks: wrong now vs could
     be better → issue vs suggestion; "I verified it" → good, not note.
   - `severity` — `high` / `medium` / `low`, on issue, suggestion and
     note.
   - `intent` — one short line: what made you write this note
     ("checked the lock path for leaked waiters", "flagging an intended
     breaking change").
   - `confidence` — `low` / `medium` / `high`, as today.
   - cross-review: `raised_by`, `disputed_by`, `ruling` as today; a
     double-checking agent may write `status: agreed|disagreed|deferred`.
   - **audience rule**: "would the author change code or reply because of
     this? → author. Did you write it so the user understands or trusts the
     change? → reviewer."
3. **The default display block** to include unless there is a reason not
   to — coloured words, no glyphs (R14):

```json
"display": {
  "kind": {"show": ["badge", "count"], "order": 1, "values": {
    "issue":      {"color": "red",    "audience": "author"},
    "suggestion": {"color": "blue",   "audience": "author"},
    "nitpick":    {"color": "grey",   "audience": "author"},
    "question":   {"color": "purple", "audience": "author"},
    "note":       {"color": "yellow", "audience": "reviewer"},
    "good":       {"color": "green",  "audience": "reviewer"}}},
  "severity": {"show": ["badge", "frame"], "order": 2, "style": "text", "values": {
    "high":   {"color": "red",    "level": "high"},
    "medium": {"color": "orange", "level": "medium"},
    "low":    {"color": "grey",   "level": "low"}}},
  "audience": {"as": "audience", "order": 3},
  "intent": {"show": ["line"], "order": 4}
}
```

   (`audience` is declared with `as` and no place, so a remark can write
   `"audience": "author"` on an impact note.)
4. **Worked examples** from the babel review of PR #18249 and its
   siblings, one per kind, as review JSON:
   - config modules cached forever → `kind: issue`, `severity: high`;
   - the no-op catch → `kind: good`, `intent: verified the rejection path`;
   - unknown callers now get ESM → `kind: note`, `severity: high`,
     `audience: author` (the migration guide needs it);
   - no fixture for `break` → `kind: suggestion`, `severity: low`;
   - browser stub signature drift → `kind: nitpick`.
5. The old `meta.severity = bug | risk | design | nit` instruction is
   removed.

A test parses the default block from the skill text and validates it with
`notemeta.Validate` (no problems allowed).

## 10. Errors

- Malformed `display` JSON, a `--meta` argument that is not `k=v`, or a
  key/value over its limit on the CLI → command error, exit 2, nothing
  written. Over-limit entries inside agent JSON are dropped with problems
  (§3.5), the batch still applies.
- `SetNoteMeta`/`SetNoteDisplay` on a remark → the refusal in §5.1.
- Any write on a note that is gone → the store's existing not-found error.
- A build that finds notes format > 3 → preflight Unsatisfiable, notes
  read-only, the existing upgrade notice.

## 11. Tests (TDD, real git)

- `notemeta`: validation (every dropped-field kind), inheritance, `as`
  precedence, added-meta overlay, counts, limits, glyph set.
- Notes store: format-3 round trip (`Meta`, `Display`, `Added`), the
  `format = 3` stamp, the conversion on read (tags, confidence, plain tags
  kept, a read-only command never rewrites), `ErrNewerFormat` on a newer
  file.
- `notebatch`: the three shapes with `meta` + `display`.
- Domain: the operations, remark refusal, `review:latest:<n>` refs,
  resolved display on every query, counts.
- CLI and MCP: flags, `gg note meta`, `show` text and JSON.
- TUI: e2e golden screens (note box with badges, frame, line rows,
  details problems; review sidebar counts; send panel expanded row; the
  Mark menu); fixture = remarks re-tagged with the default vocabulary,
  shaped after Appendix B.
- Web: wiring tests plus a browser check asserting the chips, border and
  line rows are visible.
- GitHub renderer: golden bodies per level, no-badge body, the
  never-sent rule (stored/added meta absent from every body).
- Skills: the default-block validation test.

## 12. Docs

CHANGELOG; README (notes section); CLAUDE.md (new `notemeta` row; `model`,
`notebatch`, `forge` rows); `docs/CLAUDE-details.md` (the declaration
grammar, semantics, the format-3 limit); skills with the version bump.

## 13. Rulings

- **R1** Meta is free key/value; gg owns no vocabulary; skills suggest one.
- **R2** A review document declares its display once; file and remark
  override; standalone notes declare their own.
- **R3** Places: `badge`, `frame`, `count`, `line`, stored-only; fixed
  colour names, optional glyph, `filled`/`text` style.
- **R4** Generic from v1: every note kind, every write surface.
- **R5** Domain offers generic operations; frontends build specific use
  cases on them, never a generic meta editor.
- **R6** v1's viewer use case is Mark agreed/disagreed/deferred — a plain
  tag (`status`), the same key a double-checking agent writes.
- **R7** Meta is internal and never sent. Adapters read only gg semantics
  (`level`, `audience`), each the ones it can use; new semantics are added
  to gg and taught to adapters.
- **R8** Skills carry the suggested vocabulary, `intent`, the audience
  rule, a default display block and worked examples.
- **R9** Send panel: nothing ticked, no filter; meta only in the expanded
  row.
- **R10** Format bump (notes format 3) over a sidecar file, accepting that
  already-installed builds drop the new fields on save.
- **R11** v1 adapter: GitHub only; Appendix A for the rest.
- **R12** Meta and marks are glance aids — never a mechanical consequence
  (no blocking, sending, resolving, ticking). `blocking` is therefore NOT a
  semantic (it was in the approved sections; dropped after this ruling).
- **R16** Mechanism (from the code, 2026-10-12): the format lives in each
  notes file, added meta on the note itself, types in the `notemeta` leaf.
- **R15** Meta written onto a note after its author wrote it — the user's
  Mark or another agent's double-check — is one mechanism: added-meta
  records; a remark's review JSON is never rewritten. `Confidence` moves
  into meta and leaves the note record.
- **R13** Converted tags with no declaration show in `line`.
- **R14** Glyphs optional, safe set only; the default block uses none.

## 14. Plans

1. **Core**: `notemeta`, the note fields, notes store format 3 + conversion
   + added meta on the note, `notebatch` shapes, domain operations and resolved display
   and counts, CLI, MCP, `gg session note` / agentdocs.
2. **TUI**: theme roles, note box (badges, frame, lines, details), review
   sidebar counts, send panel expanded row, Mark menu, i18n, goldens.
3. **Web**: the same surfaces, CSS tokens, browser check.
4. **Forge + skills**: the neutral comment and GitHub renderer (sandbox
   alert test first), skills and engine prompt, docs.

## Appendix A. Provider mapping reference

Research 2026-10-12 (official API docs; [test] = needs a live round trip).

| Target | GitHub | GitLab | Bitbucket Cloud | Bitbucket DC | Azure DevOps | Gerrit | Gitea/Forgejo |
|---|---|---|---|---|---|---|---|
| Review verdict | APPROVE / REQUEST_CHANGES / COMMENT | approve + `requested_changes` | approve / request-changes | APPROVED / NEEDS_WORK | vote 10/5/0/−5/−10 | Code-Review −2..+2 | APPROVED / REQUEST_CHANGES / COMMENT |
| Draft batch | PENDING review | draft_notes + bulk_publish | `pending` [test] | PENDING + `PUT /review` | — | drafts + publish | PENDING review |
| Resolve | GraphQL resolveReviewThread | discussions `resolved` | `/resolve` | `threadResolved` | thread `status` (7) | `unresolved` | 1.26+ only |
| `level` look | alerts (review comments [test]) | alerts ≥17.10 | text | text | text | text | text |
| Suggested fix | ```suggestion | ```suggestion:-N+M | UI only [test] | fence + apply API | fence | `fix_suggestions` | — |
| Non-resolvable note | — | plain note | plain comment | NORMAL | `closed`/`byDesign` | `unresolved:false` | plain |
| Structured custom data | — | — | — | read-only `properties` | thread `properties` | per-draft `tag`, change `custom_keyed_values` | — |
| Hidden text (marker) | HTML comment (hidden) | HTML comment ✓ | [test] | escaped → visible; try `[//]: #` | property | tag | HTML comment [test] |
| Edit after post | body | body | yes | yes (`version`) | yes | no | yes [test] |

Mapping notes for future adapters: `level` → alerts where they exist,
else the bold badge line; `audience` → unused unless a provider gains a
place for it; the marker → the provider's hidden channel above.

## Appendix B. The babel remarks under the suggested vocabulary

The 42 agent remarks stored for babel (11 reviews, 2026-10) plus one
reply, classified with §9's vocabulary: issue 11, note 13, good 8,
suggestion 5, nitpick 4; one remark is a test leftover. The old `severity` tag does not map
mechanically (of 19 `nit`: 4 nitpick, 8 good, 3 note, 3 suggestion,
1 issue) — the conversion (§2.5) therefore moves tags as they are and
never re-labels them. The per-remark table is
`2026-10-12-note-meta-babel-classification.md` beside this spec.
