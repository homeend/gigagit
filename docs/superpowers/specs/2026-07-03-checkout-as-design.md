# Remotes: check out a remote branch under a different local name

**Date:** 2026-07-03
**Status:** approved

## Problem

`c`/`s` on the Remotes panel materialize `origin/foo` as local `foo`
(`engine.SmartCheckout`), fast-forwarding an existing local `foo` when
possible. When the local branch has diverged, the op refuses with
`foo has diverged from origin/foo; cannot fast-forward` — a dead end. The
user's likely intent ("give me the remote state anyway") requires a local
name the engine already supports (`SmartCheckout.Local` is a free field)
but no frontend exposes. Two gaps:

1. On the diverged refusal, offer to check the branch out under a
   different name instead of just failing.
2. An explicit "check out as…" action to pick the local name upfront.

The engine's Decider contract is option-lists only (no free text
mid-flight), so name entry must happen in the TUI *after* the op reports
divergence, then re-dispatch.

## Design

### Engine: typed divergence error

`smart_checkout.go` replaces the `fmt.Errorf` divergence refusal with

```go
type CheckoutDivergedError struct{ Local, RemoteRef string }
```

whose `Error()` renders the identical message. No behavior change; all
refusals still run before any mutation. Callers detect it with
`errors.As`.

### TUI: "check out as" popup

New `checkout_as_popup.go` mirroring `commit_name_popup.go`: a one-line
`textfield` titled `Check out <remote>/<branch> as:`, pre-filled by the
caller. Enter (trimmed, non-empty) dispatches
`SmartCheckout{RemoteRef, Local: <typed name>, Intent}` directly — the
popup's enter *is* the confirmation, no second confirm modal. Esc cancels
via `popLayer`. An invalid name (git ref syntax) fails in the op with
git's own error; the popup does not pre-validate.

### TUI: two `.`-menu rows

The Remotes panel `.` action menu gains two rows, both gated
`canCheckoutRemote` (same gate as `c`/`s`):

- **"Check out as…"** → popup with `Intent: CheckoutStay`
- **"Switch to as…"** → popup with `Intent: CheckoutSwitch`

Both pre-fill the remote branch's own name (`rb.Branch`); the user edits
it. If they keep a name that fast-forwards cleanly, that just works.

### TUI: divergence recovery prompt

Every TUI `SmartCheckout` dispatch (`c`, `s`, popup enter) stashes
`pendingCheckoutAs{remoteRef, branch string; intent CheckoutIntent}` on
the Model. On `opFinishedMsg` the pending value is captured-and-cleared
unconditionally (the `pendingPushTags` pattern; ops never overlap, so the
next `opFinishedMsg` is the checkout's). When the captured value is set
and `errors.As` yields a `CheckoutDivergedError`, a modal offers:

- **check out as different name…** → opens the popup pre-filled with the
  first free `<branch>-2`, `<branch>-3`, … (scanning local branches), so
  enter never re-collides. Re-dispatch keeps the captured intent.
- **cancel**

`reRoot` also clears `pendingCheckoutAs` (stale-pending rule). If the
typed name itself diverges, the loop simply re-prompts — consistent, no
trap. Other checkout refusals (current branch, checked out in another
worktree) keep today's plain-error behavior.

### CLI: `--as <local>`

`gg checkout [-s|--switch] <remote>/<branch> [--as <local>]` — `--as`
(and `--as=<local>`) sets `SmartCheckout.Local`; default stays the
branch part of the ref. On a `CheckoutDivergedError` without `--as`,
`cmdCheckout` appends a hint:
`retry with --as <name> to check it out under a different local name`.
Non-interactive behavior otherwise unchanged (fail loud).

### Tests

- **Engine** (`smart_checkout_test.go`): custom `Local` creates a
  tracking branch under that name; the diverged refusal is a
  `CheckoutDivergedError` carrying `Local`/`RemoteRef` and the exact
  legacy message.
- **TUI**: menu rows present and gated; popup enter dispatches with the
  typed name and the row's intent; esc cancels; diverged `opFinishedMsg`
  opens the recovery modal and "check out as different name…" opens the
  popup with a non-colliding suggestion; suggestion helper skips taken
  names; `reRoot` clears the pending state.
- **e2e**: scenario with a diverged local branch — `gg checkout
  origin/main` fails with the hint; `gg checkout origin/main --as main-2`
  succeeds and the new branch tracks `origin/main`.

### Docs

CHANGELOG bullet; README Remotes section (`.`-menu rows) + `gg checkout`
flag; `internal/agentskill/using-gg.md` + `agentskill.Version` bump
(CLI surface changed); CLAUDE.md package-map lines for the typed error
and the CLI flag.

## Out of scope

- No "reset local to remote tip" option in the recovery prompt — that
  action already exists as the Remotes hard-reset (per user decision).
- No free-text decisions in the engine; the Decider contract is
  untouched.
- No change to the other SmartCheckout refusals (current branch,
  worktree-occupied) or to SmartPull's divergence handling.
