# Editable worktree path in the w/W create-worktree popup

**Date:** 2026-08-12
**Status:** Approved (brainstorm complete)
**Scope:** TUI popup only. CLI `--path` and the web worktree form are explicitly out of scope.

## Problem

The w/W create-worktree popup resolves the worktree path purely from the
`[worktree] path_template` (with the branch name plugged in) and shows it
read-only as `path:`. The branch name is editable (`e`, prefixes), but the
path is not — there is no way to place a worktree somewhere other than where
the template puts it.

## Design

### Interaction

- In the popup's **action state** (`stAction`), a new key **`E`** opens a
  path-edit field, mirroring how `e` edits the branch name.
- The field is seeded with the **current previewed path**, so the user edits
  rather than retypes.
- `enter` confirms the edit; `esc` discards it and returns to `stAction`.
- Confirming an **empty** field reverts to the template-derived path — the
  escape hatch back to "follow the branch".

### Behavior

- A confirmed path edit becomes a **`pathOverride`** field on `worktreePopup`
  that wins over the path template in `recompute` — exactly parallel to the
  existing `branchOverride`.
- Once confirmed, the override **sticks verbatim** regardless of later
  branch-name changes (via `e` or the prefix picker). (User-chosen behavior.)
- While actively editing, the live buffer is shown in the `path:` preview
  line (same live-preview pattern as branch editing).
- A **relative** typed path behaves exactly like a template result: the
  engine (`resolveNewWorktreePath`) anchors it at the main worktree root. An
  **absolute** path is used as-is. No new resolution logic.
- No new validation: git itself rejects an unusable/occupied path and the
  popup already surfaces op errors. `startCreateFromPopup`'s existing
  `previewErr` gate still applies to template errors when no override is set.

### Seq counters

When the path is overridden, the path template is bypassed, so its `<seq>`
counters must **not** be bumped on create — the same rule already applied
when the branch template is bypassed. `consumedSeqNames` learns about the
path override: with both branch and path overridden/fixed, only a chosen
prefix's own `<seq>` names remain.

### States

New popup state `stEditPath` alongside `stInput`/`stAction`/`stEdit`, with
its own `update` case (enter = confirm to `pathOverride`, esc = discard,
otherwise keys go to the edit buffer) and its own hint line. `ctrl+c` still
quits from every state; esc never traps.

### Hints & i18n

- `stAction` hint lines gain `[E] edit path` (all variants: with/without
  hook, with/without switchOnCreate).
- `stEditPath` gets its own hint line (`[type] edit path  [enter] done
  [esc] discard`).
- Every new user-visible string goes through `i18n.T` with literal keys
  present in **all four bundles** (ja/ko/zh/ru); AST-gate tests enforce this.
- In-app help text for the worktree popup is updated if it enumerates keys.

## Testing (TDD)

In `internal/tui/worktree_popup_test.go` (or a sibling file):

1. `E` in `stAction` opens path edit seeded with the previewed path.
2. Typing updates the live `path:` preview.
3. `enter` confirms; the override survives a subsequent branch-name edit
   (path sticks verbatim).
4. `esc` discards the in-progress edit (preview returns to template result).
5. Confirming an empty field reverts to the template-derived path.
6. `createOp` carries the overridden path.
7. `consumedSeqNames` excludes path-template `<seq>` names when the path is
   overridden.
8. i18n scan gates pass (new keys in all bundles).

## Out of scope

- CLI `gg worktree add --path` flag.
- Web worktree-create form path field.
- Path-existence / collision pre-checks in the popup.

## Rejected alternative

A unified edit mode where tab cycles branch↔path fields — more keystrokes to
reach, and it would tangle the existing branch-edit/prefix flow for no gain.
