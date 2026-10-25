# Progressive Diffs Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans (this repo forbids implementer subagents — CLAUDE.md). Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A diff's text shows at once; syntax colours and (web) the rows below the screen arrive after it, and colouring is cached per file version and cancelled when the reader moves on.

**Architecture:** The domain `Differ` splits alignment from lexing: a request in `TokDefer` mode returns the aligned rows plus `TokPending`, a `TokOnly` request returns just the runs. Runs live in a content-keyed token store (SHA-256 of a side's bytes) with an in-flight registry that shares and cancels lexes. The web answers the two halves on the same `/api/diff` URL (`defer_tok=1`, then `tok=1`), paints tables in chunks behind a `finishPaint` accessor, and patches colours into painted cells; the TUI shows the plain diff and applies the runs from a follow-up message.

**Tech Stack:** Go 1.26, chroma (via `internal/syntax`), Bubble Tea, vanilla ES modules in `internal/web/static`, node guards for pure JS, Playwright probe.

**Spec:** `docs/superpowers/specs/2026-10-25-progressive-diff-design.md`

## Global Constraints

- `Request.Tok` zero value = today's behaviour (tokens attached before `Diff` returns). Callers that do not opt in see no change.
- Inline threshold: a side of at most **64 KB** (`InlineSyntaxBytes = 64 << 10`) is lexed inline even in `TokDefer` mode.
- `MaxSyntaxBytes` (1 MB) and `MaxDiffBytes` stay; a side over `MaxSyntaxBytes` is never lexed and never pending.
- Content sum = lowercase hex SHA-256 of the side's bytes; `""` for an absent side. A frontend applies runs only when both sums equal the shown diff's.
- A lex cancelled by its context stores nothing.
- The finished web DOM equals today's one-shot `diffHTML` output.
- Opted in: web `#history-diff` and the main single-file pane's opens (`files.js` getDiff callers at the entry-diff, solo diff, `fileDiffURL` file and status opens); TUI `loadHistoryDiffCmd`, `loadHistoryDiffFullCmd`, `loadCommitDiffCmd`, `loadStatusDiffCmd`, `loadCompareDiffCmd`. Not opted in: stacked view, `quietRefreshFile`, bookmarks, shelves, reviews, overviews.
- No new dependencies. Go source LF, gofmt. New Go tests `t.Parallel()`. After any `static/*.js` edit run `go test ./internal/web` (source-pin tests). Engine/web prose English; no new TUI strings.
- Work only in `/work/gigagit/.claude/worktrees/progressive-diff`; `gg add` + `git -C <abs> commit -F <msgfile>` with the session trailers.

## Review Focus

1. **Rename** (old path ≠ path, e.g. `.go` → `.py`): the token request must lex each side with its own language, as today — test in Task 3 (`TestDifferTokOnlyRenameUsesOldLang`) and Task 4 (`TestDiffTokOnlyRenameForm`).
2. **Pure add / pure delete** (one side absent): its sum is `""`, its runs nil, never pending on the missing side — Task 3 `TestDifferDeferAbsentSide`.
3. **Highlighting turned off mid-session** (`[ui] diff_syntax = "off"`): no side is pending, `TokOnly` returns nil runs — Task 3 `TestDifferDeferSyntaxOff`.
4. **Binary, too-large, and > `MaxSyntaxBytes` sides**: never pending (the client would wait for colours that never come) — Task 3 `TestDifferDeferNeverPendingWithoutLexer`.
5. **A reader acting while the table is still painting** (in-view search, `.`/`,`, go to line): finds rows past the first chunk — Task 6 guard test + Task 8 probe step.

---

### Task 1: Cancellable lexing

**Files:**
- Modify: `internal/syntax/syntax.go` (`Lex`, ~line 95)
- Test: `internal/syntax/syntax_test.go`

**Interfaces:**
- Produces: `func LexCtx(ctx context.Context, lang string, src []byte) ([][]Tok, error)`; `Lex(lang, src)` becomes `LexCtx(context.Background(), …)` with the error dropped.

- [ ] **Step 1: Write the failing tests**

```go
func TestLexCtxMatchesLex(t *testing.T) {
	t.Parallel()
	src := []byte("package p\n\nfunc f() int { return 1 } // c\n")
	got, err := LexCtx(context.Background(), "go", src)
	if err != nil || !reflect.DeepEqual(got, Lex("go", src)) {
		t.Fatalf("LexCtx = %v, %v; want Lex's runs", got, err)
	}
}

func TestLexCtxCancelled(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got, err := LexCtx(ctx, "go", bytes.Repeat([]byte("x := 1\n"), 50000))
	if !errors.Is(err, context.Canceled) || got != nil {
		t.Fatalf("LexCtx(cancelled) = %v, %v; want nil, context.Canceled", got, err)
	}
}
```

- [ ] **Step 2: Run** `go test ./internal/syntax -run LexCtx` — Expected: FAIL (undefined `LexCtx`).
- [ ] **Step 3: Implement `LexCtx`** — drain the chroma iterator token by token instead of `it.Tokens()`, checking `ctx.Err()` before the first token and every 1024 tokens; on cancel return `nil, ctx.Err()`. `Lex` calls it.
- [ ] **Step 4: Run** `go test ./internal/syntax` — Expected: PASS.
- [ ] **Step 5: Commit** `feat(syntax): LexCtx — a lex that stops when its context ends`

---

### Task 2: Token store — content-keyed cache + shared, cancellable lexes

**Files:**
- Create: `internal/domain/difftok.go`
- Test: `internal/domain/difftok_test.go`

**Interfaces:**
- Consumes: `syntax.LexCtx` (Task 1), `cache.Cache` / `cache.Load`.
- Produces:
  - `func contentSum(b []byte) string` — hex SHA-256; `""` when `b == nil`.
  - `type tokStore struct` with `newTokStore(c cache.Cache) *tokStore` (`c` may be nil: no caching, sharing still works) and a test seam field `lex func(ctx context.Context, lang string, src []byte) ([][]syntax.Tok, error)` (defaults to `syntax.LexCtx`).
  - `func (s *tokStore) Get(lang, sum string) ([][]syntax.Tok, bool)`
  - `func (s *tokStore) Lex(ctx context.Context, lang, sum string, src []byte) ([][]syntax.Tok, error)` — cache hit, else join a running lex for `lang+":"+sum`, else start one.
  - Cached value type `tokEntry{toks [][]syntax.Tok}` implementing `cache.Sized` with the per-line formula now in `Diff.Size` (24 + 24×runs per line).

- [ ] **Step 1: Write the failing tests** (`lex` seam counts calls and can block on a channel)
  - `TestTokStoreCachesBySum`: two `Lex` calls with equal bytes → seam called once; `Get` hits afterwards.
  - `TestTokStoreSharesRunningLex`: two goroutines `Lex` the same key while the seam blocks → seam called once, both get the same runs.
  - `TestTokStoreLastWaiterCancels`: two waiters; cancel the first → seam's ctx still live; cancel the second → seam's ctx done; `Get` misses; a later `Lex` runs the seam again.
  - `TestTokStoreCancelledStoresNothing`: one waiter cancels mid-lex → `Lex` returns `context.Canceled`, `Get` misses.
  - `TestContentSum`: `contentSum(nil) == ""`, `contentSum([]byte{})` is the SHA-256 of empty (not `""`), equal bytes → equal sums.
- [ ] **Step 2: Run** `go test ./internal/domain -run 'TokStore|ContentSum'` — Expected: FAIL (undefined).
- [ ] **Step 3: Implement** — a `map[string]*lexJob` under a mutex; `lexJob{waiters int; cancel context.CancelFunc; done chan struct{}; toks [][]syntax.Tok; err error}`. The job runs on its own `context.WithCancel(context.Background())`; each waiter selects on `job.done` vs its own `ctx.Done()`, and a leaving waiter decrements `waiters` and cancels the job at zero. The job stores into the cache only when `err == nil`.
- [ ] **Step 4: Run** `go test -race ./internal/domain -run 'TokStore|ContentSum'` — Expected: PASS.
- [ ] **Step 5: Commit** `feat(domain): token store — colours cached per file version, lexes shared and cancelled`

---

### Task 3: Differ — alignment first, colours on request

**Files:**
- Modify: `internal/domain/differ.go`, `internal/domain/service.go:419-426`
- Test: `internal/domain/differ_test.go`

**Interfaces:**
- Consumes: `tokStore`, `contentSum` (Task 2).
- Produces:
  - `type TokMode uint8` with `TokInline` (zero; today), `TokDefer`, `TokOnly`; `Request.Tok TokMode`.
  - `const InlineSyntaxBytes = 64 << 10`.
  - `Diff` gains `TokPending bool` and `OldSum, NewSum string` (always set for a text or binary diff that read its sides; `""` for an absent side).
  - `DifferOptions.Tokens cache.Cache` (the token store's cache; nil = uncached store).
  - `Service.Differ()` passes `Tokens: s.factory.Cache("diff-tok")`.

Per-side rule (both modes but `TokOnly`): no lexer (syntax off, unknown language, absent side, binary, too large, > `MaxSyntaxBytes`) → nil, not pending. Store hit → attach. `TokInline` or side ≤ `InlineSyntaxBytes` → lex now via the store (both sides concurrently, as today). Otherwise → nil and `TokPending = true`. `TokOnly` → no alignment (`Result` zero), sums set, both lexable sides lexed via the store, error = the context's if it died. The alignment cache key drops the `"s:"` segment (tokens are no longer in the cached value); the cached `Diff` keeps sums but never tokens. On an alignment cache hit that still needs a lex, read the sides again through the `ByteSource`s.

- [ ] **Step 1: Write the failing tests** (a ~200 KB Go source built with `strings.Repeat` for "large", a 1 KB one for "small"; `on := func() bool { return true }`)
  - `TestDifferDeferLargeIsPending`: `Tok: TokDefer`, large Go file → rows present, `TokPending`, `OldTok == nil && NewTok == nil`, sums non-empty.
  - `TestDifferDeferSmallIsInline`: small file → `!TokPending`, runs present.
  - `TestDifferTokOnlyEqualsInline`: `TokOnly` on the large pair → `OldTok/NewTok` deep-equal the `TokInline` result's; `len(Result.Rows) == 0`; sums equal.
  - `TestDifferTokOnlyRenameUsesOldLang`: `Path: "a.py", OldPath: "a.go"` → `OldTok` equals `syntax.Lex("go", old)`.
  - `TestDifferDeferAbsentSide`: `Old: nil`, large new → `OldSum == ""`, `OldTok == nil`, pending only for the new side (`TokOnly` then gives nil old runs).
  - `TestDifferDeferSyntaxOff`: `Syntax: func() bool { return false }` → `!TokPending`; `TokOnly` → nil runs.
  - `TestDifferDeferNeverPendingWithoutLexer`: binary side, `.unknownext` path, side > `MaxSyntaxBytes` → `!TokPending` each.
  - `TestDifferAdjacentRevisionsLexSharedSideOnce`: cached differ with a counting store seam; diff (A→B) then (B→C) inline → B lexed once.
  - `TestDifferRevisitAfterTokOnlyIsColoured`: cached differ, large pair: `TokDefer` (pending) → `TokOnly` → `TokDefer` again → the third result has runs attached and `!TokPending`, and its call read no `ByteSource` (counting sources).
  - Existing `differ_test.go` tests stay green (adjust only the key-prefix test if it asserts `"s:"`).
- [ ] **Step 2: Run** `go test ./internal/domain -run Differ` — Expected: new tests FAIL.
- [ ] **Step 3: Implement** — split `plainDiffer.Diff` into alignment (returns the `Diff` with sums plus the bytes it read) and an attach step applying the per-side rule above; `cachedDiffer` caches the alignment only and runs the attach step on every call. Remove tokens from `Diff.Size()` (they live in the store).
- [ ] **Step 4: Run** `go test -race ./internal/domain` — Expected: PASS.
- [ ] **Step 5: Commit** `feat(domain): Differ — TokDefer returns the rows first, TokOnly the colours`

---

### Task 4: `/api/diff` and `/api/entry-diff` answer in two halves

**Files:**
- Modify: `internal/web/diff.go` (`handleDiff` ~186-222, `handleRevDiff` ~263-299, the worktree diff builder ~412, `writeDiffJSON` ~488, `diffPayload` ~548), `internal/web/compare.go:538`
- Test: `internal/web/difftok_test.go` (new)

**Interfaces:**
- Consumes: `domain.TokMode`, `Diff.TokPending/OldSum/NewSum` (Task 3).
- Produces (wire):
  - `func tokModeOf(r *http.Request) domain.TokMode` — `tok=1` → `TokOnly`, `defer_tok=1` → `TokDefer`, else `TokInline`. Set as `Tok:` on each of the four `domain.Request` literals.
  - `diffPayload` adds `"tok_pending": d.TokPending`, `"old_sum"`, `"new_sum"`.
  - `writeDiffJSON` with `tok=1` writes `{"old_sum": s, "new_sum": s, "old": L, "new": L}` where `L[i]` = `tokTriples(side, i+1)` (`null` for a line without runs, `null` for the whole side when it has no runs). `img=` keeps precedence.
  - The worktree builder skips hunk tagging when the mode is `TokOnly`.

- [ ] **Step 1: Write the failing tests** (repo fixture helpers as in `diff_test.go`; a ~200 KB `big.go` committed then edited)
  - `TestDiffDeferTokCommitForm`: `/api/diff?sha=…&path=big.go&status=M&defer_tok=1` → `tok_pending: true`, rows present, no row has `left_tok`/`right_tok`, sums non-empty.
  - `TestDiffTokOnlyCommitForm`: same URL with `tok=1` → sums equal the defer response's, `len(new) == line count`, and the runs equal the inline (no-flag) response's `right_tok` for every row.
  - `TestDiffTokOnlyRenameForm`: renamed `.go` → `.py` with `old=` → `old` runs non-null.
  - `TestDiffDeferTokRevAndEntryForms`: `left/right` form and `/api/entry-diff` both honour `defer_tok=1` and `tok=1`.
  - `TestDiffTokOnlyWorktreeSumsMove`: `wt=unstaged` defer, rewrite the file, `tok=1` → `new_sum` differs from the defer response's.
  - `TestDiffNoFlagUnchanged`: no flag → `tok_pending` false and runs inline (today's contract).
- [ ] **Step 2: Run** `go test ./internal/web -run 'DiffDeferTok|DiffTokOnly|DiffNoFlag'` — Expected: FAIL.
- [ ] **Step 3: Implement** per Interfaces.
- [ ] **Step 4: Run** `go test ./internal/web` — Expected: PASS.
- [ ] **Step 5: Commit** `feat(web): /api/diff defer_tok + tok — the rows first, the colours by the same URL`

---

### Task 5: TUI — plain first, colours by a follow-up message

**Files:**
- Modify: `internal/tui/diff_view.go` (diffView fields ~50, `applyDiff` ~858, loaders 776/884/958), `internal/tui/history_view.go` (388, 408, `selectCmd` 428), `internal/tui/model.go` (`diffMsg` ~800, `historyDiffMsg` ~1463), `internal/tui/layer_stack.go` (`popLayer`, `clearLayers`)
- Test: `internal/tui/difftok_test.go` (new)

**Interfaces:**
- Consumes: `domain.TokDefer/TokOnly`, `Diff.TokPending/OldSum/NewSum`.
- Produces:
  - `diffView` fields `oldSum, newSum string`, `tokReq *domain.Request` (set when the load came back pending), `tokCancel context.CancelFunc`.
  - `applyDiff` copies `OldSum/NewSum`; each opted-in loader sets `Tok: domain.TokDefer` and, when `out.TokPending`, `v.tokReq = &req`.
  - `type diffTokMsg struct { tag string; hist *historyView; out domain.Diff; err error }`
  - `func (m Model) diffTokCmd(v *diffView, tag string, hist *historyView) tea.Cmd` — nil when `v.tokReq == nil`; else makes a cancellable context, stores its cancel in `v.tokCancel`, and runs `differ.Diff(ctx, req with Tok: TokOnly)`.
  - `diffMsg` handler: cancel the replaced view's `tokCancel` before `*dv = *msg.view`, then return `m.diffTokCmd(dv, msg.tag, nil)` alongside its existing commands. `historyDiffMsg`: same with `msg.view` and `msg.hist`. `historyView.selectCmd` cancels the outgoing `h.diff.tokCancel`. `popLayer`/`clearLayers` cancel a popped `*diffView`'s `tokCancel`.
  - `diffTokMsg` handler: error → ignore. `hist != nil` → apply to `hist.diff` when `hist.diffTag == tag` and both sums match. Else apply to `m.diffLayer()` when `m.diffTag == tag` and sums match. Applying = set `oldTok/newTok`, clear `tokReq`, and invalidate any per-view render cache that holds styled cells (check `diff_view.go` for one during the step).

- [ ] **Step 1: Write the failing tests**
  - `TestDiffTokMsgAppliesToLiveView`: a Model with a diff layer whose tag/sums match → after `Update(diffTokMsg{…})` the layer's `oldTok/newTok` are the message's.
  - `TestDiffTokMsgDroppedForStaleTag` and `…ForOtherSums`: tag or sum differs → runs unchanged (nil).
  - `TestHistoryTokMsgAppliesToHistoryPane`: `hist.diff` gets the runs when `hist.diffTag` matches.
  - `TestHistorySelectCancelsPendingTokens`: `selectCmd` after a pending load calls the old `tokCancel` (seam: a cancel func that flips a bool).
  - `TestLoadCommitDiffDefersLargeFile`: with a real repo fixture and a ~200 KB Go file, the loader's `diffMsg` view has rows, nil runs and a non-nil `tokReq`; running `diffTokCmd` yields runs equal to an inline load's.
- [ ] **Step 2: Run** `go test ./internal/tui -run 'DiffTok|HistoryTok|HistorySelectCancels|LoadCommitDiffDefers'` — Expected: FAIL.
- [ ] **Step 3: Implement** per Interfaces.
- [ ] **Step 4: Run** `go test ./internal/tui` — Expected: PASS.
- [ ] **Step 5: Commit** `feat(tui): diffs show plain first; colours land by a follow-up and are cancelled on move`

---

### Task 6: Web — chunked painting behind `finishPaint`

**Files:**
- Create: `internal/web/static/diffpaint.js`
- Modify: `internal/web/static/files.js` (`diffHTML` 1931-2197, `renderDiff` 2198-2217, every `#diff-body` reader), `internal/web/static/filehist.js` (`renderHistoryDiff` ~336, its `#history-diff` readers), `internal/web/static/index.html` only if module load order needs it
- Test: `internal/web/diffpaintjs_test.go` (new; node guard pattern of `diffcollapsejs_test.go`), `internal/web/diffpaint_static_test.go` (new; source guard)

**Interfaces:**
- Produces:
  - `files.js`: `function diffTable(d, paneWidth, notesOn, open, nctx, hctx, kctx)` → `{ html }` for every non-table outcome (image, binary, too large, no changed lines) or `{ head, n, row(k), cell(i, side), tail }` where `row(k)` is the HTML of the k-th painted item (incl. its note rows), `cell(i, side)` the `<span class="pan">` inner HTML for `d.rows[i]` on `"l"`/`"r"` with the current hits/spans/runs. `diffHTML(...)` keeps its signature and returns `head + row(0..n-1) + tail` (byte-identical to today).
  - `diffpaint.js`: `paintTable(host, t, { first, chunk = 400 })` paints `head + row(0..first-1) + "</table>" + tail` synchronously, then appends the rest with `insertAdjacentHTML("beforeend", …)` on the table's last `tBody`, one chunk per `requestIdleCallback` (fallback `setTimeout(…, 0)`); a later `paintTable`/`finishPaint` on the host supersedes it (generation). `finishPaint(host)` paints all remaining rows now; no-op when none. `onPainted(host, fn)` runs `fn` when the host's paint completes (immediately if it has). `first` = rows filling `host.clientHeight × 2` at the measured first-row height, at least 50.
  - Pure, node-guarded section `// --- chunk ranges (pure; guarded against Go) ---` … `// --- end chunk ranges ---`: `chunkRanges(n, first, size) → [[start, end), …]`.
  - Accessors in `files.js`: `diffBodyEl()` = the raw element (scroll, pan bars, painting), `diffBody()` = `finishPaint` then the element (any reader of rows: search, change steps, go to line, note/link landing, copy, hunk picks, fold clicks, `markDiffRow`). Same pair in `filehist.js`: `histDiffEl()` / `histDiff()`.
  - `renderDiff` and `renderHistoryDiff` paint through `paintTable`; `mountPanBars` and `diffSearchBar.paint()` run on the first chunk and again in `onPainted`.

- [ ] **Step 1: Write the failing tests**
  - `TestChunkRangesJS` (node): `chunkRanges(0,50,400)` → `[]`; `(30,50,400)` → `[[0,30]]`; `(1000,50,400)` → `[[0,50],[50,450],[450,850],[850,1000]]` — every index once, in order.
  - `TestDiffBodyReadsGoThroughAccessor` (source guard): in `static/*.js`, the strings `$("diff-body")`, `getElementById("diff-body")`, `"#diff-body` and `$("history-diff")` occur only inside `diffBodyEl`/`histDiffEl` definitions and `diffpaint.js`; fails listing file:line otherwise.
- [ ] **Step 2: Run** `go test ./internal/web -run 'ChunkRangesJS|DiffBodyReadsGoThroughAccessor'` — Expected: FAIL.
- [ ] **Step 3: Implement `diffTable` + `diffHTML` on top of it** — move the per-item loop bodies into `row(k)` closures over the same locals; no output change.
- [ ] **Step 4: Implement `diffpaint.js` and switch both hosts to it.**
- [ ] **Step 5: Classify every `#diff-body` / `#history-diff` reference** as `…El()` (no rows read) or the finishing accessor (rows read). Stack code (`stackview.js`) uses `diffBodyEl()` for the element and `diffBody()` where it reads rows; `finishPaint` is a no-op there.
- [ ] **Step 6: Run** `go test ./internal/web` — Expected: PASS (incl. existing source-pin tests).
- [ ] **Step 7: Commit** `feat(web): diffs paint the first screen at once and the rest in chunks`

---

### Task 7: Web — colours by a second request, patched in place

**Files:**
- Create: `internal/web/static/difftok.js`
- Modify: `internal/web/static/files.js` (`getDiff` 1262, the five opted-in callers at ~853/~898/~1460/~1717 and the solo/entry opens, `renderDiff`), `internal/web/static/filehist.js` (`openHistoryDiff` ~306, `closeHistory`)
- Test: `internal/web/difftokjs_test.go` (new, node guard), Go source-pin additions in the same file

**Interfaces:**
- Consumes: wire from Task 4; `diffTable(...).cell`, `diffBodyEl`, `histDiffEl`, `paintTable`/`onPainted` from Task 6.
- Produces:
  - `getDiff(url, { deferTok = false } = {})` — fetches `url + "&defer_tok=1"` when asked; `d.url` stays `url`.
  - Pure guarded section in `difftok.js`: `mergeToks(rows, t)` → array of row indexes whose `left_tok`/`right_tok` changed, reading `t.old[r.left_no - 1]` / `t.new[r.right_no - 1]` (missing → unchanged).
  - `fetchToks(d, signal)` → `GET d.url + "&tok=1"`; returns `null` when `t.old_sum !== d.old_sum || t.new_sum !== d.new_sum`, else `mergeToks(d.rows, t)`.
  - `patchCells(host, table, idx)` — re-renders the `td.side` spans of painted rows in `idx` (`tr[data-i]`; side from the td's `l`/`r` class, a class-less unified `same` cell = `"r"`) via `table.cell(i, side)`, through the chunk scheduler, viewport rows first. While a non-collapsed selection intersects `host`, it waits for the `selectionchange` that ends it. Rows painted later read the merged rows anyway.
  - `renderDiff`: on a new `d`, abort `state.tokAbort`; if `d.tok_pending`, start `fetchToks` with a fresh `AbortController` in `state.tokAbort` and patch when `state.lastDiff === d && !state.stack`. A re-render of the same `d` after the merge simply paints the coloured rows.
  - `openHistoryDiff`: same with `hist.tokAbort`, guarded by `hist.gen`, `hist.sel === i` and `hist.diff === d`; `closeHistory` and the next `openHistoryDiff` abort it.

- [ ] **Step 1: Write the failing tests**
  - `TestMergeToksJS` (node): rows `[{left_no:1,right_no:1},{left_no:0,right_no:2},{left_no:2,right_no:0}]` with `t = {old:[[[0,3,"kw"]], null], new:[null, [[1,2,"str"]]]}` → returns `[0, 1]`; row 0 `left_tok` set, `right_tok` unchanged; row 2 unchanged.
  - `TestGetDiffDeferPinned` (source pin): the five opted-in callers pass `{ deferTok: true }`; `stackview.js` and `quietRefreshFile` do not.
  - `TestHistoryAbortsStaleTok` (source pin): `closeHistory` and `openHistoryDiff` both call `hist.tokAbort` abort.
- [ ] **Step 2: Run** `go test ./internal/web -run 'MergeToksJS|GetDiffDeferPinned|HistoryAbortsStaleTok'` — Expected: FAIL.
- [ ] **Step 3: Implement** per Interfaces.
- [ ] **Step 4: Run** `go test ./internal/web` — Expected: PASS.
- [ ] **Step 5: Commit** `feat(web): diff colours arrive by a second request and paint into the shown rows`

---

### Task 8: Probe, docs, gate

**Files:**
- Create: `/tmp/claude-1000/-work-gigagit/<session>/scratchpad/progressive-probe.mjs` (not in the repo)
- Modify: `CHANGELOG.md`, `docs/CLAUDE-details.md` (the Differ / syntax entries: modes, store, sums), `CLAUDE.md` `syntax` row (`LexCtx`) only if the one-liner otherwise misleads

- [ ] **Step 1: Build** `cd <worktree> && go build -o bin/gg ./cmd/gg`; start `bin/gg web --addr 127.0.0.1:87xx` on `/work/gigagit` (run_in_background).
- [ ] **Step 2: Probe in Firefox and Chromium** (Playwright from `/tmp/claude-1000/-work-gigagit/blame-probe/node_modules`), CHANGELOG.md history, a revision not yet loaded, "changes only" on and off. Pass when: first `#history-diff tr` visible ≤ 200 ms after `openHistoryDiff`; a `.tk-` span appears later without a scroll jump; after `onPainted` the table HTML equals a one-shot `diffHTML` of the same merged rows; j ×8 then stop → the server log / request list shows earlier `tok=1` requests aborted; starting `/` search and `.` while painting finds a row past the first chunk; same checks on the main pane via a commit's CHANGELOG.md. Record the timings.
- [ ] **Step 3: TUI check** — `./tui-capture.sh` on CHANGELOG.md history: the right pane shows rows on the first capture after selection (no `loading`), coloured on a later capture.
- [ ] **Step 4: Docs** — CHANGELOG entry (user-visible: large diffs show at once, colours follow; history steps re-colour one side); CLAUDE-details Differ section.
- [ ] **Step 5: Gate** — `./test.sh race > <scratchpad>/race.log 2>&1` (background), then read the verdict line; green only on "all green".
- [ ] **Step 6: Commit** `docs: progressive diffs — CHANGELOG + Differ details`; stop the probe server by pid; report `<worktree>/bin/gg` absolute path and the measured timings; ask before merging.
