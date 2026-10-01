package domain

import (
	"context"
	"os"
	"path/filepath"
	"strings"
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

// A staged RENAME's old side is HEAD's text at the OLD path: the link names
// the new path, so the resolver has to follow the rename or every such link
// would read "changed" the moment it was copied.
func TestResolveLinkStagedRenameOldSideReadsTheOldPath(t *testing.T) {
	t.Parallel()
	dir := linkRepoWithRemote(t, "gigagit")
	if err := os.WriteFile(filepath.Join(dir, "old.txt"), []byte("alpha\nbeta\ngamma\ndelta\nepsilon\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitIn(t, dir, "add", "old.txt")
	runGitIn(t, dir, "commit", "-m", "c1")
	runGitIn(t, dir, "mv", "old.txt", "new.txt")
	svc := Open(dir)
	l := model.Link{Repo: model.LinkRepo{Name: "gigagit"}, Path: "new.txt", Side: model.NoteSideOld, Line: 2,
		Target: model.LinkTarget{State: model.StateStaged}, Fingerprint: model.LineFingerprint("beta")}
	res, err := ResolveLink(context.Background(), l, ResolveOpts{Cwd: svc})
	if err != nil {
		t.Fatal(err)
	}
	if res.Line != 2 || res.Anchor.State != AnchorSame {
		t.Errorf("line=%d anchor=%+v, want line 2 same (HEAD:old.txt)", res.Line, res.Anchor)
	}
	// The producer reads the same text.
	l.Fingerprint = ""
	if got := svc.LinkLineFingerprint(context.Background(), l); got != model.LineFingerprint("beta") {
		t.Errorf("LinkLineFingerprint = %q, want beta's", got)
	}
}

// The resolver reads what the DIFF shows: a file the diff treats as text (a
// NUL only past git's 8000-byte window) keeps its fingerprints, one it calls
// binary or too large does not.
func TestLinkSideLinesAgreesWithTheDiffOnBinaryAndSize(t *testing.T) {
	t.Parallel()
	dir := linkRepoWithRemote(t, "gigagit")
	svc := Open(dir)
	ctx := context.Background()
	link := func(path string) model.Link {
		return model.Link{Repo: model.LinkRepo{Name: "gigagit"}, Path: path, Side: model.NoteSideNew, Line: 1,
			Target: model.LinkTarget{State: model.StateUnstaged}}
	}
	write := func(name string, body []byte) {
		if err := os.WriteFile(filepath.Join(dir, name), body, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	pad := strings.Repeat("x", 99) + "\n"

	// A stray NUL beyond the first 8000 bytes: text, as far as the diff goes.
	write("late.txt", []byte("alpha\n"+strings.Repeat(pad, 100)+"nul\x00here\n"))
	if got := svc.LinkLineFingerprint(ctx, link("late.txt")); got != model.LineFingerprint("alpha") {
		t.Errorf("late NUL: LinkLineFingerprint = %q, want alpha's", got)
	}
	l := link("late.txt")
	l.Fingerprint = model.LineFingerprint("alpha")
	if res, err := ResolveLink(ctx, l, ResolveOpts{Cwd: svc}); err != nil || res.Anchor.State != AnchorSame {
		t.Errorf("late NUL: anchor=%+v err=%v, want same", res.Anchor, err)
	}

	// A NUL inside that window: binary. No fingerprint; a link that has one is "changed".
	write("bin.dat", []byte("alpha\n\x00\x01\x02\n"))
	if got := svc.LinkLineFingerprint(ctx, link("bin.dat")); got != "" {
		t.Errorf("binary: LinkLineFingerprint = %q, want none", got)
	}
	l = link("bin.dat")
	l.Fingerprint = model.LineFingerprint("alpha")
	if res, err := ResolveLink(ctx, l, ResolveOpts{Cwd: svc}); err != nil || res.Anchor.State != AnchorChanged || res.Line != 1 {
		t.Errorf("binary: line=%d anchor=%+v err=%v, want changed at 1", res.Line, res.Anchor, err)
	}

	// Over the diff's size cap: not scanned at all.
	big := make([]byte, 0, MaxDiffBytes+200)
	big = append(big, "alpha\n"...)
	for len(big) <= MaxDiffBytes {
		big = append(big, pad...)
	}
	write("big.txt", big)
	if got := svc.LinkLineFingerprint(ctx, link("big.txt")); got != "" {
		t.Errorf("too large: LinkLineFingerprint = %q, want none", got)
	}
	l = link("big.txt")
	l.Fingerprint = model.LineFingerprint("alpha")
	if res, err := ResolveLink(ctx, l, ResolveOpts{Cwd: svc}); err != nil || res.Anchor.State != AnchorChanged || res.Line != 1 {
		t.Errorf("too large: line=%d anchor=%+v err=%v, want changed at 1", res.Line, res.Anchor, err)
	}
}
