package tui

import (
	"errors"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/steer"
)

// Review finding: a switch that adopts a look closes the own inbox first, and
// the inbox a running agent holds stays behind with a stale "active" file.
func TestPresenceInKeptInboxAfterAdoptedLook(t *testing.T) {
	m, dir, _ := steerBlocked(t)
	m.width, m.height = 120, 40
	s := startTestSession(t, m, `sleep 5`)
	m.childInbox = map[domain.SessionID]string{s.Info().ID: dir}
	m.touchSteerPresence()
	m.viewed = "/looked/at"
	m, _ = m.activateWeb()
	if m.steerDir == dir {
		t.Fatal("precondition: the adopt closes the own inbox")
	}
	p, live := steer.Live(dir, steer.TUIPresence)
	if !live || !p.Inactive || p.URL != "http://127.0.0.1:4242" {
		t.Fatalf("the agent's inbox must lead to the page: %+v live %v", p, live)
	}
}

// Review finding: activation must also reload the sleeping members; their
// watch events were dropped while blocked.
func TestActivationReloadsSleepingMembers(t *testing.T) {
	f := installFakeHost(t)
	f.clients = 1
	m := loadedModel(t)
	m.width, m.height = 160, 50
	m, b := openTwo(t, m)
	m, cmd := m.activateWeb()
	m = runOne(t, m, cmd)
	m, cmd = m.activateTUI()
	got := false
	for _, msg := range runLeaves(cmd) {
		if ml, ok := msg.(memberLoadedMsg); ok && ml.svc == b.svc {
			got = true
		}
	}
	if !got {
		t.Fatal("the sleeping member was not read on activation")
	}
}

// Review claim: a member look is not adopted before the flip.
func TestSwitchFromMemberLookMovesThePage(t *testing.T) {
	f := installFakeHost(t)
	f.clients = 1
	m := loadedModel(t)
	m.width, m.height = 160, 50
	m, cmd := m.activateWeb()
	m = runOne(t, m, cmd)
	m, _ = m.activateTUI()
	m, b := openTwo(t, m)
	nm, ok := m.switchRepo(b, "", true)
	if !ok {
		t.Fatal(nm.statusMsg)
	}
	before := len(f.reroots)
	m, cmd = nm.activateWeb()
	runLeaves(cmd)
	if len(f.reroots) != before+1 || f.reroots[before] != b.svc {
		t.Fatalf("the page must follow to the member on screen: reroots %d", len(f.reroots)-before)
	}
}

// Review finding: enter in the quit popup over the block opened a session,
// which may re-root the terminal and move the page.
func TestQuitPopupEnterDoesNothingOverBlock(t *testing.T) {
	m, _ := blockedModel(t)
	m.width, m.height = 120, 40
	s := startTestSession(t, m, `sleep 5`)
	_ = s
	m, _ = m.openSessionsPopup(true)
	if m.quitPopupUp() == nil {
		t.Fatal("precondition")
	}
	m, _ = updated(t, m, keyMsg("enter"))
	if m.quitPopupUp() == nil || m.console != nil {
		t.Fatalf("enter left the quit popup: console %+v", m.console)
	}
}

// Review finding: a failed --web start must do what activation does, or the
// health result it dropped takes the launch prompts with it.
func TestWebLaunchPortTakenProbesHealth(t *testing.T) {
	f := installFakeHost(t)
	f.startErr = errors.New("address already in use")
	m := loadedModel(t).applyWebLaunch(webLaunchOptions{Web: true})
	nm, cmd := m.onWebStarted(webStartedMsg{err: f.startErr})
	if nm.webActive {
		t.Fatal("still blocked")
	}
	saw := false
	for _, msg := range runLeaves(cmd) {
		if _, ok := msg.(repoHealthMsg); ok {
			saw = true
		}
	}
	if !saw || !strings.Contains(nm.statusMsg, "address already in use") {
		t.Fatalf("health probed %v status %q", saw, nm.statusMsg)
	}
}
