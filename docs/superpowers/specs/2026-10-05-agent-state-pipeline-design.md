# Agent state pipeline — design (refactor, no behaviour change)

Date: 2026-10-05 · Branch: `refactor/agent-state-pipeline` · Follows the
title-state signal (main `af0d458d`).

## Goal

"What state is this agent in?" is answered today in four places with no
single entry and no shared interface: `agentstate` (≈10 loose functions +
four per-agent maps), `StateWatcher.observe` (157 lines, five parallel
per-session maps), and `agent_wait` (its own settle rule). After this
refactor:

- **One entry:** `StateWatcher.observe` — every rule that decides an
  agent's state is one hop away from it, behind a named type.
- **One fixed interface** where several pieces do the same job:
  `agentstate.Detector`, implemented by the screen, title and progress
  parts and composed per agent.
- **One table** says what each agent uses (`agentstate/agents.go`).
- **One output:** `SessionActivity`; consumers (TUI, web, agent verbs,
  `agent_wait`) keep no state rules of their own.
- **No behaviour change.** Every existing watcher, `agent_wait` and
  classifier expectation holds; only mechanical renames in tests.

User ruling (2026-10-05): approach A — a `Detector` interface + a concrete
per-session tracker; interfaces only where there are several
implementations.

## Pipeline

```
agentsession (screen, title, progress, last output)
        │ stateSource.Observe(id) → agentstate.Observation        (fixed type)
        ▼
agentstate.Profile.Read(obs) → agentstate.Reading                 (fixed type, pure)
   └ Profile.Detector: Detector interface — First(Title…, ProgressReport, Screen…)
        ▼
domain.sessionTracker.Step(reading, info, lastOut, now, timing)   (per session)
   → SessionActivity + notices + recheck
        ▼
StateWatcher (states map, notice ring, subscriptions)  → TUI · web · agent verbs · agent_wait
```

## 1. `agentstate` — pure

### Types

```go
// What gg observed of one session at one moment.
type Observation struct {
	Text     string   // the screen (dialog options read the raw text)
	Lines    []string // Tail(Text, 15)
	Title    string   // last OSC 0/2 title
	Progress int      // last OSC 9;4 state, −1 none
}

// What a detector concludes. Unknown = no verdict.
type Verdict struct {
	State    State
	IdleHint bool // a non-screen source says idle (may shorten the hold)
	Spinning bool // a non-screen source says working (feeds the trust guard)
}

type Detector interface{ Read(Observation) Verdict }

// What a profile concludes: the verdict plus the agent-agnostic extras.
type Reading struct {
	Verdict
	Options  []Option      // DialogOptions(Text, Lines) when State == Question
	StepFor  time.Duration // StepDuration(Lines)
	Progress string        // Progress(Lines): the tail without spinner/timer
}

// An agent's way of being read.
type Profile struct {
	Detector  Detector
	Dedicated bool // knows this agent's screens (not the generic set): the stall-from-Unknown rule
}
func (p Profile) Read(o Observation) Reading
```

### Parts (each its own small file)

| Part | Reads | Verdict |
|---|---|---|
| `Screen{Working, Waiting, Own, Question []*regexp.Regexp}` | `Lines`, today's `Classify` order: Working → Waiting → Own (last line → Unknown) → Question | the state, no hints |
| `Title{Working, Question, Idle}` | `Title` | Question; Working (+`Spinning`); idle → Unknown + `IdleHint` |
| `ProgressReport{}` | `Progress` | 1/3 → Working (+`Spinning`); 0 → Unknown + `IdleHint` |
| `First(parts…)` | combinator | the first part whose `State` is not Unknown decides; parts before it contribute their hints (OR); parts after it are not consulted. A decisive non-screen part therefore carries `IdleHint=false`, as today. |

`IdleHint` is the raw signal; the tracker decides whether it is trusted.

### The table — `agentstate/agents.go`

```go
var agents = map[string]Profile{
	"claude":      {First(Title(claudeTitle), Screen(claudeScreen)), true},
	"codex":       {First(Title(codexTitle), Screen(codexScreen)), true},
	"kimi":        {First(ProgressReport{}, Screen(kimiScreen)), true},
	"junie":       {Screen(junieScreen), true},
	"antigravity": {Screen(agyScreen), true},
	"":            {Screen(genericScreen), false},
}
func ForAgent(id string) Profile          // unknown ids → the generic profile
func Known(id string) bool                // replaces HasDefaults
func WithScreen(id string, working, waiting, question []string) (Profile, error)
```

`WithScreen` is the config path (`screen_*` lists): it compiles the given
lists, fills each empty list from the agent's built-in screen (today's
per-list merge), keeps the built-in `Own`, and swaps that screen into the
agent's composition — title and progress parts stay built-in. For an
unknown id it is a bare `Screen` with only the given lists. Result is
`Dedicated: true` (bound rules are dedicated today).

The current pattern lists, comments and capture notes move into this file
unchanged. `Classify`, `ClassifyWith`, `SignalState`, `Signal`, `Rules`,
`DefaultRules`, `Compile`, `HasDefaults`, `titleRules`, `progressAgents`,
`ownMenus`, `defaults` are removed once nothing uses them. `Tail`,
`StepDuration`, `Progress`, `DialogOptions`, `Options`, `CursorOptions`
stay (agent-agnostic helpers).

## 2. `domain` — the tracker and the watcher

### `sessionTracker` (new file `session_tracker.go`)

Per session; replaces the watcher's parallel maps `pendingQ`,
`pendingIdle`, `progress`, `animated` (and owns the session's
`SessionActivity`, which the watcher publishes in `states`).

```go
type sessionTracker struct {
	dedicated   bool
	act         SessionActivity
	pendingIdle time.Time    // first idle read while working (zero: none)
	pendingQ    bool         // a question seen inside the grace, not yet announced
	progress    progressMark // spinner-only stall clock
	animated    bool         // a non-screen source has said working (trust guard)
}

type stateTiming struct{ grace, stall, spinStall, idleSettle, titleSettle time.Duration }

// Step folds one reading into the session's activity. It returns the new
// activity, the notices to post (unnumbered), and how soon a pending idle
// wants another look (0: none).
func (t *sessionTracker) Step(rd agentstate.Reading, info agentsession.Info, lastOut, now time.Time, tm stateTiming) (SessionActivity, []ActivityNotice, time.Duration)
```

`Step` carries today's `observe` body for one session, rule for rule:
trust (`Spinning` → `animated`; hold = `titleSettle` iff `IdleHint &&
animated`, else `idleSettle`); pending idle (Since = first idle read);
Unknown keeps the state (and its Options); grace + the delayed question
notice; idle notice; both stall rules (no output for `stall` while working
or Unknown-and-dedicated; spinner-only progress for `spinStall` while
working); `StepFor`; Options.

### `StateWatcher` (session_states.go) — the entry

- `stateSource` becomes `List() []agentsession.Info` +
  `Observe(id) (agentstate.Observation, time.Time /*lastOut*/, bool)`;
  `managerSource.Observe` reads the screen text, `Tail`, the session's
  `Signals()` and `LastOutput()`.
- `observe(now)`: snapshot `stateTiming`; for each running session with a
  profile (`profileFor`: the bound profile, else `ForAgent` for a known
  agent id; terminals and custom commands without a bound profile are not
  classified): `Observe` + `profile.Read` outside `w.mu`; then under
  `w.mu` the session's tracker `Step`s, the activity goes to `states`,
  notices are numbered and posted; trackers of sessions no longer running
  are dropped. Returns the minimum recheck.
- `Bind(id, agentstate.Profile)`; the rule store holds profiles.
  `SessionRules(tc)` → `SessionProfile(tc) (Profile, custom bool, error)`
  via `agentstate.WithScreen`; `SessionRulesWarning` unchanged in text.
- Unchanged: the run loop, the promote timer, `Get`, `Subscribe`,
  notices, `PostNotice`, `Wake`, `NewStaticStates`, the test hooks
  (`UseStateTiming`, `UseIdleSettle`, `UseTitleSettle`).

## 3. Output and `agent_wait`

- `SessionActivity.Settle` → **`ReadyAt time.Time`**: idle `Since + hold`
  (the hold it passed), question and working `Since`. Set by the tracker.
- `agent_wait`: an idle is delivered once `now ≥ ReadyAt` (zero `ReadyAt`,
  e.g. a static watcher in tests → `Since + idleSettle`); the freshness
  rule (`Since` after the caller's last input) stays — it is about the
  caller, not detection. `idleHold` is removed.

## Non-goals

No new signals, no timing changes, no new config, no frontend change, no
change to notices or the wire. `agentstate` stays a stdlib-only DAG leaf;
frontends still never import it (domain aliases unchanged).

## Testing

1. Existing `TestStates*`, `TestAgentWait*`, `TestSessionRules*`,
   `TestEverySessionAgentHasRules` stay, with mechanical changes only
   (`fakeStates.Observe`, `Settle` → `ReadyAt`, `SessionRules` →
   `SessionProfile`, `Rules` assertions → `Profile.Read` on fixtures).
2. `agentstate`: the existing classifier tables (screens, titles, own
   menus, Kimi progress, `ClassifyWith` cases) are rewritten to call
   `ForAgent(id).Read(obs)` / the parts, with the same fixtures and
   expectations; plus `First` combinator tests (decisive part stops the
   chain, hints from earlier parts, no hints from later parts).
3. `sessionTracker.Step` unit tests without a watcher: hold choice ×
   trust, Since = first idle, grace + delayed question, both stalls,
   Unknown keeps state and Options, `ReadyAt`.
4. `archtest` unchanged and green.
5. `./test.sh race` prints "all green"; live: a real Claude in a gg
   console — one working → idle transition ~1 s after a multi-tool turn;
   a manual-mode Write dialog → needs input.
