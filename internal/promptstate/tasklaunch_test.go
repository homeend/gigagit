package promptstate

import (
	"path/filepath"
	"testing"
)

func TestTaskLaunchChoiceRoundTrip(t *testing.T) {
	t.Parallel()
	fs := NewFileStore(filepath.Join(t.TempDir(), "prompts.toml"))
	if _, ok := fs.TaskLaunchChoice("review"); ok {
		t.Fatal("empty store must have no choice")
	}
	want := TaskLaunch{Agent: "id:claude", Mode: "background", Command: "Claude Code (interactive)"}
	if err := fs.SetTaskLaunchChoice("review", want); err != nil {
		t.Fatal(err)
	}
	if err := fs.SuppressPrompt("x"); err != nil { // a sibling write keeps it
		t.Fatal(err)
	}
	got, ok := fs.TaskLaunchChoice("review")
	if !ok || got != want {
		t.Fatalf("got %+v %v", got, ok)
	}
	if _, ok := fs.TaskLaunchChoice("commit_message"); ok {
		t.Fatal("choices are per kind")
	}
}
