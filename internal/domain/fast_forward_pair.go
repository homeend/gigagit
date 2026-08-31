package domain

import (
	"context"
)

// FFPair reports whether one of two branches can be fast-forwarded to the
// other: Behind's tip is a strict ancestor of Ahead's, so advancing Behind to
// Ahead needs no merge commit and rewrites nothing. OK is false when the tips
// are equal, diverged, or share no history.
type FFPair struct {
	Behind, Ahead string
	OK            bool
}

// FastForwardPair resolves which of branches a and b, if either, is strictly
// behind the other. Frontends use it to decide whether a pair menu offers a
// fast-forward row (and in which direction). At most one direction can apply;
// unrelated histories simply report OK=false (IsAncestor is a yes/no probe,
// not an error).
func (s *Service) FastForwardPair(ctx context.Context, a, b string) (FFPair, error) {
	return query(ctx, s, "ff-pair:"+a+":"+b, func(ctx context.Context) (FFPair, error) {
		ta, err := s.repo.RevParse(ctx, "refs/heads/"+a)
		if err != nil {
			return FFPair{}, err
		}
		tb, err := s.repo.RevParse(ctx, "refs/heads/"+b)
		if err != nil {
			return FFPair{}, err
		}
		if ta == tb {
			return FFPair{}, nil
		}
		if behind, err := s.repo.IsAncestor(ctx, ta, tb); err != nil {
			return FFPair{}, err
		} else if behind {
			return FFPair{Behind: a, Ahead: b, OK: true}, nil
		}
		if behind, err := s.repo.IsAncestor(ctx, tb, ta); err != nil {
			return FFPair{}, err
		} else if behind {
			return FFPair{Behind: b, Ahead: a, OK: true}, nil
		}
		return FFPair{}, nil
	})
}
