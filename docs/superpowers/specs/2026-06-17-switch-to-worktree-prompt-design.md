# Switch-to-worktree prompt on `s` — design

## Problem

In the TUI Branches panel, pressing `s` runs `engine.SmartSwitch{Branch}`, which
does `git switch <branch>`. When the selected branch is already checked out in
another worktree, git refuses (`fatal: '<branch>' is already checked out at
…`) and the TUI just surfaces that error in the status bar. The user's actual
intent in that moment is almost always "take me to where that branch lives" —
so gg should offer to jump to that worktree instead of failing.

## Key insight

"Switch to the worktree" is **navigation, not a git operation.** The branch is
already checked out there; git state does not change. Jumping means re-rooting
the UI at the worktree path and cd-ing the shell there on exit (the existing
`reRoot` + `switchTarget` / `--cwd-file` mechanism). Navigation/cwd is a
frontend concern — it lives in `cmd/gg`/`shellinit`, not the engine. Therefore
this feature belongs entirely in the TUI `s` handler, **not** in the
`SmartSwitch` engine operation. `SmartSwitch` is unchanged.

Scope (confirmed): **TUI only.** CLI (`gg switch`) keeps today's behavior
(git's error). If wanted later, `domain.WorktreeForBranch` already exists as the
shared detection query for a CLI guard — a separate follow-up, no engine work.

## Behavior

When `s` is pressed on a Branches selection:

1. Scan the already-loaded `m.worktrees` for one whose `Branch` equals the
   selected branch and whose path is **not** the current worktree.
2. **No match** → run `engine.SmartSwitch{Branch}` exactly as today.
3. **Match** → open a centered modal:

   ```
   feature/x is checked out in another worktree.

   → go to worktree   (~/code/wt-x)
     cancel
   ```

   - `enter` on **go to worktree** → `m.reRoot(wt.Path)` — identical to pressing
     `enter` on the Worktrees panel: re-roots the UI and records `switchTarget`
     so the shell cd's there on exit.
   - `esc`, or selecting **cancel** → close the modal, no-op.

No "switch anyway" option: `git switch --ignore-other-worktrees` leaves two
worktrees on the same branch, a footgun. This feature is purely a redirect to
navigation; it never mutates git state.

## Mechanism

- **Keep the gate unchanged.** `canSwitchBranch` stays as-is so `s` remains
  live on the selected branch. The worktree check happens *inside* the
  `case "s"` handler — we **redirect**, we don't disable. (Extending the gate
  would make `s` silently no-op on an other-worktree branch, the opposite of
  the intent.)
- **No new popup type, no new async query.** Reuse the existing centered
  decision modal (`decisionState` + its arrow/enter/esc nav and `view.go`
  rendering). The worktrees are already in `m.worktrees`, so detection is a
  synchronous scan — no `domain` round-trip.
- **Frontend resolution.** `decisionState` today resolves by sending the chosen
  option to an engine reply channel (`reply chan engine.DecisionResponse`). Add
  an optional frontend-resolution callback (e.g. `onResolve func(opt string)
  (tea.Model, tea.Cmd)`); when set, the modal key handler calls it on
  enter/esc instead of replying to an engine op. The `go to worktree` option
  resolves to `reRoot(path)`; `cancel`/`esc` closes. Engine-driven modals are
  unaffected (callback nil → existing reply-channel path).

### Touch points

| File | Change |
|------|--------|
| `internal/tui/model.go` | `case "s"`: scan `m.worktrees`; on match open the frontend modal, else `SmartSwitch`. Modal key handler: honor `onResolve` when set. |
| `internal/tui/op.go` | `decisionState`: add optional `onResolve` callback field. |
| `internal/tui/help.go` | Note that `s` may redirect to the branch's worktree. |
| `internal/tui/footer.go` | (If hint wording needs it) — keep `[s]witch` hint; no gate change. |

## Testing (TDD)

1. **Redirect path:** with a second worktree holding `feature/e`, pressing `s`
   on that branch (from the Branches panel) opens the modal — assert the modal
   prompt names the branch + worktree path and offers the two options. Choosing
   **go to worktree** sets `switchTarget` to that worktree's resolved path
   (mirror the existing `TestEnterOnWorktreePanelSwitches` assertion style).
2. **Cancel:** `esc` / **cancel** closes the modal and does **not** set
   `switchTarget` or start an op.
3. **No-conflict path unchanged:** `s` on a branch *not* checked out elsewhere
   still runs `SmartSwitch` (no modal). Guard against regressing the normal
   switch.

## Docs

- `CHANGELOG.md` (Unreleased / Added).
- Help pane (`?`) line for `s` updated.
- `README.md` only if the Branches-panel key table calls out `s` behavior.
- No CLI surface change → no `agentskill` bump.
