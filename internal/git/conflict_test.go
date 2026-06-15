package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/gigagit/gg/internal/gitexec"
	"github.com/gigagit/gg/internal/observ"
)

func TestConflictVerbArgv(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git checkout --theirs", gitexec.Result{})
	f.SetResponse("git checkout-index (base)", gitexec.Result{})
	f.SetResponse("git rm", gitexec.Result{})
	r := &Repo{Runner: f}
	_ = r.CheckoutSide(context.Background(), "p.txt", "theirs")
	_ = r.CheckoutBaseStage(context.Background(), "p.txt")
	_ = r.RemoveFile(context.Background(), "p.txt")
	want := [][]string{
		{"checkout", "--theirs", "--", "p.txt"},
		{"checkout-index", "--stage=1", "-f", "--", "p.txt"},
		{"rm", "-f", "--", "p.txt"},
	}
	for i, w := range want {
		if !reflect.DeepEqual(f.Calls[i].Argv, w) {
			t.Errorf("call %d argv = %v, want %v", i, f.Calls[i].Argv, w)
		}
	}
}

// conflictRepo builds a real repo with a UU and a DU conflict, returns its dir.
func conflictRepo(t *testing.T) (string, *Repo) {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil && args[0] != "merge" {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(name, content string) { os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644) }
	run("init", "-q", "-b", "main")
	write("uu.txt", "base\n")
	write("md.txt", "base\n")
	run("add", "-A")
	run("commit", "-qm", "base")
	run("checkout", "-q", "-b", "feature")
	write("uu.txt", "theirs\n")
	write("md.txt", "theirs-mod\n")
	run("add", "-A")
	run("commit", "-qm", "feature")
	run("checkout", "-q", "main")
	write("uu.txt", "ours\n")
	run("add", "-A")
	run("rm", "-q", "md.txt")
	run("commit", "-qm", "main")
	run("merge", "feature") // conflicts (exit 1) — tolerated above
	return dir, &Repo{Runner: gitexec.NewExecRunner("git", dir, observ.NewRing(50))}
}

func TestCheckoutSideAndBaseReal(t *testing.T) {
	dir, r := conflictRepo(t)
	ctx := context.Background()
	// keep theirs on the both-modified file
	if err := r.CheckoutSide(ctx, "uu.txt", "theirs"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "uu.txt")); string(b) != "theirs\n" {
		t.Errorf("uu.txt = %q, want theirs", b)
	}
	// keep base
	if err := r.CheckoutBaseStage(ctx, "uu.txt"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "uu.txt")); string(b) != "base\n" {
		t.Errorf("uu.txt = %q, want base", b)
	}
	// modify/delete: keep the present (theirs) side
	if err := r.CheckoutSide(ctx, "md.txt", "theirs"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "md.txt")); string(b) != "theirs-mod\n" {
		t.Errorf("md.txt = %q, want theirs-mod", b)
	}
}

func TestRemoveFileReal(t *testing.T) {
	dir, r := conflictRepo(t)
	ctx := context.Background()
	// md.txt is a modify/delete conflict (worktree copy differs from index);
	// RemoveFile must force past git rm's "local modifications" guard.
	if err := r.RemoveFile(ctx, "md.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "md.txt")); !os.IsNotExist(err) {
		t.Errorf("md.txt should be gone, stat err = %v", err)
	}
	st, err := r.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range st.Conflicts() {
		if f.Path == "md.txt" {
			t.Errorf("md.txt should have left Conflicts() after rm")
		}
	}
}

func TestMergeHeadNameReal(t *testing.T) {
	_, r := conflictRepo(t) // merge of feature into main
	name, err := r.MergeHeadName(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if name != "feature" {
		t.Errorf("MergeHeadName = %q, want feature", name)
	}
}

func TestRebasePartiesReal(t *testing.T) {
	dir := t.TempDir()
	gitRun := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil && args[0] != "rebase" {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	w := func(name, c string) { os.WriteFile(filepath.Join(dir, name), []byte(c), 0o644) }
	gitRun("init", "-q", "-b", "main")
	w("f.txt", "base\n")
	gitRun("add", "-A")
	gitRun("commit", "-qm", "base")
	gitRun("checkout", "-q", "-b", "feature")
	w("f.txt", "theirs\n")
	gitRun("add", "-A")
	gitRun("commit", "-qm", "feature")
	gitRun("checkout", "-q", "main")
	w("f.txt", "ours\n")
	gitRun("add", "-A")
	gitRun("commit", "-qm", "main")
	gitRun("checkout", "-q", "feature")
	gitRun("rebase", "main") // conflicts (exit 1) — tolerated above
	r := &Repo{Runner: gitexec.NewExecRunner("git", dir, observ.NewRing(50))}
	branch, onto, err := r.RebaseParties(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if branch != "feature" || onto != "main" {
		t.Errorf("RebaseParties = (%q,%q), want (feature,main)", branch, onto)
	}
}

func TestCleanRefName(t *testing.T) {
	cases := map[string]string{
		"refs/heads/feature": "feature",
		"feature~2":          "feature",
		"feature^0":          "feature",
		"undefined":          "", // name-rev's no-match sentinel → no attribution
		"abc1234":            "abc1234",
	}
	for in, want := range cases {
		if got := cleanRefName(in); got != want {
			t.Errorf("cleanRefName(%q) = %q, want %q", in, got, want)
		}
	}
}
