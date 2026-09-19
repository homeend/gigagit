package domain

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

// stashFixture is one tracked edit plus one untracked file (in a directory
// the seed commit does not have), stashed with -u. It returns the stash's
// first parent and its own sha, resolved through the verb under test.
func stashFixture(t *testing.T) (dir string, svc *Service, parent, sha string) {
	t.Helper()
	dir, svc = newRealRepo(t)
	writeIn(t, dir, "tracked.txt", "v1\n")
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-qm", "seed")
	writeIn(t, dir, "tracked.txt", "v2\n")
	if err := os.MkdirAll(filepath.Join(dir, "scratch"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeIn(t, dir, "scratch/notes.txt", "untracked\n")
	gitIn(t, dir, "stash", "push", "-u", "-m", "my wip")
	parent, sha, err := svc.StashPair(context.Background(), "stash@{0}")
	if err != nil {
		t.Fatal(err)
	}
	return dir, svc, parent, sha
}

func TestStashPairIsTwoFullShas(t *testing.T) {
	t.Parallel()
	dir, svc, parent, sha := stashFixture(t)
	if parent != headHashFull(t, dir) {
		t.Errorf("parent = %s, want HEAD", parent)
	}
	for _, s := range []string{parent, sha} {
		if len(s) < 40 || strings.ContainsAny(s, "@{}") {
			t.Errorf("%q is not a full sha", s)
		}
	}
	if _, _, err := svc.StashPair(context.Background(), "stash@{7}"); err == nil {
		t.Error("a stash that does not exist must error")
	}
}

func TestStashPairSetIncludesUntrackedFromTheThirdParent(t *testing.T) {
	t.Parallel()
	_, svc, parent, sha := stashFixture(t)
	ctx := context.Background()

	// THE TWO ARMS MUST DIFFER, asserted up front: the plain first-parent diff
	// does not contain the untracked file. If it ever does, the assertions
	// below prove nothing about the third parent.
	plain, err := svc.CompareFiles(ctx, mustTestCommit(t, parent), mustTestCommit(t, sha))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := statuses(plain)["scratch/notes.txt"]; ok {
		t.Fatal("fixture broken: a..b already lists the untracked file")
	}

	fs, err := svc.EvalEndpoint(ctx, mustTestPair(t, parent, sha))
	if err != nil {
		t.Fatal(err)
	}
	if got := fs.Paths(); !slices.Equal(got, []string{"scratch/notes.txt", "tracked.txt"}) {
		t.Fatalf("paths = %v", got)
	}
	if !fs.Has("scratch/notes.txt") {
		t.Error("the untracked member must have bytes")
	}
	if fs.Source("tracked.txt") != fs.Endpoint() {
		t.Error("a tracked member reads from b")
	}
	if fs.Source("scratch/notes.txt") == fs.Endpoint() {
		t.Error("the untracked member must read from the third parent")
	}
	b, err := svc.ResolveBytes(ctx, fs.Source("scratch/notes.txt").FileRef("scratch/notes.txt"))
	if err != nil || string(b) != "untracked\n" {
		t.Fatalf("bytes = %q, %v", b, err)
	}
}

// The comparison must READ the untracked member, which only happens when the
// path exists on BOTH sides — an A/D row is decided without a byte read and so
// cannot see where the bytes come from. The working tree gets the same path
// back with different content, which forces sameBytes through Source(path).
func TestComparingAStashSetReadsTheUntrackedMemberFromItsOwnSource(t *testing.T) {
	t.Parallel()
	dir, svc, parent, sha := stashFixture(t)
	ctx := context.Background()
	if err := os.MkdirAll(filepath.Join(dir, "scratch"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeIn(t, dir, "scratch/notes.txt", "rewritten since\n")

	left, err := svc.EvalEndpoint(ctx, mustTestPair(t, parent, sha))
	if err != nil {
		t.Fatal(err)
	}
	right, err := svc.EvalEndpoint(ctx, model.WorkTreeEndpoint())
	if err != nil {
		t.Fatal(err)
	}
	files, err := svc.CompareSets(ctx, left, right)
	if err != nil {
		t.Fatalf("CompareSets: %v", err) // reading b:scratch/notes.txt hard-errors: it is not in b's tree
	}
	st := statuses(files)
	if st["scratch/notes.txt"] != "M" || st["tracked.txt"] != "M" {
		t.Fatalf("statuses = %v, want both M", st)
	}
}

func TestStashSetAgainstItsParentReportsTheUntrackedFileAsAdded(t *testing.T) {
	t.Parallel()
	_, svc, parent, sha := stashFixture(t)
	ctx := context.Background()
	left, err := svc.EvalEndpoint(ctx, mustTestCommit(t, parent))
	if err != nil {
		t.Fatal(err)
	}
	right, err := svc.EvalEndpoint(ctx, mustTestPair(t, parent, sha))
	if err != nil {
		t.Fatal(err)
	}
	files, err := svc.CompareSets(ctx, left, right)
	if err != nil {
		t.Fatal(err)
	}
	st := statuses(files)
	if st["scratch/notes.txt"] != "A" || st["tracked.txt"] != "M" || len(st) != 2 {
		t.Fatalf("statuses = %v", st)
	}
}

// narrowTo REBUILDS the set from the endpoint, so the per-path source is the
// one thing it can silently drop.
func TestNarrowedStashLinkKeepsTheThirdParentSource(t *testing.T) {
	t.Parallel()
	_, svc, parent, sha := stashFixture(t)
	l, err := model.ParseLink("gg://r/scratch/notes.txt@" + parent + ".." + sha)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	fs, err := svc.EvalLink(ctx, l)
	if err != nil {
		t.Fatal(err)
	}
	if !fs.Has("scratch/notes.txt") || fs.Source("scratch/notes.txt") == fs.Endpoint() {
		t.Fatal("narrowTo dropped the per-path source")
	}
	b, err := svc.ResolveBytes(ctx, fs.Source("scratch/notes.txt").FileRef("scratch/notes.txt"))
	if err != nil || string(b) != "untracked\n" {
		t.Fatalf("bytes = %q, %v", b, err)
	}
}

// An octopus merge has three parents too. Its third parent is NOT a root, and
// treating it as a stash would pour that parent's whole tree into the set.
func TestOctopusMergeIsNotAStash(t *testing.T) {
	t.Parallel()
	dir, svc := newRealRepo(t)
	ctx := context.Background()
	writeIn(t, dir, "seed.txt", "seed\n")
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-qm", "seed")
	base := headHashFull(t, dir)
	for _, b := range []string{"b1", "b2", "b3"} {
		gitIn(t, dir, "checkout", "-q", "-b", b, base)
		writeIn(t, dir, b+".txt", b+"\n")
		gitIn(t, dir, "add", ".")
		gitIn(t, dir, "commit", "-qm", "on "+b)
	}
	gitIn(t, dir, "checkout", "-q", "b1")
	a := headHashFull(t, dir)
	gitIn(t, dir, "merge", "-q", "--no-edit", "b2", "b3")
	m := headHashFull(t, dir)
	parents, err := svc.commitParents(ctx, m)
	if err != nil || len(parents) != 3 || parents[0] != a {
		t.Fatalf("fixture broken: merge parents = %v, %v", parents, err)
	}

	fs, err := svc.EvalEndpoint(ctx, mustTestPair(t, a, m))
	if err != nil {
		t.Fatal(err)
	}
	// a..m adds b2.txt and b3.txt. b3's TREE also holds seed.txt — the file
	// that would leak in if the root test were dropped.
	if got := fs.Paths(); !slices.Equal(got, []string{"b2.txt", "b3.txt"}) {
		t.Fatalf("paths = %v, want exactly the merge's own change-set", got)
	}
}

// BOTH stash arms are described: the -u one (three parents) and a plain one
// (two parents — which an ordinary merge also has, hence the subject test).
func TestDescribeLinkNamesBothStashShapesButNotAMerge(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	_, svc, parent, sha := stashFixture(t)
	l, err := model.ParseLink("gg://r@" + parent + ".." + sha)
	if err != nil {
		t.Fatal(err)
	}
	if got := svc.DescribeLink(ctx, l); !strings.HasPrefix(got, "stash: On ") || !strings.HasSuffix(got, ": my wip") {
		t.Errorf("-u stash desc = %q", got)
	}

	dir, svc2 := newRealRepo(t)
	writeIn(t, dir, "t.txt", "v1\n")
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-qm", "seed")
	writeIn(t, dir, "t.txt", "v2\n")
	gitIn(t, dir, "stash", "push", "-m", "plain one") // NO -u: two parents
	pp, ps, err := svc2.StashPair(ctx, "stash@{0}")
	if err != nil {
		t.Fatal(err)
	}
	if parents, _ := svc2.commitParents(ctx, ps); len(parents) != 2 {
		t.Fatalf("fixture broken: a plain stash has %d parents", len(parents))
	}
	l, _ = model.ParseLink("gg://r@" + pp + ".." + ps)
	if got := svc2.DescribeLink(ctx, l); !strings.HasSuffix(got, ": plain one") || !strings.HasPrefix(got, "stash: ") {
		t.Errorf("plain stash desc = %q", got)
	}

	// A real two-parent merge with a as its first parent is NOT a stash.
	base := headHashFull(t, dir)
	gitIn(t, dir, "checkout", "-q", "-b", "side", base)
	writeIn(t, dir, "side.txt", "s\n")
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-qm", "side")
	gitIn(t, dir, "checkout", "-q", "-")
	writeIn(t, dir, "main.txt", "m\n")
	gitIn(t, dir, "add", "main.txt")
	gitIn(t, dir, "commit", "-qm", "mainline")
	a := headHashFull(t, dir)
	gitIn(t, dir, "merge", "-q", "--no-ff", "--no-edit", "side")
	m := headHashFull(t, dir)
	l, _ = model.ParseLink("gg://r@" + a + ".." + m)
	if got := svc2.DescribeLink(ctx, l); !strings.HasPrefix(got, "link: ") {
		t.Errorf("merge desc = %q, want the link: fallback", got)
	}
}

func headHashFull(t *testing.T, dir string) string {
	t.Helper()
	svc := Open(dir)
	sha, ok, err := svc.ResolveRev(context.Background(), "HEAD")
	if err != nil || !ok {
		t.Fatalf("HEAD: %v %v", ok, err)
	}
	return sha
}
