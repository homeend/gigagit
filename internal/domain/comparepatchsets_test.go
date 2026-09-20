package domain

import (
	"context"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

// The rule is per SIDE: a set survives the ENDPOINT lane exactly when
// re-deriving it from its endpoint reproduces it. The two shelf rows share an
// endpoint and must disagree.
func TestPatchLosesKeySetPerSide(t *testing.T) {
	t.Parallel()
	shelfEP, err := model.ShelfEndpoint("wt-parser-9f3a1")
	if err != nil {
		t.Fatal(err)
	}
	commit := model.Endpoint{}
	for _, c := range []struct {
		name  string
		fs    FileSet
		loses bool
	}{
		{"an unbounded point", FileSet{ep: commit}, false},
		{"a whole shelf entry", FileSet{ep: shelfEP, bounded: true, paths: []string{"a", "b"}}, false},
		{"the same shelf entry, narrowed to a path", FileSet{ep: shelfEP, bounded: true, narrowed: true, paths: []string{"a"}}, true},
		{"a pair (bounded, endpoint = commit b)", FileSet{ep: commit, bounded: true, paths: []string{"a"}}, true},
		{"a file link at a commit (narrowed)", FileSet{ep: commit, bounded: true, narrowed: true, paths: []string{"a"}}, true},
		{"an EMPTY bounded set still loses", FileSet{ep: commit, bounded: true}, true},
	} {
		if got := c.fs.patchLosesKeySet(); got != c.loses {
			t.Errorf("%s: loses = %v, want %v", c.name, got, c.loses)
		}
	}
}

func evalText(t *testing.T, svc *Service, text string) FileSet {
	t.Helper()
	l, err := model.ParseLink(text)
	if err != nil {
		t.Fatalf("%s: %v", text, err)
	}
	fs, err := svc.EvalLink(context.Background(), l)
	if err != nil {
		t.Fatalf("EvalLink(%s): %v", text, err)
	}
	return fs
}

// patchFiles is the b/<path> of every file a patch renders, in order.
func patchFiles(patch string) []string {
	var out []string
	for _, ln := range strings.Split(patch, "\n") {
		if strings.HasPrefix(ln, "+++ b/") {
			out = append(out, strings.TrimPrefix(ln, "+++ b/"))
		} else if strings.HasPrefix(ln, "Binary files a/") {
			out = append(out, "binary")
		}
	}
	return out
}

// A FILE link narrows its commit to one file. The two commits differ in two,
// so the look-alike whole-endpoint call must render both — one fixture, two
// answers. This is the comparison --patch used to refuse.
func TestComparePatchSetsRendersExactlyTheListedFiles(t *testing.T) {
	t.Parallel()
	dir, svc := newRealRepo(t)
	ctx := context.Background()
	a := writeAndCommit(t, dir, "a", map[string]string{"one.txt": "1\n", "two.txt": "1\n"})
	b := writeAndCommit(t, dir, "b", map[string]string{"one.txt": "2\n", "two.txt": "2\n"})

	whole, err := svc.ComparePatchSets(ctx, evalText(t, svc, "gg://x@"+a), evalText(t, svc, "gg://x@"+b))
	if err != nil {
		t.Fatal(err)
	}
	if got := patchFiles(whole); len(got) != 2 {
		t.Fatalf("fixture: the two points differ in two files, the patch renders %v", got)
	}
	narrow, err := svc.ComparePatchSets(ctx, evalText(t, svc, "gg://x/one.txt@"+a), evalText(t, svc, "gg://x@"+b))
	if err != nil {
		t.Fatalf("a file link × a point: %v", err)
	}
	if got := patchFiles(narrow); len(got) != 1 || got[0] != "one.txt" {
		t.Fatalf("a file link × a point renders %v, want exactly one.txt", got)
	}
	if !strings.Contains(narrow, "-1\n+2\n") {
		t.Fatalf("the patch reads left → right:\n%s", narrow)
	}
	// A change-set renders its members only: a..b touched both files, c
	// touches a third, and the pair × c comparison must not render it.
	c := writeAndCommit(t, dir, "c", map[string]string{"three.txt": "3\n"})
	pair, err := svc.ComparePatchSets(ctx, evalText(t, svc, "gg://x@"+a+".."+b), evalText(t, svc, "gg://x@"+c))
	if err != nil {
		t.Fatalf("a pair × a point: %v", err)
	}
	if got := patchFiles(pair); len(got) != 0 {
		// b and c agree on both of the pair's members: nothing differs, and
		// three.txt — which the endpoints DO differ in — is not a member.
		t.Fatalf("pair × a later point renders %v, want nothing (its members are unchanged)", got)
	}
}

// A `-u` stash keeps its untracked files on a THIRD parent: the member's bytes
// come from Source(path), never from the set's own endpoint.
func TestComparePatchSetsReadsAMemberFromItsSource(t *testing.T) {
	t.Parallel()
	_, svc, parent, sha := stashFixture(t)
	patch, err := svc.ComparePatchSets(context.Background(), evalText(t, svc, "gg://x@"+parent), evalText(t, svc, "gg://x@"+parent+".."+sha))
	if err != nil {
		t.Fatal(err)
	}
	got := patchFiles(patch)
	if len(got) != 2 || !strings.Contains(patch, "+untracked") || !strings.Contains(patch, "+v2") {
		t.Fatalf("the stash's tracked edit AND its untracked file must render, got %v:\n%s", got, patch)
	}
}

// A reversed live pair — the working tree, then a commit — used to be refused
// (model.DiffSpec has no -R). Rendered per member it is the forward patch with
// the sides swapped.
func TestComparePatchSetsRendersAReversedLivePair(t *testing.T) {
	t.Parallel()
	dir, svc := newRealRepo(t)
	ctx := context.Background()
	head := writeAndCommit(t, dir, "a", map[string]string{"one.txt": "committed\n"})
	writeIn(t, dir, "one.txt", "edited\n")
	wt, commit := evalText(t, svc, "gg://x"), evalText(t, svc, "gg://x@"+head)
	fwd, err := svc.ComparePatchSets(ctx, commit, wt)
	if err != nil {
		t.Fatal(err)
	}
	rev, err := svc.ComparePatchSets(ctx, wt, commit)
	if err != nil {
		t.Fatalf("the reversed pair: %v", err)
	}
	if !strings.Contains(fwd, "-committed\n+edited\n") || !strings.Contains(rev, "-edited\n+committed\n") {
		t.Fatalf("forward:\n%s\nreversed:\n%s", fwd, rev)
	}
}

// A rename row reads its LEFT side at the row's old path. Only an unbounded ×
// unbounded comparison lists renames (the key-based lanes pair files by path),
// and the one such pair the per-member lane renders is the reversed live pair.
func TestComparePatchSetsReadsARenamesLeftSideAtItsOldPath(t *testing.T) {
	t.Parallel()
	dir, svc := newRealRepo(t)
	ctx := context.Background()
	body := strings.Repeat("line\n", 40)
	head := writeAndCommit(t, dir, "a", map[string]string{"old.txt": body})
	gitRun(t, dir, "mv", "old.txt", "new.txt")
	writeIn(t, dir, "new.txt", body+"tail\n")
	gitRun(t, dir, "add", "-A")
	left, right := evalText(t, svc, "gg://x"), evalText(t, svc, "gg://x@"+head)
	rows, err := svc.CompareSets(ctx, left, right)
	if err != nil {
		t.Fatal(err)
	}
	var row *model.CommitFile
	for i := range rows {
		if rows[i].OldPath != "" {
			row = &rows[i]
		}
	}
	if row == nil {
		t.Fatalf("fixture: no rename row in %+v", rows)
	}
	patch, err := svc.ComparePatchSets(ctx, left, right)
	if err != nil {
		t.Fatal(err)
	}
	// worktree → commit: new.txt (with its tail) becomes old.txt (without).
	if strings.Count(patch, "-line\n")+strings.Count(patch, "+line\n") != 0 || !strings.Contains(patch, "-tail") {
		t.Fatalf("row %+v: a rename must diff the two NAMES of one file (one removed line):\n%s", *row, patch)
	}
	if !strings.Contains(patch, "--- a/"+row.OldPath) || !strings.Contains(patch, "+++ b/"+row.Path) {
		t.Fatalf("row %+v: headers must name old → new:\n%s", *row, patch)
	}
}
