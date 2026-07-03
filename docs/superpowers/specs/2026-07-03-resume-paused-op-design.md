# Resume a paused rebase/merge after external conflict resolution

**Date:** 2026-07-03
**Status:** Approved design, pre-plan

## Problem

When a rebase (or merge/cherry-pick/revert) pauses on conflicts and the user
resolves them **outside gg** (editor, CLI), gg goes blind: `domain.Conflict`
early-returns the zero `ConflictState` whenever `st.Counts().Conflicted == 0`
(`internal/domain/conflict.go:43,64`), so after `r` the repo looks clean while
git is still mid-rebase on a detached HEAD. The status-bar conflict notice is
gated on `len(m.status.Conflicts()) > 0` (`view.go`) and so is the `x` entry
key (`model.go`), so there is **no path back into the conflict process** — the
user must run `git rebase --continue` by hand. The only code that detects
"op in progress, zero conflicts" is `InProgressOp`, called solely from inside
an already-open `conflictProcess`.

Everything needed to *act* already exists: `engine.ContinueOp`/`AbortOp`
(generic merge→rebase→cherry-pick→revert dispatch), the conflict process's
`c`/`a` keys with `canContinue()` = zero files + op in progress. The missing
piece is **detection from a cold/refreshed state** and a surface that asks.

## Goals

- After any status refresh (manual `r`, background `[refresh] status` lane,
  file-watcher event, post-op reload, or cold startup), gg detects a paused
  sequencer op whose conflicts are all resolved and **asks once**:
  continue or abort.
- A persistent, non-modal indicator while any op is paused, plus a working
  `x` entry point, so declining the prompt never traps the user.
- Detection is repo-state-driven (works for rebases started outside gg and
  survives gg restarts) and costs ~nothing on a clean repo.

## Non-goals

- No new polling/background machinery — detection rides existing status
  arrivals. If auto-refresh is off, `r` is the trigger.
- No `rebase --skip` option. A continue that fails ("nothing to commit" after
  an emptying resolution) surfaces as a normal op error, same as the existing
  conflict process's `c`.
- No CLI changes (`git rebase --continue` already serves CLI users).
- No modeling of `git am` (a `rebase-apply` dir *without* the `rebasing`
  marker file is ignored).

## Design

### 1. Detection — stat-level paused-op probe (internal/git + domain)

New pure helper in `internal/git`, e.g. `PausedOpIn(gitDir string) string`
(no Runner, no ctx — pure `os.Stat`), probing this worktree's git dir in the
established precedence order (rebase before cherry-pick, since a paused rebase
pick also sets `CHERRY_PICK_HEAD`; mirrors `conflictState`):

| check | result |
|---|---|
| `MERGE_HEAD` exists | `"merge"` |
| `rebase-merge/` dir exists | `"rebase"` |
| `rebase-apply/` dir + `rebase-apply/rebasing` file | `"rebase"` |
| `CHERRY_PICK_HEAD` exists | `"cherry-pick"` |
| `REVERT_HEAD` exists | `"revert"` |
| none | `""` |

The git dir comes from the existing `GitDir` verb
(`internal/git/worktree.go:75`, one `git rev-parse --absolute-git-dir`),
resolved **lazily once per `domain.Service`** and cached (a repo's git dir
never moves during a session; `reRoot` builds a fresh service). The first
resolution runs under the usual `query()` Read reservation; every later probe
is gate-free file stats — the "clean status pays nothing" contract of
`Conflict` is preserved (zero subprocesses on the steady-state clean path).

`domain.Conflict(ctx, st)` / `conflictState` change behavior:

- `Conflicted > 0`: exactly as today (git probes for attribution under the
  gate).
- `Conflicted == 0`: run the stat probe. If it reports an op, return a
  `ConflictState` with that `Op` and best-effort attribution — for rebase,
  `head-name`/`onto` via the existing `RebaseParties` (rare path, runs under
  the gate); for merge, `MergeHeadName`; cherry-pick/revert via their
  `*HeadSummary` verbs. If the probe reports nothing: zero value, as today.
- `ConflictState` keeps its shape (`Op`/`Source`/`Target`); its doc comment
  updates to "Op != \"\" means a sequencer op is in progress, with or without
  unresolved conflicts". The TUI distinguishes the two cases via
  `len(m.status.Conflicts())`, which it already has.

`loadSnapshot`'s call into `conflictState` picks up the same behavior for
free (it already holds a reservation; the stat probe takes none — the
no-nested-reservation rule from the gate-deadlock fix still holds).

### 2. Ask once — resume prompt (internal/tui)

On every `srcStatus` arrival (both the `dataAvailableMsg` path and the
snapshot path, which already set `m.conflict`), evaluate:

```
paused-resolved := m.conflict.Op != "" && len(m.status.Conflicts()) == 0
```

If `paused-resolved` **and** gg is idle (`opsIdle()`) **and** no layer/modal/
process is open **and** the prompt has not fired for this pause instance →
push a confirm-style popup (same family as the slow-op confirm /
related-option prompts):

> **Rebase paused — all conflicts resolved.**
> rebasing feat/x onto main
>
> `[c] Continue rebase   [a] Abort rebase   [esc] Not now`

- **Continue** → `startOp` with `engine.ContinueOp{}` (exists).
- **Abort** → `startOp` with `engine.AbortOp{}` (exists). The prompt itself
  is the confirmation; no second confirm.
- **Not now / esc** → close; the indicator (below) remains the way back in.

If continue hits the next conflicted commit, the following status reload
shows conflicts as today (normal conflict flow); no special handling.

**One-shot flag** (`resumePromptShown bool` on `Model`): set only when the
popup is actually shown (a busy/blocked skip leaves it unset so the next
refresh retries). Reset whenever the observed state is *not* paused-resolved
— op finished or aborted (`Op == ""`), conflicts reappeared
(`Conflicts() > 0`), or `reRoot`. A second external pause therefore prompts
again, correctly.

### 3. Never trap — indicator + `x` re-entry (internal/tui)

- **Status-bar segment**: while `m.conflict.Op != ""` and there are zero
  conflicted files, show `⏸ rebase paused — x resume` (op name from
  `m.conflict.Op`; existing red conflict notice unchanged when conflicts
  exist). Both states also advertise via the footer's context bindings and
  `help.go` per the advertise-in-help-and-footer convention.
- **`x` gate relaxed**: `model.go`'s `x` handler and `startConflictProcess`
  currently require `len(Conflicts()) > 0`; relax both to
  `len(Conflicts()) > 0 || m.conflict.Op != ""`. The conflict process already
  renders the zero-file state correctly (`canContinue()`; `refreshed` +
  `loadInProgressCmd` populate `inProgress`), and already auto-releases the
  slot when both files and op are gone.

### 4. Engine / CLI

No changes. `ContinueOp`, `AbortOp`, and all git verbs
(`RebaseContinue`, `RebaseAbort`, `MergeContinue`, …) exist.

## Error handling

- Git-dir resolution failure (first probe): treated as "no paused op" for
  that round; recorded through the normal `query()` failure seam; next status
  refresh retries.
- Stat errors are indistinguishable from absence (`os.Stat` != nil → not
  present) — safe default is "no op", never a false prompt.
- `ContinueOp` failure (e.g. empty patch wanting `--skip`): standard op-error
  surface; the repo stays paused, the indicator stays, `x` still works, and
  the reset rule re-arms the prompt only after the state actually changes.

## Testing

- **internal/git**: table test for `PausedOpIn` over a temp dir with each
  marker combination (incl. rebase-apply with/without `rebasing`, precedence
  rebase > cherry-pick). Real-repo test: start a conflicting rebase in a
  `t.TempDir()` repo, resolve + `git add`, assert probe reports `"rebase"`.
- **internal/domain**: `Conflict` with a zero-conflict status returns
  `Op == "rebase"` mid-paused-rebase and the zero value on a clean repo;
  git-dir caching (FakeRunner: `rev-parse --absolute-git-dir` invoked once
  across repeated calls).
- **internal/tui** (model tests): status arrival in paused-resolved state
  pushes the prompt once; second arrival doesn't re-prompt; prompt suppressed
  while a layer is open and retried next arrival; `c`/`a` dispatch
  `ContinueOp`/`AbortOp`; esc leaves indicator; flag resets when `Op` clears
  and re-arms on a new pause; `x` opens the conflict process with zero
  conflicted files; footer/help coverage guard stays green.
- **e2e**: none needed (TUI-only surface; existing conflict scenarios
  unaffected).

## Docs wrap-up (per project convention)

`CHANGELOG.md` entry; `README.md` key/behavior note for the new prompt,
segment, and relaxed `x`; `CLAUDE.md` package-map lines for
`domain.Conflict`'s new contract and the TUI resume prompt. No CLI change →
no agent-skill bump.
