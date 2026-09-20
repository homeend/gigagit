package domain

import (
	"context"
	"fmt"
	"strings"

	"github.com/homeend/gigagit/internal/model"
)

// BaseSuggestion answers the base picker (spec §5.1) for one link: whether a
// base could bound it at all, and which base to offer.
type BaseSuggestion struct {
	// Kind is the link's bound kind AS LOCATED. model.Link.BoundKind cannot
	// tell a local-form file link from a whole tree; this can, so a frontend
	// shows a base row on this field's say-so, never on the pure one's alone.
	Kind model.LinkBoundKind
	// Base is the suggestion — a ref name for a ref, the first parent's full
	// sha for a commit — or "" when there is nothing to suggest (the row is
	// still offered, empty).
	Base string
	// Why names where Base came from: "upstream", "trunk", "parent", or "".
	Why string
	// Self is a commit link's OWN full sha. The link may hold an abbreviation
	// a user typed; model.Link.WithBase needs the full one for `@<p>..<sha>`.
	Self string
}

// SuggestBase classifies a link and suggests its base. It locates the link
// first (o.Cwd defaults to s), for the reason EvalLinkText does: only a locate
// divides a local-form link's checkout from its file. A link this checkout
// cannot answer for — another repository, an unknown one — has no base row:
// the zero suggestion, no error. The comparison itself reports such a link.
//
// A ref's base is its upstream, else the TRUNK: origin's default branch
// (refs/remotes/origin/HEAD), else a local `main`, else a local `master`. A
// candidate equal to the ref itself is skipped — a branch is not bounded by
// itself. gg has no trunk setting; this order is the whole notion.
func (s *Service) SuggestBase(ctx context.Context, l model.Link, o ResolveOpts) (BaseSuggestion, error) {
	if l.BoundKind() == model.LinkBoundNone {
		return BaseSuggestion{}, nil
	}
	if o.Cwd == nil {
		o.Cwd = s
	}
	top, err := s.TopLevel(ctx)
	if err != nil {
		return BaseSuggestion{}, err
	}
	checkout, rel, err := LocateLink(ctx, l, o)
	if err != nil {
		if ctx.Err() != nil {
			return BaseSuggestion{}, ctx.Err()
		}
		return BaseSuggestion{}, nil
	}
	l.Path = rel
	if !SameCheckout(checkout, top) {
		return BaseSuggestion{}, nil
	}
	switch kind := l.BoundKind(); kind {
	case model.LinkBoundRef:
		base, why, err := s.suggestRefBase(ctx, l.Target.Ref)
		if err != nil {
			return BaseSuggestion{}, err
		}
		return BaseSuggestion{Kind: kind, Base: base, Why: why}, nil
	case model.LinkBoundCommit:
		self, ok, err := s.ResolveRev(ctx, l.Target.Commit)
		if err != nil {
			return BaseSuggestion{}, err
		}
		if !ok {
			return BaseSuggestion{}, fmt.Errorf("%s does not resolve here", l.Target.Commit)
		}
		self = strings.TrimSpace(self)
		parents, err := s.commitParents(ctx, self)
		if err != nil {
			return BaseSuggestion{}, err
		}
		out := BaseSuggestion{Kind: kind, Self: self}
		if len(parents) > 0 { // a root commit has no parent to be relative to
			out.Base, out.Why = parents[0], "parent"
		}
		return out, nil
	}
	return BaseSuggestion{}, nil
}

// suggestRefBase walks the candidates in order and returns the first that is
// not the ref itself. Every probe treats "absent" as absent, never as an
// error: no upstream, no origin/HEAD and no local main are all ordinary.
func (s *Service) suggestRefBase(ctx context.Context, ref string) (base, why string, err error) {
	up, _ := queryQuiet(ctx, s, "upstream:"+ref, func(ctx context.Context) (string, error) {
		return s.repo.UpstreamRef(ctx, ref)
	})
	if err := ctx.Err(); err != nil {
		return "", "", err
	}
	if up = strings.TrimSpace(up); up != "" && up != ref {
		return up, "upstream", nil
	}
	head, _ := queryQuiet(ctx, s, "remote-head:origin", func(ctx context.Context) (string, error) {
		return s.repo.RemoteDefaultBranch(ctx, "origin")
	})
	if err := ctx.Err(); err != nil {
		return "", "", err
	}
	if head = strings.TrimSpace(head); head != "" && head != ref {
		return head, "trunk", nil
	}
	for _, name := range []string{"main", "master"} {
		if name == ref {
			continue
		}
		_, ok, err := s.ResolveRev(ctx, "refs/heads/"+name)
		if err != nil {
			return "", "", err
		}
		if ok {
			return name, "trunk", nil
		}
	}
	return "", "", nil
}
