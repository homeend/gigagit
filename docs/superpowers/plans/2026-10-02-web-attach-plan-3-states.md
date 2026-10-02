# Web attach plan 3 — session states — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:executing-plans
> (native, this session — the repo forbids implementer subagents). Steps use
> checkbox (`- [ ]`) syntax for tracking. One read-only review subagent at
> the end.

**Goal:** gg knows what every agent session is doing — working, idle, needs
input, stalled — shows it on the session rows and console titles of the TUI
and the web page, tells the user once when a session needs them, and reports
it to agents through `agent_list` / `agent_screen`.

**Architecture:** a new DAG leaf `internal/agentstate` (a port of erbrus's
`internal/screen` classifier) reads the plain screen text. One process-global
watcher in `domain` (`SessionStates()`) classifies every running agent
session, keeps a `SessionActivity` per session, and keeps a numbered ring of
notices. Frontends subscribe through a `Broadcaster` and read a snapshot —
each with its own notice cursor, so the terminal and the page it hosts never
steal each other's wakeups.

**Tech stack:** Go 1.26, RE2 (`regexp`), the existing `agentsession`
broadcaster, the web live hub (`sessions` event), JS-in-Go pure-section
tests, Playwright for the browser check.

**Spec:** `docs/superpowers/specs/2026-09-28-web-attach-design.md`, section
"Session states (plan 3)" (+ rulings 8, the domain and testing sections).

**User decisions for this plan (2026-10-02):** states surface to agents +
TUI + web. `gg agent wait` and the report channel are the NEXT plan — not
here.

## Global constraints

- No implementer subagents; work in `/work/gigagit/.claude/worktrees/web-attach-states`
  (branch `feat/web-attach-states`); `gg add` + `git commit -F <msgfile>`;
  never `git add -A`; ask before merging.
- `web`, `mcp`, `tui`, `cli` never import `agentstate` or `agentsession` —
  only `domain` does (archtest); `agentstate` is a stdlib-only leaf.
- Every user-visible TUI string goes through `i18n.T` with a literal key in
  the ja/ko/zh/ru bundles (skill `adding-translations`).
- A new never-ending TUI command is gated by `m.quiet` (e2e golden screens).
- Nothing auto-answers a dialog. `Options` are carried, never pressed.
- Human labels: `working 7m` · `idle 3m` (never "waiting") · `needs input` ·
  `stalled · …`. Protocol values (wire, agents): `working` · `idle` ·
  `question` · `""`, plus a `stalled` flag.
- Timings: tail = last 15 non-empty lines; grace 30 s after start; stalled =
  no output for 120 s; watcher tick 2 s; output coalesce 300 ms.
- Gates before merge: `./test.sh` and `./test.sh race` — green only on the
  literal "all green" line. After merge: `./build.sh install`, `./build.sh web`.
- Browser checks assert visibility and are seen failing on the unfixed build.
- Deferred minors of plan 2 stay untouched.

## Rulings made while planning (spec text vs. the code as it is)

1. **`SessionActivity`, not `SessionState`.** `domain.SessionState` is already
   the lifecycle alias (running/exited). The new record is
   `domain.SessionActivity`; the wire field stays `agent_state` (reserved in
   plan 1).
2. **No `Events()` channel.** One shared channel is what `Broadcaster`
   replaced (a hosted page and the terminal would steal each other's events).
   The watcher exposes `Subscribe()` (a broadcaster), `Get`/`All` snapshots
   and `Notices(after uint64)` — a numbered ring; each frontend keeps its own
   cursor.
3. **Agent ids are gg's:** `claude`, `codex`, `junie`, `kimi`, `""` generic.
   `antigravity` (binary `agy`) has no verified rules in erbrus → generic. A
   domain test pins that every session-capable `exttool.Builtins()` id
   resolves to a rule set.
4. **The watcher wakes on output.** The manager's signal fires only on
   start/exit/remove, so the watcher subscribes to each running session,
   coalesces 300 ms, then reads `Text()`. The 2 s tick serves the stall clock.
5. **Rules are bound at start.** `Info` has no command; config changes later
   and sessions span repos. A command with any `screen_*` list is compiled
   when the session starts and bound to its id; everything else uses
   `DefaultRules(AgentID)`. A custom command (no agent id) is classified only
   when it has bound lists. Terminals are never classified.
6. **No ANSI port.** `Session.Text()` is already plain cells; erbrus's
   `ansi.go` (`Strip`, `ToHTML`) is not ported.
7. **The badge is immediate, the notice waits.** A state shows as soon as it
   is classified. Notices are held for the first 30 s; a question still
   standing when the grace ends is posted then (a trust dialog at start is
   exactly what the user must hear about); an idle-after-working inside the
   grace is never posted.
8. **Stall from "unknown" only with dedicated rules.** A session on the
   generic rules stalls only from `working` — otherwise every idle agent gg
   has no rules for would be called stalled after two minutes.
9. **Invalid `screen_*` pattern:** the built-in rules apply and the start
   says so once (TUI status line / web toast). No config-wide warning system
   exists; none is added.
10. **`screen_*` lists are not part of `ToolFingerprint`** (tuning rules is
    not editing the template) and survive a template upgrade: block rendering
    writes them, and `ReplaceToolCommandIf` carries the old block's lists
    over when the new block has none.
11. **Row text.** Sub-rows are narrow: a known state REPLACES `running 12m`
    (`└ ● Claude  idle 3m`). The popup row and the console title are wide:
    they APPEND it (`running 12m · idle 3m`). An unknown state leaves today's
    text.
12. **A notice is not posted to a frontend whose focused console shows that
    session** — the user is looking at it.
13. **Agents:** `agent_list` rows gain `activity`, `activity_since`,
    `stalled`; `agent_screen` gains `activity`, `options`. `state` keeps its
    meaning (running/exited — already protocol). The registry listing
    (`gg agent list` outside gg) is unchanged.
14. **`StepFor` is carried, not displayed.** Labels tick from `Since` on the
    client; a per-second `StepFor` change never wakes subscribers.

## Review focus

1. A phrase in scrollback ("Do you want to proceed", "(y/n)") while the
   prompt box is on screen → must read idle, never needs input.
2. A wide glyph (kimi's moon spinner, a radio glyph) — `Text()` emits a
   space for its right half, which tmux captures never had; rules and
   cursor-column math must still hold.
3. A session that exits, is removed, or whose manager is swapped mid-watch —
   no leaked subscription goroutine, no state left behind, no panic.
4. A repo switch (`reRoot`) and a hosted page — the watcher survives, both
   frontends see the same state, each gets each notice exactly once.
5. A template upgrade of a `[[tools.command]]` block with tuned `screen_*`
   lists — the lists are still there afterwards.

## File map

| File | Role |
|---|---|
| `internal/agentstate/classify.go`, `cursor.go`, `doc.go` (new) | `State`, `Rules`, `DefaultRules`, `HasDefaults`, `Compile`, `Tail`, `Classify`, `StepDuration`, `Option`, `Options`, `CursorOptions`, `DialogOptions` |
| `internal/agentstate/*_test.go` (new) | erbrus fixtures + gg-captured ones |
| `internal/agentsession/session.go` | `LastOutput()` |
| `internal/config/tools.go`, `tools_replace_block.go`, `template.go` | `screen_working/screen_waiting/screen_question` on `ToolCommand`, render, carry-over, settingDocs |
| `internal/domain/session_states.go` (new) | `SessionActivity`, `ActivityNotice`, `StateWatcher`, `SessionStates()`, `SessionActivityOf`, `SessionRules`, `SessionRulesWarning`, timing seams |
| `internal/domain/sessions.go` | bind rules at start; `UseSessionManager` resets the watcher |
| `internal/domain/agentverbs.go`, `internal/mcp/agenttools.go`, `internal/cli/agent.go` | activity for agents |
| `internal/web/sessions_http.go`, `live.go`, `session_lifecycle.go` | wire fields, notices on the `sessions` event, start warning |
| `internal/web/static/core.js` (pure `activityLabel`, `noticeText`), `sidebar.js`, `openfiles.js`, `console.js`, `live.js`, `style.css` | badges, toasts |
| `internal/tui/session_activity.go` (new), `worktree_sessions.go`, `sessions_popup.go`, `console.go`, `model.go`, `help.go` | badges, status notices |
| `internal/archtest` | `agentstate` leaf rule |
| Docs | CHANGELOG, README, CLAUDE.md (map row), docs/CLAUDE-details.md, `agentskill/using-gg.md` (+ `Version` 130) |

---

### Task 1: `agentstate` — the classifier leaf

**Files:** create `internal/agentstate/{doc.go,classify.go,cursor.go,classify_test.go,cursor_test.go}`; modify the archtest leaf list.

**Interfaces — produces:**

```go
type State string
const ( Unknown State = ""; Working State = "working"; Waiting State = "waiting"; Question State = "question" )
type Rules struct{ Working, Waiting, Question []*regexp.Regexp }
func DefaultRules(agentID string) Rules   // unknown id → the generic set
func HasDefaults(agentID string) bool     // false for "" and unknown ids
func Compile(working, waiting, question []string) (Rules, error)
func Tail(text string, n int) []string
func Classify(r Rules, lines []string) State
func StepDuration(lines []string) time.Duration
type Option struct {
	Key     string `json:"key"`
	Label   string `json:"label"`
	Pick    bool   `json:"pick,omitempty"`
	Current bool   `json:"current,omitempty"`
}
func Options(lines []string) []Option
func CursorOptions(raw string) []Option
func DialogOptions(raw string, lines []string) []Option
```

- [ ] **Step 1: write the tests first.** Copy
  `/mnt/t/others/erbrus/internal/screen/classify_test.go` and `cursor_test.go`
  verbatim into the new package with these edits only: package name;
  `DefaultRules("claude-code")` → `DefaultRules("claude")`; drop
  `TestClassifyStripsBeforeMatching` (no `Strip`). Add:

```go
// Junie and kimi rules come from erbrus's agent presets (live captures).
func TestClassifyJunieAndKimi(t *testing.T) {
	j := DefaultRules("junie")
	if got := Classify(j, Tail("⠹ Running tests esc to stop\n> Type your prompt...\n~ /work/x\n", 15)); got != Working {
		t.Errorf("junie working: %q", got)
	}
	if got := Classify(j, Tail("Done.\n> Type your prompt...\n~ /work/x\n", 15)); got != Waiting {
		t.Errorf("junie idle: %q", got)
	}
	if got := Classify(j, Tail("    Junie needs your trust decision\n  → Trust this project\n    Keep untrusted\n", 15)); got != Question {
		t.Errorf("junie question: %q", got)
	}
	k := DefaultRules("kimi")
	// gg's Text() puts a space after a wide glyph (its right-half cell).
	if got := Classify(k, Tail("🌒  Thinking\n", 15)); got != Working {
		t.Errorf("kimi working (wide glyph): %q", got)
	}
	if got := Classify(k, Tail("│ > \n╰──────╯\n", 15)); got != Waiting {
		t.Errorf("kimi idle: %q", got)
	}
}

func TestHasDefaultsAndGenericFallback(t *testing.T) {
	for _, id := range []string{"claude", "codex", "junie", "kimi"} {
		if !HasDefaults(id) {
			t.Errorf("%s has no rules", id)
		}
	}
	if HasDefaults("") || HasDefaults("antigravity") {
		t.Error("generic ids must not claim dedicated rules")
	}
	if got := Classify(DefaultRules("antigravity"), Tail("continue? (y/n)", 5)); got != Question {
		t.Errorf("generic fallback: %q", got)
	}
}

// A wide cursor glyph shifts the text column by its phantom cell; every
// sibling shifts with it, so the list is still read.
func TestCursorOptionsWideMarker(t *testing.T) {
	got := CursorOptions("Pick one\n▶  Yes\n   No\n")
	if len(got) != 2 || !got[0].Current || got[1].Label != "No" {
		t.Fatalf("options = %+v", got)
	}
}
```

- [ ] **Step 2: run** `rtk go test ./internal/agentstate/` → FAIL (package has no code).
- [ ] **Step 3: port.** Copy `classify.go` and `cursor.go` from erbrus.
  Edits: package doc in `doc.go` ("what an agent's screen says it is doing;
  pure, stdlib only"); the `defaults` map keyed `claude`, `codex`, `junie`,
  `kimi`, `""`, with junie's and kimi's lists copied from
  `erbrus/internal/agents/agents.go` (`^[⠋-⠿] ` / `^>[^\n]*\n~ ` / the six
  junie question strings; `^[🌑🌒🌓🌔🌕🌖🌗🌘] `, `Retrying \(\d+/\d+\)` /
  `^│ >[^\n]*\n╰` / `Trust this folder\?`, `Enter select`, `Esc exit`);
  `HasDefaults("")` is false (`id != ""` guard); drop `Patterns` and
  `MatchKind` (erbrus's settings page, unused here); `CursorOptions` reads
  `raw` directly (no `Strip`); error prefix `agentstate pattern %q`.
- [ ] **Step 4: run** the package tests → PASS. If the kimi wide-glyph or the
  wide-marker case fails, fix the rule/column math (not the test) — that is
  review-focus item 2.
- [ ] **Step 5: archtest.** Add `agentstate` to the leaf list the way
  `agentsession`/`branchfilter` are listed; `rtk go test ./internal/archtest/` → PASS.
- [ ] **Step 6: commit** `feat(agentstate): classify an agent's screen (port of erbrus screen)`.

### Task 2: `LastOutput` and the `screen_*` config lists

**Files:** `internal/agentsession/session.go` (+ test), `internal/config/tools.go`, `tools_replace_block.go`, `template.go` (+ tests). Use skill `adding-config-entries`.

**Interfaces — produces:**

```go
// agentsession
func (s *Session) LastOutput() time.Time // zero before the first chunk
// config.ToolCommand gains
ScreenWorking  []string `toml:"screen_working"`
ScreenWaiting  []string `toml:"screen_waiting"`
ScreenQuestion []string `toml:"screen_question"`
func (tc ToolCommand) HasScreenRules() bool
```

- [ ] **Step 1: failing tests.**
  - `agentsession`: a scripted child that prints one line → `LastOutput()`
    is non-zero and not before the start; a session that printed nothing →
    zero (use the package's existing scripted-child helper).
  - `config`: (a) a TOML block with the three lists decodes into the fields;
    (b) `RenderToolCommand` writes them as `screen_working = ["…", "…"]`
    (TOML basic strings via `%q`) before `command`, and writes nothing for
    empty lists; render → parse round-trips patterns holding `\(`, `"` and
    non-ASCII glyphs; (c) `ToolFingerprint` is equal with and without lists;
    (d) `ReplaceToolCommandIf` on a block with lists, given a `tc` without
    any → the rewritten block still has the old lists; given a `tc` with its
    own lists → the new ones win; (e) a settingDocs row exists for each key
    (extend the existing settingDocs coverage test's expectations).
- [ ] **Step 2: run** → FAIL.
- [ ] **Step 3: implement.** `LastOutput` returns
  `time.Unix(0, s.lastOut.Load())`, zero when the stamp is 0 (`lastOut` is
  already stored by `pumpOut`). Config: the fields; `HasScreenRules`;
  `renderToolCommand` writes non-empty lists; in `ReplaceToolCommandIf`,
  after the old block is located and decoded (the fingerprint check already
  decodes it), copy its three lists into `tc` when `!tc.HasScreenRules()`.
  `%q` is valid TOML only for what Go and TOML escape alike — the round-trip
  test in (b) is the guard; if a glyph breaks it, write the strings with a
  small `tomlString` helper (`\\`, `\"`, control chars as `\uXXXX`).
  settingDocs: extend the `tools.command` doc with "session commands accept
  `screen_working`, `screen_waiting`, `screen_question`: RE2 pattern lists,
  matched in that order against the last 15 non-empty screen lines (`^`/`$`
  bound a line, `\n` spans two); set any and all three come from config; an
  invalid pattern falls back to gg's built-in rules for that agent".
- [ ] **Step 4: run** `rtk go test ./internal/agentsession/ ./internal/config/` → PASS.
- [ ] **Step 5: commit** `feat(config): screen_* rule lists on session commands; Session.LastOutput`.

### Task 3: `domain.SessionStates()` — the watcher

**Files:** create `internal/domain/session_states.go`, `session_states_test.go`; modify `internal/domain/sessions.go`.

**Interfaces — consumes:** Task 1's `agentstate`, Task 2's `LastOutput`, `HasScreenRules`. **Produces:**

```go
type (
	ActivityState  = agentstate.State
	ActivityOption = agentstate.Option
)
const (
	ActivityUnknown  = agentstate.Unknown
	ActivityWorking  = agentstate.Working
	ActivityIdle     = agentstate.Waiting // the wire and the labels say "idle"
	ActivityQuestion = agentstate.Question
)

type SessionActivity struct {
	State   ActivityState
	Since   time.Time        // when State was entered
	StepFor time.Duration    // the spinner's own timer, 0 when none
	Stalled bool
	Options []ActivityOption // question only
}
// Name is the protocol value: "working" | "idle" | "question" | "".
func (a SessionActivity) Name() string

type ActivityNotice struct {
	Seq   uint64
	ID    SessionID
	Kind  string        // "question" | "idle" | "stalled"
	Label string        // the session's label
	Dir   string        // its worktree
	Quiet time.Duration // stalled: how long nothing was printed
}

type StateWatcher struct{ /* unexported */ }
func SessionStates() *StateWatcher                      // starts it on first use, bound to Sessions()
func SessionActivityOf(id SessionID) (SessionActivity, bool) // never starts the watcher
func (w *StateWatcher) Get(id SessionID) (SessionActivity, bool)
func (w *StateWatcher) Subscribe() (<-chan struct{}, func())
func (w *StateWatcher) NoticeSeq() uint64
func (w *StateWatcher) Notices(after uint64) []ActivityNotice // oldest first, ring of 64
func (w *StateWatcher) Bind(id SessionID, r agentstate.Rules)

func SessionRules(tc config.ToolCommand) (r agentstate.Rules, custom bool, err error)
func SessionRulesWarning(tc config.ToolCommand) string // "" when the lists compile (or there are none)
func UseStateTiming(grace, stall time.Duration) func() // tests: returns the restore
```

Internals (unexported, exact shape):

```go
// stateSource is what the watcher reads; the manager adapter implements it
// and the tests fake it.
type stateSource interface {
	List() []agentsession.Info
	Text(id agentsession.ID) (string, bool)
	LastOutput(id agentsession.ID) time.Time
}

var (
	stateGrace = 30 * time.Second
	stallAfter = 120 * time.Second
	stateTick  = 2 * time.Second
	stateCoalesce = 300 * time.Millisecond
)

// observe classifies every running session once. The goroutine calls it;
// tests call it directly with a fake source and a chosen now.
func (w *StateWatcher) observe(now time.Time)
```

`observe`, per running session with rules (`rulesFor`: a bound set; else
skip a terminal; else skip an empty agent id; else `DefaultRules(AgentID)`):

```go
text, ok := w.src.Text(info.ID)
if !ok { continue }
lines := agentstate.Tail(text, 15)
st := agentstate.Classify(rules, lines)
prev := w.states[info.ID]
next := prev
switch st {
case agentstate.Question:
	next.Options = agentstate.DialogOptions(text, lines)
case agentstate.Unknown: // mid-redraw: keep what we had
default:
	next.Options = nil
}
trusted := now.Sub(info.Started) >= stateGrace
if st != agentstate.Unknown && st != prev.State {
	next.State, next.Since = st, now
	changed = true
	switch {
	case st == agentstate.Question && trusted:
		w.note(info, "question", 0)
	case st == agentstate.Question:
		w.pendingQ[info.ID] = true
	case st == agentstate.Waiting && prev.State == agentstate.Working && trusted:
		w.note(info, "idle", 0)
	}
}
if w.pendingQ[info.ID] && trusted {
	delete(w.pendingQ, info.ID)
	if next.State == agentstate.Question {
		w.note(info, "question", 0)
	}
}
next.StepFor = agentstate.StepDuration(lines)
last := w.src.LastOutput(info.ID)
dedicated := bound || agentstate.HasDefaults(info.AgentID)
stalled := !last.IsZero() && now.Sub(last) > stallAfter &&
	(next.State == agentstate.Working || (next.State == agentstate.Unknown && dedicated))
if stalled != prev.Stalled {
	changed = true
	if stalled {
		w.note(info, "stalled", now.Sub(last))
	}
}
next.Stalled = stalled
if !optionsEqual(prev.Options, next.Options) { changed = true }
w.states[info.ID] = next
```

After the loop: delete the state, pending mark and bound rules of every id
that is no longer running (`changed = true` when a state existed); `if
changed { w.bc.Signal() }`. All under `w.mu`; `note` appends to the ring
with `w.seq++`.

The goroutine (`run`): subscribe to the manager; keep one forwarding
goroutine per running session (`sess.Subscribe()` → a non-blocking send on a
shared `dirty` channel; ends on its `stop` channel, then cancels the
subscription); loop on `stop` / manager wake (re-sync the per-session
subscriptions, `observe`) / `dirty` (arm one 300 ms timer; `observe` when it
fires) / the 2 s ticker (`observe`).

`sessions.go`: bound rules live in a package-level map guarded by
`sessionsMu` (`boundRules map[SessionID]agentstate.Rules`) — written by
`startSessionPrompt`, read by `rulesFor`, pruned by `observe` — so binding
never depends on the watcher having started. `Bind` writes that map, and
`startSessionPrompt` binds after a successful start:

```go
if r, custom, err := SessionRules(tc); custom && err == nil {
	bindRules(sess.Info().ID, r)
}
```

`UseSessionManager` stops the current watcher, clears it and the map, and
its restore does the same (the next `SessionStates()` binds the manager then
current).

- [ ] **Step 1: failing tests** (`session_states_test.go`, fake source, no
  PTY, `t.Parallel()` where no global is touched). Screens are the Task 1
  fixtures (a small local `idleScreen`, `workingScreen`, `questionScreen`
  for `claude`):
  1. unknown text after idle → state and `Since` unchanged, no wakeup;
  2. working → idle past the grace → exactly one `idle` notice; a second
     `observe` posts nothing;
  3. question past the grace → a `question` notice at once, `Options` has
     the three numbered choices; leaving the question clears `Options`;
  4. question inside the grace → no notice; first `observe` after the grace
     with the question still up → one notice; with the question gone → none;
  5. working → idle inside the grace → never a notice;
  6. `LastOutput` 121 s old while working → `Stalled`, one `stalled` notice
     with `Quiet` ≥ 120 s; output resumes → `Stalled` false, no notice;
  7. stall from unknown: agent `claude` → stalled; agent `antigravity`
     (generic) → not stalled; a custom command with bound rules → stalled;
  8. an exited session's state is dropped and subscribers are woken;
  9. terminals and custom commands without bound rules are never classified;
  10. bound rules win over the built-ins (a `claude` session whose bound
      waiting rule is `^READY$`);
  11. `Notices(after)` returns only newer ones, oldest first, and the ring
      keeps the last 64;
  12. `SessionRules`: no lists → `custom=false`; one list set → all three
      from config (the two unset are empty); a bad pattern → error,
      `SessionRulesWarning` names the command and the pattern;
  13. every `exttool.Builtins()` tool that has a `session` template yields a
      non-empty `DefaultRules(id)` (waiting or question list non-empty);
  14. real manager (one PTY test): start `sh -c 'printf "$ "; sleep 30'` as
      agent id `""` with bound generic rules → within 3 s
      `SessionActivityOf` says idle (proves the per-session subscription
      wake, not the tick: set `stateTick` to an hour for this test);
      `UseSessionManager` restore leaves no forwarding goroutine (the
      session's `SubscriberCount()` returns to its pre-watch value).
- [ ] **Step 2: run** `rtk go test ./internal/domain/ -run 'SessionState|SessionRules|Activity'` → FAIL (undefined).
- [ ] **Step 3: implement** as specified above.
- [ ] **Step 4: run** the same tests, then `rtk go test -race ./internal/domain/ -run 'SessionState|SessionRules|Activity'` → PASS.
- [ ] **Step 5: commit** `feat(domain): SessionStates watcher — activity, notices, bound rules`.

### Task 4: activity for agents (`agent_list`, `agent_screen`, CLI, skill)

**Files:** `internal/domain/agentverbs.go`, `internal/mcp/agenttools.go`, `internal/cli/agent.go`, `internal/agentskill/using-gg.md`, `agentskill.go` (+ tests).

**Interfaces — produces:**

```go
// AgentEntry gains
Activity      string    `json:"activity,omitempty"`       // working | idle | question
ActivitySince time.Time `json:"activity_since,omitzero"`
Stalled       bool      `json:"stalled,omitempty"`
// AgentScreen becomes
func AgentScreen(target string) (AgentScreenResult, error)
type AgentScreenResult struct {
	State, Text, Activity string
	Options []ActivityOption
}
// agentScreenOut gains `activity`, `options`
```

- [ ] **Step 1: failing tests.** domain: with a watcher holding a question
  state for a session (install via the fake-source watcher from Task 3),
  `AgentList` carries `activity:"question"` and `AgentScreen` carries the
  options; an unclassified session has neither. cli: `gg agent list` prints
  `…  running  idle  Claude  /wt` (the activity word after the state, absent
  when unknown, `stalled` appended when set); `gg agent screen <id>` prints
  a first line `activity: question (1. Yes · 2. No)` before the text only
  when an activity is known; `--json` carries the new fields. mcp: the tool
  result schema test (if one pins the output shape) updated.
- [ ] **Step 2: run** → FAIL. **Step 3: implement** (`AgentList` and
  `AgentScreen` read `SessionActivityOf` — they never start the watcher; the
  TUI that hosts the channel does). Update every `AgentScreen` caller.
- [ ] **Step 4: skill.** `using-gg.md`: under `agent_list` / `agent_screen`
  add the activity fields, their values and "a `question` means the worker
  waits for a decision — read the options from `agent_screen`"; bump
  `agentskill.Version` to 130; the skill's version-marker test passes.
- [ ] **Step 5: run** `rtk go test ./internal/domain/ ./internal/cli/ ./internal/mcp/ ./internal/agentskill/` → PASS.
- [ ] **Step 6: commit** `feat(agent): agent_list and agent_screen report what a session is doing`.

### Task 5: web server — wire fields and notices

**Files:** `internal/web/sessions_http.go`, `live.go`, `session_lifecycle.go`, `server.go` (start/stop of the watch) (+ tests).

**Interfaces — produces (wire):**

```json
{"id":"s1","state":"running","agent_state":"idle","since":"…","step_for":0,"stalled":false,
 "options":[{"key":"1","label":"Yes"}]}
```

`liveMsg` gains `Notices []noticeWire` (`json:"notices,omitempty"`):
`{"id","kind","label","worktree","quiet_s"}`. `POST /api/session-start`'s
reply gains `warning` (the `SessionRulesWarning` text, omitted when empty).

- [ ] **Step 1: failing tests.** (a) `sessionsWire` fills `agent_state`,
  `since`, `stalled`, `options` from a watcher state and leaves them out for
  an unclassified session (JSON has no `agent_state` key); (b) a state
  change on the watcher produces a `sessions` hub message while an op holds
  the gate (the existing gate-bypass test's shape); (c) a notice reaches the
  hub exactly once, on the message that follows it, and a server started
  after a notice was posted does not replay it (cursor starts at
  `NoticeSeq()`); (d) `session-start` with an invalid `screen_working`
  pattern starts the session and returns `warning`; (e) `Close` stops the
  watch goroutine.
- [ ] **Step 2: run** → FAIL. **Step 3: implement.** `sessionsWire` takes a
  lookup `func(domain.SessionID) (domain.SessionActivity, bool)` (the server
  passes `domain.SessionActivityOf`; tests pass a map). `New` calls
  `domain.SessionStates()` (the page needs it running) and starts
  `watchSessionStates(stop)` next to `watchSessions`: its own subscription;
  on a wake, `fanOut(liveMsg{Reason:"sessions", Sessions: s.sessionsNow(),
  Notices: <new since cursor>})`.
- [ ] **Step 4: run** `rtk go test ./internal/web/ -run 'Session|Live'` → PASS.
- [ ] **Step 5: commit** `feat(web): session activity and notices on the wire`.

### Task 6: web page — badges and toasts

**Files:** `internal/web/static/core.js`, `sidebar.js`, `openfiles.js`, `console.js`, `live.js`, `sessions.js`, `style.css`; tests `sidebarjs`/`consolejs`/`switcherjs`/core JS-in-Go tests.

**Interfaces — produces (pure, in `core.js`, exported):**

```js
// activityLabel: "working 7m" | "idle 3m" | "needs input" | "stalled · working 7m"
// | "stalled · no output" | "" (unknown, not stalled). ages tick from s.since.
function activityLabel(s, now)
// activityAttn: true for needs input and stalled — the attention colour.
function activityAttn(s)
// noticeText: "claude in wt needs your input" | "… finished its turn — idle"
// | "… has printed nothing for 2m — stalled?"
function noticeText(n)
```

- [ ] **Step 1: failing JS-in-Go tests.** `activityLabel` for each state,
  exited (→ ""), stalled with and without a state; `noticeText` for the
  three kinds (worktree shown by its last path segment, `quiet_s` 125 →
  `2m`); `worktreeSessionRows`: a classified session's `meta` is the
  activity label and the row has `attn:true` for question/stalled, an
  unclassified one keeps `running 12m`; switcher `agentRows`: `meta` is
  `<age> · <label>`; `consoleTitle`: `… · running 12m · idle 3m`, unchanged
  when unknown. Wiring string tests: `live.js` toasts `msg.notices` through
  `noticeText` and skips a notice whose `id` is the focused console's
  (`consoleFocusedId()` exported by `console.js`); `sessions.js` toasts a
  start reply's `warning`.
- [ ] **Step 2: run** → FAIL. **Step 3: implement.** Rows add class `attn`
  when `activityAttn`; `style.css`: `li.wsess.attn .wpath`, `.ofrow.attn
  .meta`, `#console-label .act.attn` use the existing warn colour token (the
  one the task glyph uses, `#f2a65a`, as a `--act-attn` variable); the
  console title wraps the activity in `<span class="act">`. The console's
  1 s title tick already repaints ages — the activity age rides it; the
  sidebar and switcher repaint on every `sessions` event and on their
  existing age refresh.
- [ ] **Step 4: run** `rtk go test ./internal/web/ -run 'JS|js'` → PASS.
- [ ] **Step 5: commit** `feat(web): session rows and the console title show what an agent is doing`.

### Task 7: TUI — badges and status notices

**Files:** create `internal/tui/session_activity.go` (+ test); modify `worktree_sessions.go`, `sessions_popup.go`, `console.go`, `model.go`, `headless.go`, `help.go`, the four i18n bundles. Skills: `adding-translations`, `adding-notifications` (read it; the status line is the surface the spec names).

**Interfaces — produces:**

```go
// activityText: "working 7m" / "idle 3m" / "needs input" / "stalled · working 7m"
// / "stalled · no output"; "" when unknown and not stalled.
func activityText(a domain.SessionActivity, now time.Time) string
func activityAttn(a domain.SessionActivity) bool
// activityNoticeText: the status-line sentence for one notice.
func activityNoticeText(n domain.ActivityNotice) string
type sessionActivityMsg struct{}
func (m Model) waitActivityCmd() tea.Cmd       // nil when m.quiet or no watch
func (m Model) onSessionActivity() (Model, tea.Cmd)
```

i18n keys (all four bundles): `working %s`, `idle %s`, `needs input`,
`stalled · %s`, `no output`, `%s in %s needs your input`,
`%s in %s finished its turn — idle`,
`%s in %s has printed nothing for %s — stalled?`,
`screen rules of %s are invalid — the built-in rules apply`.

- [ ] **Step 1: failing tests.** `activityText`/`activityAttn` table;
  `sessionRowBody` with a watcher state (install a fake-source watcher) →
  `└ ● Claude  idle 3m`, attention style applied for needs input / stalled
  (assert the styled segment through the test theme the neighbours use);
  `sessionStateText` → `● Claude  running 12m · needs input`; `consoleTitle`
  likewise; `onSessionActivity`: a `question` notice sets `statusMsg` to the
  translated sentence, a notice for the session whose console is focused is
  skipped, two notices in one wake show the newest, the cursor advances (a
  second call with no new notice leaves `statusMsg`); `waitActivityCmd` is
  nil in quiet mode; starting a command with a bad pattern sets the warning
  status (the start leaf's existing test fixture). Run the i18n AST gates.
- [ ] **Step 2: run** → FAIL. **Step 3: implement.** `Model` gets
  `actWatch *activityWatch` (the `sessionWatch` shape: the subscription
  lives on a pointer so it survives the value copy and `reRoot`, cancelled
  where `sessWatch` is) and `actSeq *uint64`, both created in `New` only
  when not quiet (`New` then calls `domain.SessionStates()`); `Init` batches
  `waitActivityCmd()`; `Update` routes `sessionActivityMsg`. Rows read
  `domain.SessionActivityOf`. The attention style is `st().attnWarn`'s
  colour as a foreground (add `activityAttn lipgloss.Style` to `styles.go`
  from the same theme role — no new theme role). `help.go`: one row under
  the sessions section — "a session row shows what the agent is doing:
  working, idle, needs input, stalled".
- [ ] **Step 4: run** `rtk go test ./internal/tui/ -run 'Activity|SessionRow|ConsoleTitle|I18n|i18n'` then the whole `./internal/tui/` package and `./test.sh e2e` (golden screens must not move: quiet mode never starts the watcher) → PASS.
- [ ] **Step 5: commit** `feat(tui): session rows, the popup and the console title show what an agent is doing`.

### Task 8: prove it on real screens, browser check, docs

**Files:** `internal/web/attach_browser_test.go`, scratchpad Playwright script, `internal/agentstate/*_test.go` (new live fixtures), CHANGELOG.md, README.md, CLAUDE.md, docs/CLAUDE-details.md, memory.

- [ ] **Step 1: browser check fixture.** `TestAttachBrowserHost` additionally
  starts a second session with `AgentID: "claude"`, label `Claude`, running
  a `sh` script that draws a Claude-shaped screen and changes it on input:

```sh
printf '────────────────────\n❯ \n'; read l
printf '\033[2J\033[H Do you want to proceed?\n ❯ 1. Yes\n   2. No\n Esc to cancel\n'; read l
printf '\033[2J\033[H────────────────────\n❯ \n'; read l
```

  and wraps the run in `domain.UseStateTiming(0, 2*time.Second)` (no grace,
  a 2 s stall) so notices are observable.
- [ ] **Step 2: Playwright**, first against the installed (unfixed) `gg`
  build's page — every assertion below must FAIL there — then against the
  branch: the `Claude` sub-row is visible and reads `idle`; send Enter to
  the session (`/api/session-input`) → the row reads `needs input`, has the
  attention colour (computed style differs from the plain row's), and a
  toast containing `needs your input` is visible; open the console → the
  title contains `needs input`; `ctrl+\` → the Agents row contains it.
- [ ] **Step 3: live agents** (my own tmux session `claude-states`, the
  worktree's `bin/gg` rebuilt first; never the shared server): for each of
  `claude`, `codex`, `junie`, `kimi`, `agy` that is installed, start it from
  the TUI in a scratch repo with `GG_SESSION_TRACE` set, and record through
  `./tui-capture.sh` snapshots what the sub-row says at: the trust/start
  dialog, the idle prompt, a running step ("count to 30 slowly with a shell
  sleep"), the next idle. Every mismatch becomes a fixture test in
  `agentstate` (the screen text captured from gg's `agent_screen`) and a
  rule fix, RED → GREEN. Agents that are not installed are listed in the
  final report as unverified (their rules stay erbrus's).
- [ ] **Step 4: docs.** CHANGELOG (the feature, the config lists, the agent
  fields); README (session states on rows, the notices, `screen_*`);
  CLAUDE.md package map: one `agentstate` row + `domain` row mention of
  `SessionStates()`; docs/CLAUDE-details.md (classification order, grace,
  stall, bound rules, notice ring, rulings 1–14); memory
  `agent-sessions-feature.md` + `agent-orchestration-feature.md` (stage 3a
  done; next = `gg agent wait` + report channel).
- [ ] **Step 5: gates.** `./test.sh` and `./test.sh race` → the literal
  "all green" line in each log.
- [ ] **Step 6: commit** docs + fixtures; then the final read-only review
  subagent (most capable model) over the whole branch with the Review focus
  list; one fix pass; then ASK the user before merging.

## Self-review

- Spec coverage: classifier + rules + `StepDuration` + `DialogOptions` (T1);
  `LastOutput`, config lists + settingDocs (T2); watcher rules — unknown
  debounce, grace, stall, exited dropped, config override (T3); labels and
  attention colour on sub-rows, popup rows, console titles, both frontends
  (T6, T7); one notice per transition on the TUI status line, the web toast
  and the `sessions` hub event (T5–T7); `Options` on the wire (T5) and for
  agents (T4); re-verification against live screens (T8); docs incl. the
  `agentstate` map row (T8).
- Deliberate departures from the spec text are rulings 1, 2, 3, 7, 8, 9, 11,
  12, 14 above.
- Not in this plan: `gg agent wait`, the report channel, one-click answers,
  queued typing, a rule tester in Settings.
