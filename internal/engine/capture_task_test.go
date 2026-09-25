package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func hasEnvKey(env []string, key string) bool {
	for _, kv := range env {
		if strings.HasPrefix(kv, key+"=") {
			return true
		}
	}
	return false
}

func TestGenerateMessagePrepareCollect(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)
	stageFile(t, dir, repo, "a.txt", "one\n")
	op := GenerateMessage{Command: "true", Dir: dir, Env: []string{"GG_TASK=commit_message"}}
	in, err := op.Prepare(context.Background(), OpDeps{Repo: repo})
	if err != nil {
		t.Fatal(err)
	}
	env := envMap(in.Env)
	if env["GG_TASK"] != "commit_message" || env["GG_MESSAGE_FILE"] != in.MessageFile || in.MessageFile == "" {
		t.Fatalf("env = %v, MessageFile = %q", in.Env, in.MessageFile)
	}
	if hasEnvKey(in.Env, "PATH") {
		t.Fatalf("Prepare env must be additions only, got os.Environ entries: %v", in.Env)
	}
	if in.Command != "true" || in.Dir != dir {
		t.Fatalf("Command/Dir = %q/%q", in.Command, in.Dir)
	}
	// stdout is used while the file is empty; CRLF is normalised.
	res, _ := op.Collect(in, []byte("subj\r\n\r\nbody\r\n"))
	if res.Captured != "subj\n\nbody\n" {
		t.Fatalf("captured = %q", res.Captured)
	}
	// Non-empty file content wins over stdout.
	if err := os.WriteFile(in.MessageFile, []byte("from file\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, _ = op.Collect(in, []byte("stdout"))
	if res.Captured != "from file\n" {
		t.Fatalf("captured = %q, want the file content", res.Captured)
	}
	in.Cleanup()
	in.Cleanup() // idempotent
	for _, k := range []string{"GG_MESSAGE_FILE", "GG_CONTEXT_FILE", "GG_STAGED_DIFF"} {
		if _, err := os.Stat(env[k]); !os.IsNotExist(err) {
			t.Fatalf("%s not removed by Cleanup: %s", k, env[k])
		}
	}
}

func TestReviewChangesPrepareCollect(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)
	op := ReviewChanges{Command: "true", Dir: dir, RangeLabel: "working changes"}
	in, err := op.Prepare(context.Background(), OpDeps{Repo: repo})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Cleanup()
	env := envMap(in.Env)
	if env["GG_REVIEW_DIFF"] == "" || env["GG_MESSAGE_FILE"] != in.MessageFile {
		t.Fatalf("env = %v", in.Env)
	}
	if hasEnvKey(in.Env, "PATH") {
		t.Fatalf("Prepare env must be additions only: %v", in.Env)
	}
	res, _ := op.Collect(in, []byte("report"))
	if res.Captured != "report" {
		t.Fatalf("captured = %q", res.Captured)
	}
}

func TestConflictOpsPrepareResolveTheTemplate(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		op   CaptureTask
		task string
	}{
		{CompleteConflict{Command: "agent <context-file>", Dir: t.TempDir(), Op: "merge", ConflictedFiles: []string{"f.txt"}}, "conflict_complete"},
		{ConflictAgent{Command: "agent <context-file>", Dir: t.TempDir(), Op: "merge", ConflictedFiles: []string{"f.txt"}}, "conflict"},
	} {
		in, err := tc.op.Prepare(context.Background(), OpDeps{})
		if err != nil {
			t.Fatal(err)
		}
		env := envMap(in.Env)
		if env["GG_TASK"] != tc.task {
			t.Errorf("%T: GG_TASK = %q, want %q", tc.op, env["GG_TASK"], tc.task)
		}
		ctxFile := env["GG_CONTEXT_FILE"]
		if ctxFile == "" || !strings.Contains(in.Command, filepath.Base(ctxFile)) {
			t.Errorf("%T: command %q does not name the context file %q", tc.op, in.Command, ctxFile)
		}
		if in.MessageFile == "" || env["GG_MESSAGE_FILE"] != in.MessageFile {
			t.Errorf("%T: MessageFile %q / env %v", tc.op, in.MessageFile, in.Env)
		}
		in.Cleanup()
		if _, err := os.Stat(ctxFile); !os.IsNotExist(err) {
			t.Errorf("%T: context file survived Cleanup", tc.op)
		}
	}
}

func TestConflictOpPrepareCleansUpOnBadTemplate(t *testing.T) {
	t.Parallel()
	before, _ := filepath.Glob(filepath.Join(os.TempDir(), "gg-context-*.txt"))
	_, err := CompleteConflict{Command: "agent <bogus>", Dir: t.TempDir(), Op: "merge"}.Prepare(context.Background(), OpDeps{})
	if err == nil {
		t.Fatal("want a template error")
	}
	after, _ := filepath.Glob(filepath.Join(os.TempDir(), "gg-context-*.txt"))
	if len(after) > len(before) {
		t.Fatalf("Prepare leaked a context file on error: %d → %d", len(before), len(after))
	}
}

func TestConflictAgentRunCaptures(t *testing.T) {
	t.Parallel()
	fc := &fakeCapture{writeMsgFile: "summary"}
	res, err := ConflictAgent{Command: "agent", Dir: t.TempDir(), Op: "merge"}.Run(context.Background(), OpDeps{CaptureRunner: fc})
	if err != nil {
		t.Fatal(err)
	}
	if res.Captured != "summary" {
		t.Fatalf("captured = %q", res.Captured)
	}
	if envMap(fc.spec.Env)["PATH"] == "" {
		t.Fatalf("Run must hand the capture runner the full environment")
	}
}
