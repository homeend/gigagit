# Export a commit (or a single file's change) as a git patch

Date: 2026-07-01
Status: Approved (brainstorm) — ready for implementation plan

## Summary

Add the ability to export a commit — or one file's change within a commit — as a
git **patch** file (`git format-patch` / mailbox format, `git am`-able), through
an editable-destination dialog in the TUI and a matching CLI command.

Two related actions, sharing almost all machinery:

1. **Export commit as patch** — from the Commits panel `.` menu. Writes the whole
   commit's change set as a single `<shortsha>.patch`.
2. **Export this file's diff as patch** — from inside the full-screen diff view
   when it is showing a commit-vs-parent file diff. Writes just that one file's
   change, still with the commit's metadata header, as
   `<shortsha>-<basename>.patch`.

Both open an editable single-line dialog pre-filled with a full destination path,
defaulting to the **parent directory of the repository**, and let the user change
the file name and/or path before writing.

## Key decisions (resolved during brainstorm)

- **Format = `git format-patch`, not a plain diff.** The output is a mailbox-format
  patch that preserves the commit's author/date/message and is re-appliable with
  `git am`. File extension `.patch`.
- **File-level export is also format-patch, scoped to the one file**
  (`git format-patch -1 <sha> -- <path>`). It keeps the commit header but only
  touches that file when applied. Verified: this keeps the full metadata header.
- **v1 scope = commit-vs-parent file diffs only.** The export action appears on a
  file diff only when the diff view is showing a commit's file (commit drill-in,
  history/blame per-file view). Working-tree, staged, and compare-two-endpoints
  diffs are **out of scope for v1** (see Out of scope). This was chosen knowingly
  over the broader "every diff view" option.
- **Default destination = parent of the repo root.** For a repo at
  `/aaa/xxx/repo`, the default directory is `/aaa/xxx/`, anchored on the **main**
  worktree (so it is stable even when invoked from a linked worktree). This
  deliberately differs from the shelf/temp-export feature's `<repo>.tmp` sibling
  convention.
- **Dialog is a single editable full path** (name *and* directory editable), unlike
  the shelf temp-export dialog which only made the target subdir editable.
- **Merge commits are refused with a clear error** (see below — this is
  load-bearing correctness, not cosmetics).
- **CLI parity ships in v1** (`gg commit export-patch`), matching how shelf,
  bookmark, and tag features all shipped TUI + CLI together.

## Load-bearing correctness: the merge-commit guard

`git format-patch -1 <sha>` does **not** error on a merge commit and does **not**
emit the merge. It silently **skips the merge and emits the patch for the first
non-merge ancestor**, carrying *that other commit's* `From`/`Subject` header. A
user who exported a merge would get a plausible-looking but **wrong** patch
(a different commit's changes, mislabeled). This was verified empirically.

Therefore the merge guard is mandatory and must be **authoritative in the domain
layer**, which has git access and covers all three call sites (TUI commit, TUI
file, CLI) uniformly:

- Domain resolves the commit's parent count and returns a sentinel error
  (`ErrMergeCommitPatch`, message ≈ "cannot export a merge commit as a patch")
  **before** invoking `format-patch`. Parent count via a git verb, e.g.
  `git rev-list --parents -n1 <sha>` (>2 tokens ⇒ merge) or an equivalent.
- The TUI Commits-panel row **additionally** pre-hides itself when the in-memory
  `model.Commit.Parents` has length > 1 (nicer UX: no dead menu row). The domain
  guard remains the backstop for the file-level and reflog/tag paths where the
  `Commit` struct (and thus `Parents`) may not be readily available.

## Architecture

Fits the existing frontend-agnostic seams. Domain owns patch generation + the
merge guard + default-name logic; a new engine op performs the out-of-worktree
write; TUI/CLI drive it. TUI/CLI never import `internal/git`.

```
TUI: "." menu (Commits panel / diff view)   CLI: gg commit export-patch
                 \                                    /
        domain.CommitPatch(sha) / domain.FilePatch(sha, path)
                 |  (merge guard → verb → bytes + default name)
        git.FormatPatch(rev, paths...)   +  parent-count verb
                 |
        engine.ExportFile{Path, Data}    (NEW op: writes an absolute path
                                          outside the working tree)
```

### git (`internal/git`)

- **`FormatPatch(ctx, rev string, paths ...string) ([]byte, error)`** —
  `git format-patch -1 --binary --stdout <rev> [-- <paths…>]`, returning stdout
  bytes. One git invocation (per the "a verb is one invocation" convention).
  - `--binary` keeps genuinely-binary file changes appliable (a literal binary
    patch) rather than a non-appliable "Binary files differ" line; harmless for
    text.
  - `--stdout` streams the single patch to stdout (no `0001-*.patch` files).
- **Parent-count verb** for the merge guard — reuse an existing verb if one already
  exposes parents, else add a small `ParentCount(ctx, rev) (int, error)` (e.g.
  `git rev-list --parents -n1 <rev>`).

### domain (`internal/domain`)

- **`CommitPatch(ctx, sha) (patch []byte, defaultName string, err error)`** —
  merge-guard first (return `ErrMergeCommitPatch` if a merge), then
  `git.FormatPatch(sha)`. `defaultName = shortSHA(sha) + ".patch"`.
- **`FilePatch(ctx, sha, path) (patch []byte, defaultName string, err error)`** —
  merge-guard first, then `git.FormatPatch(sha, path)`.
  `defaultName = shortSHA(sha) + "-" + filepath.Base(path) + ".patch"`.
- **`ExportDefaultDir(ctx) (string, error)`** — the parent of the main worktree:
  `filepath.Dir(filepath.Clean(mainWorktreeTopLevel))`, where the main worktree is
  `Worktrees(ctx)[0].Path` (same anchor `TempExportBase` uses). Mirrors the
  existing `TempExportBase` split (base dir separate from suggested name), so the
  naming logic stays unit-testable in domain.

Both patch functions record failures via the standard failure seam (the `query`
boundary), excluding `context.Canceled`/`DeadlineExceeded`, like every other
domain read.

### engine (`internal/engine`)

- **`ExportFile{ Path string; Data []byte }`** op — the file-grained sibling of the
  existing `WriteFile`, but writing to an **absolute path outside the working
  tree** via `os` directly (like `ExportToDir` does), not through
  `Repo.WriteWorktreeFile`:
  - `LockMode()` = `repogate.Read` (touches neither refs nor the working tree —
    same as `ExportToDir`).
  - Creates parent dirs (`os.MkdirAll`) then writes the file.
  - **Overwrite decision keyed on the FILE existing** (not a directory):
    if `Path` already exists with different bytes, ask the Decider
    overwrite/cancel; identical bytes are a silent no-op (`Summary: "unchanged"`).
    This mirrors `WriteFile`'s exact semantics and reuses the shared
    `writeOverwrite`/`writeCancel` options + `ErrWriteCancelled`.
  - Returns `Result{Summary, Changed: true, Path}` on write.

  **Why not reuse `ExportToDir`:** `ExportToDir`'s overwrite prompt is keyed on the
  *target directory* already existing. Our default directory is the parent of the
  repo, which **always** already exists — reusing `ExportToDir` would prompt
  "overwrite?" on literally every export. `ExportFile` keys the decision on the
  file, which is correct here, and avoids changing the shipped, tested
  `ExportToDir` behavior.

### tui (`internal/tui`)

Flow mirrors the shipped `temp_export.go` pattern (resolve first off-thread, then
open an editable popup carrying the resolved payload):

1. **Action rows:**
   - **Commits panel `.` menu** (and the commit-list side of a files view, for
     panel parity): `commitExportPatchRow()` — gated like `commitShelfRow()`
     (`focus == panelCommits && opsIdle()`, real commit hash), and **pre-hidden
     when `len(commit.Parents) > 1`**. Label: **"Export commit as patch"**, id
     `commit-export-patch`.
   - **Diff view `.` menu**: `exportFilePatchRow()` — lives in the
     `availableActions` `inContentWindow()` branch (verified: `diff_view.go`
     handles `.` → `openActionMenu()`). Gated on the front surface being a
     commit-vs-parent file diff: `dv := m.diffLayer(); dv != nil && dv.rev != "" &&
     !m.inCompareMode()`. The `!m.inCompareMode()` clause is essential —
     compare-mode diffs *also* set `dv.rev`, but the patch would be
     commit-vs-parent, not the compared endpoints. Label: **"Export this file's
     diff as patch"**, id `file-export-patch`.
2. **Resolve (async):** the row's `run` starts an off-thread command
   (`startExportCommitPatch(sha)` / `startExportFilePatch(sha, path)`) that calls
   the domain function and returns a `patchResolvedMsg{data, defaultName, err}`.
3. **Msg handling:** on `err` (including `ErrMergeCommitPatch`) → set a status
   message, **do not open the dialog**. On success → open an
   `exportPatchPopup{ dest textfield, data []byte }` with `dest` pre-filled
   `filepath.Join(defaultDir, defaultName)` (the full path), reusing the
   `textfield` + `viewField` widgets exactly as `tempExportPopup` does.
4. **Popup keys:** `enter` → `startOp(engine.ExportFile{Path:
   strings.TrimSpace(dest.Value()), Data: data})`; `esc` → cancel; the field is
   fully editable (name and path). Single-line (never inserts newline).
5. **Advertise** the new keys in the popup hint line and in `help.go` (per the
   "advertise features in help AND footer" convention).

`ExportFile` needs no post-op refresh (it writes outside the working tree —
`opAffectedSources` maps it to nothing, like `ExportToDir`).

### cli (`internal/cli`)

Under the existing `cmdCommit` sub-dispatch (same shape as `gg commit reword`):

- **`gg commit export-patch <sha> [--out <path>] [-- <file>] [--force]`**
  - No `-- <file>` → whole-commit patch (`domain.CommitPatch`).
  - `-- <file>` → file-scoped patch (`domain.FilePatch`).
  - `--out <path>` overrides the computed default (`ExportDefaultDir` +
    default name); default is used when omitted.
  - `--force` answers the `ExportFile` overwrite decision as overwrite; without it
    the decision is answered by the flag policy / stdin, defaulting to cancel when
    not a terminal (same convention as `gg shelf export`).
  - A merge `<sha>` prints the `ErrMergeCommitPatch` message to stderr and exits
    non-zero.

## Error handling

- **Merge commit** → `ErrMergeCommitPatch` (domain), surfaced as a status message
  (TUI) / stderr + non-zero exit (CLI). No dialog opens in the TUI.
- **`format-patch` failure / bad rev** → domain error via the standard failure
  seam; TUI status message, CLI stderr.
- **Destination already exists** → `ExportFile` overwrite/cancel decision; cancel
  (`ErrWriteCancelled`) is the safe default; identical bytes = silent no-op.
- **Empty commit** (no changes) → `format-patch` yields a header-only patch; this
  is acceptable (a valid, empty patch). No special-casing.
- **WIP pseudo-commit rows** (the ◇ Working tree / Staged rows) carry a
  git-invalid sentinel hash. The commit-level row never sees one (`m.commits`
  stays a pure feed; WIP rows are overlaid separately). The file-level path has no
  extra guard, but the worst case is a clean "invalid revision" git error surfaced
  as a status message — this is an inherited files-view limitation, not introduced
  here, and is explicitly out of scope to fix.

## Testing

- **git `FormatPatch`** (real repo, `FakeRunner` for argv): asserts the argv is
  `format-patch -1 --binary --stdout <rev>` and, with paths,
  `… <rev> -- <path>`; real-git test that the output is a valid mailbox patch and
  that a path filter scopes it to one file while keeping the header.
- **git parent-count verb** — 1 parent for a normal commit, ≥2 for a merge, 0 for
  the root commit.
- **domain `CommitPatch` / `FilePatch`** (real git):
  - normal commit → bytes contain the `From `/`Subject:` header and the expected
    file hunks; `defaultName` is `<shortsha>.patch` / `<shortsha>-<base>.patch`.
  - **merge commit → `ErrMergeCommitPatch`, no bytes** (guards the silent-wrong
    output found in the recheck).
  - **`git am` round-trip**: apply a `FilePatch` output onto a fresh repo seeded at
    the parent content and assert the file lands with the right content and the
    commit's author is preserved (the appliability promise).
- **domain `ExportDefaultDir`** → parent of the main worktree, stable when invoked
  from a linked worktree.
- **engine `ExportFile`** (real tmpdir): writes a nested absolute path with correct
  bytes; identical-bytes no-op; overwrite decision path (overwrite writes, cancel
  leaves the existing file untouched and returns `ErrWriteCancelled`).
- **e2e scenario** (optional): `gg commit export-patch <sha> --out <dir>/x.patch`
  then assert the file exists and its bytes start with `From `; a second run
  without `--force` cancels on the existing file.

## Docs to update on completion

`CHANGELOG.md` (always); `README.md` (new menu actions + CLI command);
`CLAUDE.md` (engine package-map: new `ExportFile` op + `git.FormatPatch` verb +
domain `CommitPatch`/`FilePatch`/`ExportDefaultDir`); and
`internal/agentskill/using-gg.md` + `agentskill.Version` bump (CLI surface
changed), then `gg init --update`.

## Out of scope (YAGNI)

- **Exporting non-commit diffs** (working-tree, staged, compare-two-endpoints,
  shelf/bookmark compares) as patches — deliberately excluded from v1. "Patch" is
  ill-defined for e.g. working-tree-vs-index (no commit to attribute), and would
  require a plain-`git diff` fallback with different semantics. Revisit if asked.
- **Merge-commit patches** (first-parent or combined diff) — refused, not
  supported.
- **`git format-patch` numbering / multi-commit ranges / cover letters** — single
  commit only (`-1`).
- **Configurable default directory** — fixed to the parent of the repo by request.
- **Bookmark/shelf-commit "export as patch"** — those already have "copy to temp
  dir" (file export); a patch variant is not requested.
