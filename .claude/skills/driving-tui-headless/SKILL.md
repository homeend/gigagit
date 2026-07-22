---
name: driving-tui-headless
description: Use when you need to SEE gg's terminal TUI — driving panels/popups, reading what a screen renders, sourcing or verifying a web port against the TUI, or debugging a panel's on-screen state — without a real terminal, VM, or display. Runs the real gg binary under a headless tmux PTY and captures each screen as plain text.
---

# Driving the gg TUI headlessly

gg's TUI is Bubble Tea: `Model → Update(msg) → View() string`. It needs a
terminal only to *display*, not to *run*. `tui-capture.sh` (repo root) runs
the real `gg` binary in a headless **tmux** pseudo-terminal, sends a
keystroke script, and writes each rendered screen to a plain-text snapshot
you can read.

## When to use

- Understand exactly what a panel or popup shows (fields, labels, states,
  keybindings, layout) before changing or porting it.
- Source of truth for a **web port**: capture the TUI screen, enumerate its
  data/actions/states from the text, build the web equivalent, and verify it
  carries the same — flag only aesthetic questions to the human.
- Debug a panel's rendered state for a specific repo condition (a conflict, a
  paused op, a filtered feed).

## Invoke

```bash
./tui-capture.sh [--repo <dir>] [--size <CxR>] [--out <dir>] [--gg <path>] "<keyscript>"
```

- `--repo` — repo the TUI opens in (default: this repo, always present).
- `--size` — cols x rows (default `120x40`).
- `--out` — snapshot dir (default: a fresh temp dir; the script prints it).
- `--gg` — a prebuilt `gg` to reuse (default: it builds one from the tree, so
  captures reflect the current code). Pass a prebuilt path for faster reruns.

Snapshots are `snap-<NN>-<label>.txt` in the out dir: `00-init` is the first
screen, then one per keyscript step.

## Recording a scenario (the input side)

To author a scenario instead of hand-writing a keyscript, a human runs the
TUI with `gg --record <file>`, drives it normally, and quits (`q`). gg writes
every keystroke to `<file>` in this exact keyscript format (one token per
line), with a `#` header naming the repo it was recorded against. The
terminating quit is not written. Hand that file straight to
`tui-capture.sh <file>` (via `--repo <the header's repo>`) to replay it and
capture a snapshot of every screen. Mouse clicks, alt-modified keys, and
page/function keys are not recorded (they appear as `# unrecorded key:`
comments); keep scenarios keyboard-driven with the vocabulary above.

## Keyscript

Steps separated by `;` or newlines; each is `[label:] tokens`. Tokens sent
after the previous screen settles:

- **named keys:** `enter esc space tab up down left right bspace`
- **chords:** `C-g` (ctrl+g), `C-t`, `M-x` (meta)
- **literals:** anything else is typed as-is — `.` (opens the action menu),
  `?` (help), digits, or a word like `foo` (typed into a filter field)

Example — open the action menu, close it, focus commits, drill in:
```bash
./tui-capture.sh "menu: . ; close: esc ; commits: right ; drill: enter"
```
From the default Branches focus, `right` moves focus directly to the Commits
panel (`tab` instead cycles the left-column tabs to Files); `enter` on the
selected commit then opens its Files/diff view.

A literal text token has no spaces (spaces separate tokens); multi-word
field input isn't supported — send the words as separate tokens. Where a key
lands depends on current focus/cursor, so confirm any keyscript by reading
its snapshots rather than assuming a label is accurate.

## How settling works (and its one caveat)

After each step the script polls `capture-pane` until the frame is unchanged
between two polls, non-blank, and past gg's `⏳`/`(loading…)` placeholders
(or a ceiling: ~7s for the first screen, ~3s per step). For the unchanged
comparison it **masks the bottom two lines** — gg's footer + status
line hold an animating `⟳ remote tags…` hint that never stops, so without the
mask nothing would ever settle. The saved snapshot is still the FULL frame.
If a step logs `did not settle (captured anyway)`, an element kept animating;
the snapshot is valid, just taken at the ceiling.

## Preparing repo state

⚠️ Keystrokes drive a REAL `gg` against `--repo` (default: this repo). A
keyscript that opens the action menu (`.`) and confirms can run **mutating
or destructive** git operations — stage, discard, checkout, merge, delete a
branch/worktree, or push. For anything beyond pure navigation, point
`--repo` at a throwaway/scratch clone, not your live checkout.

To capture a screen that needs a condition (a merge conflict, a paused
rebase, a specific branch checked out), set that state up first in a scratch
repo and point `--repo` at it. The harness only drives keys; it does not
create repo state.

## What it does and does not tell you

- It shows the **rendered output** — the *what* of a screen. That is the
  load-bearing input for a faithful web port and for checking data/behavior
  parity.
- It does NOT encode interaction **semantics** in edge states (what a key does
  in a rare condition). For that, read the TUI source (`internal/tui`) or its
  tests, which drive `Update(keyMsg(...))` + `ansi.Strip(View())` directly.
- It cannot judge a web UI's **aesthetic quality** — the web frontend is a
  different renderer (rich DOM, not a character grid). That stays a human
  spot-check, but an occasional one rather than screen-by-screen.

## Gotchas

- Requires `tmux` on PATH (and `go`, unless you pass a prebuilt `--gg`).
- One tmux session per run (unique name); concurrent runs don't collide.
- The built `gg` binary and the snapshot dir are temporary — never commit them.
