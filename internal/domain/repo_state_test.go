package domain

import (
	"context"
	"slices"
	"sync"
	"testing"

	"github.com/homeend/gigagit/internal/forge"
	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/gitexec"
	"github.com/homeend/gigagit/internal/observ"
)

// The repository-scoped state — the forge verdict and its caches, the tag
// cache, the preflight verdicts — is ONE per repository, however many of
// its worktrees have a Service (NewSharing / OpenTUISharing): a slot made
// for another worktree answers from what home already probed. The TUI's
// once-per-session forge detection (CLAUDE.md) depends on it.

func TestSharingServicesShareTheForgeVerdict(t *testing.T) {
	t.Parallel()
	ff := &fakeForge{}
	home := newForgeSvc(t, ff)
	slot := NewSharing(home.Repo(), home)

	if st := home.ForgeStatus(context.Background()); !st.Available() {
		t.Fatalf("home status = %+v, want available", st)
	}
	if st := slot.ForgeStatus(context.Background()); !st.Available() {
		t.Fatalf("slot status = %+v, want available", st)
	}
	if n := ff.detects.Load(); n != 1 {
		t.Errorf("Detect ran %d times across home and slot, want 1 (one probe per repository)", n)
	}
	if home.repoState != slot.repoState {
		t.Error("the slot has its own repoState; want home's")
	}
}

func TestSharingServicesShareTheTagCache(t *testing.T) {
	t.Parallel()
	f := gitexec.NewFakeRunner()
	f.SetResponse(tagsFPSpan, gitexec.Result{Stdout: "refs/tags/v1\x00aaa\n"})
	f.SetResponse(tagsFullSpan, gitexec.Result{Stdout: tagsV1Line})
	repo := &git.Repo{Runner: f}
	home := New(repo)
	slot := NewSharing(repo, home)
	ctx := context.Background()

	if _, err := home.Tags(ctx); err != nil {
		t.Fatalf("home Tags: %v", err)
	}
	tags, err := slot.Tags(ctx)
	if err != nil {
		t.Fatalf("slot Tags: %v", err)
	}
	if len(tags) != 1 || tags[0].Name != "v1" {
		t.Fatalf("slot tags = %+v", tags)
	}
	if n := countSpan(f, tagsFullSpan); n != 1 {
		t.Errorf("full tag reads = %d, want 1 (the slot is served from home's cache)", n)
	}
}

// Preflight stays per Service: one of its probes (the legacy review
// commands) reads the asking worktree's committed .gg.toml, which differs
// by branch, so a verdict resolved through one worktree is not the other's.
func TestSharingServicesKeepTheirOwnPreflight(t *testing.T) {
	t.Parallel()
	dir := cleanDir(t)
	cr := newCountingRunner(gitexec.NewExecRunner("git", dir, observ.NewRing(50)))
	repo := &git.Repo{Runner: cr}
	home := New(repo)
	slot := NewSharing(repo, home)
	ctx := context.Background()

	if _, err := home.Preflight(ctx); err != nil {
		t.Fatalf("home Preflight: %v", err)
	}
	cr.reset()
	if _, err := slot.Preflight(ctx); err != nil {
		t.Fatalf("slot Preflight: %v", err)
	}
	if n := cr.count("git version"); n != 1 {
		t.Errorf("the slot's Preflight ran git version %d times, want 1 (its own resolution)", n)
	}
}

// The shared forge VERDICT is the repository's, but the provider a slot
// calls is rooted at the slot's own worktree: the probing worktree may be
// removed later (gg started in a linked worktree, moved home, removed it),
// and a gh rooted there would fail every call for the rest of the session.
func TestSharingServicesCallTheForgeFromTheirOwnWorktree(t *testing.T) {
	t.Parallel()
	ff := &fakeForge{}
	var mu sync.Mutex
	var dirs []string
	factory := func(workdir string, _ observ.Recorder) []forge.Provider {
		mu.Lock()
		dirs = append(dirs, workdir)
		mu.Unlock()
		return []forge.Provider{ff}
	}
	homeDir, home := newRealRepo(t)
	home.workdir = homeDir
	home.forgeFactory = factory
	slotDir := t.TempDir()
	slot := NewSharing(home.Repo(), home)
	slot.workdir = slotDir
	if st := home.ForgeStatus(context.Background()); !st.Available() {
		t.Fatalf("home status = %+v", st)
	}
	if _, err := slot.PullRequests(context.Background()); err != nil {
		t.Fatalf("slot PullRequests: %v", err)
	}
	if n := ff.detects.Load(); n != 1 {
		t.Errorf("Detect ran %d times, want 1", n)
	}
	mu.Lock()
	defer mu.Unlock()
	if !slices.Contains(dirs, slotDir) {
		t.Fatalf("the slot called the forge through %v; want a provider rooted at its own %s", dirs, slotDir)
	}
}

// A slot's Service keeps its OWN per-worktree pieces: the differ (its own
// options) and the singleflight (status is per worktree).
func TestSharingServicesKeepTheirOwnFlight(t *testing.T) {
	t.Parallel()
	ff := &fakeForge{}
	home := newForgeSvc(t, ff)
	slot := NewSharing(home.Repo(), home)
	if &home.flight == &slot.flight {
		t.Error("the singleflight is shared; status coalescing must stay per worktree")
	}
	var _ forge.Provider = ff
}

// OpenTUISharingRooted trusts the root it is given (the worktree list's
// top level): no rev-parse — a slot is made on the Update thread.
func TestOpenTUISharingRootedSkipsTheRootProbe(t *testing.T) {
	t.Parallel()
	_, home := newRealRepo(t)
	dir := t.TempDir() // not a repository: resolveRoot would leave Root empty
	s := OpenTUISharingRooted(dir, home)
	if s.repo.Root != dir || s.Root() != dir {
		t.Fatalf("repo.Root = %q, Root() = %q; want %q in both without a probe", s.repo.Root, s.Root(), dir)
	}
	if s.repoState != home.repoState {
		t.Error("the rooted slot has its own repoState; want home's")
	}
}
