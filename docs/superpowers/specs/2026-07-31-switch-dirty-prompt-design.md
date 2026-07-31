# Dirty-tree branch-switch prompt — design

**Date:** 2026-07-31
**Status:** approved (conversational brainstorm)
**Branch:** `feat/switch-dirty-prompt`

## Problem

The Branches-panel switch action (`s`) on a branch not checked out elsewhere
confirms "Switch to X?" and dispatches `engine.SmartSwitch`, which silently
autostash-switch-pops. The confirm never says uncommitted changes will be
carried, and the stash pop can conflict (real case: a worktree created by
"worktree from commit / changes unstaged" mode, switched to another branch —
instant surprise conflict). For a worktree-first client the natural answer to
"I want branch X but my tree is dirty" is often *a worktree for X*, leaving
the dirty tree untouched.

## Behavior

When the user triggers the Branches-panel switch on a local branch and the
working tree is **dirty** (per the same signal SmartSwitch stashes on:
staged + unstaged + conflicted > 0 — untracked files don't count), gg shows a
pre-dispatch modal instead of the plain confirm (this modal IS the
confirmation — it must appear even when `confirm_slow_ops` is off):

```
You have uncommitted changes. Switch to <branch>?
  worktree          — open the create-worktree popup for <branch>
                      (existing-branch mode, create & switch); dirty tree untouched
  carry changes     — today's behavior: SmartSwitch (autostash → switch → pop;
                      pop conflict handled by the existing conflict flow)
  cancel            — do nothing (esc)
```

Option order: worktree first (the worktree-first answer), carry second,
cancel last. `esc` cancels.

A **clean** tree keeps today's flow byte-identical (confirmOp → SmartSwitch).

Out of scope, unchanged:
- The "checked out in another worktree" case (already handled: go-to-worktree
  prompt, no checkout).
- The remote-branch `SmartCheckout` switch lane.
- Chained switches (`chainSwitch` after an op) and CLI `gg switch` — engine
  behavior is untouched; this is a TUI pre-empt only (the
  `checkoutCurrentBranchModal` precedent).
- Known edge: a tree dirty ONLY by EOL-reconciled changes may not trigger the
  prompt (TUI status hides them) yet still autostash — same as today.

## Implementation shape

`internal/tui/model.go`, the `canSwitchBranch()` branch of the `s` handler
(~line 1346): after the existing `worktreeForBranch` check, when
`m.status.Counts()` shows staged+unstaged+conflicted > 0, push a
`decisionState` modal (`ID: "switch-dirty"`, options
`["worktree", "carry changes", "cancel"]`) whose `onResolve`:
- `"worktree"` → `m.openWorktreePopup(true)` (existing-branch popup for the
  selected branch, create & switch semantics)
- `"carry changes"` → `m.startOp(engine.SmartSwitch{Branch: b.Name})`
- anything else → no-op.

Option values stay English protocol; labels render through
`optionDisplayName` (`i18n_display.go`) with keys in all four bundles; the
prompt string is a new `i18n` key. `options_vocab_test.go` enforces the
option/bundle wiring.

## Tests

- Dirty tree + `s` on a switchable branch → modal with the three options, in
  order; clean tree → no modal (confirm flow reached).
- `"worktree"` resolution opens the worktree popup for the selected branch;
  `"carry changes"` dispatches `SmartSwitch{Branch}`; esc/cancel leaves state
  unchanged.
- Branch-checked-out-elsewhere still takes the go-to-worktree modal (not this
  one), dirty or not.

## Docs

CHANGELOG (always), README only if it documents the switch behavior; CLAUDE.md
`tui` row one-liner. No CLI/engine surface change → no using-gg bump.
