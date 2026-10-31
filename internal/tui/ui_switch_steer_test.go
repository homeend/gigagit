package tui

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/steer"
)

func steerBlocked(t *testing.T) (Model, string, *fakeWebHost) {
	t.Helper()
	f := installFakeHost(t)
	m, dir := steerModel(t)
	m = m.ensureWeb()
	m = runOne(t, m, startWebCmd(m.homeSvc(), m.web, "", false))
	return m, dir, f
}

func TestServeFlipsWithoutConfirm(t *testing.T) {
	m, dir, f := steerBlocked(t)
	f.clients = 1
	nm, cmd := m.applySteer(steer.Command{ID: "c-1", Cmd: "serve", Wait: true})
	runLeaves(cmd)
	if !nm.webActive || nm.modal != nil {
		t.Fatalf("active %v modal %+v", nm.webActive, nm.modal)
	}
	rep, ok := steer.AwaitReply(dir, "c-1", 2e9)
	if !ok || !rep.OK || rep.Detail != "http://127.0.0.1:4242" {
		t.Fatalf("reply %+v ok %v", rep, ok)
	}
}

func TestServeRefusedWhileBusy(t *testing.T) {
	m, dir, _ := steerBlocked(t)
	m.running = true
	nm, cmd := m.applySteer(steer.Command{ID: "c-2", Cmd: "serve", Wait: true})
	runLeaves(cmd)
	if nm.webActive {
		t.Fatal("a busy terminal keeps control")
	}
	rep, ok := steer.AwaitReply(dir, "c-2", 2e9)
	if !ok || rep.OK || !strings.Contains(rep.Error, "an operation is running") {
		t.Fatalf("reply %+v ok %v", rep, ok)
	}
}

func TestBlockedSteerRefused(t *testing.T) {
	m, dir, _ := steerBlocked(t)
	m, _ = m.activateWeb()
	nm, cmd := m.applySteer(steer.Command{ID: "c-3", Cmd: "navigate", File: "a.txt", Wait: true})
	runLeaves(cmd)
	rep, ok := steer.AwaitReply(dir, "c-3", 2e9)
	if !ok || rep.OK || !strings.Contains(rep.Error, "the web UI is active in this gg") {
		t.Fatalf("reply %+v ok %v", rep, ok)
	}
	inflight := len(nm.srcInflight)
	nm, cmd = nm.applySteer(steer.Command{ID: "c-4", Cmd: "reload", Sources: []string{"status"}, Wait: true})
	runLeaves(cmd)
	rep, ok = steer.AwaitReply(dir, "c-4", 2e9)
	if !ok || !rep.OK {
		t.Fatalf("reply %+v ok %v", rep, ok)
	}
	if len(nm.srcInflight) != inflight {
		t.Fatal("a reload reads nothing behind the block")
	}
}

func TestPresenceFollowsTheFlip(t *testing.T) {
	m, dir, _ := steerBlocked(t)
	m.touchSteerPresence()
	m, _ = m.activateWeb()
	p, live := steer.Live(dir, steer.TUIPresence)
	if !live || !p.Inactive || p.URL != "http://127.0.0.1:4242" {
		t.Fatalf("blocked presence %+v live %v", p, live)
	}
	m, _ = m.activateTUI()
	p, live = steer.Live(dir, steer.TUIPresence)
	if !live || p.Inactive || p.URL != "" {
		t.Fatalf("active presence %+v live %v", p, live)
	}
}

func TestPresenceFollowsTheFlipInKeptInbox(t *testing.T) {
	m, _, _ := steerBlocked(t)
	m.width, m.height = 120, 40
	s := startTestSession(t, m, `sleep 5`)
	kept := t.TempDir()
	m.childInbox = map[domain.SessionID]string{s.Info().ID: kept}
	m = m.tendKeptInboxes()
	m, _ = m.activateWeb()
	p, live := steer.Live(kept, steer.TUIPresence)
	if !live || !p.Inactive || p.URL != "http://127.0.0.1:4242" {
		t.Fatalf("an agent's inbox must lead to the page: %+v live %v", p, live)
	}
}

func TestSnapshotNamesInterface(t *testing.T) {
	m, _, _ := steerBlocked(t)
	read := func(m Model) string {
		b, err := json.Marshal(buildSessionSnapshot(m))
		if err != nil {
			t.Fatal(err)
		}
		var v struct {
			Interface string `json:"interface"`
		}
		_ = json.Unmarshal(b, &v)
		return v.Interface
	}
	if got := read(m); got != "tui" {
		t.Fatalf("interface = %q", got)
	}
	m, _ = m.activateWeb()
	if got := read(m); got != "web" {
		t.Fatalf("interface = %q", got)
	}
}
