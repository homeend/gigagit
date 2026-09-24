//go:build windows

package agentsession

import (
	"strings"
	"testing"
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
