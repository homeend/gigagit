package forge

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/homeend/gigagit/internal/forge/forgetest"
	"github.com/homeend/gigagit/internal/gitexec"
)

func TestGHArgvIsReadOnly(t *testing.T) {
	t.Parallel()
	f := gitexec.NewFakeRunner()
	var seen [][]string
	rec := func(out string) func(context.Context, []string) (gitexec.Result, error) {
		return func(_ context.Context, argv []string) (gitexec.Result, error) {
			seen = append(seen, argv)
			return gitexec.Result{Stdout: out}, nil
		}
	}
	f.SetHandler("gh pr list (detect)", rec("[]"))
	f.SetHandler("gh pr list", rec(string(fixture(t, "pr-list.json"))))
	f.SetHandler("gh pr view", rec(string(fixture(t, "pr-view-7.json"))))
	f.SetHandler("gh api graphql (threads)", rec(string(fixture(t, "threads-7.json"))))
	f.SetHandler("gh repo view", rec(string(fixture(t, "repo-view.json"))))
	g := NewGHWithRunner(f)
	ctx := context.Background()
	if err := g.Detect(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := g.ListOpen(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := g.PR(ctx, 7); err != nil {
		t.Fatal(err)
	}
	if _, _, err := g.Comments(ctx, 7); err != nil {
		t.Fatal(err)
	}
	slug, fallback, err := g.BaseRepo(ctx)
	if err != nil || slug != "github.com/homeend/gigagit" || fallback != "git@github.com:homeend/gigagit.git" {
		t.Errorf("BaseRepo = %q %q %v", slug, fallback, err)
	}
	if len(seen) != 5 {
		t.Fatalf("calls = %d, want 5", len(seen))
	}
	for _, argv := range seen {
		for _, bad := range []string{"-X", "--method", "comment", "review", "edit", "merge", "close", "reopen", "create"} {
			if slices.Contains(argv, bad) {
				t.Errorf("argv %v contains mutating token %q", argv, bad)
			}
		}
	}
	if !slices.Contains(seen[1], "open") || !slices.Contains(seen[1], "100") {
		t.Errorf("list argv = %v", seen[1])
	}
	if !slices.Contains(seen[3], "number=7") || !slices.Contains(seen[3], "owner={owner}") {
		t.Errorf("threads argv = %v", seen[3])
	}
	if g.HeadRefspec(7) != "refs/pull/7/head" {
		t.Errorf("refspec = %q", g.HeadRefspec(7))
	}
}

func TestGHDetectFailureIsAnError(t *testing.T) {
	t.Parallel()
	f := gitexec.NewFakeRunner()
	f.SetError("gh pr list (detect)", errors.New("exit status 4: not logged in"))
	if err := NewGHWithRunner(f).Detect(context.Background()); err == nil {
		t.Fatal("Detect = nil, want an error")
	}
}

// Serial: t.Setenv. Runs the REAL subprocess path against the fake binary.
func TestGHAgainstFakeBinary(t *testing.T) {
	bin := forgetest.BuildFakeGH(t)
	dir := t.TempDir()
	forgetest.Seed(t, dir, map[string]string{
		"pr-list.json":   string(fixture(t, "pr-list.json")),
		"pr-view-7.json": string(fixture(t, "pr-view-7.json")),
		"threads-7.json": string(fixture(t, "threads-7.json")),
	})
	t.Setenv(forgetest.EnvBin, bin)
	t.Setenv(forgetest.EnvFixtures, dir)
	g := NewGH(t.TempDir(), nil)
	ctx := context.Background()
	if err := g.Detect(ctx); err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if prs, err := g.ListOpen(ctx); err != nil || len(prs) != 2 {
		t.Fatalf("ListOpen = %d, %v", len(prs), err)
	}
	if cs, _, err := g.Comments(ctx, 7); err != nil || len(cs) == 0 {
		t.Fatalf("Comments = %d, %v", len(cs), err)
	}
	if _, err := g.PR(ctx, 99); !errors.Is(err, ErrNotFound) {
		t.Errorf("PR(99) err = %v, want ErrNotFound", err)
	}
	t.Setenv(forgetest.EnvFixtures, t.TempDir()) // empty dir = a gh that cannot read the repo
	if err := g.Detect(ctx); err == nil {
		t.Error("Detect against an empty fixture dir = nil, want an error")
	}
}
