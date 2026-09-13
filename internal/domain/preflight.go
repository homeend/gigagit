package domain

import (
	"context"
	"fmt"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/preflight"
)

// ErrFeatureDisabled is returned by a query or command whose feature preflight
// turned off. Frontends render it; they never re-derive the decision.
type ErrFeatureDisabled struct {
	ID     string // English feature id
	Reason string // rendered English prose
}

func (e *ErrFeatureDisabled) Error() string {
	return fmt.Sprintf("the %s feature is unavailable in this repository: %s", e.ID, e.Reason)
}

// preflightProbes gathers everything the resolver may look at: one
// for-each-ref for the markers, one existence probe per store, one git
// version. All reads — preflight never writes.
func (s *Service) preflightProbes(ctx context.Context) (preflight.Probes, error) {
	formats, err := s.repo.StoreFormats(ctx)
	if err != nil {
		return preflight.Probes{}, err
	}
	ver, err := s.repo.GitVersion(ctx)
	if err != nil {
		return preflight.Probes{}, err
	}

	versionRefs, err := s.repo.ForEachRef(ctx, git.VersionRefPrefix)
	if err != nil {
		return preflight.Probes{}, err
	}

	return preflight.Probes{
		Stores: map[string]preflight.StoreProbe{
			StoreVersions: {Format: formats[StoreVersions], HasData: len(versionRefs) > 0},
		},
		GitVersion: ver,
	}, nil
}

// Preflight resolves every declared feature against this repository. The
// result is cached for the Service's lifetime; reRoot builds a fresh Service,
// so switching repos re-resolves automatically.
func (s *Service) Preflight(ctx context.Context) ([]preflight.Verdict, error) {
	s.preflightMu.Lock()
	defer s.preflightMu.Unlock()
	if s.preflightDone {
		return s.preflightOut, nil
	}
	p, err := s.preflightProbes(ctx)
	if err != nil {
		return nil, err
	}
	s.preflightOut = preflight.Resolve(Features(), p)
	s.preflightDone = true
	return s.preflightOut, nil
}

// FeatureEnabled reports whether id may be used. An unknown id is never
// enabled. A probe failure is treated as "enabled" so a transient git error
// cannot silently switch features off.
func (s *Service) FeatureEnabled(ctx context.Context, id string) bool {
	vs, err := s.Preflight(ctx)
	if err != nil {
		return true
	}
	for _, v := range vs {
		if v.Feature.ID == id {
			return v.State == preflight.Satisfied
		}
	}
	return false
}

// FeatureDisabledError builds the typed error an owning query returns. Returns
// nil when the feature is available.
func (s *Service) FeatureDisabledError(ctx context.Context, id string) error {
	if s.FeatureEnabled(ctx, id) {
		return nil
	}
	reason := ""
	if vs, err := s.Preflight(ctx); err == nil {
		for _, v := range vs {
			if v.Feature.ID == id && v.Reason.Format != "" {
				reason = fmt.Sprintf(v.Reason.Format, v.Reason.Args...)
			}
		}
	}
	return &ErrFeatureDisabled{ID: id, Reason: reason}
}
