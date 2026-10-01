package web

import (
	"context"
	"strconv"
	"strings"
	"sync"

	"github.com/homeend/gigagit/internal/agentdocs"
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
	mu        sync.Mutex
	svc       *domain.Service
	root, top string
}

// docsRoot is the root the served worktree's notes are filed under.
func (s *Server) docsRoot(ctx context.Context) string {
	root, _ := s.docsDirs(ctx)
	return root
}

// docsDirs is docsRoot plus the toplevel on disk it keys (an overview's
// file anchors are checked there). The git call runs OUTSIDE the cache's
// lock (it may wait behind a running op), and only a successful answer is
// cached: the fallback — the directory the service was opened at — holds
// for this call alone.
func (s *Server) docsDirs(ctx context.Context) (root, top string) {
	svc := s.service()
	s.rootc.mu.Lock()
	if s.rootc.svc == svc {
		root, top = s.rootc.root, s.rootc.top
		s.rootc.mu.Unlock()
		return root, top
	}
	s.rootc.mu.Unlock()
	top, err := svc.TopLevel(ctx)
	if err != nil {
		return domain.CheckoutKey(svc.Root()), svc.Root()
	}
	root = domain.CheckoutKey(top)
	s.rootc.mu.Lock()
	s.rootc.svc, s.rootc.root, s.rootc.top = svc, root, top
	s.rootc.mu.Unlock()
	return root, top
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
// lacks (in the background, pinned), pin exactly the noted ones, list every
// overview under the store's id (pinned) and drop the ones that left it, and
// tell the tabs — through fanOut, which the op gate never drops — naming any
// plain file an addition pushed out over the cap. Passes take turns: each
// reads the store inside its turn, so the last leaves the store's pins.
func (s *Server) followDocs() {
	s.followMu.Lock()
	defer s.followMu.Unlock()
	wt := s.service().Root()
	root := s.docsRoot(context.Background())
	paths := s.docs.NotedPaths(root)
	noted := make(map[string]bool, len(paths))
	var evicted []string
	for _, e := range s.evicted { // what a steer's own listing pushed out (listDocs)
		if e.wt == wt { // a repo switch since: that list is not the tabs' now
			evicted = append(evicted, e.path)
		}
	}
	s.evicted = nil
	for _, p := range paths {
		noted[p] = true
		k := ofKey{Src: "worktree", Path: p}
		f, ev, added := s.ofs.ensureOpen(wt, k, true)
		if added {
			s.baseline(wt, f.ID, k)
		}
		if ev != "" {
			evicted = append(evicted, ev)
		}
	}
	s.ofs.setPinned(wt, noted)
	inStore := map[string]bool{}
	stamps := map[string]string{}
	for _, o := range s.docs.Overviews(root) {
		inStore[o.ID] = true
		stamps[o.ID] = s.docs.OverviewStamp(o.ID)
		if _, ev, _ := s.ofs.ensureOpenID(wt, overviewKey(o), o.ID, o.Title); ev != "" {
			evicted = append(evicted, ev)
		}
	}
	var closed []string
	for _, id := range s.ofs.ids(wt, "overview") {
		if !inStore[id] && s.ofs.removeID(wt, id) {
			closed = append(closed, id)
		}
	}
	if h := s.liveHubRef(); h != nil {
		h.fanOut(liveMsg{Changed: []string{}, Reason: "agentdocs", Files: s.ofList(wt), Closed: closed,
			Evicted: strings.Join(evicted, ", "), Cap: capIf(len(evicted) > 0), Stamps: stamps})
	}
}

// ofEvicted is a path a steer's listing pushed out of worktree wt's list.
type ofEvicted struct{ wt, path string }

// listDocs runs add — a steer's store write and the entry it lists — in
// the follow passes' turn, so no pass lists that entry first (wt is the
// list add lists in — the caller's, read before), then runs a
// pass that tells the tabs, naming what add's listing pushed out over the
// cap (add returns it; the agent's reply names it too).
func (s *Server) listDocs(wt string, add func() (evicted string)) {
	s.followMu.Lock()
	if ev := add(); ev != "" {
		s.evicted = append(s.evicted, ofEvicted{wt: wt, path: ev})
	}
	s.followMu.Unlock()
	s.followDocs()
}

// overviewKey is an overview's list key: the TUI's display name.
func overviewKey(o agentdocs.Overview) ofKey {
	return ofKey{Src: "overview", Path: "overview-" + strconv.FormatInt(o.Seq, 10) + ".md"}
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
