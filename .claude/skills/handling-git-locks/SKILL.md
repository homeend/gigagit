---
name: handling-git-locks
description: Use when touching anything that cancels, kills, or times out a git subprocess in gigagit, when triaging an "Another git process seems to be running in this repository" / stale .git/index.lock report, or when adding a code path that removes files under .git. Covers how git's lockfiles work, why cancellation strands them, and the two-layer fix gg ships.
---

# Handling git lockfiles

## Why this exists

A user reported: in a big repo, pressing keys (space to stage, `c` to commit)
while a background fetch was running produced "another operation is already
running" plus a git lock that *never went away* — they had to `rm` it by hand.

That was gg's bug, and the shape of it generalizes: **any code that cancels a
git subprocess can corrupt the repository's usability**, and it will look like
an unrelated failure much later.

## How git lockfiles work

Git never edits a file in place. To rewrite `X` it creates `X.lock`, writes the
new content there, then renames it over `X`. The lock IS the mutual exclusion:
if `X.lock` exists, another git holds it, so a second git refuses to start.

The lock is released by:

1. **Normal exit** — via `atexit`.
2. **A trappable signal** — git's `sigchain` handler (`lockfile.c`) removes
   every lockfile it holds, then re-raises. Covers SIGINT, SIGTERM, SIGHUP,
   SIGQUIT, SIGPIPE.

It is NOT released by **SIGKILL**, a power loss, or a hard crash — nothing runs.
The lock then outlives its process forever, and every later git prints:

```
fatal: Unable to create '….git/index.lock': File exists.

Another git process seems to be running in this repository, e.g.
an editor opened by 'git commit'. …
may have crashed in this repository earlier:
remove the file manually to continue.
```

Note git's own advice is *"remove the file manually"* — which is exactly the
user experience gg exists to remove.

### Which locks matter

| Lock | Created by | Lives in |
|---|---|---|
| `index.lock` | add, commit, **status** (index writeback), checkout, stash | worktree git dir |
| `HEAD.lock` | any HEAD move: checkout, reset, commit | worktree git dir |
| `FETCH_HEAD.lock` | fetch | common dir |
| `ORIG_HEAD.lock` | merge, rebase, reset | common dir |
| `shallow.lock` | fetch in a shallow clone | common dir |
| `packed-refs.lock` | ref packing, fetch, gc | common dir |
| `config.lock` | config writes | common dir |

`index.lock` is the one users hit, and **`git status` takes it too** — it
rewrites the index to refresh the stat cache. That surprises people: a "read"
holds a write lock. On a 20 GB head that writeback is seconds long, which is
why the failure was frequent in a monorepo and never reproduced in a toy repo.

The list above is `lockCandidates` in `internal/git/lockfile.go`. It is a fixed
stat-level probe on purpose — walking `refs/` for `*.lock` is unbounded work on
a repo with hundreds of thousands of refs, and this runs on every repo load.

## Why gg was stranding locks

`internal/gitexec` runs every git through `exec.CommandContext`. Go's **default
cancel action is `Process.Kill()` — SIGKILL**, which git cannot trap.

And gg cancels git constantly. The hot path is `tui.startOp` (`internal/tui/op.go`):

```go
if m.bgCancel != nil {
    m.bgCancel() // preempt in-flight background reads so the user's op gets the slot
```

Every user keypress that starts an operation kills whatever background read is
mid-flight. If that was `git status` inside its index writeback — likely in a
big repo — the lock was stranded, and the user's *next* action failed.

Other cancel sites with the same exposure: `run.go` (quitting mid-op),
`conflict_process.go` (esc during a job). `domain/commitfeed.go` cancels
`git log`, which takes no lock — harmless.

## The fix gg ships (two layers, both needed)

### Layer 1 — cancel gracefully (`internal/gitexec`)

`ExecRunner.prepare` sets `cmd.Cancel` so cancellation sends **SIGTERM**, and
git's own handler removes its locks:

```go
cmd.Cancel = func() error { return terminate(cmd.Process) }
```

`terminate` is platform-split (`signal_unix.go` / `signal_windows.go`).
`cmd.WaitDelay` (2s) bounds it: a git that ignores or wedges on SIGTERM is
still hard-killed, so a cancelled read can never hold the repo gate forever.

**Windows has no SIGTERM.** `os.Process.Signal` rejects everything but Kill,
so `terminate` there IS a kill and locks CAN still leak. That is not a gap in
the fix — it is why layer 2 exists.

### Layer 2 — detect and offer recovery

- `git.LockFiles(dirs…)` — stat probe; returns `[]model.GitLock` (path, name,
  **mod time**). Pass BOTH the worktree git dir and the common dir: `index.lock`
  is per-worktree, `packed-refs.lock` is shared.
- `git.IsLockError(err)` — classifies git's failure message (re-exported as
  `domain.IsLockError`, since tui/cli may not import `internal/git`).
- `engine.RemoveGitLocks{Paths}` — the removal, as a real op.
- TUI: the `stale_git_lock` notice (`internal/tui/notify.go`) — offered on repo
  load AND reactively when an op fails with a lock error.
- CLI: `gg unlock [--yes]`.

## Rules for anyone touching this

1. **Never SIGKILL git as the first action.** If you add a new process runner or
   a timeout, terminate gracefully and escalate. Copy `ExecRunner.prepare`.
2. **Never decide a lock is stale on gg's behalf.** gg cannot see git processes
   it did not start (a terminal, an IDE, a hook). A live git holding a lock is
   doing real work and deleting it corrupts that write. Show the age; let the
   human choose. The notice text says this explicitly — keep it.
3. **Removal goes through `engine.RemoveGitLocks`, never an ad-hoc `os.Remove`.**
   The op takes the default **TreeWrite** (fully exclusive) reservation, which
   is what guarantees no *gg* operation is running git concurrently.
4. **Keep the path guard.** `checkLockPath` requires an absolute path, a known
   lockfile name (`git.IsLockFilePath`), and a location directly inside one of
   the repo's git dirs — the `DeleteBranchVersion` refuse-outside-the-namespace
   precedent. Without it, a frontend bug becomes an arbitrary file delete. The
   batch is validated in full **before** the first removal.
5. **A missing lock at removal time is success, not an error** — whoever held it
   finished and cleaned up. That is the desired state.
6. **The notice re-arms on a new failure.** Every other notice is an advisory
   where "Not now" holds until the next load. This one is a blocker: a session
   dismissal would leave the user with an unfixable error and no path out, so
   `maybeStaleLockNotice` clears the session dismissal on each fresh lock
   failure (never trap the user).

## Triage: "gg says another git process is running"

1. `gg unlock` — lists what is present, with ages. Removes nothing.
2. Is a real git running? `ps aux | grep git`, check IDEs and hooks. An age of
   seconds on a busy repo is probably live; hours is not.
3. If nothing is running: `gg unlock --yes`, or the `!` notice in the TUI.
4. If it keeps coming back, gg is killing git somewhere new. Look for a cancel
   site added without `cmd.Cancel`, and check whether you are on Windows (layer
   1 does not apply there — recurrence is expected after any cancelled op).

## Tests that must keep passing

- `internal/gitexec/cancel_signal_test.go` — cancellation delivers SIGTERM on
  both `Run` and `Stream`, and still escalates to kill if ignored. Uses a fake
  git script that traps TERM (ExecRunner takes `gitPath`, so this is easy).
- `internal/git/lock_cleanup_test.go` — **real git**: a cancelled `git add`
  leaves no `index.lock` and the next operation succeeds. Made deterministic
  with a slow `clean` filter (`git add` takes the lock *before* filtering), and
  it asserts the lock was actually observed so it can never pass vacuously.
- `internal/engine/remove_git_locks_test.go` — every guard branch.
- `internal/tui/notify_lock_test.go` — notice content, plural forms, the
  reactive arm, and the session-dismissal re-arm.

If you change the signal behavior, verify the test still FAILS with
`cmd.Cancel` reverted to `Process.Kill()`. It is easy to write a cancellation
test that passes either way.
