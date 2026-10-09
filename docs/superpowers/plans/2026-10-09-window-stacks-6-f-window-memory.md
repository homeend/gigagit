# Per-worktree window stacks — phase 6: the F window's memory

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task (this repo's CLAUDE.md forbids implementer subagents; the one session that wrote the plan executes it). Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A parked F window (the files-on-disk tree) does not keep its on-disk list while its worktree sleeps: the list and the rendered tree are dropped on sleep and re-read on return; the window, its filter text and its cursor path stay. On a million-file tree this is the one parked item worth not keeping per worktree.

**Architecture:** `saveView` (the leaving slot) nils `wtFiles.all`, `wtFiles.untracked`, `wtFiles.letters` and `filesView.lines`, sets `wtFiles.loading = true` and remembers the selected path in `wtFiles.keepPath`. `viewKickCmd` (the arriving slot's wake-up) adds `loadLsFilesCmd()` when the F window is open and loading; `wtLoaded` lands the cursor on `keepPath` when set and clears it. The filter (`query`, `field`, `typing`) is untouched.

**Spec:** `docs/superpowers/specs/2026-10-09-per-worktree-window-stacks.md` ("Memory").

## Global Constraints

- Same worktree and gates. The list read goes through the slot's service (`m.svc` at kick time = the arriving slot's).
- The F window's loading row (`(loading…)`) is what a returned window shows until the list lands — the existing `openWorktreeFiles` shape.

## Review Focus

1. `lsFilesMsg` must be stamped (`slotStamp`) — the re-read is window-addressed; check it is in the phase-2 list and add it if not (it was not: `wtLoaded` handles it).
2. The cursor: `contentPopup.sel` is an index into `lines`; with `lines` dropped the index is meaningless. `keepPath` (the selected row's path at sleep) is the only thing to keep; `wtLoaded` re-selects it.
3. A filter being typed (`wtFiles.typing`) at the swap: the swap is the user's own (phase 5) or refused by `steerRefusal` ("the user is typing" — `filesView.typing`); either way the field is kept verbatim.

---

### Task 1: Drop on sleep, re-read on return

**Files:** `internal/tui/worktree_view.go` (`saveView`, `viewKickCmd`), `internal/tui/files_worktree.go` (`worktreeFiles.keepPath`, `wtLoaded`, `lsFilesMsg` stamp), tests `internal/tui/files_worktree_park_test.go`.

- [ ] **Step 1: Failing tests** — `TestParkedFWindowDropsItsListAndKeepsItsFilter` (open F in A with a filter and a cursor on a path; swap to B: A's slot has `wtFiles.all == nil`, `filesView.lines == nil`, `loading`, `query` kept, `keepPath` set); `TestReturnedFWindowReReadsItsList` (back in A: the kick carries an `lsFilesMsg` for A; after it lands the list is back and the cursor is on `keepPath`).
- [ ] **Step 2: Fail. Step 3: Implement. Step 4: Green, probe, package, commit** `feat(tui): a parked F window drops its file list and re-reads it on return`.

### Task 2: Docs, race gate, merge; the restructure's closing docs

- [ ] CHANGELOG (the phase-2 paragraph gains "the F window's file list is re-read when you return"), `docs/CLAUDE-details.md`, the spec's status line ("implemented, phases 1–6"), `./test.sh race` → "all green", bin/gg, memory, merge.
- [ ] Final whole-branch read-only review by a subagent on the most capable model (the CLAUDE.md exception), findings fixed on -2, merged.
