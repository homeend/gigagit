package linknav

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

// repo builds a checkout with one committed file and one unstaged hunk.
func repo(t *testing.T) (string, *domain.Service) {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		c := exec.Command("git", args...)
		c.Dir = dir
		c.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-q", "-m", "one")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n11\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir, domain.Open(dir)
}

func resolve(t *testing.T, svc *domain.Service, s string) domain.Resolved {
	t.Helper()
	res, err := Resolve(context.Background(), "", svc, s)
	if err != nil {
		t.Fatalf("Resolve(%q): %v", s, err)
	}
	return res
}

func abs(dir string) string { return filepath.ToSlash(dir) }

func TestCommandShapes(t *testing.T) {
	t.Parallel()
	dir, svc := repo(t)
	ctx := context.Background()

	// A bare repository link has no place: RepoOnly, and Command refuses it.
	bare := resolve(t, svc, "gg://"+abs(dir))
	if !RepoOnly(bare) {
		t.Errorf("RepoOnly(%+v) = false", bare)
	}
	if _, err := Command(ctx, svc, bare); !errors.Is(err, ErrRepoOnly) {
		t.Errorf("bare link: err = %v, want ErrRepoOnly", err)
	}

	// A file:line link in the working tree.
	fl := resolve(t, svc, "gg://"+abs(dir)+"/a.txt:2")
	if RepoOnly(fl) {
		t.Error("a file link is not RepoOnly")
	}
	c, err := Command(ctx, svc, fl)
	if err != nil || c.Cmd != "navigate" || c.File != "a.txt" || c.Target == nil || c.Target.State != "unstaged" || c.Line == nil || c.Line.No != 2 || c.Line.Side != "new" {
		t.Fatalf("file link: %+v, %v", c, err)
	}
	if c.ID != "" || c.Wait {
		t.Errorf("a built command carries no id and no wait: %+v", c)
	}
	at := AtLink(fl, c)
	if !strings.HasPrefix(at.String(), "gg://") || at.Path != "a.txt" || at.Line != 2 || at.Repo.Abs == "" {
		t.Errorf("AtLink = %+v (%s)", at, at.String())
	}

	// A file link with no line and no hunk opens the file and leaves the
	// cursor alone: it is a link to the FILE, not a refusal.
	nl := resolve(t, svc, "gg://"+abs(dir)+"/a.txt")
	c, err = Command(ctx, svc, nl)
	if err != nil || c.Cmd != "navigate" || c.File != "a.txt" || c.Line != nil {
		t.Fatalf("no-line link: %+v, %v", c, err)
	}

	// A #hunk link is lowered to the FIRST line of the hunk's whole new-side
	// span, context included (HunkRange's contract): appending line 11 yields
	// @@ -8,3 +8,4 @@, so the landing is line 8.
	hk := resolve(t, svc, "gg://"+abs(dir)+"/a.txt#1")
	c, err = Command(ctx, svc, hk)
	if err != nil || c.Line == nil || c.Line.No != 8 || c.Line.Side != "new" {
		t.Fatalf("hunk link: line=%+v err=%v", c.Line, err)
	}
	if at := AtLink(hk, c); at.Hunk != 0 || at.Line != 8 {
		t.Errorf("AtLink after lowering = hunk %d line %d, want line 8", at.Hunk, at.Line)
	}

	// A commit link with no path reveals the commit.
	sha, err := svc.RevParse(ctx, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	cm := resolve(t, svc, "gg://"+abs(dir)+"@"+sha)
	if RepoOnly(cm) {
		t.Error("a commit link is not RepoOnly")
	}
	c, err = Command(ctx, svc, cm)
	if err != nil || c.Commit != sha || c.File != "" {
		t.Fatalf("commit link: %+v, %v", c, err)
	}
	if at := AtLink(cm, c); at.Target.State != model.StateCommitted || at.Target.Commit != sha {
		t.Errorf("AtLink(commit) = %+v", at.Target)
	}
}

// TestFileLinkWithNoLineOpensTheFile pins R1: `gg link <path>` emits a link
// with no line, and Command must turn it into a navigate that names the file
// and leaves the cursor alone — not the old ErrNoLine refusal, which made
// `gg open` reject a link `gg link` had just produced.
func TestFileLinkWithNoLineOpensTheFile(t *testing.T) {
	t.Parallel()
	dir, svc := repo(t)
	ctx := context.Background()
	res := resolve(t, svc, model.Link{
		Repo: model.LinkRepo{Abs: abs(dir)}, Path: "a.txt",
		Target: model.LinkTarget{State: model.StateUnstaged},
		Side:   model.NoteSideNew,
	}.String())
	c, err := Command(ctx, svc, res)
	if err != nil {
		t.Fatalf("Command: %v", err)
	}
	if c.Cmd != "navigate" || c.File != "a.txt" {
		t.Fatalf("command = %+v, want a navigate naming a.txt", c)
	}
	if c.Line != nil {
		t.Errorf("Line = %+v, want nil (open the file, do not move the cursor)", c.Line)
	}
	if c.Target == nil || c.Target.State != "unstaged" {
		t.Errorf("target = %+v, want the working tree", c.Target)
	}

	// The commit-target twin: `gg link <path> --rev <sha>` emits an
	// @<sha> target with a path and no line, the other shape the user hit.
	sha, err := svc.RevParse(ctx, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	res = resolve(t, svc, model.Link{
		Repo: model.LinkRepo{Abs: abs(dir)}, Path: "a.txt",
		Target: model.LinkTarget{State: model.StateCommitted, Commit: sha},
		Side:   model.NoteSideNew,
	}.String())
	c, err = Command(ctx, svc, res)
	if err != nil {
		t.Fatalf("Command (commit target): %v", err)
	}
	if c.Cmd != "navigate" || c.File != "a.txt" || c.Line != nil {
		t.Fatalf("commit-target command = %+v, want a navigate naming a.txt with no line", c)
	}
	if c.Target == nil || c.Target.State != "commit" || c.Target.Commit != sha {
		t.Errorf("target = %+v, want commit %s", c.Target, sha)
	}
}

func TestTargetOf(t *testing.T) {
	t.Parallel()
	if got := TargetOf(model.FileAddress{State: model.StateStaged}); got.State != "staged" {
		t.Errorf("staged → %+v", got)
	}
	if got := TargetOf(model.FileAddress{State: model.StateCommitted, Commit: "abc"}); got.State != "commit" || got.Commit != "abc" {
		t.Errorf("committed → %+v", got)
	}
	// FileState's ZERO value is StateCommitted (its sha then rides along, even
	// empty); the working tree is StateUnstaged. Both are pinned here so a
	// reordering of the enum cannot silently move the wire default.
	if got := TargetOf(model.FileAddress{}); got.State != "commit" || got.Commit != "" {
		t.Errorf("zero (committed) → %+v", got)
	}
	if got := TargetOf(model.FileAddress{State: model.StateUnstaged}); got.State != "unstaged" {
		t.Errorf("unstaged → %+v", got)
	}
	if got := TargetOf(model.FileAddress{State: model.StateUntracked}); got.State != "untracked" {
		t.Errorf("untracked → %+v", got)
	}
}
