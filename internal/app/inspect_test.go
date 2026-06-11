package app

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func newRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-b", "main")
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("hi\n"), 0o644)
	run("add", ".")
	run("commit", "-m", "initial")
	return dir
}

func TestInspectPrintsSummary(t *testing.T) {
	dir := newRepo(t)
	var out bytes.Buffer
	opts := Options{WorkDir: dir, Stdout: &out}
	if err := Inspect(context.Background(), opts); err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	s := out.String()
	if !strings.Contains(s, "branch: main") {
		t.Fatalf("output missing branch line:\n%s", s)
	}
	if !strings.Contains(s, "worktrees: 1") {
		t.Fatalf("output missing worktree count:\n%s", s)
	}
}

func TestInspectWritesDebugDump(t *testing.T) {
	dir := newRepo(t)
	dumpPath := filepath.Join(t.TempDir(), "dump.json")
	var out bytes.Buffer
	opts := Options{WorkDir: dir, Stdout: &out, DumpPath: dumpPath}
	if err := Inspect(context.Background(), opts); err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if _, err := os.Stat(dumpPath); err != nil {
		t.Fatalf("debug dump not written: %v", err)
	}
}
