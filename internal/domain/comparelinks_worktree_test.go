package domain

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/gittest"
	"github.com/homeend/gigagit/internal/repos"
)

// siblingWorktrees is compareLinksFixture's repository WITHOUT a remote (so its
// links are local-form, naming a checkout path) plus a linked worktree on a
// branch "side" that adds one commit. Both checkouts are registered, as gg
// registers every checkout it opens.
func siblingWorktrees(t *testing.T) (main, linked *Service, mainAbs, linkedAbs, c1, c2, state string) {
	t.Helper()
	dir, main := newRealRepo(t)
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

	wt := filepath.Join(t.TempDir(), "linked")
	gittest.Run(t, dir, "worktree", "add", "-q", "-b", "side", wt)
	writeIn(t, wt, "side.txt", "side\n")
	gitIn(t, wt, "add", "side.txt")
	gitIn(t, wt, "commit", "-qm", "side")
	_, linked = newRealRepoAt(t, wt)

	ctx := context.Background()
	mainTop, err := main.TopLevel(ctx)
	if err != nil {
		t.Fatal(err)
	}
	linkedTop, err := linked.TopLevel(ctx)
	if err != nil {
		t.Fatal(err)
	}
	state = filepath.Join(t.TempDir(), "repos.toml")
	for i, p := range []string{mainTop, linkedTop} {
		if err := repos.Touch(state, p, "", time.Unix(int64(9000+i), 0)); err != nil {
			t.Fatal(err)
		}
	}
	return main, linked, linkAbs(mainTop), linkAbs(linkedTop), c1, c2, state
}

// The saved-comparison store is keyed by the git COMMON dir, so every worktree
// lists the same rows — a row saved in the main checkout names the main
// checkout's path. Opening it from a linked worktree is the SAME repository,
// not a cross-repository compare.
func TestCompareLinksAcceptsASiblingWorktree(t *testing.T) {
	t.Parallel()
	main, linked, mainAbs, linkedAbs, c1, c2, state := siblingWorktrees(t)
	ctx := context.Background()

	c, err := linked.CompareLinks(ctx, "gg://"+mainAbs+"@"+c1, "gg://"+mainAbs+"@"+c2, ResolveOpts{RegistryPath: state})
	if err != nil {
		t.Fatalf("main checkout's links from the linked worktree: %v", err)
	}
	wantRows(t, c.Files, "M sub/a.go", "M sub/b.go")

	// A merge preview (the saved-set shape) names refs, read from the shared
	// ref store.
	c, err = linked.CompareLinks(ctx, "gg://"+mainAbs+"@main...side", "gg://"+mainAbs+"@main...side", ResolveOpts{RegistryPath: state})
	if err != nil {
		t.Fatalf("a preview link from the linked worktree: %v", err)
	}
	if len(c.Files) != 0 {
		t.Fatalf("a set against itself = %v, want no changes", fileRows(c.Files))
	}
	fs, err := linked.EvalLinkText(ctx, "gg://"+mainAbs+"@main...side", ResolveOpts{RegistryPath: state})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(fs.Paths(), "|"); got != "side.txt" {
		t.Fatalf("preview set = %q, want side.txt", got)
	}

	// And the mirror: the linked worktree's link from the main checkout.
	c, err = main.CompareLinks(ctx, "gg://"+linkedAbs+"@"+c1, "gg://"+linkedAbs+"@"+c2, ResolveOpts{RegistryPath: state})
	if err != nil {
		t.Fatalf("linked worktree's links from the main checkout: %v", err)
	}
	wantRows(t, c.Files, "M sub/a.go", "M sub/b.go")
}

// A ref is read in the checkout the link NAMES: HEAD is per-worktree, so the
// linked worktree's HEAD must not silently become the main checkout's.
func TestCompareLinksSiblingRefReadsTheNamedCheckout(t *testing.T) {
	t.Parallel()
	main, _, _, linkedAbs, _, _, state := siblingWorktrees(t)
	ctx := context.Background()
	fs, err := main.EvalLinkText(ctx, "gg://"+linkedAbs+"@main...HEAD", ResolveOpts{RegistryPath: state})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(fs.Paths(), "|"); got != "side.txt" {
		t.Fatalf("main...HEAD of the linked worktree = %q, want side.txt (its own HEAD)", got)
	}
}

// Another worktree's working tree or index is ITS content: this checkout would
// read its own instead. Refused, and not as a cross-repository compare.
func TestCompareLinksRefusesASiblingWorkingTree(t *testing.T) {
	t.Parallel()
	_, linked, mainAbs, _, _, _, state := siblingWorktrees(t)
	ctx := context.Background()
	for _, text := range []string{"gg://" + mainAbs + "/sub/a.go", "gg://" + mainAbs + "@staged"} {
		_, err := linked.EvalLinkText(ctx, text, ResolveOpts{RegistryPath: state})
		if !errors.Is(err, ErrLinkOtherWorktree) {
			t.Errorf("%s: err = %v, want ErrLinkOtherWorktree", text, err)
		}
		if errors.Is(err, ErrLinkCrossRepo) {
			t.Errorf("%s: a sibling worktree is the same repository: %v", text, err)
		}
	}
}

// A saved merge preview outlives the worktree it was saved from (the store is
// per repository), so a remoteless repository names its MAIN checkout.
func TestPreviewAddFromALinkedWorktreeNamesTheMainCheckout(t *testing.T) {
	t.Parallel()
	_, linked, mainAbs, _, c1, c2, _ := siblingWorktrees(t)
	linked.UsePreviewsDir(t.TempDir())
	ctx := context.Background()
	if _, err := linked.PreviewAdd(ctx, "side", "main", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := linked.PairAdd(ctx, c1, c2, ""); err != nil {
		t.Fatal(err)
	}
	top, err := linked.TopLevel(ctx)
	if err != nil {
		t.Fatal(err)
	}
	gittest.Run(t, top, "branch", "other", c1)
	if _, err := linked.SymmetricPreviewAdd(ctx, "side", "other", "main", ""); err != nil {
		t.Fatal(err)
	}
	rows, err := linked.SavedCompareList(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		t.Fatal("nothing saved")
	}
	for _, r := range rows {
		for _, text := range []string{r.Left, r.Right} {
			if text != "" && !strings.HasPrefix(text, "gg://"+mainAbs+"@") {
				t.Errorf("saved link %q does not name the main checkout %s", text, mainAbs)
			}
		}
	}
}
