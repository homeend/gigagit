package tui

import (
	"os"
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
		File: "a.go", Local: "/t/l", Base: "/t/b", Remote: "/t/r", Merged: "/r/a.go"})
	sort.Strings(env)
	want := []string{
		"GG_BASE=/t/b", "GG_CONFLICTED_FILES=a.go b.go", "GG_FILE=a.go",
		"GG_LOCAL=/t/l", "GG_MERGED=/r/a.go", "GG_OP=merge", "GG_REMOTE=/t/r",
		"GG_REPO=/r", "GG_SOURCE=f", "GG_TARGET=main",
	}
	if strings.Join(env, "|") != strings.Join(want, "|") {
		t.Errorf("env = %v\nwant %v", env, want)
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
