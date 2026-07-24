# Web diff navigation + stash untracked files — design

Date: 2026-07-24 · Branch: `feat/web-diffnav` (off `web-dev`) · Status: approved

## Goal

Two detail-view improvements from user feedback: (1) arrows at the top of the
diff pane to step next/prev **file** and next/prev **change block**; (2) fix
the empty file list on a stash whose changes are untracked files ("stash 1
for gigagit has no file in file list").

## Bug root cause (reproduced)

`git stash push -u` stores untracked files in the stash's THIRD parent
commit (`stash@{N}^3`, a parentless root commit); the stash commit's own tree
does not contain them. `CommitFiles` diffs `--first-parent -m`, so an
untracked-only stash lists zero files. Repro: untracked-only stash →
first-parent name-status is empty; `CommitFiles` on `^3` lists `A <file>`.
The TUI shares this blind spot (same `StashCommit`→`CommitFiles` pair) — out
of scope here, worth its own later fix.

## Fix — surface the untracked parent

**Server** (`internal/web/stashes.go`): each stash row gains
`untracked_sha,omitempty` — resolved at list time via
`svc.StashCommit(ctx, e.Ref+"^3")` (a plain `git rev-parse`; the input is the
server-owned `e.Ref` plus a literal — nothing client-sent). A resolve error
(no `^3`) just omits the field, same posture as the existing `sha`.

**Client** (`app.js`): the stash row's left-click / "show changes" routes to
a new `openStashDetail(st)` instead of `openCommitByHash(st.sha, …)`:

- Fetch `/api/commit/{st.sha}`; when `st.untracked_sha` is set, also fetch
  `/api/commit/{st.untracked_sha}` and CONCATENATE the file lists (tracked
  first, then untracked).
- Untracked rows carry a per-file sha override: `f.sha = st.untracked_sha`.
  `openFile` uses `f.sha || state.fileSha` when building the diff query —
  the only `openFile` change. Their status is `A` (a root commit's
  `--first-parent --root` diff), and the diff endpoint already skips the
  `sha^` read for status `A` (`diff.go`), so a root commit diffs cleanly as
  pure adds — no server diff changes.
- Everything else (title `≡ stash@{N}`, layout, files pane) matches the
  current drill-in.

## Feature — diff-pane navigation arrows

`#diff-header` (currently just the file path) becomes path + a right-aligned
toolbar of four buttons:

- **`‹ file` / `file ›`**: step `openFile(state.fileCursor ± 1)` through the
  ACTIVE list (`state.statusEntries` in status mode, else `state.files`) —
  works in commit, stash, and working-tree modes since they all share
  `openFile`/`fileCursor`. Disabled at the ends and when the list is
  empty; state recomputed whenever the files pane re-renders.
- **`‹ change` / `change ›`**: jump between change BLOCKS inside the rendered
  diff — a block = a contiguous run of `tr` rows whose class isn't `same`
  (add/del/change; in unified mode a changed pair renders as del+add rows,
  still one contiguous run). Implementation: query
  `#diff-body table.diff tr` on click, derive block-start rows, keep a
  cursor (`state.diffBlockIdx`, reset to -1 by `renderDiff`), scroll the
  target row into view (`scrollIntoView({block: "center"})`) and flash it
  (a short-lived CSS class). Clamped at the ends; no-op on a diff with no
  change rows (binary/too-large/notice states render no table).

Buttons only — NO new key bindings (`p` is already pull; the TUI's diff
file-step keys don't transfer cleanly).

## Error handling

- A failed `/api/commit/{untracked_sha}` fetch degrades to the tracked list
  alone (catch → empty list), never a broken detail.
- Arrows never throw on empty lists/diffs — they disable or no-op.

## Testing

Server (`internal/web/` — extend `opstash_test.go` or a sibling):

1. Untracked-only stash (`git stash push -u` with only a new file) → row has
   `untracked_sha`; `GET /api/commit/{untracked_sha}` lists the file with
   status `A`; `GET /api/diff?sha={untracked_sha}&path=…&status=A` returns
   rows (the added content), not an error.
2. Tracked-only stash → row has `sha` but NO `untracked_sha`.
3. Mixed stash (tracked edit + untracked file) → both fields present;
   tracked file listed under `sha`, untracked under `untracked_sha`.

Client (controller-run Playwright post-merge): untracked-only stash
drill-in shows the file and its added-content diff; `file ›` steps to the
next file in a multi-file commit; `change ›` scrolls between separated
change blocks in a file with two hunks.

## Out of scope (deliberate)

- The TUI's identical untracked-stash blind spot (separate fix).
- Keyboard bindings for the new navigation.
- Hunk-level staging (own increment).
