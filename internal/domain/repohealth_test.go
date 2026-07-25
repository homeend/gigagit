package domain

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/model"
)

func TestRepoHealthFreshRepo(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "gitconfig"))
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)
	dir, svc := newRealRepo(t)

	h, err := svc.RepoHealth(context.Background())
	if err != nil {
		t.Fatalf("RepoHealth: %v", err)
	}
	if h.GitCommonDir == "" || !filepath.IsAbs(h.GitCommonDir) {
		t.Fatalf("GitCommonDir = %q, want absolute path", h.GitCommonDir)
	}
	if h.HasCommitGraph {
		t.Fatal("fresh repo must not report a commit-graph")
	}
	if h.WriteCommitGraphSet {
		t.Fatal("fetch.writeCommitGraph is unset in a fresh repo")
	}
	_ = dir
}

func TestRepoHealthSeesCommitGraphAndConfig(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "gitconfig"))
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)
	dir, svc := newRealRepo(t)
	ctx := context.Background()

	// Write a commit-graph with real git and set the config key locally.
	cmd := exec.Command("git", "commit-graph", "write", "--reachable")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit-graph write: %v\n%s", err, out)
	}
	if err := svc.Repo().ConfigSet(ctx, git.ConfigLocal, "fetch.writeCommitGraph", "true"); err != nil {
		t.Fatalf("config set: %v", err)
	}

	h, err := svc.RepoHealth(ctx)
	if err != nil {
		t.Fatalf("RepoHealth: %v", err)
	}
	if !h.HasCommitGraph {
		t.Fatal("must detect the commit-graph file")
	}
	if !h.WriteCommitGraphSet || h.WriteCommitGraphValue != "true" {
		t.Fatalf("WriteCommitGraph = %q set=%v, want true/true", h.WriteCommitGraphValue, h.WriteCommitGraphSet)
	}
}

func TestRepoHealthCountsPackBytes(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "gitconfig"))
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)
	dir, svc := newRealRepo(t)

	// Repack everything into a pack file so objects/pack has real content.
	cmd := exec.Command("git", "repack", "-ad")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git repack: %v\n%s", err, out)
	}

	h, err := svc.RepoHealth(context.Background())
	if err != nil {
		t.Fatalf("RepoHealth: %v", err)
	}
	if h.PackBytes <= 0 {
		t.Fatalf("PackBytes = %d after repack, want > 0", h.PackBytes)
	}
}

func TestRepoHealthSeesGlobalScopeConfig(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "gitconfig"))
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)
	_, svc := newRealRepo(t)
	ctx := context.Background()

	// Set ONLY the global scope; local stays unset — the query's fallback
	// must still report the option as set (an inherited global "true"
	// suppresses the commit-graph notice just like a local one).
	if err := svc.Repo().ConfigSet(ctx, git.ConfigGlobal, "fetch.writeCommitGraph", "true"); err != nil {
		t.Fatalf("config set: %v", err)
	}

	h, err := svc.RepoHealth(ctx)
	if err != nil {
		t.Fatalf("RepoHealth: %v", err)
	}
	if !h.WriteCommitGraphSet || h.WriteCommitGraphValue != "true" {
		t.Fatalf("global-only config: set=%v value=%q, want true/true", h.WriteCommitGraphSet, h.WriteCommitGraphValue)
	}
}

func TestRepoHealthSeesCommitGraphChainDir(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "gitconfig"))
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)
	dir, svc := newRealRepo(t)

	// A split commit-graph lives in a commit-graphs/ chain dir instead of the
	// single commit-graph file (e.g. written by git maintenance).
	chain := filepath.Join(dir, ".git", "objects", "info", "commit-graphs")
	if err := os.MkdirAll(chain, 0o755); err != nil {
		t.Fatal(err)
	}

	h, err := svc.RepoHealth(context.Background())
	if err != nil {
		t.Fatalf("RepoHealth: %v", err)
	}
	if !h.HasCommitGraph {
		t.Fatal("a commit-graphs/ chain dir must count as having a commit-graph")
	}
}

func TestUnmappedFromConfig(t *testing.T) {
	cfg := [][2]string{
		{"branch.feat.remote", "origin"},
		{"branch.feat.merge", "refs/heads/feat"},
		{"branch.main.remote", "origin"},
		{"branch.main.merge", "refs/heads/main"},
		{"branch.orphan.remote", "gone-remote"}, // remote without a fetch refspec: not listed
		{"branch.orphan.merge", "refs/heads/orphan"},
		{"remote.origin.fetch", "+refs/heads/main:refs/remotes/origin/main"},
	}
	branches := []model.Branch{
		{Name: "feat", Upstream: ""},            // configured but unresolvable → listed
		{Name: "main", Upstream: "origin/main"}, // resolvable → not listed
		{Name: "orphan", Upstream: ""},          // remote has no refspec → not listed
		{Name: "local-only", Upstream: ""},      // no branch config at all → not listed
	}
	got := unmappedFromConfig(cfg, branches)
	if len(got) != 1 || got[0] != "feat" {
		t.Fatalf("unmapped = %v, want [feat]", got)
	}
}
