package linknav

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

// hunkRepo builds three commits over one file: c2 rewrites the TOP, c3 the
// BOTTOM. The two ranges therefore disagree about what "hunk 1" is, which is
// the whole point — c1..c3 has two hunks and its first is the top, while
// c3^..c3 has one and it is the bottom.
func hunkRepo(t *testing.T) (dir, c1, c3 string, svc *domain.Service) {
	t.Helper()
	dir = t.TempDir()
	run := func(args ...string) {
		t.Helper()
		c := exec.Command("git", args...)
		c.Dir = dir
		c.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	head := func() string {
		t.Helper()
		o, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
		if err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(string(o))
	}
	// Nine middle lines keep the two edits more than three context lines
	// apart, so git emits them as TWO hunks rather than merging them into one.
	run("init", "-q", "-b", "main")
	write("t1\nt2\nt3\nm1\nm2\nm3\nm4\nm5\nm6\nm7\nm8\nm9\nb1\nb2\nb3\n")
	run("add", ".")
	run("commit", "-q", "-m", "c1")
	c1 = head()
	write("TOP\nt2\nt3\nm1\nm2\nm3\nm4\nm5\nm6\nm7\nm8\nm9\nb1\nb2\nb3\n")
	run("commit", "-q", "-am", "c2 rewrites the top")
	write("TOP\nt2\nt3\nm1\nm2\nm3\nm4\nm5\nm6\nm7\nm8\nm9\nb1\nb2\nBOTTOM\n")
	run("commit", "-q", "-am", "c3 rewrites the bottom")
	c3 = head()
	return dir, c1, c3, domain.Open(dir)
}

// TestPairHunkNumbersAgainstTheRange is the silent-wrong-landing guard for a
// change-set link's `#<hunk>`.
//
// domain.Resolved.Commit carries B alone — the only single commit a pair has —
// and HunkLine lowers through HunkDiffSpec, which turns a BARE commit into
// <commit>^..<commit>. So lowering a pair's hunk against res.Commit numbered
// it against B's OWN change: `#1` of `@c1..c3` landed on line 12, the single
// hunk of c3^..c3, instead of line 1, the range's first. Exit 0, wrong place.
// The range must ride into HunkDiffSpec whole, exactly as `gg diff a..b
// --hunks` sends it.
func TestPairHunkNumbersAgainstTheRange(t *testing.T) {
	t.Parallel()
	dir, c1, c3, svc := hunkRepo(t)
	ctx := context.Background()
	res := resolve(t, svc, model.Link{
		Repo: model.LinkRepo{Abs: abs(dir)}, Path: "a.txt",
		Target: model.LinkTarget{State: model.StateCommitted, Pair: &model.LinkPair{A: c1, B: c3}},
		Side:   model.NoteSideNew, Hunk: 1,
	}.String())
	c, err := Command(ctx, svc, res)
	if err != nil {
		t.Fatalf("Command: %v", err)
	}
	if c.Line == nil {
		t.Fatal("no line: a #hunk link must lower to one")
	}
	if c.Line.No != 1 {
		t.Errorf("hunk 1 of the range landed on line %d, want 1 (the TOP edit). "+
			"Line 12 means it was numbered against B's own change, not against %s..%s",
			c.Line.No, c1[:7], c3[:7])
	}
	// Hunk 2 is the bottom edit — proof the range really has two hunks and
	// that the first was not simply clamped to the start of the file.
	res.Hunk = 2
	c, err = Command(ctx, svc, res)
	if err != nil {
		t.Fatalf("Command (hunk 2): %v", err)
	}
	if c.Line == nil || c.Line.No < 10 {
		t.Errorf("hunk 2 landed on %+v, want the BOTTOM edit near line 15", c.Line)
	}
}

// TestRefHunkNumbersAgainstTheTipsOwnChange pins the OTHER half of the rule,
// so the two shapes cannot drift together. A `@ref:<name>` link is a POINT —
// the same kind of target as `@<sha>` — so its hunks are the tip commit's own
// change (tip^..tip), NOT a range. `@ref:main#1` therefore lands on the
// bottom edit, which is exactly where the pair link must NOT land.
func TestRefHunkNumbersAgainstTheTipsOwnChange(t *testing.T) {
	t.Parallel()
	dir, _, _, svc := hunkRepo(t)
	ctx := context.Background()
	res := resolve(t, svc, model.Link{
		Repo: model.LinkRepo{Abs: abs(dir)}, Path: "a.txt",
		Target: model.LinkTarget{State: model.StateCommitted, Ref: "main"},
		Side:   model.NoteSideNew, Hunk: 1,
	}.String())
	c, err := Command(ctx, svc, res)
	if err != nil {
		t.Fatalf("Command: %v", err)
	}
	if c.Line == nil || c.Line.No < 10 {
		t.Errorf("ref hunk 1 landed on %+v, want the tip's own change near line 15", c.Line)
	}
	if c.Target == nil || c.Target.State != "ref" || c.Target.Ref != "main" {
		t.Errorf("target = %+v, want the ref NAME on the wire", c.Target)
	}
}
