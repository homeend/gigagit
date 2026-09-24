package domain

import (
	"context"
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
	s, err := svc.StartSession(context.Background(), tc, dir, 80, 10)
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
