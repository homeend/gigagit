package domain

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/sessionreg"
	"github.com/homeend/gigagit/internal/wtclaim"
)

var (
	ErrNotAgent  = errors.New("only an agent running inside gg can claim")
	ErrNotHolder = errors.New("the claim belongs to another session")
)

type SessionNotLiveError struct{ ID string }

func (e *SessionNotLiveError) Error() string { return "session " + e.ID + " is not running in any gg" }

type NotFreeError struct {
	Path      string
	BlockedBy []string
}

func (e *NotFreeError) Error() string {
	return e.Path + " is not free: " + strings.Join(e.BlockedBy, ", ")
}

type WorktreeMark struct {
	Reserved bool
	Claim    *ClaimInfo
}

// emptyClaimGrace: a claim file with no session is a claimer between O_EXCL
// and its write — alive this long, then a crashed claimer's.
const emptyClaimGrace = 10 * time.Second

// youngClaimGrace: a claim this fresh whose session its LIVE process does not
// list yet is a handover racing the registry write (sessionpublish writes
// asynchronously) — alive, not dead.
const youngClaimGrace = 2 * sessionreg.LiveWindow

// readClaim reads gitDir's claim. An unparsable file (hand-edited, torn by
// a crash) reads as an EMPTY claim, so it gets the same grace-then-dead
// treatment instead of blocking the worktree forever.
func readClaim(gitDir string) (wtclaim.Claim, bool) {
	c, ok, err := wtclaim.Read(gitDir)
	if err != nil {
		if _, exists := wtclaim.Age(gitDir); exists {
			return wtclaim.Claim{}, true
		}
		return wtclaim.Claim{}, false
	}
	return c, ok
}

// sameClaim: field-wise, with time.Equal — a numeric-offset timestamp parses
// to a fresh *Location each read, so == would never match it.
func sameClaim(a, b wtclaim.Claim) bool {
	return a.Session == b.Session && a.Agent == b.Agent && a.Note == b.Note && a.Host == b.Host && a.Parent == b.Parent && a.Since.Equal(b.Since)
}

// localHost names this machine + OS for claim stamps.
func localHost() string {
	h, _ := os.Hostname()
	return h + "/" + runtime.GOOS
}

func claimDead(c wtclaim.Claim, gitDir string, lv liveView) bool {
	if c.Host != "" && c.Host != localHost() {
		return false // another host's pids and registries: only a user releases it
	}
	if c.Session == "" {
		age, ok := wtclaim.Age(gitDir)
		return ok && age > emptyClaimGrace
	}
	if _, listed := lv.running[c.Session]; !listed && lv.procs[sessionreg.ProcOf(c.Session)] &&
		!c.Since.IsZero() && time.Since(c.Since) < youngClaimGrace {
		return false
	}
	return sessionDead(c.Session, lv)
}

// settleDeadClaim runs under the claim lock on a claim already judged dead:
// it reverts to a live Parent (the worker died, its overseer lives) or
// removes it. ok reports a claim that is still there (reverted).
func settleDeadClaim(gitDir string, c wtclaim.Claim, lv liveView) (ClaimInfo, bool) {
	if c.Parent != "" && !sessionDead(c.Parent, lv) {
		back := wtclaim.Claim{Session: c.Parent, Agent: lv.agents[c.Parent],
			Since: time.Now().UTC().Truncate(time.Second), Note: c.Note, Host: c.Host}
		if wtclaim.Replace(gitDir, back) == nil {
			return claimInfoOf(back), true
		}
		return claimInfoOf(c), true // the rewrite failed: keep the old claim, never drop it
	}
	_ = wtclaim.Remove(gitDir)
	return ClaimInfo{}, false
}

func claimInfoOf(c wtclaim.Claim) ClaimInfo {
	return ClaimInfo{Session: c.Session, Agent: c.Agent, Since: c.Since, Note: c.Note, Parent: c.Parent}
}

// liveClaim reports gitDir's claim when alive. A dead one is removed under
// the claim lock after a re-read, so a sweeper never deletes a claim someone
// created after its first read.
func liveClaim(gitDir string, lv liveView) (ClaimInfo, bool) {
	if gitDir == "" {
		return ClaimInfo{}, false
	}
	c, ok := readClaim(gitDir)
	if !ok {
		return ClaimInfo{}, false
	}
	if !claimDead(c, gitDir, lv) {
		return claimInfoOf(c), true
	}
	var info ClaimInfo
	var kept bool
	_ = wtclaim.WithLock(gitDir, func() error {
		again, ok := readClaim(gitDir)
		switch {
		case !ok:
		case !sameClaim(again, c):
			if !claimDead(again, gitDir, lv) {
				info, kept = claimInfoOf(again), true // someone re-claimed meanwhile
			}
		default:
			info, kept = settleDeadClaim(gitDir, again, lv)
		}
		return nil
	})
	return info, kept
}

func (s *Service) worktreeAt(ctx context.Context, path string) (model.Worktree, error) {
	wts, err := s.Worktrees(ctx)
	if err != nil {
		return model.Worktree{}, err
	}
	for _, w := range wts {
		if SameCheckout(w.Path, path) {
			return w, nil
		}
	}
	return model.Worktree{}, ErrUnknownWorktree
}

func (s *Service) ClaimWorktree(ctx context.Context, path, sessionID, note string, pol InventoryPolicy) error {
	if sessionID == "" {
		return ErrNotAgent
	}
	lv := readLive(s.registryDir())
	if !lv.running[sessionID] {
		return &SessionNotLiveError{ID: sessionID}
	}
	infos, err := s.inventory(ctx, pol, path, sessionID) // this worktree only, as the caller sees it; sweeps a dead claim
	if err != nil {
		return err
	}
	if len(infos) == 0 {
		return ErrUnknownWorktree
	}
	target := infos[0]
	if !target.Free {
		return &NotFreeError{Path: target.Path, BlockedBy: target.BlockedBy}
	}
	gitDir := git.GitDirAt(target.Path)
	if gitDir == "" {
		return &NotFreeError{Path: target.Path, BlockedBy: []string{"missing"}}
	}
	return wtclaim.WithLock(gitDir, func() error {
		// Between the inventory read and the lock a dead claim may have
		// appeared dead-then-live or been swept; decide again, locked.
		if c, ok := readClaim(gitDir); ok {
			if !claimDead(c, gitDir, lv) {
				if c.Session == sessionID {
					return nil // a retry of a claim we already hold
				}
				return &NotFreeError{Path: target.Path, BlockedBy: []string{"claimed"}}
			}
			if info, kept := settleDeadClaim(gitDir, c, lv); kept {
				if info.Session == sessionID {
					return nil // reverted to us
				}
				return &NotFreeError{Path: target.Path, BlockedBy: []string{"claimed"}}
			}
		}
		err := wtclaim.Create(gitDir, wtclaim.Claim{Session: sessionID, Agent: lv.agents[sessionID],
			Since: time.Now().UTC().Truncate(time.Second), Note: note, Host: localHost()})
		if errors.Is(err, wtclaim.ErrClaimed) {
			return &NotFreeError{Path: target.Path, BlockedBy: []string{"claimed"}}
		}
		return err
	})
}

func (s *Service) ReleaseWorktree(ctx context.Context, path, sessionID string, force bool) (bool, error) {
	w, err := s.worktreeAt(ctx, path)
	if err != nil {
		return false, err
	}
	gitDir := git.GitDirAt(w.Path)
	if gitDir == "" {
		return false, nil
	}
	released := false
	err = wtclaim.WithLock(gitDir, func() error {
		c, ok := liveClaimLocked(gitDir, readLive(s.registryDir()))
		if !ok {
			return nil
		}
		if !force && c.Session != sessionID {
			return ErrNotHolder
		}
		released = true
		return wtclaim.Remove(gitDir)
	})
	return released, err
}

// liveClaimLocked is liveClaim for a caller already holding the claim lock
// (filelock is not re-entrant).
func liveClaimLocked(gitDir string, lv liveView) (ClaimInfo, bool) {
	c, ok := readClaim(gitDir)
	if !ok {
		return ClaimInfo{}, false
	}
	if claimDead(c, gitDir, lv) {
		return settleDeadClaim(gitDir, c, lv)
	}
	return claimInfoOf(c), true
}

// HandOverWorktree moves path's claim from the session holding it to `to`,
// recording `from` as the claim's Parent (an overseer handing a worktree to
// the worker it spawned). ErrNotHolder when from does not hold a live claim.
func (s *Service) HandOverWorktree(ctx context.Context, path, from, to string) error {
	w, err := s.worktreeAt(ctx, path)
	if err != nil {
		return err
	}
	gitDir := git.GitDirAt(w.Path)
	if gitDir == "" {
		return ErrUnknownWorktree
	}
	lv := readLive(s.registryDir())
	return wtclaim.WithLock(gitDir, func() error {
		c, ok := liveClaimLocked(gitDir, lv)
		if !ok || c.Session != from {
			return ErrNotHolder
		}
		return wtclaim.Replace(gitDir, wtclaim.Claim{Session: to, Agent: lv.agents[to], Parent: from,
			Since: time.Now().UTC().Truncate(time.Second), Note: c.Note, Host: localHost()})
	})
}

// claimHeldBy reports whether session holds path's live claim.
func (s *Service) claimHeldBy(ctx context.Context, path, session string) (bool, error) {
	w, err := s.worktreeAt(ctx, path)
	if err != nil {
		return false, err
	}
	c, ok := liveClaim(git.GitDirAt(w.Path), readLive(s.registryDir()))
	return ok && c.Session == session, nil
}

// WorktreeMarks is the TUI's cheap view: reserve + live claim per worktree,
// no git status, over the list the caller just loaded.
func (s *Service) WorktreeMarks(wts []model.Worktree, reserved []string) map[string]WorktreeMark {
	if len(wts) == 0 {
		return nil
	}
	res := resolveReserved(wts[0].Path, reserved)
	lv := readLive(s.registryDir())
	out := map[string]WorktreeMark{}
	for _, w := range wts {
		var m WorktreeMark
		for _, r := range res {
			if SameCheckout(r, w.Path) {
				m.Reserved = true
			}
		}
		if c, ok := liveClaim(git.GitDirAt(w.Path), lv); ok {
			m.Claim = &c
		}
		if m.Reserved || m.Claim != nil {
			out[w.Path] = m
		}
	}
	return out
}

// SetWorktreeReserved adds (on) or removes path in [agents] reserved of the
// repo config at cfgPath. current is the configured (repo-only) list; new
// entries are stored relative to the main worktree (slash-separated) so a
// committed .gg.toml works on every machine.
func (s *Service) SetWorktreeReserved(ctx context.Context, cfgPath string, current []string, path string, on bool) error {
	w, err := s.worktreeAt(ctx, path)
	if err != nil {
		return err
	}
	wts, _ := s.Worktrees(ctx)
	mainPath := wts[0].Path
	abs := resolveReserved(mainPath, current)
	var next []string
	for i, r := range abs {
		if !SameCheckout(r, w.Path) {
			next = append(next, current[i])
		}
	}
	if on {
		entry := w.Path
		if rel, err := filepath.Rel(mainPath, w.Path); err == nil {
			entry = filepath.ToSlash(rel)
		}
		next = append(next, entry)
	}
	return config.SetAgentsReserved(cfgPath, next)
}
