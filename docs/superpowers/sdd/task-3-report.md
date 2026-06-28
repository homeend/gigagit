# Task 3 Report — Wire commitDecoGroup into row builder + display pipeline

Commit: `83dfdaf`  
Branch: `feat/commit-ref-decorations`

## Deliverables completed

| Deliverable | Status |
|---|---|
| `commitIdentRowAt(i, w, full, budget)` — new param, group before subject | Done |
| `commitBody(boxW, boxH)` — threads width, computes budget once per call | Done |
| `commitGroupBudget(boxW, identW)` — new helper | Done |
| `commitIdentRows` — passes `budget=-1` | Done |
| `commitHaystackAt` — appends `id.tags` to haystack | Done |
| `commitTextRevealAt` — uses `commitDecoGroup(id, -1)` instead of `pills()` | Done |
| `pills()` removed from `commit_ident.go` | Done |
| Tests in `commit_deco_row_test.go` | 3 tests: pass |
| Existing test files updated | `commit_render_test.go`, `commits_window_render_test.go`, `commits_render_bench_test.go` |

## Key design decisions

### `commitIdentRowAt` row construction

Old: `tok + " " + id.pills() + c.Subject`  
New: `tok + group + " " + c.Subject`

The `commitDecoGroup` result carries its own leading space (`" (…)"`), so for an empty group (`""`) the row is `tok + "" + " " + c.Subject` = `tok + " " + c.Subject` — identical to the pre-Task-3 lineage row. No double-space issue.

### `commitTextRevealAt` spacing

`strings.TrimSpace(id.label() + group)` cleanly handles all cases:
- `"main" + " (branch1)"` → `"main (branch1) Hello"`
- `"main" + ""` → `"main Hello"`
- `"" + " (⊙v1)"` → `"(⊙v1) Hello"`
- `"" + ""` → `"Hello"`

### `commitGroupBudget` formula

```
content = boxW - 2      // renderPanel borders
prefix  = 2             // selection prefix renderPanel prepends
       + 2 if listMode  // "● "
       + graphCols()*2+1 if graphMode
budget  = content - prefix - commitMarkerW - identW - 1 - 12
```

The `prefix` expression is identical to `commitDecoratorsRange`'s `identStart` computation (view.go:1214–1220). This is the load-bearing cross-check: both functions agree on the column at which the identity name ends, so the group string produced by `commitDecoGroup(id, budget)` is always the same string the decorator will later walk over.

### `boxW - 2` vs `boxW - 4` (innerW)

`renderPanel` uses `innerW = boxW - 4` (borders + padding). The budget formula uses `boxW - 2` (borders only), per the plan. The 2-cell difference makes the budget slightly generous — a group that fits the budget might slightly overflow `innerW` but gets tail-truncated by `renderWindow`. This is acceptable: budget gates collapse decisions only, and consistency with the decorator's column math matters more than pixel-perfect width.

### `budget=-1` in all non-display paths

`commitIdentRows`, `commitList.Row()`, `commitList.Full()`, `commitTextRevealAt` all pass `budget=-1` (never collapse). These are reveal/filter/measurement paths that need the full group text.

### `benchModel` and `TestCommitBodyWindowedMatchesFull`

`benchModel` builds commits with no `Refs` — `commitDecoGroup` always returns `""` regardless of budget. So the windowed-vs-full test is insensitive to the budget divergence between the two render paths. This is an acceptable latent gap: once bench commits carry refs, the test will start exercising collapse parity. Noted for whoever adds refs to `benchModel`.

### `commit_scope_test.go:336` (`‹` glyph check)

That test asserts `!strings.Contains(rows[1], "‹")` for a lineage row (single identity, no extras). After Task 3 the row has no `‹` pills and no `(` group, so the assertion still passes. No update required.

### `diff_render_test.go` (`‹`/`›` glyphs)

These are diff-view left/right-scroll bracket glyphs, unrelated to commit pills. Not affected.

## Test results

```
go test ./internal/tui/ → 1322 passed
go build ./cmd/gg        → OK
```

## Task-4 handoff

Task 4 extends `commitLineDecorator` / `commitDecoratorsRange` to color tag spans yellow. It needs the same `group` string to know which rune ranges to color. The key invariant:

> `commitGroupBudget` produces the budget → `commitDecoGroup(id, budget)` produces the group string → Task 4's decorator receives the same budget and calls `commitDecoGroup(id, budget)` again to recover the `tagSpans` — same function, same inputs, same output.

Task 4 must NOT recompute `commitGroupBudget` independently; it should receive `budget` threaded from `commitDecoratorsRange` (which already receives `rows` built with a specific budget). The plan calls for `commitDecoratorsRange(rows, idx, start, end, budget)` in Task 4 — this is the correct interface.
