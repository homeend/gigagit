package domain

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/wtguard"
)

// One provider per data source for "may this worktree be taken?"
// (wtguard). Each knows only its own source; wtguard_set.go composes them.

type missingGuard struct{}

func (missingGuard) Reason() string  { return "missing" }
func (missingGuard) FactKey() string { return "" }
func (missingGuard) Cheap() bool     { return true }
func (missingGuard) Check(_ context.Context, t wtguard.Target) (wtguard.Result, error) {
	if git.GitDirAt(t.Dir) == "" {
		return wtguard.Result{Blocker: &wtguard.Blocker{Reason: "missing", Detail: "the worktree directory is gone", Hard: true}}, nil
	}
	return wtguard.Result{}, nil
}

type mainGuard struct{ allow bool }

func (mainGuard) Reason() string  { return "main" }
func (mainGuard) FactKey() string { return "" }
func (mainGuard) Cheap() bool     { return true }
func (g mainGuard) Check(_ context.Context, t wtguard.Target) (wtguard.Result, error) {
	if t.Main && !g.allow {
		return wtguard.Result{Blocker: &wtguard.Blocker{Reason: "main", Detail: "the main checkout"}}, nil
	}
	return wtguard.Result{}, nil
}

type detachedGuard struct{}

func (detachedGuard) Reason() string  { return "detached" }
func (detachedGuard) FactKey() string { return "" }
func (detachedGuard) Cheap() bool     { return true }
func (detachedGuard) Check(_ context.Context, t wtguard.Target) (wtguard.Result, error) {
	if t.Detached {
		return wtguard.Result{Blocker: &wtguard.Blocker{Reason: "detached", Detail: "no branch checked out"}}, nil
	}
	return wtguard.Result{}, nil
}

type pausedOpGuard struct{}

func (pausedOpGuard) Reason() string  { return "paused-op" }
func (pausedOpGuard) FactKey() string { return "paused_op" }
func (pausedOpGuard) Cheap() bool     { return true }
func (pausedOpGuard) Check(_ context.Context, t wtguard.Target) (wtguard.Result, error) {
	gd := git.GitDirAt(t.Dir)
	if gd == "" {
		return wtguard.Result{Fact: ""}, nil
	}
	op := git.PausedOpIn(gd)
	r := wtguard.Result{Fact: op}
	if op != "" {
		r.Blocker = &wtguard.Blocker{Reason: "paused-op", Detail: "a " + op + " is in progress", Hard: true}
	}
	return r, nil
}

type gitLockGuard struct{}

func (gitLockGuard) Reason() string  { return "git-lock" }
func (gitLockGuard) FactKey() string { return "git_lock" }
func (gitLockGuard) Cheap() bool     { return true }
func (gitLockGuard) Check(_ context.Context, t wtguard.Target) (wtguard.Result, error) {
	gd := git.GitDirAt(t.Dir)
	if gd == "" {
		return wtguard.Result{Fact: false}, nil
	}
	// This worktree's own git dir only: common-dir locks (packed-refs.lock
	// during a fetch) would block every worktree at once.
	locks := git.LockFiles(gd)
	if len(locks) == 0 {
		return wtguard.Result{Fact: false}, nil
	}
	return wtguard.Result{Fact: true, Blocker: &wtguard.Blocker{Reason: "git-lock", Detail: locks[0].Name, Hard: true}}, nil
}

type reservedGuard struct{ abs []string }

func (reservedGuard) Reason() string  { return "reserved" }
func (reservedGuard) FactKey() string { return "reserved" }
func (reservedGuard) Cheap() bool     { return true }
func (g reservedGuard) Check(_ context.Context, t wtguard.Target) (wtguard.Result, error) {
	for _, r := range g.abs {
		if SameCheckout(r, t.Dir) {
			return wtguard.Result{Fact: true, Blocker: &wtguard.Blocker{Reason: "reserved", Detail: "reserved against agents"}}, nil
		}
	}
	return wtguard.Result{Fact: false}, nil
}

type claimGuard struct{ lv liveView }

func (claimGuard) Reason() string  { return "claimed" }
func (claimGuard) FactKey() string { return "claim" }
func (claimGuard) Cheap() bool     { return true }
func (g claimGuard) Check(_ context.Context, t wtguard.Target) (wtguard.Result, error) {
	c, ok := liveClaim(git.GitDirAt(t.Dir), g.lv)
	if !ok {
		return wtguard.Result{Fact: (*ClaimInfo)(nil)}, nil
	}
	r := wtguard.Result{Fact: &c}
	if t.CallerSession == "" || c.Session != t.CallerSession {
		detail := c.Agent + " since " + c.Since.Local().Format("2006-01-02 15:04")
		if c.Note != "" {
			detail += " · " + c.Note
		}
		r.Blocker = &wtguard.Blocker{Reason: "claimed", Detail: detail}
	}
	return r, nil
}

type tuiGuard struct{ lv liveView }

func (tuiGuard) Reason() string  { return "tui" }
func (tuiGuard) FactKey() string { return "tui" }
func (tuiGuard) Cheap() bool     { return true }
func (g tuiGuard) Check(_ context.Context, t wtguard.Target) (wtguard.Result, error) {
	for _, w := range g.lv.tuis {
		if SameCheckout(w, t.Dir) {
			return wtguard.Result{Fact: true, Blocker: &wtguard.Blocker{Reason: "tui", Detail: "a gg TUI is open here"}}, nil
		}
	}
	for _, w := range g.lv.viewed {
		if SameCheckout(w, t.Dir) {
			return wtguard.Result{Fact: true, Blocker: &wtguard.Blocker{Reason: "tui", Detail: "a gg TUI shows this worktree (it runs in another)"}}, nil
		}
	}
	return wtguard.Result{Fact: false}, nil
}

type sessionGuard struct{ lv liveView }

func (sessionGuard) Reason() string  { return "session" }
func (sessionGuard) FactKey() string { return "sessions" }
func (sessionGuard) Cheap() bool     { return true }
func (g sessionGuard) Check(_ context.Context, t wtguard.Target) (wtguard.Result, error) {
	refs := []SessionRef{}
	var running []string
	for d, rs := range g.lv.byDir {
		if !SameCheckout(d, t.Dir) {
			continue
		}
		refs = append(refs, rs...)
		for _, r := range rs {
			if r.State == "running" && (t.CallerSession == "" || r.ID != t.CallerSession) {
				running = append(running, r.Agent) // the caller's own session never blocks it
			}
		}
	}
	res := wtguard.Result{Fact: refs}
	if len(running) > 0 {
		res.Blocker = &wtguard.Blocker{Reason: "session", Detail: "agent session running: " + strings.Join(running, ", ")}
	}
	return res, nil
}

type dirtyGuard struct {
	svc      *Service
	stale    time.Duration
	staleErr string // stale_after did not parse: no dirty worktree is stale
	now      func() time.Time
}

func (dirtyGuard) Reason() string  { return "dirty-recent" }
func (dirtyGuard) FactKey() string { return "dirty" }
func (dirtyGuard) Cheap() bool     { return false }
func (g dirtyGuard) Check(ctx context.Context, t wtguard.Target) (wtguard.Result, error) {
	st, err := g.svc.repo.InDir(t.Dir).Status(ctx)
	if err != nil {
		// Never read a failed status as clean.
		return wtguard.Result{Fact: (*DirtyInfo)(nil), Blocker: &wtguard.Blocker{Reason: "status-failed", Detail: err.Error(), Hard: true}}, nil
	}
	c := st.Counts()
	d := &DirtyInfo{Staged: c.Staged, Unstaged: c.Unstaged, Untracked: c.Untracked}
	for _, f := range st.Files {
		fi, err := os.Stat(filepath.Join(t.Dir, f.Path))
		if err != nil {
			continue // deleted in the tree: nothing to date
		}
		if mt := fi.ModTime(); d.LastChange == nil || mt.After(*d.LastChange) {
			d.LastChange = &mt
		}
	}
	r := wtguard.Result{Fact: d}
	if len(st.Files) > 0 && g.staleErr != "" {
		r.Blocker = &wtguard.Blocker{Reason: "dirty-recent", Detail: "uncommitted changes; " + g.staleErr}
	} else if len(st.Files) > 0 && (d.LastChange == nil || g.now().Sub(*d.LastChange) < g.stale) {
		r.Blocker = &wtguard.Blocker{Reason: "dirty-recent", Detail: "uncommitted changes touched recently"}
	}
	return r, nil
}
