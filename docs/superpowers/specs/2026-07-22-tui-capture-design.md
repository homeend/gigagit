# `tui-capture` — headless TUI screen-capture harness — design

Date: 2026-07-22 · Status: approved · Branch: `feat/tui-capture`

## Goal

Give Claude (and any future session) a repeatable way to drive gg's real
Bubble Tea TUI **without a terminal, VM, or display**, capture any rendered
screen as plain text, and read it back. The captured screens become the
**source of truth** for porting TUI panels/popups to the web-native UI —
letting Claude self-verify *data and behavior parity* against the TUI and
reducing human review from "check every screen" to "occasionally judge the
web version's visual feel."

Proven feasible: a smoke test ran the real `gg` binary in a detached tmux
pseudo-terminal at a fixed size and `tmux capture-pane -p` returned clean,
fully readable screens (panels, lane graph, footer keybindings), and
`tmux send-keys .` opened the action-menu popup with every row legible.

## What this does and does not deliver

**Delivers:** the ability to see *what a screen shows* — every field, label,
state, keybinding, and layout — as text. This is the load-bearing input for
a faithful web port and for Claude self-checking parity.

**Does not deliver:** a judgment of the web UI's *aesthetic quality*. The web
frontend is a different renderer (rich DOM, not a character grid), so
"does it look good / read well" remains a human spot-check — but an
occasional one, not a screen-by-screen correctness pass.

## Non-goals (scope guards)

- **Not the actual web port.** This harness is the tool that *unblocks*
  porting; porting each panel is separate follow-on work.
- **Not the in-process Go Model-drive route.** gg's tests already drive the
  `Model` via `Update(keyMsg(...))` + `ansi.Strip(View())` for unit
  assertions. This harness deliberately uses the **real binary under a real
  PTY (tmux)** because it needs the true rendered screen (real async loads,
  real wrapping, real refresh) — higher fidelity for *reading* screens.
- **Not a golden-file regression suite.** No committed screen fixtures, no
  CI diffing. That could come later; this build is "Claude can drive and
  read screens on demand."
- **Not gg product code.** It ships as a dev tool (sibling of
  `build.sh`/`test.sh`), never imported by any `internal/` package.

## Components

### 1. `tui-capture.sh` (repo root)

A shell script wrapping the tmux lifecycle. Sibling of `test.sh`/`build.sh`.

**Invocation:**
```
./tui-capture.sh [--repo <dir>] [--size <COLSxROWS>] [--out <dir>] [--gg <path>] <keyscript>
```
- `--repo <dir>` — repository the TUI opens in. Default: the gg repo the
  script lives in (self-hosting, always present).
- `--size <COLSxROWS>` — terminal size. Default `120x40`.
- `--out <dir>` — snapshot output directory. Default a fresh temp dir under
  the system temp root; the script prints the resolved path.
- `--gg <path>` — the `gg` binary to run. Default: build one from the repo
  (`go build -o <tmp>/gg ./cmd/gg`) so captures reflect the current tree.
- `<keyscript>` — the step sequence (see format below).

**Behavior, step by step:**
1. Resolve/build the `gg` binary; resolve repo, size, out dir.
2. `tmux new-session -d -s <session> -x COLS -y ROWS -c <repo> <gg>`
   (unique session name so concurrent runs don't collide).
3. Wait for the **initial** screen to settle, then capture `snap-00-init.txt`.
4. For each step in the keyscript: send the keys, wait for the frame to
   settle, capture `snap-NN-<label>.txt`.
5. `tmux kill-session` (always, even on error — trap-based cleanup).
6. Print the out dir and the list of snapshot files.

**Settle detection (the core reliability mechanism):** after sending a step's
keys, poll `capture-pane -p` on a short interval; when two consecutive
captures are byte-identical (or a max-wait ceiling is hit), the frame is
considered settled and captured. This replaces fixed `sleep`s and absorbs
async domain loads and self-stopping spinners/blinks. A max-wait ceiling
(default ~5s) prevents a perpetually-animating element (e.g. a spinner that
never stops) from hanging the run — on ceiling, capture anyway and note it.

**Capture format:** `capture-pane -p` (plain text, ANSI already stripped by
tmux), trailing whitespace trimmed per line. Each snapshot is a `.txt` file
named `snap-<NN>-<label>.txt`; `NN` is the step index, `<label>` comes from
the keyscript step (default `stepNN` if unlabeled).

### 2. Keyscript format

A newline-or-`;`-separated list of steps. Each step is a token sequence sent
via `tmux send-keys`, optionally prefixed with a `label:`.

- **Named keys** map to tmux/bubbletea key names: `enter`, `esc`, `space`,
  `up`/`down`/`left`/`right`, `tab`, `C-g` (ctrl+g), `C-t`, `?`, digits.
- **Literal text** is sent as-is for typing into fields (e.g. a filter query).
- A step may combine several keys: `menu: .` , `nav: down down` ,
  `open: enter`.

The skill documents the exact token vocabulary and how it maps to gg's
bindings; the script passes tokens through to `send-keys` (which already
understands the `C-x` / named-key conventions), sending non-key literal
tokens with `send-keys -l`.

### 3. `.claude/skills/driving-tui-headless/SKILL.md`

The workflow doc so any session can reproduce and extend captures. Contents:
- When to use (understanding a TUI screen; sourcing a web port; debugging a
  panel's rendered state).
- How to invoke `tui-capture.sh` with a keyscript; how to read the snapshots.
- The keyscript token vocabulary and the settle model (why it exists).
- Conventions: default size/repo, where snapshots land, one-session-per-run.
- Gotchas: WSL/tmux specifics; a spinner that never settles → max-wait
  ceiling; choosing a repo with the state you need (a conflict, a paused op)
  by preparing it first; the harness reads *rendered output* (the "what"),
  while interaction *semantics* (what a key does in a rare state) still come
  from source/tests.
- The porting recipe: capture the TUI screen → enumerate its data/actions/
  states from the text → build/verify the web equivalent carries the same →
  flag only aesthetic questions to the human.

## Data flow

```
keyscript ─▶ tui-capture.sh ─▶ tmux (PTY) ─▶ real gg binary ─▶ rendered frames
                    │                                              │
                    └────────── capture-pane (settle) ◀───────────┘
                    │
                    ▼
        snap-NN-<label>.txt  ─▶  Claude reads  ─▶  web-port source-of-truth
```

## Error handling

- **tmux missing:** fail fast with a clear message (the harness depends on
  tmux; it is present in the target environment).
- **gg build fails:** surface the build error, do not start a session.
- **Session never renders (blank frames):** after the max-wait ceiling on the
  initial capture, save whatever was captured and report that the screen
  never settled (likely a repo-open error) — do not hang.
- **Cleanup:** a shell `trap` kills the tmux session on any exit path so a
  failed run never leaves an orphaned session.
- **Concurrent runs:** unique session names + per-run out dirs, so two
  captures don't interfere.

## Testing

- The harness is a dev tool exercised by **running it**, not by a Go unit
  test. Acceptance = the demo below produces readable snapshots.
- **Demo (the acceptance check):** capture a short scripted tour of the gg
  TUI against the gg repo itself — main screen, action menu (`.`), a panel
  switch, a commit drill-in, and a diff — and confirm each snapshot is
  legible. This doubles as the skill's worked example.
- No e2e scenario (the e2e harness asserts CLI state, not a live TUI loop).
- `./test.sh` must stay green (this change adds no Go code, but the final
  gate confirms nothing was disturbed).

## Deliverables

1. `tui-capture.sh` at repo root (executable).
2. `.claude/skills/driving-tui-headless/SKILL.md`.
3. A committed set of demo snapshots (or the demo commands + a short README
   in the skill) proving the harness reads real screens.
4. Doc touch: a one-line pointer in `CLAUDE.md`'s dev-workflow/build-test
   section noting `tui-capture.sh` exists (kept minimal — it is tooling,
   not architecture).

## Workflow

Worktree `feat/tui-capture` off `main` (already created). Spec committed
there; the human merges after review.

## Future doors (not built now)

- Golden-file TUI regression tests (committed reference frames + a diff runner).
- The in-process Model-drive capture path for deterministic unit-level frames.
- xterm.js streaming of the same PTY frames to a browser (the "TUI in a
  browser" `gg web --tty` idea) — the same headless-frame pipeline, a
  different consumer.
