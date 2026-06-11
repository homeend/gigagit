package app

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/gigagit/gg/internal/git"
	"github.com/gigagit/gg/internal/gitexec"
	"github.com/gigagit/gg/internal/observ"
)

func TestDumpRepoWritesValidJSON(t *testing.T) {
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-b", "main")
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a\n"), 0o644)
	run("add", ".")
	run("commit", "-m", "initial")

	ring := observ.NewRing(50)
	repo := &git.Repo{Runner: gitexec.NewExecRunner("git", dir, ring)}
	_, _ = repo.Status(context.Background()) // populate the ring with a span

	path := filepath.Join(t.TempDir(), "dump.json")
	if err := DumpRepo(context.Background(), path, repo, ring, []string{"panic: boom"}); err != nil {
		t.Fatalf("DumpRepo: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var d observ.Dump
	if err := json.Unmarshal(raw, &d); err != nil {
		t.Fatalf("dump not valid JSON: %v", err)
	}
	if d.Repo.Branch != "main" {
		t.Fatalf("dump branch = %q, want main", d.Repo.Branch)
	}
	if len(d.Errors) == 0 {
		t.Fatal("dump should include the provided errors")
	}
	if len(d.Recent) == 0 {
		t.Fatal("dump should include recent spans")
	}
}
