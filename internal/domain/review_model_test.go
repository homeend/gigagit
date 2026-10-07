package domain

import (
	"context"
	"errors"
	"os"
	"runtime"
	"testing"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/template"
)

func TestResolveReviewCommandModel(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX quoting asserted")
	}
	claude := config.ToolCommand{Name: "Claude", Category: "review", Mode: "capture", Command: `claude -p "review <range>" --output-format json`}
	got, err := ResolveReviewCommand(claude, template.CmdCtx{Range: "a..b", Model: "sonnet"})
	if err != nil || got != `claude -p "review a..b" --output-format json --model 'sonnet'` {
		t.Fatalf("claude: %q %v", got, err)
	}
	junie := config.ToolCommand{Name: "Junie", Category: "review", Mode: "capture", Command: `junie --task x`}
	if got, _ := ResolveReviewCommand(junie, template.CmdCtx{Model: "my model"}); got != `junie --task x --model='my model'` {
		t.Fatalf("junie: %q", got)
	}
	slot := config.ToolCommand{Name: "Mine", Category: "review", Mode: "capture", Command: `claude --model <model> -p x`}
	if got, _ := ResolveReviewCommand(slot, template.CmdCtx{Model: "opus"}); got != `claude --model 'opus' -p x` {
		t.Fatalf("slot wins over the flag: %q", got)
	}
	custom := config.ToolCommand{Name: "Echo", Category: "review", Mode: "capture", Command: `printf x`}
	if _, err := ResolveReviewCommand(custom, template.CmdCtx{Model: "m"}); !errors.Is(err, ErrNoModelSupport) {
		t.Fatalf("custom without <model>: %v", err)
	}
	if got, err := ResolveReviewCommand(custom, template.CmdCtx{}); err != nil || got != "printf x" {
		t.Fatalf("no model, no change: %q %v", got, err)
	}
}

func TestToolAgentID(t *testing.T) {
	t.Parallel()
	if ToolAgentID(config.ToolCommand{Command: `/usr/local/bin/codex exec x`}) != "codex" || ToolAgentID(config.ToolCommand{Command: `printf x`}) != "" {
		t.Fatal("ToolAgentID")
	}
}

// RunReview runs and parses the review and stores nothing.
func TestRunReviewStoresNothing(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh-based")
	}
	dir, svc := newRealRepo(t)
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	notes := t.TempDir()
	svc.UseNotesDir(notes)
	commitFile(t, dir, "a.txt", "one\n", "c1")
	commitFile(t, dir, "a.txt", "one\ntwo\n", "c2")
	target := ReviewTarget{Kind: ReviewRange, Range: "HEAD~1..HEAD", Diff: model.DiffSpec{Rev: "HEAD~1..HEAD"}}
	res, err := svc.RunReview(context.Background(), target, `printf '{"version":1,"summary":"ok","files":[]}' > "$GG_MESSAGE_FILE"`, nil)
	if err != nil || !res.Structured || res.NoteID != "" {
		t.Fatalf("res %+v err %v", res, err)
	}
	if ents, _ := os.ReadDir(notes); len(ents) != 0 {
		t.Fatalf("RunReview wrote the note store: %v", ents)
	}
}
