package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

// linkShapeFixture builds a one-commit repo and returns a REF link (the
// branch tip "main", a POINT) and a PAIR link (head..head, a bounded
// CHANGE-SET) into it — the two shapes Task 2 taught domain.ResolveLink to
// hand back to every consumer at once. Both links carry the SAME file+line
// so a verb with its own unrelated usage check (session highlight add needs
// a file and a line/hunk before it ever looks at the shape) sees one on
// either shape alike; the row under test is the shape gate, not that check.
func linkShapeFixture(t *testing.T) (dir, refLink, pairLink string) {
	t.Helper()
	dir = newCLIRepo(t)
	head := runGit(t, dir, "rev-parse", "HEAD")
	base := model.Link{
		Repo: model.LinkRepo{Abs: filepath.ToSlash(dir)},
		Path: "README.md",
		Line: 1,
	}
	ref := base
	ref.Target = model.LinkTarget{State: model.StateCommitted, Ref: "main"}
	pair := base
	pair.Target = model.LinkTarget{State: model.StateCommitted, Pair: &model.LinkPair{A: head, B: head}}
	return dir, ref.String(), pair.String()
}

// linkShapeRefused reports whether a verb's run against one of the two links
// hit ruling R4's shape gate specifically: exit 2 AND a refusal that names
// `gg compare`, as opposed to exiting non-zero for some UNRELATED reason
// (no live gg session, a missing launcher, …) that every one of these six
// verbs can also hit with nothing live behind them in a test.
func linkShapeRefused(code int, errb string) bool {
	return code == 2 && strings.Contains(errb, "gg compare")
}

// TestLinkShapesPerVerb is ruling R4 as a table. A pair's only single commit
// is B, and a `gg show` there would silently widen a BOUNDED change-set into
// the whole tree at B — the same mistake domain.ComparePatchSets avoids per side
// in `gg compare --patch`. Every verb accepts a ref (a tip is one commit);
// `gg show` alone refuses a pair. The note verbs and highlight USED to refuse
// it too; since pair notes (2026-09-20) a change-set is a note scope — notes
// gathered along a..b, written to b's new side — so they take it.
func TestLinkShapesPerVerb(t *testing.T) {
	t.Parallel()
	dir, refLink, pairLink := linkShapeFixture(t)

	cases := []struct {
		verb          []string
		refOK, pairOK bool
	}{
		{[]string{"diff"}, true, true},
		{[]string{"show"}, true, false},
		{[]string{"note", "list"}, true, true},
		{[]string{"open"}, true, true},
		{[]string{"session", "navigate"}, true, true},
		{[]string{"session", "highlight", "add"}, true, true},
	}

	run := func(t *testing.T, verb []string, link string, ok bool) {
		t.Helper()
		args := append(append([]string{}, verb...), link)
		code, _, errb := runCLI(t, dir, args...)
		refused := linkShapeRefused(code, errb)
		if refused == ok {
			t.Fatalf("gg %s <link>: exit=%d stderr=%q — refused=%v, want refused=%v",
				strings.Join(verb, " "), code, errb, refused, !ok)
		}
		if !ok && !strings.Contains(errb, "gg compare") {
			t.Fatalf("gg %s <link>: a refusal must name `gg compare`, stderr=%q", strings.Join(verb, " "), errb)
		}
	}

	for _, tc := range cases {
		tc := tc
		t.Run(strings.Join(tc.verb, "_")+"/ref", func(t *testing.T) {
			run(t, tc.verb, refLink, tc.refOK)
		})
		t.Run(strings.Join(tc.verb, "_")+"/pair", func(t *testing.T) {
			run(t, tc.verb, pairLink, tc.pairOK)
		})
	}
}

// TestDiffPairLinkDiffsTheRangeNotJustB is the silent-wrong-answer guard for
// ruling R4's diff row. domain.Resolved.Pair carries both halves, but
// Resolved.Commit and Addr.Commit carry B alone — so a pair link that merely
// PASSES the shape gate falls through linkDiffSpec's StateCommitted arm and
// prints B's own change (B^..B) at exit 0. Three commits, three files: c1 is
// the repo's seed, c2 touches a.txt ONLY, c3 touches b.txt ONLY — so the
// wrong answer (B^..B = c2..c3) names ONE file (b.txt) and the right answer
// (the range c1..c3) names TWO (a.txt AND b.txt).
func TestDiffPairLinkDiffsTheRangeNotJustB(t *testing.T) {
	t.Parallel()
	dir := newCLIRepo(t)
	c1 := runGit(t, dir, "rev-parse", "HEAD")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "c2")
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "c3")
	c3 := runGit(t, dir, "rev-parse", "HEAD")

	link := "gg://" + filepath.ToSlash(dir) + "@" + c1 + ".." + c3

	t.Run("name-only", func(t *testing.T) {
		code, out, errb := runCLI(t, dir, "diff", link, "--name-only")
		if code != 0 {
			t.Fatalf("exit=%d stderr=%s", code, errb)
		}
		if !strings.Contains(out, "a.txt") || !strings.Contains(out, "b.txt") {
			t.Fatalf("gg diff %s --name-only = %q, want BOTH a.txt and b.txt (the range c1..c3, not B's own change c2..c3)", link, out)
		}
	})

	t.Run("hunks", func(t *testing.T) {
		// The path a note's numbering would inherit (`gg note add <link>#N`
		// reuses linkDiffSpec through HunkDiffSpec exactly like this).
		code, out, errb := runCLI(t, dir, "diff", link, "--hunks")
		if code != 0 {
			t.Fatalf("exit=%d stderr=%s", code, errb)
		}
		if !strings.Contains(out, "a.txt") || !strings.Contains(out, "b.txt") {
			t.Fatalf("gg diff %s --hunks = %q, want hunks for BOTH a.txt and b.txt (the range, not just B^..B)", link, out)
		}
	})
}

// TestOneDoorToTheLinkResolver pins the structural half of ruling R4.
//
// The shape gate is only worth as much as its unavoidability. Task 5 first
// shipped `resolveLinkArg` (three args, no gate) sitting beside
// `resolveLinkArgShapes` (five args, gated), with a comment asserting nobody
// would reach for the short one. That is a convention, not an invariant: a
// seventh verb's author, scanning for the shortest resolver, gets a pair
// resolution with no gate, no compiler complaint and no failing test — since
// a per-verb table can only test the verbs that exist.
//
// So there is now exactly ONE function in this package that calls
// domain.ResolveLink, and it takes the allowance as a parameter. A call
// written from muscle memory — resolveLinkArg(ctx, svc, arg) — fails to
// COMPILE. This test fails if a second door is ever opened.
func TestOneDoorToTheLinkResolver(t *testing.T) {
	t.Parallel()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	var callers []string
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), "domain.ResolveLink(") {
			callers = append(callers, f)
		}
	}
	if len(callers) != 1 || callers[0] != "link.go" {
		t.Errorf("domain.ResolveLink is called from %v, want link.go alone — every "+
			"link positional must go through resolveLinkArg's shape gate (ruling R4)", callers)
	}
	src, err := os.ReadFile("link.go")
	if err != nil {
		t.Fatal(err)
	}
	// The gate is carried in the signature, not in a comment: drop the
	// allowance parameter and every call site must be revisited.
	if !strings.Contains(string(src), "func resolveLinkArg(ctx context.Context, svc *domain.Service, s string, allow linkShapes, verb string)") {
		t.Error("resolveLinkArg must take the allowance and the verb, so an ungated call cannot compile")
	}
}
