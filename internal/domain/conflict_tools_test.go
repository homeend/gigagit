package domain

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/template"
)

// A conflict tool run in a session: the resolved command runs from its
// wrapper script, in the worktree, with the GG_* environment, and the session
// ends when the tool does.
func TestStartToolSessionRunsTheScript(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh-based")
	}
	useSessions(t)
	main, svc := newRealRepo(t)
	out := filepath.Join(t.TempDir(), "out.txt")
	env := ToolEnv(template.CmdCtx{Op: "merge", Repo: main, File: "f.txt"})
	script, err := WriteToolScript(`printf '%s %s' "$GG_OP" "$GG_FILE" > ` + out)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(script)
	s, err := svc.StartToolSession(context.Background(), "Meld", script, main, "", 80, 24, env)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-s.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("the tool session never ended")
	}
	b, err := os.ReadFile(out)
	if err != nil || string(b) != "merge f.txt" {
		t.Fatalf("the script wrote %q (%v), want the GG_* values", b, err)
	}
	if in := s.Info(); in.Label != "Meld" {
		t.Errorf("label = %q, want the tool's name", in.Label)
	}
}

// The tools picker's filters, shared by both frontends: when_op narrows, a
// per-file command needs a focused both-sides conflict, a resolve & complete
// command needs a paused operation.
func TestConflictToolChoices(t *testing.T) {
	t.Parallel()
	cmds := []config.ToolCommand{
		{Name: "Any"}, {Name: "Rebase", WhenOp: "rebase"}, {Name: "Meld", PerFile: true},
	}
	names := func(cs []config.ToolCommand) string {
		var n []string
		for _, c := range cs {
			n = append(n, c.Name)
		}
		return strings.Join(n, ",")
	}
	if got := names(ConflictToolChoices(cmds, "merge", false)); got != "Any" {
		t.Errorf("no focused file = %q, want Any", got)
	}
	if got := names(ConflictToolChoices(cmds, "merge", true)); got != "Any,Meld" {
		t.Errorf("a both-sides file = %q, want Any,Meld", got)
	}
	if got := names(CompleteToolChoices(cmds, "")); got != "" {
		t.Errorf("nothing paused = %q, want none", got)
	}
	if got := names(CompleteToolChoices(cmds, "rebase")); got != "Any,Rebase,Meld" {
		t.Errorf("rebase = %q", got)
	}
}
