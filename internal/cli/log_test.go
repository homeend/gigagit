package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLogDefault(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t)
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("x\n"), 0o644)
	gitIn(t, dir, "add", "a.txt")
	gitIn(t, dir, "commit", "-m", "second commit")

	code, out, errb := runCLI(t, dir, "log")
	if code != 0 {
		t.Fatalf("exit = %d, stderr=%s", code, errb)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 lines, got %d:\n%s", len(lines), out)
	}
	// newest first: "<short-sha> <subject>"
	if !strings.HasSuffix(lines[0], " second commit") {
		t.Fatalf("line 0 = %q", lines[0])
	}
	sha := strings.Fields(lines[0])[0]
	if len(sha) < 7 || len(sha) > 12 {
		t.Fatalf("sha token looks wrong: %q", sha)
	}
}

func TestLogCountFlag(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t)
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("x\n"), 0o644)
	gitIn(t, dir, "add", "a.txt")
	gitIn(t, dir, "commit", "-m", "second commit")

	code, out, _ := runCLI(t, dir, "log", "-n", "1")
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if got := strings.Count(out, "\n"); got != 1 {
		t.Fatalf("want exactly 1 line, got %d:\n%s", got, out)
	}
}

func TestLogRange(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t)
	gitIn(t, dir, "switch", "-c", "feat")
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("y\n"), 0o644)
	gitIn(t, dir, "add", "b.txt")
	gitIn(t, dir, "commit", "-m", "on feat")

	code, out, _ := runCLI(t, dir, "log", "main..HEAD")
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if strings.Count(out, "\n") != 1 || !strings.Contains(out, "on feat") {
		t.Fatalf("range output wrong:\n%s", out)
	}
}

func TestLogTooManyArgs(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t)
	code, _, _ := runCLI(t, dir, "log", "a", "b")
	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
}
