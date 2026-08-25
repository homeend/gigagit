package engine

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/repogate"
	"github.com/homeend/gigagit/internal/template"
)

// Test 1: env + context file. The fake records the CaptureSpec; assert env
// values and that the context file's bytes match template.ConflictContextDoc.
func TestCompleteConflictEnvAndContextFile(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)

	fc := &fakeCapture{stdout: "overview"}
	op := CompleteConflict{
		Command:         "true",
		Dir:             dir,
		Op:              "merge",
		Source:          "feat/x",
		Target:          "main",
		ConflictedFiles: []string{"a.txt", "b.txt"},
	}
	_, err := op.Run(context.Background(), OpDeps{Repo: repo, CaptureRunner: fc})
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	env := envMap(fc.spec.Env)
	want := map[string]string{
		"GG_OP":               "merge",
		"GG_SOURCE":           "feat/x",
		"GG_TARGET":           "main",
		"GG_CONFLICTED_FILES": "a.txt b.txt",
		"GG_REPO":             dir,
		"GG_TASK":             "conflict_complete",
		"GG_FILE":             "",
		"GG_LOCAL":            "",
		"GG_BASE":             "",
		"GG_REMOTE":           "",
		"GG_MERGED":           "",
	}
	for k, v := range want {
		if got, ok := env[k]; !ok || got != v {
			t.Errorf("env[%s]=%q (present=%v), want %q", k, got, ok, v)
		}
	}
	if env["GG_CONTEXT_FILE"] == "" {
		t.Errorf("GG_CONTEXT_FILE must be non-empty")
	}
	if env["GG_MESSAGE_FILE"] == "" {
		t.Errorf("GG_MESSAGE_FILE must be non-empty")
	}

	wantCtx := template.ConflictContextDoc("merge", "feat/x", "main", []string{"a.txt", "b.txt"})
	if fc.ctxBody != wantCtx {
		t.Fatalf("context file body=%q, want %q", fc.ctxBody, wantCtx)
	}
}

// Test 2: file wins over stdout.
func TestCompleteConflictPrefersMessageFile(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)

	fc := &fakeCapture{stdout: "stdout noise", writeMsgFile: "file overview"}
	res, err := CompleteConflict{Command: "true", Dir: dir}.Run(context.Background(),
		OpDeps{Repo: repo, CaptureRunner: fc})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Captured != "file overview" {
		t.Fatalf("captured=%q, want %q", res.Captured, "file overview")
	}
}

// Test 3: stdout fallback when the file is left empty.
func TestCompleteConflictFallsBackToStdout(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)

	fc := &fakeCapture{stdout: "from stdout"}
	res, err := CompleteConflict{Command: "true", Dir: dir}.Run(context.Background(),
		OpDeps{Repo: repo, CaptureRunner: fc})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Captured != "from stdout" {
		t.Fatalf("captured=%q, want %q", res.Captured, "from stdout")
	}
}

// Test 4: both empty is OK, no error.
func TestCompleteConflictBothEmptyOK(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)

	fc := &fakeCapture{stdout: ""}
	res, err := CompleteConflict{Command: "true", Dir: dir}.Run(context.Background(),
		OpDeps{Repo: repo, CaptureRunner: fc})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Captured != "" {
		t.Fatalf("captured=%q, want empty", res.Captured)
	}
}

// Test 5: runner error propagates, along with whatever was captured.
func TestCompleteConflictRunnerError(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)

	wantErr := errors.New("boom")
	fc := &fakeCapture{stdout: "partial", err: wantErr}
	res, err := CompleteConflict{Command: "true", Dir: dir}.Run(context.Background(),
		OpDeps{Repo: repo, CaptureRunner: fc})
	if !errors.Is(err, wantErr) {
		t.Fatalf("err=%v, want %v", err, wantErr)
	}
	if res.Captured != "partial" {
		t.Fatalf("captured=%q, want %q", res.Captured, "partial")
	}
}

// Test 6: a resolve error runs nothing — the fake runner must never be invoked.
func TestCompleteConflictResolveErrorRunsNothing(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)

	fc := &fakeCapture{stdout: "should not run"}
	_, err := CompleteConflict{Command: "x <nosuchtoken>", Dir: dir}.Run(context.Background(),
		OpDeps{Repo: repo, CaptureRunner: fc})
	if err == nil {
		t.Fatalf("expected a resolve error, got nil")
	}
	if fc.spec.Command != "" {
		t.Fatalf("fake runner was invoked with command %q, want never invoked", fc.spec.Command)
	}
}

// Test 7: the context and message temp files are removed once Run returns.
func TestCompleteConflictTempFilesRemoved(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)

	fc := &fakeCapture{stdout: "overview"}
	_, err := CompleteConflict{Command: "true", Dir: dir}.Run(context.Background(),
		OpDeps{Repo: repo, CaptureRunner: fc})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if fc.ctxPath == "" || fc.msgPath == "" {
		t.Fatalf("fake did not capture temp paths: ctx=%q msg=%q", fc.ctxPath, fc.msgPath)
	}
	if _, err := os.Stat(fc.ctxPath); !os.IsNotExist(err) {
		t.Fatalf("context temp file not removed: %s", fc.ctxPath)
	}
	if _, err := os.Stat(fc.msgPath); !os.IsNotExist(err) {
		t.Fatalf("message temp file not removed: %s", fc.msgPath)
	}
}

// Test 8: LockMode is Read.
func TestCompleteConflictLockModeRead(t *testing.T) {
	t.Parallel()
	if (CompleteConflict{}).LockMode() != repogate.Read {
		t.Fatal("want Read")
	}
}

// Test 9: the op resolves op.Command itself, so a custom <context-file>
// token sees the REAL temp path — unlike ReviewChanges, which is handed an
// already-resolved command.
func TestCompleteConflictResolvesContextFileToken(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)

	fc := &fakeCapture{stdout: "overview"}
	_, err := CompleteConflict{Command: "cat <context-file>", Dir: dir}.Run(context.Background(),
		OpDeps{Repo: repo, CaptureRunner: fc})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if strings.Contains(fc.spec.Command, "<context-file>") {
		t.Fatalf("command still contains the unresolved token: %q", fc.spec.Command)
	}
	if fc.ctxPath == "" {
		t.Fatalf("fake never captured GG_CONTEXT_FILE")
	}
	if !strings.Contains(fc.spec.Command, fc.ctxPath) {
		t.Fatalf("resolved command %q does not reference the real context file path %q", fc.spec.Command, fc.ctxPath)
	}
}
