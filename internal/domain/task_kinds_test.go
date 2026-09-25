package domain

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/exttool"
	"github.com/homeend/gigagit/internal/model"
)

func TestTaskKeys(t *testing.T) {
	t.Parallel()
	const head = "0123456789abcdef0123456789abcdef01234567"
	if got := CommitMessageKey("/w/repo.worktrees/feat-x", head); got != "commit message — feat-x @ 0123456" {
		t.Errorf("commit key = %q", got)
	}
	if got := CommitMessageKey(`C:\w\feat-x`, head); got != "commit message — feat-x @ 0123456" {
		t.Errorf("windows commit key = %q", got)
	}
	r := ReviewTarget{Range: "aaaaaaaaaaaa..bbbbbbbbbbbb", Label: "feat"}
	if got := ReviewKey("/w/main", r); got != "review — aaaaaaa..bbbbbbb" {
		t.Errorf("review key = %q", got)
	}
	if got := ReviewKey("/w/main", WorkingReviewTarget()); got != "review — main working changes" {
		t.Errorf("working review key = %q", got)
	}
	if got := ConflictKey("/w/main", "rebase", head); got != "resolve conflict — main rebase 0123456" {
		t.Errorf("conflict key = %q", got)
	}
	if got := CompleteKey("/w/main", "merge", head); got != "resolve & complete — main merge 0123456" {
		t.Errorf("complete key = %q", got)
	}
}

func TestCommitMessageTaskSpec(t *testing.T) {
	t.Parallel()
	dir, svc := newRealRepo(t)
	tc := config.ToolCommand{Category: "commit_message", Name: "Claude (interactive)", Mode: "interactive", Command: "claude write <repo>"}
	spec, err := svc.CommitMessageTask(context.Background(), tc)
	if err != nil {
		t.Fatal(err)
	}
	if spec.Mode != TaskInteractive || spec.Kind != exttool.CatCommitMessage || spec.Agent != "Claude Code" || spec.AgentID != "claude" {
		t.Fatalf("spec = %+v", spec)
	}
	if spec.Key != CommitMessageKey(dir, headHash(t, dir)) || spec.Svc != svc || spec.Parse == nil || spec.ResultOptional {
		t.Fatalf("spec = %+v", spec)
	}
	op, ok := spec.Op.(engine.GenerateMessage)
	if !ok || !strings.Contains(op.Command, filepath.Base(dir)) || strings.Contains(op.Command, "<repo>") {
		t.Fatalf("op = %#v — want the command resolved against the worktree", spec.Op)
	}
	if _, err := svc.CommitMessageTask(context.Background(), config.ToolCommand{Category: "conflict", Name: "Meld", Mode: "terminal", PerFile: true, Command: "meld"}); err == nil {
		t.Fatal("a mergetool is not an AI task")
	}
}

func TestParseCommitMessageResult(t *testing.T) {
	t.Parallel()
	got, err := parseCommitMessage("Subject\n\nBody line\n")
	if err != nil || got != "Subject\n\nBody line" {
		t.Fatalf("got %q, %v", got, err)
	}
	if _, err := parseCommitMessage("  \n"); err == nil {
		t.Fatal("an empty message must be an error")
	}
}

func TestReviewTaskSpec(t *testing.T) {
	t.Parallel()
	dir, svc := newRealRepo(t)
	tc := config.ToolCommand{Category: "review", Name: "Claude", Mode: "capture", Command: "claude -p /code-review <range>"}
	target := ReviewTarget{Kind: ReviewRange, Range: "aaaaaaaaaaaa..bbbbbbbbbbbb", Label: "x", Diff: model.DiffSpec{Rev: "HEAD"}}
	spec, err := svc.ReviewTask(context.Background(), tc, target, "")
	if err != nil {
		t.Fatal(err)
	}
	op := spec.Op.(engine.ReviewChanges)
	if spec.Mode != TaskHeadless || !strings.Contains(op.Command, "aaaaaaaaaaaa..bbbbbbbbbbbb") || op.Dir != dir {
		t.Fatalf("spec = %+v op = %+v", spec, op)
	}
	if got, err := spec.Parse(`{"result":"## Findings"}`); err != nil || got != "## Findings" {
		t.Fatalf("Parse = %q, %v", got, err)
	}
	if _, err := spec.Parse("   "); err == nil {
		t.Fatal("an empty review must be an error")
	}
}

func TestConflictTaskSpec(t *testing.T) {
	t.Parallel()
	dir, svc := conflictedMergeRepo(t)
	tc := config.ToolCommand{Category: "conflict_complete", Name: "Claude — resolve & complete (yolo)", Mode: "terminal", Command: "claude go"}
	spec, err := svc.ConflictTask(context.Background(), tc, true)
	if err != nil {
		t.Fatal(err)
	}
	op, ok := spec.Op.(engine.CompleteConflict)
	if !ok || op.Op != "merge" || len(op.ConflictedFiles) != 1 || op.ConflictedFiles[0] != "f.txt" {
		t.Fatalf("op = %#v", spec.Op)
	}
	if spec.Key != CompleteKey(dir, "merge", headHash(t, dir)) || !spec.ResultOptional || spec.Mode != TaskInteractive {
		t.Fatalf("spec = %+v", spec)
	}
	spec, err = svc.ConflictTask(context.Background(), config.ToolCommand{Category: "conflict", Name: "Kimi", Mode: "capture", Command: "kimi -p x"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := spec.Op.(engine.ConflictAgent); !ok || spec.Key != ConflictKey(dir, "merge", headHash(t, dir)) {
		t.Fatalf("resolve spec = %+v", spec)
	}
	_, clean := newRealRepo(t)
	if _, err := clean.ConflictTask(context.Background(), tc, true); err == nil {
		t.Fatal("no paused operation must refuse")
	}
}

func TestPrepareTaskUnderTheGate(t *testing.T) {
	t.Parallel()
	dir, svc := newRealRepo(t)
	in, err := svc.PrepareTask(context.Background(), engine.GenerateMessage{Command: "true", Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Cleanup()
	if _, err := os.Stat(in.MessageFile); err != nil {
		t.Fatalf("message file: %v", err)
	}
}
