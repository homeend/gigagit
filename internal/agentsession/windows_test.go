//go:build windows

package agentsession

import (
	"strings"
	"testing"
	"time"
)

func TestWindowsConPTY(t *testing.T) {
	m := NewManager()
	s, err := m.Start(StartSpec{Dir: t.TempDir(), Argv: []string{"cmd", "/c", "echo WINOK & exit /b 5"}, Cols: 60, Rows: 10})
	if err != nil {
		t.Fatal(err)
	}
	waitDone(t, s)
	if !strings.Contains(s.screenText(), "WINOK") || s.Info().ExitCode != 5 {
		t.Fatalf("screen=%q code=%d", s.screenText(), s.Info().ExitCode)
	}
}

// ConPTY keeps the output pipe open after the child exits; the exit must be
// recorded once output goes quiet, not at the 2 s drain bound.
func TestWindowsExitIsPrompt(t *testing.T) {
	m := NewManager()
	began := time.Now()
	s, err := m.Start(StartSpec{Dir: t.TempDir(), Argv: []string{"cmd", "/c", "echo BYE & exit /b 3"}, Cols: 60, Rows: 10})
	if err != nil {
		t.Fatal(err)
	}
	waitDone(t, s)
	if d := time.Since(began); d > 1500*time.Millisecond {
		t.Fatalf("exit recorded after %v, want well under the 2 s drain bound", d)
	}
	if !strings.Contains(s.screenText(), "BYE") || s.Info().ExitCode != 3 {
		t.Fatalf("screen=%q code=%d", s.screenText(), s.Info().ExitCode)
	}
}
