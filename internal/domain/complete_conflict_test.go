package domain

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// conflictedMergeRepo builds a real repo with a paused, conflicted merge
// (two branches each editing f.txt) — the internal/web/conflict_test.go
// conflictedMergeState fixture shape, reused here since domain needs the
// same starting point. Returns the worktree dir and a Service over it.
func conflictedMergeRepo(t *testing.T) (string, *Service) {
	t.Helper()
	dir, svc := newRealRepo(t)

	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitIn(t, dir, "add", "f.txt")
	runGitIn(t, dir, "commit", "-m", "f on main")

	runGitIn(t, dir, "checkout", "-b", "feature")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("feature\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitIn(t, dir, "add", "f.txt")
	runGitIn(t, dir, "commit", "-m", "f on feature")

	runGitIn(t, dir, "checkout", "main")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("main changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitIn(t, dir, "add", "f.txt")
	runGitIn(t, dir, "commit", "-m", "f changed on main")

	// Conflicted merge: expect non-zero exit, leaving MERGE_HEAD + unmerged f.txt.
	cmd := exec.Command("git", "-c", "commit.gpgsign=false", "merge", "feature")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := cmd.CombinedOutput(); err == nil {
		t.Fatalf("merge feature unexpectedly succeeded:\n%s", out)
	}
	if out := strings.TrimSpace(gitOutput(t, dir, "ls-files", "-u")); out == "" {
		t.Fatal("no unmerged entries after conflicted merge")
	}
	return dir, svc
}

// gitOutput runs a git command against dir and returns stdout, failing the
// test on error.
func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return string(out)
}

func TestCompleteConflictReportNoPausedOp(t *testing.T) {
	t.Parallel()
	dir, svc := newRealRepo(t)
	_ = dir

	_, err := svc.CompleteConflictReport(context.Background(), "echo hi", nil)
	if err == nil || !strings.Contains(err.Error(), "no paused operation") {
		t.Fatalf("err = %v, want an error containing %q", err, "no paused operation")
	}
}

func TestCompleteConflictReportCompletesAMerge(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("uses sh/printf")
	}
	dir, svc := conflictedMergeRepo(t)

	cmd := `git checkout --theirs f.txt && git add f.txt && GIT_EDITOR=true git merge --continue && printf 'took theirs\n' > "$GG_MESSAGE_FILE"`
	res, err := svc.CompleteConflictReport(context.Background(), cmd, nil)
	if err != nil {
		t.Fatalf("CompleteConflictReport: %v", err)
	}
	if res.Overview != "took theirs" {
		t.Fatalf("Overview = %q, want %q", res.Overview, "took theirs")
	}
	if res.Op != "merge" {
		t.Fatalf("Op = %q, want %q", res.Op, "merge")
	}
	if res.StillPaused {
		t.Fatalf("StillPaused = true, want false")
	}

	st, err := svc.Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st.Counts().Conflicted != 0 {
		t.Fatalf("Conflicted = %d, want 0", st.Counts().Conflicted)
	}
	if _, err := os.Stat(filepath.Join(dir, ".git", "MERGE_HEAD")); !os.IsNotExist(err) {
		t.Fatalf("MERGE_HEAD still present: err=%v", err)
	}
}

func TestCompleteConflictReportStopEarlyLeavesPaused(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("uses sh")
	}
	dir, svc := conflictedMergeRepo(t)

	res, err := svc.CompleteConflictReport(context.Background(), "echo gave up", nil)
	if err != nil {
		t.Fatalf("CompleteConflictReport: %v", err)
	}
	if !res.StillPaused {
		t.Fatalf("StillPaused = false, want true")
	}
	if res.Op != "merge" {
		t.Fatalf("Op = %q, want %q", res.Op, "merge")
	}
	if res.Overview != "gave up" {
		t.Fatalf("Overview = %q, want %q", res.Overview, "gave up")
	}

	// Independent git-level assertions: res.StillPaused is computed by the
	// same s.Status/s.Conflict machinery under test, so it alone can't catch
	// a latent bug in Conflict()'s attribution. Mirror the completes-a-merge
	// test's symmetry by checking the raw repo state directly instead.
	if _, err := os.Stat(filepath.Join(dir, ".git", "MERGE_HEAD")); err != nil {
		t.Fatalf("MERGE_HEAD missing, want present (merge still genuinely paused): %v", err)
	}
	if out := strings.TrimSpace(gitOutput(t, dir, "ls-files", "-u")); out == "" {
		t.Fatal("ls-files -u empty, want unmerged entries still present")
	}
}

func TestCompleteConflictReportEnvReachesAgent(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("uses sh/printf")
	}
	_, svc := conflictedMergeRepo(t)

	cmd := `printf '%s' "$GG_OP" > "$GG_MESSAGE_FILE"`
	res, err := svc.CompleteConflictReport(context.Background(), cmd, nil)
	if err != nil {
		t.Fatalf("CompleteConflictReport: %v", err)
	}
	if res.Overview != "merge" {
		t.Fatalf("Overview = %q, want %q", res.Overview, "merge")
	}
}
