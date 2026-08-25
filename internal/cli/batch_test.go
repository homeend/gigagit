package cli

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runCLIStdin is runCLI with a caller-supplied stdin script.
func runCLIStdin(t *testing.T, workdir, in string, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := Run(workdir, args, strings.NewReader(in), &out, &errb, "")
	return code, out.String(), errb.String()
}

func TestPrefixWriter(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	p := &prefixWriter{w: &buf, prefix: "! "}
	p.Write([]byte("one\ntw"))
	p.Write([]byte("o\nthree\n"))
	want := "! one\n! two\n! three\n"
	if buf.String() != want {
		t.Fatalf("got %q, want %q", buf.String(), want)
	}
}

func TestBatchTwoReads(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t)
	code, out, _ := runCLIStdin(t, dir, "status\nlog -n 1\n", "batch")
	if code != 0 {
		t.Fatalf("exit=%d\n%s", code, out)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if lines[0] != "#1 ok status" {
		t.Fatalf("first header = %q", lines[0])
	}
	if !strings.Contains(out, "\n#2 ok log -n 1\n") {
		t.Fatalf("missing second header:\n%s", out)
	}
	if lines[len(lines)-1] != "#done 2 ok" {
		t.Fatalf("trailer = %q", lines[len(lines)-1])
	}
	if !strings.Contains(out, "on branch main") {
		t.Fatalf("status body missing:\n%s", out)
	}
}

func TestBatchStopsOnFailure(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t)
	code, out, _ := runCLIStdin(t, dir, "bogus-cmd\nstatus\n", "batch")
	if code != 1 {
		t.Fatalf("exit=%d, want 1", code)
	}
	if !strings.Contains(out, "#1 !2 bogus-cmd\n") {
		t.Fatalf("failure header missing:\n%s", out)
	}
	if !strings.Contains(out, "! unknown command") {
		t.Fatalf("prefixed stderr missing:\n%s", out)
	}
	if strings.Contains(out, "#2") {
		t.Fatalf("second command ran despite stop-on-error:\n%s", out)
	}
	if !strings.HasSuffix(out, "#done 0 ok, 1 failed (stopped)\n") {
		t.Fatalf("trailer wrong:\n%s", out)
	}
}

func TestBatchKeepGoing(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t)
	code, out, _ := runCLIStdin(t, dir, "bogus-cmd\nstatus\n", "batch", "--keep-going")
	if code != 1 {
		t.Fatalf("exit=%d, want 1", code)
	}
	if !strings.Contains(out, "#2 ok status\n") {
		t.Fatalf("second command did not run:\n%s", out)
	}
	if !strings.HasSuffix(out, "#done 1 ok, 1 failed\n") {
		t.Fatalf("trailer wrong:\n%s", out)
	}
}

func TestBatchSkipsCommentsBlanksAndStripsGG(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t)
	script := "# a comment\n\n  \ngg status\n"
	code, out, _ := runCLIStdin(t, dir, script, "batch")
	if code != 0 {
		t.Fatalf("exit=%d\n%s", code, out)
	}
	if !strings.HasPrefix(out, "#1 ok status\n") {
		t.Fatalf("gg prefix not stripped or comment counted:\n%s", out)
	}
	if !strings.HasSuffix(out, "#done 1 ok\n") {
		t.Fatalf("trailer wrong:\n%s", out)
	}
}

func TestBatchQuotedWriteFlow(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t)
	os.WriteFile(filepath.Join(dir, "new.txt"), []byte("x\n"), 0o644)
	script := "add new.txt\ncommit -m \"two words\"\nstatus\n"
	code, out, _ := runCLIStdin(t, dir, script, "batch")
	if code != 0 {
		t.Fatalf("exit=%d\n%s", code, out)
	}
	if !strings.Contains(out, "✓ committed") || !strings.Contains(out, "two words") {
		t.Fatalf("commit section wrong:\n%s", out)
	}
	if !strings.Contains(out, "working tree clean") {
		t.Fatalf("status after commit not clean:\n%s", out)
	}
}

func TestBatchNestedRejected(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t)
	code, out, _ := runCLIStdin(t, dir, "batch\n", "batch")
	if code != 1 {
		t.Fatalf("exit=%d, want 1", code)
	}
	if !strings.Contains(out, "#1 !2 batch\n") || !strings.Contains(out, "! batch: nested batch is not allowed") {
		t.Fatalf("nested batch not rejected properly:\n%s", out)
	}
}

func TestBatchUnterminatedQuoteIsUsageError(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t)
	code, out, errb := runCLIStdin(t, dir, "commit -m \"oops\n", "batch")
	if code != 2 {
		t.Fatalf("exit=%d, want 2 (stderr=%s)", code, errb)
	}
	if strings.Contains(out, "#1") {
		t.Fatalf("nothing should be framed on a tokenizer error:\n%s", out)
	}
}

func TestBatchEmptyScript(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t)
	code, out, _ := runCLIStdin(t, dir, "", "batch")
	if code != 0 || out != "#done 0 ok\n" {
		t.Fatalf("exit=%d out=%q", code, out)
	}
}

func TestBatchRejectsPositionalArgs(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t)
	code, _, _ := runCLIStdin(t, dir, "", "batch", "status")
	if code != 2 {
		t.Fatalf("exit=%d, want 2", code)
	}
}

// TestBatchPullNeverBlocks pins the structural contract behind the cmdPull
// stdin fix: inside a batch script, "pull" must fail loud rather than hang.
// The real regression (cmdPull reading the terminal's real os.Stdin instead
// of the caller's reader) can only be observed on a TTY, which this harness
// doesn't have; what's pinned here is that a batch line running "pull" in a
// repo with no upstream terminates with a clear, correctly-framed failure
// instead of blocking forever.
func TestBatchPullNeverBlocks(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t)
	code, out, _ := runCLIStdin(t, dir, "pull\n", "batch")
	if code != 1 {
		t.Fatalf("exit=%d, want 1", code)
	}
	if !strings.Contains(out, "#1 !1 pull") {
		t.Fatalf("pull section missing/misframed:\n%s", out)
	}
}

func TestBatchSectionNewlineGuard(t *testing.T) {
	t.Parallel()
	// Simulate the cmdBatch copy path with a non-terminated section.
	var out bytes.Buffer
	section := bytes.NewBufferString("no trailing newline")
	needsNL := section.Len() > 0 && section.Bytes()[section.Len()-1] != '\n'
	io.Copy(&out, section)
	if needsNL {
		io.WriteString(&out, "\n")
	}
	if got := out.String(); got != "no trailing newline\n" {
		t.Fatalf("got %q", got)
	}
}
