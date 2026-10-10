package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"

	"github.com/homeend/gigagit/internal/agentsession"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/engine"
)

// installSessionManager swaps in a test session manager for the test's
// lifetime (the first half of startTestSession, which is home-bound).
func installSessionManager(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("sh-based")
	}
	restore := domain.UseSessionManager(agentsession.NewManager())
	t.Cleanup(func() {
		domain.Sessions().KillAll(t.Context())
		restore()
	})
}

// Showing a console of worktree B puts B's files on screen; closing it
// (esc) brings home back with its cursor.
func TestShownConsoleViewsItsWorktreeAndCloseReturns(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	m, other := addWorktree(t, m, "wt2")
	if err := os.WriteFile(filepath.Join(other, "b-only.txt"), []byte("b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	installSessionManager(t)
	id := startSessionIn(t, m, other, "Shell")
	m.sel[panelFiles] = 1
	m, _ = m.showConsole(id, false)
	if m.viewed != model.KeyOf(other) || m.console == nil || m.console.ret.view != m.home {
		t.Fatalf("viewed=%q console=%+v", m.viewed, m.console)
	}
	m = landView(t, m)
	var seen bool
	for _, f := range m.status.Files {
		seen = seen || f.Path == "b-only.txt"
	}
	if !seen {
		t.Fatalf("status = %+v, want B's file", m.status.Files)
	}
	m = m.closeConsole()
	if m.viewed != m.home || m.sel[panelFiles] != 1 {
		t.Fatalf("after close: viewed=%q sel=%d", m.viewed, m.sel[panelFiles])
	}
}

// Looking equals switching: a file staged while B's console is shown lands
// in B's index.
func TestStagingWhileAConsoleIsShownRunsInItsWorktree(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	m, other := addWorktree(t, m, "wt2")
	if err := os.WriteFile(filepath.Join(other, "b-only.txt"), []byte("b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	installSessionManager(t)
	id := startSessionIn(t, m, other, "Shell")
	m, _ = m.showConsole(id, false)
	m = landView(t, m)
	if res, ok := m.stageCmd(engine.Stage{Paths: []string{"b-only.txt"}})().(opFinishedMsg); ok && res.err != nil {
		t.Fatal(res.err)
	}
	out, _ := exec.Command("git", "-C", other, "diff", "--cached", "--name-only").Output()
	if !strings.Contains(string(out), "b-only.txt") {
		t.Fatalf("B's index = %q", out)
	}
	out, _ = exec.Command("git", "-C", m.homeWorktree(), "diff", "--cached", "--name-only").Output()
	if strings.Contains(string(out), "b-only.txt") {
		t.Fatal("home's index took the stage")
	}
}

// alt+a over sessions in two worktrees: two distinct views, then round
// again — the ring never stops at home.
func TestAltACyclesWorktreesRoundAndRound(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	m, wtA := addWorktree(t, m, "wtA")
	m, wtB := addWorktree(t, m, "wtB")
	installSessionManager(t)
	startSessionIn(t, m, wtA, "A")
	startSessionIn(t, m, wtB, "B")
	m = pressAlt(t, m, 'a')
	first := m.viewed
	m = pressAlt(t, m, 'a')
	second := m.viewed
	m = pressAlt(t, m, 'a')
	if m.console == nil || m.viewed != first {
		t.Fatalf("third press: console=%v viewed=%q, want %q again", m.console != nil, m.viewed, first)
	}
	if first == second || first == m.home || second == m.home {
		t.Fatalf("views %q %q %q must differ", first, second, m.home)
	}
}

// A console in gg's own worktree swaps nothing.
func TestConsoleInHomeSwapsNothing(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	s := startTestSession(t, m, `sleep 5`)
	m, _ = m.showConsole(s.Info().ID, false)
	if m.viewed != m.home || m.viewKick {
		t.Fatalf("viewed=%q kick=%v", m.viewed, m.viewKick)
	}
}

// With an operation running the console still shows, but the view stays
// (the swap is refused and said on the status line).
func TestConsoleShowRefusesTheSwapWhileAnOpRuns(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	m, other := addWorktree(t, m, "wt2")
	installSessionManager(t)
	id := startSessionIn(t, m, other, "Shell")
	m.running = true
	m, _ = m.showConsole(id, false)
	if m.console == nil || m.viewed != m.home || m.statusMsg == "" {
		t.Fatalf("console=%+v viewed=%q msg=%q", m.console, m.viewed, m.statusMsg)
	}
}
