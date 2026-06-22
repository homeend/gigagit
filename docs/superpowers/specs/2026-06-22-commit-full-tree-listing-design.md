# Full file tree at a commit (approach A) — Design

**Goal:** In the commit files view (`l`), let the user toggle from the *changed*
files of a commit to **every file that exists in that commit's tree** — the full
set you'd get by checking the commit out — listed in the same file tree.

**Status:** Approved approach A (flat full-tree mode), v1 / exploratory.

## Background & measured cost

`git ls-tree -r --name-only <commit>` lists every file in a commit's tree by
walking tree objects — no checkout, no working-tree stat, works for any commit.
Measured: babel 27,609 files in 0.05 s; Linux ~80–95k files in ~0.1–0.3 s. The
git side is cheap; the cost is TUI-side and entirely about *eager* work at 10⁴–10⁵
rows (the changed-files view was built for dozens).

This is **approach A** from the feasibility analysis: reuse the existing flat,
dir-grouped files view. A real collapsible tree (approach B) is out of scope.

## Design

- **Git verb** `Repo.TreeFiles(ctx, commit) ([]model.CommitFile, error)` — one
  `ls-tree -r --name-only -z <commit>` invocation, NUL-split, each path →
  `CommitFile{Path: p}` (empty `Status` — it is the whole tree, not a change set).
- **Domain query** `Service.TreeFiles(ctx, hash)` — mirrors `CommitFiles`, under
  the Read reservation, singleflight key `tree-files:<hash>`.
- **TUI mode** `Model.filesAllFiles bool`. The files view (`l`) opens in
  changed-files mode (false). Pressing **`a`** in the view toggles to full-tree
  mode and reloads; `a` again returns to changed files. Opening/closing the view
  resets it to false. The title shows `Files <sha> (all files) <subject>` in
  full-tree mode.
- **Off-thread build (the perf point).** `loadTreeFilesCmd(hash)` fetches AND
  builds the `[]contentLine` (`commitFileLines`, which sorts dir-major) inside the
  command goroutine, returning `treeFilesMsg{hash, lines, err}`. The UI thread
  only assigns the pre-built slice — no 80k-row sort on the render thread. (The
  changed-files path keeps its existing on-arrival build; it is tiny.)
- **`enter` in full-tree mode** diffs the file's version **at the commit
  (left/base) against the working tree (right)** — `loadCompareDiffCmd(
  Endpoint{EndpointCommit, hash}, Endpoint{EndpointWorkTree}, line)`. This is
  useful for any file (not just ones changed in the commit, which would be an
  empty parent-diff). `h`/`b` (history/blame at `path@hash`) already work
  unchanged.

## Known v1 limitations (documented, not fixed here)

- **Filtering stays O(n) per keystroke** (`contentPopup.visible()` scans all
  lines). At 80k that is a few ms + allocation per keystroke — usable, not crisp.
  Lazy/windowed filtering is a follow-up (same lesson as the commit feed).
- **Not a real tree.** One heading level, no nesting/expansion; a full repo shows
  thousands of dir headings through a flat scroll. Good for searching, not
  browsing. That is approach B.
- **`enter` empties** only when the committed file equals your working copy
  (a true no-diff) — expected.

## Testing

- **git** (`internal/git`): `TreeFiles` returns every path in a real commit's
  tree (real-git, a repo with nested dirs), empty `Status`, NUL-safe.
- **domain** (`internal/domain`): `TreeFiles` query round-trips via `FakeRunner`
  (argv `ls-tree -r --name-only -z <hash>`) and the real path through a temp repo.
- **TUI**: `a` toggles `filesAllFiles` and dispatches `loadTreeFilesCmd`;
  `treeFilesMsg` with a matching hash populates the view (pre-built lines); a
  stale-hash msg is dropped; `enter` in full-tree mode opens a compare diff with
  `EndpointCommit`→`EndpointWorkTree` (assert the tag/endpoints); changed-files
  `enter` unchanged. Footer/help advertise `a`.

## Out of scope (v1)

Lazy/windowed filtering; a collapsible tree (B); a `.`-menu row (the `a` key is
the v1 lever); CLI surface.
