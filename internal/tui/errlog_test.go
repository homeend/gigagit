package tui

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultErrLogPathName(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	p := defaultErrLogPath()
	if p == "" {
		t.Fatal("expected a path with XDG_STATE_HOME set")
	}
	if filepath.Base(p) != "errors.log" {
		t.Fatalf("want basename errors.log, got %q", p)
	}
}

func TestOpenErrorLogCreatesAppendable(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	f, path, err := OpenErrorLog()
	if err != nil {
		t.Fatalf("OpenErrorLog: %v", err)
	}
	if f == nil {
		t.Fatal("expected a file handle with a state dir set")
	}
	defer f.Close()
	if _, err := f.WriteString("x\n"); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("errors.log not created: %v", err)
	}
}
