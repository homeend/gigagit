# Progressive diffs — the text first, colours and the rest of the page after

Status: draft for review. Origin: the user's report (2026-10-25), "loading a
given file version from file history in the web UI takes quite a lot of
time … in the TUI it shows faster", then the ask: "render only the part that
should be viewed, and in the background load the rest of the data and render
the rest of the file".

## Why

Measured on `CHANGELOG.md` (841 KB, 13.6k lines, 13.5k diff rows), one
revision not yet cached:

| Step | TUI | Web |
|---|---|---|
| chroma lexing, both sides in parallel | 1.1 s | 1.1 s (same `Differ`) |
| alignment (`textdiff.Compare`, enhanced) | 6 ms | 6 ms |
| JSON encode + transfer | — | 27 ms + 100–140 ms (3.2 MB) |
| painting | ~50 visible rows, <1 ms | "changes only" on: ~100 ms; off: whole table, 0.9 s Firefox / 1.4 s Chromium |
| **total** | **~1.1 s** | **~1.3 s** ("changes only" on) to **2.3 s** (off, Firefox) |

Two costs, one shared and one web-only:

1. **Syntax colouring dominates and blocks everything.** `plainDiffer.Diff`
   lexes the whole of both sides before it returns, so nothing is shown until
   chroma is done. It is redone for every revision although revision N's new
   side is revision N+1's old side, and a j/k walk starts one 1.1 s lex per
   row passed with nothing cancelled (TUI: `context.Background()`; web: no
   abort).
2. **The web paints the whole table in one go.** `diffHTML` builds one string
   for every row and assigns it to `innerHTML`; the TUI draws only what is on
   screen. A cached revision still costs the web ~1 s in Firefox with
   "changes only" off.

## Goals

- The first screen of a diff shows in about 0.1 s whatever the file size:
  the text uncoloured if the colours are not ready, the rows below the screen
  painted after it.
- Colours arrive by themselves when lexing finishes and paint over the rows
  already shown, with no jump in scroll position, folds or selection.
- Stepping through file history lexes one new side per step, not two.
- Moving on cancels the colouring of what was left behind, so a j/k walk does
  not queue seconds of chroma ahead of the row the cursor stops on.
- The finished page is identical to today's: same rows, same colours, same
  DOM once painting completes.

## Non-goals

- Faster chroma, a different highlighter, or a lower `MaxSyntaxBytes`.
- Shrinking the wire format (sending only folded-in rows, compression).
- Windowed/virtual rendering in the web (only the rows near the screen ever
  exist). Chunked painting was chosen because the finished DOM is unchanged
  and the ~70 readers of `#diff-body` rows keep working.
- Blame, the F preview and the file viewer, which lex whole files through
  other endpoints. Same remedy could follow later; not here.
- The stacked view's per-section painting (sections are already lazy).

## Anti-goals (a failure even if it works)

- A small file flickering from plain to coloured. Below the inline threshold
  (below) a diff arrives coloured in its first response, exactly as today.
- Any feature that reads the painted rows (search, `.`/`,` change stepping,
  go to line, note landing, copy, link landing, hunk picks) acting on a
  half-painted table and missing rows.
- A colour response for a revision the user has left painting over the one
  now shown.
- Uncoloured diffs cached for good because a lex was cancelled.

## Design

### 1. Domain: alignment and colours become two steps

**`Request.DeferTokens bool`** (new, default false). False keeps today's
contract exactly — `Diff` returns with tokens — so callers that do not opt in
are untouched. True means: return as soon as the alignment is done; attach
tokens only if they are already cached or the side is small, else set
**`Diff.TokPending`**.

**Inline threshold:** a side of at most **64 KB** is lexed inline even when
deferred (chroma ≈ 1.3 µs/byte → ≤ ~85 ms), so ordinary files never show
plain first. *(My call; the builder may tune the number with a measurement.)*

**`Differ.Tokens(ctx, req) (old, new [][]syntax.Tok, err)`** (new) returns the
per-line runs for both sides of the same `Request`. It is what a frontend calls
after a `TokPending` diff.

**A token cache keyed by content, not by diff.** Key = language + SHA-256 of
the side's bytes (hashing 841 KB is about 1 ms). Revision N's new side and
N+1's old side are the same bytes, so a history walk hits it. Working-tree
sides key the same way, so an unchanged file re-diffed after an unrelated edit
also hits. The cached `Diff` records each side's content sum (`OldSum`,
`NewSum`) so `Tokens` after a `Diff` cache hit needs no second `git show`.
Tokens move out of the cached `Diff` value (they were counted in `Size()`)
into this cache, which gets its own byte budget from the cache factory.
*(Budget size: builder's call, from the existing 64 MiB diff budget's
precedent.)*

**Content sums travel with every diff.** `Diff` always carries `OldSum` /
`NewSum` (SHA-256 of each side's bytes), cached or not, and `Tokens` returns
the sums it lexed. A frontend applies colour runs only when their sums equal
the diff's on screen; otherwise it drops them (the file changed between the
two reads — the working tree's auto-refresh repaints it anyway). This is what
makes the split safe for live sides (`wt=unstaged|staged|head`, the TUI's
status diff, compares with a working-tree side), which are uncached and
re-read their content on the second request. *(My call; it was a hole in the
first draft.)*

**Cancellation.** `syntax.Lex` gains a context (or a `LexCtx` variant) that
stops between tokens when the context is done; a cancelled lex stores
nothing. `cache.Load` does **not** coalesce (concurrent misses both compute —
see `internal/cache/cache.go`), so the token cache gets its own **registry of
lexes in progress**, keyed like the cache, with a count of waiters: a second
caller for the same side joins the running lex instead of starting another,
and the lex is cancelled when its last waiter leaves. The shared `cache`
package stays as it is. The existing rule — never cache a syntax-on result
produced under a dead context — carries over to the token cache.

`Diff(DeferTokens: false)` is `Diff` + `Tokens` composed, so both paths share
the cache.

### 2. Web: `/api/diff` answers in two requests

- A client opts in with `defer_tok=1` on any `/api/diff` form. The JSON gains
  `"tok_pending": true` when colours are still to come.
- The colours are read from **the same URL plus `tok=1`**, which answers
  `{"old": [[[start,end,class],…] per line], "new": …}` by source line number
  (the `img=old|new` precedent in `writeDiffJSON`: one handler, one request
  builder, so the two halves can never describe different diffs).
- The client merges the runs into its rows (`left_tok` / `right_tok` by
  `left_no` / `right_no`) and **patches the painted cells in place** — no new
  paint — so the scroll position, folds, search hits, the marked range and a
  text selection the reader is dragging all stay. Rows not yet painted pick
  the runs up from the merged data when the chunk painter reaches them.
  *(My call: a repaint would wipe a drag-selection started in the first
  second.)*
- Each colour fetch carries an `AbortController`. Moving to another row,
  revision or file, or closing the overlay, aborts it, and the server
  handler's request context cancels the lex.
- The response is applied only if the diff it belongs to is still the one
  shown (the history overlay's `gen` + `sel` guard, `state.lastDiff` for the
  main pane).

Surfaces that opt in: the **file-history overlay** and the **main single-file
diff pane** (every form `fileDiffURL` builds, `/api/entry-diff` included —
it answers through the same `writeDiffJSON`). *(My call: the stacked view
and the other diff hosts keep today's single request; they can opt in
later.)*

### 3. Web: painting in chunks

`diffHTML` stays the one builder of row HTML, split so that it can produce a
range of its items. A painter for one host:

1. Synchronously paints the first chunk from the top: enough rows to fill
   the viewport plus one screen of margin. A landing deep in the file (a
   note, a `gg://` link, go to line) calls `finishPaint` and then scrolls —
   it costs the full paint, i.e. today's time, never more. *(My call: a
   paint-the-middle-first painter is a much bigger build for a rare path.)*
2. Appends the rest in chunks of a few hundred rows per `requestIdleCallback`
   (`setTimeout` fallback), each through `insertAdjacentHTML` on the table
   body.
3. Has a generation: a new paint of the host (another revision, a resize, a
   notes refresh, `f`) cancels the old one.
4. Exposes **`finishPaint(host)`**, which paints every remaining row
   synchronously. **Every reader of a host's rows calls it first** — search,
   change stepping, go to line, note/link landing, copy, hunk picks, the
   folding click handlers. One accessor, so the rule is checked in one place
   rather than at ~70 call sites. *(How to make the accessor unavoidable —
   e.g. an AST-free grep test like the `handover()` one — is the builder's
   call; there must be one.)*

Hosts: `#history-diff` and `#diff-body`. The pan bars (`mountPanBars`) and the
search count update when the paint finishes.

### 4. TUI: plain first, colours after

- The history view's right pane, enter's full-screen diff and the main diff
  views (`loadCommitDiffCmd` and siblings in `diff_view.go`) set
  `DeferTokens`.
- When the result is `TokPending`, the view shows the plain diff and the load
  returns a follow-up command that calls `Tokens` and yields a message
  carrying the runs for **that** `*diffView`; the update applies them only if
  that view is still the one shown (the existing tag / slot-stamp guards) and
  the content sums match.
- The follow-up command gets a context that is cancelled when the selection
  moves or the view closes, so a walk stops lexing what it passed.
- The TUI already draws only what is on screen; nothing changes there.
- Bookmark, shelf and review diffs keep the blocking path *(my call)*.

### Expected result (CHANGELOG.md, uncached revision)

| | Today | After |
|---|---|---|
| TUI: first screen | 1.1 s | ~10 ms plain; colours at ~1.1 s |
| Web: first screen | 1.3–2.3 s | ~0.1–0.2 s plain; colours at ~1.2 s |
| Web: cached revision, "changes only" off | ~1 s (Firefox) | first screen ~0.1 s |
| One history step back | 2 sides lexed | 1 side lexed (~0.5 s of chroma, parallel to showing the text) |

## Testing

- Domain: `DeferTokens` returns pending for a large side and inline for a
  small one; `Tokens` equals what the blocking path produced; the token cache
  hits for the shared side of two adjacent revisions; a cancelled lex stores
  nothing and a later call lexes again; two concurrent callers for one side
  run one lex, and it stops only when both leave; `DeferTokens: false` is
  unchanged (existing tests stay green).
- Web Go: `defer_tok=1` / `tok=1` on each `/api/diff` form, including a
  rename's old path and a pure add/delete; `tok=1` on a small file answers
  the same runs as the inline ones; a working-tree file edited between the
  two requests answers sums that differ from the diff's.
- Web JS (node guards, the `diffcollapsejs_test.go` pattern): the chunk
  splitter covers every item exactly once in order; `finishPaint` yields the
  same HTML as one-shot `diffHTML`; the reader-accessor rule's guard test.
- Browser probe (Playwright, Firefox + Chromium, CHANGELOG.md): first rows
  visible ≤ 0.2 s after the click; colours arrive; final `#history-diff` and
  `#diff-body` HTML equal today's; j/k ×8 then stop → the server ran at most
  the stopped row's lex to completion; search/`.`/go-to-line issued during
  painting find rows past the first chunk.
- TUI: a headless test that the plain diff lands first, the colour message
  applies to the same view, and a colour message for a view that was replaced
  is dropped.

## Left to the builder

The inline threshold value, the token-cache budget, chunk size, the waiter
counting for cancellation, the exact names, and how the accessor rule is
enforced — within the rules above.
