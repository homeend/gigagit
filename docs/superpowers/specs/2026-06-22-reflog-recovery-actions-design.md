# Reflog recovery actions — design

**Date:** 2026-06-22
**Status:** Approved (brainstorm)
**Feature:** Add reset / checkout / create-branch recovery actions to a reflog
entry's `.` menu — the deferred v1 scope of [[reflog-window-feature]], lazygit's
"rescue lost work" via the reflog.

## Goal

The reflog window (merged `9901a66`) is read-only. This adds the actions that
turn it into a recovery tool: from a reflog entry's `.` menu, **reset** the
current branch to that entry, or **check out** the entry (detached, or as a new
branch you switch to). All three work on dangling commits — the whole point of
reflog rescue.

## Scope (decided in brainstorm)

- Three recovery actions: **reset to entry**, **checkout detached**, **create
  branch at entry and switch to it**.
- All reuse existing, tested engine ops. No new operation logic.
- `.` menu only — no keybinds (lazygit's `space`); consistent with the rest of
  gg's reflog surface.

## Reused engine ops

| Op | Reuse |
|----|-------|
| `engine.Reset{Commit}` | As-is. Mid-flight `reset-mode` decision (soft/mixed/hard/cancel) + non-ancestor `reset-confirm`. The reflog SHA goes straight into `Commit`. The non-ancestor confirm is the **common** case here (rescue = reset off the current branch) and is correct. |
| `engine.Checkout{Ref, Branch}` | **Renamed** from `engine.CheckoutTag{Name, Branch}` (see Refactor). `Branch == ""` ⇒ `SwitchDetach(Ref)`; else `SwitchCreate(Branch, Ref)` (create + switch). Already commit-ish-agnostic. |

## Refactor: `CheckoutTag` → `Checkout`

`CheckoutTag` is already a generic commit-ish checkout (`git switch --detach
<ref>` / `git switch -c <branch> <ref>`); only its name is tag-specific. Rename
so tag-checkout and reflog-checkout share one op:

- `internal/engine/checkout_tag.go` → `internal/engine/checkout.go`: struct
  `CheckoutTag` → `Checkout`, field `Name` → `Ref`. Summary strings change from
  "checked out <tag>" to "checked out <ref>".
- Update the 3 construction sites + test:
  - `internal/cli/tag.go:63` — `engine.Checkout{Ref: rest[0], Branch: *branch}`
  - `internal/tui/tags_actions.go:35` — `engine.Checkout{Ref: name}`
  - `internal/tui/tag_checkout_popup.go:28` — `engine.Checkout{Ref: p.tag, Branch: p.name.Value()}`
  - `internal/engine/checkout_tag_test.go` → `checkout_test.go`, updated.
- **No behavior change for tags.** The `gg tag checkout` CLI command name is
  unaffected (only the internal struct changes).

## TUI wiring (reflog-anchored)

Two new rows, appended in `availableActions` alongside the existing reflog rows.
Each gates on `m.focus == panelReflog && m.opsIdle()` and reads the entry under
the cursor via `m.reflog[backingIndex(panelReflog)]` — **reflog-anchored
helpers, never the `commitX` helpers** (which gate on `panelCommits`). This is
the codebase's recurring display-vs-backing trap; the row tests anchor on a
non-zero cursor to prove it.

### `reflogResetRow` (`internal/tui/reflog_view.go`)

```
id: "reflog-reset", label: "Reset to this entry"
run: m.startOp(engine.Reset{Commit: e.Hash})
```

Mirrors `commitResetRow`, reflog-anchored.

### `reflogCheckoutRow` (`internal/tui/reflog_view.go`)

```
id: "reflog-checkout", label: "Check out this entry…"
run: opens a decision modal {Detached, Create branch…, Cancel}
```

Mirrors `tagCheckoutRow`'s flow:
- **Detached** → `m.startOp(engine.Checkout{Ref: e.Hash})`.
- **Create branch…** → `pushLayer(&reflogCheckoutPopup{ref: e.Hash, name: newTextField("")})`;
  on enter → `engine.Checkout{Ref: ref, Branch: name}` (create + switch). The
  popup mirrors `tagCheckoutPopup` (esc cancels, space dropped, `enter` with
  empty name is inert). New file `internal/tui/reflog_checkout_popup.go`.
- **Cancel** → no-op (never-trap).

The modal/popup are reflog-specific (separate ids) so nothing leaks onto the
Tags or Commits panels.

## Post-op behavior

Reset and checkout mutate the reflog itself (the action becomes the new
`HEAD@{0}`). The existing `opFinishedMsg` → `loadCmd` refresh reloads the reflog
(it rides the Snapshot). Build/eyeball check: after the op the cursor should not
land on a surprising row (the list grew by one at the top).

## Dangling commits

All three ops act on a commit-ish ref; a dangling reflog SHA is reachable via
the reflog, so `reset`/`switch --detach`/`switch -c` resolve it. Confirmed for
`git show` in the reflog-window work; a one-line build-time check (reset + branch
onto a dangling SHA) closes it for these ops.

## Testing

- **engine:** the rename is covered by the migrated `checkout_test.go`
  (detached + create-branch on a tag *and* on a plain commit SHA, proving
  commit-ish generality). `Reset` already tested.
- **TUI:** `reflogResetRow`/`reflogCheckoutRow` exist and are anchored on a
  **non-zero** reflog cursor (display-vs-backing guard); the checkout modal
  offers Detached/Create branch…/Cancel and leads with a non-destructive
  option; the create-branch popup dispatches `Checkout{Ref, Branch}`; neither
  row appears on the Commits/Tags/Files panels (no leak).
- **CLI:** `gg tag checkout` unchanged — existing tag tests pass after the
  rename.

## Docs to update on completion

`CHANGELOG.md` (always); `README.md` (the Reflog tab description gains the
recovery actions); `internal/tui/help.go` (Reflog panel section). No
`agentskill` bump (no CLI surface change — the rename keeps `gg tag checkout`).
`CLAUDE.md` unchanged.
