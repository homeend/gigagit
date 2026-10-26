package domain

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/homeend/gigagit/internal/model"
)

// LinkRepo is THIS repository's link identity: the remote-named form when the
// repo has a remote, else the absolute checkout path in slash form.
//
// It lives in domain, not in a frontend, because more than one producer needs
// it: `gg link`'s buildLink composes links a user copies, and the
// saved-comparison producers compose links nobody typed (those through
// StoredLinkRepo, which differs only in the checkout it names). Two
// derivations would be two arms that must agree and eventually would not — a
// link built by one and parsed against the other names a different repository.
//
// The local form carries the CHECKOUT path, which is no more expressible than
// a file path is: a checkout under /home/user@corp or /mnt/backup#1 would
// yield a link ParseLink refuses (the first '@' is the target separator, the
// first '#' the hunk one). That is an error rather than a mangled link.
func (s *Service) LinkRepo(ctx context.Context) (model.LinkRepo, error) {
	// A remote-query failure DEGRADES to the local form rather than failing,
	// the same policy RepoName already applies one line further in (a
	// RemoteURL error reads as "no remote"). Composing a link is now part of
	// saving a merge preview, and a transient `git remote` failure must not
	// be able to stop a user saving one.
	return s.linkRepoAt(ctx, s.TopLevel)
}

// StoredLinkRepo is LinkRepo for a link the per-REPOSITORY saved-comparison
// store keeps: a remoteless repository names its MAIN checkout rather than
// this worktree. The store is keyed by the git common dir, so a row outlives
// the worktree that saved it, and a link naming that worktree would stop
// locating the day it is removed. Only links whose targets are objects (refs,
// commits, pairs, previews) may use it: a working-tree link names ITS
// checkout's files and must keep LinkRepo.
func (s *Service) StoredLinkRepo(ctx context.Context) (model.LinkRepo, error) {
	return s.linkRepoAt(ctx, s.RepoMainPath)
}

// linkRepoAt is the one derivation behind both: the remote name, else the
// checkout path checkout() answers.
func (s *Service) linkRepoAt(ctx context.Context, checkout func(context.Context) (string, error)) (model.LinkRepo, error) {
	name, _ := s.RepoName(ctx)
	if name != "" {
		return model.LinkRepo{Name: name}, nil
	}
	top, err := checkout(ctx)
	if err != nil {
		return model.LinkRepo{}, err
	}
	abs := filepath.ToSlash(filepath.Clean(top))
	if !model.LinkAbsOK(abs) {
		return model.LinkRepo{}, fmt.Errorf("%w: this repository has no remote and its checkout path %q contains @, # or ? — no gg link", model.ErrLink, abs)
	}
	return model.LinkRepo{Abs: abs}, nil
}
