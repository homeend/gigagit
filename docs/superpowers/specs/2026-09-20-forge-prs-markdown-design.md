# Forge pull requests — markdown rendering of PR text

Date: 2026-09-20 · Branch: `feat/forge-prs-markdown` · Follows
`2026-09-19-forge-prs-design.md` (ruling 7: "markdown rendering: follow-up,
plain text first") and `2026-09-19-forge-prs-web-design.md`.

## 1. Goal and scope

Pull-request text written on the forge is markdown. Today gg shows it as plain
text everywhere. This stage renders it in the **web** and the **TUI**.

Rendered text (all of it read-only forge text):

| Text | Web | TUI |
|------|-----|-----|
| PR description | details overlay | PR hub popup |
| conversation comments, review verdict bodies | details overlay | PR hub popup |
| outdated threads | details overlay | PR hub popup |
| review threads in the PR diff (root + replies) | forge note boxes | forge note boxes |

Out of scope, by ruling:

- **CLI** (`gg pr view`, `gg pr comments`) keeps printing the raw markdown —
  agents read it, and raw markdown is the right format for them.
- **Local review notes** (the user's and agents') stay exactly as typed.
- **The AI review report viewer** stays plain text (`review.js`, an earlier
  deliberate ruling).
- No network: an image is never fetched, a link is never followed by gg.
- Nothing is written to the forge. A suggestion block is shown, never applied.

## 2. User rulings (2026-09-20 — do not re-ask)

1. Surfaces: web + TUI; CLI raw.
2. A small own renderer, no markdown library.
3. Subset beyond the basics: tables, task lists, suggestion blocks, syntax
   colouring of fenced code.
4. Local notes: no markdown.
5. Web links: clickable, `http`/`https` only, new tab, `noopener noreferrer`;
   any other scheme renders as plain text.
6. TUI links: `text (url)` — text underlined, url dim; no OSC 8 escapes.
7. Images: `[image: alt]` as a link to the URL; never fetched.
8. Web code colouring: the server lexes, token runs travel on the wire.
9. One parser, in Go; the web paints a tree (follows from 8 — two parsers
   would have to agree on every fence boundary).

## 3. Architecture

```
forge text ──► internal/markdown.Parse ──► Doc (block tree)
                   │  fenced code with a known language: syntax.Lex
                   ├──► domain: wire `md` fields ──► static/markdown.js (painter) ──► HTML
                   └──► tui: mdRows(doc, width) ──► styled rows (hub popup, note boxes)
```

### 3.1 `internal/markdown` (new package)

Pure: stdlib + `internal/syntax`. No git, domain, TUI or web imports. One
entry point per need:

```go
func Parse(src string) Doc                // total: never fails, never panics
func Summary(src string) (summary, rest string) // the thread split, §6
func PlainInline(src string) string       // one line, markers stripped
```

`Doc` is a JSON-serialisable tree. The JSON shape IS the wire contract, so it
is fixed here:

```go
type Doc struct { Blocks []Block `json:"blocks"` }

type Block struct {
    Kind   string     `json:"k"`              // p h list quote code table hr
    Level  int        `json:"level,omitempty"`  // h: 1–6
    Inline []Inline   `json:"in,omitempty"`     // p, h
    Ordered bool      `json:"ordered,omitempty"`// list
    Start  int        `json:"start,omitempty"`  // list: first number
    Items  []Item     `json:"items,omitempty"`  // list
    Blocks []Block    `json:"blocks,omitempty"` // quote
    Lang   string     `json:"lang,omitempty"`   // code: info string's first word
    Lines  []CodeLine `json:"lines,omitempty"`  // code
    Head   [][]Inline `json:"head,omitempty"`   // table
    Rows   [][][]Inline `json:"rows,omitempty"` // table
    Align  []string   `json:"align,omitempty"`  // table: "" left center right
}
type Item struct {
    Task   string  `json:"task,omitempty"` // "" | "open" | "done"
    Blocks []Block `json:"blocks"`
}
type CodeLine struct {
    Text string    `json:"t"`
    Toks []WireTok `json:"toks,omitempty"` // rune offsets + class name
}
type WireTok struct { Start int `json:"s"`; End int `json:"e"`; Class string `json:"c"` }
type Inline struct {
    Kind string   `json:"k"`            // text strong em del code link image ref br
    Text string   `json:"t,omitempty"`  // text, code, ref; image: the alt
    URL  string   `json:"url,omitempty"`// link, image — ONLY ever http(s), see §5
    In   []Inline `json:"in,omitempty"` // strong, em, del, link
}
```

Supported syntax (GitHub's everyday subset, not full CommonMark):

- Blocks: ATX headings (`#`…`######`), paragraphs, bullet lists (`-`, `*`,
  `+`), ordered lists (`1.`, `1)`), nesting by indentation, task items
  (`- [ ]`, `- [x]`), block quotes (`>`, nestable), fenced code (``` and
  `~~~`, info string), pipe tables with an alignment row, thematic breaks.
  An indented code block is a paragraph (rare in PR text, ambiguous with
  list continuation).
- Inline: `**strong**`/`__strong__`, `*em*`/`_em_` (with GitHub's intraword
  rule for `_`), `~~del~~`, `` `code` `` (any backtick run length), links
  `[text](url "title")`, autolinks `<https://…>` and bare `https://…`,
  images `![alt](url)`, `@mention` and `#123` refs (kind `ref`, no URL, no
  action), hard breaks (two trailing spaces, backslash, and — as GitHub does
  in comments — every single newline inside a paragraph), backslash escapes.
- Not supported, rendered as the literal text: raw HTML (incl. `<details>`,
  `<img>`, comments), reference-style links, footnotes, setext headings,
  emoji shortcodes, math.
- A `suggestion` fence is an ordinary code block with `Lang == "suggestion"`;
  frontends label it. It is not lexed.

Robustness: `Parse` is linear-ish and bounded — input over 256 KiB, nesting
over 8 levels, or a table over 64 columns degrades to paragraphs/text rather
than recursing or allocating without bound. Unclosed constructs fall back to
literal text (an unclosed fence runs to the end, as on GitHub). `\r\n` is
normalised. A fuzz test asserts no panic and that the concatenated text
leaves contain every non-marker rune of the input.

Code colouring: `Parse` maps the fence language through a small alias table
to a `syntax` lexer name (`syntax.Detect("x."+lang)` plus aliases such as
`sh`/`bash`, `js`, `ts`, `py`, `yml`, `golang`) and calls `syntax.Lex`. An
unknown language, or a block over 2 000 lines, has no `Toks`. Lexing is
synchronous inside `Parse`; callers already run off the UI thread (§4, §7).

### 3.2 Domain

- `forgeNote` (`forge_notes.go`) splits the body with `markdown.Summary`
  instead of `strings.Cut` (§6).
- `WireNote` gains `MD *MarkdownDoc` (`json:"md,omitempty"`, the parsed
  RATIONALE) and `SummaryMD []markdown.Inline` (`json:"summary_md,omitempty"`,
  the summary line's inline tree). They are OPT-IN: only
  `ToWireNoteRendered` — the web's builder — fills them, only for forge
  notes (replies included). `ToWireNote` / `ToWireNotePreview`, which the CLI
  and MCP hand to agents, never carry a tree, and a local note is never
  parsed. The summary's source line travels on `ResolvedNote.SummarySrc` (a
  domain type; nothing new is stored).
- `model.ForgeComment` and `model.PullRequest` stay plain data (model cannot
  import markdown's lexer-backed tree without pulling `syntax` into `model`).
  The web details answer wraps them instead (§4).
- Parsing is not cached separately: it is cheap next to the forge read, runs
  on already-cached text, and both consumers are off the hot path. (If a
  profile ever disagrees, the PR cache entry is the place.)

**R2 holds:** parsing is local; no route added here calls the forge.

## 4. Web

Wire:

- `GET`/`POST /api/pr/details`: `pr` gains a sibling `body_md` (Doc); every
  entry of `hub` and `outdated` is sent as the comment plus `md` (Doc). The
  raw `body` strings stay on the wire (copy, tests, fallback).
- `GET /api/pr/notes`: forge `WireNote`s carry `md` / `summary_md` (§3.2).

`static/markdown.js` — new, import-free (node-testable like `notebox.js`):

```js
export function mdHTML(doc, esc)        // Doc → HTML string
export function mdInlineHTML(inl, esc)  // []Inline → HTML string
```

Safety is structural, not a sanitizer:

- Every string from the tree passes through `esc()`; the painter's own tags
  are a closed list: `p h3…h6 ul ol li blockquote pre code table thead tbody
  tr th td hr strong em del a span br`.
- Headings render one size class per level but never as `<h1>/<h2>` (the
  overlay owns those); level 1–6 → `.md-h1`…`.md-h6` on `h3`…`h6`.
- `<a>` is emitted only when the URL matches `^https?://` (case-insensitive)
  — re-checked in the painter even though the parser already enforces it —
  with `target="_blank" rel="noopener noreferrer"` and a `title` of the URL.
  Anything else paints as its text.
- An image paints as a link whose text is `[image: alt]` (`[image]` when the
  alt is empty). No `<img>` is ever emitted.
- A task item paints a `☐`/`☑` glyph as text; no input element.
- Code lines paint token spans with the existing `.tk-*` classes; the
  selector-scoped `.tk-*` rules in `style.css` are extended to `.md-code`.
  A `suggestion` block gets a `suggestion` caption row.
- `align` becomes a class (`md-al-center`/`md-al-right`), never a style
  attribute.

Consumers:

- `prdetails.js`: description, conversation, verdicts, outdated threads use
  `mdHTML(x.md)`; a missing `md` falls back to today's `esc(body)`.
- `files.js` `noteBoxHTML`: a forge box's summary uses
  `mdInlineHTML(n.summary_md)`, its rationale `mdHTML(n.md)`; local boxes are
  untouched. `notebox.js` `noteTitle` (collapsed row, tooltips) keeps using
  the plain `summary`, which §6 already strips of markers.
- CSS: `.md` block styles (lists, quotes, code, tables scroll horizontally
  inside their box, never widening the overlay or the diff table).
- Clicking a link inside a note box must not toggle the box or open the ◆
  menu: the existing box click handlers ignore clicks whose target is an
  `a[href]`.

## 5. The URL rule

The parser resolves a link/image destination once: trimmed, and kept only if
it matches `^https?://` and holds no control characters or whitespace.
Otherwise the node is demoted to text: its inline children (or the image
label), followed by ` (dest)` so the reader still sees a relative path or a
`mailto:` — except a `javascript:`, `data:` or `vbscript:` destination, which
is dropped from view entirely. Both frontends therefore
never see a non-http URL in `URL`; the web painter checks again anyway.

## 6. The thread summary/rationale split

Today: summary = the body's first line, rationale = the rest. With markdown
the first line can be a fence opener, a table row, or `## Heading`.

`markdown.Summary(body)`:

1. Parse. Take the first block.
2. Paragraph or heading → summary = `PlainInline` of its FIRST line; the rest
   of that paragraph plus all later blocks = rationale source.
3. List → summary = `PlainInline` of the first item's first line; rationale
   = the WHOLE body (the list must stay a list).
4. Quote, code, table, hr → summary = a label (`quote`, `code`,
   `suggestion`, `table`, `—`); rationale = the whole body.
5. Empty body → both empty.

`Note.Summary` therefore stays a clean one-line plain string — collapsed
rows, the notes list, `gg pr comments`-independent surfaces, the node
`noteTitle` helper and the TUI's bold row all keep working — and a
plain-prose body splits exactly as it does today. `summary_md` (web) and the
TUI's summary row render the same first line WITH inline styling (code
spans, emphasis), computed from the same line.

CLI is unaffected: it prints `ForgeComment.Body`, not the note split.

## 7. TUI

One renderer, `internal/tui/md_render.go`:

```go
type mdRow struct {
    text string
    cls  []syntax.Class // per-rune class mask (code colouring) — nil when none
    emph []mdEmph       // per-rune inline style: strong em del code link url ref
    kind mdRowKind      // para heading quote code table rule listItem blank
    noWrap bool         // code + table rows: clipped, never reflowed
}
func mdRows(doc markdown.Doc, width int) []mdRow
```

- Wrapping happens here, at the given width, with a hang indent for list
  items (`- `, `1. `, `☐ `/`☑ `) and quotes (`│ `). Code and table rows are
  not wrapped; they are clipped by the host like any over-long row. Tables
  are laid out as aligned columns from measured display widths
  (`lipgloss.Width`, wide glyphs count 2), each column capped so the table
  fits when it can; an over-wide table clips.
- Styles come from theme roles. New roles (each added to `roleFields` /
  `RoleDocs`, all three themes): `md_heading`, `md_code_bg` (inline + block
  code), `md_quote`, `md_link`. Emphasis uses bold/italic/strikethrough
  attributes; the `(url)` tail uses the existing dim role. Every string is
  passed through the existing `sanitizeLine` (ANSI/control stripping) BEFORE
  styling — forge text must never inject escapes.
- **PR hub popup:** `contentLine` gains an optional per-rune emphasis mask
  beside its existing `cls`; the hub builds its body from `mdRows`. The hub
  is built at message time but can be maximised (ctrl+t) later, so the hub
  keeps the parsed `Doc`s and re-lays its lines when the popup width changes
  (the same place the popup already recomputes its window). Search (`/`),
  copy line, and the cursor operate on the rendered text.
- **Forge note boxes (diff view):** `noteLine` gains the same masks;
  `noteBoxLines` renders a forge note's summary from its inline tree and its
  rationale from `mdRows(doc, innerW-indent)`. Local notes take today's path
  unchanged. Parsing happens when notes load (`notesLoadedMsg`, already
  async), not per frame; rows are re-laid on resize from the kept `Doc`.
- The user-visible labels (`suggestion`, `[image: …]`, `quote`, `code`,
  `table`) are i18n keys in all four bundles for the TUI; the domain-made
  summary labels of §6 are English protocol text translated at render.

## 8. Testing and verification

- `internal/markdown`: a fixture table `testdata/*.md` → golden `*.json`
  (the wire tree), plus unit tests for `Summary`, `PlainInline`, the URL
  rule, the bounds, and a fuzz test (`FuzzParse`: no panic, text preserved).
- Web painter: a node test feeds the SAME golden JSON to `markdown.js` and
  asserts the HTML; a hostile set (`<script>`, `<img onerror>`,
  `[x](javascript:alert(1))`, `[x](JaVaScRiPt:…)`, `"` and `'` in URLs and
  titles, a class name injected through `lang`) asserts no tag or attribute
  outside the closed list appears. A static test asserts `markdown.js` is
  import-free and that `prdetails.js`/`files.js` never `innerHTML` a forge
  body that did not come through `mdHTML`/`esc`.
- Domain/web Go tests: `md` present on forge wire notes and absent on local
  ones; `body_md` on both details verbs; the GET still never calls the
  forge.
- TUI: `mdRows` table tests (wrap, hang indent, table layout, wide glyphs,
  ANSI stripped), hub + note box snapshot tests, i18n gates.
- Browser (real chromium against the fake gh; the fixture bodies gain
  markdown + the hostile strings): assert VISIBILITY of a rendered
  `<code>`, `<td>`, `<a href^="https">`, a coloured `.tk-*` span inside
  `.md-code`, a task glyph; assert `window.__pwned` is unset and no `<img>`
  / `<script>` exists under the overlay or a note box; assert a local note
  containing `**x**` still shows the asterisks. Each check is watched
  failing with its guard removed; a script abort counts as a failure.
- TUI under `tui-capture.sh`: hub popup and a PR diff thread with a list, a
  code block and a table, at two widths and maximised.
- `./test.sh race` from a clean tree before asking to merge.

## 9. Docs

`CHANGELOG.md`, `README.md` (PR section), `docs/web-tui-parity.md`,
`docs/CLAUDE-details.md` (forge sections), the theme role docs / config
settings registry for the new roles, and one new row in `CLAUDE.md`'s package
map for `markdown`. `using-gg.md` is unchanged (no CLI surface change).

## 10. Plan split

Two plans, each independently mergeable:

1. **Core + web:** `internal/markdown`, the domain wire fields, `markdown.js`,
   the overlay and note boxes, browser verification.
2. **TUI:** theme roles, `mdRows`, hub popup, note boxes, tui-capture
   verification.
