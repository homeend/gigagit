package web

// gg web as a session-docs READER (spec 2026-10-18-gg-session-docs §4,
// §6.4). A standalone server attaches the served worktree's root to that
// worktree's docs directory — under the same session dir the steer inbox
// lives in — at Start and on every re-root, and closes its store at Close.
// A HOSTED page's store is agentdocs.Shared(), which the terminal attaches
// and closes: the page never attaches, detaches or closes anything.

import (
	"context"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/steer"
)

// docsDirFor is the docs directory of the worktree at top whose git common
// dir is commonDir; "" when the session dir cannot be resolved (then
// nothing attaches). The directory does not depend on [ui] steering: the
// TUI attaches regardless, and so does the page.
func (s *Server) docsDirFor(commonDir, top string) string {
	if s.docsDirOf != nil {
		return s.docsDirOf(commonDir, top) // tests
	}
	return steer.DocsDir(config.SessionSteerDir(commonDir, top))
}

// attachDocs binds the served worktree's root to its docs directory. Only a
// standalone server; only on a resolved toplevel (docsDirs's fallback key
// would file the directory under the wrong root). A directory that cannot
// be made or read attaches nothing and says nothing: the page runs without
// session rows.
func (s *Server) attachDocs(ctx context.Context) {
	if s.hosted {
		return
	}
	svc := s.service()
	top, err := svc.TopLevel(ctx)
	if err != nil {
		return
	}
	cd, err := svc.GitCommonDir(ctx)
	if err != nil {
		return
	}
	root, _ := s.docsDirs(ctx)
	if dir := s.docsDirFor(cd, top); dir != "" {
		_ = s.docs.Attach(root, top, dir)
	}
}

// detachDocs forgets the served worktree's documents (a re-root: called
// BEFORE the service swap, while docsDirs still keys the old one). Only a
// standalone server.
func (s *Server) detachDocs(ctx context.Context) {
	if s.hosted {
		return
	}
	if root := s.docsRoot(ctx); root != "" {
		s.docs.Detach(root)
	}
}
