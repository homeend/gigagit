package web

import (
	"context"
	"sync"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/steer"
)

// The agent-docs store, followed (spec 2026-10-01-agent-docs-web): the
// page's list holds every noted working-tree file of the served worktree,
// pinned, and the tabs hear every change as "agentdocs". The store is
// shared with the TUI when the TUI hosts this page.

// docsRootCache keys the store by the served worktree's TOPLEVEL — the
// TUI's key — computed once per service (svc.Root() is the directory the
// service was opened at, which may be a subdirectory).
type docsRootCache struct {
	mu   sync.Mutex
	svc  *domain.Service
	root string
}

// docsRoot is the root the served worktree's notes are filed under. The git
// call runs OUTSIDE the cache's lock (it may wait behind a running op), and
// only a successful answer is cached: the fallback — the directory the
// service was opened at — holds for this call alone.
func (s *Server) docsRoot(ctx context.Context) string {
	svc := s.service()
	s.rootc.mu.Lock()
	if s.rootc.svc == svc {
		root := s.rootc.root
		s.rootc.mu.Unlock()
		return root
	}
	s.rootc.mu.Unlock()
	top, err := svc.TopLevel(ctx)
	if err != nil {
		return domain.CheckoutKey(svc.Root())
	}
	root := domain.CheckoutKey(top)
	s.rootc.mu.Lock()
	s.rootc.svc, s.rootc.root = svc, root
	s.rootc.mu.Unlock()
	return root
}

// ofList is wt's open files with each working-tree file's note count.
func (s *Server) ofList(wt string) []steer.OpenFile {
	l := s.ofs.list(wt)
	root := s.docsRoot(context.Background())
	for i := range l {
		if l[i].Source == "worktree" {
			l[i].Notes = s.docs.NoteCount(root, l[i].Path)
		}
	}
	return l
}

// followDocs is one pass over the store: list every noted file the page
// lacks (in the background, pinned), pin exactly the noted ones, and tell
// the tabs — through fanOut, which the op gate never drops.
func (s *Server) followDocs() {
	wt := s.service().Root()
	paths := s.docs.NotedPaths(s.docsRoot(context.Background()))
	noted := make(map[string]bool, len(paths))
	for _, p := range paths {
		noted[p] = true
		k := ofKey{Src: "worktree", Path: p}
		if f, _, added := s.ofs.ensureOpen(wt, k, true); added {
			s.baseline(wt, f.ID, k)
		}
	}
	s.ofs.setPinned(wt, noted)
	if h := s.liveHubRef(); h != nil {
		h.fanOut(liveMsg{Changed: []string{}, Reason: "agentdocs", Files: s.ofList(wt)})
	}
}

// startDocsFollow runs a pass now and after every store change until Close
// (Start calls it after startOpenFilesWatch, which makes ofStop).
func (s *Server) startDocsFollow() {
	ch, cancel := s.docs.Subscribe()
	stop := s.ofStop
	go func() {
		defer cancel()
		s.followDocs()
		for {
			select {
			case <-stop:
				return
			case <-s.closing:
				return
			case <-ch:
				s.followDocs()
			}
		}
	}()
}
