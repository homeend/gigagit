# TUI e2e golden screens — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:executing-plans
> (this repo forbids implementer subagents — CLAUDE.md "NO IMPLEMENTER SUB
> AGENTS"; one read-only review subagent at the end is allowed). Steps use
> checkbox (`- [ ]`) syntax for tracking.

**Goal:** e2e scenarios that build a repo, drive the real TUI `Model` with
keys through a deterministic in-process loop, and compare the rendered
screens to committed golden files.

**Architecture:** `internal/clock` (freezable time) → `internal/tui`
gains virtual timers (`m.tick`), a quiet mode, an op-waiter descriptor, a
key-token inverse and `tui.Headless` (own message loop + `x/vt` drawing) →
the `e2e` harness gains a `[tui]` block, a fake review tool, golden-file
comparison and `-update`.

**Tech Stack:** Go 1.26, Bubble Tea v1.3.10, `github.com/charmbracelet/x/vt`,
`github.com/pelletier/go-toml/v2`, real `git`.

**Spec:** `docs/superpowers/specs/2026-10-01-tui-e2e-golden-screens-design.md`
(read it first; section numbers below refer to it).

## Global Constraints

- Work in the worktree `/work/gigagit/.claude/worktrees/tui-e2e-golden-screens`
  on branch `feat/tui-e2e-golden-screens`; `cd` there in EVERY shell command
  (the shell's cwd resets). Never commit in the main checkout.
- `internal/tui` never imports `internal/git` (archtest). `internal/clock`
  is a stdlib-only DAG leaf.
- Every user-visible TUI string goes through `i18n.T` with a key in all four
  bundles (this plan adds none — headless errors are test-facing Go errors,
  not UI text).
- Tests call `t.Parallel()` unless they touch process globals (the clock,
  theme, colour profile); those say why in a comment.
- Stage files by name with `gg add <paths>`; commit with
  `git commit -F <msgfile>` (gg commit has no -F). Never `git add -A`.
  Every commit message ends with:
  `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>` and
  `Claude-Session: https://claude.ai/code/session_01EgS7NPUx5zB8gvNEPT54wu`.
- Default TUI size `160x40`; settle budget 500 messages / 30 s; reading of
  the screen drops colours and trailing spaces.
- Golden byte comparison is skipped on Windows (`runtime.GOOS == "windows"`);
  `screen_contains`/`screen_excludes` run everywhere.
- Before merging: `./test.sh race > <log> 2>&1`, and the log must end with
  `all green` and contain no `FAIL` line.

## Review Focus

1. **A key whose effect arrives through the op waiter while no decision is
   pending** (e.g. a plain branch switch): the step must block until the op
   finishes, not settle mid-op. Pinned in Task 6.
2. **A step right after start-up in a repo with a sticky notice** (e.g. a
   stale lock): the notice must still be on screen (timers parked, not
   fired). Pinned in Task 4.
3. **A golden file left behind by a renamed checkpoint** must fail the
   scenario, not pass silently. Pinned in Task 9.
4. **A scenario that changes theme or language** must fail with a clear
   message instead of corrupting parallel scenarios. Pinned in Task 5.
5. **A frame line wider than the terminal** must clip, not wrap into the
   next row. Pinned in Task 5.

---

### Task 1: `internal/clock` and the stored/drawn call sites

**Files:**
- Create: `internal/clock/clock.go`, `internal/clock/clock_test.go`
- Modify (convert `time.Now()`/`time.Since(x)` → `clock.Now()`/`clock.Since(x)`):
  - drawn: `internal/tui/all_notes_popup.go:632`, `blame_view.go:303`,
    `branch_filter.go:113`, `branch_popup.go:77`, `branch_reviews.go:53`,
    `commit_generate.go:49` + `commit_popup.go:178` (pair), `diff_notes.go:540`,
    `notify.go:216`, `op.go:266` (`m.opStart`) + `view.go:480` +
    `model.go:3386` (the three `opStart` uses together), `pr_hub.go:56,82`,
    `prefix_settings.go:154`, `repo_popup.go:91`, `review_view.go:156`,
    `task_tab.go:321`, `worktree_popup.go:184,226`
  - stored: `internal/tui/load.go:136`, `internal/tui/source.go:489`,
    `internal/cli/cli.go:86` (the three `repos.Touch`), `internal/shelf/file_store.go:168,208,234`,
    `internal/bookmark/file_store.go:97`, `internal/savedcompare/file_store.go:137`,
    `internal/domain/linkhiststore.go:79`, `internal/domain/tasks.go:129,173,315,415`,
    `internal/notes/store.go:24` (`var Now = time.Now` → `var Now = clock.Now`)
  - **keep `time.Now`** (timing, or paired with a real-time store): mouse,
    refresh scheduling (`model.go:1623,3241-3253`, `refresh.go`,
    `settings_popup.go:353` — pairs with `refreshLastRun`), source/PR/tool
    durations, steer, snapshot, oplog, recorder, console/session elapsed.

**Interfaces:**
- Produces: `clock.Now() time.Time`, `clock.Since(t time.Time) time.Duration`,
  `clock.Freeze(t time.Time) (restore func())`.

- [ ] **Step 1: Write the failing test** — `internal/clock/clock_test.go`:

```go
package clock

import (
	"testing"
	"time"
)

// Not parallel: Freeze sets the package clock.
func TestFreezeAndRestore(t *testing.T) {
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	restore := Freeze(at)
	if got := Now(); !got.Equal(at) {
		t.Fatalf("Now() = %v, want %v", got, at)
	}
	if got := Since(at.Add(-90 * time.Second)); got != 90*time.Second {
		t.Fatalf("Since = %v, want 90s", got)
	}
	restore()
	if d := time.Since(Now()); d < 0 || d > time.Minute {
		t.Fatalf("after restore Now() is not the real time: %v", Now())
	}
}
```

- [ ] **Step 2: Run** `cd <wt> && go test ./internal/clock/` → FAIL (package does not exist).

- [ ] **Step 3: Implement** `internal/clock/clock.go`:

```go
// Package clock is gg's one freezable "now" for times that are STORED
// (a note's, shelf entry's, bookmark's creation) or DRAWN (ages, date
// lines, elapsed time on screen). Timing code — debounces, double-click
// windows, timeouts, measured durations — keeps time.Now: freezing it
// would stall them. Tests (the e2e golden screens) freeze it so a screen
// renders the same text on every run. Stdlib-only DAG leaf.
package clock

import (
	"sync/atomic"
	"time"
)

var frozen atomic.Pointer[time.Time]

// Now is the frozen instant when Freeze is in effect, else time.Now().
func Now() time.Time {
	if t := frozen.Load(); t != nil {
		return *t
	}
	return time.Now()
}

// Since is Now().Sub(t).
func Since(t time.Time) time.Duration { return Now().Sub(t) }

// Freeze pins Now to t until the returned restore runs. Process-global:
// callers freeze once (a TestMain) or run serially.
func Freeze(t time.Time) (restore func()) {
	prev := frozen.Swap(&t)
	return func() { frozen.Store(prev) }
}
```

- [ ] **Step 4: Run** `go test ./internal/clock/` → PASS.

- [ ] **Step 5: Convert the call sites listed above.** At each site open the
  file, replace `time.Now()` with `clock.Now()` and `time.Since(x)` with
  `clock.Since(x)`, add the import `"github.com/homeend/gigagit/internal/clock"`,
  and drop `"time"` if it became unused. For `internal/notes/store.go:24`
  write `var Now = clock.Now`. Then `go build ./... && go vet ./internal/...`.

- [ ] **Step 6: Pin one drawn site with a test** — append to
  `internal/tui/review_view_test.go` (not parallel: freezes the clock):

```go
// The review view's meta line draws the review's age from clock.Now, so a
// frozen clock makes it the same text on every run (the golden screens
// rely on this).
func TestReviewAgeUsesTheFrozenClock(t *testing.T) {
	restore := clock.Freeze(time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC))
	defer restore()
	created := time.Date(2026, 2, 27, 12, 0, 0, 0, time.UTC)
	if got := ageString(clock.Now(), created); got != ageString(time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC), created) {
		t.Fatalf("ageString under the frozen clock = %q", got)
	}
	if !strings.Contains(reviewMetaLine(&reviewViewState{review: domain.Review{Created: created, Agent: "a"}}), ageString(clock.Now(), created)) {
		t.Fatal("reviewMetaLine must draw the age against clock.Now")
	}
}
```

  (Add the `clock` import; `strings`, `time`, `domain` are already imported
  in that file — check and add if not.) Run
  `go test ./internal/tui -run TestReviewAgeUsesTheFrozenClock` → PASS, then
  `go test ./internal/tui ./internal/domain ./internal/shelf ./internal/bookmark ./internal/savedcompare ./internal/cli ./internal/notes` → PASS.

- [ ] **Step 7: Commit** — `gg add internal/clock internal/tui internal/domain internal/shelf internal/bookmark internal/savedcompare internal/cli internal/notes`; message
  `feat(clock): one freezable now for stored and drawn times`.

---

### Task 2: Virtual timers — `m.tick` + guard

**Files:**
- Create: `internal/tui/tick.go`, `internal/tui/tick_test.go`
- Modify: `internal/tui/entry_gone.go:31`, `commit_generate.go:59`,
  `files_worktree.go:354`, `files_view.go:452`, `op.go:181`, `notify.go:111`
  (each `tea.Tick(d, fn)` → `m.tick(d, fn)`; `heartbeatCmd()` becomes a
  `Model` method `m.heartbeatCmd()` and its two callers — `Init` and the
  `heartbeatMsg` case — call it on the model);
  `internal/tui/model.go` (add `quiet bool` to `Model`)

**Interfaces:**
- Produces: `func (m Model) tick(d time.Duration, fn func(time.Time) tea.Msg) tea.Cmd`;
  `type timerMsg struct{ due time.Duration; fire func(time.Time) tea.Msg }`;
  `Model.quiet bool` (set only by Headless).

- [ ] **Step 1: Write the failing tests** — `internal/tui/tick_test.go`:

```go
package tui

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// Quiet mode hands the loop a descriptor instead of sleeping: time never
// passes in a headless test, a step says when "the user paused".
func TestTickInQuietModeIsADescriptor(t *testing.T) {
	t.Parallel()
	var m Model
	m.quiet = true
	msg := m.tick(5*time.Second, func(time.Time) tea.Msg { return "fired" })()
	tm, ok := msg.(timerMsg)
	if !ok || tm.due != 5*time.Second || tm.fire(time.Time{}) != "fired" {
		t.Fatalf("quiet tick = %#v, want a timerMsg carrying due and fire", msg)
	}
}

// Every timer goes through m.tick, and no TUI command sleeps on the wall
// clock outside the allow-list — or a headless settle would wait on time.
func TestNoRawTimersOutsideTick(t *testing.T) {
	t.Parallel()
	raw := regexp.MustCompile(`tea\.(Tick|Every)\(|time\.(After|Sleep)\(`)
	allowed := map[string]string{
		"tick.go":    "the one tea.Tick call",
		"console.go": "waitSessionCmd's sleep: agent consoles never open in quiet mode",
	}
	files, _ := filepath.Glob("*.go")
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") || allowed[f] != "" {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(b), "\n") {
			if raw.MatchString(line) && !strings.HasPrefix(strings.TrimSpace(line), "//") {
				t.Errorf("%s:%d: raw timer %q — use m.tick (or allow-list it with a reason)", f, i+1, strings.TrimSpace(line))
			}
		}
	}
}
```

- [ ] **Step 2: Run** `go test ./internal/tui -run 'TestTickInQuietMode|TestNoRawTimers'` →
  FAIL (`m.tick`/`timerMsg`/`quiet` undefined).

- [ ] **Step 3: Implement** `internal/tui/tick.go`:

```go
package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// timerMsg is a timer in quiet mode: the headless loop parks it and fires
// it only when a step asks (Headless.FireTimers) — time never passes in a
// golden-screen test.
type timerMsg struct {
	due  time.Duration
	fire func(time.Time) tea.Msg
}

// tick is the TUI's one timer: tea.Tick normally, a timerMsg descriptor in
// quiet mode (headless.go).
func (m Model) tick(d time.Duration, fn func(time.Time) tea.Msg) tea.Cmd {
	if m.quiet {
		return func() tea.Msg { return timerMsg{due: d, fire: fn} }
	}
	return tea.Tick(d, fn)
}
```

  Add to the `Model` struct in `model.go`, near `recorder`:
  `quiet bool // headless golden-screen driver: no never-ending commands, virtual timers (headless.go)`.
  Convert the six `tea.Tick` sites to `m.tick(...)`. For `heartbeatCmd`
  (`op.go:180`) change to `func (m Model) heartbeatCmd() tea.Cmd { return m.tick(time.Second, func(time.Time) tea.Msg { return heartbeatMsg{} }) }`
  and update its callers (`grep -n 'heartbeatCmd()' internal/tui/*.go`).
  For `repo_popup.go:57` (`time.After`): move the stall behind `if !m.quiet`
  (read the function; keep behaviour identical when not quiet). If the
  function has no model, pass `quiet` in as a parameter from its caller.

- [ ] **Step 4: Run** `go test ./internal/tui -run 'TestTickInQuietMode|TestNoRawTimers'` → PASS; then
  `go test ./internal/tui` → PASS.

- [ ] **Step 5: Commit** — `gg add internal/tui`; message
  `feat(tui): one timer helper — virtual in quiet mode`.

---

### Task 3: Key tokens — `keyMsgFor`, Alt as `M-<x>`

**Files:**
- Modify: `internal/tui/recorder.go` (`keyToken`, `note`)
- Create: `internal/tui/keytoken.go`, `internal/tui/keytoken_test.go`
- Modify: `internal/tui/recorder_test.go` (the alt test that expects
  "# unrecorded key" — update it to expect the `M-` token)

**Interfaces:**
- Produces: `func keyMsgFor(tok string) (tea.KeyMsg, error)` — one token →
  one key; `func splitLiteral(tok string) []string` — a multi-rune literal
  → one token per rune (named tokens and chords pass through as one).

- [ ] **Step 1: Write the failing tests** — `internal/tui/keytoken_test.go`:

```go
package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// What the recorder writes is exactly what the headless driver accepts.
func TestKeyTokenRoundTrip(t *testing.T) {
	t.Parallel()
	toks := []string{"enter", "esc", "space", "tab", "up", "down", "left", "right",
		"bspace", "delete", "home", "end", "pgup", "pgdown",
		"C-t", "C-g", "C-c", "C-\\", "C-]", "M-a", "M-t", "M-down",
		"a", ".", "?", "#", "D", " "}
	for _, tok := range toks {
		msg, err := keyMsgFor(tok)
		if err != nil {
			t.Errorf("keyMsgFor(%q): %v", tok, err)
			continue
		}
		back, ok := keyToken(msg)
		if !ok || back != tok {
			t.Errorf("round trip %q → %#v → %q (ok=%v)", tok, msg, back, ok)
		}
	}
}

func TestKeyMsgForRejectsDiagnostics(t *testing.T) {
	t.Parallel()
	for _, bad := range []string{"<f1>", "", "C-", "M-"} {
		if _, err := keyMsgFor(bad); err == nil {
			t.Errorf("keyMsgFor(%q) must fail", bad)
		}
	}
}

// space is KeySpace carrying the rune, as Bubble Tea delivers it.
func TestSpaceCarriesItsRune(t *testing.T) {
	t.Parallel()
	msg, _ := keyMsgFor("space")
	if msg.Type != tea.KeySpace || string(msg.Runes) != " " {
		t.Fatalf("space = %#v", msg)
	}
}

func TestSplitLiteral(t *testing.T) {
	t.Parallel()
	cases := map[string][]string{"foo": {"f", "o", "o"}, "enter": {"enter"}, "C-t": {"C-t"}, "M-a": {"M-a"}, ".": {"."}}
	for in, want := range cases {
		got := splitLiteral(in)
		if len(got) != len(want) {
			t.Fatalf("splitLiteral(%q) = %q, want %q", in, got, want)
		}
		for i := range got {
			if got[i] != want[i] {
				t.Fatalf("splitLiteral(%q) = %q, want %q", in, got, want)
			}
		}
	}
}
```

- [ ] **Step 2: Run** `go test ./internal/tui -run 'TestKeyToken|TestKeyMsgFor|TestSpaceCarries|TestSplitLiteral'` → FAIL.

- [ ] **Step 3: Implement.** In `recorder.go` `keyToken`, replace the Alt
  early-return with:

```go
	if msg.Alt {
		plain := msg
		plain.Alt = false
		tok, ok := keyToken(plain)
		if !ok || strings.HasPrefix(tok, "<") || strings.HasPrefix(tok, "C-") {
			return "", false // alt+ctrl and alt+<unnamed> stay unrecorded
		}
		return "M-" + tok, true
	}
```

  and update its doc comment (Alt keys now record as `M-<token>`; alt+ctrl
  and unnamed ones remain `# unrecorded key` comments). `note` needs no
  change. Create `internal/tui/keytoken.go`:

```go
package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// namedKeys is keyToken's named vocabulary, inverted.
var namedKeys = map[string]tea.KeyType{
	"enter": tea.KeyEnter, "esc": tea.KeyEsc, "space": tea.KeySpace, "tab": tea.KeyTab,
	"up": tea.KeyUp, "down": tea.KeyDown, "left": tea.KeyLeft, "right": tea.KeyRight,
	"bspace": tea.KeyBackspace, "delete": tea.KeyDelete, "home": tea.KeyHome, "end": tea.KeyEnd,
	"pgup": tea.KeyPgUp, "pgdown": tea.KeyPgDown,
}

// ctrlKeys maps "ctrl+x" (tea.KeyMsg.String) to its KeyType, built once
// from Bubble Tea's own names so it can never disagree with keyToken.
var ctrlKeys = func() map[string]tea.KeyType {
	out := map[string]tea.KeyType{}
	for kt := tea.KeyType(-200); kt < 200; kt++ {
		if s := (tea.KeyMsg{Type: kt}).String(); strings.HasPrefix(s, "ctrl+") {
			out[s] = kt
		}
	}
	return out
}()

// keyMsgFor is keyToken's inverse: one keyscript token → one key.
func keyMsgFor(tok string) (tea.KeyMsg, error) {
	if strings.HasPrefix(tok, "M-") && len(tok) > 2 {
		k, err := keyMsgFor(tok[2:])
		if err != nil {
			return k, err
		}
		k.Alt = true
		return k, nil
	}
	if kt, ok := namedKeys[tok]; ok {
		k := tea.KeyMsg{Type: kt}
		if kt == tea.KeySpace {
			k.Runes = []rune{' '}
		}
		return k, nil
	}
	if strings.HasPrefix(tok, "C-") && len(tok) > 2 {
		if kt, ok := ctrlKeys["ctrl+"+tok[2:]]; ok {
			return tea.KeyMsg{Type: kt}, nil
		}
		return tea.KeyMsg{}, fmt.Errorf("unknown ctrl chord %q", tok)
	}
	if r := []rune(tok); len(r) == 1 {
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: r}, nil
	}
	return tea.KeyMsg{}, fmt.Errorf("not a single keyscript token: %q", tok)
}

// splitLiteral turns a step's token into presses: a named key or chord is
// one press, a multi-rune literal is one press per rune (as the recorder
// writes it).
func splitLiteral(tok string) []string {
	if _, err := keyMsgFor(tok); err == nil {
		return []string{tok}
	}
	var out []string
	for _, r := range tok {
		out = append(out, string(r))
	}
	return out
}
```

  If `TestKeyTokenRoundTrip` fails for `" "` (keyToken of a KeyRunes " "
  returns `" "`, keyMsgFor(" ") → KeyRunes " ") that is correct; if it
  fails for a ctrl chord, print `(tea.KeyMsg{Type: kt}).String()` for that
  type and fix the `ctrlKeys` range. Update `recorder_test.go`'s Alt test
  to expect a written `M-…` line.

- [ ] **Step 4: Run** `go test ./internal/tui -run 'TestKeyToken|TestKeyMsgFor|TestSpaceCarries|TestSplitLiteral|Recorder'` → PASS.

- [ ] **Step 5: Commit** — `gg add internal/tui`; message
  `feat(tui): keyscript tokens invert to keys; alt keys record as M-<x>`.

---

### Task 4: Quiet mode + `tui.Headless` (loop, start-up, drawing, teardown)

**Files:**
- Create: `internal/tui/headless.go`, `internal/tui/headless_test.go`
- Modify: `internal/tui/run.go` (extract `prepareModel`), the quiet gates in
  `model.go` (`Init` at :505; `configReadyMsg` handler at :1633-1634;
  `reRoot` at :4712; `heartbeatMsg` at :3231-3256; `steerStartedMsg` at
  :3267/:3275), `console.go:186` (`waitSessionsCmd`), the `waitTasksCmd`
  constructor (`task_track.go`), `webhost.go:136,240` (`waitWebSwitchCmd`),
  `open_files_watch.go:163,320,336` (`docWatchListenCmd`/`syncDocWatch`),
  `settings_tools.go` (`refreshToolStatusesCmd`)

**Interfaces:**
- Consumes: `m.tick`, `timerMsg`, `Model.quiet` (Task 2); `keyMsgFor`,
  `splitLiteral` (Task 3).
- Produces (all in package `tui`, exported):

```go
type HeadlessOptions struct {
	Width, Height int    // 0 → 160x40
	StatePath     string // per-scenario repos.toml/prompts.toml location; "" → none
}
type Headless struct{ /* unexported */ }
func NewHeadless(svc *domain.Service, opts HeadlessOptions) (*Headless, error)
func (h *Headless) Press(tok string) error // splitLiteral + one settle per press
func (h *Headless) FireTimers() error
func (h *Headless) Screen() string
func (h *Headless) Close()
```

- [ ] **Step 1: Extract `prepareModel`.** In `run.go`, move everything in
  `Run` from `m := New(svc)` through `m = m.initSteerInbox()` EXCEPT the
  `--web`/`--at` lines, `m.statePath = repos.DefaultStatePath()`, the
  `initHomeDir` lookup, the operation-log enable, and the steer lines, into

```go
// prepareModel is the model set-up tui.Run and the headless driver share,
// so the two cannot drift: config, theme, branch filters, tasks config,
// snapshot target.
func prepareModel(svc *domain.Service) Model
```

  and call it from `Run` (which keeps the excluded lines around it, in the
  same order as before). `go build ./... && go test ./internal/tui` → PASS
  (pure refactor).

- [ ] **Step 2: Write the failing tests** — `internal/tui/headless_test.go`:

```go
package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
)

// headlessRepo is a two-commit repo on main plus a "feature" branch at the
// FIRST commit, so a.txt differs between them (a dirty a.txt then makes a
// switch ask a decision — Task 6).
func headlessRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	git := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_DATE=2026-01-01T00:00:00Z", "GIT_COMMITTER_DATE=2026-01-01T00:00:00Z")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q", "-b", "main")
	git("config", "user.name", "t")
	git("config", "user.email", "t@t")
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a\n"), 0o644)
	git("add", ".")
	git("commit", "-qm", "first")
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("b\n"), 0o644)
	git("commit", "-qam", "second")
	git("branch", "feature", "HEAD~1")
	return dir
}

func newHeadless(t *testing.T, dir string) *Headless {
	t.Helper()
	h, err := NewHeadless(domain.OpenTUI(dir), HeadlessOptions{Width: 120, Height: 30, StatePath: filepath.Join(t.TempDir(), "repos.toml")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.Close)
	return h
}

// Start-up settles to a fixed point (quiet mode starts nothing that never
// ends) and draws the real screen.
func TestHeadlessStartsAndDraws(t *testing.T) {
	t.Parallel()
	h := newHeadless(t, headlessRepo(t))
	s := h.Screen()
	for _, want := range []string{"second", "first", "main"} {
		if !strings.Contains(s, want) {
			t.Fatalf("screen lacks %q:\n%s", want, s)
		}
	}
	if n := len(strings.Split(s, "\n")); n != 30 {
		t.Fatalf("screen has %d rows, want 30", n)
	}
	// A pause right after start-up also settles (the heartbeat's work is
	// gated in quiet mode).
	if err := h.FireTimers(); err != nil {
		t.Fatal(err)
	}
}

// Keys move the real model: the cursor walks the Commits list.
func TestHeadlessPressMovesTheModel(t *testing.T) {
	t.Parallel()
	h := newHeadless(t, headlessRepo(t))
	if err := h.Press("right"); err != nil { // focus Commits
		t.Fatal(err)
	}
	before := h.Screen()
	if err := h.Press("down"); err != nil {
		t.Fatal(err)
	}
	if h.Screen() == before {
		t.Fatal("down changed nothing on screen")
	}
	if err := h.Press("<f1>"); err == nil {
		t.Fatal("a diagnostic token must be rejected")
	}
}

// Timers are parked, not fired, until FireTimers: a sticky notice stays on
// screen after the step that raised it (Review Focus 2).
func TestHeadlessParksTimers(t *testing.T) {
	t.Parallel()
	h := newHeadless(t, headlessRepo(t))
	var c tea.Cmd
	h.m, c = h.m.stickyNotice("sticky-probe")
	if err := h.settle([]tea.Cmd{c}); err != nil { // the expiry timer parks
		t.Fatal(err)
	}
	if !strings.Contains(h.Screen(), "sticky-probe") {
		t.Fatal("the notice must be visible before any timer fires")
	}
	if len(h.timers) == 0 {
		t.Fatal("the notice's expiry must be parked")
	}
	if err := h.FireTimers(); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(h.Screen(), "sticky-probe") {
		t.Fatal("FireTimers must expire the notice")
	}
}

// A line wider than the terminal clips at the right edge — it never wraps
// into the next row (Review Focus 5).
func TestPaintClipsWideLines(t *testing.T) {
	t.Parallel()
	got := paintScreen(strings.Repeat("x", 15)+"\nsecond", 10, 3)
	want := "xxxxxxxxxx\nsecond\n"
	if got != want {
		t.Fatalf("paint = %q, want %q", got, want)
	}
}
```

- [ ] **Step 3: Run** `go test ./internal/tui -run 'TestHeadless|TestPaintClips'` → FAIL
  (`NewHeadless` undefined).

- [ ] **Step 4: Implement `headless.go`:**

```go
package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/vt"

	"github.com/homeend/gigagit/internal/domain"
)

// Headless drives the real Model without a terminal, for the e2e golden
// screens (docs/superpowers/specs/2026-10-01-tui-e2e-golden-screens-design.md).
// It replaces Bubble Tea's scheduler with its own loop: a key's commands
// run one at a time, in order, until none are left — the settled state —
// and the frame is then painted into an x/vt terminal and read back as
// text. Nothing here waits on a clock.
type Headless struct {
	m       Model
	w, h    int
	timers  []timerMsg
	opWait  *opWaitMsg // the held op waiter (op.go), nil when no op runs
	quit    bool
}

// HeadlessOptions sizes the terminal and isolates machine-global state.
type HeadlessOptions struct {
	Width, Height int
	StatePath     string
}

const (
	settleMaxMsgs = 500
	settleMaxTime = 30 * time.Second
)

// NewHeadless builds the model as tui.Run does, in quiet mode, and settles
// its start-up.
func NewHeadless(svc *domain.Service, opts HeadlessOptions) (*Headless, error) {
	w, hgt := opts.Width, opts.Height
	if w <= 0 || hgt <= 0 {
		w, hgt = 160, 40
	}
	m := prepareModel(svc)
	m.quiet = true
	m.statePath = opts.StatePath
	h := &Headless{m: m, w: w, h: hgt}
	if err := h.settle([]tea.Cmd{h.m.Init()}); err != nil {
		return nil, fmt.Errorf("start-up: %w", err)
	}
	if err := h.deliver(tea.WindowSizeMsg{Width: w, Height: hgt}); err != nil {
		return nil, fmt.Errorf("window size: %w", err)
	}
	return h, nil
}

// Press sends one step token (a multi-rune literal is one press per rune),
// settling after each press.
func (h *Headless) Press(tok string) error {
	for _, t := range splitLiteral(tok) {
		k, err := keyMsgFor(t)
		if err != nil {
			return err
		}
		if err := h.deliver(k); err != nil {
			return fmt.Errorf("key %q: %w", t, err)
		}
	}
	return nil
}

// FireTimers fires every timer parked right now, soonest first, settling
// after each; timers they create stay parked for the next call.
func (h *Headless) FireTimers() error {
	due := h.timers
	h.timers = nil
	sort.SliceStable(due, func(i, j int) bool { return due[i].due < due[j].due })
	for _, tm := range due {
		if err := h.deliver(tm.fire(time.Time{})); err != nil {
			return fmt.Errorf("timer (%s): %w", tm.due, err)
		}
	}
	return nil
}

// Screen is the settled frame as text: one line per terminal row, colours
// dropped, trailing spaces trimmed.
func (h *Headless) Screen() string {
	return strings.TrimSuffix(paintScreen(h.m.View(), h.w, h.h), "\n")
}

// deliver runs one message through the filter and Update, then settles.
func (h *Headless) deliver(msg tea.Msg) error {
	if h.quit {
		return fmt.Errorf("the TUI has quit")
	}
	cmd, err := h.update(msg)
	if err != nil {
		return err
	}
	return h.settle([]tea.Cmd{cmd})
}

// update applies quitFilter as the real program does, then Update.
func (h *Headless) update(msg tea.Msg) (tea.Cmd, error) {
	if msg = quitFilter(h.m, msg); msg == nil {
		return nil, nil
	}
	if _, ok := msg.(tea.QuitMsg); ok {
		h.quit = true
		return nil, nil
	}
	nm, cmd := h.m.Update(msg)
	m, ok := nm.(Model)
	if !ok {
		return nil, fmt.Errorf("Update returned %T, not Model", nm)
	}
	h.m = m
	return cmd, nil
}

// settle runs queued commands one at a time until none are left (the
// spec's §3.5): batches flatten, timers park, the op waiter is held and
// read only while the op is working (not while it waits on a decision).
func (h *Headless) settle(queue []tea.Cmd) error {
	start, n := time.Now(), 0
	seen := map[string]int{}
	for {
		for len(queue) > 0 {
			if n++; n > settleMaxMsgs || time.Since(start) > settleMaxTime {
				return fmt.Errorf("no fixed point after %d messages / %s; most frequent: %s", n, time.Since(start).Round(time.Millisecond), topTypes(seen))
			}
			c := queue[0]
			queue = queue[1:]
			if c == nil {
				continue
			}
			msg := c()
			switch v := msg.(type) {
			case nil:
				continue
			case tea.BatchMsg:
				queue = append(queue, v...)
				continue
			case timerMsg:
				h.timers = append(h.timers, v)
				continue
			case opWaitMsg:
				w := v
				h.opWait = &w
				continue
			}
			if isExecMsg(msg) {
				return fmt.Errorf("a terminal handover (%T) cannot run headless", msg)
			}
			if isSequenceMsg(msg) {
				return fmt.Errorf("tea.Sequence is not supported headless")
			}
			seen[fmt.Sprintf("%T", msg)]++
			cmd, err := h.update(msg)
			if err != nil {
				return err
			}
			if h.quit {
				return nil
			}
			queue = append(queue, cmd)
		}
		// Queue empty: settled, unless an op is working (not waiting on the
		// user) — then its next message is the next thing to happen.
		if h.opWait == nil || h.m.awaitingDecision() {
			return nil
		}
		w := h.opWait
		h.opWait = nil
		queue = append(queue, func() tea.Msg { return <-w.ch })
	}
}

func topTypes(seen map[string]int) string {
	type kv struct {
		k string
		v int
	}
	var all []kv
	for k, v := range seen {
		all = append(all, kv{k, v})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].v > all[j].v })
	var parts []string
	for i := 0; i < len(all) && i < 3; i++ {
		parts = append(parts, fmt.Sprintf("%s×%d", all[i].k, all[i].v))
	}
	return strings.Join(parts, ", ")
}

// isExecMsg / isSequenceMsg recognise Bubble Tea's unexported messages by
// type name (tea.Exec → execMsg; tea.Sequence → sequenceMsg).
func isExecMsg(msg tea.Msg) bool     { return fmt.Sprintf("%T", msg) == "tea.execMsg" }
func isSequenceMsg(msg tea.Msg) bool { return fmt.Sprintf("%T", msg) == "tea.sequenceMsg" }

// paintScreen writes a frame into an x/vt terminal with autowrap off and
// CRLF line ends (as Bubble Tea's renderer does), then reads the cells back.
func paintScreen(frame string, w, hgt int) string {
	emu := vt.NewEmulator(w, hgt)
	_, _ = emu.WriteString("\x1b[?7l" + strings.ReplaceAll(frame, "\n", "\r\n"))
	var b strings.Builder
	for y := 0; y < hgt; y++ {
		var row strings.Builder
		for x := 0; x < w; x++ {
			c := emu.CellAt(x, y)
			if c == nil || c.Width == 0 { // a wide glyph's continuation cell
				continue
			}
			if c.Content == "" {
				row.WriteByte(' ')
				continue
			}
			row.WriteString(c.Content)
		}
		b.WriteString(strings.TrimRight(row.String(), " "))
		b.WriteByte('\n')
	}
	return b.String()
}

// Close does what tui.Run does after the program ends.
func (h *Headless) Close() {
	m := h.m
	if m.opCancel != nil {
		m.opCancel()
	}
	killAll(context.Background())
	removeSnapshotFile(m.snapshotPath)
	m = m.closeSteerInbox()
	m = m.releaseKeptInboxes()
	m.closeWeb()
	m.cancelSubscriptions()
}
```

  Notes for the implementer, each checked while writing the code:
  - `quitFilter`'s signature: read `grep -n 'func quitFilter' internal/tui/*.go`
    and call it the way `tea.WithFilter` does (`func(tea.Model, tea.Msg) tea.Msg`).
  - `killAll`: factor the two `KillAll` lines of `Run` (`run.go:106-108`)
    into `func killAll(ctx context.Context)` (with its own `killAllGrace`
    timeout) and call it from both.
  - `cancelSubscriptions`: add a small `Model` method that cancels the
    `sessionWatch` and `taskTrack` subscriptions (read `console.go:42-51`
    and `task_track.go:30-38` for their cancel funcs; if none exists, add
    one there that the manager-change path also uses).
  - `x/vt` cell fields: confirm with
    `go doc github.com/charmbracelet/ultraviolet Cell` — adjust `Content`
    / `Width` names to the real ones.
  - `awaitingDecision` and `opWaitMsg` come in Task 6; until then write
    `func (m Model) awaitingDecision() bool { return false }` and
    `type opWaitMsg struct{ ch chan tea.Msg }` in `headless.go` with a
    `// Task 6` comment, so this task compiles.

- [ ] **Step 5: Quiet gates.** Add `if m.quiet { … }` guards so none of
  these start in quiet mode (each returns `nil` instead of its command):
  - `Init()` (`model.go:505`): drop `m.startSteerCmd(m.steerGen)`,
    `m.waitSessionsCmd()`, `m.waitTasksCmd()`, `m.startupWebCmd()`,
    `m.refreshToolStatusesCmd()` from the batch when quiet. Keep
    `bootstrapCmd`, `loadSearchHistCmd`, `m.heartbeatCmd()` (a parked
    timer now), `repoHealthCmd`.
  - Gate at the constructors, so every caller is covered:
    `startWatchCmd`, `steerListenCmd`, `waitSessionsCmd`, `waitTasksCmd`,
    `waitWebSwitchCmd`, `docWatchListenCmd`/`syncDocWatch`'s watcher start,
    `refreshToolStatusesCmd`, `startSteerCmd`, `startupWebCmd`,
    `webRerootCmd`. For free functions (`steerListenCmd`,
    `docWatchListenCmd`) gate at their call sites
    (`model.go:3267,3275`, `open_files_watch.go:320,336`).
  - `heartbeatMsg` handler (`model.go:3231-3256`): when quiet, run none of
    `refreshTick`, `prCommentsTick`, `openFilesTick`, `maybeWriteSnapshot`,
    `touchSteerPresence`, `tendKeptInboxes`, `drainSteer`,
    `expirePendingHint`; return `m, m.heartbeatCmd()` only (it re-parks).
  - `kickForgeProbe` stays (finite).

- [ ] **Step 6: Run** `go test ./internal/tui -run 'TestHeadless|TestPaintClips'` → PASS.
  If start-up fails with "no fixed point", the message names the looping
  type: find its constructor and gate it (Step 5). Then `go test ./internal/tui` → PASS.

- [ ] **Step 7: Commit** — `gg add internal/tui`; message
  `feat(tui): Headless — the real Model in a deterministic loop, drawn through x/vt`.

---

### Task 5: Process-global guards (theme, language) and the width check

**Files:**
- Modify: `internal/tui/headless.go`, `internal/tui/headless_test.go`

**Interfaces:**
- Produces: `Headless.Press`/`FireTimers` fail when the active theme or UI
  language differs from its value at `NewHeadless`.

- [ ] **Step 1: Write the failing test** (append; not parallel — it changes
  the process-global theme):

```go
// A scenario may not change the process-global theme or language: parallel
// scenarios share them (Review Focus 4).
func TestHeadlessRefusesGlobalChanges(t *testing.T) {
	prev := activeTheme()
	defer setTheme(prev)
	h := newHeadless(t, headlessRepo(t))
	setTheme(theme.Dark)
	if err := h.Press("down"); err == nil || !strings.Contains(err.Error(), "theme") {
		t.Fatalf("Press after a theme change = %v, want a theme error", err)
	}
}
```

  (import `github.com/homeend/gigagit/internal/theme`.)

- [ ] **Step 2: Run** `go test ./internal/tui -run TestHeadlessRefusesGlobalChanges` → FAIL.

- [ ] **Step 3: Implement.** Record `activeTheme().Name` and
  `i18n.Language()` (read `internal/i18n` for the getter's real name) in
  `NewHeadless`; at the end of `deliver`, compare and return
  `fmt.Errorf("the step changed the process-global %s (%q → %q); scenarios may not", …)`.

- [ ] **Step 4: Run** `go test ./internal/tui -run 'TestHeadless'` → PASS.

- [ ] **Step 5: Commit** — `gg add internal/tui`; message
  `feat(tui): Headless refuses steps that change the global theme or language`.

---

### Task 6: The op waiter as a descriptor

**Files:**
- Modify: `internal/tui/op.go` (`waitForOp`), every caller of `waitForOp`
  (`grep -n 'waitForOp(' internal/tui/*.go`), `internal/tui/headless.go`
  (drop the Task 4 stubs), `internal/tui/headless_test.go`

**Interfaces:**
- Produces: `type opWaitMsg struct{ ch chan tea.Msg }`;
  `func (m Model) waitForOp(msgs chan tea.Msg) tea.Cmd` (method);
  `func (m Model) awaitingDecision() bool` — true while `m.modal` is a
  `*decisionState` whose `reply` is non-nil (an engine decision is open).

- [ ] **Step 1: Write the failing tests** (append):

```go
// An op with no decision finishes inside the step: the loop reads the op
// waiter while the op works (Review Focus 1).
func TestHeadlessWaitsForAnOpToFinish(t *testing.T) {
	t.Parallel()
	dir := headlessRepo(t)
	h := newHeadless(t, dir)
	// Branches panel is focused at start; "feature" is the second row.
	for _, k := range []string{"down", "enter"} {
		if err := h.Press(k); err != nil {
			t.Fatal(err)
		}
	}
	if h.m.running || h.opWaitHeld() {
		t.Fatal("the switch must have finished inside the step")
	}
	out, _ := exec.Command("git", "-C", dir, "branch", "--show-current").Output()
	if strings.TrimSpace(string(out)) != "feature" {
		t.Fatalf("current branch = %q, want feature", out)
	}
}

// An op blocked on a decision settles with the modal on screen; the next
// key answers it and the op resumes (the review's blocker B1).
func TestHeadlessSettlesOnADecision(t *testing.T) {
	t.Parallel()
	dir := headlessRepo(t)
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("dirty\n"), 0o644) // a dirty switch asks
	h := newHeadless(t, dir)
	for _, k := range []string{"down", "enter"} {
		if err := h.Press(k); err != nil {
			t.Fatal(err)
		}
	}
	if !h.m.awaitingDecision() {
		// If SmartSwitch carried the change silently, read the screen and
		// pick the switch path that asks (see engine SmartSwitch decision
		// ids in docs/CLAUDE-details.md) — the test needs a real decision.
		t.Fatalf("want an open decision, screen:\n%s", h.Screen())
	}
	if err := h.Press("esc"); err != nil { // esc = abort
		t.Fatal(err)
	}
	if h.m.awaitingDecision() || h.m.running {
		t.Fatal("esc must answer the decision and let the op end")
	}
}
```

  Add `func (h *Headless) opWaitHeld() bool { return h.opWait != nil }` in
  `headless.go`. Before relying on the key sequence, confirm with
  `go test -run TestHeadlessPressMovesTheModel -v` plus a temporary
  `t.Log(h.Screen())` that "feature" is the second Branches row and that
  enter on a branch row switches (`enter` = checkout in the Branches panel;
  if it opens a menu instead, use the key `help.go` lists for checkout).

- [ ] **Step 2: Run** `go test ./internal/tui -run 'TestHeadlessWaitsForAnOp|TestHeadlessSettlesOnADecision'` →
  FAIL (the loop deadlocks → 30 s guard, or the op is still running).

- [ ] **Step 3: Implement.** In `op.go`:

```go
// opWaitMsg is the op waiter in quiet mode: the headless loop holds it and
// reads the channel only while the op works — a decision blocks the op
// goroutine until a key answers, so reading it then would deadlock.
type opWaitMsg struct{ ch chan tea.Msg }

// waitForOp blocks (off the UI thread) for the next op message.
func (m Model) waitForOp(msgs chan tea.Msg) tea.Cmd {
	if m.quiet {
		return func() tea.Msg { return opWaitMsg{ch: msgs} }
	}
	return func() tea.Msg { return <-msgs }
}

// awaitingDecision: an engine decision is open, the op is blocked on it.
func (m Model) awaitingDecision() bool {
	d, ok := m.modal.(*decisionState)
	return ok && d.reply != nil
}
```

  (Check `m.modal`'s declared type; if it is `*decisionState` rather than an
  interface, write `return m.modal != nil && m.modal.reply != nil`.) Replace
  every `waitForOp(x)` call with `m.waitForOp(x)` (use the model value in
  scope at each site). Delete the Task 4 stubs from `headless.go`.

- [ ] **Step 4: Run** the two tests → PASS; `go test ./internal/tui` → PASS.

- [ ] **Step 5: Commit** — `gg add internal/tui`; message
  `feat(tui): the op waiter is a descriptor in quiet mode — decisions settle headless`.

---

### Task 7: e2e environment — frozen clock, fixed git dates, SHELL, fake review tool

**Files:**
- Modify: `e2e/env_test.go` (TestMain), `e2e/runner.go` (`ExpandArgs` gains
  `{{ggfake}}`), `e2e/expand_test.go`
- Create: `e2e/internal/ggfake/main.go`, `e2e/fixtures/review.md`,
  `e2e/ggfake_test.go`

**Interfaces:**
- Produces: package var `ggFakeBin string` (set in TestMain);
  `ExpandArgs(argv []string, dir string) []string` also substitutes
  `{{ggfake}}`; `const frozenNow` (= `dateBase.Add(24 * time.Hour)`);
  `ExpandText(s, dir string) string` (the same substitution for `[input]`
  `write` content — the `.gg.toml` with the fake tool).

- [ ] **Step 1: Write `e2e/internal/ggfake/main.go`:**

```go
// Command ggfake stands in for an AI agent in e2e scenarios: `ggfake review`
// prints the canned review document next to this source tree's fixtures.
package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 || os.Args[1] != "review" {
		fmt.Fprintln(os.Stderr, "usage: ggfake review")
		os.Exit(2)
	}
	b, err := os.ReadFile(os.Getenv("GGFAKE_REVIEW"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Stdout.Write(b)
}
```

  and `e2e/fixtures/review.md` — a valid gg review document. Copy the
  shape `s80_cli_review.toml` feeds its tool (open it; if it inlines the
  document, move that text here) so `gg review` stores it without the
  "not in gg review format" warning.

- [ ] **Step 2: Write the failing test** — `e2e/ggfake_test.go`:

```go
package e2e

import (
	"os/exec"
	"strings"
	"testing"
)

func TestGGFakePrintsTheReview(t *testing.T) {
	t.Parallel()
	out, err := exec.Command(ggFakeBin, "review").Output()
	if err != nil || !strings.Contains(string(out), "Verdict") {
		t.Fatalf("ggfake review: %v\n%s", err, out)
	}
}
```

  (Adjust `"Verdict"` to a heading that is in `review.md`.) Add to
  `expand_test.go`: `ExpandArgs([]string{"{{ggfake}} review"}, "/x")[0]`
  must equal the quoted `ggFakeBin` + `" review"`.

- [ ] **Step 3: Run** `go test ./e2e -run 'TestGGFake|TestExpand'` → FAIL.

- [ ] **Step 4: Implement in `TestMain`** (before `m.Run()`):

```go
	// The fake agent (e2e/internal/ggfake), built once; scenarios name it
	// as {{ggfake}} in a capture tool's command.
	ggFakeBin = filepath.Join(dir, "ggfake"+exeSuffix())
	if out, err := exec.Command("go", "build", "-o", ggFakeBin, "./internal/ggfake").CombinedOutput(); err != nil {
		panic(fmt.Sprintf("build ggfake: %v\n%s", err, out))
	}
	abs, _ := filepath.Abs(filepath.Join("fixtures", "review.md"))
	os.Setenv("GGFAKE_REVIEW", abs)
	// Capture commands run through $SHELL (engine/capture_runner.go); a
	// developer's zsh/fish must not decide how a scenario's command parses.
	if runtime.GOOS != "windows" {
		os.Setenv("SHELL", "/bin/sh")
	}
	// One frozen "now" for every stored/drawn time, and one date for every
	// commit gg itself makes: scenarios run in parallel in one process, so
	// neither can be per scenario (spec §4).
	clock.Freeze(frozenNow)
	os.Setenv("GIT_AUTHOR_DATE", frozenNow.Format(time.RFC3339))
	os.Setenv("GIT_COMMITTER_DATE", frozenNow.Format(time.RFC3339))
```

  with `func exeSuffix() string { if runtime.GOOS == "windows" { return ".exe" }; return "" }`,
  `var ggFakeBin string`, and in `builder.go` next to `dateBase`:
  `var frozenNow = dateBase.Add(24 * time.Hour)`. The builder's own git
  calls pass their dates per command (`builder.go:43`), overriding the
  process-wide value — unchanged. In `runner.go`, extend `ExpandArgs` and
  add `ExpandText`:

```go
// ExpandText substitutes {{ggfake}} (the fake agent, quoted for the shell
// capture commands run through) in scenario-supplied text.
func ExpandText(s string) string {
	return strings.ReplaceAll(s, "{{ggfake}}", shellQuote(ggFakeBin))
}

func shellQuote(p string) string {
	if runtime.GOOS == "windows" {
		return `"` + p + `"`
	}
	return "'" + strings.ReplaceAll(p, "'", `'\''`) + "'"
}
```

  call `ExpandText` on every argument in `ExpandArgs`, and in
  `builder.go`'s `"write"` case write `ExpandText(st.Content)`.

- [ ] **Step 5: Run** `go test ./e2e -run 'TestGGFake|TestExpand'` → PASS.

- [ ] **Step 6: The fixed date against the existing scenarios.** Run
  `go test ./e2e -run 'TestScenarios/(s82|s95|version|migrate)' -count=1`
  then the whole `go test ./e2e -count=1`. If a scenario fails because two
  gg-made commits now tie on date or share a SHA, record which one in the
  commit message and fall back as the spec says: move the two
  `GIT_*_DATE` Setenv lines out of TestMain into the TUI scenario runner,
  and run TUI scenarios serially (Task 8 then drops `t.Parallel()` for
  scenarios with a `[tui]` block). Otherwise → PASS, keep it.

- [ ] **Step 7: Commit** — `gg add e2e`; message
  `test(e2e): fake review agent, frozen clock and fixed gg commit dates`.

---

### Task 8: `[tui]` scenarios — parse, run, goldens

**Files:**
- Modify: `e2e/scenario.go` (types + validation), `e2e/harness_test.go`
  (run the TUI after `[[run]]`s), `e2e/builder.go` (fixed root for TUI
  scenarios), `.gitignore` (`*.txt.actual`)
- Create: `e2e/tui.go`, `e2e/tui_test.go`

**Interfaces:**
- Consumes: `tui.NewHeadless`, `Headless.Press/FireTimers/Screen/Close`
  (Tasks 4–6); `ExpandText` (Task 7).
- Produces:

```go
type TUI struct {
	Size  string    `toml:"size"` // "COLSxROWS", default "160x40"
	Steps []TUIStep `toml:"step"`
}
type TUIStep struct {
	Name           string   `toml:"name"`
	Keys           []string `toml:"keys"`
	Wait           bool     `toml:"wait"`
	ScreenContains []string `toml:"screen_contains"`
	ScreenExcludes []string `toml:"screen_excludes"`
}
// Scenario gains: TUI *TUI `toml:"tui"`
func runTUI(t *testing.T, sb *Sandbox, sc *Scenario, file string)
var update = flag.Bool("update", false, "rewrite TUI golden screens")
```

- [ ] **Step 1: Write the failing tests** — `e2e/tui_test.go`:

```go
package e2e

import (
	"strings"
	"testing"
)

func TestParseTUIBlock(t *testing.T) {
	t.Parallel()
	sc, err := parseScenario([]byte(`name = "x"
[tui]
size = "100x30"
[[tui.step]]
name = "commits"
keys = ["right"]
screen_excludes = ["◆ 1"]
[[tui.step]]
keys = ["down"]
wait = true
`))
	if err != nil {
		t.Fatal(err)
	}
	if sc.TUI == nil || sc.TUI.Size != "100x30" || len(sc.TUI.Steps) != 2 || sc.TUI.Steps[0].Name != "commits" || !sc.TUI.Steps[1].Wait {
		t.Fatalf("parsed %+v", sc.TUI)
	}
}

func TestTUIValidation(t *testing.T) {
	t.Parallel()
	for _, bad := range []string{
		"[tui]\nsize = \"wide\"\n",                                       // bad size
		"[tui]\n[[tui.step]]\nname = \"A B\"\n",                           // bad name
		"[tui]\n[[tui.step]]\nname = \"a\"\n[[tui.step]]\nname = \"a\"\n", // duplicate
	} {
		if _, err := parseScenario([]byte("name = \"x\"\n" + bad)); err == nil {
			t.Errorf("want an error for:\n%s", bad)
		}
	}
}

func TestRootPlaceholderKeepsWidth(t *testing.T) {
	t.Parallel()
	root := "/tmp/gg-tui/abc"
	got := normalizeRoot("x "+root+"/local y", root)
	if !strings.Contains(got, "{{root}}") || len([]rune(got)) != len([]rune("x "+root+"/local y")) {
		t.Fatalf("normalizeRoot = %q (width must not change)", got)
	}
}
```

  (If `LoadScenario` does not already split into a byte-level
  `parseScenario`, split it: `LoadScenario` reads the file and calls
  `parseScenario(b)`.)

- [ ] **Step 2: Run** `go test ./e2e -run 'TestParseTUI|TestTUIValidation|TestRootPlaceholder'` → FAIL.

- [ ] **Step 3: Implement** the types in `scenario.go` and validation in
  `parseScenario` (size matches `^\d+x\d+$`; names match `^[a-z0-9-]+$` and
  are unique; `keys` non-empty unless `wait` is set). Then `e2e/tui.go`:

```go
package e2e

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/tui"
)

var update = flag.Bool("update", false, "rewrite TUI golden screens")

// runTUI drives the scenario's [tui] steps against the built sandbox and
// compares every named checkpoint with <scenario>.screens/NN-<name>.txt.
func runTUI(t *testing.T, sb *Sandbox, sc *Scenario, file string) {
	t.Helper()
	w, h := parseSize(sc.TUI.Size)
	svc := domain.OpenTUI(sb.LocalDir)
	hd, err := tui.NewHeadless(svc, tui.HeadlessOptions{Width: w, Height: h, StatePath: filepath.Join(sb.Root, "state", "repos.toml")})
	if err != nil {
		t.Fatalf("tui start: %v", err)
	}
	defer hd.Close()
	dir := strings.TrimSuffix(file, ".toml") + ".screens"
	wanted := map[string]bool{}
	ord := 0
	var keysSoFar []string
	for i, st := range sc.TUI.Steps {
		for _, k := range st.Keys {
			if err := hd.Press(k); err != nil {
				t.Fatalf("tui step %d (%s) key %q: %v", i, st.Name, k, err)
			}
			keysSoFar = append(keysSoFar, k)
		}
		if st.Wait {
			if err := hd.FireTimers(); err != nil {
				t.Fatalf("tui step %d (%s) wait: %v", i, st.Name, err)
			}
			keysSoFar = append(keysSoFar, "<wait>")
		}
		screen := normalizeRoot(hd.Screen(), sb.Root)
		for _, s := range st.ScreenContains {
			if !strings.Contains(screen, s) {
				t.Fatalf("tui step %d (%s): screen lacks %q\n%s", i, st.Name, s, screen)
			}
		}
		for _, s := range st.ScreenExcludes {
			if strings.Contains(screen, s) {
				t.Fatalf("tui step %d (%s): screen contains %q\n%s", i, st.Name, s, screen)
			}
		}
		if st.Name == "" {
			continue
		}
		ord++
		golden := filepath.Join(dir, fmt.Sprintf("%02d-%s.txt", ord, st.Name))
		wanted[filepath.Base(golden)] = true
		compareGolden(t, golden, screen, st.Name, keysSoFar)
	}
	checkStaleGoldens(t, dir, wanted)
}

func parseSize(s string) (int, int) {
	if s == "" {
		return 160, 40
	}
	c, r, _ := strings.Cut(s, "x")
	w, _ := strconv.Atoi(c)
	h, _ := strconv.Atoi(r)
	return w, h
}

// normalizeRoot replaces the sandbox root with {{root}} padded or cut to
// the root's own display width, so column alignment survives.
func normalizeRoot(screen, root string) string {
	ph := "{{root}}"
	n := len([]rune(root))
	switch {
	case len(ph) < n:
		ph += strings.Repeat("_", n-len(ph))
	case len(ph) > n:
		ph = ph[:n]
	}
	return strings.ReplaceAll(screen, root, ph)
}

func compareGolden(t *testing.T, golden, screen, name string, keys []string) {
	t.Helper()
	if *update {
		if err := os.MkdirAll(filepath.Dir(golden), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, []byte(screen+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	if runtime.GOOS == "windows" {
		return // paths differ; contains/excludes already ran (spec §4)
	}
	b, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("checkpoint %q: no golden %s (run with -update, then READ it before committing)", name, golden)
	}
	want := strings.TrimSuffix(string(b), "\n")
	if want == screen {
		_ = os.Remove(golden + ".actual")
		return
	}
	_ = os.WriteFile(golden+".actual", []byte(screen+"\n"), 0o644)
	t.Fatalf("checkpoint %q (keys %v) differs from %s:\n%s", name, keys, golden, lineDiff(want, screen))
}

// checkStaleGoldens fails on a golden with no checkpoint (a renamed step),
// or deletes it under -update (Review Focus 3).
func checkStaleGoldens(t *testing.T, dir string, wanted map[string]bool) {
	t.Helper()
	files, _ := filepath.Glob(filepath.Join(dir, "*.txt"))
	for _, f := range files {
		if wanted[filepath.Base(f)] {
			continue
		}
		if *update {
			_ = os.Remove(f)
			continue
		}
		t.Fatalf("stale golden %s: no checkpoint produces it (rename it, or run -update)", f)
	}
}

// lineDiff marks each differing row: "-" golden, "+" actual.
func lineDiff(want, got string) string {
	a, b := strings.Split(want, "\n"), strings.Split(got, "\n")
	var out strings.Builder
	for i := 0; i < len(a) || i < len(b); i++ {
		var x, y string
		if i < len(a) {
			x = a[i]
		}
		if i < len(b) {
			y = b[i]
		}
		if x != y {
			fmt.Fprintf(&out, "row %d\n- %s\n+ %s\n", i+1, x, y)
		}
	}
	return out.String()
}
```

  In `harness_test.go`, after the `[[run]]` loop and before
  `assertExpect`: `if sc.TUI != nil { runTUI(t, sb, sc, file) }`. In
  `buildSandbox`, when `sc.TUI != nil`, use a fixed root instead of
  `t.TempDir()`:

```go
	root := t.TempDir()
	if sc.TUI != nil {
		root = filepath.Join(os.TempDir(), "gg-tui", sc.fileStem)
		_ = os.RemoveAll(root)
		if err := os.MkdirAll(root, 0o755); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.RemoveAll(root) })
	}
```

  (`sc.fileStem`: set it in `LoadScenario` from the file's base name; add
  the field with `toml:"-"`.) Add `*.txt.actual` to `.gitignore`.

- [ ] **Step 4: Run** `go test ./e2e -run 'TestParseTUI|TestTUIValidation|TestRootPlaceholder'` → PASS.

- [ ] **Step 5: A smoke scenario** — `e2e/scenarios/tui_smoke.toml`:

```toml
name = "tui smoke: the TUI starts on a two-commit repo and draws the Commits list"

[input]
steps = [
  { write = "a.txt", content = "a\n" }, { commit = "first" },
  { write = "a.txt", content = "b\n" }, { commit = "second" },
]

[tui]
size = "120x30"

[[tui.step]]
name = "start"
keys = ["right"]
screen_contains = ["second", "first"]
```

  Run `go test ./e2e -run 'TestScenarios/tui_smoke' -update -count=1`,
  then **read** `e2e/scenarios/tui_smoke.screens/01-start.txt` in full and
  check it shows the real start screen (header with `{{root}}` path, both
  commits, no stray escape codes). Run again without `-update` → PASS.
  Rename the step to `begin` and run → FAIL "stale golden"; rename back.

- [ ] **Step 6: Commit** — `gg add e2e .gitignore`; message
  `test(e2e): [tui] scenario steps with golden screens and -update`.

---

### Task 9: The first real scenarios (watch them fail first)

**Files:**
- Create: `e2e/scenarios/tui_review_mixed.toml`, `e2e/scenarios/tui_reading_width.toml`
  and their `.screens/` directories

- [ ] **Step 1: `tui_review_mixed.toml`** — the spec §2 example verbatim
  (with `switch`, the per-scenario `.gg.toml` carrying the `fake` tool as
  an `[input]` write, the note and both reviews as `[[run]]`s), plus
  steps that pin each item of spec §6.1:

```toml
[tui]
size = "160x40"

[[tui.step]]
name = "commits"
keys = ["right"]
screen_contains = ["✎"]
screen_excludes = ["◆ 1"]

[[tui.step]]
name = "files"
keys = ["enter"]
screen_contains = ["Reviews", "◆ 1"]

[[tui.step]]
name = "stack"
keys = ["down", "down", "enter"]
screen_excludes = ["@notes/"]

[[tui.step]]
name = "step-back"
keys = ["P"]
screen_excludes = ["@notes/"]
```

  Adjust the key paths by reading each screen (`t.Log` via `-v`, or the
  `.actual` files): the Commits cursor must be on the reviewed commit, the
  Files view must list the Reviews row above `a.txt`/`b.txt`, and the
  stack must be opened from a file row. Then `-update`, and **read every
  golden** against spec §6.1.

- [ ] **Step 2: Watch it fail.** In a scratch copy of the tree
  (`git stash push -m tui-watch-fail-<date>` is forbidden — use a temporary
  commit instead): revert `fb2ec0f1`'s one-line fix in
  `internal/domain/notes.go` (the `continue` after a review note in
  `NoteCounts`), run the scenario → it must FAIL on `screen_excludes ["◆ 1"]`
  (or the golden). Revert `97be8e58`'s skip in `diff_stack_keys.go` → it
  must FAIL on `@notes/`. Record both failing outputs in the commit
  message body, then restore the fixes (`git checkout -- <files>`) and
  confirm PASS.

- [ ] **Step 3: `tui_reading_width.toml`** — size `200x45`; one long note
  (`gg note add … --summary "<200+ chars>"`) and a review via the fake
  tool; steps: open View all notes (read `help.go`/the palette for its key;
  if it is palette-only, drive the palette: `:` or the palette key, type
  the command name, `enter`), `C-t`, checkpoint `all-notes`; close; open
  the review overview (Files view of the reviewed commit, the `≡ Overview`
  row, `enter`), `C-t`, checkpoint `overview`. `screen_contains` a word
  from the note's END (it must wrap, not cut). `-update`, **read** the
  goldens: text column ≤120 wide and centred. Watch it fail by setting
  `readingWidthDefault` to 1000 temporarily → the goldens differ; restore.

- [ ] **Step 4: Run** `go test ./e2e -count=1` → PASS (all scenarios).

- [ ] **Step 5: Commit** — `gg add e2e`; message
  `test(e2e): golden screens for mixed reviews/notes and the reading width`.

---

### Task 10: Skills, docs, full gate

**Files:**
- Modify: `.claude/skills/writing-e2e-scenarios/SKILL.md`,
  `.claude/skills/adding-features/SKILL.md`, `CLAUDE.md` (the `e2e` and
  `tui` rows), `CHANGELOG.md`

- [ ] **Step 1: `writing-e2e-scenarios`** — add a "TUI scenarios" section:
  the `[tui]`/`[[tui.step]]` fields (copy the table from spec §2), the fake
  review tool pattern (`{{ggfake}} review` in a per-scenario `.gg.toml`),
  `-update` then READ every new/changed golden before committing, the
  mixed-feature rule, the bug-from-a-real-repo procedure (spec §1), what
  cannot be checkpointed (AI-tasks surfaces, tool-update notices, agent
  consoles, handovers), `wait = true` semantics, Windows skips golden bytes.
- [ ] **Step 2: `adding-features`** — checklist item: "A feature with a TUI
  surface ships a TUI scenario with golden screens of that surface
  (writing-e2e-scenarios → TUI scenarios)."
- [ ] **Step 3: CLAUDE.md** — `e2e` row: append "+ TUI screens (`[tui]`
  steps drive `tui.Headless` → golden files, `-update`)"; `tui` row:
  append "`Headless` (quiet mode, virtual timers) drives the Model for e2e
  golden screens". Keep each row one line.
- [ ] **Step 4: CHANGELOG** — a new top section "TUI golden-screen tests"
  (Added: `[tui]` scenario steps, `tui.Headless`, `internal/clock`,
  `M-<x>` recording of Alt keys).
- [ ] **Step 5: Full gate** —
  `cd <wt> && ./test.sh race > /tmp/claude-1000/-work-gigagit/a403a284-e3c5-4673-a528-e986250a112e/scratchpad/race-tui-e2e.log 2>&1; echo exit=$?`;
  the log must end with `all green` and contain no `FAIL`.
- [ ] **Step 6: Commit** — `gg add .claude/skills CLAUDE.md CHANGELOG.md`;
  message `docs: TUI golden-screen scenarios in the skills, CLAUDE.md and CHANGELOG`.
- [ ] **Step 7: Final review** — one read-only review subagent (most
  capable model) over `git diff main...HEAD`, then address its findings.
