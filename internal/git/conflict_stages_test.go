package git

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/gitexec"
	"github.com/homeend/gigagit/internal/observ"
)

// conflictedRepo builds a repo whose f.txt is UU-conflicted AND whose base
// content itself contains literal 7-char conflict markers (the once-committed
// -unresolved case that makes the worktree marker text unparseable).
func conflictedRepo(t *testing.T, gitCfg ...string) (string, gitexec.Runner) {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			// the merge is EXPECTED to fail with a conflict
			if args[0] != "merge" {
				t.Fatalf("git %v: %v\n%s", args, err, out)
			}
		}
	}
	run("init", "-b", "main")
	for i := 0; i+1 < len(gitCfg); i += 2 {
		run("config", gitCfg[i], gitCfg[i+1])
	}
	write := func(content string) {
		if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Base: contains a full old marker block as plain content.
	write("top\n<<<<<<< HEAD\nold ours\n=======\nold theirs\n>>>>>>> old (v2)\nbottom\n")
	run("add", "f.txt")
	run("commit", "-m", "base with committed markers")
	run("checkout", "-b", "side")
	write("top\n<<<<<<< HEAD\nold ours\n=======\nold theirs\n>>>>>>> old (v2)\nbottom side\n")
	run("commit", "-am", "side change")
	run("checkout", "main")
	write("top\n<<<<<<< HEAD\nold ours\n=======\nold theirs\n>>>>>>> old (v2)\nbottom main\n")
	run("commit", "-am", "main change")
	run("merge", "side") // conflicts: f.txt UU
	return dir, gitexec.NewExecRunner("git", dir, observ.NewRing(50))
}

func TestUnmergedStagesReadsAllThree(t *testing.T) {
	dir, runner := conflictedRepo(t)
	repo := &Repo{Runner: runner}
	base, cur, inc, err := repo.UnmergedStages(context.Background(), "f.txt")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(base, []byte("bottom\n")) {
		t.Fatalf("base = %q", base)
	}
	if !bytes.Contains(cur, []byte("bottom main\n")) || !bytes.Contains(inc, []byte("bottom side\n")) {
		t.Fatalf("cur = %q inc = %q", cur, inc)
	}
	// the checkout-index temp files must not survive in the worktree
	m, _ := filepath.Glob(filepath.Join(dir, ".merge_file_*"))
	if len(m) != 0 {
		t.Fatalf("stray checkout-index temps: %v", m)
	}
}

func TestUnmergedStagesRejectsNonUnmerged(t *testing.T) {
	_, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}
	if _, _, _, err := repo.UnmergedStages(context.Background(), "README.md"); err == nil {
		t.Fatal("a non-unmerged path must error (callers fall back to the worktree text)")
	}
}

func TestUnmergedStagesSmudgesCRLF(t *testing.T) {
	// autocrlf=true: blobs stay LF, checkout converts to CRLF. The stages must
	// come back CONVERTED or a later resolution rewrites the file's endings.
	_, runner := conflictedRepo(t, "core.autocrlf", "true")
	repo := &Repo{Runner: runner}
	_, cur, _, err := repo.UnmergedStages(context.Background(), "f.txt")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(cur, []byte("\r\n")) {
		t.Fatal("autocrlf stages must come back CRLF (checkout conversion applied)")
	}
}

func TestRegenerateConflictOversizedMarkers(t *testing.T) {
	_, runner := conflictedRepo(t)
	repo := &Repo{Runner: runner}
	base, cur, inc, err := repo.UnmergedStages(context.Background(), "f.txt")
	if err != nil {
		t.Fatal(err)
	}
	out, err := repo.RegenerateConflict(context.Background(), base, cur, inc, 31)
	if err != nil {
		t.Fatal(err) // positive exit (= conflict count) must NOT error
	}
	s := string(out)
	if !strings.Contains(s, strings.Repeat("<", 31)) || !strings.Contains(s, strings.Repeat(">", 31)) {
		t.Fatalf("expected 31-char markers:\n%s", s)
	}
	// the old committed 7-char markers survive as plain content
	if !strings.Contains(s, "<<<<<<< HEAD\n") || !strings.Contains(s, ">>>>>>> old (v2)\n") {
		t.Fatalf("old markers must remain content:\n%s", s)
	}
	// diff3 config must not leak base sections into the output
	out2, err := repo.RegenerateConflict(context.Background(), base, cur, inc, 31)
	if err != nil || strings.Contains(string(out2), strings.Repeat("|", 31)) {
		t.Fatalf("pinned conflictStyle must stay classic: err=%v", err)
	}
}
