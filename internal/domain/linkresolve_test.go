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

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/repos"
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
