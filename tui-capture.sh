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

BUILD_DIR=""
if [[ -z "$GG" ]]; then
  BUILD_DIR="$(mktemp -d)"
  GG="$BUILD_DIR/gg"
  echo "tui-capture: building gg ..." >&2
  ( cd "$SCRIPT_DIR" && go build -o "$GG" ./cmd/gg ) \
    || { echo "tui-capture: gg build failed" >&2; rm -rf "$BUILD_DIR"; exit 1; }
fi

OUT="${OUT:-$(mktemp -d -t tui-capture.XXXXXX)}"
mkdir -p "$OUT"

SESSION="ggcap_$$"
cleanup() {
  tmux kill-session -t "$SESSION" 2>/dev/null || true
  [[ -n "$BUILD_DIR" ]] && rm -rf "$BUILD_DIR"
  return 0
}
trap cleanup EXIT

# stable_sig: pane contents MINUS the last 2 lines. The app footer + status/
# spinner line are volatile (an animating "remote tags…" hint never stops),
# so masking them lets settle() converge. Used ONLY for settle comparison.
stable_sig() { tmux capture-pane -t "$SESSION" -p 2>/dev/null | sed 's/[[:space:]]*$//' | head -n -2; }

# full_frame: the whole pane, trailing whitespace trimmed. What we save.
full_frame() { tmux capture-pane -t "$SESSION" -p 2>/dev/null | sed 's/[[:space:]]*$//'; }

# settle: poll until the stable signature is unchanged between two polls, or
# the ceiling (argument, in 0.1s units) is reached. Returns 1 on ceiling.
# NOTE: counter uses x=$((x+1)); (( x++ )) would trip `set -e` at x=0.
# A byte-identical-across-polls check alone false-positives twice on a real
# repo load: (1) the pane is genuinely empty for the first ~0.4s before gg's
# first frame paints, so "" == "" looks stable at try 0; (2) the app itself
# passes through stable-but-incomplete placeholders (the startup "(loading…)"
# banner, then per-panel "⏳" spinners while branches/commits/tags load) that
# can each hold steady well past one 0.1s tick. Both are the app *telling* us
# it isn't done, so "stable" also requires non-empty content with no visible
# loading marker — not just two matching polls.
settle() {
  local max="${1:-30}" prev cur tries=0
  prev="$(stable_sig)"
  while (( tries < max )); do
    sleep 0.1
    cur="$(stable_sig)"
    if [[ -n "$cur" && "$cur" == "$prev" ]] && ! grep -qE '⏳|\(loading' <<<"$cur"; then
      return 0
    fi
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

# launch gg in a headless PTY sized COLSxROWS, opening REPO
tmux new-session -d -s "$SESSION" -x "$COLS" -y "$ROWS" -c "$REPO" "$GG"

# initial screen: repo open + first async loads → allow a longer ceiling (~7s)
settle 70 || echo "tui-capture: initial screen did not settle (captured anyway)" >&2

if ! tmux has-session -t "$SESSION" 2>/dev/null; then
  echo "tui-capture: gg exited on launch (bad --repo, or gg failed to start)" >&2
  exit 1
fi

idx=0
write_snap "$idx" "init"

# walk the keyscript: steps separated by ';' or newline, each optionally
# "label: tokens". Send keys, wait for the screen to settle, snapshot.
if [[ -n "$KEYSCRIPT" ]]; then
  script="${KEYSCRIPT//;/$'\n'}"
  while IFS= read -r step; do
    step="${step#"${step%%[![:space:]]*}"}"   # ltrim
    step="${step%"${step##*[![:space:]]}"}"   # rtrim
    [[ -z "$step" ]] && continue
    # Skip comment lines. The recorder writes every comment as "# <text>"
    # (hash + space), so match that exactly — a lone "#" step is a real
    # literal '#' keystroke (gg binds # to goto-commit) and must NOT be
    # skipped, or that keystroke would silently vanish on replay.
    [[ "$step" == "# "* ]] && continue
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

echo "tui-capture: $((idx + 1)) snapshot(s) in $OUT"
