package domain

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/gittest"
)

// symRepo is previewRepo (main, feat/x) plus feat/y off main's FIRST commit
// adding y.txt, so all three names differ and the two three-dot sets are
// distinct: main...feat/x = {a.txt, b.txt}, main...feat/y = {y.txt}.
func symRepo(t *testing.T) (string, *Service) {
	t.Helper()
	dir, svc := previewRepo(t)
	gittest.Run(t, dir, "checkout", "-q", "-b", "feat/y", "main~1")
	if err := os.WriteFile(filepath.Join(dir, "y.txt"), []byte("y\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gittest.Run(t, dir, "add", "y.txt")
	gittest.Run(t, dir, "commit", "-q", "-m", "add y")
	gittest.Run(t, dir, "checkout", "-q", "main")
	return dir, svc
}

func TestSymmetricPreviewAddStoresBaseFirstALeft(t *testing.T) {
	t.Parallel()
	_, svc := symRepo(t)
	ctx := context.Background()
	c, err := svc.SymmetricPreviewAdd(ctx, "feat/x", "feat/y", "main", "")
	if err != nil {
		t.Fatal(err)
	}
	// Assert the TEXT: a double swap would round-trip through SymmetricOf.
	if !strings.HasSuffix(c.Left, "@main...feat/x") || !strings.HasSuffix(c.Right, "@main...feat/y") {
		t.Fatalf("links = %q | %q", c.Left, c.Right)
	}
	if c.Label != "feat/x vs feat/y (base: main)" {
		t.Fatalf("label = %q", c.Label)
	}
	sym, ok := SymmetricOf(c)
	if !ok || sym != (Symmetric{A: "feat/x", B: "feat/y", Base: "main"}) {
		t.Fatalf("SymmetricOf = %+v, %v", sym, ok)
	}
	dup, err := svc.SymmetricPreviewAdd(ctx, "feat/x", "feat/y", "main", "other")
	if !errors.Is(err, ErrSavedCompareExists) || dup.ID != c.ID {
		t.Fatalf("duplicate = %+v, %v; want the existing row + ErrSavedCompareExists", dup, err)
	}
}

func TestSymmetricPreviewAddRefusals(t *testing.T) {
	t.Parallel()
	_, svc := symRepo(t)
	ctx := context.Background()
	for name, args := range map[string][3]string{
		"a == b":      {"feat/x", "feat/x", "main"},
		"base == a":   {"feat/x", "feat/y", "feat/x"},
		"base == b":   {"feat/x", "feat/y", "feat/y"},
		"empty base":  {"feat/x", "feat/y", ""},
		"empty a":     {"", "feat/y", "main"},
		"unknown":     {"feat/x", "nope", "main"},
		"link-unsafe": {"feat/x", "feat/y", "ma?in"},
	} {
		if _, err := svc.SymmetricPreviewAdd(ctx, args[0], args[1], args[2], ""); err == nil {
			t.Errorf("%s: want a refusal", name)
		}
	}
	if cs, _ := svc.SavedCompareList(ctx); len(cs) != 0 {
		t.Fatalf("a refusal stored %+v", cs)
	}
}

// Live: the entry holds NAMES, so a moved tip shows up on the next open.
func TestSymmetricPreviewIsLive(t *testing.T) {
	t.Parallel()
	dir, svc := symRepo(t)
	ctx := context.Background()
	c, err := svc.SymmetricPreviewAdd(ctx, "feat/x", "feat/y", "main", "")
	if err != nil {
		t.Fatal(err)
	}
	gittest.Run(t, dir, "checkout", "-q", "feat/y")
	if err := os.WriteFile(filepath.Join(dir, "z.txt"), []byte("z\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gittest.Run(t, dir, "add", "z.txt")
	gittest.Run(t, dir, "commit", "-q", "-m", "add z")
	gittest.Run(t, dir, "checkout", "-q", "main")
	cmp, err := svc.CompareLinks(ctx, c.Left, c.Right, ResolveOpts{Cwd: svc})
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, f := range cmp.Files {
		paths = append(paths, f.Path)
	}
	if !strings.Contains(strings.Join(paths, " "), "z.txt") {
		t.Fatalf("the reopened comparison misses feat/y's new commit: %v", paths)
	}
}

func TestSymmetricOfShapes(t *testing.T) {
	t.Parallel()
	const r = "gg://gigagit"
	for name, tc := range map[string]struct {
		left, right string
		ok          bool
	}{
		"symmetric":      {r + "@main...a", r + "@main...b", true},
		"different base": {r + "@main...a", r + "@dev...b", false},
		"same source":    {r + "@main...a", r + "@main...a", false},
		"path-bearing":   {r + "/x.txt@main...a", r + "@main...b", false},
		"commit pair":    {r + "@main...a", r + "@aaaaaaa..bbbbbbb", false},
		"other repo":     {r + "@main...a", "gg://other@main...b", false},
		"a set":          {r + "@main...a", "", false},
	} {
		_, ok := SymmetricOf(SavedCompare{Left: tc.left, Right: tc.right})
		if ok != tc.ok {
			t.Errorf("%s: ok = %v, want %v", name, ok, tc.ok)
		}
	}
}
