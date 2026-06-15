package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gigagit/gg/internal/rebaseplan"
)

func TestRunRebaseSeqRewritesTodo(t *testing.T) {
	dir := t.TempDir()
	planPath := filepath.Join(dir, "plan.json")
	todoPath := filepath.Join(dir, "git-rebase-todo")

	p := rebaseplan.Plan{Entries: []rebaseplan.Entry{
		{Sha: "aaaaaaa", Action: rebaseplan.Pick, Orig: "A"},
		{Sha: "bbbbbbb", Action: rebaseplan.Drop, Orig: "B"},
	}}
	b, _ := rebaseplan.Marshal(p)
	if err := os.WriteFile(planPath, b, 0o644); err != nil {
		t.Fatal(err)
	}
	// git would write the original todo here; content is irrelevant — we overwrite.
	if err := os.WriteFile(todoPath, []byte("pick aaaaaaa A\npick bbbbbbb B\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := runRebaseSeq([]string{planPath, todoPath}); err != nil {
		t.Fatalf("runRebaseSeq: %v", err)
	}
	got, _ := os.ReadFile(todoPath)
	if string(got) != "pick aaaaaaa\n" {
		t.Fatalf("todo = %q, want %q", string(got), "pick aaaaaaa\n")
	}
}
