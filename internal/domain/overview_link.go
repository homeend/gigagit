package domain

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/homeend/gigagit/internal/model"
)

// ErrOverviewLinkMismatch refuses an overview link moved onto another commit.
var ErrOverviewLinkMismatch = errors.New("the link does not match the overview")

// OverviewLink is overview id's gg:// link: the commit it explains (a PR's
// head for a PR overview) plus ?overview=<id>.
func (s *Service) OverviewLink(ctx context.Context, id string) (string, error) {
	o, err := s.Overview(ctx, id)
	if err != nil {
		return "", err
	}
	return s.linkText(ctx, model.Link{Target: model.LinkTarget{State: model.StateCommitted, Commit: o.Commit},
		Hint: model.LinkHint{Kind: model.OverviewHintKind, ID: o.ID}})
}

// checkOverviewHint: the overview exists in res's checkout and explains the
// commit the link names.
func checkOverviewHint(ctx context.Context, svc *Service, res Resolved) error {
	o, err := svc.Overview(ctx, res.Hint.ID)
	if err != nil {
		return err
	}
	if res.Commit == "" || !strings.HasPrefix(o.Commit, res.Commit) && !strings.HasPrefix(res.Commit, o.Commit) {
		return fmt.Errorf("%w: overview %s explains %s", ErrOverviewLinkMismatch, o.ID, shortSHA(o.Commit))
	}
	return nil
}
