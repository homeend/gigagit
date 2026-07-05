package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeCfg(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadParsesToolCommands(t *testing.T) {
	dir := t.TempDir()
	repo := writeCfg(t, dir, ".gg.toml", `
[[tools.command]]
category = "conflict"
name = "Claude"
mode = "terminal"
per_file = false
when_op = ""
command = '''
claude "resolve <op>"
'''
`)
	cfg, err := Load("", repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Tools.Command) != 1 {
		t.Fatalf("got %d commands, want 1", len(cfg.Tools.Command))
	}
	tc := cfg.Tools.Command[0]
	if tc.Category != "conflict" || tc.Name != "Claude" || tc.Mode != "terminal" {
		t.Errorf("parsed %+v", tc)
	}
	if want := "claude \"resolve <op>\"\n"; tc.Command != want {
		t.Errorf("command = %q, want %q", tc.Command, want)
	}
}

func TestToolsOverlayConcatRepoWins(t *testing.T) {
	dir := t.TempDir()
	global := writeCfg(t, dir, "global.toml", `
[[tools.command]]
category = "conflict"
name = "Claude"
mode = "terminal"
command = "claude-global"

[[tools.command]]
category = "conflict"
name = "Meld"
mode = "terminal"
per_file = true
command = "meld-global"
`)
	repo := writeCfg(t, dir, ".gg.toml", `
[[tools.command]]
category = "conflict"
name = "Claude"
mode = "terminal"
command = "claude-repo"
`)
	cfg, err := Load(global, repo)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, tc := range cfg.Tools.Command {
		got[tc.Name] = tc.Command
	}
	if len(cfg.Tools.Command) != 2 {
		t.Fatalf("want concat of 2 distinct commands, got %d: %v", len(cfg.Tools.Command), got)
	}
	if got["Claude"] != "claude-repo" {
		t.Errorf("repo must win the (category,name) collision: got %q", got["Claude"])
	}
	if got["Meld"] != "meld-global" {
		t.Errorf("global-only command must survive: got %q", got["Meld"])
	}
}

func TestValidateToolCommand(t *testing.T) {
	ok := ToolCommand{Category: "conflict", Name: "X", Mode: "terminal", Command: "x"}
	if err := ValidateToolCommand(ok); err != nil {
		t.Errorf("valid command rejected: %v", err)
	}
	bad := []ToolCommand{
		{Category: "nope", Name: "X", Mode: "terminal", Command: "x"},
		{Category: "conflict", Name: "", Mode: "terminal", Command: "x"},
		{Category: "conflict", Name: "X", Mode: "sideways", Command: "x"},
		{Category: "conflict", Name: "X", Mode: "terminal", Command: ""},
		{Category: "commit_message", Name: "X", Mode: "terminal", PerFile: true, Command: "x"},
		{Category: "conflict", Name: "X", Mode: "terminal", WhenOp: "push", Command: "x"},
	}
	for i, tc := range bad {
		if err := ValidateToolCommand(tc); err == nil {
			t.Errorf("bad[%d] %+v: want error", i, tc)
		}
	}
}

func TestAppendToolCommands(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml") // file does not exist yet
	cmds := []ToolCommand{{Category: "conflict", Name: "Meld", Mode: "terminal", PerFile: true,
		Command: "meld --auto-merge --output=<merged> <local> <base> <remote>"}}
	if err := AppendToolCommands(path, cmds); err != nil {
		t.Fatal(err)
	}
	// Round-trips through Load.
	cfg, err := Load(path, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Tools.Command) != 1 || cfg.Tools.Command[0].Name != "Meld" || !cfg.Tools.Command[0].PerFile {
		t.Fatalf("round-trip got %+v", cfg.Tools.Command)
	}
	// Appending more preserves existing content byte-for-byte.
	before, _ := os.ReadFile(path)
	if err := AppendToolCommands(path, []ToolCommand{{Category: "conflict", Name: "Claude", Mode: "terminal", Command: "claude x"}}); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	if !strings.HasPrefix(string(after), string(before)) {
		t.Error("append must not rewrite existing content")
	}
	cfg, err = Load(path, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Tools.Command) != 2 {
		t.Fatalf("want 2 after second append, got %d", len(cfg.Tools.Command))
	}
	// A command containing the ''' delimiter is refused, not corrupted.
	if err := AppendToolCommands(path, []ToolCommand{{Category: "conflict", Name: "Evil", Mode: "terminal", Command: "x''' oops"}}); err == nil {
		t.Error("''' in command must be refused")
	}
}

// The scalar writers must not corrupt a file holding [[tools.command]] blocks
// (their multi-line command values could contain section-header-like lines).
func TestScalarWriterSurvivesToolBlocks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := AppendToolCommands(path, []ToolCommand{{Category: "conflict", Name: "X", Mode: "terminal",
		Command: "line1\n[worktree]\nline3"}}); err != nil {
		t.Fatal(err)
	}
	if err := SetGlobalRefreshEnabled(path, true); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path, "")
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Refresh.Enabled {
		t.Error("refresh.enabled not set")
	}
	if len(cfg.Tools.Command) != 1 || cfg.Tools.Command[0].Command != "line1\n[worktree]\nline3\n" {
		t.Errorf("tool block corrupted: %+v", cfg.Tools.Command)
	}
}
