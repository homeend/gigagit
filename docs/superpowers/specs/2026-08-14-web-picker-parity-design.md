# Web conflict-picker parity — design

**Date:** 2026-08-14
**Status:** approved (user confirmed scope = conflict picker only, UI stays
inline in the diff panel)
**Branch rule:** all work on `feat/web-picker-parity`, branched from and
merging back into `web-dev` (never main). `web-dev` was fast-forwarded to
main first (it was 0 ahead), so the hunkpick toggle helpers,
`Service.ConflictPickerFile`, and `ParseConflictSized` are all available.

## Problem

Sub-project 3 of the GitKraken-inspired picker plan. The TUI conflict picker
has ordered line picks, tri-state side toggles (left/right/both — toggle
order = result order), touched-empty regions, a checkbox hierarchy, and a
live output pane. The web conflict picker has none of that: one exclusive
whole-side pick per region (`"ours"`/`"theirs"`). Worse, the web server
still parses **raw worktree text** (`ParseConflict` on `WorktreeFile`), so a
file whose content contains marker-like lines (a conflict once committed
unresolved) is refused with "no usable conflict markers" — the TUI fixed
this by regenerating from index stages (`ConflictPickerFile`), web never
adopted it.

Out of scope (later web waves): the staging picker stays whole-block; no web
unstage lane; no zoom analog (the browser panel already has space).

## Server (`internal/web/conflict.go`)

### Load path

`loadConflictDoc` keeps its status eligibility gate (unknown → 404, known
but not conflicted → 422) and then swaps:

- OLD: `svc.WorktreeFile(ctx, path)` → binary check → `ParseConflict(work)`.
- NEW: `content, markerSize, err := svc.ConflictPickerFile(ctx, path)`
  (regenerates from index stages with oversized markers; falls back to
  worktree text + size 7 internally) → binary check on `content` →
  `hunkpick.ParseConflictSized(content, markerSize)`.

The freshness hash becomes sha256 of the **regenerated content** — picks
stay positional against exactly the bytes the client saw; the POST recomputes
via the same path and 409s on hash drift, unchanged rule. Parse failure /
zero blocks keeps the existing 422 ("no usable conflict markers …").

### Wire: GET /api/conflict-hunks

Unchanged shape (`items` of `text`/`block` with `ours`/`theirs` lines,
`count`, `hash`) — the data is simply regenerated now. No client cache to
migrate (the SPA is embedded and versioned with the binary).

### Wire: POST /api/resolve-hunks

`picks[i]` grows from a string to a tagged object (no legacy string support —
both ends ship together):

```json
{"mode": "ours"}
{"mode": "theirs"}
{"mode": "lines", "lines": [{"side": "ours", "line": 0}, {"side": "theirs", "line": 2}]}
```

- `ours`/`theirs`: whole side, as today (`Mode = TakeCurrent/TakeIncoming`).
- `lines`: the ordered line-pick model — result order = array order; sides
  may interleave; an **empty `lines` array is decided-empty** (drop both
  sides). Server sets `Mode = LineByLine` and builds `Block.Picks`
  (`hunkpick.Pick{Side, Line}`) directly, validating every entry: side ∈
  {ours, theirs}, line within that side's length, no duplicate (side,line)
  pair — 400 with a precise message otherwise.
- Every region must be decided (`len(picks) == len(blocks)`, as today).
- Assembly and apply unchanged: `Doc.Resolved()` →
  `engine.ResolveConflictHunks` → 200 body = fresh status.

## Client (`internal/web/static/files.js` + `style.css`)

Mouse-first upgrade of the inline region UI (no new overlay, no keyboard
scheme):

- **State**: `conflictPick.choices[i]` becomes
  `{picks: Array<{side, line}>, touched: bool}` per region (order = pick
  order). Derived per side: all/some/none → tri-state.
- **Side tag = tri-state checkbox**: clicking a side's tag toggles the whole
  side (off if fully on, else on — appending its unpicked lines in line
  order), mirroring the TUI's `c`/`i`. Both sides can be on; the tag row
  shows which side came first when both are on (the `%s first` suffix).
- **Per-line checkboxes**: each rendered line gets a checkbox; clicking the
  line (or its box) toggles that line, appending/removing from the ordered
  pick list.
- **Decided-empty**: a region whose picks were touched and are now empty is
  decided, labelled `empty` (matches the TUI's suffix); an untouched region
  is undecided and gates the resolve button.
- **Resolve bar**: `N/M decided` + the existing all-ours / all-theirs
  buttons become tri-state whole-document toggles (complete-everywhere →
  clear, else complete — the TUI's `C`/`I` semantics via the same
  derivation, implemented client-side).
- **Live output pane**: a collapsible pane below the document (`#cf-doc`),
  assembled entirely client-side: passthrough text verbatim, each region's
  ordered picked lines, `‹region N undecided›` placeholder for untouched
  regions. Updates on every pick. Default expanded; collapse state kept for
  the picker's lifetime only.
- POST sends the tagged-object picks; whole-side-only regions send the
  `ours`/`theirs` fast path, anything else sends `lines`. 409 → reload
  picker, unchanged.
- Existing niceties preserved: whitespace glyphs (`·`/`→`/`¶`), the
  `(empty — this side has no lines)` placeholder (a zero-line side renders
  it and is NOT toggleable — the TUI's zero-line-side rule), CRLF `\r`
  tolerance in the blank test.

## Testing

- **Go** (`internal/web/conflict_test.go`, real git repos in `t.TempDir()`
  with `GIT_CONFIG_GLOBAL` isolation like the file's existing tests):
  - nested-marker fixture: commit a file CONTAINING literal `<<<<<<<`
    7-char marker lines, create a real conflict on it, GET
    /api/conflict-hunks succeeds (regenerated, oversized markers) — this
    exact case 422s today; also assert the returned block content matches
    the stage-derived sides, not the raw worktree ambiguity.
  - resolve with `lines` picks: interleaved both-sides order round-trips
    into the staged bytes in exactly array order.
  - decided-empty region: empty `lines` array drops both sides in the
    staged content.
  - validation: bad side, out-of-range line, duplicate pair, wrong picks
    count, stale hash → 400/400/400/400/409.
  - `ours`/`theirs` fast path still works end to end.
- **Client, headless CDP** against a real conflicted scratch repo (the
  playwright/CDP recipes in memory): broken-before/fixed-after on the
  nested-marker file; a mixed pick (side toggle + line toggles) shows the
  assembled output pane and, after resolve, `git show :0:path` equals the
  pane's content.

## Docs / delivery

- README web section: the conflict picker paragraph gains the
  line-level/toggle/output-pane description.
- CHANGELOG bullet (web section style).
- After the `web-dev` merge: `./build.sh web` and deliver the Windows exe
  per the standing convention (run-win.cmd self-swaps).
- No i18n work: the web frontend is English-only (i18n is a TUI layer).
