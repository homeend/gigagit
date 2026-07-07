package git

import (
	"context"
	"os"
	"path/filepath"
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
