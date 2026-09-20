# Forge PR markdown — plan 1: parser core + web

> Executed inline by the session that wrote it (project rule: NEVER subagents),
> task by task, TDD, one commit per task. Steps use `- [ ]` for tracking.

**Goal:** PR descriptions, conversation comments, verdict bodies and review
threads render as markdown in `gg web`; the parser they share lands as a leaf
package the TUI plan (plan 2) reuses.

**Architecture:** one parser in Go (`internal/markdown`) → a JSON block tree;
the domain/web wire carries the tree beside the raw text; `static/markdown.js`
is an import-free PAINTER (tree → HTML) that escapes every leaf and only emits
`href` for http(s). No JS parser, no sanitizer, no new dependency.

**Spec:** `docs/superpowers/specs/2026-09-20-forge-prs-markdown-design.md`
(§3–§6, §8). Plan 2 covers §7.

## Global constraints

- Worktree `/mnt/t/others/gigagit/.claude/worktrees/forge-prs-markdown`,
  branch `feat/forge-prs-markdown`; every command `cd`s there / uses
  `git -C`; Write/Edit use the worktree's absolute paths.
- R1 wire = PR number · R2 a GET never calls the forge · R3 hidden-by-id.
- `internal/markdown` imports stdlib + `internal/syntax` only.
- Local notes and the review report viewer stay plain; the CLI is untouched.
- New Go tests call `t.Parallel()`. Node tests skip when node is absent.
- Commits end with the two trailers (Co-Authored-By, Claude-Session).
- Restore a guard-mutated file by re-writing the saved text, never
  `git checkout` (lesson from plan 5).

## File map

| File | Role |
|------|------|
| `internal/markdown/doc.go` | package doc + the tree types (spec §3.1, JSON tags verbatim) |
| `internal/markdown/block.go` | line-based block parser |
| `internal/markdown/inline.go` | inline parser + the URL rule |
| `internal/markdown/code.go` | fence language → `syntax.Lex` → `WireTok` |
| `internal/markdown/summary.go` | `Summary`, `PlainInline` |
| `internal/markdown/testdata/*.md` + `*.json` | golden fixtures (shared with the node test) |
| `internal/archtest/import_guard_test.go` | leaf budget for `markdown` |
| `internal/domain/forge_notes.go`, `notewire.go` | split via `markdown.Summary`; `md`/`summary_md` |
| `internal/web/prnotes.go` | `body_md`, per-comment `md` on both details verbs |
| `internal/web/static/markdown.js` | painter |
| `internal/web/static/prdetails.js`, `files.js`, `style.css` | consumers + `.md` styles |
| `internal/web/markdownjs_test.go`, `prnotes_static_test.go` | node + static guards |

---

### Task 1: tree types, leaf guard, block parser

**Files:** create `internal/markdown/{doc.go,block.go,block_test.go}`; modify
`internal/archtest/import_guard_test.go` (add
`"markdown": {"git","engine","domain","tui","cli","mcp","web","app","model","config","i18n","theme"}`
to the forbidden-imports table next to `syntax`).

**Produces:** `Parse(src string) Doc`, types `Doc Block Item CodeLine WireTok
Inline` exactly as spec §3.1. In this task inline content is a single
`{k:"text"}` leaf per line group (Task 2 replaces `parseInline`), and code
lines carry no `Toks` (Task 3).

- [ ] Step 1 — failing tests (`block_test.go`), table-driven over
  `kinds(Parse(src))` (a helper rendering the tree as a compact s-expr, e.g.
  `h2 p list(item(p) item[done](p list(item(p)))) quote(p) code:go[2] table[2x3] hr`):
  - `"# T\n\ntext"` → `h1 p`
  - `"####### x"` → `p` (7 hashes is not a heading); `"#x"` → `p`
  - `"- a\n- b\n  - c\n\npara"` → `list(item(p) item(p list(item(p)))) p`
  - `"1. a\n2) b"` → two lists? No: GitHub starts a new list on a delimiter
    change → `list(item(p)) list(item(p))`; `"3. a"` → `Start == 3`
  - `"- [ ] a\n- [x] b\n- [X] c"` → tasks `open done done`
  - `"> q\n> > deep\nlazy"` → `quote(p quote(p))` with the lazy line joined
  - "```go\nx := 1\n```" → `code:go[1]`; `~~~` fence; a fence of 4 backticks
    is not closed by 3; an unclosed fence runs to EOF; fence inside a list
    item stays in the item
  - "```suggestion\nx\n```" → `code:suggestion[1]`
  - `"a | b\n:-|-:\n1 | 2"` and the `|`-bordered form → `table[1x2]`, `Align
    == ["left","right"]`; a row with fewer cells pads, more cells drops; an
    escaped `\|` stays in the cell; a header without a valid delimiter row
    → `p`
  - `"---"`, `"***"`, `"_ _ _"` → `hr`; `"--"` → `p`
  - `"    indented"` → `p` (no indented code)
  - `"<script>alert(1)</script>"` → `p` whose text is the literal string
  - `"a\r\nb"` normalised; empty / whitespace-only → zero blocks
  - bounds: 9 nested quotes → depth capped at 8, the rest literal text;
    300 KiB input → a single `p`-per-line-group doc without block parsing
    (assert no panic + `len(Blocks) > 0`); 70-column table → `p`
- [ ] Step 2 — run `go test ./internal/markdown/` → FAIL (no package).
- [ ] Step 3 — implement. `block.go` is a recursive line-container parser:
  `parseBlocks(lines []string, depth int) []Block`. Loop over lines: blank →
  close paragraph; fence opener (`^ {0,3}(`{3,}|~{3,})(info)`, info holds no
  backtick for backtick fences) → consume to the matching closer (same char,
  run ≥ opener) or EOF, strip up to the opener's indent from each line; ATX
  heading; thematic break (checked BEFORE list marker so `* * *` is an hr);
  quote (`^ {0,3}> ?`) → gather the run incl. lazy continuation lines of an
  open paragraph, strip markers, recurse with depth+1; list marker
  (`^( *)([-*+]|\d{1,9}[.)])( +|$)`) → gather the item: following lines
  indented ≥ content column, or blank lines followed by such a line; strip
  the content indent, recurse; a sibling is the same marker type (bullet
  char class / ordered delimiter) at the same indent; task marker
  `^\[( |x|X)\] ` on the item's first line; table: a paragraph-start line
  containing `|` followed by a delimiter row `^\s*\|?\s*:?-+:?\s*(\|\s*:?-+:?\s*)*\|?\s*$`
  with the same cell count (≤ 64) → rows until a blank line or a line
  without `|`; else paragraph text. Cell split honours `\|` and backtick
  spans. `depth > 8` → treat the rest as paragraph lines. `len(src) > 256<<10`
  → paragraphs split on blank lines only.
- [ ] Step 4 — tests PASS; `go test ./internal/archtest/` PASS.
- [ ] Step 5 — commit `feat(markdown): block parser for forge PR text`.

### Task 2: inline parser + URL rule

**Files:** create `internal/markdown/{inline.go,inline_test.go}`; `block.go`
calls `parseInline(text string) []Inline` for p / h / table cells.

- [ ] Step 1 — failing tests over `inl(src)` (s-expr of the inline tree):
  - `**a** __b__ *c* _d_ ~~e~~` → `strong(a) strong(b) em(c) em(d) del(e)`
    with text leaves between; `snake_case_name` → plain text (intraword `_`);
    `**a *b* c**` nests; unclosed `**a` → literal
  - `` `x` ``, ``` ``a`b`` ```, `` `**no**` `` → code leaf, no inner parsing
  - `[t](https://e.x/p "title")` → `link[https://e.x/p](t)`; `[**b**](http://x)`
    nests; `[t](<https://e.x/a b>)` rejected (whitespace) → text
  - autolinks: `<https://a.b>`; bare `see https://a.b/c.` → link without the
    trailing `.`; `(https://a.b)` → link without the `)`; `www.x.y` → text
  - `![alt](https://i/x.png)` → `image[https://i/x.png](alt)`; empty alt ok
  - URL rule (spec §5): `[x](javascript:alert(1))`, `[x](JaVaScRiPt:1)`,
    `[x](data:text/html,1)`, `[x](vbscript:1)` → text `x` ONLY (no dest);
    `[x](../rel.md)` and `[x](mailto:a@b)` → text `x (../rel.md)`; a URL
    with `\x00`, `\n`, `"`-only is kept only if it still matches
    `^https?://[^\s\x00-\x1f]+$`
  - `@octo-cat`, `#123`, `org/repo#9` → `ref`; `a@b.c` and `x#1y` → text;
    `#` alone → text
  - hard breaks: `a\nb` → `text br text`; two trailing spaces; trailing `\`
  - escapes: `\*not\*` → literal `*not*`; `\\` → `\`
  - `<b>x</b>` and `<!-- c -->` → literal text leaves
  - adjacent text leaves are merged (no two `text` siblings)
- [ ] Step 2 — run → FAIL.
- [ ] Step 3 — implement a single left-to-right scanner with a delimiter
  stack (strong/em/del), recursion for link text, depth cap 8, and
  `safeURL(dest string) (string, verdict)` where verdict ∈ ok | show | drop.
  No regexp backtracking on user text: hand-rolled scans only.
- [ ] Step 4 — tests PASS (Task 1's still green).
- [ ] Step 5 — commit `feat(markdown): inline parser, http(s)-only links`.

### Task 3: code colouring, Summary, PlainInline, goldens, fuzz

**Files:** create `internal/markdown/{code.go,summary.go,summary_test.go,golden_test.go,fuzz_test.go}`,
`testdata/{basic,lists,code,table,hostile,thread}.md` + `.json`.

- [ ] Step 1 — failing tests:
  - `code.go`: "```go\nfunc main() {}\n```" → line 0 has a `kw` tok at
    `[0,4)`; aliases `golang sh bash shell js ts py yml` resolve; `suggestion`,
    unknown `zzz`, and a 2 001-line block → no toks; tok offsets are RUNE
    offsets (a line with `"é"` before a keyword).
  - `Summary` (spec §6): `"Fix **this**\n\nbecause"` → (`Fix this`,
    `because`); `"line1\nline2\n\nmore"` → (`line1`, `line2\n\nmore`);
    `"## Title\nbody"` → (`Title`, `body`); `"- a\n- b"` → (`a`, whole
    body); "```suggestion\nx\n```" → (`suggestion`, whole body); table →
    `table`; quote → `quote`; `"---"` → `—`; `""` → (`""`, `""`); a plain
    prose body equals today's `strings.Cut` + TrimSpace result (property
    test over 6 samples).
  - `PlainInline("see `x` and [t](https://u) **b**")` → `see x and t b`.
  - golden: for each `testdata/*.md`, `json.MarshalIndent(Parse(src))`
    equals `*.json` (`-update` flag rewrites). `hostile.md` holds
    `<script>window.__pwned=1</script>`, `<img src=x onerror=…>`, the
    javascript/data links, a `lang` of `"><script>`, an ANSI `\x1b[31m`, and
    `"`/`'` in a URL and a title.
  - `FuzzParse`: no panic; every letter/digit rune of the input appears in
    the tree's concatenated text + code + urls (seed corpus = testdata).
- [ ] Step 2 — run → FAIL.
- [ ] Step 3 — implement; `lexName(lang)` = alias map, else
  `syntax.Detect("x." + strings.ToLower(lang))`; `Lang` is kept only if it
  matches `^[A-Za-z0-9_+#.-]{1,32}$`, else `""` (so a hostile info string
  never reaches a class name).
- [ ] Step 4 — `go test ./internal/markdown/` PASS; run
  `go test -fuzz=FuzzParse -fuzztime=30s ./internal/markdown/` once, fix
  anything found, keep new corpus entries.
- [ ] Step 5 — commit `feat(markdown): code tokens, thread summary split, goldens + fuzz`.

### Task 4: domain — the split and the wire fields

**Files:** modify `internal/domain/forge_notes.go` (`forgeNote`),
`internal/domain/notewire.go`; tests in `forge_notes_test.go`,
`notewire_test.go` (extend `TestWireNoteForgeFields`).

**Produces:** `WireNote.MD *markdown.Doc` (`json:"md,omitempty"`),
`WireNote.SummaryMD []markdown.Inline` (`json:"summary_md,omitempty"`);
`domain.ParseMarkdown(src string) *markdown.Doc` (nil for empty text) — the
one door web handlers use, so `web` never imports `markdown` directly
(archtest: web is domain-only).

- [ ] Step 1 — failing tests: a forge comment body "```suggestion\nx\n```"
  yields `Summary == "suggestion"` and a rationale holding the fence; a
  plain two-line body splits as before; `ToWireNote` of a forge thread has
  `md` on root AND reply when the rationale is non-empty, `summary_md`
  non-empty, and a user/agent note whose summary is `**x**` has neither
  field (assert on the marshalled JSON: no `"md"` key).
- [ ] Step 2 — run → FAIL.
- [ ] Step 3 — implement. `markdown.Summary` returns a third value, the RAW
  first line (markers intact); `markdown.ParseInline` is exported. The raw
  line is NOT stored on `model.Note` (a stored type): `ResolvedNote` (a
  domain type) gains `SummarySrc string`, filled by `forgeNote`, empty for
  local notes. `ToWireNote` sets `SummaryMD = ParseInline(r.SummarySrc)` and
  `MD = ParseMarkdown(r.Note.Rationale)` only when the source is forge.
- [ ] Step 4 — `go test ./internal/domain/ -run 'Forge|WireNote'` PASS, then
  the whole package.
- [ ] Step 5 — commit `feat(domain): forge threads split on markdown blocks, md on the wire`.

### Task 5: web wire — details

**Files:** modify `internal/web/prnotes.go` (`prDetailsBody`); tests in
`internal/web/prnotes_test.go`.

- [ ] Step 1 — failing tests: POST and cached GET both answer `body_md`
  (object with `blocks`) when the PR body is non-empty and omit it when
  empty; every `hub[i]` / `outdated[i]` has the comment's fields PLUS `md`;
  raw `body` strings unchanged; `TestPRDetailsGetIsCacheOnly` still proves
  zero forge calls.
- [ ] Step 3 — implement: a `wireComment` struct embedding
  `model.ForgeComment` + `MD *markdown.Doc \`json:"md,omitempty"\`` — typed
  via a domain alias (`domain.MarkdownDoc = markdown.Doc`) so web stays
  domain-only; `body_md` from `domain.ParseMarkdown(pr.Body)`.
- [ ] Step 5 — commit `feat(web): details answer carries the parsed markdown`.

### Task 6: `static/markdown.js` painter

**Files:** create `internal/web/static/markdown.js`,
`internal/web/markdownjs_test.go`.

**Produces:** `mdHTML(doc, esc)`, `mdInlineHTML(inl, esc)`; import-free; `esc`
is injected (node test passes its own copy of core.js's `esc`).

- [ ] Step 1 — failing node test (pattern of `noteboxjs_test.go`): copy
  `markdown.js` + every `internal/markdown/testdata/*.json` into a temp dir,
  paint each, and assert:
  - basic: `<h3 class="md-h md-h1">`, `<strong>`, `<em>`, `<del>`,
    `<code class="md-ic">`, `<a href="https://e.x/p" target="_blank"
    rel="noopener noreferrer" title="https://e.x/p">`
  - lists: `<ul class="md-list">`, `<ol start="3">`, task glyphs `☐ ` / `☑ `
    inside `<li class="md-task">`
  - code: `<pre class="md-code" data-lang="go"><code>` with
    `<span class="tk-kw">func</span>`; suggestion block preceded by
    `<div class="md-cap">suggestion</div>`
  - table: `<table class="md-table">`, `<th class="md-al-right">`
  - image: `<a …>[image: alt]</a>`, never `<img`
  - hostile: output contains no `<script`, `<img`, `onerror=`, `javascript:`,
    `style=`; every `<` in the output opens a tag from the closed list
    (regex over the output); a hand-built tree with `url: "javascript:x"`,
    `k: "script"`, `lang: '"><script>'`, `align: ['x" onload="y']`, and a
    tok class `'x" onload="'` paints inert text / is dropped (the painter
    whitelists kinds, `^https?://` URLs, `^[a-z]{1,4}$` tok classes,
    `left|center|right` aligns, `^[A-Za-z0-9_+#.-]{1,32}$` langs).
  - `mdHTML(null)`/`undefined`/`{}` → `""`.
- [ ] Step 3 — implement (one `switch` per kind; unknown kind → its escaped
  text or nothing).
- [ ] Step 5 — commit `feat(web): markdown.js paints the parsed tree, safe by construction`.

### Task 7: consumers + styles

**Files:** modify `static/prdetails.js`, `static/files.js` (`noteBoxHTML`
+ the note click handlers), `static/style.css`; extend
`internal/web/notesjs_test.go` (`TestForgeNoteBoxesJS`: inline `markdown.js`
like `noteboxInline`) and `prnotes_static_test.go`.

- [ ] Step 1 — failing tests: node — a forge note with `md` renders
  `.notetext.md` containing `<ul`, with `summary_md` renders `<code` inside
  `.notesum`; a LOCAL note with summary `**x**` renders the literal
  asterisks and no `.md`; a forge note WITHOUT `md` falls back to
  `esc(rationale)`. Static — `prdetails.js` and `files.js` import
  `./markdown.js`; `style.css` has `#prdetails .tk-kw` and `.md-code`;
  `prdetails.js` has no `esc(pr.body)` left outside the fallback branch
  (assert the fallback is the only `pr.body` use).
- [ ] Step 3 — implement:
  - `prdetails.js`: `const body = (md, raw) => md ? `<div class="prd-text md">${mdHTML(md, esc)}</div>` : `<div class="prd-text">${esc(raw || "")}</div>``
    for the description (`d.body_md`), comments and outdated roots (`c.md`).
  - `files.js`: `part`/`text` take the note; forge notes use
    `mdInlineHTML(n.summary_md, esc)` (the `↳ author: ` head stays escaped
    text in front) and `mdHTML(n.md, esc)`.
  - click handlers on note boxes / the overlay: `if (e.target.closest("a[href]")) return;`
    first, so a link click neither folds the box nor opens the ◆ menu.
  - CSS: `.md` block spacing, `.md-h1…6` sizes, `.md-list`, `.md-task`
    (no bullet), `blockquote.md-q`, `.md-ic`, `pre.md-code` (`overflow-x:
    auto; max-width: 100%`), `.md-cap`, `.md-table` inside an
    `overflow-x: auto` wrapper, `.md a`, `.md-ref`; `.tk-*` selector list
    gains `#prdetails .tk-*` (note boxes already sit under `table.diff`).
    `.prd-text.md` and `.notetext.md` drop `white-space: pre-wrap` (the
    tree carries its own breaks) — check the current rule first.
- [ ] Step 4 — `go test ./internal/web/` PASS.
- [ ] Step 5 — commit `feat(web): PR details and review threads render markdown`.

### Task 8: browser verification (real chromium, fake gh)

**Files (scratchpad, not committed):** `prweb/threads-7.base.json` +
details fixture bodies gain: a heading, a nested/task list, a `go` fence, a
`suggestion` fence, a table, a link, an image, a mention, and the hostile
strings; new `pw/md.mjs`; `run5.sh` node line → `md.mjs`; `guards.py` gains
the guards below. First `curl /api/repo` to prove the port is mine; binary
identified by md5 + `GET /static/markdown.js` = 200.

- [ ] Checks (each asserts VISIBILITY — `isVisible()` + non-zero box):
  overlay: `.md-h`, `li.md-task`, `pre.md-code .tk-kw` with a computed
  colour ≠ the plain text colour, `.md-cap` "suggestion", `td`,
  `a[href^="https"][target=_blank][rel~=noopener]`, `[image: …]` link;
  `window.__pwned === undefined`; zero `script, img` under `#prdetails-body`
  and under `tr.note`; thread box in the PR diff: `.notesum code`,
  `.notetext.md ul`; a link click inside a box leaves it expanded (popup
  blocked/captured, no navigation of the page); a LOCAL note typed as
  `**x**` shows asterisks; cached re-open (stale-first) still renders md;
  `prs5.mjs` + `det.mjs` re-run green (regex updates only where the text
  moved into tags).
- [ ] Guards removed one at a time, each must turn its check red, a script
  abort = FAIL: the `.tk-*` `#prdetails` selector; the `md ?` branch in
  prdetails (falls back to plain); the painter's URL whitelist (hand-fed
  tree via `page.evaluate`); the `a[href]` click guard; `body_md` on the
  cached GET.
- [ ] Re-run all green on the restored tree; `git status` clean.

### Task 9: docs, gate, delivery

- [ ] `CHANGELOG.md`, `README.md` (PR section: what renders, what does not,
  CLI stays raw), `docs/web-tui-parity.md` (web renders markdown; TUI pending
  plan 2), `docs/CLAUDE-details.md` (forge web section: tree on the wire,
  painter whitelist, the split rule), `CLAUDE.md` package map row:
  `markdown` — "Pure GitHub-subset markdown parser for forge text: `Parse` →
  JSON block tree (the web wire shape) with chroma token runs on fenced code,
  `Summary` (thread split), http(s)-only URLs. Leaf: stdlib + `syntax`."
  Spec: tick the §10 plan-1 line. Commit `docs: markdown rendering of PR text (web)`.
- [ ] `gofmt -l`, `go vet ./...`, then `./test.sh race` from a CLEAN tree
  (start it at once; never wait for other sessions).
- [ ] Build the stripped verify binary in the worktree, SendUserFile + the
  absolute path; report; ASK before merging. After the merge:
  `./build.sh install`, `./build.sh web`, remove worktree? — NO: plan 2
  continues on a fresh branch off the new main; remove this worktree and
  branch, update memory `forge-prs-feature.md`.
