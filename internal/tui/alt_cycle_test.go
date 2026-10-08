package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/homeend/gigagit/internal/domain"
)

func pressAlt(t *testing.T, m Model, r rune) Model {
	t.Helper()
	mm, _ := m.Update(altKey(r))
	return mm.(Model)
}

// alt+a over the stash list binds the agent at once; leaving the console
// (step out, then esc) brings the stash list back, focus with it. The ring
// itself never returns there.
func TestAltAOverTheStashListBindsAndEscBringsItBack(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	s := startTestSession(t, m, `sleep 5`)
	sv := &stashView{tag: "stash"}
	m.stashView, m.focus = sv, panelCommits
	m = pressAlt(t, m, 'a')
	if m.console == nil || m.console.id != s.Info().ID || !m.console.focused || m.stashView != nil {
		t.Fatalf("alt+a: console=%+v stash=%v, want the agent bound", m.console, m.stashView)
	}
	m = pressAlt(t, m, 'a')
	if m.console == nil || !m.console.focused {
		t.Fatalf("second alt+a: console=%+v, want the only agent kept", m.console)
	}
	mm, _ := m.Update(ctrlBracket())
	m = press(t, mm.(Model), "esc")
	if m.console != nil || m.stashView != sv || m.focus != panelCommits {
		t.Fatalf("esc must return: console=%+v stash=%v focus=%v", m.console, m.stashView, m.focus)
	}
}

// From a bound agent alt+a goes to the next one in order and binds it,
// then round again: a → b → a, never the screen before the agent.
func TestAltAFromABoundAgentGoesRoundTheAgents(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	a := startTestSession(t, m, `sleep 5`)
	b := startSecondSession(t, a, "b", false)
	m.focus = panelWorktrees
	m, _ = m.openConsole(a.Info().ID)
	m = pressAlt(t, m, 'a')
	if m.console == nil || m.console.id != b.Info().ID || !m.console.focused {
		t.Fatalf("alt+a in bound a: console=%+v, want b bound", m.console)
	}
	m = pressAlt(t, m, 'a')
	if m.console == nil || m.console.id != a.Info().ID || !m.console.focused {
		t.Fatalf("second alt+a: console=%+v, want a again", m.console)
	}
}

// One agent, bound: alt+a stays put.
func TestAltAWithTheOnlyAgentBoundStaysPut(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	a := startTestSession(t, m, `sleep 5`)
	m.focus = panelBranches
	m, _ = m.openConsole(a.Info().ID)
	m = pressAlt(t, m, 'a')
	if m.console == nil || m.console.id != a.Info().ID || !m.console.focused || m.focus != panelCommits {
		t.Fatalf("console=%+v focus=%v", m.console, m.focus)
	}
}

// alt+t after alt+a walks terminals; the return point (where esc goes)
// survives the switch of kind.
func TestAltTAfterAltAKeepsTheReturnPoint(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	a := startTestSession(t, m, `sleep 5`)
	term := startSecondSession(t, a, "Terminal", true)
	sv := &stashView{tag: "stash"}
	m.stashView, m.focus = sv, panelCommits
	m = pressAlt(t, m, 'a')
	m = pressAlt(t, m, 't')
	if m.console == nil || m.console.id != term.Info().ID || !m.console.focused {
		t.Fatalf("alt+t: console=%+v, want the terminal bound", m.console)
	}
	m = pressAlt(t, m, 't')
	if m.console == nil || m.console.id != term.Info().ID {
		t.Fatalf("second alt+t: console=%+v, want the only terminal kept", m.console)
	}
	mm, _ := m.Update(ctrlBracket())
	m = press(t, mm.(Model), "esc")
	if m.console != nil || m.stashView != sv {
		t.Fatalf("esc: console=%+v stash=%v", m.console, m.stashView)
	}
}

// A ctrl+t-pinned panel is a full-screen return point: the console shows
// maximised and bound; stepping out keeps it full, esc brings the pin back.
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
	if m.console == nil || !m.console.maximized || !m.console.focused || m.fullMaxed {
		t.Fatalf("console=%+v fullMaxed=%v", m.console, m.fullMaxed)
	}
	mm, _ := m.Update(ctrlBracket())
	m = mm.(Model)
	if m.console == nil || !m.console.maximized || m.console.focused {
		t.Fatalf("step-out over a pin stays full-screen: %+v", m.console)
	}
	m = press(t, m, "esc")
	if m.console != nil || !m.fullMaxActive() || m.fullMax != panelBranches || m.focus != panelBranches {
		t.Fatalf("return: console=%+v pin=%v/%v focus=%v", m.console, m.fullMaxed, m.fullMax, m.focus)
	}
}

// gg takes alt+a even inside a bound console: the next agent in order.
func TestFocusedConsoleGivesAltAToTheCycle(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	s := startTestSession(t, m, `sleep 5`)
	other := startSecondSession(t, s, "other", false)
	time.Sleep(2 * time.Millisecond)
	other.Touch()
	m, _ = m.openConsole(s.Info().ID)
	m = pressAlt(t, m, 'a')
	if m.console == nil || m.console.id != other.Info().ID || !m.console.focused {
		t.Fatalf("console = %+v: alt+a in a bound console goes on, bound", m.console)
	}
}

// From a diff view: sessions show full-screen and bound, the diff is
// parked (the same window, cursor and all); the walk goes round, and esc
// from an unbound console brings the diff back on top.
func TestAltAFromDiffViewCyclesFullScreenAndReturns(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	a := startTestSession(t, m, `sleep 5`)
	b := startSecondSession(t, a, "b", false)
	time.Sleep(2 * time.Millisecond)
	b.Touch() // last use must not matter: a started first
	dv := &diffView{title: "a.go", rev: "abc123"}
	m = m.pushLayer(dv)
	m = pressAlt(t, m, 'a')
	if m.console == nil || m.console.id != a.Info().ID || !m.console.focused || !m.console.maximized {
		t.Fatalf("first alt+a over a diff: console=%+v", m.console)
	}
	if m.topLayer() != nil {
		t.Fatal("the diff must be parked off the live stack")
	}
	m = pressAlt(t, m, 'a')
	if m.console == nil || m.console.id != b.Info().ID || !m.console.maximized {
		t.Fatalf("second alt+a: console=%+v", m.console)
	}
	m = pressAlt(t, m, 'a')
	if m.console == nil || m.console.id != a.Info().ID {
		t.Fatalf("third alt+a goes round: console=%+v", m.console)
	}
	mm, _ := m.Update(ctrlBracket())
	m = press(t, mm.(Model), "esc")
	if m.console != nil || m.topLayer() != layer(dv) {
		t.Fatalf("esc must bring the same diff back: console=%+v top=%T", m.console, m.topLayer())
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
// brings the console's parked views back beneath it and parks the lot;
// esc later restores the whole stack.
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
	mm, _ := m.Update(ctrlBracket())
	m = press(t, mm.(Model), "esc")
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

// A switch (steer, hosted web) is refused over a parked view exactly as over
// a live one: a file viewer whose search is being typed would lose it.
func TestSteerRefusalReadsTheParkedStack(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	s := startTestSession(t, m, `sleep 5`)
	fv := &fileViewer{openFile: &openFile{p: &contentPopup{}}}
	fv.p.search.typing = true
	m = m.pushLayer(fv)
	m, _ = m.openConsole(s.Info().ID)
	if m.topLayer() != nil {
		t.Fatal("precondition: the viewer is parked")
	}
	if m.steerRefusal() == "" {
		t.Fatal("a parked viewer with a typed search must refuse a switch")
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
	mm, _ := m.Update(ctrlBracket()) // alt+a binds; the tests want it unbound and full
	m = mm.(Model)
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

// An unfocused full-screen console advertises what its keys do there: esc
// goes back, alt+a/alt+t go on; no maximise (it is) and no panels (hidden).
func TestUnfocusedFullScreenConsoleHints(t *testing.T) {
	m, _ := fullScreenAgent(t)
	frame := ansi.Strip(m.View())
	for _, want := range []string{"[esc] back", "[esc/ctrl+]] back", "[alt+a/alt+t] next"} {
		if !strings.Contains(frame, want) {
			t.Errorf("frame lacks %q", want)
		}
	}
	for _, bad := range []string{"[ctrl+t] maximise", "[tab] panels"} {
		if strings.Contains(frame, bad) {
			t.Errorf("frame advertises %q", bad)
		}
	}
}

// A view parked under a console still receives its async results: a history
// still loading when the cycle parked it is filled when its walk lands.
func TestParkedViewReceivesItsAsyncResults(t *testing.T) {
	m, _ := fullScreenAgent(t)
	h := &historyView{loading: true, gen: 1}
	m.console.ret.layers = append(m.console.ret.layers, h)
	mm, _ := m.Update(historyChunkMsg{view: h, gen: 1, done: true})
	m = mm.(Model)
	if h.loading {
		t.Fatal("the parked history never got its list")
	}
	if m.console == nil || m.topLayer() != nil || len(m.console.ret.layers) != 2 {
		t.Fatalf("the stack must stay parked: console=%+v top=%T", m.console, m.topLayer())
	}
}

// A diff opened from a commit's file list leaves the files tree focused; the
// full-screen console covering both still owns its keys.
func TestFullScreenConsoleOwnsKeysOverAFocusedFilesTree(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	startTestSession(t, m, `sleep 5`)
	m.filesView, m.filesTreeFocused = &contentPopup{}, true
	m = m.pushLayer(&diffView{title: "a.go"})
	m = pressAlt(t, m, 'a')
	m = press(t, m, "enter")
	if m.console == nil || !m.console.focused {
		t.Fatalf("enter must focus the full-screen console, console=%+v", m.console)
	}
	if !strings.Contains(m.footerLine(), "every key goes to the agent") {
		t.Fatalf("footer = %q", m.footerLine())
	}
}

// Keys that would open something hidden behind an unfocused full-screen
// console (the files finder, the stash list, a pull) are swallowed.
func TestUnfocusedFullScreenConsoleSwallowsOpeners(t *testing.T) {
	m, _ := fullScreenAgent(t)
	for _, k := range []string{"F", "S", "c", "g"} {
		m = press(t, m, k)
		if m.console == nil || !m.consoleFull() || m.stashView != nil || m.filesView != nil || m.topLayer() != nil {
			t.Fatalf("%s: console=%+v stash=%v files=%v top=%T", k, m.console, m.stashView, m.filesView, m.topLayer())
		}
	}
}

// A console that opens on its own (an agent started from a popup flow, an
// AI task) never takes a live popup's keyboard: only a full-screen view is
// parked.
func TestConsoleOpenedUnderAPopupLeavesItLive(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	s := startTestSession(t, m, `sleep 5`)
	pop := &contentPopup{}
	m = m.pushLayer(pop)
	m, _ = m.openConsole(s.Info().ID)
	if m.topLayer() != layer(pop) {
		t.Fatalf("top = %T, want the popup still live", m.topLayer())
	}
	if m.console.maximized {
		t.Fatal("a popup is not a full-screen return point")
	}
}

// A repo switch drops what the console parked at once, not only once the new
// repository has loaded: an esc in between must not bring the old diff back.
func TestReRootDropsParkedViews(t *testing.T) {
	m, _ := fullScreenAgent(t)
	sv := &stashView{tag: "stash"}
	m.console.ret.stashView = sv
	mm, _ := m.reRoot(m.currentWorktree)
	m = mm.(Model)
	if m.console == nil {
		t.Fatal("precondition: the console survives reRoot")
	}
	m = m.closeConsole()
	if m.topLayer() != nil || m.stashView != nil {
		t.Fatalf("after the switch: top=%T stash=%v, want the old repo's views gone", m.topLayer(), m.stashView)
	}
}

// A file preview comes back only with the files view it lived in.
func TestOrphanPreviewIsNotRestored(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	startTestSession(t, m, `sleep 5`)
	m.filesView = &contentPopup{}
	m.filesPreview = &openFile{}
	m.focus = panelCommits
	m = pressAlt(t, m, 'a')
	m.filesView = nil // an agent's navigate closed the files view meanwhile
	m = m.closeConsole()
	if m.filesPreview != nil {
		t.Fatal("a preview without its files view came back")
	}
}

// A console alt+a shows takes its box's size: what is shown is what the
// program lays out for (a PTY left at another size cuts its lines or leaves
// the box half empty).
func TestAltAShownConsoleTakesItsBoxSize(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	s := startTestSession(t, m, `sleep 5`)
	m = pressAlt(t, m, 'a')
	if m.console == nil || !m.console.focused {
		t.Fatalf("console = %+v", m.console)
	}
	w, h := m.consoleBox()
	cols, rows := consoleInner(w, h)
	if in := s.Input(); in.Cols != max(cols, 20) || in.Rows != max(rows, 5) {
		t.Fatalf("PTY %dx%d, box content %dx%d", in.Cols, in.Rows, cols, rows)
	}
}
