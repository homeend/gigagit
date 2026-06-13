# Startup timing report — why `gg` takes seconds to load

Date: 2026-06-13 · Method: `gg --time-track` span log (one JSON span per git
subprocess), TUI launched under a pty, repeated runs (warm cache). Binary at
repo tip `798ddaa`, git on WSL2.

## Numbers

Startup runs `loadCmd()` (`internal/tui/load.go`): **7 git subprocesses,
strictly sequential**. Measured per-span durations:

| span | gg repo (`/mnt/t`) | lazygit repo (`/mnt/t`) | lazygit clone (ext4 `~`) |
|---|---:|---:|---:|
| `git status --porcelain=v2 -z --branch -uall` | **1 046–1 093 ms** | **2 764 ms** | 91 ms |
| `git for-each-ref` (branches) | **767–797 ms** | 134 ms | 3 ms |
| `git log -n 50` (commits) | 171–175 ms | 77 ms | 7 ms |
| `git worktree list --porcelain` | 93–96 ms | 25 ms | 3 ms |
| `git log --no-walk` (worktree HEAD times) | 78–95 ms | 43 ms | 4 ms |
| `git rev-parse` ×2 (toplevel, common-dir) | ~27 ms | ~27 ms | ~6 ms |
| **Total git time per startup** | **≈ 2.3 s** | **≈ 3.1 s** | **≈ 0.11 s** |

Identical binary, identical git, identical repo content: ext4 is **~28×
faster** end to end.

## Root cause: per-stat latency of the Windows drive mount (drvfs/9p)

Both repos live on `/mnt/t`, a Windows drive mounted into WSL2. Every file
metadata operation crosses the 9p protocol boundary at roughly 1 ms each,
and git's working style is "stat everything":

- **`git status` dominates** because it refreshes the index by `lstat`-ing
  every tracked file (lazygit: 2 187 files ≈ 2 s of stats). The untracked
  scan is NOT the problem: `-uall` 2.56 s, `-unormal` 2.58 s, even `-uno`
  still 1.95 s.
- **Any repo-touching git call pays a fixed tax**: bare
  `git rev-parse --show-toplevel` costs ~230 ms cold on `/mnt/t` while
  `git version` (no repo I/O) is 0 ms — so process spawn is not the issue,
  repo discovery/lock I/O is.
- **`for-each-ref` is slow on the gg repo specifically** (0.77 s vs 0.13 s
  in lazygit): `%(committerdate:unix)` must open each branch tip commit and
  the gg repo has ~1 330 loose objects (140 unpushed commits) — loose-object
  reads over 9p add up. `git gc` would shrink this one.

Because `loadCmd` runs its 7 calls **back-to-back**, the latencies sum:
~2.3 s on gg, ~3.1 s on lazygit, matching the "few seconds" you see.

## What would actually make it faster

Ordered by payoff for gg-the-product:

1. **Parallelize `loadCmd`.** Status, branches, log, and worktree-list are
   independent reads; running them concurrently drops total wall time to
   max(spans) ≈ the status call alone (gg: 2.3 s → ~1.1 s, lazygit:
   3.1 s → ~2.8 s). Cheap, pure Go change.
2. **Progressive paint.** Deliver each panel's data as its own message
   instead of one `dataLoadedMsg` snapshot: branches/commits/worktrees
   appear in 100–800 ms while the status panel finishes. On a 100 GB
   monorepo (gg's stated target) `git status` is *inherently* seconds, so
   the architecture should never gate the whole UI on it.
3. **Snapshot cache.** Persist the last load (e.g. under
   `<git-common-dir>/gg/`), paint it instantly on startup marked stale,
   refresh in the background — the lazygit/GitKraken trick. Biggest
   perceived win, more design work (staleness, invalidation).
4. **Environmental (user-side, not code):** keep working repos on the Linux
   filesystem (`~/...`) instead of `/mnt/*` — 28× on everything; `git gc`
   repos with many loose objects. git's builtin fsmonitor won't help here:
   it relies on inotify, which drvfs doesn't deliver.

Non-finding: loading 50 commits costs 77–175 ms — commit loading is **not**
a startup bottleneck (relevant to the separate on-demand-commits feature,
which is justified by gigantic-repo memory/latency, not by startup time).
