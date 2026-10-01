package sessionreg

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestWriteLiveRoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	r := Registry{PID: 1, Worktree: "/w", Sessions: []Entry{{ID: "p/s1", Dir: "/w2", Agent: "claude", State: "running"}}}
	if err := Write(dir, "p", r); err != nil {
		t.Fatal(err)
	}
	got := Live(dir)
	if len(got) != 1 || got[0].Sessions[0].ID != "p/s1" || got[0].Worktree != "/w" {
		t.Fatalf("Live = %+v", got)
	}
}

func TestLiveSweepsStaleAndGarbage(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := Write(dir, "old", Registry{PID: 2}); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-2 * LiveWindow)
	os.Chtimes(filepath.Join(dir, "old.json"), past, past)
	os.WriteFile(filepath.Join(dir, "bad.json"), []byte("{"), 0o644)
	if got := Live(dir); len(got) != 0 {
		t.Fatalf("Live = %+v, want none", got)
	}
	for _, n := range []string{"old.json", "bad.json"} {
		if _, err := os.Stat(filepath.Join(dir, n)); !os.IsNotExist(err) {
			t.Fatalf("%s not swept", n)
		}
	}
}

func TestTouchKeepsAlive(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	Write(dir, "p", Registry{PID: 3})
	past := time.Now().Add(-2 * LiveWindow)
	os.Chtimes(filepath.Join(dir, "p.json"), past, past)
	if err := Touch(dir, "p"); err != nil {
		t.Fatal(err)
	}
	if len(Live(dir)) != 1 {
		t.Fatal("touched registry must be live")
	}
}

func TestLiveMissingDir(t *testing.T) {
	t.Parallel()
	if got := Live(filepath.Join(t.TempDir(), "nope")); got != nil {
		t.Fatalf("Live = %+v", got)
	}
}

func TestLiveFillsProc(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	Write(dir, "123-456", Registry{PID: 123})
	if got := Live(dir); len(got) != 1 || got[0].Proc != "123-456" {
		t.Fatalf("Live = %+v", got)
	}
}

func TestPIDOfAndProcOf(t *testing.T) {
	t.Parallel()
	if PIDOf("4711-99/s3") != 4711 || ProcOf("4711-99/s3") != "4711-99" {
		t.Fatal("parse")
	}
	if PIDOf("p/s1") != 0 || PIDOf("") != 0 {
		t.Fatal("malformed must be 0")
	}
}

func TestProcAlive(t *testing.T) {
	t.Parallel()
	if !ProcAlive(os.Getpid()) {
		t.Fatal("self must be alive")
	}
	if ProcAlive(0) || ProcAlive(-1) {
		t.Fatal("non-positive pid must be dead")
	}
	c := exec.Command(os.Args[0], "-test.run=^$") // exits at once
	if err := c.Run(); err != nil {
		t.Fatal(err)
	}
	if ProcAlive(c.Process.Pid) {
		t.Skip("pid reused already — cannot assert on this machine")
	}
}
