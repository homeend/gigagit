package domain

import (
	"context"
	"path/filepath"
	"strings"
	"time"

	"github.com/homeend/gigagit/internal/linkhist"
)

// LinkHistStatePath overrides the link-history root. "" uses the default XDG
// location. cmd/gg leaves it ""; tests point it at a temp dir.
var LinkHistStatePath string

// UseLinkHistDir points THIS service's link history at dir. Unlike the
// package-level LinkHistStatePath it is per service, so parallel tests (and
// the TUI's, which may not import internal/linkhist to inject a store) can
// each hold their own. It re-arms an already-resolved store: a read and a
// write must never disagree about where the data lives.
func (s *Service) UseLinkHistDir(dir string) {
	s.mu.Lock()
	s.linkHistRoot = dir
	s.linkhist = nil
	s.mu.Unlock()
}

// SetLinkHistStore injects a store (tests).
func (s *Service) SetLinkHistStore(st linkhist.Store) {
	s.mu.Lock()
	s.linkhist = st
	s.mu.Unlock()
}

// linkHistStore resolves (once) the per-repo copied-link history store, keyed
// by git common dir under the XDG state dir. Returns nil (history disabled)
// when no state dir is resolvable — mirroring the search/shelf/bookmark
// posture.
func (s *Service) linkHistStore(ctx context.Context) linkhist.Store {
	s.mu.Lock()
	if s.linkhist != nil {
		st := s.linkhist
		s.mu.Unlock()
		return st
	}
	root := s.linkHistRoot
	s.mu.Unlock()

	if root == "" {
		root = LinkHistStatePath
	}
	if root == "" {
		base := stateBaseDir("linkhist")
		if base == "" {
			return nil
		}
		key := "unknown"
		if cd, err := s.GitCommonDir(ctx); err == nil {
			key = repoKey(strings.TrimSpace(cd)) // reuse shelfstore.go's repoKey
		}
		root = filepath.Join(base, key)
	}
	st := linkhist.NewFileStore(root)
	s.mu.Lock()
	s.linkhist = st
	s.mu.Unlock()
	return st
}

// RecordLink appends a copied link. BEST-EFFORT: a nil store (no state home)
// or a write error is silently ignored, exactly as RecordSearch is — a
// history that cannot be written must never fail the copy the user asked
// for.
func (s *Service) RecordLink(ctx context.Context, link, desc string) {
	st := s.linkHistStore(ctx)
	if st == nil {
		return
	}
	_ = st.Record(linkhist.Entry{Link: link, Desc: desc, Created: time.Now().UTC().Format(time.RFC3339)})
}

// LinkHistory returns the ring newest-first; nil on any failure (history
// disabled, or a read error).
func (s *Service) LinkHistory(ctx context.Context) []linkhist.Entry {
	st := s.linkHistStore(ctx)
	if st == nil {
		return nil
	}
	es, err := st.List()
	if err != nil {
		return nil
	}
	return es
}
