package tui

import (
	"os"
	"testing"

	"github.com/homeend/gigagit/internal/steer"
)

// Serial (these three): startTestSession installs a process-global manager.

func TestKeptInboxSurvivesReRootAndIsDrained(t *testing.T) {
	m, a := steerModel(t)
	s := startTestSession(t, m, `sleep 5`)
	m.childInbox[s.Info().ID] = a
	m = m.initSteerInbox()
	m = m.closeSteerInbox() // reRoot's first step
	if _, ok := steer.Live(a, steer.TUIPresence); !ok {
		t.Fatal("reRoot must not drop the presence of an inbox a running child holds")
	}
	m.steerDir = t.TempDir() // …and the re-home to the new worktree
	m = m.tendKeptInboxes()
	if _, ok := steer.Live(a, steer.TUIPresence); !ok {
		t.Fatal("a running child's inbox must keep a live presence")
	}
	if _, err := steer.Post(a, steer.Command{Cmd: "focus", Panel: "branches"}); err != nil {
		t.Fatal(err)
	}
	m, _ = m.drainSteer()
	if left := steer.Drain(a); len(left) != 0 {
		t.Fatal("a kept inbox must be drained")
	}
}

func TestKeptInboxReleasedWhenChildEnds(t *testing.T) {
	m, _ := steerModel(t)
	s := startTestSession(t, m, `exit 0`)
	a := t.TempDir()
	m.childInbox[s.Info().ID] = a
	<-s.Done()
	m.keptSteer[a] = true
	if err := steer.Touch(a, steer.TUIPresence, steer.Presence{PID: os.Getpid()}); err != nil {
		t.Fatal(err)
	}
	m = m.tendKeptInboxes()
	if _, ok := steer.Live(a, steer.TUIPresence); ok {
		t.Fatal("the presence must go when the last child holding the inbox ends")
	}
	if len(m.childInbox) != 0 || len(m.keptSteer) != 0 {
		t.Fatalf("bookkeeping left behind: %v %v", m.childInbox, m.keptSteer)
	}
}

func TestKeptInboxNotStolenFromAnotherGG(t *testing.T) {
	m, _ := steerModel(t)
	s := startTestSession(t, m, `sleep 5`)
	a := t.TempDir()
	if err := steer.Touch(a, steer.TUIPresence, steer.Presence{PID: os.Getpid() + 1}); err != nil {
		t.Fatal(err)
	}
	m.childInbox[s.Info().ID] = a
	m = m.tendKeptInboxes()
	if p, _ := steer.Live(a, steer.TUIPresence); p.PID == os.Getpid() {
		t.Fatal("another live gg's presence must not be overwritten")
	}
	if m.keptSteer[a] {
		t.Fatal("an inbox another gg owns is not ours to drain")
	}
}
