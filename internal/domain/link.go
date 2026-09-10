package domain

import (
	"context"

	"github.com/homeend/gigagit/internal/model"
)

// RemoteURL returns the fetch URL configured for remote name, under a Read
// reservation.
func (s *Service) RemoteURL(ctx context.Context, name string) (string, error) {
	return query(ctx, s, "remote-url:"+name, func(ctx context.Context) (string, error) {
		return s.repo.RemoteURL(ctx, name)
	})
}

// RepoName is this repository's LINK identity: the repository name of its
// default remote — "origin" when present, else the first remote in `git
// remote` order — with any trailing ".git" stripped, so
// git@github.com:homeend/gigagit.git and https://github.com/homeend/gigagit
// both give "gigagit".
//
// "" means "no usable remote": the caller then builds the local
// (absolute-checkout-path) link form. A remote whose URL cannot be read is
// treated the same way — a link is a convenience and must never turn a
// misconfigured remote into a failure. A genuine failure to LIST remotes
// (not a repository, git missing) is still returned as an error.
func (s *Service) RepoName(ctx context.Context) (string, error) {
	names, err := s.RemoteNames(ctx)
	if err != nil {
		return "", err
	}
	remote := pickDefaultRemote(names)
	if remote == "" {
		return "", nil
	}
	url, err := s.RemoteURL(ctx, remote)
	if err != nil {
		return "", nil
	}
	return model.RepoNameFromURL(url), nil
}
