package domain

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"

	"github.com/homeend/gigagit/internal/agentdocs"
	"github.com/homeend/gigagit/internal/model"
)

// A SESSION overview's link (spec 2026-10-28-overview-links-design): the
// local form naming the worktree whose session-docs directory holds it, with
// no address — gg://<abs worktree>?overview=<id>. A permanent overview's
// link always carries its commit, so the missing address is what tells the
// two apart, never the id.

var (
	// ErrOverviewNamesNoFiles refuses a session overview link to a resolver
	// that reads files (diff, note, link text, compare): the link names a
	// document, not a place in the tree.
	ErrOverviewNamesNoFiles = fmt.Errorf("%w: an overview link names no files — read it with gg overview show <link>", model.ErrLink)
	// ErrSessionOverviewGone: the worktree's session docs no longer hold it.
	ErrSessionOverviewGone = errors.New("session overview gone")
)

// sessionOverviewGone is ErrSessionOverviewGone saying which.
func sessionOverviewGone(id string) error {
	return fmt.Errorf("%w: overview %s no longer exists", ErrSessionOverviewGone, id)
}

// SessionOverviewLink is the link to overview id in worktree's session docs.
func SessionOverviewLink(worktree, id string) (string, error) {
	if !agentdocs.ValidID(id) {
		return "", fmt.Errorf("%w: %q is not an overview id", model.ErrLink, id)
	}
	abs := filepath.ToSlash(filepath.Clean(worktree))
	if !model.LinkAbsOK(abs) {
		return "", fmt.Errorf("%w: the worktree path %q contains @, # or ? — no gg link", model.ErrLink, abs)
	}
	return linkTextIn(model.LinkRepo{Abs: abs}, model.Link{
		Target: model.LinkTarget{State: model.StateUnstaged},
		Hint:   model.LinkHint{Kind: model.OverviewHintKind, ID: id},
	})
}

// IsSessionOverviewLink reports whether l names a session overview: a local
// link with an overview hint and nothing else.
func IsSessionOverviewLink(l model.Link) bool {
	return l.IsLocal() && l.Hint.Kind == model.OverviewHintKind && l.Path == "" &&
		hintOnlyTarget(l.Target) && l.Target.State == model.StateUnstaged && l.Line == 0
}

// SessionOverview reports whether r is a resolved session overview link.
func (r Resolved) SessionOverview() bool {
	return r.Hint.Kind == model.OverviewHintKind && r.Commit == ""
}

// resolveSessionOverview resolves a session overview link: the link's path
// IS the checkout (no registry, no cwd), and the overview must be there.
func resolveSessionOverview(ctx context.Context, l model.Link, opts ResolveOpts) (Resolved, error) {
	if opts.SessionDocs == nil {
		return Resolved{}, ErrOverviewNamesNoFiles
	}
	if opts.OpenFn == nil {
		opts.OpenFn = Open
	}
	abs := filepath.FromSlash(l.Repo.Abs)
	notHere := fmt.Errorf("%w: %s is not a checkout here", ErrLinkUnknownRepo, l.Repo.Abs)
	want, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return Resolved{}, notHere
	}
	svc := opts.OpenFn(want)
	// top stays git's own spelling: it is the checkout every consumer keys
	// the session docs by (SessionSteerDir), so the resolve reports the very
	// path whose directory it checked. Only the identity test resolves
	// symlinks (a Windows junction or subst drive spells it otherwise).
	top, err := svc.TopLevel(ctx)
	if err != nil || top == "" {
		return Resolved{}, notHere
	}
	real := top
	if r, err := filepath.EvalSymlinks(top); err == nil {
		real = r
	}
	if !SamePath(real, want) {
		return Resolved{}, notHere
	}
	cd, err := svc.GitCommonDir(ctx)
	if err != nil {
		return Resolved{}, err
	}
	dir := opts.SessionDocs(cd, top)
	if dir == "" {
		return Resolved{}, sessionOverviewGone(l.Hint.ID)
	}
	if _, err := agentdocs.ReadDoc(dir, l.Hint.ID, agentdocs.KindOverview); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Resolved{}, sessionOverviewGone(l.Hint.ID)
		}
		return Resolved{}, fmt.Errorf("overview %s: %w", l.Hint.ID, err)
	}
	return Resolved{
		Checkout: top,
		Addr:     model.FileAddress{State: model.StateUnstaged, Worktree: top},
		Side:     model.NoteSideNew,
		Hint:     l.Hint,
	}, nil
}
