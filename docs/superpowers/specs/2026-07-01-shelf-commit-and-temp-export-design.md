# Shelf a commit + copy shelf/bookmark items to a temp directory

Date: 2026-07-01
Status: Approved (brainstorm) — ready for implementation plan

## Summary

Two related capabilities:

1. **Shelf a commit** — freeze a commit's *changed files* into the shelf as one
   durable entry, so it can be restored even after the commit is no longer
   reachable in git (mirroring the shelf's existing "frozen content" promise for
   files).
2. **Copy to temp dir** — a context-menu action on shelf entries and bookmarks
   that writes their content into a fixed sibling directory of the repo,
   `<repoRoot>.tmp/`, under a descriptively-named subdirectory.

## Key decision: "a commit" = its changed files only

When a commit is shelved or copied out, we snapshot **only the files the commit
changed**, not the full repository tree at that commit.

- Rationale: gigagit targets ~20GB-head monorepos. A full-tree `git archive`
  stored durably per shelved commit would be multi-GB — a disk/time hazard on
  first real use. Changed-files-only is small, durable, and matches the natural
  reading of "what this commit did".
- Implementation: `git diff-tree --no-commit-id --name-only -r <sha>` yields the
  changed paths; `git archive <sha> -- <paths>` yields a tar of just those
  files' content **at that commit**. Deleted-by-the-commit paths have no content
  at `<sha>` and are simply absent from the archive.
- **Content only.** No commit message, author, or parents are preserved. This is
  a file export, not a commit recreation.

## Directory layout

- **Base dir is fixed** (not configurable): the **main worktree** root with a
  `.tmp` suffix. `/aaa/xxx/repo` ⇒ `/aaa/xxx/repo.tmp`. Anchoring on the main
  worktree (not the current, possibly-linked worktree) keeps `.tmp` a sibling of
  the repo, mirroring how worktree paths resolve. The base sits **outside** the
  repo working tree, so no `.gitignore` entry is needed.
- Each copy-out lands in its own **subdirectory** under the base, named
  descriptively by source type ("Descriptive per type"):

  | Source                       | Subdir name                         |
  |------------------------------|-------------------------------------|
  | shelf commit / bookmark commit | `commit-<shortsha>`               |
  | shelf **file** entry         | the shelf entry ID (e.g. `worktree-foo-a1b2c3d`) |
  | bookmark **file**            | `bookmark-<label-slug>`             |
  | fallback (no good name)      | `unshelf-<random>`                  |

  Files land inside the subdir preserving their repo-relative path, e.g.
  `<repoRoot>.tmp/commit-a1b2c3d/src/foo.go`.

## Architecture

Fits the existing frontend-agnostic seams: domain owns shelf/bookmark
resolution, a new engine op performs the (out-of-worktree) writes, TUI/CLI drive
it. TUI/CLI never import `internal/git`.

### model (`internal/model`)

- Add an explicit `ShelfEntry.Kind` field: `ShelfKindFile` | `ShelfKindCommit`.
  A shelf blob now carries **two incompatible payloads** — raw file bytes vs. a
  tar archive to extract — and copy-out forks on exactly this. The kind is
  **stored explicitly**, never inferred from an empty path, so a misclassified
  entry can never silently write garbage.
- `ShelfEntry.IsCommit()` convenience returns `Kind == ShelfKindCommit`.
- Bookmarks keep their existing `IsCommit()` inference (`Path == "" &&
  State == StateCommitted`); they store no divergent blob format, so inference is
  safe there.
- Add an `ExportFile` value type (`RelPath string`, `Data []byte`, optional
  `Mode`) — the unit passed from domain to the engine write op.

### domain (`internal/domain`)

- `ShelfAddCommit(ctx, sha) (model.ShelfEntry, error)` — resolve changed paths,
  `git archive <sha> -- <paths>` → tar bytes → `store.Put` with
  `Kind = ShelfKindCommit`, `Origin = {Commit: sha, Path: "", State: committed}`.
  One shelf entry per commit.
- `TempExportBase(ctx) (string, error)` — compute `mainWorktreeTopLevel + ".tmp"`.
- `ExportSource(ctx, src) (files []model.ExportFile, suggestedName string, err error)`
  — resolve a shelf entry or bookmark into the files to write plus the default
  subdir name:
  - shelf/bookmark **file** → 1 `ExportFile` via existing `ResolveBytes` /
    `BookmarkBytes`.
  - shelf **commit** → read the stored tar blob and extract to `[]ExportFile`
    (no git needed — this is the durability path).
  - bookmark **commit** → `git archive <sha> -- <changed paths>` live at copy
    time (bookmarks are live-by-address by design; only shelf freezes content).

Tar extraction uses the Go stdlib (`archive/tar`) — pure, no git/TUI deps.

New/reused git verbs (`internal/git`):
- changed-paths: reuse existing `DiffTreeFiles` / commit-files verb if suitable,
  else add `ChangedPaths(rev)` = `git diff-tree --no-commit-id --name-only -r <rev>`.
- `ArchiveTree(rev string, paths []string) ([]byte, error)` =
  `git archive --format=tar <rev> -- <paths>`.

### engine (`internal/engine`)

- `ExportToDir{ Dir string; Files []model.ExportFile }` op:
  - creates `Dir` and any parent dirs for each file,
  - writes each file, emitting a `Progress` event per file,
  - if `Dir` already exists (e.g. re-exporting the same commit), emits an
    overwrite/cancel `Decision` (mirrors `WriteFile`'s existing-file handling),
  - returns `Result{Summary, Changed: true}` on success.
  This is the **first op that writes outside the worktree** — it writes to an
  absolute path rather than through `Repo.WriteWorktreeFile`.

### tui (`internal/tui`)

- **Commits panel `.` menu**: add **"Shelf this commit"** next to the existing
  "Bookmark this commit" row → `domain.ShelfAddCommit(sha)`.
- **Shelf `G` popup** (`shelf_popup.go`) and **Bookmark `g` popup**
  (`bookmark_popup.go`): add **`[t]` Copy to temp dir**. It opens an editable
  destination popup pre-filled with `<base>/<suggestedName>` (reusing the
  existing paste/restore `[p]` destination-popup pattern); `enter` resolves
  `ExportSource` and runs `startOp(engine.ExportToDir{...})`; `esc` cancels.
- Shelf commit entries render via `FileAddress.Display()` (already renders a
  path-less commit as its sha + subject).
- Advertise the `[t]` key in the popup hint line and in `help.go` (per the
  "advertise features in help AND footer" convention).

### cli (`internal/cli`)

Lean parity:
- `gg shelf commit <sha>` — shelve a commit.
- `gg shelf export <entry-id> [--dir <path>]` — export a shelf entry to the temp
  dir; `--dir` overrides the computed default. The `ExportToDir` decision is
  answered by the flag policy / stdin, defaulting to cancel on conflict when not
  a terminal.
- Bookmark export parity (`gg bookmark export`) is optional and may be deferred.

## Error handling

- Shelving a commit with no changed files (e.g. an empty commit): produce a clear
  error rather than an empty entry.
- `git archive` / extraction failures surface as domain errors (recorded via the
  standard failure seam).
- `ExportToDir` existing-dir conflict → overwrite/cancel decision; cancel is the
  safe default.
- Copy-out of a **path-less commit bookmark** goes through the commit branch
  (`git archive`), not the file branch — guarded so it never hits
  `BookmarkBytes` (which errors on `IsCommit()`).

## Testing

- **engine `ExportToDir`** (real tmpdir): writes nested files with correct
  content; overwrite decision path; Progress event per file; cancel leaves no
  partial write (or documents partial-write behavior explicitly).
- **domain `ShelfAddCommit` + export round-trip** (real git repo): shelve a
  commit, export, assert the changed files land with correct content and
  repo-relative paths; **assert it still works after the commit is made
  unreachable/gc'd** — the durability promise.
- **domain base-dir computation**: `<repoRoot>.tmp`, anchored on the main
  worktree even when invoked from a linked worktree.
- **bookmark commit export** (live `git archive`) vs **shelf commit export**
  (stored tar) produce equivalent file sets for the same commit.
- e2e scenario (optional): `gg shelf commit <sha>` then `gg shelf export` and
  assert files appear under `<repo>.tmp/commit-<sha>/`.

## Docs to update on completion

`CHANGELOG.md` (always); `README.md` (new user-facing menu action + CLI);
`CLAUDE.md` shelf/bookmark/engine package-map entries; and
`internal/agentskill/using-gg.md` + `agentskill.Version` bump (CLI surface
changed), then `gg init --update`.

## Out of scope (YAGNI)

- Configurable temp base dir (fixed by request).
- Full-tree commit snapshots (changed-files-only chosen).
- Preserving commit metadata (message/author/parents).
- Renaming individual files within the destination popup (only the target subdir
  is editable).
