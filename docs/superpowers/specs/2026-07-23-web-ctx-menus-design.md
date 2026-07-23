# Web sidebar context menus — design

Date: 2026-07-23 · Branch: `feat/web-ctx-menus` (off `web-dev`) · Status: approved

## Goal

Right-click context menus on ALL three sidebar sections (user feedback: only
branches had one). Branches gain **delete branch**; worktrees and tags gain
menus with copy + destructive rows, all over the existing op transport and
decision modal.

## Menu contents (engine-backed only)

- **Branches** (menu exists, grows one row): go to tip · switch to (`!is_head`)
  · **delete branch** (`!is_head`, destructive styling).
- **Worktrees** (new menu): **copy path** (clipboard) · **remove worktree**
  (destructive; hidden on the served worktree's own row — see gating).
- **Tags** (new menu): **show commit** (same as left-click) · **copy tag name**
  · **delete tag** (destructive).

Out: delete-remote-tag / push-tag (network lanes, later), "switch to
worktree" (server re-root, own increment), create-tag/annotate (later).

## Engine facts (verified)

- `engine.DeleteBranch{Name}` — fail-fast guards BEFORE any prompt: the
  checked-out branch and a branch checked out in another worktree both return
  clear errors. Then decision `"delete-branch"` (`delete`/`abort`) parks; an
  unmerged branch parks a second fork `"branch-unmerged"`
  (`force-delete`/`keep`). Snapshots the tip to `refs/gg/versions` first, so a
  web-deleted branch is recoverable via `gg versions` (CHANGELOG-worthy).
- `engine.RemoveWorktree{Path, Branch}` — guards: the worktree you're in and
  the main worktree are refused with clear errors. Decision `"remove-scope"`
  (`worktree-only`/`worktree-and-branch`/`abort`; the branch option only when
  `Branch != ""`). A locked worktree parks `"worktree-locked"`
  (`unlock-and-remove`/`abort`); a dirty one parks its force fork. All render
  in the generic modal for free.
- `engine.DeleteTag{Name}` — **decision-free** (the TUI confirms before
  dispatch). The web must therefore confirm client-side via the existing
  `showLocalConfirm` (the pull pre-flight pattern) before starting the op.

## Server (`internal/web/ophttp.go`)

Three new cases in `handleOpStart`:

1. `"delete-branch"`: `req.Branch` non-empty + `isGitArgSafe` (byte-for-byte
   the `switch` case's guard) → `engine.DeleteBranch{Name: req.Branch}`.
2. `"remove-worktree"`: new request field `path`. The client-sent path is an
   IDENTIFIER, not an argument: the server reads `svc.Worktrees(ctx)` and
   looks the row up by exact path match — only the SERVER's own
   `wt.Path`/`wt.Branch` values reach the op (allowlist resolution; paths
   legitimately contain characters `isGitArgSafe` would reject). No match →
   404 `unknown worktree`. Matched → `engine.RemoveWorktree{Path: wt.Path,
   Branch: wt.Branch}`. The engine's own current/main-worktree guards stay the
   backstop (surface as `done{ok:false}` with their messages).
3. `"delete-tag"`: new request field `tag`, non-empty + `isGitArgSafe` →
   `engine.DeleteTag{Name: req.Tag}`.

`opStartRequest` grows `Path string `json:"path"`` and `Tag string
`json:"tag"``. No other transport changes.

## Client (`internal/web/static/`)

- **Generalize the menu**: extract `showCtxMenu(items, x, y)` from
  `showBranchMenu` (positioning + rendering + the existing outside-click /
  esc dismissal); `showBranchMenu` becomes a thin item-list builder. Add
  `contextmenu` listeners on `#worktrees-list` and `#tags-list` (worktree
  rows gain a `data-p` path attribute; tag rows already carry
  `data-n`/`data-h`).
- **Destructive rows**: menu items accept `danger: true` → a `.danger` class
  (red text, one CSS rule). Applied to delete branch / remove worktree /
  delete tag rows. (Destructive styling of MODAL OPTIONS stays deferred to
  transport hardening.)
- **Copy rows**: `navigator.clipboard.writeText(...)`, fire-and-forget with a
  status-line note on failure (clipboard needs a secure context; localhost
  qualifies).
- **delete tag confirm**: `showLocalConfirm("Delete tag <name>?", ["delete",
  "abort"], ...)` gates the dispatch — a right-click plus one click must never
  delete a ref with zero confirmation, and this op has no engine confirm.
- **Gating**: delete branch hidden on `is_head` rows (engine guard is the
  backstop for the worktree-checked-out case — its error names the worktree).
  Remove worktree hidden on the row whose `path` equals the served worktree's
  path — `/api/repo` already returns `worktree` (the TopLevel path; verified);
  the client keeps it on `state`. Engine guard again the backstop for
  main/current.
- **Known quirk, accepted**: the `branch-unmerged` and `remove-scope` forks
  where esc maps to "abort" work as today; `branch-unmerged` has no option
  named `abort`, so esc leaves that modal open — `keep` is the safe click.
- **Ops with `changed:true` must refresh the sidebar lists**: verify
  `refreshAfterOp` reloads worktrees/tags (it reloads repo/branches/status
  today; if worktrees/tags ride a separate loader, add them to
  `refreshAfterOp`).

## Error handling

- Engine guard refusals (current branch, main/served worktree) →
  `done{ok:false}` with the engine's message on the op line.
- 404 on an unknown worktree path; 400 on empty/unsafe branch or tag.
- Clipboard failure → status-line note, nothing else.

## Testing

Server (`internal/web/`, one test file per op case, `newRepoDir`-style local
fixtures — no remote needed):

- delete-branch: merged delete (confirm → gone, `changed:true`); confirm-abort
  (kept, `changed:false`, summary `cancelled`); unmerged → `keep` (second park
  `branch-unmerged` asserted, kept); unmerged → `force-delete` (gone);
  current branch → `done{ok:false}` naming checked-out; branch checked out in
  a second worktree → `done{ok:false}` naming the worktree; empty/unsafe name
  → 400.
- remove-worktree: `worktree-only` (dir gone, branch remains); `abort`
  (`changed:false`); `worktree-and-branch` (dir AND branch gone); unknown path
  → 404; the main worktree's path → `done{ok:false}` (engine guard).
- delete-tag: existing tag deleted (`changed:true`); missing tag →
  `done{ok:false}`; empty/unsafe name → 400.

Client: Playwright script (controller-run, post-merge pattern) — right-click
each section, exercise delete-branch confirm modal, remove-worktree scope
modal, delete-tag local confirm; screenshot the menus.

## Out of scope (deliberate)

- Delete/push remote tags, create/annotate tag.
- Switch-to-worktree (server re-root increment).
- Destructive-option styling inside the decision modal (transport hardening).
- Context menus outside the sidebar (commit rows etc.).
