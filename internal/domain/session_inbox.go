package domain

import (
	"context"
	"errors"

	"github.com/homeend/gigagit/internal/config"
)

// SessionInboxOf is the steer inbox of worktree, from that worktree's own git
// common dir: what a `gg overview show <link>` for it will look in.
func SessionInboxOf(ctx context.Context, worktree string) (string, error) {
	cd, err := Open(worktree).GitCommonDir(ctx)
	if err != nil {
		return "", err
	}
	dir := config.SessionSteerDir(cd, worktree)
	if dir == "" {
		return "", errors.New("no state directory for " + worktree)
	}
	return dir, nil
}
