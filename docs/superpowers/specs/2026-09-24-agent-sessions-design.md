# Agent sessions — interactive agent consoles inside gg

Date: 2026-09-24 · Branch: `feat/agent-sessions` · Status: design agreed, spec under review

## Goal

Start an interactive AI agent (Claude Code, Codex, Junie, Antigravity, Kimi,
or any configured command) **inside a worktree from the Worktrees tab**, run
it in a live terminal console that takes the Commits panel's area, keep it
running while the user moves around gg, and reach every running session at
any moment from one global popup. Several sessions may run at once in one gg
process; gg is never restarted.

Success: the user starts `claude` under worktree A, types to it, steps out to
watch the Files panel update while it edits, switches to worktree B (and even
another repo with `R`), starts `codex` there, and brings either console back
from the sessions popup — all without leaving gg and without either agent
being interrupted.

## Rulings (agreed in brainstorming — do not re-ask)

1. **Process model.** gg never re-execs on a worktree/repo switch (`reRoot`,
   `internal/tui/model.go`, rebuilds per-repo state in place; `--cwd-file`
   only tells the shell where to `cd` on exit). Sessions are owned by a
   **process-global manager that `reRoot` never touches**.
2. **Repo switch (`R`) keeps sessions running.** The sessions popup lists
   everything this gg process owns, grouped by repo → worktree. Opening a
   session from another repo shows its console; it does **not** reRoot.
3. **Exited sessions stay listed** as `exited (code N)` with their last screen
   readable, until the user removes them explicitly.
4. **Quit guard.** Quitting gg with live sessions opens the sessions popup in
   quit mode: **Kill all and quit** / **Cancel** (esc = cancel).
5. **Command source = a new exttool category `session`.** Built-ins: plain
   interactive launch per agent + an opt-in **yolo** variant. Custom entries
   via `[[tools.command]]` with `category = "session"`. Commands go through
   the existing first-run approval gate (`promptstate` command hash).
6. **Auto-configure on first use.** If no `session` command is configured,
   the first **Start agent…** detects installed agents (off-thread, with a
   visible *"Detecting installed agents…"* busy notice), writes the **safe
   (non-yolo)** entries for every detected agent to the **global** config,
   reports once what it added and where, then shows the chooser. Afterwards
   the config is the source of truth; yolo stays opt-in via
   Settings → External tools.
7. **Reserved keys — only two bytes are taken from the agent:**
   - `ctrl+]` (0x1d) — **step out one level**.
   - `ctrl+\` (0x1c) — **sessions popup**, from anywhere (console included).
   Both configurable. Rejected: ctrl+esc / esc / `ctrl+[` (= ESC byte, the
   agent needs it), `ctrl+/` (= 0x1f, Claude Code uses it), `ctrl+.` (no
   control byte without the kitty keyboard protocol, which Bubble Tea v1
   lacks), `ctrl+g` (taken by gg), ctrl+letters (readline/agent editing).
8. **Console state machine** (below) — docked focused / docked unfocused /
   maximized focused / closed.
9. **Approach: in-process PTY + Go terminal emulator, spike first.** tmux
   backend rejected (no Windows, lossy `send-keys`, polling lag);
   `handover()` rejected (suspends gg — no concurrency, no live panel).
10. **Frontends:** TUI in this feature. The web attach (xterm.js over the raw
    byte tap) is a later, separate spec; the core is built to allow it.

## Console state machine

| State | Key | Result |
|---|---|---|
| closed | `enter` on a session sub-row / pick in the `ctrl+\` popup / Start agent… | docked in the Commits area, **focused** |
| docked, focused | `ctrl+]` | docked, **unfocused** (still live-updating) |
| docked, unfocused | `enter` | docked, focused |
| docked, unfocused | `ctrl+t` | **maximized + focused** |
| maximized, focused | `ctrl+]` | shrinks to the Commits area, **unfocused** |
| docked, unfocused | `esc` | closed; Commits panel returns; session keeps running |
| any | `ctrl+\` | sessions popup |

While a console is **focused, every key except the two reserved ones goes to
the agent** — including `esc`, `ctrl+t`, and gg's single-letter shortcuts.
While **unfocused**, the console is an ordinary panel for focus cycling
(tab/shift+tab reach it like the Commits panel) and gg's keys work
normally elsewhere. Only one console is on screen at a time; opening another
session replaces the displayed one (the replaced session keeps running).

## Architecture

```
 internal/domain ── Sessions(): the ONE process-global agentsession.Manager; resolves `session`
              │     commands from exttool+config; first-run detect+write
              │
 internal/tui ── console panel · Worktrees sub-rows · ctrl+\ popup · quit guard
              │
 internal/agentsession ── Manager · Session · PTY (xpty) · Emulator iface
```

### `internal/agentsession` (new; DAG leaf)

Imports: stdlib, `charmbracelet/x/xpty` (creack/pty on Unix, ConPTY on
Windows), and the chosen emulator. **No git, domain, or TUI imports.**

- `Manager` — `map[ID]*Session` under a mutex. The single instance is held
  by `domain.Sessions()` (lazily created, process-global — the `repogate`
  registry precedent), so it survives every `reRoot` (which reopens the
  `Service`).
  - `Start(spec StartSpec) (*Session, error)` — `StartSpec{Label, AgentID,
    Argv/CommandLine, Dir, RepoName, Cols, Rows, Env}`.
  - `List() []Info` (stable order: start time), `Get(id)`, `Remove(id)`
    (exited only), `KillAll(ctx)`, `LiveCount()`.
- `Session`
  - `Info{ID, Label, AgentID, Repo, Dir, Started, State, ExitCode}`,
    `State ∈ {Running, Exited}`.
  - `Write(p []byte)`, `Resize(cols, rows)`, `Screen() Screen` (a copied
    snapshot: cell grid with rune/width/fg/bg/attrs + cursor pos/visibility
    + alt-screen flag), `Kill()`, `Changed() <-chan struct{}` (coalesced,
    capacity-1 signal), `Tap() (<-chan []byte, cancel)` (raw byte fan-out
    for the future web attach; a slow tap drops, never blocks the reader).
  - One reader goroutine: PTY → emulator (under the session mutex) →
    scrollback ring → signal `Changed` → taps. `Wait` goroutine records the
    exit code, flips `State`, signals `Changed`.
  - Scrollback ring capped at **10 000 lines** per session.
- `Emulator` interface (`Write`, `Resize`, `Snapshot`) keeps `x/vt` vs
  `hinshun/vt10x` swappable; the spike picks one, the other is not built.
- Kill: Unix — SIGTERM to the process group, SIGKILL after a 3 s grace;
  Windows — close the ConPTY and terminate the job object. Children are put
  in a Windows job object (kill-on-close) so a crashed gg leaves no orphans;
  on Unix the PTY master closing delivers SIGHUP.
- Env: inherited + `GG_SESSION_ID=<id>` + `TERM=xterm-256color`
  (`COLORTERM=truecolor` if the host terminal advertises it).

### `internal/domain`

- `domain.Sessions()` returns the process-global manager (test seam
  `UseSessionManager`); type aliases (`SessionInfo`, `SessionScreen`, …)
  let frontends use it without importing `agentsession` (added to the archtest domain-only rule).
- `SessionCommands(cfg) []SessionCommand` — the `session` category entries
  from the effective config, filtered to this frontend.
- `EnsureSessionCommands(ctx, globalPath)` — the first-run auto-configure:
  `exttool.Detect` → safe `session` templates → append `[[tools.command]]`
  blocks to the global config (the Settings wizard's writer, shared, never
  rewriting existing `(category,name)` blocks) → returns what was added.
  Runs off the Update goroutine.
- `StartSession(cmd, worktreePath, cols, rows)` resolves the command
  template (`template` package: `<bin>`, `<worktree>` tokens) and the repo
  NAME for grouping, then `Manager.Start`.

### `internal/exttool`

- `CatSession Category = "session"`; `ModeTerminal`-like interactive mode
  (new `ModeSession` so capture/handover code never picks it up).
- Built-ins (each safe + opt-in yolo):
  - claude: `<bin>` · yolo `<bin> --dangerously-skip-permissions`
  - codex: `<bin>` · yolo `<bin> --dangerously-bypass-approvals-and-sandbox`
  - junie: `<bin>` · yolo `<bin> --brave`
  - antigravity: `<bin>` · yolo `<bin> --dangerously-skip-permissions`
  - kimi: `<bin>` (no yolo flag known — none shipped)
  Exact flags are re-verified against each installed CLI's `--help` during
  implementation; a flag that does not exist is dropped, not guessed.
- The Settings → External tools wizard lists them automatically;
  `defaultToolChecked` already leaves `OptIn` rows unticked.

### `internal/tui`

- **Console panel** — replaces the Commits panel's rectangle when docked, or
  the whole frame when maximized. Paints `Screen()` cell by cell through
  lipgloss styles (runs of equal style merged); a focused console shows the
  agent's cursor. Border title: `claude · <worktree> · running 12m` /
  `exited (3)`; footer while focused: `ctrl+] step out · ctrl+\ sessions`.
- **Repaint** — a `tea.Cmd` waits on `Changed()` and returns a
  `sessionChangedMsg{id, gen}`; the model re-arms the waiter and repaints at
  most once per **33 ms** (a trailing tick catches the last change). A
  `gen` counter drops messages for a console no longer displayed.
- **Input** — `tea.KeyMsg` → xterm byte encoding (runes incl. alt, ctrl
  letters, arrows/home/end/pgup/pgdn/insert/delete/F-keys with modifiers,
  enter/tab/backspace/esc) → `Write`. Bracketed paste (`tea.Paste`) is
  wrapped in `ESC[200~ … ESC[201~` when the agent enabled it. The two
  reserved keys are matched first and never forwarded. Keys to an exited
  console are ignored and the footer shows `x remove · esc close`.
- **Resize** — on `tea.WindowSizeMsg` and on dock↔maximize, the panel's inner
  size is pushed to `Resize` (only when it changed).
- **Worktrees tab** — under each worktree row with sessions, one sub-row per
  session: `└ ● claude  running 12m` / `└ ○ codex  exited (0)`. Sub-rows are
  display rows (`backingIndex` reports no worktree for them). `enter` opens
  the console. `.` menu: **Start agent…** on a worktree row; **Open session /
  Kill session / Remove session** on a sub-row. Sessions of the current repo
  only appear here (other repos' are in the popup).
- **Sessions popup (`ctrl+\`)** — grouped repo → worktree; columns: state
  glyph, label, worktree, age/exit code. `enter` open, `k` kill (confirm for
  a running one), `x` remove (exited only), `/` filter, esc close. A session
  whose worktree directory vanished is grouped under *orphaned worktree*.
  Opening from a popup follows the "window returns to its popup" rule only
  when the popup was opened over another popup.
- **Start agent…** — if `SessionCommands` is empty: busy notice
  *"Detecting installed agents…"*, off-thread `EnsureSessionCommands`, then a
  notice *"Added claude, codex to <path> — edit there or in Settings →
  External tools"*, then the chooser. Nothing detected → notice explaining
  how to add a `[[tools.command]] category = "session"` block. The chooser is
  the numbered tool chooser used by other categories; one entry skips it.
  Unapproved command → existing approval prompt.
- **Quit guard** — `q`/`ctrl+c` quit path checks `LiveCount()`; > 0 opens the
  popup in quit mode (**Kill all and quit** / **Cancel**). Kill-all runs
  off-thread with the busy notice, then quits.
- **Notices** — an agent exiting while its console is not focused raises a
  status notice *"claude in <worktree> exited (N)"*.
- **Keys config** — `[session] keys = { step_out = "ctrl+]", sessions =
  "ctrl+\\" }` with settingDocs; help (`?`) and footer advertise both.
- i18n: every string in all four bundles (AST gates).

## Error handling

- Missing binary / spawn failure → error notice; no session created.
- Worktree directory removed while a session runs → session keeps running;
  shown under *orphaned worktree* in the popup; its sub-row disappears with
  the worktree row.
- Deleting a worktree (`d`) that has a **running** session → the delete
  confirmation names it and offers *Kill session and delete* / *Cancel*.
- Emulator panic on malformed output → recovered in the reader goroutine;
  session marked exited with a synthetic error line, logged via `observ`.
- A slow TUI never blocks the agent: the reader never waits on the UI
  (coalesced signal; taps drop).

## Testing

- **agentsession** — real PTY with scripted children
  (`sh -c 'printf …; read x; exit 3'`; `cmd /c` on Windows): start, write
  echo, screen snapshot contents, `Resize` observed via `stty size`, exit
  code, kill removes the whole process group, remove refuses a running
  session, scrollback cap, `Changed` coalescing, slow tap does not block.
  `t.Parallel()` where no global state.
- **domain** — first-run detect+write into a scratch config dir (never the
  real global), existing blocks untouched, approval hash, manager survives a
  service reopen (the `reRoot` path).
- **exttool** — catalog tests for the `session` templates; wizard defaults
  (yolo unticked).
- **tui** — model tests for the full state table, reserved-key interception,
  a key→bytes table test, throttle/gen dropping, sub-rows + `.` menu, popup
  grouping and quit mode, worktree-delete guard; a `tui-capture.sh` headless
  run with a real child (`bash` or a fake agent script) asserting the docked
  console, a sub-row, and the popup.
- **archtest** — `tui`/`cli`/`web`/`mcp` never import `agentsession`.

## Phasing

0. **Spike (throwaway, not merged)** — bare Bubble Tea harness running
   `claude` via `xpty` + `x/vt` on Linux/WSL and Windows: alt-screen,
   256/true colour, wide glyphs, resize, bracketed paste, esc/ctrl keys.
   Decides `x/vt` vs `vt10x`; findings reported before phase 1.
1. `agentsession` core + domain wiring + exttool `session` category +
   first-run auto-configure.
2. TUI: console panel, state machine, Worktrees sub-rows + menu, sessions
   popup, quit guard, keys config, i18n, help/footer.
3. *(separate spec)* web attach via the raw tap + xterm.js.

Docs per stage: CHANGELOG (always), README (new surface), CLAUDE.md package
map row for `agentsession`, `docs/CLAUDE-details.md` for the state machine and
reserved-key rationale. No CLI surface change planned (using-gg untouched).

## Out of scope

Sessions outliving the gg process; scrollback viewing/search and copy mode in
the console; more than one console on screen; the web attach (designed for,
not built); CLI/MCP session verbs.

## Spike findings (2026-09-24)

Harness: Bubble Tea v1.3.10 + `x/xpty v0.1.4` + `x/vt v0.0.0-20260920004010-53e2afe73ae5`
(`SafeEmulator`), the pumpIn-queue design, driven headless in a tmux PTY on
Linux/WSL.

| Check | Result |
|---|---|
| Claude Code starts (no stall → emulator query replies reach the child) | PASS |
| Claude trust prompt: ↓ + enter navigate its menu | PASS |
| Prompt typed + enter → streamed answer renders (`PONG`) | PASS |
| Colours: 24-bit SGR from bash, Claude's styled UI, box-drawing | PASS |
| Resize 120×40 → 80×30 re-lays out Claude's UI | PASS |
| Bracketed paste of 3 lines lands as one multi-line input (not submitted) | PASS |
| esc reaches Claude (esc esc cleared the input) | PASS |
| `ctrl+]` intercepted by the host, never reaches the child | PASS |
| Child gone after the host closes the PTY (SIGHUP) | PASS |
| vim: alt-screen, insert mode, `:q!` → exit status reported | PASS |
| Codex starts and renders its trust prompt | PASS |
| Windows ConPTY (`spike.exe cmd.exe`, run by the user in Windows Terminal) | PASS — cmd renders, typing works, bare ctrl sends nothing (v2), resize reaches the child (`mode con` 188×54), `ctrl+]` intercepted, Claude Code's start screen renders at two window sizes. One artifact: a stray `Claude Code` title left of the logo in the smaller window (likely ConPTY's reflowing repaint after a resize vs. the emulator's non-reflowing resize) |

Findings that change the plan:
1. **`x/vt` is kept** (no `vt10x` fallback needed). `SafeEmulator` suffices;
   it embeds `*Emulator`, so `Close` is reachable. It has no cursor-visibility
   getter — use the `CursorVisibility` callback.
2. **Dependency bump is forced**: `x/vt` needs `x/ansi v0.11.7`, which breaks
   the `x/cellbuf` pseudo-version Bubble Tea v1.3.10 pins — **`x/cellbuf
   v0.0.15` must be required explicitly**. The bump also moves
   `colorprofile` 0.4.2, `go-runewidth` 0.0.23, `go-colorful` 1.4.0,
   `x/term` 0.2.2, `x/sys` 0.47.0. `go build ./...` is clean; `./test.sh unit`
   has exactly two failures, both in `internal/tui/wide_glyph_test.go`: the
   new width tables measure ☰ (U+2630) as 2 cells (it was 1), so the
   `widthUnsafe` guard no longer needs to rewrite it — lipgloss now agrees
   with terminals. Only ☰ changed among 17 probed glyphs (● ◇ ↓ ▾ ✓ ✗ ⚠ ❯ ─
   ⏵ ☀ ♦ unchanged). Fix = swap the test fixtures to a still-width-1 symbol
   (☀ U+2600) in Plan 1 Task 1.
3. **Batched runes**: a `tea.KeyRunes` message can carry several runes (fast
   typing, tmux `send-keys`); send it with `SendText`, never rune-by-rune
   through `SendKey` with only `Runes[0]` (Plan 2 key mapping).
4. **Agents detect a host tmux through the inherited `TMUX` env var**
   (Claude printed a tmux scroll hint). Sessions should drop `TMUX` and
   `TMUX_PANE` from the child environment, since the console is not a tmux
   pane (Plan 1 Task 2).
5. **Windows: a bare modifier key-down arrives as `KeyRunes{0}`.** Bubble Tea
   v1's console reader (`key_windows.go`) filters only VK_SHIFT, so pressing
   Ctrl/Alt/Win alone yields a NUL rune, and the child echoed `^@` on every
   ctrl press. The console must drop NUL-only rune messages (Plan 2 key
   mapping; spike v2 does).
6. **Windows resize artifact.** ConPTY reflows and repaints its own buffer
   on resize; `x/vt` resizes without reflow, so stale cells can survive one
   frame. Plan 2's console task pins a resize → full-redraw check on Windows.
