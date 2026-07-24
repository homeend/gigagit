# TUI stash untracked-files fix — design

Date: 2026-07-25 · Branch: `fix/tui-stash-untracked` off `main` · Status: awaiting review

## Goal

A `-u` stash's untracked files appear in the TUI stash drill-in (`S` →
`l`), and their preview / diff / history resolve against the right commit —
closing the `^3` first-parent blind spot the web frontend already fixed.

## Background (verified, file:line)

- A `git stash push -u` stash stores untracked files in a THIRD parent
  (`<stash>^3`, a root commit), invisible to the stash commit's
  first-parent diff.
- Drill-in chain: `S` opens the stash list (`stash_view.go:174`); `l` →
  `loadStashFilesCmd(ref)` (`stash_view.go:44-59`): `svc.StashCommit(ctx,
  ref)` → `svc.CommitFiles(ctx, sha)` → `stashFilesMsg{tag, sha, lines:
  commitFileLines(files)}`. Handler (`model.go:2181-2192`) is tag-gated
  (`msg.tag != m.filesStashTag` drops stale results) and sets
  `m.filesHash = msg.sha`.
- The files view has ONE commit reference for the whole tree
  (`m.filesHash`); `contentLine` (the rendered row) has no sha field.
  Consumers that resolve a file's content by `m.filesHash`: diff open
  (`files_view.go:648`), file preview (`file_preview.go:78`),
  history/blame nav (`files_view.go:514,526`), plus stash-mode `.`-menu
  actions (copy-to-working-dir is the known one to check).
- `git.CommitFiles` already carries `--root`, so the `^3` root commit
  lists its files as `A` (`internal/git/log.go:121-130`; root-commit
  behavior covered by `TestCommitFilesRealRepo`). `StashCommit` is a plain
  `git rev-parse <ref>` and resolves `ref+"^3"` fine.
- Web reference: `internal/web/stashes.go` resolves `untracked_sha`
  best-effort; `app.js openStashDetail` concatenates the `^3` file list
  with a per-file sha override consumed as `f.sha || state.fileSha`.
- No test at ANY layer currently drives a `-u` stash's `^3` parent.

## Changes

### 1. Load-time merge (`loadStashFilesCmd`)

After the tracked list loads, additionally resolve `svc.StashCommit(ctx,
ref+"^3")`. On success, `svc.CommitFiles(ctx, usha)` and append those
files to the tree with a per-line sha of `usha`. A `^3` resolve failure
(plain, non-`-u` stash) skips silently; a `CommitFiles` failure degrades
to the tracked-only list (web parity — never an error).

### 2. Per-line sha override

`contentLine` gains `sha string` (empty = use the view's `m.filesHash`).
Tracked lines keep an empty sha; the merged untracked lines carry `usha`.
A small resolver (line sha wins over `m.filesHash`) is applied at the
consumer sites listed in Background — diff open, file preview,
history/blame nav, and any stash-mode `.`-menu action that reads content
by `m.filesHash` (the plan enumerates the exact call sites).

### 3. Presentation

Untracked files are appended after the tracked ones with their natural
`A` status — plain concatenation, matching the web. No new heading, no
new Model fields (the sha rides on `contentLine`, so `closeFilesView`'s
existing teardown already covers it).

## Out of scope

The web frontend (already fixed); the stash list panel's row display; the
apply/pop/drop popup; the shelf/bookmark surfaces.

## Testing

- git-level (`internal/git/log_test.go` area): a `-u` stash fixture;
  `StashCommit(ref+"^3")` resolves; `CommitFiles(usha)` lists the
  untracked file as `A` — the first test anywhere to drive `^3`.
- TUI-level (extend `internal/tui/stash_view_test.go`): on a `-u` stash,
  `S` → `l` shows the untracked file in the tree; opening its diff
  dispatches against the `^3` sha (assert `m.diffTag ==
  "commit:<usha>:<path>"`); a plain stash's tree is unchanged.

## Docs

CHANGELOG entry. CLAUDE.md: the `web` row's two "the same first-parent
blind spot exists in the TUI, unfixed" callouts must be updated to say the
TUI is fixed too.

## Merge target

Branch is cut from `main` (`9e4d7d89`). Per the standing rule this is NOT
web work: the user merges it into `main` (Claude never merges to main).
