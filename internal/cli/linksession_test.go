package cli

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/steer"
)

// A link naming the CWD's own checkout keeps the injected inbox dir, so this
// stays a parallel test. (Cross-checkout steering needs a real state home and
// is covered by the serial test in linksession_serial_test.go.)
func TestSessionNavigateWithALinkPostsTheSameCommandAsTheFlags(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	livePresence(t, dir)
	repo := newCLIRepo(t)
	svc := domain.Open(repo)

	var lout bytes.Buffer
	if code := runLink(linkState(t), svc, repo, []string{"README.md:2"}, &lout, os.Stderr); code != 0 {
		t.Fatalf("gg link: exit %d", code)
	}
	link := strings.TrimSpace(lout.String())

	var out, errb bytes.Buffer
	if code := runSession(dir, svc, []string{"navigate", link, "--no-wait"}, &out, &errb); code != 0 {
		t.Fatalf("exit = %d (stderr %q)", code, errb.String())
	}
	viaLink := steer.Drain(dir)
	if len(viaLink) != 1 {
		t.Fatalf("inbox = %+v, want one command", viaLink)
	}

	out.Reset()
	errb.Reset()
	if code := runSession(dir, svc, []string{"navigate", "--file", "README.md", "--new-line", "2", "--no-wait"}, &out, &errb); code != 0 {
		t.Fatalf("flag form: exit = %d (stderr %q)", code, errb.String())
	}
	viaFlags := steer.Drain(dir)
	if len(viaFlags) != 1 {
		t.Fatalf("inbox = %+v", viaFlags)
	}
	a, b := viaLink[0], viaFlags[0]
	a.ID, b.ID = "", "" // ids are per-post
	if a.File != b.File || a.Line == nil || b.Line == nil || *a.Line != *b.Line ||
		a.Target == nil || b.Target == nil || *a.Target != *b.Target || a.Cmd != b.Cmd {
		t.Errorf("link command %+v differs from the flag command %+v", a, b)
	}
}

func TestSessionHighlightAddWithALinkPostsTheSameCommandAsTheFlags(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	livePresence(t, dir)
	repo := newCLIRepo(t)
	svc := domain.Open(repo)

	var lout bytes.Buffer
	if code := runLink(linkState(t), svc, repo, []string{"README.md:2"}, &lout, os.Stderr); code != 0 {
		t.Fatalf("gg link: exit %d", code)
	}
	link := strings.TrimSpace(lout.String())

	var out, errb bytes.Buffer
	if code := runSession(dir, svc, []string{"highlight", "add", link, "--tone", "warn", "--no-wait"}, &out, &errb); code != 0 {
		t.Fatalf("exit = %d (stderr %q)", code, errb.String())
	}
	viaLink := steer.Drain(dir)
	if len(viaLink) != 1 {
		t.Fatalf("inbox = %+v, want one command", viaLink)
	}

	out.Reset()
	errb.Reset()
	if code := runSession(dir, svc, []string{"highlight", "add", "--file", "README.md", "--start", "2", "--tone", "warn", "--no-wait"}, &out, &errb); code != 0 {
		t.Fatalf("flag form: exit = %d (stderr %q)", code, errb.String())
	}
	viaFlags := steer.Drain(dir)
	if len(viaFlags) != 1 {
		t.Fatalf("inbox = %+v", viaFlags)
	}
	a, b := viaLink[0], viaFlags[0]
	a.ID, b.ID = "", "" // ids are per-post
	if a.File != b.File || a.Side != b.Side || a.Start != b.Start || a.End != b.End || a.Tone != b.Tone ||
		a.Target == nil || b.Target == nil || *a.Target != *b.Target || a.Cmd != b.Cmd {
		t.Errorf("link command %+v differs from the flag command %+v", a, b)
	}
}

func TestSessionLinkRejectsTheFlagsItReplaces(t *testing.T) {
	t.Parallel()
	repo := newCLIRepo(t)
	svc := domain.Open(repo)
	var lout bytes.Buffer
	_ = runLink(linkState(t), svc, repo, []string{"README.md:2"}, &lout, os.Stderr)
	link := strings.TrimSpace(lout.String())

	for _, args := range [][]string{
		{"navigate", link, "--file", "README.md"},
		{"navigate", link, "--new-line", "3"},
		{"navigate", link, "--cached"},
		{"navigate", link, "--rev", "HEAD"},
		{"navigate", link, "--hunk", "1"},
		{"highlight", "add", link, "--file", "README.md"},
		// Ruling P21: --side is a REPLACED flag (the link already carries a
		// side), not an orthogonal one.
		{"highlight", "add", link, "--side", "old"},
		// Ruling 12: a link's "-<end>" range suffix and --end name the same
		// thing (mixed = exit 2), and an explicit range that ends before it
		// starts is refused just like the flag form.
		{"highlight", "add", link + "-5", "--end", "7"},
		{"highlight", "add", link + "-1"},
	} {
		args := args
		t.Run(strings.Join(args[:2], " ")+" "+args[len(args)-2], func(t *testing.T) {
			t.Parallel()
			// Each subtest gets its OWN inbox: the assertion below drains it,
			// and a shared dir across parallel subtests would let one row's
			// bug be caught (or missed) by a different row's drain.
			dir := t.TempDir()
			livePresence(t, dir)
			var out, errb bytes.Buffer
			if code := runSession(dir, svc, args, &out, &errb); code != 2 {
				t.Errorf("exit = %d, want 2; stderr %q", code, errb.String())
			}
			if got := steer.Drain(dir); len(got) != 0 {
				t.Errorf("inbox = %+v, want nothing posted", got)
			}
		})
	}
}

func TestSessionHighlightAddTakesALinkRange(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	livePresence(t, dir)
	repo := newCLIRepo(t)
	svc := domain.Open(repo)
	var lout bytes.Buffer
	_ = runLink(linkState(t), svc, repo, []string{"README.md:2"}, &lout, os.Stderr)
	link := strings.TrimSpace(lout.String())

	var out, errb bytes.Buffer
	if code := runSession(dir, svc, []string{"highlight", "add", link + "-5", "--tone", "warn", "--no-wait"}, &out, &errb); code != 0 {
		t.Fatalf("exit = %d (stderr %q)", code, errb.String())
	}
	got := steer.Drain(dir)
	if len(got) != 1 {
		t.Fatalf("inbox = %+v", got)
	}
	c := got[0]
	if c.Cmd != "highlight" || c.Start != 2 || c.End != 5 || c.Side != "new" || c.Tone != "warn" || c.File != "README.md" {
		t.Errorf("posted %+v, want highlight README.md new:2-5 warn", c)
	}
}

// TestSessionHighlightAddTakesALinkHunk is controller ruling P22: a #<hunk>
// link is a range, not a bare line — `highlight add` must post the hunk's
// WHOLE span (Start/End), the same span `svc.HunkRange` (the verb `note add
// --hunk` and `gg diff --hunks` both key off) gives for that hunk number.
func TestSessionHighlightAddTakesALinkHunk(t *testing.T) {
	t.Parallel()
	dir, _, sha2 := linkTwoCommitFixture(t)
	inbox := t.TempDir()
	livePresence(t, inbox)
	svc := domain.Open(dir)
	ctx := context.Background()

	var lout bytes.Buffer
	if code := runLink(linkState(t), svc, dir, []string{"--rev", sha2, "README.md#1"}, &lout, os.Stderr); code != 0 {
		t.Fatalf("gg link: exit %d", code)
	}
	link := strings.TrimSpace(lout.String())

	spec, err := svc.HunkDiffSpec(ctx, false, sha2, []string{"README.md"})
	if err != nil {
		t.Fatalf("HunkDiffSpec: %v", err)
	}
	wantSide, wantRng, err := svc.HunkRange(ctx, spec, "README.md", 1)
	if err != nil {
		t.Fatalf("HunkRange: %v", err)
	}

	var out, errb bytes.Buffer
	if code := runSession(inbox, svc, []string{"highlight", "add", link, "--tone", "warn", "--no-wait"}, &out, &errb); code != 0 {
		t.Fatalf("exit = %d (stderr %q)", code, errb.String())
	}
	got := steer.Drain(inbox)
	if len(got) != 1 {
		t.Fatalf("inbox = %+v, want one command", got)
	}
	c := got[0]
	if c.Cmd != "highlight" || c.File != "README.md" || c.Side != string(wantSide) ||
		c.Start != wantRng[0] || c.End != wantRng[1] || c.Tone != "warn" {
		t.Errorf("posted %+v, want side=%s start=%d end=%d", c, wantSide, wantRng[0], wantRng[1])
	}
}

// TestSessionHighlightAddHunkLinkRejectsEndFlag: a #<hunk> link already
// names a range, so --end alongside it is a usage error (ruling P22),
// exactly like mixing --start with any link.
func TestSessionHighlightAddHunkLinkRejectsEndFlag(t *testing.T) {
	t.Parallel()
	dir, _, sha2 := linkTwoCommitFixture(t)
	inbox := t.TempDir()
	livePresence(t, inbox)
	svc := domain.Open(dir)

	var lout bytes.Buffer
	if code := runLink(linkState(t), svc, dir, []string{"--rev", sha2, "README.md#1"}, &lout, os.Stderr); code != 0 {
		t.Fatalf("gg link: exit %d", code)
	}
	link := strings.TrimSpace(lout.String())

	var out, errb bytes.Buffer
	if code := runSession(inbox, svc, []string{"highlight", "add", link, "--end", "5", "--no-wait"}, &out, &errb); code != 2 {
		t.Fatalf("exit = %d, want 2; stderr %q", code, errb.String())
	}
	if got := steer.Drain(inbox); len(got) != 0 {
		t.Errorf("inbox = %+v, want nothing posted", got)
	}
}

func TestSessionHighlightAddLinkWithBogusToneExitsTwoAndPostsNothing(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	livePresence(t, dir)
	repo := newCLIRepo(t)
	svc := domain.Open(repo)
	var lout bytes.Buffer
	_ = runLink(linkState(t), svc, repo, []string{"README.md:2"}, &lout, os.Stderr)
	link := strings.TrimSpace(lout.String())

	var out, errb bytes.Buffer
	if code := runSession(dir, svc, []string{"highlight", "add", link, "--tone", "bogus", "--no-wait"}, &out, &errb); code != 2 {
		t.Fatalf("exit = %d, want 2; stderr %q", code, errb.String())
	}
	if got := steer.Drain(dir); len(got) != 0 {
		t.Errorf("inbox = %+v, want nothing posted", got)
	}
}

func TestSplitLinkRange(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		link string
		end  int
	}{
		{"gg://r/a.go:40-46", "gg://r/a.go:40", 46},
		{"gg://r/a.go:40", "gg://r/a.go:40", 0},
		{"gg://r/a.go", "gg://r/a.go", 0},
		// A dash that is not in the last ":" segment belongs to the path.
		{"gg://r/my-file.go:7", "gg://r/my-file.go:7", 0},
		{"gg://r/my-file.go", "gg://r/my-file.go", 0},
		{"gg:///mnt/a-b/c.go:3-9", "gg:///mnt/a-b/c.go:3", 9},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()
			link, end := splitLinkRange(tc.in)
			if link != tc.link || end != tc.end {
				t.Errorf("splitLinkRange(%q) = %q, %d; want %q, %d", tc.in, link, end, tc.link, tc.end)
			}
		})
	}
}
