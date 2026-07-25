# Web hunk-staging UI — design

Date: 2026-07-25 · Branch: `feat/web-hunks-ui` (off `web-dev`) · Wave 3, track 2

## Goal

Surface the wave-2 hunk-staging backend in the diff pane: a per-file **hunk
view** that lists the file's index↔worktree change blocks with checkboxes and
stages the picked ones via `POST /api/stage-hunks`. Client-only track
(`app.js`, `index.html`, `style.css`) — the backend shipped in wave 2.

## Backend contract (shipped, wave 2 — consumed as-is)

- `GET /api/hunks?path=<p>` → 200
  `{count, hash, blocks: [{index, del: [lines], add: [lines]}]}`.
  `hash` = sha256(index‖0x00‖worktree), the freshness token. Errors:
  400 `invalid path`; 404 `unreadable path: …`; 422 with user-ready
  messages for untracked / binary / CRLF (each ends
  "— stage the whole file instead").
- `POST /api/stage-hunks` body `{path, picks: [block indexes], hash}`
  (writeGuard: JSON content type + loopback origin). Success **200 = a fresh
  `/api/status` payload** (the `/api/stage` convention). Errors: 400
  (`picks required`, out-of-range), **409 `file changed; refresh`** on hash
  mismatch, the same 404/422 guard chain as GET.
- Picks are POSITIONAL against the exact bytes the client saw — after EVERY
  successful round the hash changes (the index moved), so the client MUST
  refetch before offering more picks.

## UX design

### Entry points (both self-gating)

Eligibility: a status entry with `section === "changes"` and
`kind === "tracked"` (hunks are index↔worktree; untracked/conflicted rows
never qualify — the server would 422/404 anyway, this is just not offering
dead ends).

1. **Diff-nav button**: `<button id="hunks-btn" title="stage hunks">hunks</button>`
   appended inside `#diff-nav` (after `next-change`). Enabled only when the
   currently open diff is an eligible unstaged status diff
   (`state.filesMode === "status"` and the focused entry qualifies);
   disabled otherwise (same disabled styling as the other nav buttons).
   Click → `enterHunkView(path)`.
2. **RMB menu**: in the `#files-list` contextmenu handler, for an eligible
   row, add `{ label: "stage hunks…", act: () => enterHunkView(f.path) }`
   immediately AFTER the existing `stage <path>` item.

### Hunk view (pane content, not a layer)

`enterHunkView(path)`:
1. `GET /api/hunks?path=` — on non-200, `opLine(server message, true)` and
   stay in the normal diff (422 messages are user-ready).
2. On 200: set `state.hunks = {path, hash, blocks, picks: new Set()}` and
   render into `#diff-body` (replacing the diff), `#diff-title` set to
   `path — stage hunks`.

Render (`renderHunks()`):
- A top action bar: `Stage selected (n)` (disabled at n=0), `Select all`,
  `Clear`, `‹ back to diff`.
- Each block: a header row `hunk <i+1>/<count>` with a checkbox (checked =
  picked), then the block's `del` lines (−, `var(--del)` background) and
  `add` lines (+, `var(--add)` background), in a `<pre>`-like monospace list
  (reuse the diff table row styling classes where practical, else new
  `.hunk-*` classes). Blocks separated by a dim `⋯` divider (blocks carry no
  context lines or line numbers — position is conveyed by order).
- Clicking a block's header or checkbox toggles its pick; the action-bar
  count updates.

Keyboard: none in v1 (buttons only) — no new global keys, no layer push;
esc handling below.

### Staging round

`Stage selected` → `POST /api/stage-hunks {path, picks: [...sorted], hash}`:
- **200** (fresh status payload): `applyStatus(resp); renderFiles();` then
  REFETCH `GET /api/hunks?path=`:
  - 200 with `count > 0` → re-render the hunk view with the NEW hash and
    cleared picks (more hunks remain).
  - 200 with `count === 0`, or 404/422 (file left the eligible set — fully
    staged) → `exitHunkView()` back to the status diff/list, `opLine`
    showing the op summary is unnecessary (the status pane already moved);
    reconcile `state.fileCursor` against the rebuilt `state.statusEntries`
    (the file may now appear only under Staged).
- **409** `file changed; refresh` → `opLine(message, true)` + automatic
  refetch (same as the 200 tail: re-render with fresh blocks/hash, cleared
  picks, or exit if no longer eligible).
- Other errors → `opLine(message, true)`, keep the view (picks intact) —
  the user can retry or back out.

### Exiting

`exitHunkView()` clears `state.hunks` and re-opens the current entry's
normal diff (`openStatusDiff(state.fileCursor)` when the entry still exists,
else `renderFiles()` + empty diff notice). Exits happen on: the back button,
opening any file (`openFile`/`openStatusDiff` start by clearing
`state.hunks`), a completed round that empties the file, and Escape — via a
ONE-LINE guard at the top of `drillOut()`:

```js
if (state.hunks) { exitHunkView(); return; }
```

(Without this, esc inside the hunk view would exit the whole status screen.)

Refresh interplay: `refreshAfterOp`/`reconcileStatusView` may rebuild
`state.statusEntries` while the hunk view is open (e.g. a background op
finishing). The hunk view holds only `path` — after any status re-read,
`reconcileStatusView` gains a guard: if `state.hunks` is set and its path no
longer has an eligible `changes` row, `exitHunkView()`.

## Declared edit regions (for cross-track merge safety)

This track edits ONLY:
- `app.js`: (a) one NEW contiguous region (hunk-view functions:
  `enterHunkView`, `renderHunks`, `exitHunkView`, helpers) placed with the
  diff-pane region (after `renderDiff`/nav wiring, before the
  focus/keyboard region); (b) small edits INSIDE `openFile`,
  `openStatusDiff` (clear `state.hunks` on entry; hunks-btn eligibility),
  `updateDiffNav` (hunks-btn disabled logic), `drillOut` (the one-line
  guard), `reconcileStatusView` (the eligibility guard); (c) ONE added item
  in the `#files-list` contextmenu handler, directly after the existing
  `stage <path>` item; (d) `state` gains a `hunks: null` field.
- `index.html`: `#hunks-btn` inside `#diff-nav` only.
- `style.css`: appended `.hunk-*` rules only.

It does NOT touch: the keydown router, the footer, `#top`, the layer stack,
or any Go file. Known cross-track merge point: the discard track also adds
an item to the SAME contextmenu handler (at the END of the items list) —
textual adjacency may conflict at merge; resolution is keep-both.

## Testing

- `node --check internal/web/static/app.js` gate.
- Playwright scenario (scratch repo under /tmp, a tracked file with two
  separated edits → two blocks):
  1. Open working tree → the file's diff → `hunks` button enabled; click →
     two blocks rendered, action bar shows `Stage selected (0)` disabled.
  2. Pick block 0 → stage → status pane now shows the file under BOTH
     Staged and Changes; the view refetched and shows ONE remaining block
     with a new hash (assert picks cleared).
  3. Stage the remaining block → view exits (file fully staged, gone from
     Changes), file listed under Staged only.
  4. 409 path: mutate the file on disk (execSync) after render, then stage →
     status strip shows `file changed; refresh`, view re-rendered with
     fresh blocks.
  5. RMB on an eligible row shows `stage hunks…`; RMB on an untracked row
     does not. Untracked file via direct `enterHunkView` → 422 message on
     the strip (feature self-gates, but the guard message must surface).
  6. Esc inside the hunk view returns to the diff, NOT the commit list.

## Out of scope

- Unstaging hunks (the backend stages picks only; per-file unstage stays
  whole-file `u`).
- Line-grained picks (blocks only — matches the backend's positional picks).
- Conflict-file hunk resolution in the browser (TUI's x editor owns it).
- CRLF files (the backend 422s them today; the wave-3 discard/CRLF track
  relaxes that server-side — this UI simply surfaces whatever the server
  answers, so it needs no change when that lands).
