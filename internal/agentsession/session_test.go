package agentsession

import (
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"
)

func needSh(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("scripted children use sh; the Windows path is covered by TestWindowsConPTY")
	}
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh")
	}
}

func startSh(t *testing.T, script string) *Session {
	t.Helper()
	s, err := start("t1", StartSpec{Label: "sh", Dir: t.TempDir(), Argv: []string{"sh", "-c", script}, Cols: 40, Rows: 10})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.kill(); <-s.Done() })
	return s
}

func waitDone(t *testing.T, s *Session) {
	t.Helper()
	select {
	case <-s.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("session did not exit")
	}
}

func TestSessionExitCode(t *testing.T) {
	t.Parallel()
	needSh(t)
	s := startSh(t, "printf hello; exit 3")
	waitDone(t, s)
	info := s.Info()
	if info.State != Exited || info.ExitCode != 3 {
		t.Fatalf("got state=%v code=%d, want Exited/3", info.State, info.ExitCode)
	}
	if !strings.Contains(s.screenText(), "hello") {
		t.Fatalf("screen = %q", s.screenText())
	}
}

func TestSessionEnvAndDir(t *testing.T) {
	t.Parallel()
	needSh(t)
	dir := t.TempDir()
	s, err := start("envid", StartSpec{Dir: dir, Argv: []string{"sh", "-c", `printf '%s|%s|' "$GG_SESSION_ID" "$TERM"; pwd; sleep 0.3`}, Cols: 200, Rows: 5})
	if err != nil {
		t.Fatal(err)
	}
	waitDone(t, s)
	got := s.screenText()
	if !strings.Contains(got, "envid|xterm-256color|") || !strings.Contains(got, dir) {
		t.Fatalf("screen = %q", got)
	}
}

// Agents detect a host tmux through TMUX/TMUX_PANE; the console is not a
// tmux pane, so the child must not inherit them (spike finding 4).
func TestSessionDropsHostTmuxEnv(t *testing.T) {
	t.Setenv("TMUX", "/tmp/fake,1,0")
	t.Setenv("TMUX_PANE", "%9")
	needSh(t)
	s := startSh(t, `printf 'T[%s|%s]' "${TMUX-unset}" "${TMUX_PANE-unset}"; sleep 0.3`)
	waitDone(t, s)
	if got := s.screenText(); !strings.Contains(got, "T[unset|unset]") {
		t.Fatalf("screen = %q", got)
	}
}

func TestChangedCoalesces(t *testing.T) {
	t.Parallel()
	needSh(t)
	s := startSh(t, "i=0; while [ $i -lt 2000 ]; do echo line$i; i=$((i+1)); done; sleep 0.2")
	waitDone(t, s)
	n := 0
drain:
	for {
		select {
		case <-s.Changed():
			n++
		default:
			break drain
		}
	}
	if n != 1 {
		t.Fatalf("pending Changed signals = %d, want exactly 1 (coalesced)", n)
	}
}

func TestEmulatorRepliesReachChild(t *testing.T) {
	t.Parallel()
	needSh(t)
	// Ask for the cursor position (ESC[6n) and read the reply (ESC[row;colR)
	// in raw mode; a missing emulator→PTY pump leaves dd blocked.
	s := startSh(t, `stty raw -echo; printf '\033[6n'; r=$(dd bs=1 count=6 2>/dev/null | od -An -c | tr -d ' \n'); stty sane; printf 'GOT[%s]' "$r"; sleep 0.3`)
	waitDone(t, s)
	if got := s.screenText(); !strings.Contains(got, "GOT[033[1;1R") {
		t.Fatalf("cursor-position reply not delivered; screen = %q", got)
	}
}

// TracePath records the child's raw output from the very first byte — the
// evidence for emulator mismatches (e.g. ConPTY repaints) replayed offline.
func TestSessionTraceRecordsRawOutput(t *testing.T) {
	t.Parallel()
	needSh(t)
	trace := t.TempDir() + "/s.raw"
	s, err := start("tr", StartSpec{Dir: t.TempDir(), Argv: []string{"sh", "-c", `printf '\033[1mFIRST\033[0m'; printf LAST`}, Cols: 40, Rows: 5, TracePath: trace})
	if err != nil {
		t.Fatal(err)
	}
	waitDone(t, s)
	b, err := os.ReadFile(trace)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(b), "\x1b[1mFIRST") || !strings.HasSuffix(string(b), "LAST") {
		t.Fatalf("trace = %q", b)
	}
}

// The .events sidecar logs the size at every byte offset where it changed,
// so a replay resizes at the same points the live emulator did.
func TestSessionTraceLogsResizes(t *testing.T) {
	t.Parallel()
	needSh(t)
	trace := t.TempDir() + "/s.raw"
	s, err := start("tr", StartSpec{Dir: t.TempDir(), Argv: []string{"sh", "-c", `printf AB; read _; printf CD`}, Cols: 40, Rows: 5, TracePath: trace})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(s.screenText(), "AB") && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if err := s.Resize(50, 7); err != nil {
		t.Fatal(err)
	}
	s.SendText("\r")
	waitDone(t, s)
	ev, err := os.ReadFile(trace + ".events")
	if err != nil {
		t.Fatal(err)
	}
	if got := string(ev); got != "0 40 5\n2 50 7\n" {
		t.Fatalf("events = %q", got)
	}
}

func TestSessionUTF8TitleDoesNotLeak(t *testing.T) {
	t.Parallel()
	needSh(t)
	s := startSh(t, `printf '\033]0;\342\234\263 Claude Code\007READY'; sleep 0.3`)
	waitDone(t, s)
	if got := s.screenText(); !strings.HasPrefix(got, "READY") {
		t.Fatalf("screen = %q", strings.SplitN(got, "\n", 2)[0])
	}
}

// Cwd runs the process somewhere else than the worktree it belongs to (a
// worktree git recorded under the other environment's notation, reached via
// its translated path); Dir stays the identity every frontend groups by.
func TestStartCwdOverridesTheProcessDirOnly(t *testing.T) {
	t.Parallel()
	needSh(t)
	cwd := t.TempDir()
	s, err := start("t-cwd", StartSpec{Label: "sh", Dir: "/w/identity", Cwd: cwd, Argv: []string{"sh", "-c", `printf 'AT[%s]' "$(pwd)"; sleep 2`}, Cols: 80, Rows: 10})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.kill(); <-s.Done() })
	eventually(t, "pwd", func() bool { return strings.Contains(s.screenText(), "AT["+cwd+"]") })
	if got := s.Info().Dir; got != "/w/identity" {
		t.Fatalf("Info.Dir = %q, want the worktree identity", got)
	}
}
