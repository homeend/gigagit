package tui

import (
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/template"
)

func TestToolCommandHash(t *testing.T) {
	a, b := toolCommandHash("cmd one"), toolCommandHash("cmd two")
	if len(a) != 16 || a == b {
		t.Errorf("hash: %q vs %q", a, b)
	}
	if a != toolCommandHash("cmd one") {
		t.Error("hash must be deterministic")
	}
}

func TestToolEnv(t *testing.T) {
	env := toolEnv(template.CmdCtx{Op: "merge", Source: "f", Target: "main",
		Repo: "/r", ConflictedFiles: []string{"a.go", "b.go"},
		File: "a.go", Local: "/t/l", Base: "/t/b", Remote: "/t/r", Merged: "/r/a.go",
		ContextFile: "/tmp/gg-context-1.txt"})
	sort.Strings(env)
	want := []string{
		"GG_BASE=/t/b", "GG_CONFLICTED_FILES=a.go b.go", "GG_CONTEXT_FILE=/tmp/gg-context-1.txt",
		"GG_FILE=a.go", "GG_LOCAL=/t/l", "GG_MERGED=/r/a.go", "GG_OP=merge", "GG_REMOTE=/t/r",
		"GG_REPO=/r", "GG_SOURCE=f", "GG_TARGET=main",
	}
	if len(env) != 11 {
		t.Fatalf("want 11 entries, got %d: %v", len(env), env)
	}
	if strings.Join(env, "|") != strings.Join(want, "|") {
		t.Errorf("env = %v\nwant %v", env, want)
	}
}

func TestToolContextFileContent(t *testing.T) {
	ctx := template.CmdCtx{
		Op: "merge", Source: "feature", Target: "main",
		ConflictedFiles: []string{"a$(x) b.go", "c.go"},
	}
	path, err := toolContextFile(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(path)
	if !strings.HasPrefix(filepath.Base(path), "gg-context-") {
		t.Errorf("path = %q, want a gg-context-* temp file", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := "op: merge\nsource: feature\ntarget: main\nconflicted:\na$(x) b.go\nc.go\n"
	if string(data) != want {
		t.Errorf("context file content = %q\nwant %q", data, want)
	}
}

// TestToolContextFileContentControlCharPath is the CRITICAL-finding
// regression: a conflicted path containing a newline (legal in a git tree)
// must not be written byte-exact — that would forge an extra line under
// "conflicted:". It must instead render C-quoted on a single line.
func TestToolContextFileContentControlCharPath(t *testing.T) {
	ctx := template.CmdCtx{
		Op: "merge", Source: "feature", Target: "main",
		ConflictedFiles: []string{"innocent.go\nFAKE", "c.go"},
	}
	path, err := toolContextFile(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(path)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := "op: merge\nsource: feature\ntarget: main\nconflicted:\n\"innocent.go\\nFAKE\"\nc.go\n"
	if string(data) != want {
		t.Errorf("context file content = %q\nwant %q", data, want)
	}
}

// TestToolContextFileContentControlCharSource guards the header values, not
// just the conflicted-paths list: Source for a cherry-pick/revert comes from
// a commit subject via git %s, which collapses an embedded \n to a space but
// leaves a raw \r untouched. An unquoted \r in the "source:" line would still
// be inconsistent with the file's control-character hardening, so it must
// come out C-quoted like a conflicted path would.
func TestToolContextFileContentControlCharSource(t *testing.T) {
	ctx := template.CmdCtx{
		Op: "cherry-pick", Source: "abc1234 fix\rthing", Target: "",
		ConflictedFiles: []string{"c.go"},
	}
	path, err := toolContextFile(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(path)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := "op: cherry-pick\nsource: \"abc1234 fix\\rthing\"\ntarget: \nconflicted:\nc.go\n"
	if string(data) != want {
		t.Errorf("context file content = %q\nwant %q", data, want)
	}
}

func TestCQuotePath(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain path unquoted", "a/b.go", "a/b.go"},
		{"shell metacharacters stay byte-exact", "a$(x) b.go", "a$(x) b.go"},
		{"newline forces C-quoting", "innocent.go\nFAKE", `"innocent.go\nFAKE"`},
		{"carriage return", "a\rb", `"a\rb"`},
		{"tab", "a\tb", `"a\tb"`},
		{"embedded quote and backslash", "a\"b\\c\n", `"a\"b\\c\n"`},
		{"other control byte uses octal", "a\x01b", `"a\001b"`},
		{"empty path unquoted", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := cQuotePath(c.in); got != c.want {
				t.Errorf("cQuotePath(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestToolScriptAndExecCmd(t *testing.T) {
	script, err := toolScript("echo hello")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(script)
	data, _ := os.ReadFile(script)
	if !strings.Contains(string(data), "echo hello") {
		t.Errorf("script content: %q", data)
	}
	if runtime.GOOS == "windows" {
		if !strings.HasSuffix(script, ".bat") {
			t.Errorf("windows script must be .bat: %s", script)
		}
	} else if !strings.HasSuffix(script, ".sh") {
		t.Errorf("posix script must be .sh: %s", script)
	}
	cmd := toolExecCmd(script, "/tmp", []string{"GG_OP=merge"})
	if cmd.Dir != "/tmp" {
		t.Errorf("Dir = %q", cmd.Dir)
	}
	joined := strings.Join(cmd.Env, "|")
	if !strings.Contains(joined, "GG_OP=merge") {
		t.Error("extra env missing")
	}
	if !strings.Contains(strings.Join(cmd.Args, " "), script) {
		t.Errorf("argv %v must reference the script", cmd.Args)
	}
}
