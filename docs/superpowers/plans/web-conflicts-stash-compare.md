# Task 5 — conflicts, stashing, and comparing against a stored entry

**Depends on task 0.** Read `README-web-parallel-tasks.md` first.

Three gaps that all live in the files pane and the diff view, which is why
they are one task: nobody else may touch `files.js` while you hold it.

## Your files

| Server | Client |
|--------|--------|
| `internal/web/conflict.go` (**yours**) | `static/files.js` (**yours exclusively**) |
| `internal/web/op_conflict_extra.go` (new) | `static/conflicts.js` (new) |
| `internal/web/stashes.go` (**yours**) | |
| `internal/web/compare.go` (**yours**) | |
| tests alongside each | |

Rows via `registerRows("file", …)` / `registerRows("stash", …)`; help via
`registerHelp`. Do not touch `ophttp.go`, `server.go`, `commits.js`,
`sidebar.js` or `index.html`.

## What already exists

- `engine.MarkAllResolved{Paths []string}` — stages every conflicted path as
  it stands. The TUI's `A` in the conflict popup. The web has only per-file
  "mark resolved (stage as-is)".
- `engine.ResolveConflict{Path string, Action ConflictAction}` — the whole-file
  picks: keep current, keep incoming, and for a modify/delete conflict keep
  modified / delete / keep base. The web has **only** the hunk picker, which
  is the slow path when you already know which side wins.
- `engine.Stash{Message string, Paths []string, IncludeUntracked bool}` — the
  web always sends the whole tree; `Paths` is the unused half behind the TUI's
  per-file checklist (`s` on the Files panel).
- `domain.CompareFiles(ctx, left, right model.Endpoint)` and
  `domain.ComparePatch(ctx, left, right)`, where
  `model.Endpoint{Kind, Hash, ShelfID}` addresses **a commit or a shelf
  entry**. The compare view currently only ever passes commits; the shelf side
  is already modelled and unused.
- Bookmarks and the shelf themselves are done (sections, add, remove, open,
  restore, cherry-pick). What is missing is comparing *against* one.

## Work

1. **Mark all resolved.** One control on the conflict bar, guarded to the
   conflicted set, running `MarkAllResolved`. It stages markers as-is, so the
   label must say that plainly — this is the destructive-sounding one that
   users reach for when they have already resolved in an editor.
2. **Whole-file conflict picks.** In the file menu for a conflicted file:
   *keep current* / *keep incoming*, and for modify/delete the three-way set.
   The hunk picker stays as the detailed path; these are the shortcuts.
3. **Stash a selection.** The stash prompt grows a checklist of unstaged and
   untracked files (the TUI's popup: space toggles, name defaults to
   `WIP on <branch>`), sending `Paths` when a subset is picked and omitting it
   when everything is. Conflicted files are never stashable — leave them out.
4. **Compare against a bookmark or a shelf entry.** From a focused file or a
   marked commit: pick an entry, then diff against it. A shelved commit
   compares as `Endpoint{Kind: EndpointShelf, ShelfID: …}`; a live commit as
   `EndpointCommit`. Both sides resolve server-side — the wire carries an id
   and a kind from an allowlist, never a path into the store.
5. Where a compare falls back to a frozen side (the live commit is gone), say
   so in the title. The domain layer already marks it; do not swallow it.

## Acceptance

- Go tests: mark-all stages every conflicted path and nothing else; each
  whole-file action leaves the expected content and stages it; a modify/delete
  conflict offers exactly its three actions; `Stash{Paths}` stashes the subset
  and leaves the rest dirty; compare against a shelf entry returns the file
  list and a patch, including after the original commit is gone.
- Browser, control run first: a two-file conflict resolves with one click per
  file and the conflict bar clears; the stash checklist stashes one file of
  two; comparing a file against a shelf entry opens the diff view with both
  sides labelled.
- `./test.sh race` green. CHANGELOG bullet. `registerHelp` row.

## Notes

- Conflict actions must never be offered for a binary file or a path with no
  usable markers — `conflict.go` already returns those refusals; surface them,
  do not hide the rows.
- The compare view caches diffs. A mutable ref name as a cache key poisons it
  (that bug shipped once): key by resolved hash / entry id, never by branch
  name.
- Stash + conflicts do not mix: while a sequencer op is paused, the mass rows
  are hidden for good reasons. Keep that gate.
