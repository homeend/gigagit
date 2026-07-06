package engine

import (
	"context"
	"os"
	"path/filepath"
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
	diffBody string
	ctxBody  string
}

func (f *fakeCapture) Capture(_ context.Context, s CaptureSpec, _ func(string)) ([]byte, error) {
	f.spec = s
	env := envMap(s.Env)
	f.diffPath, f.ctxPath = env["GG_STAGED_DIFF"], env["GG_CONTEXT_FILE"]
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

	// Temp files are removed once Run returns.
	if _, err := os.Stat(fc.diffPath); !os.IsNotExist(err) {
		t.Fatalf("diff temp file not removed: %s", fc.diffPath)
	}
	if _, err := os.Stat(fc.ctxPath); !os.IsNotExist(err) {
		t.Fatalf("context temp file not removed: %s", fc.ctxPath)
	}
}

func TestGenerateMessageOverCapTruncates(t *testing.T) {
	dir, repo := newRepo(t)
	// A staged change whose diff exceeds MaxDiffBytes.
	var b strings.Builder
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
}

func TestGenerateMessageLockModeRead(t *testing.T) {
	if (GenerateMessage{}).LockMode() != repogate.Read {
		t.Fatal("want Read")
	}
}
