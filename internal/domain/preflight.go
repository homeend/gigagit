package domain

import (
	"context"
	"fmt"
	"maps"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/preflight"
)

// ErrFeatureDisabled is returned by a query or command whose feature preflight
// turned off. Frontends render it; they never re-derive the decision.
type ErrFeatureDisabled struct {
	ID     string // English feature id
	Reason string // rendered English prose (CLI/MCP/web: English by design)
	// ReasonFormat/ReasonArgs are the SAME prose unrendered, so a localized
	// frontend (the TUI) can put the format through i18n.T instead of
	// printing Error()'s English. Error() itself stays English: it is the
	// agent-facing protocol value.
	ReasonFormat string
	ReasonArgs   []any
}

func (e *ErrFeatureDisabled) Error() string {
	return fmt.Sprintf("the %s feature is unavailable in this repository: %s", e.ID, e.Reason)
}

// ErrRequiredFeatureUnsatisfiable is returned by PreflightRequired when a
// Required feature cannot be satisfied. Frontends without a UI to render a
// Verdict (CLI, MCP, web) print Error() to stderr and exit — the same
// message the TUI's pre-launch gate composes from the same verdict, minus
// i18n (CLI/MCP/web prose stays English by design).
type ErrRequiredFeatureUnsatisfiable struct {
	ID     string // English feature id
	Reason string // rendered English prose naming what failed
}

func (e *ErrRequiredFeatureUnsatisfiable) Error() string {
	return fmt.Sprintf("gg cannot start: %s", e.Reason)
}

// preflightProbes gathers everything the resolver may look at: one
// for-each-ref for the markers, one existence probe per store, one git
// version. All reads — preflight never writes.
func (s *Service) preflightProbes(ctx context.Context) (preflight.Probes, map[string]int, error) {
	formats, err := s.repo.StoreFormats(ctx)
	if err != nil {
		return preflight.Probes{}, nil, err
	}
	return s.probesFrom(ctx, formats)
}

// probesFrom completes the probe set from a marker map the caller already
// read, so the cached marker snapshot is EXACTLY the map Resolve saw — and a
// marker-change re-resolve does not pay a second for-each-ref.
func (s *Service) probesFrom(ctx context.Context, formats map[string]int) (preflight.Probes, map[string]int, error) {
	ver, err := s.repo.GitVersion(ctx)
	if err != nil {
		return preflight.Probes{}, nil, err
	}

	versionRefs, err := s.repo.ForEachRef(ctx, git.VersionRefPrefix)
	if err != nil {
		return preflight.Probes{}, nil, err
	}

	return preflight.Probes{
		Stores: map[string]preflight.StoreProbe{
			StoreVersions: {Format: formats[StoreVersions], HasData: len(versionRefs) > 0},
		},
		GitVersion: ver,
	}, formats, nil
}

// Preflight resolves every declared feature against this repository.
//
// The verdicts are cached, but the cache is validated against the repository
// on EVERY call: several gg processes (a TUI, gg web, an agent's gg mcp) share
// one repo as peers, coordinated only by the process-local repogate, so a
// migration run in one process must not leave the others answering from a
// snapshot taken at their own startup. A long-lived `gg mcp` server never
// re-roots, and the ruling that MCP simply reports a gated feature as
// unavailable only holds if "unavailable" describes the CURRENT repo.
//
// Store formats are one cheap for-each-ref, so the validation is just that
// read: markers unchanged → serve the cache; changed → re-resolve fully from
// the markers just read. Deliberately synchronous and obvious — no TTL, no
// watcher, no background refresher.
//
// Fail open, as everywhere else here: if the marker re-read ERRORS, the cached
// verdicts are served unchanged. A transient git error must never flip a
// feature off or fail a query.
func (s *Service) Preflight(ctx context.Context) ([]preflight.Verdict, error) {
	s.preflightMu.Lock()
	defer s.preflightMu.Unlock()
	var formats map[string]int
	haveFormats := false
	if s.preflightDone {
		cur, err := s.repo.StoreFormats(ctx)
		if err != nil {
			return s.preflightOut, nil // fail open: keep the cache
		}
		if maps.Equal(cur, s.preflightMarks) {
			return s.preflightOut, nil
		}
		// Another process changed a store marker underneath us. Re-resolve
		// every probe against the markers we just read.
		formats, haveFormats = cur, true
	}
	var p preflight.Probes
	var err error
	if haveFormats {
		p, formats, err = s.probesFrom(ctx, formats)
	} else {
		p, formats, err = s.preflightProbes(ctx)
	}
	if err != nil {
		s.preflightDone, s.preflightOut, s.preflightMarks = false, nil, nil
		return nil, err
	}
	s.preflightOut = preflight.Resolve(Features(), p)
	s.preflightMarks = formats
	s.preflightDone = true
	return s.preflightOut, nil
}

// PreflightRequired resolves ONLY the Required features and reports whether
// gg must refuse to start. It is deliberately cheaper than Preflight: when
// none of the Required features have a requirement that needs store probes
// (today's case — core needs only GitVersion, which never touches the
// repository), it pays exactly one git invocation instead of the full
// probe set's three. It is used by frontends with no pre-UI gate of their
// own (CLI, MCP, web) — the TUI already gates via the full Preflight/
// Preflight.go pre-launch flow and does not call this.
//
// This NEVER populates the cached preflightOut/preflightDone/preflightMarks
// fields, even when it happens to resolve the full probe set: those fields
// exist for FeatureEnabled/Preflight, and a partial-probe resolve cached
// there would make a later FeatureEnabled("versions") answer from verdicts
// computed with an empty Probes.Stores, where a store with data but no
// marker reads as "no data" — Satisfied — silently opening a gate that
// should have stayed shut. A single extra `git version` call is cheap enough
// that caching this result is not worth that risk.
//
// Fails open on a probe error, exactly like FeatureEnabled: a transient git
// failure must never keep gg from starting.
func (s *Service) PreflightRequired(ctx context.Context) error {
	required := preflight.RequiredFeatures(Features())
	if len(required) == 0 {
		return nil
	}

	var verdicts []preflight.Verdict
	if preflight.NeedsStoreProbes(required) {
		// A Required feature needs store data to resolve correctly — fall
		// back to the full probe set rather than fabricate empty stores.
		full, _, err := s.preflightProbes(ctx)
		if err != nil {
			return nil // fail open
		}
		verdicts = preflight.Resolve(required, full)
	} else {
		ver, err := s.repo.GitVersion(ctx)
		if err != nil {
			return nil // fail open
		}
		verdicts = preflight.Resolve(required, preflight.Probes{GitVersion: ver})
	}

	for _, v := range verdicts {
		if v.State == preflight.Unsatisfiable {
			return &ErrRequiredFeatureUnsatisfiable{
				ID:     v.Feature.ID,
				Reason: fmt.Sprintf(v.Reason.Format, v.Reason.Args...),
			}
		}
	}
	return nil
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
	reason, format := "", ""
	var args []any
	if vs, err := s.Preflight(ctx); err == nil {
		for _, v := range vs {
			if v.Feature.ID == id && v.Reason.Format != "" {
				reason = fmt.Sprintf(v.Reason.Format, v.Reason.Args...)
				format, args = v.Reason.Format, v.Reason.Args
			}
		}
	}
	return &ErrFeatureDisabled{ID: id, Reason: reason, ReasonFormat: format, ReasonArgs: args}
}

// PendingMigration is one repairable feature, already resolved to the refs it
// will discard and the English consequence prose to show before consent.
type PendingMigration struct {
	Feature     string
	Store       string
	From, To    int
	Consequence string // rendered English (CLI and web print this as-is)
	// ConsequenceFormat/ConsequenceArgs are the same prose UNRENDERED. The
	// consent screen is the one surface the spec's Prose section names, so the
	// TUI must be able to translate it: it puts the format through i18n.T the
	// way renderVerdictReason does for a Verdict's Reason. Pre-formatting it
	// here would make that impossible.
	ConsequenceFormat string
	ConsequenceArgs   []any
	Refs              []string
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
		consequence, format := "", ""
		var args []any
		if mig.Describe != nil {
			t := mig.Describe()
			consequence = fmt.Sprintf(t.Format, t.Args...)
			format, args = t.Format, t.Args
		}
		out = append(out, PendingMigration{
			Feature:           v.Feature.ID,
			Store:             mig.Store,
			From:              mig.From,
			To:                mig.To,
			Consequence:       consequence,
			ConsequenceFormat: format,
			ConsequenceArgs:   args,
			Refs:              refs,
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
	s.preflightDone, s.preflightOut, s.preflightMarks = false, nil, nil
	s.preflightMu.Unlock()
	return nil
}
