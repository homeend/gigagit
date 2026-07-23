# Web stashes — design

Date: 2026-07-24 · Branch: `feat/web-stashes` (off `web-dev`) · Status: approved

## Goal

Surface git stashes in the web client: a 4th sidebar section listing them,
left-click opens the stash's changes in the existing commit detail, a
right-click menu applies/pops/drops, and a status-pane button creates one.
Ops #8-11 on the transport.

## Engine/domain facts (verified)

- `domain.StashList(ctx) ([]model.StashEntry, error)` — `StashEntry{Ref,
  Subject}` (`"stash@{0}"`, text after the ref), newest first.
- `domain.StashCommit(ctx, ref) (string, error)` — resolves a stash ref to
  its commit SHA under a Read reservation.
- The TUI's own drill-in is `StashCommit(ref)` → `CommitFiles(sha)`
  (`internal/tui/stash_view.go`) — the exact pair behind the web's existing
  `/api/commit/{sha}` detail, so stash commits provably work there.
- Engine ops, ALL decision-free: `Stash{Message, Paths, IncludeUntracked}`,
  `StashApply{Ref}`, `StashPop{Ref}`, `StashDrop{Ref}`. TUI parity for
  confirms: apply/pop dispatch directly; **drop confirms first** (the TUI's
  y/n); create goes through an explicit popup in the TUI → a local confirm in
  the web.

## Server (`internal/web/`)

1. **`GET /api/stashes`** (new `stashes.go`): `svc.StashList`, then
   `svc.StashCommit` per entry (stash lists are short; a per-entry resolve
   failure drops the sha, not the row). Rows: `{ref, subject, sha}` —
   the sha rides along so the client's left-click needs no second request
   (the tags-row pattern). Registered as `GET /api/stashes` next to
   `/api/tags`.
2. **Op cases** in `handleOpStart` (new request field `ref`):
   - `"stash-apply"` / `"stash-pop"` / `"stash-drop"`: the client-sent `ref`
     is an IDENTIFIER — resolved by exact match against the server's own
     `svc.StashList` (the remove-worktree allowlist pattern; only the
     server's `entry.Ref` reaches the op); 400 on empty, 404 `unknown stash`
     on no match. Then `engine.StashApply{Ref}` / `StashPop{Ref}` /
     `StashDrop{Ref}`.
   - `"stash"`: `engine.Stash{Message: req.Message, IncludeUntracked: true}`
     — message optional (empty = git's default subject), `Paths` empty = all
     changes. No nothing-to-stash guard: git's own error surfaces as
     `done{ok:false}`.
3. An apply/pop conflict is a plain op error (`done{ok:false}`); the client's
   status refresh shows the conflicted tree — the established convention.

## Client (`internal/web/static/`)

- **Sidebar**: `stashes` section (header + list) under tags in `index.html`;
  dblclick-collapse comes free from the existing `["branches", "worktrees",
  "tags"]` loop — extend it with `"stashes"`. `fetchBranches`'s `Promise.all`
  gains `getJSON("/api/stashes")` (`state.stashes`), so `refreshAfterOp`
  refreshes the section with zero new wiring. Rows render `stash@{N}` +
  dim subject, `data-r` (ref) + `data-h` (sha) attributes.
- **Left-click**: `openCommitByHash(sha, "≡ " + ref)` — the existing detail.
  A row whose sha failed to resolve ignores left-click (its context menu
  still works — apply/pop/drop go by ref, not sha).
- **Right-click** (`showCtxMenu`): **show changes** (same as left-click) ·
  **apply** · **pop** · **drop** (danger, gated behind
  `showLocalConfirm("Drop " + ref + "?", ["drop", "abort"], …)` — the op is
  decision-free, so the confirm lives client-side, the delete-tag precedent).
  Apply and pop dispatch `startOp` directly (TUI parity — no confirm).
- **Create**: a `stash` button in the status pane beside `commit`, enabled
  whenever the working tree has files (any section). It takes the
  commit-message box's current text as the stash message (may be empty) and
  dispatches after
  `showLocalConfirm("Stash all working-tree changes?", ["stash", "abort"], …)`.
  On success the message box clears (the commit convention) and the status
  pane reconciles via the normal refresh.

## Error handling

- 400 empty ref; 404 forged/stale ref (allowlist miss) — leaks nothing.
- Apply/pop conflict → `done{ok:false}` + conflicted status pane.
- Nothing-to-stash → git's error on the op line.
- A `StashCommit` resolve failure at list time drops only that row's sha
  (row still listed, actions still work — only drill-in degrades).

## Testing (`internal/web/opstash_test.go` + `stashes.go` handler tests)

Local fixtures (`newRepoDir` + dirty file + `git stash`):

1. List: two stashes → rows carry ref/subject/sha, newest first; sha equals
   `git rev-parse stash@{N}`.
2. Create: dirty tree → `{"op":"stash","message":"wip x"}` → done
   `changed:true`; status clean; list gains a row whose subject contains
   "wip x".
3. Apply: `changed:true`, file modified again, stash still listed.
4. Pop: `changed:true`, file modified again, stash gone.
5. Drop: `changed:true`, stash gone, working tree untouched.
6. Pop-with-conflict: stash touching a file, then commit a conflicting
   change, pop → `done{ok:false}`; `/api/status` shows the file conflicted.
7. Unknown/forged ref (`stash@{9}`, `--flag`) → 404; empty ref → 400.

Client: controller-run Playwright post-merge — section renders, left-click
detail, apply/pop/drop flows, create button.

## Out of scope (deliberate)

- Path-scoped stashing and the untracked toggle (TUI popup parity later).
- Stash-branch (`git stash branch`) — no engine op exists.
- Destructive-option styling inside the decision modal (transport hardening).
