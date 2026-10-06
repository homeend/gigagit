# alt+a / alt+t cycle with a return point — Implementation Plan

> **For agentic workers:** this repo forbids implementer subagents (CLAUDE.md):
> execute with superpowers:executing-plans in the main session. Steps use
> checkbox (`- [ ]`) syntax for tracking.

**Goal:** alt+a / alt+t cycle the repository's sessions from any screen —
focused agent, panels, a full-screen view — and every cycle passes once
through the screen the user came from.

**Architecture:** a console remembers the screen it was shown over
(`consoleReturn`, captured on the first show, carried over when a console
replaces a console). Closing a console restores it; the ring's last stop
closes the console. A full-screen view origin is parked off the live layer
stack and the console shows maximised. No new rendering.

**Tech Stack:** Go 1.26, Bubble Tea v1 (`internal/tui`).

**Spec:** `docs/superpowers/specs/2026-10-06-alt-a-cycle-design.md`

## Global Constraints

- TUI only; gg web unchanged; keys not configurable.
- Every user-visible TUI string through `i18n.T` with a literal key present
  in ja/ko/zh/ru (`internal/i18n/lang/*.toml`).
- Unfocused shows never `Touch` a session (the walk must not reorder).
- New tests that start sessions are serial (the session manager is global);
  pure tests call `t.Parallel()`.
- Work only in `/work/gigagit/.claude/worktrees/alt-a-cycle`, absolute paths.

## Review Focus

1. A session exits/removed while shown full-screen over a parked diff → the
   diff comes back (never lost). Pinned in Task 2.
2. A popup (`.` menu, `?`, ctrl+\) opened over the full-screen agent, then
   the console returns → the popup stays on top, the diff beneath. Task 2.
3. Repo switch while a diff is parked → parked view discarded, no stale
   stash/preview restored. Task 2.
4. Mouse click/tab while a full-screen agent shows → focus never leaves the
   console's column (the hidden panels must not take keys). Task 3.
5. An unfocused full-screen console renders the session at its current PTY
   size (the existing "unfocused never resizes" rule): a session last
   focused docked shows docked-sized until enter. Kept deliberately; called
   out in the handoff, no test change.

---

### Task 1: The return point, the ring's return stop, alt+a inside a focused console

**Files:**
- Modify: `internal/tui/console.go` (consoleState, showConsole, dropConsole,
  closeConsole, cycleSessions, updateConsoleKey focused branch,
  onSessionsChanged removal arm)
- Modify: `internal/tui/last_session_test.go` (wrap test, focused test)
- Modify: `internal/tui/console_test.go` (`TestOpenConsoleClearsFullscreenPin`)
- Create: `internal/tui/alt_cycle_test.go`

**Interfaces:**
- Produces: `type consoleReturn struct{ layers []layer; full bool; fullMaxed bool; fullMax panel; stashView *stashView; filesPreview *openFile; focus panel }`;
  `consoleState.ret *consoleReturn`; `(Model) captureReturn() (Model, *consoleReturn)`;
  `(Model) detachConsole() Model` (old dropConsole body);
  `(Model) consoleFull() bool`; `closeConsole` = return to `ret`.

- [ ] **Step 1: Write the failing tests** — `internal/tui/alt_cycle_test.go`:

```go
package tui

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
)

func pressAlt(t *testing.T, m Model, r rune) Model {
	t.Helper()
	mm, _ := m.Update(altKey(r))
	return mm.(Model)
}

// The ring ends on the screen the cycle started from: the stash list in the
// right column comes back, focus with it.
func TestAltAReturnsToTheStartingScreen(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	s := startTestSession(t, m, `sleep 5`)
	sv := &stashView{tag: "stash"}
	m.stashView, m.focus = sv, panelCommits
	m = pressAlt(t, m, 'a')
	if m.console == nil || m.console.id != s.Info().ID || m.console.focused || m.stashView != nil {
		t.Fatalf("first alt+a: console=%+v stash=%v", m.console, m.stashView)
	}
	m = pressAlt(t, m, 'a')
	if m.console != nil || m.stashView != sv || m.focus != panelCommits {
		t.Fatalf("second alt+a must return: console=%+v stash=%v focus=%v", m.console, m.stashView, m.focus)
	}
}

// From a focused agent the walk starts at the agent used before it, never
// Touches, and its return stop is the screen before the agent was shown.
func TestAltAFromFocusedAgentReturnsToTheScreenBeforeIt(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	a := startTestSession(t, m, `sleep 5`)
	b := startSecondSession(t, a, "b", false)
	m.focus = panelWorktrees
	m, _ = m.openConsole(a.Info().ID) // a is now the most recent
	usedA, usedB := a.Info().LastUsed, b.Info().LastUsed
	m = pressAlt(t, m, 'a')
	if m.console == nil || m.console.id != b.Info().ID || m.console.focused {
		t.Fatalf("alt+a in focused a: console=%+v, want b unfocused", m.console)
	}
	if !a.Info().LastUsed.Equal(usedA) || !b.Info().LastUsed.Equal(usedB) {
		t.Fatal("the walk touched a session")
	}
	m = pressAlt(t, m, 'a')
	if m.console != nil || m.focus != panelWorktrees {
		t.Fatalf("return stop: console=%+v focus=%v, want the Worktrees panel", m.console, m.focus)
	}
	m = pressAlt(t, m, 'a')
	if m.console == nil || m.console.id != a.Info().ID {
		t.Fatalf("after the return stop: console=%+v, want a", m.console)
	}
}

// One agent, focused: alt+a goes straight back.
func TestAltAWithOneFocusedAgentReturns(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	a := startTestSession(t, m, `sleep 5`)
	m.focus = panelBranches
	m, _ = m.openConsole(a.Info().ID)
	m = pressAlt(t, m, 'a')
	if m.console != nil || m.focus != panelBranches {
		t.Fatalf("console=%+v focus=%v", m.console, m.focus)
	}
}

// alt+t in the middle of an alt+a cycle walks terminals; the return point
// survives the switch of kind.
func TestAltTMidCycleKeepsTheReturnPoint(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	a := startTestSession(t, m, `sleep 5`)
	term := startSecondSession(t, a, "Terminal", true)
	sv := &stashView{tag: "stash"}
	m.stashView, m.focus = sv, panelCommits
	m = pressAlt(t, m, 'a')
	m = pressAlt(t, m, 't')
	if m.console == nil || m.console.id != term.Info().ID {
		t.Fatalf("alt+t: console=%+v, want the terminal", m.console)
	}
	m = pressAlt(t, m, 't')
	if m.console != nil || m.stashView != sv {
		t.Fatalf("alt+t return stop: console=%+v stash=%v", m.console, m.stashView)
	}
}

// A ctrl+t-pinned panel is a full-screen return point: the console shows
// maximised, the pin comes back on return.
func TestAltAFromPinnedPanelShowsFullAndRestoresThePin(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	startTestSession(t, m, `sleep 5`)
	m.focus = panelBranches
	m = press(t, m, "ctrl+t")
	if !m.fullMaxActive() {
		t.Fatal("baseline: Branches should be pinned")
	}
	m = pressAlt(t, m, 'a')
	if m.console == nil || !m.console.maximized || m.console.focused || m.fullMaxed {
		t.Fatalf("console=%+v fullMaxed=%v", m.console, m.fullMaxed)
	}
	m = pressAlt(t, m, 'a')
	if m.console != nil || !m.fullMaxActive() || m.fullMax != panelBranches || m.focus != panelBranches {
		t.Fatalf("return: console=%+v pin=%v/%v focus=%v", m.console, m.fullMaxed, m.fullMax, m.focus)
	}
}

// gg takes alt+a even inside a focused console.
func TestFocusedConsoleGivesAltAToTheCycle(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	s := startTestSession(t, m, `sleep 5`)
	other := startSecondSession(t, s, "other", false)
	time.Sleep(2 * time.Millisecond)
	other.Touch()
	m, _ = m.openConsole(s.Info().ID) // s most recent, other next
	m = pressAlt(t, m, 'a')
	if m.console == nil || m.console.id != other.Info().ID || m.console.focused {
		t.Fatalf("console = %+v: alt+a in a focused console cycles", m.console)
	}
	_ = domain.SessionRunning
	_ = tea.KeyMsg{}
}
```

(Drop the two `_ =` lines if the imports end up used elsewhere in the file
by Task 2/3 tests; they only keep this step compiling.)

Edit `internal/tui/last_session_test.go`:
- `TestAltACyclesAgentsByLastUseAndEnterPromotes`: replace the block from
  `press(altKey('a'))\n\tshows(a, "fourth alt+a wraps")` through
  `shows(c, "back on the third")` with:

```go
	press(altKey('a'))
	if m.console != nil {
		t.Fatalf("fourth alt+a returns to the starting screen, console = %+v", m.console)
	}
	press(altKey('a'))
	shows(a, "fifth alt+a starts over")
	press(altKey('a'))
	press(altKey('a'))
	shows(c, "back on the third")
```

- Delete `TestFocusedConsoleKeepsAltA` (superseded by
  `TestFocusedConsoleGivesAltAToTheCycle`).

Edit `internal/tui/console_test.go` — replace `TestOpenConsoleClearsFullscreenPin`
(its premise changed: a pin is now a full-screen return point):

```go
// Opening a console while a panel is pinned fullscreen shows it maximised
// (the pin is a full-screen return point) and closing it brings the pin back.
func TestOpenConsoleOverPinShowsFullAndRestoresPin(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	s := startTestSession(t, m, `sleep 5`)
	m.focus = panelBranches
	m = press(t, m, "ctrl+t")
	if !m.fullMaxActive() {
		t.Fatal("baseline: Branches should be fullscreen")
	}
	m, _ = m.openConsole(s.Info().ID)
	if m.fullMaxed || !m.console.focused || !m.console.maximized || m.focus != panelCommits {
		t.Fatalf("after open: fullMaxed=%v console=%+v focus=%v", m.fullMaxed, m.console, m.focus)
	}
	w, h := m.consoleBox()
	cols, rows := consoleInner(w, h)
	if sc := s.Screen(); sc.Cols != cols || sc.Rows != rows {
		t.Fatalf("emulator %dx%d, want the body %dx%d", sc.Cols, sc.Rows, cols, rows)
	}
	m = m.closeConsole()
	if !m.fullMaxActive() || m.fullMax != panelBranches || m.focus != panelBranches {
		t.Fatalf("after close: pin=%v/%v focus=%v", m.fullMaxed, m.fullMax, m.focus)
	}
}
```

- [ ] **Step 2: Run to see them fail**

Run: `cd /work/gigagit/.claude/worktrees/alt-a-cycle && go test ./internal/tui -run 'AltA|AltT|FocusedConsoleGives|OpenConsoleOverPin|CloseConsole|ConsoleClosesWhen' -count=1`
Expected: FAIL (wrap still wraps; focused console keeps alt+a; pin cleared).

- [ ] **Step 3: Implement in `internal/tui/console.go`**

Add after `consoleState`:

```go
// consoleReturn is the screen a console was shown over: where esc, the
// step-out key twice and the last stop of an alt+a / alt+t cycle go back
// to. Captured by the first console shown, carried over when a console
// replaces a console, so "the screen before the agent" survives a cycle.
type consoleReturn struct {
	layers       []layer // the live stack, parked while the console shows
	full         bool    // a full-screen view or a ctrl+t pin: consoles show maximised
	fullMaxed    bool
	fullMax      panel
	stashView    *stashView
	filesPreview *openFile
	focus        panel
}
```

Add field `ret *consoleReturn` to `consoleState` (comment: `// the screen to go back to (never nil on a shown console)`).

Add:

```go
// captureReturn records the screen a console is about to cover and parks
// the live layer stack (it would draw over the console and keep the keys).
// A stack topped by a full-screen view makes the return point full.
func (m Model) captureReturn() (Model, *consoleReturn) {
	r := &consoleReturn{
		fullMaxed: m.fullMaxed, fullMax: m.fullMax,
		stashView: m.stashView, filesPreview: m.filesPreview,
		focus: m.focus,
	}
	r.full = m.fullMaxActive()
	if m.layers != nil && len(m.layers.entries) > 0 {
		r.layers = m.layers.entries
		m.layers.entries = nil
		for _, l := range r.layers {
			if isFullScreenLayer(l) {
				r.full = true
			}
		}
	}
	return m, r
}

// consoleFull reports a console shown over a full-screen return point.
func (m Model) consoleFull() bool {
	return m.console != nil && m.console.ret != nil && m.console.ret.full
}

// restoreLayersBeneath puts a parked stack back UNDER whatever is live now
// (a popup opened over the console stays on top).
func (m Model) restoreLayersBeneath(parked []layer) Model {
	if len(parked) == 0 {
		return m
	}
	if m.layers == nil {
		m.layers = &layerStack{}
	}
	m.layers.entries = append(append([]layer{}, parked...), m.layers.entries...)
	return m
}
```

Rewrite `showConsole` body from `if m.focus != panelCommits {` to the end:

```go
	var ret *consoleReturn
	if m.console != nil {
		ret = m.console.ret // a console replacing a console keeps the way back
	}
	if ret == nil {
		m, ret = m.captureReturn()
	}
	if m.focus != panelCommits {
		m = m.rememberLeftFocus()
	}
	// What the console covers is in ret; cleared here so a closed console
	// never leaves two right-column owners or a pin over a hidden box.
	m.stashView = nil
	m.filesPreview = nil
	m.fullMaxed = false
	m = m.detachConsole()
	if focused {
		s.Touch()
	}
	screen, cancel := s.Subscribe()
	m.console = &consoleState{id: id, focused: focused, maximized: ret.full, gen: gen, screen: screen, cancel: cancel, ret: ret}
	m.focus = panelCommits
	m = m.syncConsoleSizeIfFocused()
	return m, waitSessionCmd(m.console, id, gen)
```

Update the doc comment: the pin, stash list and file preview are saved in
the return point, not dropped; a full return point shows it maximised.

Replace `dropConsole` with:

```go
// detachConsole clears the console and its screen subscription. Every place
// that sets m.console = nil goes through it, so a closed console never
// leaves a subscriber behind on its session.
func (m Model) detachConsole() Model {
	if m.console != nil && m.console.cancel != nil {
		m.console.cancel()
	}
	m.console = nil
	return m
}

// dropConsole is a console stepping aside for another right-column owner
// (stash list, file preview, a solo): the session keeps running and the
// return point is dropped — except a parked view, which is never lost.
func (m Model) dropConsole() Model {
	if m.console != nil && m.console.ret != nil {
		m = m.restoreLayersBeneath(m.console.ret.layers)
	}
	return m.detachConsole()
}
```

Replace `closeConsole`:

```go
// closeConsole hides the console (the session keeps running) and puts back
// the screen it was shown over. Focus returns only when it sat in the
// console's column: a click elsewhere already moved it.
func (m Model) closeConsole() Model {
	if m.console == nil {
		return m
	}
	r := m.console.ret
	m = m.detachConsole()
	if r == nil {
		m.focus = m.lastLeftPanel
		return m.reconcileFullscreenFocus()
	}
	m = m.restoreLayersBeneath(r.layers)
	m.fullMaxed, m.fullMax = r.fullMaxed, r.fullMax
	m.stashView, m.filesPreview = r.stashView, r.filesPreview
	if m.focus == panelCommits {
		m.focus = r.focus
	}
	return m.reconcileFullscreenFocus()
}
```

In `onSessionsChanged`, the removed-session arm becomes just
`m = m.closeConsole()` (both branches; the focus rule moved into
closeConsole). Keep the comment's first sentence.

Rewrite `cycleSessions` after the empty-list arm:

```go
	// The ring is the sessions then the return point: from the shown session
	// at i go to i+1, past the last one back to the screen the cycle came
	// from. Anything else (no console, the other kind, an exited one)
	// starts at the most recent.
	next := 0
	if m.console != nil {
		for i, info := range list {
			if info.ID == m.console.id {
				next = i + 1
				break
			}
		}
	}
	if next == len(list) {
		return m.closeConsole(), nil
	}
	info := list[next]
```

(keep the existing showConsole + statusMsg tail). Update its doc comment:
"…pressed again, the next one back in last-used order, then the screen the
cycle started from (the console's return point), then around again."

In `updateConsoleKey`'s focused branch, right after the `key == "x"` check:

```go
		// alt+a / alt+t are gg's even here: the cycle starts from this
		// agent and comes back to the screen it was shown over.
		if key == "alt+a" || key == "alt+t" {
			nm, cmd := m.cycleSessions(key == "alt+t")
			return nm, cmd, true
		}
```

- [ ] **Step 4: Run the console tests**

Run: `go test ./internal/tui -run 'Console|AltA|AltT|Session|Fullscreen' -count=1`
Expected: PASS. Fix any other test that asserted "openConsole clears the pin"
or "closeConsole focuses lastLeftPanel" by checking it against the spec, not
by bending the code.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/console.go internal/tui/alt_cycle_test.go internal/tui/last_session_test.go internal/tui/console_test.go
git commit -m "feat(tui): alt+a/alt+t cycle ends on the screen it started from"
```

---

### Task 2: A full-screen view as the return point

**Files:**
- Modify: `internal/tui/model.go:2218-2224` (the alt+a/alt+t gate)
- Modify: `internal/tui/console.go` (`cycleSessions`: a surface over a console)
- Modify: `internal/tui/console_scope.go` (`settleConsole`: repo switch)
- Modify: `internal/tui/steer.go` (`steerRefusal`: a parked stack's top)
- Test: `internal/tui/alt_cycle_test.go`

**Interfaces:**
- Consumes: Task 1's `captureReturn`, `restoreLayersBeneath`, `closeConsole`.
- Produces: `(Model) cycleReachable() bool`.

- [ ] **Step 1: Write the failing tests** (append to `alt_cycle_test.go`):

```go
// From a diff view: sessions show full-screen and unfocused, the diff is
// parked (the same window, cursor and all) and comes back on top.
func TestAltAFromDiffViewCyclesFullScreenAndReturns(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	a := startTestSession(t, m, `sleep 5`)
	b := startSecondSession(t, a, "b", false)
	time.Sleep(2 * time.Millisecond)
	b.Touch() // last used: b, a
	dv := &diffView{title: "a.go", rev: "abc123"}
	m = m.pushLayer(dv)
	m = pressAlt(t, m, 'a')
	if m.console == nil || m.console.id != b.Info().ID || m.console.focused || !m.console.maximized {
		t.Fatalf("first alt+a over a diff: console=%+v", m.console)
	}
	if m.topLayer() != nil {
		t.Fatal("the diff must be parked off the live stack")
	}
	m = pressAlt(t, m, 'a')
	if m.console == nil || m.console.id != a.Info().ID || !m.console.maximized {
		t.Fatalf("second alt+a: console=%+v", m.console)
	}
	m = pressAlt(t, m, 'a')
	if m.console != nil || m.topLayer() != layer(dv) {
		t.Fatalf("third alt+a must bring the same diff back: console=%+v top=%T", m.console, m.topLayer())
	}
}

// A removed session never strands the parked view.
func TestRemovedSessionOverParkedDiffBringsTheDiffBack(t *testing.T) {
	m := newTestModel(t)
	m.width, m.height = 120, 40
	s := startTestSession(t, m, "sleep 0.3")
	dv := &diffView{title: "a.go", rev: "abc123"}
	m = m.pushLayer(dv)
	m = pressAlt(t, m, 'a')
	m, _ = m.onSessionsChanged()
	<-s.Done()
	m, _ = m.onSessionsChanged()
	if err := domain.Sessions().Remove(s.Info().ID); err != nil {
		t.Fatal(err)
	}
	m, _ = m.onSessionsChanged()
	if m.console != nil || m.topLayer() != layer(dv) {
		t.Fatalf("console=%+v top=%T, want the diff back", m.console, m.topLayer())
	}
}

// A popup opened over the full-screen agent stays on top when the console
// returns beneath it.
func TestPopupOverFullScreenAgentStaysOnTopOfTheReturnedDiff(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	startTestSession(t, m, `sleep 5`)
	dv := &diffView{title: "a.go", rev: "abc123"}
	m = m.pushLayer(dv)
	m = pressAlt(t, m, 'a')
	pop := &contentPopup{}
	m = m.pushLayer(pop)
	m = m.closeConsole()
	if len(m.layers.entries) != 2 || m.layers.entries[0] != layer(dv) || m.layers.entries[1] != layer(pop) {
		t.Fatalf("stack = %T, want [diff popup]", m.layers.entries)
	}
}

// The cycle starts only where it can come back: base panels and the
// poppable views; not over a popup, the rebase editor, or a typing search.
func TestAltAGate(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	startTestSession(t, m, `sleep 5`)
	for name, push := range map[string]func(Model) Model{
		"popup":         func(m Model) Model { return m.pushLayer(&contentPopup{}) },
		"rebase editor": func(m Model) Model { return m.pushLayer(&irebaseEditor{}) },
		"typing search": func(m Model) Model {
			fv := &fileViewer{}
			fv.p.search.typing = true
			return m.pushLayer(fv)
		},
	} {
		mm := push(m)
		if mm.cycleReachable() {
			t.Errorf("%s: alt+a must not start a cycle", name)
		}
	}
	for name, push := range map[string]func(Model) Model{
		"panels":  func(m Model) Model { return m },
		"diff":    func(m Model) Model { return m.pushLayer(&diffView{title: "a.go"}) },
		"history": func(m Model) Model { return m.pushLayer(&historyView{}) },
		"blame":   func(m Model) Model { return m.pushLayer(&blameView{}) },
	} {
		if !push(m).cycleReachable() {
			t.Errorf("%s: alt+a must start a cycle", name)
		}
	}
}

// A repo switch closing a foreign console drops what it had parked: those
// views show the old repository.
func TestSettleConsoleDropsTheReturnPoint(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	startTestSession(t, m, `sleep 5`)
	m = m.pushLayer(&diffView{title: "a.go"})
	m = pressAlt(t, m, 'a')
	m.worktrees = nil // the new repository owns none of the session's dirs
	m.consoleSwitch.armed = true
	m, _ = m.settleConsole()
	if m.console != nil || m.topLayer() != nil {
		t.Fatalf("console=%+v top=%T, want both gone", m.console, m.topLayer())
	}
}
```

Before running, check the zero values compile and render-free:
`&fileViewer{}`'s search field path (`fv.p.search.typing`) matches
`steer.go:226`; `&irebaseEditor{}` / `&historyView{}` / `&blameView{}`
literals are used only for type checks here (no render). If `m.worktrees`
is not the field `inRepo` reads, use the one it does
(`grep -n 'func (m Model) inRepo' -A8 internal/tui/console_scope.go`).

- [ ] **Step 2: Run to see them fail**

Run: `go test ./internal/tui -run 'AltA|RemovedSessionOverParked|PopupOverFullScreen|SettleConsoleDrops' -count=1`
Expected: FAIL — `cycleReachable` undefined; alt+a ignored over a diff.

- [ ] **Step 3: Implement**

`internal/tui/model.go` — beside `paletteReachable`, add:

```go
// cycleReachable reports where alt+a / alt+t may start a cycle: a screen
// the cycle can come back to. The base panels, or a poppable full-screen
// view on top (diff, history, blame, file viewer — the views steerRefusal
// pops); never a popup, an editor holding an operation's input, or text
// being typed.
func (m Model) cycleReachable() bool {
	if m.filterTyping {
		return false
	}
	switch l := m.topLayer().(type) {
	case nil, *diffView, *historyView, *blameView:
		return true
	case *fileViewer:
		return !l.p.search.typing
	}
	return false
}
```

and change the gate at `model.go:2222` to
`if k := msg.String(); (k == "alt+a" || k == "alt+t") && m.cycleReachable() {`
with the comment: "…shown unfocused. From the base panels or a full-screen
view (parked while the sessions show maximised); a focused console handled
the key above."

`internal/tui/console.go` `cycleSessions`, before computing `next`:

```go
	// A view pushed over a shown console (the palette opened a diff from a
	// full-screen agent) is a new starting screen: the console goes back
	// first — its parked views slot in beneath — and the cycle starts over
	// with the whole stack as its return point.
	if m.console != nil && m.topLayer() != nil {
		m = m.closeConsole()
	}
```

`internal/tui/console_scope.go` `settleConsole`, right after
`m.consoleSwitch = consoleSwitch{}`:

```go
	// What the console was shown over belongs to the old checkout: a switch
	// pops such views anyway, and a stash list / preview there is stale.
	if m.console != nil {
		m.console.ret = nil
	}
```

`internal/tui/steer.go` `steerRefusal`: the final `switch l := m.topLayer().(type)`
should read the parked stack when the live one is empty, so a switch is
refused over a parked rebase editor exactly as over a live one:

```go
	top := m.topLayer()
	if top == nil && m.console != nil && m.console.ret != nil && len(m.console.ret.layers) > 0 {
		top = m.console.ret.layers[len(m.console.ret.layers)-1]
	}
	switch l := top.(type) {
```

- [ ] **Step 4: Run**

Run: `go test ./internal/tui -count=1`
Expected: PASS (whole package — the gate and park touch shared routing).

- [ ] **Step 5: Commit**

```bash
git add internal/tui/model.go internal/tui/console.go internal/tui/console_scope.go internal/tui/steer.go internal/tui/alt_cycle_test.go
git commit -m "feat(tui): alt+a over a diff/history/blame shows sessions full-screen and comes back"
```

---

### Task 3: The unfocused full-screen console

**Files:**
- Modify: `internal/tui/console.go` (`updateConsoleKey`, `onSessionsChanged` exit arm)
- Test: `internal/tui/alt_cycle_test.go`

**Interfaces:**
- Consumes: `consoleFull()` (Task 1).

- [ ] **Step 1: Write the failing tests** (append):

```go
func fullScreenAgent(t *testing.T) (Model, *domain.AgentSession, *diffView) {
	t.Helper()
	m := loadedModel(t)
	m.width, m.height = 120, 40
	s := startTestSession(t, m, `sleep 5`)
	dv := &diffView{title: "a.go", rev: "abc123"}
	m = m.pushLayer(dv)
	m = pressAlt(t, m, 'a')
	if !m.consoleFull() || m.console.focused {
		t.Fatalf("precondition: console=%+v", m.console)
	}
	return m, s, dv
}

func TestUnfocusedFullScreenConsoleKeys(t *testing.T) {
	m, _, dv := fullScreenAgent(t)
	for _, k := range []string{"tab", "shift+tab", "left", "h", "ctrl+left", "ctrl+right", "j"} {
		m = press(t, m, k)
		if m.console == nil || m.focus != panelCommits || !m.console.maximized {
			t.Fatalf("%s: console=%+v focus=%v — the hidden panels must not take keys", k, m.console, m.focus)
		}
	}
	m = press(t, m, "enter")
	if !m.console.focused || !m.console.maximized {
		t.Fatalf("enter: %+v, want focused full-screen", m.console)
	}
	mm, _ := m.Update(ctrlBracket())
	m = mm.(Model)
	if m.console == nil || m.console.focused || !m.console.maximized {
		t.Fatalf("step-out from a full return point stays full-screen: %+v", m.console)
	}
	m = press(t, m, "esc")
	if m.console != nil || m.topLayer() != layer(dv) {
		t.Fatalf("esc: console=%+v top=%T, want the diff", m.console, m.topLayer())
	}
}

// ctrl+t on a DOCKED console maximises it; stepping out docks it again.
func TestStepOutOfCtrlTMaximisedDockedConsoleDocks(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	s := startTestSession(t, m, `sleep 5`)
	m, _ = m.showConsole(s.Info().ID, false)
	m = press(t, m, "ctrl+t")
	if !m.console.maximized || !m.console.focused {
		t.Fatalf("ctrl+t: %+v", m.console)
	}
	mm, _ := m.Update(ctrlBracket())
	m = mm.(Model)
	if m.console.maximized || m.console.focused {
		t.Fatalf("step-out: %+v, want docked unfocused", m.console)
	}
}

// The agent exiting under a focused full-screen console steps out but stays
// full-screen until the user leaves.
func TestExitInFullScreenConsoleStaysFullScreen(t *testing.T) {
	m := newTestModel(t)
	m.width, m.height = 120, 40
	s := startTestSession(t, m, "sleep 0.3")
	m = m.pushLayer(&diffView{title: "a.go"})
	m = pressAlt(t, m, 'a')
	m = press(t, m, "enter")
	m, _ = m.onSessionsChanged()
	<-s.Done()
	m, _ = m.onSessionsChanged()
	if m.console == nil || m.console.focused || !m.console.maximized {
		t.Fatalf("console=%+v", m.console)
	}
}

// A click (focus moved by the mouse) never leaves a full-screen console's
// keys to the hidden panels.
func TestFullScreenConsoleSnapsFocusBack(t *testing.T) {
	m, _, _ := fullScreenAgent(t)
	m = press(t, m, "enter")
	m.focus = panelBranches // as a mouse click would
	m = press(t, m, "x")
	if m.console == nil || !m.console.focused || !m.console.maximized || m.focus != panelCommits {
		t.Fatalf("console=%+v focus=%v", m.console, m.focus)
	}
}
```

- [ ] **Step 2: Run to see them fail**

Run: `go test ./internal/tui -run 'FullScreen|StepOutOfCtrlT|ExitInFullScreen' -count=1`
Expected: FAIL (tab moves focus; step-out un-maximises; exit un-maximises;
the focus guard un-maximises).

- [ ] **Step 3: Implement in `internal/tui/console.go`**

Add beside `consolePassthrough`:

```go
// consoleFocusMoves are the passthrough keys that move panel focus: a
// full-screen console hides every panel, so they are swallowed there.
var consoleFocusMoves = map[string]bool{
	"tab": true, "shift+tab": true, "left": true, "h": true, "ctrl+left": true, "ctrl+right": true,
}
```

In `updateConsoleKey`, replace the "Focus left the console's column" block:

```go
	// Focus left the console's column (a mouse click on another panel): the
	// keyboard is gg's again. A full-screen console hides every panel, so
	// the click cannot have meant one: focus snaps back.
	if m.focus != panelCommits && m.consoleFull() {
		m.focus = panelCommits
	}
	if m.console.focused && m.focus != panelCommits {
		m.console.focused = false
		if m.console.maximized {
			m.console.maximized = false
			m = m.syncConsoleSize()
		}
	}
```

In the focused branch, the step-out arm:

```go
		if key == m.stepOutKey() {
			m.console.focused = false
			// Over a full-screen return point the console stays full-screen
			// (a second press or esc goes back); a ctrl+t-maximised docked
			// one docks again.
			if m.console.maximized && !m.consoleFull() {
				m.console.maximized = false
				m = m.syncConsoleSize()
			}
			return m, nil, true
		}
```

In the unfocused tail, just before `if consolePassthrough[key] {`:

```go
	if m.console.maximized && consoleFocusMoves[key] {
		return m, nil, true
	}
```

In `onSessionsChanged`'s exit arm, change
`if m.console.maximized {` to `if m.console.maximized && !m.consoleFull() {`.

- [ ] **Step 4: Run**

Run: `go test ./internal/tui -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/console.go internal/tui/alt_cycle_test.go
git commit -m "feat(tui): unfocused full-screen console — enter focuses, esc returns, panel keys swallowed"
```

---

### Task 4: Help text, translations, docs, gates

**Files:**
- Modify: `internal/tui/help.go:91-92`
- Modify: `internal/i18n/lang/{ja,ko,zh,ru}.toml` (the two help keys)
- Modify: `CHANGELOG.md`, `docs/CLAUDE-details.md` (the alt+a paragraph
  ~line 4048), `README.md` only if `grep -n 'alt+a' README.md` finds a row.

- [ ] **Step 1: Help strings** — `help.go:91` new English key:

```
show this repository's running agent sessions one by one, unfocused, most recently used first, then the screen the cycle started from, and around again (exited ones are skipped); enter focuses the shown one, which makes it the most recent. Works inside a focused console too (it starts from that agent and comes back to the screen it was opened over) and over a diff, history, blame or file view, where the sessions show full-screen and the view comes back last. While a console is unfocused, or works in another worktree than this gg, the status line ends with its worktree path
```

`help.go:92`:

```
the same for this repository's running terminals (Open terminal): cycle them by last use, then back to the starting screen; enter focuses one
```

Replace the old keys in all four bundles (old alt+t key: find with
`grep -n 'the same for this repository' internal/i18n/lang/ja.toml`) with
the new keys and these translations:

ja (alt+a): `このリポジトリの実行中のエージェントセッションを、フォーカスせずに最近使った順に1つずつ表示し、最後にサイクルを始めた画面に戻って、また繰り返します(終了したものは飛ばします)。enter で表示中のものにフォーカスし、それが最新になります。フォーカス中のコンソール内でも動作し(そのエージェントから始まり、それを開いた画面に戻ります)、diff・履歴・blame・ファイル表示の上でも動作します。その場合セッションは全画面で表示され、最後にその表示に戻ります。コンソールがフォーカスされていない間、またはこの gg とは別のワークツリーで動いている間は、ステータス行の末尾にそのワークツリーのパスが表示されます`
ja (alt+t): `このリポジトリの実行中のターミナル(Open terminal)も同様に、最近使った順に切り替え、最後に開始画面に戻ります。enter でフォーカスします`

ko (alt+a): `이 저장소의 실행 중인 에이전트 세션을 포커스 없이 최근 사용 순으로 하나씩 표시하고, 마지막에 순환을 시작한 화면으로 돌아온 뒤 다시 반복합니다(종료된 세션은 건너뜁니다). enter는 표시된 세션에 포커스를 주고 가장 최근으로 만듭니다. 포커스된 콘솔 안에서도 동작하며(그 에이전트에서 시작해 그것을 연 화면으로 돌아옵니다), diff·히스토리·blame·파일 보기 위에서도 동작합니다. 이때 세션은 전체 화면으로 표시되고 마지막에 그 보기로 돌아옵니다. 콘솔에 포커스가 없거나 이 gg와 다른 워크트리에서 실행 중이면 상태 줄 끝에 해당 워크트리 경로가 표시됩니다`
ko (alt+t): `이 저장소의 실행 중인 터미널(Open terminal)도 같은 방식으로 최근 사용 순으로 순환하고 마지막에 시작 화면으로 돌아옵니다. enter로 포커스합니다`

zh (alt+a): `以非焦点方式按最近使用顺序逐个显示此仓库运行中的代理会话,最后回到开始循环时的画面,然后再次循环(已退出的会跳过);enter 聚焦当前显示的会话,并使其成为最近使用的。在获得焦点的控制台内同样有效(从该代理开始,最后回到打开它时的画面),在 diff、历史、blame 或文件视图上也有效:此时会话全屏显示,最后回到该视图。当控制台未获得焦点,或运行在与此 gg 不同的工作树中时,状态行末尾会显示其工作树路径`
zh (alt+t): `对此仓库运行中的终端(Open terminal)同样按最近使用顺序循环,最后回到开始画面;enter 聚焦`

ru (alt+a): `показывать работающие сеансы агентов этого репозитория по одному, без фокуса, начиная с последнего использованного, затем экран, с которого начался обход, и снова по кругу (завершённые пропускаются); enter даёт фокус показанному, и он становится последним использованным. Работает и в консоли с фокусом (обход начинается с этого агента и возвращается к экрану, над которым он был открыт), и над просмотром diff, истории, blame или файла — тогда сеансы показываются на весь экран, а просмотр возвращается последним. Пока консоль не в фокусе или работает в другом рабочем дереве, чем этот gg, строка состояния заканчивается путём её рабочего дерева`
ru (alt+t): `то же для работающих терминалов этого репозитория (Open terminal): обход по последнему использованию, затем возврат к начальному экрану; enter даёт фокус`

- [ ] **Step 2: i18n gates**

Run: `go test ./internal/tui -run 'I18n|Vocab|MenuLabels|EngineProse' -count=1 && go test ./internal/i18n -count=1`
Expected: PASS.

- [ ] **Step 3: Docs**

`docs/CLAUDE-details.md`, in the "alt+a / alt+t = cycle by last use"
bullet, replace from "`cycleSessions` shows the head UNFOCUSED" through
"Not configurable; not in the web." with:

```
`cycleSessions` walks a RING: the sessions, then the console's return point
(2026-10-06). `consoleState.ret` (`consoleReturn`) is captured by the first
console shown over a non-console screen (`captureReturn`: the live layer
stack is PARKED, the ctrl+t pin / stash list / file preview / focus saved)
and carried over when a console replaces a console, so "the screen before
the agent" survives a cycle; `closeConsole` restores it (parked layers go
BENEATH whatever is live — a popup opened over the agent stays on top;
focus only when it sat in the console's column). `ret.full` (a full-screen
layer parked or an active pin) shows every console of the cycle maximised;
`maximized && !focused` exists only there: enter/ctrl+t focus, esc /
step-out return, `consoleFocusMoves` swallowed, focus snaps back to the
column; step-out from a focused full console stays full (`consoleFull`),
from a ctrl+t-maximised docked one docks. `dropConsole` (another
right-column owner) restores parked layers too — a parked view is never
lost; `detachConsole` is the bare clear. A repo switch (`settleConsole`)
drops `ret`; `steerRefusal` reads a parked stack's top when the live stack
is empty. Only a FOCUSED show is a use: `openConsole` and the unfocused
console's `enter`/`ctrl+t` (`touchConsole`) Touch; the unfocused show must
not, or the walk would reorder itself. The web counts through typing only.
The gate is `cycleReachable` (base panels or a diff/history/blame/file
viewer on top — steerRefusal's poppable views — not typing); a focused
console intercepts alt+a/alt+t before its program (`updateConsoleKey`).
Not ctrl+a (a common tmux prefix — gg never sees it) nor ctrl+l (Commits'
load-more). Not configurable; not in the web.
```

`CHANGELOG.md` — new entry at the top of the unreleased section:

```
- **alt+a / alt+t come back where they started.** The cycle walks the
  repository's agents (terminals) by last use and then returns to the
  screen it started from. It works inside a focused agent (the agent no
  longer receives alt+a / alt+t; the way back is the screen the agent was
  opened over) and over a diff, history, blame or file view — the sessions
  show full-screen, unfocused, and the view comes back untouched. In an
  unfocused full-screen agent enter focuses, esc returns.
```

- [ ] **Step 4: Full gates**

Run: `./test.sh` then `./test.sh race 2>&1 | tee /tmp/claude-1000/-work-gigagit/80902b0d-e595-4504-900a-0f3b8f4c7156/scratchpad/race.log; grep -c 'all green' /tmp/claude-1000/-work-gigagit/80902b0d-e595-4504-900a-0f3b8f4c7156/scratchpad/race.log`
Expected: both green; the race log says "all green".

- [ ] **Step 5: Build the verify binary and commit**

```bash
go build -o bin/gg ./cmd/gg
git add internal/tui/help.go internal/i18n/lang/*.toml CHANGELOG.md docs/CLAUDE-details.md
git commit -m "docs: alt+a/alt+t cycle return point — help, translations, changelog, details"
```

(`bin/` is gitignored — never `git add -A`.)

- [ ] **Step 6: Manual check under tmux** (`driving-tui-headless` skill):
open a commit's file diff, alt+a twice with two agents running, confirm the
frames show agent full-screen → second agent → the same diff; from the
Commits column confirm docked → docked → Commits.

- [ ] **Step 7: Final review** — one read-only review subagent over
`git diff main...feat/alt-a-cycle` against the spec.
