package domain

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/homeend/gigagit/internal/branchfilter"
	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/wtguard"
)

// ErrUnknownWorktree: the path names no worktree of this repository.
var ErrUnknownWorktree = errors.New("no such worktree")

// InventoryPolicy is the [agents] config in effect for one inventory.
type InventoryPolicy struct {
	StaleAfter time.Duration
	StaleErr   string // a stale_after that did not parse: every dirty worktree counts as recent
	Reserved   []string // as configured (relative to main or absolute)
	AllowMain  bool
	Now        func() time.Time // nil = time.Now
}

// PolicyFromConfig parses [agents] into an InventoryPolicy. A stale_after
// typo never fails the inventory or a recycle: it only keeps dirty worktrees
// from counting as stale (StaleErr says why).
func PolicyFromConfig(c config.AgentsConfig) InventoryPolicy {
	p := InventoryPolicy{Reserved: c.Reserved, AllowMain: c.AllowMain}
	age := c.StaleAfter
	if age == "" {
		age = "14d"
	}
	d, err := branchfilter.ParseAge(age)
	if err != nil {
		p.StaleErr = fmt.Sprintf("[agents] stale_after %q: %v", age, err)
		return p
	}
	p.StaleAfter = d
	return p
}

// DirtyInfo is the dirty guard's fact.
type DirtyInfo struct {
	Staged     int        `json:"staged"`
	Unstaged   int        `json:"unstaged"`
	Untracked  int        `json:"untracked"`
	LastChange *time.Time `json:"last_change"`
}

// ClaimInfo is the claim guard's fact.
type ClaimInfo struct {
	Session string    `json:"session"`
	Agent   string    `json:"agent"`
	Since   time.Time `json:"since"`
	Note    string    `json:"note"`
	Parent  string    `json:"parent,omitempty"` // the agent that handed it over
}

// WorktreeInfo is one worktree's inventory row: identity, every guard's
// fact under its own key, and the verdict.
type WorktreeInfo struct {
	Path, Branch, Head string
	Main, Detached     bool
	Facts              map[string]any
	Blockers           []wtguard.Blocker
	BlockedBy          []string
	Free               bool
	Recycle            string // "none" | "shelve" | "" (not free)
}

// Dirty is the dirty guard's fact (nil when it did not run).
func (w WorktreeInfo) Dirty() *DirtyInfo { d, _ := w.Facts["dirty"].(*DirtyInfo); return d }

// Claim is the live claim (nil when none).
func (w WorktreeInfo) Claim() *ClaimInfo { c, _ := w.Facts["claim"].(*ClaimInfo); return c }

// UseSessionRegistryDir points this Service's registry reads at dir (tests).
func (s *Service) UseSessionRegistryDir(dir string) { s.mu.Lock(); s.sessRegDir = dir; s.mu.Unlock() }

func (s *Service) registryDir() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sessRegDir != "" {
		return s.sessRegDir
	}
	return SessionRegistryDir()
}

// resolveReserved turns configured paths into absolute cleaned ones.
func resolveReserved(mainPath string, list []string) []string {
	out := make([]string, 0, len(list))
	for _, p := range list {
		if !filepath.IsAbs(p) && mainPath != "" {
			p = filepath.Join(mainPath, p)
		}
		out = append(out, filepath.Clean(p))
	}
	return out
}

func (s *Service) guardSources(pol InventoryPolicy, wts []model.Worktree) GuardSources {
	mainPath := ""
	if len(wts) > 0 {
		mainPath = wts[0].Path
	}
	return GuardSources{Policy: pol, Reserved: resolveReserved(mainPath, pol.Reserved), Live: readLive(s.registryDir()), Svc: s}
}

func targetOf(w model.Worktree, isMain bool, caller string) wtguard.Target {
	return wtguard.Target{Dir: w.Path, Branch: w.Branch, Main: isMain, Detached: w.Detached || w.Branch == "", CallerSession: caller}
}

// WorktreeInventory runs the composed guards over every worktree; freeOnly
// keeps the free ones, clean first, then stale-dirty by oldest change.
func (s *Service) WorktreeInventory(ctx context.Context, pol InventoryPolicy, freeOnly bool) ([]WorktreeInfo, error) {
	out, err := s.inventory(ctx, pol, "", "")
	if err != nil || !freeOnly {
		return out, err
	}
	out = slices.DeleteFunc(out, func(w WorktreeInfo) bool { return !w.Free })
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i].Dirty(), out[j].Dirty()
		ac, bc := a == nil || a.LastChange == nil, b == nil || b.LastChange == nil
		if ac != bc {
			return ac // clean first
		}
		if ac {
			return false
		}
		return a.LastChange.Before(*b.LastChange) // oldest change first
	})
	return out, nil
}

// inventory runs the composed guard set per worktree (only != "": that one
// alone — the claim path, no status fan-out), in parallel; the Runner's
// LimitRunner caps the concurrent git processes.
func (s *Service) inventory(ctx context.Context, pol InventoryPolicy, only, caller string) ([]WorktreeInfo, error) {
	wts, err := s.Worktrees(ctx)
	if err != nil {
		return nil, err
	}
	guards := WorktreeGuardSet(s.guardSources(pol, wts))
	var out []WorktreeInfo
	var targets []wtguard.Target
	for i, w := range wts {
		if w.Bare || w.Path == "" || (only != "" && !SameCheckout(w.Path, only)) {
			continue
		}
		t := targetOf(w, i == 0, caller)
		out = append(out, WorktreeInfo{Path: w.Path, Branch: w.Branch, Head: w.Head, Main: t.Main, Detached: t.Detached})
		targets = append(targets, t)
	}
	var wg sync.WaitGroup
	for i := range out {
		wg.Add(1)
		go func(w *WorktreeInfo, t wtguard.Target) {
			defer wg.Done()
			r := wtguard.Run(ctx, guards, t)
			w.Facts, w.Blockers, w.BlockedBy = r.Facts, r.Blockers, r.Reasons()
			w.Free = len(r.Blockers) == 0
			switch d := w.Dirty(); {
			case !w.Free:
				w.Recycle = ""
			case d != nil && d.Staged+d.Unstaged+d.Untracked > 0:
				w.Recycle = "shelve"
			default:
				w.Recycle = "none"
			}
		}(&out[i], targets[i])
	}
	wg.Wait()
	return out, nil
}

// GuardReport runs the composed guards on one worktree as the caller sees
// it (the claim guard exempts the caller's own claim). It backs
// OpDeps.Guards, so it runs INSIDE an op that already holds this repo's
// reservation: it never goes through a gated query (s.Worktrees would wait
// on the op itself — writer-preferring gate) and reads through s.repo
// directly, as shelveStagedIn does.
func (s *Service) GuardReport(ctx context.Context, t wtguard.Target) (wtguard.Report, error) {
	wts, err := s.repo.Worktrees(ctx)
	if err != nil {
		return wtguard.Report{}, err
	}
	ac, _, err := AgentsConfigFrom(wts)
	if err != nil {
		return wtguard.Report{}, err
	}
	pol := PolicyFromConfig(ac)
	for i, w := range wts {
		if SameCheckout(w.Path, t.Dir) {
			return wtguard.Run(ctx, WorktreeGuardSet(s.guardSources(pol, wts)), targetOf(w, i == 0, t.CallerSession)), nil
		}
	}
	return wtguard.Report{}, ErrUnknownWorktree
}

// AgentsConfig is [agents] plus the active repo config path it came from,
// anchored on the MAIN worktree: a TUI or CLI running in a linked worktree
// must read and write the same file the rest of the repo uses (the TUI's
// own repoTOML follows the cwd worktree's committed .gg.toml).
func (s *Service) AgentsConfig(ctx context.Context) (config.AgentsConfig, string, error) {
	wts, err := s.Worktrees(ctx)
	if err != nil {
		return config.AgentsConfig{}, "", err
	}
	return AgentsConfigFrom(wts)
}

// AgentsConfigFrom is AgentsConfig over an already-read worktree list (the
// ungated GuardReport path, and the TUI loader that just read the list).
func AgentsConfigFrom(wts []model.Worktree) (config.AgentsConfig, string, error) {
	if len(wts) == 0 || wts[0].Path == "" {
		return config.AgentsConfig{}, "", ErrUnknownWorktree
	}
	mainPath := wts[0].Path
	path := config.ActiveRepoConfigPath(filepath.Join(mainPath, ".gg.toml"), config.PrivateRepoPath(mainPath))
	cfg, err := config.Load(config.DefaultGlobalPath(), path)
	if err != nil {
		return config.AgentsConfig{}, "", err
	}
	return cfg.Agents, path, nil
}
