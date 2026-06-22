package domain

import (
	"context"

	"github.com/gigagit/gg/internal/git"
	"github.com/gigagit/gg/internal/model"
)

// Identity reads the current git user identity, keeping the global and
// repo-local values distinct (each with a set flag) plus the effective merged
// value, under a Read reservation.
func (s *Service) Identity(ctx context.Context) (model.Identity, error) {
	return query(ctx, s, "identity", func(ctx context.Context) (model.Identity, error) {
		var id model.Identity
		id.GlobalName, _, _ = s.repo.ConfigGet(ctx, git.ConfigGlobal, "user.name")
		id.GlobalEmail, id.GlobalSet, _ = s.repo.ConfigGet(ctx, git.ConfigGlobal, "user.email")
		if id.GlobalName != "" {
			id.GlobalSet = true
		}
		id.LocalName, _, _ = s.repo.ConfigGet(ctx, git.ConfigLocal, "user.name")
		id.LocalEmail, id.LocalSet, _ = s.repo.ConfigGet(ctx, git.ConfigLocal, "user.email")
		if id.LocalName != "" {
			id.LocalSet = true
		}
		id.EffectiveName, _, _ = s.repo.ConfigGet(ctx, git.ConfigEffective, "user.name")
		id.EffectiveEmail, _, _ = s.repo.ConfigGet(ctx, git.ConfigEffective, "user.email")
		return id, nil
	})
}
