# Remotes: smart prompt when checking out the current branch's remote

**Date:** 2026-07-04
**Status:** approved

## Problem

`c`/`s` on the remote counterpart of the CURRENT branch (e.g. `origin/main`
while on `main`) dead-ends with the engine refusal
`main is the current branch; use pull to update it`. The hint is misleading
whenever the branch is not behind (e.g. ↑143 ↓0 — local already contains the
remote tip, a pull is a no-op), and the just-shipped checkout-as recovery
deliberately doesn't fire here (it keys on `CheckoutDivergedError` only).

Unlike divergence, this case is knowable at keypress time: `rb.Branch ==
m.status.Branch`, and `m.status.Upstream`/`Behind` are already loaded. So the
TUI can open a smart prompt *instead of dispatching a doomed op*.

## Design

### TUI: dispatch-time modal (no op run)

In the `c` and `s` Remotes key handlers, before arming `pendingCheckout` and
calling `confirmOp`: when `rb.Branch == m.status.Branch` (and the HEAD is
attached — `m.status.Branch` non-empty and not `"(detached)"`), open a
frontend `decisionState` modal (the `switch-to-worktree` pattern) instead of
dispatching. The pressed key's intent carries through (`c` → `CheckoutStay`,
`s` → `CheckoutSwitch`).

Modal ID `checkout-current-branch`; options are state-aware:

- **`pull now`** — included ONLY when `rb.Name == m.status.Upstream` AND
  `m.status.Behind > 0`. Resolving it dispatches `engine.SmartPull{}` via
  `startOp` (no extra confirm — choosing the option is the confirmation;
  SmartPull's own decision ladder handles merge/rebase/dirty trees). The
  upstream equality check prevents offering a pull that would fetch from a
  different remote than the one selected (e.g. `upstream/main` selected while
  `main` tracks `origin/main`).
- **`check out as different name…`** — always included. Opens the existing
  `checkoutAsPopup` via `openCheckoutAsPopup(rb.Name,
  suggestLocalName(m.branches, rb.Branch), intent)` — pre-filled with the
  first free `<branch>-2/-3…` suggestion, because the branch's own name is by
  definition taken by the current branch.
- **`cancel`** — always last (esc resolves to it via `abortOption`).

Prompt text (three forms — "already contains" is only claimed when provable):

- behind its upstream: `main is the current branch (behind origin/main by N).`
- its upstream, not behind: `main is the current branch and already contains
  origin/main (nothing to pull).`
- a non-upstream remote (containment unknown): `main is the current branch.`

The prompt builder + modal constructor live in `checkout_as_popup.go` beside
`checkoutDivergedModal` (same family of recovery UX); the key handlers only
gain the `rb.Branch == m.status.Branch` branch.

### Engine: typed current-branch refusal

`smart_checkout.go`'s `cur == op.Local` refusal becomes

```go
type CheckoutCurrentBranchError struct{ Local, RemoteRef string }
```

with a byte-identical message (`<local> is the current branch; use pull to
update it`), mirroring `CheckoutDivergedError`. No behavior change; the guard
remains the backstop when the TUI's status is stale (branch switched
externally between refresh and keypress) — that race surfaces today's plain
error, no modal (no `opFinishedMsg` hook for this error).

### CLI: same `--as` hint

`cmdCheckout` prints the existing hint line
(`hint: retry with --as <name> to check it out under a different local name`)
also when `errors.As` matches `CheckoutCurrentBranchError` and `--as` was not
given. One shared condition with the diverged case.

### Tests

- **Engine** (`smart_checkout_test.go`): the current-branch refusal is a
  `CheckoutCurrentBranchError` carrying `Local`/`RemoteRef` with the exact
  legacy message (extend the existing `TestSmartCheckoutCurrentBranchRefuses`).
- **TUI** (`checkout_as_popup_test.go`): `c` on the current branch's upstream
  with `Behind > 0` → modal contains `pull now`; resolving `pull now` returns
  a non-nil cmd (SmartPull dispatched); `Behind == 0` → no `pull now` option;
  same-name branch on a NON-upstream remote → no `pull now`; resolving
  `check out as different name…` opens the popup pre-filled with the
  suggestion and the pressed key's intent (`s` → `CheckoutSwitch`); `cancel`
  is inert; no `pendingCheckout` is armed by the modal path (nothing was
  dispatched).
- **e2e**: existing current-branch scenarios keep exit 1 (the hint line stays
  uncovered — the harness has no stderr matcher; accepted, same as the
  diverged hint).

### Docs

CHANGELOG bullet (Unreleased → Added); README Remotes clause; help.go `c` row
gains the current-branch prompt mention; CLAUDE.md engine row notes the second
typed error. No agentskill change (CLI surface/flags unchanged — the hint is
an error-path nicety) — no Version bump.

## Out of scope

- No reset-to-remote option in this modal (exists as the Remotes `.`-menu
  hard reset).
- No `opFinishedMsg` recovery hook for `CheckoutCurrentBranchError` (the
  dispatch-time check makes it near-unreachable from the TUI).
- No engine Decider changes; no change to the diverged recovery flow.
