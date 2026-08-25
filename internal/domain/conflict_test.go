package domain

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/gitexec"
	"github.com/homeend/gigagit/internal/hunkpick"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/observ"
)

func TestInProgressOpNoneWhenClean(t *testing.T) {
	t.Parallel()
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

// revertConflictDir leaves a paused revert: base→v2→v3 on main, then revert the
// v2 commit, which conflicts with v3.
func revertConflictDir(t *testing.T) string {
	dir := t.TempDir()
	gitRunDir(t, dir, "", "init", "-q", "-b", "main")
	writeFile(t, dir, "f.txt", "base\n")
	gitRunDir(t, dir, "", "add", "-A")
	gitRunDir(t, dir, "", "commit", "-qm", "base")
	writeFile(t, dir, "f.txt", "v2\n")
	gitRunDir(t, dir, "", "commit", "-qam", "to v2")
	v2 := gitOutDir(t, dir, "rev-parse", "HEAD")
	writeFile(t, dir, "f.txt", "v3\n")
	gitRunDir(t, dir, "", "commit", "-qam", "to v3")
	gitRunDir(t, dir, "revert", "revert", "--no-edit", v2)
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
	svc := svcAt(cherryPickConflictDir(t))
	op, err := svc.InProgressOp(context.Background())
	if err != nil || op != "cherry-pick" {
		t.Fatalf("InProgressOp = (%q, %v), want cherry-pick", op, err)
	}
}

func TestConflictStateRevert(t *testing.T) {
	t.Parallel()
	svc := svcAt(revertConflictDir(t))
	st, err := svc.repo.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	cs := svc.conflictState(context.Background(), st)
	if cs.Op != "revert" || cs.Source == "" {
		t.Fatalf("conflictState = %+v, want revert with a source", cs)
	}
	if got := cs.Describe(); !strings.HasPrefix(got, "reverting ") {
		t.Errorf("Describe = %q, want 'reverting …'", got)
	}
}

func TestInProgressOpRevert(t *testing.T) {
	t.Parallel()
	svc := svcAt(revertConflictDir(t))
	op, err := svc.InProgressOp(context.Background())
	if err != nil || op != "revert" {
		t.Fatalf("InProgressOp = (%q, %v), want revert", op, err)
	}
}

// A paused rebase pick also sets CHERRY_PICK_HEAD; probe order must report
// "rebase", not "cherry-pick".
func TestInProgressOpRebaseWinsOverCherryPickHead(t *testing.T) {
	t.Parallel()
	svc := svcAt(rebaseConflictDir(t))
	op, err := svc.InProgressOp(context.Background())
	if err != nil || op != "rebase" {
		t.Fatalf("InProgressOp during rebase = (%q, %v), want rebase", op, err)
	}
}

func TestConflictStateCleanIsZero(t *testing.T) {
	t.Parallel()
	svc := svcAt(cleanDir(t))
	st, _ := svc.repo.Status(context.Background())
	if cs := svc.conflictState(context.Background(), st); cs.Op != "" || cs.Describe() != "" {
		t.Errorf("clean conflictState = %+v, want zero", cs)
	}
}

func TestSnapshotCarriesConflictSource(t *testing.T) {
	t.Parallel()
	snap, err := svcAt(mergeConflictDir(t)).Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snap.Conflict.Describe() != "merging feature into main" {
		t.Errorf("snapshot conflict = %+v", snap.Conflict)
	}
}

func TestConflictCleanRepoIsZero(t *testing.T) {
	t.Parallel()
	s := svcAt(cleanDir(t))
	st, err := s.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := s.Conflict(context.Background(), st); got != (ConflictState{}) {
		t.Errorf("clean repo conflict = %+v, want zero", got)
	}
}

// resolvePause stages a resolution for the f.txt conflict without continuing
// the paused op — the "resolved outside gg" state the resume prompt detects.
func resolvePause(t *testing.T, dir string) {
	t.Helper()
	writeFile(t, dir, "f.txt", "resolved\n")
	gitRunDir(t, dir, "", "add", "-A")
}

func TestConflictDetectsResolvedPausedRebase(t *testing.T) {
	t.Parallel()
	dir := rebaseConflictDir(t)
	resolvePause(t, dir)
	s := svcAt(dir)
	st, err := s.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n := st.Counts().Conflicted; n != 0 {
		t.Fatalf("Conflicted = %d, want 0 after resolving", n)
	}
	cs := s.Conflict(context.Background(), st)
	if cs.Op != "rebase" {
		t.Fatalf("Conflict = %+v, want Op=rebase", cs)
	}
	if cs.Source != "feature" || cs.Target != "main" {
		t.Errorf("attribution = %+v, want feature onto main", cs)
	}
}

func TestConflictDetectsResolvedPausedMerge(t *testing.T) {
	t.Parallel()
	dir := mergeConflictDir(t)
	resolvePause(t, dir)
	s := svcAt(dir)
	st, err := s.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	cs := s.Conflict(context.Background(), st)
	if cs.Op != "merge" || cs.Source != "feature" || cs.Target != "main" {
		t.Fatalf("Conflict = %+v, want {merge feature main}", cs)
	}
}

func TestSnapshotCarriesResolvedPausedOp(t *testing.T) {
	t.Parallel()
	dir := rebaseConflictDir(t)
	resolvePause(t, dir)
	snap, err := svcAt(dir).Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snap.Conflict.Op != "rebase" {
		t.Errorf("snapshot conflict = %+v, want Op=rebase", snap.Conflict)
	}
}

// Steady-state clean path: after the first Conflict call caches the git dir,
// repeated clean-status calls must run ZERO further git invocations — the
// paused-op probe is pure file stats.
func TestConflictCleanSteadyStateCachesGitDir(t *testing.T) {
	t.Parallel()
	fake := gitexec.NewFakeRunner()
	gitDir := t.TempDir() // stands in for the resolved git dir; no sequencer markers
	fake.SetResponse("git rev-parse (git-dir)", gitexec.Result{Stdout: gitDir + "\n"})
	fake.SetResponse("git rev-parse (common-dir)", gitexec.Result{Stdout: gitDir + "\n"})
	s := New(&git.Repo{Runner: fake})
	st := model.WorkingTreeStatus{} // clean: zero conflicted files
	for i := 0; i < 3; i++ {
		if cs := s.Conflict(context.Background(), st); cs != (ConflictState{}) {
			t.Fatalf("call %d: clean Conflict = %+v, want zero", i, cs)
		}
	}
	n := 0
	for _, c := range fake.Calls {
		if c.Name == "git rev-parse (git-dir)" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("git dir resolved %d times across 3 clean calls, want 1 (cached)", n)
	}
}

// TestConflictPickerFileRegenerates: the base content itself contains literal
// 7-char conflict markers (committed unresolved once), so the worktree marker
// text is unparseable — the picker text must be regenerated from the stages
// with oversized markers.
func TestConflictPickerFileRegenerates(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	old := "top\n<<<<<<< HEAD\nold ours\n=======\nold theirs\n>>>>>>> old (v2)\n"
	gitRunDir(t, dir, "", "init", "-q", "-b", "main")
	writeFile(t, dir, "f.txt", old+"bottom\n")
	gitRunDir(t, dir, "", "add", "-A")
	gitRunDir(t, dir, "", "commit", "-m", "base")
	gitRunDir(t, dir, "", "checkout", "-q", "-b", "side")
	writeFile(t, dir, "f.txt", old+"bottom side\n")
	gitRunDir(t, dir, "", "commit", "-am", "side")
	gitRunDir(t, dir, "", "checkout", "-q", "main")
	writeFile(t, dir, "f.txt", old+"bottom main\n")
	gitRunDir(t, dir, "", "commit", "-am", "main")
	gitRunDir(t, dir, "merge", "merge", "side")
	svc := New(&git.Repo{Runner: gitexec.NewExecRunner("git", dir, observ.NewRing(50))})

	content, size, err := svc.ConflictPickerFile(context.Background(), "f.txt")
	if err != nil {
		t.Fatal(err)
	}
	if size <= 7 {
		t.Fatalf("size = %d, want > 7 (content imitates 7-char markers)", size)
	}
	d, perr := hunkpick.ParseConflictSized(content, size)
	if perr != nil {
		t.Fatalf("regenerated text must parse: %v", perr)
	}
	if len(d.Blocks()) == 0 {
		t.Fatal("want at least one conflict region")
	}
	if !strings.Contains(string(content), "<<<<<<< HEAD") {
		t.Fatal("the old committed markers must survive as content")
	}

	// A path that is not unmerged falls back to worktree bytes at size 7.
	writeFile(t, dir, "g.txt", "plain\n")
	content, size, err = svc.ConflictPickerFile(context.Background(), "g.txt")
	if err != nil || string(content) != "plain\n" || size != 7 {
		t.Fatalf("fallback = %q size=%d err=%v, want worktree bytes at 7", content, size, err)
	}
}
