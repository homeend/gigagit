package engine

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/repogate"
)

// fakeCapture is a hand-written CaptureRunner fake (per the Task-2
// CaptureRunner interface) that records the spec it was invoked with and
// snapshots the GG_STAGED_DIFF/GG_CONTEXT_FILE temp files' CONTENT at
// invocation time — before GenerateMessage's deferred os.Remove cleans them
// up on return — so assertions on file content can run after Run() returns.
type fakeCapture struct {
	spec   CaptureSpec
	stdout string
	err    error

	sawEnv   bool
	diffPath string
	ctxPath  string
	msgPath  string
	diffBody string
	ctxBody  string
}

func (f *fakeCapture) Capture(_ context.Context, s CaptureSpec, _ func(string)) ([]byte, error) {
	f.spec = s
	env := envMap(s.Env)
	f.diffPath, f.ctxPath, f.msgPath = env["GG_STAGED_DIFF"], env["GG_CONTEXT_FILE"], env["GG_MESSAGE_FILE"]
	f.sawEnv = f.diffPath != "" && f.ctxPath != ""
	if b, err := os.ReadFile(f.diffPath); err == nil {
		f.diffBody = string(b)
	}
	if b, err := os.ReadFile(f.ctxPath); err == nil {
		f.ctxBody = string(b)
	}
	return []byte(f.stdout), f.err
}

// envMap turns a []"K=V" env slice (as passed to CaptureSpec.Env) into a map
// for easy lookup in assertions.
func envMap(env []string) map[string]string {
	m := make(map[string]string, len(env))
	for _, kv := range env {
		if i := strings.IndexByte(kv, '='); i >= 0 {
			m[kv[:i]] = kv[i+1:]
		}
	}
	return m
}

func stageFile(t *testing.T, dir string, repo *git.Repo, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := repo.StagePaths(context.Background(), []string{name}); err != nil {
		t.Fatal(err)
	}
}

func TestGenerateMessageBuildsContextAndCaptures(t *testing.T) {
	dir, repo := newRepo(t)
	ctx := context.Background()
	stageFile(t, dir, repo, "a.txt", "one\ntwo\n")

	fc := &fakeCapture{stdout: `{"result":"Subject line\n\nBody text."}`}
	res, err := GenerateMessage{Command: "true", Dir: dir}.Run(ctx,
		OpDeps{Repo: repo, CaptureRunner: fc})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Captured != fc.stdout {
		t.Fatalf("captured=%q, want %q", res.Captured, fc.stdout)
	}

	if !fc.sawEnv {
		t.Fatalf("runner did not receive GG_STAGED_DIFF/GG_CONTEXT_FILE: %v", fc.spec.Env)
	}
	// The diff file holds the staged patch; the context file's stat block
	// names the same file.
	if !strings.Contains(fc.diffBody, "a.txt") {
		t.Fatalf("diff file does not name staged file a.txt:\n%s", fc.diffBody)
	}
	if !strings.Contains(fc.ctxBody, "a.txt") {
		t.Fatalf("context file does not mention a.txt:\n%s", fc.ctxBody)
	}
	if !strings.Contains(fc.ctxBody, fc.diffPath) {
		t.Fatalf("summary does not reference diff path %s:\n%s", fc.diffPath, fc.ctxBody)
	}

	// The empty output file is provisioned in the env for a task-agent to write.
	if fc.msgPath == "" {
		t.Fatalf("runner did not receive GG_MESSAGE_FILE: %v", fc.spec.Env)
	}

	// Temp files are removed once Run returns.
	if _, err := os.Stat(fc.diffPath); !os.IsNotExist(err) {
		t.Fatalf("diff temp file not removed: %s", fc.diffPath)
	}
	if _, err := os.Stat(fc.ctxPath); !os.IsNotExist(err) {
		t.Fatalf("context temp file not removed: %s", fc.ctxPath)
	}
	if _, err := os.Stat(fc.msgPath); !os.IsNotExist(err) {
		t.Fatalf("message temp file not removed: %s", fc.msgPath)
	}
}

func TestGenerateMessageOverCapTruncates(t *testing.T) {
	dir, repo := newRepo(t)
	// A staged change whose diff exceeds MaxDiffBytes; the marker line lets
	// us assert the ORIGINAL diff body is fully REPLACED by the truncation
	// note, not merely appended to.
	const marker = "UNIQUE_MARKER_LINE_ZZZ_NOT_ELSEWHERE"
	var b strings.Builder
	b.WriteString(marker + "\n")
	for b.Len() < MaxDiffBytes+1024 {
		b.WriteString("filler line filler line filler line filler line\n")
	}
	stageFile(t, dir, repo, "big.txt", b.String())

	fc := &fakeCapture{stdout: "Subject\n\nBody."}
	if _, err := (GenerateMessage{Command: "true", Dir: dir}).Run(context.Background(),
		OpDeps{Repo: repo, CaptureRunner: fc}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(strings.ToLower(fc.diffBody), "truncat") {
		t.Fatalf("expected truncation note in diff file, got:\n%s", fc.diffBody)
	}
	if !strings.Contains(fc.ctxBody, "big.txt") {
		t.Fatalf("expected stat block to still name big.txt in context file:\n%s", fc.ctxBody)
	}
	if strings.Contains(fc.diffBody, marker) {
		t.Fatalf("expected the original diff body to be replaced by the truncation note (not appended to), but the marker line survived:\n%s", fc.diffBody)
	}
}

func TestGenerateMessageEmptyStagedNoError(t *testing.T) {
	dir, repo := newRepo(t)

	fc := &fakeCapture{stdout: "Subject\n\nBody."}
	res, err := GenerateMessage{Command: "true", Dir: dir}.Run(context.Background(),
		OpDeps{Repo: repo, CaptureRunner: fc})
	if err != nil {
		t.Fatalf("run with no staged changes: %v", err)
	}
	if res.Captured != fc.stdout {
		t.Fatalf("captured=%q", res.Captured)
	}
	if !strings.Contains(fc.ctxBody, "(no staged changes)") {
		t.Fatalf("expected context file to report no staged changes, got:\n%s", fc.ctxBody)
	}
}

// TestGenerateMessageMultiFileStatNewlineDelimited pins the fix for the
// NUL-delimited `git diff --numstat -z` output: staging TWO files must
// produce a "Files changed" block with one record per LINE (not one
// NUL-laced run-on line), and the context file must never carry a raw NUL
// byte.
func TestGenerateMessageMultiFileStatNewlineDelimited(t *testing.T) {
	dir, repo := newRepo(t)
	stageFile(t, dir, repo, "a.txt", "one\ntwo\n")
	stageFile(t, dir, repo, "b.txt", "three\nfour\n")

	fc := &fakeCapture{stdout: "Subject\n\nBody."}
	if _, err := (GenerateMessage{Command: "true", Dir: dir}).Run(context.Background(),
		OpDeps{Repo: repo, CaptureRunner: fc}); err != nil {
		t.Fatalf("run: %v", err)
	}

	if strings.ContainsRune(fc.ctxBody, 0) {
		t.Fatalf("context file must not contain a raw NUL byte:\n%q", fc.ctxBody)
	}

	var aLine, bLine = -1, -1
	for i, line := range strings.Split(fc.ctxBody, "\n") {
		if strings.Contains(line, "a.txt") {
			aLine = i
		}
		if strings.Contains(line, "b.txt") {
			bLine = i
		}
	}
	if aLine == -1 || bLine == -1 {
		t.Fatalf("expected both a.txt and b.txt named in context file:\n%s", fc.ctxBody)
	}
	if aLine == bLine {
		t.Fatalf("a.txt and b.txt run together on one line — stat NUL separators were not converted to newlines:\n%q", fc.ctxBody)
	}
}

// TestGenerateMessagePrefersMessageFile exercises the output-channel contract
// end-to-end with the REAL ShellCaptureRunner (not the fake): a task-agent tool
// that writes the message to $GG_MESSAGE_FILE — while printing an unrelated
// report to stdout, as Junie does — has the FILE content returned as
// Result.Captured, not the stdout. This is the whole point of the file channel.
func TestGenerateMessagePrefersMessageFile(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	if runtime.GOOS == "windows" {
		t.Skip("uses sh/printf")
	}
	dir, repo := newRepo(t)
	stageFile(t, dir, repo, "a.txt", "one\ntwo\n")

	// The script's stdout must be ignored; the file content must win.
	cmd := `printf 'this stdout is a work report, not the message\n'; ` +
		`printf 'File subject\n\nFile body from the agent.\n' > "$GG_MESSAGE_FILE"`
	res, err := GenerateMessage{Command: cmd, Dir: dir}.Run(context.Background(),
		OpDeps{Repo: repo, CaptureRunner: ShellCaptureRunner{}})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	const want = "File subject\n\nFile body from the agent.\n"
	if res.Captured != want {
		t.Fatalf("captured=%q, want the message-file content %q (file must win over stdout)", res.Captured, want)
	}
}

// TestGenerateMessageFallsBackToStdoutWhenFileEmpty pins the other branch: a
// stdout tool (e.g. Claude, whose --output-format json .result IS the message)
// leaves $GG_MESSAGE_FILE untouched, so Result.Captured is its stdout verbatim
// — the pre-existing behavior must not regress now that a file is provided.
func TestGenerateMessageFallsBackToStdoutWhenFileEmpty(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	if runtime.GOOS == "windows" {
		t.Skip("uses sh/printf")
	}
	dir, repo := newRepo(t)
	stageFile(t, dir, repo, "a.txt", "one\ntwo\n")

	// Writes nothing to $GG_MESSAGE_FILE; only stdout carries the message.
	res, err := GenerateMessage{Command: `printf 'Stdout subject\n\nStdout body.\n'`, Dir: dir}.Run(
		context.Background(), OpDeps{Repo: repo, CaptureRunner: ShellCaptureRunner{}})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	const want = "Stdout subject\n\nStdout body.\n"
	if res.Captured != want {
		t.Fatalf("captured=%q, want stdout %q (empty message file must fall back to stdout)", res.Captured, want)
	}
}

func TestGenerateMessageLockModeRead(t *testing.T) {
	if (GenerateMessage{}).LockMode() != repogate.Read {
		t.Fatal("want Read")
	}
}
