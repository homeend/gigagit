# Action menu in every navigable window — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:executing-plans. Steps use `- [ ]`.

**Goal:** `.` opens the action menu from the file tree, stash list, diff view, history, and blame — showing the in-view copy actions — without opening over modals/popups/transient editors.

**Spec:** `docs/superpowers/specs/2026-06-17-action-menu-in-windows-design.md` (full code there).

**Architecture:** treat the menu as a modal-like overlay (out-ranks content windows in both dispatch and render); each navigable window opts in to `.`; the menu is window-aware (copy rows only inside a window).

**Working location:** the `fix/menu-in-windows` worktree at `.claude/worktrees/menu-in-windows`. All commands from its root.

---

## Task 1: Window-aware copy rows (pure, no precedence change)

**Files:** `internal/tui/action_menu.go`; test `internal/tui/action_menu_test.go`.

- [ ] Write failing tests: `TestInContentWindow*` (diffView/filesView/stashView/history/blame → true; irebase/hunkPicker stack → false; base panels → false); `TestContextCopyRowsDiffView`, `TestContextCopyRowsHistory`, `TestContextCopyRowsFilesTree` (tree-focused file + filesHash → path/name+commit id), `TestContextCopyRowsStashFileTree` (filesHash=="" → path/name only), `TestContextCopyRowsStashList` (copy-stash-ref), `TestAvailableActionsContentWindowCopyOnly` (filesView open, focus panelCommits, filesHash set → rows are only copy ids, NO `commit-files`).
- [ ] Run → fail.
- [ ] Implement `inContentWindow`, `fileCopyRows`, `fileCopyPathName`, window-aware `contextCopyRows` prefix, and the `availableActions` content-window early return — exactly as in the spec.
- [ ] Run `go test ./internal/tui/ -run 'TestInContentWindow|TestContextCopyRows|TestAvailableActions'` → pass. `go vet`, `gofmt -l`.
- [ ] Commit: `feat(tui): window-aware . menu copy rows (file tree/diff/history/blame/stash)`.

## Task 2: `.` opt-in + dispatch precedence move

**Files:** `internal/tui/model.go`, `files_view.go`, `stash_view.go`, `diff_view.go`, `history_view.go`, `blame_view.go`; tests.

- [ ] Write failing tests: `TestDotOpensMenuFromFilesView`, `...FromDiffView`, `...FromStashView`, `...FromHistory`, `...FromBlame` (press `.` → `m.actionMenu != nil`); `TestMenuOwnsKeysOverDiffView` (m.diffView+m.actionMenu set; `Update(esc)` → actionMenu nil AND diffView intact); `TestMenuOwnsKeysOverHistory` (same, pushed historyView); `TestDotInertOverModal`, `TestDotInertOverRepoPopup`, `TestDotInertOverIrebaseEditor`, `TestDotInertOverHunkPicker`.
- [ ] Run → fail.
- [ ] Add `case ".": return m.openActionMenu(), nil` to the 5 surface handlers (files/stash/diff/history/blame `update`s).
- [ ] In `model.go` `case tea.KeyMsg`, MOVE `if m.actionMenu != nil { return m.updateActionMenuKey(msg) }` to right after the modal block (before the `stackTop()` check) and delete it from its old position.
- [ ] Run the relevant tests → pass; then full `go test ./internal/tui/` to catch dispatch regressions. `go vet`, `gofmt -l`.
- [ ] Commit: `fix(tui): . opens the action menu inside navigable windows`.

## Task 3: Render overlay hoist

**Files:** `internal/tui/view.go`; tests.

- [ ] Write failing tests: `TestMenuRendersOverDiffView` (m.diffView+m.actionMenu set → `View()` contains "Actions"), `TestMenuRendersOverHistory` (pushed historyView + menu → contains "Actions").
- [ ] Run → fail.
- [ ] Add `menuBackground()`; hoist the `actionMenu` overlay to just after the modal block in `View()`; remove the redundant lower `actionMenu` branch and drop `m.actionMenu == nil` from the no-overlay guard (spec code).
- [ ] Run tests → pass; full `go test ./internal/tui/`. `go vet`, `gofmt -l`.
- [ ] Commit: `fix(tui): render the action menu over diff/history/blame windows`.

## Task 4: Docs + gate

**Files:** `CHANGELOG.md`, `README.md`.

- [ ] CHANGELOG `[Unreleased]`: the `.` menu now opens in every navigable window (file tree, stash list, diff, history, blame), showing that window's copy actions; stash list adds Copy stash ref.
- [ ] README: update the `.` row to say it works in all navigable windows.
- [ ] Run `./test.sh race` → all green.
- [ ] Commit docs. Then finishing-a-development-branch.

## Self-review

- Spec §1 (precedence) → Tasks 2+3; §2 (opt-in) → Task 2; §3 (window-aware) → Task 1; docs → Task 4.
- Type names consistent with spec: `inContentWindow`, `menuBackground`, `fileCopyRows`, `fileCopyPathName`, ids `copy-file-path`/`copy-file-name`/`copy-commit-id`/`copy-stash-ref`.
- Tasks 1 is pure (no precedence risk); 2 and 3 isolate the dispatch and render moves respectively, each followed by the full tui suite.
