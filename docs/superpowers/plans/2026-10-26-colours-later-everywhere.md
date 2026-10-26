# Colours later, everywhere — Implementation Plan

> Executed inline by the session that wrote it (no implementer subagents).

**Goal:** every surface that shows a file's text gets it through one
colouring service, one text-now/colours-later contract, one chunked painter
and one late-colour applier.

**Spec:** `docs/superpowers/specs/2026-10-26-colours-later-everywhere-design.md`

## Global constraints

- Lexing rules unchanged: `MaxSyntaxBytes` (1 MB), `InlineSyntaxBytes`
  (64 KB), the bare-CR refusal on line surfaces, `[ui] diff_syntax`.
- Late runs apply only when the content sum matches what is shown.
- Every writer of a host's rows aborts that host's colour fetch; every
  reader of a host's rows finishes its paint first (accessor pattern).
- TUI strings through `i18n.T` (no new strings expected).
- Each client opt-in to `defer_tok` lands with its applier (no plain-forever
  commit).

## Review focus

1. A worktree file edited between text and colours: colours dropped, view
   plain until its reload — never wrong colours.
2. A host re-rendered / closed while its colours are in flight: no patch on
   the new content, server lex cancelled.
3. Search, `:` go-to-line, cursor moves, note boxes, range marks on a host
   still painting: they finish the paint first and land right.
4. CRLF / no trailing newline / bare CR files: line counts of runs match the
   rows (blame joins lines; file content splits on `\n`).
5. Syntax switched off at runtime: no pending, no fetch, plain rows.

---

### Phase 1 — domain colouring service

**Task 1: `Highlighter`** (`internal/domain/highlight.go`, `service.go`,
`differ.go`)
- Produces: `type Runs struct{Sum string; Tok [][]syntax.Tok; Pending bool}`;
  `func (s *Service) Highlighter() *Highlighter`;
  `func (h *Highlighter) Runs(ctx, path string, src []byte, mode TokMode) (Runs, error)`;
  `func (h *Highlighter) Content(ctx, path string, data []byte, mode TokMode) (Runs, error)`
  (adds the bare-CR refusal); `func (h *Highlighter) Blame(ctx, path string, lines []model.BlameLine, mode TokMode) (Runs, error)`
  (joins lines + "\n", bare-CR refusal).
- The Service builds one store over cache `syntax-runs`; `Differ()` gets the
  same store (`DifferOptions.Highlighter`; `Tokens` kept for tests).
- Tests (`highlight_test.go`): plain for no lexer / switch off / > Max;
  Defer pends a > 64 KB side and lexes a small one; a stored side answers
  without the lex seam; Only/Inline wait; a diff's lexed side answers a
  later `Content` of the same bytes from the store (shared store);
  `Content`/`Blame` refuse bare CR; `Blame` line count = lines.
- Existing differ tests unchanged and green.

**Task 2: every server-side lex of shown content through it**
- `LexBlameLines` removed; web `handleBlame`, web `contentRows`, TUI
  `lexBlame`, `lexPreview`, `lexPickerDoc` ask the Highlighter (TokInline
  for now — behaviour unchanged, now cached and shared).
- TUI surfaces read the Service switch (drop their own `on` argument).
- Tests: a second blame / file-content request of the same content does
  not lex again (store hit); existing blame/preview/picker tests green.

### Phase 2 — wire (server only)

**Task 3: `defer_tok` / `tok=1` on `/api/file-content` and `/api/blame`**
- `defer_tok=1`: rows without runs when pending; body `tok_pending`, `sum`.
- `tok=1`: `{sum, tok: [[triples]|null]}` via TokOnly.
- Tests (`internal/web/linetok_test.go`): both endpoints: deferred answer
  has no row runs + pending + sum; `tok=1` runs equal the one-shot rows'
  runs; no-flag answer unchanged; a small file answers inline even deferred.

### Phase 3 — web painter + applier

**Task 4: one chunked painter for any row host** (`diffpaint.js`)
- Source gains `close` (default `</table>`); `chunkOpen` styles the first
  tag of `open`/`head`; `job.first` = the host's first chunk element
  (`host.firstElementChild` of the painted chunk, not `table.diff`).
- `diffTable` unchanged in output; tests: chunk ranges, the existing pins,
  a node test that a div source paints `n` rows across chunks.

**Task 5: one late-colour applier** (`difftok.js`)
- `patchRows(host, scroller, rows, redraw, live)` (on-screen first, idle
  slices, selection-end hold + retry); `patchCells` becomes the diff's
  `redraw` over it. `fetchToks` handles `tok` (line) besides `old`/`new`.
- `mergeLineToks(lines, t)` pure (guarded) + node test.
- Tests: existing difftok pins; `TestMergeLineToksJS`.

**Task 6: blame** (`filehist.js`): chunked paint, `blameEl()/blameBody()`
accessors (no `children[i]`), `defer_tok=1`, late colours via
`patchRows` + `paintBlameRow`, abort on every writer. Probe: text ≤ 0.3 s.

**Task 7: file viewer (file mode) + F preview** (`viewer.js`,
`wtfinder.js`): same; note boxes stay inside the chunk after their line.

**Task 8: viewer one-column diff**: diff-shaped source on the same painter
and applier (`left_no`/`right_no`).

**Task 9: stacked view slots + quietRefreshFile**: each slot body its own
painted host; `defer_tok=1`; late colours per slot; stack readers finish
the slot's paint.

### Phase 4 — TUI colours path

**Task 10: generic `tokCmd` for line views**: blame + F preview load with
TokDefer, show text, receive `linesTokMsg{tag, sum, tok}`; cancel on
close / re-point. Tests: deferred load has no runs + pending; msg applies
only on tag + sum; closing cancels.

**Task 11: picker + bookmark / shelf / shelf-note / review diffs** via
`runDiff` + `diffTokCmd` (picker: its two sides through `Content` with
TokDefer + the same msg path).

### Phase 5 — finish

**Task 12:** CHANGELOG, CLAUDE-details "Diff syntax runs", measurements
(acceptance table), race gate, final review subagent.
