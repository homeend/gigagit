# Confirm slow working-tree operations — design

**Date:** 2026-06-26
**Status:** approved (brainstorm), pre-implementation
**Scope:** TUI only (`internal/tui`) + one `[ui]` config entry.

## Problem

On a very large monorepo (~20GB head) a single keystroke can launch an
operation that rewrites the working tree and takes a long time — most acutely
`s` (SmartSwitch). There is currently no guard: the op fires immediately, so an
accidental keypress commits the user to a multi-second/minute tree rewrite.

## Goal

Before launching a **slow working-tree-rewriting** operation from the TUI, pop a
**yes/no confirmation** with a **movable selection that defaults to "No"**.
Confirm with `enter` on the highlighted option; also accept `y`/`n`/`esc` direct
keys. On by default; opt out via config.

Non-goals: CLI prompting (the scriptable CLI must never block on a human),
repo-size detection / thresholds (always confirm for the chosen ops), a
"don't ask again this session" affordance (YAGNI — config opt-out suffices).

## The confirm dialog

Reuses the existing `decisionState` modal (`internal/tui/op.go`,
handled in `model.go` ~518-545) — the same machinery behind the `discard` /
`discard-all` / `switch-to-worktree` modals.

A new helper:

```go
// confirmOp wraps a slow working-tree op behind a yes/no modal (default: No).
// When confirmation is disabled (config), it launches the op directly.
func (m Model) confirmOp(op engine.Operation, prompt string) (tea.Model, tea.Cmd)
```

Behavior:

- If `!m.confirmSlowOps()` → `return m.startOp(op)` directly (no modal).
- Else set `m.modal = &decisionState{...}` with:
  - `req.Options = []string{"Yes", "No"}` (affirmative-first, matching the
    `discard` convention).
  - `sel` initialized to the index of `"No"` (1) — so the highlighted default,
    and therefore `enter`, is **No**. This is the literal "default = No"
    requirement; `decisionState.sel` otherwise defaults to 0.
  - `confirm: true` — a **new bool field** on `decisionState` that turns on the
    direct-key accelerators in the modal key handler.
  - `onResolve(m, opt)`: `"Yes"` → `m.startOp(op)`; otherwise `m, nil`.

Modal key handler additions (`model.go`, inside the `if m.modal != nil` block),
**gated on `m.modal.confirm`** so engine-driven decisions (push-rejected,
conflict forks, reflog/tag checkout) are never hijacked by a stray `y`/`n`:

- `y` / `Y` → resolve as `"Yes"` (run the op).
- `n` / `N` → resolve as `"No"` (cancel).
- `esc` → already cancels via `abortOption`; ensure it resolves to `"No"`.
  (`abortOption` picks the option whose lowercased text looks like an abort;
  verify it selects `"No"` for these options, else handle `esc` explicitly in
  the confirm branch.)

Existing `up/k`, `down/j` movement and `enter` already work unchanged.

## Call-site inventory

Wrap each of these `m.startOp(<op>)` calls with `m.confirmOp(<op>, <prompt>)`.
Prompts are short and name the target, e.g.
`"Switch to " + b.Name + "?"`, `"Reset to " + shortHash(hash) + "? This moves the branch ref."`.

**Wrapped (slow tree rewrite, single user gesture, no pre-existing modal):**

| File:line (current) | Op | Trigger |
|---|---|---|
| `model.go:746` | `SmartPull` (`pullForFocus()` — stay + background) | `p` key |
| `model.go:759` | `SmartCheckout{…CheckoutStay}` | remote `c` key |
| `model.go:780` | `SmartCheckout{…CheckoutSwitch}` | remote `s` key |
| `model.go:808` | `SmartSwitch` | `s` key |
| `branch_pull.go:40` | `SmartPull` (background) | menu |
| `branch_actions.go:27` | `SmartMerge` | `.` menu |
| `branch_actions.go:46` | `SmartRebase` | `.` menu |
| `remote_actions.go:82` | `SmartMerge` | `.` menu |
| `remote_actions.go:100` | `SmartRebase` | `.` menu |
| `tags_actions.go:94` | `SmartMerge` | `.` menu |
| `tags_actions.go:115` | `SmartRebase` | `.` menu |
| `commit_scope.go:766` | `FastForward` | `.` menu |
| `commit_scope.go:787` | `Reset` (commit) | `.` menu |
| `reflog_view.go:26` | `Reset` (reflog) | `.` menu |

Line numbers are a snapshot — match on the op expression, not the line.

**Carve-outs (explicitly NOT wrapped):**

- **CreateWorktree / CreateWorktreeForBranch** (`worktree_popup.go:322/324`) —
  reached only by opening the worktree popup and filling a multi-field form; the
  form submit *is* the deliberate confirmation. Double-prompting after a form is
  noise.
- **reflog/tag Checkout** (`reflog_view.go:56`, `tags_actions.go:58`) — already
  behind their own decision modal (`Detached / Create branch… / Cancel`). The
  decision *is* the gesture; a second modal would stack.
- **chainSwitch** (`model.go:1343`) — a continuation of an already-initiated /
  already-confirmed switch (fires after a refs reload). Confirming here would
  double-prompt a flow the user already started.
- **go-to-worktree** (`model.go:799-805`, the `switch-to-worktree` modal's
  "go to worktree" branch) — `reRoot` navigation, not a working-tree rewrite.

## Config

Default ON, opt-out. The config overlay only propagates `true` upward
(`config.go` `overlayUI`, e.g. the `ShowEOLOnlyChanges` / `LogOperations`
"inverted polarity" pattern), so a default-ON flag must be expressed as an
**inverted field**:

- `UIConfig.DisableSlowOpConfirm bool` `toml:"disable_slow_op_confirm"` —
  default `false` (= confirmation ON). A `true` in global/repo config overlays
  up to disable it.
- `func (m Model) confirmSlowOps() bool { return !m.cfg.UI.DisableSlowOpConfirm }`.
  `m.cfg` is the zero `config.Config` only before the first load; ops cannot
  fire during `m.loading`, so by the time any wrapped key is reachable `m.cfg`
  is populated. Zero-value (`false`) also yields confirm-ON, which is the
  desired default — safe either way.

Follow the **adding-config-entries** checklist:
1. Add the field to `UIConfig` (config.go) with the `toml` tag + comment.
2. Add the overlay line in `overlayUI` (`if src.DisableSlowOpConfirm { dst… = true }`).
3. Add a `settingDoc` row in `config/template.go`
   (`{"ui", "disable_slow_op_confirm", false, "skip the yes/no confirmation before slow working-tree ops (switch, pull, merge, rebase, fast-forward, reset)"}`).
4. Hand-sync the 3 literals the skill names; ensure `TestSettingDocsCoverage` passes.

## Testing

- **confirm helper / modal:** table test driving `confirmOp` → assert a modal
  with `Yes/No`, `sel` on `No`, `confirm == true`; then feed `enter` (No →
  no op), `y` (op starts), `n`/`esc` (no op), and `↓`+`enter` (Yes → op starts).
  Reuse the `modal_test.go` / `decision_integration_test.go` drive style.
- **gating:** with `DisableSlowOpConfirm = true`, pressing `s` starts the op
  with no modal.
- **carve-outs:** assert that reflog/tag checkout and the worktree popup submit
  do **not** raise the slow-op confirm (no double modal), and `chainSwitch`
  resolution does not re-prompt.
- **config:** `TestSettingDocsCoverage` green; overlay test for the new field.

## Out of scope / follow-ups

- Per-op or size-threshold gating.
- A footer/help hint that `y`/`n` work in the confirm (help text update is
  cheap and may be folded in, but is not load-bearing).
