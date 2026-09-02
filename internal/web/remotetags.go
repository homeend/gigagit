package web

import (
	"context"
	"sync"
	"time"

	"github.com/homeend/gigagit/internal/domain"
)

// The TUI's Tags panel appends ▲ to every tag the default remote is known to
// have (Model.remoteTagNames, fed by its background remote-tag lane). The
// browser had no such memory: the interval lane listed the remote and threw
// the answer away, so the sidebar could not tell a pushed tag from a local
// one — and offered "delete from remote" on tags the remote never had.
//
// remoteTagCache is that memory, server-side: the last listing taken for the
// CURRENT service. It is keyed to the service pointer, so a re-root starts
// unknown again rather than showing the previous repo's answer. nil names =
// never listed; a row then carries no verdict at all (the TUI shows nothing
// for an unknown, never a wrong "local only").
type remoteTagCache struct {
	mu     sync.Mutex
	svc    *domain.Service
	names  map[string]bool
	kicked bool // the one background listing handleTags starts per service
}

// remoteTagListBudget bounds the background listing handleTags kicks off.
// The interval lane and the pre-push check bring their own budgets.
const remoteTagListBudget = 30 * time.Second

// remoteTagNames returns the last listing for svc and whether one exists.
func (s *Server) remoteTagNames(svc *domain.Service) (map[string]bool, bool) {
	s.rt.mu.Lock()
	defer s.rt.mu.Unlock()
	if s.rt.svc != svc || s.rt.names == nil {
		return nil, false
	}
	return s.rt.names, true
}

// storeRemoteTags records a fresh listing for svc. nil (a failed or timed-out
// lookup) records nothing — the previous answer stays, as in the TUI.
func (s *Server) storeRemoteTags(svc *domain.Service, names map[string]bool) {
	if names == nil {
		return
	}
	s.rt.mu.Lock()
	defer s.rt.mu.Unlock()
	if s.rt.svc != svc {
		s.rt.svc = svc
		s.rt.kicked = false
	}
	s.rt.names = names
}

// markRemoteTag folds one op's outcome into the listing — the TUI's
// pendingRemoteTagSet/Unset, applied on success. With no listing yet there is
// nothing to fold into (a one-name map would read as "every other tag is
// local"), so the op instead earns a fresh listing.
func (s *Server) markRemoteTag(svc *domain.Service, name string, present bool) {
	s.rt.mu.Lock()
	known := s.rt.svc == svc && s.rt.names != nil
	if known {
		if present {
			s.rt.names[name] = true
		} else {
			delete(s.rt.names, name)
		}
	}
	s.rt.mu.Unlock()
	if !known {
		s.kickRemoteTags(svc, true)
	}
}

// kickRemoteTags starts ONE background listing for svc (again when force is
// set), bounded, quiet, and off the request path: the tags payload answers
// now without a verdict, and the hub's "tags" event brings the ▲s in once
// the remote has answered — the TUI's initial "⟳ remote tags…" lane.
func (s *Server) kickRemoteTags(svc *domain.Service, force bool) {
	s.rt.mu.Lock()
	if s.rt.svc != svc {
		s.rt.svc = svc
		s.rt.names = nil
		s.rt.kicked = false
	}
	if s.rt.kicked && !force {
		s.rt.mu.Unlock()
		return
	}
	s.rt.kicked = true
	s.rt.mu.Unlock()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), remoteTagListBudget)
		defer cancel()
		s.refreshRemoteTags(ctx, svc)
	}()
}

// refreshRemoteTags lists the remote once, stores the answer, and tells the
// browser the tags changed so the markers render. Errors are the listing's
// own business (offline, no remote): nothing is stored and nothing is said.
func (s *Server) refreshRemoteTags(ctx context.Context, svc *domain.Service) {
	names, err := svc.RemoteTagsFresh(ctx)
	if err != nil || names == nil {
		return
	}
	s.storeRemoteTags(svc, names)
	if h := s.liveHubRef(); h != nil {
		h.emit(liveMsg{Changed: []string{"tags"}, Reason: "remote-tags"})
	}
}
