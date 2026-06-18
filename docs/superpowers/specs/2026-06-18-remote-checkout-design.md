# Remote-branch checkout (`c` / `s`) — design

Date: 2026-06-18
Status: brainstorm (decisions settled in the parent remotes-tab brainstorm); ready for review
Branch/worktree: `worktree-remote-checkout`
Parent: chunk 2 of `2026-06-18-remotes-tab-design.md`

## Summary

On the **Remotes** tab (shipped in chunk 1), add two actions on the selected
remote-tracking branch:

- **`c`** — *checkout, stay*: materialize the remote branch as a local tracking
  branch, without leaving the current branch.
- **`s`** — *checkout and switch*: same, then switch the working tree to it.

Both are **fast-forward-safe**: an existing local branch of the same name is
reused only when it can be fast-forwarded to the remote ref; a diverged local
branch is never clobbered. This is a new engine operation `SmartCheckout`,
acting on the already-fetched remote-tracking ref (no network).

## Goals

1. A `SmartCheckout` engine op with `Intent ∈ {CheckoutStay, CheckoutSwitch}`.
2. Window-scoped `c` / `s` on the Remotes tab wired through `domain.Execute`.
3. Footer + `.`-menu entries and help copy for both.

## Non-goals (this chunk)

- **No CLI command** (`gg checkout`) yet — engine + TUI only, to keep the chunk
  small. The op is frontend-agnostic, so a CLI frontend is a clean follow-up.
- **No network fetch.** Checkout operates on the remote-tracking ref as last
  fetched; refreshing it is the deferred fetch/prune chunk.
- **No commit-preview** (separate deferred chunk).
- **No worktree-jump** for a target already checked out elsewhere — see edge
  rules below; refuse safely instead.

## c / s semantics (settled in the parent brainstorm)

For a selected remote ref `origin/foo`, the target local name is `foo` (the ref
minus its remote prefix — `RemoteBranch.Branch`):

| State of local `foo` | `c` (stay) | `s` (switch) |
|---|---|---|
| **absent** | create local tracking branch `foo` → `origin/foo`; stay | create it, then switch to it |
| **exists, fast-forwardable** (local is an ancestor of `origin/foo`) | fast-forward `foo` to `origin/foo`; stay | fast-forward, then switch |
| **exists, already at `origin/foo`** | no-op success | switch only |
| **exists, diverged / ahead** (local is NOT an ancestor of `origin/foo`) | **refuse** — clear message, no force, no clobber | **refuse** |

"Fast-forwardable" = `git merge-base --is-ancestor <local> origin/foo` exits 0.
A branch *ahead* of the remote (local has commits the remote lacks) is therefore
**not** fast-forwardable and is refused — fast-forwarding would discard those
commits, which violates "reuse only if it can be fast-forwarded."

### Edge: target local branch is checked out (current branch or another worktree)

Git refuses to update a branch ref that is checked out in any worktree via the
fetch-refspec trick, so a fast-forward of a checked-out branch can't be done
this way. **Default for this chunk: refuse with a clear message** rather than
build the worktree-jump flow:

- target == current branch → refuse: *"foo is the current branch; use `p` to
  pull"* (FF of HEAD is a pull, out of scope here).
- target checked out in another worktree → refuse: *"foo is checked out in
  <path>"*.

(Reusing the existing switch-to-worktree jump modal for `s` is a possible
follow-up; flagged, not built.)

## Architecture by layer

### git — four new local-only verbs (`internal/git/mutate.go`, `sync.go`)

```go
// LocalBranchExists reports whether refs/heads/<name> exists.
//   git show-ref --verify --quiet refs/heads/<name>   (exit 0 = exists)
func (r *Repo) LocalBranchExists(ctx context.Context, name string) (bool, error)

// IsAncestor reports whether commit a is an ancestor of commit b (FF possible).
//   git merge-base --is-ancestor <a> <b>   (exit 0 = yes, exit 1 = no)
func (r *Repo) IsAncestor(ctx context.Context, a, b string) (bool, error)

// CreateTrackingBranch creates refs/heads/<name> at <upstream> with tracking,
// without switching.
//   git branch --track <name> <upstream>
func (r *Repo) CreateTrackingBranch(ctx context.Context, name, upstream string) error

// FastForwardToRef fast-forwards a NON-checked-out local branch to a local ref
// (e.g. a remote-tracking ref) without a checkout or network access. Fails if
// the update is not a fast-forward, or if <branch> is checked out anywhere.
//   git fetch --no-write-fetch-head . <source>:<branch>
func (r *Repo) FastForwardToRef(ctx context.Context, branch, source string) error
```

`LocalBranchExists` and `IsAncestor` return the boolean from the command's exit
code (exit 1 is *false*, not an error — mirror `CurrentBranch`'s exit-1
handling); any other non-zero exit is a real error.

### engine — `SmartCheckout` (`internal/engine/smart_checkout.go`)

```go
type CheckoutIntent int
const (
    CheckoutStay   CheckoutIntent = iota // c
    CheckoutSwitch                        // s
)

// SmartCheckout materializes a remote-tracking branch as a local branch,
// fast-forward-safe, optionally switching to it. RemoteRef is the full short
// ref ("origin/foo"); Local is the target local name ("foo").
type SmartCheckout struct {
    RemoteRef string
    Local     string
    Intent    CheckoutIntent
}
```

`Run` algorithm (emits `Progress`; no `Decider` needed — the only fork is a
refusal, surfaced as an error):

1. `exists ← deps.Repo.LocalBranchExists(ctx, op.Local)`.
2. **absent**: `emit Progress{Step:"creating tracking branch", Detail: op.Local}`;
   `CreateTrackingBranch(op.Local, op.RemoteRef)`.
3. **exists**:
   a. `cur ← CurrentBranch`. If `cur == op.Local` → refuse (current-branch rule).
   b. `wt ← WorktreeForBranch(op.Local)`. If non-nil and not the current
      worktree → refuse (checked-out-elsewhere rule).
   c. `ff ← IsAncestor(op.Local, op.RemoteRef)`. If `!ff` → refuse (diverged).
   d. If `ff` and local already equals the ref, `FastForwardToRef` is a safe
      no-op; otherwise `FastForwardToRef(op.Local, op.RemoteRef)`.
4. **switch** (only when `Intent == CheckoutSwitch` and `cur != op.Local`):
   delegate to the autostash switch path — reuse `SmartSwitch{Branch: op.Local}`
   by running it inline (`SmartSwitch{op.Local}.Run(ctx, deps)`), so stash/restore
   and the stash-pop-conflict `DecisionNeeded` behave identically to a normal
   switch. Thread its Result/error through.
5. Return `Result{Summary: …, Changed: true}` — summary names the action taken
   ("checked out origin/foo as foo" / "switched to foo").

Refusals return `(Result{}, fmt.Errorf(...))` with a user-facing message; the
frontend shows it in the status line (no partial state — nothing is mutated
before the refusal checks pass for the exists branch; for the absent branch the
create is the first and only mutation).

Add `SmartCheckout`'s four verbs to the `GitOps` interface
(`internal/engine/gitops.go`) and the compile-time `_ GitOps = (*git.Repo)(nil)`
proof keeps `*git.Repo` honest.

### domain

No new query needed — `Execute` already runs any `Operation`. `SmartCheckout`
runs under the default exclusive lock (it's a ref-write + possible
tree-changing switch); it does not declare a reduced `LockMode`.

### tui (`internal/tui/model.go`, `footer.go`, `help.go`)

- A `selectedRemote() (model.RemoteBranch, bool)` helper (mirrors
  `selectedBranch()`), reading the `panelRemotes` selection via the existing
  `backingIndex`/`panelView` machinery.
- **`c` handler**: in the existing `case "c":`, branch on focus — when
  `m.focus == panelRemotes` and a remote is selected and ops are idle, start
  `SmartCheckout{RemoteRef: rb.Name, Local: rb.Branch, Intent: CheckoutStay}`;
  otherwise fall through to the existing commit behavior.
- **`s` handler**: in the existing `case "s":`, add a `panelRemotes` branch
  (alongside the Files-stash and Branches-switch branches) starting
  `SmartCheckout{… Intent: CheckoutSwitch}`.
- Both via `m.startOp(...)` (the existing op-runner), so events/modal/reload flow
  unchanged. A successful op reloads the snapshot, so the new local branch
  appears on the Branches tab and (for `s`) HEAD moves.
- **footer**: two `scopeRow` entries gated on `m.focus == panelRemotes` and a new
  `canCheckoutRemote()` predicate (a remote row is selected and ops idle):
  `{"checkout-remote", "c", "[c]heckout", …}` and
  `{"switch-remote", "s", "[s]witch", …}`. The `.` menu picks them up via scope.
- **help**: under the new "Remotes panel" section, document `c` and `s`.

## Testing (TDD)

- **git verbs** (`mutate_test.go` / `sync_test.go`, real-git `newTestRepo`):
  - `LocalBranchExists` true for `main`, false for a bogus name.
  - `IsAncestor`: HEAD~1 is an ancestor of HEAD (true); a sibling commit is not.
  - `CreateTrackingBranch` creates the ref at the upstream and sets `upstream`
    (assert via `for-each-ref %(upstream:short)`).
  - `FastForwardToRef`: fast-forwards a behind branch to a ref; errors on a
    diverged branch; errors when the branch is checked out.
- **engine** (`smart_checkout_test.go`, real-git repo with a fabricated
  `refs/remotes/origin/foo`):
  - absent local + `CheckoutStay` → local `foo` created at the ref, HEAD
    unchanged.
  - absent local + `CheckoutSwitch` → `foo` created and checked out.
  - existing behind local + stay → fast-forwarded.
  - diverged local → refuses with a non-nil error, local ref unchanged.
  - current-branch target → refuses.
- **tui** (`model`/`avail` tests): `c`/`s` on `panelRemotes` start
  `SmartCheckout` with the right `Intent`/`Local`/`RemoteRef`; `c` on
  `panelStaged`/Files still commits; `canCheckoutRemote` gating.

## Risks / watch-items

- **`git branch --track <name> <origin/foo>`** sets the upstream to the remote
  ref — confirm `%(upstream:short)` reads back `origin/foo` in the verb test.
- **`FastForwardToRef` source resolution**: `git fetch . origin/foo:foo` must
  resolve `origin/foo` to `refs/remotes/origin/foo`; verify in the verb test
  using a fabricated remote-tracking ref.
- **No partial state on refusal**: the exists-branch refusals all run before any
  mutation; keep that ordering when implementing.
- **Test-file naming**: avoid `_GOOS`/`_GOARCH` tokens before `_test.go`.

## Slicing for the plan

1. git verbs + tests (`LocalBranchExists`, `IsAncestor`, `CreateTrackingBranch`,
   `FastForwardToRef`) + add to `GitOps`.
2. `SmartCheckout` engine op + tests.
3. tui wiring (`selectedRemote`, `c`/`s` handlers, footer, help) + tests.
4. docs (CHANGELOG, README).
