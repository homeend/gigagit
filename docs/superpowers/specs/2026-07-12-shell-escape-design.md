# Shell escape — ctrl+o subshell + palette "Run shell command…" — design

**Date:** 2026-07-12
**Branch:** `feat/shell-escape`
**Status:** approved design; spec pending user review

## Problem

When git wedges in a state gg has no verb for, the user is trapped. The
motivating case: a cherry-pick whose resolution is an EMPTY change —
`git cherry-pick --continue` exits 1 telling the user to run
`git commit --allow-empty` or `git cherry-pick --skip`, neither of which gg
offers. The conflict process shows "Resolve failed: …" and every road from
there needs a shell. Today that means quitting gg (losing window state) or
switching terminals. Midnight Commander solved this class of problem with
ctrl+o; lazygit and tig ship the same escape.

## Goal

Two ways to reach a shell without leaving gg, both over the existing
`tea.ExecProcess` terminal-handover machinery ($EDITOR / external-viewer /
conflict-tool precedents):

1. **`ctrl+o` — interactive subshell.** Suspend gg, hand the real terminal
   to `$SHELL` in the current worktree, `exit` returns to gg with a full
   reload and the window stack intact. Works from ANY surface, including the
   conflict process and its "Resolve failed" message screen.
2. **Palette "Run shell command…" — one-off command** (vim's `:!`). Type a
   command, it runs with terminal handover, ends with a "press enter to
   return" pause so its output is readable, then gg reloads.

## Non-goals

- **A persistent background PTY** (true MC ctrl+o toggling between live
  screens). Heavy (PTY muxing under Bubble Tea) for no extra emergency
  value; the spawn-a-subshell model is what lazygit/tig ship.
- **Command allow-lists or approval gates.** The user types the command at
  the moment of execution — it is their explicit intent, the `$EDITOR`
  standing. (External-tool commands keep their approval gate because those
  come from persisted config.)
- **Capturing output into a gg viewer.** The terminal handover shows output
  natively; the one-off wrapper's end-pause makes it readable.
- **Resolver options for the empty cherry-pick** (`--skip` /
  `--allow-empty`). Considered and declined by the user — the shell escape
  covers it. (Recorded here so the decision is findable.)
- **Windows PowerShell detection.** `%COMSPEC%` (fallback `cmd.exe`) is the
  contract, matching git's own shell conventions on Windows; users who want
  pwsh can set COMSPEC or run it from cmd.

## Design

### 1. Shell resolution + wrapper scripts (`internal/tui/shell_escape.go`, new)

A small pure layer (unit-testable; no tea imports in the pure part):

- `userShell() (path string)` — `$SHELL`, fallback `/bin/sh`; on Windows
  `%COMSPEC%`, fallback `cmd.exe`. (`runtime.GOOS` switch; tests inject the
  env lookup as a `func(string) string`, the `clipboard`/`exttool` seam
  pattern.)
- **Subshell wrapper** — POSIX: a temp script (the `tool_run.go`
  `toolScript` precedent)
  `echo "gg subshell — 'exit' returns to gg"; exec <shell>` run as
  `<shell> <script>`; Windows: `cmd /K "echo gg subshell — 'exit' returns
  to gg"` (`/K` keeps the session open with the banner printed; no temp
  script needed).
- **One-off wrapper** — POSIX: temp script
  `<command>` then `st=$?; echo; printf '[exit %s] press enter to return to
  gg' "$st"; read -r _ </dev/tty; exit $st` run as `<shell> <script>`;
  Windows: `cmd /C "<command> & echo. & pause"`. The user's command text is
  written INTO the script body, never spliced into an argv string — no
  quoting/injection surface beyond what the user themselves typed.
- Both run with cwd = the current worktree root (`m.currentWorktree`) and
  the inherited environment plus `GG=1` (so scripts/prompts can detect they
  are under gg — one line, matches MC's `MC_SID` convention).
- Temp scripts are removed in the ExecProcess callback (best-effort), the
  `tool_run.go` cleanup precedent.

### 2. ctrl+o — central key (`internal/tui/model.go` + `shell_escape.go`)

- Handled in `Model.Update`'s central key section next to the existing
  `ctrl+p` handler — ABOVE the layer/process dispatch, so it fires
  regardless of what window, popup, process, or message screen is open.
- Gate: `opsIdle()` only. Busy → status notice `an operation is running —
  shell available when it finishes`. (The motivating stuck-cherry-pick
  state IS idle: the failed continue op already returned; the pause lives
  in git's sequencer, not in a running gg op.)
- No layer teardown: the stack is untouched; the shell runs; on return gg
  repaints exactly where the user was.
- On return (`shellDoneMsg` from the ExecProcess callback): status line
  `returned from shell` (or the error if the shell itself failed to start),
  then a **full manual-grade reload** (`reloadAllCmd(manual=true)`, the `r`
  path) — the user may have committed, skipped, rebased, or anything else.
  The existing status→conflict wiring then updates or clears the conflict
  process naturally.

### 3. Palette entries (`internal/tui/command_palette.go` + `shell_escape.go`)

Two new `paletteCommand` rows:

- **"Open shell"** (right-hint `ctrl+o`) — same dispatch as the key.
- **"Run shell command…"** — pushes `shellCmdPopup`, a one-line textfield
  popup (the `repoPathPopup` pattern): `enter` runs the typed command via
  the one-off wrapper (empty input = no-op), `esc` cancels. The field wires
  into the existing per-scope search-history recall (`recallUpdate` /
  `recordSearch`, new `scopeShellCmd`) so `alt+↑/↓` recalls previous
  commands across the session.

Both palette rows are gated the same way the palette itself is (it only
opens over read-only surfaces; rows additionally check `opsIdle()` at
dispatch, the existing palette convention).

### 4. Discoverability

- `?` help window: `ctrl+o  open a shell in the repo (exit returns)` in the
  global-keys section (help.go).
- The two palette entries are themselves discoverable via ctrl+p (the
  established home for rarely-used commands — File history/blame, Open
  repo, Apply patch all live there).
- Footer: not added — the footer is already at overflow on normal widths;
  ctrl+o appears in the help window's global section instead (the footer
  overflow feature lists hidden keys there).

### 5. What does NOT change

- No engine op, no `internal/domain` changes, no repogate reservation —
  the shell has the same standing as `$EDITOR` (documented precedent in
  `edit_actions.go` / `tool_run.go`): gg is suspended, git is not being
  driven by gg, and the post-return reload re-reads whatever the user did.
- No CLI surface (a CLI user already has a shell).
- `internal/exttool` untouched — these are not catalog tools.

## Failure handling

- Shell binary missing / script write fails → status notice with the error;
  nothing suspended, stack untouched.
- The subshell or command exiting non-zero is NOT an error for gg — the
  one-off wrapper displays `[exit N]` to the user; gg's return path treats
  any exit the same (reload + status line). Only a failure to LAUNCH
  (ExecProcess callback error) surfaces as an error status.
- Temp-script removal is best-effort (callback), and scripts carry only
  what the user typed — nothing sensitive beyond their own input.

## Testing (TDD)

- **Pure resolution**: `userShell` env/fallback matrix incl. Windows COMSPEC
  (injected env seam, no real env mutation); wrapper argv/script content for
  both wrappers on both OS shapes (script body contains the banner / the
  command + pause; Windows argv uses `/K` and `/C` correctly).
- **Central key**: ctrl+o over the bare panels, over an open popup, and
  over the conflict process → returns a non-nil cmd and leaves the layer
  stack untouched; busy (`m.running`) → notice and nil cmd.
- **Return path**: `shellDoneMsg` triggers the full reload (sources
  in-flight markers set) and the status line; launch-error variant surfaces
  the error.
- **Palette**: both rows present and labeled; "Run shell command…" pushes
  the popup; enter with text dispatches (cmd non-nil) and records recall
  history; enter with empty input no-ops; esc pops.
- **Not tested**: the live terminal handover itself (`tea.ExecProcess`) —
  same as the $EDITOR precedent; construction is tested, the handover is
  manual-verified.

## Docs to update at completion

`CHANGELOG.md`, `README.md` (global keys / palette section), `CLAUDE.md`
(`tui` package-map row: shell escape), help window covered by the feature
itself. No `agentskill` bump (CLI unchanged).
