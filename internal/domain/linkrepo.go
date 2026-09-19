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
// it: `gg link`'s buildLink composes links a user copies, and the previews
// migration composes links nobody typed. Two derivations would be two arms
// that must agree and eventually would not — a link built by one and parsed
// against the other names a different repository.
//
// The local form carries the CHECKOUT path, which is no more expressible than
// a file path is: a checkout under /home/user@corp or /mnt/backup#1 would
// yield a link ParseLink refuses (the first '@' is the target separator, the
// first '#' the hunk one). That is an error rather than a mangled link.
func (s *Service) LinkRepo(ctx context.Context) (model.LinkRepo, error) {
	name, err := s.RepoName(ctx)
	if err != nil {
		return model.LinkRepo{}, err
	}
	if name != "" {
		return model.LinkRepo{Name: name}, nil
	}
	top, err := s.TopLevel(ctx)
	if err != nil {
		return model.LinkRepo{}, err
	}
	abs := filepath.ToSlash(filepath.Clean(top))
	if !model.LinkAbsOK(abs) {
		return model.LinkRepo{}, fmt.Errorf("%w: this repository has no remote and its checkout path %q contains @, # or ? — no gg link", model.ErrLink, abs)
	}
	return model.LinkRepo{Abs: abs}, nil
}
