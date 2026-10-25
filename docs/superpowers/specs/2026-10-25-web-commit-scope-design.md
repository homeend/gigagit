# gg web — multi-branch commit view and all files (parity stage H)

Two rows of docs/web-tui-parity.md, ported from the TUI as it is.

## 1. Multi-branch "Add to commit view"

### What the TUI does (the spec)

- One ordered list, `commitScopeBranches`, is the Commits feed's scope. Empty
  = every local branch (plus HEAD). Solo is a list of one: Solo this branch /
  Solo this tag / Solo from this commit SET the list to that one entry, or
  clear it when it already is that one entry.
- Branches `.` menu: **Add to commit view** / **Remove from commit view** on
  the selected local branch toggles it in the list (`commitToggleRow`); a
  removal of the last member returns to all. A sha entry left by Solo from
  this commit stays in the list when a branch is added.
- **Show all branches** clears the list. Offered on the Branches and the
  Commits `.` menus, only while the feed is scoped.
- The walk is `Branches` + `Upstreams`: the upstream of every local branch in
  scope (every local branch when unscoped), only if that remote-tracking ref
  exists (`feedUpstreams`).
- Commits title: `all` / `solo: <entry>` (a 40-hex sha shown short) /
  `<n> branches`.
- Branches panel: `◉` in the gutter of every branch in the list.
- Territory segments colour the dots of any scoped, unfiltered feed. A
  boundary is a merge-base fork point against any branch NOT in scope, a local
  tip not in scope, or a remote tip that is not a scoped branch's own
  upstream (`segBoundary`).
- Review visibility follows the list: `viewBranches` is the whole list, else
  the checked-out branch.

### Web today

`s.solo` is one stored string (a ref, or `commit\x00<sha>`); the chip says
`solo: X ✕`; no Upstreams in any walk; segments only for one ref; the branch
list has no scope marker.

### Design

Server (`internal/web/solo.go`, `search.go`, `commits.go`):

- `s.solo string` → `s.scope []string`, each entry encoded as today
  (`encodeSolo`/`decodeSolo`), in order. One source of truth, re-read at every
  feed build as `s.solo` was.
- `POST /api/solo` keeps both existing request shapes (`{branch}`,
  `{kind, ref}` = set to that one entry, empty = clear) and adds
  `{"op":"add","ref":<branch>}` and `{"op":"remove","kind":…,"ref":…}`.
  Explicit ops, not a toggle: two tabs share this state, and the TUI row
  decides add vs remove from what it shows. Add validates like a branch solo
  (`isGitArgSafe` + local-branch lookup; tags are not added, as in the TUI,
  whose row is a Branches row) and is a no-op for a member. Remove needs only
  membership, so a branch deleted since it was added can still be removed.
- Every answer that carries `solo`/`solo_kind` also carries
  `scope: [{kind, ref}]`; `solo`/`solo_kind` stay filled for a list of one and
  are empty for a list of several.
- The walk: `LogScope{Branches: entries, Upstreams: …}`. Upstreams are
  computed PER `/api/commits` request (the TUI recomputes its scope signature
  on every refresh): the Upstream of each in-scope local branch (all of them
  when unscoped) that exists in the remote-branch list, deduped, in branch
  order. The web feed is not reset after an op, so a build-time list would go
  stale the moment a push sets an upstream; per request, a changed list
  changes `scopeSig` and the existing `ApplyScope` path re-walks.
- Segments: `soloSegments(ref)` becomes `scopeSegments(entries)` with the
  TUI's rule over the set (scoped map, own-upstream map of every scoped
  branch, `ScopeBoundaries(entries, others)`); served for any scoped,
  unfiltered feed.
- Lifecycle unchanged: whatever the web did with `s.solo` on a reroot or a
  checkout, the list does the same (nothing clears it today).

Client:

- `state.scope` = `[{kind, ref}]` from every answer; `state.solo` /
  `state.soloKind` stay as the single-entry view (empty for several) so the
  existing solo rows keep their meaning.
- The chip: `solo: <label> ✕` for one, `<n> branches ✕` for several (the
  TUI's words — "branches" even when a member is a tag or a sha); a click is
  Show all branches. `scopeLabel(scope)` is a pure function.
- Branch `.` menu (web's lowercase): `add to commit view` /
  `remove from commit view` beside the solo row, and `show all branches`
  while scoped — except when the solo row above it already reads
  `exit solo (show every branch)` (the same act twice). The commit menu gets
  `show all branches` on the same rule.
- Branch rows: `◉` before the name of every branch in the list (its own
  `.scope-mark` span, tooltip "in the commit view").
- `reviews.js viewBranches`: the refs of the whole list.
- Help (`?`): the new rows.

## 2. `a` — all files of a commit

### What the TUI does

- In a commit's files view (not a stash, not a compare, not a review), `a`
  toggles between the changed files and EVERY file of the commit's tree
  (`TreeFiles`, `ls-tree -r`). Works from the tree side and from the commit
  list side of the view.
- The title reads `Files <sha> (all files) <subject>`.
- The mode sticks while the view is open: moving to another commit loads that
  commit's whole tree. Closing the files view drops it.
- Rows carry no status letter. Opening one diffs the commit's version against
  the working tree (`<sha> ↔ working tree`) — the file may be unchanged in the
  commit, so a parent diff would be empty. The stacked view is not offered on
  the full tree (`diffStacked && !inFullTree`).
- Row ◆ note badges show (`filesCommitBadges` includes the full tree); the
  diff, a compare, carries no notes.
- Footer `[a] all files`; help `a toggle all files: …`.

### Design

- `GET /api/commit/{sha}?all=1` answers `svc.TreeFiles` in place of
  `CommitFiles` (empty status per file), plus `"all": true`; meta, message and
  reviews as today.
- `state.filesAll` beside `filesMode === "commit"` (no new mode). `allFilesOK()`
  gates `a`: files layout, commit mode, a commit sha, not a stash detail
  (`state.fileStash`, set by `openStashDetail`), not a review.
- `openCommit` / `openCommitByHash` keep the mode only while the screen they
  replace is a commit files view in it (the TUI's "sticks while open"); any
  other screen (the list, a compare, status, stash, review) drops it.
- `fileDiffURL`: in all-files mode,
  `/api/entry-diff?left=commit:<sha>&right=worktree&path=…` (status empty:
  both sides read; the working side leniently). Diff title suffix
  `<short> ↔ working tree`; diff ctx `notes: false`.
- The stacked view: a full-tree file always opens alone, as in the TUI
  (`buildTreeStack` excludes the full tree); `S` stays a saved preference.
- Title: `(all files)` after the short sha. Footer chip `a all files`
  (`.on` while on), help row.

### Scale (ruling)

The web file list is one `innerHTML` render; a monorepo tree of 10⁵–10⁶
paths makes a large answer and a slow paint. The TUI lists the whole tree
too, so the port does; recorded as a known limit for the big-repo work, not
solved here.

## Out of scope

Directory heading rows (the web's commit list is flat in both modes); the
TUI's ctrl+g solo+tip; clearing the scope on reroot/checkout (unchanged).
