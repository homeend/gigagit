package domain

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/repos"
)

// compareLinksFixture is a checkout named "r" with two commits that BOTH
// change sub/a.go and sub/b.go, so a whole-tree comparison lists two files and
// a file link's one — the two arms differ, which is what lets a test see the
// narrowing. abs is the checkout spelled the way a local-form link holds it.
func compareLinksFixture(t *testing.T) (svc *Service, abs, c1, c2 string) {
	t.Helper()
	dir, svc := newRealRepo(t)
	gitIn(t, dir, "remote", "add", "origin", "git@github.com:homeend/r.git")
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeIn(t, dir, "sub/a.go", "package a // v1\n")
	writeIn(t, dir, "sub/b.go", "package b // v1\n")
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-qm", "c1")
	c1 = headHashFull(t, dir)
	writeIn(t, dir, "sub/a.go", "package a // v2\n")
	writeIn(t, dir, "sub/b.go", "package b // v2\n")
	gitIn(t, dir, "commit", "-qam", "c2")
	c2 = headHashFull(t, dir)
	top, err := svc.TopLevel(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return svc, linkAbs(top), c1, c2
}

// linkAbs spells a checkout path the way a local-form link holds it: slashes,
// and a leading one (a Windows drive path renders as gg:///C:/…).
func linkAbs(dir string) string {
	abs := filepath.ToSlash(dir)
	if !strings.HasPrefix(abs, "/") {
		abs = "/" + abs
	}
	return abs
}

func fileRows(files []model.CommitFile) []string {
	out := make([]string, 0, len(files))
	for _, f := range files {
		out = append(out, f.Status+" "+f.Path)
	}
	return out
}

func wantRows(t *testing.T, got []model.CommitFile, want ...string) {
	t.Helper()
	g := fileRows(got)
	if strings.Join(g, "|") != strings.Join(want, "|") {
		t.Fatalf("files = %v, want %v", g, want)
	}
}

// A parsed LOCAL-form file link holds the checkout and the file undivided in
// Repo.Abs. The door locates before it evaluates; skip that and the file link
// silently compares the whole tree.
func TestCompareLinksLocalFormFileLinkIsOneFile(t *testing.T) {
	t.Parallel()
	svc, abs, c1, c2 := compareLinksFixture(t)
	ctx := context.Background()
	whole, err := svc.CompareLinks(ctx, "gg://"+abs+"@"+c1, "gg://"+abs+"@"+c2, ResolveOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if len(whole.Files) != 2 {
		t.Fatalf("fixture: the whole-tree comparison lists %v, want 2 files — the arms must differ", fileRows(whole.Files))
	}
	c, err := svc.CompareLinks(ctx, "gg://"+abs+"/sub/a.go@"+c1, "gg://"+abs+"/sub/a.go@"+c2, ResolveOpts{})
	if err != nil {
		t.Fatal(err)
	}
	wantRows(t, c.Files, "M sub/a.go")
}

func TestCompareLinksNameFormAgreesWithLocalForm(t *testing.T) {
	t.Parallel()
	svc, _, c1, c2 := compareLinksFixture(t)
	c, err := svc.CompareLinks(context.Background(), "gg://r/sub/a.go@"+c1, "gg://r/sub/a.go@"+c2, ResolveOpts{})
	if err != nil {
		t.Fatal(err)
	}
	wantRows(t, c.Files, "M sub/a.go")
}

// Two files of ONE checkout are not two repositories. Comparing the unsplit
// Repo.Abs values says they are. On links a.go vs b.go is a D and an A, never
// a diff: the algebra is keyed by path (spec D8).
func TestCompareLinksTwoLocalFileLinksAreOneCheckout(t *testing.T) {
	t.Parallel()
	svc, abs, c1, c2 := compareLinksFixture(t)
	c, err := svc.CompareLinks(context.Background(), "gg://"+abs+"/sub/a.go@"+c1, "gg://"+abs+"/sub/b.go@"+c2, ResolveOpts{})
	if err != nil {
		t.Fatalf("two files of one checkout were refused: %v", err)
	}
	wantRows(t, c.Files, "D sub/a.go", "A sub/b.go")
}

// The refusal is only reachable when the OTHER checkout is known here: with no
// registry the link has no candidate at all and fails earlier, as unknown.
func TestCompareLinksRefusesAnotherCheckout(t *testing.T) {
	t.Parallel()
	svc, abs, c1, _ := compareLinksFixture(t)
	other, _ := newRealRepo(t)
	ctx := context.Background()
	left, right := "gg://"+abs+"@"+c1, "gg://"+linkAbs(other)+"/README.md"

	_, err := svc.CompareLinks(ctx, left, right, ResolveOpts{})
	if !errors.Is(err, ErrLinkUnknownRepo) {
		t.Fatalf("fixture: without a registry err = %v, want ErrLinkUnknownRepo — the arms must differ", err)
	}

	state := filepath.Join(t.TempDir(), "repos.toml")
	if err := repos.Touch(state, other, "", time.Unix(9000, 0)); err != nil {
		t.Fatal(err)
	}
	_, err = svc.CompareLinks(ctx, left, right, ResolveOpts{RegistryPath: state})
	if !errors.Is(err, ErrLinkCrossRepo) {
		t.Fatalf("err = %v, want ErrLinkCrossRepo", err)
	}
	if errors.Is(err, model.ErrLink) {
		t.Errorf("a well-formed link to another checkout must not read as a grammar error: %v", err)
	}
	var se *LinkSideError
	if !errors.As(err, &se) || se.Side != LinkSideRight {
		t.Errorf("err = %#v, want a LinkSideError naming the right side", err)
	}
}

func TestCompareLinksGrammarErrorNamesItsSide(t *testing.T) {
	t.Parallel()
	svc, abs, c1, _ := compareLinksFixture(t)
	good, bad := "gg://"+abs+"@"+c1, "gg://r@not a target"
	for _, c := range []struct {
		name        string
		left, right string
		want        LinkSide
	}{
		{"left", bad, good, LinkSideLeft},
		{"right", good, bad, LinkSideRight},
	} {
		_, err := svc.CompareLinks(context.Background(), c.left, c.right, ResolveOpts{})
		if !errors.Is(err, model.ErrLink) {
			t.Fatalf("%s: err = %v, want model.ErrLink through the wrapper", c.name, err)
		}
		var se *LinkSideError
		if !errors.As(err, &se) || se.Side != c.want {
			t.Errorf("%s: err = %#v, want side %v", c.name, err, c.want)
		}
	}
}

// The texts are the comparison's identity (the TUI's view tag, a saved row):
// a name-form link must not come back rewritten to the local form.
func TestCompareLinksKeepsTheTextsAsGiven(t *testing.T) {
	t.Parallel()
	svc, abs, c1, c2 := compareLinksFixture(t)
	left, right := "gg://r@"+c1, "gg://"+abs+"@"+c2
	c, err := svc.CompareLinks(context.Background(), "  "+left, right+"\n", ResolveOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if c.LeftText != left || c.RightText != right {
		t.Errorf("texts = %q, %q; want %q, %q", c.LeftText, c.RightText, left, right)
	}
}

func TestCompareLinksStashSetReadsItsOwnSource(t *testing.T) {
	t.Parallel()
	dir, svc, parent, sha := stashFixture(t)
	abs := linkAbs(dir)
	c, err := svc.CompareLinks(context.Background(), "gg://"+abs+"@"+parent, "gg://"+abs+"@"+parent+".."+sha, ResolveOpts{})
	if err != nil {
		t.Fatal(err)
	}
	wantRows(t, c.Files, "A scratch/notes.txt", "M tracked.txt")
	if c.Right.Source("scratch/notes.txt") == c.Right.Endpoint() {
		t.Error("the untracked member must keep its own byte source (the stash's third parent)")
	}
}
