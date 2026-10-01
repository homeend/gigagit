package domain

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

func TestAnchorLine(t *testing.T) {
	t.Parallel()
	fp := model.LineFingerprint
	lines := []string{"alpha", "}", "beta", "}", "gamma", "}"}
	for _, c := range []struct {
		name    string
		asked   int
		fp      string
		line    int
		state   string
		matches int
	}{
		{"same", 3, fp("beta"), 3, AnchorSame, 1},
		{"moved, one match", 1, fp("gamma"), 5, AnchorMoved, 1},
		{"moved, nearest of several", 5, fp("}"), 4, AnchorMoved, 3},
		{"a tie picks the lower line", 3, fp("}"), 2, AnchorMoved, 3},
		{"same wins over other matches", 4, fp("}"), 4, AnchorSame, 3},
		{"changed", 3, fp("delta"), 3, AnchorChanged, 0},
		{"asked past the end, text found", 40, fp("alpha"), 1, AnchorMoved, 1},
		{"asked past the end, text gone", 40, fp("delta"), 40, AnchorChanged, 0},
	} {
		line, state, matches := anchorLine(lines, c.asked, c.fp)
		if line != c.line || state != c.state || matches != c.matches {
			t.Errorf("%s: anchorLine = %d,%q,%d want %d,%q,%d", c.name, line, state, matches, c.line, c.state, c.matches)
		}
	}
}

func TestAnchorNote(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		asked, line, matches int
		state, want          string
	}{
		{33, 33, 1, AnchorSame, ""},
		{33, 33, 0, "", ""},
		{33, 41, 1, AnchorMoved, "line 33 moved to 41"},
		{33, 41, 3, AnchorMoved, "line 33 moved to 41 (nearest of 3 matching lines)"},
		{33, 33, 0, AnchorChanged, "line 33 has changed since this link was copied"},
	} {
		if got := AnchorNote(c.asked, c.line, c.state, c.matches); got != c.want {
			t.Errorf("AnchorNote(%d,%d,%q,%d) = %q, want %q", c.asked, c.line, c.state, c.matches, got, c.want)
		}
	}
}

// The side decides WHICH text is checked: the working file, the index or HEAD.
func TestResolveLinkReanchorsOnTheRightText(t *testing.T) {
	t.Parallel()
	dir := linkRepoWithRemote(t, "gigagit")
	write := func(body string) {
		if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("alpha\nbeta\ngamma\n")
	runGitIn(t, dir, "add", "f.txt")
	runGitIn(t, dir, "commit", "-m", "c1") // HEAD: beta at 2
	write("new\nalpha\nbeta\ngamma\n")
	runGitIn(t, dir, "add", "f.txt")                    // index: beta at 3
	write("new\r\nnewer\r\nalpha\r\nbeta\r\ngamma\r\n") // working file (CRLF): beta at 4
	fp := model.LineFingerprint("beta")
	svc := Open(dir)
	for _, c := range []struct {
		name string
		l    model.Link
		line int
	}{
		{"working file", model.Link{Path: "f.txt", Side: model.NoteSideNew, Target: model.LinkTarget{State: model.StateUnstaged}}, 4},
		{"unstaged old side = index", model.Link{Path: "f.txt", Side: model.NoteSideOld, Target: model.LinkTarget{State: model.StateUnstaged}}, 3},
		{"staged new side = index", model.Link{Path: "f.txt", Side: model.NoteSideNew, Target: model.LinkTarget{State: model.StateStaged}}, 3},
		{"staged old side = HEAD", model.Link{Path: "f.txt", Side: model.NoteSideOld, Target: model.LinkTarget{State: model.StateStaged}}, 2},
		{"content link", model.Link{Path: "f.txt", Side: model.NoteSideNew, Hint: model.ContentHint, Target: model.LinkTarget{State: model.StateUnstaged}}, 4},
	} {
		l := c.l
		l.Line, l.Fingerprint = 1, fp // copied when beta was line 1
		l.Repo.Name = "gigagit"
		res, err := ResolveLink(context.Background(), l, ResolveOpts{Cwd: svc})
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if res.Line != c.line || res.Anchor.State != AnchorMoved || res.Anchor.Asked != 1 {
			t.Errorf("%s: line=%d anchor=%+v, want line %d moved from 1", c.name, res.Line, res.Anchor, c.line)
		}
	}
	// No fingerprint: untouched, no read, no anchor.
	l := model.Link{Repo: model.LinkRepo{Name: "gigagit"}, Path: "f.txt", Side: model.NoteSideNew, Line: 1, Target: model.LinkTarget{State: model.StateUnstaged}}
	res, err := ResolveLink(context.Background(), l, ResolveOpts{Cwd: svc})
	if err != nil || res.Line != 1 || res.Anchor.State != "" {
		t.Errorf("plain link: line=%d anchor=%+v err=%v", res.Line, res.Anchor, err)
	}
	// The file is gone: changed, at the asked line, no error.
	os.Remove(filepath.Join(dir, "f.txt"))
	l.Fingerprint = fp
	res, err = ResolveLink(context.Background(), l, ResolveOpts{Cwd: svc})
	if err != nil || res.Line != 1 || res.Anchor.State != AnchorChanged {
		t.Errorf("deleted file: line=%d anchor=%+v err=%v", res.Line, res.Anchor, err)
	}
}
