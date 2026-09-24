package domain

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/agentsession"
	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/exttool"
)

// Serial: touches the process-global manager.
func TestSessionsIsProcessGlobal(t *testing.T) {
	if Sessions() != Sessions() {
		t.Fatal("Sessions() must return one manager for the process")
	}
	m := agentsession.NewManager()
	restore := UseSessionManager(m)
	if Sessions() != m {
		t.Fatal("UseSessionManager did not install the manager")
	}
	restore()
	if Sessions() == m {
		t.Fatal("restore did not reinstall the previous manager")
	}
}

func TestSessionCommandsFilters(t *testing.T) {
	t.Parallel()
	cfg := config.Config{}
	cfg.Tools.Command = []config.ToolCommand{
		{Category: "review", Name: "R", Mode: "capture", Command: "x"},
		{Category: "session", Name: "Claude", Mode: "session", Command: "claude"},
		{Category: "session", Name: "WebOnly", Mode: "session", Command: "x", Frontends: []string{"web"}},
		{Category: "session", Name: "", Mode: "session", Command: "broken"}, // invalid → inert
	}
	got := SessionCommands(cfg, "tui")
	if len(got) != 1 || got[0].Name != "Claude" {
		t.Fatalf("SessionCommands = %+v", got)
	}
}

func fakeDetect() []exttool.Detection {
	var out []exttool.Detection
	for _, tl := range exttool.Builtins() {
		if tl.ID == "claude" || tl.ID == "codex" {
			out = append(out, exttool.Detection{Tool: tl, Bin: tl.ID})
		}
	}
	return out
}

func TestEnsureSessionCommandsWritesSafeOnly(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.toml")
	added, err := EnsureSessionCommands(config.Config{}, path, fakeDetect)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(added, ",") != "Claude,Codex" {
		t.Fatalf("added = %v", added)
	}
	body, _ := os.ReadFile(path)
	if strings.Contains(string(body), "yolo") || !strings.Contains(string(body), `category = "session"`) {
		t.Fatalf("config body:\n%s", body)
	}
	cfg, err := config.Load(path, "")
	if err != nil {
		t.Fatal(err)
	}
	if n := len(SessionCommands(cfg, "tui")); n != 2 {
		t.Fatalf("written config yields %d session commands, want 2", n)
	}
	if again, _ := EnsureSessionCommands(cfg, path, fakeDetect); again != nil {
		t.Fatalf("second run added %v, want nil (already configured)", again)
	}
}

func TestEnsureSessionCommandsNothingDetected(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.toml")
	added, err := EnsureSessionCommands(config.Config{}, path, func() []exttool.Detection { return nil })
	if err != nil || added != nil {
		t.Fatalf("added=%v err=%v", added, err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("nothing detected must not create the config file")
	}
}

// Serial: installs a process-global manager.
func TestStartSessionRunsInWorktree(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh-based")
	}
	restore := UseSessionManager(agentsession.NewManager())
	defer restore()
	dir := cleanDir(t)
	svc := Open(dir)
	tc := config.ToolCommand{Category: "session", Name: "Shell", Mode: "session", Command: `printf 'IN[%s]' "$(pwd)"; exit 7`}
	s, err := svc.StartSession(context.Background(), tc, dir, "", 80, 10, nil)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-s.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("no exit")
	}
	info := s.Info()
	if info.ExitCode != 7 || info.Dir != dir || info.Label != "Shell" || info.Repo == "" {
		t.Fatalf("info = %+v", info)
	}
	if len(Sessions().List()) != 1 {
		t.Fatal("session not registered with the process-global manager")
	}
}

func TestAgentIDFor(t *testing.T) {
	t.Parallel()
	for cmd, want := range map[string]string{
		"claude":                        "claude",
		"/usr/local/bin/codex --x":      "codex",
		`"C:\Tools\agy.exe" --dangerou`: "antigravity",
		"bash":                          "",
		"":                              "",
	} {
		if got := agentIDFor(config.ToolCommand{Command: cmd}); got != want {
			t.Errorf("agentIDFor(%q) = %q, want %q", cmd, got, want)
		}
	}
}

// A session block the user wrote — even one hidden from this frontend or
// invalid — means "configured": the first-run writer must not append more.
func TestEnsureSessionCommandsRespectsExistingBlock(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.toml")
	cfg := config.Config{}
	cfg.Tools.Command = []config.ToolCommand{{Category: "session", Name: "Mine", Mode: "terminal", Command: "x"}} // invalid mode
	added, err := EnsureSessionCommands(cfg, path, fakeDetect)
	if err != nil || added != nil {
		t.Fatalf("added=%v err=%v, want nothing", added, err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("an existing session block must leave the config file untouched")
	}
}

// cmd.exe does not understand the \" escaping Windows argv composition
// applies, so a quoted install path must reach it verbatim: /S /C "<line>".
func TestSessionShellLineWindows(t *testing.T) {
	t.Parallel()
	argv, cmdline := sessionShell(`"C:\Program Files\Claude\claude.exe" --dangerously-skip-permissions`, "windows", func(k string) string {
		if k == "COMSPEC" {
			return `C:\Windows\system32\cmd.exe`
		}
		return ""
	})
	if want := `"C:\Windows\system32\cmd.exe" /S /C ""C:\Program Files\Claude\claude.exe" --dangerously-skip-permissions"`; cmdline != want {
		t.Fatalf("cmdline = %s\nwant      %s", cmdline, want)
	}
	if len(argv) == 0 || argv[0] != `C:\Windows\system32\cmd.exe` {
		t.Fatalf("argv = %q", argv)
	}
}

func TestSessionShellLinePOSIX(t *testing.T) {
	t.Parallel()
	argv, cmdline := sessionShell(`claude --x; echo done`, "linux", func(k string) string {
		if k == "SHELL" {
			return "/bin/zsh"
		}
		return ""
	})
	if cmdline != "" || strings.Join(argv, "|") != "/bin/zsh|-c|claude --x; echo done" {
		t.Fatalf("argv=%q cmdline=%q", argv, cmdline)
	}
}

func TestSessionShellLineWindowsMultiLine(t *testing.T) {
	t.Parallel()
	_, cmdline := sessionShell("set X=1\necho %X%", "windows", func(string) string { return "" })
	if want := `"cmd.exe" /S /C "set X=1 & echo %X%"`; cmdline != want {
		t.Fatalf("cmdline = %s, want %s", cmdline, want)
	}
}

// GG_SESSION_TRACE=<dir> records every session's raw output there (serial:
// env + the process-global manager).
func TestStartSessionTraceEnv(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh-based")
	}
	dir := t.TempDir()
	t.Setenv("GG_SESSION_TRACE", dir)
	restore := UseSessionManager(agentsession.NewManager())
	defer restore()
	wt := cleanDir(t)
	s, err := Open(wt).StartSession(context.Background(), config.ToolCommand{Category: "session", Name: "Shell", Mode: "session", Command: `printf TRACED`}, wt, "", 80, 10, nil)
	if err != nil {
		t.Fatal(err)
	}
	<-s.Done()
	files, _ := filepath.Glob(filepath.Join(dir, "*.raw"))
	if len(files) != 1 {
		t.Fatalf("trace files = %v", files)
	}
	b, _ := os.ReadFile(files[0])
	if !strings.Contains(string(b), "TRACED") {
		t.Fatalf("trace = %q", b)
	}
}

func TestTerminalShell(t *testing.T) {
	t.Parallel()
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	have := func(names ...string) func(string) (string, error) {
		return func(n string) (string, error) {
			for _, h := range names {
				if h == n {
					return `C:\bin\` + n + ".exe", nil
				}
			}
			return "", errors.New("not found")
		}
	}
	for _, c := range []struct {
		name, goos, override string
		env                  map[string]string
		path                 []string
		want                 string
	}{
		{"unix SHELL", "linux", "", map[string]string{"SHELL": "/bin/zsh"}, nil, "/bin/zsh"},
		{"unix fallback", "linux", "", nil, nil, "/bin/sh"},
		{"override wins", "linux", "/usr/bin/fish", map[string]string{"SHELL": "/bin/zsh"}, nil, "/usr/bin/fish"},
		{"win pwsh", "windows", "", nil, []string{"pwsh", "powershell", "cmd"}, `C:\bin\pwsh.exe`},
		{"win powershell", "windows", "", nil, []string{"powershell", "cmd"}, `C:\bin\powershell.exe`},
		{"win cmd via COMSPEC", "windows", "", map[string]string{"COMSPEC": `C:\Windows\system32\cmd.exe`}, nil, `C:\Windows\system32\cmd.exe`},
		{"win cmd fallback", "windows", "", nil, nil, "cmd.exe"},
		{"win override", "windows", "nu", nil, []string{"pwsh"}, "nu"},
	} {
		if got := terminalShell(c.goos, env(c.env), have(c.path...), c.override); len(got) != 1 || got[0] != c.want {
			t.Errorf("%s: %v, want [%s]", c.name, got, c.want)
		}
	}
}

func waitSessionText(t *testing.T, s *AgentSession, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, l := range s.Screen().Lines {
			if strings.Contains(l, want) {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("screen never showed %q", want)
}

// Serial: installs a process-global manager.
func TestStartSessionPassesEnv(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh-based")
	}
	restore := UseSessionManager(agentsession.NewManager())
	defer restore()
	defer Sessions().KillAll(context.Background())
	dir := cleanDir(t)
	tc := config.ToolCommand{Category: "session", Name: "Shell", Mode: "session", Command: `printf "INBOX=%s" "$GG_INBOX"; sleep 5`}
	s, err := Open(dir).StartSession(context.Background(), tc, dir, "", 80, 10, []string{"GG_INBOX=/tmp/inbox-x"})
	if err != nil {
		t.Fatal(err)
	}
	waitSessionText(t, s, "INBOX=/tmp/inbox-x")
}

// Serial: installs a process-global manager.
func TestStartTerminalRunsTheShell(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh-based")
	}
	restore := UseSessionManager(agentsession.NewManager())
	defer restore()
	defer Sessions().KillAll(context.Background())
	dir := cleanDir(t)
	s, err := Open(dir).StartTerminal(context.Background(), "/bin/sh", dir, "", 80, 10, []string{"GG_INBOX=/tmp/inbox-t"})
	if err != nil {
		t.Fatal(err)
	}
	if got := s.Info().Label; got != "Terminal" {
		t.Fatalf("label = %q", got)
	}
	s.SendText("echo \"T-$GG_INBOX\"\r")
	waitSessionText(t, s, "T-/tmp/inbox-t")
}
