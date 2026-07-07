package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/gitexec"
)

func TestIsMailboxPatch(t *testing.T) {
	cases := []struct {
		name string
		data string
		want bool
	}{
		{"format-patch head", "From 3f2a1b0c4d5e6f708192a3b4c5d6e7f801234567 Mon Sep 17 00:00:00 2001\nFrom: A U Thor <a@t>\n", true},
		{"plain git diff", "diff --git a/foo.go b/foo.go\nindex 000..111 100644\n", false},
		{"unified diff", "--- a/foo.go\n+++ b/foo.go\n@@ -1 +1 @@\n", false},
		{"leading blank lines", "\n\nFrom 3f2a1b0c Mon Sep 17 00:00:00 2001\n", true},
		{"empty", "", false},
		{"From: header alone is not the mbox sentinel", "From: A U Thor <a@t>\n", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := IsMailboxPatch([]byte(c.data)); got != c.want {
				t.Fatalf("IsMailboxPatch = %v, want %v", got, c.want)
			}
		})
	}
}

func TestApplyPatchArgv(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git apply", gitexec.Result{})
	r := &Repo{Runner: f}
	if err := r.ApplyPatch(context.Background(), "/tmp/x.patch", true); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(f.Calls[0].Argv, " "); got != "apply --3way /tmp/x.patch" {
		t.Fatalf("argv = %q", got)
	}

	f2 := gitexec.NewFakeRunner()
	f2.SetResponse("git apply", gitexec.Result{})
	r2 := &Repo{Runner: f2}
	if err := r2.ApplyPatch(context.Background(), "/tmp/x.patch", false); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(f2.Calls[0].Argv, " "); got != "apply /tmp/x.patch" {
		t.Fatalf("no-3way argv = %q", got)
	}
}

func TestPatchPathsArgv(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git apply", gitexec.Result{Stdout: "1\t0\tfoo.txt\x00"})
	r := &Repo{Runner: f}
	got, err := r.PatchPaths(context.Background(), "/tmp/x.patch")
	if err != nil {
		t.Fatal(err)
	}
	if argv := strings.Join(f.Calls[0].Argv, " "); argv != "apply --numstat -z /tmp/x.patch" {
		t.Fatalf("argv = %q", argv)
	}
	if want := []string{"foo.txt"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("paths = %v, want %v", got, want)
	}
}

// TestPatchPathsRealRepo builds a real patch (via `git diff`) touching one
// modified file and one added file, and asserts PatchPaths reports both —
// the shape engine.ApplyPatch's --3way-fallback unstage relies on.
func TestPatchPathsRealRepo(t *testing.T) {
	dir, runner := newTestRepo(t)
	r := &Repo{Runner: runner}
	ctx := context.Background()

	os.WriteFile(filepath.Join(dir, "mod.txt"), []byte("one\n"), 0o644)
	gitOutT(t, dir, "add", "mod.txt")
	gitOutT(t, dir, "commit", "-q", "-m", "base")

	// Modify the tracked file and stage a brand-new file.
	os.WriteFile(filepath.Join(dir, "mod.txt"), []byte("one\ntwo\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "added.txt"), []byte("new\n"), 0o644)
	patch := gitOutT(t, dir, "diff", "HEAD", "--", "mod.txt")
	gitOutT(t, dir, "add", "added.txt")
	addedPatch := gitOutT(t, dir, "diff", "--cached", "--", "added.txt")
	gitOutT(t, dir, "reset", "-q") // unstage added.txt, keep both diffs standalone

	p := filepath.Join(t.TempDir(), "combined.patch")
	if err := os.WriteFile(p, []byte(patch+"\n"+addedPatch+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := r.PatchPaths(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"mod.txt": true, "added.txt": true}
	if len(got) != len(want) {
		t.Fatalf("paths = %v, want exactly %v", got, want)
	}
	for _, p := range got {
		if !want[p] {
			t.Fatalf("unexpected path %q in %v", p, got)
		}
	}
}

// gitOutT runs git in dir with a real identity (needed for commit) and
// returns trimmed stdout.
func gitOutT(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestAmMailboxAndAbortArgv(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git am", gitexec.Result{})
	f.SetResponse("git am --abort", gitexec.Result{})
	r := &Repo{Runner: f}
	if err := r.AmMailbox(context.Background(), "/tmp/x.patch", true); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(f.Calls[0].Argv, " "); got != "am --3way /tmp/x.patch" {
		t.Fatalf("am argv = %q", got)
	}
	if err := r.AmAbort(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(f.Calls[1].Argv, " "); got != "am --abort" {
		t.Fatalf("abort argv = %q", got)
	}
}

// AmInProgress: false on a clean repo; true once rebase-apply/applying exists
// (the am marker); false for a bare rebase-apply dir (that shape belongs to a
// paused rebase using the apply backend — aborting am there would abort the
// user's REBASE).
func TestAmInProgress(t *testing.T) {
	dir, runner := newTestRepo(t)
	r := &Repo{Runner: runner}
	ctx := context.Background()

	if in, err := r.AmInProgress(ctx); err != nil || in {
		t.Fatalf("clean repo AmInProgress = %v, %v; want false, nil", in, err)
	}

	gd, err := r.GitDir(ctx)
	if err != nil {
		t.Fatal(err)
	}
	mkdir(t, gd, "rebase-apply")
	if in, err := r.AmInProgress(ctx); err != nil || in {
		t.Fatalf("bare rebase-apply AmInProgress = %v, %v; want false, nil (rebase owns it)", in, err)
	}
	touch(t, gd, "rebase-apply", "applying")
	if in, err := r.AmInProgress(ctx); err != nil || !in {
		t.Fatalf("with applying marker AmInProgress = %v, %v; want true, nil", in, err)
	}
	_ = dir
}

func mkdir(t *testing.T, dir string, name string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, name), 0o755); err != nil {
		t.Fatal(err)
	}
}

func touch(t *testing.T, dir string, parts ...string) {
	t.Helper()
	p := filepath.Join(append([]string{dir}, parts...)...)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}
