# Colours later, everywhere a file is shown

Date: 2026-10-26 · Branch: `feat/colours-later` · Builds on the progressive
diffs (b8c700f1), chunked painting (600aa48c) and their leftovers (114364a9).

## Why

Big files are shown at once only in gg web's single-file diff pane and file
history: the text comes first, the syntax colours follow, and the browser
paints the screen first and the rest in the background. Every other place a
file is shown still does it the old way, each in its own way — colour the
whole file, then send it, then draw every line. Measured on CHANGELOG.md
(841 KB, 13.4k lines), Chromium, nothing cached:

| View | Wait | Server (colouring) | Browser (drawing all) |
|---|---|---|---|
| Blame | 2.6 s | 1.7 s | 0.9 s |
| File viewer | 2.0 s | 1.3 s | 0.7 s |
| F preview | ≈ viewer | 1.2 s | ≈0.7 s |
| Stacked view slot / refresh after staging | 1.8 s (0.05 s cached) | 1.8 s | one-shot table |
| Diff pane today | 0.05 s to text | colours ~1.7 s later | 3 screens, rest idle |

Blame and the file content endpoint colour on every request (nothing is
remembered). The TUI's blame, F preview and conflict picker colour inside
their load command (a ~1 s "loading", no freeze); its bookmark, shelf,
shelf-note and review diffs colour inline.

The user's ruling: no per-view shortcuts — one mechanism, used everywhere a
file is shown.

## Goal

Every surface that shows a file's text (diff, blame, file content) gets it
through the SAME four pieces the diff pane already uses:

1. **One colouring service** (domain): every lex of shown file content goes
   through the content-keyed token store — shared while running, cancelled
   when nobody waits, cached by content (the file viewer of a revision and
   the diff that ends at it hash the same bytes and share their runs; blame
   hashes its lines re-joined, so it shares only with itself).
2. **One "text now, colours later" contract**: a load answers the text at
   once (runs only if cached or small) plus `pending` and the content sum;
   a second request answers the runs for that sum; a frontend applies them
   only when the sum still matches what it shows.
3. **One chunked painter** (web): any host of rows paints three screens at
   once and the rest in idle slices, each chunk its own
   `content-visibility: auto` container; readers finish the paint first.
4. **One late-colour applier** (web): patches the painted rows in place,
   on-screen rows first, holding only the cells under a selection's ends.
   The TUI has one colours message path for all its views.

## Non-goals

- Sending part of a file first. The text transfer is not the slow part
  (2.6 MB in 0.04 s locally); colouring and drawing are.
- Overview HTML export and markdown fenced code (not file views; they lex
  small inputs once).
- Changing what is coloured or how (lexers, limits, the bare-CR refusal,
  the `[ui] diff_syntax` switch all keep their rules).
- The TUI's drawing (it draws only the screen already).

## Design

### 1. Domain: `Highlighter`

`domain/highlight.go` exports the token store's front door; the Service owns
ONE (built once, like `Differ()`), and the differ is given that same one.

```go
type Runs struct {
	Sum     string         // contentSum(src); "" for nil src
	Tok     [][]syntax.Tok // per source line (index = line-1); nil = plain or pending
	Pending bool           // a lex is owed: ask again with TokOnly
}
func (s *Service) Highlighter() *Highlighter
func (h *Highlighter) Runs(ctx context.Context, path string, src []byte, mode TokMode) (Runs, error)
```

- Plain for good (no Pending): syntax switch off, no lexer for `path`,
  `len(src) > MaxSyntaxBytes`.
- `TokInline` / `TokOnly`: lex through the store and wait.
- `TokDefer`: cached → runs; `len(src) ≤ InlineSyntaxBytes` → lex now;
  else `Pending`.
- The bare-CR refusal stays with the line surfaces (they check
  `HasBareCR` before asking, as today).
- The differ's per-side logic (`attach`, `tokOnly`) calls the same store;
  its behaviour does not change (its tests stay green unchanged). The cache
  is renamed `syntax-runs` (it now serves every surface; budgets are per
  factory, not per name).
- ONE switch: the Highlighter reads the Service's `syntaxOn`, which the TUI
  (`applyServicePolicies`, on load and every config reload) and gg web
  (`settings.go`) keep equal to `[ui] diff_syntax`; the TUI surfaces stop
  reading the config themselves.
- `LexBlameLines`, web `contentRows`, TUI `lexPreview` / `lexPickerDoc`
  stop calling `syntax.Lex` and ask the Highlighter.

### 2. Wire (gg web)

`/api/file-content` and `/api/blame` take the same flags as `/api/diff`:

- `defer_tok=1`: rows carry runs only when the answer had them; the body
  adds `tok_pending` and `sum`.
- `tok=1`: `{sum, tok: [runs|null per line]}` — the per-line encoding of the
  diff's `tok=1` `old`/`new`. The server re-reads the content (blame lines
  come from the blame LRU) and answers through the Highlighter with TokOnly.
- No flag: unchanged (CLI/MCP-style callers and tests).
- A working-tree file edited between the two requests answers another sum:
  its colours are dropped and the view stays plain until its own
  file-change reload (the viewer and F preview already reload on change).

The stacked view's slot loads and `quietRefreshFile` ask `/api/diff` with
`defer_tok=1`.

### 3. Web: one chunked painter

`diffpaint.js` becomes the painter of ANY row host (rename kept: it already
owns the chunk rule). A source is `{head, open, close, n, row(k), tail}`:
`open`/`close` are the chunk container's tags (`<table …>`/`</table>` for a
diff, `<div class="rchunk">`/`</div>` for line rows), `head` the first
chunk's opening (it may carry leading content, e.g. a diff's file-level
notes). Line hosts — blame, the file viewer (file and one-column diff), the
F preview — and each stacked-view slot paint through it.

Measured before choosing (13,985 CHANGELOG lines, rows built by
`renderCell`):

| Paint | Chromium first / fill / worst frame gap | Firefox |
|---|---|---|
| one-shot (today) | 447 ms / 0.54 s / 550 ms | 1234 ms / 1.2 s / 1234 ms |
| flat appends, idle slices | 18 ms / 3.2 s / 67 ms | 17 ms / 1.7 s / 17 ms |
| chunk containers, `content-visibility:auto` | 4 ms / 1.1 s / 17 ms | 9 ms / 1.1 s / 17 ms |

So line rows use the same chunk containers as the diff tables. Every reader of a
host's rows goes through that host's finishing accessor (the `diffBody()`
pattern), pinned by the existing accessor test extended to the new hosts.
Row lookups that relied on `children[i]` move to selectors / an index that
spans chunks.

### 4. Web: one late-colour applier

`difftok.js` exports one applier for every host:
`patchRows(host, scroller, rows, redraw, live)` — `rows` the painted row
elements to refresh, `redraw(el, ends)` the host's own row painter (returns
false when it left a cell holding a selection end). The diff's cell patch
and the line hosts' row painters (`paintBlameRow`, the viewer's and the F
preview's) are its `redraw`s. Rows painted later are built from the merged
data and need no patch. `fetchToks` serves both shapes (diff: `old`/`new`;
line content: `tok`). The viewer's one-column diff is a DIFF source in a
line host: its colours come through the diff shape and its row painter
maps `left_no`/`right_no`. Every writer of a host aborts that host's fetch (the
`abortDiffTok` rule, per host).

### 5. TUI: one colours path

`runDiff`/`diffTokCmd` become the pattern for every view: the load command
asks with `TokDefer`, the view shows the text, and a `tokCmd` (cancellable,
tag + sum gated) delivers the runs. Blame, F preview, the conflict picker
and the bookmark / shelf / shelf-note / review diffs use it. Closing or
re-pointing a view cancels its pending colours (the `cancelLayerTok` rule).

## Acceptance

On CHANGELOG.md, nothing cached, Chromium and Firefox:

- Blame, file viewer, F preview, a stacked-view slot: text on screen in
  ≤ 0.3 s; colours land in place later; the selection, scroll, search hits
  and cursor survive the patch.
- No long task > ~120 ms while the rest paints or the colours land.
- A second open of the same content shows its colours with the text.
- TUI blame and F preview show text without the ~1 s wait; colours follow.
- A closed / moved-on view never starts or keeps a lex (server waiters 0).
- `./test.sh race` green; the existing pins keep holding.

## Phases (one commit series each)

1. Domain Highlighter; every server-side lex of shown content through it.
2. Wire, server only: `defer_tok` / `tok=1` on file-content and blame. No
   client sends them yet.
3. Web painter + applier generalised; blame, viewer, F preview, stack slots
   and quietRefresh on them. Every client opt-in to `defer_tok` lands in
   the same commit as the applier that colours it — never a commit where a
   big file shows plain for good.
4. TUI colours path for blame, F preview, picker, bookmark/shelf/review
   diffs.
5. Docs (CHANGELOG, CLAUDE-details "Diff syntax runs"), measurements.
