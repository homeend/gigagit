# Session window keys — plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task (this repo's CLAUDE.md forbids implementer subagents; the one session that wrote the plan executes it). Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** the key set of `docs/superpowers/specs/2026-10-10-session-window-keys.md`: `alt+U` unlock, `alt+A` / `alt+T` worktree-scoped session walks, `alt+f` size toggle, `alt+b` bind toggle.

**Architecture:** `cycleSessions(terminal)` gains a `scoped` flag (the ring filtered to the viewed worktree, silent when empty). `alt+f` / `alt+b` are handled in `updateConsoleKey` for a FOCUSED console (bound or not), ahead of the bound-console branch; both end bound + focused (`alt+b` from bound ends unbound). The unlock key constant moves to `alt+U`. Every new user-visible string goes through `i18n.T` with keys in the four bundles.

**Tech Stack:** Go 1.26, Bubble Tea, the TUI test helpers (`loadedModel`, `addWorktree`, `installSessionManager`, `startSessionIn`, `pressAlt`, `press`).

## Global Constraints

- Worktree `/work/gigagit/.claude/worktrees/fast-worktree-switch-2`; TDD; `gg add` + `git commit -F`; attribution lines; never push; `./test.sh race` → "all green" before the merge into `feat/fast-worktree-switch`.
- The e2e goldens may change where a footer hint moved (`alt+A` → `alt+U`): inspect, then `-update`.

## Tasks

### Task 1: `alt+U`
- [ ] Failing test: `press alt+U` writes the dump; `alt+A` does nothing (emergency_unlock_test.go; main_test.go references).
- [ ] `emergencyUnlockKey = "alt+U"`, footer `[alt+U] unlock`, help row key + text (i18n: new keys in ja/ko/ru/zh, old removed), README, CHANGELOG, CLAUDE-details (`emergency-unlock` section), memory file name stays.

### Task 2: `alt+A` / `alt+T`
- [ ] Failing tests: `TestAltShiftAWalksTheViewedWorktreesAgents` (two agents in A, one in B; from A `alt+A` binds A1, again A2, again A1; never B), `TestAltShiftAWithNoAgentHereDoesNothing` (B has none: no console, statusMsg unchanged), the terminal twin.
- [ ] `cycleSessionsIn(terminal, scoped bool)`; `cycleSessions` = unscoped; the key routing at `model.go` (~2286) and the bound-console branch (`console.go` ~940) take `alt+A`/`alt+T`; `sessionRing` gains a worktree filter.
- [ ] Help rows + bundles, footer (`[alt+A] agent here`?) — footer only if it fits the overflow trim; README, CHANGELOG.

### Task 3: `alt+f`
- [ ] Failing tests: docked bound → `alt+f` → maximized, bound, focused; again → docked, bound; unbound focused → `alt+f` → maximized and bound; not focused (another panel) → nothing.
- [ ] `updateConsoleKey`: `if m.console != nil && m.focus == panelCommits && key == "alt+f"`: `maximized = !maximized`, `ret.full = maximized` (so `ctrl+]` and the return agree), `focused = true`, `touchConsole`, `syncConsoleSize`.
- [ ] Help, footer hint while focused, bundles, README, CHANGELOG.

### Task 4: `alt+b`
- [ ] Failing tests: bound → `alt+b` → unbound, still shown and focused; unbound focused → `alt+b` → bound; not focused → nothing.
- [ ] `updateConsoleKey`: route to the step-out branch when bound, to the enter-binds path when not.
- [ ] Help, footer hint, bundles, README, CHANGELOG.

### Task 5: docs sweep, race gate, merge
- [ ] `docs/CLAUDE-details.md` console section: the three states, focused vs bound, the key table; the alt+a/alt+t help/README text says "Branches tab's order".
- [ ] `./test.sh race` → "all green"; `go build -o bin/gg`; memory; merge into the parent; rebuild the parent's bin/gg.
