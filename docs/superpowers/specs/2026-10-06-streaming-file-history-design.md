# Streaming file history (TUI `h`) — design

Date: 2026-10-06 · Branch: `feat/streaming-file-history`

## Problem

`h` on a file runs `git log --follow -M --name-status -n 200 -- <path>` as ONE
buffered call (`git.Repo.FileLog` → `domain.Service.FileLog` →
`loadHistoryListCmd`) and shows `(loading…)` until git exits. On the linux
repo a rarely-touched file is slow because git walks the whole history to
look for 200 hits:

| file | first commit on stdout | full walk |
|---|---|---|
| `drivers/net/ethernet/3com/3c509.c` (85 hits) | 0.06 s | 19 s |
| `COPYING` (4 hits) | 2.7 s | 7 s |
| `kernel/sched/core.c` (200 hits) | ~0 s | 1.1 s |

The user almost always wants the NEWEST changes, which git produces first.

## Goal

The history window fills in as git finds commits. The newest commit is
selected and its diff loads as soon as it arrives; the user can browse, open
the full diff (`enter`), use its `.` menu (Copy to working dir, …), blame,
open in the editor — all while older commits are still being found.

Out of scope: the web `/api/filelog` and the CLI (unchanged, still use the
buffered `FileLog`); a "load more past 200" action; any new restore action
(Copy to working dir from the diff's `.` menu already exists).

## Behaviour

- `h` opens the window at once (as today). Until the first commit arrives the
  list shows `(loading…)`.
- Each arriving batch is APPENDED to the list (git emits newest first, so
  existing rows never move and the selection never jumps).
- The first batch selects row 0 and loads its right-pane diff immediately.
- While the walk runs the header reads `history: <path> · loading… N found`;
  when it ends the suffix disappears. A finished walk with no commits shows
  `(no history)`.
- An error after some commits keeps them and shows the error on the header
  suffix (`· error: …`); an error before any commit shows it in the list as
  today.
- Leaving the window cancels the walk (the git process is terminated):
  - `esc`/`h` in the history window cancels at once;
  - every other way the view can disappear (a repo switch, `clearLayers`, …)
    is caught lazily: the next batch for a view that is no longer on the
    stack (nor parked) cancels the walk and is dropped.
- The 200-commit cap stays (`historyMaxCommits`).

## Repo gate

The streaming walk runs WITHOUT a repogate reservation (user ruling,
2026-10-06). `git log` reads immutable objects, resolves its start rev once,
and never takes `index.lock`, so a concurrent tree write cannot tear what it
reads. Holding a Read reservation for a 20 s walk would make Copy to working
dir (a TreeWrite op) wait for the whole walk, and every read queue behind
that waiting writer. The buffered `FileLog` keeps its reservation.

## Architecture

### git — `internal/git/file_log.go`

- An incremental parser `fileLogParser` with `line(string) (done
  *model.FileCommit)` + `flush() *model.FileCommit`: a format line opens a new
  commit (returning the previous one as complete), a tab line fills the open
  commit's status/paths. `ParseFileLog` is rewritten on top of it, so the
  buffered and streamed parses cannot drift.
- `func (r *Repo) FileLogStream(ctx, rev, path string, limit int, emit
  func(model.FileCommit)) error` — the same argv as `FileLog`, run through
  `r.Runner.Stream`, feeding each line to the parser and calling `emit` for
  each completed commit (plus the final `flush`). One invocation.

### domain — `internal/domain/query.go`

- `func (s *Service) FileLogStream(ctx, rev, path string, limit int, emit
  func(model.FileCommit)) error` — calls the git verb directly: no
  singleflight (a stream has no shared result), no reservation (see above).
  Failures are noted to observ like `query` does, except `context.Canceled`.

### TUI — `internal/tui/history_view.go`

- `historyView` gains `cancel context.CancelFunc`, `streaming bool`
  (walk still running) and `streamErr error` (error after partial results).
  `loading` keeps meaning "no commit yet".
- `loadHistoryListCmd(h *historyView) tea.Cmd` (callers pass the view, not a
  ctx+tag): creates a cancellable context stored on `h.cancel`, starts a
  goroutine running `svc.FileLogStream`, which pushes commits into a
  batcher; the batcher sends a `historyChunkMsg{view, commits, done, err}`
  on a channel when ≥20 commits are pending or 50 ms passed since the first
  pending one, and a final message with `done=true` (and the error) when the
  walk returns. The returned cmd waits for the NEXT message; the Update
  handler re-arms it (`waitHistoryChunk(ch)`) until `done`.
- `historyChunkMsg` handler: if the view is not live (`m.hasLayer(view)` —
  which already sees views parked under a console, `dispatchParkedAware` puts
  them back for non-key messages — or in the files view's parked
  `filesReturnLayers`) → cancel and drop (no re-arm). Else
  append commits; on the first non-empty batch set `sel=0`, `loading=false`
  and return `selectCmd`; on `done` clear `streaming`, record the error
  (list error if no commits, `streamErr` otherwise), call `cancel`.
- `esc`/`h` in `historyView.update` call `h.stop()` (cancel if set) before
  popping.
- `historyListMsg` is removed (its only producer was the buffered loader).
- Header: `history: %s` plus the translated suffix `· loading… %d found` /
  `· error: %s` — new i18n keys in all four bundles.

## Testing

- git: `ParseFileLog` keeps its existing tests; new test feeds a real
  repo with a rename through `FileLogStream` and asserts the same commits,
  order and A/M/R fields as `FileLog`; a cancelled ctx returns promptly.
- domain: `FileLogStream` emits the same commits as `FileLog` and does not
  hold the gate (a TreeWrite `Acquire` succeeds while a stream is blocked in
  `emit`).
- tui: a batch arriving selects row 0 and returns a diff load before `done`;
  a later batch appends without moving `sel`; the header shows the
  `loading… N found` suffix until `done`; `esc` cancels the context; a batch
  for a popped view cancels and is not re-armed.
- Manual: `./tui-capture.sh` (or the real binary) on
  `/home/homeend/others/linux` with `drivers/net/ethernet/3com/3c509.c` —
  first commit + diff visible within ~1 s; `enter` → `.` → Copy to working
  dir works mid-walk.

## Docs

CHANGELOG (always), README if the history section describes loading, help
text unchanged (no new keys), `docs/CLAUDE-details.md` history entry gets the
"streamed, ungated" note.
