// Package preflight resolves what a gg feature needs against what a repository
// actually offers. It is a DAG leaf: stdlib only. It never runs git — callers
// hand it probe results and it returns verdicts, so the whole decision table is
// testable with plain values.
package preflight

import "fmt"

// Criticality says what happens when a feature cannot be satisfied.
type Criticality int

const (
	Optional Criticality = iota // gg runs with the feature disabled
	Required                    // gg explains and exits
)

// State is a feature's verdict. The zero value is Satisfied, and the ordering
// is significant: Resolve keeps the WORST state across a feature's
// requirements, so Unsatisfiable must sort above Repairable.
type State int

const (
	Satisfied State = iota
	Repairable
	Unsatisfiable
)

func (s State) String() string {
	switch s {
	case Satisfied:
		return "satisfied"
	case Repairable:
		return "repairable"
	case Unsatisfiable:
		return "unsatisfiable"
	}
	return "unknown"
}

// Fit is how one requirement sits against the probes.
type Fit int

const (
	FitOK Fit = iota
	FitTooOld
	FitTooNew
)

// Text is consent/reason prose. Deliberately the same shape as engine.Msg;
// domain converts at the boundary. preflight cannot import engine without
// breaking the DAG.
type Text struct {
	Format string
	Args   []any
}

// StoreProbe is what the resolver knows about one gg-owned store. Format is
// the marker value; 0 means NO MARKER, which is resolved against HasData —
// data without a marker is the pre-marker era, i.e. format 1.
type StoreProbe struct {
	Format  int
	HasData bool
}

// LegacyProbe is what the resolver knows about one superseded store. Present
// means its data file is still on THIS machine.
//
// Deliberately not a StoreProbe: a StoreProbe's Format comes from a git-ref
// marker inside .git, which is the wrong scope for a machine-local file — one
// .git opened from two environments (a Windows checkout reached from WSL, say)
// is ONE marker and TWO data directories, so a marker-based verdict would
// report one side's migration as the other side's fact and orphan the second
// file permanently.
type LegacyProbe struct{ Present bool }

// Probes is everything the resolver is allowed to look at.
type Probes struct {
	Stores     map[string]StoreProbe
	Legacy     map[string]LegacyProbe
	GitVersion [3]int
	Forge      *ForgeProbe // nil = not probed (yet)
}

// ForgeProbe is what detection learned about a code forge for this repo.
// Provider is the usable provider's name ("" = none); Err says why not.
type ForgeProbe struct {
	Provider string
	Err      string
}

// Requirement is one thing a feature needs.
type Requirement interface {
	Fit(p Probes) Fit
	Reason(p Probes) Text
	// Remedy names what would fix an Unsatisfiable verdict from OUTSIDE gg
	// (upgrade git, upgrade gg) — never a `gg migrate` line, since that path
	// only exists for Repairable verdicts.
	Remedy(p Probes) Text
	// RepairStore names the store a migration could repair, or "" when nothing
	// can (a git version is never migratable).
	RepairStore() string
	// NeedsStoreProbes reports whether this requirement's Fit reads
	// Probes.Stores at all. DataFormat is true; GitVersion is false. A
	// Required-only resolve (domain.PreflightRequired) uses this to decide
	// whether it can skip the for-each-ref store probes entirely — the leaf
	// stays authoritative about what each requirement kind needs, rather than
	// domain type-switching on Requirement implementations.
	NeedsStoreProbes() bool
}

// DataFormat requires a store to sit within an inclusive format range. The
// range — rather than one expected number — exists for the above-Max case: an
// older gg cannot downgrade data a newer gg wrote, so it must disable the
// feature rather than touch it.
type DataFormat struct {
	Store    string
	Min, Max int
}

func (d DataFormat) found(p Probes) (int, bool) {
	s := p.Stores[d.Store]
	if s.Format == 0 {
		if !s.HasData {
			return 0, false // nothing stored yet: nothing to check
		}
		return 1, true // legacy: data without a marker is format 1 by definition
	}
	return s.Format, true
}

func (d DataFormat) Fit(p Probes) Fit {
	n, ok := d.found(p)
	if !ok {
		return FitOK
	}
	switch {
	case n < d.Min:
		return FitTooOld
	case n > d.Max:
		return FitTooNew
	}
	return FitOK
}

func (d DataFormat) Reason(p Probes) Text {
	n, _ := d.found(p)
	return Text{
		Format: "the %s store is at format %d; this build needs %d-%d",
		Args:   []any{d.Store, n, d.Min, d.Max},
	}
}

// Remedy names the same "upgrade gg" fix regardless of direction, but in
// practice only ever surfaces for the above-Max case (an old gg reading data
// a newer gg wrote): a below-Min miss is Repairable — not Unsatisfiable — as
// long as the feature declares a matching Migrate; Resolve only reaches this
// method on a state that outranks Repairable, i.e. above-Max, or a below-Min
// miss with no matching Migrate declared.
func (d DataFormat) Remedy(p Probes) Text {
	return Text{Format: "Upgrade gg to use this feature."}
}

func (d DataFormat) RepairStore() string { return d.Store }

func (d DataFormat) NeedsStoreProbes() bool { return true }

// LegacyStore requires that a superseded store's data has been absorbed —
// i.e. that its file is gone from this machine.
//
// There are no format NUMBERS here, unlike DataFormat: the old store and the
// new one are different FILES, so "is the legacy file still present" is the
// whole question. That also makes the check machine-local, which a git-ref
// format marker cannot be (see LegacyProbe).
type LegacyStore struct{ Store string }

func (l LegacyStore) Fit(p Probes) Fit {
	if p.Legacy[l.Store].Present {
		return FitTooOld
	}
	return FitOK
}

func (l LegacyStore) Reason(p Probes) Text {
	return Text{Format: "the %s store still holds data from an older layout", Args: []any{l.Store}}
}

func (l LegacyStore) Remedy(p Probes) Text {
	return Text{Format: "Upgrade gg to use this feature."}
}

func (l LegacyStore) RepairStore() string { return l.Store }

// NeedsStoreProbes is FALSE: this requirement reads Probes.Legacy, never
// Probes.Stores, so it never obliges a caller to run the git-ref probes.
func (l LegacyStore) NeedsStoreProbes() bool { return false }

// GitVersion requires a minimum git binary version. Never repairable.
type GitVersion struct {
	Min [3]int
}

func (g GitVersion) Fit(p Probes) Fit {
	if less(p.GitVersion, g.Min) {
		return FitTooOld
	}
	return FitOK
}

func (g GitVersion) Reason(p Probes) Text {
	return Text{
		Format: "git %s is installed; this build needs %s or newer",
		Args:   []any{verString(p.GitVersion), verString(g.Min)},
	}
}

func (g GitVersion) Remedy(p Probes) Text {
	return Text{
		Format: "Upgrade to git %s or newer to use this feature.",
		Args:   []any{verString(g.Min)},
	}
}

func (g GitVersion) RepairStore() string { return "" }

func (g GitVersion) NeedsStoreProbes() bool { return false }

// ForgeUsable requires a forge CLI that can read this repository's pull
// requests. Unprobed counts as unusable: the feature lights up only on a
// positive verdict.
type ForgeUsable struct{}

func (ForgeUsable) Fit(p Probes) Fit {
	if p.Forge != nil && p.Forge.Provider != "" {
		return FitOK
	}
	return FitTooOld
}

func (ForgeUsable) Reason(p Probes) Text {
	if p.Forge == nil || p.Forge.Err == "" {
		return Text{Format: "no forge CLI can read this repository's pull requests"}
	}
	return Text{Format: "no forge CLI can read this repository's pull requests: %s", Args: []any{p.Forge.Err}}
}

func (ForgeUsable) Remedy(Probes) Text {
	return Text{Format: "install the GitHub CLI and run `gh auth login`, then restart gg"}
}

func (ForgeUsable) RepairStore() string    { return "" }
func (ForgeUsable) NeedsStoreProbes() bool { return false }

func less(a, b [3]int) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}

func verString(v [3]int) string { return fmt.Sprintf("%d.%d.%d", v[0], v[1], v[2]) }

// Migration repairs one store. Describe returns the consequence prose shown
// before consent.
type Migration struct {
	Store    string
	From, To int
	// Action names the BODY domain will construct for this migration — an
	// English protocol value ("discard-refs", "convert-previews"), not code,
	// so this package stays a stdlib leaf whose whole decision table is
	// testable with plain values.
	Action string
	// Lossless says this migration destroys nothing, so it may run without
	// asking: there is no loss to confess.
	//
	// The polarity is deliberate and the ZERO VALUE IS THE SAFE ONE. Spelled
	// the other way round ("Consent bool"), a migration that simply forgot to
	// declare would destroy a user's data unasked — and the branch-versions
	// migration, the one that really does destroy, declares nothing here.
	// Forgetting must fail towards the consent screen, never past it.
	Lossless bool
	Describe func() Text
}

// Feature is one declared unit of gg behaviour.
type Feature struct {
	ID          string // English protocol value
	Criticality Criticality
	Requires    []Requirement
	Migrate     *Migration // nil when nothing is repairable
	// Silent features never surface a "feature unavailable" notice: their
	// absence is the normal state of a box without the external tool.
	Silent bool
}

// Verdict is a feature's resolved state plus, when not Satisfied, why and
// (for Unsatisfiable) how to fix it. Reason and Remedy always come from the
// SAME requirement — they are set together at the one site that assigns the
// worst state, so they can never disagree.
type Verdict struct {
	Feature Feature
	State   State
	Reason  Text
	Remedy  Text
}

// RequiredFeatures returns the subset of fs whose Criticality is Required.
// gg must explain and exit rather than run with one of these disabled, so a
// caller that only cares about that gate (domain.PreflightRequired) can
// resolve against this narrower list instead of every declared feature.
func RequiredFeatures(fs []Feature) []Feature {
	out := make([]Feature, 0, len(fs))
	for _, f := range fs {
		if f.Criticality == Required {
			out = append(out, f)
		}
	}
	return out
}

// NeedsStoreProbes reports whether any requirement across fs needs
// Probes.Stores populated to resolve correctly. When false, a caller may
// pass a Probes with a nil/empty Stores map (only GitVersion is read) without
// risking a DataFormat requirement silently reading "no data" from an
// UNPROBED store rather than a genuinely empty one.
func NeedsStoreProbes(fs []Feature) bool {
	for _, f := range fs {
		for _, r := range f.Requires {
			if r.NeedsStoreProbes() {
				return true
			}
		}
	}
	return false
}

// Resolve evaluates every feature against the probes. A feature takes the
// worst verdict among its requirements.
func Resolve(fs []Feature, p Probes) []Verdict {
	out := make([]Verdict, 0, len(fs))
	for _, f := range fs {
		v := Verdict{Feature: f, State: Satisfied}
		for _, r := range f.Requires {
			st := Satisfied
			switch r.Fit(p) {
			case FitTooNew:
				st = Unsatisfiable
			case FitTooOld:
				if f.Migrate != nil && r.RepairStore() != "" && f.Migrate.Store == r.RepairStore() {
					st = Repairable
				} else {
					st = Unsatisfiable
				}
			}
			if st > v.State {
				v.State, v.Reason, v.Remedy = st, r.Reason(p), r.Remedy(p)
			}
		}
		out = append(out, v)
	}
	return out
}
