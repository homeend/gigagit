package domain

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/gittest"
	"github.com/homeend/gigagit/internal/model"
)

// gitOut runs one git command in dir and returns its trimmed stdout.
// gittest.Run returns nothing (it only asserts the exit status), and the
// three-dot test below has to compare against what `git merge-base` actually
// says — the whole point of that assertion is that the base is git's answer,
// not one this package computed twice.
func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return strings.TrimSpace(string(out))
}

// EvalLink is where the spec's §3.3 rule 1 lives: COMPARE IGNORES THE HINT.
// A bookmarked commit and the same commit picked off the log are ONE endpoint
// — that is what keeps the matrix a 2×2 instead of a bookmark × shelf ×
// commit grid.
func TestEvalLinkIgnoresTheHint(t *testing.T) {
	t.Parallel()
	f := newCompareFixture(t)
	ctx := context.Background()

	plain, err := model.ParseLink("gg://x@" + f.c2)
	if err != nil {
		t.Fatal(err)
	}
	hinted, err := model.ParseLink("gg://x@" + f.c2 + "?bookmark=whatever")
	if err != nil {
		t.Fatal(err)
	}
	a, err := f.svc.EvalLink(ctx, plain)
	if err != nil {
		t.Fatal(err)
	}
	b, err := f.svc.EvalLink(ctx, hinted)
	if err != nil {
		t.Fatal(err)
	}
	if a.Endpoint() != b.Endpoint() || a.Bounded() != b.Bounded() {
		t.Fatalf("the hint changed the endpoint: %+v vs %+v", a.Endpoint(), b.Endpoint())
	}
}

// The ONE exception to rule 1: a shelf hint on a link with NO address. The
// shelved bytes were never in git, so there the hint is the only CONTENT
// source and the endpoint is the shelf, not the working tree. Asserted on the
// endpoint alone — EvalEndpoint would need a shelf store, which this fixture
// has no reason to stand up.
func TestEndpointForLinkShelfHintIsTheOnlyContentSource(t *testing.T) {
	t.Parallel()
	f := newCompareFixture(t)
	ctx := context.Background()

	shelved, err := model.ParseLink("gg://x/a.txt?shelf=entry7")
	if err != nil {
		t.Fatal(err)
	}
	ep, err := f.svc.EndpointForLink(ctx, shelved)
	if err != nil {
		t.Fatal(err)
	}
	if ep.Kind() != model.EndpointShelf || ep.ShelfID() != "entry7" {
		t.Fatalf("an address-less shelf link must be a shelf endpoint, got kind=%d id=%q", ep.Kind(), ep.ShelfID())
	}

	// The same link with a bookmark hint keeps the working tree: a bookmark
	// is a landing, never a content source.
	marked, err := model.ParseLink("gg://x/a.txt?bookmark=entry7")
	if err != nil {
		t.Fatal(err)
	}
	ep, err = f.svc.EndpointForLink(ctx, marked)
	if err != nil {
		t.Fatal(err)
	}
	if ep.Kind() != model.EndpointWorkTree {
		t.Fatalf("a bookmark hint must not move the endpoint, got kind=%d", ep.Kind())
	}

	// And a shelf hint on a link that DOES address a commit stays the commit:
	// the exception is for links with no address at all.
	addressed, err := model.ParseLink("gg://x@" + f.c2 + "?shelf=entry7")
	if err != nil {
		t.Fatal(err)
	}
	ep, err = f.svc.EndpointForLink(ctx, addressed)
	if err != nil {
		t.Fatal(err)
	}
	if ep.Kind() != model.EndpointCommit || ep.Hash() != f.c2 {
		t.Fatalf("a shelf hint on an ADDRESSED link must be ignored, got kind=%d hash=%q", ep.Kind(), ep.Hash())
	}
}

// A /<path> makes ANY link bounded to exactly one member (spec §3.2's last
// grammar row). This is the row that makes "compare one file against a
// commit" work without a special case.
func TestEvalLinkWithAPathIsBoundedToOne(t *testing.T) {
	t.Parallel()
	f := newCompareFixture(t)
	l, err := model.ParseLink("gg://x/a.txt@" + f.c2)
	if err != nil {
		t.Fatal(err)
	}
	fs, err := f.svc.EvalLink(context.Background(), l)
	if err != nil {
		t.Fatal(err)
	}
	if !fs.Bounded() || len(fs.Paths()) != 1 || fs.Paths()[0] != "a.txt" {
		t.Fatalf("a path link must bound the set to that one path, got bounded=%v paths=%v", fs.Bounded(), fs.Paths())
	}
}

// A /<path> naming a file that does not EXIST at the target is still a legal
// bounded set — it is bounded to one member with no bytes — and comparing it
// reports A or D rather than failing to read the file (ruling R6).
func TestEvalLinkWithAnAbsentPath(t *testing.T) {
	t.Parallel()
	f := newCompareFixture(t)
	ctx := context.Background()
	// b.txt exists at c2 and was deleted at c3.
	gone, err := model.ParseLink("gg://x/b.txt@" + f.c3)
	if err != nil {
		t.Fatal(err)
	}
	there, err := model.ParseLink("gg://x/b.txt@" + f.c2)
	if err != nil {
		t.Fatal(err)
	}
	ls, err := f.svc.EvalLink(ctx, there)
	if err != nil {
		t.Fatal(err)
	}
	rs, err := f.svc.EvalLink(ctx, gone)
	if err != nil {
		t.Fatalf("a link to an absent path must evaluate, not error: %v", err)
	}
	got, err := f.svc.CompareSets(ctx, ls, rs)
	if err != nil {
		t.Fatalf("comparing against an absent path must not error: %v", err)
	}
	if len(got) != 1 || got[0].Path != "b.txt" || got[0].Status != "D" {
		t.Fatalf("got %v, want exactly b.txt D", got)
	}
}

// Narrowing a link that addresses a LIVE point: the working tree is unbounded,
// so the one member's presence is a stat, and a path that is not on disk is
// bounded-with-no-bytes rather than an error.
func TestEvalLinkNarrowsALivePoint(t *testing.T) {
	t.Parallel()
	f := newCompareFixture(t)
	ctx := context.Background()

	// a.txt is on disk (c3 left it there); b.txt is not (c3 deleted it).
	here, err := model.ParseLink("gg://x/a.txt")
	if err != nil {
		t.Fatal(err)
	}
	fs, err := f.svc.EvalLink(ctx, here)
	if err != nil {
		t.Fatal(err)
	}
	if !fs.Bounded() || fs.Endpoint().Kind() != model.EndpointWorkTree || !fs.Has("a.txt") {
		t.Fatalf("a.txt is on disk: bounded=%v kind=%d has=%v", fs.Bounded(), fs.Endpoint().Kind(), fs.Has("a.txt"))
	}
	absent, err := model.ParseLink("gg://x/b.txt")
	if err != nil {
		t.Fatal(err)
	}
	fs, err = f.svc.EvalLink(ctx, absent)
	if err != nil {
		t.Fatalf("a path that is not on disk must still evaluate: %v", err)
	}
	if !fs.Bounded() || fs.Has("b.txt") {
		t.Fatalf("b.txt was deleted at c3: bounded=%v has=%v", fs.Bounded(), fs.Has("b.txt"))
	}
}

// A ref link is a POINT — the whole tree at that tip — and EvalEndpoint is the
// only place the moving name may die: the set's endpoint is a COMMIT, never
// an EndpointRef (plan 1b ruling R2).
func TestEvalLinkRefResolvesToACommit(t *testing.T) {
	t.Parallel()
	f := newCompareFixture(t)
	l, err := model.ParseLink("gg://x@ref:main")
	if err != nil {
		t.Fatal(err)
	}
	fs, err := f.svc.EvalLink(context.Background(), l)
	if err != nil {
		t.Fatal(err)
	}
	if fs.Bounded() {
		t.Fatalf("a ref is a point, so its set is UNBOUNDED; got paths=%v", fs.Paths())
	}
	if fs.Endpoint().Kind() != model.EndpointCommit || fs.Endpoint().Hash() != f.c3 {
		t.Fatalf("a ref must resolve to its tip commit, got kind=%d hash=%q want %s",
			fs.Endpoint().Kind(), fs.Endpoint().Hash(), f.c3)
	}
}

// A two-dot pair link is BOUNDED to the change-set, and its halves may be
// refnames — which EndpointForLink resolves to FULL shas, because
// model.PairEndpoint refuses anything else.
func TestEvalLinkPairIsBoundedToTheChangeSet(t *testing.T) {
	t.Parallel()
	f := newCompareFixture(t)
	ctx := context.Background()
	l, err := model.ParseLink("gg://x@" + f.c1 + "..main")
	if err != nil {
		t.Fatal(err)
	}
	ep, err := f.svc.EndpointForLink(ctx, l)
	if err != nil {
		t.Fatal(err)
	}
	if ep.Kind() != model.EndpointPair || ep.PairA() != f.c1 || ep.PairB() != f.c3 {
		t.Fatalf("pair = kind %d %q..%q, want a pair %s..%s", ep.Kind(), ep.PairA(), ep.PairB(), f.c1, f.c3)
	}
	fs, err := f.svc.EvalLink(ctx, l)
	if err != nil {
		t.Fatal(err)
	}
	// c1..c3 touches a.txt (M) and b.txt (added at c2, deleted at c3 — so it
	// is NOT in the c1..c3 change-set at all).
	if !fs.Bounded() || len(fs.Paths()) != 1 || fs.Paths()[0] != "a.txt" {
		t.Fatalf("c1..main = bounded=%v paths=%v, want exactly [a.txt]", fs.Bounded(), fs.Paths())
	}
}

// A three-dot preview link resolves to the merge base, exactly as the shipped
// preview vocabulary does: merge-base(target, source)..source. The endpoint is
// a PAIR of resolved shas — never two branch names (plan 1b ruling R1).
func TestEvalLinkThreeDotResolvesToTheMergeBase(t *testing.T) {
	t.Parallel()
	f := newCompareFixture(t)
	ctx := context.Background()

	// main is at c3. Branch "topic" off c1 and put one commit on top, so
	// merge-base(main, topic) is c1 and neither tip is an ancestor of the
	// other.
	gittest.Run(t, f.dir, "checkout", "-b", "topic", f.c1)
	if err := os.WriteFile(filepath.Join(f.dir, "t.txt"), []byte("topic\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gittest.Run(t, f.dir, "add", "t.txt")
	gittest.Run(t, f.dir, "commit", "-m", "topic tip")
	topic := gitOut(t, f.dir, "rev-parse", "HEAD")
	gittest.Run(t, f.dir, "checkout", "main")

	l, err := model.ParseLink("gg://x@main...topic")
	if err != nil {
		t.Fatal(err)
	}
	ep, err := f.svc.EndpointForLink(ctx, l)
	if err != nil {
		t.Fatal(err)
	}
	if ep.Kind() != model.EndpointPair {
		t.Fatalf("a preview link is a PAIR endpoint, got kind %d", ep.Kind())
	}
	// Two independent sources for the same answer: the fixture knows the base
	// is c1, and git is asked for it directly. Agreeing with only one of them
	// would let a resolver that returns, say, the target tip pass.
	base := gitOut(t, f.dir, "merge-base", "main", "topic")
	if ep.PairA() != base {
		t.Fatalf("PairA = %q, want `git merge-base main topic` = %q", ep.PairA(), base)
	}
	if ep.PairA() != f.c1 {
		t.Fatalf("PairA = %q, want the fixture's c1 = %q", ep.PairA(), f.c1)
	}
	if ep.PairB() != topic {
		t.Fatalf("PairB = %q, want topic's tip %q", ep.PairB(), topic)
	}

	// The set is the preview's change-set: t.txt alone (a.txt differs between
	// the two TIPS, but not between the base and topic).
	fs, err := f.svc.EvalLink(ctx, l)
	if err != nil {
		t.Fatal(err)
	}
	if !fs.Bounded() || len(fs.Paths()) != 1 || fs.Paths()[0] != "t.txt" {
		t.Fatalf("preview set = bounded=%v paths=%v, want exactly [t.txt]", fs.Bounded(), fs.Paths())
	}

	// A preview whose source is already MERGED has merge-base == source, and
	// model.PairEndpoint calls a == b legal (ruling R7): the link still
	// evaluates, to the EMPTY change-set, rather than erroring.
	gittest.Run(t, f.dir, "branch", "merged", "main")
	ml, err := model.ParseLink("gg://x@main...merged")
	if err != nil {
		t.Fatal(err)
	}
	mep, err := f.svc.EndpointForLink(ctx, ml)
	if err != nil {
		t.Fatalf("a fully merged preview must still evaluate: %v", err)
	}
	if mep.PairA() != mep.PairB() || mep.PairA() != f.c3 {
		t.Fatalf("a merged preview = %q..%q, want %s..%s", mep.PairA(), mep.PairB(), f.c3, f.c3)
	}
	mfs, err := f.svc.EvalLink(ctx, ml)
	if err != nil {
		t.Fatal(err)
	}
	if !mfs.Bounded() || len(mfs.Paths()) != 0 {
		t.Fatalf("a merged preview is the EMPTY bounded set, got bounded=%v paths=%v", mfs.Bounded(), mfs.Paths())
	}
}

// Unrelated histories have no merge base, and a preview link over them
// surfaces domain's existing ErrNoMergeBase rather than a new sentinel: the
// question ("what is merge-base(a, b)") is the same one CompareOrigins asks,
// so callers keep one errors.Is target.
func TestEvalLinkThreeDotWithoutACommonAncestor(t *testing.T) {
	t.Parallel()
	f := newCompareFixture(t)

	// An orphan branch shares no history with main.
	gittest.Run(t, f.dir, "checkout", "--orphan", "alien")
	gittest.Run(t, f.dir, "rm", "-rf", ".")
	if err := os.WriteFile(filepath.Join(f.dir, "alien.txt"), []byte("alien\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gittest.Run(t, f.dir, "add", "alien.txt")
	gittest.Run(t, f.dir, "commit", "-m", "alien")
	gittest.Run(t, f.dir, "checkout", "main")

	l, err := model.ParseLink("gg://x@main...alien")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.EndpointForLink(context.Background(), l); err == nil {
		t.Fatal("two unrelated histories have no preview; want an error")
	} else if !strings.Contains(err.Error(), "no common ancestor") {
		t.Fatalf("want ErrNoMergeBase, got %v", err)
	}
}

// A link that addresses nothing comparable is refused, not silently read as
// some default endpoint.
func TestEndpointForLinkRefusesAnUnknownRevision(t *testing.T) {
	t.Parallel()
	f := newCompareFixture(t)
	l, err := model.ParseLink("gg://x@nosuchbranch..main")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.EndpointForLink(context.Background(), l); err == nil {
		t.Fatal("an unresolvable pair half must be an error")
	}
}

// The worked examples of spec §3.6, end to end: a link on each side, the lane
// they land in, and the result.
func TestCompareTwoLinks(t *testing.T) {
	t.Parallel()
	f := newCompareFixture(t)
	ctx := context.Background()
	left, err := model.ParseLink("gg://x/a.txt@" + f.c1)
	if err != nil {
		t.Fatal(err)
	}
	right, err := model.ParseLink("gg://x@" + f.c3)
	if err != nil {
		t.Fatal(err)
	}
	ls, err := f.svc.EvalLink(ctx, left)
	if err != nil {
		t.Fatal(err)
	}
	rs, err := f.svc.EvalLink(ctx, right)
	if err != nil {
		t.Fatal(err)
	}
	got, err := f.svc.CompareSets(ctx, ls, rs)
	if err != nil {
		t.Fatal(err)
	}
	// bounded (one file) × unbounded (a tree): the result is keyed on a.txt
	// alone, and a.txt's bytes differ between c1 and c3.
	if len(got) != 1 || got[0].Path != "a.txt" || got[0].Status != "M" {
		t.Fatalf("got %v, want exactly a.txt M", got)
	}
}
