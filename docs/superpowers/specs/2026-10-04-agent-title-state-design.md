# Agent title & progress as a session-state signal — design

Date: 2026-10-04 · Branch: `feat/agent-title-state` · Follows the agent
orchestration minors batch (main `dc0ce1ce`).

## Goal

A running agent's row reads **working from the start of a turn to its end**
— no idle flicker between two steps — and turns **idle about 1 s after the
turn ends** instead of 2 s, for the agents that announce their state in the
terminal title or the OSC 9;4 progress report. Questions and the agent's own
menus still come from the screen. A hung call still becomes stalled.
Surfaces (rows, notices, web badges, `gg agent list`, `agent_wait` events)
are unchanged: only their input gets better.

Reference: herdr (`~/others/herdr`, `src/detect/manifests/{claude,codex}.toml`,
`src/pane/agent_detection.rs`) ranks the title spinner as working above every
screen rule and the idle title as a low fallback; this design adopts that
order.

## Live capture (2026-10-04, raw bytes under `script`, env as gg gives it)

| Agent | While working | When idle | During a question |
|---|---|---|---|
| Claude Code 2.1.289 | OSC 0 `◐ <topic>` / `◑ <topic>`, alternating ~1/s through thinking, tool calls and a 6 s Bash call | `✳ <topic>` | `✳ <topic>` (stops spinning) |
| Codex 0.160.0 | OSC 0 `⠋ <task> \| repo` (braille, ~200 ms) | `<task> \| repo` (no glyph) | not seen (herdr: title contains `Action Required`) |
| Kimi Code 2.1.1 | OSC 9;4 `4;3` | OSC 9;4 `4;0` | not seen (the turn failed on a 403) |
| Junie 26.9.22, agy 1.2.16 | nothing usable (plain title / none) | — | — |

Claude's bundle (2.1.289): `title = (isAnimating && !underMux ? ["◐","◑"][frame] : "✳") + " " + text`;
`underMux` = `TMUX`, `STY` or `ZELLIJ` in its environment (flag
`tengu_static_title_under_mux`, default on); frames advance every 960 ms
only while the terminal has focus — the glyph stays ◐ without it. Claude
≤ 2.1.227 used braille frames (herdr). No agent emitted OSC 9;4 except Kimi.

## Design

### 1. Capture — `internal/agentsession`

- `oscFilter` already walks every escape sequence before x/vt sees it. It
  also records, from the UNfiltered bytes:
  - the payload of the last **OSC 0 or OSC 2** (window title), full UTF-8,
    capped at **256 bytes** (bytes beyond the cap are dropped; a cut UTF-8
    tail is trimmed to a rune boundary);
  - the state of the last **OSC 9;4** (`9;4;<state>[;<value>]`), state 0–4.
  It buffers only those two kinds: the OSC number is read first, and any
  other payload (OSC 8 links, OSC 52 clipboard data) is never accumulated.
  A sequence split across reads is captured the same; a cancelled one
  (CAN/SUB) records nothing. The bytes handed to the emulator are
  byte-identical to today.
- `Session.Signals() Signals` returns `{Title string; Progress int}` under
  a mutex: `Progress` is −1 until an OSC 9;4 arrives.
- `childEnv` also drops `STY` and every `ZELLIJ*` variable, as it drops
  `TMUX` today: the agent's terminal is gg's console, and under a
  multiplexer Claude freezes its title at "✳".

### 2. Rules — `internal/agentstate` (built-in only)

`Rules` gains `TitleWorking`, `TitleQuestion`, `TitleIdle []*regexp.Regexp`
and `ProgressBusy bool` (OSC 9;4 states 1 and 3 = working, 0 = idle; 2
error and 4 paused say nothing). No config lists: a tool's `screen_*`
config replaces only the screen lists; `domain.SessionRules` carries the
title rules from the defaults the way it carries `Own`.

| agent id | TitleWorking | TitleQuestion | TitleIdle | ProgressBusy |
|---|---|---|---|---|
| claude | `^[◐◑◒◓⠀-⣿] ` | — | `^✳ ` | no |
| codex | `(?:^\| )[⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏](?: \|$)` | `Action Required` | `\S` when neither of the others matches | no |
| kimi | — | — | — | yes |
| others | — | — | — | no |

A new `Signal{Title string; Progress int; Trusted bool}` (Trusted = the
guard below) and
`ClassifyWith(r Rules, lines []string, sig Signal) (st State, titleIdle bool)`
apply, first match wins (herdr's order):

1. title question → Question
2. title working, or progress busy → Working
3. the screen, unchanged: Working → Waiting → Own (last line) → Question
An idle title (or progress 0) never decides the state — it only confirms
an idle the screen shows (review ruling, 2026-10-04: Claude titles ANY
waiting screen "✳" — a picker opened mid-turn, a dialog the question rules
miss — so an unreadable screen under "✳" stays Unknown and the session
keeps its state; a false idle would let a parent type into it).

`titleIdle` is true when a trusted title/progress says idle (the watcher
uses it for the hold). `Classify(r, lines)` stays as
the screen-only form (`ClassifyWith` with an empty signal).

**Guard:** a session's title-idle counts only once the same session has
shown a title-working (or progress-busy) signal at least once. An agent
whose title never animates (a multiplexer gg did not strip, an older
version, a user's `CLAUDE_CODE_DISABLE_TERMINAL_TITLE`) keeps today's
screen-only behaviour, 2 s hold included.

### 3. Watcher — `internal/domain/session_states.go`

- `stateSource` gains `Signals(id) (agentsession.Signals, bool)`; `observe`
  reads it with the screen (before taking `w.mu`, like the screen).
- The watcher keeps `animated map[SessionID]bool` (set on the first
  title-working/progress-busy read; dropped with the session) and passes
  it as `Signal.Trusted`.
- **Idle hold:** a working session that reads Waiting still goes through
  `pendingIdle`, but the hold is **700 ms** when the read is title-idle
  (`titleSettle`) and `idleSettle` (2 s) otherwise. `Since` stays the
  first idle read. A title-working read clears a pending idle, as a screen
  Working does today.
- `SessionActivity` gains `Settle time.Duration` — the hold the current
  idle passed (0 for other states). `agent_wait` trusts an idle once
  `now − Since ≥ Settle` (falling back to `idleSettle` when 0), so it wakes
  ~700 ms after a titled turn ends.
- **Stalls unchanged:** title frames are output, as the spinner's timer
  already is; the 10-minute "progress unchanged while working" rule
  (`spinStallAfter`) still catches a hung call, the 2-minute no-output rule
  a frozen process.
- `UseTitleSettle(d)` test hook beside `UseIdleSettle`.

### 4. Not in scope

Showing the title text (Claude's topic) anywhere; config lists for title
rules; Junie/agy/generic signals; OSC 9;4 for Claude/Codex (they send none).

## Behaviour summary

| Situation | Before | After |
|---|---|---|
| Claude between two tool calls (screen spinner gone briefly) | working (2 s hold hides it) | working (title spins) |
| Claude turn ends | idle after 2 s | idle after ~0.7–1 s |
| Claude permission dialog | question | question (title "✳", screen decides) |
| Claude under an unstripped mux (static "✳") | screen-only | screen-only (guard) |
| Codex turn / end | screen-only, 2 s | title-working / idle after 0.7 s |
| Codex "Action Required" title | screen rules | question |
| Kimi turn / end | screen-only | progress-busy / idle after 0.7 s |
| Junie, agy, generic | screen-only | unchanged |
| hung tool call, title spinning | stalled after 10 min | unchanged |

## Testing (test-first)

- `oscFilter`: captured byte slices (Claude ◐/◑/✳ titles, Codex braille and
  plain titles, Kimi `9;4;3` / `9;4;0`) — whole, split at every byte
  boundary across two reads; BEL and ESC-\ terminators; the 256-byte cap
  on a rune boundary; an OSC 52 payload is never recorded; CAN cancels;
  the emulator output equals the pre-change filter's output for the same
  input.
- `childEnv`: `STY`, `ZELLIJ`, `ZELLIJ_SESSION_NAME` dropped.
- `agentstate`: table over captured titles × screens: dialog under "✳" →
  question; static "✳" + screen spinner → working; "◐" + empty prompt box
  → working; "✳" + unknown screen → unknown; guard off → screen-only;
  Codex `Action Required` → question; Kimi progress 3/0.
- `domain`: `SessionRules` with custom screen lists keeps title rules;
  watcher with a fake source — a turn whose screen drops the spinner stays
  working; titled idle shows after 700 ms with `Since` = first read;
  untitled idle after 2 s; never-animated "✳" keeps 2 s; `agent_wait`
  wakes 700 ms after a titled idle.
- Live: a real Claude in a gg console (own tmux session, every screen read
  before keys): the row stays working through a multi-tool turn and turns
  idle ~1 s after it ends.
