package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
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

func TestCompareReversePairFriendlyError(t *testing.T) {
	t.Parallel()
	dir := newCLIRepo(t)
	code, _, errb := runCLI(t, dir, "compare", "@worktree", "HEAD") // reverse order
	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	if strings.Contains(errb, "DiffTreeFiles") || strings.Contains(errb, "endpoint pair") {
		t.Fatalf("error leaks internals: %s", errb)
	}
	if !strings.Contains(errb, "oldest") {
		t.Fatalf("expected an ordering hint:\n%s", errb)
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
