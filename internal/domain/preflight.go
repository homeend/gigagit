package domain

import (
	"context"
	"fmt"

	"github.com/homeend/gigagit/internal/engine"
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

// PendingMigration is one repairable feature, already resolved to the refs it
// will discard and the English consequence prose to show before consent.
type PendingMigration struct {
	Feature     string
	Store       string
	From, To    int
	Consequence string
	Refs        []string
}

// storeRefs lists the refs a store owns, so a migration can report exactly
// what it will remove.
func (s *Service) storeRefs(ctx context.Context, store string) ([]string, error) {
	if store != StoreVersions {
		return nil, nil
	}
	infos, err := s.repo.ForEachRef(ctx, git.VersionRefPrefix)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(infos))
	for _, info := range infos {
		out = append(out, info.Ref)
	}
	return out, nil
}

// PendingMigrations lists every Repairable feature. Nothing is applied here.
func (s *Service) PendingMigrations(ctx context.Context) ([]PendingMigration, error) {
	vs, err := s.Preflight(ctx)
	if err != nil {
		return nil, err
	}
	var out []PendingMigration
	for _, v := range vs {
		if v.State != preflight.Repairable || v.Feature.Migrate == nil {
			continue
		}
		mig := v.Feature.Migrate
		refs, err := s.storeRefs(ctx, mig.Store)
		if err != nil {
			return nil, err
		}
		consequence := ""
		if mig.Describe != nil {
			t := mig.Describe()
			consequence = fmt.Sprintf(t.Format, t.Args...)
		}
		out = append(out, PendingMigration{
			Feature:     v.Feature.ID,
			Store:       mig.Store,
			From:        mig.From,
			To:          mig.To,
			Consequence: consequence,
			Refs:        refs,
		})
	}
	return out, nil
}

// RunMigration applies one pending migration through Execute, then drops the
// cached verdicts so the next Preflight sees the new state.
func (s *Service) RunMigration(ctx context.Context, m PendingMigration) error {
	op := engine.ApplyMigration{Feature: m.Feature, Store: m.Store, To: m.To, Refs: m.Refs}
	if _, err := s.Execute(ctx, op, nil, nil); err != nil {
		return err
	}
	s.preflightMu.Lock()
	s.preflightDone, s.preflightOut = false, nil
	s.preflightMu.Unlock()
	return nil
}
