package app

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/gittest"
)

func newRepo(t *testing.T) string {
	t.Helper()
	return gittest.BasicRepo(t, "hi\n")
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
