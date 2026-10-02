# Range links in gg web — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:executing-plans
> (inline, this session — no implementer subagents). Steps use `- [ ]`.

**Goal:** the browser marks a range when a range link opens, lets the reader
mark lines and copy a range link, and draws a bar beside a ranged note's lines.

**Architecture:** pure decisions (block fingerprint, link text, range step,
row set, bar set) are plain functions tested under node against Go; the DOM
side is one post-render paint pass (`paintRangeMarks`) over `tr[data-i]`, so
none of the three diff layouts' HTML strings change. No new endpoint.

**Tech stack:** Go tests (`internal/web`), vanilla JS in `internal/web/static`,
node for JS guards, playwright for the browser check.

**Spec:** `docs/superpowers/specs/2026-10-02-web-range-links-design.md`

## Global Constraints

- `go test ./internal/web` after EVERY `static/*.js` edit (source-pin tests).
- One fingerprint over the whole block; a range is never re-found.
- Shift+click on a line number is only the range gesture; staging ignores it.
- Web hides by ID, not a global `.hidden`.
- Browser checks assert VISIBILITY, against the rebuilt binary.
- Commit with `gg add <paths>` + `git commit -F`; never `git add -A`.

## Ruling made while planning

- The spec's `static/rangemark.js` is dropped: pure helpers live next to
  their callers (`links.js` guarded section, `files.js`) and are pulled into
  node by the existing `jsFunc` / guarded-section harnesses. A new module
  would need embed + import wiring for no gain. Cost if wrong: one file move.

## Review Focus

1. Unified layout (< 950px): a `same` row has no `l`/`r` class on its text
   cell — the band and bar must still show on the right side.
2. A re-render (notes refresh, fold toggle, resize) must keep the band.
3. Stack: a range in slot A must vanish when a row in slot B is marked.
4. A range whose block holds U+FFFD: link stays plain (single-line rule).
5. `diffPartial` (changes-only view): the end line is folded away on landing.

---

### Task 1: `blockFingerprint` + range in `linkFor` (pure)

**Files:** `internal/web/static/links.js`, `internal/web/linkfpjs_test.go`

**Produces:**
- `blockFingerprint(lines: string[]) → string` — each line `GO_TRIM`med,
  joined with `"\n"`, FNV-1a 32, 8 hex; `""` when every line trims empty.
- `linkFor(repo, worktree, ctx, side, no, text, end, block)` — `end > no`
  makes `:no-end`; the fp slot is `blockFingerprint(block)` for uncommitted
  states when `block.length === end-no+1` and no line holds U+FFFD, else
  none; `end <= no` or absent = today's link. The `cmpSides` recursion passes
  `end`/`block` through. A preview's old side still degrades to the file form.

- [ ] Test (extend `TestLineFingerprintJSMatchesGo` or add
  `TestBlockFingerprintJSMatchesGo`): blocks `["a","b"]`, `["  a\t","b\r"]`,
  `["a","","b"]`, `[""," \t"]` (→ ""), `["a ","﻿b"]` vs
  `model.BlockFingerprint`; link cases vs `model.Link{…End}.String()` /
  literal: unstaged new `:5-7~fp`, unstaged old `:old:5-7~fp`, staged
  `@staged:5-7~fp`, commit `@sha:5-7`, pair `@a..b:old:5-7`, preview new
  `@t...s:5-7`, preview old → file form, content hint `:5-7~fp?view=content`,
  block of wrong length → no fp, U+FFFD → no fp, `end == no` → single form.
- [ ] Run → FAIL (blockFingerprint undefined).
- [ ] Implement; run `go test ./internal/web` → PASS. Commit.

### Task 2: range helpers (pure) in `files.js`

**Files:** `internal/web/static/files.js`, new `internal/web/rangemarkjs_test.go`

**Produces:**
- `extendRange(mark, hit)` — `mark`: `{side,no}|null`, `hit`: `{side,no}` →
  `{side, first, last}`; no mark or other side → `{side:hit.side,
  first:hit.no, last:hit.no}`; order-free.
- `rangeRows(rows, side, first, last)` → `{idx: number[], block: string[]}`:
  indexes of the rows whose number on `side` is in `first..last`, plus every
  row BETWEEN the first and last such row (the band is contiguous over rows);
  `block` = the texts of that side in order. Caller refuses when
  `block.length !== last-first+1`.
- `noteBars(notes)` → `{old: Set<number>, new: Set<number>}` for notes with
  `range && range[1] > range[0] && !file_level`.

- [ ] Node test via `jsFunc`: extendRange (null mark, other side, backwards);
  rangeRows over rows with a del-only row inside a new-side range (idx
  includes it, block does not), a missing number (short block); noteBars
  (single-line note → none; `[3,5]` new → 3,4,5).
- [ ] Run → FAIL. Implement. Run → PASS. Commit.

### Task 3: diff — state, paint, gesture, menu

**Files:** `static/files.js`, `static/stackview.js`, `static/style.css`,
`static/core.js` (state field), `static/keys.js` (Esc), source-pin tests as
they fail.

**Consumes:** Task 1, Task 2. **Produces:**
- `state.diffRange` / slot `.range` = `{side, first, last}|null`;
  `setDiffRange(tr, range)` (records on the slot in a stack, clears the
  others), `clearDiffRange() → bool`, `activeRange(row)` (the range of the
  row's own file), `paintRangeMarks(root, rows, range, notes)`.
- `paintRangeMarks`: for each `tr[data-i]` under root, toggles `rng-l` /
  `rng-r` (row index in `rangeRows(...).idx`) and `nbar-l` / `nbar-r` (the
  row's number on that side in `noteBars(notes)`, only for notes whose box is
  rendered: same filter `noteRowsHTML` uses). Called at the end of
  `renderDiff`, of every slot paint in `stackview.js`, and after a notes
  refresh.
- CSS: `tr.rng-l > td.l, tr.rng-r > td.r, tr.same.rng-r > td.side:not(.l)`
  background `color-mix(in srgb, var(--accent) 14%, transparent)`;
  `tr.nbar-l > td.no.l, tr.nbar-r > td.no.r` `box-shadow: inset -3px 0 0
  var(--note)` (the note colour variable in use for `.notebox`).
- Gesture: in the diff-body click handler, `shift` + target inside `td.no`
  → side from the cell (`l`/`r`), number from `rows[data-i]`, gated on
  `diffLinkCtx(tr) || notesArmed(...)`; `extendRange(current mark of that
  file, hit)`; if `first === last` mark the row only. The staging handlers
  (`mousedown` preventDefault, `click` → `clickRow`, the capture-phase
  outside-click clear, the dblclick) return early when the target is in
  `td.no` and shift is held. A plain click (existing `markDiffRow` path)
  calls `clearDiffRange()`.
- Esc: `clearDiffRange()` first; if it cleared something, stop.
- Menu: in the `contextmenu` handler, when `activeRange(row)` covers the
  row's number on that side and `rangeRows` yields a full block →
  `linkFor(..., first, block[0], last, block)`, label
  `copy gg link to lines a-b`, Desc as today.
- `renderDiff` on a NEW diff (`d !== state.lastDiff`) sets
  `state.diffRange = null`.

- [ ] Source-pin/unit tests first where pure (label text, handler guards
  via existing `*js_test.go` pin style); watch FAIL.
- [ ] Implement; `go test ./internal/web` → PASS. Commit.

### Task 4: landing — diff, stack, viewer

**Files:** `static/live.js`, `static/stackview.js`, `static/viewer.js`

- `steerNavigateLand`: after `revealDiffRow(side, line)`, when
  `s.end_line > s.line`: `revealDiffRow(side, s.end_line)` (unfolds; null is
  tolerated → clamp to the last present number ≤ end and `opLine` "lines
  a-b: only a-x are in this diff"), re-find the first row (a re-render
  replaced it), `markDiffRow`, `setDiffRange`.
- `landStackLine(path, side, line, end)`: same inside the section.
- `steerNavigateContent`: after `openViewer`, if `end_line > line` and
  `line <= view.lines.length`: `view.range = {start: line, end: min(end,
  len)}`, `rerenderKeepingScroll()`.
- Viewer: shift+click on `.vno` → `view.range` from `view.cur` to the
  clicked line (cursor stays); plain click clears `view.range` unless the
  viewer was opened from an overview anchor (`view.from` — keep that band's
  current behaviour); Esc clears the range before closing; `openViewerMenu`
  → `copy file link (lines a-b)` with `linkFor(..., start, text, end,
  block)`, block only when `view.src === "worktree"`.
- The page's link prompt (`gotoLink`): confirm a stale range shows the
  resolver's text; add a Go test on the handler that the error body carries
  "no longer valid".

- [ ] Tests first (Go handler test for the stale message; node test for the
  viewer clamp as a pure `viewerRange(line, end, len)`); FAIL → implement →
  PASS. Commit.

### Task 5: browser check + docs

- [ ] Build `bin/gg`, start `gg web` on a fixture repo (a commit diff, an
  uncommitted change, a ranged note via `gg note add <range link>`).
  Playwright script in the scratchpad asserting, with visibility checks:
  band after shift+click; staging selection count unchanged by a shift+click
  on a number of a changed working-tree row; menu label + copied text
  `:a-b`; `gg session navigate <range link>` bands the rows in single diff,
  stack and viewer; the note bar on the ranged note's lines and none on a
  one-line note; Esc clears the band first. Run once against `main`'s binary
  for the landing assertion to see it fail.
- [ ] `CHANGELOG.md`, `README.md` (web section), `docs/CLAUDE-details.md`,
  the web `?` help overlay row; memory.
- [ ] `./test.sh race` → "all green"; read-only review subagent; fix pass;
  ask before merging; after merge `./build.sh install`, `./build.sh web`.
