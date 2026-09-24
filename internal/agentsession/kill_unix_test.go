//go:build !windows

package agentsession

import (
	"os"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

func TestKillTakesProcessGroup(t *testing.T) {
	t.Parallel()
	needSh(t)
	m := NewManager()
	// The shell backgrounds a grandchild that ignores SIGHUP and prints its
	// pid. Ignoring HUP matters: when the leader dies the kernel HUPs the
	// terminal's foreground group, which would hide a leader-only kill.
	s, err := m.Start(StartSpec{Dir: t.TempDir(), Argv: []string{"sh", "-c", `(trap '' HUP; exec sleep 61) & echo "GC=$!"; wait`}, Cols: 40, Rows: 10})
	if err != nil {
		t.Fatal(err)
	}
	var gc int
	eventually(t, "grandchild pid", func() bool {
		txt := s.screenText()
		i := strings.Index(txt, "GC=")
		if i < 0 {
			return false
		}
		f := strings.Fields(txt[i+3:])
		if len(f) == 0 {
			return false
		}
		gc, _ = strconv.Atoi(f[0])
		return gc > 0
	})
	if err := m.Kill(s.Info().ID); err != nil {
		t.Fatal(err)
	}
	waitDone(t, s)
	t.Cleanup(func() { _ = syscall.Kill(gc, syscall.SIGKILL) })
	eventually(t, "grandchild gone", func() bool {
		p, _ := os.FindProcess(gc)
		return p.Signal(syscall.Signal(0)) != nil
	})
}
