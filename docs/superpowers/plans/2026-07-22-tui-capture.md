# `tui-capture` Headless TUI Harness Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A repo-root shell tool (`tui-capture.sh`) that drives gg's real Bubble Tea TUI under a headless tmux PTY and writes each rendered screen as plain text, plus a project skill documenting the workflow — so Claude can read TUI screens and use them as the source of truth for a web port.

**Architecture:** A single bash script launches `gg` in a detached tmux session at a fixed size, walks a keystroke script, and after each step polls `tmux capture-pane` until the frame settles, saving a numbered ANSI-stripped snapshot. No Go code is added; the script is a dev tool like `test.sh`/`build.sh`.

**Tech Stack:** bash, tmux, coreutils (`sed`, `head`, `mktemp`), and `go build` to produce the `gg` binary under test.

Spec: `docs/superpowers/specs/2026-07-22-tui-capture-design.md`.

## Global Constraints

- **Worktree:** ALL work happens in `/mnt/t/others/gigagit.worktrees/feat-tui-capture` on branch `feat/tui-capture`. Subagents start in the main checkout — `cd` to the worktree FIRST and verify: `git branch --show-current` must print `feat/tui-capture`. Write/Edit tools need the worktree's ABSOLUTE paths.
- **No Go code, no dependency changes:** `go.mod`/`go.sum` must not change. The only executable added is the bash script; the only Go interaction is `go build ./cmd/gg`.
- **Not gg product code:** nothing under `internal/` imports or references this tool. It is repo-root tooling + a `.claude/skills/` doc.
- **Runtime prerequisites:** `tmux` and `go` must be on PATH (both are, in the target environment). The script must fail fast with a clear message if `tmux` is missing.
- **Bash hygiene:** every script starts with `#!/usr/bin/env bash` and `set -euo pipefail`. Two mandatory gotchas: (1) NEVER use `(( x++ ))` for counters — under `set -e` it exits when the pre-increment value is 0; use `x=$((x + 1))`. (2) `settle` returns non-zero on its ceiling; ALWAYS call it as `settle N || <handle>`, never bare (bare non-zero under `set -e` exits).
- **Commits:** use `gg add <paths>` + `gg commit -m "..."` (dogfood). NEVER `gg add` a built `gg` binary or a temp snapshot dir. Every commit message ends with the two trailer lines shown in the commit steps.
- **After each task:** `bash -n tui-capture.sh` must pass (syntax). If `shellcheck` is available, run it; SC2086 on `for t in $1` is intentional (word-splitting tokens) and may be ignored — no other warnings.
- Snapshots and the built binary go to temp dirs and are NEVER committed.

---

### Task 1: `tui-capture.sh` core — launch, settle, initial capture

**Files:**
- Create: `tui-capture.sh` (repo root, executable)

**Interfaces:**
- Consumes: `tmux`, `go build ./cmd/gg`.
- Produces (later tasks rely on these): the shell functions `stable_sig`, `full_frame`, `settle <maxTenths>`, `write_snap <idx> <label>`, and the variables `SESSION`, `OUT`, `REPO`, `COLS`, `ROWS`, `GG`, `idx`. Snapshot files are named `snap-<NN>-<label>.txt` in `$OUT`.

- [ ] **Step 1: Write the failing acceptance check**

There is no unit-test framework for shell here; the "test" is running the tool and asserting on its output. First confirm the tool does not yet exist:

Run: `cd /mnt/t/others/gigagit.worktrees/feat-tui-capture && ./tui-capture.sh 2>&1 | head -1`
Expected: FAIL — `no such file or directory` (the script doesn't exist yet).

- [ ] **Step 2: Write the script**

Create `tui-capture.sh` at the worktree root:

```bash
#!/usr/bin/env bash
# tui-capture.sh — drive gg's real TUI under a headless tmux PTY and capture
# each rendered screen as plain text. Dev tool; not gg product code.
set -euo pipefail

usage() {
  cat <<'EOF'
Usage: tui-capture.sh [options] [keyscript]

Drive gg's TUI in a headless tmux session and write a plain-text snapshot of
each screen to an output directory.

Options:
  --repo <dir>     repository the TUI opens in (default: this repo)
  --size <CxR>     terminal size, cols x rows (default: 120x40)
  --out <dir>      snapshot output dir (default: a fresh temp dir)
  --gg <path>      gg binary to run (default: build from this repo)
  -h, --help       show this help

keyscript: steps separated by ';' or newlines. Each step is
  [label:] <tokens>
where tokens are keys/text sent after the previous screen settles:
  named keys: enter esc space tab up down left right bspace
  chords:     C-g C-t (ctrl), M-x (meta)
  literals:   any other token is typed as-is (".", "?", digits, "foo")
Example:  ./tui-capture.sh "menu: . ; nav: down down ; open: enter"
EOF
}

REPO="" SIZE="120x40" OUT="" GG="" KEYSCRIPT=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --repo) REPO="$2"; shift 2 ;;
    --size) SIZE="$2"; shift 2 ;;
    --out)  OUT="$2";  shift 2 ;;
    --gg)   GG="$2";   shift 2 ;;
    -h|--help) usage; exit 0 ;;
    --*) echo "tui-capture: unknown option: $1" >&2; usage >&2; exit 2 ;;
    *) KEYSCRIPT="$1"; shift ;;
  esac
done

command -v tmux >/dev/null 2>&1 || { echo "tui-capture: tmux is required" >&2; exit 1; }

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO="${REPO:-$SCRIPT_DIR}"
COLS="${SIZE%x*}"; ROWS="${SIZE#*x}"

if [[ -z "$GG" ]]; then
  GG="$(mktemp -d)/gg"
  echo "tui-capture: building gg ..." >&2
  ( cd "$SCRIPT_DIR" && go build -o "$GG" ./cmd/gg ) \
    || { echo "tui-capture: gg build failed" >&2; exit 1; }
fi

OUT="${OUT:-$(mktemp -d -t tui-capture.XXXXXX)}"
mkdir -p "$OUT"

SESSION="ggcap_$$"
cleanup() { tmux kill-session -t "$SESSION" 2>/dev/null || true; }
trap cleanup EXIT

# stable_sig: pane contents MINUS the last 2 lines. The app footer + status/
# spinner line are volatile (an animating "remote tags…" hint never stops),
# so masking them lets settle() converge. Used ONLY for settle comparison.
stable_sig() { tmux capture-pane -t "$SESSION" -p | sed 's/[[:space:]]*$//' | head -n -2; }

# full_frame: the whole pane, trailing whitespace trimmed. What we save.
full_frame() { tmux capture-pane -t "$SESSION" -p | sed 's/[[:space:]]*$//'; }

# settle: poll until the stable signature is unchanged between two polls, or
# the ceiling (argument, in 0.1s units) is reached. Returns 1 on ceiling.
# NOTE: counter uses x=$((x+1)); (( x++ )) would trip `set -e` at x=0.
settle() {
  local max="${1:-30}" prev cur tries=0
  prev="$(stable_sig)"
  while (( tries < max )); do
    sleep 0.1
    cur="$(stable_sig)"
    [[ "$cur" == "$prev" ]] && return 0
    prev="$cur"
    tries=$((tries + 1))
  done
  return 1
}

write_snap() { # idx label
  local idx="$1" label="$2" n
  printf -v n "%02d" "$idx"
  full_frame > "$OUT/snap-$n-$label.txt"
  echo "wrote $OUT/snap-$n-$label.txt"
}

# launch gg in a headless PTY sized COLSxROWS, opening REPO
tmux new-session -d -s "$SESSION" -x "$COLS" -y "$ROWS" -c "$REPO" "$GG"

# initial screen: repo open + first async loads → allow a longer ceiling (~7s)
settle 70 || echo "tui-capture: initial screen did not settle (captured anyway)" >&2
idx=0
write_snap "$idx" "init"

echo "tui-capture: $((idx + 1)) snapshot(s) in $OUT"
```

Make it executable: `chmod +x tui-capture.sh`.

- [ ] **Step 3: Verify syntax**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-tui-capture && bash -n tui-capture.sh && echo "syntax ok"`
Expected: `syntax ok`. If `command -v shellcheck` succeeds, also run `shellcheck tui-capture.sh` — only SC2086 (intentional, appears in Task 2) is acceptable.

- [ ] **Step 4: Run the acceptance check**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-tui-capture && OUTD=$(mktemp -d) && ./tui-capture.sh --out "$OUTD" >/dev/null 2>&1; ls "$OUTD"; grep -l "gigagit" "$OUTD"/snap-00-init.txt`
Expected: `snap-00-init.txt` is listed and the `grep -l` prints its path (the main screen contains the header "gigagit"). Then eyeball it: `sed -n '1,12p' "$OUTD"/snap-00-init.txt` should show the header + the `[Branches]`/`Commits` panels as readable box-drawn text.

If the initial screen is blank: raise the `settle 70` ceiling, or check `go build ./cmd/gg` succeeds standalone. Do not proceed until `snap-00-init.txt` is legible.

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-tui-capture
gg add tui-capture.sh
gg commit -m "feat(tooling): tui-capture.sh core — headless TUI launch + initial capture

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01HJ4EsSe6QUvrEADAwdC9HG"
```

---

### Task 2: keyscript walk — per-step keys + snapshot

**Files:**
- Modify: `tui-capture.sh` (add two functions + the walk block)

**Interfaces:**
- Consumes: `SESSION`, `OUT`, `idx`, `KEYSCRIPT`, and `settle`/`write_snap` from Task 1.
- Produces: `send_tokens <tokens>` (maps named keys/chords/literals to `tmux send-keys`), `sanitize <s>` (filename-safe label), and per-step snapshots `snap-<NN>-<label>.txt`.

- [ ] **Step 1: Write the failing acceptance check**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-tui-capture && OUTD=$(mktemp -d) && ./tui-capture.sh --out "$OUTD" "menu: ." >/dev/null 2>&1; ls "$OUTD"/snap-01-menu.txt 2>&1`
Expected: FAIL — `No such file or directory` (Task 1 only writes `snap-00-init`; the keyscript is ignored because the walk isn't implemented yet).

- [ ] **Step 2: Add the two functions**

In `tui-capture.sh`, directly AFTER the `write_snap` function and BEFORE the `# launch gg` comment, insert:

```bash
send_tokens() { # tokens (whitespace-separated); word-splitting is intentional
  local t
  for t in $1; do
    case "$t" in
      enter|Enter)      tmux send-keys -t "$SESSION" Enter ;;
      esc|escape|Esc)   tmux send-keys -t "$SESSION" Escape ;;
      space|Space)      tmux send-keys -t "$SESSION" Space ;;
      tab|Tab)          tmux send-keys -t "$SESSION" Tab ;;
      up|Up)            tmux send-keys -t "$SESSION" Up ;;
      down|Down)        tmux send-keys -t "$SESSION" Down ;;
      left|Left)        tmux send-keys -t "$SESSION" Left ;;
      right|Right)      tmux send-keys -t "$SESSION" Right ;;
      bspace|backspace) tmux send-keys -t "$SESSION" BSpace ;;
      C-*|M-*)          tmux send-keys -t "$SESSION" "$t" ;;   # ctrl/meta chords
      *)                tmux send-keys -t "$SESSION" -l "$t" ;; # literal: ".", "?", digits, words
    esac
    sleep 0.05
  done
}

sanitize() { echo "$1" | tr -cd '[:alnum:]_-'; }
```

- [ ] **Step 3: Add the walk block**

In `tui-capture.sh`, directly BEFORE the final `echo "tui-capture: $((idx + 1)) snapshot(s) in $OUT"` line, insert:

```bash
# walk the keyscript: steps separated by ';' or newline, each optionally
# "label: tokens". Send keys, wait for the screen to settle, snapshot.
if [[ -n "$KEYSCRIPT" ]]; then
  script="${KEYSCRIPT//;/$'\n'}"
  while IFS= read -r step; do
    step="${step#"${step%%[![:space:]]*}"}"   # ltrim
    step="${step%"${step##*[![:space:]]}"}"   # rtrim
    [[ -z "$step" ]] && continue
    idx=$((idx + 1))
    if [[ "$step" == *:* ]]; then
      label="$(sanitize "${step%%:*}")"
      tokens="${step#*:}"
    else
      label="step$idx"
      tokens="$step"
    fi
    [[ -z "$label" ]] && label="step$idx"
    send_tokens "$tokens"
    settle 30 || echo "tui-capture: step $idx ($label) did not settle (captured anyway)" >&2
    write_snap "$idx" "$label"
  done <<< "$script"
fi
```

- [ ] **Step 4: Verify syntax + acceptance**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-tui-capture && bash -n tui-capture.sh && echo "syntax ok"`
Expected: `syntax ok`.

Then: `OUTD=$(mktemp -d) && ./tui-capture.sh --out "$OUTD" "menu: ." >/dev/null 2>&1; ls "$OUTD"; grep -iE "filter|run|close" "$OUTD"/snap-01-menu.txt | head -3`
Expected: both `snap-00-init.txt` and `snap-01-menu.txt` exist, and the grep prints the action-menu footer (`[key]/[enter] run  [/] filter  [z] mode  [esc] close`), proving the `.` keystroke opened the menu and it was captured.

Also verify a multi-step script and literal typing work:
`OUTD2=$(mktemp -d) && ./tui-capture.sh --out "$OUTD2" "menu: . ; close: esc" >/dev/null 2>&1; ls "$OUTD2"`
Expected: `snap-00-init.txt`, `snap-01-menu.txt`, `snap-02-close.txt`.

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-tui-capture
gg add tui-capture.sh
gg commit -m "feat(tooling): tui-capture keyscript walk — per-step keys + snapshots

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01HJ4EsSe6QUvrEADAwdC9HG"
```

---

### Task 3: the skill + demo + CLAUDE.md pointer

**Files:**
- Create: `.claude/skills/driving-tui-headless/SKILL.md`
- Modify: `CLAUDE.md` (one line in the Build/test section)

**Interfaces:** none (documentation + the acceptance demo).

- [ ] **Step 1: Run the demo tour (acceptance)**

Capture a scripted tour against the gg repo itself and confirm every snapshot is legible:

```bash
cd /mnt/t/others/gigagit.worktrees/feat-tui-capture
OUTD=$(mktemp -d)
./tui-capture.sh --out "$OUTD" "menu: . ; close: esc ; commits: tab ; drill: enter ; back: esc"
ls "$OUTD"
for f in "$OUTD"/snap-*.txt; do echo "=== $f ==="; sed -n '1,6p' "$f"; done
```
Expected: six snapshots (`00-init` … `05-back`), each showing readable box-drawn panels. This proves the harness reads real screens. (The exact panel a `tab`/`enter` lands on depends on gg's current focus model; the point is that each capture is legible, not the specific screen.) Note the resolved `$OUTD` for your report; do NOT commit these snapshots.

- [ ] **Step 2: Write the skill**

Create `.claude/skills/driving-tui-headless/SKILL.md`:

```markdown
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

## Keyscript

Steps separated by `;` or newlines; each is `[label:] tokens`. Tokens sent
after the previous screen settles:

- **named keys:** `enter esc space tab up down left right bspace`
- **chords:** `C-g` (ctrl+g), `C-t`, `M-x` (meta)
- **literals:** anything else is typed as-is — `.` (opens the action menu),
  `?` (help), digits, or a word like `foo` (typed into a filter field)

Example — open the action menu, close it, focus commits, drill in:
```bash
./tui-capture.sh "menu: . ; close: esc ; commits: tab ; drill: enter"
```
A literal text token has no spaces (spaces separate tokens); multi-word
field input isn't supported — send the words as separate tokens.

## How settling works (and its one caveat)

After each step the script polls `capture-pane` until the frame is unchanged
between two polls (or a ceiling: ~7s for the first screen, ~3s per step).
For the comparison it **masks the bottom two lines** — gg's footer + status
line hold an animating `⟳ remote tags…` hint that never stops, so without the
mask nothing would ever settle. The saved snapshot is still the FULL frame.
If a step logs `did not settle (captured anyway)`, an element kept animating;
the snapshot is valid, just taken at the ceiling.

## Preparing repo state

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

- Requires `tmux` and `go` on PATH.
- One tmux session per run (unique name); concurrent runs don't collide.
- The built `gg` binary and the snapshot dir are temporary — never commit them.
```

- [ ] **Step 3: Add the CLAUDE.md pointer**

In `CLAUDE.md`, find the `## Build / test` section (the fenced block listing `go build ./cmd/gg` and `./test.sh`). Directly AFTER that fenced code block, add:

```markdown
To read the TUI headlessly (no terminal/VM) — e.g. when porting a panel to
another frontend — `./tui-capture.sh "<keyscript>"` drives the real `gg` under
a tmux PTY and writes a plain-text snapshot per screen. See the
`driving-tui-headless` skill.
```

- [ ] **Step 4: Full gate**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-tui-capture && ./test.sh 2>&1 | tail -3`
Expected: `all green` — no Go code changed, but this confirms nothing was disturbed.

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-tui-capture
gg add .claude/skills/driving-tui-headless/SKILL.md CLAUDE.md
gg commit -m "docs(tooling): driving-tui-headless skill + CLAUDE.md pointer

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01HJ4EsSe6QUvrEADAwdC9HG"
```

---

## After the plan

The harness is the tool that unblocks porting TUI panels to the web-native
UI: capture a panel's screen, enumerate its data/actions/states, build the
web equivalent, and self-verify parity — surfacing only aesthetic questions
to the human. That porting work is separate follow-on, per the spec's
non-goals.
