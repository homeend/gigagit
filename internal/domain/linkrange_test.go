package domain

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

const rangeBody = "l1\nl2\nl3\nl4\nl5\nl6\nl7\nl8\n"

func rangeRepo(t *testing.T) (string, *Service, func(string)) {
	t.Helper()
	dir := linkRepoWithRemote(t, "gigagit")
	write := func(body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(rangeBody)
	runGitIn(t, dir, "add", "f.txt")
	runGitIn(t, dir, "commit", "-m", "c1")
	return dir, Open(dir), write
}

func rangeLink(state model.FileState, side model.NoteSide, a, b int, fp string) model.Link {
	return model.Link{Repo: model.LinkRepo{Name: "gigagit"}, Path: "f.txt", Side: side, Line: a, End: b,
		Fingerprint: fp, Target: model.LinkTarget{State: state}}
}

func TestResolveRangeLink(t *testing.T) {
	t.Parallel()
	dir, svc, write := rangeRepo(t)
	ctx := context.Background()
	fp := model.BlockFingerprint([]string{"l3", "l4", "l5"})
	l := rangeLink(model.StateUnstaged, model.NoteSideNew, 3, 5, fp)
	if got := svc.LinkBlockFingerprint(ctx, rangeLink(model.StateUnstaged, model.NoteSideNew, 3, 5, "")); got != fp {
		t.Fatalf("LinkBlockFingerprint = %q, want %q", got, fp)
	}
	if got := svc.LinkBlockFingerprint(ctx, rangeLink(model.StateUnstaged, model.NoteSideNew, 7, 12, "")); got != "" {
		t.Errorf("a range past the end has no fingerprint, got %q", got)
	}

	res, err := ResolveLink(ctx, l, ResolveOpts{Cwd: svc})
	if err != nil || res.Line != 3 || res.End != 5 || res.Anchor.State != AnchorSame {
		t.Fatalf("same: %+v, %v", res, err)
	}
	if res.AnchorNote() != "" {
		t.Errorf("a valid range has nothing to say, got %q", res.AnchorNote())
	}

	// Re-indented, CRLF: still the same block.
	write("l1\r\nl2\r\n  l3\r\n\tl4\r\nl5\r\nl6\r\n")
	if _, err := ResolveLink(ctx, l, ResolveOpts{Cwd: svc}); err != nil {
		t.Errorf("re-indent / CRLF is not a change: %v", err)
	}

	stale := func(name string) {
		t.Helper()
		_, err := ResolveLink(ctx, l, ResolveOpts{Cwd: svc})
		if !errors.Is(err, ErrLinkStale) {
			t.Fatalf("%s: err = %v, want ErrLinkStale", name, err)
		}
		if want := "the link is no longer valid: lines 3-5 of f.txt have changed since it was copied"; err.Error() != want {
			t.Errorf("%s: message %q, want %q", name, err, want)
		}
	}
	write(strings.Replace(rangeBody, "l4", "L4", 1))
	stale("edited inside")
	write("top\n" + rangeBody) // the block is intact at 4-6: NOT followed
	stale("moved")
	write("l1\nl2\nl3\nl4\n")
	stale("past the end")
	os.Remove(filepath.Join(dir, "f.txt"))
	stale("deleted")
}

func TestResolveRangeLinkWithoutAFingerprintReadsNothing(t *testing.T) {
	t.Parallel()
	dir, svc, _ := rangeRepo(t)
	os.Remove(filepath.Join(dir, "f.txt"))
	res, err := ResolveLink(context.Background(), rangeLink(model.StateUnstaged, model.NoteSideNew, 3, 5, ""), ResolveOpts{Cwd: svc})
	if err != nil || res.Line != 3 || res.End != 5 || res.Anchor.State != "" {
		t.Errorf("%+v, %v", res, err)
	}
}

func TestResolveRangeLinkSides(t *testing.T) {
	t.Parallel()
	dir, svc, write := rangeRepo(t)
	write("i1\n" + rangeBody)
	runGitIn(t, dir, "add", "f.txt") // index: l3..l5 at 4-6; HEAD: 3-5
	fp := model.BlockFingerprint([]string{"l3", "l4", "l5"})
	ctx := context.Background()
	if _, err := ResolveLink(ctx, rangeLink(model.StateStaged, model.NoteSideOld, 3, 5, fp), ResolveOpts{Cwd: svc}); err != nil {
		t.Errorf("staged old side reads HEAD: %v", err)
	}
	if _, err := ResolveLink(ctx, rangeLink(model.StateStaged, model.NoteSideNew, 4, 6, fp), ResolveOpts{Cwd: svc}); err != nil {
		t.Errorf("staged new side reads the index: %v", err)
	}
	if _, err := ResolveLink(ctx, rangeLink(model.StateStaged, model.NoteSideNew, 3, 5, fp), ResolveOpts{Cwd: svc}); !errors.Is(err, ErrLinkStale) {
		t.Errorf("index 3-5 is another block: %v", err)
	}
}

func TestLinkText(t *testing.T) {
	t.Parallel()
	dir, svc, write := rangeRepo(t)
	ctx := context.Background()
	c1 := rangeHead(t, dir)
	write(strings.Replace(rangeBody, "l4", "c2-4", 1))
	runGitIn(t, dir, "commit", "-am", "c2")
	c2 := rangeHead(t, dir)
	write(strings.Replace(rangeBody, "l4", "idx-4", 1))
	runGitIn(t, dir, "add", "f.txt")
	write(strings.Replace(rangeBody, "l4", "wt-4", 1))

	text := func(l model.Link) LinkText {
		t.Helper()
		l.Repo.Name = "gigagit"
		res, err := ResolveLink(ctx, l, ResolveOpts{Cwd: svc})
		if err != nil {
			t.Fatal(err)
		}
		lt, err := svc.LinkText(ctx, res)
		if err != nil {
			t.Fatal(err)
		}
		return lt
	}
	commit := func(sha string) model.LinkTarget { return model.LinkTarget{State: model.StateCommitted, Commit: sha} }
	for _, c := range []struct {
		name   string
		l      model.Link
		want4  string
		target string
	}{
		{"working tree", model.Link{Target: model.LinkTarget{State: model.StateUnstaged}, Side: model.NoteSideNew}, "wt-4", "working tree"},
		{"unstaged old = index", model.Link{Target: model.LinkTarget{State: model.StateUnstaged}, Side: model.NoteSideOld}, "idx-4", "working tree"},
		{"staged new = index", model.Link{Target: model.LinkTarget{State: model.StateStaged}, Side: model.NoteSideNew}, "idx-4", "index"},
		{"staged old = HEAD", model.Link{Target: model.LinkTarget{State: model.StateStaged}, Side: model.NoteSideOld}, "c2-4", "index"},
		{"commit new", model.Link{Target: commit(c2), Side: model.NoteSideNew}, "c2-4", c2[:7]},
		{"commit old = parent", model.Link{Target: commit(c2), Side: model.NoteSideOld}, "l4", c2[:7]},
		{"pair old = a", model.Link{Target: model.LinkTarget{State: model.StateCommitted, Pair: &model.LinkPair{A: c1, B: c2}}, Side: model.NoteSideOld}, "l4", c1[:7] + ".." + c2[:7]},
		{"pair new = b", model.Link{Target: model.LinkTarget{State: model.StateCommitted, Pair: &model.LinkPair{A: c1, B: c2}}, Side: model.NoteSideNew}, "c2-4", c1[:7] + ".." + c2[:7]},
	} {
		l := c.l
		l.Path, l.Line, l.End = "f.txt", 3, 5
		lt := text(l)
		if len(lt.Lines) != 3 || lt.Lines[1] != c.want4 || lt.Start != 3 || lt.End != 5 || lt.Target != c.target || lt.Side != l.Side || lt.Path != "f.txt" {
			t.Errorf("%s: %+v", c.name, lt)
		}
	}
	// One line.
	lt := text(model.Link{Path: "f.txt", Line: 2, Side: model.NoteSideNew, Target: model.LinkTarget{State: model.StateUnstaged}})
	if len(lt.Lines) != 1 || lt.Lines[0] != "l2" || lt.Start != 2 || lt.End != 2 {
		t.Errorf("one line: %+v", lt)
	}
	// Past the end, no line.
	res, _ := ResolveLink(ctx, model.Link{Repo: model.LinkRepo{Name: "gigagit"}, Path: "f.txt", Line: 7, End: 12, Side: model.NoteSideNew, Target: commit(c2)}, ResolveOpts{Cwd: svc})
	if _, err := svc.LinkText(ctx, res); err == nil || !strings.Contains(err.Error(), "8 lines") {
		t.Errorf("past the end: %v", err)
	}
	res, _ = ResolveLink(ctx, model.Link{Repo: model.LinkRepo{Name: "gigagit"}, Path: "f.txt", Side: model.NoteSideNew, Target: commit(c2)}, ResolveOpts{Cwd: svc})
	if _, err := svc.LinkText(ctx, res); !errors.Is(err, ErrLinkNoLines) {
		t.Errorf("no line: %v", err)
	}
}

func rangeHead(t *testing.T, dir string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

// Trailing blank lines are lines of the diff, so a range may end on one.
func TestResolveRangeLinkEndingOnATrailingBlankLine(t *testing.T) {
	t.Parallel()
	_, svc, write := rangeRepo(t)
	write("a\nb\n\n")
	ctx := context.Background()
	fp := model.BlockFingerprint([]string{"b", ""})
	if got := svc.LinkBlockFingerprint(ctx, rangeLink(model.StateUnstaged, model.NoteSideNew, 2, 3, "")); got != fp {
		t.Fatalf("LinkBlockFingerprint = %q, want %q", got, fp)
	}
	if _, err := ResolveLink(ctx, rangeLink(model.StateUnstaged, model.NoteSideNew, 2, 3, fp), ResolveOpts{Cwd: svc}); err != nil {
		t.Errorf("lines 2-3 of a file ending in a blank line: %v", err)
	}
}

// The old side of a RENAMED file is its text at the old path — for a commit's
// own change and for a pair; a file with no old side says so plainly.
func TestLinkTextOldSideFollowsARename(t *testing.T) {
	t.Parallel()
	dir, svc, write := rangeRepo(t)
	ctx := context.Background()
	c1 := rangeHead(t, dir)
	runGitIn(t, dir, "mv", "f.txt", "g.txt")
	runGitIn(t, dir, "commit", "-m", "rename")
	c2 := rangeHead(t, dir)
	_ = write
	if err := os.WriteFile(filepath.Join(dir, "n.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitIn(t, dir, "add", "n.txt")
	runGitIn(t, dir, "commit", "-m", "add n")
	c3 := rangeHead(t, dir)
	old := func(target model.LinkTarget, path string) (LinkText, error) {
		res, err := ResolveLink(ctx, model.Link{Repo: model.LinkRepo{Name: "gigagit"}, Path: path, Side: model.NoteSideOld,
			Line: 3, End: 5, Target: target}, ResolveOpts{Cwd: svc})
		if err != nil {
			t.Fatal(err)
		}
		return svc.LinkText(ctx, res)
	}
	for name, target := range map[string]model.LinkTarget{
		"commit": {State: model.StateCommitted, Commit: c2},
		"pair":   {State: model.StateCommitted, Pair: &model.LinkPair{A: c1, B: c3}},
	} {
		lt, err := old(target, "g.txt")
		if err != nil || len(lt.Lines) != 3 || lt.Lines[0] != "l3" {
			t.Errorf("%s: %+v, %v", name, lt, err)
		}
	}
	_, err := old(model.LinkTarget{State: model.StateCommitted, Commit: c3}, "n.txt")
	if err == nil || !strings.Contains(err.Error(), "n.txt has no text on the old side of "+c3[:7]) {
		t.Errorf("an added file's old side: %v", err)
	}
}
