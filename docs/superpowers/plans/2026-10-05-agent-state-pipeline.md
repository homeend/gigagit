# Agent State Pipeline Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Restructure agent-state detection into one entry (`StateWatcher.observe`) → a fixed `agentstate.Detector` interface composed per agent → a per-session `sessionTracker` → one `SessionActivity` output, with no behaviour change.

**Architecture:** `agentstate` gains `Observation`/`Verdict`/`Reading`/`Profile`, the `Detector` parts (`Screen`, `Title`, `ProgressReport`, `First`) and one agent table; the old loose classifier API is proven equivalent, then removed. `domain` moves the per-session maps and rules of `observe` into `sessionTracker.Step`, reads sessions through `stateSource.Observe`, binds `Profile`s, and replaces `SessionActivity.Settle` with `ReadyAt` so `agent_wait` keeps no rule of its own.

**Tech Stack:** Go 1.26, stdlib `regexp`.

**Spec:** `docs/superpowers/specs/2026-10-05-agent-state-pipeline-design.md`

## Global Constraints

- No behaviour change: every existing expectation in `TestStates*`, `TestAgentWait*`, `TestSessionRules*` and the agentstate classifier tests holds; test edits are mechanical (names/types), never a changed expected value.
- `agentstate` stays a stdlib-only DAG leaf; frontends never import it (archtest); domain aliases (`ActivityState`, `ActivityOption`, `Activity*` consts) unchanged.
- No timing, config, wire, notice or frontend change.
- Test-first for new code; `gofmt -w` before each commit; commits via `gg add <files>` + `git commit -F <msgfile>`, messages end with the two attribution lines (Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com> / Claude-Session: https://claude.ai/code/session_01VGLjRP4v2My6Wa2d9xXfGz).
- New tests call `t.Parallel()` unless they swap process globals.
- An `Observation` built in tests sets `Progress: -1` unless the test is about progress (0 means Kimi's "clear").

## Review Focus

1. Options: today a Question read sets Options, an Unknown read keeps the previous Options, any other read clears them — decided on the read's state BEFORE the idle hold turns a held Waiting into Unknown. The tracker must do the same (Task 3 test `OptionsFollowTheReadNotTheHold`).
2. `First` must stop at the first decisive part: a Codex "Action Required" title must carry no idle hint, and a screen verdict after an idle title must carry the hint (Task 1 tests).
3. A `screen_*` block on a known agent fills its empty lists from the agent's built-ins and keeps `Own`, title and progress; on an unknown command it has only its own lists and no `Own` (Task 1 `WithScreen` tests, Task 4 domain tests).
4. `agent_wait` with a zero `ReadyAt` (static watcher) waits `idleSettle` after `Since`, as `Settle == 0` does today (Task 4).
5. Trackers and published states of sessions that stop running are both dropped; a profile bound before its session is listed survives until seen (the rule store's `keep`, unchanged).

---

### Task 1: agentstate — Detector parts, Profile, the agent table (beside the old API)

**Files:**
- Create: `internal/agentstate/detector.go` (types, `Profile.Read`, `First`)
- Create: `internal/agentstate/screen.go` (`ScreenPatterns`, `Screen`, `CompileScreen`)
- Create: `internal/agentstate/signals.go` (`TitlePatterns`, `Title`, `CompileTitle`, `ProgressReport`)
- Create: `internal/agentstate/agents.go` (`recipe`, `agents`, `ForAgent`, `Known`, `WithScreen`)
- Test: `internal/agentstate/detector_test.go`

**Interfaces:**
- Produces:
  ```go
  type Observation struct { Text string; Lines []string; Title string; Progress int }
  type Verdict struct { State State; IdleHint, Spinning bool }
  type Detector interface { Read(Observation) Verdict }
  type Reading struct { Verdict; Options []Option; StepFor time.Duration; Progress string }
  type Profile struct { Detector Detector; Dedicated bool }
  func (p Profile) Read(o Observation) Reading
  func First(parts ...Detector) Detector
  type ScreenPatterns struct { Working, Waiting, Own, Question []string }
  type Screen struct { Working, Waiting, Own, Question []*regexp.Regexp }
  func CompileScreen(p ScreenPatterns) (*Screen, error)
  type TitlePatterns struct { Working, Question, Idle []string }
  type Title struct { Working, Question, Idle []*regexp.Regexp }
  func CompileTitle(p TitlePatterns) (*Title, error)
  type ProgressReport struct{}
  func ForAgent(id string) Profile
  func Known(id string) bool
  func WithScreen(id string, working, waiting, question []string) (Profile, error)
  ```

- [ ] **Step 1: Write the failing tests** — `detector_test.go`:

```go
package agentstate

import (
	"slices"
	"strings"
	"testing"
)

func obs(text, title string, progress int) Observation {
	return Observation{Text: text, Lines: Tail(text, 15), Title: title, Progress: progress}
}

// Every screen fixture of the package × every title/progress the captures
// showed × trust: the new pipeline answers what ClassifyWith answered.
// (Removed with the old API in Task 5; the converted tests keep the cases.)
func TestProfileReadEqualsClassifyWith(t *testing.T) {
	t.Parallel()
	screens := []string{workingScreen, hooksScreen, starSpinnerScreen, waitingScreen, questionScreen, typedScreen,
		titledBoxScreen, codexDialog, codexIdle, codexApproval, ownMenuScreen, trustScreen,
		agyIdle, agyWork, agyRun, agyPerm, agyTrustLive, agyDone, codexStart, codexTrust,
		"just some text\nmid redraw\n", " │ >                    │\n ╰────────────────────╯\n", ""}
	titles := []string{"", "◐ x", "◑ Claude Code", "⠂ x", "✳ x", "Claude Code", "⠋ Run | repo", "Run | repo", "Action Required | repo"}
	progress := []int{-1, 0, 1, 2, 3, 4}
	for _, id := range []string{"claude", "codex", "kimi", "junie", "antigravity", "", "mystery"} {
		old, p := DefaultRules(id), ForAgent(id)
		if p.Dedicated != HasDefaults(id) {
			t.Errorf("%s: Dedicated = %v", id, p.Dedicated)
		}
		for _, sc := range screens {
			for _, ti := range titles {
				for _, pr := range progress {
					o := obs(sc, ti, pr)
					rd := p.Read(o)
					for _, trusted := range []bool{false, true} {
						sg := Signal{Title: ti, Progress: pr, Trusted: trusted}
						st, titleIdle := ClassifyWith(old, o.Lines, sg)
						if rd.State != st || (rd.IdleHint && trusted) != titleIdle {
							t.Fatalf("%s %q %q %d trusted=%v: new %q hint=%v, old %q titleIdle=%v",
								id, firstLine(sc), ti, pr, trusted, rd.State, rd.IdleHint, st, titleIdle)
						}
					}
					if want := SignalState(old, Signal{Title: ti, Progress: pr}) == Working; rd.Spinning != want {
						t.Fatalf("%s %q %q %d: Spinning = %v", id, firstLine(sc), ti, pr, rd.Spinning)
					}
					var wantOpts []Option
					if rd.State == Question {
						wantOpts = DialogOptions(o.Text, o.Lines)
					}
					if !slices.Equal(rd.Options, wantOpts) || rd.StepFor != StepDuration(o.Lines) || rd.Progress != Progress(o.Lines) {
						t.Fatalf("%s %q: extras differ", id, firstLine(sc))
					}
				}
			}
		}
	}
}

func firstLine(s string) string { l, _, _ := strings.Cut(strings.TrimSpace(s), "\n"); return l }

type fixed Verdict

func (f fixed) Read(Observation) Verdict { return Verdict(f) }

func TestFirstStopsAtTheFirstVerdict(t *testing.T) {
	t.Parallel()
	o := Observation{Progress: -1}
	cases := []struct {
		name  string
		parts []Detector
		want  Verdict
	}{
		{"decisive first part carries no later hint", []Detector{fixed{State: Question}, fixed{IdleHint: true}, fixed{State: Waiting}}, Verdict{State: Question}},
		{"an earlier hint reaches the deciding part", []Detector{fixed{IdleHint: true}, fixed{State: Waiting}}, Verdict{State: Waiting, IdleHint: true}},
		{"spinning travels with its verdict", []Detector{fixed{State: Working, Spinning: true}, fixed{State: Waiting}}, Verdict{State: Working, Spinning: true}},
		{"no verdict keeps the hints", []Detector{fixed{IdleHint: true}, fixed{}}, Verdict{IdleHint: true}},
		{"empty", nil, Verdict{}},
	}
	for _, c := range cases {
		if got := First(c.parts...).Read(o); got != c.want {
			t.Errorf("%s: %+v, want %+v", c.name, got, c.want)
		}
	}
}

func TestWithScreen(t *testing.T) {
	t.Parallel()
	// A known agent: the given list replaces its built-in one, the other
	// lists, Own, title and progress stay.
	p, err := WithScreen("claude", nil, nil, []string{`CONFIRM`})
	if err != nil || !p.Dedicated {
		t.Fatalf("err=%v dedicated=%v", err, p.Dedicated)
	}
	for _, c := range []struct {
		o    Observation
		want State
	}{
		{obs("please CONFIRM", "", -1), Question},
		{obs(questionScreen, "", -1), Unknown}, // the built-in question list is replaced, not merged
		{obs(waitingScreen, "", -1), Waiting},  // built-in waiting kept
		{obs(workingScreen, "", -1), Working},  // built-in working kept
		{obs("please CONFIRM\n ❯ 1. View tools\n Esc to back", "", -1), Unknown}, // Own kept
		{obs("noise", "◐ x", -1), Working},     // title kept
	} {
		if got := p.Read(c.o).State; got != c.want {
			t.Errorf("claude block %q: %q, want %q", firstLine(c.o.Text), got, c.want)
		}
	}
	k, _ := WithScreen("kimi", nil, []string{`^READY$`}, nil)
	if got := k.Read(obs("noise", "", 3)).State; got != Working {
		t.Errorf("kimi block lost its progress part: %q", got)
	}
	// An unknown command: only its own lists, no Own.
	u, _ := WithScreen("mystery", nil, []string{`^READY$`}, nil)
	if got := u.Read(obs("READY", "", -1)).State; got != Waiting || !u.Dedicated {
		t.Errorf("custom: %q dedicated=%v", got, u.Dedicated)
	}
	if got := u.Read(obs("continue? (y/n)", "", -1)).State; got != Unknown {
		t.Errorf("custom has no generic question list: %q", got)
	}
	if _, err := WithScreen("claude", []string{`(`}, nil, nil); err == nil {
		t.Error("bad pattern accepted")
	}
}

func TestKnown(t *testing.T) {
	t.Parallel()
	for _, id := range []string{"claude", "codex", "junie", "kimi", "antigravity"} {
		if !Known(id) || ForAgent(id).Dedicated != true {
			t.Errorf("%s not known", id)
		}
	}
	if Known("") || Known("mystery") || ForAgent("mystery").Dedicated {
		t.Error("generic ids must not claim dedicated rules")
	}
}
```

- [ ] **Step 2: Run** — `go test ./internal/agentstate/ -count=1` → compile FAIL (`ForAgent`, `First`, `WithScreen`… undefined).

- [ ] **Step 3: Implement**

`detector.go`:

```go
package agentstate

import "time"

// Observation is what gg observed of one session at one moment: its screen
// and what it announced outside it (the window title, the OSC 9;4 report).
type Observation struct {
	Text     string   // the screen (dialog options read the raw text)
	Lines    []string // Tail(Text, 15)
	Title    string   // last OSC 0/2 title
	Progress int      // last OSC 9;4 state, −1 none
}

// Verdict is what one detector concludes. Unknown: no verdict — the next
// part decides, and with none the session keeps the state it had.
type Verdict struct {
	State State
	// IdleHint: a source other than the screen says idle. It never decides;
	// the tracker may hold a trusted one shorter.
	IdleHint bool
	// Spinning: a source other than the screen says working — the trust the
	// idle hint needs.
	Spinning bool
}

// Detector reads one kind of evidence. Every source of agent state —
// the screen, the title, the progress report — implements it, and an
// agent's Profile composes them (agents.go).
type Detector interface{ Read(Observation) Verdict }

// Reading is a profile's conclusion: the verdict plus what any screen
// shows whatever the agent — the dialog's choices, the spinner's timer,
// the tail without what moves while nothing happens (the stall key).
type Reading struct {
	Verdict
	Options  []Option      // when State is Question
	StepFor  time.Duration // StepDuration(Lines)
	Progress string        // Progress(Lines)
}

// Profile is how one agent is read.
type Profile struct {
	Detector Detector
	// Dedicated: the rules know this agent's screens (not the generic set);
	// an unreadable screen of a dedicated agent counts towards a stall.
	Dedicated bool
}

// Read applies the profile's detector and adds the agent-agnostic extras.
func (p Profile) Read(o Observation) Reading {
	var v Verdict
	if p.Detector != nil {
		v = p.Detector.Read(o)
	}
	rd := Reading{Verdict: v, StepFor: StepDuration(o.Lines), Progress: Progress(o.Lines)}
	if v.State == Question {
		rd.Options = DialogOptions(o.Text, o.Lines)
	}
	return rd
}

// First consults its parts in order: the first one with a verdict decides,
// carrying the hints of the parts before it; later parts are not asked.
// (herdr's order: a title question or spinner outranks every screen rule;
// an idle title only hints.)
func First(parts ...Detector) Detector { return first(parts) }

type first []Detector

func (f first) Read(o Observation) Verdict {
	var hints Verdict
	for _, d := range f {
		v := d.Read(o)
		hints.IdleHint = hints.IdleHint || v.IdleHint
		hints.Spinning = hints.Spinning || v.Spinning
		if v.State != Unknown {
			v.IdleHint, v.Spinning = hints.IdleHint, hints.Spinning
			return v
		}
	}
	return hints
}
```

`screen.go` — move the `Classify` doc and body here:

```go
package agentstate

import (
	"regexp"
	"strings"
)

// ScreenPatterns are an agent's screen rules as RE2 sources (built-ins in
// agents.go, or a command's screen_* lists).
type ScreenPatterns struct {
	Working, Waiting, Own, Question []string
}

// Screen reads the last tail lines. Checks are ordered Working → Waiting →
// Own → Question so that a phrase quoted in scrollback can never outrank the
// prompt box that is still on screen. Patterns run in multi-line mode over
// the joined tail lines, so ^ and $ bound a line and "\n" lets a rule span
// two adjacent lines (e.g. rule-line + prompt). Own are the agent's OWN menus
// (a settings or server menu the user opened), matched against the LAST tail
// line (their footer): they read as Unknown, so the session keeps the state
// it had — idle stays idle, and a dialog's sub-step keeps its question.
type Screen struct {
	Working, Waiting, Own, Question []*regexp.Regexp
}

// CompileScreen compiles p; a bad pattern is an error naming it.
func CompileScreen(p ScreenPatterns) (*Screen, error) {
	var s Screen
	var err error
	if s.Working, err = compileAll(p.Working); err != nil {
		return nil, err
	}
	if s.Waiting, err = compileAll(p.Waiting); err != nil {
		return nil, err
	}
	if s.Own, err = compileAll(p.Own); err != nil {
		return nil, err
	}
	if s.Question, err = compileAll(p.Question); err != nil {
		return nil, err
	}
	return &s, nil
}

func (s *Screen) Read(o Observation) Verdict {
	text := strings.Join(o.Lines, "\n")
	switch {
	case anyMatch(s.Working, text):
		return Verdict{State: Working}
	case anyMatch(s.Waiting, text):
		return Verdict{State: Waiting}
	case len(o.Lines) > 0 && anyMatch(s.Own, o.Lines[len(o.Lines)-1]):
		// Only the last line: a menu's footer sits there, and the same
		// words quoted higher up (a diff the agent asks to apply) must
		// not hide the dialog below them.
		return Verdict{}
	case anyMatch(s.Question, text):
		return Verdict{State: Question}
	}
	return Verdict{}
}
```

`signals.go`:

```go
package agentstate

// TitlePatterns are an agent's window-title (OSC 0/2) rules as RE2 sources.
type TitlePatterns struct {
	Working, Question, Idle []string
}

// Title reads the agent's window title: a question or a spinner decides
// (above every screen rule); an idle title only hints — Claude titles ANY
// waiting screen "✳" (a picker opened mid-turn, a dialog the screen rules
// miss), so it may confirm the screen's idle, never create one.
type Title struct {
	Working, Question, Idle []*regexp.Regexp
}

func CompileTitle(p TitlePatterns) (*Title, error) {
	var t Title
	var err error
	if t.Working, err = compileAll(p.Working); err != nil {
		return nil, err
	}
	if t.Question, err = compileAll(p.Question); err != nil {
		return nil, err
	}
	if t.Idle, err = compileAll(p.Idle); err != nil {
		return nil, err
	}
	return &t, nil
}

func (t *Title) Read(o Observation) Verdict {
	if o.Title == "" {
		return Verdict{}
	}
	switch {
	case anyMatch(t.Question, o.Title):
		return Verdict{State: Question}
	case anyMatch(t.Working, o.Title):
		return Verdict{State: Working, Spinning: true}
	case anyMatch(t.Idle, o.Title):
		return Verdict{IdleHint: true}
	}
	return Verdict{}
}

// ProgressReport reads the OSC 9;4 report: 1 (normal) and 3 (busy) are
// working, 0 (clear) hints idle; 2 (error) and 4 (paused) say nothing.
type ProgressReport struct{}

func (ProgressReport) Read(o Observation) Verdict {
	switch o.Progress {
	case 1, 3:
		return Verdict{State: Working, Spinning: true}
	case 0:
		return Verdict{IdleHint: true}
	}
	return Verdict{}
}
```

(add `import "regexp"` to `signals.go`.)

Note the order inside `SignalState` today: title question → title working → progress busy → title idle → progress idle. `First(title, progress, screen)` reproduces it: a title idle is a hint (not decisive), so a busy progress after it still decides Working — matching `SignalState`, where progress-busy is checked before title-idle. No agent has both parts today; the equivalence test covers each agent as configured.

`agents.go` — the ONE table; move the pattern lists and their comments from `classify.go` (`defaults`, `ownMenus`, `titleRules`, `progressAgents`) into per-agent `ScreenPatterns`/`TitlePatterns` values here, unchanged:

```go
package agentstate

// recipe is how an agent is read: signal parts consulted before its screen
// (in order), then its screen rules.
type recipe struct {
	signals []Detector
	screen  ScreenPatterns
}

// agents: what each gg agent id (the exttool tool id) is read by. "" is the
// generic fallback for ids gg has no verified rules for.
var agents = map[string]recipe{
	"claude":      {signals: []Detector{claudeTitle}, screen: claudeScreen},
	"codex":       {signals: []Detector{codexTitle}, screen: codexScreen},
	"kimi":        {signals: []Detector{ProgressReport{}}, screen: kimiScreen},
	"junie":       {screen: junieScreen},
	"antigravity": {screen: agyScreen},
	"":            {screen: genericScreen},
}

var (
	claudeScreen = ScreenPatterns{
		Working:  []string{ /* defaults["claude"][0] with its comment */ },
		Waiting:  []string{ /* defaults["claude"][1] with its comment */ },
		Own:      []string{ /* ownMenus["claude"] with its comment */ },
		Question: []string{ /* defaults["claude"][2] */ },
	}
	// … codexScreen, agyScreen, junieScreen, kimiScreen, genericScreen likewise
	claudeTitle = mustTitle(TitlePatterns{Working: []string{`^[◐◑◒◓⠀-⣿] `}, Idle: []string{`^✳ `}})
	codexTitle  = mustTitle(TitlePatterns{Working: []string{`(?:^| )[⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏](?: |$)`}, Question: []string{`Action Required`}, Idle: []string{`\S`}})
)

// compiled holds every built-in profile, compiled once.
var compiled = func() map[string]Profile {
	out := map[string]Profile{}
	for id, r := range agents {
		s, err := CompileScreen(r.screen)
		if err != nil {
			panic(err) // built-ins are constants; a bad one is a programming error
		}
		out[id] = r.profile(s, id != "")
	}
	return out
}()

func (r recipe) profile(s *Screen, dedicated bool) Profile {
	if len(r.signals) == 0 {
		return Profile{Detector: s, Dedicated: dedicated}
	}
	return Profile{Detector: First(append(slices.Clone(r.signals), Detector(s))...), Dedicated: dedicated}
}

// ForAgent is the built-in profile of an agent id; unknown ids get the
// generic one.
func ForAgent(id string) Profile {
	if p, ok := compiled[id]; ok {
		return p
	}
	return compiled[""]
}

// Known reports whether gg ships dedicated rules for this agent id.
func Known(id string) bool { _, ok := agents[id]; return ok && id != "" }

// WithScreen is the profile of a command with its own screen_* lists. For a
// known agent each empty list keeps the agent's built-in one and the Own,
// title and progress parts stay; for any other command only the given lists
// apply. Either way the rules are dedicated (they know this program).
func WithScreen(id string, working, waiting, question []string) (Profile, error) {
	r, known := agents[id]
	if !known || id == "" {
		r = recipe{}
	}
	p := ScreenPatterns{Working: working, Waiting: waiting, Question: question}
	if known && id != "" {
		if len(p.Working) == 0 {
			p.Working = r.screen.Working
		}
		if len(p.Waiting) == 0 {
			p.Waiting = r.screen.Waiting
		}
		if len(p.Question) == 0 {
			p.Question = r.screen.Question
		}
		p.Own = r.screen.Own
	}
	s, err := CompileScreen(p)
	if err != nil {
		return Profile{}, err
	}
	return r.profile(s, true), nil
}

func mustTitle(p TitlePatterns) *Title {
	t, err := CompileTitle(p)
	if err != nil {
		panic(err)
	}
	return t
}
```

(imports: `slices`.) During this task `classify.go` keeps its old API (the equivalence test calls it), so the pattern data briefly exists twice: `DefaultRules` keeps reading `defaults`/`ownMenus`/`titleRules`/`progressAgents`. Copy, don't move, in this task; Task 5 deletes the old maps. Ledger this as a ruling (spec says "replacing today's four maps" — done in Task 5).

- [ ] **Step 4: Run** — `go test ./internal/agentstate/ -count=1` → PASS (including the equivalence test over 7 ids × 23 screens × 9 titles × 6 progress × trust).

- [ ] **Step 5: Probe** — temporarily change `first.Read` to return the LAST verdict instead of the first; run `-run ProfileReadEquals|FirstStops` → FAIL; restore from a saved copy (never `git checkout`).

- [ ] **Step 6: Commit** — gofmt; `gg add` the 5 files; `refactor(agentstate): Detector parts, Profile and the agent table`.

---

### Task 2: domain — sessionTracker.Step (not yet wired)

**Files:**
- Create: `internal/domain/session_tracker.go`
- Test: `internal/domain/session_tracker_test.go`

**Interfaces:**
- Consumes: `agentstate.Reading`, `agentstate.Verdict`.
- Produces:
  ```go
  type stateTiming struct{ grace, stall, spinStall, idleSettle, titleSettle time.Duration }
  func currentTiming() stateTiming   // snapshot under statesMu
  type sessionTracker struct { dedicated bool; act SessionActivity; pendingIdle time.Time; pendingQ bool; progress progressMark; animated bool }
  func (t *sessionTracker) Step(rd agentstate.Reading, info agentsession.Info, lastOut, now time.Time, tm stateTiming) (act SessionActivity, notes []ActivityNotice, changed bool, recheck time.Duration)
  ```
  and `SessionActivity.ReadyAt time.Time` (added beside `Settle` in this task; `Settle` is removed in Task 4).

- [ ] **Step 1: Write the failing tests** — `session_tracker_test.go`:

```go
package domain

import (
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/agentsession"
	"github.com/homeend/gigagit/internal/agentstate"
)

var trkTiming = stateTiming{grace: 30 * time.Second, stall: 2 * time.Minute, spinStall: 10 * time.Minute, idleSettle: 2 * time.Second, titleSettle: 700 * time.Millisecond}

var trkInfo = agentsession.Info{ID: "s1", Label: "Claude", Dir: "/wt/a", Started: actT0}

func rdg(st agentstate.State) agentstate.Reading {
	return agentstate.Reading{Verdict: agentstate.Verdict{State: st}, Progress: string(st)}
}

func noteKinds(ns []ActivityNotice) string {
	s := ""
	for _, n := range ns {
		s += n.Kind + ","
	}
	return s
}

func TestTrackerTitledIdleHoldsShortOnlyWhenTrusted(t *testing.T) {
	t.Parallel()
	late := actT0.Add(time.Minute)
	for _, c := range []struct {
		name     string
		spunOnce bool
		hint     bool
		want     time.Duration
	}{
		{"trusted hint", true, true, 700 * time.Millisecond},
		{"untrusted hint", false, true, 2 * time.Second},
		{"no hint", true, false, 2 * time.Second},
	} {
		var tr sessionTracker
		w := rdg(agentstate.Working)
		w.Spinning = c.spunOnce
		tr.Step(w, trkInfo, late, late, trkTiming)
		idle := rdg(agentstate.Waiting)
		idle.IdleHint = c.hint
		if _, _, _, again := tr.Step(idle, trkInfo, late, late.Add(time.Second), trkTiming); again != c.want {
			t.Errorf("%s: recheck %v, want %v", c.name, again, c.want)
		}
		act, notes, _, _ := tr.Step(idle, trkInfo, late, late.Add(time.Second+c.want), trkTiming)
		if act.State != ActivityIdle || !act.Since.Equal(late.Add(time.Second)) || !act.ReadyAt.Equal(late.Add(time.Second+c.want)) || noteKinds(notes) != "idle," {
			t.Errorf("%s: %+v notes %q", c.name, act, noteKinds(notes))
		}
	}
}

// A held idle reads as Unknown for the state, but Options follow the read:
// a Waiting read clears a question's options even while it is held.
func TestTrackerOptionsFollowTheReadNotTheHold(t *testing.T) {
	t.Parallel()
	late := actT0.Add(time.Minute)
	var tr sessionTracker
	q := rdg(agentstate.Question)
	q.Options = []agentstate.Option{{Key: "1", Label: "Yes"}}
	tr.Step(rdg(agentstate.Working), trkInfo, late, late, trkTiming)
	act, _, _, _ := tr.Step(q, trkInfo, late, late.Add(time.Second), trkTiming)
	if len(act.Options) != 1 {
		t.Fatalf("question options: %+v", act)
	}
	act, _, _, _ = tr.Step(rdg(agentstate.Unknown), trkInfo, late, late.Add(2*time.Second), trkTiming)
	if len(act.Options) != 1 || act.State != ActivityQuestion {
		t.Fatalf("unknown must keep state and options: %+v", act)
	}
	tr.Step(rdg(agentstate.Working), trkInfo, late, late.Add(3*time.Second), trkTiming)
	act, _, _, _ = tr.Step(rdg(agentstate.Waiting), trkInfo, late, late.Add(4*time.Second), trkTiming) // held
	if act.State != ActivityWorking || act.Options != nil {
		t.Fatalf("held idle: %+v", act)
	}
}

func TestTrackerQuestionInsideTheGraceIsAnnouncedAfterIt(t *testing.T) {
	t.Parallel()
	var tr sessionTracker
	_, notes, _, _ := tr.Step(rdg(agentstate.Question), trkInfo, actT0, actT0.Add(time.Second), trkTiming)
	if len(notes) != 0 {
		t.Fatalf("noticed inside the grace: %q", noteKinds(notes))
	}
	_, notes, _, _ = tr.Step(rdg(agentstate.Question), trkInfo, actT0, actT0.Add(31*time.Second), trkTiming)
	if noteKinds(notes) != "question," {
		t.Fatalf("after the grace: %q", noteKinds(notes))
	}
}

func TestTrackerStalls(t *testing.T) {
	t.Parallel()
	late := actT0.Add(time.Minute)
	// no output while working
	var tr sessionTracker
	tr.Step(rdg(agentstate.Working), trkInfo, late, late, trkTiming)
	act, notes, _, _ := tr.Step(rdg(agentstate.Working), trkInfo, late, late.Add(3*time.Minute), trkTiming)
	if !act.Stalled || noteKinds(notes) != "stalled," || notes[0].Spinning {
		t.Fatalf("silent stall: %+v %+v", act, notes)
	}
	// only the spinner moves for spinStall (output keeps coming)
	var sp sessionTracker
	sp.Step(rdg(agentstate.Working), trkInfo, late, late, trkTiming)
	at := late.Add(11 * time.Minute)
	act, notes, _, _ = sp.Step(rdg(agentstate.Working), trkInfo, at, at, trkTiming)
	if !act.Stalled || len(notes) != 1 || !notes[0].Spinning {
		t.Fatalf("spinner stall: %+v %+v", act, notes)
	}
	// unknown counts only for dedicated rules
	gen := sessionTracker{dedicated: false}
	if act, _, _, _ := gen.Step(rdg(agentstate.Unknown), trkInfo, late, late.Add(3*time.Minute), trkTiming); act.Stalled {
		t.Fatal("generic unknown stalled")
	}
	ded := sessionTracker{dedicated: true}
	if act, _, _, _ := ded.Step(rdg(agentstate.Unknown), trkInfo, late, late.Add(3*time.Minute), trkTiming); !act.Stalled {
		t.Fatal("dedicated unknown did not stall")
	}
}

func TestTrackerReadyAtOfAQuestionIsItsSince(t *testing.T) {
	t.Parallel()
	var tr sessionTracker
	at := actT0.Add(time.Minute)
	act, _, changed, _ := tr.Step(rdg(agentstate.Question), trkInfo, at, at, trkTiming)
	if !changed || !act.ReadyAt.Equal(at) || !act.Since.Equal(at) {
		t.Fatalf("%+v changed=%v", act, changed)
	}
}
```

- [ ] **Step 2: Run** — `go test ./internal/domain/ -run Tracker -count=1` → compile FAIL (`sessionTracker`, `stateTiming`, `ReadyAt` undefined).

- [ ] **Step 3: Implement** — add `ReadyAt time.Time // when the state counts as settled: an idle after the hold it passed, others at Since` to `SessionActivity` (keep `Settle` for now). `session_tracker.go`:

```go
package domain

import (
	"slices"
	"time"

	"github.com/homeend/gigagit/internal/agentsession"
	"github.com/homeend/gigagit/internal/agentstate"
)

// stateTiming is one snapshot of the timing rules (session_states.go).
type stateTiming struct{ grace, stall, spinStall, idleSettle, titleSettle time.Duration }

func currentTiming() stateTiming {
	statesMu.Lock()
	defer statesMu.Unlock()
	return stateTiming{grace: stateGrace, stall: stallAfter, spinStall: spinStallAfter, idleSettle: idleSettle, titleSettle: titleSettle}
}

// sessionTracker folds one session's readings into its activity over time:
// the trust its idle hint needs, the idle hold, the grace after a start, the
// question that opened inside it, both stall rules. One per running session,
// owned by the watcher (under its mu).
type sessionTracker struct {
	dedicated   bool            // the profile knows this agent's screens
	act         SessionActivity // the last published activity
	pendingIdle time.Time       // the first idle read while working (zero: none)
	pendingQ    bool            // a question seen inside the grace, not yet announced
	progress    progressMark    // the spinner-only stall clock
	animated    bool            // a non-screen source has said working: its idle hint is trusted
}

// Step folds one reading in. It returns the new activity, the notices to
// post (unnumbered), whether anything a subscriber shows changed, and how
// soon a pending idle wants another look (0: none). An Unknown reading
// changes nothing (output lands mid-redraw often enough that acting on it
// would flap). A working session that reads idle shows idle only once that
// has held — titleSettle when a trusted idle hint agrees, else idleSettle —
// and then from when it began.
func (t *sessionTracker) Step(rd agentstate.Reading, info agentsession.Info, lastOut, now time.Time, tm stateTiming) (SessionActivity, []ActivityNotice, bool, time.Duration) {
	var notes []ActivityNotice
	changed := false
	var recheck time.Duration
	if rd.Spinning {
		t.animated = true
	}
	prev := t.act
	next := prev
	st := rd.State
	switch st {
	case agentstate.Question:
		next.Options = rd.Options
	case agentstate.Unknown:
	default:
		next.Options = nil
	}
	note := func(kind string, quiet time.Duration, spinning bool) {
		notes = append(notes, ActivityNotice{ID: info.ID, Kind: kind, Label: info.Label, Dir: info.Dir, Quiet: quiet, Spinning: spinning})
		changed = true
	}
	trusted := now.Sub(info.Started) >= tm.grace
	since := now
	hold := tm.idleSettle
	if rd.IdleHint && t.animated {
		hold = tm.titleSettle
	}
	switch {
	case st == agentstate.Waiting && prev.State == agentstate.Working:
		if t.pendingIdle.IsZero() {
			t.pendingIdle = now
		}
		if held := now.Sub(t.pendingIdle); held < hold {
			st = agentstate.Unknown // not yet: the session stays working
			recheck = hold - held
		} else {
			since, t.pendingIdle = t.pendingIdle, time.Time{}
		}
	case st != agentstate.Unknown:
		t.pendingIdle = time.Time{} // working again, or a question
	}
	if st != agentstate.Unknown && st != prev.State {
		next.State, next.Since, next.ReadyAt, next.Settle = st, since, since, 0
		if st == agentstate.Waiting {
			next.ReadyAt, next.Settle = since.Add(hold), hold
		}
		changed = true
		switch {
		case st == agentstate.Question && trusted:
			note("question", 0, false)
		case st == agentstate.Question:
			t.pendingQ = true
		case st == agentstate.Waiting && prev.State == agentstate.Working && since.Sub(info.Started) >= tm.grace:
			note("idle", 0, false)
		}
	} else if t.pendingQ && trusted {
		// The question that opened inside the grace is still up: the user
		// must hear about it (a trust dialog at start).
		t.pendingQ = false
		if next.State == agentstate.Question {
			note("question", 0, false)
		}
	}
	if next.State != agentstate.Question {
		t.pendingQ = false
	}
	next.StepFor = rd.StepFor
	stalled := !lastOut.IsZero() && now.Sub(lastOut) > tm.stall &&
		(next.State == agentstate.Working || (next.State == agentstate.Unknown && t.dedicated))
	quiet, spinning := now.Sub(lastOut), false
	if rd.Progress != t.progress.key || t.progress.since.IsZero() {
		t.progress = progressMark{key: rd.Progress, since: now}
	}
	if !stalled && next.State == agentstate.Working && now.Sub(t.progress.since) >= tm.spinStall {
		stalled, quiet, spinning = true, now.Sub(t.progress.since), true
	}
	if stalled != prev.Stalled {
		changed = true
		if stalled {
			note("stalled", quiet, spinning)
		}
	}
	next.Stalled = stalled
	if !slices.Equal(prev.Options, next.Options) {
		changed = true
	}
	t.act = next
	return next, notes, changed, recheck
}
```

Deviation check against today's `observe` (rule for rule): `hold` uses `rd.IdleHint && t.animated` where today used `titleIdle` = hint && (animated-before || says==Working) — equal, since a Working signal is decisive and then carries no idle hint. Ledger as a note.

- [ ] **Step 4: Run** — `go test ./internal/domain/ -run Tracker -count=1 -race` → PASS.

- [ ] **Step 5: Commit** — gofmt; `gg add internal/domain/session_tracker.go internal/domain/session_tracker_test.go internal/domain/session_states.go`; `refactor(domain): sessionTracker — one session's state over time`.

---

### Task 3: domain — observe runs on Observe + Profile + the tracker

**Files:**
- Modify: `internal/domain/session_states.go` (`stateSource`, `managerSource`, `ruleStore`, `StateWatcher` fields, `Bind`, `rulesFor`→`profileFor`, `observe`, `bindSessionRules`, `SessionRules`→`SessionProfile`, `SessionRulesWarning`)
- Modify: `internal/domain/sessions.go:175-177` (bind the profile)
- Modify: `internal/domain/session_states_test.go` (mechanical)

**Interfaces:**
- Consumes: Task 1 `Profile`, `ForAgent`, `Known`, `WithScreen`, `Tail`, `Observation`; Task 2 `sessionTracker`, `currentTiming`.
- Produces: `stateSource{ List() []agentsession.Info; Observe(id agentsession.ID) (agentstate.Observation, time.Time, bool) }`; `(*StateWatcher).Bind(id SessionID, p agentstate.Profile)`; `SessionProfile(tc config.ToolCommand) (agentstate.Profile, bool, error)`.

- [ ] **Step 1: Mechanical test edits first** (they will not compile until Step 3):
  - `fakeStates`: add
    ```go
    func (f *fakeStates) Observe(id agentsession.ID) (agentstate.Observation, time.Time, bool) {
    	text, ok := f.Text(id)
    	if !ok {
    		return agentstate.Observation{}, time.Time{}, false
    	}
    	sig, _ := f.Signals(id)
    	return agentstate.Observation{Text: text, Lines: agentstate.Tail(text, stateTailLines), Title: sig.Title, Progress: sig.Progress}, f.last[id], true
    }
    ```
    (keep `Text`/`Signals`/`LastOutput` as the fake's helpers).
  - Every `r, _ := agentstate.Compile(working, waiting, question)` → `r, _ := agentstate.WithScreen("", working, waiting, question)` (5 sites; the `Bind` calls stay).
  - `TestSessionRules`: `SessionRules` → `SessionProfile`; replace the length assertion with behaviour:
    ```go
    	r, custom, err := SessionProfile(one)
    	if !custom || err != nil {
    		t.Fatalf("one list: custom=%v err=%v", custom, err)
    	}
    	read := func(text string) agentstate.State {
    		return r.Read(agentstate.Observation{Text: text, Lines: agentstate.Tail(text, 15), Progress: -1}).State
    	}
    	if read("READY") != agentstate.Waiting || read(actWorking) != agentstate.Working || read(actQuestion) != agentstate.Question {
    		t.Fatal("one list (claude keeps its other built-ins)")
    	}
    ```
  - `TestEverySessionAgentHasRules`: `r := agentstate.DefaultRules(tl.ID); if len(r.Waiting)==0 || len(r.Question)==0` → `if !agentstate.Known(tl.ID)` with message `"%s: no dedicated rules"`.
  - `TestSessionRulesMergeWithTheAgentBuiltins`: `SessionRules` → `SessionProfile`; every `agentstate.Classify(r, agentstate.Tail(x, 15))` → `r.Read(agentstate.Observation{Text: x, Lines: agentstate.Tail(x, 15), Progress: -1}).State`; the title check → `r.Read(agentstate.Observation{Lines: agentstate.Tail(actIdle, 15), Title: "◐ x", Progress: -1}).State != agentstate.Working`; `!kr.ProgressBusy` → `kr.Read(agentstate.Observation{Progress: 3}).State != agentstate.Working`; the custom-command check `len(r.Working) != 0 || len(r.Question) != 0 || len(r.Waiting) != 1` → `read("continue? (y/n)") != agentstate.Unknown || read("READY") != agentstate.Waiting` (custom has only its own list).

- [ ] **Step 2: Run** — `go test ./internal/domain/ -run 'States|SessionRules|EverySession|StartSession' -count=1` → compile FAIL (`Observe` not in the interface is fine; `SessionProfile` undefined, `Bind` takes `Rules`).

- [ ] **Step 3: Implement** in `session_states.go`:
  - `stateSource`:
    ```go
    // stateSource is where the watcher observes sessions.
    type stateSource interface {
    	List() []agentsession.Info
    	// Observe is the session's screen, title and progress as one
    	// observation, and when it last printed.
    	Observe(id agentsession.ID) (agentstate.Observation, time.Time, bool)
    }
    ```
    `managerSource`: replace `Text`/`LastOutput`/`Signals` with
    ```go
    func (s managerSource) Observe(id agentsession.ID) (agentstate.Observation, time.Time, bool) {
    	sess, ok := s.m.Get(id)
    	if !ok {
    		return agentstate.Observation{}, time.Time{}, false
    	}
    	text, sig := sess.Text(), sess.Signals()
    	return agentstate.Observation{Text: text, Lines: agentstate.Tail(text, stateTailLines), Title: sig.Title, Progress: sig.Progress}, sess.LastOutput(), true
    }
    ```
  - `ruleStore` holds `agentstate.Profile` (rename the map's value type; methods `bind/get` take/return `agentstate.Profile`); `Bind(id SessionID, p agentstate.Profile)`.
  - `StateWatcher`: drop `pendingQ`, `pendingIdle`, `progress`, `animated`; add `trackers map[SessionID]*sessionTracker` (init in `newStateWatcher`). Keep `states` (the published copies — `NewStaticStates` and `Get` read it).
  - `rulesFor` → 
    ```go
    // profileFor: the bound profile, else the built-in one of a known agent.
    // Terminals and custom commands without a bound profile are never
    // classified.
    func (w *StateWatcher) profileFor(info agentsession.Info) (agentstate.Profile, bool) {
    	if p, bound := w.rules.get(info.ID); bound {
    		return p, true
    	}
    	if info.Terminal || info.AgentID == "" {
    		return agentstate.Profile{}, false
    	}
    	return agentstate.ForAgent(info.AgentID), true
    }
    ```
  - `observe` (replace the whole body; doc comment: "observe is THE entry of state detection: for each running session it observes (stateSource.Observe), reads the observation with the session's profile (agentstate — the screen, title and progress detectors composed per agent), and folds the reading into the session's tracker (sessionTracker.Step — holds, grace, stalls), publishing the activity and its notices. Screens are read before taking w.mu: each read takes its session's emulator lock, and Get must not wait on that. It returns how soon a pending idle wants another look (0: none)."):
    ```go
    func (w *StateWatcher) observe(now time.Time) (recheck time.Duration) {
    	if w.src == nil {
    		return 0
    	}
    	tm := currentTiming()
    	type read struct {
    		info      agentsession.Info
    		dedicated bool
    		rd        agentstate.Reading
    		last      time.Time
    	}
    	var reads []read
    	live := map[SessionID]bool{}
    	for _, info := range w.src.List() {
    		if info.State != agentsession.Running {
    			continue
    		}
    		live[info.ID] = true
    		prof, ok := w.profileFor(info)
    		if !ok {
    			continue
    		}
    		obs, last, ok := w.src.Observe(info.ID)
    		if !ok {
    			continue
    		}
    		reads = append(reads, read{info: info, dedicated: prof.Dedicated, rd: prof.Read(obs), last: last})
    	}
    	changed := false
    	w.mu.Lock()
    	for _, r := range reads {
    		t := w.trackers[r.info.ID]
    		if t == nil {
    			t = &sessionTracker{}
    			w.trackers[r.info.ID] = t
    		}
    		t.dedicated = r.dedicated
    		act, notes, ch, again := t.Step(r.rd, r.info, r.last, now, tm)
    		w.states[r.info.ID] = act
    		for _, n := range notes {
    			w.post(n)
    		}
    		changed = changed || ch
    		if again > 0 && (recheck == 0 || again < recheck) {
    			recheck = again
    		}
    	}
    	for id := range w.trackers {
    		if !live[id] {
    			delete(w.trackers, id)
    		}
    	}
    	for id := range w.states {
    		if !live[id] {
    			delete(w.states, id)
    			changed = true
    		}
    	}
    	w.mu.Unlock()
    	w.rules.keep(live)
    	if changed {
    		w.bc.Signal()
    	}
    	return recheck
    }
    ```
    Remove the now-unused `maps` import if nothing else uses it.
  - `bindSessionRules(id, r agentstate.Rules)` → `bindSessionProfile(id, p agentstate.Profile)`.
  - `SessionRules` →
    ```go
    // SessionProfile is the profile of a command with its own screen_* lists
    // (agentstate.WithScreen: a known agent keeps its other built-ins). custom
    // is false when the command has no lists; err reports a bad pattern.
    func SessionProfile(tc config.ToolCommand) (p agentstate.Profile, custom bool, err error) {
    	if !tc.HasScreenRules() {
    		return agentstate.Profile{}, false, nil
    	}
    	p, err = agentstate.WithScreen(agentIDFor(tc), tc.ScreenWorking, tc.ScreenWaiting, tc.ScreenQuestion)
    	if err != nil {
    		return agentstate.Profile{}, false, err
    	}
    	return p, true, nil
    }
    ```
    `SessionRulesWarning` calls `SessionProfile` (text unchanged). `sessions.go:175-177` → `if p, custom, _ := SessionProfile(tc); custom { bindSessionProfile(sess.Info().ID, p) }` (update its comment's `SessionRulesWarning` mention only if it names `SessionRules`).
  - Note: `WithScreen` for an id that is not a known agent behaves like today's `Compile` (no merge) — `agentIDFor` returning "" or an unknown id is covered.

- [ ] **Step 4: Run** — `go test ./internal/domain/ -count=1 -race` → PASS (all `TestStates*`, `TestAgentWait*`, session rule tests, tracker tests). Then `go build ./... && go vet ./internal/...`.

- [ ] **Step 5: Commit** — gofmt; `gg add` the three files; `refactor(domain): observe runs on Observe, Profile and the tracker`.

---

### Task 4: agent_wait reads ReadyAt; Settle goes

**Files:**
- Modify: `internal/domain/agentwait.go` (the idle check, delete `idleHold`)
- Modify: `internal/domain/session_states.go` (`SessionActivity.Settle` removed)
- Modify: `internal/domain/session_tracker.go` (stop setting `Settle`)
- Modify: `internal/domain/session_states_test.go`, `internal/domain/agentwait_test.go` (mechanical)

- [ ] **Step 1: Mechanical test edits:** in `session_states_test.go` the assertions `a.Settle != 700*time.Millisecond` → `!a.ReadyAt.Equal(a.Since.Add(700*time.Millisecond))` and `a.Settle != 2*time.Second` → `!a.ReadyAt.Equal(a.Since.Add(2*time.Second))`; in `agentwait_test.go` `TestAgentWaitHonoursTheIdlesSettle`: `Settle: 100 * time.Millisecond` → `ReadyAt: start.Add(100 * time.Millisecond)`. Add one test:
  ```go
  // A static watcher's idle without ReadyAt waits idleSettle after Since.
  func TestAgentWaitZeroReadyAtFallsBackToIdleSettle(t *testing.T) {
  	ov, w1, _, s1, _, w := waitFixture(t)
  	t.Cleanup(UseIdleSettle(400 * time.Millisecond))
  	start := time.Now()
  	setStatic(w, s1.Info().ID, SessionActivity{State: ActivityIdle, Since: start})
  	res := doWait(t, ov, w1, "idle", 2*time.Second)
  	if res.TimedOut || time.Since(start) < 350*time.Millisecond {
  		t.Fatalf("delivered before idleSettle: %+v after %v", res, time.Since(start))
  	}
  }
  ```
  (This is today's `TestAgentWaitIdleSettle` behaviour restated; keep that test too.)

- [ ] **Step 2: Run** — `go test ./internal/domain/ -run 'AgentWait|States' -count=1` → compile FAIL? No — `ReadyAt` exists since Task 2 and `Settle` still exists: the edited tests compile; `TestAgentWaitHonoursTheIdlesSettle` FAILS (agent_wait still reads `Settle`, now 0 → waits idleSettle 2 s). That is the RED.

- [ ] **Step 3: Implement** — `agentwait.go`:
  ```go
  	if want["idle"] && a.State == ActivityIdle && !now.Before(readyAt(a, settle)) && !m.idleSince.Equal(a.Since) {
  ```
  ```go
  // readyAt is when the watcher's idle counts as settled; an activity without
  // one (a static watcher in tests) settles idleSettle after it began.
  func readyAt(a SessionActivity, settle time.Duration) time.Time {
  	if !a.ReadyAt.IsZero() {
  		return a.ReadyAt
  	}
  	return a.Since.Add(settle)
  }
  ```
  Delete `idleHold`. Remove the `Settle` field from `SessionActivity` and its assignments in `session_tracker.go`.

- [ ] **Step 4: Run** — `go test ./internal/domain/ -count=1 -race` → PASS; `go build ./...`.

- [ ] **Step 5: Commit** — `refactor(domain): agent_wait trusts the tracker's ReadyAt`.

---

### Task 5: remove the old agentstate API; convert its tests

**Files:**
- Modify: `internal/agentstate/classify.go` (keep `State` + consts, `compileAll`, `anyMatch`, `Tail`, `StepDuration`, spinner/timer regexps, `Progress`, `Option`, `DialogOptions`, `Options`; delete `Rules`, `defaults`, `ownMenus`, `titleRules`, `progressAgents`, `DefaultRules`, `Compile`, `Classify`, `Signal`, `SignalState`, `ClassifyWith`, `HasDefaults`)
- Modify: `internal/agentstate/classify_test.go`, `internal/agentstate/live_test.go`, `internal/agentstate/detector_test.go`

- [ ] **Step 1: Convert the tests** (expected values unchanged):
  - Add to `classify_test.go`:
    ```go
    // read is the state a profile reads from a screen with no title or progress.
    func read(p Profile, lines []string) State {
    	return p.Read(Observation{Text: strings.Join(lines, "\n"), Lines: lines, Progress: -1}).State
    }
    ```
  - `Classify(DefaultRules(id), x)` → `read(ForAgent(id), x)`; `r := DefaultRules(id)` → `r := ForAgent(id)` and `Classify(r, x)` → `read(r, x)`.
  - `TestCompileOverridesAndErrors`: `Compile(w, wa, q)` → `WithScreen("", w, wa, q)` (custom command), `Classify(r, …)` → `read(r, …)`.
  - `TestHasDefaultsAndGenericFallback`: `HasDefaults` → `Known`.
  - `TestSignalState` → `TestSignalParts`: same case table; replace `SignalState(c.r, c.s)` by reading the agent's signal parts through its profile over an empty screen: `v := ForAgent(id).Read(Observation{Title: c.title, Progress: c.progress})`, and map the old `want`: Question/Working → `v.State`; Waiting (old "idle") → `v.State == Unknown && v.IdleHint`; Unknown → `v.State == Unknown && !v.IdleHint`. Rewrite the table rows to carry `id string, title string, progress int` instead of `r Rules, s Signal` (same values).
  - `TestClassifyWith` → `TestProfileReadsSignalsBeforeTheScreen`: same rows; `ClassifyWith(claude, lines, s)` → `rd := ForAgent("claude").Read(Observation{Lines: lines, Title: s.title, Progress: -1})`; assert `rd.State == want && (rd.IdleHint && trusted) == wantIdle` where `trusted` is the row's old `Signal.Trusted`.
  - `TestClassifyIsClassifyWithoutASignal` → delete (it compared two old functions); its four screens are covered by the converted Claude tests.
  - `TestProfileReadEqualsClassifyWith` (Task 1) → delete (it compared against the old API).
  - `sig()` helper → delete if unused.

- [ ] **Step 2: Run** — `go test ./internal/agentstate/ -count=1` with the old API still present → PASS (conversions are correct against the new API).

- [ ] **Step 3: Delete the old API** listed above from `classify.go` (move `Tail`'s and the helpers' docs unchanged); update the package doc (`doc.go`) if it names `Classify`/`Rules`.

- [ ] **Step 4: Run** — `go build ./... && go vet ./... && go test ./internal/agentstate/ ./internal/domain/ ./internal/archtest/ -count=1` → PASS; `grep -rn 'DefaultRules\|ClassifyWith\|SignalState\|HasDefaults\|agentstate\.Rules\|agentstate\.Compile\b' internal/` → no hits.

- [ ] **Step 5: Commit** — `refactor(agentstate): drop the loose classifier API`.

---

### Task 6: docs, race gate, live check, review

- [ ] **Step 1: Docs** — `CLAUDE.md` `agentstate` row: "Pure agent-state detection: `Observation` → `Detector` parts (`Screen` rules, window `Title`, OSC 9;4 `ProgressReport`) composed per gg agent id by `First` in one table (`agents.go`: `ForAgent`, `Known`, `WithScreen` for `screen_*` lists) → `Profile.Read` → `Reading` (verdict + dialog options, step timer, stall key); `Tail`, `Options`/`CursorOptions`/`DialogOptions`. Stdlib-only DAG leaf, reached through `domain.SessionStates()`." (one line). `domain` row: mention "`SessionStates()` = `observe` (the entry) → `agentstate` profile → per-session `sessionTracker` (holds, grace, stalls) → `SessionActivity` (`ReadyAt` for `agent_wait`)" replacing the current watcher phrase, still one line. `docs/CLAUDE-details.md`: new section "Agent state pipeline (2026-10-05)" with the pipeline diagram from the spec, the type table, where each rule now lives, and "how to add a signal: a `Detector` part + a line in `agents`". `CHANGELOG.md`: under the newest section, an "### Internal" bullet: agent-state detection restructured (one entry, Detector parts per agent, a per-session tracker) — no behaviour change.

- [ ] **Step 2: Commit** — `docs: agent state pipeline`.

- [ ] **Step 3: Race gate** — `./test.sh race > <ws>/race.log 2>&1`; green only on the literal `all green`.

- [ ] **Step 4: Live check** — build `bin/gg` in the worktree; own tmux session `claude-pipeline`; start Claude from a Worktrees row's `.` menu in the scratch repo (read every screen before each key; `W` opens the create-worktree popup — use `ctrl+→` to reach the Worktrees tab); a multi-tool turn → one working→idle transition ~1 s after it ends; manual mode (shift+tab) Write → "needs input". Kill only `claude-pipeline`; confirm no leftover `bin/gg`/claude processes.

- [ ] **Step 5:** final read-only whole-branch review on Fable (ask the user before falling back to Opus), fix pass, ask before merging.
