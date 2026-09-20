package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/linkhist"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/repos"
)

// noResolve fails the test if parseEndpoint calls it — used for the
// @worktree/@staged/@index cases, which must never touch the resolver.
func noResolve(t *testing.T) func(string) (string, bool, error) {
	return func(rev string) (string, bool, error) {
		t.Fatalf("resolve(%q) called; @worktree/@staged/@index must not resolve", rev)
		return "", false, nil
	}
}

func TestParseEndpoint(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want model.Endpoint
	}{
		{"@worktree", model.WorkTreeEndpoint()},
		{"@staged", model.IndexEndpoint()},
		{"@index", model.IndexEndpoint()},
	}
	for _, c := range cases {
		got, err := parseEndpoint(c.in, noResolve(t))
		if err != nil {
			t.Fatalf("parseEndpoint(%q): %v", c.in, err)
		}
		if got != c.want {
			t.Errorf("parseEndpoint(%q) = %+v, want %+v", c.in, got, c.want)
		}
	}
}

// TestParseEndpointResolvesCommitish pins the fix for the shipped cache bug:
// a default-case token (HEAD, a branch name, "HEAD~2", an abbreviated sha)
// must be resolved to a sha through resolve before model.CommitEndpoint
// builds the Endpoint — CacheTag() returns Hash verbatim and is the session
// diff-cache key, so a rev-spec there would key the cache on a name that
// moves.
func TestParseEndpointResolvesCommitish(t *testing.T) {
	t.Parallel()
	resolve := func(rev string) (string, bool, error) {
		if rev != "HEAD~2" {
			t.Fatalf("resolve called with unexpected rev %q", rev)
		}
		return "abc1234", true, nil
	}
	got, err := parseEndpoint("HEAD~2", resolve)
	if err != nil {
		t.Fatalf("parseEndpoint: %v", err)
	}
	want, werr := model.CommitEndpoint("abc1234")
	if werr != nil {
		t.Fatalf("model.CommitEndpoint: %v", werr)
	}
	if got != want {
		t.Fatalf("parseEndpoint(%q) = %+v, want %+v", "HEAD~2", got, want)
	}
}

// TestParseEndpointUnresolvableRevErrors covers a typo or a gc'd sha: resolve
// reports ok=false (CommitLookup's "missing is not an error" convention),
// and parseEndpoint must turn that into a non-nil error rather than handing
// back a zero Endpoint silently.
func TestParseEndpointUnresolvableRevErrors(t *testing.T) {
	t.Parallel()
	resolve := func(rev string) (string, bool, error) { return "", false, nil }
	if _, err := parseEndpoint("no-such-rev", resolve); err == nil {
		t.Fatal("parseEndpoint should error on an unresolvable rev")
	}
}

// TestParseEndpointResolveErrorPropagates covers resolve itself failing
// (e.g. context cancellation) — the error must not be discarded.
func TestParseEndpointResolveErrorPropagates(t *testing.T) {
	t.Parallel()
	wantErr := errors.New("boom")
	resolve := func(rev string) (string, bool, error) { return "", false, wantErr }
	_, err := parseEndpoint("HEAD", resolve)
	if !errors.Is(err, wantErr) {
		t.Fatalf("parseEndpoint error = %v, want wrapping %v", err, wantErr)
	}
}

func TestCompareCommitRange(t *testing.T) {
	t.Parallel()
	dir := newCLIRepo(t) // one commit: README.md, on main
	// second commit adds b.txt
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b\n"), 0o644)
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-q", "-m", "c2")

	code, out, errb := runCLI(t, dir, "compare", "HEAD~1", "HEAD")
	if code != 0 {
		t.Fatalf("exit = %d, stderr: %s", code, errb)
	}
	if !strings.Contains(out, "b.txt") {
		t.Fatalf("compare HEAD~1 HEAD must list b.txt:\n%s", out)
	}
}

// TestCompareShortCoreAbbrev pins the core.abbrev regression. The commit-ish
// resolver used to read svc.CommitLookup, i.e. `git log --format=%h`, whose
// width honours core.abbrev — git's legal minimum is 4, while
// model.CommitEndpoint requires 7..64. On `core.abbrev = 4` every commit-ish
// token therefore failed — today's wording is `"dead" is not a commit id
// (want 7 to 64 hex characters)` — and exited 2, on a path that used to work. Both pairs are covered: the live
// pair (never cached) and the commit↔commit pair (the one that caches).
func TestCompareShortCoreAbbrev(t *testing.T) {
	t.Parallel()
	dir := newCLIRepo(t) // one commit: README.md, on main
	gitRun(t, dir, "config", "core.abbrev", "4")
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b\n"), 0o644)
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-q", "-m", "c2")
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("dirtied\n"), 0o644)

	code, out, errb := runCLI(t, dir, "compare", "HEAD", "@worktree")
	if code != 0 {
		t.Fatalf("compare HEAD @worktree under core.abbrev=4: exit = %d, stderr: %s", code, errb)
	}
	if !strings.Contains(out, "README.md") {
		t.Fatalf("compare HEAD @worktree must list README.md:\n%s", out)
	}

	code, out, errb = runCLI(t, dir, "compare", "HEAD~1", "HEAD")
	if code != 0 {
		t.Fatalf("compare HEAD~1 HEAD under core.abbrev=4: exit = %d, stderr: %s", code, errb)
	}
	if !strings.Contains(out, "b.txt") {
		t.Fatalf("compare HEAD~1 HEAD must list b.txt:\n%s", out)
	}
}

// TestCompareUnknownRevExitsUsage: an unresolvable rev is bad INPUT, so it
// exits 2 (usage). A resolve that fails for another reason exits 1 — see
// resolveCompareSpec's errUnknownRev split and its KNOWN GAP note.
func TestCompareUnknownRevExitsUsage(t *testing.T) {
	t.Parallel()
	dir := newCLIRepo(t)
	code, _, errb := runCLI(t, dir, "compare", "no-such-rev", "@worktree")
	if code != 2 {
		t.Fatalf("unresolvable rev must exit 2 (usage), got %d; stderr: %s", code, errb)
	}
	if !strings.Contains(errb, "unknown revision") {
		t.Fatalf("stderr should name the unknown revision:\n%s", errb)
	}
}

// TestResolveCompareSpecFailedResolveExitsOne pins the other half of the
// split: parseEndpoint's error for a FAILED resolve does not wrap
// errUnknownRev, so resolveCompareSpec must not classify it as usage.
func TestResolveCompareSpecFailedResolveExitsOne(t *testing.T) {
	t.Parallel()
	_, err := parseEndpoint("HEAD", func(string) (string, bool, error) {
		return "", false, errors.New("repo is locked")
	})
	if err == nil {
		t.Fatal("a failed resolve must produce an error")
	}
	if errors.Is(err, errUnknownRev) {
		t.Fatalf("a failed resolve must NOT be classified as a usage error: %v", err)
	}
	_, err = parseEndpoint("no-such-rev", func(string) (string, bool, error) {
		return "", false, nil
	})
	if !errors.Is(err, errUnknownRev) {
		t.Fatalf("an unresolvable rev must wrap errUnknownRev, got %v", err)
	}
}

func TestCompareDefaultsToWorktree(t *testing.T) {
	t.Parallel()
	dir := newCLIRepo(t)
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("dirtied\n"), 0o644) // unstaged change

	code, out, errb := runCLI(t, dir, "compare", "HEAD")
	if code != 0 {
		t.Fatalf("exit = %d, stderr: %s", code, errb)
	}
	if !strings.Contains(out, "README.md") {
		t.Fatalf("compare HEAD (vs working tree) must list README.md:\n%s", out)
	}
}

// TestCompareReversedPairNowCompares replaces the old
// TestCompareReversePairFriendlyError. `gg compare @worktree HEAD` used to be
// a usage error: validComparePair screened for the four forward forms git's
// own diff can walk and refused everything else with "order endpoints
// oldest→newest". That predicate is GONE — domain.CompareSets asks a reversed
// pair forward and inverts the answer — so the reverse order is now a
// comparison, and the statuses read from the LEFT side towards the right.
func TestCompareReversedPairNowCompares(t *testing.T) {
	t.Parallel()
	dir := newCLIRepo(t)
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("dirtied\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "fresh.txt"), []byte("new\n"), 0o644) // untracked

	code, out, errb := runCLI(t, dir, "compare", "@worktree", "HEAD") // reverse order
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (the algebra is total); stderr: %s", code, errb)
	}
	if !strings.Contains(out, "M\tREADME.md") {
		t.Errorf("stdout must carry M\\tREADME.md:\n%s", out)
	}
	// Forward this is an addition; reversed the comparison ENDS at the commit,
	// where the untracked file does not exist — so it reads deleted.
	if !strings.Contains(out, "D\tfresh.txt") {
		t.Errorf("stdout must carry D\\tfresh.txt (untracked on disk, absent at HEAD):\n%s", out)
	}
	if strings.Contains(errb, "oldest") || strings.Contains(errb, "not the reverse") {
		t.Errorf("the deleted ordering refusal is still being printed:\n%s", errb)
	}
	if strings.Contains(errb, "DiffTreeFiles") || strings.Contains(errb, "endpoint pair") {
		t.Errorf("error leaks internals: %s", errb)
	}
}

func TestCompareNoArgsUsage(t *testing.T) {
	t.Parallel()
	dir := newCLIRepo(t)
	code, _, errb := runCLI(t, dir, "compare")
	if code != 2 {
		t.Fatalf("exit = %d, want 2 (usage)", code)
	}
	if !strings.Contains(errb, "usage") {
		t.Fatalf("missing usage on stderr:\n%s", errb)
	}
}

// linkCompareRepo builds the fixture every link-compare test below shares:
//
//	c1  README.md = "hi\nthere\n"            (newCLIRepo's initial commit)
//	c2  + b.txt = "b\n"
//	c3  b.txt = "bb\n", README.md = "changed\n"
//
// Two files that move at different times is the point: a link that addresses
// ONE of them must produce exactly one row, which is what proves the link's
// path survived.
func linkCompareRepo(t *testing.T) (dir, c1, c2, c3 string) {
	t.Helper()
	dir = newCLIRepo(t)
	c1 = strings.TrimSpace(gitOut(t, dir, "rev-parse", "HEAD"))
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b\n"), 0o644)
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-q", "-m", "c2")
	c2 = strings.TrimSpace(gitOut(t, dir, "rev-parse", "HEAD"))
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("bb\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("changed\n"), 0o644)
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-q", "-m", "c3")
	c3 = strings.TrimSpace(gitOut(t, dir, "rev-parse", "HEAD"))
	return dir, c1, c2, c3
}

// mustLink runs `gg link` with a scratch registry and returns the link it
// printed, so a compare test addresses exactly what a user would paste.
func mustLink(t *testing.T, dir string, args ...string) string {
	t.Helper()
	code, out, errb := runLinkCLI(t, dir, args...)
	if code != 0 {
		t.Fatalf("gg link %v: exit %d (stderr %q)", args, code, errb)
	}
	return strings.TrimSpace(out)
}

// runCompareAt runs `gg compare` against an explicit repo registry, the way
// runLinkCLI runs `gg link`. The registry is a parameter rather than the
// process-global RepoStatePath so a cross-repo test can register a second
// checkout and still run t.Parallel(). args are the verb's own arguments — the
// "compare" token is not one of them, since cmdCompare is entered past it.
func runCompareAt(t *testing.T, statePath, dir string, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := cmdCompare(statePath, openCLIService(t, dir), args, &out, &errb)
	return code, out.String(), errb.String()
}

// TestCompareAcceptsLinks: a gg:// link works on EITHER side, and mixes with
// the old vocabulary — links are the primary spelling of an endpoint, not a
// mode you switch the verb into.
func TestCompareAcceptsLinks(t *testing.T) {
	t.Parallel()
	dir, c1, c2, c3 := linkCompareRepo(t)

	// A CHANGE-SET link is bounded: c1..c2 enumerates b.txt and nothing else.
	// Against HEAD (=c3, where b.txt reads "bb\n") that is one modification —
	// README.md also differs between c2 and c3, and must NOT appear, because
	// the bounded side supplies the key set.
	pair := mustLink(t, dir, "--pair", c1+".."+c2)
	code, out, errb := runCLI(t, dir, "compare", pair, "HEAD")
	if code != 0 {
		t.Fatalf("compare <pair-link> HEAD: exit %d (stderr %q)", code, errb)
	}
	if out != "M\tb.txt\n" {
		t.Errorf("compare <pair-link> HEAD = %q, want exactly \"M\\tb.txt\\n\" (the change-set's one member)", out)
	}

	// A link on the RIGHT, old vocabulary on the left. A ref link is a POINT —
	// the whole tree at main's tip — so c1 → main's tip is the plain tree diff.
	ref := mustLink(t, dir, "--ref", "main")
	code, out, errb = runCLI(t, dir, "compare", c1, ref)
	if code != 0 {
		t.Fatalf("compare c1 <ref-link>: exit %d (stderr %q)", code, errb)
	}
	if !strings.Contains(out, "A\tb.txt") || !strings.Contains(out, "M\tREADME.md") {
		t.Errorf("compare c1 <ref-link> = %q, want b.txt added and README.md modified", out)
	}

	// Links on BOTH sides.
	commit := mustLink(t, dir, "--rev", c3)
	code, out, errb = runCLI(t, dir, "compare", pair, commit)
	if code != 0 {
		t.Fatalf("compare <pair-link> <commit-link>: exit %d (stderr %q)", code, errb)
	}
	if out != "M\tb.txt\n" {
		t.Errorf("compare <pair-link> <commit-link> = %q, want exactly \"M\\tb.txt\\n\"", out)
	}
}

// TestCompareFileLinkIsSplitBeforeItIsEvaluated is the regression test for the
// one silent-wrong-answer this feature could ship. A PARSED local-form link
// carries the checkout AND the file path undivided in Repo.Abs with Path == ""
// — the grammar puts no delimiter between them. Evaluate it without splitting
// and the FILE link reads as a WHOLE-TREE link: the comparison would answer
// about every file in the repository and report no error at all.
//
// The fixture makes that failure visible: c1 → c3 changes TWO files, and a
// link addressing one of them must produce exactly one row.
func TestCompareFileLinkIsSplitBeforeItIsEvaluated(t *testing.T) {
	t.Parallel()
	dir, c1, _, c3 := linkCompareRepo(t)
	fileLink := mustLink(t, dir, "--rev", c3, "README.md")
	if !strings.HasSuffix(fileLink, "/README.md@"+c3) {
		t.Fatalf("gg link printed %q, want a local-form link ending /README.md@%s", fileLink, c3)
	}
	code, out, errb := runCompareAt(t, linkState(t), dir, fileLink, c1)
	if code != 0 {
		t.Fatalf("compare <file-link> c1: exit %d (stderr %q)", code, errb)
	}
	if out != "M\tREADME.md\n" {
		t.Fatalf("compare <file-link> c1 = %q, want exactly \"M\\tREADME.md\\n\" — "+
			"b.txt in the output means the link's path was dropped and the whole tree compared", out)
	}
}

// TestCompareTwoLocalFormFileLinksThroughTheDoor: BOTH tokens are links, the
// lane that goes through domain.CompareLinks. The expectation is the one the
// MCP and TUI legs share (spec §5 "one door"): a local-form file link is one
// file, never the whole tree.
func TestCompareTwoLocalFormFileLinksThroughTheDoor(t *testing.T) {
	t.Parallel()
	dir, _, c2, c3 := linkCompareRepo(t)
	state := linkState(t)
	code, out, errb := runCompareAt(t, state, dir, mustLink(t, dir, "--rev", c2), mustLink(t, dir, "--rev", c3))
	if code != 0 || out != "M\tREADME.md\nM\tb.txt\n" {
		t.Fatalf("fixture: whole-tree c2..c3 = %d %q (stderr %q), want two files — the arms must differ", code, out, errb)
	}
	left, right := mustLink(t, dir, "--rev", c2, "b.txt"), mustLink(t, dir, "--rev", c3, "b.txt")
	if !strings.Contains(left, "/b.txt@") || !strings.HasPrefix(left, "gg:///") {
		t.Fatalf("gg link printed %q, want a local-form file link", left)
	}
	code, out, errb = runCompareAt(t, state, dir, left, right)
	if code != 0 || out != "M\tb.txt\n" {
		t.Fatalf("compare <file-link> <file-link> = %d %q (stderr %q), want exactly \"M\\tb.txt\\n\"", code, out, errb)
	}
}

// TestCompareTwoLinksKeepsItsExitCodesAndWording: the two-link lane reports a
// failing side exactly as the one-link lane does — same exit codes, and no
// "left:"/"right:" prefix leaking out of the door into messages scripts read.
func TestCompareTwoLinksKeepsItsExitCodesAndWording(t *testing.T) {
	t.Parallel()
	here, _, _, c3 := linkCompareRepo(t)
	other := newCLIRepo(t)
	state := linkState(t)
	if err := repos.Touch(state, other, "", time.Unix(9000, 0)); err != nil {
		t.Fatal(err)
	}
	good := mustLink(t, here, "--rev", c3)
	for _, c := range []struct {
		name, left, right string
		code              int
		want              string
	}{
		{"cross-repo right", good, "gg://" + filepath.ToSlash(other) + "/README.md", 2, "compare: gg://" + filepath.ToSlash(other) + "/README.md names a different repository"},
		{"cross-repo left", "gg://" + filepath.ToSlash(other) + "/README.md", good, 2, "cross-repository compare is not supported yet"},
		{"bad link right", good, "gg://somerepo@nothex", 2, "compare: bad gg link"},
		{"unknown repo right", good, "gg://nowhere-on-this-machine@ref:main", 1, "is not in this machine's gg history"},
	} {
		code, out, errb := runCompareAt(t, state, here, c.left, c.right)
		if code != c.code {
			t.Errorf("%s: exit %d, want %d (stderr %q)", c.name, code, c.code, errb)
		}
		if !strings.Contains(errb, c.want) {
			t.Errorf("%s: stderr %q, want it to contain %q", c.name, errb, c.want)
		}
		if strings.Contains(errb, "left: ") || strings.Contains(errb, "right: ") {
			t.Errorf("%s: stderr %q leaks the door's side prefix", c.name, errb)
		}
		if out != "" {
			t.Errorf("%s: stdout must stay empty, got %q", c.name, out)
		}
	}
}

// TestCompareBackCompatVocabulary: every old spelling keeps working, with the
// same exit codes. Nothing about links is a mode.
func TestCompareBackCompatVocabulary(t *testing.T) {
	t.Parallel()
	dir, c1, _, c3 := linkCompareRepo(t)

	// commit ↔ commit
	code, out, errb := runCLI(t, dir, "compare", c1, c3)
	if code != 0 {
		t.Fatalf("compare c1 c3: exit %d (stderr %q)", code, errb)
	}
	if !strings.Contains(out, "A\tb.txt") || !strings.Contains(out, "M\tREADME.md") {
		t.Errorf("compare c1 c3 = %q, want b.txt added and README.md modified", out)
	}

	// <right> still defaults to @worktree.
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("dirtied\n"), 0o644)
	code, out, errb = runCLI(t, dir, "compare", "HEAD")
	if code != 0 {
		t.Fatalf("compare HEAD: exit %d (stderr %q)", code, errb)
	}
	if out != "M\tb.txt\n" {
		t.Errorf("compare HEAD = %q, want exactly \"M\\tb.txt\\n\"", out)
	}

	// commit → @staged
	gitRun(t, dir, "add", "b.txt")
	code, out, errb = runCLI(t, dir, "compare", "HEAD", "@staged")
	if code != 0 {
		t.Fatalf("compare HEAD @staged: exit %d (stderr %q)", code, errb)
	}
	if out != "M\tb.txt\n" {
		t.Errorf("compare HEAD @staged = %q, want exactly \"M\\tb.txt\\n\"", out)
	}

	// An unresolvable rev is still bad INPUT: exit 2.
	code, _, errb = runCLI(t, dir, "compare", "no-such-rev")
	if code != 2 {
		t.Fatalf("compare no-such-rev: exit %d, want 2 (usage)", code)
	}
	if !strings.Contains(errb, "unknown revision") {
		t.Errorf("stderr should name the unknown revision:\n%s", errb)
	}

	// No arguments is still the usage error, now naming links.
	code, _, errb = runCLI(t, dir, "compare")
	if code != 2 {
		t.Fatalf("compare (no args): exit %d, want 2", code)
	}
	if !strings.Contains(errb, "usage: gg compare") || !strings.Contains(errb, "gg:// link") {
		t.Errorf("usage must mention the gg:// link form:\n%s", errb)
	}
}

// TestCompareLinkExitCodes pins the shipped convention (spec §6): a link the
// GRAMMAR refuses is bad input (exit 2), a link that cannot be RESOLVED on
// this machine is a failure (exit 1).
func TestCompareLinkExitCodes(t *testing.T) {
	t.Parallel()
	dir, _, _, _ := linkCompareRepo(t)

	// Unparseable: "nothex" is not a target the grammar knows.
	code, out, errb := runCLI(t, dir, "compare", "gg://somerepo@nothex", "HEAD")
	if code != 2 {
		t.Fatalf("malformed link: exit %d, want 2 (usage); stderr %q", code, errb)
	}
	if !strings.Contains(errb, "compare: bad gg link") {
		t.Errorf("stderr must be the grammar's own refusal, prefixed by the verb:\n%s", errb)
	}
	if out != "" {
		t.Errorf("stdout must stay empty on a usage error, got %q", out)
	}

	// Well-formed, but no checkout on this machine answers to it.
	code, out, errb = runCLI(t, dir, "compare", "gg://nowhere-on-this-machine@ref:main", "HEAD")
	if code != 1 {
		t.Fatalf("unknown repository: exit %d, want 1 (a failure, not usage); stderr %q", code, errb)
	}
	if !strings.Contains(errb, "is not in this machine's gg history") {
		t.Errorf("stderr must say the repository is unknown:\n%s", errb)
	}
	if out != "" {
		t.Errorf("stdout must stay empty on a failure, got %q", out)
	}

	// A link whose path escapes the checkout it names is malformed, not
	// unresolvable: exit 2. The REMOTE-named form is used because only there is
	// the path half divided from the repository half at parse time — the local
	// form's ".." lands inside Repo.Abs and simply matches no checkout.
	gitRun(t, dir, "remote", "add", "origin", "git@github.com:homeend/gigagit.git")
	code, _, errb = runCLI(t, dir, "compare", "gg://gigagit/../outside.txt", "HEAD")
	if code != 2 {
		t.Fatalf("escaping path: exit %d, want 2; stderr %q", code, errb)
	}
	if !strings.Contains(errb, "escapes the checkout") {
		t.Errorf("stderr must name the escape:\n%s", errb)
	}
}

// TestCompareRefusesACrossRepoLink: a link naming a DIFFERENT repository is
// refused with an explicit message, not silently compared against this one.
// Cross-repository compare is deferred (spec §9) because both reads would have
// to happen under two repogate reservations taken in a fixed global order.
func TestCompareRefusesACrossRepoLink(t *testing.T) {
	t.Parallel()
	here, _, _, _ := linkCompareRepo(t)
	other := newCLIRepo(t)
	state := linkState(t)
	if err := repos.Touch(state, other, "", time.Unix(9000, 0)); err != nil {
		t.Fatalf("repos.Touch: %v", err)
	}
	otherLink := "gg://" + filepath.ToSlash(other) + "/README.md"

	code, out, errb := runCompareAt(t, state, here, otherLink, "HEAD")
	if code != 2 {
		t.Fatalf("cross-repo link: exit %d, want 2 (usage); stderr %q", code, errb)
	}
	if !strings.Contains(errb, "names a different repository") ||
		!strings.Contains(errb, "cross-repository compare is not supported yet") {
		t.Errorf("stderr must refuse explicitly:\n%s", errb)
	}
	if !strings.Contains(errb, other) {
		t.Errorf("stderr must name the checkout it located (%s):\n%s", other, errb)
	}
	if out != "" {
		t.Errorf("stdout must stay empty, got %q", out)
	}

	// The same refusal on the RIGHT side: neither position is a loophole.
	code, out, errb = runCompareAt(t, state, here, "HEAD", otherLink)
	if code != 2 {
		t.Fatalf("cross-repo link on the right: exit %d, want 2; stderr %q", code, errb)
	}
	if !strings.Contains(errb, "names a different repository") {
		t.Errorf("stderr must refuse the right side too:\n%s", errb)
	}
	if out != "" {
		t.Errorf("stdout must stay empty, got %q", out)
	}
}

// The shelf ↔ live refusal is gone too, and that removal answers a real
// question: "is my shelved work already in my working tree?" projects the
// frozen member list onto the live tree. It used to print "a frozen shelf
// entry pairs only with a commit or another shelf entry".
func TestCompareShelfAgainstTheWorkingTree(t *testing.T) {
	dir := newCLIRepo(t)
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	writeFile(t, dir, "f.txt", "shelved\n")
	gitc(t, dir, "add", ".")
	gitc(t, dir, "commit", "-m", "doomed")
	doomed := headSha(t, dir)
	id := shelfCommitID(t, dir, doomed)

	code, out, errb := runCompare(t, dir, "compare", "shelf:"+id, "@worktree")
	if code != 0 {
		t.Fatalf("compare shelf:<id> @worktree: exit %d, want 0; stderr %q", code, errb)
	}
	// The shelf's one member is byte-identical to the file on disk, so the
	// projection finds no difference — a RESULT, where the old CLI refused to
	// look at all.
	if out != "" {
		t.Errorf("stdout = %q, want no rows (the shelved bytes are already on disk)", out)
	}
	if strings.Contains(errb, "pairs only with") {
		t.Errorf("the deleted shelf refusal is still being printed:\n%s", errb)
	}
}

// patchedFiles is the b/<path> of every file a --patch output renders, sorted.
func patchedFiles(patch string) []string {
	var out []string
	for _, ln := range strings.Split(patch, "\n") {
		if p, ok := strings.CutPrefix(ln, "+++ b/"); ok {
			out = append(out, p)
		} else if p, ok := strings.CutPrefix(ln, "--- a/"); ok && strings.Contains(patch, ln+"\n+++ /dev/null") {
			out = append(out, p) // a deletion has no b/ side
		}
	}
	sort.Strings(out)
	return out
}

// listedFiles is the path column of the default listing, sorted.
func listedFiles(listing string) []string {
	var out []string
	for _, ln := range strings.Split(strings.TrimSpace(listing), "\n") {
		if _, p, ok := strings.Cut(ln, "\t"); ok {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}

// TestComparePatchRendersExactlyTheListing: --patch is the patch of the rows
// the default listing shows — no more, no fewer. It used to print the
// ENDPOINTS' whole diff for a file set (silently a different comparison), then
// to refuse (exit 2); a file set now renders per member. The reachable shape is
// the everyday one: a single-file link is what every "copy gg link" button in
// the product emits.
//
// SERIAL (no t.Parallel) because the pair × shelf subtests need
// XDG_STATE_HOME, which t.Setenv cannot set from a parallel test.
func TestComparePatchRendersExactlyTheListing(t *testing.T) {
	dir, c1, c2, _ := linkCompareRepo(t)
	file := mustLink(t, dir, "--rev", c2, "b.txt")
	pair := mustLink(t, dir, "--pair", c1+".."+c2)

	// The look-alike: the two POINTS differ in more than b.txt, so a patch that
	// still rendered whole endpoints would show more than the listing does.
	code, whole, errb := runCLI(t, dir, "compare", "--patch", c2, "HEAD")
	if code != 0 {
		t.Fatalf("fixture: exit %d (stderr %q)", code, errb)
	}
	if got := patchedFiles(whole); len(got) < 2 {
		t.Fatalf("fixture: the endpoints must differ in more than one file, got %v", got)
	}

	for _, tc := range []struct{ name, left, right string }{
		{"single-file link", file, "HEAD"},
		{"change-set link", pair, "HEAD"},
		{"both sides", pair, file},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, listing, errb := runCLI(t, dir, "compare", tc.left, tc.right)
			if code != 0 {
				t.Fatalf("listing: exit %d (stderr %q)", code, errb)
			}
			code, patch, errb := runCLI(t, dir, "compare", "--patch", tc.left, tc.right)
			if code != 0 || errb != "" {
				t.Fatalf("--patch: exit %d, stderr %q", code, errb)
			}
			if got, want := patchedFiles(patch), listedFiles(listing); !slices.Equal(got, want) {
				t.Errorf("--patch renders %v, the listing shows %v", got, want)
			}
		})
	}

	// pair × shelf, BOTH orders — the combination that once answered wrongly:
	// a PAIR set carries commit b as its endpoint, so re-deriving from it
	// yields b's WHOLE TREE, the patch reported a file the listing calls A as M
	// and dropped the only file the change-set named.
	t.Run("pair against a frozen shelf entry", func(t *testing.T) {
		sdir := newCLIRepo(t)
		t.Setenv("XDG_STATE_HOME", t.TempDir())

		// The shelved commit: it touches f.txt and nothing else.
		writeFile(t, sdir, "f.txt", "base\n")
		gitc(t, sdir, "add", ".")
		gitc(t, sdir, "commit", "-m", "base")
		baseSha := headSha(t, sdir)
		writeFile(t, sdir, "f.txt", "doomed\n")
		gitc(t, sdir, "add", ".")
		gitc(t, sdir, "commit", "-m", "doomed")
		id := shelfCommitID(t, sdir, headSha(t, sdir))
		gitc(t, sdir, "reset", "--hard", baseSha)
		gitc(t, sdir, "reflog", "expire", "--expire=now", "--all")
		gitc(t, sdir, "gc", "--prune=now")

		// The change-set: it touches other.txt and nothing else.
		writeFile(t, sdir, "other.txt", "one\n")
		gitc(t, sdir, "add", ".")
		gitc(t, sdir, "commit", "-m", "other")
		pair := mustLink(t, sdir, "--pair", baseSha+".."+headSha(t, sdir))
		shelf := "shelf:" + id

		code, out, errb := runCompare(t, sdir, "compare", pair, shelf)
		if code != 0 {
			t.Fatalf("listing: exit %d (stderr %q)", code, errb)
		}
		if out != "A\tf.txt\nD\tother.txt\n" {
			t.Fatalf("listing = %q, want \"A\\tf.txt\\nD\\tother.txt\\n\"", out)
		}
		for _, tc := range []struct{ name, left, right, added string }{
			{"pair on the left", pair, shelf, "+doomed"},
			{"pair on the right", shelf, pair, "+one"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				code, patch, errb := runCompare(t, sdir, "compare", "--patch", tc.left, tc.right)
				if code != 0 {
					t.Fatalf("exit %d, stderr %q", code, errb)
				}
				if got := patchedFiles(patch); !slices.Equal(got, []string{"f.txt", "other.txt"}) {
					t.Errorf("--patch renders %v, want the listing's two files:\n%s", got, patch)
				}
				if !strings.Contains(patch, tc.added) {
					t.Errorf("the patch must read left → right (%q):\n%s", tc.added, patch)
				}
			})
		}
	})
}

// --patch of a REVERSED live pair used to be refused (model.DiffSpec has no
// `-R`). The listing already inverted it (TestCompareReversedPairNowCompares);
// the patch now reads the same way round — left → right, as typed.
func TestComparePatchOfAReversedPairReadsAsTyped(t *testing.T) {
	t.Parallel()
	dir := newCLIRepo(t)
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("dirtied\n"), 0o644)
	code, out, errb := runCLI(t, dir, "compare", "--patch", "@worktree", "HEAD")
	if code != 0 || errb != "" {
		t.Fatalf("compare --patch @worktree HEAD: exit %d, stderr %q", code, errb)
	}
	if !strings.Contains(out, "-dirtied") || strings.Contains(out, "+dirtied") {
		t.Fatalf("the working tree is the LEFT side, so its line is removed:\n%s", out)
	}
}

// compareHistSvc opens dir with an isolated, injected linkhist store — the
// compare-side twin of link_test.go's linkHistSvc.
func compareHistSvc(t *testing.T, dir string) *domain.Service {
	t.Helper()
	svc := openCLIService(t, dir)
	svc.SetLinkHistStore(linkhist.NewFileStore(t.TempDir()))
	return svc
}

// TestCompareRecordsLinkPositionalsOnSuccess: R8 — a gg:// link positional is
// recorded on a successful compare; a non-link positional (a plain commit-ish
// here) is not, even though it sits right beside a link that IS recorded.
func TestCompareRecordsLinkPositionalsOnSuccess(t *testing.T) {
	t.Parallel()
	dir, c1, c2, _ := linkCompareRepo(t)
	svc := compareHistSvc(t, dir)
	pair := mustLink(t, dir, "--pair", c1+".."+c2)

	var out, errb bytes.Buffer
	code := cmdCompare(linkState(t), svc, []string{pair, "HEAD"}, &out, &errb)
	if code != 0 {
		t.Fatalf("compare %s HEAD: exit %d (stderr %q)", pair, code, errb.String())
	}

	hist := svc.LinkHistory(context.Background())
	if len(hist) != 1 {
		t.Fatalf("LinkHistory = %v, want exactly 1 entry (the link side only, not HEAD)", hist)
	}
	if hist[0].Link != pair {
		t.Errorf("recorded Link = %q, want %q", hist[0].Link, pair)
	}
}

// TestCompareRecordsBothSidesWhenBothAreLinks.
func TestCompareRecordsBothSidesWhenBothAreLinks(t *testing.T) {
	t.Parallel()
	dir, c1, c2, c3 := linkCompareRepo(t)
	svc := compareHistSvc(t, dir)
	pair := mustLink(t, dir, "--pair", c1+".."+c2)
	commit := mustLink(t, dir, "--rev", c3)

	var out, errb bytes.Buffer
	code := cmdCompare(linkState(t), svc, []string{pair, commit}, &out, &errb)
	if code != 0 {
		t.Fatalf("compare %s %s: exit %d (stderr %q)", pair, commit, code, errb.String())
	}

	hist := svc.LinkHistory(context.Background())
	if len(hist) != 2 {
		t.Fatalf("LinkHistory = %v, want 2 entries", hist)
	}
	got := map[string]bool{hist[0].Link: true, hist[1].Link: true}
	if !got[pair] || !got[commit] {
		t.Fatalf("LinkHistory = %v, want both %q and %q", hist, pair, commit)
	}
}

// TestCompareDoesNotRecordOnFailure: R8 — nothing is recorded when compare
// fails at the FIRST possible point (an unresolvable positional), even
// though the left side is a perfectly good link.
func TestCompareDoesNotRecordOnFailure(t *testing.T) {
	t.Parallel()
	dir, c1, c2, _ := linkCompareRepo(t)
	svc := compareHistSvc(t, dir)
	pair := mustLink(t, dir, "--pair", c1+".."+c2)

	var out, errb bytes.Buffer
	code := cmdCompare(linkState(t), svc, []string{pair, "not-a-real-rev"}, &out, &errb)
	if code == 0 {
		t.Fatalf("compare %s not-a-real-rev: exit 0, want a failure", pair)
	}

	if hist := svc.LinkHistory(context.Background()); len(hist) != 0 {
		t.Fatalf("LinkHistory = %v, want no entries after a failed compare", hist)
	}
}

// TestComparePatchOfALinkRecordsIt: R8's other half. --patch on a bounded
// link used to be the one failure that happened AFTER both positionals
// resolved (a refusal), and this test pinned that it recorded nothing. The
// refusal is gone — that comparison renders — so what is left to pin is the
// success path: a link that compared, with --patch, is recorded like any other.
func TestComparePatchOfALinkRecordsIt(t *testing.T) {
	t.Parallel()
	dir, c1, c2, _ := linkCompareRepo(t)
	svc := compareHistSvc(t, dir)
	pair := mustLink(t, dir, "--pair", c1+".."+c2)

	var out, errb bytes.Buffer
	if code := cmdCompare(linkState(t), svc, []string{"--patch", pair}, &out, &errb); code != 0 {
		t.Fatalf("compare --patch %s: exit %d (stderr %q)", pair, code, errb.String())
	}
	if hist := svc.LinkHistory(context.Background()); len(hist) != 1 || hist[0].Link != pair {
		t.Fatalf("LinkHistory = %v, want the compared link", hist)
	}
}

// TestCompareAStashLinkListsItsUntrackedFiles is spec §3.4 at the CLI: a `-u`
// stash keeps its untracked files in a third parent that `<parent>..<stash>`
// cannot see, and the link must still mean "what I stashed".
func TestCompareAStashLinkListsItsUntrackedFiles(t *testing.T) {
	t.Parallel()
	dir := newCLIRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "scratch.txt"), []byte("u\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "stash", "push", "-u", "-m", "wip")
	sha := strings.TrimSpace(gitOut(t, dir, "rev-parse", "stash@{0}"))
	parent := strings.TrimSpace(gitOut(t, dir, "rev-parse", "stash@{0}^1"))

	// The arms differ: git's own first-parent diff does not name the file.
	if plain := gitOut(t, dir, "diff", "--name-only", parent, sha); strings.Contains(plain, "scratch.txt") {
		t.Fatalf("fixture broken: the plain diff already lists scratch.txt: %q", plain)
	}
	pair := mustLink(t, dir, "--pair", parent+".."+sha)
	code, out, errb := runCLI(t, dir, "compare", parent, pair)
	if code != 0 {
		t.Fatalf("exit %d (stderr %q)", code, errb)
	}
	if !strings.Contains(out, "A\tscratch.txt") || !strings.Contains(out, "M\tREADME.md") {
		t.Errorf("compare <parent> <stash link> = %q, want scratch.txt added and README.md modified", out)
	}
}
