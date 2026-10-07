package domain

import (
	"context"
	"errors"
	"os"
	"runtime"
	"strings"
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
	// A multi-line ''' config command ends in a newline: the flag must
	// join its last line, not become a command of its own.
	multi := config.ToolCommand{Name: "Claude", Category: "review", Mode: "capture", Command: "claude -p x \\\n  --output-format json\n"}
	if got, _ := ResolveReviewCommand(multi, template.CmdCtx{Model: "haiku"}); got != "claude -p x \\\n  --output-format json --model 'haiku'" {
		t.Fatalf("multi-line: %q", got)
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

// A command the agent's flag cannot be appended to — something after the
// agent's own arguments would take it — is refused with a fix, not run on
// the default model; without a model it runs as before.
func TestResolveReviewCommandRefusesAnUnreachableFlag(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell splitting asserted")
	}
	for cmd, sep := range map[string]string{
		`claude -p x | tee /tmp/log`:    "|",
		`claude -p x # my review`:       "#",
		`claude -p x; echo done`:        ";",
		"claude -p x\necho done\n":      "a line break",
		`claude -p x && notify-send ok`: "&&",
	} {
		tc := config.ToolCommand{Name: "Mine", Category: "review", Mode: "capture", Command: cmd}
		_, err := ResolveReviewCommand(tc, template.CmdCtx{Model: "opus"})
		if !errors.Is(err, ErrNoModelSupport) || !strings.Contains(err.Error(), "after "+map[bool]string{true: sep, false: "`" + sep + "`"}[sep == "a line break"]) || !strings.Contains(err.Error(), "<model:--model>") {
			t.Errorf("%q: %v", cmd, err)
		}
		if _, err := ResolveReviewCommand(tc, template.CmdCtx{}); err != nil {
			t.Errorf("%q without a model: %v", cmd, err)
		}
	}
	// Junie's flag is "--model=", but the fix is <model:--model> too: a
	// "--model=<model>" would leave a bare --model= on every run without one.
	junie := config.ToolCommand{Name: "Junie", Category: "review", Mode: "capture", Command: `junie --task x | cat`}
	if _, err := ResolveReviewCommand(junie, template.CmdCtx{Model: "m"}); err == nil || !strings.Contains(err.Error(), "put <model:--model> ") {
		t.Errorf("junie: %v", err)
	}
}

// ReviewTakesModel answers only the model question: a tool broken for
// another reason (a token the review lane cannot fill) is not reported as
// unable to take a model — its run fails with its own error.
func TestReviewTakesModel(t *testing.T) {
	t.Parallel()
	for cmd, want := range map[string]bool{
		`claude -p x`:                 true,
		`claude -p x | cat`:           false,
		`printf x`:                    false,
		`claude -p <context-file> -x`: true,
	} {
		if got := ReviewTakesModel(config.ToolCommand{Name: "T", Category: "review", Command: cmd}); got != want {
			t.Errorf("%q: %v, want %v", cmd, got, want)
		}
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
