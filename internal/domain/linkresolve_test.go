package domain

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/bookmark"
	"github.com/homeend/gigagit/internal/gittest"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/repos"
	"github.com/homeend/gigagit/internal/shelf"
)

// linkRepoWithRemote makes a repo whose origin URL yields name.
func linkRepoWithRemote(t *testing.T, name string) string {
	t.Helper()
	dir, _ := newRealRepo(t)
	runGitIn(t, dir, "remote", "add", "origin", "git@github.com:homeend/"+name+".git")
	return dir
}

// headOf returns the repo's HEAD sha.
func headOf(t *testing.T, dir string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

func TestResolveLinkPrefersTheCwdRepo(t *testing.T) {
	t.Parallel()
	here := linkRepoWithRemote(t, "gigagit")
	other := linkRepoWithRemote(t, "gigagit")
	state := filepath.Join(t.TempDir(), "repos.toml")
	// `other` is in the registry and `here` is not: the cwd must still win.
	if err := repos.Touch(state, other, "gigagit", time.Unix(9000, 0)); err != nil {
		t.Fatal(err)
	}
	l, err := model.ParseLink("gg://gigagit/README.md:3")
	if err != nil {
		t.Fatal(err)
	}
	got, err := ResolveLink(context.Background(), l, ResolveOpts{RegistryPath: state, Cwd: Open(here)})
	if err != nil {
		t.Fatalf("ResolveLink: %v", err)
	}
	if !samePathLink(got.Checkout, here) {
		t.Errorf("Checkout = %q, want the cwd repo %q", got.Checkout, here)
	}
	if got.Addr.Path != "README.md" || got.Addr.State != model.StateUnstaged {
		t.Errorf("Addr = %+v", got.Addr)
	}
	if !samePathLink(got.Addr.Worktree, here) {
		t.Errorf("Addr.Worktree = %q, want %q", got.Addr.Worktree, here)
	}
	if got.Line != 3 || got.Side != model.NoteSideNew {
		t.Errorf("line/side = %d/%s", got.Line, got.Side)
	}
}

// The cwd's OWN entry sitting in the registry too must never make it
// "ambiguous with itself" — it is deduplicated by path before any ambiguity
// check runs.
func TestResolveLinkCwdAlsoInRegistryIsNotAmbiguous(t *testing.T) {
	t.Parallel()
	here := linkRepoWithRemote(t, "gigagit")
	state := filepath.Join(t.TempDir(), "repos.toml")
	_ = repos.Touch(state, here, "gigagit", time.Unix(1000, 0))
	l, _ := model.ParseLink("gg://gigagit/README.md:1")
	got, err := ResolveLink(context.Background(), l, ResolveOpts{RegistryPath: state, Cwd: Open(here)})
	if err != nil {
		t.Fatalf("ResolveLink: %v", err)
	}
	if !samePathLink(got.Checkout, here) {
		t.Errorf("Checkout = %q, want %q", got.Checkout, here)
	}
}

func TestResolveLinkFindsTheRegistryEntryByRemoteName(t *testing.T) {
	t.Parallel()
	target := linkRepoWithRemote(t, "gigagit")
	state := filepath.Join(t.TempDir(), "repos.toml")
	_ = repos.Touch(state, target, "gigagit", time.Unix(1000, 0))
	l, _ := model.ParseLink("gg://gigagit/README.md")
	got, err := ResolveLink(context.Background(), l, ResolveOpts{RegistryPath: state})
	if err != nil {
		t.Fatalf("ResolveLink: %v", err)
	}
	if !samePathLink(got.Checkout, target) {
		t.Errorf("Checkout = %q, want %q", got.Checkout, target)
	}
}

// A registered checkout's TRUE remote name does not match the link's: the
// backfill still records the true name (it is not the link's job to decide
// it), but the entry does not become a candidate.
func TestResolveLinkBackfilledRemoteMustMatchTheLink(t *testing.T) {
	t.Parallel()
	other := linkRepoWithRemote(t, "other-repo")
	state := filepath.Join(t.TempDir(), "repos.toml")
	_ = repos.Touch(state, other, "", time.Unix(1000, 0))
	l, _ := model.ParseLink("gg://gigagit/README.md")
	_, err := ResolveLink(context.Background(), l, ResolveOpts{RegistryPath: state})
	if !errors.Is(err, ErrLinkUnknownRepo) {
		t.Fatalf("err = %v, want ErrLinkUnknownRepo", err)
	}
	got := repos.Load(state)
	if len(got) != 1 || got[0].Remote != "other-repo" {
		t.Fatalf("entries = %+v, want the TRUE remote backfilled despite the miss", got)
	}
}

// An entry written by an older gg has no remote. The resolver computes it,
// uses it, and writes it back — without reordering the MRU.
func TestResolveLinkBackfillsAnOldRegistryEntry(t *testing.T) {
	t.Parallel()
	target := linkRepoWithRemote(t, "gigagit")
	state := filepath.Join(t.TempDir(), "repos.toml")
	_ = repos.Touch(state, target, "", time.Unix(1000, 0))
	l, _ := model.ParseLink("gg://gigagit")
	if _, err := ResolveLink(context.Background(), l, ResolveOpts{RegistryPath: state}); err != nil {
		t.Fatalf("ResolveLink: %v", err)
	}
	got := repos.Load(state)
	if len(got) != 1 || got[0].Remote != "gigagit" {
		t.Fatalf("entries = %+v, want the remote backfilled", got)
	}
	if !got[0].LastOpened.Equal(time.Unix(1000, 0)) {
		t.Errorf("LastOpened = %v, want it unchanged", got[0].LastOpened)
	}
}

// Two clones of one repo: only one contains the commit, so a commit link is
// not ambiguous.
func TestResolveLinkPicksTheCheckoutContainingTheCommit(t *testing.T) {
	t.Parallel()
	a := linkRepoWithRemote(t, "gigagit")
	b := linkRepoWithRemote(t, "gigagit")
	// The two repos are seeded identically, and the test helpers freeze the
	// author/committer dates — so their seed commits share a sha and BOTH
	// would contain it. Give b a commit a cannot have.
	runGitIn(t, b, "commit", "--allow-empty", "-m", "only in b")
	sha := headOf(t, b)
	state := filepath.Join(t.TempDir(), "repos.toml")
	_ = repos.Touch(state, a, "gigagit", time.Unix(2000, 0)) // newer in MRU
	_ = repos.Touch(state, b, "gigagit", time.Unix(1000, 0))
	l, _ := model.ParseLink("gg://gigagit/README.md@" + sha + ":1")
	got, err := ResolveLink(context.Background(), l, ResolveOpts{RegistryPath: state})
	if err != nil {
		t.Fatalf("ResolveLink: %v", err)
	}
	if !samePathLink(got.Checkout, b) {
		t.Errorf("Checkout = %q, want the containing clone %q", got.Checkout, b)
	}
	if got.Commit != sha || got.Addr.Commit != sha {
		t.Errorf("commit = %q / %q, want %q", got.Commit, got.Addr.Commit, sha)
	}
}

// A SHORT sha in the link comes back as the full one.
func TestResolveLinkExpandsAShortSHA(t *testing.T) {
	t.Parallel()
	a := linkRepoWithRemote(t, "gigagit")
	sha := headOf(t, a)
	state := filepath.Join(t.TempDir(), "repos.toml")
	_ = repos.Touch(state, a, "gigagit", time.Unix(1000, 0))
	l, _ := model.ParseLink("gg://gigagit/README.md@" + sha[:8])
	got, err := ResolveLink(context.Background(), l, ResolveOpts{RegistryPath: state})
	if err != nil {
		t.Fatalf("ResolveLink: %v", err)
	}
	if got.Commit != sha {
		t.Errorf("Commit = %q, want the full %q", got.Commit, sha)
	}
}

// Two checkouts of one repo, a WORKING-TREE link, neither live: refuse.
func TestResolveLinkRefusesAnAmbiguousWorkingTreeLink(t *testing.T) {
	t.Parallel()
	a := linkRepoWithRemote(t, "gigagit")
	b := linkRepoWithRemote(t, "gigagit")
	state := filepath.Join(t.TempDir(), "repos.toml")
	_ = repos.Touch(state, a, "gigagit", time.Unix(1000, 0))
	_ = repos.Touch(state, b, "gigagit", time.Unix(2000, 0))
	l, _ := model.ParseLink("gg://gigagit/README.md:1")
	_, err := ResolveLink(context.Background(), l, ResolveOpts{RegistryPath: state})
	if !errors.Is(err, ErrLinkAmbiguous) {
		t.Fatalf("err = %v, want ErrLinkAmbiguous", err)
	}
	if !strings.Contains(err.Error(), a) || !strings.Contains(err.Error(), b) {
		t.Errorf("error %q must list both candidates", err)
	}
}

// …unless exactly one of them has a live gg session.
func TestResolveLinkPrefersTheLiveCheckout(t *testing.T) {
	t.Parallel()
	a := linkRepoWithRemote(t, "gigagit")
	b := linkRepoWithRemote(t, "gigagit")
	state := filepath.Join(t.TempDir(), "repos.toml")
	_ = repos.Touch(state, a, "gigagit", time.Unix(1000, 0))
	_ = repos.Touch(state, b, "gigagit", time.Unix(2000, 0))
	l, _ := model.ParseLink("gg://gigagit/README.md:1")
	got, err := ResolveLink(context.Background(), l, ResolveOpts{
		RegistryPath: state,
		LiveFn:       func(_, checkout string) bool { return samePathLink(checkout, a) },
	})
	if err != nil {
		t.Fatalf("ResolveLink: %v", err)
	}
	if !samePathLink(got.Checkout, a) {
		t.Errorf("Checkout = %q, want the live one %q", got.Checkout, a)
	}
}

// Three checkouts, TWO live: the liveness filter narrows to those two, and
// MRU then breaks the remaining tie for a commit link.
func TestResolveLinkNarrowsToTheLiveSubsetThenAppliesMRU(t *testing.T) {
	t.Parallel()
	a := linkRepoWithRemote(t, "gigagit")
	b := linkRepoWithRemote(t, "gigagit")
	c := linkRepoWithRemote(t, "gigagit")
	sha := headOf(t, a) // identical seed commit across all three
	state := filepath.Join(t.TempDir(), "repos.toml")
	_ = repos.Touch(state, a, "gigagit", time.Unix(1000, 0))
	_ = repos.Touch(state, b, "gigagit", time.Unix(3000, 0)) // newest, but NOT live
	_ = repos.Touch(state, c, "gigagit", time.Unix(2000, 0))
	l, _ := model.ParseLink("gg://gigagit/README.md@" + sha)
	got, err := ResolveLink(context.Background(), l, ResolveOpts{
		RegistryPath: state,
		LiveFn: func(_, checkout string) bool {
			return samePathLink(checkout, a) || samePathLink(checkout, c)
		},
	})
	if err != nil {
		t.Fatalf("ResolveLink: %v", err)
	}
	// Live subset is {a, c}; c is the newer of the two.
	if !samePathLink(got.Checkout, c) {
		t.Errorf("Checkout = %q, want the newer live checkout %q", got.Checkout, c)
	}
}

// The WORKING-TREE variant: three checkouts, two live — still ambiguous (a
// working-tree link never takes the newest by itself), but the error must
// list exactly the LIVE subset, not the non-live third checkout.
func TestResolveLinkAmbiguousWorkingTreeListsOnlyTheLiveSubset(t *testing.T) {
	t.Parallel()
	a := linkRepoWithRemote(t, "gigagit")
	b := linkRepoWithRemote(t, "gigagit")
	c := linkRepoWithRemote(t, "gigagit")
	state := filepath.Join(t.TempDir(), "repos.toml")
	_ = repos.Touch(state, a, "gigagit", time.Unix(1000, 0))
	_ = repos.Touch(state, b, "gigagit", time.Unix(3000, 0)) // not live
	_ = repos.Touch(state, c, "gigagit", time.Unix(2000, 0))
	l, _ := model.ParseLink("gg://gigagit/README.md:1")
	_, err := ResolveLink(context.Background(), l, ResolveOpts{
		RegistryPath: state,
		LiveFn: func(_, checkout string) bool {
			return samePathLink(checkout, a) || samePathLink(checkout, c)
		},
	})
	if !errors.Is(err, ErrLinkAmbiguous) {
		t.Fatalf("err = %v, want ErrLinkAmbiguous", err)
	}
	if !strings.Contains(err.Error(), a) || !strings.Contains(err.Error(), c) {
		t.Errorf("error %q must list the two live checkouts", err)
	}
	if strings.Contains(err.Error(), b) {
		t.Errorf("error %q must NOT list the non-live checkout %s", err, b)
	}
}

func TestResolveLinkLocalFormSplitsTheCheckoutFromThePath(t *testing.T) {
	t.Parallel()
	dir, _ := newRealRepo(t)
	state := filepath.Join(t.TempDir(), "repos.toml")
	_ = repos.Touch(state, dir, "", time.Unix(1000, 0))
	l, err := model.ParseLink("gg://" + filepath.ToSlash(dir) + "/README.md:5")
	if err != nil {
		t.Fatal(err)
	}
	got, err := ResolveLink(context.Background(), l, ResolveOpts{RegistryPath: state})
	if err != nil {
		t.Fatalf("ResolveLink: %v", err)
	}
	if !samePathLink(got.Checkout, dir) {
		t.Errorf("Checkout = %q, want %q", got.Checkout, dir)
	}
	if got.Addr.Path != "README.md" || got.Line != 5 {
		t.Errorf("Addr.Path = %q, Line = %d", got.Addr.Path, got.Line)
	}
}

// A local link naming the checkout root itself (no trailing path) resolves
// with an EMPTY Addr.Path — linkSplit's ("", true) case.
func TestResolveLinkLocalFormAtCheckoutRootHasEmptyPath(t *testing.T) {
	t.Parallel()
	dir, _ := newRealRepo(t)
	state := filepath.Join(t.TempDir(), "repos.toml")
	_ = repos.Touch(state, dir, "", time.Unix(1000, 0))
	l, err := model.ParseLink("gg://" + filepath.ToSlash(dir))
	if err != nil {
		t.Fatal(err)
	}
	got, err := ResolveLink(context.Background(), l, ResolveOpts{RegistryPath: state})
	if err != nil {
		t.Fatalf("ResolveLink: %v", err)
	}
	if got.Addr.Path != "" {
		t.Errorf("Addr.Path = %q, want empty (the checkout itself)", got.Addr.Path)
	}
}

// A registered worktree NESTED inside the cwd's own checkout must win the
// longest-prefix contest — the cwd must not shadow it just because the cwd
// sorts first among ties. Regression for the bug where the cwd was added as
// a candidate OUTSIDE the registry's longest-prefix contest.
func TestResolveLinkLocalFormNestedWorktreeWinsOverParentCwd(t *testing.T) {
	t.Parallel()
	parent, _ := newRealRepo(t)
	nested := filepath.Join(parent, "wt")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	seedRepoAt(t, nested)
	if err := os.MkdirAll(filepath.Join(nested, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "sub", "file.go"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitIn(t, nested, "add", "sub/file.go")
	runGitIn(t, nested, "commit", "-m", "add sub file")
	state := filepath.Join(t.TempDir(), "repos.toml")
	_ = repos.Touch(state, nested, "", time.Unix(1000, 0))
	l, err := model.ParseLink("gg://" + filepath.ToSlash(nested) + "/sub/file.go:1")
	if err != nil {
		t.Fatal(err)
	}
	got, err := ResolveLink(context.Background(), l, ResolveOpts{RegistryPath: state, Cwd: Open(parent)})
	if err != nil {
		t.Fatalf("ResolveLink: %v", err)
	}
	if !samePathLink(got.Checkout, nested) {
		t.Errorf("Checkout = %q, want the nested worktree %q, not the parent cwd", got.Checkout, nested)
	}
	if got.Addr.Path != "sub/file.go" {
		t.Errorf("Addr.Path = %q, want sub/file.go", got.Addr.Path)
	}
}

// A checkout that MOVED: the recorded path is gone, but an entry whose
// directory name matches a segment of the link's path takes over.
func TestResolveLinkLocalFormFallsBackToAMovedCheckout(t *testing.T) {
	t.Parallel()
	moved := filepath.Join(t.TempDir(), "test-1")
	if err := os.MkdirAll(moved, 0o755); err != nil {
		t.Fatal(err)
	}
	seedRepoAt(t, moved)
	state := filepath.Join(t.TempDir(), "repos.toml")
	_ = repos.Touch(state, moved, "", time.Unix(1000, 0))
	// The link still names the OLD location, which no longer exists.
	l, err := model.ParseLink("gg:///gone/place/test-1/README.md:2")
	if err != nil {
		t.Fatal(err)
	}
	got, err := ResolveLink(context.Background(), l, ResolveOpts{RegistryPath: state})
	if err != nil {
		t.Fatalf("ResolveLink: %v", err)
	}
	if !samePathLink(got.Checkout, moved) {
		t.Errorf("Checkout = %q, want the moved checkout %q", got.Checkout, moved)
	}
	if got.Addr.Path != "README.md" {
		t.Errorf("Addr.Path = %q, want README.md", got.Addr.Path)
	}
}

// The link's OWN location still exists as a real, unregistered checkout — an
// ancestor holds a .git. A DIFFERENT, registered repo happens to share the
// same base name ("test-1"): the moved-checkout fallback must NOT guess that
// one. Ruling P5a.
func TestResolveLinkLocalFormRefusesWhenTheOldLocationStillExists(t *testing.T) {
	t.Parallel()
	unregistered := filepath.Join(t.TempDir(), "test-1")
	if err := os.MkdirAll(unregistered, 0o755); err != nil {
		t.Fatal(err)
	}
	seedRepoAt(t, unregistered)

	registered := filepath.Join(t.TempDir(), "test-1") // same base name, different repo
	if err := os.MkdirAll(registered, 0o755); err != nil {
		t.Fatal(err)
	}
	seedRepoAt(t, registered)
	state := filepath.Join(t.TempDir(), "repos.toml")
	_ = repos.Touch(state, registered, "", time.Unix(1000, 0))

	l, err := model.ParseLink("gg://" + filepath.ToSlash(unregistered) + "/README.md:2")
	if err != nil {
		t.Fatal(err)
	}
	_, err = ResolveLink(context.Background(), l, ResolveOpts{RegistryPath: state})
	if !errors.Is(err, ErrLinkUnknownRepo) {
		t.Fatalf("err = %v, want ErrLinkUnknownRepo (not a guess at %s)", err, registered)
	}
}

// The same refusal, with the file NESTED below the checkout top: the
// surviving checkout is two directories above the link's own path, so
// "does the deepest existing directory hold a .git?" is not enough on its own
// — the fallback must also refuse an entry whose matched prefix still exists
// as a checkout. Ruling P5a again.
func TestResolveLinkLocalFormRefusesWhenANestedOldLocationStillExists(t *testing.T) {
	t.Parallel()
	unregistered := filepath.Join(t.TempDir(), "test-1")
	if err := os.MkdirAll(filepath.Join(unregistered, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	seedRepoAt(t, unregistered)

	registered := filepath.Join(t.TempDir(), "test-1") // same base name, different repo
	if err := os.MkdirAll(registered, 0o755); err != nil {
		t.Fatal(err)
	}
	seedRepoAt(t, registered)
	state := filepath.Join(t.TempDir(), "repos.toml")
	_ = repos.Touch(state, registered, "", time.Unix(1000, 0))

	l, err := model.ParseLink("gg://" + filepath.ToSlash(unregistered) + "/sub/README.md:2")
	if err != nil {
		t.Fatal(err)
	}
	_, err = ResolveLink(context.Background(), l, ResolveOpts{RegistryPath: state})
	if !errors.Is(err, ErrLinkUnknownRepo) {
		t.Fatalf("err = %v, want ErrLinkUnknownRepo (not a guess at %s)", err, registered)
	}
}

// A commit link whose ONLY candidate (the cwd itself) does not contain the
// named sha is refused as unknown, not silently pointed at the wrong commit.
func TestResolveLinkCommitNotInTheOnlyCandidateIsUnknown(t *testing.T) {
	t.Parallel()
	here := linkRepoWithRemote(t, "gigagit")
	elsewhere := linkRepoWithRemote(t, "gigagit")
	runGitIn(t, elsewhere, "commit", "--allow-empty", "-m", "only over there")
	sha := headOf(t, elsewhere)
	l, _ := model.ParseLink("gg://gigagit@" + sha)
	_, err := ResolveLink(context.Background(), l, ResolveOpts{Cwd: Open(here)})
	if !errors.Is(err, ErrLinkUnknownRepo) {
		t.Fatalf("err = %v, want ErrLinkUnknownRepo", err)
	}
}

func TestResolveLinkUnknownRepo(t *testing.T) {
	t.Parallel()
	state := filepath.Join(t.TempDir(), "repos.toml")
	l, _ := model.ParseLink("gg://nobody-has-this/README.md")
	_, err := ResolveLink(context.Background(), l, ResolveOpts{RegistryPath: state})
	if !errors.Is(err, ErrLinkUnknownRepo) {
		t.Fatalf("err = %v, want ErrLinkUnknownRepo", err)
	}
	if !strings.Contains(err.Error(), "open it once in gg") {
		t.Errorf("error %q should say how to fix it", err)
	}
}

// A missing registry file must not stop a cwd resolve — that is exactly the
// e2e sandbox's situation.
func TestResolveLinkWorksWithNoRegistryFile(t *testing.T) {
	t.Parallel()
	here, _ := newRealRepo(t)
	l, err := model.ParseLink("gg://" + filepath.ToSlash(here) + "/README.md")
	if err != nil {
		t.Fatal(err)
	}
	got, err := ResolveLink(context.Background(), l, ResolveOpts{RegistryPath: "", Cwd: Open(here)})
	if err != nil {
		t.Fatalf("ResolveLink: %v", err)
	}
	if !samePathLink(got.Checkout, here) {
		t.Errorf("Checkout = %q, want %q", got.Checkout, here)
	}
}

func TestResolveLinkStagedTargetKeepsTheWorktree(t *testing.T) {
	t.Parallel()
	here, _ := newRealRepo(t)
	l, _ := model.ParseLink("gg://" + filepath.ToSlash(here) + "/README.md@staged:2")
	got, err := ResolveLink(context.Background(), l, ResolveOpts{Cwd: Open(here)})
	if err != nil {
		t.Fatalf("ResolveLink: %v", err)
	}
	if got.Addr.State != model.StateStaged || !samePathLink(got.Addr.Worktree, here) {
		t.Errorf("Addr = %+v, want staged in %q", got.Addr, here)
	}
}

// A crafted remote-named link whose path escapes the checkout via ".." must
// be refused, not silently resolved outside the repo.
func TestResolveLinkRefusesAPathThatEscapesTheCheckout(t *testing.T) {
	t.Parallel()
	target := linkRepoWithRemote(t, "gigagit")
	state := filepath.Join(t.TempDir(), "repos.toml")
	_ = repos.Touch(state, target, "gigagit", time.Unix(1000, 0))
	l, err := model.ParseLink("gg://gigagit/../secret")
	if err != nil {
		t.Fatal(err)
	}
	_, err = ResolveLink(context.Background(), l, ResolveOpts{RegistryPath: state})
	if !errors.Is(err, model.ErrLink) {
		t.Fatalf("err = %v, want model.ErrLink", err)
	}
}

// StateCommitted is FileState's zero value: a programmatically built Link
// (not via ParseLink, which never leaves this combination) must not reach
// ResolveRev with an empty ref.
func TestResolveLinkRefusesACommittedTargetWithNoSHA(t *testing.T) {
	t.Parallel()
	l := model.Link{Repo: model.LinkRepo{Name: "gigagit"}, Target: model.LinkTarget{State: model.StateCommitted}}
	_, err := ResolveLink(context.Background(), l, ResolveOpts{})
	if !errors.Is(err, model.ErrLink) {
		t.Fatalf("err = %v, want model.ErrLink", err)
	}
}

// A5(b): a registry entry for a repo with NO remote is probed ONCE. The
// answer is memoised as repos.NoRemote, so a second resolve never opens that
// checkout again — previously every resolve re-ran RepoName (two git
// invocations under the entry's own Read reservation) forever.
func TestResolveLinkMemoisesARemotelessEntry(t *testing.T) {
	t.Parallel()
	remoteless, _ := newRealRepo(t) // no remote configured
	state := filepath.Join(t.TempDir(), "repos.toml")
	_ = repos.Touch(state, remoteless, "", time.Unix(1000, 0))

	var opened []string
	opts := ResolveOpts{RegistryPath: state, OpenFn: func(dir string) *Service {
		opened = append(opened, dir)
		return Open(dir)
	}}
	l, _ := model.ParseLink("gg://gigagit/README.md")
	if _, err := ResolveLink(context.Background(), l, opts); !errors.Is(err, ErrLinkUnknownRepo) {
		t.Fatalf("err = %v, want ErrLinkUnknownRepo", err)
	}
	if len(opened) != 1 {
		t.Fatalf("first resolve opened %v, want exactly one probe", opened)
	}
	got := repos.Load(state)
	if len(got) != 1 || got[0].Remote != repos.NoRemote {
		t.Fatalf("entries = %+v, want the %q sentinel memoised", got, repos.NoRemote)
	}
	if !got[0].LastOpened.Equal(time.Unix(1000, 0)) {
		t.Errorf("LastOpened = %v, want it unchanged (resolving is not opening)", got[0].LastOpened)
	}

	opened = nil
	if _, err := ResolveLink(context.Background(), l, opts); !errors.Is(err, ErrLinkUnknownRepo) {
		t.Fatalf("second resolve err = %v, want ErrLinkUnknownRepo", err)
	}
	if len(opened) != 0 {
		t.Errorf("second resolve opened %v, want no probe at all", opened)
	}
}

// The sentinel is a sentinel, never a name: a link that literally says "-"
// must not match the memoised entry.
func TestResolveLinkSentinelNeverMatchesALink(t *testing.T) {
	t.Parallel()
	remoteless, _ := newRealRepo(t)
	state := filepath.Join(t.TempDir(), "repos.toml")
	_ = repos.Touch(state, remoteless, repos.NoRemote, time.Unix(1000, 0))
	l, _ := model.ParseLink("gg://" + repos.NoRemote + "/README.md")
	if _, err := ResolveLink(context.Background(), l, ResolveOpts{RegistryPath: state}); !errors.Is(err, ErrLinkUnknownRepo) {
		t.Fatalf("err = %v, want ErrLinkUnknownRepo", err)
	}
}

// A5(a): when the cwd already answers a WORKING-TREE link, ResolveLink
// short-circuits on it — so the registry walk (which would open every entry
// with no stored remote) must not happen at all.
func TestResolveLinkCwdMatchStopsBeforeTheRegistryWalk(t *testing.T) {
	t.Parallel()
	here := linkRepoWithRemote(t, "gigagit")
	other, _ := newRealRepo(t) // no remote: probing it would cost git calls
	state := filepath.Join(t.TempDir(), "repos.toml")
	_ = repos.Touch(state, other, "", time.Unix(9000, 0))

	var opened []string
	l, _ := model.ParseLink("gg://gigagit/README.md:3")
	got, err := ResolveLink(context.Background(), l, ResolveOpts{
		RegistryPath: state, Cwd: Open(here),
		OpenFn: func(dir string) *Service {
			opened = append(opened, dir)
			return Open(dir)
		},
	})
	if err != nil {
		t.Fatalf("ResolveLink: %v", err)
	}
	if !samePathLink(got.Checkout, here) {
		t.Errorf("Checkout = %q, want the cwd %q", got.Checkout, here)
	}
	if len(opened) != 0 {
		t.Errorf("opened %v, want no other checkout opened once the cwd matched", opened)
	}
	if e := repos.Load(state); len(e) != 1 || e[0].Remote != "" {
		t.Errorf("entries = %+v, want the untouched entry (no probe, no backfill)", e)
	}
}

// B2: path.Clean knows nothing about '\', so a crafted link must be checked
// on the backslash-normalised text too.
func TestResolveLinkRefusesABackslashEscape(t *testing.T) {
	t.Parallel()
	target := linkRepoWithRemote(t, "gigagit")
	state := filepath.Join(t.TempDir(), "repos.toml")
	_ = repos.Touch(state, target, "gigagit", time.Unix(1000, 0))
	l := model.Link{
		Repo: model.LinkRepo{Name: "gigagit"}, Path: `..\..\secret`,
		Side: model.NoteSideNew, Target: model.LinkTarget{State: model.StateUnstaged},
	}
	_, err := ResolveLink(context.Background(), l, ResolveOpts{RegistryPath: state})
	if !errors.Is(err, model.ErrLink) {
		t.Fatalf("err = %v, want model.ErrLink", err)
	}
}

// B9: a cancelled resolve reports cancellation, not "not in gg history".
func TestResolveLinkReportsCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	l, _ := model.ParseLink("gg://gigagit/README.md")
	_, err := ResolveLink(ctx, l, ResolveOpts{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

// B3: the moved-checkout fallback must survive a .git ABOVE the gone path's
// deepest surviving ancestor — a dotfiles repo in $HOME used to disable it
// for every link underneath.
func TestResolveLinkMovedCheckoutIgnoresADotfilesRepoAbove(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	// $HOME itself is a git repository (a dotfiles checkout).
	seedRepoAt(t, home)
	// …and the link's old location lived under an ordinary directory in it,
	// which still exists but is NOT a checkout.
	code := filepath.Join(home, "code")
	if err := os.MkdirAll(code, 0o755); err != nil {
		t.Fatal(err)
	}
	moved := filepath.Join(t.TempDir(), "test-1")
	if err := os.MkdirAll(moved, 0o755); err != nil {
		t.Fatal(err)
	}
	seedRepoAt(t, moved)
	state := filepath.Join(t.TempDir(), "repos.toml")
	_ = repos.Touch(state, moved, "", time.Unix(1000, 0))

	l, err := model.ParseLink("gg://" + filepath.ToSlash(filepath.Join(code, "gone", "test-1", "README.md")) + ":2")
	if err != nil {
		t.Fatal(err)
	}
	got, err := ResolveLink(context.Background(), l, ResolveOpts{RegistryPath: state})
	if err != nil {
		t.Fatalf("ResolveLink: %v", err)
	}
	if !samePathLink(got.Checkout, moved) {
		t.Errorf("Checkout = %q, want the moved checkout %q", got.Checkout, moved)
	}
	if got.Addr.Path != "README.md" {
		t.Errorf("Addr.Path = %q, want README.md", got.Addr.Path)
	}
}

// SamePath is exported for Task 6's CLI to reuse.
func TestSamePath(t *testing.T) {
	t.Parallel()
	if !SamePath("/a/b/c", "/a/b/c/") {
		t.Errorf("SamePath should ignore a trailing slash")
	}
	if !SamePath(filepath.FromSlash("/a/b"), "/a/b") {
		t.Errorf("SamePath should compare across separator styles")
	}
	if SamePath("/a/b", "/a/c") {
		t.Errorf("SamePath should distinguish different paths")
	}
}

// seedRepoAt initialises a real repo with one commit at an existing dir
// (gittest.BasicRepo chooses its own dir, which the moved-checkout test
// cannot use — it needs a directory with a specific base NAME).
func seedRepoAt(t *testing.T, dir string) {
	t.Helper()
	runGitIn(t, dir, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitIn(t, dir, "add", "README.md")
	runGitIn(t, dir, "commit", "-m", "seed")
}

// linkRepoWithBranch makes a repo with a remote name, a second commit on
// branch, and returns the checkout. The default branch really is "main":
// linkRepoWithRemote → newRealRepo → gittest.BasicRepo runs
// `git init -b main` (internal/gittest/template.go:88).
func linkRepoWithBranch(t *testing.T, name, branch string) string {
	t.Helper()
	dir := linkRepoWithRemote(t, name)
	runGitIn(t, dir, "checkout", "-q", "-b", branch)
	if err := os.WriteFile(filepath.Join(dir, "feat.txt"), []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitIn(t, dir, "add", ".")
	runGitIn(t, dir, "commit", "-q", "-m", "feat work")
	runGitIn(t, dir, "checkout", "-q", "main")
	return dir
}

func TestResolveLinkPreviewResolvesOnTheCheckoutWithBothBranches(t *testing.T) {
	t.Parallel()
	with := linkRepoWithBranch(t, "gigagit", "feat/x")
	without := linkRepoWithRemote(t, "gigagit") // main only
	state := filepath.Join(t.TempDir(), "repos.toml")
	// `without` is the MORE recently opened entry: only the both-branch filter
	// can keep the resolve off it.
	if err := repos.Touch(state, with, "gigagit", time.Unix(1000, 0)); err != nil {
		t.Fatal(err)
	}
	if err := repos.Touch(state, without, "gigagit", time.Unix(9000, 0)); err != nil {
		t.Fatal(err)
	}
	l, err := model.ParseLink("gg://gigagit/feat.txt@main...feat/x:2")
	if err != nil {
		t.Fatal(err)
	}
	got, err := ResolveLink(context.Background(), l, ResolveOpts{RegistryPath: state})
	if err != nil {
		t.Fatalf("ResolveLink: %v", err)
	}
	if !samePathLink(got.Checkout, with) {
		t.Fatalf("Checkout = %q, want the checkout holding both branches %q", got.Checkout, with)
	}
	if got.Preview == nil || !got.Preview.OK() {
		t.Fatal("Resolved.Preview must carry the resolved note set")
	}
	if got.Preview.Source != "feat/x" || got.Preview.Target != "main" {
		t.Errorf("Preview pair = %s...%s", got.Preview.Target, got.Preview.Source)
	}
	if got.Commit != got.Preview.Tip || got.Addr.Commit != got.Preview.Tip {
		t.Errorf("Commit/Addr.Commit = %q/%q, want the tip %q", got.Commit, got.Addr.Commit, got.Preview.Tip)
	}
	if got.Addr.State != model.StateCommitted || got.Addr.Path != "feat.txt" {
		t.Errorf("Addr = %+v", got.Addr)
	}
	if got.Line != 2 || got.Side != model.NoteSideNew {
		t.Errorf("line/side = %d/%s", got.Line, got.Side)
	}
}

// The cwd is NOT exempt from the both-branch filter for a preview link: a
// working-tree link short-circuits on the cwd, but a preview that the cwd
// cannot show must still resolve elsewhere.
func TestResolveLinkPreviewCwdIsNotExemptFromTheBranchFilter(t *testing.T) {
	t.Parallel()
	here := linkRepoWithRemote(t, "gigagit") // main only: cannot show the pair
	with := linkRepoWithBranch(t, "gigagit", "feat/x")
	state := filepath.Join(t.TempDir(), "repos.toml")
	if err := repos.Touch(state, with, "gigagit", time.Unix(1000, 0)); err != nil {
		t.Fatal(err)
	}
	l, _ := model.ParseLink("gg://gigagit@main...feat/x")
	got, err := ResolveLink(context.Background(), l, ResolveOpts{RegistryPath: state, Cwd: Open(here)})
	if err != nil {
		t.Fatalf("ResolveLink: %v", err)
	}
	if !samePathLink(got.Checkout, with) {
		t.Errorf("Checkout = %q, want %q — the cwd has no feat/x", got.Checkout, with)
	}
	if got.Addr.Path != "" {
		t.Errorf("Addr.Path = %q, want empty (a repo-level preview link)", got.Addr.Path)
	}
}

func TestResolveLinkPreviewCwdWinsTheTie(t *testing.T) {
	t.Parallel()
	here := linkRepoWithBranch(t, "gigagit", "feat/x")
	other := linkRepoWithBranch(t, "gigagit", "feat/x")
	state := filepath.Join(t.TempDir(), "repos.toml")
	_ = repos.Touch(state, other, "gigagit", time.Unix(9000, 0))
	l, _ := model.ParseLink("gg://gigagit@main...feat/x")
	got, err := ResolveLink(context.Background(), l, ResolveOpts{RegistryPath: state, Cwd: Open(here)})
	if err != nil {
		t.Fatalf("ResolveLink: %v", err)
	}
	if !samePathLink(got.Checkout, here) {
		t.Errorf("Checkout = %q, want the cwd %q", got.Checkout, here)
	}
}

func TestResolveLinkPreviewWithNoCandidateIsUnknown(t *testing.T) {
	t.Parallel()
	here := linkRepoWithRemote(t, "gigagit")
	state := filepath.Join(t.TempDir(), "repos.toml")
	l, _ := model.ParseLink("gg://gigagit@main...feat/nope")
	_, err := ResolveLink(context.Background(), l, ResolveOpts{RegistryPath: state, Cwd: Open(here)})
	if !errors.Is(err, ErrLinkUnknownRepo) {
		t.Fatalf("err = %v, want ErrLinkUnknownRepo", err)
	}
}

// A programmatically built commit link with no sha is still refused; the guard
// must not fire for a preview link, which legitimately has none.
func TestResolveLinkStillRefusesACommitLinkWithNoSHA(t *testing.T) {
	t.Parallel()
	here := linkRepoWithRemote(t, "gigagit")
	l := model.Link{Repo: model.LinkRepo{Name: "gigagit"}, Path: "a.go",
		Target: model.LinkTarget{State: model.StateCommitted}}
	_, err := ResolveLink(context.Background(), l, ResolveOpts{Cwd: Open(here)})
	if !errors.Is(err, model.ErrLink) {
		t.Fatalf("err = %v, want an ErrLink refusal", err)
	}
}

// LocateLink answers only "which checkout, and what path inside it" — and it
// must answer for the two link kinds ResolveLink deliberately refuses. The
// local form is the case that matters: its checkout and its file path arrive
// UNDIVIDED in Repo.Abs, and only the registry can split them, so a consumer
// that evaluates a link's own target (domain.EvalLink) has no other way to
// learn the path.
func TestLocateLinkSplitsALocalLinkForEveryTarget(t *testing.T) {
	t.Parallel()
	here, svc := newRealRepo(t)
	head := headOf(t, here)
	abs := filepath.ToSlash(here)
	for _, tc := range []struct{ name, link, wantPath string }{
		{"working tree", "gg://" + abs + "/README.md", "README.md"},
		{"staged", "gg://" + abs + "/README.md@staged", "README.md"},
		{"commit", "gg://" + abs + "/README.md@" + head, "README.md"},
		{"ref tip", "gg://" + abs + "/README.md@ref:main", "README.md"},
		{"change-set", "gg://" + abs + "/README.md@" + head + ".." + head, "README.md"},
		{"repo only", "gg://" + abs, ""},
		{"ref, no path", "gg://" + abs + "@ref:main", ""},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			l, err := model.ParseLink(tc.link)
			if err != nil {
				t.Fatalf("ParseLink(%q): %v", tc.link, err)
			}
			checkout, rel, err := LocateLink(context.Background(), l, ResolveOpts{Cwd: svc})
			if err != nil {
				t.Fatalf("LocateLink(%q): %v", tc.link, err)
			}
			if !samePathLink(checkout, here) {
				t.Errorf("checkout = %q, want %q", checkout, here)
			}
			if rel != tc.wantPath {
				t.Errorf("relPath = %q, want %q", rel, tc.wantPath)
			}
		})
	}
}

// TestResolveLinkRefYieldsTheTipAndKeepsTheName pins R2: a @ref: link resolves
// so a consumer can navigate it, and the NAME survives resolution — freezing
// the tip at resolve time and dropping the name is exactly what the preview
// lane refuses to do.
func TestResolveLinkRefYieldsTheTipAndKeepsTheName(t *testing.T) {
	t.Parallel()
	dir := gittest.BasicRepo(t, "hello\n")
	svc := Open(dir)
	head, _, err := svc.ResolveRev(context.Background(), "main")
	if err != nil {
		t.Fatal(err)
	}
	l, err := model.ParseLink(model.Link{
		Repo:   model.LinkRepo{Abs: filepath.ToSlash(dir)},
		Target: model.LinkTarget{State: model.StateCommitted, Ref: "main"},
		Side:   model.NoteSideNew,
	}.String())
	if err != nil {
		t.Fatal(err)
	}
	res, err := ResolveLink(context.Background(), l, ResolveOpts{Cwd: svc})
	if err != nil {
		t.Fatalf("ResolveLink: %v", err)
	}
	if res.Ref != "main" {
		t.Errorf("Ref = %q, want main", res.Ref)
	}
	if res.Commit != strings.TrimSpace(head) {
		t.Errorf("Commit = %q, want the tip %q", res.Commit, head)
	}
}

// TestResolveLinkPairResolvesBothHalves pins that a change-set's two halves
// each resolve to a FULL sha, and Commit carries B — the newer end (ruling
// R4): A is captured before a second commit moves "main" forward, B rides the
// ref name and must resolve to the NEW tip, not the frozen A.
func TestResolveLinkPairResolvesBothHalves(t *testing.T) {
	t.Parallel()
	dir := gittest.BasicRepo(t, "hello\n")
	svc := Open(dir)
	ctx := context.Background()
	first, _, err := svc.ResolveRev(ctx, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	first = strings.TrimSpace(first)
	runGitIn(t, dir, "commit", "--allow-empty", "-m", "second")
	tip, _, err := svc.ResolveRev(ctx, "main")
	if err != nil {
		t.Fatal(err)
	}
	tip = strings.TrimSpace(tip)
	abs := filepath.ToSlash(dir)
	l, err := model.ParseLink("gg://" + abs + "@" + first + "..main")
	if err != nil {
		t.Fatal(err)
	}
	res, err := ResolveLink(ctx, l, ResolveOpts{Cwd: svc})
	if err != nil {
		t.Fatalf("ResolveLink: %v", err)
	}
	if res.Pair == nil {
		t.Fatal("Resolved.Pair must be set")
	}
	if res.Pair.A != first || res.Pair.B != tip {
		t.Errorf("Pair = %+v, want A=%q B=%q", res.Pair, first, tip)
	}
	if res.Commit != tip || res.Addr.Commit != tip {
		t.Errorf("Commit/Addr.Commit = %q/%q, want B %q", res.Commit, res.Addr.Commit, tip)
	}
}

// TestResolveLinkRefSkipsACheckoutWithoutTheBranch pins R3: a ref link is
// StateCommitted with an EMPTY Commit, so it skips the containment filter
// (Commit != "" is load-bearing there) — without a dedicated candidate filter
// the resolver would take the MRU checkout regardless of whether it holds the
// branch. withoutBranch is the MORE recently opened entry: only the ref
// filter can keep the resolve off it.
func TestResolveLinkRefSkipsACheckoutWithoutTheBranch(t *testing.T) {
	t.Parallel()
	withoutBranch := linkRepoWithRemote(t, "gigagit") // main only
	withBranch := linkRepoWithBranch(t, "gigagit", "feat/x")
	state := filepath.Join(t.TempDir(), "repos.toml")
	if err := repos.Touch(state, withoutBranch, "gigagit", time.Unix(9000, 0)); err != nil {
		t.Fatal(err)
	}
	if err := repos.Touch(state, withBranch, "gigagit", time.Unix(1000, 0)); err != nil {
		t.Fatal(err)
	}
	l, err := model.ParseLink("gg://gigagit@ref:feat/x")
	if err != nil {
		t.Fatal(err)
	}
	got, err := ResolveLink(context.Background(), l, ResolveOpts{RegistryPath: state})
	if err != nil {
		t.Fatalf("ResolveLink: %v", err)
	}
	if !samePathLink(got.Checkout, withBranch) {
		t.Errorf("Checkout = %q, want the checkout holding feat/x %q", got.Checkout, withBranch)
	}
	if got.Ref != "feat/x" {
		t.Errorf("Ref = %q, want feat/x", got.Ref)
	}
}

// TestResolveLinkPairRefusedWhenNoCheckoutHoldsBothHalves mirrors the preview
// refusal's words: the error names the repo and both halves.
func TestResolveLinkPairRefusedWhenNoCheckoutHoldsBothHalves(t *testing.T) {
	t.Parallel()
	here := linkRepoWithRemote(t, "gigagit")
	state := filepath.Join(t.TempDir(), "repos.toml")
	l, err := model.ParseLink("gg://gigagit@main..feat/nope")
	if err != nil {
		t.Fatal(err)
	}
	_, err = ResolveLink(context.Background(), l, ResolveOpts{RegistryPath: state, Cwd: Open(here)})
	if !errors.Is(err, ErrLinkUnknownRepo) {
		t.Fatalf("err = %v, want ErrLinkUnknownRepo", err)
	}
	if !strings.Contains(err.Error(), "gigagit") || !strings.Contains(err.Error(), "main") || !strings.Contains(err.Error(), "feat/nope") {
		t.Errorf("error %q must name the repo and both halves", err)
	}
}

// A link naming a repository this machine has never opened is located nowhere,
// and says so with ErrLinkUnknownRepo — the same sentinel ResolveLink uses, so
// a consumer keeps ONE errors.Is target.
func TestLocateLinkUnknownRepositoryIsErrLinkUnknownRepo(t *testing.T) {
	t.Parallel()
	_, svc := newRealRepo(t)
	l, err := model.ParseLink("gg://nowhere-on-this-machine@ref:main")
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = LocateLink(context.Background(), l, ResolveOpts{Cwd: svc})
	if !errors.Is(err, ErrLinkUnknownRepo) {
		t.Fatalf("err = %v, want ErrLinkUnknownRepo", err)
	}
}

// A registered OTHER checkout is located as itself, which is what lets a
// frontend compare the answer against its own top level and refuse a
// cross-repository request instead of silently answering about the wrong tree.
func TestLocateLinkFindsAnotherRegisteredCheckout(t *testing.T) {
	t.Parallel()
	here, svc := newRealRepo(t)
	other, _ := newRealRepo(t)
	state := filepath.Join(t.TempDir(), "repos.toml")
	if err := repos.Touch(state, other, "", time.Unix(9000, 0)); err != nil {
		t.Fatal(err)
	}
	l, err := model.ParseLink("gg://" + filepath.ToSlash(other) + "/README.md")
	if err != nil {
		t.Fatal(err)
	}
	checkout, rel, err := LocateLink(context.Background(), l, ResolveOpts{RegistryPath: state, Cwd: svc})
	if err != nil {
		t.Fatalf("LocateLink: %v", err)
	}
	if !samePathLink(checkout, other) {
		t.Errorf("checkout = %q, want the OTHER checkout %q (cwd is %q)", checkout, other, here)
	}
	if rel != "README.md" {
		t.Errorf("relPath = %q, want README.md", rel)
	}
}

// The escape refusal is LocateLink's too: a crafted link carrying ".." must
// not hand a consumer a path outside the checkout it named.
func TestLocateLinkRefusesAPathEscapingTheCheckout(t *testing.T) {
	t.Parallel()
	here := linkRepoWithRemote(t, "gigagit")
	l, err := model.ParseLink("gg://gigagit/../outside.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := LocateLink(context.Background(), l, ResolveOpts{Cwd: Open(here)}); !errors.Is(err, model.ErrLink) {
		t.Fatalf("err = %v, want a model.ErrLink escape refusal", err)
	}
}

// --- Task 6: the hint carries through, and degrades with a hard error only
// when the link has no other content (spec §3.3, ruling S11) --------------

// hintTestSvc opens dir with fresh, hermetic bookmark/shelf stores (never the
// user's real XDG state) and a ResolveOpts whose OpenFn always answers with
// THIS service — the only checkout every test below resolves against, so a
// hint's presence check reaches the store the test actually seeded.
func hintTestSvc(t *testing.T, dir string) (*Service, ResolveOpts) {
	t.Helper()
	svc := Open(dir)
	svc.SetBookmarkStore(bookmark.NewFileStore(t.TempDir()))
	svc.SetShelfStore(shelf.NewFileStore(t.TempDir()))
	return svc, ResolveOpts{Cwd: svc, OpenFn: func(string) *Service { return svc }}
}

// TestResolveLinkCarriesHintThroughRefArm pins ruling S2 applied to the hint:
// finishLink's ref arm returns EARLY (line ~488), so a per-arm assignment
// would silently drop the hint for this shape. The hint composes with an
// address here — a ref names a whole tree — so no presence check runs.
func TestResolveLinkCarriesHintThroughRefArm(t *testing.T) {
	t.Parallel()
	dir := gittest.BasicRepo(t, "hello\n")
	_, opts := hintTestSvc(t, dir)
	l, err := model.ParseLink("gg://" + filepath.ToSlash(dir) + "@ref:main?bookmark=b1")
	if err != nil {
		t.Fatal(err)
	}
	res, err := ResolveLink(context.Background(), l, opts)
	if err != nil {
		t.Fatalf("ResolveLink: %v", err)
	}
	if res.Hint.Kind != "bookmark" || res.Hint.ID != "b1" {
		t.Errorf("Hint = %+v, want bookmark/b1 (the ref arm's early return must not drop it)", res.Hint)
	}
}

// TestResolveLinkCarriesHintThroughPairArm is the pair arm's twin of the ref
// test above: finishLink's pair arm also returns early.
func TestResolveLinkCarriesHintThroughPairArm(t *testing.T) {
	t.Parallel()
	dir := gittest.BasicRepo(t, "hello\n")
	svc, opts := hintTestSvc(t, dir)
	ctx := context.Background()
	first, _, err := svc.ResolveRev(ctx, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	first = strings.TrimSpace(first)
	runGitIn(t, dir, "commit", "--allow-empty", "-m", "second")
	abs := filepath.ToSlash(dir)
	l, err := model.ParseLink("gg://" + abs + "@" + first + "..main?shelf=s1")
	if err != nil {
		t.Fatal(err)
	}
	res, err := ResolveLink(ctx, l, opts)
	if err != nil {
		t.Fatalf("ResolveLink: %v", err)
	}
	if res.Hint.Kind != "shelf" || res.Hint.ID != "s1" {
		t.Errorf("Hint = %+v, want shelf/s1 (the pair arm's early return must not drop it)", res.Hint)
	}
}

// TestResolveLinkWithAddressHintAbsentDoesNotError pins ruling S11's WITH-
// an-address row: domain copies the hint BLIND and never looks the entry up
// itself, so an absent id is not domain's problem — the consumer decides.
// This is the exit-0 half of the S5 pair task 6 owes (see
// TestLinkResolveAddressLessAbsentHintIsAHardError in the cli package for the
// exit-2 half on the SAME absent id).
func TestResolveLinkWithAddressHintAbsentDoesNotError(t *testing.T) {
	t.Parallel()
	dir := gittest.BasicRepo(t, "hello\n")
	head := headOf(t, dir)
	_, opts := hintTestSvc(t, dir) // no bookmark/shelf ever added: "X" is absent
	l, err := model.ParseLink("gg://" + filepath.ToSlash(dir) + "/README.md@" + head + "?shelf=X")
	if err != nil {
		t.Fatal(err)
	}
	res, err := ResolveLink(context.Background(), l, opts)
	if err != nil {
		t.Fatalf("ResolveLink: %v, want success — an address-carrying link never fails on an absent hint", err)
	}
	if res.Hint.Kind != "shelf" || res.Hint.ID != "X" {
		t.Errorf("Hint = %+v, want shelf/X copied through regardless of presence", res.Hint)
	}
	if res.Addr.Path != "README.md" || res.Addr.Commit != head {
		t.Errorf("address = %+v, want the file to still land", res.Addr)
	}
}

// TestResolveLinkAddressLessHintPresentBookmarkSucceeds is the PRESENT half
// of S11's address-less row: the hint is the link's only content, and it is
// there, so the resolve succeeds with no address at all.
func TestResolveLinkAddressLessHintPresentBookmarkSucceeds(t *testing.T) {
	t.Parallel()
	dir := gittest.BasicRepo(t, "hello\n")
	svc, opts := hintTestSvc(t, dir)
	b, err := svc.BookmarkAdd(context.Background(), model.Bookmark{
		State: model.StateUnstaged, Worktree: dir, Path: "README.md",
	})
	if err != nil {
		t.Fatalf("BookmarkAdd: %v", err)
	}
	l, err := model.ParseLink("gg://" + filepath.ToSlash(dir) + "?bookmark=" + b.ID)
	if err != nil {
		t.Fatal(err)
	}
	res, err := ResolveLink(context.Background(), l, opts)
	if err != nil {
		t.Fatalf("ResolveLink: %v, want success — the bookmark exists", err)
	}
	if res.Hint.Kind != "bookmark" || res.Hint.ID != b.ID {
		t.Errorf("Hint = %+v, want bookmark/%s", res.Hint, b.ID)
	}
	if res.Addr.Path != "" || res.Commit != "" || res.Preview != nil {
		t.Errorf("an address-less resolve must gain no address: %+v", res)
	}
}

// TestResolveLinkAddressLessHintAbsentBookmarkIsAHardError is the ABSENT half:
// with nothing else to resolve to, a missing bookmark is a hard error (spec
// §6), not a notice — the notice is a CONSUMER behaviour for the
// with-address case only.
func TestResolveLinkAddressLessHintAbsentBookmarkIsAHardError(t *testing.T) {
	t.Parallel()
	dir := gittest.BasicRepo(t, "hello\n")
	_, opts := hintTestSvc(t, dir)
	l, err := model.ParseLink("gg://" + filepath.ToSlash(dir) + "?bookmark=nope")
	if err != nil {
		t.Fatal(err)
	}
	_, err = ResolveLink(context.Background(), l, opts)
	if !errors.Is(err, model.ErrLink) {
		t.Fatalf("err = %v, want a model.ErrLink hard error", err)
	}
}

// TestResolveLinkAddressLessHintAbsentShelfIsAHardError is the shelf twin.
func TestResolveLinkAddressLessHintAbsentShelfIsAHardError(t *testing.T) {
	t.Parallel()
	dir := gittest.BasicRepo(t, "hello\n")
	_, opts := hintTestSvc(t, dir)
	l, err := model.ParseLink("gg://" + filepath.ToSlash(dir) + "?shelf=nope")
	if err != nil {
		t.Fatal(err)
	}
	_, err = ResolveLink(context.Background(), l, opts)
	if !errors.Is(err, model.ErrLink) {
		t.Fatalf("err = %v, want a model.ErrLink hard error", err)
	}
}

// TestResolveLinkAddressLessStashHintIsAlwaysAHardError pins ruling S9's
// third kind: "stash" parses (model.linkHintKinds) but has no producer and no
// presence lookup a resolver can fall back on, so an address-less "?stash="
// link is ALWAYS a hard error — never a silent pass-through into the switch
// below (which would otherwise treat it as a bare working-tree link).
func TestResolveLinkAddressLessStashHintIsAlwaysAHardError(t *testing.T) {
	t.Parallel()
	dir := gittest.BasicRepo(t, "hello\n")
	_, opts := hintTestSvc(t, dir)
	l, err := model.ParseLink("gg://" + filepath.ToSlash(dir) + "?stash=1")
	if err != nil {
		t.Fatal(err)
	}
	_, err = ResolveLink(context.Background(), l, opts)
	if !errors.Is(err, model.ErrLink) {
		t.Fatalf("err = %v, want a model.ErrLink hard error", err)
	}
}

func TestResolveLinkContentLink(t *testing.T) {
	t.Parallel()
	dir := gittest.BasicRepo(t, "hi\n")
	svc := Open(dir)
	abs := filepath.ToSlash(dir)
	with, err := model.ParseLink("gg://" + abs + "/README.md?view=content")
	if err != nil {
		t.Fatal(err)
	}
	res, err := ResolveLink(context.Background(), with, ResolveOpts{Cwd: svc})
	if err != nil {
		t.Fatalf("ResolveLink(content link with a path) = %v", err)
	}
	if res.Addr.Path != "README.md" || res.Hint != model.ContentHint {
		t.Errorf("resolved = %+v, want README.md with the content hint", res)
	}
	bare, err := model.ParseLink("gg://" + abs + "?view=content")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveLink(context.Background(), bare, ResolveOpts{Cwd: svc}); err == nil ||
		!errors.Is(err, model.ErrLink) || !strings.Contains(err.Error(), "needs a file path") {
		t.Fatalf("ResolveLink(address-less content link) = %v, want an ErrLink naming the missing path", err)
	}
}
