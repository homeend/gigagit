package gitexec

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// fakeBinary writes an executable named name into a fresh dir and returns the dir.
func fakeBinary(t *testing.T, name string) string {
	t.Helper()
	dir := t.TempDir()
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestResolveBinaryFindsOnPath(t *testing.T) {
	want, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not on PATH")
	}
	if got := resolveBinary("git"); got != want {
		t.Fatalf("resolveBinary(git) = %q, want %q", got, want)
	}
}

// A cached lookup must not outlive the PATH it was made under.
func TestResolveBinaryFollowsPathChanges(t *testing.T) {
	a, b := fakeBinary(t, "gg-fake-tool"), fakeBinary(t, "gg-fake-tool")
	t.Setenv("PATH", a)
	first := resolveBinary("gg-fake-tool")
	if filepath.Dir(first) != a {
		t.Fatalf("under PATH=a: got %q, want a file in %q", first, a)
	}
	t.Setenv("PATH", b)
	if got := resolveBinary("gg-fake-tool"); filepath.Dir(got) != b {
		t.Fatalf("under PATH=b: got %q, want a file in %q", got, b)
	}
}

// A name that is not found (or already a path) passes through unchanged, so
// exec reports the same error it always did.
func TestResolveBinaryPassesThrough(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if got := resolveBinary("gg-no-such-tool"); got != "gg-no-such-tool" {
		t.Fatalf("missing tool: got %q, want the bare name", got)
	}
	p := filepath.Join(t.TempDir(), "git")
	if got := resolveBinary(p); got != p {
		t.Fatalf("explicit path: got %q, want %q", got, p)
	}
}
