package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/rebaseplan"
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
	if want := "pick aaaaaaa\ndrop bbbbbbb\n"; string(got) != want {
		t.Fatalf("todo = %q, want %q", string(got), want)
	}
}

func TestRunRebaseMessageAmendsHead(t *testing.T) {
	dir := t.TempDir()
	git := func(args ...string) {
		c := exec.Command("git", args...)
		c.Dir = dir
		c.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-b", "main")
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x\n"), 0o644)
	git("add", ".")
	git("commit", "-m", "original")

	planPath := filepath.Join(dir, "plan.json")
	p := rebaseplan.Plan{Entries: []rebaseplan.Entry{
		{Sha: "head", Action: rebaseplan.Reword, Orig: "original", NewMsg: "reworded title\n\nbody"},
	}}
	b, _ := rebaseplan.Marshal(p)
	os.WriteFile(planPath, b, 0o644)

	// runRebaseMessage runs `git commit --amend` in the current directory, so
	// run it from dir.
	cwd, _ := os.Getwd()
	defer os.Chdir(cwd)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	if err := runRebaseMessage([]string{planPath, "0"}); err != nil {
		t.Fatalf("runRebaseMessage: %v", err)
	}
	out, _ := exec.Command("git", "-C", dir, "log", "-1", "--pretty=%B").Output()
	if got := strings.TrimSpace(string(out)); got != "reworded title\n\nbody" {
		t.Fatalf("HEAD message = %q, want %q", got, "reworded title\n\nbody")
	}
}
