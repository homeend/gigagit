package tui

import (
	"testing"
	"time"

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
}

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
	m := loadedModel(t)
	m.width, m.height = 120, 40
	s := startTestSession(t, m, "sleep 0.3")
	dv := &diffView{title: "a.go", rev: "abc123"}
	m = m.pushLayer(dv)
	m = pressAlt(t, m, 'a')
	if !m.consoleFull() || m.topLayer() != nil {
		t.Fatalf("precondition: a full-screen agent over the parked diff, console=%+v", m.console)
	}
	m, _ = m.onSessionsChanged()
	select {
	case <-s.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("session did not exit")
	}
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

// A view pushed over a shown console is a new starting screen: alt+a there
// brings the console's parked views back beneath it and parks the lot.
func TestAltAOverAViewOpenedFromTheAgentParksTheWholeStack(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	startTestSession(t, m, `sleep 5`)
	dv := &diffView{title: "a.go", rev: "abc123"}
	m = m.pushLayer(dv)
	m = pressAlt(t, m, 'a')
	hv := &historyView{}
	m = m.pushLayer(hv)
	m = pressAlt(t, m, 'a')
	if m.console == nil || !m.console.maximized || m.topLayer() != nil {
		t.Fatalf("console=%+v top=%T", m.console, m.topLayer())
	}
	m = pressAlt(t, m, 'a')
	if m.console != nil || len(m.layers.entries) != 2 || m.layers.entries[0] != layer(dv) || m.layers.entries[1] != layer(hv) {
		t.Fatalf("console=%+v stack=%T, want [diff history]", m.console, m.layers.entries)
	}
}

// The cycle starts only where it can come back: base panels and the
// poppable views; not over a popup, the rebase editor, or a typed search.
func TestAltAGate(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	typingViewer := &fileViewer{openFile: &openFile{p: &contentPopup{}}}
	typingViewer.p.search.typing = true
	typingDiff := &diffView{title: "a.go"}
	typingDiff.search.typing = true
	typingBlame := &blameView{}
	typingBlame.search.typing = true
	for name, l := range map[string]layer{
		"popup":         &contentPopup{},
		"rebase editor": &irebaseEditor{},
		"viewer search": typingViewer,
		"diff search":   typingDiff,
		"blame search":  typingBlame,
	} {
		if m.pushLayer(l).cycleReachable() {
			t.Errorf("%s: alt+a must not start a cycle", name)
		}
		m = m.clearLayers()
	}
	for name, l := range map[string]layer{
		"diff":    &diffView{title: "a.go"},
		"history": &historyView{},
		"blame":   &blameView{},
		"viewer":  &fileViewer{openFile: &openFile{p: &contentPopup{}}},
	} {
		if !m.pushLayer(l).cycleReachable() {
			t.Errorf("%s: alt+a must start a cycle", name)
		}
		m = m.clearLayers()
	}
	if !m.cycleReachable() {
		t.Error("panels: alt+a must start a cycle")
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

// A switch (steer, hosted web) is refused over a parked editor exactly as
// over a live one: it would throw the operation's pending input away.
func TestSteerRefusalReadsTheParkedStack(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	s := startTestSession(t, m, `sleep 5`)
	m = m.pushLayer(&irebaseEditor{})
	m, _ = m.openConsole(s.Info().ID)
	if m.topLayer() != nil {
		t.Fatal("precondition: the editor is parked")
	}
	if m.steerRefusal() == "" {
		t.Fatal("a parked rebase editor must refuse a switch")
	}
}

func fullScreenAgent(t *testing.T) (Model, *diffView) {
	t.Helper()
	m := loadedModel(t)
	m.width, m.height = 120, 40
	startTestSession(t, m, `sleep 5`)
	dv := &diffView{title: "a.go", rev: "abc123"}
	m = m.pushLayer(dv)
	m = pressAlt(t, m, 'a')
	if !m.consoleFull() || m.console.focused {
		t.Fatalf("precondition: console=%+v", m.console)
	}
	return m, dv
}

func TestUnfocusedFullScreenConsoleKeys(t *testing.T) {
	m, dv := fullScreenAgent(t)
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
	m := loadedModel(t)
	m.width, m.height = 120, 40
	s := startTestSession(t, m, "sleep 0.3")
	m = m.pushLayer(&diffView{title: "a.go"})
	m = pressAlt(t, m, 'a')
	m = press(t, m, "enter")
	m, _ = m.onSessionsChanged()
	select {
	case <-s.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("session did not exit")
	}
	m, _ = m.onSessionsChanged()
	if m.console == nil || m.console.focused || !m.console.maximized {
		t.Fatalf("console=%+v", m.console)
	}
}

// A click (focus moved by the mouse) never leaves a full-screen console's
// keys to the hidden panels.
func TestFullScreenConsoleSnapsFocusBack(t *testing.T) {
	m, _ := fullScreenAgent(t)
	m = press(t, m, "enter")
	m.focus = panelBranches // as a mouse click would
	m = press(t, m, "x")
	if m.console == nil || !m.console.focused || !m.console.maximized || m.focus != panelCommits {
		t.Fatalf("console=%+v focus=%v", m.console, m.focus)
	}
}
