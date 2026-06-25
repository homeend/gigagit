# Smart-push rejection recovery — design

Date: 2026-06-26
Status: approved (brainstorm), pending implementation plan
Depends on: the push-error message classifier (`friendlyPushError` / `friendlyOpError`
in `internal/tui`, branch `feat/push-error-messages`) — this feature shares its
non-fast-forward signature strings.

## Problem

A plain push that the remote rejects because it is ahead is a dead-end today.
The user sees an error in the status bar and must manually run pull/rebase or
reach for `--force`. The real-world log that motivated this showed exactly that:
a plain push rejected `(non-fast-forward)`, a `--force-with-lease` rejected
`(stale info)`, then a blind `--force` — three manual attempts, the last of
which silently overwrote unseen remote commits.

gg's thesis is GitKraken-style one-key smart operations (the worktree-aware
`SmartPull` decision tree is the hero feature). Push should fall out of the same
contract: one keypress, a guided recovery when the remote has moved.

## Goal

When a **plain** push is rejected for being behind the remote, the single push
action detects it and offers a guided recovery, reusing existing engine
machinery (the `Decider` contract, the `Pull` verb, the `rebase-conflict`
decision, and the existing `push-force` decision). No new keybinding.

## Non-goals

- No change to the **explicit** force-push entry (the Commits/Branches `.` menu
  `Push{Force:true}` → `push-force` decision). That stays as-is.
- No `--autostash` handling for a dirty working tree (v1 surfaces git's error).
- No auto-retry loop (one recovery attempt per push).
- CLI push-error *message* unification (routing the CLI through the shared
  classifier) is tracked separately, not in this spec.

## Behavior

Trigger: a push issued with `Force == false` returns an error classified as a
**non-fast-forward** rejection (`non-fast-forward` / `fetch first` / `tip of
your current branch is behind`). Any other failure (credentials, protected
branch / pre-receive hook, network, etc.) is returned unchanged — rebase cannot
fix those.

On that trigger, the op raises a new decision:

```
ID:      "push-rejected"
Prompt:  "Remote has new commits on <branch> — how do you want to push?"
Options: ["rebase", "force", "cancel"]
```

- **rebase** — run `Pull(remote, branch, PullRebase)` (replays your commits on
  top of the remote tip), then re-push (plain). After a clean rebase the branch
  is ahead of the remote, so the re-push fast-forwards. If the rebase
  **conflicts**, chain the existing `rebase-conflict` decision
  (`keep-conflicts` / `abort`):
  - `keep-conflicts` → leave the conflicted tree, return an error; the TUI
    conflict process picks it up exactly as it does for squash/drop/cherry-pick.
    The push does not happen; after the user resolves and the rebase completes,
    a fresh plain push fast-forwards.
  - `abort` → `rebase --abort`, return "push cancelled (rebase aborted)".
- **force** — chain the **existing** `push-force` decision
  (`force-with-lease` / `force` / `abort`) and push with the chosen mode. This
  keeps the history-overwriting path behind its current confirmation. Choosing
  `force-with-lease` here will itself be rejected `(stale info)` because
  `origin/<branch>` is stale (that is *why* we are in this flow) — the user then
  sees the now-friendly stale-info message steering them to fetch/rebase. This
  is accepted: reusing the standard force confirm avoids a special case and the
  outcome is honest.
- **cancel** (or empty / esc) — return "push cancelled", no remote change.

Bound: the recovery runs **once**. If the post-rebase re-push is itself rejected
non-fast-forward (the remote moved again during the rebase), return the friendly
error rather than re-entering the recovery decision.

## Architecture / where the code goes

### `engine.Push` (augment, no new op)

`Push.Run` already branches on `op.Force` to raise `push-force`. Add a
post-push branch on the non-force path:

```
if !op.Force {
    err := repo.Push(... PushNoForce)
    if err != nil && git.IsNonFastForward(err) {
        // raise "push-rejected"; act on rebase / force / cancel
    }
    return err  // any other error unchanged
}
```

`Push` declares no `LockMode`, so `domain.Execute` already holds it under
exclusive **`TreeWrite`** — the rebase needs no escalation. Operations never
block on a human: the recovery is expressed purely as `decide(...)` forks over
the option list, identical to `SmartPull`.

TUI `P` (`model.go`) and the plain `gg push` (`internal/cli`) already construct
`Push{Force:false}`, so both inherit the behavior with no call-site change.

### Rejection classifier in `internal/git`

Add an exported predicate, e.g.:

```
// IsNonFastForward reports whether err is a push rejected because the remote
// branch has commits the local tip lacks (the recoverable case).
func IsNonFastForward(err error) bool
```

It matches the same signature substrings the TUI's `friendlyPushError` uses.
Factor those signatures into one shared, unexported var (or small helper) so the
TUI message and the engine trigger cannot drift apart. The classifier inspects
the wrapped error string (git's stderr is already carried through the
`gitexec` wrapping); a typed sentinel is not required for v1 but the predicate
keeps the string-matching in one tested place.

### CLI (`gg push`)

Add a policy flag answered by `cliDecider` for the new fork:

```
--on-reject=rebase|force|abort   (default: abort)
```

- `abort` (default) → the fork resolves to `cancel`; the command exits non-zero
  with the friendly non-fast-forward message. Keeps scripts non-blocking.
- `rebase` / `force` → resolve the `push-rejected` fork accordingly.
- The nested `push-force` fork (when `--on-reject=force`) is answered by the
  **existing** `--force` / `--force-with-lease` flags.
- Interactive stdin may still prompt, mirroring the existing force behavior.

## Edge cases / limitations

- **Dirty working tree**: `pull --rebase` refuses; the op surfaces git's error
  (no autostash in v1).
- **Second rejection after rebase**: returns the friendly error, no loop.
- **Non-recoverable rejections** (hook/protected/credentials): never enter the
  recovery; returned unchanged (and already get their own friendly message).
- **Empty/already-applied rebase**: a no-op rebase still leaves the branch ahead
  or equal; the re-push either fast-forwards or reports "everything up-to-date".

## Testing

- **Engine** (`FakeRunner` + `MapDecider`), one test per branch:
  - clean rebase → re-push succeeds;
  - rebase conflict → `keep-conflicts` returns error + leaves tree;
  - rebase conflict → `abort` runs `rebase --abort`;
  - force → each of `force-with-lease` / `force` / `abort`;
  - cancel;
  - second rejection after rebase returns the error (no second decision);
  - a non-recoverable error never raises `push-rejected`.
  - One real-`git` test (two clones / the e2e git server) for the
    rebase-then-push happy path.
- **CLI**: `--on-reject` mapping (rebase/force/abort) + default-abort exit code
  and message.
- **Classifier**: `IsNonFastForward` over the non-fast-forward signatures and a
  negative case (hook/credential), sharing fixtures with the existing
  `friendlyPushError` tests.

## Docs to update on completion

- `CHANGELOG.md` (always).
- `README.md` (push behavior is now user-visible smart-op).
- `internal/agentskill/using-gg.md` + bump `agentskill.Version` (the `gg push`
  `--on-reject` flag is a CLI surface change), then `gg init --update`.
- `CLAUDE.md` engine row only if the op list framing changes (it does not — this
  augments `Push`).
