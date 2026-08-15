# Task 4 — finding things: deep search, real filters, solo from a commit

**Depends on task 0.** Read `README-web-parallel-tasks.md` first.

The browser's `/` filters **only the commits already loaded**, client-side, on
subject / author / sha-prefix. In a repo with 600k commits that is a rounding
error: the thing you are looking for is almost never in the first pages. The
TUI has three answers — eager paging into unloaded history, a server-side
`git log` filter, and a fuzzy file finder — and the web has none of them.

## Your files

| Server | Client |
|--------|--------|
| `internal/web/search.go` (new) | `static/search.js` (new) |
| `internal/web/commits.go` (**yours exclusively**) | `static/commits.js` (**yours exclusively**) |
| `internal/web/solo.go` (**yours exclusively**) | |
| `internal/web/commitedit.go` (**yours exclusively**) | |
| `internal/web/search_test.go` (new) | |

You own the commit feed end to end. No other task may touch these files, and
you must not touch `ophttp.go`, `server.go`, `files.js`, `sidebar.js` or
`index.html` (rows via `registerRows`, help via `registerHelp`).

## What already exists

- `git.LogScope{Branches, Upstreams, Paths, Author, Grep, Since, Until}` —
  the feed's scope already carries every filter the TUI's `\` popup offers.
  The web sets only `Branches`. Everything else is unused wire.
- `domain.CommitFeed` — paged walking with `LoadInitial` / `LoadMore` /
  `Refresh`, a generation counter, and a scope cache. `Refresh` reconciles a
  fresh first page against what is loaded instead of trimming the list, which
  is what makes paging deep safe: a background refresh will not throw your
  search away.
- `solo.go` stores the solo scope as a **branch name** and turns it into a
  `LogScope` when the feed is built. Solo-from-a-commit needs a second shape
  (a commit id), which is the only structural change in this task.

## Work

1. **Server-side feed filter.** `/api/commits` accepts `path`, `author`,
   `grep`, `since`, `until`; they fill `LogScope` and reset the feed. Values
   are passed to `git log` — they are user text, so they travel as separate
   argv entries (the `gitcmd` builder already does this) and never get spliced
   into a string. A filter bar in the commits pane, cleared by one control,
   and the commit graph hides while a content filter is active (lanes are
   meaningless over a subset — the TUI does the same).
2. **Eager search.** A *search deeper* action that pages unloaded history
   looking for the next match of the active query and jumps to it. Each press
   digs past what is already loaded; a hit already on screen does not stop it.
   Ask before loading many more pages, and keep the query engaged so repeated
   presses keep digging.
3. **Fuzzy file finder.** A palette-style overlay over the repo's tracked
   paths (`git ls-files`, cached per HEAD), filtering as you type, opening the
   file's history on enter. The TUI's `F`.
4. **Marked commits.** The web can mark rows (ctrl+click) but does nothing
   with a pair. Add the TUI's two: **compare the two marked commits** (the
   compare view already takes two hashes) and **squash the marked range** into
   one commit (`commitedit.go` builds the plan server-side from a fresh read —
   extend it; never accept a plan off the wire).
5. **Solo from a commit.** The scope becomes "history reachable from this
   commit". Extend the stored solo shape from a bare branch name to
   `{kind: "branch"|"commit", ref}` — keep the wire allowlisted, and keep the
   existing branch behaviour byte-identical. The header shows
   `solo: <short-sha>` and the existing chip clears it.

## Acceptance

- Go tests: each filter narrows the feed (a repo with two authors and two
  paths is enough); filters compose with a branch scope; an unknown filter
  key is ignored rather than erroring; solo-by-commit returns only that
  commit's ancestry and solo-by-branch is unchanged.
- Browser, control run first: typing a path filter reduces the list and hides
  the graph; clearing restores both; eager search finds a commit that is NOT
  in the first page and lands the cursor on it; the finder opens a file's
  history; solo-from-a-commit narrows the list and the chip clears it.
- `./test.sh race` green. CHANGELOG bullet. `registerHelp` row.

## Notes

- **Do not reset the feed on every keystroke.** Debounce, and cancel the
  in-flight walk — the feed already carries a generation counter for exactly
  this, and a stale page arriving late must be dropped, not appended.
- Keep the cursor's commit across a refilter where you can: read the cursor's
  hash *before* the refresh, not after. The working-tree row shifts every
  display index, and reading it afterwards anchors to the wrong commit — that
  bug shipped once already.
- The graph is expensive on big repos. When a filter is active, do not compute
  lanes at all.
- 600k-commit repos are the target. Anything you add must be O(page), not
  O(history).
