package domain

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gigagit/gg/internal/git"
	"github.com/gigagit/gg/internal/gitexec"
	"github.com/gigagit/gg/internal/observ"
)

func TestInProgressOpNoneWhenClean(t *testing.T) {
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
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("hi\n"), 0o644)
	run("add", ".")
	run("commit", "-m", "initial")
	svc := New(&git.Repo{Runner: gitexec.NewExecRunner("git", dir, observ.NewRing(50))})

	got, err := svc.InProgressOp(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Errorf("clean repo InProgressOp = %q, want \"\"", got)
	}
}

// gitRunDir runs git in dir, tolerating a non-zero exit for the named verb
// (merge/rebase exit 1 on conflict).
func gitRunDir(t *testing.T, dir, tolerate string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := cmd.CombinedOutput(); err != nil && args[0] != tolerate {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// divergedDir builds main and feature that both change f.txt from a shared base.
func divergedDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitRunDir(t, dir, "", "init", "-q", "-b", "main")
	writeFile(t, dir, "f.txt", "base\n")
	gitRunDir(t, dir, "", "add", "-A")
	gitRunDir(t, dir, "", "commit", "-qm", "base")
	gitRunDir(t, dir, "", "checkout", "-q", "-b", "feature")
	writeFile(t, dir, "f.txt", "theirs\n")
	gitRunDir(t, dir, "", "add", "-A")
	gitRunDir(t, dir, "", "commit", "-qm", "feature")
	gitRunDir(t, dir, "", "checkout", "-q", "main")
	writeFile(t, dir, "f.txt", "ours\n")
	gitRunDir(t, dir, "", "add", "-A")
	gitRunDir(t, dir, "", "commit", "-qm", "main")
	return dir
}

// mergeConflictDir leaves a paused merge of feature into main.
func mergeConflictDir(t *testing.T) string {
	dir := divergedDir(t)
	gitRunDir(t, dir, "merge", "merge", "feature")
	return dir
}

// rebaseConflictDir leaves a paused rebase of feature onto main.
func rebaseConflictDir(t *testing.T) string {
	dir := divergedDir(t)
	gitRunDir(t, dir, "", "checkout", "-q", "feature")
	gitRunDir(t, dir, "rebase", "rebase", "main")
	return dir
}

// cherryPickConflictDir leaves a paused cherry-pick of feature's commit onto main.
func cherryPickConflictDir(t *testing.T) string {
	dir := divergedDir(t)
	pick := gitOutDir(t, dir, "rev-parse", "feature")
	gitRunDir(t, dir, "cherry-pick", "cherry-pick", pick)
	return dir
}

func gitOutDir(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return strings.TrimSpace(string(out))
}

func cleanDir(t *testing.T) string {
	dir := t.TempDir()
	gitRunDir(t, dir, "", "init", "-q", "-b", "main")
	writeFile(t, dir, "f.txt", "hi\n")
	gitRunDir(t, dir, "", "add", "-A")
	gitRunDir(t, dir, "", "commit", "-qm", "base")
	return dir
}

func svcAt(dir string) *Service {
	return New(&git.Repo{Runner: gitexec.NewExecRunner("git", dir, observ.NewRing(50))})
}

func TestConflictStateMerge(t *testing.T) {
	svc := svcAt(mergeConflictDir(t))
	st, err := svc.repo.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	cs := svc.conflictState(context.Background(), st)
	if cs.Op != "merge" || cs.Source != "feature" || cs.Target != "main" {
		t.Fatalf("conflictState = %+v, want {merge feature main}", cs)
	}
	if got := cs.Describe(); got != "merging feature into main" {
		t.Errorf("Describe = %q", got)
	}
}

func TestConflictStateRebase(t *testing.T) {
	svc := svcAt(rebaseConflictDir(t))
	st, err := svc.repo.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	cs := svc.conflictState(context.Background(), st)
	if cs.Op != "rebase" || cs.Source != "feature" || cs.Target != "main" {
		t.Fatalf("conflictState = %+v, want {rebase feature main}", cs)
	}
	if got := cs.Describe(); got != "rebasing feature onto main" {
		t.Errorf("Describe = %q", got)
	}
}

func TestConflictStateCherryPick(t *testing.T) {
	svc := svcAt(cherryPickConflictDir(t))
	st, err := svc.repo.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	cs := svc.conflictState(context.Background(), st)
	if cs.Op != "cherry-pick" || cs.Source == "" {
		t.Fatalf("conflictState = %+v, want cherry-pick with a source", cs)
	}
	if got := cs.Describe(); !strings.HasPrefix(got, "cherry-picking ") {
		t.Errorf("Describe = %q, want 'cherry-picking …'", got)
	}
}

func TestInProgressOpCherryPick(t *testing.T) {
	svc := svcAt(cherryPickConflictDir(t))
	op, err := svc.InProgressOp(context.Background())
	if err != nil || op != "cherry-pick" {
		t.Fatalf("InProgressOp = (%q, %v), want cherry-pick", op, err)
	}
}

// A paused rebase pick also sets CHERRY_PICK_HEAD; probe order must report
// "rebase", not "cherry-pick".
func TestInProgressOpRebaseWinsOverCherryPickHead(t *testing.T) {
	svc := svcAt(rebaseConflictDir(t))
	op, err := svc.InProgressOp(context.Background())
	if err != nil || op != "rebase" {
		t.Fatalf("InProgressOp during rebase = (%q, %v), want rebase", op, err)
	}
}

func TestConflictStateCleanIsZero(t *testing.T) {
	svc := svcAt(cleanDir(t))
	st, _ := svc.repo.Status(context.Background())
	if cs := svc.conflictState(context.Background(), st); cs.Op != "" || cs.Describe() != "" {
		t.Errorf("clean conflictState = %+v, want zero", cs)
	}
}

func TestSnapshotCarriesConflictSource(t *testing.T) {
	snap, err := svcAt(mergeConflictDir(t)).Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snap.Conflict.Describe() != "merging feature into main" {
		t.Errorf("snapshot conflict = %+v", snap.Conflict)
	}
}
