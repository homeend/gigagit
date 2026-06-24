package domain

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/gitexec"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/observ"
)

// eolRepo creates a real repo with core.autocrlf=false (so git applies no
// line-ending normalization of its own — the gg-side filter is what must drop
// the EOL-only file) and three tracked files set up for the reconcile cases.
func eolRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run("init", "-b", "main")
	run("config", "core.autocrlf", "false")

	write("eol.txt", "a\nb\nc\n") // will become EOL-only modified → dropped
	write("real.txt", "a\nb\n")   // will get a genuine edit → kept
	write("staged.txt", "x\ny\n") // staged real edit + unstaged EOL-only → kept (staged)
	run("add", "eol.txt", "real.txt", "staged.txt")
	run("commit", "-m", "init")

	// eol.txt: only line endings change.
	write("eol.txt", "a\r\nb\r\nc\r\n")
	// real.txt: a true content change.
	write("real.txt", "a\nBEE\n")
	// staged.txt: stage a real change, then add CRLF on top (unstaged = EOL-only
	// vs the staged index).
	write("staged.txt", "x\nYELLOW\n")
	run("add", "staged.txt")
	write("staged.txt", "x\r\nYELLOW\r\n")

	return dir
}

func eolService(t *testing.T, dir string) *Service {
	t.Helper()
	return New(&git.Repo{Runner: gitexec.NewExecRunner("git", dir, observ.NewRing(50))})
}

func fileByPath(files []model.FileStatus, path string) (model.FileStatus, bool) {
	for _, f := range files {
		if f.Path == path {
			return f, true
		}
	}
	return model.FileStatus{}, false
}

// By default the EOL-only file is hidden, the genuine edit stays, and a file
// that is staged-modified but only EOL-different in the working tree keeps its
// staged entry (with the noise unstaged 'M' cleared).
func TestStatusDropsEOLOnly(t *testing.T) {
	dir := eolRepo(t)
	st, err := eolService(t, dir).Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}

	if _, ok := fileByPath(st.Files, "eol.txt"); ok {
		t.Errorf("eol.txt (line-endings-only change) should be dropped; files=%+v", st.Files)
	}
	if f, ok := fileByPath(st.Files, "real.txt"); !ok || f.Unstaged != 'M' {
		t.Errorf("real.txt should remain unstaged-modified; got %+v ok=%v", f, ok)
	}
	if f, ok := fileByPath(st.Files, "staged.txt"); !ok || f.Staged != 'M' || f.Unstaged != '.' {
		t.Errorf("staged.txt should keep its staged 'M' with unstaged cleared; got %+v ok=%v", f, ok)
	}

	c := st.Counts()
	if c.Unstaged != 1 {
		t.Errorf("Unstaged count = %d, want 1 (only real.txt)", c.Unstaged)
	}
	if c.Staged != 1 {
		t.Errorf("Staged count = %d, want 1 (staged.txt)", c.Staged)
	}
}

// SetShowEOLOnlyChanges(true) opts out of the filter: the EOL-only file is
// surfaced as modified again (the [ui] show_eol_only_changes escape hatch).
func TestStatusShowEOLOnlyWhenEnabled(t *testing.T) {
	dir := eolRepo(t)
	svc := eolService(t, dir)
	svc.SetShowEOLOnlyChanges(true)
	st, err := svc.Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if f, ok := fileByPath(st.Files, "eol.txt"); !ok || f.Unstaged != 'M' {
		t.Errorf("with show enabled, eol.txt should appear unstaged-modified; got %+v ok=%v", f, ok)
	}
}
