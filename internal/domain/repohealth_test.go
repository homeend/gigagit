package domain

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/homeend/gigagit/internal/git"
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
