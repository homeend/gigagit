package tui

import (
	"reflect"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

// Second post-merge hunt of the fast worktree switch (2026-10-10): the
// operation boundary, the keys as a state machine, the replay queue and
// the shared state.

// An async step between an op key and its startOp (the pre-push remote-tag
// check) is addressed to the worktree it was asked from: landing after a
// swap it must not push the worktree now on screen.
func TestPushTagCheckAfterASwapIsDropped(t *testing.T) {
	t.Parallel()
	m := loadedModel(t)
	svcA := m.svc
	m.pushCheckGen++
	gen := m.pushCheckGen
	m, _ = viewedOther(t, m)
	mm, cmd := m.Update(pushTagCheckMsg{gen: gen, svc: svcA, remoteSet: map[string]bool{}})
	m = mm.(Model)
	if m.running || cmd != nil || m.modal != nil {
		t.Fatalf("a push check from the worktree that left the screen must not start a push here (running=%v cmd=%v modal=%v)", m.running, cmd, m.modal)
	}
	if m.statusMsg == "" {
		t.Fatal("the dropped push must be said")
	}
}

// Likewise the cherry-pick probe of the bookmark switcher (a parkable
// popup): its confirm must not open over, nor pick into, another worktree.
func TestPickProbeAfterASwapIsDropped(t *testing.T) {
	t.Parallel()
	m := loadedModel(t)
	svcA := m.svc
	m.pickGen++
	gen := m.pickGen
	m, _ = viewedOther(t, m)
	mm, _ := m.Update(pickProbeMsg{gen: gen, svc: svcA, target: pickTarget{sha: "abc"},
		line: model.LogLine{Hash: "abc1234", Subject: "s"}, found: true})
	m = mm.(Model)
	if m.modal != nil {
		t.Fatal("a probe from the worktree that left the screen must not open its confirm here")
	}
}

// Every message whose handler reaches startOp (or a confirm that starts
// one) after an async step names the worktree it was asked from — the
// Service it was read through, or a slot stamp — and its handler drops a
// mismatch: between the key and the op the panels may have swapped, and
// the op would run in the worktree on screen THEN. The range loads had
// this from the first; the push check and the pick probe did not.
func TestAsyncOpStartersNameTheirWorktree(t *testing.T) {
	t.Parallel()
	for _, msg := range []any{
		pushTagCheckMsg{}, pickProbeMsg{},
		rebaseRangeLoadedMsg{}, squashRangeLoadedMsg{}, dropRangeLoadedMsg{}, irebaseLoadedMsg{},
		amendPrefillMsg{}, conflictFileLoadedMsg{}, stageHunksLoadedMsg{}, unstageHunksLoadedMsg{}, reviewTargetReadyMsg{},
	} {
		if _, ok := msg.(slotMsg); ok {
			continue
		}
		if f, ok := reflect.TypeOf(msg).FieldByName("svc"); !ok || f.Type != reflect.TypeOf((*domain.Service)(nil)) {
			t.Errorf("%T carries neither a slot stamp nor the *domain.Service it was asked through", msg)
		}
	}
}

// The editor's exit re-reads the status with no op of its own; its result
// must not clear the busy flag of an op started meanwhile (a pull pressed
// before the slow read landed): with it cleared the swap refusal lifted
// mid-op, and a second op key overwrote the first op's channel.
func TestEditorStatusReadKeepsARunningOpBusy(t *testing.T) {
	t.Parallel()
	m := loadedModel(t)
	m.running, m.opName = true, "pull"
	mm, _ := m.Update(m.reloadStatusCmd("")())
	m = mm.(Model)
	if !m.running || m.opName != "pull" {
		t.Fatalf("the editor's status read cleared the running op (running=%v opName=%q)", m.running, m.opName)
	}
	if m.switchRefusalBy(true) == "" {
		t.Fatal("alt+w must stay refused while the op runs")
	}
}

// A staging round's own result does clear it: that round set it.
func TestStagingResultClearsBusy(t *testing.T) {
	t.Parallel()
	m := loadedModel(t)
	m.running = true
	mm, _ := m.Update(statusRefreshedMsg{svc: m.svc, staging: true, status: m.status})
	if mm.(Model).running {
		t.Fatal("a staging round's result must clear the busy flag it set")
	}
}

// A parkable popup that arrived UNDER a shown console (B's half-typed
// commit box, displaced when alt+a showed B's agent) is still parkable: the
// user's own swap must judge the same top steerRefusal does — the parked
// one — not the empty live pile, which made alt+w say "cannot switch while
// a window is open" with nothing but the console on screen.
func TestAltWUnderAConsoleOverAParkedPopupHidesIt(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	m, other := addWorktree(t, m, "wt2")
	installSessionManager(t)
	id := startSessionIn(t, m, other, "Agent")
	home := m.home
	m, ok := m.switchView(other)
	if !ok {
		t.Fatalf("switchView refused: %s", m.statusMsg)
	}
	box := &commitPopup{title: newTextField("half a message")}
	m = m.pushLayer(box)
	m = pressAlt(t, m, 'w') // the box parks with wt2; home shows
	if m.viewed != home || m.topLayer() != nil {
		t.Fatalf("precondition: viewed=%q top=%T", m.viewed, m.topLayer())
	}
	m, _ = m.showConsoleBy(id, true, true) // alt+a onto wt2's agent: wt2 arrives under the console
	if m.viewed != model.KeyOf(other) || m.topLayer() != nil || m.consoleParked == nil || len(m.consoleParked.layers) != 1 {
		t.Fatalf("precondition: viewed=%q top=%T parked=%+v", m.viewed, m.topLayer(), m.consoleParked)
	}
	m, _ = m.cycleWorktrees()
	if m.console != nil {
		t.Fatalf("alt+w must hide the console first (status %q)", m.statusMsg)
	}
	if m.topLayer() != box {
		t.Fatalf("the parked box must be live again, got %T", m.topLayer())
	}
}

// The walk (alt+a) shows the Branches tab for its session row; closing the
// console must put the keyboard on the tab that is SHOWN, not on the tab
// the console was opened from (Worktrees), which is not on screen: no lit
// border, no cursor, a footer for the wrong panel, and enter switching to a
// worktree row the user cannot see.
func TestClosingAConsoleOpenedFromAnotherTabFocusesTheShownTab(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	installSessionManager(t)
	startSessionIn(t, m, m.currentWorktree, "A1")
	m.activeLeftTab, m.focus, m.lastLeftPanel = panelWorktrees, panelWorktrees, panelWorktrees
	m = pressAlt(t, m, 'a')
	if m.console == nil || m.activeLeftTab != panelBranches {
		t.Fatalf("precondition: console=%v tab=%v", m.console != nil, m.activeLeftTab)
	}
	mm, _ := m.Update(ctrlBracket()) // unbind
	m = mm.(Model)
	mm, _ = m.Update(ctrlBracket()) // close
	m = mm.(Model)
	if m.console != nil {
		t.Fatal("precondition: the second ctrl+] closes the console")
	}
	if m.focus != m.activeLeftTab {
		t.Fatalf("focus=%v but the shown tab is %v", m.focus, m.activeLeftTab)
	}
}

// A `t`-maximised Worktrees tab stays maximised as Branches when the walk
// shows the Branches tab (activateTab's own re-pin rule): the pin must
// not name a hidden tab.
func TestTheWalkRepinsAMaximisedLeftTab(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	installSessionManager(t)
	startSessionIn(t, m, m.currentWorktree, "A1")
	m.activeLeftTab, m.focus, m.lastLeftPanel = panelWorktrees, panelWorktrees, panelWorktrees
	m.leftMaxed, m.leftMax = true, panelWorktrees
	m = pressAlt(t, m, 'a')
	if m.leftMax != panelBranches {
		t.Fatalf("leftMax=%v, want the shown Branches tab", m.leftMax)
	}
}
