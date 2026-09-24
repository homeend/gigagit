package agentsession

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestManagerLifecycle(t *testing.T) {
	t.Parallel()
	needSh(t)
	m := NewManager()
	a, err := m.Start(StartSpec{Label: "a", Dir: t.TempDir(), Argv: []string{"sh", "-c", "sleep 30"}, Cols: 40, Rows: 10})
	if err != nil {
		t.Fatal(err)
	}
	b, err := m.Start(StartSpec{Label: "b", Dir: t.TempDir(), Argv: []string{"sh", "-c", "exit 4"}, Cols: 40, Rows: 10})
	if err != nil {
		t.Fatal(err)
	}
	waitDone(t, b)
	if got := m.LiveCount(); got != 1 {
		t.Fatalf("LiveCount = %d, want 1", got)
	}
	l := m.List()
	if len(l) != 2 || l[0].ID != a.Info().ID || l[1].ExitCode != 4 {
		t.Fatalf("List = %+v", l)
	}
	if got, ok := m.Get(a.Info().ID); !ok || got != a {
		t.Fatal("Get did not return the started session")
	}
	if err := m.Remove(a.Info().ID); !errors.Is(err, ErrRunning) {
		t.Fatalf("Remove(running) = %v, want ErrRunning", err)
	}
	if err := m.Remove(b.Info().ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := m.Get(b.Info().ID); ok {
		t.Fatal("removed session still listed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	m.KillAll(ctx)
	if m.LiveCount() != 0 {
		t.Fatal("KillAll left a live session")
	}
}

func TestLifecycleMisuse(t *testing.T) {
	t.Parallel()
	needSh(t)
	m := NewManager()
	if err := m.Kill("nope"); !errors.Is(err, ErrNoSession) {
		t.Fatalf("Kill(unknown) = %v", err)
	}
	if err := m.Remove("nope"); !errors.Is(err, ErrNoSession) {
		t.Fatalf("Remove(unknown) = %v", err)
	}
	s, err := m.Start(StartSpec{Dir: t.TempDir(), Argv: []string{"sh", "-c", "exit 0"}, Cols: 40, Rows: 10})
	if err != nil {
		t.Fatal(err)
	}
	waitDone(t, s)
	if err := m.Kill(s.Info().ID); err != nil { // exited: no-op
		t.Fatalf("Kill(exited) = %v", err)
	}
	if err := m.Kill(s.Info().ID); err != nil { // twice: still no-op
		t.Fatalf("second Kill = %v", err)
	}
	s.SendText("late") // must not panic
}

func TestTapDropsWhenFull(t *testing.T) {
	t.Parallel()
	needSh(t)
	m := NewManager()
	s, err := m.Start(StartSpec{Dir: t.TempDir(), Argv: []string{"sh", "-c", `sleep 0.3; i=0; while [ $i -lt 5000 ]; do echo $i; i=$((i+1)); done`}, Cols: 40, Rows: 10})
	if err != nil {
		t.Fatal(err)
	}
	ch, cancel := s.Tap() // never read: the child must still run to completion
	defer cancel()
	waitDone(t, s)
	if len(ch) == 0 {
		t.Fatal("tap received nothing")
	}
}

func TestManagerChangedOnStartAndExit(t *testing.T) {
	t.Parallel()
	needSh(t)
	m := NewManager()
	s, err := m.Start(StartSpec{Dir: t.TempDir(), Argv: []string{"sh", "-c", "exit 0"}, Cols: 40, Rows: 10})
	if err != nil {
		t.Fatal(err)
	}
	waitDone(t, s)
	select {
	case <-m.Changed():
	case <-time.After(2 * time.Second):
		t.Fatal("no manager-level change signal")
	}
}

func TestStartMissingBinary(t *testing.T) {
	t.Parallel()
	m := NewManager()
	if _, err := m.Start(StartSpec{Dir: t.TempDir(), Argv: []string{"gg-no-such-binary-xyz"}}); err == nil {
		t.Fatal("Start of a missing binary must fail")
	}
	if len(m.List()) != 0 {
		t.Fatal("a failed start must not register a session")
	}
}
